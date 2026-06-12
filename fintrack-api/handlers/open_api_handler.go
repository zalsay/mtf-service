package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type OpenAPIHandler struct {
	openAPIService *services.OpenAPIService
	watchlist      *services.WatchlistService
	mtfAgent       *services.MTFAgentService
	aiModels       *services.AIModelConfigService
	financeNews    *services.FinanceNewsService
	tempTokens     services.APIKeyTempTokenStore
}

func NewOpenAPIHandler(openAPIService *services.OpenAPIService, watchlist *services.WatchlistService, mtfAgent *services.MTFAgentService, aiModels *services.AIModelConfigService, financeNews *services.FinanceNewsService) *OpenAPIHandler {
	return &OpenAPIHandler{
		openAPIService: openAPIService,
		watchlist:      watchlist,
		mtfAgent:       mtfAgent,
		aiModels:       aiModels,
		financeNews:    financeNews,
	}
}

func (h *OpenAPIHandler) SetAPIKeyTempTokenStore(store services.APIKeyTempTokenStore) {
	h.tempTokens = store
}

func (h *OpenAPIHandler) CreateAPIKey(c *gin.Context) {
	var req models.OpenAPIKeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	response, err := h.openAPIService.CreateKey(c.Request.Context(), req)
	if err != nil {
		if strings.Contains(err.Error(), "invalid username or password") {
			writeOpenAPIError(c, http.StatusUnauthorized, "invalid_credentials", "invalid username or password", false)
			return
		}
		writeOpenAPIError(c, http.StatusInternalServerError, "create_api_key_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, response)
}

func (h *OpenAPIHandler) CreateAPIKeyTempToken(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	userID, ok := userIDValue.(int)
	if !ok || userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	if h.tempTokens == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "temporary token store is not configured"})
		return
	}
	token, err := services.GenerateAPIKeyTempToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate temporary token"})
		return
	}
	if err := h.tempTokens.Save(c.Request.Context(), token, userID, services.APIKeyTempTokenTTL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save temporary token"})
		return
	}
	c.JSON(http.StatusOK, models.OpenAPIKeyTempTokenResponse{
		Token:     token,
		ExpiresIn: int(services.APIKeyTempTokenTTL.Seconds()),
	})
}

func (h *OpenAPIHandler) CreateAPIKeyFromTempToken(c *gin.Context) {
	var req models.OpenAPIKeyTempTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	if h.tempTokens == nil {
		writeOpenAPIError(c, http.StatusServiceUnavailable, "token_store_unavailable", "temporary token store is not configured", true)
		return
	}
	userID, ok, err := h.tempTokens.Consume(c.Request.Context(), req.Token)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "token_read_failed", err.Error(), true)
		return
	}
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "invalid_or_expired_token", "temporary token is invalid or expired", false)
		return
	}
	response, err := h.openAPIService.CreateKeyForUser(c.Request.Context(), userID, req.Name)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "create_api_key_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, response)
}

func (h *OpenAPIHandler) AuthMiddleware(requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := openAPIBearerToken(c.GetHeader("Authorization"))
		record, user, err := h.openAPIService.ValidateKey(c.Request.Context(), token)
		if err != nil {
			status, code := openAPIAuthError(err)
			writeOpenAPIError(c, status, code, err.Error(), false)
			c.Abort()
			return
		}
		for _, scope := range requiredScopes {
			if !services.HasOpenAPIScope(record.Scopes, scope) {
				writeOpenAPIError(c, http.StatusForbidden, "scope_denied", "scope "+scope+" is required", false)
				c.Abort()
				return
			}
		}
		c.Set("user_id", user.ID)
		c.Set("user_email", user.Email)
		c.Set("is_admin", false)
		c.Set("user", user)
		c.Set("open_api_key_id", record.ID)
		c.Next()
	}
}

func (h *OpenAPIHandler) HotETF(c *gin.Context) {
	result, err := h.financeNews.ListHotETF(c.Request.Context())
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	writeOpenAPIData(c, http.StatusOK, result)
}

func (h *OpenAPIHandler) ETFQuotes(c *gin.Context) {
	var req models.BatchSymbolsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	quotes, err := h.watchlist.GetLatestQuotesBySymbols(req.Symbols)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "quotes_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"quotes": quotes})
}

