package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/gin-gonic/gin"
)

const (
	tickflowProviderName        = "tickflow-go"
	eastmoneyProviderName       = "eastmoney-push2his"
	baiduProviderName           = "baidu-gushitong"
	defaultTickflowAdjust       = "forward_additive"
	defaultTickflowBaseURL      = "https://api.tickflow.org"
	defaultTickflowFreeBaseURL  = "https://free-api.tickflow.org"
	defaultEastmoneyBaseURL     = "http://push2his.eastmoney.com"
	defaultBaiduBaseURL         = "https://finance.pae.baidu.com"
	maxTickflowKlinePageSize    = 10000
	defaultTickflowHTTPTimeout  = 30 * time.Second
	defaultTickflowTradingCheck = "000001.SH"
	defaultHistoryProviderOrder = "eastmoney,baidu,tickflow"
)

type TickflowHistoryRequest struct {
	Symbol    string      `json:"symbol"`
	StockType interface{} `json:"stock_type"`
	StartDate string      `json:"start_date"`
	EndDate   string      `json:"end_date"`
	Adjust    string      `json:"adjust"`
}

type TickflowHistoryResponse struct {
	Code      int                    `json:"code"`
	Symbol    string                 `json:"symbol"`
	StockType int                    `json:"stock_type"`
	Provider  string                 `json:"provider"`
	Rows      int                    `json:"rows"`
	Data      []TickflowMarketRecord `json:"data"`
}

type TickflowMarketRecord struct {
	Datetime         string  `json:"datetime"`
	DatetimeInt      int64   `json:"datetime_int"`
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
	TradeDate        string  `json:"trade_date,omitempty"`
	DateStr          string  `json:"date_str,omitempty"`
	Symbol           string  `json:"symbol,omitempty"`
	Name             string  `json:"name,omitempty"`
	Timestamp        int64   `json:"timestamp,omitempty"`
}

type tickflowKlinesResponse struct {
	Data tickflowCompactKlineData `json:"data"`
}

type tickflowCompactKlineData struct {
	Timestamp []int64   `json:"timestamp"`
	Open      []float64 `json:"open"`
	High      []float64 `json:"high"`
	Low       []float64 `json:"low"`
	Close     []float64 `json:"close"`
	Volume    []int64   `json:"volume"`
	Amount    []float64 `json:"amount"`
	PrevClose []float64 `json:"prev_close"`
}

type tickflowInstrumentsResponse struct {
	Data []struct {
		Symbol string `json:"symbol"`
		Name   string `json:"name"`
	} `json:"data"`
}

type tickflowAPIError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Details any    `json:"details,omitempty"`
}

type eastmoneyKlineResponse struct {
	Data *struct {
		Code   string   `json:"code"`
		Market int      `json:"market"`
		Name   string   `json:"name"`
		Klines []string `json:"klines"`
	} `json:"data"`
}

type baiduKlineResponse struct {
	Result struct {
		NewMarketData struct {
			MarketData string `json:"marketData"`
		} `json:"newMarketData"`
	} `json:"Result"`
}

func (h *DatabaseHandler) tickflowHistoryHandler(c *gin.Context) {
	var req TickflowHistoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_JSON", "message": err.Error()})
		return
	}

	records, stockType, provider, err := h.LoadTickflowHistory(c.Request.Context(), req)
	if err != nil {
		log.Printf("market history fetch failed: symbol=%s stock_type=%v err=%v", req.Symbol, req.StockType, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": "HISTORY_FETCH_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, TickflowHistoryResponse{
		Code:      200,
		Symbol:    req.Symbol,
		StockType: stockType,
		Provider:  provider,
		Rows:      len(records),
		Data:      records,
	})
}

