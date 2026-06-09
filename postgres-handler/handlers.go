package main

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func buildFutureDatesKey(dates []string) string {
	normalized := make([]string, 0, len(dates))
	for _, date := range dates {
		trimmed := strings.TrimSpace(date)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return strings.Join(normalized, ",")
}

func buildMTFDirectUniqueKey(symbol string, stockType int, horizonLen int, contextLen int, futureDatesKey string) string {
	sum := sha1.Sum([]byte(futureDatesKey))
	return fmt.Sprintf(
		"%s_direct_st_%d_hlen_%d_clen_%d_fd_%x",
		strings.TrimSpace(symbol),
		stockType,
		horizonLen,
		contextLen,
		sum[:8],
	)
}

func marshalOptionalJSONObject(value interface{}) (string, error) {
	if value == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "{}", nil
	}
	return string(raw), nil
}

func normalizedPredictionType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "non_cov", "non-cov", "lite", "mtf_lite", "mtf-lite":
		return "mtf-lite"
	case "cov", "pro", "mtf_pro", "mtf-pro":
		return "mtf-pro"
	default:
		return strings.TrimSpace(value)
	}
}

func predictionTypeUsesCovariates(value string) bool {
	return normalizedPredictionType(value) == "mtf-pro"
}

func (h *DatabaseHandler) buildActualValuesForDates(ctx context.Context, symbol string, stockType int, startDateStr string, endDateStr string, dates []string) ([]float64, string, error) {
	if len(dates) == 0 {
		return nil, "", fmt.Errorf("dates are required")
	}
	if strings.TrimSpace(startDateStr) == "" {
		startDateStr = dates[0]
	}
	if strings.TrimSpace(endDateStr) == "" {
		endDateStr = dates[len(dates)-1]
	}

	records, _, provider, err := h.LoadTickflowHistory(ctx, TickflowHistoryRequest{
		Symbol:    symbol,
		StockType: stockType,
		StartDate: startDateStr,
		EndDate:   endDateStr,
		Adjust:    "none",
	})
	if err == nil && len(records) > 0 {
		closeByDate := make(map[string]float64, len(records))
		for _, record := range records {
			dateKey := strings.TrimSpace(record.DateStr)
			if dateKey == "" {
				dateKey = strings.TrimSpace(record.TradeDate)
			}
			if dateKey == "" && len(record.Datetime) >= len("2006-01-02") {
				dateKey = record.Datetime[:len("2006-01-02")]
			}
			if dateKey != "" {
				closeByDate[dateKey] = record.Close
			}
		}
		values := make([]float64, 0, len(dates))
		for _, date := range dates {
			value, ok := closeByDate[date]
			if !ok {
				return nil, provider, fmt.Errorf("provider %s missing close for date %s", provider, date)
			}
			values = append(values, value)
		}
		return values, provider, nil
	}

	sd, parseStartErr := time.Parse("2006-01-02", startDateStr)
	if parseStartErr != nil {
		return nil, provider, fmt.Errorf("invalid start_date format (YYYY-MM-DD)")
	}
	ed, parseEndErr := time.Parse("2006-01-02", endDateStr)
	if parseEndErr != nil {
		return nil, provider, fmt.Errorf("invalid end_date format (YYYY-MM-DD)")
	}
	rows, stockDataErr := h.GetStockDataByDateRange(symbol, stockType, sd, ed)
	if stockDataErr != nil {
		if err != nil {
			return nil, provider, fmt.Errorf("provider failed: %v; stock_data failed: %v", err, stockDataErr)
		}
		return nil, provider, fmt.Errorf("stock_data failed: %v", stockDataErr)
	}
	closeByDate := make(map[string]float64, len(rows))
	for _, row := range rows {
		closeByDate[row.Datetime.Format("2006-01-02")] = row.Close
	}
	values := make([]float64, 0, len(dates))
	for _, date := range dates {
		value, ok := closeByDate[date]
		if !ok {
			return nil, "stock_data", fmt.Errorf("stock_data missing close for date %s", date)
		}
		values = append(values, value)
	}
	return values, "stock_data", nil
}

func (h *DatabaseHandler) batchInsertMTFForecastHandler(c *gin.Context) {
	var req []struct {
		Symbol          string  `json:"symbol"`
		Ds              string  `json:"ds"`
		Tsf             float64 `json:"tsf"`
		Tsf01           float64 `json:"tsf_01"`
		Tsf02           float64 `json:"tsf_02"`
		Tsf03           float64 `json:"tsf_03"`
		Tsf04           float64 `json:"tsf_04"`
		Tsf05           float64 `json:"tsf_05"`
		Tsf06           float64 `json:"tsf_06"`
		Tsf07           float64 `json:"tsf_07"`
		Tsf08           float64 `json:"tsf_08"`
		Tsf09           float64 `json:"tsf_09"`
		ChunkIndex      int     `json:"chunk_index"`
		BestQuantile    string  `json:"best_quantile"`
		BestQuantilePct string  `json:"best_quantile_pct"`
		BestPredPct     float64 `json:"best_pred_pct"`
		ActualPct       float64 `json:"actual_pct"`
		DiffPct         float64 `json:"diff_pct"`
		MSE             float64 `json:"mse"`
		MAE             float64 `json:"mae"`
		CombinedScore   float64 `json:"combined_score"`
		UserID          int     `json:"user_id"`
		Version         float64 `json:"version"`
		HorizonLen      int     `json:"horizon_len"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty list"})
		return
	}
	list := make([]MTFForecast, 0, len(req))
	for i, v := range req {
		if v.Symbol == "" || v.Ds == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d missing symbol or ds", i)})
			return
		}
		var t time.Time
		var err error
		layouts := []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02"}
		for _, layout := range layouts {
			t, err = time.Parse(layout, v.Ds)
			if err == nil {
				break
			}
		}
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid ds format: %s", v.Ds)})
			return
		}
		list = append(list, MTFForecast{
			Symbol: v.Symbol, Ds: t, Tsf: v.Tsf, Tsf01: v.Tsf01, Tsf02: v.Tsf02, Tsf03: v.Tsf03, Tsf04: v.Tsf04, Tsf05: v.Tsf05, Tsf06: v.Tsf06, Tsf07: v.Tsf07, Tsf08: v.Tsf08, Tsf09: v.Tsf09,
			ChunkIndex: v.ChunkIndex, BestQuantile: v.BestQuantile, BestQuantilePct: v.BestQuantilePct, BestPredPct: v.BestPredPct, ActualPct: v.ActualPct, DiffPct: v.DiffPct, MSE: v.MSE, MAE: v.MAE, CombinedScore: v.CombinedScore,
			UserID: v.UserID, Version: v.Version, HorizonLen: v.HorizonLen,
		})
	}
	if err := h.BatchInsertMTFForecast(list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success"})
}

func (h *DatabaseHandler) getMTFForecastBySymbolVersionHorizon(c *gin.Context) {
	var req struct {
		Symbol     string  `json:"symbol"`
		Version    float64 `json:"version"`
		HorizonLen int     `json:"horizon_len"`
		Limit      *int    `json:"limit"`
		Offset     *int    `json:"offset"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.Symbol) == "" || req.HorizonLen <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol and horizon_len are required"})
		return
	}
	limit := 200
	offset := 0
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}
	if req.Offset != nil && *req.Offset >= 0 {
		offset = *req.Offset
	}
	rows, err := h.db.Raw(`
        SELECT symbol, ds, tsf, tsf_01, tsf_02, tsf_03, tsf_04, tsf_05, tsf_06, tsf_07, tsf_08, tsf_09,
               chunk_index, best_quantile, best_quantile_pct, best_pred_pct, actual_pct, diff_pct, mse, mae, combined_score,
               version, horizon_len
        FROM mtf_forecast
        WHERE symbol = $1 AND version = $2 AND horizon_len = $3
        ORDER BY ds ASC
        LIMIT $4 OFFSET $5`, req.Symbol, req.Version, req.HorizonLen, limit, offset).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []MTFForecast{}
	for rows.Next() {
		var v MTFForecast
		if err := rows.Scan(
			&v.Symbol, &v.Ds, &v.Tsf, &v.Tsf01, &v.Tsf02, &v.Tsf03, &v.Tsf04, &v.Tsf05, &v.Tsf06, &v.Tsf07, &v.Tsf08, &v.Tsf09,
			&v.ChunkIndex, &v.BestQuantile, &v.BestQuantilePct, &v.BestPredPct, &v.ActualPct, &v.DiffPct, &v.MSE, &v.MAE, &v.CombinedScore,
			&v.Version, &v.HorizonLen,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		list = append(list, v)
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: list})
}

