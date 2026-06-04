package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"fintrack-api/models"
)

const minAStockTradeShares = 100.0
const shareLotEpsilon = 0.000001

type backtestTradeExecutionPoint struct {
	Date  string
	Price float64
}

type mtfBacktestParams struct {
	BuyThresholdPct        float64
	SellThresholdPct       float64
	InitialCash            float64
	EnableRebalance        bool
	MaxPositionPct         float64
	MinPositionPct         float64
	SlopePositionPerPct    float64
	RebalanceTolerancePct  float64
	TradeFeeRate           float64
	TakeProfitThresholdPct float64
	TakeProfitSellFrac     float64
}

func normalizeBacktestSellSize(
	shares float64,
	requestedSellSize float64,
	startPrice float64,
	portfolioValueStart float64,
	minPositionPct float64,
) float64 {
	if shares <= 0 || requestedSellSize <= 0 {
		return 0
	}

	sellSize := math.Min(requestedSellSize, shares)
	if sellSize < minAStockTradeShares {
		sellSize = math.Min(minAStockTradeShares, shares)
	} else if sellSize < shares-shareLotEpsilon {
		sellSize = math.Floor((sellSize+shareLotEpsilon)/minAStockTradeShares) * minAStockTradeShares
	}

	remainingShares := shares - sellSize
	remainingPositionPct := 0.0
	if portfolioValueStart > 0 {
		remainingPositionPct = remainingShares * startPrice / portfolioValueStart
	}
	if remainingShares > 0 && (remainingShares < minAStockTradeShares || remainingPositionPct < minPositionPct || !isWholeShareLot(remainingShares)) {
		return shares
	}
	return sellSize
}

func normalizeBacktestBuySize(requestedBuySize float64, maxAffordable float64) float64 {
	if requestedBuySize <= 0 || maxAffordable < minAStockTradeShares {
		return 0
	}
	buySize := requestedBuySize
	if buySize < minAStockTradeShares {
		buySize = minAStockTradeShares
	}
	buySize = math.Floor((buySize+shareLotEpsilon)/minAStockTradeShares) * minAStockTradeShares
	return math.Min(buySize, math.Floor((maxAffordable+shareLotEpsilon)/minAStockTradeShares)*minAStockTradeShares)
}

func isWholeShareLot(shares float64) bool {
	if shares <= shareLotEpsilon {
		return true
	}
	lots := shares / minAStockTradeShares
	return math.Abs(lots-math.Round(lots)) <= shareLotEpsilon
}

func chunkTradeExecutionPoint(chunk models.SaveMTFValChunkRequest, action string, fallbackPrice float64) backtestTradeExecutionPoint {
	point := backtestTradeExecutionPoint{
		Date:  chunk.StartDate,
		Price: fallbackPrice,
	}

	bestIndex := -1
	bestClose := math.NaN()
	for i, price := range chunk.Actual {
		if !validFloat(price) {
			continue
		}
		if bestIndex < 0 ||
			(strings.EqualFold(action, "buy") && price < bestClose) ||
			(strings.EqualFold(action, "sell") && price > bestClose) {
			bestIndex = i
			bestClose = price
		}
	}
	if bestIndex < 0 {
		return point
	}
	point.Price = bestClose
	if strings.EqualFold(action, "buy") && bestIndex < len(chunk.Open) && validFloat(chunk.Open[bestIndex]) && chunk.Open[bestIndex] > 0 {
		point.Price = chunk.Open[bestIndex]
	}
	if bestIndex < len(chunk.Dates) && strings.TrimSpace(chunk.Dates[bestIndex]) != "" {
		point.Date = chunk.Dates[bestIndex]
	}
	return point
}

type postgresHandlerStockRangeResponse struct {
	Code    int                         `json:"code"`
	Message string                      `json:"message"`
	Data    []postgresHandlerStockPrice `json:"data"`
	Error   string                      `json:"error"`
}

type postgresHandlerStockPrice struct {
	DateStr  string  `json:"date_str"`
	Datetime string  `json:"datetime"`
	Open     float64 `json:"open"`
}

func resolveMTFBacktestStockType(req *models.MTFBacktestRequest, symbol string) int {
	if req != nil && req.StockType != nil {
		normalized := strings.ToLower(strings.TrimSpace(*req.StockType))
		switch normalized {
		case "2", "etf", "fund":
			return 2
		case "1", "stock", "a", "a_stock":
			return 1
		}
		if value, err := strconv.Atoi(normalized); err == nil && value > 0 {
			return value
		}
	}
	return inferLookupStockTypes(symbol)[0]
}