func (h *DatabaseHandler) tickflowTradingDayHandler(c *gin.Context) {
	location := shanghaiLocation()
	target, err := parseTickflowDate(c.Query("date"), location)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_DATE", "message": err.Error()})
		return
	}

	records, _, provider, err := h.LoadTickflowHistory(c.Request.Context(), TickflowHistoryRequest{
		Symbol:    defaultTickflowTradingCheck,
		StockType: 1,
		StartDate: target.Format("2006-01-02"),
		EndDate:   target.Format("2006-01-02"),
		Adjust:    "none",
	})
	isTradingDay := len(records) > 0
	if err != nil {
		log.Printf("market trading-day check failed, using weekday fallback: date=%s err=%v", target.Format("2006-01-02"), err)
		weekday := target.Weekday()
		isTradingDay = weekday != time.Saturday && weekday != time.Sunday
	} else {
		log.Printf("market trading-day check: date=%s provider=%s is_trading_day=%v", target.Format("2006-01-02"), provider, isTradingDay)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":           200,
		"date":           target.Format("2006-01-02"),
		"is_trading_day": isTradingDay,
	})
}

func (h *DatabaseHandler) tickflowInstrumentsHandler(c *gin.Context) {
	rawSymbols := strings.TrimSpace(c.Query("symbols"))
	if rawSymbols == "" {
		rawSymbols = strings.TrimSpace(c.Query("symbol"))
	}
	if rawSymbols == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "MISSING_SYMBOLS", "message": "symbols is required"})
		return
	}

	items := make([]gin.H, 0)
	for _, raw := range strings.Split(rawSymbols, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		stockType := inferStockTypeFromSymbol(raw)
		symbol := toTickflowSymbol(raw, stockType)
		name := h.lookupInstrumentName(c.Request.Context(), symbol, stockType)
		items = append(items, gin.H{
			"symbol":     symbol,
			"name":       name,
			"stock_type": stockType,
		})
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "provider": tickflowProviderName, "data": items})
}

func (h *DatabaseHandler) LoadTickflowHistory(ctx context.Context, req TickflowHistoryRequest) ([]TickflowMarketRecord, int, string, error) {
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		return nil, 0, "", errors.New("symbol is required")
	}
	stockType, err := normalizeTickflowStockType(req.StockType)
	if err != nil {
		return nil, 0, "", err
	}
	location := shanghaiLocation()
	startDate, err := parseTickflowDate(req.StartDate, location)
	if err != nil {
		return nil, 0, "", fmt.Errorf("invalid start_date: %w", err)
	}
	endDate, err := parseTickflowDate(req.EndDate, location)
	if err != nil {
		return nil, 0, "", fmt.Errorf("invalid end_date: %w", err)
	}
	if startDate.After(endDate) {
		return nil, 0, "", errors.New("start_date must be before or equal to end_date")
	}

	tickflowSymbol := toTickflowSymbol(symbol, stockType)
	adjust := normalizeTickflowAdjust(firstNonEmpty(req.Adjust, getEnv("TICKFLOW_DEFAULT_ADJUST", defaultTickflowAdjust)))
	var providerErrors []string
	for _, provider := range historyProviderOrder() {
		var records []TickflowMarketRecord
		var fetchErr error
		switch provider {
		case eastmoneyProviderName, "eastmoney":
			records, fetchErr = fetchEastmoneyKlines(ctx, tickflowSymbol, stockType, startDate, endDate, adjust)
		case baiduProviderName, "baidu":
			records, fetchErr = fetchBaiduKlines(ctx, tickflowSymbol, stockType, startDate, endDate)
		case tickflowProviderName, "tickflow":
			var klines tickflowCompactKlineData
			klines, fetchErr = fetchTickflowKlines(ctx, tickflowSymbol, startDate, endDate, adjust)
			if fetchErr == nil {
				name := h.lookupInstrumentName(ctx, tickflowSymbol, stockType)
				records = normalizeTickflowKlines(klines, location, tickflowSymbol, name)
			}
		default:
			continue
		}
		if fetchErr != nil {
			providerErrors = append(providerErrors, fmt.Sprintf("%s: %v", provider, fetchErr))
			log.Printf("market history provider failed: provider=%s symbol=%s err=%v", provider, tickflowSymbol, fetchErr)
			continue
		}
		if len(records) == 0 {
			providerErrors = append(providerErrors, fmt.Sprintf("%s: no rows", provider))
			continue
		}
		return records, stockType, canonicalHistoryProviderName(provider), nil
	}
	return nil, 0, "", fmt.Errorf("all history providers failed: %s", strings.Join(providerErrors, "; "))
}

