package main

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type DatabaseHandler struct {
	db *gorm.DB
}

func normalizeStockSymbol(symbol string) string {
	raw := strings.TrimSpace(symbol)
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"sh", "sz", "bj"} {
		if strings.HasPrefix(lower, prefix) && len(raw) > len(prefix) {
			return raw[len(prefix):]
		}
	}
	return raw
}

func normalizedStockSymbolSQL(column string) string {
	return fmt.Sprintf(
		"CASE WHEN char_length(%[1]s) > 2 AND lower(left(%[1]s, 2)) IN ('sh', 'sz', 'bj') THEN substring(%[1]s from 3) ELSE %[1]s END",
		column,
	)
}

func (h *DatabaseHandler) Close() error {
	sqlDB, err := h.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (h *DatabaseHandler) InsertStockData(data *StockData) error {
	// Ensure date_str is populated (YYYY-MM-DD)
	dateStr := data.DateStr
	if dateStr == "" {
		dateStr = data.Datetime.Format("2006-01-02")
	}
	data.Symbol = normalizeStockSymbol(data.Symbol)
	query := `
    INSERT INTO stock_data (
        datetime, date_str, open, close, high, low, volume, amount, amplitude,
        percentage_change, amount_change, turnover_rate, type, symbol
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    ON CONFLICT (date_str, symbol, type) DO UPDATE SET
        datetime = EXCLUDED.datetime,
        open = EXCLUDED.open,
        close = EXCLUDED.close,
        high = EXCLUDED.high,
        low = EXCLUDED.low,
        volume = EXCLUDED.volume,
        amount = EXCLUDED.amount,
        amplitude = EXCLUDED.amplitude,
        percentage_change = EXCLUDED.percentage_change,
        amount_change = EXCLUDED.amount_change,
        turnover_rate = EXCLUDED.turnover_rate,
        updated_at = CURRENT_TIMESTAMP
    RETURNING id, created_at, updated_at`
	row := h.db.Raw(query,
		data.Datetime, dateStr, data.Open, data.Close, data.High, data.Low,
		data.Volume, data.Amount, data.Amplitude, data.PercentageChange,
		data.AmountChange, data.TurnoverRate, data.Type, data.Symbol,
	).Row()
	return row.Scan(&data.ID, &data.CreatedAt, &data.UpdatedAt)
}

func (h *DatabaseHandler) BatchInsertStockData(dataList []StockData) error {
	tx := h.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %v", tx.Error)
	}
	upsertSQL := `
	    INSERT INTO stock_data (
	        datetime, date_str, open, close, high, low, volume, amount, amplitude,
	        percentage_change, amount_change, turnover_rate, type, symbol
	    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	    ON CONFLICT (date_str, symbol, type) DO UPDATE SET
	        datetime = EXCLUDED.datetime,
	        open = EXCLUDED.open,
	        close = EXCLUDED.close,
	        high = EXCLUDED.high,
	        low = EXCLUDED.low,
	        volume = EXCLUDED.volume,
	        amount = EXCLUDED.amount,
	        amplitude = EXCLUDED.amplitude,
	        percentage_change = EXCLUDED.percentage_change,
	        amount_change = EXCLUDED.amount_change,
	        turnover_rate = EXCLUDED.turnover_rate,
	        updated_at = CURRENT_TIMESTAMP`
	dailyUpsertSQL := `
	    INSERT INTO daily_data (
	        symbol, stock_type, trading_date, latest_price, change_amount, change_percent,
	        open, high, low, volume, turnover, turnover_rate
	    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	    ON CONFLICT (symbol, stock_type) DO UPDATE SET
	        trading_date = EXCLUDED.trading_date,
	        latest_price = EXCLUDED.latest_price,
	        change_amount = EXCLUDED.change_amount,
	        change_percent = EXCLUDED.change_percent,
	        open = EXCLUDED.open,
	        high = EXCLUDED.high,
	        low = EXCLUDED.low,
	        volume = EXCLUDED.volume,
	        turnover = EXCLUDED.turnover,
	        turnover_rate = EXCLUDED.turnover_rate,
	        updated_at = NOW()
	    WHERE daily_data.trading_date <= EXCLUDED.trading_date`
	for _, data := range dataList {
		// Ensure date_str is populated (YYYY-MM-DD)
		dateStr := data.DateStr
		if dateStr == "" {
			dateStr = data.Datetime.Format("2006-01-02")
		}
		normalizedSymbol := normalizeStockSymbol(data.Symbol)
		if err := tx.Exec(upsertSQL,
			data.Datetime, dateStr, data.Open, data.Close, data.High, data.Low,
			data.Volume, data.Amount, data.Amplitude, data.PercentageChange,
			data.AmountChange, data.TurnoverRate, data.Type, normalizedSymbol,
		).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute batch insert: %v", err)
		}
		tradingDate := data.Datetime
		if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
			tradingDate = parsed
		}
		if err := tx.Exec(dailyUpsertSQL,
			normalizedSymbol, data.Type, tradingDate, data.Close, data.AmountChange, data.PercentageChange,
			data.Open, data.High, data.Low, data.Volume, int64(data.Amount), data.TurnoverRate,
		).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to upsert daily_data: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}
	return nil
}

