package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type MTFAgentHandler struct {
	mtfAgentService      *services.MTFAgentService
	aiModelConfigService *services.AIModelConfigService
}

func NewMTFAgentHandler(mtfAgentService *services.MTFAgentService, aiModelConfigService *services.AIModelConfigService) *MTFAgentHandler {
	return &MTFAgentHandler{
		mtfAgentService:      mtfAgentService,
		aiModelConfigService: aiModelConfigService,
	}
}

func (h *MTFAgentHandler) Session(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	aiConfig, err := h.loadAIModelConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load AI model config"})
		return
	}

	response, err := h.mtfAgentService.Session(c.Request.Context(), userID, aiConfig)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *MTFAgentHandler) SendMessage(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req models.MTFAgentMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	aiConfig, err := h.loadAIModelConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load AI model config"})
		return
	}

	response, err := h.mtfAgentService.SendMessage(c.Request.Context(), userID, req.Message, aiConfig)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *MTFAgentHandler) StartMessageJob(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req models.MTFAgentMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	aiConfig, err := h.loadAIModelConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load AI model config"})
		return
	}

	response, err := h.mtfAgentService.StartMessageJob(c.Request.Context(), userID, req.Message, aiConfig)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, response)
}

func (h *MTFAgentHandler) GetMessageJob(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	response, err := h.mtfAgentService.GetMessageJob(userID, c.Param("jobID"))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *MTFAgentHandler) SendMessageStream(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req models.MTFAgentMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	aiConfig, err := h.loadAIModelConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load AI model config"})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming is not supported"})
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("Content-Encoding", "identity")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeMTFAgentSSEComment(c, strings.Repeat(" ", 2048))
	writeMTFAgentSSEEvent(c, "start", gin.H{"status": "running"})
	flusher.Flush()

	events, err := h.mtfAgentService.StreamMessage(c.Request.Context(), userID, req.Message, aiConfig)
	if err != nil {
		writeMTFAgentSSEEvent(c, "error", gin.H{"error": err.Error()})
		flusher.Flush()
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			writeMTFAgentSSEEvent(c, "heartbeat", gin.H{"timestamp": time.Now().UTC().Format(time.RFC3339)})
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.Type {
			case "delta":
				writeMTFAgentSSEEvent(c, "delta", gin.H{"text": event.Text})
				flusher.Flush()
			case "done":
				writeMTFAgentSSEEvent(c, "done", event.Response)
				flusher.Flush()
				return
			case "error":
				writeMTFAgentSSEEvent(c, "error", gin.H{"error": event.Error})
				flusher.Flush()
				return
			}
		}
	}
}

func (h *MTFAgentHandler) ListMessages(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	response, err := h.mtfAgentService.ListMessages(userID)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *MTFAgentHandler) Reset(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	aiConfig, err := h.loadAIModelConfig(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load AI model config"})
		return
	}

	response, err := h.mtfAgentService.Reset(c.Request.Context(), userID, aiConfig)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *MTFAgentHandler) ListMemories(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	memories, err := h.mtfAgentService.ListMemories(userID)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"memories": memories})
}

func (h *MTFAgentHandler) ClearMemories(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if err := h.mtfAgentService.ClearMemories(userID); err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "memories cleared"})
}

func (h *MTFAgentHandler) HistoryTrendsSkill(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	args := gin.H{}
	addStringSkillArg(args, c, "symbol")
	addStringSkillArg(args, c, "unique_key")
	addStringSkillArg(args, c, "prediction_type")
	addIntSkillArg(args, c, "horizon_len")
	addIntSkillArg(args, c, "limit")
	addIntSkillArg(args, c, "chunk_limit")
	addIntSkillArg(args, c, "point_limit")

	result, err := h.mtfAgentService.ExecuteMTFAgentSkill(c.Request.Context(), userID, "history_trends", args)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *MTFAgentHandler) UZIReportsSkill(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	args := gin.H{}
	addStringSkillArg(args, c, "ticker")
	addIntSkillArg(args, c, "limit")

	result, err := h.mtfAgentService.ExecuteMTFAgentSkill(c.Request.Context(), userID, "uzi_reports", args)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func addStringSkillArg(args gin.H, c *gin.Context, key string) {
	if value := strings.TrimSpace(c.Query(key)); value != "" {
		args[key] = value
	}
}

func addIntSkillArg(args gin.H, c *gin.Context, key string) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return
	}
	args[key] = value
}

func writeMTFAgentSSEEvent(c *gin.Context, event string, payload interface{}) {
	c.SSEvent(event, payload)
}

func writeMTFAgentSSEComment(c *gin.Context, comment string) {
	_, _ = c.Writer.WriteString(": " + comment + "\n\n")
}

func (h *MTFAgentHandler) loadAIModelConfig(userID int) (*models.AIModelConfig, error) {
	if h.aiModelConfigService == nil {
		return nil, errors.New("ai model config service is not configured")
	}
	return h.aiModelConfigService.GetByUserID(userID)
}

func (h *MTFAgentHandler) writeServiceError(c *gin.Context, err error) {
	message := err.Error()
	switch {
	case strings.Contains(message, services.AIModelConfigRequiredMsg):
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": services.AIModelConfigRequiredMsg})
	case strings.Contains(message, "message is required"):
		c.JSON(http.StatusBadRequest, gin.H{"error": "message is required"})
	case strings.Contains(message, "runtime is not configured"):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": message})
	case strings.Contains(strings.ToLower(message), "deepseek runtime"),
		strings.Contains(message, "DeepSeek"),
		strings.Contains(message, "Failed to send message"):
		c.JSON(http.StatusBadGateway, gin.H{"error": message})
	default:
		log.Printf("MTF Agent handler error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "MTF Agent request failed"})
	}
}
