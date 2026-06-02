package services

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fintrack-api/config"
	"fintrack-api/models"
)

func TestRunTimesfmBacktestOnChunksTradesAndCurves(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "601766_best_hlen_7_clen_2048_v_2.5",
		Symbol:             "601766",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "tsf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
		CovariateConfig:    `{"enabled":true}`,
		CovariateSignature: "sig-1",
		CovariateAnalysis:  `{"ok":true}`,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-08",
			Predictions: map[string]interface{}{
				"tsf-0.5": []interface{}{102.0, 106.0, 110.0},
			},
			Actual: []float64{100, 105, 110},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-08"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-08",
			EndDate:    "2026-01-15",
			Predictions: map[string]interface{}{
				"tsf-0.5": []interface{}{108.0, 106.0, 104.0},
			},
			Actual: []float64{110, 108, 105},
			Dates:  []string{"2026-01-08", "2026-01-09", "2026-01-15"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        5,
		SellThresholdPct:       -1,
		InitialCash:            10000,
		EnableRebalance:        false,
		MaxPositionPct:         1,
		MinPositionPct:         0,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 100,
		TakeProfitSellFrac:     0.5,
	}

	saveReq, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	assertFloatNear(t, "total_return_pct", result["total_return_pct"].(float64), 10)
	assertFloatNear(t, "actual_total_return_pct", result["actual_total_return_pct"].(float64), 5)
	if got := len(result["trades"].([]map[string]interface{})); got != 2 {
		t.Fatalf("trades length = %d, want 2", got)
	}
	curve := result["equity_curve_values"].([]float64)
	if len(curve) != 2 {
		t.Fatalf("equity curve length = %d, want 2", len(curve))
	}
	assertFloatNear(t, "first equity point", curve[0], 11000)
	assertFloatNear(t, "second equity point", curve[1], 11000)
	if saveReq.UsedQuantile != "tsf-0.5" {
		t.Fatalf("saveReq.UsedQuantile = %q, want tsf-0.5", saveReq.UsedQuantile)
	}
	if saveReq.CovariateSignature != "sig-1" {
		t.Fatalf("saveReq.CovariateSignature = %q, want sig-1", saveReq.CovariateSignature)
	}
	if saveReq.ValidationStartDate != "2026-01-01" || saveReq.ValidationEndDate != "2026-01-15" {
		t.Fatalf("validation dates = %s..%s, want 2026-01-01..2026-01-15", saveReq.ValidationStartDate, saveReq.ValidationEndDate)
	}
}

func TestRunTimesfmBacktestOnChunksIgnoresChunksWithoutBestQuantileForMetrics(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600246_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600246",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.7",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-08",
			Predictions: map[string]interface{}{
				"mtf-0.7": []interface{}{102.0, 106.0, 110.0},
			},
			Actual: []float64{100, 105, 110},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-08"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-08",
			EndDate:    "2026-01-15",
			Predictions: map[string]interface{}{
				"mtf-0.7": []interface{}{108.0, 106.0, 104.0},
			},
			Actual: []float64{110, 108, 105},
			Dates:  []string{"2026-01-08", "2026-01-09", "2026-01-15"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 2,
			StartDate:  "2026-01-16",
			EndDate:    "2026-01-22",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{1000.0, 1005.0, 1010.0},
			},
			Actual: []float64{1000, 1005, 1010},
			Dates:  []string{"2026-01-16", "2026-01-17", "2026-01-22"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        5,
		SellThresholdPct:       -1,
		InitialCash:            10000,
		EnableRebalance:        false,
		MaxPositionPct:         1,
		MinPositionPct:         0,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 100,
		TakeProfitSellFrac:     0.5,
	}

	saveReq, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	assertFloatNear(t, "actual_total_return_pct", result["actual_total_return_pct"].(float64), 5)
	assertFloatNear(t, "benchmark_return_pct", result["benchmark_return_pct"].(float64), 5)
	if got := result["validation_end_date"]; got != "2026-01-15" {
		t.Fatalf("validation_end_date = %v, want 2026-01-15", got)
	}
	if got := result["context_len"]; got != 2048 {
		t.Fatalf("context_len = %v, want 2048", got)
	}
	if got := result["horizon_len"]; got != 7 {
		t.Fatalf("horizon_len = %v, want 7", got)
	}
	if saveReq.ValidationEndDate != "2026-01-15" {
		t.Fatalf("saveReq.ValidationEndDate = %s, want 2026-01-15", saveReq.ValidationEndDate)
	}
}

