package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	if len(req.Symbols) == 0 && len(req.Items) == 0 {
		c.JSON(http.StatusOK, gin.H{"quotes": []models.LatestQuote{}})
		return
	}

	quotes, err := h.watchlistService.GetLatestQuotes(&req)
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

func (h *WatchlistHandler) GetMTFBestValueByUniqueKey(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if uniqueKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	item, err := h.watchlistService.GetMTFBestValueByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"value": item})
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

func normalizeAdjustRawChunk(value interface{}) map[string]interface{} {
	switch typed := value.(type) {
	case nil:
		return nil
	case map[string]interface{}:
		return typed
	case []interface{}:
		for _, item := range typed {
			if chunk, ok := item.(map[string]interface{}); ok {
				return chunk
			}
		}
	case []map[string]interface{}:
		if len(typed) > 0 {
			return typed[0]
		}
	}
	return nil
}

func adjustRawSeries(raw map[string]interface{}, field string, bestKey string, toFloatSlice func(interface{}) []float64) []float64 {
	if raw == nil {
		return nil
	}
	value, ok := raw[field]
	if !ok {
		return nil
	}
	if bestKey != "" {
		if byKey, ok := value.(map[string]interface{}); ok {
			return toFloatSlice(byKey[bestKey])
		}
	}
	return toFloatSlice(value)
}

func adjustRawDates(raw map[string]interface{}) []string {
	if raw == nil {
		return nil
	}
	rawDates, ok := raw["dates"].([]interface{})
	if !ok {
		return nil
	}
	dates := make([]string, 0, len(rawDates))
	for _, value := range rawDates {
		date := strings.TrimSpace(fmt.Sprint(value))
		if date != "" {
			dates = append(dates, date)
		}
	}
	return dates
}

func filterFloatByIndexes(values []float64, indexes []int) []float64 {
	if len(values) == 0 || len(indexes) == 0 {
		return []float64{}
	}
	out := make([]float64, 0, len(indexes))
	for _, index := range indexes {
		if index >= 0 && index < len(values) {
			out = append(out, values[index])
		}
	}
	return out
}

func filterStringByIndexes(values []string, indexes []int) []string {
	if len(values) == 0 || len(indexes) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		if index >= 0 && index < len(values) {
			out = append(out, values[index])
		}
	}
	return out
}

func filterAdjustRawChunk(raw map[string]interface{}, indexes []int, dates []string, actual []float64, pred []float64, actualChange []float64, predChange []float64, bestKey string) map[string]interface{} {
	if raw == nil {
		return nil
	}
	filtered := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		filtered[key] = value
	}
	if len(dates) > 0 {
		filtered["dates"] = filterStringByIndexes(dates, indexes)
	}
	if len(actual) > 0 {
		filtered["actual_values"] = filterFloatByIndexes(actual, indexes)
	}
	if len(pred) > 0 && bestKey != "" {
		filtered["predictions"] = map[string]interface{}{
			bestKey: filterFloatByIndexes(pred, indexes),
		}
	}
	if len(actualChange) > 0 {
		filtered["actual_change_percent"] = filterFloatByIndexes(actualChange, indexes)
	}
	if len(predChange) > 0 && bestKey != "" {
		filtered["predicted_change_percent"] = map[string]interface{}{
			bestKey: filterFloatByIndexes(predChange, indexes),
		}
	}
	return filtered
}

func slimMTFBestPredictionResponse(item models.MTFBestPrediction) gin.H {
	best := gin.H{
		"unique_key":           item.UniqueKey,
		"symbol":               item.Symbol,
		"mtf_version":          item.MTFVersion,
		"best_prediction_item": item.BestPredictionItem,
		"best_metrics":         item.BestMetrics,
		"prediction_type":      item.PredictionType,
		"short_name":           item.ShortName,
		"watchlist_count":      item.WatchlistCount,
		"context_len":          item.ContextLen,
		"horizon_len":          item.HorizonLen,
		"stock_type":           item.StockType,
		"created_at":           item.CreatedAt,
		"updated_at":           item.UpdatedAt,
		"covariate_signature":  item.CovariateSignature,
	}
	if item.CovariateSignature == "" {
		delete(best, "covariate_signature")
	}
	return best
}

