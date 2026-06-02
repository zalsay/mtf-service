-- 优化官方推荐策略参数：保留 unique_key，避免影响已绑定策略。
ALTER TABLE timesfm_strategy_params ADD COLUMN IF NOT EXISTS is_public SMALLINT NOT NULL DEFAULT 0;

INSERT INTO timesfm_strategy_params (
    unique_key, name, is_public, user_id,
    buy_threshold_pct, sell_threshold_pct, initial_cash,
    enable_rebalance, max_position_pct, min_position_pct,
    slope_position_per_pct, rebalance_tolerance_pct,
    trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
) VALUES
(
    'strategy_conservative', '稳健防守', 1, NULL,
    2.4, -0.8, 10000,
    true, 0.55, 0.10,
    0.08, 0.08,
    0.006, 8.0, 0.40
),
(
    'strategy_balanced', '均衡轮动', 1, NULL,
    1.5, -1.2, 10000,
    true, 0.75, 0.15,
    0.12, 0.06,
    0.006, 12.0, 0.50
),
(
    'strategy_aggressive', '趋势进取', 1, NULL,
    0.8, -1.8, 10000,
    true, 0.95, 0.20,
    0.18, 0.04,
    0.006, 18.0, 0.35
)
ON CONFLICT (unique_key) DO UPDATE SET
    name = EXCLUDED.name,
    is_public = 1,
    user_id = NULL,
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
    updated_at = CURRENT_TIMESTAMP;
