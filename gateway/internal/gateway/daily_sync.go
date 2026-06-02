package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type DailyStockSyncer struct {
	client          *http.Client
	postgresBaseURL string
	historyBaseURL  string
	apiToken        string
	location        *time.Location
	scheduleHour    int
	scheduleMinute  int
	maxConcurrency  int
	lookbackDays    int
}

type stockSymbolMeta struct {
	Symbol     string `json:"symbol"`
	StockType  int    `json:"stock_type"`
	LatestDate string `json:"latest_date"`
}

type apiResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type tradingDayResponse struct {
	Code         int    `json:"code"`
	Date         string `json:"date"`
	IsTradingDay bool   `json:"is_trading_day"`
}

type historyResponse struct {
	Code      int             `json:"code"`
	Symbol    string          `json:"symbol"`
	StockType int             `json:"stock_type"`
	Provider  string          `json:"provider"`
	Rows      int             `json:"rows"`
	Data      []historyRecord `json:"data"`
}

type historyRecord struct {
	Datetime         string  `json:"datetime"`
	Open             float64 `json:"open"`
	Close            float64 `json:"close"`
	High             float64 `json:"high"`
	Low              float64 `json:"low"`
	Volume           int64   `json:"volume"`
	Amount           float64 `json:"amount"`
	Amplitude        float64 `json:"amplitude"`
	PercentageChange float64 `json:"percentage_change"`
	AmountChange     float64 `json:"amount_change"`
	TurnoverRate     float64 `json:"turnover_rate"`
}

type stockBatchRecord struct {
	Datetime         string  `json:"datetime"`
	Open             float64 `json:"open"`
	Close            float64 `json:"close"`
	High             float64 `json:"high"`
	Low              float64 `json:"low"`
	Volume           int64   `json:"volume"`
	Amount           float64 `json:"amount"`
	Amplitude        float64 `json:"amplitude"`
	PercentageChange float64 `json:"percentage_change"`
	AmountChange     float64 `json:"amount_change"`
	TurnoverRate     float64 `json:"turnover_rate"`
	Type             int     `json:"type"`
	Symbol           string  `json:"symbol"`
	DateStr          string  `json:"date_str"`
}

type syncRunSummary struct {
	TradingDay      bool
	TargetDate      string
	TotalSymbols    int
	EligibleSymbols int
	SyncedSymbols   int
	SkippedSymbols  int
	FailedSymbols   int
	InsertedRows    int
}

func normalizeStorageSymbol(symbol string) string {
	raw := strings.TrimSpace(symbol)
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"sh", "sz", "bj"} {
		if strings.HasPrefix(lower, prefix) && len(raw) > len(prefix) {
			return raw[len(prefix):]
		}
	}
	return raw
}

func parseSymbolLatestDate(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", "20060102", time.RFC3339, time.RFC3339Nano} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	if idx := strings.Index(value, "T"); idx > 0 {
		parsed, err := time.Parse("2006-01-02", value[:idx])
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func compactDate(t time.Time) string {
	return t.Format("20060102")
}

func dashedDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func historyRecordDate(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if idx := strings.Index(value, "T"); idx >= 0 {
		value = value[:idx]
	}
	if len(value) >= len("2006-01-02") {
		return value[:len("2006-01-02")]
	}
	return value
}

func NewDailyStockSyncer(
	postgresBaseURL string,
	historyBaseURL string,
	apiToken string,
	location *time.Location,
	scheduleHour int,
	scheduleMinute int,
	maxConcurrency int,
	lookbackDays int,
) *DailyStockSyncer {
	if location == nil {
		location = time.Local
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}
	if lookbackDays < 0 {
		lookbackDays = 0
	}
	return &DailyStockSyncer{
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
		postgresBaseURL: strings.TrimRight(postgresBaseURL, "/"),
		historyBaseURL:  strings.TrimRight(historyBaseURL, "/"),
		apiToken:        apiToken,
		location:        location,
		scheduleHour:    scheduleHour,
		scheduleMinute:  scheduleMinute,
		maxConcurrency:  maxConcurrency,
		lookbackDays:    lookbackDays,
	}
}

func (s *DailyStockSyncer) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *DailyStockSyncer) loop(ctx context.Context) {
	for {
		now := time.Now().In(s.location)
		nextRun := s.nextRunAfter(now)
		wait := time.Until(nextRun)
		log.Printf("daily stock sync scheduled at %s", nextRun.Format(time.RFC3339))

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if _, err := s.RunOnce(ctx); err != nil {
			log.Printf("daily stock sync run failed: %v", err)
		}
	}
}

