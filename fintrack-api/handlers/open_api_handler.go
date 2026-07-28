package handlers

import (
	"errors"
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

func (h *OpenAPIHandler) PublicAPIKeyV2(c *gin.Context) {
	response, err := h.openAPIService.PublicV2Key()
	if err != nil {
		if errors.Is(err, services.ErrOpenAPIV2PrivateKeyUnavailable) {
			writeOpenAPIError(c, http.StatusServiceUnavailable, "v2_public_key_unavailable", "v2 API private key is not configured", true)
			return
		}
		writeOpenAPIError(c, http.StatusInternalServerError, "v2_public_key_failed", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, response)
}

func (h *OpenAPIHandler) CreateAPIKeyV2(c *gin.Context) {
	var req models.OpenAPIV2KeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	response, err := h.openAPIService.CreateV2Key(c.Request.Context(), req.EncryptedPayload)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrOpenAPIV2PrivateKeyUnavailable):
			writeOpenAPIError(c, http.StatusServiceUnavailable, "v2_key_unavailable", "v2 API private key is not configured", true)
		case errors.Is(err, services.ErrOpenAPIV2InvalidPayload), errors.Is(err, services.ErrOpenAPIV2TimestampExpired):
			writeOpenAPIError(c, http.StatusBadRequest, "invalid_encrypted_payload", "encrypted payload is invalid or expired", false)
		default:
			writeOpenAPIError(c, http.StatusInternalServerError, "create_v2_api_key_failed", err.Error(), false)
		}
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

func (h *OpenAPIHandler) AuthMiddlewareV2(requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := openAPIBearerToken(c.GetHeader("Authorization"))
		record, err := h.openAPIService.ValidateV2Key(c.Request.Context(), token)
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
		c.Set("v2_server_name", record.ServerName)
		c.Set("v2_external_user_id", record.ExternalUserID)
		c.Set("open_api_v2_key_id", record.ID)
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
	quotes, err := h.watchlist.GetLatestQuotes(&req)
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
	horizonLen, contextLen, ok := optionalHorizonContext(c)
	if !ok {
		return
	}
	if horizonLen == nil || contextLen == nil {
		stockType, ok := optionalPositiveIntQuery(c, "stock_type")
		if !ok {
			return
		}
		result, err := h.watchlist.ListMTFBestUniqueKeysBySymbolConfig(c.Query("symbol"), stockType, horizonLen, contextLen, "")
		if err != nil {
			writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
			return
		}
		writeOpenAPIData(c, http.StatusOK, result)
		return
	}
	item, err := h.watchlist.GetMTFBestUniqueKeysByConfig(c.Query("symbol"), *horizonLen, *contextLen, "")
	if err != nil {
		writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
		return
	}
	writeOpenAPIData(c, http.StatusOK, item)
}

func (h *OpenAPIHandler) MTFBestByConfigV2(c *gin.Context) {
	if strings.TrimSpace(c.Query("symbol")) == "" {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "symbol is required", false)
		return
	}
	horizonLen, contextLen, ok := optionalV2HorizonContext(c)
	if !ok {
		return
	}
	if horizonLen == nil || contextLen == nil {
		stockType, ok := optionalPositiveIntQuery(c, "stock_type")
		if !ok {
			return
		}
		result, err := h.watchlist.ListMTFBestUniqueKeysBySymbolConfig(c.Query("symbol"), stockType, horizonLen, contextLen, "")
		if err != nil {
			writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
			return
		}
		writeOpenAPIData(c, http.StatusOK, result)
		return
	}
	item, err := h.watchlist.GetMTFBestUniqueKeysByConfig(c.Query("symbol"), *horizonLen, *contextLen, "")
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
	predictDate, ok := optionalOpenAPIPredictDate(c)
	if !ok {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "predict_date must be a valid date", false)
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
	if predictDate != nil {
		predictReq.PredictDate = predictDate
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
	message := "未找到指定日期的单次预测缓存"
	if rawMessage, exists := body["message"]; exists && rawMessage != nil {
		if value, ok := rawMessage.(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				message = value
			}
		}
	}
	writeOpenAPIError(c, http.StatusNotFound, "prediction_cache_not_found", message, false)
}

