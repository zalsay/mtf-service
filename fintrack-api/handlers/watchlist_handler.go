package handlers

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"strconv"

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

// 查询某用户的TimesFM最佳分位预测列表
func (h *WatchlistHandler) ListTimesfmBestByUser(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	results, err := h.watchlistService.ListTimesfmBestByUserID(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"predictions": results,
		"count":       len(results),
	})
}

// 按 unique_key 查询单条 TimesFM 最佳分位预测（公开）
func (h *WatchlistHandler) GetTimesfmBestByUniqueKey(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	item, err := h.watchlistService.GetTimesfmBestByUniqueKey(uniqueKey)
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

func (h *WatchlistHandler) GetTimesfmBestUniqueKeysByConfig(c *gin.Context) {
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

	item, err := h.watchlistService.GetTimesfmBestUniqueKeysByConfig(symbol, horizonLen, contextLen, c.Query("timesfm_version"))
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

// 按 unique_key 查询单条 TimesFM 回测结果
func (h *WatchlistHandler) GetTimesfmBacktestByUniqueKey(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	item, err := h.watchlistService.GetTimesfmBacktestByUniqueKey(uniqueKey)
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

func (h *WatchlistHandler) buildTimesfmBestWithValidationResponse(items []models.TimesfmBestPrediction) ([]gin.H, error) {
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

// 公开查询：返回 is_public = 1 的 timesfm-best，并联查对应的验证分块数据
func (h *WatchlistHandler) ListPublicTimesfmBestWithValidation(c *gin.Context) {
	horizonLen := 0
	if hStr := c.Query("horizon_len"); hStr != "" {
		if val, err := strconv.Atoi(hStr); err == nil {
			horizonLen = val
		}
	}
	symbol := c.Query("symbol")
	isAdmin, _ := c.Get("is_admin")
	includePrivate, _ := isAdmin.(bool)

	items, err := h.watchlistService.ListPublicTimesfmBest(horizonLen, symbol, includePrivate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := h.buildTimesfmBestWithValidationResponse(items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": result, "count": len(result)})
}

func (h *WatchlistHandler) ListAccessibleTimesfmBestWithValidation(c *gin.Context) {
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

	items, err := h.watchlistService.ListAccessibleTimesfmBest(userIDValue.(int), horizonLen, symbol, includePrivate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := h.buildTimesfmBestWithValidationResponse(items)
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

// 保存 TimesFM 回测结果
func (h *WatchlistHandler) SaveTimesfmBacktest(c *gin.Context) {
	var req models.SaveTimesfmBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.watchlistService.SaveTimesfmBacktest(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "unique_key": req.UniqueKey})
}

func (h *WatchlistHandler) TriggerTimesfmPredict(c *gin.Context) {
	var req models.TimesfmPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	status, body, err := h.watchlistService.TriggerTimesfmPredict(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) TriggerTimesfmPredictBestAuthorized(c *gin.Context) {
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

	var req models.TimesfmBestTrainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedReq, err := services.NormalizeTimesfmBestTrainRequest(&req, user.MembershipLevel, user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	status, body, err := h.watchlistService.TriggerTimesfmPredict(normalizedReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) TriggerTimesfmPredictOnce(c *gin.Context) {
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

	var req models.TimesfmPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedReq, err := services.NormalizeTimesfmPredictOnceRequest(&req, user.MembershipLevel, user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}

	status, body, err := h.watchlistService.TriggerTimesfmPredictOnce(normalizedReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) GetTimesfmPredictOnceCached(c *gin.Context) {
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

	var req models.TimesfmPredictRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	normalizedReq, err := services.NormalizeTimesfmPredictOnceRequest(&req, user.MembershipLevel, user.ID, user.IsAdmin)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		return
	}
	status, body, err := h.watchlistService.GetTimesfmPredictOnceCached(normalizedReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) GetTimesfmJobStatus(c *gin.Context) {
	jobID := c.Param("jobID")
	if jobID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "jobID is required"})
		return
	}
	status, body, err := h.watchlistService.GetTimesfmJobStatus(jobID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

func (h *WatchlistHandler) RunTimesfmBacktestProxy(c *gin.Context) {
	var req models.TimesfmBacktestRequest
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
	status, body, err := h.watchlistService.RunTimesfmBacktest(&req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(status, body)
}

// SaveTimesfmBest
func (h *WatchlistHandler) SaveTimesfmBest(c *gin.Context) {
	var req models.SaveTimesfmBestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.watchlistService.SaveTimesfmBest(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "unique_key": req.UniqueKey})
}

// SaveTimesfmValChunk
func (h *WatchlistHandler) SaveTimesfmValChunk(c *gin.Context) {
	var req models.SaveTimesfmValChunkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.watchlistService.SaveTimesfmValChunk(&req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "unique_key": req.UniqueKey, "chunk_index": req.ChunkIndex})
}
