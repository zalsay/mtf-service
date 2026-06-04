package models

import (
	"time"
)

type Stock struct {
	ID          int       `json:"id" db:"id"`
	Symbol      string    `json:"symbol" db:"symbol"`
	CompanyName string    `json:"company_name" db:"company_name"`
	Exchange    *string   `json:"exchange" db:"exchange"`
	Sector      *string   `json:"sector" db:"sector"`
	Industry    *string   `json:"industry" db:"industry"`
	MarketCap   *int64    `json:"market_cap" db:"market_cap"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type StockPrice struct {
	ID            int       `json:"id" db:"id"`
	StockID       int       `json:"stock_id" db:"stock_id"`
	Price         float64   `json:"price" db:"price"`
	ChangePercent *float64  `json:"change_percent" db:"change_percent"`
	Volume        *int64    `json:"volume" db:"volume"`
	MarketCap     *int64    `json:"market_cap" db:"market_cap"`
	RecordedAt    time.Time `json:"recorded_at" db:"recorded_at"`
}

type StockPrediction struct {
	ID             int       `json:"id" db:"id"`
	StockID        int       `json:"stock_id" db:"stock_id"`
	PredictedHigh  *float64  `json:"predicted_high" db:"predicted_high"`
	PredictedLow   *float64  `json:"predicted_low" db:"predicted_low"`
	Confidence     *float64  `json:"confidence" db:"confidence"`
	Sentiment      *string   `json:"sentiment" db:"sentiment"`
	Analysis       *string   `json:"analysis" db:"analysis"`
	PredictionDate time.Time `json:"prediction_date" db:"prediction_date"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
}

type UserWatchlist struct {
	ID                int       `json:"id" db:"id"`
	UserID            int       `json:"user_id" db:"user_id"`
	StockID           int       `json:"stock_id" db:"stock_id"`
	AddedAt           time.Time `json:"added_at" db:"added_at"`
	Notes             *string   `json:"notes" db:"notes"`
	StockType         *int      `json:"stock_type" db:"stock_type"`
	StrategyUniqueKey *string   `json:"strategy_unique_key" db:"strategy_unique_key"`
}

type UserPortfolio struct {
	ID           int       `json:"id" db:"id"`
	UserID       int       `json:"user_id" db:"user_id"`
	StockID      int       `json:"stock_id" db:"stock_id"`
	Shares       float64   `json:"shares" db:"shares"`
	AverageCost  float64   `json:"average_cost" db:"average_cost"`
	PurchaseDate time.Time `json:"purchase_date" db:"purchase_date"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// DTOs for API responses
type StockData struct {
	Symbol        string           `json:"symbol"`
	CompanyName   string           `json:"company_name"`
	CurrentPrice  float64          `json:"current_price"`
	ChangePercent float64          `json:"change_percent"`
	Prediction    *StockPrediction `json:"prediction,omitempty"`
}

type WatchlistItem struct {
	ID                int              `json:"id"`
	Stock             Stock            `json:"stock"`
	CurrentPrice      *StockPrice      `json:"current_price,omitempty"`
	Prediction        *StockPrediction `json:"prediction,omitempty"`
	AddedAt           time.Time        `json:"added_at"`
	Notes             *string          `json:"notes"`
	UniqueKey         string           `json:"unique_key,omitempty"`
	StockType         *int             `json:"stock_type"`
	StrategyUniqueKey string           `json:"strategy_unique_key,omitempty"`
	StrategyName      string           `json:"strategy_name,omitempty"`
	IsOverLimit       bool             `json:"is_over_limit"`
	WatchlistLimit    int              `json:"watchlist_limit"`
}

type AddToWatchlistRequest struct {
	Symbol    string  `json:"symbol" binding:"required"`
	StockType *int    `json:"stock_type"`
	Notes     *string `json:"notes"`
}

type UpdateWatchlistRequest struct {
	Notes *string `json:"notes"`
}

type BindStrategyRequest struct {
	Symbol            string `json:"symbol" binding:"required"`
	StrategyUniqueKey string `json:"strategy_unique_key" binding:"required"`
}

type PortfolioItem struct {
	ID              int         `json:"id"`
	Stock           Stock       `json:"stock"`
	Shares          float64     `json:"shares"`
	AverageCost     float64     `json:"average_cost"`
	CurrentPrice    *StockPrice `json:"current_price,omitempty"`
	TotalValue      float64     `json:"total_value"`
	GainLoss        float64     `json:"gain_loss"`
	GainLossPercent float64     `json:"gain_loss_percent"`
	PurchaseDate    time.Time   `json:"purchase_date"`
}

// MTF最佳分位预测保存的模型与请求体
type MTFBestPrediction struct {
	ID                 int       `json:"id" db:"id"`
	UniqueKey          string    `json:"unique_key" db:"unique_key"`
	Symbol             string    `json:"symbol" db:"symbol"`
	MTFVersion         string    `json:"mtf_version" db:"mtf_version"`
	BestPredictionItem string    `json:"best_prediction_item" db:"best_prediction_item"`
	BestMetrics        string    `json:"best_metrics" db:"best_metrics"` // JSON string
	PredictionType     string    `json:"prediction_type" db:"prediction_type"`
	CovariateConfig    string    `json:"-" db:"covariate_config"`
	CovariateSignature string    `json:"covariate_signature" db:"covariate_signature"`
	CovariateAnalysis  string    `json:"covariate_analysis" db:"covariate_analysis"`
	IsPublic           int       `json:"is_public" db:"is_public"`
	TrainStartDate     time.Time `json:"train_start_date" db:"train_start_date"`
	TrainEndDate       time.Time `json:"train_end_date" db:"train_end_date"`
	TestStartDate      time.Time `json:"test_start_date" db:"test_start_date"`
	TestEndDate        time.Time `json:"test_end_date" db:"test_end_date"`
	ValStartDate       time.Time `json:"val_start_date" db:"val_start_date"`
	ValEndDate         time.Time `json:"val_end_date" db:"val_end_date"`
	ContextLen         int       `json:"context_len" db:"context_len"`
	HorizonLen         int       `json:"horizon_len" db:"horizon_len"`
	StockType          int       `json:"stock_type" db:"stock_type"`
	ShortName          string    `json:"short_name" db:"short_name"`
	WatchlistCount     int       `json:"watchlist_count" db:"watchlist_count"`
	CreatedAt          time.Time `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time `json:"updated_at" db:"updated_at"`
}