func TestNormalizeBacktestBuySizeUsesShareLots(t *testing.T) {
	tests := []struct {
		name          string
		requested     float64
		maxAffordable float64
		want          float64
	}{
		{name: "below minimum but affordable", requested: 99.99, maxAffordable: 100, want: 100},
		{name: "below minimum and unaffordable", requested: 99.99, maxAffordable: 99.99, want: 0},
		{name: "minimum", requested: 100, maxAffordable: 100, want: 100},
		{name: "floors fractional lot", requested: 260.4, maxAffordable: 300, want: 200},
		{name: "keeps whole lots", requested: 300, maxAffordable: 300, want: 300},
		{name: "caps by affordable lot", requested: 300, maxAffordable: 250, want: 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFloatNear(t, "buy size", normalizeBacktestBuySize(tt.requested, tt.maxAffordable), tt.want)
		})
	}
}

func TestNormalizeBacktestSellSizeKeepsRemainingPositionInShareLots(t *testing.T) {
	got := normalizeBacktestSellSize(300, 120, 10, 3000, 0)
	assertFloatNear(t, "sell size", got, 100)

	got = normalizeBacktestSellSize(150, 60, 10, 1500, 0)
	assertFloatNear(t, "sell size clearing old odd lot remainder", got, 150)
}

func TestChunkTradeExecutionPointKeepsSellDateWhenHighIsFirstPoint(t *testing.T) {
	chunk := models.SaveTimesfmValChunkRequest{
		StartDate: "2026-01-01",
		EndDate:   "2026-01-03",
		Actual:    []float64{15, 12, 11},
		Dates:     []string{"2026-01-01", "2026-01-02", "2026-01-03"},
	}

	point := chunkTradeExecutionPoint(chunk, "sell", 15)

	if point.Date != "2026-01-01" {
		t.Fatalf("sell date = %s, want 2026-01-01", point.Date)
	}
	assertFloatNear(t, "sell price", point.Price, 15)
}

func TestRunTimesfmBacktestOnChunksClearsSmallPositionOnSellSignal(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600186_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600186",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-08",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{102.0, 106.0, 110.0},
			},
			Actual: []float64{100, 110, 120},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-08"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-09",
			EndDate:    "2026-01-16",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{120.0, 120.5, 121.0},
			},
			Actual: []float64{120, 120, 120},
			Dates:  []string{"2026-01-09", "2026-01-10", "2026-01-16"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 2,
			StartDate:  "2026-01-17",
			EndDate:    "2026-01-24",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{30.0, 25.0, 20.0},
			},
			Actual: []float64{30, 30, 30},
			Dates:  []string{"2026-01-17", "2026-01-18", "2026-01-24"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        1,
		SellThresholdPct:       -1,
		InitialCash:            150000,
		EnableRebalance:        true,
		MaxPositionPct:         0.2,
		MinPositionPct:         0.2,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.08,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 100,
		TakeProfitSellFrac:     0.4,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 2 {
		t.Fatalf("trades length = %d, want 2; trades=%v", got, trades)
	}
	lastTrade := trades[1]
	if lastTrade["action"] != "sell" || lastTrade["reason"] != "rebalance_down-> 0.00" {
		t.Fatalf("last trade = %v, want sell rebalance_down-> 0.00", lastTrade)
	}
	assertFloatNear(t, "last sell size", lastTrade["size"].(float64), 300)
}