func (h *DatabaseHandler) hasTickflowDailyKline(ctx context.Context, symbol string, startDate time.Time, endDate time.Time, adjust string) (bool, error) {
	klines, err := fetchTickflowKlines(ctx, symbol, startDate, endDate, adjust)
	if err != nil {
		return false, err
	}
	return len(klines.Timestamp) > 0, nil
}

func fetchTickflowKlines(ctx context.Context, symbol string, startDate time.Time, endDate time.Time, adjust string) (tickflowCompactKlineData, error) {
	endInclusive := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 999000000, endDate.Location())
	values := url.Values{}
	values.Set("symbol", symbol)
	values.Set("period", "1d")
	values.Set("count", strconv.Itoa(maxTickflowKlinePageSize))
	values.Set("start_time", strconv.FormatInt(startDate.UnixMilli(), 10))
	values.Set("end_time", strconv.FormatInt(endInclusive.UnixMilli(), 10))
	values.Set("adjust", normalizeTickflowAdjust(adjust))

	endpoint := tickflowBaseURL() + "/v1/klines?" + values.Encode()
	body, err := tickflowGet(ctx, endpoint)
	if err != nil {
		return tickflowCompactKlineData{}, err
	}
	var parsed tickflowKlinesResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return tickflowCompactKlineData{}, err
	}
	return parsed.Data, nil
}

func fetchTickflowInstrumentName(ctx context.Context, symbol string) (string, error) {
	values := url.Values{}
	values.Set("symbols", symbol)
	body, err := tickflowGet(ctx, tickflowBaseURL()+"/v1/instruments?"+values.Encode())
	if err != nil {
		return "", err
	}
	var parsed tickflowInstrumentsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	for _, item := range parsed.Data {
		if strings.EqualFold(item.Symbol, symbol) {
			return strings.TrimSpace(item.Name), nil
		}
	}
	return "", nil
}

func tickflowGet(ctx context.Context, endpoint string) ([]byte, error) {
	return marketHTTPGet(ctx, endpoint, nil)
}

