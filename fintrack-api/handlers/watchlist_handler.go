package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type WatchlistHandler struct {
	watchlistService *services.WatchlistService
}

func NewWatchlistHandler(watchlistService *services.WatchlistService) *WatchlistHandler {
	return &WatchlistHandler{watchlistService: watchlistService}
}

func extractPaymentCredential(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	lower := strings.ToLower(header)
	if strings.HasPrefix(lower, "alipay-ai-pay ") {
		return strings.TrimSpace(header[len("alipay-ai-pay "):])
	}
	if strings.HasPrefix(lower, "bearer ") {
		return strings.TrimSpace(header[len("bearer "):])
	}
	return header
}

func (h *WatchlistHandler) writePaymentRequired(c *gin.Context) {
	cfg := h.watchlistService.Config()
	payCfg := cfg.AlipayService
	resourceID := payCfg.ResourceID
	if strings.TrimSpace(resourceID) == "" {
		resourceID = "mtf.predict.once"
	}
	resourceName := payCfg.ResourceName
	if strings.TrimSpace(resourceName) == "" {
		resourceName = "MTF 单次预测"
	}
	c.JSON(http.StatusPaymentRequired, gin.H{
		"error": "payment required",
		"payment": gin.H{
			"status_code": http.StatusPaymentRequired,
			"protocol":    "alipay-ai-pay-402",
			"product": gin.H{
				"resource_id":  resourceID,
				"name":         resourceName,
				"amount_cents": payCfg.AmountCents,
				"currency":     payCfg.Currency,
			},
			"payee": gin.H{
				"merchant_id":   payCfg.MerchantID,
				"merchant_name": payCfg.MerchantName,
			},
		},
	})
}

func (h *WatchlistHandler) AddToWatchlist(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.AddToWatchlistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.watchlistService.AddToWatchlist(userID.(int), &req)
	if err != nil {
		if err.Error() == services.ErrSymbolNotFound.Error() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "symbol not found"})
			return
		}
		if err.Error() == services.ErrDuplicateSymbol.Error() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "duplicate symbol"})
			return
		}
		var limitErr services.WatchlistLimitExceededError
		if errors.As(err, &limitErr) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "watchlist limit exceeded",
				"limit": limitErr.Limit,
				"count": limitErr.Count,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Trigger stock data sync asynchronously (non-blocking)
	go h.watchlistService.SyncStockData(req.Symbol)

	c.JSON(http.StatusCreated, gin.H{"message": "Stock added to watchlist successfully"})
}

func (h *WatchlistHandler) GetWatchlist(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	items, err := h.watchlistService.GetWatchlist(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get watchlist"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"watchlist": items,
		"count":     len(items),
	})
}

func (h *WatchlistHandler) RemoveFromWatchlist(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	watchlistIDStr := c.Param("id")
	watchlistID, err := strconv.Atoi(watchlistIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid watchlist ID"})
		return
	}

	err = h.watchlistService.RemoveFromWatchlist(userID.(int), watchlistID)
	if err != nil {
		if err.Error() == "watchlist item not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove from watchlist"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Stock removed from watchlist successfully"})
}

func (h *WatchlistHandler) UpdateWatchlistItem(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	watchlistIDStr := c.Param("id")
	watchlistID, err := strconv.Atoi(watchlistIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid watchlist ID"})
		return
	}

	var req models.UpdateWatchlistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	item, err := h.watchlistService.UpdateWatchlistItem(userID.(int), watchlistID, &req)
	if err != nil {
		if err.Error() == "watchlist item not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update watchlist item"})
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *WatchlistHandler) BindStrategy(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.BindStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.watchlistService.BindStrategy(userID.(int), &req)
	if err != nil {
		if err.Error() == "strategy not found" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Strategy bound successfully"})
}

func (h *WatchlistHandler) GetUserStrategies(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	strategies, err := h.watchlistService.GetUserStrategies(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"strategies": strategies})
}

func (h *WatchlistHandler) GetBatchLatestQuotes(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.BatchSymbolsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Symbols) == 0 {
		c.JSON(http.StatusOK, gin.H{"quotes": []models.LatestQuote{}})
		return
	}

	quotes, err := h.watchlistService.GetLatestQuotesBySymbols(req.Symbols)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_ = userID
	c.JSON(http.StatusOK, gin.H{"quotes": quotes})
}