func (s *WatchlistService) fetchPostgresHandlerOpenPricesByDate(symbol string, stockType int, startDate string, endDate string) (map[string]float64, error) {
	if s == nil || s.config == nil || strings.TrimSpace(s.config.PostgresHandler.BaseURL) == "" {
		return nil, fmt.Errorf("postgres handler is not configured")
	}

	payload := map[string]interface{}{
		"type":       stockType,
		"start_date": startDate,
		"end_date":   endDate,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(s.config.PostgresHandler.BaseURL), "/")
	requestURL := fmt.Sprintf("%s/api/v1/stock-data/%s/range", baseURL, url.PathEscape(strings.TrimSpace(symbol)))
	req, err := http.NewRequest(http.MethodPost, requestURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(s.config.PostgresHandler.APIToken); token != "" {
		req.Header.Set("X-Token", token)
	}

	timeout := s.config.PostgresHandler.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	client := newInferenceGatewayHTTPClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request postgres handler stock range failed: %w", err)
	}
	defer resp.Body.Close()

	var decoded postgresHandlerStockRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode postgres handler stock range response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := decoded.Error
		if message == "" {
			message = decoded.Message
		}
		return nil, fmt.Errorf("postgres handler stock range returned status %d: %s", resp.StatusCode, message)
	}
	if decoded.Code != 0 && decoded.Code != http.StatusOK {
		message := decoded.Error
		if message == "" {
			message = decoded.Message
		}
		return nil, fmt.Errorf("postgres handler stock range returned code %d: %s", decoded.Code, message)
	}

	out := make(map[string]float64, len(decoded.Data))
	for _, row := range decoded.Data {
		date := strings.TrimSpace(row.DateStr)
		if date == "" && len(row.Datetime) >= 10 {
			date = row.Datetime[:10]
		}
		if date == "" || !validFloat(row.Open) || row.Open <= 0 {
			continue
		}
		out[date] = row.Open
	}
	return out, nil
}

func (s *WatchlistService) hydrateBacktestOpenPrices(symbol string, stockType int, chunks []models.SaveMTFValChunkRequest) ([]models.SaveMTFValChunkRequest, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}
	startDate := strings.TrimSpace(chunks[0].StartDate)
	endDate := strings.TrimSpace(chunks[len(chunks)-1].EndDate)
	if startDate == "" || endDate == "" {
		return chunks, nil
	}

	openByDate, err := s.fetchPostgresHandlerOpenPricesByDate(symbol, stockType, startDate, endDate)
	if err != nil {
		return nil, err
	}
	for i := range chunks {
		opens := make([]float64, len(chunks[i].Dates))
		for j, date := range chunks[i].Dates {
			opens[j] = openByDate[strings.TrimSpace(date)]
		}
		chunks[i].Open = opens
	}
	return chunks, nil
}

func (s *WatchlistService) RunMTFBacktest(req *models.MTFBacktestRequest) (int, map[string]interface{}, error) {
	uniqueKey := strings.TrimSpace(req.UniqueKey)
	if uniqueKey == "" {
		uniqueKey = buildMTFBacktestUniqueKey(req)
	}
	if uniqueKey == "" {
		return 400, map[string]interface{}{
			"success": false,
			"message": "回测需要 unique_key，或同时提供 symbol/context_len/horizon_len/mtf_version",
		}, nil
	}

	best, err := s.GetMTFBestByUniqueKey(uniqueKey)
	if err != nil {
		if err == sql.ErrNoRows {
			return 404, map[string]interface{}{
				"success": false,
				"message": "暂无可用于回测的预测结果，请先执行 MTF 预测推理。",
			}, nil
		}
		return 0, nil, err
	}

	chunks, err := s.ListValidationChunksByUniqueKey(uniqueKey)
	if err != nil {
		return 0, nil, err
	}
	chunks = currentValidationWindowChunks(best, chunks)
	if len(chunks) == 0 {
		return 404, map[string]interface{}{
			"success":    false,
			"stock_code": best.Symbol,
			"message":    "暂无可用于回测的预测结果，请先执行 MTF 预测推理。",
		}, nil
	}
	stockType := resolveMTFBacktestStockType(req, best.Symbol)
	chunks, err = s.hydrateBacktestOpenPrices(best.Symbol, stockType, chunks)
	if err != nil {
		return 400, map[string]interface{}{
			"success":    false,
			"stock_code": best.Symbol,
			"message":    "获取回测开盘价失败",
			"error":      err.Error(),
		}, nil
	}

	params := buildMTFBacktestParams(req)
	if strategy, err := s.GetStrategyParamsByUniqueKey(uniqueKey); err == nil && strategy != nil {
		params = backtestParamsFromStrategy(strategy)
	} else if err != nil && err != sql.ErrNoRows {
		return 0, nil, err
	}

	saveReq, backtest, err := runMTFBacktestOnChunks(best, chunks, params)
	if err != nil {
		return 400, map[string]interface{}{
			"success":    false,
			"stock_code": best.Symbol,
			"message":    "回测失败",
			"error":      err.Error(),
		}, nil
	}
	saveReq.UserID = req.UserID
	saveReq.StrategyParamsID = req.StrategyParamsID
	if err := s.SaveMTFBacktest(saveReq); err != nil {
		return 0, nil, err
	}

	return 200, map[string]interface{}{
		"success":    true,
		"stock_code": best.Symbol,
		"message":    "回测完成",
		"backtest":   backtest,
	}, nil
}