func (h *DatabaseHandler) saveMTFBestHandler(c *gin.Context) {
	var req struct {
		UniqueKey          string                 `json:"unique_key"`
		Symbol             string                 `json:"symbol"`
		MTFVersion         string                 `json:"mtf_version"`
		BestPredictionItem string                 `json:"best_prediction_item"`
		BestMetrics        map[string]interface{} `json:"best_metrics"`
		PredictionType     string                 `json:"prediction_type"`
		CovariateConfig    map[string]interface{} `json:"covariate_config"`
		CovariateSignature string                 `json:"covariate_signature"`
		CovariateAnalysis  map[string]interface{} `json:"covariate_analysis"`
		IsPublic           *int                   `json:"is_public"`
		TrainStartDate     string                 `json:"train_start_date"`
		TrainEndDate       string                 `json:"train_end_date"`
		TestStartDate      string                 `json:"test_start_date"`
		TestEndDate        string                 `json:"test_end_date"`
		ValStartDate       string                 `json:"val_start_date"`
		ValEndDate         string                 `json:"val_end_date"`
		ContextLen         int                    `json:"context_len"`
		HorizonLen         int                    `json:"horizon_len"`
		ShortName          string                 `json:"short_name"`
		StockType          int                    `json:"stock_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		slog.Error("error binding json", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.UniqueKey) == "" || strings.TrimSpace(req.Symbol) == "" || strings.TrimSpace(req.MTFVersion) == "" || strings.TrimSpace(req.BestPredictionItem) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key, symbol, mtf_version, best_prediction_item are required"})
		return
	}
	metricsJSON, err := json.Marshal(req.BestMetrics)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "best_metrics must be JSON object"})
		return
	}
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_config must be JSON object"})
		return
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_analysis must be JSON object"})
		return
	}
	covSignature := strings.TrimSpace(req.CovariateSignature)
	// MTF best 结果统一公开，便于不同用户复用历史走势。
	isPublic := 1
	if strings.TrimSpace(req.ShortName) == "" {
		if req.StockType == 2 {
			// Try getting ETF data to fill ShortName. Use offset 0 to get the latest record.
			etfData, errEtf := h.GetEtfDaily(req.Symbol, 1, 0)
			// slog.Info("GetEtfDaily", "symbol", req.Symbol, "data", etfData, "err", errEtf)
			if errEtf == nil {
				if len(etfData) > 0 {
					req.ShortName = etfData[0].Name
				}
			}
		}
		if req.StockType == 1 {
			code := strings.TrimLeft(req.Symbol, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
			stockData, errStock := h.GetAStockCommentDailyByCode(code, 1, 0)
			// slog.Info("GetAStockCommentDailyByCode", "symbol", req.Symbol, "data", stockData, "err", errStock)
			if errStock == nil {
				if len(stockData) > 0 {
					req.ShortName = stockData[0].Name
				}
			}
		}
	}
	predictionType := normalizedPredictionType(req.PredictionType)
	err = h.db.Exec(`
        INSERT INTO mtf_best_predictions (
            unique_key, symbol, mtf_version, best_prediction_item, best_metrics,
            prediction_type, covariate_config, covariate_signature, covariate_analysis,
            is_public,
            train_start_date, train_end_date,
            test_start_date, test_end_date,
            val_start_date, val_end_date,
            context_len, horizon_len, short_name, stock_type
        ) VALUES (
            $1, $2, $3, $4, $5::jsonb,
            $6, $7::jsonb, $8, $9::jsonb,
            $10,
            $11::date, $12::date,
            $13::date, $14::date,
            $15::date, $16::date,
            $17, $18, $19, $20
        )
        ON CONFLICT (unique_key) DO UPDATE SET
            symbol = EXCLUDED.symbol,
            mtf_version = EXCLUDED.mtf_version,
            best_prediction_item = EXCLUDED.best_prediction_item,
            best_metrics = EXCLUDED.best_metrics,
            prediction_type = EXCLUDED.prediction_type,
            covariate_config = EXCLUDED.covariate_config,
            covariate_signature = EXCLUDED.covariate_signature,
            covariate_analysis = EXCLUDED.covariate_analysis,
            is_public = EXCLUDED.is_public,
            train_start_date = EXCLUDED.train_start_date,
            train_end_date = EXCLUDED.train_end_date,
            test_start_date = EXCLUDED.test_start_date,
            test_end_date = EXCLUDED.test_end_date,
            val_start_date = EXCLUDED.val_start_date,
            val_end_date = EXCLUDED.val_end_date,
            context_len = EXCLUDED.context_len,
            horizon_len = EXCLUDED.horizon_len,
            short_name = EXCLUDED.short_name,
			stock_type = EXCLUDED.stock_type,
            updated_at = CURRENT_TIMESTAMP`,
		req.UniqueKey, req.Symbol, req.MTFVersion, req.BestPredictionItem, string(metricsJSON),
		predictionType, covConfigJSON, covSignature, covAnalysisJSON,
		isPublic,
		req.TrainStartDate, req.TrainEndDate,
		req.TestStartDate, req.TestEndDate,
		req.ValStartDate, req.ValEndDate,
		req.ContextLen, req.HorizonLen, req.ShortName, req.StockType,
	).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to upsert mtf_best_predictions: %v", err)})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"unique_key": req.UniqueKey}})
}

func (h *DatabaseHandler) saveMTFValChunkHandler(c *gin.Context) {
	var req struct {
		UniqueKey          string                 `json:"unique_key"`
		ChunkIndex         int                    `json:"chunk_index"`
		StartDate          string                 `json:"start_date"`
		EndDate            string                 `json:"end_date"`
		Symbol             string                 `json:"symbol"`
		UserID             *int                   `json:"user_id"`
		Predictions        map[string]interface{} `json:"predictions"`
		Actual             []float64              `json:"actual_values"`
		PredictedChangePct map[string]interface{} `json:"predicted_change_percent"`
		ActualChangePct    []float64              `json:"actual_change_percent"`
		ChangeBaseValue    *float64               `json:"change_base_value"`
		ChangeBaseDate     string                 `json:"change_base_date"`
		AdjustRawChunks    json.RawMessage        `json:"adjust_raw_chunks"`
		Dates              []string               `json:"dates"`
		PredictionType     string                 `json:"prediction_type"`
		CovariateConfig    map[string]interface{} `json:"covariate_config"`
		CovariateSignature string                 `json:"covariate_signature"`
		CovariateAnalysis  map[string]interface{} `json:"covariate_analysis"`
		StockName          string                 `json:"stock_name"`
		StockType          int                    `json:"stock_type"`
		HorizonLen         int                    `json:"horizon_len"` // 预测长度，不保存
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	// 基本键必填：用于定位或创建记录
	if strings.TrimSpace(req.UniqueKey) == "" || req.ChunkIndex < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key and non-negative chunk_index are required"})
		return
	}
	if req.StockName == "" {
		if req.StockType == 2 {
			// Try getting ETF data to fill ShortName. Use offset 0 to get the latest record.
			etfData, errEtf := h.GetEtfDaily(req.Symbol, 1, 0)
			slog.Info("GetEtfDaily", "symbol", req.Symbol, "data", etfData, "err", errEtf)
			if errEtf == nil {
				if len(etfData) > 0 {
					req.StockName = etfData[0].Name
				}
			}
		}
		if req.StockType == 1 {
			code := strings.TrimLeft(req.Symbol, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
			stockData, errStock := h.GetAStockCommentDailyByCode(code, 1, 0)
			slog.Info("GetAStockCommentDailyByCode", "symbol", req.Symbol, "data", stockData, "err", errStock)
			if errStock == nil {
				if len(stockData) > 0 {
					req.StockName = stockData[0].Name
				}
			}
		}
	}
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_config must be JSON object"})
		return
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_analysis must be JSON object"})
		return
	}
	predictionType := normalizedPredictionType(req.PredictionType)
	covSignature := strings.TrimSpace(req.CovariateSignature)
	// 查询是否存在记录
	var existingID int
	row := h.db.Raw(`SELECT id FROM mtf_best_validation_chunks WHERE unique_key = $1 AND chunk_index = $2 LIMIT 1`, req.UniqueKey, req.ChunkIndex).Row()
	scanErr := row.Scan(&existingID)
	if scanErr != nil && scanErr != sql.ErrNoRows {
		c.JSON(http.StatusInternalServerError, gin.H{"error": scanErr.Error()})
		return
	}

	var providerActual []float64
	var providerActualSource string
	var providerActualErr error
	if len(req.Dates) > 0 && strings.TrimSpace(req.Symbol) != "" && req.StockType > 0 {
		providerActual, providerActualSource, providerActualErr = h.buildActualValuesForDates(
			c.Request.Context(),
			req.Symbol,
			req.StockType,
			req.StartDate,
			req.EndDate,
			req.Dates,
		)
		if providerActualErr != nil {
			slog.Error("failed to build actual_values from provider", "symbol", req.Symbol, "stock_type", req.StockType, "error", providerActualErr)
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("cannot build actual_values from provider: %v", providerActualErr)})
			return
		} else {
			slog.Info("rebuilt actual_values from provider", "symbol", req.Symbol, "stock_type", req.StockType, "provider", providerActualSource, "count", len(providerActual))
		}
	}

	// 如果存在，则只更新非空字段；否则执行插入（插入时必须提供所有 NOT NULL 字段）
	if scanErr == nil {
		// 动态构造 UPDATE 语句
		setParts := make([]string, 0)
		args := make([]interface{}, 0)

		if strings.TrimSpace(req.Symbol) != "" {
			setParts = append(setParts, fmt.Sprintf("symbol = $%d", len(args)+1))
			args = append(args, req.Symbol)
		}
		if strings.TrimSpace(req.StartDate) != "" {
			setParts = append(setParts, fmt.Sprintf("start_date = $%d::date", len(args)+1))
			args = append(args, req.StartDate)
		}
		if strings.TrimSpace(req.EndDate) != "" {
			setParts = append(setParts, fmt.Sprintf("end_date = $%d::date", len(args)+1))
			args = append(args, req.EndDate)
		}
		if req.Predictions != nil && len(req.Predictions) > 0 {
			predsJSON, err := json.Marshal(req.Predictions)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "predictions must be JSON object"})
				return
			}
			setParts = append(setParts, fmt.Sprintf("predictions = $%d::jsonb", len(args)+1))
			args = append(args, string(predsJSON))
		}
		// 优先按 dates 使用当前行情 provider 重建 actual_values，避免上游或旧 stock_data 口径污染。
		if providerActual != nil {
			actualJSON, _ := json.Marshal(providerActual)
			setParts = append(setParts, fmt.Sprintf("actual_values = $%d::jsonb", len(args)+1))
			args = append(args, string(actualJSON))
		} else if req.Actual != nil {
			actualJSON, _ := json.Marshal(req.Actual)
			setParts = append(setParts, fmt.Sprintf("actual_values = $%d::jsonb", len(args)+1))
			args = append(args, string(actualJSON))
		} else {
			// 未提供 actual_values，则尝试用数据库中对应日期的收盘价填充。
			// 日期来源优先使用 req.Dates；否则使用 req.StartDate 和 req.EndDate。
			var datesForActual []string
			if req.Dates != nil && len(req.Dates) > 0 {
				datesForActual = req.Dates
			}
			startDateStr := strings.TrimSpace(req.StartDate)
			endDateStr := strings.TrimSpace(req.EndDate)
			if (startDateStr == "" || endDateStr == "") && len(datesForActual) > 0 {
				startDateStr = datesForActual[0]
				endDateStr = datesForActual[len(datesForActual)-1]
			}
			if startDateStr == "" || endDateStr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "cannot infer date range to build actual_values"})
				return
			}
			sd, err := time.Parse("2006-01-02", startDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format (YYYY-MM-DD)"})
				return
			}
			ed, err := time.Parse("2006-01-02", endDateStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format (YYYY-MM-DD)"})
				return
			}
			slog.Info("query stock data by date range", "symbol", req.Symbol, "stock_type", req.StockType, "start_date", startDateStr, "end_date", endDateStr)
			actualVals := make([]float64, 0)
			// 统一使用 stock_data 表查询（ETF 也走此路径）
			rows, err := h.GetStockDataByDateRange(req.Symbol, req.StockType, sd, ed)
			if err != nil {
				slog.Error("failed to query stock data by date range", "error", err)
			}
			slog.Info("queried stock data rows", "count", len(rows))
			closeByDate := make(map[string]float64, len(rows))
			for _, r := range rows {
				d := r.Datetime.Format("2006-01-02")
				closeByDate[d] = r.Close
			}
			// 按 dates 逐日匹配，不依赖 horizon_len
			if len(datesForActual) > 0 {
				for _, d := range datesForActual {
					v, ok := closeByDate[d]
					if !ok {
						slog.Error("missing close value for date", "date", d)
					}
					actualVals = append(actualVals, v)
				}
			}
			req.Actual = actualVals
			actualJSON, _ := json.Marshal(req.Actual)
			setParts = append(setParts, fmt.Sprintf("actual_values = $%d::jsonb", len(args)+1))
			args = append(args, string(actualJSON))
		}
		if req.PredictedChangePct != nil {
			predictedChangeJSON, err := json.Marshal(req.PredictedChangePct)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "predicted_change_percent must be JSON object"})
				return
			}
			setParts = append(setParts, fmt.Sprintf("predicted_change_percent = $%d::jsonb", len(args)+1))
			args = append(args, string(predictedChangeJSON))
		}
		if req.ActualChangePct != nil {
			actualChangeJSON, _ := json.Marshal(req.ActualChangePct)
			setParts = append(setParts, fmt.Sprintf("actual_change_percent = $%d::jsonb", len(args)+1))
			args = append(args, string(actualChangeJSON))
		}
		if req.ChangeBaseValue != nil {
			setParts = append(setParts, fmt.Sprintf("change_base_value = $%d", len(args)+1))
			args = append(args, *req.ChangeBaseValue)
		}
		if strings.TrimSpace(req.ChangeBaseDate) != "" {
			setParts = append(setParts, fmt.Sprintf("change_base_date = $%d::date", len(args)+1))
			args = append(args, req.ChangeBaseDate)
		}
		if req.StockType == 1 && len(req.AdjustRawChunks) > 0 {
			if string(req.AdjustRawChunks) == "null" {
				setParts = append(setParts, "adjust_raw_chunks = NULL")
			} else {
				setParts = append(setParts, fmt.Sprintf("adjust_raw_chunks = $%d::jsonb", len(args)+1))
				args = append(args, string(req.AdjustRawChunks))
			}
		}
		if req.Dates != nil && len(req.Dates) > 0 {
			datesJSON, _ := json.Marshal(req.Dates)
			setParts = append(setParts, fmt.Sprintf("dates = $%d::jsonb", len(args)+1))
			args = append(args, string(datesJSON))
		}
		if req.UserID != nil {
			setParts = append(setParts, fmt.Sprintf("user_id = $%d", len(args)+1))
			args = append(args, *req.UserID)
		}
		if strings.TrimSpace(req.StockName) != "" {
			setParts = append(setParts, fmt.Sprintf("stock_name = $%d", len(args)+1))
			args = append(args, req.StockName)
		}
		if req.StockType > 0 {
			setParts = append(setParts, fmt.Sprintf("stock_type = $%d", len(args)+1))
			args = append(args, req.StockType)
		}
		setParts = append(setParts, fmt.Sprintf("prediction_type = $%d", len(args)+1))
		args = append(args, predictionType)
		if req.CovariateConfig != nil {
			setParts = append(setParts, fmt.Sprintf("covariate_config = $%d::jsonb", len(args)+1))
			args = append(args, covConfigJSON)
		}
		if req.CovariateAnalysis != nil {
			setParts = append(setParts, fmt.Sprintf("covariate_analysis = $%d::jsonb", len(args)+1))
			args = append(args, covAnalysisJSON)
		}
		if strings.TrimSpace(req.CovariateSignature) != "" {
			setParts = append(setParts, fmt.Sprintf("covariate_signature = $%d", len(args)+1))
			args = append(args, req.CovariateSignature)
		}
		if len(setParts) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
			return
		}

		// 拼接最终 SQL
		updateSQL := fmt.Sprintf("UPDATE mtf_best_validation_chunks SET %s, updated_at = CURRENT_TIMESTAMP WHERE unique_key = $%d AND chunk_index = $%d",
			strings.Join(setParts, ", "), len(args)+1, len(args)+2,
		)
		// slog.Info("updateSQL", updateSQL)
		args = append(args, req.UniqueKey, req.ChunkIndex)
		if err := h.db.Exec(updateSQL, args...).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update mtf_best_validation_chunks: %v", err)})
			return
		}
		c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success"})
		return
	}

	// 不存在：执行插入，要求提供所有 NOT NULL 字段（actual_values 允许为空，默认插入 [] 或从数据库计算）
	if strings.TrimSpace(req.StartDate) == "" || strings.TrimSpace(req.EndDate) == "" || req.Predictions == nil || len(req.Predictions) == 0 || req.Dates == nil || len(req.Dates) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required fields for insert (start_date, end_date, predictions, dates)"})
		return
	}
	predsJSON, err := json.Marshal(req.Predictions)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "predictions must be JSON object"})
		return
	}
	// actual_values 允许为空：当未提供或为 nil 时，严格按 HorizonLen 从 stock_data 补齐
	var actualJSON string
	if providerActual != nil {
		b, _ := json.Marshal(providerActual)
		actualJSON = string(b)
	} else if req.Actual != nil {
		b, _ := json.Marshal(req.Actual)
		actualJSON = string(b)
	} else {
		// 构造日期区间与收盘价序列
		sd, err := time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date format (YYYY-MM-DD)"})
			return
		}
		ed, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date format (YYYY-MM-DD)"})
			return
		}
		// 改为仅基于 dates 匹配，不依赖 horizon_len
		if req.Dates == nil || len(req.Dates) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dates are required when actual_values is empty"})
			return
		}

		// 统一使用 stock_data 表查询（A股与ETF均走此路径）
		rows, e := h.GetStockDataByDateRange(req.Symbol, req.StockType, sd, ed)
		if e != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to query stock data by date range: %v", e)})
			return
		}
		closeByDate := make(map[string]float64, len(rows))
		for _, r := range rows {
			d := r.Datetime.Format("2006-01-02")
			closeByDate[d] = r.Close
		}
		actualVals := make([]float64, 0, len(req.Dates))
		for _, d := range req.Dates {
			v, ok := closeByDate[d]
			if !ok {
				slog.Error("missing close value for date", "date", d)
			}
			actualVals = append(actualVals, v)
		}
		b, _ := json.Marshal(actualVals)
		actualJSON = string(b)
	}
	predictedChangeJSON := "{}"
	if req.PredictedChangePct != nil {
		b, err := json.Marshal(req.PredictedChangePct)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "predicted_change_percent must be JSON object"})
			return
		}
		predictedChangeJSON = string(b)
	}
	actualChangeJSON := "[]"
	if req.ActualChangePct != nil {
		b, _ := json.Marshal(req.ActualChangePct)
		actualChangeJSON = string(b)
	}
	datesJSON, _ := json.Marshal(req.Dates)
	var uidArg interface{}
	if req.UserID != nil {
		uidArg = *req.UserID
	} else {
		uidArg = nil
	}
	var changeBaseValueArg interface{}
	if req.ChangeBaseValue != nil {
		changeBaseValueArg = *req.ChangeBaseValue
	}
	var changeBaseDateArg interface{}
	if strings.TrimSpace(req.ChangeBaseDate) != "" {
		changeBaseDateArg = req.ChangeBaseDate
	}
	var adjustRawChunksArg interface{}
	if req.StockType == 1 && len(req.AdjustRawChunks) > 0 && string(req.AdjustRawChunks) != "null" {
		adjustRawChunksArg = string(req.AdjustRawChunks)
	}
	if err := h.db.Exec(`
        INSERT INTO mtf_best_validation_chunks (
            unique_key, chunk_index, user_id, symbol, start_date, end_date,
            predictions, actual_values, predicted_change_percent, actual_change_percent, change_base_value, change_base_date, dates,
            prediction_type, covariate_config, covariate_signature, covariate_analysis, stock_name, stock_type, adjust_raw_chunks
        ) VALUES (
            $1, $2, $3, $4, $5::date, $6::date,
            $7::jsonb, $8::jsonb, $9::jsonb, $10::jsonb, $11, $12::date, $13::jsonb,
            $14, $15::jsonb, $16, $17::jsonb, $18, $19, $20::jsonb
        )`,
		req.UniqueKey, req.ChunkIndex, uidArg, req.Symbol, req.StartDate, req.EndDate,
		string(predsJSON), actualJSON, predictedChangeJSON, actualChangeJSON, changeBaseValueArg, changeBaseDateArg, string(datesJSON),
		predictionType, covConfigJSON, covSignature, covAnalysisJSON, req.StockName, req.StockType, adjustRawChunksArg,
	).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to insert mtf_best_validation_chunks: %v", err)})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success"})
}

func (h *DatabaseHandler) getMTFBestByUniqueKeyHandler(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if strings.TrimSpace(uniqueKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}
	row := h.db.Raw(`
        SELECT id, unique_key, symbol, mtf_version, best_prediction_item, best_metrics::text,
               prediction_type, COALESCE(covariate_config, '{}'::jsonb)::text,
               COALESCE(covariate_signature, ''),
               COALESCE(covariate_analysis, '{}'::jsonb)::text,
               is_public,
               train_start_date, train_end_date,
               test_start_date, test_end_date,
               val_start_date, val_end_date,
               context_len, horizon_len,
               created_at, updated_at
        FROM mtf_best_predictions
        WHERE unique_key = $1
        LIMIT 1`, uniqueKey).Row()
	var item struct {
		ID                 int       `json:"id"`
		UniqueKey          string    `json:"unique_key"`
		Symbol             string    `json:"symbol"`
		MTFVersion         string    `json:"mtf_version"`
		BestPredictionItem string    `json:"best_prediction_item"`
		BestMetrics        string    `json:"best_metrics"`
		PredictionType     string    `json:"prediction_type"`
		CovariateConfig    string    `json:"-"`
		CovariateSignature string    `json:"covariate_signature"`
		CovariateAnalysis  string    `json:"covariate_analysis"`
		IsPublic           int       `json:"is_public"`
		TrainStartDate     time.Time `json:"train_start_date"`
		TrainEndDate       time.Time `json:"train_end_date"`
		TestStartDate      time.Time `json:"test_start_date"`
		TestEndDate        time.Time `json:"test_end_date"`
		ValStartDate       time.Time `json:"val_start_date"`
		ValEndDate         time.Time `json:"val_end_date"`
		ContextLen         int       `json:"context_len"`
		HorizonLen         int       `json:"horizon_len"`
		CreatedAt          time.Time `json:"created_at"`
		UpdatedAt          time.Time `json:"updated_at"`
	}
	if err := row.Scan(
		&item.ID, &item.UniqueKey, &item.Symbol, &item.MTFVersion, &item.BestPredictionItem, &item.BestMetrics,
		&item.PredictionType,
		&item.CovariateConfig, &item.CovariateSignature, &item.CovariateAnalysis,
		&item.IsPublic,
		&item.TrainStartDate, &item.TrainEndDate,
		&item.TestStartDate, &item.TestEndDate,
		&item.ValStartDate, &item.ValEndDate,
		&item.ContextLen, &item.HorizonLen,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: item})
}

func (h *DatabaseHandler) getMTFBestKeysByConfigHandler(c *gin.Context) {
	symbol := strings.TrimSpace(c.Query("symbol"))
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required"})
		return
	}

	horizonLen, err := strconv.Atoi(strings.TrimSpace(c.Query("horizon_len")))
	if err != nil || horizonLen <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid horizon_len is required"})
		return
	}

	contextLen, err := strconv.Atoi(strings.TrimSpace(c.Query("context_len")))
	if err != nil || contextLen <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid context_len is required"})
		return
	}

	mtfVersion := strings.TrimSpace(c.Query("mtf_version"))

	rows, err := h.db.Raw(`
        SELECT prediction_type, unique_key
        FROM (
            SELECT DISTINCT ON (prediction_type)
                prediction_type,
                unique_key,
                updated_at,
                id
            FROM mtf_best_predictions
            WHERE symbol = $1
              AND horizon_len = $2
              AND context_len = $3
              AND ($4 = '' OR mtf_version = $4)
            ORDER BY prediction_type, updated_at DESC, id DESC
        ) latest
    `, symbol, horizonLen, contextLen, mtfVersion).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	response := gin.H{
		"symbol":              symbol,
		"mtf_version":         mtfVersion,
		"horizon_len":         horizonLen,
		"context_len":         contextLen,
		"mtf_lite_unique_key": "",
		"mtf_pro_unique_key":  "",
	}

	found := 0
	for rows.Next() {
		var predictionType string
		var uniqueKey string
		if err := rows.Scan(&predictionType, &uniqueKey); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		switch normalizedPredictionType(predictionType) {
		case "mtf-pro":
			response["mtf_pro_unique_key"] = uniqueKey
			found++
		default:
			response["mtf_lite_unique_key"] = uniqueKey
			found++
		}
	}

	if found == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: response})
}

// 获取指定 unique_key 的最新验证分块（按 chunk_index DESC 取一条）
func (h *DatabaseHandler) getLatestMTFValChunkHandler(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if strings.TrimSpace(uniqueKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	row := h.db.Raw(`
        SELECT 
            unique_key,
            chunk_index,
            start_date::text,
            end_date::text,
            symbol,
            predictions,
            COALESCE(actual_values, '[]'::jsonb) AS actual_values,
            COALESCE(predicted_change_percent, '{}'::jsonb) AS predicted_change_percent,
            COALESCE(actual_change_percent, '[]'::jsonb) AS actual_change_percent,
            change_base_value,
            change_base_date::text,
            COALESCE(dates, '[]'::jsonb) AS dates,
            COALESCE(prediction_type, 'mtf-lite') AS prediction_type,
            COALESCE(covariate_config, '{}'::jsonb) AS covariate_config,
            COALESCE(covariate_signature, '') AS covariate_signature,
            COALESCE(covariate_analysis, '{}'::jsonb) AS covariate_analysis,
            COALESCE(stock_name, '') AS stock_name,
            COALESCE(stock_type, 1) AS stock_type,
            COALESCE(adjust_raw_chunks, 'null'::jsonb) AS adjust_raw_chunks
        FROM mtf_best_validation_chunks
        WHERE unique_key = $1
        ORDER BY chunk_index DESC
        LIMIT 1`, uniqueKey).Row()

	var (
		uk               string
		chunkIndex       int
		startDate        string
		endDate          string
		symbol           string
		predsJSON        []byte
		actualJSON       []byte
		predChangeJSON   []byte
		actualChangeJSON []byte
		changeBaseValue  sql.NullFloat64
		changeBaseDate   sql.NullString
		datesJSON        []byte
		predictionType   string
		covConfigJSON    []byte
		covSignature     string
		covAnalysisJSON  []byte
		stockName        string
		stockType        int
		adjustRawJSON    []byte
	)

	if err := row.Scan(&uk, &chunkIndex, &startDate, &endDate, &symbol, &predsJSON, &actualJSON, &predChangeJSON, &actualChangeJSON, &changeBaseValue, &changeBaseDate, &datesJSON, &predictionType, &covConfigJSON, &covSignature, &covAnalysisJSON, &stockName, &stockType, &adjustRawJSON); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var preds map[string]interface{}
	var actual []float64
	var predChange map[string]interface{}
	var actualChange []float64
	var dates []string
	var covConfig map[string]interface{}
	var covAnalysis map[string]interface{}
	var adjustRawChunks interface{}
	if err := json.Unmarshal(predsJSON, &preds); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal predictions"})
		return
	}
	if err := json.Unmarshal(actualJSON, &actual); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal actual_values"})
		return
	}
	if err := json.Unmarshal(predChangeJSON, &predChange); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal predicted_change_percent"})
		return
	}
	if err := json.Unmarshal(actualChangeJSON, &actualChange); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal actual_change_percent"})
		return
	}
	if err := json.Unmarshal(datesJSON, &dates); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal dates"})
		return
	}
	if err := json.Unmarshal(covConfigJSON, &covConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal covariate_config"})
		return
	}
	if err := json.Unmarshal(covAnalysisJSON, &covAnalysis); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal covariate_analysis"})
		return
	}
	if err := json.Unmarshal(adjustRawJSON, &adjustRawChunks); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal adjust_raw_chunks"})
		return
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{
		"unique_key":               uk,
		"chunk_index":              chunkIndex,
		"start_date":               startDate,
		"end_date":                 endDate,
		"symbol":                   symbol,
		"predictions":              preds,
		"actual_values":            actual,
		"predicted_change_percent": predChange,
		"actual_change_percent":    actualChange,
		"change_base_value": func() interface{} {
			if changeBaseValue.Valid {
				return changeBaseValue.Float64
			}
			return nil
		}(),
		"change_base_date": func() interface{} {
			if changeBaseDate.Valid {
				return changeBaseDate.String
			}
			return nil
		}(),
		"dates":               dates,
		"prediction_type":     predictionType,
		"covariate_signature": covSignature,
		"covariate_analysis":  covAnalysis,
		"stock_name":          stockName,
		"stock_type":          stockType,
		"adjust_raw_chunks":   adjustRawChunks,
	}})
}

// 获取指定 unique_key 的所有验证分块（按 chunk_index ASC 排序）
func (h *DatabaseHandler) getMTFValChunkListHandler(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	if strings.TrimSpace(uniqueKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}

	rows, err := h.db.Raw(`
        SELECT 
            unique_key,
            chunk_index,
            start_date::text,
            end_date::text,
            symbol,
            predictions,
            COALESCE(actual_values, '[]'::jsonb) AS actual_values,
            COALESCE(predicted_change_percent, '{}'::jsonb) AS predicted_change_percent,
            COALESCE(actual_change_percent, '[]'::jsonb) AS actual_change_percent,
            change_base_value,
            change_base_date::text,
            COALESCE(dates, '[]'::jsonb) AS dates,
            COALESCE(prediction_type, 'mtf-lite') AS prediction_type,
            COALESCE(covariate_config, '{}'::jsonb) AS covariate_config,
            COALESCE(covariate_signature, '') AS covariate_signature,
            COALESCE(covariate_analysis, '{}'::jsonb) AS covariate_analysis,
            COALESCE(stock_name, '') AS stock_name,
            COALESCE(stock_type, 1) AS stock_type,
            COALESCE(adjust_raw_chunks, 'null'::jsonb) AS adjust_raw_chunks
        FROM mtf_best_validation_chunks
        WHERE unique_key = $1
        ORDER BY chunk_index ASC`, uniqueKey).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Item struct {
		UniqueKey          string                 `json:"unique_key"`
		ChunkIndex         int                    `json:"chunk_index"`
		StartDate          string                 `json:"start_date"`
		EndDate            string                 `json:"end_date"`
		Symbol             string                 `json:"symbol"`
		Predictions        map[string]interface{} `json:"predictions"`
		Actual             []float64              `json:"actual_values"`
		PredictedChangePct map[string]interface{} `json:"predicted_change_percent"`
		ActualChangePct    []float64              `json:"actual_change_percent"`
		ChangeBaseValue    *float64               `json:"change_base_value"`
		ChangeBaseDate     *string                `json:"change_base_date"`
		Dates              []string               `json:"dates"`
		PredictionType     string                 `json:"prediction_type"`
		CovariateSignature string                 `json:"covariate_signature"`
		CovariateAnalysis  map[string]interface{} `json:"covariate_analysis"`
		StockName          string                 `json:"stock_name"`
		StockType          int                    `json:"stock_type"`
		AdjustRawChunks    interface{}            `json:"adjust_raw_chunks"`
	}
	list := make([]Item, 0, 64)
	for rows.Next() {
		var (
			uk               string
			chunkIndex       int
			startDate        string
			endDate          string
			symbol           string
			predsJSON        []byte
			actualJSON       []byte
			predChangeJSON   []byte
			actualChangeJSON []byte
			changeBaseValue  sql.NullFloat64
			changeBaseDate   sql.NullString
			datesJSON        []byte
			predictionType   string
			covConfigJSON    []byte
			covSignature     string
			covAnalysisJSON  []byte
			stockName        string
			stockType        int
			adjustRawJSON    []byte
		)
		if err := rows.Scan(&uk, &chunkIndex, &startDate, &endDate, &symbol, &predsJSON, &actualJSON, &predChangeJSON, &actualChangeJSON, &changeBaseValue, &changeBaseDate, &datesJSON, &predictionType, &covConfigJSON, &covSignature, &covAnalysisJSON, &stockName, &stockType, &adjustRawJSON); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var preds map[string]interface{}
		var actual []float64
		var predChange map[string]interface{}
		var actualChange []float64
		var dates []string
		var covConfig map[string]interface{}
		var covAnalysis map[string]interface{}
		var adjustRawChunks interface{}
		if err := json.Unmarshal(predsJSON, &preds); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal predictions"})
			return
		}
		if err := json.Unmarshal(actualJSON, &actual); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal actual_values"})
			return
		}
		if err := json.Unmarshal(predChangeJSON, &predChange); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal predicted_change_percent"})
			return
		}
		if err := json.Unmarshal(actualChangeJSON, &actualChange); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal actual_change_percent"})
			return
		}
		if err := json.Unmarshal(datesJSON, &dates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal dates"})
			return
		}
		if err := json.Unmarshal(covConfigJSON, &covConfig); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal covariate_config"})
			return
		}
		if err := json.Unmarshal(covAnalysisJSON, &covAnalysis); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal covariate_analysis"})
			return
		}
		if err := json.Unmarshal(adjustRawJSON, &adjustRawChunks); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal adjust_raw_chunks"})
			return
		}
		item := Item{
			UniqueKey:          uk,
			ChunkIndex:         chunkIndex,
			StartDate:          startDate,
			EndDate:            endDate,
			Symbol:             symbol,
			Predictions:        preds,
			Actual:             actual,
			PredictedChangePct: predChange,
			ActualChangePct:    actualChange,
			Dates:              dates,
			PredictionType:     predictionType,
			CovariateSignature: covSignature,
			CovariateAnalysis:  covAnalysis,
			StockName:          stockName,
			StockType:          stockType,
			AdjustRawChunks:    adjustRawChunks,
		}
		if changeBaseValue.Valid {
			value := changeBaseValue.Float64
			item.ChangeBaseValue = &value
		}
		if changeBaseDate.Valid {
			value := changeBaseDate.String
			item.ChangeBaseDate = &value
		}
		list = append(list, item)
	}
	if len(list) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: list})
}

func (h *DatabaseHandler) saveMTFDirectHandler(c *gin.Context) {
	var req struct {
		UniqueKey            string                 `json:"unique_key"`
		Symbol               string                 `json:"symbol"`
		StockType            int                    `json:"stock_type"`
		MTFVersion           string                 `json:"mtf_version"`
		ContextLen           int                    `json:"context_len"`
		HorizonLen           int                    `json:"horizon_len"`
		FutureDates          []string               `json:"future_dates"`
		RequestEndDate       string                 `json:"request_end_date"`
		LatestDataDate       string                 `json:"latest_data_date"`
		LatestClose          *float64               `json:"latest_close"`
		HistoryRows          *int                   `json:"history_rows"`
		BestPredictionItem   string                 `json:"best_prediction_item"`
		BestPredictionValues []float64              `json:"best_prediction_values"`
		Predictions          map[string]interface{} `json:"predictions"`
		PredictedChangePct   map[string]interface{} `json:"predicted_change_percent"`
		ChangeBaseValue      *float64               `json:"change_base_value"`
		ChangeBaseDate       string                 `json:"change_base_date"`
		PredictionChangeBase map[string]interface{} `json:"prediction_change_base"`
		CovariateConfig      map[string]interface{} `json:"covariate_config"`
		CovariateSignature   string                 `json:"covariate_signature"`
		CovariateAnalysis    map[string]interface{} `json:"covariate_analysis"`
		ShortName            string                 `json:"short_name"`
		UserID               *int                   `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	req.Symbol = strings.TrimSpace(req.Symbol)
	req.MTFVersion = strings.TrimSpace(req.MTFVersion)
	futureDatesKey := buildFutureDatesKey(req.FutureDates)
	if req.Symbol == "" || req.MTFVersion == "" || req.ContextLen <= 0 || req.HorizonLen <= 0 || futureDatesKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol, mtf_version, context_len, horizon_len and future_dates are required"})
		return
	}
	if req.StockType <= 0 {
		req.StockType = 1
	}
	if req.UniqueKey == "" {
		req.UniqueKey = buildMTFDirectUniqueKey(req.Symbol, req.StockType, req.HorizonLen, req.ContextLen, futureDatesKey)
	}
	if req.ShortName == "" {
		if req.StockType == 2 {
			etfData, errEtf := h.GetEtfDaily(req.Symbol, 1, 0)
			if errEtf == nil && len(etfData) > 0 {
				req.ShortName = etfData[0].Name
			}
		}
		if req.StockType == 1 {
			code := strings.TrimLeft(req.Symbol, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
			stockData, errStock := h.GetAStockCommentDailyByCode(code, 1, 0)
			if errStock == nil && len(stockData) > 0 {
				req.ShortName = stockData[0].Name
			}
		}
	}

	futureDatesJSON, _ := json.Marshal(req.FutureDates)
	bestPredictionValuesJSON, _ := json.Marshal(req.BestPredictionValues)
	predictionsJSON, err := json.Marshal(req.Predictions)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "predictions must be JSON object"})
		return
	}
	predictedChangeJSON, err := marshalOptionalJSONObject(req.PredictedChangePct)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "predicted_change_percent must be JSON object"})
		return
	}
	predictionChangeBaseJSON, err := marshalOptionalJSONObject(req.PredictionChangeBase)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prediction_change_base must be JSON object"})
		return
	}
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_config must be JSON object"})
		return
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_analysis must be JSON object"})
		return
	}
	covSignature := strings.TrimSpace(req.CovariateSignature)

	var latestCloseArg interface{}
	if req.LatestClose != nil {
		latestCloseArg = *req.LatestClose
	} else {
		latestCloseArg = nil
	}
	var historyRowsArg interface{}
	if req.HistoryRows != nil {
		historyRowsArg = *req.HistoryRows
	} else {
		historyRowsArg = nil
	}
	var changeBaseValueArg interface{}
	if req.ChangeBaseValue != nil {
		changeBaseValueArg = *req.ChangeBaseValue
	} else {
		changeBaseValueArg = nil
	}
	var changeBaseDateArg interface{}
	if strings.TrimSpace(req.ChangeBaseDate) != "" {
		changeBaseDateArg = req.ChangeBaseDate
	} else {
		changeBaseDateArg = nil
	}
	var userIDArg interface{}
	if req.UserID != nil {
		userIDArg = *req.UserID
	} else {
		userIDArg = nil
	}

	err = h.db.Exec(`
	        INSERT INTO mtf_direct_predictions (
	            unique_key, symbol, stock_type, mtf_version, context_len, horizon_len,
	            future_dates_key, future_dates, request_end_date, latest_data_date,
	            latest_close, history_rows, best_prediction_item, best_prediction_values,
	            predictions, predicted_change_percent, change_base_value, change_base_date, prediction_change_base,
	            covariate_config, covariate_signature, covariate_analysis, short_name, user_id
	        ) VALUES (
	            $1, $2, $3, $4, $5, $6,
	            $7, $8::jsonb, $9::date, $10::date,
	            $11, $12, $13, $14::jsonb,
	            $15::jsonb, $16::jsonb, $17, $18::date, $19::jsonb,
	            $20::jsonb, $21, $22::jsonb, $23, $24
	        )
        ON CONFLICT (symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature) DO UPDATE SET
            unique_key = EXCLUDED.unique_key,
            mtf_version = EXCLUDED.mtf_version,
            request_end_date = EXCLUDED.request_end_date,
            latest_data_date = EXCLUDED.latest_data_date,
            latest_close = EXCLUDED.latest_close,
            history_rows = EXCLUDED.history_rows,
            best_prediction_item = EXCLUDED.best_prediction_item,
	            best_prediction_values = EXCLUDED.best_prediction_values,
	            predictions = EXCLUDED.predictions,
	            predicted_change_percent = EXCLUDED.predicted_change_percent,
	            change_base_value = EXCLUDED.change_base_value,
	            change_base_date = EXCLUDED.change_base_date,
	            prediction_change_base = EXCLUDED.prediction_change_base,
	            covariate_config = EXCLUDED.covariate_config,
            covariate_signature = EXCLUDED.covariate_signature,
            covariate_analysis = EXCLUDED.covariate_analysis,
            short_name = EXCLUDED.short_name,
            user_id = EXCLUDED.user_id,
            updated_at = CURRENT_TIMESTAMP`,
		req.UniqueKey, req.Symbol, req.StockType, req.MTFVersion, req.ContextLen, req.HorizonLen,
		futureDatesKey, string(futureDatesJSON), req.RequestEndDate, req.LatestDataDate,
		latestCloseArg, historyRowsArg, req.BestPredictionItem, string(bestPredictionValuesJSON),
		string(predictionsJSON), predictedChangeJSON, changeBaseValueArg, changeBaseDateArg, predictionChangeBaseJSON,
		covConfigJSON, covSignature, covAnalysisJSON, req.ShortName, userIDArg,
	).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to upsert mtf_direct_predictions: %v", err)})
		return
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{
		"unique_key":       req.UniqueKey,
		"future_dates_key": futureDatesKey,
	}})
}