func (h *OpenAPIHandler) ETFLookup(c *gin.Context) {
	symbol := strings.TrimSpace(c.Query("symbol"))
	if symbol == "" {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "symbol is required", false)
		return
	}
	name, err := h.watchlist.LookupStockName(symbol, 2)
	if err != nil {
		if err == services.ErrSymbolNotFound {
			writeOpenAPIError(c, http.StatusNotFound, "not_found", "symbol not found", false)
			return
		}
		writeOpenAPIError(c, http.StatusInternalServerError, "lookup_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"symbol": symbol, "stock_type": 2, "name": name})
}

func (h *OpenAPIHandler) MTFBest(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	if symbol := strings.TrimSpace(c.Query("symbol")); symbol != "" {
		if !h.requireWatchlistSymbol(c, userID, symbol) {
			return
		}
	}
	horizonLen := queryInt(c, "horizon_len")
	items, err := h.watchlist.ListWatchlistMTFBest(userID, horizonLen, c.Query("symbol"))
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "mtf_read_failed", err.Error(), false)
		return
	}
	items = filterMTFBestByStockType(items, queryInt(c, "stock_type"))
	if strings.EqualFold(c.DefaultQuery("include_validation", "true"), "false") {
		writeOpenAPIData(c, http.StatusOK, gin.H{"items": items, "count": len(items)})
		return
	}
	result, err := h.buildMTFBestWithValidationResponse(items)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "mtf_validation_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"items": result, "count": len(result)})
}