func buildMTFBacktestUniqueKey(req *models.MTFBacktestRequest) string {
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" || req.HorizonLen == nil || req.ContextLen == nil {
		return ""
	}
	version := "2.5"
	if req.MTFVersion != nil && strings.TrimSpace(*req.MTFVersion) != "" {
		version = strings.TrimSpace(*req.MTFVersion)
	}
	return fmt.Sprintf("%s_best_hlen_%d_clen_%d_v_%s", normalizeMTFSymbolReadKey(symbol), *req.HorizonLen, *req.ContextLen, version)
}

func buildMTFBacktestParams(req *models.MTFBacktestRequest) mtfBacktestParams {
	params := mtfBacktestParams{
		BuyThresholdPct:        3.0,
		SellThresholdPct:       -1.0,
		InitialCash:            100000.0,
		EnableRebalance:        true,
		MaxPositionPct:         1.0,
		MinPositionPct:         0.2,
		SlopePositionPerPct:    0.1,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0.006,
		TakeProfitThresholdPct: 10.0,
		TakeProfitSellFrac:     0.5,
	}
	if req.BuyThresholdPct != nil {
		params.BuyThresholdPct = *req.BuyThresholdPct
	}
	if req.SellThresholdPct != nil {
		params.SellThresholdPct = *req.SellThresholdPct
	}
	if req.InitialCash != nil {
		params.InitialCash = *req.InitialCash
	}
	if req.EnableRebalance != nil {
		params.EnableRebalance = *req.EnableRebalance
	}
	if req.MaxPositionPct != nil {
		params.MaxPositionPct = *req.MaxPositionPct
	}
	if req.MinPositionPct != nil {
		params.MinPositionPct = *req.MinPositionPct
	}
	if req.SlopePositionPerPct != nil {
		params.SlopePositionPerPct = *req.SlopePositionPerPct
	}
	if req.RebalanceTolerancePct != nil {
		params.RebalanceTolerancePct = *req.RebalanceTolerancePct
	}
	if req.TradeFeeRate != nil {
		params.TradeFeeRate = *req.TradeFeeRate
	}
	if req.TakeProfitThresholdPct != nil {
		params.TakeProfitThresholdPct = *req.TakeProfitThresholdPct
	}
	if req.TakeProfitSellFrac != nil {
		params.TakeProfitSellFrac = *req.TakeProfitSellFrac
	}
	return params
}

func backtestParamsFromStrategy(strategy *models.StrategyParams) mtfBacktestParams {
	return mtfBacktestParams{
		BuyThresholdPct:        strategy.BuyThresholdPct,
		SellThresholdPct:       strategy.SellThresholdPct,
		InitialCash:            strategy.InitialCash,
		EnableRebalance:        strategy.EnableRebalance,
		MaxPositionPct:         strategy.MaxPositionPct,
		MinPositionPct:         strategy.MinPositionPct,
		SlopePositionPerPct:    strategy.SlopePositionPerPct,
		RebalanceTolerancePct:  strategy.RebalanceTolerancePct,
		TradeFeeRate:           strategy.TradeFeeRate,
		TakeProfitThresholdPct: strategy.TakeProfitThresholdPct,
		TakeProfitSellFrac:     strategy.TakeProfitSellFrac,
	}
}