func (s *DailyStockSyncer) RunOnce(ctx context.Context) (*syncRunSummary, error) {
	now := time.Now().In(s.location)
	targetDate := now.Format("20060102")
	targetDateDashed := now.Format("2006-01-02")

	isTradingDay, err := s.isTradingDay(ctx, targetDate)
	if err != nil {
		return nil, err
	}

	summary := &syncRunSummary{
		TradingDay: false,
		TargetDate: targetDateDashed,
	}
	if !isTradingDay {
		log.Printf("daily stock sync skipped: %s is not a trading day", targetDateDashed)
		return summary, nil
	}
	summary.TradingDay = true

	symbols, err := s.listStockSymbols(ctx)
	if err != nil {
		return nil, err
	}
	summary.TotalSymbols = len(symbols)

	eligible := make([]stockSymbolMeta, 0, len(symbols))
	for _, item := range symbols {
		if strings.TrimSpace(item.Symbol) == "" || item.StockType <= 0 {
			continue
		}
		if strings.TrimSpace(item.LatestDate) == targetDateDashed {
			summary.SkippedSymbols++
			continue
		}
		eligible = append(eligible, item)
	}
	summary.EligibleSymbols = len(eligible)
	if len(eligible) == 0 {
		log.Printf("daily stock sync skipped: no eligible symbols for %s", targetDateDashed)
		return summary, nil
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, s.maxConcurrency)
	results := make(chan struct {
		rows int
		err  error
	}, len(eligible))

	for _, item := range eligible {
		wg.Add(1)
		go func(meta stockSymbolMeta) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				results <- struct {
					rows int
					err  error
				}{err: ctx.Err()}
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			rows, err := s.syncSymbolRange(ctx, meta, targetDate, targetDateDashed)
			results <- struct {
				rows int
				err  error
			}{rows: rows, err: err}
		}(item)
	}

	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			summary.FailedSymbols++
			log.Printf("daily stock sync symbol failed: %v", result.err)
			continue
		}
		if result.rows > 0 {
			summary.SyncedSymbols++
			summary.InsertedRows += result.rows
		} else {
			summary.SkippedSymbols++
		}
	}

	log.Printf(
		"daily stock sync finished: date=%s trading_day=%t total=%d eligible=%d synced=%d skipped=%d failed=%d rows=%d",
		summary.TargetDate,
		summary.TradingDay,
		summary.TotalSymbols,
		summary.EligibleSymbols,
		summary.SyncedSymbols,
		summary.SkippedSymbols,
		summary.FailedSymbols,
		summary.InsertedRows,
	)
	return summary, nil
}

func (s *DailyStockSyncer) nextRunAfter(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), s.scheduleHour, s.scheduleMinute, 0, 0, s.location)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func (s *DailyStockSyncer) isTradingDay(ctx context.Context, date string) (bool, error) {
	url := s.historyBaseURL + "/api/v1/trading-day?date=" + date
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("trading day check failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed tradingDayResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, err
	}
	return parsed.IsTradingDay, nil
}