func (h *DatabaseHandler) getMTFDirectByRequestHandler(c *gin.Context) {
	symbol := strings.TrimSpace(c.Query("symbol"))
	stockType, err := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("stock_type", "1")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "stock_type must be an integer"})
		return
	}
	horizonLen, err := strconv.Atoi(strings.TrimSpace(c.Query("horizon_len")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "horizon_len must be an integer"})
		return
	}
	contextLen, err := strconv.Atoi(strings.TrimSpace(c.Query("context_len")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "context_len must be an integer"})
		return
	}
	futureDatesCSV := strings.TrimSpace(c.Query("future_dates"))
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol is required"})
		return
	}
	covariateSignature := strings.TrimSpace(c.DefaultQuery("covariate_signature", ""))
	predictionType := normalizedPredictionType(c.Query("prediction_type"))
	mtfVersion := strings.TrimSpace(c.Query("mtf_version"))

	selectSQL := `
		SELECT
			unique_key,
			symbol,
			stock_type,
			mtf_version,
			context_len,
			horizon_len,
			future_dates,
			request_end_date::text,
			latest_data_date::text,
			latest_close,
			history_rows,
			COALESCE(best_prediction_item, '') AS best_prediction_item,
				best_prediction_values,
				predictions,
				COALESCE(predicted_change_percent, '{}'::jsonb) AS predicted_change_percent,
				change_base_value,
				change_base_date::text,
				COALESCE(prediction_change_base, '{}'::jsonb) AS prediction_change_base,
				COALESCE(covariate_config, '{}'::jsonb) AS covariate_config,
			COALESCE(covariate_signature, '') AS covariate_signature,
			COALESCE(covariate_analysis, '{}'::jsonb) AS covariate_analysis,
			COALESCE(short_name, '') AS short_name,
			user_id,
			created_at,
			updated_at
		FROM mtf_direct_predictions
		WHERE symbol = $1 AND stock_type = $2 AND horizon_len = $3 AND context_len = $4`
	args := []interface{}{symbol, stockType, horizonLen, contextLen}
	nextArg := 5
	if mtfVersion != "" {
		selectSQL += fmt.Sprintf(` AND mtf_version = $%d`, nextArg)
		args = append(args, mtfVersion)
		nextArg++
	}
	if futureDatesCSV != "" {
		futureDatesKey := buildFutureDatesKey(strings.Split(futureDatesCSV, ","))
		selectSQL += fmt.Sprintf(` AND future_dates_key = $%d`, nextArg)
		args = append(args, futureDatesKey)
		nextArg++
	}
	if covariateSignature != "" {
		selectSQL += fmt.Sprintf(` AND COALESCE(covariate_signature, '') = $%d`, nextArg)
		args = append(args, covariateSignature)
		nextArg++
	} else if predictionTypeUsesCovariates(predictionType) {
		selectSQL += ` AND COALESCE(covariate_signature, '') <> ''`
	} else {
		selectSQL += fmt.Sprintf(` AND COALESCE(covariate_signature, '') = $%d`, nextArg)
		args = append(args, covariateSignature)
		nextArg++
	}
	if futureDatesCSV == "" {
		selectSQL += ` ORDER BY latest_data_date DESC, updated_at DESC, id DESC LIMIT 1`
	} else {
		selectSQL += ` LIMIT 1`
	}

	row := h.db.Raw(selectSQL, args...).Row()

	var (
		item struct {
			UniqueKey          string
			Symbol             string
			StockType          int
			MTFVersion         string
			ContextLen         int
			HorizonLen         int
			RequestEndDate     string
			LatestDataDate     string
			BestPredictionItem string
			ShortName          string
			CreatedAt          time.Time
			UpdatedAt          time.Time
		}
		futureDatesJSON          []byte
		bestPredictionValuesJSON []byte
		predictionsJSON          []byte
		predictedChangeJSON      []byte
		predictionChangeBaseJSON []byte
		covConfigJSON            []byte
		covSignature             string
		covAnalysisJSON          []byte
		latestClose              sql.NullFloat64
		changeBaseValue          sql.NullFloat64
		changeBaseDate           sql.NullString
		historyRows              sql.NullInt64
		userID                   sql.NullInt64
	)

	if err := row.Scan(
		&item.UniqueKey,
		&item.Symbol,
		&item.StockType,
		&item.MTFVersion,
		&item.ContextLen,
		&item.HorizonLen,
		&futureDatesJSON,
		&item.RequestEndDate,
		&item.LatestDataDate,
		&latestClose,
		&historyRows,
		&item.BestPredictionItem,
		&bestPredictionValuesJSON,
		&predictionsJSON,
		&predictedChangeJSON,
		&changeBaseValue,
		&changeBaseDate,
		&predictionChangeBaseJSON,
		&covConfigJSON,
		&covSignature,
		&covAnalysisJSON,
		&item.ShortName,
		&userID,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var futureDates []string
	if len(futureDatesJSON) > 0 {
		if err := json.Unmarshal(futureDatesJSON, &futureDates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal future_dates"})
			return
		}
	}
	var bestPredictionValues []float64
	if len(bestPredictionValuesJSON) > 0 && string(bestPredictionValuesJSON) != "null" {
		if err := json.Unmarshal(bestPredictionValuesJSON, &bestPredictionValues); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal best_prediction_values"})
			return
		}
	}
	var predictions map[string]interface{}
	var predictedChange map[string]interface{}
	var predictionChangeBase map[string]interface{}
	var covConfig map[string]interface{}
	var covAnalysis map[string]interface{}
	if err := json.Unmarshal(predictionsJSON, &predictions); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal predictions"})
		return
	}
	if err := json.Unmarshal(predictedChangeJSON, &predictedChange); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal predicted_change_percent"})
		return
	}
	if err := json.Unmarshal(predictionChangeBaseJSON, &predictionChangeBase); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal prediction_change_base"})
		return
	}
	if err := json.Unmarshal(covConfigJSON, &covConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal covariate_config"})
		return
	}
	if err := json.Unmarshal(covAnalysisJSON, &covAnalysis); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unmarshal covariate_analysis"})
		return
	}

	response := gin.H{
		"unique_key":               item.UniqueKey,
		"stock_code":               item.Symbol,
		"stock_type":               item.StockType,
		"mtf_version":              item.MTFVersion,
		"context_len":              item.ContextLen,
		"horizon_len":              item.HorizonLen,
		"future_dates":             futureDates,
		"request_end_date":         item.RequestEndDate,
		"latest_data_date":         item.LatestDataDate,
		"best_prediction_item":     item.BestPredictionItem,
		"best_prediction_values":   bestPredictionValues,
		"predictions":              predictions,
		"predicted_change_percent": predictedChange,
		"prediction_change_base":   predictionChangeBase,
		"covariate_signature":      covSignature,
		"covariate_analysis":       covAnalysis,
		"short_name":               item.ShortName,
		"created_at":               item.CreatedAt,
		"updated_at":               item.UpdatedAt,
	}
	if latestClose.Valid {
		response["latest_close"] = latestClose.Float64
	}
	if historyRows.Valid {
		response["history_rows"] = historyRows.Int64
	}
	if changeBaseValue.Valid {
		response["change_base_value"] = changeBaseValue.Float64
	}
	if changeBaseDate.Valid {
		response["change_base_date"] = changeBaseDate.String
	}
	if userID.Valid {
		response["user_id"] = userID.Int64
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: response})
}