func marketHTTPGet(ctx context.Context, endpoint string, headers map[string]string) ([]byte, error) {
	client := &http.Client{Timeout: tickflowTimeout()}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if strings.Contains(endpoint, "tickflow.org") {
		if apiKey := strings.TrimSpace(getEnv("TICKFLOW_API_KEY", "tk_70e27ad14ba64397811ab0773e3a6c2e")); apiKey != "" {
			request.Header.Set("x-api-key", apiKey)
		}
	}
	if apiKey := strings.TrimSpace(request.Header.Get("x-api-key")); apiKey != "" {
		request.Header.Set("x-api-key", apiKey)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var parsed tickflowAPIError
		_ = json.Unmarshal(body, &parsed)
		message := strings.TrimSpace(parsed.Message)
		if message == "" {
			message = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("tickflow request failed: status=%d message=%s", response.StatusCode, message)
	}
	return body, nil
}

func fetchEastmoneyKlines(ctx context.Context, symbol string, stockType int, startDate time.Time, endDate time.Time, adjust string) ([]TickflowMarketRecord, error) {
	secid, err := eastmoneySecID(symbol, stockType)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	values.Set("secid", secid)
	values.Set("fields1", "f1,f2,f3,f4,f5,f6")
	values.Set("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61")
	values.Set("klt", "101")
	values.Set("fqt", eastmoneyFQT(adjust))
	values.Set("beg", startDate.Format("20060102"))
	values.Set("end", endDate.Format("20060102"))

	body, err := marketHTTPGet(ctx, eastmoneyBaseURL()+"/api/qt/stock/kline/get?"+values.Encode(), map[string]string{
		"Referer":    "https://quote.eastmoney.com/",
		"User-Agent": "Mozilla/5.0",
	})
	if err != nil {
		return nil, err
	}
	var parsed eastmoneyKlineResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Data == nil {
		return nil, errors.New("eastmoney returned empty data")
	}
	return normalizeEastmoneyKlines(parsed.Data.Klines, parsed.Data.Name, symbol)
}

func fetchBaiduKlines(ctx context.Context, symbol string, stockType int, startDate time.Time, endDate time.Time) ([]TickflowMarketRecord, error) {
	values := url.Values{}
	values.Set("all", "1")
	values.Set("isIndex", strconv.FormatBool(stockType == 3))
	values.Set("isBk", "false")
	values.Set("isBlock", "false")
	values.Set("isFutures", "false")
	values.Set("isStock", strconv.FormatBool(stockType != 3))
	values.Set("newFormat", "1")
	values.Set("group", "quotation_kline_ab")
	values.Set("finClientType", "pc")
	values.Set("code", digitsOnly(symbol))
	values.Set("ktype", "1")

	body, err := marketHTTPGet(ctx, baiduBaseURL()+"/selfselect/getstockquotation?"+values.Encode(), map[string]string{
		"Accept":     "application/vnd.finance-web.v1+json",
		"Origin":     "https://gushitong.baidu.com",
		"Referer":    "https://gushitong.baidu.com/",
		"User-Agent": "Mozilla/5.0",
	})
	if err != nil {
		return nil, err
	}
	var parsed baiduKlineResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if strings.TrimSpace(parsed.Result.NewMarketData.MarketData) == "" {
		return nil, errors.New("baidu returned empty kline data")
	}
	return normalizeBaiduKlines(parsed.Result.NewMarketData.MarketData, symbol, startDate, endDate)
}

func (h *DatabaseHandler) lookupInstrumentName(ctx context.Context, symbol string, stockType int) string {
	if !strings.EqualFold(strings.TrimSpace(getEnv("TICKFLOW_DISABLE_DB_NAMES", "")), "true") &&
		strings.TrimSpace(getEnv("TICKFLOW_DISABLE_DB_NAMES", "")) != "1" {
		if name, err := h.LookupInstrumentNameFromDB(symbol, stockType); err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		} else if err != nil {
			log.Printf("tickflow metadata database lookup failed: symbol=%s stock_type=%d err=%v", symbol, stockType, err)
		}
	}
	if name, err := fetchTickflowInstrumentName(ctx, symbol); err == nil {
		return strings.TrimSpace(name)
	} else if err != nil {
		log.Printf("tickflow instrument metadata fetch failed: symbol=%s err=%v", symbol, err)
	}
	return ""
}

func (h *DatabaseHandler) LookupInstrumentNameFromDB(symbol string, stockType int) (string, error) {
	candidates, digits := symbolNameLookupCandidates(symbol)
	if len(candidates) == 0 {
		return "", nil
	}
	if stockType == 2 {
		return h.lookupNameFromTable("etf_daily", "code", "name", "trading_date", candidates, digits)
	}
	if stockType == 1 {
		name, err := h.lookupNameFromTable("a_stock_comment_daily", "code", "name", "trading_date", candidates, digits)
		if err != nil || name != "" {
			return name, err
		}
	}
	return h.lookupNameFromTable("stocks", "symbol", "company_name", "symbol", candidates, digits)
}