func (h *DatabaseHandler) upsertDailyDataFromEtf(tx interface {
	Exec(string, ...interface{}) *gorm.DB
}, d EtfDailyData) error {
	upsertSQL := `
	    INSERT INTO daily_data (
	        symbol, stock_type, trading_date, name, latest_price, change_amount, change_percent,
	        buy, sell, prev_close, open, high, low, volume, turnover, turnover_rate
	    ) VALUES ($1, 2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, 0)
	    ON CONFLICT (symbol, stock_type) DO UPDATE SET
	        trading_date = EXCLUDED.trading_date,
	        name = EXCLUDED.name,
	        latest_price = EXCLUDED.latest_price,
	        change_amount = EXCLUDED.change_amount,
	        change_percent = EXCLUDED.change_percent,
	        buy = EXCLUDED.buy,
	        sell = EXCLUDED.sell,
	        prev_close = EXCLUDED.prev_close,
	        open = EXCLUDED.open,
	        high = EXCLUDED.high,
	        low = EXCLUDED.low,
	        volume = EXCLUDED.volume,
	        turnover = EXCLUDED.turnover,
	        turnover_rate = EXCLUDED.turnover_rate,
	        updated_at = NOW()
	    WHERE daily_data.trading_date <= EXCLUDED.trading_date`
	return tx.Exec(upsertSQL,
		normalizeStockSymbol(d.Code), d.TradingDate, d.Name, d.LatestPrice, d.ChangeAmount, d.ChangePercent,
		d.Buy, d.Sell, d.PrevClose, d.Open, d.High, d.Low, d.Volume, d.Turnover,
	).Error
}

func (h *DatabaseHandler) UpsertEtfDaily(data *EtfDailyData) error {
	tx := h.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %v", tx.Error)
	}
	query := `
	    INSERT INTO etf_daily (
	        code, trading_date, name, latest_price, change_amount, change_percent,
        buy, sell, prev_close, open, high, low, volume, turnover
    ) VALUES ($1, $2, $3, $4, $5, $6,
              $7, $8, $9, $10, $11, $12, $13, $14)
    ON CONFLICT (code, trading_date) DO UPDATE SET
        name = EXCLUDED.name,
        latest_price = EXCLUDED.latest_price,
        change_amount = EXCLUDED.change_amount,
        change_percent = EXCLUDED.change_percent,
        buy = EXCLUDED.buy,
        sell = EXCLUDED.sell,
        prev_close = EXCLUDED.prev_close,
        open = EXCLUDED.open,
	        high = EXCLUDED.high,
	        low = EXCLUDED.low,
	        volume = EXCLUDED.volume,
	        turnover = EXCLUDED.turnover`
	if err := tx.Exec(query,
		data.Code, data.TradingDate, data.Name, data.LatestPrice, data.ChangeAmount, data.ChangePercent,
		data.Buy, data.Sell, data.PrevClose, data.Open, data.High, data.Low, data.Volume, data.Turnover,
	).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := h.upsertDailyDataFromEtf(tx, *data); err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to upsert daily_data from etf_daily: %v", err)
	}
	return tx.Commit().Error
}