func runMTFBacktestOnChunks(best *models.MTFBestPrediction, chunks []models.SaveMTFValChunkRequest, params mtfBacktestParams) (*models.SaveMTFBacktestRequest, map[string]interface{}, error) {
	if best == nil {
		return nil, nil, fmt.Errorf("missing best prediction")
	}
	fixedQuantile := strings.TrimSpace(best.BestPredictionItem)
	if fixedQuantile == "" {
		return nil, nil, fmt.Errorf("missing best prediction item")
	}
	if params.InitialCash <= 0 {
		return nil, nil, fmt.Errorf("initial_cash must be positive")
	}
	chunks = currentValidationWindowChunks(best, chunks)

	cash := params.InitialCash
	shares := 0.0
	lastPrice := math.NaN()
	totalFeesPaid := 0.0
	predictedChanges := make([]float64, 0, len(chunks))
	perChunkSignals := make([]map[string]interface{}, 0, len(chunks))
	trades := make([]map[string]interface{}, 0)
	equityCurveValues := make([]float64, 0, len(chunks))
	equityCurvePct := make([]float64, 0, len(chunks))
	equityCurvePctGross := make([]float64, 0, len(chunks))
	curveDates := make([]string, 0, len(chunks))
	actualEndPrices := make([]float64, 0, len(chunks))
	usableChunks := make([]models.SaveMTFValChunkRequest, 0, len(chunks))

	for _, chunk := range chunks {
		if !isUsableBacktestChunk(chunk, fixedQuantile) {
			continue
		}
		startPrice := chunk.Actual[0]
		buyPoint := chunkTradeExecutionPoint(chunk, "buy", startPrice)
		sellPoint := chunkTradeExecutionPoint(chunk, "sell", startPrice)
		predValues := coerceFloatSlice(chunk.Predictions[fixedQuantile])
		usableChunks = append(usableChunks, chunk)
		predictedPctChange := ((predValues[len(predValues)-1] / startPrice) - 1) * 100
		predictedChanges = append(predictedChanges, predictedPctChange)
		if len(perChunkSignals) < 50 {
			perChunkSignals = append(perChunkSignals, map[string]interface{}{
				"chunk_index":          chunk.ChunkIndex,
				"date":                 chunk.StartDate,
				"best_key":             fixedQuantile,
				"predicted_pct_change": predictedPctChange,
				"start_price":          startPrice,
			})
		}

		portfolioValueStart := cash + shares*startPrice
		currentPositionPct := 0.0
		if portfolioValueStart > 0 {
			currentPositionPct = shares * startPrice / portfolioValueStart
		}

		targetPositionPct := currentPositionPct
		forceClearPosition := false
		rebalanceNeeded := false
		if params.EnableRebalance {
			if predictedPctChange >= params.BuyThresholdPct {
				extraStrength := math.Max(0, predictedPctChange-params.BuyThresholdPct)
				targetPositionPct = math.Min(params.MaxPositionPct, params.MinPositionPct+params.SlopePositionPerPct*extraStrength)
			} else if predictedPctChange <= params.SellThresholdPct {
				targetPositionPct = 0
				forceClearPosition = shares > 0
			}
			deltaPct := targetPositionPct - currentPositionPct
			rebalanceNeeded = (forceClearPosition || math.Abs(deltaPct) > params.RebalanceTolerancePct) && portfolioValueStart > 0
		}

		tpFrac := clamp(params.TakeProfitSellFrac, 0, 1)
		if !rebalanceNeeded && params.TakeProfitThresholdPct > 0 && tpFrac > 0 && shares > 0 && portfolioValueStart > 0 {
			cumReturnStartPct := (portfolioValueStart/params.InitialCash - 1) * 100
			if cumReturnStartPct >= params.TakeProfitThresholdPct {
				sellSize := normalizeBacktestSellSize(shares, shares*tpFrac, sellPoint.Price, portfolioValueStart, params.MinPositionPct)
				proceeds := sellSize * sellPoint.Price
				fee := proceeds * params.TradeFeeRate
				shares -= sellSize
				cash += proceeds - fee
				totalFeesPaid += fee
				trades = append(trades, backtestTrade(sellPoint.Date, "sell", sellPoint.Price, sellSize, chunk.ChunkIndex, fmt.Sprintf("take_profit>= %.2f", params.TakeProfitThresholdPct), fee))
			}
		}

		if params.EnableRebalance {
			if rebalanceNeeded {
				if targetPositionPct > currentPositionPct {
					targetShares := (targetPositionPct * portfolioValueStart) / buyPoint.Price
					buySize := targetShares - shares
					maxAffordable := cash / (buyPoint.Price * (1 + params.TradeFeeRate))
					buySize = normalizeBacktestBuySize(buySize, maxAffordable)
					if buySize > 0 {
						cost := buySize * buyPoint.Price
						fee := cost * params.TradeFeeRate
						shares += buySize
						cash -= cost + fee
						totalFeesPaid += fee
						trades = append(trades, backtestTrade(buyPoint.Date, "buy", buyPoint.Price, buySize, chunk.ChunkIndex, fmt.Sprintf("rebalance_up-> %.2f", targetPositionPct), fee))
					}
				} else {
					targetShares := (targetPositionPct * portfolioValueStart) / sellPoint.Price
					sellSize := normalizeBacktestSellSize(shares, shares-targetShares, sellPoint.Price, portfolioValueStart, params.MinPositionPct)
					if sellSize > 0 {
						proceeds := sellSize * sellPoint.Price
						fee := proceeds * params.TradeFeeRate
						shares -= sellSize
						cash += proceeds - fee
						totalFeesPaid += fee
						trades = append(trades, backtestTrade(sellPoint.Date, "sell", sellPoint.Price, sellSize, chunk.ChunkIndex, fmt.Sprintf("rebalance_down-> %.2f", targetPositionPct), fee))
					}
				}
			}
		} else if predictedPctChange >= params.BuyThresholdPct {
			size := cash / (buyPoint.Price * (1 + params.TradeFeeRate))
			size = normalizeBacktestBuySize(size, size)
			if size > 0 {
				cost := size * buyPoint.Price
				fee := cost * params.TradeFeeRate
				shares += size
				cash -= cost + fee
				totalFeesPaid += fee
				trades = append(trades, backtestTrade(buyPoint.Date, "buy", buyPoint.Price, size, chunk.ChunkIndex, fmt.Sprintf("pred_pct>=%v", params.BuyThresholdPct), fee))
			}
		} else if predictedPctChange <= params.SellThresholdPct && shares > 0 {
			sellSize := normalizeBacktestSellSize(shares, shares, sellPoint.Price, portfolioValueStart, params.MinPositionPct)
			proceeds := sellSize * sellPoint.Price
			fee := proceeds * params.TradeFeeRate
			cash += proceeds - fee
			totalFeesPaid += fee
			trades = append(trades, backtestTrade(sellPoint.Date, "sell", sellPoint.Price, sellSize, chunk.ChunkIndex, fmt.Sprintf("pred_pct<=%v", params.SellThresholdPct), fee))
			shares -= sellSize
		}

		endPrice, ok := lastValidFloat(chunk.Actual)
		if !ok {
			endPrice = startPrice
		}
		pvEnd := cash + shares*endPrice
		equityCurveValues = append(equityCurveValues, pvEnd)
		equityCurvePct = append(equityCurvePct, (pvEnd/params.InitialCash-1)*100)
		pvEndGross := pvEnd + totalFeesPaid
		equityCurvePctGross = append(equityCurvePctGross, (pvEndGross/params.InitialCash-1)*100)
		actualEndPrices = append(actualEndPrices, endPrice)
		curveDates = append(curveDates, chunk.EndDate)
		lastPrice = endPrice
	}

	if len(equityCurveValues) == 0 {
		return nil, nil, fmt.Errorf("no usable validation chunks for quantile %s", fixedQuantile)
	}

	finalValue := cash
	if validFloat(lastPrice) {
		finalValue += shares * lastPrice
	}
	totalReturn := (finalValue/params.InitialCash - 1) * 100
	periodDays := backtestPeriodDays(usableChunks)
	annualized := (math.Pow(finalValue/params.InitialCash, 365.0/float64(periodDays)) - 1) * 100
	grossFinalValue := finalValue + totalFeesPaid
	grossTotalReturn := (grossFinalValue/params.InitialCash - 1) * 100
	grossAnnualized := (math.Pow(grossFinalValue/params.InitialCash, 365.0/float64(periodDays)) - 1) * 100
	benchmarkReturn, benchmarkAnnualized := benchmarkReturnPct(usableChunks, periodDays)

	validationPeriodDays := validationPeriodDays(usableChunks)
	validationBenchmarkReturn, validationBenchmarkAnnualized := benchmarkReturnPct(usableChunks, validationPeriodDays)
	validationStartDate := firstChunkDate(usableChunks, true)
	validationEndDate := firstChunkDate(usableChunks, false)
	actualTotalReturnPct := actualTotalReturn(usableChunks)

	positionControl := map[string]interface{}{
		"enable_rebalance":          params.EnableRebalance,
		"max_position_pct":          params.MaxPositionPct,
		"min_position_pct":          params.MinPositionPct,
		"slope_position_per_pct":    params.SlopePositionPerPct,
		"rebalance_tolerance_pct":   params.RebalanceTolerancePct,
		"take_profit_threshold_pct": params.TakeProfitThresholdPct,
		"take_profit_sell_frac":     params.TakeProfitSellFrac,
	}
	stats := predictedChangeStats(predictedChanges, params)
	result := map[string]interface{}{
		"unique_key":                      best.UniqueKey,
		"symbol":                          best.Symbol,
		"mtf_version":                     best.MTFVersion,
		"context_len":                     best.ContextLen,
		"horizon_len":                     best.HorizonLen,
		"covariate_signature":             best.CovariateSignature,
		"initial_cash":                    params.InitialCash,
		"final_value":                     finalValue,
		"total_return_pct":                totalReturn,
		"annualized_return_pct":           annualized,
		"final_value_gross":               grossFinalValue,
		"total_return_pct_gross":          grossTotalReturn,
		"annualized_return_pct_gross":     grossAnnualized,
		"net_profit":                      finalValue - params.InitialCash,
		"gross_profit":                    grossFinalValue - params.InitialCash,
		"trades":                          trades,
		"buy_threshold_pct":               params.BuyThresholdPct,
		"sell_threshold_pct":              params.SellThresholdPct,
		"used_quantile":                   fixedQuantile,
		"predicted_change_stats":          stats,
		"per_chunk_signals":               perChunkSignals,
		"benchmark_return_pct":            benchmarkReturn,
		"benchmark_annualized_return_pct": benchmarkAnnualized,
		"period_days":                     periodDays,
		"position_control":                positionControl,
		"trade_fee_rate":                  params.TradeFeeRate,
		"total_fees_paid":                 totalFeesPaid,
		"actual_total_return_pct":         actualTotalReturnPct,
		"equity_curve_values":             equityCurveValues,
		"equity_curve_pct":                equityCurvePct,
		"equity_curve_pct_gross":          equityCurvePctGross,
		"curve_dates":                     curveDates,
		"actual_end_prices":               actualEndPrices,
		"validation_start_date":           validationStartDate,
		"validation_end_date":             validationEndDate,
		"validation_benchmark_return_pct": validationBenchmarkReturn,
		"validation_benchmark_annualized_return_pct": validationBenchmarkAnnualized,
		"validation_period_days":                     validationPeriodDays,
	}

	saveReq := &models.SaveMTFBacktestRequest{
		UniqueKey:                              best.UniqueKey,
		Symbol:                                 best.Symbol,
		MTFVersion:                             best.MTFVersion,
		ContextLen:                             best.ContextLen,
		HorizonLen:                             best.HorizonLen,
		UsedQuantile:                           fixedQuantile,
		BuyThresholdPct:                        params.BuyThresholdPct,
		SellThresholdPct:                       params.SellThresholdPct,
		TradeFeeRate:                           params.TradeFeeRate,
		TotalFeesPaid:                          totalFeesPaid,
		ActualTotalReturnPct:                   actualTotalReturnPct,
		BenchmarkReturnPct:                     benchmarkReturn,
		BenchmarkAnnualizedReturnPct:           benchmarkAnnualized,
		PeriodDays:                             periodDays,
		ValidationStartDate:                    validationStartDate,
		ValidationEndDate:                      validationEndDate,
		ValidationBenchmarkReturnPct:           validationBenchmarkReturn,
		ValidationBenchmarkAnnualizedReturnPct: validationBenchmarkAnnualized,
		ValidationPeriodDays:                   validationPeriodDays,
		PositionControl:                        positionControl,
		PredictedChangeStats:                   stats,
		PerChunkSignals:                        perChunkSignals,
		EquityCurveValues:                      equityCurveValues,
		EquityCurvePct:                         equityCurvePct,
		EquityCurvePctGross:                    equityCurvePctGross,
		CurveDates:                             curveDates,
		ActualEndPrices:                        actualEndPrices,
		Trades:                                 trades,
		CovariateConfig:                        parseJSONObject(best.CovariateConfig),
		CovariateSignature:                     best.CovariateSignature,
		CovariateAnalysis:                      parseJSONObject(best.CovariateAnalysis),
	}
	return saveReq, result, nil
}