func (h *WatchlistHandler) LookupStockName(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required"})
		return
	}
	stockTypeStr := c.Query("stock_type")
	stockType := 1
	if stockTypeStr != "" {
		if v, err := strconv.Atoi(stockTypeStr); err == nil {
			stockType = v
		}
	}

	name, err := h.watchlistService.LookupStockName(symbol, stockType)
	if err != nil {
		if err.Error() == services.ErrSymbolNotFound.Error() {
			c.JSON(http.StatusNotFound, gin.H{"error": "symbol not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"symbol": symbol, "name": name})
}

// 查询某用户的MTF最佳分位预测列表
func (h *WatchlistHandler) ListMTFBestByUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	results, err := h.watchlistService.ListMTFBestByUserID(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"predictions": results,
		"count":       len(results),
	})
}

// 按 unique_key 查询单条 MTF 最佳分位预测（公开）
func (h *WatchlistHandler) GetMTFBestByUniqueKey(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	item, err := h.watchlistService.GetMTFBestByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"prediction": item})
}

func (h *WatchlistHandler) GetMTFBestUniqueKeysByConfig(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required"})
		return
	}

	horizonLen, err := strconv.Atoi(c.Query("horizon_len"))
	if err != nil || horizonLen <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid horizon_len is required"})
		return
	}

	contextLen, err := strconv.Atoi(c.Query("context_len"))
	if err != nil || contextLen <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid context_len is required"})
		return
	}

	item, err := h.watchlistService.GetMTFBestUniqueKeysByConfig(symbol, horizonLen, contextLen, "")
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"prediction": item})
}

// 按 unique_key 查询单条 MTF 回测结果
func (h *WatchlistHandler) GetMTFBacktestByUniqueKey(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	item, err := h.watchlistService.GetMTFBacktestByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, item)
}