func slimAccessibleMTFBestResponse(best gin.H) gin.H {
	out := gin.H{
		"unique_key":           best["unique_key"],
		"symbol":               best["symbol"],
		"mtf_version":          best["mtf_version"],
		"best_prediction_item": best["best_prediction_item"],
		"best_metrics":         slimBestMetrics(best["best_metrics"]),
		"prediction_type":      best["prediction_type"],
		"watchlist_count":      best["watchlist_count"],
		"context_len":          best["context_len"],
		"horizon_len":          best["horizon_len"],
		"updated_at":           best["updated_at"],
	}
	if value, ok := best["short_name"]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
		out["short_name"] = value
	}
	if value, ok := best["covariate_signature"]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
		out["covariate_signature"] = value
	}
	return out
}

func slimBestMetrics(value interface{}) gin.H {
	metrics := gin.H{}
	var raw map[string]interface{}
	switch v := value.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &raw); err != nil {
			return metrics
		}
	case map[string]interface{}:
		raw = v
	case gin.H:
		raw = map[string]interface{}(v)
	default:
		return metrics
	}
	if composite, ok := raw["composite_score"]; ok {
		metrics["composite_score"] = composite
	}
	return metrics
}

func slimMTFValidationChunkResponse(chunk models.SaveMTFValChunkRequest) gin.H {
	out := gin.H{
		"chunk_index":              chunk.ChunkIndex,
		"start_date":               chunk.StartDate,
		"end_date":                 chunk.EndDate,
		"symbol":                   chunk.Symbol,
		"stock_type":               chunk.StockType,
		"predictions":              chunk.Predictions,
		"actual_values":            chunk.Actual,
		"predicted_change_percent": chunk.PredictedChangePct,
		"actual_change_percent":    chunk.ActualChangePct,
		"change_base_value":        chunk.ChangeBaseValue,
		"change_base_date":         chunk.ChangeBaseDate,
		"dates":                    chunk.Dates,
		"prediction_type":          chunk.PredictionType,
	}
	if chunk.ChangeBaseValue == nil {
		delete(out, "change_base_value")
	}
	if chunk.ChangeBaseDate == nil {
		delete(out, "change_base_date")
	}
	if len(chunk.PredictedChangePct) == 0 {
		delete(out, "predicted_change_percent")
	}
	if len(chunk.ActualChangePct) == 0 {
		delete(out, "actual_change_percent")
	}
	return out
}

type latestActualPoint struct {
	Date  string
	Price float64
}

func latestActualPointKey(symbol string, stockType int) string {
	return fmt.Sprintf("%d:%s", stockType, normalizeMTFResponseSymbol(symbol))
}

func slimAccessibleMTFValidationChunkResponse(chunk gin.H, bestKey string) gin.H {
	out := gin.H{
		"start_date":    chunk["start_date"],
		"actual_values": chunk["actual_values"],
		"dates":         chunk["dates"],
	}
	if stockType := ginHInt(chunk, "stock_type"); stockType == 1 {
		out["stock_type"] = stockType
	}
	if predictions := pickPredictionSeries(chunk["predictions"], bestKey); len(predictions) > 0 {
		out["predictions"] = predictions
	}
	if value, ok := chunk["actual_change_percent"]; ok {
		out["actual_change_percent"] = value
	}
	if value, ok := chunk["predicted_change_percent"]; ok {
		if predictedChange := pickPredictionSeries(value, bestKey); len(predictedChange) > 0 {
			out["predicted_change_percent"] = predictedChange
		}
	}
	if raw := slimAdjustRawChunk(chunk["adjust_raw_chunks"], bestKey); hasSlimAdjustRawChunk(raw) {
		out["adjust_raw_chunks"] = raw
	}
	return out
}

func latestDrawableAccessibleChunk(chunks []gin.H, bestKey string) (gin.H, bool) {
	var latest gin.H
	var latestTime time.Time
	for _, chunk := range chunks {
		if !isDrawableAccessibleChunk(chunk, bestKey) {
			continue
		}
		chunkTime := parseMTFChunkDate(fmt.Sprint(chunk["start_date"]))
		if latest == nil || chunkTime.After(latestTime) {
			latest = chunk
			latestTime = chunkTime
		}
	}
	if latest == nil {
		return nil, false
	}
	return latest, true
}

func isDrawableAccessibleChunk(chunk gin.H, bestKey string) bool {
	if len(interfaceToStringSlice(chunk["dates"])) == 0 {
		return false
	}
	if len(interfaceToFloatSlice(chunk["actual_values"])) == 0 {
		return false
	}
	predictions := pickPredictionSeries(chunk["predictions"], bestKey)
	if len(predictions) == 0 {
		return false
	}
	for _, series := range predictions {
		return len(interfaceToFloatSlice(series)) > 0
	}
	return false
}

