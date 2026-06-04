package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvWithAliases(keys []string, defaultValue string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return defaultValue
}

func NewDatabaseHandler() (*DatabaseHandler, error) {
	_ = godotenv.Load()

	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "")
	dbName := getEnv("DB_NAME", "fintrack")
	dbSSLMode := getEnv("DB_SSLMODE", "disable")

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		dbHost, dbPort, dbUser, dbPassword, dbName, dbSSLMode)

	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}
	handler := &DatabaseHandler{db: gdb}
	if err := handler.initializeDatabase(); err != nil {
		return nil, fmt.Errorf("failed to initialize database: %v", err)
	}
	return handler, nil
}

func (h *DatabaseHandler) initializeDatabase() error {
	createMainTableSQL := `
    CREATE TABLE IF NOT EXISTS stock_data (
        id SERIAL,
        datetime TIMESTAMP NOT NULL,
        date_str VARCHAR(10) NOT NULL,
        open DECIMAL(10,4) NOT NULL,
        close DECIMAL(10,4) NOT NULL,
        high DECIMAL(10,4) NOT NULL,
        low DECIMAL(10,4) NOT NULL,
        volume BIGINT NOT NULL,
        amount DECIMAL(15,2) NOT NULL,
        amplitude DECIMAL(8,4) NOT NULL,
        percentage_change DECIMAL(8,4) NOT NULL,
        amount_change DECIMAL(10,4) NOT NULL,
        turnover_rate DECIMAL(8,4) NOT NULL,
        type INTEGER NOT NULL,
        symbol VARCHAR(20) NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        PRIMARY KEY (id, type)
    ) PARTITION BY RANGE (type);`
	if err := h.db.Exec(createMainTableSQL).Error; err != nil {
		return fmt.Errorf("failed to create main table: %v", err)
	}

	partitions := []struct {
		name     string
		minValue int
		maxValue int
	}{
		{"stock_data_stocks", 1, 2},
		{"stock_data_funds", 2, 3},
		{"stock_data_indices", 3, 4},
		{"stock_data_others", 4, 100},
	}
	for _, p := range partitions {
		createPartitionSQL := fmt.Sprintf(`
        CREATE TABLE IF NOT EXISTS %s PARTITION OF stock_data
        FOR VALUES FROM (%d) TO (%d);`, p.name, p.minValue, p.maxValue)
		if err := h.db.Exec(createPartitionSQL).Error; err != nil {
			return fmt.Errorf("failed to create partition %s: %v", p.name, err)
		}
		createIndexSQL := fmt.Sprintf(`
        CREATE INDEX IF NOT EXISTS idx_%s_datetime ON %s (datetime);
        CREATE INDEX IF NOT EXISTS idx_%s_date_str ON %s (date_str);
        CREATE INDEX IF NOT EXISTS idx_%s_symbol ON %s (symbol);
        CREATE INDEX IF NOT EXISTS idx_%s_symbol_datetime ON %s (symbol, datetime);`,
			p.name, p.name,
			p.name, p.name,
			p.name, p.name,
			p.name, p.name)
		if err := h.db.Exec(createIndexSQL).Error; err != nil {
			log.Printf("Warning: failed to create index for %s: %v", p.name, err)
		}
	}

	createEtfDailySQL := `
    CREATE TABLE IF NOT EXISTS etf_daily (
        code TEXT NOT NULL,
        trading_date DATE NOT NULL,
        name TEXT,
        latest_price NUMERIC(12,4),
        change_amount NUMERIC(12,4),
        change_percent NUMERIC(12,4),
        buy NUMERIC(12,4),
        sell NUMERIC(12,4),
        prev_close NUMERIC(12,4),
        open NUMERIC(12,4),
        high NUMERIC(12,4),
        low NUMERIC(12,4),
        volume BIGINT,
        turnover BIGINT,
        created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
        PRIMARY KEY (code, trading_date)
    );
    CREATE INDEX IF NOT EXISTS idx_etf_daily_trading_date ON etf_daily (trading_date);
    CREATE INDEX IF NOT EXISTS idx_etf_daily_code ON etf_daily (code);
    `
	if err := h.db.Exec(createEtfDailySQL).Error; err != nil {
		return fmt.Errorf("failed to create etf_daily table: %v", err)
	}

	createIndexInfoSQL := `
    CREATE TABLE IF NOT EXISTS index_info (
        code TEXT PRIMARY KEY,
        display_name TEXT,
        publish_date DATE,
        created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW()
    );
    CREATE INDEX IF NOT EXISTS idx_index_info_display_name ON index_info (display_name);
    `
	if err := h.db.Exec(createIndexInfoSQL).Error; err != nil {
		return fmt.Errorf("failed to create index_info table: %v", err)
	}

	createIndexDailySQL := `
    CREATE TABLE IF NOT EXISTS index_daily (
        code TEXT NOT NULL,
        trading_date DATE NOT NULL,
        open NUMERIC(12,4),
        close NUMERIC(12,4),
        high NUMERIC(12,4),
        low NUMERIC(12,4),
        volume BIGINT,
        amount NUMERIC(20,4),
        change_percent NUMERIC(12,4),
        created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
        PRIMARY KEY (code, trading_date)
    );
    CREATE INDEX IF NOT EXISTS idx_index_daily_code ON index_daily (code);
    CREATE INDEX IF NOT EXISTS idx_index_daily_trading_date ON index_daily (trading_date);
    `
	if err := h.db.Exec(createIndexDailySQL).Error; err != nil {
		return fmt.Errorf("failed to create index_daily table: %v", err)
	}

	createAStockCommentDailySQL := `
    CREATE TABLE IF NOT EXISTS a_stock_comment_daily (
        code TEXT NOT NULL,
        trading_date DATE NOT NULL,
        name TEXT,
        latest_price NUMERIC(12,4),
        change_percent NUMERIC(12,4),
        turnover_rate NUMERIC(12,4),
        pe_ratio NUMERIC(12,4),
        main_cost NUMERIC(12,4),
        institution_participation NUMERIC(12,4),
        composite_score NUMERIC(12,4),
        rise BIGINT,
        current_rank BIGINT,
        attention_index NUMERIC(12,4),
        created_at TIMESTAMP WITHOUT TIME ZONE DEFAULT NOW(),
        PRIMARY KEY (code, trading_date)
    );
    CREATE INDEX IF NOT EXISTS idx_a_stock_comment_daily_code ON a_stock_comment_daily (code);
    CREATE INDEX IF NOT EXISTS idx_a_stock_comment_daily_trading_date ON a_stock_comment_daily (trading_date);
    CREATE INDEX IF NOT EXISTS idx_a_stock_comment_daily_name ON a_stock_comment_daily (name);
    `
	if err := h.db.Exec(createAStockCommentDailySQL).Error; err != nil {
		return fmt.Errorf("failed to create a_stock_comment_daily table: %v", err)
	}

	createForecastTableSQL := `
    CREATE TABLE IF NOT EXISTS mtf_forecast (
        id SERIAL PRIMARY KEY,
        symbol VARCHAR(20) NOT NULL,
        ds TIMESTAMP NOT NULL,
        tsf DECIMAL(10,4) NOT NULL,
        tsf_01 DECIMAL(10,4) NOT NULL,
        tsf_02 DECIMAL(10,4) NOT NULL,
        tsf_03 DECIMAL(10,4) NOT NULL,
        tsf_04 DECIMAL(10,4) NOT NULL,
        tsf_05 DECIMAL(10,4) NOT NULL,
        tsf_06 DECIMAL(10,4) NOT NULL,
        tsf_07 DECIMAL(10,4) NOT NULL,
        tsf_08 DECIMAL(10,4) NOT NULL,
        tsf_09 DECIMAL(10,4) NOT NULL,
        chunk_index INTEGER NOT NULL,
        best_quantile VARCHAR(20) NOT NULL,
        best_quantile_pct VARCHAR(20) NOT NULL,
        best_pred_pct DECIMAL(10,6) NOT NULL,
        actual_pct DECIMAL(10,6) NOT NULL,
        diff_pct DECIMAL(10,6) NOT NULL,
        mse DECIMAL(12,6) NOT NULL,
        mae DECIMAL(12,6) NOT NULL,
        combined_score DECIMAL(12,6) NOT NULL,
        version FLOAT8 NOT NULL,
        horizon_len INTEGER NOT NULL,
        user_id INTEGER,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );`
	if err := h.db.Exec(createForecastTableSQL).Error; err != nil {
		return fmt.Errorf("failed to create mtf_forecast table: %v", err)
	}

	createStrategyParamsSQL := `
    CREATE TABLE IF NOT EXISTS mtf_strategy_params (
        id SERIAL PRIMARY KEY,
        unique_key VARCHAR(255) NOT NULL,
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
        updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT uni_mtf_strategy_params_unique_key UNIQUE (unique_key)
    );
    CREATE INDEX IF NOT EXISTS idx_strategy_params_user ON mtf_strategy_params(user_id);
    `
	if err := h.db.Exec(createStrategyParamsSQL).Error; err != nil {
		return fmt.Errorf("failed to create mtf_strategy_params table: %v", err)
	}
	// 为已存在的表补充唯一约束（如果缺失）
	ensureUniqueConstraintSQL := `
    DO $$
    BEGIN
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint WHERE conname = 'uni_mtf_strategy_params_unique_key'
        ) THEN
            ALTER TABLE mtf_strategy_params
            ADD CONSTRAINT uni_mtf_strategy_params_unique_key UNIQUE (unique_key);
        END IF;
    END
    $$;`
	_ = h.db.Exec(ensureUniqueConstraintSQL).Error
	_ = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mtf_forecast_symbol_ds ON mtf_forecast (symbol, ds);`).Error
	_ = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mtf_forecast_svhl_ds ON mtf_forecast (symbol, version, horizon_len, ds);`).Error

	createMTFBestSQL := `
    CREATE TABLE IF NOT EXISTS mtf_best_predictions (
        id SERIAL PRIMARY KEY,
        unique_key TEXT NOT NULL UNIQUE,
        symbol VARCHAR(20) NOT NULL,
        mtf_version VARCHAR(20) NOT NULL,
        best_prediction_item VARCHAR(50) NOT NULL,
        best_metrics JSONB NOT NULL,
        prediction_type TEXT NOT NULL DEFAULT 'mtf-lite',
        covariate_config JSONB,
        covariate_signature TEXT,
        covariate_analysis JSONB,
        is_public SMALLINT NOT NULL DEFAULT 0,
        train_start_date DATE NOT NULL,
        train_end_date DATE NOT NULL,
        test_start_date DATE NOT NULL,
        test_end_date DATE NOT NULL,
        val_start_date DATE NOT NULL,
        val_end_date DATE NOT NULL,
        context_len INTEGER NOT NULL,
        horizon_len INTEGER NOT NULL,
        short_name TEXT,
        stock_type INTEGER,
        created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_mtf_best_predictions_symbol ON mtf_best_predictions(symbol);
    `
	if err := h.db.Exec(createMTFBestSQL).Error; err != nil {
		return fmt.Errorf("failed to create mtf_best_predictions table: %v", err)
	}
	// 为已存在的表补充缺失列（如果缺失）
	_ = h.db.Exec(`ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS stock_type INTEGER`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS prediction_type TEXT NOT NULL DEFAULT 'mtf-lite'`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_predictions ALTER COLUMN prediction_type SET DEFAULT 'mtf-lite'`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_config JSONB`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_signature TEXT`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_predictions ADD COLUMN IF NOT EXISTS covariate_analysis JSONB`).Error

	createMTFValChunksSQL := `
    CREATE TABLE IF NOT EXISTS mtf_best_validation_chunks (
        id SERIAL PRIMARY KEY,
        unique_key TEXT NOT NULL,
        chunk_index INTEGER NOT NULL,
        user_id INT4,
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
        stock_name TEXT,
        stock_type INTEGER,
        created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT fk_mtf_best FOREIGN KEY (unique_key)
            REFERENCES mtf_best_predictions (unique_key) ON DELETE CASCADE,
        CONSTRAINT uq_mtf_best_chunk UNIQUE (unique_key, chunk_index)
    );
    CREATE INDEX IF NOT EXISTS idx_mtf_best_validation_chunks_user_id ON mtf_best_validation_chunks(user_id);
    CREATE INDEX IF NOT EXISTS idx_mtf_best_validation_chunks_symbol ON mtf_best_validation_chunks(symbol);
    `
	if err := h.db.Exec(createMTFValChunksSQL).Error; err != nil {
		return fmt.Errorf("failed to create mtf_best_validation_chunks table: %v", err)
	}
	// 为已存在的表补充缺失列（如果缺失）
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS stock_name TEXT`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS stock_type INTEGER`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS prediction_type TEXT NOT NULL DEFAULT 'mtf-lite'`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ALTER COLUMN prediction_type SET DEFAULT 'mtf-lite'`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_config JSONB`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_signature TEXT`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS covariate_analysis JSONB`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS predicted_change_percent JSONB NOT NULL DEFAULT '{}'::jsonb`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS actual_change_percent JSONB NOT NULL DEFAULT '[]'::jsonb`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS change_base_value DOUBLE PRECISION`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_best_validation_chunks ADD COLUMN IF NOT EXISTS change_base_date DATE`).Error

	createMTFBacktestsSQL := `
    CREATE TABLE IF NOT EXISTS mtf_backtests (
        id SERIAL PRIMARY KEY,
        unique_key VARCHAR(255) NOT NULL UNIQUE,
        user_id INTEGER,
        strategy_params_id INTEGER,
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
    `
	if err := h.db.Exec(createMTFBacktestsSQL).Error; err != nil {
		return fmt.Errorf("failed to create mtf_backtests table: %v", err)
	}
	_ = h.db.Exec(`ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS strategy_params_id INTEGER`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_config JSONB`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_signature TEXT`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_backtests ADD COLUMN IF NOT EXISTS covariate_analysis JSONB`).Error
	_ = h.db.Exec(`DO $$ BEGIN
        IF NOT EXISTS (
            SELECT 1
            FROM pg_constraint
            WHERE conname = 'fk_backtests_strategy_params'
        ) THEN
            ALTER TABLE mtf_backtests
            ADD CONSTRAINT fk_backtests_strategy_params
            FOREIGN KEY (strategy_params_id)
            REFERENCES mtf_strategy_params(id)
            ON DELETE SET NULL;
        END IF;
    END $$;`).Error
	_ = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mtf_backtests_strategy_params_id ON mtf_backtests(strategy_params_id)`).Error
	_ = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mtf_backtests_strategy_params_unique ON mtf_backtests(strategy_params_id, unique_key)`).Error

	createMTFDirectSQL := `
    CREATE TABLE IF NOT EXISTS mtf_direct_predictions (
        id SERIAL PRIMARY KEY,
        unique_key TEXT NOT NULL UNIQUE,
        symbol VARCHAR(20) NOT NULL,
        stock_type INTEGER NOT NULL DEFAULT 1,
        mtf_version VARCHAR(20) NOT NULL,
        context_len INTEGER NOT NULL,
        horizon_len INTEGER NOT NULL,
        future_dates_key TEXT NOT NULL,
        future_dates JSONB NOT NULL,
        request_end_date DATE NOT NULL,
        latest_data_date DATE NOT NULL,
        latest_close DOUBLE PRECISION,
        history_rows INTEGER,
        best_prediction_item VARCHAR(50),
        best_prediction_values JSONB,
        predictions JSONB NOT NULL,
        covariate_config JSONB,
        covariate_signature TEXT NOT NULL DEFAULT '',
        covariate_analysis JSONB,
        short_name TEXT,
        user_id INTEGER,
        created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
        CONSTRAINT uq_mtf_direct_prediction_key UNIQUE (symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature)
    );
    CREATE INDEX IF NOT EXISTS idx_mtf_direct_predictions_symbol ON mtf_direct_predictions(symbol);
    CREATE INDEX IF NOT EXISTS idx_mtf_direct_predictions_lookup ON mtf_direct_predictions(symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature);
    `
	if err := h.db.Exec(createMTFDirectSQL).Error; err != nil {
		return fmt.Errorf("failed to create mtf_direct_predictions table: %v", err)
	}
	_ = h.db.Exec(`ALTER TABLE mtf_direct_predictions ADD COLUMN IF NOT EXISTS covariate_config JSONB`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_direct_predictions ADD COLUMN IF NOT EXISTS covariate_signature TEXT`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_direct_predictions ADD COLUMN IF NOT EXISTS covariate_analysis JSONB`).Error
	_ = h.db.Exec(`UPDATE mtf_direct_predictions SET covariate_signature = '' WHERE covariate_signature IS NULL`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_direct_predictions ALTER COLUMN covariate_signature SET DEFAULT ''`).Error
	_ = h.db.Exec(`ALTER TABLE mtf_direct_predictions ALTER COLUMN covariate_signature SET NOT NULL`).Error
	_ = h.db.Exec(`DO $$ BEGIN
        IF EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conname = 'uq_mtf_direct_prediction_key'
        ) THEN
            ALTER TABLE mtf_direct_predictions DROP CONSTRAINT uq_mtf_direct_prediction_key;
        END IF;
        ALTER TABLE mtf_direct_predictions
        ADD CONSTRAINT uq_mtf_direct_prediction_key
        UNIQUE (symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature);
    END $$;`).Error
	_ = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_mtf_direct_predictions_lookup_cov ON mtf_direct_predictions(symbol, stock_type, horizon_len, context_len, future_dates_key, covariate_signature)`).Error
	if err := h.db.AutoMigrate(&EtfDailyData{}, &IndexInfo{}, &IndexDailyData{}, &StockCommentDaily{}, &MTFForecast{}); err != nil {
		log.Printf("AutoMigrate warning: %v", err)
	}
	return nil
}