func (h *WatchlistHandler) SaveStrategyParams(c *gin.Context) {
	var req models.SaveStrategyParamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	assignStrategyParamsUserID(c, &req)
	if err := h.watchlistService.SaveStrategyParams(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Saved strategy params", "unique_key": req.UniqueKey})
}

func assignStrategyParamsUserID(c *gin.Context, req *models.SaveStrategyParamsRequest) {
	if req == nil || req.UserID != nil {
		return
	}
	if uid, ok := c.Get("user_id"); ok {
		if userID, ok := uid.(int); ok {
			req.UserID = &userID
		}
	}
}

func (h *WatchlistHandler) GetStrategyParamsByUniqueKey(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}
	item, err := h.watchlistService.GetStrategyParamsByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *WatchlistHandler) buildMTFBestWithValidationResponse(items []models.MTFBestPrediction) ([]gin.H, error) {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.UniqueKey)
	}
	chunksByKey, err := h.watchlistService.ListValidationChunksByUniqueKeys(keys)
	if err != nil {
		return nil, err
	}

	result := make([]gin.H, 0, len(items))
	for _, it := range items {
		chunks := chunksByKey[it.UniqueKey]

		var maxDevPercent float64 = 0
		for idx := range chunks {
			chunk := &chunks[idx]

			// helper: convert interface slice to []float64
			toFloatSlice := func(val interface{}) []float64 {
				res := []float64{}
				switch arr := val.(type) {
				case []float64:
					return arr
				case []float32:
					for _, p := range arr {
						res = append(res, float64(p))
					}
				case []int:
					for _, p := range arr {
						res = append(res, float64(p))
					}
				case []int64:
					for _, p := range arr {
						res = append(res, float64(p))
					}
				case []interface{}:
					for _, p := range arr {
						switch v := p.(type) {
						case float64:
							res = append(res, v)
						case float32:
							res = append(res, float64(v))
						case int:
							res = append(res, float64(v))
						case int64:
							res = append(res, float64(v))
						case json.Number:
							if f, err := v.Float64(); err == nil {
								res = append(res, f)
							}
						}
					}
				}
				return res
			}

			// best predictions series
			var bestPred []float64
			if val, ok := chunk.Predictions[it.BestPredictionItem]; ok {
				bestPred = toFloatSlice(val)
			}
			bestPredChange := []float64{}
			if val, ok := chunk.PredictedChangePct[it.BestPredictionItem]; ok {
				bestPredChange = toFloatSlice(val)
			}

			// align-filter: remove points where actual==0 or predicted==0 or invalid, keeping indices consistent across actual/pred/dates
			filteredActual := make([]float64, 0, len(chunk.Actual))
			filteredPred := make([]float64, 0, len(bestPred))
			filteredDates := make([]string, 0, len(chunk.Dates))
			filteredActualChange := make([]float64, 0, len(chunk.ActualChangePct))
			filteredPredChange := make([]float64, 0, len(bestPredChange))

			maxLen := len(chunk.Actual)
			if len(bestPred) < maxLen {
				maxLen = len(bestPred)
			}
			if len(chunk.Dates) < maxLen {
				maxLen = len(chunk.Dates)
			}
			for i := 0; i < maxLen; i++ {
				a := chunk.Actual[i]
				p := bestPred[i]
				if a == 0 || p == 0 || math.IsNaN(a) || math.IsNaN(p) || math.IsInf(a, 0) || math.IsInf(p, 0) {
					continue
				}
				filteredActual = append(filteredActual, a)
				filteredPred = append(filteredPred, p)
				filteredDates = append(filteredDates, chunk.Dates[i])
				if i < len(chunk.ActualChangePct) {
					filteredActualChange = append(filteredActualChange, chunk.ActualChangePct[i])
				}
				if i < len(bestPredChange) {
					filteredPredChange = append(filteredPredChange, bestPredChange[i])
				}
			}

			// overwrite chunk with filtered aligned data
			chunk.Actual = filteredActual
			if _, ok := chunk.Predictions[it.BestPredictionItem]; ok {
				// store back as []float64 so JSON renders as array of numbers
				chunk.Predictions[it.BestPredictionItem] = filteredPred
			}
			if len(filteredActualChange) > 0 {
				chunk.ActualChangePct = filteredActualChange
			}
			if _, ok := chunk.PredictedChangePct[it.BestPredictionItem]; ok {
				chunk.PredictedChangePct[it.BestPredictionItem] = filteredPredChange
			}
			chunk.Dates = filteredDates

			// update max deviation percent from filtered series
			for i := 0; i < len(filteredActual) && i < len(filteredPred); i++ {
				a := filteredActual[i]
				p := filteredPred[i]
				if a != 0 { // redundant but safe
					dev := math.Abs((p-a)/a) * 100
					if dev > maxDevPercent {
						maxDevPercent = dev
					}
				}
			}
		}

		result = append(result, gin.H{
			"best":                  it,
			"chunks":                chunks,
			"max_deviation_percent": maxDevPercent,
		})
	}

	return result, nil
}

// 公开查询：返回 is_public = 1 的 mtf-best，并联查对应的验证分块数据
func (h *WatchlistHandler) ListPublicMTFBestWithValidation(c *gin.Context) {
	horizonLen := 0
	if hStr := c.Query("horizon_len"); hStr != "" {
		if val, err := strconv.Atoi(hStr); err == nil {
			horizonLen = val
		}
	}
	symbol := c.Query("symbol")
	isAdmin, _ := c.Get("is_admin")
	includePrivate, _ := isAdmin.(bool)

	items, err := h.watchlistService.ListPublicMTFBest(horizonLen, symbol, includePrivate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := h.buildMTFBestWithValidationResponse(items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": result, "count": len(result)})
}

func (h *WatchlistHandler) ListAccessibleMTFBestWithValidation(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	horizonLen := 0
	if hStr := c.Query("horizon_len"); hStr != "" {
		if val, err := strconv.Atoi(hStr); err == nil {
			horizonLen = val
		}
	}
	symbol := c.Query("symbol")
	isAdmin, _ := c.Get("is_admin")
	includePrivate, _ := isAdmin.(bool)

	items, err := h.watchlistService.ListAccessibleMTFBest(userIDValue.(int), horizonLen, symbol, includePrivate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := h.buildMTFBestWithValidationResponse(items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": result, "count": len(result)})
}

func (h *WatchlistHandler) GetFuturePredictions(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}
	dates, preds, predLatest, actualLatest, changePct, err := h.watchlistService.ListFuturePredictionsByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"unique_key":               uniqueKey,
		"dates":                    dates,
		"predictions":              preds,
		"count":                    len(dates),
		"predicted_latest":         predLatest,
		"actual_latest":            actualLatest,
		"predicted_change_percent": changePct,
	})
}

func (h *WatchlistHandler) GetLatestValidationChunk(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	item, err := h.watchlistService.GetLatestValidationChunkByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": item})
}

// 保存 MTF 回测结果
func (h *WatchlistHandler) SaveMTFBacktest(c *gin.Context) {
	var req models.SaveMTFBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.watchlistService.SaveMTFBacktest(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "unique_key": req.UniqueKey})
}