func TestRunTimesfmBacktestOnChunksTakeProfitClearsBelowMinimumLot(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600186_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600186",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-08",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{102.0, 106.0, 110.0},
			},
			Actual: []float64{100, 110, 120},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-08"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-09",
			EndDate:    "2026-01-16",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{120.0, 120.2, 120.5},
			},
			Actual: []float64{120, 120, 120},
			Dates:  []string{"2026-01-09", "2026-01-10", "2026-01-16"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        1,
		SellThresholdPct:       -1,
		InitialCash:            10000,
		EnableRebalance:        true,
		MaxPositionPct:         1,
		MinPositionPct:         0.01,
		SlopePositionPerPct:    1,
		RebalanceTolerancePct:  0.08,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 1,
		TakeProfitSellFrac:     0.4,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 2 {
		t.Fatalf("trades length = %d, want 2; trades=%v", got, trades)
	}
	takeProfitTrade := trades[1]
	if takeProfitTrade["action"] != "sell" || takeProfitTrade["reason"] != "take_profit>= 1.00" {
		t.Fatalf("take profit trade = %v, want sell take_profit>= 1.00", takeProfitTrade)
	}
	assertFloatNear(t, "take profit sell size", takeProfitTrade["size"].(float64), 100)
}

func TestRunTimesfmBacktestOnChunksTakeProfitSellUsesMinimumLot(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600186_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600186",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-08",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{102.0, 106.0, 110.0},
			},
			Actual: []float64{100, 110, 120},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-08"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-09",
			EndDate:    "2026-01-16",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{120.0, 120.2, 120.5},
			},
			Actual: []float64{120, 120, 120},
			Dates:  []string{"2026-01-09", "2026-01-10", "2026-01-16"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        1,
		SellThresholdPct:       -1,
		InitialCash:            30000,
		EnableRebalance:        true,
		MaxPositionPct:         1,
		MinPositionPct:         0.01,
		SlopePositionPerPct:    1,
		RebalanceTolerancePct:  0.08,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 1,
		TakeProfitSellFrac:     0.1,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 2 {
		t.Fatalf("trades length = %d, want 2; trades=%v", got, trades)
	}
	assertFloatNear(t, "take profit sell size", trades[1]["size"].(float64), 100)
}

func TestRunTimesfmBacktestOnChunksAvoidsDuplicateSellWhenRebalanceClearsPosition(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600186_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600186",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-08",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{100.0, 110.0},
			},
			Actual: []float64{100, 120},
			Dates:  []string{"2026-01-01", "2026-01-08"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-09",
			EndDate:    "2026-01-16",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{120.0, 100.0},
			},
			Actual: []float64{120, 120},
			Dates:  []string{"2026-01-09", "2026-01-16"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        1,
		SellThresholdPct:       -1,
		InitialCash:            20000,
		EnableRebalance:        true,
		MaxPositionPct:         1,
		MinPositionPct:         1,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.08,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 1,
		TakeProfitSellFrac:     0.4,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 2 {
		t.Fatalf("trades length = %d, want 2; trades=%v", got, trades)
	}
	lastTrade := trades[1]
	if lastTrade["action"] != "sell" || lastTrade["reason"] != "rebalance_down-> 0.00" {
		t.Fatalf("last trade = %v, want single rebalance sell", lastTrade)
	}
	assertFloatNear(t, "single clear sell size", lastTrade["size"].(float64), 200)
}