func (h *DatabaseHandler) saveMTFBacktestHandler(c *gin.Context) {
	var req struct {
		UniqueKey                              string                   `json:"unique_key"`
		Symbol                                 string                   `json:"symbol"`
		MTFVersion                             string                   `json:"mtf_version"`
		ContextLen                             int                      `json:"context_len"`
		HorizonLen                             int                      `json:"horizon_len"`
		UserID                                 *int                     `json:"user_id"`
		StrategyParamsID                       *int                     `json:"strategy_params_id"`
		CovariateConfig                        map[string]interface{}   `json:"covariate_config"`
		CovariateSignature                     string                   `json:"covariate_signature"`
		CovariateAnalysis                      map[string]interface{}   `json:"covariate_analysis"`
		UsedQuantile                           string                   `json:"used_quantile"`
		BuyThresholdPct                        float64                  `json:"buy_threshold_pct"`
		SellThresholdPct                       float64                  `json:"sell_threshold_pct"`
		TradeFeeRate                           float64                  `json:"trade_fee_rate"`
		TotalFeesPaid                          float64                  `json:"total_fees_paid"`
		ActualTotalReturnPct                   float64                  `json:"actual_total_return_pct"`
		BenchmarkReturnPct                     float64                  `json:"benchmark_return_pct"`
		BenchmarkAnnualizedReturnPct           float64                  `json:"benchmark_annualized_return_pct"`
		PeriodDays                             int                      `json:"period_days"`
		ValidationStartDate                    string                   `json:"validation_start_date"`
		ValidationEndDate                      string                   `json:"validation_end_date"`
		ValidationBenchmarkReturnPct           float64                  `json:"validation_benchmark_return_pct"`
		ValidationBenchmarkAnnualizedReturnPct float64                  `json:"validation_benchmark_annualized_return_pct"`
		ValidationPeriodDays                   int                      `json:"validation_period_days"`
		PositionControl                        map[string]interface{}   `json:"position_control"`
		PredictedChangeStats                   map[string]interface{}   `json:"predicted_change_stats"`
		PerChunkSignals                        map[string]interface{}   `json:"per_chunk_signals"`
		EquityCurveValues                      []float64                `json:"equity_curve_values"`
		EquityCurvePct                         []float64                `json:"equity_curve_pct"`
		EquityCurvePctGross                    []float64                `json:"equity_curve_pct_gross"`
		CurveDates                             []string                 `json:"curve_dates"`
		ActualEndPrices                        []float64                `json:"actual_end_prices"`
		Trades                                 []map[string]interface{} `json:"trades"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.UniqueKey) == "" || strings.TrimSpace(req.Symbol) == "" || strings.TrimSpace(req.MTFVersion) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key, symbol, mtf_version are required"})
		return
	}
	posJSON, _ := json.Marshal(req.PositionControl)
	statsJSON, _ := json.Marshal(req.PredictedChangeStats)
	signalsJSON, _ := json.Marshal(req.PerChunkSignals)
	eqValsJSON, _ := json.Marshal(req.EquityCurveValues)
	eqPctJSON, _ := json.Marshal(req.EquityCurvePct)
	eqPctGrossJSON, _ := json.Marshal(req.EquityCurvePctGross)
	curveDatesJSON, _ := json.Marshal(req.CurveDates)
	actualEndJSON, _ := json.Marshal(req.ActualEndPrices)
	tradesJSON, _ := json.Marshal(req.Trades)
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_config must be JSON object"})
		return
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "covariate_analysis must be JSON object"})
		return
	}
	var covSignatureArg interface{}
	if strings.TrimSpace(req.CovariateSignature) != "" {
		covSignatureArg = req.CovariateSignature
	}
	var uidArg interface{}
	if req.UserID != nil {
		uidArg = *req.UserID
	} else {
		uidArg = nil
	}
	var spIDArg interface{}
	if req.StrategyParamsID != nil {
		spIDArg = *req.StrategyParamsID
	} else {
		var spID int
		if req.UserID != nil {
			row := h.db.Raw(`SELECT id FROM mtf_strategy_params WHERE unique_key = $1 AND user_id = $2 LIMIT 1`, req.UniqueKey, *req.UserID).Row()
			if err := row.Scan(&spID); err == nil {
				spIDArg = spID
			} else {
				spIDArg = nil
			}
		} else {
			row := h.db.Raw(`SELECT id FROM mtf_strategy_params WHERE unique_key = $1 LIMIT 1`, req.UniqueKey).Row()
			if err := row.Scan(&spID); err == nil {
				spIDArg = spID
			} else {
				spIDArg = nil
			}
		}
	}
	err = h.db.Exec(`
        INSERT INTO mtf_backtests (
            unique_key, user_id, strategy_params_id, symbol, mtf_version, context_len, horizon_len,
            covariate_config, covariate_signature, covariate_analysis,
            used_quantile, buy_threshold_pct, sell_threshold_pct, trade_fee_rate, total_fees_paid, actual_total_return_pct,
            benchmark_return_pct, benchmark_annualized_return_pct, period_days,
            validation_start_date, validation_end_date, validation_benchmark_return_pct, validation_benchmark_annualized_return_pct, validation_period_days,
            position_control, predicted_change_stats, per_chunk_signals,
            equity_curve_values, equity_curve_pct, equity_curve_pct_gross, curve_dates, actual_end_prices, trades
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7,
            $8::jsonb, $9, $10::jsonb,
            $11, $12, $13, $14, $15, $16,
            $17, $18, $19,
            $20::date, $21::date, $22, $23, $24,
            $25::jsonb, $26::jsonb, $27::jsonb,
            $28::jsonb, $29::jsonb, $30::jsonb, $31::jsonb, $32::jsonb, $33::jsonb
        )
        ON CONFLICT (unique_key) DO UPDATE SET
            user_id = EXCLUDED.user_id,
            strategy_params_id = EXCLUDED.strategy_params_id,
            symbol = EXCLUDED.symbol,
            mtf_version = EXCLUDED.mtf_version,
            context_len = EXCLUDED.context_len,
            horizon_len = EXCLUDED.horizon_len,
            covariate_config = EXCLUDED.covariate_config,
            covariate_signature = EXCLUDED.covariate_signature,
            covariate_analysis = EXCLUDED.covariate_analysis,
            used_quantile = EXCLUDED.used_quantile,
            buy_threshold_pct = EXCLUDED.buy_threshold_pct,
            sell_threshold_pct = EXCLUDED.sell_threshold_pct,
            trade_fee_rate = EXCLUDED.trade_fee_rate,
            total_fees_paid = EXCLUDED.total_fees_paid,
            actual_total_return_pct = EXCLUDED.actual_total_return_pct,
            benchmark_return_pct = EXCLUDED.benchmark_return_pct,
            benchmark_annualized_return_pct = EXCLUDED.benchmark_annualized_return_pct,
            period_days = EXCLUDED.period_days,
            validation_start_date = EXCLUDED.validation_start_date,
            validation_end_date = EXCLUDED.validation_end_date,
            validation_benchmark_return_pct = EXCLUDED.validation_benchmark_return_pct,
            validation_benchmark_annualized_return_pct = EXCLUDED.validation_benchmark_annualized_return_pct,
            validation_period_days = EXCLUDED.validation_period_days,
            position_control = EXCLUDED.position_control,
            predicted_change_stats = EXCLUDED.predicted_change_stats,
            per_chunk_signals = EXCLUDED.per_chunk_signals,
            equity_curve_values = EXCLUDED.equity_curve_values,
            equity_curve_pct = EXCLUDED.equity_curve_pct,
            equity_curve_pct_gross = EXCLUDED.equity_curve_pct_gross,
            curve_dates = EXCLUDED.curve_dates,
            actual_end_prices = EXCLUDED.actual_end_prices,
            trades = EXCLUDED.trades,
            updated_at = CURRENT_TIMESTAMP`,
		req.UniqueKey, uidArg, spIDArg, req.Symbol, req.MTFVersion, req.ContextLen, req.HorizonLen,
		covConfigJSON, covSignatureArg, covAnalysisJSON,
		req.UsedQuantile, req.BuyThresholdPct, req.SellThresholdPct, req.TradeFeeRate, req.TotalFeesPaid, req.ActualTotalReturnPct,
		req.BenchmarkReturnPct, req.BenchmarkAnnualizedReturnPct, req.PeriodDays,
		req.ValidationStartDate, req.ValidationEndDate, req.ValidationBenchmarkReturnPct, req.ValidationBenchmarkAnnualizedReturnPct, req.ValidationPeriodDays,
		string(posJSON), string(statsJSON), string(signalsJSON),
		string(eqValsJSON), string(eqPctJSON), string(eqPctGrossJSON), string(curveDatesJSON), string(actualEndJSON), string(tradesJSON),
	).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to upsert mtf_backtests: %v", err)})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"unique_key": req.UniqueKey}})
}

func (h *DatabaseHandler) saveStrategyParamsHandler(c *gin.Context) {
	var req struct {
		UniqueKey              string  `json:"unique_key"`
		UserID                 *int    `json:"user_id"`
		BuyThresholdPct        float64 `json:"buy_threshold_pct"`
		SellThresholdPct       float64 `json:"sell_threshold_pct"`
		InitialCash            float64 `json:"initial_cash"`
		EnableRebalance        bool    `json:"enable_rebalance"`
		MaxPositionPct         float64 `json:"max_position_pct"`
		MinPositionPct         float64 `json:"min_position_pct"`
		SlopePositionPerPct    float64 `json:"slope_position_per_pct"`
		RebalanceTolerancePct  float64 `json:"rebalance_tolerance_pct"`
		TradeFeeRate           float64 `json:"trade_fee_rate"`
		TakeProfitThresholdPct float64 `json:"take_profit_threshold_pct"`
		TakeProfitSellFrac     float64 `json:"take_profit_sell_frac"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.UniqueKey) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key is required"})
		return
	}
	var uidArg interface{}
	if req.UserID != nil {
		uidArg = *req.UserID
	} else {
		uidArg = nil
	}
	err := h.db.Exec(`
        INSERT INTO mtf_strategy_params (
            unique_key, user_id,
            buy_threshold_pct, sell_threshold_pct, initial_cash,
            enable_rebalance, max_position_pct, min_position_pct,
            slope_position_per_pct, rebalance_tolerance_pct,
            trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
        ) VALUES (
            $1, $2,
            $3, $4, $5,
            $6, $7, $8,
            $9, $10,
            $11, $12, $13
        )
        ON CONFLICT (unique_key) DO UPDATE SET
            user_id = EXCLUDED.user_id,
            buy_threshold_pct = EXCLUDED.buy_threshold_pct,
            sell_threshold_pct = EXCLUDED.sell_threshold_pct,
            initial_cash = EXCLUDED.initial_cash,
            enable_rebalance = EXCLUDED.enable_rebalance,
            max_position_pct = EXCLUDED.max_position_pct,
            min_position_pct = EXCLUDED.min_position_pct,
            slope_position_per_pct = EXCLUDED.slope_position_per_pct,
            rebalance_tolerance_pct = EXCLUDED.rebalance_tolerance_pct,
            trade_fee_rate = EXCLUDED.trade_fee_rate,
            take_profit_threshold_pct = EXCLUDED.take_profit_threshold_pct,
            take_profit_sell_frac = EXCLUDED.take_profit_sell_frac,
            updated_at = CURRENT_TIMESTAMP`,
		req.UniqueKey, uidArg,
		req.BuyThresholdPct, req.SellThresholdPct, req.InitialCash,
		req.EnableRebalance, req.MaxPositionPct, req.MinPositionPct,
		req.SlopePositionPerPct, req.RebalanceTolerancePct,
		req.TradeFeeRate, req.TakeProfitThresholdPct, req.TakeProfitSellFrac,
	).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to upsert mtf_strategy_params: %v", err)})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"unique_key": req.UniqueKey}})
}