func (h *WatchlistHandler) TriggerMTFPredict(c *gin.Context) {
	var req models.MTFPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	status, body, err := h.watchlistService.TriggerMTFPredict(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) TriggerMTFPredictBestAuthorized(c *gin.Context) {
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	user, ok := userValue.(*models.User)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.MTFBestTrainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedReq, err := services.NormalizeMTFBestTrainRequest(&req, user.MembershipLevel, user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	status, body, err := h.watchlistService.TriggerMTFPredict(normalizedReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) TriggerMTFPredictOnce(c *gin.Context) {
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	user, ok := userValue.(*models.User)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.MTFPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedReq, err := services.NormalizeMTFPredictOnceRequest(&req, user.MembershipLevel, user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	status, body, err := h.watchlistService.TriggerMTFPredictOnce(normalizedReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) TriggerPaidMTFPredictOnce(c *gin.Context) {
	credential := extractPaymentCredential(c.GetHeader("Authorization"))
	if credential == "" {
		h.writePaymentRequired(c)
		return
	}
	var req models.MTFPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := h.watchlistService.Config()
	resourceID := cfg.AlipayService.ResourceID
	if resourceID == "" {
		resourceID = "mtf.predict.once"
	}
	verifyClient := services.NewAlipayServiceClient(cfg.AlipayService)
	verifyResult, err := verifyClient.Verify(c.Request.Context(), services.AlipayVerifyRequest{
		Credential: credential,
		ResourceID: resourceID,
		OrderID:    c.GetHeader("X-Alipay-Order-Id"),
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if !verifyResult.Valid {
		h.writePaymentRequired(c)
		return
	}
	orderID := strings.TrimSpace(c.GetHeader("X-Alipay-Order-Id"))
	if orderID == "" {
		orderID = strings.TrimSpace(verifyResult.OrderID)
	}
	if req.UserID == nil {
		userID := 0
		req.UserID = &userID
	}
	record := services.PaidPredictOnceRecord{
		ResourceID: resourceID,
		OrderID:    orderID,
		Credential: credential,
		Request:    &req,
	}
	run, err := h.watchlistService.BeginPaidPredictOnce(c.Request.Context(), record)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if run.InProgress {
		c.JSON(http.StatusConflict, gin.H{"error": "paid prediction is already processing"})
		return
	}
	if !run.ShouldRun {
		c.JSON(run.Status, run.Body)
		return
	}
	status, body, err := h.watchlistService.TriggerMTFPredictOnce(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := h.watchlistService.CompletePaidPredictOnce(c.Request.Context(), record, status, body); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) GetMTFPredictOnceCached(c *gin.Context) {
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	user, ok := userValue.(*models.User)
	if !ok || user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.MTFPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedReq, err := services.NormalizeMTFPredictOnceRequest(&req, user.MembershipLevel, user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	status, body, err := h.watchlistService.GetMTFPredictOnceCached(normalizedReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) GetMTFJobStatus(c *gin.Context) {
	jobID := c.Param("jobID")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "jobID is required"})
		return
	}
	status, body, err := h.watchlistService.GetMTFJobStatus(jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) RunMTFBacktestProxy(c *gin.Context) {
	var req models.MTFBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if uid, ok := c.Get("user_id"); ok {
		if req.UserID == nil {
			u := uid.(int)
			req.UserID = &u
		}
	}
	status, body, err := h.watchlistService.RunMTFBacktest(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

// SaveMTFBest
func (h *WatchlistHandler) SaveMTFBest(c *gin.Context) {
	var req models.SaveMTFBestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.watchlistService.SaveMTFBest(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "unique_key": req.UniqueKey})
}

// SaveMTFValChunk
func (h *WatchlistHandler) SaveMTFValChunk(c *gin.Context) {
	var req models.SaveMTFValChunkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.watchlistService.SaveMTFValChunk(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "unique_key": req.UniqueKey, "chunk_index": req.ChunkIndex})
}