func currentValidationWindowChunks(best *models.MTFBestPrediction, chunks []models.SaveMTFValChunkRequest) []models.SaveMTFValChunkRequest {
	if best == nil || best.ValStartDate.IsZero() || best.ValEndDate.IsZero() || len(chunks) == 0 {
		return chunks
	}

	ordered := append([]models.SaveMTFValChunkRequest(nil), chunks...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ChunkIndex < ordered[j].ChunkIndex
	})

	valStart := best.ValStartDate.Format("2006-01-02")
	valEnd := best.ValEndDate.Format("2006-01-02")
	anchor := -1
	for i, chunk := range ordered {
		if normalizedBacktestDate(chunk.StartDate) == valStart {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		return chunks
	}

	current := make([]models.SaveMTFValChunkRequest, 0, len(ordered)-anchor)
	previousStart := ""
	for _, chunk := range ordered[anchor:] {
		startDate := normalizedBacktestDate(chunk.StartDate)
		endDate := normalizedBacktestDate(chunk.EndDate)
		if startDate == "" || endDate == "" {
			continue
		}
		if previousStart != "" && startDate < previousStart {
			break
		}
		if startDate > valEnd || endDate > valEnd {
			break
		}
		current = append(current, chunk)
		previousStart = startDate
		if endDate >= valEnd {
			break
		}
	}
	if len(current) == 0 {
		return chunks
	}
	return current
}