func (s *DailyStockSyncer) listStockSymbols(ctx context.Context) ([]stockSymbolMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.postgresBaseURL+"/api/v1/stock-data/symbols", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Token", s.apiToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list stock symbols failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var wrapped apiResponse
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, err
	}
	var result []stockSymbolMeta
	if len(wrapped.Data) == 0 {
		return result, nil
	}
	if err := json.Unmarshal(wrapped.Data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *DailyStockSyncer) syncSymbolRange(ctx context.Context, meta stockSymbolMeta, targetDate string, targetDateDashed string) (int, error) {
	startDate := targetDate
	startDateDashed := targetDateDashed
	if latestDate, ok := parseSymbolLatestDate(meta.LatestDate); ok {
		nextDate := latestDate.In(s.location).AddDate(0, 0, 1)
		startDate = compactDate(nextDate)
		startDateDashed = dashedDate(nextDate)
	}
	if s.lookbackDays > 0 {
		target, err := time.ParseInLocation("2006-01-02", targetDateDashed, s.location)
		if err == nil {
			lookbackStart := target.AddDate(0, 0, -s.lookbackDays)
			lookbackStartDashed := dashedDate(lookbackStart)
			if lookbackStartDashed < startDateDashed {
				startDate = compactDate(lookbackStart)
				startDateDashed = lookbackStartDashed
			}
		}
	}
	if startDateDashed > targetDateDashed {
		return 0, nil
	}

	payload := map[string]any{
		"symbol":     meta.Symbol,
		"stock_type": meta.StockType,
		"start_date": startDate,
		"end_date":   targetDate,
		"adjust":     "forward_additive",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.historyBaseURL+"/api/v1/history", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("history fetch failed: symbol=%s stock_type=%d range=%s~%s err=%w", meta.Symbol, meta.StockType, startDate, targetDate, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("history fetch failed: symbol=%s stock_type=%d range=%s~%s status=%d body=%s", meta.Symbol, meta.StockType, startDate, targetDate, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var history historyResponse
	if err := json.Unmarshal(respBody, &history); err != nil {
		return 0, err
	}

	records := make([]stockBatchRecord, 0, len(history.Data))
	storageSymbol := normalizeStorageSymbol(meta.Symbol)
	for _, item := range history.Data {
		dateStr := historyRecordDate(item.Datetime)
		if dateStr < startDateDashed || dateStr > targetDateDashed {
			continue
		}
		records = append(records, stockBatchRecord{
			Datetime:         item.Datetime,
			Open:             item.Open,
			Close:            item.Close,
			High:             item.High,
			Low:              item.Low,
			Volume:           item.Volume,
			Amount:           item.Amount,
			Amplitude:        item.Amplitude,
			PercentageChange: item.PercentageChange,
			AmountChange:     item.AmountChange,
			TurnoverRate:     item.TurnoverRate,
			Type:             meta.StockType,
			Symbol:           storageSymbol,
			DateStr:          dateStr,
		})
	}
	if len(records) == 0 {
		return 0, nil
	}

	insertBody, err := json.Marshal(records)
	if err != nil {
		return 0, err
	}
	insertReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.postgresBaseURL+"/api/v1/stock-data/batch", bytes.NewReader(insertBody))
	if err != nil {
		return 0, err
	}
	insertReq.Header.Set("Content-Type", "application/json")
	insertReq.Header.Set("X-Token", s.apiToken)

	insertResp, err := s.client.Do(insertReq)
	if err != nil {
		return 0, fmt.Errorf("stock batch insert failed: symbol=%s stock_type=%d range=%s~%s err=%w", meta.Symbol, meta.StockType, startDate, targetDate, err)
	}
	defer insertResp.Body.Close()

	insertRespBody, err := io.ReadAll(insertResp.Body)
	if err != nil {
		return 0, err
	}
	if insertResp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("stock batch insert failed: symbol=%s stock_type=%d range=%s~%s status=%d body=%s", meta.Symbol, meta.StockType, startDate, targetDate, insertResp.StatusCode, strings.TrimSpace(string(insertRespBody)))
	}
	return len(records), nil
}
