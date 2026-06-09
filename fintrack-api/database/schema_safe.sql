-- 手动数据库初始化脚本
-- 注意：这个脚本不包含 DROP TABLE 语句，不会清空现有数据

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    username VARCHAR(50) NOT NULL UNIQUE,
    first_name VARCHAR(50),
    last_name VARCHAR(50),
    is_active BOOLEAN DEFAULT TRUE,
    is_premium BOOLEAN DEFAULT FALSE,
    is_admin BOOLEAN DEFAULT FALSE,
    membership_level INT DEFAULT 0,
    membership_expires_at TIMESTAMP WITH TIME ZONE,
    daily_stock_analysis_user_id VARCHAR(128),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_sessions (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS membership_invite_codes (
    id SERIAL PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE,
    membership_level INT NOT NULL,
    duration_days INT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    used_count INT NOT NULL DEFAULT 0,
    max_uses INT NOT NULL DEFAULT 50,
    note TEXT,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (membership_level >= 1 AND membership_level <= 3),
    CHECK (duration_days > 0),
    CHECK (max_uses > 0)
);

CREATE TABLE IF NOT EXISTS user_ai_model_configs (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    provider_name VARCHAR(80) NOT NULL DEFAULT 'DeepSeek',
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    model_id VARCHAR(160) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mtf_agent_sessions (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    deepseek_thread_id VARCHAR(128) NOT NULL,
    model_id VARCHAR(160) NOT NULL,
    last_used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mtf_agent_memories (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    memory_type VARCHAR(80) NOT NULL,
    content TEXT NOT NULL,
    source VARCHAR(80) NOT NULL DEFAULT 'explicit',
    confidence DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (char_length(trim(content)) > 0),
    CHECK (confidence >= 0 AND confidence <= 1)
);

CREATE TABLE IF NOT EXISTS mtf_agent_messages (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id VARCHAR(128) NOT NULL,
    role VARCHAR(24) NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CHECK (role IN ('user', 'assistant', 'system')),
    CHECK (char_length(trim(content)) > 0)
);

CREATE TABLE IF NOT EXISTS stocks (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(20) NOT NULL UNIQUE,
    company_name VARCHAR(255),
    exchange VARCHAR(50),
    sector VARCHAR(100),
    industry VARCHAR(100),
    market_cap BIGINT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS stock_prices (
    id SERIAL PRIMARY KEY,
    stock_id INTEGER NOT NULL REFERENCES stocks(id) ON DELETE CASCADE,
    price DECIMAL(18, 4) NOT NULL,
    change_percent DECIMAL(10, 4),
    volume BIGINT,
    market_cap BIGINT,
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_watchlist (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    symbol VARCHAR(20) NOT NULL,
    stock_type SMALLINT NOT NULL DEFAULT 1,
    added_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    notes TEXT,
    strategy_unique_key VARCHAR(255),
    UNIQUE(user_id, symbol)
);

CREATE TABLE IF NOT EXISTS mtf_strategy_params (
    id SERIAL PRIMARY KEY,
    unique_key VARCHAR(255) NOT NULL UNIQUE,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    name VARCHAR(255),
    is_public SMALLINT NOT NULL DEFAULT 1,
    buy_threshold_pct DOUBLE PRECISION,
    sell_threshold_pct DOUBLE PRECISION,
    initial_cash DOUBLE PRECISION,
    enable_rebalance BOOLEAN,
    max_position_pct DOUBLE PRECISION,
    min_position_pct DOUBLE PRECISION,
    slope_position_per_pct DOUBLE PRECISION,
    rebalance_tolerance_pct DOUBLE PRECISION,
    trade_fee_rate DOUBLE PRECISION,
    take_profit_threshold_pct DOUBLE PRECISION,
    take_profit_sell_frac DOUBLE PRECISION,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mtf_best_predictions (
    id SERIAL PRIMARY KEY,
    unique_key VARCHAR(255) NOT NULL UNIQUE,
    symbol VARCHAR(20) NOT NULL,
    mtf_version VARCHAR(20) NOT NULL,
    best_prediction_item VARCHAR(50) NOT NULL,
    best_prediction_quantile DOUBLE PRECISION,
    best_metrics JSONB NOT NULL,
    prediction_type TEXT NOT NULL DEFAULT 'mtf-lite',
    covariate_config JSONB,
    covariate_signature TEXT,
    covariate_analysis JSONB,
    is_public SMALLINT NOT NULL DEFAULT 1,
    train_start_date DATE NOT NULL,
    train_end_date DATE NOT NULL,
    test_start_date DATE NOT NULL,
    test_end_date DATE NOT NULL,
    val_start_date DATE NOT NULL,
    val_end_date DATE NOT NULL,
    context_len INTEGER NOT NULL,
    horizon_len INTEGER NOT NULL,
    best_prediction_values JSONB,
    future_dates JSONB,
    adjust_raw_best_prediction_values JSONB,
    short_name VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mtf_best_validation_chunks (
    id SERIAL PRIMARY KEY,
    unique_key VARCHAR(255) NOT NULL REFERENCES mtf_best_predictions(unique_key) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    user_id INTEGER,
    symbol VARCHAR(20),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    predictions JSONB NOT NULL,
    actual_values JSONB NOT NULL,
    predicted_change_percent JSONB NOT NULL DEFAULT '{}'::jsonb,
    actual_change_percent JSONB NOT NULL DEFAULT '[]'::jsonb,
    change_base_value DOUBLE PRECISION,
    change_base_date DATE,
    dates JSONB NOT NULL,
    prediction_type TEXT NOT NULL DEFAULT 'mtf-lite',
    covariate_config JSONB,
    covariate_signature TEXT,
    covariate_analysis JSONB,
    stock_type SMALLINT NOT NULL DEFAULT 1,
    adjust_raw_chunks JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(unique_key, chunk_index)
);

CREATE TABLE IF NOT EXISTS mtf_backtests (
    id SERIAL PRIMARY KEY,
    unique_key VARCHAR(255) NOT NULL UNIQUE,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    strategy_params_id INTEGER REFERENCES mtf_strategy_params(id) ON DELETE SET NULL,
    symbol VARCHAR(20) NOT NULL,
    mtf_version VARCHAR(20) NOT NULL,
    context_len INTEGER NOT NULL,
    horizon_len INTEGER NOT NULL,
    covariate_config JSONB,
    covariate_signature TEXT,
    covariate_analysis JSONB,
    used_quantile VARCHAR(50),
    buy_threshold_pct DOUBLE PRECISION,
    sell_threshold_pct DOUBLE PRECISION,
    trade_fee_rate DOUBLE PRECISION,
    total_fees_paid DOUBLE PRECISION,
    actual_total_return_pct DOUBLE PRECISION,
    benchmark_return_pct DOUBLE PRECISION,
    benchmark_annualized_return_pct DOUBLE PRECISION,
    period_days INTEGER,
    validation_start_date DATE,
    validation_end_date DATE,
    validation_benchmark_return_pct DOUBLE PRECISION,
    validation_benchmark_annualized_return_pct DOUBLE PRECISION,
    validation_period_days INTEGER,
    position_control JSONB,
    predicted_change_stats JSONB,
    per_chunk_signals JSONB,
    equity_curve_values JSONB,
    equity_curve_pct JSONB,
    equity_curve_pct_gross JSONB,
    curve_dates JSONB,
    actual_end_prices JSONB,
    trades JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS uzi_reports (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ticker VARCHAR(32) NOT NULL,
    depth VARCHAR(16),
    status VARCHAR(32) NOT NULL DEFAULT 'succeeded',
    directory_name VARCHAR(255) NOT NULL,
    date_tag VARCHAR(32),
    report_relative_path VARCHAR(512) NOT NULL,
    report_url TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    exit_code INTEGER,
    duration_seconds DOUBLE PRECISION,
    stdout_tail TEXT,
    stderr_tail TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE TABLE IF NOT EXISTS ai_payment_records (
    id SERIAL PRIMARY KEY,
    resource_id TEXT NOT NULL,
    order_id TEXT NOT NULL,
    credential_hash TEXT NOT NULL,
    request_signature TEXT NOT NULL,
    period_key DATE NOT NULL,
    payment_status TEXT NOT NULL DEFAULT 'paid',
    fulfillment_status TEXT NOT NULL DEFAULT 'processing',
    response_status INTEGER,
    response_body JSONB,
    paid_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    fulfilled_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_ai_payment_records_once
        UNIQUE (resource_id, order_id, request_signature, period_key),
    CONSTRAINT ai_payment_records_payment_status_check
        CHECK (payment_status IN ('paid', 'unpaid', 'refunded')),
    CONSTRAINT ai_payment_records_fulfillment_status_check
        CHECK (fulfillment_status IN ('processing', 'fulfilled', 'failed'))
);

CREATE TABLE IF NOT EXISTS open_api_keys (
    id SERIAL PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    owner_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMP WITH TIME ZONE,
    rate_limit_per_minute INTEGER NOT NULL DEFAULT 60,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP WITH TIME ZONE,
    CHECK (status IN ('active', 'disabled')),
    CHECK (rate_limit_per_minute > 0)
);

CREATE TABLE IF NOT EXISTS open_api_audit_logs (
    id SERIAL PRIMARY KEY,
    request_id TEXT NOT NULL,
    key_id INTEGER REFERENCES open_api_keys(id) ON DELETE SET NULL,
    fintrack_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    scope TEXT,
    status_code INTEGER,
    latency_ms INTEGER,
    error_code TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS membership_level INT DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS membership_expires_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS daily_stock_analysis_user_id VARCHAR(128);
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS code VARCHAR(64);
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS membership_level INT;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS duration_days INT;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS used_count INT NOT NULL DEFAULT 0;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS max_uses INT NOT NULL DEFAULT 50;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS note TEXT;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS created_by INTEGER REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE membership_invite_codes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'membership_invite_codes_max_uses_check'
    ) THEN
        ALTER TABLE membership_invite_codes
        ADD CONSTRAINT membership_invite_codes_max_uses_check CHECK (max_uses > 0);
    END IF;
END $$;
ALTER TABLE user_ai_model_configs ADD COLUMN IF NOT EXISTS provider_name VARCHAR(80) NOT NULL DEFAULT 'DeepSeek';
ALTER TABLE user_ai_model_configs ADD COLUMN IF NOT EXISTS base_url TEXT;
ALTER TABLE user_ai_model_configs ADD COLUMN IF NOT EXISTS api_key TEXT;
ALTER TABLE user_ai_model_configs ADD COLUMN IF NOT EXISTS model_id VARCHAR(160);
ALTER TABLE user_ai_model_configs ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE user_ai_model_configs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE mtf_agent_sessions ADD COLUMN IF NOT EXISTS deepseek_thread_id VARCHAR(128);
ALTER TABLE mtf_agent_sessions ADD COLUMN IF NOT EXISTS model_id VARCHAR(160);
ALTER TABLE mtf_agent_sessions ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE mtf_agent_sessions ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE mtf_agent_sessions ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE mtf_agent_memories ADD COLUMN IF NOT EXISTS memory_type VARCHAR(80);
ALTER TABLE mtf_agent_memories ADD COLUMN IF NOT EXISTS content TEXT;
ALTER TABLE mtf_agent_memories ADD COLUMN IF NOT EXISTS source VARCHAR(80) NOT NULL DEFAULT 'explicit';
ALTER TABLE mtf_agent_memories ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE mtf_agent_memories ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE mtf_agent_memories ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE mtf_agent_messages ADD COLUMN IF NOT EXISTS user_id INTEGER;
ALTER TABLE mtf_agent_messages ADD COLUMN IF NOT EXISTS thread_id VARCHAR(128);
ALTER TABLE mtf_agent_messages ADD COLUMN IF NOT EXISTS role VARCHAR(24);
ALTER TABLE mtf_agent_messages ADD COLUMN IF NOT EXISTS content TEXT;
ALTER TABLE mtf_agent_messages ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE user_watchlist ADD COLUMN IF NOT EXISTS symbol VARCHAR(20);
ALTER TABLE user_watchlist ADD COLUMN IF NOT EXISTS stock_type SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE user_watchlist ADD COLUMN IF NOT EXISTS strategy_unique_key VARCHAR(255);
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS is_public SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE mtf_best_predictions ALTER COLUMN is_public SET DEFAULT 1;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS short_name VARCHAR(255);
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS best_prediction_quantile DOUBLE PRECISION;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS prediction_type TEXT NOT NULL DEFAULT 'mtf-lite';
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_config JSONB;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_signature TEXT;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_analysis JSONB;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS best_prediction_values JSONB;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS future_dates JSONB;
ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS adjust_raw_best_prediction_values JSONB;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS prediction_type TEXT NOT NULL DEFAULT 'mtf-lite';
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_config JSONB;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_signature TEXT;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_analysis JSONB;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS stock_type SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS adjust_raw_chunks JSONB;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS predicted_change_percent JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS actual_change_percent JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS change_base_value DOUBLE PRECISION;
ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS change_base_date DATE;
ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_config JSONB;
ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_signature TEXT;
ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_analysis JSONB;
ALTER TABLE mtf_strategy_params ADD COLUMN IF NOT EXISTS is_public SMALLINT NOT NULL DEFAULT 1;
ALTER TABLE mtf_strategy_params ALTER COLUMN is_public SET DEFAULT 1;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS depth VARCHAR(16);
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS status VARCHAR(32) NOT NULL DEFAULT 'succeeded';
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS directory_name VARCHAR(255);
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS date_tag VARCHAR(32);
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS report_url TEXT;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS size_bytes BIGINT NOT NULL DEFAULT 0;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS exit_code INTEGER;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS duration_seconds DOUBLE PRECISION;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS stdout_tail TEXT;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS stderr_tail TEXT;
ALTER TABLE uzi_reports ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS resource_id TEXT;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS order_id TEXT;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS credential_hash TEXT;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS request_signature TEXT;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS period_key DATE;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS payment_status TEXT NOT NULL DEFAULT 'paid';
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS fulfillment_status TEXT NOT NULL DEFAULT 'processing';
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS response_status INTEGER;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS response_body JSONB;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS paid_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE ai_payment_records ADD COLUMN IF NOT EXISTS fulfilled_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS key_hash TEXT;
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT 'default';
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS owner_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS scopes TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS rate_limit_per_minute INTEGER NOT NULL DEFAULT 60;
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE open_api_keys ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP WITH TIME ZONE;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_dsa_user_id
ON users(daily_stock_analysis_user_id)
WHERE daily_stock_analysis_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON user_sessions(token_hash);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON user_sessions(user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_membership_invite_codes_code ON membership_invite_codes(code);
CREATE INDEX IF NOT EXISTS idx_membership_invite_codes_active ON membership_invite_codes(is_active);
CREATE INDEX IF NOT EXISTS idx_user_ai_model_configs_user_id ON user_ai_model_configs(user_id);
CREATE INDEX IF NOT EXISTS idx_mtf_agent_sessions_user_id ON mtf_agent_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_mtf_agent_memories_user_id ON mtf_agent_memories(user_id);
CREATE INDEX IF NOT EXISTS idx_mtf_agent_messages_user_thread_created ON mtf_agent_messages(user_id, thread_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_stocks_symbol ON stocks(symbol);
CREATE INDEX IF NOT EXISTS idx_prices_stock ON stock_prices(stock_id);
CREATE INDEX IF NOT EXISTS idx_prices_recorded ON stock_prices(recorded_at);
CREATE INDEX IF NOT EXISTS idx_watchlist_user ON user_watchlist(user_id);
CREATE INDEX IF NOT EXISTS idx_watchlist_symbol ON user_watchlist(symbol);
CREATE INDEX IF NOT EXISTS idx_strategy_params_user ON mtf_strategy_params(user_id);
CREATE INDEX IF NOT EXISTS idx_mtf_best_predictions_symbol ON mtf_best_predictions(symbol);
CREATE INDEX IF NOT EXISTS idx_mtf_best_validation_chunks_user_id ON mtf_best_validation_chunks(user_id);
CREATE INDEX IF NOT EXISTS idx_mtf_best_validation_chunks_symbol ON mtf_best_validation_chunks(symbol);
CREATE INDEX IF NOT EXISTS idx_mtf_backtests_symbol ON mtf_backtests(symbol);
CREATE INDEX IF NOT EXISTS idx_mtf_backtests_strategy_params_id ON mtf_backtests(strategy_params_id);
CREATE INDEX IF NOT EXISTS idx_uzi_reports_user_ticker_updated ON uzi_reports(user_id, ticker, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_uzi_reports_user_deleted ON uzi_reports(user_id, deleted_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uzi_reports_user_path_active
ON uzi_reports(user_id, report_relative_path)
WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_ai_payment_records_order
ON ai_payment_records(resource_id, order_id, period_key);
CREATE INDEX IF NOT EXISTS idx_ai_payment_records_status
ON ai_payment_records(fulfillment_status, updated_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_open_api_keys_hash ON open_api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_open_api_keys_user ON open_api_keys(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_open_api_audit_logs_key_created ON open_api_audit_logs(key_id, created_at DESC);

-- 官方推荐策略预设。保留 unique_key，避免影响已绑定策略。
INSERT INTO mtf_strategy_params (
    unique_key, name, is_public, user_id,
    buy_threshold_pct, sell_threshold_pct, initial_cash,
    enable_rebalance, max_position_pct, min_position_pct,
    slope_position_per_pct, rebalance_tolerance_pct,
    trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
) VALUES
('strategy_conservative', '稳健防守', 1, NULL, 2.4, -0.8, 10000, true, 0.55, 0.10, 0.08, 0.08, 0.006, 8.0, 0.40),
('strategy_balanced', '均衡轮动', 1, NULL, 1.5, -1.2, 10000, true, 0.75, 0.15, 0.12, 0.06, 0.006, 12.0, 0.50),
('strategy_aggressive', '趋势进取', 1, NULL, 0.8, -1.8, 10000, true, 0.95, 0.20, 0.18, 0.04, 0.006, 18.0, 0.35)
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