func (h *DatabaseHandler) getStrategyParamsByUniqueKeyHandler(c *gin.Context) {
	uniqueKey := c.Query("unique_key")
	userId, err := strconv.Atoi(c.Query("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id must be an integer"})
		return
	}
	if strings.TrimSpace(uniqueKey) == "" || userId == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unique_key and user_id are required"})
		return
	}
	row := h.db.Raw(`
        SELECT id, unique_key, user_id,
               buy_threshold_pct, sell_threshold_pct, initial_cash,
               enable_rebalance, max_position_pct, min_position_pct,
               slope_position_per_pct, rebalance_tolerance_pct,
               trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac,
               created_at, updated_at
        FROM mtf_strategy_params
        WHERE unique_key = $1 AND user_id = $2
        LIMIT 1`, uniqueKey, userId).Row()
	var item StrategyParams
	var spID int
	var uid sql.NullInt64
	if err := row.Scan(
		&spID, &item.UniqueKey, &uid,
		&item.BuyThresholdPct, &item.SellThresholdPct, &item.InitialCash,
		&item.EnableRebalance, &item.MaxPositionPct, &item.MinPositionPct,
		&item.SlopePositionPerPct, &item.RebalanceTolerancePct,
		&item.TradeFeeRate, &item.TakeProfitThresholdPct, &item.TakeProfitSellFrac,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if uid.Valid {
		v := int(uid.Int64)
		item.UserID = &v
	} else {
		item.UserID = nil
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{
		"id":                        spID,
		"unique_key":                item.UniqueKey,
		"user_id":                   item.UserID,
		"buy_threshold_pct":         item.BuyThresholdPct,
		"sell_threshold_pct":        item.SellThresholdPct,
		"initial_cash":              item.InitialCash,
		"enable_rebalance":          item.EnableRebalance,
		"max_position_pct":          item.MaxPositionPct,
		"min_position_pct":          item.MinPositionPct,
		"slope_position_per_pct":    item.SlopePositionPerPct,
		"rebalance_tolerance_pct":   item.RebalanceTolerancePct,
		"trade_fee_rate":            item.TradeFeeRate,
		"take_profit_threshold_pct": item.TakeProfitThresholdPct,
		"take_profit_sell_frac":     item.TakeProfitSellFrac,
		"created_at":                item.CreatedAt,
		"updated_at":                item.UpdatedAt,
	}})
}