type MTFBestUniqueKeysByConfig struct {
	Symbol           string `json:"symbol"`
	MTFVersion       string `json:"mtf_version"`
	HorizonLen       int    `json:"horizon_len"`
	ContextLen       int    `json:"context_len"`
	MTFLiteUniqueKey string `json:"mtf_lite_unique_key"`
	MTFProUniqueKey  string `json:"mtf_pro_unique_key"`
}

type SaveMTFBestRequest struct {
	UniqueKey          string                 `json:"unique_key" binding:"required"`
	Symbol             string                 `json:"symbol" binding:"required"`
	MTFVersion         string                 `json:"mtf_version" binding:"required"`
	BestPredictionItem string                 `json:"best_prediction_item" binding:"required"`
	BestMetrics        map[string]interface{} `json:"best_metrics" binding:"required"`
	PredictionType     string                 `json:"prediction_type"`
	IsPublic           *int                   `json:"is_public"`
	TrainStartDate     string                 `json:"train_start_date" binding:"required"`
	TrainEndDate       string                 `json:"train_end_date" binding:"required"`
	TestStartDate      string                 `json:"test_start_date" binding:"required"`
	TestEndDate        string                 `json:"test_end_date" binding:"required"`
	ValStartDate       string                 `json:"val_start_date" binding:"required"`
	ValEndDate         string                 `json:"val_end_date" binding:"required"`
	ContextLen         int                    `json:"context_len" binding:"required"`
	HorizonLen         int                    `json:"horizon_len" binding:"required"`
	CovariateConfig    map[string]interface{} `json:"covariate_config"`
	CovariateSignature string                 `json:"covariate_signature"`
	CovariateAnalysis  map[string]interface{} `json:"covariate_analysis"`
}