func normalizedBacktestDate(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 10 {
		return trimmed[:10]
	}
	return trimmed
}

func (s *WatchlistService) applyCurrentBacktestReferenceMetrics(uniqueKey string, out map[string]interface{}) {
	if out == nil {
		return
	}

	best, err := s.GetMTFBestByUniqueKey(uniqueKey)
	if err != nil || best == nil {
		return
	}
	fixedQuantile := strings.TrimSpace(best.BestPredictionItem)
	if fixedQuantile == "" {
		return
	}
	chunks, err := s.ListValidationChunksByUniqueKey(uniqueKey)
	if err != nil {
		return
	}
	usableChunks := usableBacktestChunks(currentValidationWindowChunks(best, chunks), fixedQuantile)
	if len(usableChunks) == 0 {
		return
	}

	periodDays := backtestPeriodDays(usableChunks)
	benchmarkReturn, benchmarkAnnualized := benchmarkReturnPct(usableChunks, periodDays)
	validationPeriodDays := validationPeriodDays(usableChunks)
	validationBenchmarkReturn, validationBenchmarkAnnualized := benchmarkReturnPct(usableChunks, validationPeriodDays)

	// 读取历史回测时修正参考收益，避免旧结果被未参与回测的分块污染。
	out["actual_total_return_pct"] = actualTotalReturn(usableChunks)
	out["benchmark_return_pct"] = benchmarkReturn
	out["benchmark_annualized_return_pct"] = benchmarkAnnualized
	out["period_days"] = periodDays
	out["validation_start_date"] = firstChunkDate(usableChunks, true)
	out["validation_end_date"] = firstChunkDate(usableChunks, false)
	out["validation_benchmark_return_pct"] = validationBenchmarkReturn
	out["validation_benchmark_annualized_return_pct"] = validationBenchmarkAnnualized
	out["validation_period_days"] = validationPeriodDays
}

