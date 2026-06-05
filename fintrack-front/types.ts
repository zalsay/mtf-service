
export type View = 'mtfAgent' | 'dashboard' | 'watchlist' | 'pricing' | 'portfolio' | 'news' | 'settings' | 'admin';

export interface PredictionChartData {
  dates: string[];
  actuals: number[];
  predictions: number[];
  proPredictions?: number[];
  actualChangePercents?: number[];
  predictedChangePercents?: number[];
  proPredictedChangePercents?: number[];
}

export interface StockPrediction {
  predicted_high: number;
  predicted_low: number;
  confidence: number;
  sentiment: 'Bullish' | 'Bearish' | 'Neutral';
  analysis: string;
  modelName?: string;
  contextLen?: number;
  horizonLen?: number;
  chartData?: PredictionChartData;
  latestChartData?: PredictionChartData;
  nextChunkChartData?: PredictionChartData;
  maxDeviationPercent?: number;
  proModelName?: string;
  proContextLen?: number;
  proHorizonLen?: number;
  proConfidence?: number;
  proMaxDeviationPercent?: number;
  proPredictedChangePercent?: number;
}

export interface StockData {
  symbol: string;
  companyName: string;
  stockType?: 1 | 2;
  watchlistCount?: number;
  isWatchlisted?: boolean;
  currentPrice: number;
  changePercent: number;
  predictedChangePercent?: number;
  futurePredictedChangePercent?: number;
  prediction?: StockPrediction;
}

export interface MTFBest {
  unique_key: string;
  symbol: string;
  mtf_version: string;
  best_prediction_item: string;
  best_metrics: string | Record<string, unknown>;
  prediction_type?: string;
  mtf_lite_unique_key?: string;
  mtf_pro_unique_key?: string;
  covariate_signature?: string;
  covariate_config?: string | Record<string, unknown> | null;
  covariate_analysis?: string | Record<string, unknown> | null;
  is_public: number;
  short_name?: string;
  watchlist_count?: number;
  context_len?: number;
  horizon_len?: number;
  updated_at?: string;
}

export interface MTFChunk {
  unique_key: string;
  chunk_index: number;
  start_date: string;
  end_date: string;
  symbol: string;
  predictions: Record<string, number[]>;
  actual_values: number[];
  predicted_change_percent?: Record<string, number[]>;
  actual_change_percent?: number[];
  change_base_value?: number | null;
  change_base_date?: string | null;
  dates: string[];
  prediction_type?: string;
}

export interface PublicPredictionItem {
  best: MTFBest;
  chunks: MTFChunk[];
  max_deviation_percent?: number;
}

export interface PublicPredictionResponse {
  items: PublicPredictionItem[];
  count: number;
}

export interface StrategyParams {
  unique_key: string;
  name?: string;
  is_public?: number;
  buy_threshold_pct: number;
  sell_threshold_pct: number;
  initial_cash: number;
  enable_rebalance: boolean;
  max_position_pct: number;
  min_position_pct: number;
  slope_position_per_pct: number;
  rebalance_tolerance_pct: number;
  trade_fee_rate: number;
  take_profit_threshold_pct: number;
  take_profit_sell_frac: number;
  user_id?: number;
}

export interface SaveStrategyParamsRequest extends StrategyParams {}