func filterMTFBestByStockType(items []models.MTFBestPrediction, stockType int) []models.MTFBestPrediction {
	if stockType <= 0 {
		return items
	}
	filtered := make([]models.MTFBestPrediction, 0, len(items))
	for _, item := range items {
		if item.StockType == stockType {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (h *OpenAPIHandler) MTFBestByConfig(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	if !h.requireWatchlistSymbol(c, userID, c.Query("symbol")) {
		return
	}
	horizonLen, contextLen, ok := requiredHorizonContext(c)
	if !ok {
		return
	}
	item, err := h.watchlist.GetMTFBestUniqueKeysByConfig(c.Query("symbol"), horizonLen, contextLen, "")
	if err != nil {
		writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, item)
}

func (h *OpenAPIHandler) MTFFuture(c *gin.Context) {
	user, ok := openAPIUser(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	userID := user.ID
	uniqueKey := strings.TrimSpace(c.Query("unique_key"))
	if uniqueKey == "" {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "unique_key is required", false)
		return
	}
	symbol, err := h.watchlist.GetMTFBestSymbolByUniqueKey(uniqueKey)
	if err != nil {
		writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
		return
	}
	if !h.requireWatchlistSymbol(c, userID, symbol) {
		return
	}
	predictReq, err := h.watchlist.GetMTFBestPredictOnceRequestByUniqueKey(uniqueKey)
	if err != nil {
		writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
		return
	}
	normalized, err := services.NormalizeMTFPredictOnceRequest(predictReq, user.MembershipLevel, userID, false)
	if err != nil {
		writeOpenAPIError(c, http.StatusForbidden, "validation_error", err.Error(), false)
		return
	}
	status, body, err := h.watchlist.GetMTFPredictOnceCached(normalized)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	if status != http.StatusNotFound {
		writeOpenAPIData(c, status, buildOpenAPIFutureFromPredictOnce(uniqueKey, body))
		return
	}
	status, body, err = h.watchlist.TriggerMTFPredictOnce(normalized)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	if data, ok := predictOnceData(body); ok {
		writeOpenAPIData(c, status, buildOpenAPIFuturePayload(uniqueKey, data))
		return
	}
	writeOpenAPIData(c, status, body)
}

func buildOpenAPIFutureFromPredictOnce(uniqueKey string, body map[string]interface{}) gin.H {
	if data, ok := predictOnceData(body); ok {
		return buildOpenAPIFuturePayload(uniqueKey, data)
	}
	return gin.H{"unique_key": uniqueKey, "predict_once": body}
}

func predictOnceData(body map[string]interface{}) (map[string]interface{}, bool) {
	if body == nil {
		return nil, false
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok || data == nil {
		return nil, false
	}
	return data, true
}

func buildOpenAPIFuturePayload(uniqueKey string, data map[string]interface{}) gin.H {
	dates := stringSliceFromInterface(data["future_dates"])
	predictions := floatSliceFromInterface(data["best_prediction_values"])
	maxLen := len(dates)
	if len(predictions) < maxLen {
		maxLen = len(predictions)
	}
	dates = dates[:maxLen]
	predictions = predictions[:maxLen]
	predictedLatest := lastFiniteFloat(predictions)
	actualLatest := firstFiniteFloat(data["latest_close"], data["adjust_raw_latest_close"], data["change_base_value"])
	changePct := 0.0
	if actualLatest > 0 && predictedLatest > 0 {
		changePct = (predictedLatest - actualLatest) / actualLatest * 100
	}
	if values := floatSliceFromInterface(data["predicted_change_percent"]); len(values) > 0 {
		changePct = values[len(values)-1]
	}
	return gin.H{
		"unique_key":               uniqueKey,
		"dates":                    dates,
		"predictions":              predictions,
		"count":                    len(dates),
		"predicted_latest":         predictedLatest,
		"actual_latest":            actualLatest,
		"predicted_change_percent": changePct,
	}
}

func stringSliceFromInterface(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" && text != "<nil>" {
			out = append(out, text)
		}
	}
	return out
}

func floatSliceFromInterface(value interface{}) []float64 {
	switch typed := value.(type) {
	case []float64:
		return typed
	case []interface{}:
		out := make([]float64, 0, len(typed))
		for _, item := range typed {
			if value, ok := finiteFloat(item); ok {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func finiteFloat(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if !math.IsNaN(typed) && !math.IsInf(typed, 0) {
			return typed, true
		}
	case float32:
		value := float64(typed)
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value, true
		}
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		if value, err := strconv.ParseFloat(string(typed), 64); err == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value, true
		}
	}
	return 0, false
}

func lastFiniteFloat(values []float64) float64 {
	for i := len(values) - 1; i >= 0; i-- {
		if !math.IsNaN(values[i]) && !math.IsInf(values[i], 0) {
			return values[i]
		}
	}
	return 0
}

func firstFiniteFloat(values ...interface{}) float64 {
	for _, value := range values {
		if parsed, ok := finiteFloat(value); ok {
			return parsed
		}
	}
	return 0
}

func (h *OpenAPIHandler) MTFPredictOnce(c *gin.Context) {
	user, ok := openAPIUser(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.OpenAPIMTFPredictOnceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	normalized, err := services.NormalizeMTFPredictOnceRequest(&req.MTFPredictRequest, user.MembershipLevel, user.ID, false)
	if err != nil {
		writeOpenAPIError(c, http.StatusForbidden, "validation_error", err.Error(), false)
		return
	}
	if normalized.StockType == nil {
		normalized.StockType = 2
	}
	if req.PreferCache {
		status, body, err := h.watchlist.GetMTFPredictOnceCached(normalized)
		if err != nil {
			writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
			return
		}
		if status != http.StatusNotFound {
			writeOpenAPIData(c, status, body)
			return
		}
	}
	status, body, err := h.watchlist.TriggerMTFPredictOnce(normalized)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	writeOpenAPIData(c, status, body)
}

func (h *OpenAPIHandler) MTFPredictBest(c *gin.Context) {
	user, ok := openAPIUser(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.MTFBestTrainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	if req.StockType == nil {
		req.StockType = 2
	}
	normalized, err := services.NormalizeMTFBestTrainRequest(&req, user.MembershipLevel, user.ID, false)
	if err != nil {
		writeOpenAPIError(c, http.StatusForbidden, "validation_error", err.Error(), false)
		return
	}
	status, body, err := h.watchlist.TriggerMTFPredict(normalized)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	writeOpenAPIData(c, status, body)
}

func (h *OpenAPIHandler) MTFBacktest(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.MTFBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	req.UserID = &userID
	status, body, err := h.watchlist.RunMTFBacktest(&req)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	writeOpenAPIData(c, status, body)
}

func (h *OpenAPIHandler) MTFJob(c *gin.Context) {
	status, body, err := h.watchlist.GetMTFJobStatus(c.Param("jobID"))
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "upstream_unavailable", err.Error(), true)
		return
	}
	writeOpenAPIData(c, status, body)
}

func (h *OpenAPIHandler) StrategyList(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	items, err := h.watchlist.GetUserStrategies(userID)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "strategy_read_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"strategies": items})
}

func (h *OpenAPIHandler) SaveStrategy(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.SaveStrategyParamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	req.UserID = &userID
	if err := h.watchlist.SaveStrategyParams(&req); err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "strategy_write_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"unique_key": req.UniqueKey})
}

func (h *OpenAPIHandler) Watchlist(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	items, err := h.watchlist.GetWatchlist(userID)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "watchlist_read_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"watchlist": items, "count": len(items)})
}

func (h *OpenAPIHandler) AddWatchlist(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.AddToWatchlistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	if req.StockType == nil {
		stockType := 2
		req.StockType = &stockType
	}
	if err := h.watchlist.AddToWatchlist(userID, &req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "watchlist_write_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusCreated, gin.H{"message": "watchlist item added"})
}

func (h *OpenAPIHandler) BindWatchlistStrategy(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.BindStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	if err := h.watchlist.BindStrategy(userID, &req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "watchlist_bind_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, gin.H{"message": "strategy bound"})
}

func (h *OpenAPIHandler) AgentMessage(c *gin.Context) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	var req models.MTFAgentMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	aiConfig, err := h.aiModels.GetByUserID(userID)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "ai_model_config_failed", err.Error(), false)
		return
	}
	response, err := h.mtfAgent.SendMessage(c.Request.Context(), userID, req.Message, aiConfig)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadGateway, "agent_failed", err.Error(), true)
		return
	}
	writeOpenAPIData(c, http.StatusOK, response)
}