func (h *DatabaseHandler) getStrategyParamsByUserHandler(c *gin.Context) {
	uidStr := c.Query("user_id")
	if strings.TrimSpace(uidStr) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user_id"})
		return
	}
	rows, err := h.db.Raw(`
        SELECT unique_key, user_id,
               buy_threshold_pct, sell_threshold_pct, initial_cash,
               enable_rebalance, max_position_pct, min_position_pct,
               slope_position_per_pct, rebalance_tolerance_pct,
               trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac,
               created_at, updated_at
        FROM mtf_strategy_params
        WHERE user_id = $1
        ORDER BY updated_at DESC`, uid).Rows()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []StrategyParams{}
	for rows.Next() {
		var item StrategyParams
		var uidN sql.NullInt64
		if err := rows.Scan(
			&item.UniqueKey, &uidN,
			&item.BuyThresholdPct, &item.SellThresholdPct, &item.InitialCash,
			&item.EnableRebalance, &item.MaxPositionPct, &item.MinPositionPct,
			&item.SlopePositionPerPct, &item.RebalanceTolerancePct,
			&item.TradeFeeRate, &item.TakeProfitThresholdPct, &item.TakeProfitSellFrac,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if uidN.Valid {
			v := int(uidN.Int64)
			item.UserID = &v
		} else {
			item.UserID = nil
		}
		list = append(list, item)
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: list})
}