func backtestTrade(date string, action string, price float64, size float64, chunkIndex int, reason string, fee float64) map[string]interface{} {
	return map[string]interface{}{
		"date":        date,
		"action":      action,
		"price":       price,
		"size":        size,
		"chunk_index": chunkIndex,
		"reason":      reason,
		"fee":         fee,
	}
}

func usableBacktestChunks(chunks []models.SaveMTFValChunkRequest, fixedQuantile string) []models.SaveMTFValChunkRequest {
	usable := make([]models.SaveMTFValChunkRequest, 0, len(chunks))
	for _, chunk := range chunks {
		if isUsableBacktestChunk(chunk, fixedQuantile) {
			usable = append(usable, chunk)
		}
	}
	return usable
}

func isUsableBacktestChunk(chunk models.SaveMTFValChunkRequest, fixedQuantile string) bool {
	if len(chunk.Actual) == 0 {
		return false
	}
	startPrice := chunk.Actual[0]
	if !validFloat(startPrice) || startPrice == 0 {
		return false
	}
	return len(coerceFloatSlice(chunk.Predictions[fixedQuantile])) > 0
}

func coerceFloatSlice(value interface{}) []float64 {
	switch typed := value.(type) {
	case []float64:
		return typed
	case []interface{}:
		out := make([]float64, 0, len(typed))
		for _, item := range typed {
			switch v := item.(type) {
			case float64:
				out = append(out, v)
			case int:
				out = append(out, float64(v))
			}
		}
		return out
	default:
		return nil
	}
}