type SaveMTFValChunkRequest struct {
	UniqueKey          string                 `json:"unique_key" binding:"required"`
	ChunkIndex         int                    `json:"chunk_index" binding:"gte=0"`
	StartDate          string                 `json:"start_date" binding:"required"`
	EndDate            string                 `json:"end_date" binding:"required"`
	Symbol             string                 `json:"symbol"`
	UserID             *int                   `json:"user_id"`
	Predictions        map[string]interface{} `json:"predictions" binding:"required"` // best item vector or all columns
	Actual             []float64              `json:"actual_values" binding:"required"`
	Open               []float64              `json:"open_values"`
	PredictedChangePct map[string]interface{} `json:"predicted_change_percent"`
	ActualChangePct    []float64              `json:"actual_change_percent"`
	ChangeBaseValue    *float64               `json:"change_base_value"`
	ChangeBaseDate     *string                `json:"change_base_date"`
	Dates              []string               `json:"dates" binding:"required"`
	PredictionType     string                 `json:"prediction_type"`
	CovariateConfig    map[string]interface{} `json:"covariate_config"`
	CovariateSignature string                 `json:"covariate_signature"`
	CovariateAnalysis  map[string]interface{} `json:"covariate_analysis"`
}

// 保存 MTF 回测结果的请求模型
type SaveMTFBacktestRequest struct {
	UniqueKey        string `json:"unique_key" binding:"required"`
	Symbol           string `json:"symbol" binding:"required"`
	MTFVersion       string `json:"mtf_version" binding:"required"`
	ContextLen       int    `json:"context_len" binding:"required"`
	HorizonLen       int    `json:"horizon_len" binding:"required"`
	UserID           *int   `json:"user_id"`
	StrategyParamsID *int   `json:"strategy_params_id"`

	UsedQuantile         string  `json:"used_quantile"`
	BuyThresholdPct      float64 `json:"buy_threshold_pct"`
	SellThresholdPct     float64 `json:"sell_threshold_pct"`
	TradeFeeRate         float64 `json:"trade_fee_rate"`
	TotalFeesPaid        float64 `json:"total_fees_paid"`
	ActualTotalReturnPct float64 `json:"actual_total_return_pct"`

	BenchmarkReturnPct           float64 `json:"benchmark_return_pct"`
	BenchmarkAnnualizedReturnPct float64 `json:"benchmark_annualized_return_pct"`
	PeriodDays                   int     `json:"period_days"`

	ValidationStartDate                    string  `json:"validation_start_date"`
	ValidationEndDate                      string  `json:"validation_end_date"`
	ValidationBenchmarkReturnPct           float64 `json:"validation_benchmark_return_pct"`
	ValidationBenchmarkAnnualizedReturnPct float64 `json:"validation_benchmark_annualized_return_pct"`
	ValidationPeriodDays                   int     `json:"validation_period_days"`

	PositionControl      map[string]interface{}   `json:"position_control"`
	PredictedChangeStats map[string]interface{}   `json:"predicted_change_stats"`
	PerChunkSignals      []map[string]interface{} `json:"per_chunk_signals"`

	EquityCurveValues   []float64                `json:"equity_curve_values"`
	EquityCurvePct      []float64                `json:"equity_curve_pct"`
	EquityCurvePctGross []float64                `json:"equity_curve_pct_gross"`
	CurveDates          []string                 `json:"curve_dates"`
	ActualEndPrices     []float64                `json:"actual_end_prices"`
	Trades              []map[string]interface{} `json:"trades"`
	CovariateConfig     map[string]interface{}   `json:"covariate_config"`
	CovariateSignature  string                   `json:"covariate_signature"`
	CovariateAnalysis   map[string]interface{}   `json:"covariate_analysis"`
}

type MTFPredictRequest struct {
	StockCode          string                 `json:"stock_code" binding:"required"`
	StockType          interface{}            `json:"stock_type,omitempty"`
	TimeStep           *int                   `json:"time_step,omitempty"`
	Years              *int                   `json:"years,omitempty"`
	PredictionType     string                 `json:"prediction_type,omitempty"`
	HorizonLen         *int                   `json:"horizon_len,omitempty"`
	ContextLen         *int                   `json:"context_len,omitempty"`
	UserID             *int                   `json:"user_id,omitempty"`
	ForceEnqueue       *bool                  `json:"force_enqueue,omitempty"`
	ForceRequeue       *bool                  `json:"force_requeue,omitempty"`
	QueuePriority      string                 `json:"queue_priority,omitempty"`
	RefreshReason      string                 `json:"refresh_reason,omitempty"`
	CovariatePreset    *string                `json:"covariate_preset,omitempty"`
	CovariateSignature string                 `json:"covariate_signature,omitempty"`
	CovariateConfig    map[string]interface{} `json:"covariate_config,omitempty"`
	Covariates         map[string]interface{} `json:"covariates,omitempty"`
}