func (h *DatabaseHandler) insertStockDataHandler(c *gin.Context) {
	var data StockData
	if err := c.ShouldBindJSON(&data); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if err := h.InsertStockData(&data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success"})
}

func (h *DatabaseHandler) batchInsertStockDataHandler(c *gin.Context) {
	var dataList []StockData
	if err := c.ShouldBindJSON(&dataList); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if err := h.BatchInsertStockData(dataList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Batch insert successful"})
}

func (h *DatabaseHandler) getStockDataHandler(c *gin.Context) {
	symbol := c.Param("symbol")
	var req struct {
		Type   *int `json:"type"`
		Limit  *int `json:"limit"`
		Offset *int `json:"offset"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	stockType := 1
	if req.Type != nil {
		stockType = *req.Type
	}
	limit := 100
	if req.Limit != nil {
		limit = *req.Limit
	}
	offset := 0
	if req.Offset != nil {
		offset = *req.Offset
	}
	data, err := h.GetStockData(symbol, stockType, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) listStockSymbolsHandler(c *gin.Context) {
	data, err := h.ListStockSymbols()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) getStockDataByDateRangeHandler(c *gin.Context) {
	symbol := c.Param("symbol")
	var req struct {
		Type      *int   `json:"type"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	stockType := 1
	if req.Type != nil {
		stockType = *req.Type
	}
	if req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date parameters are required"})
		return
	}
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format (YYYY-MM-DD)"})
		return
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format (YYYY-MM-DD)"})
		return
	}
	data, err := h.GetStockDataByDateRange(symbol, stockType, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) insertEtfDailyHandler(c *gin.Context) {
	var req struct {
		Code          string  `json:"code"`
		TradingDate   string  `json:"trading_date"`
		Name          string  `json:"name"`
		LatestPrice   float64 `json:"latest_price"`
		ChangeAmount  float64 `json:"change_amount"`
		ChangePercent float64 `json:"change_percent"`
		Buy           float64 `json:"buy"`
		Sell          float64 `json:"sell"`
		PrevClose     float64 `json:"prev_close"`
		Open          float64 `json:"open"`
		High          float64 `json:"high"`
		Low           float64 `json:"low"`
		Volume        int64   `json:"volume"`
		Turnover      int64   `json:"turnover"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if req.Code == "" || req.TradingDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code and trading_date are required"})
		return
	}
	tDate, err := time.Parse("2006-01-02", req.TradingDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid trading_date format (YYYY-MM-DD)"})
		return
	}
	data := EtfDailyData{Code: req.Code, TradingDate: tDate, Name: req.Name, LatestPrice: req.LatestPrice, ChangeAmount: req.ChangeAmount, ChangePercent: req.ChangePercent, Buy: req.Buy, Sell: req.Sell, PrevClose: req.PrevClose, Open: req.Open, High: req.High, Low: req.Low, Volume: req.Volume, Turnover: req.Turnover}
	if err := h.UpsertEtfDaily(&data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "ETF daily upsert success"})
}

func (h *DatabaseHandler) batchInsertEtfDailyHandler(c *gin.Context) {
	var reqList []struct {
		Code          string  `json:"code"`
		TradingDate   string  `json:"trading_date"`
		Name          string  `json:"name"`
		LatestPrice   float64 `json:"latest_price"`
		ChangeAmount  float64 `json:"change_amount"`
		ChangePercent float64 `json:"change_percent"`
		Buy           float64 `json:"buy"`
		Sell          float64 `json:"sell"`
		PrevClose     float64 `json:"prev_close"`
		Open          float64 `json:"open"`
		High          float64 `json:"high"`
		Low           float64 `json:"low"`
		Volume        int64   `json:"volume"`
		Turnover      int64   `json:"turnover"`
	}
	if err := c.ShouldBindJSON(&reqList); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if len(reqList) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty list"})
		return
	}
	dataList := make([]EtfDailyData, 0, len(reqList))
	for i, r := range reqList {
		if r.Code == "" || r.TradingDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("code and trading_date required at index %d", i)})
			return
		}
		tDate, err := time.Parse("2006-01-02", r.TradingDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid trading_date at index %d", i)})
			return
		}
		dataList = append(dataList, EtfDailyData{Code: r.Code, TradingDate: tDate, Name: r.Name, LatestPrice: r.LatestPrice, ChangeAmount: r.ChangeAmount, ChangePercent: r.ChangePercent, Buy: r.Buy, Sell: r.Sell, PrevClose: r.PrevClose, Open: r.Open, High: r.High, Low: r.Low, Volume: r.Volume, Turnover: r.Turnover})
	}
	if err := h.BatchUpsertEtfDaily(dataList); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "ETF daily batch upsert success", Data: gin.H{"count": len(dataList)}})
}

func (h *DatabaseHandler) getEtfDailyHandler(c *gin.Context) {
	code := c.Param("code")
	var req struct {
		Limit  *int `json:"limit"`
		Offset *int `json:"offset"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	limit := 100
	if req.Limit != nil {
		limit = *req.Limit
	}
	offset := 0
	if req.Offset != nil {
		offset = *req.Offset
	}
	data, err := h.GetEtfDaily(code, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) getEtfDailyByDateRangeHandler(c *gin.Context) {
	code := c.Param("code")
	var req struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date are required"})
		return
	}
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format (YYYY-MM-DD)"})
		return
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format (YYYY-MM-DD)"})
		return
	}
	data, err := h.GetEtfDailyByDateRange(code, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) insertIndexInfoHandler(c *gin.Context) {
	var req struct {
		Code        string `json:"code"`
		DisplayName string `json:"display_name"`
		PublishDate string `json:"publish_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if req.Code == "" || req.PublishDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code and publish_date are required"})
		return
	}
	pDate, err := time.Parse("2006-01-02", req.PublishDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid publish_date format (YYYY-MM-DD)"})
		return
	}
	payload := IndexInfo{Code: req.Code, DisplayName: req.DisplayName, PublishDate: pDate}
	if err := h.UpsertIndexInfo(&payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"affected": 1}})
}

func (h *DatabaseHandler) batchInsertIndexInfoHandler(c *gin.Context) {
	var req []struct {
		Code        string `json:"code"`
		DisplayName string `json:"display_name"`
		PublishDate string `json:"publish_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty list"})
		return
	}
	list := make([]IndexInfo, 0, len(req))
	for i, v := range req {
		if v.Code == "" || v.PublishDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d missing code or publish_date", i)})
			return
		}
		pDate, err := time.Parse("2006-01-02", v.PublishDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d invalid publish_date format", i)})
			return
		}
		list = append(list, IndexInfo{Code: v.Code, DisplayName: v.DisplayName, PublishDate: pDate})
	}
	if err := h.BatchUpsertIndexInfo(list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"affected": len(list)}})
}