func parseJSONObject(raw string) map[string]interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]interface{}{}
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]interface{}{}
	}
	if out == nil {
		return map[string]interface{}{}
	}
	return out
}

func validFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func lastValidFloat(values []float64) (float64, bool) {
	for i := len(values) - 1; i >= 0; i-- {
		if validFloat(values[i]) {
			return values[i], true
		}
	}
	return 0, false
}

func firstLastActual(chunks []models.SaveMTFValChunkRequest) (float64, float64, bool) {
	start := math.NaN()
	end := math.NaN()
	for _, chunk := range chunks {
		if len(chunk.Actual) > 0 && validFloat(chunk.Actual[0]) {
			start = chunk.Actual[0]
			break
		}
	}
	for i := len(chunks) - 1; i >= 0; i-- {
		if value, ok := lastValidFloat(chunks[i].Actual); ok {
			end = value
			break
		}
	}
	return start, end, validFloat(start) && validFloat(end) && start != 0
}

func actualTotalReturn(chunks []models.SaveMTFValChunkRequest) float64 {
	start, end, ok := firstLastActual(chunks)
	if !ok {
		return 0
	}
	return (end/start - 1) * 100
}

func benchmarkReturnPct(chunks []models.SaveMTFValChunkRequest, days int) (float64, float64) {
	start, end, ok := firstLastActual(chunks)
	if !ok {
		return 0, 0
	}
	total := (end/start - 1) * 100
	annualized := (math.Pow(end/start, 365.0/float64(maxInt(days, 1))) - 1) * 100
	return total, annualized
}

func backtestPeriodDays(chunks []models.SaveMTFValChunkRequest) int {
	if len(chunks) == 0 {
		return 1
	}
	return daysBetween(chunks[0].StartDate, chunks[len(chunks)-1].EndDate)
}

func validationPeriodDays(chunks []models.SaveMTFValChunkRequest) int {
	if len(chunks) == 0 {
		return 0
	}
	return daysBetween(chunks[0].StartDate, chunks[len(chunks)-1].EndDate)
}

func firstChunkDate(chunks []models.SaveMTFValChunkRequest, start bool) string {
	if len(chunks) == 0 {
		return ""
	}
	if start {
		return chunks[0].StartDate
	}
	return chunks[len(chunks)-1].EndDate
}

func daysBetween(startRaw string, endRaw string) int {
	start, errStart := parseBacktestDate(startRaw)
	end, errEnd := parseBacktestDate(endRaw)
	if errStart != nil || errEnd != nil {
		return 1
	}
	days := int(end.Sub(start).Hours() / 24)
	return maxInt(days, 1)
}

func parseBacktestDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) >= len("2006-01-02") {
		raw = raw[:len("2006-01-02")]
	}
	return time.Parse("2006-01-02", raw)
}

func predictedChangeStats(values []float64, params mtfBacktestParams) map[string]interface{} {
	if len(values) == 0 {
		return map[string]interface{}{}
	}
	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)
	sum := 0.0
	aboveBuy := 0
	belowSell := 0
	for _, value := range values {
		sum += value
		if value >= params.BuyThresholdPct {
			aboveBuy++
		}
		if value <= params.SellThresholdPct {
			belowSell++
		}
	}
	return map[string]interface{}{
		"count_chunks":     len(values),
		"mean":             sum / float64(len(values)),
		"median":           percentile(sortedValues, 50),
		"p75":              percentile(sortedValues, 75),
		"p90":              percentile(sortedValues, 90),
		"above_buy_count":  aboveBuy,
		"below_sell_count": belowSell,
	}
}

func percentile(sortedValues []float64, pct float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	if len(sortedValues) == 1 {
		return sortedValues[0]
	}
	rank := (pct / 100) * float64(len(sortedValues)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sortedValues[lo]
	}
	weight := rank - float64(lo)
	return sortedValues[lo]*(1-weight) + sortedValues[hi]*weight
}

func clamp(value float64, minValue float64, maxValue float64) float64 {
	return math.Max(minValue, math.Min(maxValue, value))
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