func (h *DatabaseHandler) lookupNameFromTable(table string, codeColumn string, nameColumn string, orderColumn string, candidates []string, digits string) (string, error) {
	placeholders := make([]string, 0, len(candidates))
	args := make([]interface{}, 0, len(candidates)+1)
	for _, candidate := range candidates {
		args = append(args, strings.ToLower(candidate))
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	args = append(args, digits)
	digitsPlaceholder := fmt.Sprintf("$%d", len(args))
	query := fmt.Sprintf(
		`SELECT COALESCE(%s, '')
		   FROM %s
		  WHERE lower(trim(%s)) IN (%s)
		     OR regexp_replace(lower(trim(%s)), '[^0-9]', '', 'g') = %s
		  ORDER BY %s DESC
		  LIMIT 1`,
		nameColumn,
		table,
		codeColumn,
		strings.Join(placeholders, ","),
		codeColumn,
		digitsPlaceholder,
		orderColumn,
	)
	row := h.db.Raw(query, args...).Row()
	var name string
	if err := row.Scan(&name); err != nil {
		return "", nil
	}
	return strings.TrimSpace(name), nil
}

func normalizeTickflowKlines(data tickflowCompactKlineData, location *time.Location, symbol string, name string) []TickflowMarketRecord {
	n := minLen(len(data.Timestamp), len(data.Open), len(data.High), len(data.Low), len(data.Close), len(data.Volume), len(data.Amount))
	records := make([]TickflowMarketRecord, 0, n)
	var previousClose float64
	for i := 0; i < n; i++ {
		ts := time.UnixMilli(data.Timestamp[i]).In(location)
		tradeDate := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, time.UTC)
		currentClose := data.Close[i]
		prevClose := previousClose
		if i < len(data.PrevClose) && data.PrevClose[i] > 0 {
			prevClose = data.PrevClose[i]
		}

		percentageChange := 0.0
		amountChange := 0.0
		amplitude := 0.0
		if prevClose > 0 {
			percentageChange = (currentClose/prevClose - 1) * 100
			amountChange = currentClose - prevClose
			amplitude = (data.High[i] - data.Low[i]) / prevClose * 100
		}

		records = append(records, TickflowMarketRecord{
			Datetime:         tradeDate.Format("2006-01-02T15:04:05Z"),
			DatetimeInt:      tradeDate.Unix(),
			Open:             finiteFloat(data.Open[i]),
			Close:            finiteFloat(currentClose),
			High:             finiteFloat(data.High[i]),
			Low:              finiteFloat(data.Low[i]),
			Volume:           data.Volume[i],
			Amount:           finiteFloat(data.Amount[i]),
			Amplitude:        finiteFloat(amplitude),
			PercentageChange: finiteFloat(percentageChange),
			AmountChange:     finiteFloat(amountChange),
			TurnoverRate:     0,
			TradeDate:        ts.Format("2006-01-02"),
			DateStr:          ts.Format("2006-01-02"),
			Symbol:           symbol,
			Name:             name,
			Timestamp:        data.Timestamp[i],
		})
		previousClose = currentClose
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Datetime < records[j].Datetime
	})
	return records
}

func toTickflowSymbol(symbol string, stockType int) string {
	raw := strings.TrimSpace(strings.ToUpper(symbol))
	raw = strings.TrimPrefix(raw, "INDEX_")
	if strings.Contains(raw, ".") {
		return raw
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "sh") || strings.HasPrefix(lower, "sz") || strings.HasPrefix(lower, "bj") {
		return raw[2:] + "." + raw[:2]
	}
	if stockType == 3 {
		if strings.HasPrefix(raw, "399") {
			return raw + ".SZ"
		}
		return raw + ".SH"
	}
	if strings.HasPrefix(raw, "5") || strings.HasPrefix(raw, "6") || strings.HasPrefix(raw, "9") {
		return raw + ".SH"
	}
	if strings.HasPrefix(raw, "0") || strings.HasPrefix(raw, "1") || strings.HasPrefix(raw, "2") || strings.HasPrefix(raw, "3") {
		return raw + ".SZ"
	}
	return raw
}

func normalizeTickflowStockType(value interface{}) (int, error) {
	switch v := value.(type) {
	case nil:
		return 1, nil
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		raw := strings.TrimSpace(strings.ToLower(v))
		switch raw {
		case "", "stock", "stocks", "a", "equity":
			return 1, nil
		case "2", "fund", "funds", "etf":
			return 2, nil
		case "3", "index", "indices":
			return 3, nil
		default:
			n, err := strconv.Atoi(raw)
			if err != nil {
				return 0, fmt.Errorf("invalid stock_type: %v", value)
			}
			return n, nil
		}
	default:
		return 0, fmt.Errorf("invalid stock_type: %v", value)
	}
}

func inferStockTypeFromSymbol(symbol string) int {
	digits := digitsOnly(symbol)
	if strings.HasPrefix(digits, "5") || strings.HasPrefix(digits, "15") || strings.HasPrefix(digits, "16") || strings.HasPrefix(digits, "18") {
		return 2
	}
	return 1
}