func (h *DatabaseHandler) insertIndexDailyHandler(c *gin.Context) {
	var req struct {
		Code          string  `json:"code"`
		TradingDate   string  `json:"trading_date"`
		Open          float64 `json:"open"`
		Close         float64 `json:"close"`
		High          float64 `json:"high"`
		Low           float64 `json:"low"`
		Volume        int64   `json:"volume"`
		Amount        float64 `json:"amount"`
		ChangePercent float64 `json:"change_percent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if req.Code == "" || req.TradingDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code and trading_date are required"})
		return
	}
	tDate, err := time.Parse("2006-01-02", req.TradingDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid trading_date format (YYYY-MM-DD)"})
		return
	}
	payload := IndexDailyData{Code: req.Code, TradingDate: tDate, Open: req.Open, Close: req.Close, High: req.High, Low: req.Low, Volume: req.Volume, Amount: req.Amount, ChangePercent: req.ChangePercent}
	if err := h.UpsertIndexDaily(&payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"affected": 1}})
}

func (h *DatabaseHandler) batchInsertIndexDailyHandler(c *gin.Context) {
	var req []struct {
		Code          string  `json:"code"`
		TradingDate   string  `json:"trading_date"`
		Open          float64 `json:"open"`
		Close         float64 `json:"close"`
		High          float64 `json:"high"`
		Low           float64 `json:"low"`
		Volume        int64   `json:"volume"`
		Amount        float64 `json:"amount"`
		ChangePercent float64 `json:"change_percent"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty list"})
		return
	}
	list := make([]IndexDailyData, 0, len(req))
	for i, v := range req {
		if v.Code == "" || v.TradingDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d missing code or trading_date", i)})
			return
		}
		tDate, err := time.Parse("2006-01-02", v.TradingDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d invalid trading_date format", i)})
			return
		}
		list = append(list, IndexDailyData{Code: v.Code, TradingDate: tDate, Open: v.Open, Close: v.Close, High: v.High, Low: v.Low, Volume: v.Volume, Amount: v.Amount, ChangePercent: v.ChangePercent})
	}
	if err := h.BatchUpsertIndexDaily(list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"affected": len(list)}})
}

func (h *DatabaseHandler) batchInsertAStockCommentDailyHandler(c *gin.Context) {
	var req []struct {
		Code                     string  `json:"code"`
		TradingDate              string  `json:"trading_date"`
		Name                     string  `json:"name"`
		LatestPrice              float64 `json:"latest_price"`
		ChangePercent            float64 `json:"change_percent"`
		TurnoverRate             float64 `json:"turnover_rate"`
		PeRatio                  float64 `json:"pe_ratio"`
		MainCost                 float64 `json:"main_cost"`
		InstitutionParticipation float64 `json:"institution_participation"`
		CompositeScore           float64 `json:"composite_score"`
		Rise                     int64   `json:"rise"`
		CurrentRank              int64   `json:"current_rank"`
		AttentionIndex           float64 `json:"attention_index"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty list"})
		return
	}
	list := make([]StockCommentDaily, 0, len(req))
	for i, v := range req {
		if v.Code == "" || v.TradingDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d missing code or trading_date", i)})
			return
		}
		tDate, err := time.Parse("2006-01-02", v.TradingDate)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("item %d invalid trading_date format", i)})
			return
		}
		list = append(list, StockCommentDaily{Code: v.Code, TradingDate: tDate, Name: v.Name, LatestPrice: v.LatestPrice, ChangePercent: v.ChangePercent, TurnoverRate: v.TurnoverRate, PeRatio: v.PeRatio, MainCost: v.MainCost, InstitutionParticipation: v.InstitutionParticipation, CompositeScore: v.CompositeScore, Rise: v.Rise, CurrentRank: v.CurrentRank, AttentionIndex: v.AttentionIndex})
	}
	if err := h.BatchUpsertAStockCommentDaily(list); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: gin.H{"affected": len(list)}})
}

func (h *DatabaseHandler) getAStockCommentDailyByNameHandler(c *gin.Context) {
	var req struct {
		Name      string `json:"name"`
		Limit     *int   `json:"limit"`
		Offset    *int   `json:"offset"`
		StockType int    `json:"stock_type"`
		Symbol    string `json:"symbol"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	limit := 20
	if req.Limit != nil && *req.Limit > 0 {
		limit = *req.Limit
	}
	offset := 0
	if req.Offset != nil && *req.Offset >= 0 {
		offset = *req.Offset
	}
	data, err := h.GetAStockCommentDailyByName(req.Name, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) getAStockCommentDailyByCodeHandler(c *gin.Context) {
	code := c.Param("code")
	limit := 1
	offset := 0
	data, err := h.GetAStockCommentDailyByCode(code, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) getIndexDailyHandler(c *gin.Context) {
	code := c.Param("code")
	var req struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Limit = 20
		req.Offset = 0
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	data, err := h.GetIndexDaily(code, req.Limit, req.Offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

func (h *DatabaseHandler) getIndexDailyByDateRangeHandler(c *gin.Context) {
	code := c.Param("code")
	var req struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return
	}
	if req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date and end_date are required"})
		return
	}
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid start_date format (YYYY-MM-DD)"})
		return
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid end_date format (YYYY-MM-DD)"})
		return
	}
	data, err := h.GetIndexDailyByDateRange(code, startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, ApiResponse{Code: 200, Message: "Success", Data: data})
}

// saveLlmTokenUsageHandler saves a new LLM token usage record
func (h *DatabaseHandler) saveLlmTokenUsageHandler(c *gin.Context) {
	var usage LlmTokenUsage
	if err := c.ShouldBindJSON(&usage); err != nil {
		c.JSON(http.StatusBadRequest, ApiResponse{Code: 400, Message: fmt.Sprintf("Invalid request body: %v", err)})
		return
	}

	// Validate required fields
	if usage.UserID == 0 {
		c.JSON(http.StatusBadRequest, ApiResponse{Code: 400, Message: "user_id is required"})
		return
	}
	if usage.Provider == "" {
		c.JSON(http.StatusBadRequest, ApiResponse{Code: 400, Message: "provider is required"})
		return
	}
	if usage.Model == "" {
		c.JSON(http.StatusBadRequest, ApiResponse{Code: 400, Message: "model is required"})
		return
	}

	if err := h.SaveLlmTokenUsage(&usage); err != nil {
		slog.Error("Failed to save LLM token usage", "error", err)
		c.JSON(http.StatusInternalServerError, ApiResponse{Code: 500, Message: fmt.Sprintf("Failed to save token usage: %v", err)})
		return
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 0, Message: "success", Data: usage})
}

// getLlmTokenUsageByUserHandler retrieves LLM token usage records for a user
func (h *DatabaseHandler) getLlmTokenUsageByUserHandler(c *gin.Context) {
	userIDStr := c.Param("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ApiResponse{Code: 400, Message: "Invalid user_id"})
		return
	}

	// Parse query parameters
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	var startDate, endDate *time.Time
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		parsed, err := time.Parse(time.RFC3339, startDateStr)
		if err == nil {
			startDate = &parsed
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		parsed, err := time.Parse(time.RFC3339, endDateStr)
		if err == nil {
			endDate = &parsed
		}
	}

	var provider, model *string
	if p := c.Query("provider"); p != "" {
		provider = &p
	}
	if m := c.Query("model"); m != "" {
		model = &m
	}

	records, err := h.GetLlmTokenUsageByUser(userID, limit, offset, startDate, endDate, provider, model)
	if err != nil {
		slog.Error("Failed to get LLM token usage", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, ApiResponse{Code: 500, Message: fmt.Sprintf("Failed to retrieve token usage: %v", err)})
		return
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 0, Message: "success", Data: records})
}

// getLlmTokenUsageStatsHandler retrieves aggregated token usage statistics for a user
func (h *DatabaseHandler) getLlmTokenUsageStatsHandler(c *gin.Context) {
	userIDStr := c.Param("user_id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, ApiResponse{Code: 400, Message: "Invalid user_id"})
		return
	}

	var startDate, endDate *time.Time
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		parsed, err := time.Parse(time.RFC3339, startDateStr)
		if err == nil {
			startDate = &parsed
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		parsed, err := time.Parse(time.RFC3339, endDateStr)
		if err == nil {
			endDate = &parsed
		}
	}

	stats, err := h.GetLlmTokenUsageStats(userID, startDate, endDate)
	if err != nil {
		slog.Error("Failed to get LLM token usage stats", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, ApiResponse{Code: 500, Message: fmt.Sprintf("Failed to retrieve token usage stats: %v", err)})
		return
	}

	c.JSON(http.StatusOK, ApiResponse{Code: 0, Message: "success", Data: stats})
}