func (h *DatabaseHandler) BatchInsertMTFForecast(list []MTFForecast) error {
	if len(list) == 0 {
		return fmt.Errorf("empty list")
	}
	tx := h.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	insertSQL := `
        INSERT INTO mtf_forecast (
            symbol, ds, tsf, tsf_01, tsf_02, tsf_03, tsf_04, tsf_05, tsf_06, tsf_07, tsf_08, tsf_09,
            chunk_index, best_quantile, best_quantile_pct, best_pred_pct, actual_pct, diff_pct, mse, mae, combined_score,
            version, horizon_len
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
            $13, $14, $15, $16, $17, $18, $19, $20, $21,
            $22, $23
        )`
	for _, v := range list {
		if err := tx.Exec(insertSQL,
			v.Symbol, v.Ds, v.Tsf, v.Tsf01, v.Tsf02, v.Tsf03, v.Tsf04, v.Tsf05, v.Tsf06, v.Tsf07, v.Tsf08, v.Tsf09,
			v.ChunkIndex, v.BestQuantile, v.BestQuantilePct, v.BestPredPct, v.ActualPct, v.DiffPct, v.MSE, v.MAE, v.CombinedScore,
			v.Version, v.HorizonLen,
		).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

func (h *DatabaseHandler) BatchUpsertEtfDaily(dataList []EtfDailyData) error {
	tx := h.db.Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %v", tx.Error)
	}
	upsertSQL := `
    INSERT INTO etf_daily (
        code, trading_date, name, latest_price, change_amount, change_percent,
        buy, sell, prev_close, open, high, low, volume, turnover
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
    ON CONFLICT (code, trading_date) DO UPDATE SET
        name = EXCLUDED.name,
        latest_price = EXCLUDED.latest_price,
        change_amount = EXCLUDED.change_amount,
        change_percent = EXCLUDED.change_percent,
        buy = EXCLUDED.buy,
        sell = EXCLUDED.sell,
        prev_close = EXCLUDED.prev_close,
        open = EXCLUDED.open,
        high = EXCLUDED.high,
        low = EXCLUDED.low,
        volume = EXCLUDED.volume,
        turnover = EXCLUDED.turnover`
	for _, d := range dataList {
		if err := tx.Exec(upsertSQL,
			d.Code, d.TradingDate, d.Name, d.LatestPrice, d.ChangeAmount, d.ChangePercent,
			d.Buy, d.Sell, d.PrevClose, d.Open, d.High, d.Low, d.Volume, d.Turnover,
		).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute etf upsert batch: %v", err)
		}
		if err := h.upsertDailyDataFromEtf(tx, d); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to upsert daily_data from etf batch: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit etf upsert batch: %v", err)
	}
	return nil
}

func (h *DatabaseHandler) UpsertIndexInfo(info *IndexInfo) error {
	return h.db.Exec(`
    INSERT INTO index_info (code, display_name, publish_date)
    VALUES ($1, $2, $3)
    ON CONFLICT (code) DO UPDATE SET
        display_name = EXCLUDED.display_name,
        publish_date = EXCLUDED.publish_date`, info.Code, info.DisplayName, info.PublishDate).Error
}