func TestRunTimesfmBacktestOnChunksIgnoresStaleChunksAfterCurrentValidationWindow(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "300442_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "300442",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.6",
		ContextLen:         2048,
		HorizonLen:         7,
		ValStartDate:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ValEndDate:         time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-02",
			Predictions: map[string]interface{}{
				"mtf-0.6": []interface{}{10.0, 11.0},
			},
			Actual: []float64{10, 8},
			Dates:  []string{"2026-01-01", "2026-01-02"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-03",
			EndDate:    "2026-01-04",
			Predictions: map[string]interface{}{
				"mtf-0.6": []interface{}{8.0, 7.0},
			},
			Actual: []float64{8, 12},
			Dates:  []string{"2026-01-03", "2026-01-04"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 2,
			StartDate:  "2025-01-01",
			EndDate:    "2025-01-02",
			Predictions: map[string]interface{}{
				"mtf-0.6": []interface{}{6.0, 7.0},
			},
			Actual: []float64{6, 5},
			Dates:  []string{"2025-01-01", "2025-01-02"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 3,
			StartDate:  "2026-01-02",
			EndDate:    "2026-01-03",
			Predictions: map[string]interface{}{
				"mtf-0.6": []interface{}{8.0, 9.0},
			},
			Actual: []float64{7, 9},
			Dates:  []string{"2026-01-02", "2026-01-03"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        5,
		SellThresholdPct:       -1,
		InitialCash:            10000,
		EnableRebalance:        false,
		MaxPositionPct:         1,
		MinPositionPct:         0,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 100,
		TakeProfitSellFrac:     0.5,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 2 {
		t.Fatalf("trades length = %d, want 2; trades=%v", got, trades)
	}
	if trades[0]["chunk_index"] != 0 || trades[1]["chunk_index"] != 1 {
		t.Fatalf("trade chunks = %v, want only current validation chunks 0 and 1", trades)
	}
}

func TestRunTimesfmBacktestOnChunksUsesChunkLowForBuyAndHighForSell(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600186_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600186",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-03",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{10.0, 10.5, 11.0},
			},
			Actual: []float64{10, 8, 12},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-03"},
		},
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 1,
			StartDate:  "2026-01-04",
			EndDate:    "2026-01-06",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{12.0, 10.0, 9.0},
			},
			Actual: []float64{12, 15, 11},
			Dates:  []string{"2026-01-04", "2026-01-05", "2026-01-06"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        5,
		SellThresholdPct:       -1,
		InitialCash:            10000,
		EnableRebalance:        false,
		MaxPositionPct:         1,
		MinPositionPct:         0,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 100,
		TakeProfitSellFrac:     0.5,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 2 {
		t.Fatalf("trades length = %d, want 2; trades=%v", got, trades)
	}
	buyTrade := trades[0]
	if buyTrade["action"] != "buy" || buyTrade["date"] != "2026-01-02" {
		t.Fatalf("buy trade = %v, want buy on 2026-01-02", buyTrade)
	}
	assertFloatNear(t, "buy price", buyTrade["price"].(float64), 8)
	sellTrade := trades[1]
	if sellTrade["action"] != "sell" || sellTrade["date"] != "2026-01-05" {
		t.Fatalf("sell trade = %v, want sell on 2026-01-05", sellTrade)
	}
	assertFloatNear(t, "sell price", sellTrade["price"].(float64), 15)
}

func TestRunTimesfmBacktestOnChunksUsesOpenPriceOnLowestCloseDateForBuy(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "600186_best_hlen_7_clen_2048_v_2.5_cov",
		Symbol:             "600186",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "mtf-0.5",
		ContextLen:         2048,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:  best.UniqueKey,
			ChunkIndex: 0,
			StartDate:  "2026-01-01",
			EndDate:    "2026-01-03",
			Predictions: map[string]interface{}{
				"mtf-0.5": []interface{}{10.0, 10.5, 11.0},
			},
			Actual: []float64{10, 8, 12},
			Open:   []float64{9.8, 7.6, 11.5},
			Dates:  []string{"2026-01-01", "2026-01-02", "2026-01-03"},
		},
	}
	params := timesfmBacktestParams{
		BuyThresholdPct:        5,
		SellThresholdPct:       -1,
		InitialCash:            10000,
		EnableRebalance:        false,
		MaxPositionPct:         1,
		MinPositionPct:         0,
		SlopePositionPerPct:    0,
		RebalanceTolerancePct:  0.05,
		TradeFeeRate:           0,
		TakeProfitThresholdPct: 100,
		TakeProfitSellFrac:     0.5,
	}

	_, result, err := runTimesfmBacktestOnChunks(best, chunks, params)
	if err != nil {
		t.Fatalf("runTimesfmBacktestOnChunks returned error: %v", err)
	}

	trades := result["trades"].([]map[string]interface{})
	if got := len(trades); got != 1 {
		t.Fatalf("trades length = %d, want 1; trades=%v", got, trades)
	}
	buyTrade := trades[0]
	if buyTrade["date"] != "2026-01-02" {
		t.Fatalf("buy date = %v, want 2026-01-02", buyTrade["date"])
	}
	assertFloatNear(t, "buy price", buyTrade["price"].(float64), 7.6)
}