func parseMTFChunkDate(value string) time.Time {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02", "20060102", time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func appendLatestActualPointToLastChunk(chunks []gin.H, latest latestActualPoint) []gin.H {
	if len(chunks) == 0 || latest.Date == "" || !(latest.Price > 0) {
		return chunks
	}
	last := chunks[len(chunks)-1]
	dates := interfaceToStringSlice(last["dates"])
	if len(dates) == 0 {
		return chunks
	}
	actuals := interfaceToFloatSlice(last["actual_values"])
	if len(actuals) == 0 {
		return chunks
	}
	lastDate := strings.TrimSpace(dates[len(dates)-1])
	if lastDate == latest.Date || lastDate > latest.Date {
		return chunks
	}
	nextDates := append(append([]string{}, dates...), latest.Date)
	nextActuals := append(append([]float64{}, actuals...), latest.Price)
	last["dates"] = nextDates
	last["actual_values"] = nextActuals
	if changes := interfaceToFloatSlice(last["actual_change_percent"]); len(changes) > 0 {
		change := 0.0
		if len(actuals) > 0 && actuals[len(actuals)-1] > 0 {
			change = (latest.Price - actuals[len(actuals)-1]) / actuals[len(actuals)-1] * 100
		}
		last["actual_change_percent"] = append(append([]float64{}, changes...), change)
	}
	chunks[len(chunks)-1] = last
	return chunks
}

func interfaceToStringSlice(value interface{}) []string {
	switch arr := value.(type) {
	case []string:
		return arr
	case []interface{}:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	}
	return nil
}

func interfaceToFloatSlice(value interface{}) []float64 {
	out := []float64{}
	switch arr := value.(type) {
	case []float64:
		return arr
	case []float32:
		for _, item := range arr {
			out = append(out, float64(item))
		}
	case []int:
		for _, item := range arr {
			out = append(out, float64(item))
		}
	case []int64:
		for _, item := range arr {
			out = append(out, float64(item))
		}
	case []interface{}:
		for _, item := range arr {
			switch v := item.(type) {
			case float64:
				out = append(out, v)
			case float32:
				out = append(out, float64(v))
			case int:
				out = append(out, float64(v))
			case int64:
				out = append(out, float64(v))
			case json.Number:
				if f, err := v.Float64(); err == nil {
					out = append(out, f)
				}
			case string:
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					out = append(out, f)
				}
			}
		}
	}
	return out
}

func pickPredictionSeries(value interface{}, bestKey string) gin.H {
	if bestKey == "" {
		return gin.H{}
	}
	switch predictions := value.(type) {
	case map[string]interface{}:
		if series, ok := predictions[bestKey]; ok {
			return gin.H{bestKey: series}
		}
	case gin.H:
		if series, ok := predictions[bestKey]; ok {
			return gin.H{bestKey: series}
		}
	}
	return gin.H{}
}

func ginHInt(values gin.H, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return n
		}
	}
	return 0
}

func hasSlimAdjustRawChunk(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return false
	case gin.H:
		return len(v) > 0
	case []gin.H:
		return len(v) > 0
	default:
		return true
	}
}

