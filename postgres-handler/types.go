package main

import "time"

type StockData struct {
	ID               int       `json:"id" gorm:"column:id;primaryKey"`
	Datetime         time.Time `json:"datetime" gorm:"column:datetime"`
	DateStr          string    `json:"date_str" gorm:"column:date_str"`
	Open             float64   `json:"open" gorm:"column:open"`
	Close            float64   `json:"close" gorm:"column:close"`
	High             float64   `json:"high" gorm:"column:high"`
	Low              float64   `json:"low" gorm:"column:low"`
	Volume           int64     `json:"volume" gorm:"column:volume"`
	Amount           float64   `json:"amount" gorm:"column:amount"`
	Amplitude        float64   `json:"amplitude" gorm:"column:amplitude"`
	PercentageChange float64   `json:"percentage_change" gorm:"column:percentage_change"`
	AmountChange     float64   `json:"amount_change" gorm:"column:amount_change"`
	TurnoverRate     float64   `json:"turnover_rate" gorm:"column:turnover_rate"`
	Type             int       `json:"type" gorm:"column:type"`
	Symbol           string    `json:"symbol" gorm:"column:symbol"`
	CreatedAt        time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (StockData) TableName() string { return "stock_data" }

type EtfDailyData struct {
	Code          string    `json:"code" gorm:"column:code;primaryKey"`
	TradingDate   time.Time `json:"trading_date" gorm:"column:trading_date;primaryKey"`
	Name          string    `json:"name" gorm:"column:name"`
	LatestPrice   float64   `json:"latest_price" gorm:"column:latest_price"`
	ChangeAmount  float64   `json:"change_amount" gorm:"column:change_amount"`
	ChangePercent float64   `json:"change_percent" gorm:"column:change_percent"`
	Buy           float64   `json:"buy" gorm:"column:buy"`
	Sell          float64   `json:"sell" gorm:"column:sell"`
	PrevClose     float64   `json:"prev_close" gorm:"column:prev_close"`
	Open          float64   `json:"open" gorm:"column:open"`
	High          float64   `json:"high" gorm:"column:high"`
	Low           float64   `json:"low" gorm:"column:low"`
	Volume        int64     `json:"volume" gorm:"column:volume"`
	Turnover      int64     `json:"turnover" gorm:"column:turnover"`
}

func (EtfDailyData) TableName() string { return "etf_daily" }

type IndexInfo struct {
	Code        string    `json:"code" gorm:"column:code;primaryKey"`
	DisplayName string    `json:"display_name" gorm:"column:display_name"`
	PublishDate time.Time `json:"publish_date" gorm:"column:publish_date"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at"`
}

func (IndexInfo) TableName() string { return "index_info" }

type IndexDailyData struct {
	Code          string    `json:"code" gorm:"column:code;primaryKey"`
	TradingDate   time.Time `json:"trading_date" gorm:"column:trading_date;primaryKey"`
	Open          float64   `json:"open" gorm:"column:open"`
	Close         float64   `json:"close" gorm:"column:close"`
	High          float64   `json:"high" gorm:"column:high"`
	Low           float64   `json:"low" gorm:"column:low"`
	Volume        int64     `json:"volume" gorm:"column:volume"`
	Amount        float64   `json:"amount" gorm:"column:amount"`
	ChangePercent float64   `json:"change_percent" gorm:"column:change_percent"`
}

func (IndexDailyData) TableName() string { return "index_daily" }

type MTFForecast struct {
	Symbol          string    `json:"symbol" gorm:"column:symbol"`
	Ds              time.Time `json:"ds" gorm:"column:ds"`
	Tsf             float64   `json:"tsf" gorm:"column:tsf"`
	Tsf01           float64   `json:"tsf_01" gorm:"column:tsf_01"`
	Tsf02           float64   `json:"tsf_02" gorm:"column:tsf_02"`
	Tsf03           float64   `json:"tsf_03" gorm:"column:tsf_03"`
	Tsf04           float64   `json:"tsf_04" gorm:"column:tsf_04"`
	Tsf05           float64   `json:"tsf_05" gorm:"column:tsf_05"`
	Tsf06           float64   `json:"tsf_06" gorm:"column:tsf_06"`
	Tsf07           float64   `json:"tsf_07" gorm:"column:tsf_07"`
	Tsf08           float64   `json:"tsf_08" gorm:"column:tsf_08"`
	Tsf09           float64   `json:"tsf_09" gorm:"column:tsf_09"`
	ChunkIndex      int       `json:"chunk_index" gorm:"column:chunk_index"`
	BestQuantile    string    `json:"best_quantile" gorm:"column:best_quantile"`
	BestQuantilePct string    `json:"best_quantile_pct" gorm:"column:best_quantile_pct"`
	BestPredPct     float64   `json:"best_pred_pct" gorm:"column:best_pred_pct"`
	ActualPct       float64   `json:"actual_pct" gorm:"column:actual_pct"`
	DiffPct         float64   `json:"diff_pct" gorm:"column:diff_pct"`
	MSE             float64   `json:"mse" gorm:"column:mse"`
	MAE             float64   `json:"mae" gorm:"column:mae"`
	CombinedScore   float64   `json:"combined_score" gorm:"column:combined_score"`
	UserID          int       `json:"user_id" gorm:"column:user_id"`
	Version         float64   `json:"version" gorm:"column:version"`
	HorizonLen      int       `json:"horizon_len" gorm:"column:horizon_len"`
}

func (MTFForecast) TableName() string { return "mtf_forecast" }

type StockCommentDaily struct {
	Code                     string    `json:"code" gorm:"column:code;primaryKey"`
	TradingDate              time.Time `json:"trading_date" gorm:"column:trading_date;primaryKey"`
	Name                     string    `json:"name" gorm:"column:name"`
	LatestPrice              float64   `json:"latest_price" gorm:"column:latest_price"`
	ChangePercent            float64   `json:"change_percent" gorm:"column:change_percent"`
	TurnoverRate             float64   `json:"turnover_rate" gorm:"column:turnover_rate"`
	PeRatio                  float64   `json:"pe_ratio" gorm:"column:pe_ratio"`
	MainCost                 float64   `json:"main_cost" gorm:"column:main_cost"`
	InstitutionParticipation float64   `json:"institution_participation" gorm:"column:institution_participation"`
	CompositeScore           float64   `json:"composite_score" gorm:"column:composite_score"`
	Rise                     int64     `json:"rise" gorm:"column:rise"`
	CurrentRank              int64     `json:"current_rank" gorm:"column:current_rank"`
	AttentionIndex           float64   `json:"attention_index" gorm:"column:attention_index"`
}

func (StockCommentDaily) TableName() string { return "a_stock_comment_daily" }

type ApiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type StockSymbolMeta struct {
	Symbol     string `json:"symbol"`
	StockType  int    `json:"stock_type"`
	LatestDate string `json:"latest_date"`
	Name       string `json:"name,omitempty"`
}

type StrategyParams struct {
	UniqueKey              string    `json:"unique_key" gorm:"column:unique_key;size:255"`
	UserID                 *int      `json:"user_id" gorm:"column:user_id"`
	BuyThresholdPct        float64   `json:"buy_threshold_pct" db:"buy_threshold_pct"`
	SellThresholdPct       float64   `json:"sell_threshold_pct" gorm:"column:sell_threshold_pct"`
	InitialCash            float64   `json:"initial_cash" gorm:"column:initial_cash"`
	EnableRebalance        bool      `json:"enable_rebalance" gorm:"column:enable_rebalance"`
	MaxPositionPct         float64   `json:"max_position_pct" gorm:"column:max_position_pct"`
	MinPositionPct         float64   `json:"min_position_pct" gorm:"column:min_position_pct"`
	SlopePositionPerPct    float64   `json:"slope_position_per_pct" gorm:"column:slope_position_per_pct"`
	RebalanceTolerancePct  float64   `json:"rebalance_tolerance_pct" gorm:"column:rebalance_tolerance_pct"`
	TradeFeeRate           float64   `json:"trade_fee_rate" gorm:"column:trade_fee_rate"`
	TakeProfitThresholdPct float64   `json:"take_profit_threshold_pct" gorm:"column:take_profit_threshold_pct"`
	TakeProfitSellFrac     float64   `json:"take_profit_sell_frac" gorm:"column:take_profit_sell_frac"`
	CreatedAt              time.Time `json:"created_at" gorm:"column:created_at"`
	UpdatedAt              time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (StrategyParams) TableName() string { return "mtf_strategy_params" }

type LlmTokenUsage struct {
	ID               int       `json:"id" gorm:"column:id;primaryKey"`
	UserID           int       `json:"user_id" gorm:"column:user_id"`
	Provider         string    `json:"provider" gorm:"column:provider"`
	Model            string    `json:"model" gorm:"column:model"`
	PromptTokens     int       `json:"prompt_tokens" gorm:"column:prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens" gorm:"column:completion_tokens"`
	TotalTokens      int       `json:"total_tokens" gorm:"column:total_tokens"`
	RequestTime      time.Time `json:"request_time" gorm:"column:request_time"`
	Metadata         *string   `json:"metadata,omitempty" gorm:"column:metadata;type:jsonb"`
	CreatedAt        time.Time `json:"created_at" gorm:"column:created_at"`
}

func (LlmTokenUsage) TableName() string { return "llm_token_usage" }
