-- PostgreSQL 股票数据处理服务 - 数据库初始化脚本
-- 此脚本会在PostgreSQL容器首次启动时自动执行

-- 设置数据库编码和时区
SET client_encoding = 'UTF8';
SET timezone = 'Asia/Shanghai';

-- 创建扩展（如果需要）
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
ALTER TABLE stock_data
ADD COLUMN change_percent DECIMAL(8,4);
ADD COLUMN outstanding_share BIGINT;

CREATE TABLE IF NOT EXISTS mtf_strategy_params (
    id SERIAL PRIMARY KEY,
    unique_key VARCHAR(255) NOT NULL UNIQUE,
    user_id INTEGER,
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
CREATE INDEX IF NOT EXISTS idx_strategy_params_user ON mtf_strategy_params(user_id);
CREATE INDEX IF NOT EXISTS idx_strategy_params_unique_key ON mtf_strategy_params(unique_key);
CREATE INDEX IF NOT EXISTS idx_strategy_params_user_unique_key ON mtf_strategy_params(user_id, unique_key);

-- LLM Token Usage Tracking Table
CREATE TABLE IF NOT EXISTS llm_token_usage (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    provider VARCHAR(50) NOT NULL,
    model VARCHAR(100) NOT NULL,
    prompt_tokens INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    total_tokens INTEGER NOT NULL,
    request_time TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_llm_token_usage_user_id ON llm_token_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_llm_token_usage_provider ON llm_token_usage(provider);
CREATE INDEX IF NOT EXISTS idx_llm_token_usage_request_time ON llm_token_usage(request_time);
CREATE INDEX IF NOT EXISTS idx_llm_token_usage_user_time ON llm_token_usage(user_id, request_time DESC);

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
CREATE INDEX IF NOT EXISTS idx_ai_payment_records_order
ON ai_payment_records(resource_id, order_id, period_key);
CREATE INDEX IF NOT EXISTS idx_ai_payment_records_status
ON ai_payment_records(fulfillment_status, updated_at DESC);


-- 输出初始化完成信息
DO $$
BEGIN
    RAISE NOTICE '=== PostgreSQL 股票数据处理服务数据库初始化完成 ===';
    RAISE NOTICE '数据库名称: fintrack';
    RAISE NOTICE '编码: UTF-8';
    RAISE NOTICE '时区: Asia/Shanghai';
    RAISE NOTICE '股票数据表将由应用程序自动创建和分区';
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'mtf_best_predictions') THEN
        EXECUTE 'ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS prediction_type TEXT NOT NULL DEFAULT ''mtf-lite''';
        EXECUTE 'ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_config JSONB';
        EXECUTE 'ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_signature TEXT';
        EXECUTE 'ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_analysis JSONB';
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'mtf_best_validation_chunks') THEN
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS prediction_type TEXT NOT NULL DEFAULT ''mtf-lite''';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_config JSONB';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_signature TEXT';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_analysis JSONB';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS predicted_change_percent JSONB NOT NULL DEFAULT ''{}''::jsonb';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS actual_change_percent JSONB NOT NULL DEFAULT ''[]''::jsonb';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS change_base_value DOUBLE PRECISION';
        EXECUTE 'ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS change_base_date DATE';
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'mtf_backtests') THEN
        EXECUTE 'ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_config JSONB';
        EXECUTE 'ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_signature TEXT';
        EXECUTE 'ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_analysis JSONB';
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'mtf_direct_predictions') THEN
        EXECUTE 'ALTER TABLE mtf_direct_predictions ADD COLUMN IF NOT EXISTS covariate_config JSONB';
        EXECUTE 'ALTER TABLE mtf_direct_predictions ADD COLUMN IF NOT EXISTS covariate_signature TEXT';
        EXECUTE 'ALTER TABLE mtf_direct_predictions ADD COLUMN IF NOT EXISTS covariate_analysis JSONB';
        EXECUTE 'UPDATE mtf_direct_predictions SET covariate_signature = '''' WHERE covariate_signature IS NULL';
        EXECUTE 'ALTER TABLE mtf_direct_predictions ALTER COLUMN covariate_signature SET DEFAULT ''''';
        EXECUTE 'ALTER TABLE mtf_direct_predictions ALTER COLUMN covariate_signature SET NOT NULL';
        IF EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conname = 'uq_mtf_direct_prediction_key'
        ) THEN
            EXECUTE 'ALTER TABLE mtf_direct_predictions DROP CONSTRAINT uq_mtf_direct_prediction_key';
        END IF;
        EXECUTE 'ALTER TABLE mtf_direct_predictions
                 ADD CONSTRAINT uq_mtf_direct_prediction_key
                 UNIQUE (symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature)';
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_mtf_direct_predictions_lookup_cov
                 ON mtf_direct_predictions(symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature)';
    END IF;
END $$;

-- =========================
-- 为 mtf_backtests 增加策略参数ID及索引
-- 目的：使用 mtf_strategy_params.id 作为联合查询条件，加速回测结果检索
-- =========================
DO $$
BEGIN
    -- 若回测结果表已存在，则补充列、外键与索引
    IF EXISTS (
        SELECT 1 FROM information_schema.tables 
        WHERE table_name = 'mtf_backtests'
    ) THEN
        -- 添加列：strategy_params_id（引用 mtf_strategy_params.id）
        EXECUTE 'ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS strategy_params_id INTEGER';

        -- 添加外键约束（若尚不存在）
        IF NOT EXISTS (
            SELECT 1
            FROM pg_constraint
            WHERE conname = 'fk_backtests_strategy_params'
        ) THEN
            EXECUTE 'ALTER TABLE mtf_backtests
                     ADD CONSTRAINT fk_backtests_strategy_params
                     FOREIGN KEY (strategy_params_id)
                     REFERENCES mtf_strategy_params(id)
                     ON DELETE SET NULL';
        END IF;

        -- 单列索引：按策略参数ID检索
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_mtf_backtests_strategy_params_id ON mtf_backtests(strategy_params_id)';

        -- 复合索引：策略参数ID + unique_key 联合查询优化
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_mtf_backtests_strategy_params_unique ON mtf_backtests(strategy_params_id, unique_key)';
    END IF;
END $$;