func normalizeTickflowAdjust(adjust string) string {
	raw := strings.TrimSpace(strings.ToLower(adjust))
	switch raw {
	case "forward", "backward", "forward_additive", "backward_additive", "none":
		return raw
	case "qfq":
		return "forward_additive"
	case "hfq":
		return "backward_additive"
	case "":
		return defaultTickflowAdjust
	default:
		return "none"
	}
}

func parseTickflowDate(value string, location *time.Location) (time.Time, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		now := time.Now().In(location)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location), nil
	}
	for _, layout := range []string{"20060102", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, raw, location); err == nil {
			return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, location), nil
		}
	}
	return time.Time{}, fmt.Errorf("expected YYYYMMDD or YYYY-MM-DD, got %q", value)
}

func symbolNameLookupCandidates(symbol string) ([]string, string) {
	lower := strings.ToLower(strings.TrimSpace(symbol))
	digits := digitsOnly(lower)
	values := []string{lower}
	if digits != "" {
		values = append(values, digits, "sh"+digits, "sz"+digits, "bj"+digits)
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, digits
}

func digitsOnly(value string) string {
	var builder strings.Builder
	for _, ch := range value {
		if ch >= '0' && ch <= '9' {
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}

func tickflowBaseURL() string {
	if base := strings.TrimSpace(getEnv("TICKFLOW_BASE_URL", "")); base != "" {
		return strings.TrimRight(base, "/")
	}
	if strings.TrimSpace(getEnv("TICKFLOW_API_KEY", "tk_70e27ad14ba64397811ab0773e3a6c2e")) == "" {
		return defaultTickflowFreeBaseURL
	}
	return defaultTickflowBaseURL
}

func tickflowTimeout() time.Duration {
	raw := strings.TrimSpace(getEnv("TICKFLOW_TIMEOUT_SECONDS", "30"))
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return defaultTickflowHTTPTimeout
	}
	return time.Duration(seconds) * time.Second
}

func shanghaiLocation() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Local
	}
	return location
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minLen(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func finiteFloat(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func normalizeEastmoneyKlines(rows []string, name string, symbol string) ([]TickflowMarketRecord, error) {
	records := make([]TickflowMarketRecord, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, ",")
		if len(parts) < 11 {
			continue
		}
		tradeDate, err := time.Parse("2006-01-02", strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		open := parseMarketFloat(parts[1])
		closeValue := parseMarketFloat(parts[2])
		high := parseMarketFloat(parts[3])
		low := parseMarketFloat(parts[4])
		volume := parseMarketInt(parts[5])
		amount := parseMarketFloat(parts[6])
		amplitude := parseMarketFloat(parts[7])
		percentageChange := parseMarketFloat(parts[8])
		amountChange := parseMarketFloat(parts[9])
		turnoverRate := parseMarketFloat(parts[10])
		records = append(records, marketRecordFromDate(tradeDate, symbol, name, open, closeValue, high, low, volume, amount, amplitude, percentageChange, amountChange, turnoverRate))
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Datetime < records[j].Datetime
	})
	return records, nil
}

func normalizeBaiduKlines(marketData string, symbol string, startDate time.Time, endDate time.Time) ([]TickflowMarketRecord, error) {
	rows := strings.Split(marketData, ";")
	records := make([]TickflowMarketRecord, 0, len(rows))
	for _, row := range rows {
		parts := strings.Split(row, ",")
		if len(parts) < 12 {
			continue
		}
		tradeDate, err := time.Parse("2006-01-02", strings.TrimSpace(parts[1]))
		if err != nil {
			continue
		}
		if tradeDate.Before(dateOnlyUTC(startDate)) || tradeDate.After(dateOnlyUTC(endDate)) {
			continue
		}
		open := parseMarketFloat(parts[2])
		closeValue := parseMarketFloat(parts[3])
		volume := parseMarketInt(parts[4])
		high := parseMarketFloat(parts[5])
		low := parseMarketFloat(parts[6])
		amount := parseMarketFloat(parts[7])
		amountChange := parseMarketFloat(parts[8])
		percentageChange := parseMarketFloat(parts[9])
		turnoverRate := parseMarketFloat(parts[10])
		prevClose := parseMarketFloat(parts[11])
		amplitude := 0.0
		if prevClose > 0 {
			amplitude = (high - low) / prevClose * 100
		}
		records = append(records, marketRecordFromDate(tradeDate, symbol, "", open, closeValue, high, low, volume, amount, amplitude, percentageChange, amountChange, turnoverRate))
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Datetime < records[j].Datetime
	})
	return records, nil
}

func marketRecordFromDate(tradeDate time.Time, symbol string, name string, open float64, closeValue float64, high float64, low float64, volume int64, amount float64, amplitude float64, percentageChange float64, amountChange float64, turnoverRate float64) TickflowMarketRecord {
	utcDate := dateOnlyUTC(tradeDate)
	return TickflowMarketRecord{
		Datetime:         utcDate.Format("2006-01-02T15:04:05Z"),
		DatetimeInt:      utcDate.Unix(),
		Open:             finiteFloat(open),
		Close:            finiteFloat(closeValue),
		High:             finiteFloat(high),
		Low:              finiteFloat(low),
		Volume:           volume,
		Amount:           finiteFloat(amount),
		Amplitude:        finiteFloat(amplitude),
		PercentageChange: finiteFloat(percentageChange),
		AmountChange:     finiteFloat(amountChange),
		TurnoverRate:     finiteFloat(turnoverRate),
		TradeDate:        utcDate.Format("2006-01-02"),
		DateStr:          utcDate.Format("2006-01-02"),
		Symbol:           symbol,
		Name:             strings.TrimSpace(name),
		Timestamp:        utcDate.UnixMilli(),
	}
}

func dateOnlyUTC(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func parseMarketFloat(value string) float64 {
	raw := strings.TrimSpace(strings.TrimPrefix(value, "+"))
	if raw == "" || raw == "-" || raw == "--" || strings.EqualFold(raw, "null") {
		return 0
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return finiteFloat(parsed)
}

func parseMarketInt(value string) int64 {
	raw := strings.TrimSpace(value)
	if raw == "" || raw == "-" || raw == "--" {
		return 0
	}
	if strings.Contains(raw, ".") {
		return int64(parseMarketFloat(raw))
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func eastmoneySecID(symbol string, stockType int) (string, error) {
	normalized := strings.ToUpper(toTickflowSymbol(symbol, stockType))
	parts := strings.Split(normalized, ".")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", fmt.Errorf("invalid eastmoney symbol: %s", symbol)
	}
	switch parts[1] {
	case "SH":
		return "1." + parts[0], nil
	case "SZ", "BJ":
		return "0." + parts[0], nil
	default:
		return "", fmt.Errorf("unsupported eastmoney market: %s", parts[1])
	}
}

func eastmoneyFQT(adjust string) string {
	switch normalizeTickflowAdjust(adjust) {
	case "forward", "forward_additive":
		return "1"
	case "backward", "backward_additive":
		return "2"
	default:
		return "0"
	}
}

func historyProviderOrder() []string {
	raw := strings.TrimSpace(getEnv("HISTORY_PROVIDER_ORDER", defaultHistoryProviderOrder))
	if raw == "" {
		raw = defaultHistoryProviderOrder
	}
	items := strings.Split(raw, ",")
	providers := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		provider := canonicalHistoryProviderName(strings.TrimSpace(strings.ToLower(item)))
		if provider == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		providers = append(providers, provider)
	}
	if len(providers) == 0 {
		return []string{eastmoneyProviderName, baiduProviderName, tickflowProviderName}
	}
	return providers
}

func canonicalHistoryProviderName(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "eastmoney", "eastmoney-push2his", "em":
		return eastmoneyProviderName
	case "baidu", "baidu-gushitong":
		return baiduProviderName
	case "tickflow", "tickflow-go":
		return tickflowProviderName
	default:
		return ""
	}
}

func eastmoneyBaseURL() string {
	if base := strings.TrimSpace(getEnv("EASTMONEY_BASE_URL", "")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return defaultEastmoneyBaseURL
}

func baiduBaseURL() string {
	if base := strings.TrimSpace(getEnv("BAIDU_FINANCE_BASE_URL", "")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return defaultBaiduBaseURL
}