type MTFJobStatusResponse struct {
	JobID string `json:"job_id"`
}

type MTFBestTrainRequest struct {
	StockCode      string      `json:"stock_code" binding:"required"`
	StockType      interface{} `json:"stock_type,omitempty"`
	PredictionType string      `json:"prediction_type,omitempty"`
	Years          *int        `json:"years,omitempty"`
	HorizonLen     int         `json:"horizon_len" binding:"required"`
	ContextLen     int         `json:"context_len" binding:"required"`
}

type MTFBacktestRequest struct {
	UniqueKey              string   `json:"unique_key,omitempty"`
	Symbol                 string   `json:"symbol,omitempty"`
	StockType              *string  `json:"stock_type,omitempty"`
	Years                  *int     `json:"years,omitempty"`
	HorizonLen             *int     `json:"horizon_len,omitempty"`
	ContextLen             *int     `json:"context_len,omitempty"`
	TimeStep               *int     `json:"time_step,omitempty"`
	StartDate              *string  `json:"start_date,omitempty"`
	EndDate                *string  `json:"end_date,omitempty"`
	MTFVersion             *string  `json:"mtf_version,omitempty"`
	UserID                 *int     `json:"user_id,omitempty"`
	StrategyParamsID       *int     `json:"strategy_params_id,omitempty"`
	BuyThresholdPct        *float64 `json:"buy_threshold_pct,omitempty"`
	SellThresholdPct       *float64 `json:"sell_threshold_pct,omitempty"`
	InitialCash            *float64 `json:"initial_cash,omitempty"`
	EnableRebalance        *bool    `json:"enable_rebalance,omitempty"`
	MaxPositionPct         *float64 `json:"max_position_pct,omitempty"`
	MinPositionPct         *float64 `json:"min_position_pct,omitempty"`
	SlopePositionPerPct    *float64 `json:"slope_position_per_pct,omitempty"`
	RebalanceTolerancePct  *float64 `json:"rebalance_tolerance_pct,omitempty"`
	TradeFeeRate           *float64 `json:"trade_fee_rate,omitempty"`
	TakeProfitThresholdPct *float64 `json:"take_profit_threshold_pct,omitempty"`
	TakeProfitSellFrac     *float64 `json:"take_profit_sell_frac,omitempty"`
}

type BatchSymbolsRequest struct {
	Symbols []string `json:"symbols" binding:"required"`
}

type LatestQuote struct {
	Symbol        string   `json:"symbol"`
	LatestPrice   *float64 `json:"latest_price"`
	ChangePercent *float64 `json:"change_percent"`
	TradingDate   *string  `json:"trading_date"`
	TurnoverRate  *float64 `json:"turnover_rate"`
}

type StrategyParams struct {
	ID                     int     `json:"id" db:"id"`
	UniqueKey              string  `json:"unique_key" db:"unique_key"`
	UserID                 *int    `json:"user_id" db:"user_id"`
	Name                   *string `json:"name" db:"name"`
	IsPublic               int     `json:"is_public" db:"is_public"`
	BuyThresholdPct        float64 `json:"buy_threshold_pct" db:"buy_threshold_pct"`
	SellThresholdPct       float64 `json:"sell_threshold_pct" db:"sell_threshold_pct"`
	InitialCash            float64 `json:"initial_cash" db:"initial_cash"`
	EnableRebalance        bool    `json:"enable_rebalance" db:"enable_rebalance"`
	MaxPositionPct         float64 `json:"max_position_pct" db:"max_position_pct"`
	MinPositionPct         float64 `json:"min_position_pct" db:"min_position_pct"`
	SlopePositionPerPct    float64 `json:"slope_position_per_pct" db:"slope_position_per_pct"`
	RebalanceTolerancePct  float64 `json:"rebalance_tolerance_pct" db:"rebalance_tolerance_pct"`
	TradeFeeRate           float64 `json:"trade_fee_rate" db:"trade_fee_rate"`
	TakeProfitThresholdPct float64 `json:"take_profit_threshold_pct" db:"take_profit_threshold_pct"`
	TakeProfitSellFrac     float64 `json:"take_profit_sell_frac" db:"take_profit_sell_frac"`
}

type SaveStrategyParamsRequest struct {
	UniqueKey              string  `json:"unique_key" binding:"required"`
	UserID                 *int    `json:"user_id"`
	Name                   *string `json:"name"`
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