func (h *DatabaseHandler) BatchUpsertIndexInfo(list []IndexInfo) error {
	tx := h.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	upsertSQL := `
        INSERT INTO index_info (code, display_name, publish_date)
        VALUES ($1, $2, $3)
        ON CONFLICT (code) DO UPDATE SET
            display_name = EXCLUDED.display_name,
            publish_date = EXCLUDED.publish_date`
	for _, v := range list {
		if err := tx.Exec(upsertSQL, v.Code, v.DisplayName, v.PublishDate).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("batch upsert index_info failed: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}

func (h *DatabaseHandler) UpsertIndexDaily(d *IndexDailyData) error {
	return h.db.Exec(`
    INSERT INTO index_daily (
        code, trading_date, open, close, high, low, volume, amount, change_percent
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    ON CONFLICT (code, trading_date) DO UPDATE SET
        open = EXCLUDED.open,
        close = EXCLUDED.close,
        high = EXCLUDED.high,
        low = EXCLUDED.low,
        volume = EXCLUDED.volume,
        amount = EXCLUDED.amount,
        change_percent = EXCLUDED.change_percent`,
		d.Code, d.TradingDate, d.Open, d.Close, d.High, d.Low, d.Volume, d.Amount, d.ChangePercent).Error
}

func (h *DatabaseHandler) BatchUpsertIndexDaily(list []IndexDailyData) error {
	tx := h.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	upsertSQL := `
        INSERT INTO index_daily (
            code, trading_date, open, close, high, low, volume, amount, change_percent
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (code, trading_date) DO UPDATE SET
            open = EXCLUDED.open,
            close = EXCLUDED.close,
            high = EXCLUDED.high,
            low = EXCLUDED.low,
            volume = EXCLUDED.volume,
            amount = EXCLUDED.amount,
            change_percent = EXCLUDED.change_percent`
	for _, v := range list {
		if err := tx.Exec(upsertSQL, v.Code, v.TradingDate, v.Open, v.Close, v.High, v.Low, v.Volume, v.Amount, v.ChangePercent).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("batch upsert index_daily failed: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}

func (h *DatabaseHandler) BatchUpsertAStockCommentDaily(list []StockCommentDaily) error {
	tx := h.db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	upsertSQL := `
        INSERT INTO a_stock_comment_daily (
            code, trading_date, name, latest_price, change_percent, turnover_rate,
            pe_ratio, main_cost, institution_participation, composite_score,
            rise, current_rank, attention_index
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
        ON CONFLICT (code, trading_date) DO UPDATE SET
            name = EXCLUDED.name,
            latest_price = EXCLUDED.latest_price,
            change_percent = EXCLUDED.change_percent,
            turnover_rate = EXCLUDED.turnover_rate,
            pe_ratio = EXCLUDED.pe_ratio,
            main_cost = EXCLUDED.main_cost,
            institution_participation = EXCLUDED.institution_participation,
            composite_score = EXCLUDED.composite_score,
            rise = EXCLUDED.rise,
            current_rank = EXCLUDED.current_rank,
            attention_index = EXCLUDED.attention_index`
	for _, v := range list {
		if err := tx.Exec(upsertSQL,
			v.Code, v.TradingDate, v.Name, v.LatestPrice, v.ChangePercent, v.TurnoverRate,
			v.PeRatio, v.MainCost, v.InstitutionParticipation, v.CompositeScore,
			v.Rise, v.CurrentRank, v.AttentionIndex,
		).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("batch upsert a_stock_comment_daily failed: %v", err)
		}
	}
	if err := tx.Commit().Error; err != nil {
		return err
	}
	return nil
}

func (h *DatabaseHandler) GetAStockCommentDailyByCode(code string, limit int, offset int) ([]StockCommentDaily, error) {
	rows, err := h.db.Raw(`
    SELECT 
        code,
        trading_date,
        COALESCE(name, ''),
        COALESCE(latest_price, 0),
        COALESCE(change_percent, 0),
        COALESCE(turnover_rate, 0),
        COALESCE(pe_ratio, 0),
        COALESCE(main_cost, 0),
        COALESCE(institution_participation, 0),
        COALESCE(composite_score, 0),
        COALESCE(rise, 0),
        COALESCE(current_rank, 0),
        COALESCE(attention_index, 0)
    FROM a_stock_comment_daily
    WHERE code ILIKE $1
    ORDER BY trading_date DESC
    LIMIT $2 OFFSET $3`, "%"+code+"%", limit, offset).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query a_stock_comment_daily by code: %v", err)
	}
	defer rows.Close()
	var result []StockCommentDaily
	for rows.Next() {
		var item StockCommentDaily
		if err := rows.Scan(
			&item.Code, &item.TradingDate, &item.Name, &item.LatestPrice,
			&item.ChangePercent, &item.TurnoverRate, &item.PeRatio, &item.MainCost,
			&item.InstitutionParticipation, &item.CompositeScore, &item.Rise,
			&item.CurrentRank, &item.AttentionIndex,
		); err != nil {
			return nil, fmt.Errorf("failed to scan a_stock_comment_daily row: %v", err)
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
func (h *DatabaseHandler) GetAStockCommentDailyByName(name string, limit int, offset int) ([]StockCommentDaily, error) {
	rows, err := h.db.Raw(`
    SELECT 
        code,
        trading_date,
        COALESCE(name, ''),
        COALESCE(latest_price, 0),
        COALESCE(change_percent, 0),
        COALESCE(turnover_rate, 0),
        COALESCE(pe_ratio, 0),
        COALESCE(main_cost, 0),
        COALESCE(institution_participation, 0),
        COALESCE(composite_score, 0),
        COALESCE(rise, 0),
        COALESCE(current_rank, 0),
        COALESCE(attention_index, 0)
    FROM a_stock_comment_daily
    WHERE name ILIKE $1
    ORDER BY trading_date DESC
    LIMIT $2 OFFSET $3`, "%"+name+"%", limit, offset).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query a_stock_comment_daily by name: %v", err)
	}
	defer rows.Close()
	var result []StockCommentDaily
	for rows.Next() {
		var item StockCommentDaily
		if err := rows.Scan(
			&item.Code, &item.TradingDate, &item.Name, &item.LatestPrice,
			&item.ChangePercent, &item.TurnoverRate, &item.PeRatio, &item.MainCost,
			&item.InstitutionParticipation, &item.CompositeScore, &item.Rise,
			&item.CurrentRank, &item.AttentionIndex,
		); err != nil {
			return nil, fmt.Errorf("failed to scan a_stock_comment_daily row: %v", err)
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (h *DatabaseHandler) GetStockData(symbol string, stockType int, limit int, offset int) ([]StockData, error) {
	normalizedSymbol := normalizeStockSymbol(symbol)
	rows, err := h.db.Raw(fmt.Sprintf(`
    SELECT id, datetime, open, close, high, low, volume, amount, amplitude,
           percentage_change, amount_change, turnover_rate, type, %s AS symbol, date_str,
           created_at, updated_at
    FROM stock_data
    WHERE %s = $1 AND type = $2
    ORDER BY datetime DESC
    LIMIT $3 OFFSET $4`, normalizedStockSymbolSQL("symbol"), normalizedStockSymbolSQL("symbol")), normalizedSymbol, stockType, limit, offset).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query stock data: %v", err)
	}
	defer rows.Close()
	var results []StockData
	for rows.Next() {
		var data StockData
		if err := rows.Scan(
			&data.ID, &data.Datetime, &data.Open, &data.Close, &data.High, &data.Low,
			&data.Volume, &data.Amount, &data.Amplitude, &data.PercentageChange,
			&data.AmountChange, &data.TurnoverRate, &data.Type, &data.Symbol, &data.DateStr,
			&data.CreatedAt, &data.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}
	return results, nil
}

func (h *DatabaseHandler) GetStockDataByDateRange(symbol string, stockType int, startDate, endDate time.Time) ([]StockData, error) {
	normalizedSymbol := normalizeStockSymbol(symbol)
	rows, err := h.db.Raw(fmt.Sprintf(`
    SELECT id, datetime, open, close, high, low, volume, amount, amplitude,
           percentage_change, amount_change, turnover_rate, type, %s AS symbol, date_str,
           created_at, updated_at
    FROM stock_data
    WHERE %s = $1 AND type = $2 AND datetime >= $3 AND datetime <= $4
    ORDER BY datetime ASC`, normalizedStockSymbolSQL("symbol"), normalizedStockSymbolSQL("symbol")), normalizedSymbol, stockType, startDate, endDate).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query stock data by date range: %v", err)
	}
	defer rows.Close()
	var results []StockData
	for rows.Next() {
		var data StockData
		if err := rows.Scan(
			&data.ID, &data.Datetime, &data.Open, &data.Close, &data.High, &data.Low,
			&data.Volume, &data.Amount, &data.Amplitude, &data.PercentageChange,
			&data.AmountChange, &data.TurnoverRate, &data.Type, &data.Symbol, &data.DateStr,
			&data.CreatedAt, &data.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		results = append(results, data)
	}
	return results, nil
}

func (h *DatabaseHandler) ListStockSymbols() ([]StockSymbolMeta, error) {
	normalizedSymbolExpr := normalizedStockSymbolSQL("symbol")
	rows, err := h.db.Raw(fmt.Sprintf(`
    SELECT normalized_symbol AS symbol, type AS stock_type, COALESCE(MAX(date_str), '') AS latest_date
    FROM (
        SELECT %s AS normalized_symbol, type, date_str
        FROM stock_data
        UNION ALL
        SELECT %s AS normalized_symbol, stock_type AS type, '' AS date_str
        FROM user_watchlist
        WHERE symbol IS NOT NULL AND trim(symbol) <> '' AND stock_type > 0
    ) normalized_rows
    GROUP BY normalized_symbol, type
    ORDER BY type ASC, normalized_symbol ASC`, normalizedSymbolExpr, normalizedSymbolExpr)).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to list stock symbols: %v", err)
	}
	defer rows.Close()

	result := make([]StockSymbolMeta, 0)
	for rows.Next() {
		var item StockSymbolMeta
		if err := rows.Scan(&item.Symbol, &item.StockType, &item.LatestDate); err != nil {
			return nil, fmt.Errorf("failed to scan stock symbol row: %v", err)
		}
		if name, err := h.LookupInstrumentNameFromDB(item.Symbol, item.StockType); err == nil && strings.TrimSpace(name) != "" {
			item.Name = strings.TrimSpace(name)
		}
		result = append(result, item)
	}
	return result, nil
}

func (h *DatabaseHandler) GetEtfDaily(code string, limit int, offset int) ([]EtfDailyData, error) {
	rows, err := h.db.Raw(`
    SELECT code, trading_date, name, latest_price, change_amount, change_percent,
           buy, sell, prev_close, open, high, low, volume, turnover
    FROM etf_daily
    WHERE code = $1
    ORDER BY trading_date DESC
    LIMIT $2 OFFSET $3`, code, limit, offset).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query etf_daily: %v", err)
	}
	defer rows.Close()
	var results []EtfDailyData
	for rows.Next() {
		var d EtfDailyData
		if err := rows.Scan(
			&d.Code, &d.TradingDate, &d.Name, &d.LatestPrice, &d.ChangeAmount, &d.ChangePercent,
			&d.Buy, &d.Sell, &d.PrevClose, &d.Open, &d.High, &d.Low, &d.Volume, &d.Turnover,
		); err != nil {
			return nil, fmt.Errorf("failed to scan etf_daily row: %v", err)
		}
		results = append(results, d)
	}
	return results, nil
}

func (h *DatabaseHandler) GetEtfDailyByDateRange(code string, startDate, endDate time.Time) ([]EtfDailyData, error) {
	rows, err := h.db.Raw(`
    SELECT code, trading_date, name, latest_price, change_amount, change_percent,
           buy, sell, prev_close, open, high, low, volume, turnover
    FROM etf_daily
    WHERE code = $1 AND trading_date >= $2 AND trading_date <= $3
    ORDER BY trading_date ASC`, code, startDate, endDate).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query etf_daily by date range: %v", err)
	}
	defer rows.Close()
	var results []EtfDailyData
	for rows.Next() {
		var d EtfDailyData
		if err := rows.Scan(
			&d.Code, &d.TradingDate, &d.Name, &d.LatestPrice, &d.ChangeAmount, &d.ChangePercent,
			&d.Buy, &d.Sell, &d.PrevClose, &d.Open, &d.High, &d.Low, &d.Volume, &d.Turnover,
		); err != nil {
			return nil, fmt.Errorf("failed to scan etf_daily row: %v", err)
		}
		results = append(results, d)
	}
	return results, nil
}

func (h *DatabaseHandler) GetIndexDaily(code string, limit int, offset int) ([]IndexDailyData, error) {
	rows, err := h.db.Raw(`
    SELECT code, trading_date, open, close, high, low, volume, amount, change_percent
    FROM index_daily
    WHERE code = $1
    ORDER BY trading_date DESC
    LIMIT $2 OFFSET $3`, code, limit, offset).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query index_daily: %v", err)
	}
	defer rows.Close()
	var result []IndexDailyData
	for rows.Next() {
		var item IndexDailyData
		if err := rows.Scan(&item.Code, &item.TradingDate, &item.Open, &item.Close, &item.High, &item.Low, &item.Volume, &item.Amount, &item.ChangePercent); err != nil {
			return nil, fmt.Errorf("failed to scan index_daily row: %v", err)
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func (h *DatabaseHandler) GetIndexDailyByDateRange(code string, startDate, endDate time.Time) ([]IndexDailyData, error) {
	rows, err := h.db.Raw(`
    SELECT code, trading_date, open, close, high, low, volume, amount, change_percent
    FROM index_daily
    WHERE code = $1 AND trading_date BETWEEN $2 AND $3
    ORDER BY trading_date ASC`, code, startDate, endDate).Rows()
	if err != nil {
		return nil, fmt.Errorf("failed to query index_daily by date range: %v", err)
	}
	defer rows.Close()
	var result []IndexDailyData
	for rows.Next() {
		var item IndexDailyData
		if err := rows.Scan(&item.Code, &item.TradingDate, &item.Open, &item.Close, &item.High, &item.Low, &item.Volume, &item.Amount, &item.ChangePercent); err != nil {
			return nil, fmt.Errorf("failed to scan index_daily row: %v", err)
		}
		result = append(result, item)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

// SaveLlmTokenUsage saves a new LLM token usage record
func (h *DatabaseHandler) SaveLlmTokenUsage(usage *LlmTokenUsage) error {
	result := h.db.Create(usage)
	if result.Error != nil {
		return fmt.Errorf("failed to save LLM token usage: %v", result.Error)
	}
	return nil
}

// GetLlmTokenUsageByUser retrieves LLM token usage records for a user with optional filters
func (h *DatabaseHandler) GetLlmTokenUsageByUser(userID int, limit, offset int, startDate, endDate *time.Time, provider, model *string) ([]LlmTokenUsage, error) {
	query := h.db.Where("user_id = ?", userID)

	if startDate != nil {
		query = query.Where("request_time >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("request_time <= ?", *endDate)
	}
	if provider != nil && *provider != "" {
		query = query.Where("provider = ?", *provider)
	}
	if model != nil && *model != "" {
		query = query.Where("model = ?", *model)
	}

	var records []LlmTokenUsage
	result := query.Order("request_time DESC").Limit(limit).Offset(offset).Find(&records)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to query LLM token usage: %v", result.Error)
	}

	return records, nil
}

// LlmTokenUsageStats represents token usage statistics
type LlmTokenUsageStats struct {
	TotalTokens           int                      `json:"total_tokens"`
	TotalPromptTokens     int                      `json:"total_prompt_tokens"`
	TotalCompletionTokens int                      `json:"total_completion_tokens"`
	RequestCount          int                      `json:"request_count"`
	ByProvider            map[string]ProviderStats `json:"by_provider"`
}

type ProviderStats struct {
	TotalTokens int            `json:"total_tokens"`
	Models      map[string]int `json:"models"`
}

// GetLlmTokenUsageStats retrieves aggregated token usage statistics for a user
func (h *DatabaseHandler) GetLlmTokenUsageStats(userID int, startDate, endDate *time.Time) (*LlmTokenUsageStats, error) {
	query := h.db.Model(&LlmTokenUsage{}).Where("user_id = ?", userID)

	if startDate != nil {
		query = query.Where("request_time >= ?", *startDate)
	}
	if endDate != nil {
		query = query.Where("request_time <= ?", *endDate)
	}

	// Get total statistics
	var totalStats struct {
		TotalTokens           int
		TotalPromptTokens     int
		TotalCompletionTokens int
		RequestCount          int
	}

	err := query.Select(
		"COALESCE(SUM(total_tokens), 0) as total_tokens",
		"COALESCE(SUM(prompt_tokens), 0) as total_prompt_tokens",
		"COALESCE(SUM(completion_tokens), 0) as total_completion_tokens",
		"COUNT(*) as request_count",
	).Scan(&totalStats).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get total stats: %v", err)
	}

	// Get provider and model breakdown
	var records []LlmTokenUsage
	if err := query.Select("provider", "model", "total_tokens").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("failed to get provider breakdown: %v", err)
	}

	byProvider := make(map[string]ProviderStats)
	for _, record := range records {
		providerStats, exists := byProvider[record.Provider]
		if !exists {
			providerStats = ProviderStats{
				TotalTokens: 0,
				Models:      make(map[string]int),
			}
		}
		providerStats.TotalTokens += record.TotalTokens
		providerStats.Models[record.Model] += record.TotalTokens
		byProvider[record.Provider] = providerStats
	}

	stats := &LlmTokenUsageStats{
		TotalTokens:           totalStats.TotalTokens,
		TotalPromptTokens:     totalStats.TotalPromptTokens,
		TotalCompletionTokens: totalStats.TotalCompletionTokens,
		RequestCount:          totalStats.RequestCount,
		ByProvider:            byProvider,
	}

	return stats, nil
}
