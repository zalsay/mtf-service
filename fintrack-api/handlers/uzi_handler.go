package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type UZIHandler struct {
	uziService           *services.UZIService
	aiModelConfigService *services.AIModelConfigService
}

var uziStatusUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewUZIHandler(uziService *services.UZIService, aiModelConfigService *services.AIModelConfigService) *UZIHandler {
	return &UZIHandler{
		uziService:           uziService,
		aiModelConfigService: aiModelConfigService,
	}
}

func (h *UZIHandler) Health(c *gin.Context) {
	status, body, err := h.uziService.Health()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *UZIHandler) Analyze(c *gin.Context) {
	var req models.UZIAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	userIDInt, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	aiConfig, err := h.aiModelConfigService.GetByUserID(userIDInt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load AI model config"})
		return
	}
	if !services.IsAIModelConfigReady(aiConfig) {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": services.AIModelConfigRequiredMsg})
		return
	}

	status, body, err := h.uziService.EnqueueAnalyze(&req, aiConfig)
	if err != nil {
		h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
			Status:  services.UZIAnalyzeStatusFailed,
			Ticker:  strings.TrimSpace(req.Ticker),
			Summary: err.Error(),
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if status >= http.StatusBadRequest {
		h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
			Status:  services.UZIAnalyzeStatusFailed,
			Ticker:  strings.TrimSpace(req.Ticker),
			Summary: firstNonEmptyHandlerString(stringValueHandler(body["error"]), stringValueHandler(body["message"]), "研报生成入队失败"),
		})
		c.JSON(status, body)
		return
	}

	jobID := stringValueHandler(body["job_id"])
	h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
		Status:  "queued",
		JobID:   jobID,
		Ticker:  strings.TrimSpace(req.Ticker),
		Stage:   "queued",
		Summary: "排队中",
	})
	c.JSON(status, body)
}

func (h *UZIHandler) GetAnalyzeJobStatus(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userIDInt, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	status, body, err := h.uziService.GetQueuedAnalyzeJob(c.Param("jobID"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if status >= http.StatusBadRequest {
		c.JSON(status, body)
		return
	}

	ticker := firstNonEmptyHandlerString(stringValueHandler(body["ticker"]), stringValueHandler(body["stock_code"]))
	jobID := firstNonEmptyHandlerString(stringValueHandler(body["job_id"]), c.Param("jobID"))
	switch strings.TrimSpace(stringValueHandler(body["status"])) {
	case "queued", "pending":
		h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
			Status:  "queued",
			JobID:   jobID,
			Ticker:  ticker,
			Stage:   "queued",
			Summary: "排队中",
		})
	case "running":
		h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
			Status:  services.UZIAnalyzeStatusRunning,
			JobID:   jobID,
			Ticker:  ticker,
			Stage:   firstNonEmptyHandlerString(stringValueHandler(body["current_stage"]), "analysis"),
			Summary: "生成中",
		})
	case "succeeded":
		result, _ := body["result"].(map[string]interface{})
		reportMap, _ := result["report"].(map[string]interface{})
		depth := stringValueHandler(reportMap["depth"])
		req := &models.UZIAnalyzeRequest{Ticker: firstNonEmptyHandlerString(stringValueHandler(result["ticker"]), ticker)}
		if depth != "" {
			req.Depth = &depth
		}
		report, persistErr := h.uziService.PersistAnalyzeResult(userIDInt, req, result)
		if persistErr != nil {
			body["status"] = services.UZIAnalyzeStatusFailed
			body["error"] = fmt.Sprintf("persist uzi report: %v", persistErr)
			h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
				Status:  services.UZIAnalyzeStatusFailed,
				JobID:   jobID,
				Ticker:  ticker,
				Summary: stringValueHandler(body["error"]),
			})
			c.JSON(http.StatusBadGateway, body)
			return
		}
		if report != nil {
			body["report"] = report
			result["report"] = report
		}
		h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
			Status:  services.UZIAnalyzeStatusCompleted,
			JobID:   jobID,
			Ticker:  firstNonEmptyHandlerString(req.Ticker, ticker),
			Summary: "研报生成完成，可直接打开查看",
			Report:  report,
		})
	case "failed":
		h.uziService.UpdateAnalyzeStatus(userIDInt, models.UZIAnalyzeStatus{
			Status:  services.UZIAnalyzeStatusFailed,
			JobID:   jobID,
			Ticker:  ticker,
			Summary: firstNonEmptyHandlerString(stringValueHandler(body["error"]), "研报生成失败"),
		})
	}

	c.JSON(status, body)
}

func (h *UZIHandler) GetAnalyzeStatus(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	c.JSON(http.StatusOK, h.uziService.GetAnalyzeStatus(userID.(int)))
}

func (h *UZIHandler) AnalyzeStatusWebSocket(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	conn, err := uziStatusUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	statuses, unsubscribe := h.uziService.SubscribeAnalyzeStatus(userID.(int))
	defer unsubscribe()

	conn.SetReadLimit(512)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case status, ok := <-statuses:
			if !ok {
				return
			}
			if err := conn.WriteJSON(status); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		case <-c.Request.Context().Done():
			return
		}
	}
}

func (h *UZIHandler) ListReports(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	status, body, err := h.uziService.ListReports(userID.(int), c.Query("ticker"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *UZIHandler) DeleteReport(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	status, body, err := h.uziService.DeleteReport(userID.(int), c.Query("relative_path"))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *UZIHandler) GetReport(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	resp, err := h.uziService.FetchReport(userID.(int), c.Param("path"))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	extraHeaders := map[string]string{}
	if cacheControl := resp.Header.Get("Cache-Control"); cacheControl != "" {
		extraHeaders["Cache-Control"] = cacheControl
	}

	c.Status(resp.StatusCode)
	c.DataFromReader(resp.StatusCode, resp.ContentLength, contentType, resp.Body, extraHeaders)
}

func writeRawSSEEvent(writer gin.ResponseWriter, eventName string, payload string) error {
	if _, err := writer.Write([]byte("event: " + eventName + "\n")); err != nil {
		return err
	}
	if payload == "" {
		if _, err := writer.Write([]byte("data: \n\n")); err != nil {
			return err
		}
		writer.Flush()
		return nil
	}
	for _, line := range strings.Split(payload, "\n") {
		if _, err := writer.Write([]byte("data: " + line + "\n")); err != nil {
			return err
		}
	}
	if _, err := writer.Write([]byte("\n")); err != nil {
		return err
	}
	writer.Flush()
	return nil
}

func writeSSEEvent(writer gin.ResponseWriter, eventName string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeRawSSEEvent(writer, eventName, string(body))
}

func stringValueHandler(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func firstNonEmptyHandlerString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