func slimAdjustRawChunk(value interface{}, bestKey string) interface{} {
	if value == nil {
		return nil
	}
	filter := func(raw map[string]interface{}) gin.H {
		out := gin.H{}
		for _, key := range []string{"dates", "actual_values", "actual_change_percent", "change_base_value", "change_base_date"} {
			if v, ok := raw[key]; ok {
				out[key] = v
			}
		}
		if v, ok := raw["predictions"]; ok {
			if predictions := pickPredictionSeries(v, bestKey); len(predictions) > 0 {
				out["predictions"] = predictions
			}
		}
		if v, ok := raw["predicted_change_percent"]; ok {
			if predictedChange := pickPredictionSeries(v, bestKey); len(predictedChange) > 0 {
				out["predicted_change_percent"] = predictedChange
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	switch raw := value.(type) {
	case map[string]interface{}:
		return filter(raw)
	case gin.H:
		return filter(map[string]interface{}(raw))
	case []interface{}:
		items := make([]gin.H, 0, len(raw))
		for _, item := range raw {
			if m, ok := item.(map[string]interface{}); ok {
				if slim := filter(m); len(slim) > 0 {
					items = append(items, slim)
				}
			}
		}
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

func slimAccessibleMTFBestItems(items []gin.H, latestByKey map[string]latestActualPoint) []gin.H {
	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		best, ok := item["best"].(gin.H)
		if !ok {
			result = append(result, item)
			continue
		}
		bestKey := strings.TrimSpace(fmt.Sprint(best["best_prediction_item"]))
		slimChunks := []gin.H{}
		if chunks, ok := item["chunks"].([]gin.H); ok {
			if chunk, ok := latestDrawableAccessibleChunk(chunks, bestKey); ok {
				slimChunks = append(slimChunks, slimAccessibleMTFValidationChunkResponse(chunk, bestKey))
			}
		}
		stockType := 0
		switch value := best["stock_type"].(type) {
		case int:
			stockType = value
		case int64:
			stockType = int(value)
		case float64:
			stockType = int(value)
		}
		if latest, ok := latestByKey[latestActualPointKey(fmt.Sprint(best["symbol"]), stockType)]; ok {
			slimChunks = appendLatestActualPointToLastChunk(slimChunks, latest)
		}
		out := gin.H{
			"best":   slimAccessibleMTFBestResponse(best),
			"chunks": slimChunks,
		}
		if value, ok := item["max_deviation_percent"]; ok {
			out["max_deviation_percent"] = value
		}
		result = append(result, out)
	}
	return result
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
			rawChunk := normalizeAdjustRawChunk(chunk.AdjustRawChunks)
			rawPred := adjustRawSeries(rawChunk, "predictions", it.BestPredictionItem, toFloatSlice)
			rawActual := adjustRawSeries(rawChunk, "actual_values", "", toFloatSlice)
			rawPredChange := adjustRawSeries(rawChunk, "predicted_change_percent", it.BestPredictionItem, toFloatSlice)
			rawActualChange := adjustRawSeries(rawChunk, "actual_change_percent", "", toFloatSlice)
			rawDates := adjustRawDates(rawChunk)
			metricActual := chunk.Actual
			metricPred := bestPred
			metricActualChange := chunk.ActualChangePct
			metricPredChange := bestPredChange
			if chunk.StockType == 1 && len(rawActual) > 0 && len(rawPred) > 0 {
				metricActual = rawActual
				metricPred = rawPred
				if len(rawActualChange) > 0 {
					metricActualChange = rawActualChange
				}
				if len(rawPredChange) > 0 {
					metricPredChange = rawPredChange
				}
			}
			metricDates := chunk.Dates
			if chunk.StockType == 1 && len(rawDates) > 0 {
				metricDates = rawDates
			}

			// align-filter: remove points where actual==0 or predicted==0 or invalid, keeping indices consistent across actual/pred/dates
			filteredActual := make([]float64, 0, len(metricActual))
			filteredPred := make([]float64, 0, len(metricPred))
			filteredDates := make([]string, 0, len(chunk.Dates))
			filteredActualChange := make([]float64, 0, len(chunk.ActualChangePct))
			filteredPredChange := make([]float64, 0, len(bestPredChange))
			keptIndexes := make([]int, 0, len(metricActual))

			maxLen := len(metricActual)
			if len(metricPred) < maxLen {
				maxLen = len(metricPred)
			}
			if len(metricDates) < maxLen {
				maxLen = len(metricDates)
			}
			for i := 0; i < maxLen; i++ {
				a := metricActual[i]
				p := metricPred[i]
				if a == 0 || p == 0 || math.IsNaN(a) || math.IsNaN(p) || math.IsInf(a, 0) || math.IsInf(p, 0) {
					continue
				}
				keptIndexes = append(keptIndexes, i)
				filteredActual = append(filteredActual, a)
				filteredPred = append(filteredPred, p)
				filteredDates = append(filteredDates, metricDates[i])
				if i < len(metricActualChange) {
					filteredActualChange = append(filteredActualChange, metricActualChange[i])
				}
				if i < len(metricPredChange) {
					filteredPredChange = append(filteredPredChange, metricPredChange[i])
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
			if chunk.StockType == 1 && rawChunk != nil {
				chunk.AdjustRawChunks = filterAdjustRawChunk(rawChunk, keptIndexes, rawDates, rawActual, rawPred, rawActualChange, rawPredChange, it.BestPredictionItem)
			}

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

		slimChunks := make([]gin.H, 0, len(chunks))
		for _, chunk := range chunks {
			slimChunks = append(slimChunks, slimMTFValidationChunkResponse(chunk))
		}
		result = append(result, gin.H{
			"best":                  slimMTFBestPredictionResponse(it),
			"chunks":                slimChunks,
			"max_deviation_percent": maxDevPercent,
		})
	}

	return result, nil
}

func groupMTFBestVariantsBySymbol(items []gin.H) []gin.H {
	type symbolGroup struct {
		symbol   string
		name     string
		stockTyp int
		variants []gin.H
	}
	groups := make([]symbolGroup, 0)
	groupByKey := make(map[string]int)

	for _, item := range items {
		best, ok := item["best"].(gin.H)
		if !ok {
			groups = append(groups, symbolGroup{variants: []gin.H{item}})
			continue
		}
		symbol := strings.TrimSpace(fmt.Sprint(best["symbol"]))
		if symbol == "" {
			groups = append(groups, symbolGroup{variants: []gin.H{item}})
			continue
		}
		key := normalizeMTFResponseSymbol(symbol)
		index, exists := groupByKey[key]
		if !exists {
			stockType := 0
			switch value := best["stock_type"].(type) {
			case int:
				stockType = value
			case int64:
				stockType = int(value)
			case float64:
				stockType = int(value)
			}
			groups = append(groups, symbolGroup{
				symbol:   symbol,
				name:     strings.TrimSpace(fmt.Sprint(best["short_name"])),
				stockTyp: stockType,
				variants: []gin.H{},
			})
			index = len(groups) - 1
			groupByKey[key] = index
		}
		groups[index].variants = append(groups[index].variants, item)
	}

	out := make([]gin.H, 0, len(groups))
	for _, group := range groups {
		item := gin.H{
			"symbol":   group.symbol,
			"variants": group.variants,
		}
		if group.name != "" {
			item["short_name"] = group.name
		}
		if group.stockTyp > 0 {
			item["stock_type"] = group.stockTyp
		}
		out = append(out, item)
	}
	return out
}

func normalizeMTFResponseSymbol(symbol string) string {
	trimmed := strings.ToLower(strings.TrimSpace(symbol))
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, trimmed)
	if digits != "" {
		return digits
	}
	return trimmed
}

// 公开查询：返回 is_public = 1 的 mtf-best，并联查对应的验证分块数据
func (h *WatchlistHandler) ListPublicMTFBestWithValidation(c *gin.Context) {
	horizonLen := 0
	if hStr := c.Query("horizon_len"); hStr != "" {
		if val, err := strconv.Atoi(hStr); err == nil {
			horizonLen = val
		}
	}
	limit := 0
	if limitStr := c.Query("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
		}
	}
	offset := 0
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if val, err := strconv.Atoi(offsetStr); err == nil && val > 0 {
			offset = val
		}
	}
	stockType := 0
	if stockTypeStr := c.Query("stock_type"); stockTypeStr != "" {
		if val, err := strconv.Atoi(stockTypeStr); err == nil && val > 0 {
			stockType = val
		}
	}
	symbol := c.Query("symbol")
	isAdmin, _ := c.Get("is_admin")
	includePrivate, _ := isAdmin.(bool)

	page, err := h.watchlistService.ListPublicMTFBestPageByStockType(horizonLen, symbol, includePrivate, limit, offset, stockType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := h.buildMTFBestWithValidationResponse(page.Items)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result = groupMTFBestVariantsBySymbol(result)

	c.JSON(http.StatusOK, gin.H{
		"items":    result,
		"count":    len(result),
		"total":    page.Total,
		"limit":    page.Limit,
		"offset":   page.Offset,
		"has_more": page.Limit > 0 && page.Offset+len(result) < page.Total,
	})
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
	latestByKey := map[string]latestActualPoint{}
	quoteReq := models.BatchSymbolsRequest{Items: make([]models.BatchSymbolInput, 0, len(items))}
	for _, item := range items {
		stockType := item.StockType
		if stockType != 1 && stockType != 2 {
			stockType = 1
		}
		quoteReq.Items = append(quoteReq.Items, models.BatchSymbolInput{Symbol: item.Symbol, StockType: stockType})
	}
	if len(quoteReq.Items) > 0 {
		quotes, quoteErr := h.watchlistService.GetLatestDailyQuotes(&quoteReq)
		if quoteErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": quoteErr.Error()})
			return
		}
		for index, quote := range quotes {
			if quote.TradingDate == nil || quote.LatestPrice == nil || index >= len(quoteReq.Items) {
				continue
			}
			item := quoteReq.Items[index]
			latestByKey[latestActualPointKey(item.Symbol, item.StockType)] = latestActualPoint{Date: *quote.TradingDate, Price: *quote.LatestPrice}
		}
	}
	result = slimAccessibleMTFBestItems(result, latestByKey)

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