func (h *OpenAPIHandler) MTFFutureV2(c *gin.Context) {
	uniqueKey := strings.TrimSpace(c.Query("unique_key"))
	if uniqueKey == "" {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "unique_key is required", false)
		return
	}
	predictDate, ok := optionalOpenAPIPredictDate(c)
	if !ok {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "predict_date must be a valid date", false)
		return
	}
	predictReq, err := h.watchlist.GetMTFBestPredictOnceRequestByUniqueKey(uniqueKey)
	if err != nil {
		writeOpenAPIError(c, http.StatusNotFound, "not_found", err.Error(), false)
		return
	}
	if predictDate != nil {
		predictReq.PredictDate = predictDate
	}
	normalized, err := services.NormalizeMTFPredictOnceRequestV2(predictReq)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
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
	message := "未找到指定日期的单次预测缓存"
	if rawMessage, exists := body["message"]; exists && rawMessage != nil {
		if value, ok := rawMessage.(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				message = value
			}
		}
	}
	writeOpenAPIError(c, http.StatusNotFound, "prediction_cache_not_found", message, false)
}

func optionalOpenAPIPredictDate(c *gin.Context) (*string, bool) {
	raw := strings.TrimSpace(c.Query("predict_date"))
	if raw == "" {
		return nil, true
	}
	parsed := parseMTFChunkDate(raw)
	if parsed.IsZero() {
		return nil, false
	}
	value := parsed.Format("2006-01-02")
	return &value, true
}

func buildOpenAPIFutureFromPredictOnce(uniqueKey string, body map[string]interface{}) interface{} {
	if data, ok := predictOnceData(body); ok {
		return buildOpenAPIFutureData(uniqueKey, data)
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

func buildOpenAPIFutureData(uniqueKey string, data map[string]interface{}) gin.H {
	out := gin.H{"unique_key": uniqueKey}
	for key, value := range data {
		out[key] = value
	}
	return out
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

func (h *OpenAPIHandler) MTFPredictOnceV2(c *gin.Context) {
	var req models.OpenAPIMTFPredictOnceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
	}
	normalized, err := services.NormalizeMTFPredictOnceRequestV2(&req.MTFPredictRequest)
	if err != nil {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", err.Error(), false)
		return
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

func optionalHorizonContext(c *gin.Context) (*int, *int, bool) {
	horizonRaw := strings.TrimSpace(c.Query("horizon_len"))
	contextRaw := strings.TrimSpace(c.Query("context_len"))
	if horizonRaw == "" && contextRaw == "" {
		return nil, nil, true
	}
	var horizonLen *int
	if horizonRaw != "" {
		value, err := strconv.Atoi(horizonRaw)
		if err != nil || value <= 0 {
			writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid horizon_len is required", false)
			return nil, nil, false
		}
		horizonLen = &value
	}
	var contextLen *int
	if contextRaw != "" {
		value, err := strconv.Atoi(contextRaw)
		if err != nil || value <= 0 {
			writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid context_len is required", false)
			return nil, nil, false
		}
		contextLen = &value
	}
	return horizonLen, contextLen, true
}

func optionalV2HorizonContext(c *gin.Context) (*int, *int, bool) {
	horizonRaw := strings.TrimSpace(c.Query("horizon_len"))
	horizonLen := services.MTFV2HorizonLen
	if horizonRaw != "" {
		value, err := strconv.Atoi(horizonRaw)
		if err != nil || value <= 0 {
			writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid horizon_len is required", false)
			return nil, nil, false
		}
		if value != services.MTFV2HorizonLen {
			writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "v2 only supports horizon_len="+strconv.Itoa(services.MTFV2HorizonLen), false)
			return nil, nil, false
		}
		horizonLen = value
	}

	contextRaw := strings.TrimSpace(c.Query("context_len"))
	var contextLen *int
	if contextRaw != "" {
		value, err := strconv.Atoi(contextRaw)
		if err != nil || value <= 0 {
			writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid context_len is required", false)
			return nil, nil, false
		}
		contextLen = &value
	}
	return &horizonLen, contextLen, true
}

func optionalPositiveIntQuery(c *gin.Context, name string) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		writeOpenAPIError(c, http.StatusBadRequest, "validation_error", "valid "+name+" is required", false)
		return 0, false
	}
	return value, true
}