func (h *OpenAPIHandler) AgentHistoryTrends(c *gin.Context) {
	h.agentSkill(c, "history_trends")
}

func (h *OpenAPIHandler) AgentUZIReports(c *gin.Context) {
	h.agentSkill(c, "uzi_reports")
}

func (h *OpenAPIHandler) agentSkill(c *gin.Context, skill string) {
	userID, ok := openAPIUserID(c)
	if !ok {
		writeOpenAPIError(c, http.StatusUnauthorized, "user_mapping_required", "user is required", false)
		return
	}
	args := gin.H{}
	for _, key := range []string{"symbol", "unique_key", "prediction_type", "ticker"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			args[key] = value
		}
	}
	for _, key := range []string{"horizon_len", "limit", "chunk_limit", "point_limit"} {
		if value := queryInt(c, key); value > 0 {
			args[key] = value
		}
	}
	result, err := h.mtfAgent.ExecuteMTFAgentSkill(c.Request.Context(), userID, skill, args)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "agent_skill_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, result)
}

func (h *OpenAPIHandler) buildMTFBestWithValidationResponse(items []models.MTFBestPrediction) ([]gin.H, error) {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.UniqueKey)
	}
	chunksByKey, err := h.watchlist.ListValidationChunksByUniqueKeys(keys)
	if err != nil {
		return nil, err
	}
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, gin.H{
			"best":   item,
			"chunks": chunksByKey[item.UniqueKey],
		})
	}
	return result, nil
}

func (h *OpenAPIHandler) requireWatchlistSymbol(c *gin.Context, userID int, symbol string) bool {
	if strings.TrimSpace(symbol) == "" {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "symbol is required", false)
		return false
	}
	exists, err := h.watchlist.IsSymbolInUserWatchlist(userID, symbol)
	if err != nil {
		writeOpenAPIError(c, http.StatusInternalServerError, "watchlist_check_failed", err.Error(), false)
		return false
	}
	if !exists {
		writeOpenAPIError(c, http.StatusForbidden, "watchlist_required", "symbol must be in the user's watchlist", false)
		return false
	}
	return true
}

func writeOpenAPIData(c *gin.Context, status int, data interface{}) {
	c.JSON(status, models.OpenAPIEnvelope{
		RequestID: openAPIRequestID(c),
		Status:    "ok",
		Data:      data,
	})
}

func writeOpenAPIError(c *gin.Context, status int, code string, message string, retryable bool) {
	c.JSON(status, models.OpenAPIEnvelope{
		RequestID: openAPIRequestID(c),
		Status:    "error",
		Error: models.OpenAPIErrorBody{
			Code:      code,
			Message:   message,
			Retryable: retryable,
		},
	})
}

func openAPIRequestID(c *gin.Context) string {
	if value := strings.TrimSpace(c.GetHeader("X-Request-Id")); value != "" {
		return value
	}
	if value, ok := c.Get("request_id"); ok {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return "req_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func openAPIBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return header
}

func openAPIAuthError(err error) (int, string) {
	message := err.Error()
	switch message {
	case "invalid_api_key":
		return http.StatusUnauthorized, "invalid_api_key"
	case "api_key_disabled":
		return http.StatusForbidden, "api_key_disabled"
	case "api_key_expired":
		return http.StatusUnauthorized, "api_key_expired"
	default:
		return http.StatusUnauthorized, "invalid_api_key"
	}
}

func openAPIUserID(c *gin.Context) (int, bool) {
	value, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	id, ok := value.(int)
	return id, ok
}

func openAPIUser(c *gin.Context) (*models.User, bool) {
	value, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	user, ok := value.(*models.User)
	return user, ok && user != nil
}

func requiredHorizonContext(c *gin.Context) (int, int, bool) {
	horizonLen, err := strconv.Atoi(c.Query("horizon_len"))
	if err != nil || horizonLen <= 0 {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid horizon_len is required", false)
		return 0, 0, false
	}
	contextLen, err := strconv.Atoi(c.Query("context_len"))
	if err != nil || contextLen <= 0 {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid context_len is required", false)
		return 0, 0, false
	}
	return horizonLen, contextLen, true
}