func TestFetchPostgresHandlerOpenPricesByDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stock-data/600186/range" {
			t.Fatalf("path = %s, want /api/v1/stock-data/600186/range", r.URL.Path)
		}
		if r.Header.Get("X-Token") != "test-token" {
			t.Fatalf("X-Token = %q, want test-token", r.Header.Get("X-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"message": "Success",
			"data": [
				{"date_str": "2026-01-01", "open": 9.8},
				{"date_str": "2026-01-02", "open": 7.6}
			]
		}`))
	}))
	defer server.Close()

	service := NewWatchlistService(nil, &config.Config{
		PostgresHandler: config.PostgresHandlerConfig{
			BaseURL:  server.URL,
			APIToken: "test-token",
			Timeout:  2,
		},
	})

	opens, err := service.fetchPostgresHandlerOpenPricesByDate("600186", 1, "2026-01-01", "2026-01-02")
	if err != nil {
		t.Fatalf("fetchPostgresHandlerOpenPricesByDate returned error: %v", err)
	}
	if strings.Join(mapOpenDates(opens), ",") != "2026-01-01,2026-01-02" {
		t.Fatalf("open dates = %v, want ordered map keys for both dates", mapOpenDates(opens))
	}
	assertFloatNear(t, "2026-01-01 open", opens["2026-01-01"], 9.8)
	assertFloatNear(t, "2026-01-02 open", opens["2026-01-02"], 7.6)
}

func TestRunTimesfmBacktestOnChunksRejectsMissingQuantile(t *testing.T) {
	best := &models.TimesfmBestPrediction{
		UniqueKey:          "000001_best_hlen_7_clen_512_v_2.5",
		Symbol:             "000001",
		TimesfmVersion:     "2.5",
		BestPredictionItem: "tsf-0.5",
		ContextLen:         512,
		HorizonLen:         7,
	}
	chunks := []models.SaveTimesfmValChunkRequest{
		{
			UniqueKey:   best.UniqueKey,
			ChunkIndex:  0,
			StartDate:   "2026-01-01",
			EndDate:     "2026-01-08",
			Predictions: map[string]interface{}{"tsf-0.6": []interface{}{100.0, 101.0}},
			Actual:      []float64{100, 101},
			Dates:       []string{"2026-01-01", "2026-01-08"},
		},
	}

	_, _, err := runTimesfmBacktestOnChunks(best, chunks, buildTimesfmBacktestParams(&models.TimesfmBacktestRequest{}))
	if err == nil {
		t.Fatal("expected missing quantile error, got nil")
	}
}

func assertFloatNear(t *testing.T, name string, actual float64, expected float64) {
	t.Helper()
	if math.Abs(actual-expected) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, actual, expected)
	}
}

func mapOpenDates(values map[string]float64) []string {
	out := make([]string, 0, len(values))
	for _, date := range []string{"2026-01-01", "2026-01-02"} {
		if _, ok := values[date]; ok {
			out = append(out, date)
		}
	}
	return out
}
