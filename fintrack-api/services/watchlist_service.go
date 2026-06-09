package services

import (
	"bytes"
	"database/sql"
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

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/models"
	"github.com/lib/pq"
)

type WatchlistService struct {
	db     *database.DB
	config *config.Config
}

type MTFBestPage struct {
	Items  []models.MTFBestPrediction
	Total  int
	Limit  int
	Offset int
}

type postgresHandlerDirectPredictionResponse struct {
	Code    int                    `json:"code"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
	Error   string                 `json:"error"`
}

const directPredictionCacheDateLayout = "2006-01-02"

func NewWatchlistService(db *database.DB, cfg *config.Config) *WatchlistService {
	return &WatchlistService{db: db, config: cfg}
}

func (s *WatchlistService) Config() *config.Config {
	return s.config
}

var ErrSymbolNotFound = errors.New("symbol not found")

const mtfBestStaleRefreshAfter = 180 * 24 * time.Hour
const mtfPredictOnceBestMaxAgeDays = 180

var ErrDuplicateSymbol = errors.New("duplicate symbol")

type WatchlistLimitExceededError struct {
	Limit int
	Count int
}

func (e WatchlistLimitExceededError) Error() string {
	return "watchlist limit exceeded"
}

func newWatchlistLimitExceededError(limit int, count int) WatchlistLimitExceededError {
	return WatchlistLimitExceededError{Limit: limit, Count: count}
}

func watchlistLimitForMembershipLevel(level int) int {
	switch {
	case level >= 3:
		return 50
	case level == 2:
		return 10
	default:
		return 3
	}
}

func applyWatchlistOverflow(items []models.WatchlistItem, limit int) {
	if limit < 0 {
		limit = 0
	}
	type itemOrder struct {
		index   int
		id      int
		addedAt time.Time
	}
	order := make([]itemOrder, 0, len(items))
	for i := range items {
		items[i].WatchlistLimit = limit
		order = append(order, itemOrder{
			index:   i,
			id:      items[i].ID,
			addedAt: items[i].AddedAt,
		})
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].addedAt.Equal(order[j].addedAt) {
			return order[i].id < order[j].id
		}
		return order[i].addedAt.Before(order[j].addedAt)
	})
	for position, entry := range order {
		items[entry.index].IsOverLimit = position >= limit
	}
}

func newInferenceGatewayHTTPClient(timeoutSeconds int) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Timeout:   time.Duration(timeoutSeconds) * time.Second,
		Transport: transport,
	}
}

func readGatewayJSONResponse(resp *http.Response, requestURL string, operation string) (map[string]interface{}, error) {
	if resp == nil {
		return nil, fmt.Errorf("%s: empty http response", operation)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response body failed from %s (status=%d): %v", operation, requestURL, resp.StatusCode, err)
	}
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return nil, fmt.Errorf("%s: empty response body from %s (status=%d)", operation, requestURL, resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		raw := string(bodyBytes)
		if len(raw) > 512 {
			raw = raw[:512] + "...(truncated)"
		}
		return nil, fmt.Errorf("%s: decode response body failed from %s (status=%d, body=%q): %v", operation, requestURL, resp.StatusCode, raw, err)
	}
	return body, nil
}

func marshalOptionalJSONObject(value interface{}) (string, error) {
	if value == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "{}", nil
	}
	return string(raw), nil
}

func marshalOptionalFloat64Array(values []float64, field string) (interface{}, error) {
	if len(values) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s: %v", field, err)
	}
	return string(raw), nil
}

func marshalOptionalStringArray(values []string, field string) (interface{}, error) {
	if len(values) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal %s: %v", field, err)
	}
	return string(raw), nil
}

func bestPredictionQuantileArg(req *models.SaveMTFBestRequest) interface{} {
	if req.BestPredictionQuantile != nil {
		return *req.BestPredictionQuantile
	}
	quantile, ok := parseBestPredictionQuantile(req.BestPredictionItem)
	if !ok {
		return nil
	}
	return quantile
}

func parseBestPredictionQuantile(item string) (float64, bool) {
	trimmed := strings.TrimSpace(item)
	if trimmed == "" {
		return 0, false
	}
	idx := strings.LastIndex(trimmed, "-")
	if idx < 0 || idx == len(trimmed)-1 {
		return 0, false
	}
	value, err := strconv.ParseFloat(trimmed[idx+1:], 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func normalizedPredictionType(value string) string {
	return normalizeTrainPredictionType(value)
}

func normalizeMTFSymbolReadKey(symbol string) string {
	trimmed := strings.ToLower(strings.TrimSpace(symbol))
	if trimmed == "" {
		return ""
	}

	var digitsOnly strings.Builder
	for _, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			digitsOnly.WriteRune(ch)
		}
	}
	if digitsOnly.Len() > 0 {
		return digitsOnly.String()
	}

	return trimmed
}

func mtfCanonicalSymbolExpr(column string) string {
	return fmt.Sprintf(
		"CASE WHEN regexp_replace(lower(trim(%[1]s)), '[^0-9]', '', 'g') <> '' THEN regexp_replace(lower(trim(%[1]s)), '[^0-9]', '', 'g') ELSE lower(trim(%[1]s)) END",
		column,
	)
}

func symbolNameLookupCandidates(symbol string) []string {
	lower := strings.ToLower(strings.TrimSpace(symbol))
	code := normalizeMarketQuoteCode(lower)
	values := []string{lower}
	if code != "" && isDigitsOnly(code) {
		values = append(values, code, "sh"+code, "sz"+code)
	}
	return uniqueStrings(values)
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func inferLookupStockTypes(symbol string) []int {
	code := normalizeMarketQuoteCode(symbol)
	if strings.HasPrefix(code, "5") || strings.HasPrefix(code, "15") || strings.HasPrefix(code, "16") || strings.HasPrefix(code, "18") {
		return []int{2, 1}
	}
	return []int{1, 2}
}

func canonicalWatchlistSymbol(symbol string, stockType int) string {
	lower := strings.ToLower(strings.TrimSpace(symbol))
	code := normalizeMarketQuoteCode(lower)
	if code == "" || !isDigitsOnly(code) {
		return lower
	}
	if stockType == 2 {
		if strings.HasPrefix(code, "15") || strings.HasPrefix(code, "16") || strings.HasPrefix(code, "18") {
			return "sz" + code
		}
		return "sh" + code
	}
	if strings.HasPrefix(code, "6") {
		return "sh" + code
	}
	return "sz" + code
}

func (s *WatchlistService) lookupDisplayName(symbol string) string {
	for _, stockType := range inferLookupStockTypes(symbol) {
		name, err := s.LookupStockName(symbol, stockType)
		if err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func (s *WatchlistService) getUserMembershipLevel(userID int) (int, error) {
	var level int
	err := s.db.Conn.QueryRow(`SELECT COALESCE(membership_level, 0) FROM users WHERE id = $1`, userID).Scan(&level)
	if err != nil {
		return 0, fmt.Errorf("failed to get user membership level: %v", err)
	}
	return level, nil
}

func (s *WatchlistService) watchlistCount(userID int) (int, error) {
	var count int
	err := s.db.Conn.QueryRow(`SELECT COUNT(*) FROM user_watchlist WHERE user_id = $1`, userID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count watchlist: %v", err)
	}
	return count, nil
}

func (s *WatchlistService) AddToWatchlist(userID int, req *models.AddToWatchlistRequest) error {
	stockType := 1
	if req.StockType != nil {
		stockType = *req.StockType
	}

	var name sql.NullString
	symLower := canonicalWatchlistSymbol(req.Symbol, stockType)

	var exists bool
	if err := s.db.Conn.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_watchlist WHERE user_id = $1 AND symbol = $2)`, userID, symLower).Scan(&exists); err != nil {
		return fmt.Errorf("failed to check duplicate: %v", err)
	}
	if exists {
		return ErrDuplicateSymbol
	}
	membershipLevel, err := s.getUserMembershipLevel(userID)
	if err != nil {
		return err
	}
	limit := watchlistLimitForMembershipLevel(membershipLevel)
	count, err := s.watchlistCount(userID)
	if err != nil {
		return err
	}
	if count >= limit {
		return newWatchlistLimitExceededError(limit, count)
	}
	candidates := symbolNameLookupCandidates(symLower)
	var table string
	if stockType == 2 {
		table = "etf_daily"
	} else {
		table = "a_stock_comment_daily"
	}
	query := fmt.Sprintf("SELECT COALESCE(name, '') FROM %s WHERE code = ANY($1) ORDER BY trading_date DESC LIMIT 1", table)
	err = s.db.Conn.QueryRow(query, pq.Array(candidates)).Scan(&name)
	if err == sql.ErrNoRows {
		return ErrSymbolNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to query %s: %v", table, err)
	}

	nameStr := strings.TrimSpace(name.String)
	if nameStr != "" {
		_, err = s.db.Conn.Exec(
			`INSERT INTO stocks (symbol, company_name) VALUES ($1, $2)
             ON CONFLICT (symbol) DO UPDATE SET company_name = EXCLUDED.company_name`,
			symLower, nameStr,
		)
		if err != nil {
			log.Printf("upsert stocks failed, continue without company_name: %v", err)
		}
	}

	_, err = s.db.Conn.Exec(`
		INSERT INTO user_watchlist (user_id, symbol, notes, stock_type) 
		VALUES ($1, $2, $3, $4)
	`, userID, symLower, req.Notes, stockType)

	if err != nil {
		return fmt.Errorf("failed to add to watchlist: %v", err)
	}

	return nil
}

func (s *WatchlistService) GetLatestQuotesBySymbols(symbols []string) ([]models.LatestQuote, error) {
	if len(symbols) == 0 {
		return []models.LatestQuote{}, nil
	}

	codes := make([]string, 0, len(symbols))
	codeToSymbol := make(map[string]string, len(symbols))
	for _, sym := range symbols {
		c := normalizeMarketQuoteCode(sym)
		if c == "" {
			continue
		}
		codes = append(codes, c)
		if _, exists := codeToSymbol[c]; !exists {
			codeToSymbol[c] = sym
		}
	}

	if len(codes) == 0 {
		return []models.LatestQuote{}, nil
	}

	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes))
	for i, c := range codes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = c
	}

	qStock := fmt.Sprintf(`
        SELECT DISTINCT ON (regexp_replace(lower(trim(code)), '[^0-9]', '', 'g'))
            code, trading_date, latest_price, change_percent, turnover_rate
        FROM a_stock_comment_daily
        WHERE regexp_replace(lower(trim(code)), '[^0-9]', '', 'g') IN (%s)
        ORDER BY regexp_replace(lower(trim(code)), '[^0-9]', '', 'g'), trading_date DESC
    `, strings.Join(placeholders, ","))

	rows, err := s.db.Conn.Query(qStock, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query a_stock_comment_daily: %v", err)
	}
	defer rows.Close()

	quotes := make(map[string]models.LatestQuote)
	for rows.Next() {
		var code string
		var dt time.Time
		var price sql.NullFloat64
		var change sql.NullFloat64
		var turnover sql.NullFloat64
		if err := rows.Scan(&code, &dt, &price, &change, &turnover); err != nil {
			return nil, fmt.Errorf("failed to scan a_stock_comment_daily: %v", err)
		}
		normalizedCode := normalizeMarketQuoteCode(code)
		sym := codeToSymbol[normalizedCode]
		p := models.LatestQuote{Symbol: sym}
		ds := dt.Format("2006-01-02")
		p.TradingDate = &ds
		if price.Valid {
			p.LatestPrice = &price.Float64
		}
		if change.Valid {
			p.ChangePercent = &change.Float64
		}
		if turnover.Valid {
			p.TurnoverRate = &turnover.Float64
		}
		quotes[normalizedCode] = p
	}

	qEtf := fmt.Sprintf(`
        SELECT DISTINCT ON (regexp_replace(lower(trim(code)), '[^0-9]', '', 'g'))
            code, trading_date, latest_price, change_percent
        FROM etf_daily
        WHERE regexp_replace(lower(trim(code)), '[^0-9]', '', 'g') IN (%s)
        ORDER BY regexp_replace(lower(trim(code)), '[^0-9]', '', 'g'), trading_date DESC
    `, strings.Join(placeholders, ","))

	rows2, err := s.db.Conn.Query(qEtf, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query etf_daily: %v", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var code string
		var dt time.Time
		var price sql.NullFloat64
		var change sql.NullFloat64
		if err := rows2.Scan(&code, &dt, &price, &change); err != nil {
			return nil, fmt.Errorf("failed to scan etf_daily: %v", err)
		}
		normalizedCode := normalizeMarketQuoteCode(code)
		sym := codeToSymbol[normalizedCode]
		p := models.LatestQuote{Symbol: sym}
		ds := dt.Format("2006-01-02")
		p.TradingDate = &ds
		if price.Valid {
			p.LatestPrice = &price.Float64
		}
		if change.Valid {
			p.ChangePercent = &change.Float64
		}
		if _, exists := quotes[normalizedCode]; !exists {
			quotes[normalizedCode] = p
		}
	}

	result := make([]models.LatestQuote, 0, len(symbols))
	for _, c := range codes {
		if q, ok := quotes[c]; ok {
			result = append(result, q)
		} else {
			result = append(result, models.LatestQuote{Symbol: codeToSymbol[c]})
		}
	}
	mergeLatestExternalQuotes(result, codes, codeToSymbol, fetchEastmoneyPreviousTradingQuotes(codes, codeToSymbol))
	return result, nil
}

func normalizeMarketQuoteCode(symbol string) string {
	trimmed := strings.ToLower(strings.TrimSpace(symbol))
	if trimmed == "" {
		return ""
	}

	var digits strings.Builder
	for _, ch := range trimmed {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}
	if digits.Len() > 0 {
		return digits.String()
	}
	return trimmed
}

func mergeLatestExternalQuotes(result []models.LatestQuote, codes []string, codeToSymbol map[string]string, external map[string]models.LatestQuote) {
	if len(external) == 0 {
		return
	}
	for index, code := range codes {
		externalQuote, ok := external[code]
		if !ok || !isExternalQuoteNewer(result[index], externalQuote) {
			continue
		}
		externalQuote.Symbol = codeToSymbol[code]
		result[index] = externalQuote
	}
}

func isExternalQuoteNewer(current models.LatestQuote, external models.LatestQuote) bool {
	if external.LatestPrice == nil || external.TradingDate == nil || strings.TrimSpace(*external.TradingDate) == "" {
		return false
	}
	if current.TradingDate == nil || strings.TrimSpace(*current.TradingDate) == "" {
		return true
	}
	currentDate, currentErr := time.Parse("2006-01-02", strings.TrimSpace(*current.TradingDate))
	externalDate, externalErr := time.Parse("2006-01-02", strings.TrimSpace(*external.TradingDate))
	if currentErr != nil {
		return true
	}
	if externalErr != nil {
		return false
	}
	return externalDate.After(currentDate)
}

func fetchEastmoneyPreviousTradingQuotes(codes []string, codeToSymbol map[string]string) map[string]models.LatestQuote {
	quotes := make(map[string]models.LatestQuote)
	client := &http.Client{Timeout: 6 * time.Second}
	for _, code := range uniqueStrings(codes) {
		quote, err := fetchEastmoneyPreviousTradingQuote(client, code, codeToSymbol[code])
		if err != nil {
			log.Printf("eastmoney quote fallback failed for %s: %v", code, err)
			continue
		}
		quotes[code] = quote
	}
	return quotes
}

func fetchEastmoneyPreviousTradingQuote(client *http.Client, code string, symbol string) (models.LatestQuote, error) {
	secID := eastmoneySecID(code, symbol)
	now := chinaNow()
	end := now.Format("20060102")
	beg := now.AddDate(0, -2, 0).Format("20060102")
	requestURL := fmt.Sprintf("https://45.push2his.eastmoney.com/api/qt/stock/kline/get?secid=%s&fields1=f1,f2,f3,f4,f5,f6&fields2=f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61&klt=101&fqt=1&beg=%s&end=%s", secID, beg, end)

	httpReq, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return models.LatestQuote{}, err
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0")
	httpReq.Header.Set("Accept", "application/json,text/plain,*/*")
	resp, err := client.Do(httpReq)
	if err != nil {
		return models.LatestQuote{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return models.LatestQuote{}, fmt.Errorf("status %d", resp.StatusCode)
	}

	var body eastmoneyKlineResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return models.LatestQuote{}, err
	}
	return latestPreviousTradingQuoteFromKlines(symbol, body.Data.Klines, now)
}

type eastmoneyKlineResponse struct {
	Data struct {
		Klines []string `json:"klines"`
	} `json:"data"`
}

func latestPreviousTradingQuoteFromKlines(symbol string, klines []string, now time.Time) (models.LatestQuote, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	for i := len(klines) - 1; i >= 0; i-- {
		quote, quoteDate, err := parseEastmoneyKline(symbol, klines[i], now.Location())
		if err != nil {
			continue
		}
		if quoteDate.Before(today) {
			return quote, nil
		}
	}
	return models.LatestQuote{}, errors.New("previous trading quote not found")
}

func parseEastmoneyKline(symbol string, line string, loc *time.Location) (models.LatestQuote, time.Time, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 11 {
		return models.LatestQuote{}, time.Time{}, errors.New("invalid kline fields")
	}
	quoteDate, err := time.ParseInLocation("2006-01-02", fields[0], loc)
	if err != nil {
		return models.LatestQuote{}, time.Time{}, err
	}
	price, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return models.LatestQuote{}, time.Time{}, err
	}
	changePercent, _ := strconv.ParseFloat(fields[8], 64)
	turnoverPercent, _ := strconv.ParseFloat(fields[10], 64)
	tradingDate := quoteDate.Format("2006-01-02")
	turnoverRate := turnoverPercent / 100
	return models.LatestQuote{
		Symbol:        symbol,
		LatestPrice:   &price,
		ChangePercent: &changePercent,
		TradingDate:   &tradingDate,
		TurnoverRate:  &turnoverRate,
	}, quoteDate, nil
}

func eastmoneySecID(code string, symbol string) string {
	lowerSymbol := strings.ToLower(strings.TrimSpace(symbol))
	market := "0"
	if strings.HasPrefix(lowerSymbol, "sh") || strings.HasSuffix(lowerSymbol, ".sh") || strings.HasPrefix(code, "5") || strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
		market = "1"
	}
	return market + "." + code
}

func chinaNow() time.Time {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*60*60)
	}
	return time.Now().In(loc)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *WatchlistService) LookupStockName(symbol string, stockType int) (string, error) {
	var name sql.NullString
	candidates := symbolNameLookupCandidates(symbol)
	var table string
	if stockType == 2 {
		table = "etf_daily"
		query := fmt.Sprintf("SELECT COALESCE(name, '') FROM %s WHERE code = ANY($1) ORDER BY trading_date DESC LIMIT 1", table)
		err := s.db.Conn.QueryRow(query, pq.Array(candidates)).Scan(&name)
		if err == sql.ErrNoRows {
			return "", ErrSymbolNotFound
		}
		if err != nil {
			return "", fmt.Errorf("failed to query %s: %v", table, err)
		}
		return strings.TrimSpace(name.String), nil
	} else {
		table = "a_stock_comment_daily"
		query := fmt.Sprintf("SELECT COALESCE(name, '') FROM %s WHERE code = ANY($1) ORDER BY trading_date DESC LIMIT 1", table)
		err := s.db.Conn.QueryRow(query, pq.Array(candidates)).Scan(&name)
		if err == sql.ErrNoRows {
			return "", ErrSymbolNotFound
		}
		if err != nil {
			return "", fmt.Errorf("failed to query %s: %v", table, err)
		}
		return strings.TrimSpace(name.String), nil
	}
}

func (s *WatchlistService) GetWatchlist(userID int) ([]models.WatchlistItem, error) {
	membershipLevel, err := s.getUserMembershipLevel(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Conn.Query(`
		SELECT 
			uw.id, uw.symbol, uw.added_at, uw.notes, COALESCE(uw.stock_type, 1),
			COALESCE(s.company_name, ''),
			COALESCE(tbp.unique_key, ''),
			COALESCE(tbp.mtf_version, ''),
			COALESCE(uw.strategy_unique_key, ''),
			COALESCE(tsp.name, '')
		FROM user_watchlist uw
		LEFT JOIN stocks s ON uw.symbol = s.symbol
		LEFT JOIN LATERAL (
			SELECT unique_key, mtf_version
			FROM mtf_best_predictions
			WHERE CASE
				WHEN regexp_replace(lower(trim(symbol)), '[^0-9]', '', 'g') <> ''
				THEN regexp_replace(lower(trim(symbol)), '[^0-9]', '', 'g')
				ELSE lower(trim(symbol))
			END = CASE
				WHEN regexp_replace(lower(trim(uw.symbol)), '[^0-9]', '', 'g') <> ''
				THEN regexp_replace(lower(trim(uw.symbol)), '[^0-9]', '', 'g')
				ELSE lower(trim(uw.symbol))
			END
			ORDER BY created_at DESC
			LIMIT 1
		) tbp ON true
		LEFT JOIN mtf_strategy_params tsp ON uw.strategy_unique_key = tsp.unique_key
		WHERE uw.user_id = $1
		ORDER BY uw.added_at DESC
	`, userID)

	if err != nil {
		return nil, fmt.Errorf("failed to get watchlist: %v", err)
	}
	defer rows.Close()

	items := []models.WatchlistItem{}
	for rows.Next() {
		var item models.WatchlistItem
		var uniqueKey, version, strategyKey, strategyName string
		var stockType int

		err := rows.Scan(
			&item.ID, &item.Stock.Symbol, &item.AddedAt, &item.Notes, &stockType,
			&item.Stock.CompanyName,
			&uniqueKey, &version,
			&strategyKey, &strategyName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan watchlist item: %v", err)
		}
		if strings.TrimSpace(item.Stock.CompanyName) == "" {
			if name, lookupErr := s.LookupStockName(item.Stock.Symbol, stockType); lookupErr == nil {
				item.Stock.CompanyName = name
			}
		}

		item.UniqueKey = uniqueKey
		item.StockType = &stockType
		item.StrategyUniqueKey = strategyKey
		item.StrategyName = strategyName
		items = append(items, item)
	}

	applyWatchlistOverflow(items, watchlistLimitForMembershipLevel(membershipLevel))
	return items, nil
}

func (s *WatchlistService) BindStrategy(userID int, req *models.BindStrategyRequest) error {
	// Check if strategy exists
	var exists bool
	err := s.db.Conn.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM mtf_strategy_params WHERE unique_key = $1 AND (user_id = $2 OR user_id IS NULL))
	`, req.StrategyUniqueKey, userID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check strategy existence: %v", err)
	}
	if !exists {
		return fmt.Errorf("strategy not found")
	}

	// Update user_watchlist
	// Note: symbol in request might need normalization or we assume it matches what's in DB
	// Assuming symbol matches.
	res, err := s.db.Conn.Exec(`
		UPDATE user_watchlist 
		SET strategy_unique_key = $1
		WHERE user_id = $2 AND symbol = $3
	`, req.StrategyUniqueKey, userID, req.Symbol)

	if err != nil {
		return fmt.Errorf("failed to bind strategy: %v", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("watchlist item not found for symbol %s", req.Symbol)
	}

	return nil
}

func (s *WatchlistService) RemoveFromWatchlist(userID, watchlistID int) error {
	// Check if the watchlist item belongs to the user
	var exists bool
	err := s.db.Conn.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM user_watchlist WHERE id = $1 AND user_id = $2)
	`, watchlistID, userID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check watchlist ownership: %v", err)
	}
	if !exists {
		return fmt.Errorf("watchlist item not found")
	}

	// Remove from watchlist
	_, err = s.db.Conn.Exec(`
		DELETE FROM user_watchlist WHERE id = $1 AND user_id = $2
	`, watchlistID, userID)
	if err != nil {
		return fmt.Errorf("failed to remove from watchlist: %v", err)
	}

	return nil
}

func (s *WatchlistService) UpdateWatchlistItem(userID, watchlistID int, req *models.UpdateWatchlistRequest) (*models.WatchlistItem, error) {
	// Update the watchlist item
	_, err := s.db.Conn.Exec(`
		UPDATE user_watchlist 
		SET notes = $1
		WHERE id = $2 AND user_id = $3
	`, req.Notes, watchlistID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to update watchlist item: %v", err)
	}

	// Return the updated item
	return s.getWatchlistItemByID(watchlistID)
}

func (s *WatchlistService) getWatchlistItemByID(watchlistID int) (*models.WatchlistItem, error) {
	row := s.db.Conn.QueryRow(`
		SELECT 
			uw.id, uw.symbol, uw.added_at, uw.notes, COALESCE(uw.stock_type, 1), COALESCE(uw.strategy_unique_key, ''),
			COALESCE(s.id, 0), COALESCE(s.company_name, ''), s.exchange, s.sector, s.industry, s.market_cap, s.created_at, s.updated_at,
			sp.price, sp.change_percent, sp.volume
		FROM user_watchlist uw
		LEFT JOIN stocks s ON uw.symbol = s.symbol
		LEFT JOIN LATERAL (
			SELECT price, change_percent, volume
			FROM stock_prices sp
			JOIN stocks ss ON sp.stock_id = ss.id
			WHERE ss.symbol = uw.symbol
			ORDER BY recorded_at DESC 
			LIMIT 1
		) sp ON true
		WHERE uw.id = $1
	`, watchlistID)

	var item models.WatchlistItem
	var stockType int
	var strategyUniqueKey string
	var stockCreatedAt sql.NullTime
	var stockUpdatedAt sql.NullTime
	var price sql.NullFloat64
	var changePercent sql.NullFloat64
	var volume sql.NullInt64

	err := row.Scan(
		&item.ID, &item.Stock.Symbol, &item.AddedAt, &item.Notes, &stockType, &strategyUniqueKey,
		&item.Stock.ID, &item.Stock.CompanyName,
		&item.Stock.Exchange, &item.Stock.Sector, &item.Stock.Industry,
		&item.Stock.MarketCap, &stockCreatedAt, &stockUpdatedAt,
		&price, &changePercent, &volume,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get watchlist item: %v", err)
	}

	item.StockType = &stockType
	item.StrategyUniqueKey = strategyUniqueKey
	if stockCreatedAt.Valid {
		item.Stock.CreatedAt = stockCreatedAt.Time
	}
	if stockUpdatedAt.Valid {
		item.Stock.UpdatedAt = stockUpdatedAt.Time
	}

	// Set current price if available
	if price.Valid {
		var changePercentValue *float64
		if changePercent.Valid {
			changePercentValue = &changePercent.Float64
		}
		var volumeValue *int64
		if volume.Valid {
			volumeValue = &volume.Int64
		}
		item.CurrentPrice = &models.StockPrice{
			Price:         price.Float64,
			ChangePercent: changePercentValue,
			Volume:        volumeValue,
		}
	}

	return &item, nil
}

// SyncStockData calls the inference gateway to sync stock data.
func (s *WatchlistService) SyncStockData(symbol string) {
	// Parse exchange prefix to determine stock type
	stockType := 1 // default to Shanghai (sh)
	if strings.HasPrefix(symbol, "sz") {
		stockType = 2 // Shenzhen
	}

	// Remove prefix for the actual stock code
	cleanSymbol := strings.TrimPrefix(strings.TrimPrefix(symbol, "sh"), "sz")

	// Prepare request payload
	payload := map[string]interface{}{
		"symbol":     cleanSymbol,
		"stock_type": stockType,
		"batch_size": 1000,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal sync request: %v", err)
		return
	}

	// Call inference gateway.
	url := fmt.Sprintf("%s/api/sync-stock", s.config.InferenceGateway.BaseURL)
	client := newInferenceGatewayHTTPClient(s.config.InferenceGateway.Timeout)

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to call inference gateway sync service: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Inference gateway sync service returned non-200 status: %d", resp.StatusCode)
		return
	}

	log.Printf("Successfully triggered stock data sync for %s (type: %d)", cleanSymbol, stockType)
}

type mtfMembershipTrainPolicy struct {
	AllowedPredictionTypes map[string]struct{}
	AllowedContextLens     map[int]struct{}
	AllowedHorizonLens     map[int]struct{}
}

func newIntSet(values ...int) map[int]struct{} {
	out := make(map[int]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func newStringSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func getMTFMembershipTrainPolicy(level int) mtfMembershipTrainPolicy {
	switch level {
	case 3:
		return mtfMembershipTrainPolicy{
			AllowedPredictionTypes: newStringSet("mtf-lite", "mtf-pro"),
			AllowedContextLens:     newIntSet(512, 1024, 2048),
			AllowedHorizonLens:     newIntSet(7, 14, 28),
		}
	case 2:
		return mtfMembershipTrainPolicy{
			AllowedPredictionTypes: newStringSet("mtf-lite", "mtf-pro"),
			AllowedContextLens:     newIntSet(512, 1024),
			AllowedHorizonLens:     newIntSet(7, 14, 28),
		}
	case 1:
		return mtfMembershipTrainPolicy{
			AllowedPredictionTypes: newStringSet("mtf-lite", "mtf-pro"),
			AllowedContextLens:     newIntSet(512),
			AllowedHorizonLens:     newIntSet(7, 14, 28),
		}
	default:
		return mtfMembershipTrainPolicy{
			AllowedPredictionTypes: newStringSet("mtf-lite"),
			AllowedContextLens:     newIntSet(512),
			AllowedHorizonLens:     newIntSet(7),
		}
	}
}

func normalizeTrainPredictionType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "non_cov", "non-cov", "lite", "mtf_lite", "mtf-lite":
		return "mtf-lite"
	case "cov", "pro", "mtf_pro", "mtf-pro":
		return "mtf-pro"
	default:
		return strings.TrimSpace(value)
	}
}

func predictionTypeUsesCovariates(value string) bool {
	return normalizeTrainPredictionType(value) == "mtf-pro"
}

func buildMarketCovariateConfig() map[string]interface{} {
	return map[string]interface{}{
		"enabled":   true,
		"xreg_mode": "mtf + xreg",
	}
}

func NormalizeMTFBestTrainRequest(req *models.MTFBestTrainRequest, membershipLevel int, userID int, isAdmin bool) (*models.MTFPredictRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	policy := getMTFMembershipTrainPolicy(membershipLevel)
	predictionType := normalizeTrainPredictionType(req.PredictionType)
	if _, ok := policy.AllowedPredictionTypes[predictionType]; !ok {
		return nil, fmt.Errorf("membership level %d does not support prediction_type=%s", membershipLevel, predictionType)
	}
	if _, ok := policy.AllowedContextLens[req.ContextLen]; !ok {
		return nil, fmt.Errorf("membership level %d does not support context_len=%d", membershipLevel, req.ContextLen)
	}
	years := 15
	if req.Years != nil && *req.Years > 0 {
		years = *req.Years
	}

	horizonLen := req.HorizonLen
	contextLen := req.ContextLen
	uid := userID
	forceEnqueue := isAdmin
	normalized := &models.MTFPredictRequest{
		StockCode:      req.StockCode,
		StockType:      req.StockType,
		PredictionType: predictionType,
		Years:          &years,
		HorizonLen:     &horizonLen,
		ContextLen:     &contextLen,
		UserID:         &uid,
	}
	if isAdmin {
		normalized.ForceEnqueue = &forceEnqueue
	}
	if predictionTypeUsesCovariates(predictionType) {
		covariatePreset := "market_cov_v1"
		normalized.CovariatePreset = &covariatePreset
		normalized.CovariateConfig = buildMarketCovariateConfig()
	}

	return normalized, nil
}

func NormalizeMTFPredictOnceRequest(req *models.MTFPredictRequest, membershipLevel int, userID int, isAdmin bool) (*models.MTFPredictRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}

	policy := getMTFMembershipTrainPolicy(membershipLevel)
	predictionType := normalizeTrainPredictionType(req.PredictionType)
	if _, ok := policy.AllowedPredictionTypes[predictionType]; !ok {
		return nil, fmt.Errorf("membership level %d does not support prediction_type=%s", membershipLevel, predictionType)
	}
	if req.ContextLen != nil {
		if _, ok := policy.AllowedContextLens[*req.ContextLen]; !ok {
			return nil, fmt.Errorf("membership level %d does not support context_len=%d", membershipLevel, *req.ContextLen)
		}
	}
	normalized := *req
	uid := userID
	normalized.UserID = &uid
	normalized.ForceEnqueue = nil
	normalized.ForceRequeue = nil
	if isAdmin {
		force := true
		normalized.ForceEnqueue = &force
		normalized.ForceRequeue = &force
	}

	return &normalized, nil
}

func normalizeMTFPredictStockType(value interface{}) int {
	switch typed := value.(type) {
	case nil:
		return 1
	case int:
		if typed == 2 {
			return 2
		}
	case int64:
		if typed == 2 {
			return 2
		}
	case float64:
		if int(typed) == 2 {
			return 2
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed == 2 {
			return 2
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "2", "etf", "fund":
			return 2
		}
	}
	return 1
}

func intValueOrDefault(value *int, fallback int) int {
	if value != nil && *value > 0 {
		return *value
	}
	return fallback
}

func (s *WatchlistService) TriggerMTFPredict(req *models.MTFPredictRequest) (int, map[string]interface{}, error) {
	payload := map[string]interface{}{
		"stock_code": req.StockCode,
	}
	if req.StockType != nil {
		payload["stock_type"] = req.StockType
	}
	if req.TimeStep != nil {
		payload["time_step"] = *req.TimeStep
	}
	if req.Years != nil {
		payload["years"] = *req.Years
	}
	if strings.TrimSpace(req.PredictionType) != "" {
		payload["prediction_type"] = strings.TrimSpace(req.PredictionType)
	}
	if req.HorizonLen != nil {
		payload["horizon_len"] = *req.HorizonLen
	}
	if req.ContextLen != nil {
		payload["context_len"] = *req.ContextLen
	}
	if req.UserID != nil {
		payload["user_id"] = *req.UserID
	}
	if req.ForceEnqueue != nil {
		payload["force_enqueue"] = *req.ForceEnqueue
	}
	if strings.TrimSpace(req.QueuePriority) != "" {
		payload["queue_priority"] = strings.TrimSpace(req.QueuePriority)
	}
	if strings.TrimSpace(req.RefreshReason) != "" {
		payload["refresh_reason"] = strings.TrimSpace(req.RefreshReason)
	}
	if req.CovariatePreset != nil && strings.TrimSpace(*req.CovariatePreset) != "" {
		payload["covariate_preset"] = strings.TrimSpace(*req.CovariatePreset)
	}
	if strings.TrimSpace(req.CovariateSignature) != "" {
		payload["covariate_signature"] = strings.TrimSpace(req.CovariateSignature)
	}
	if req.CovariateConfig != nil {
		payload["covariate_config"] = req.CovariateConfig
	}
	if req.Covariates != nil {
		payload["covariates"] = req.Covariates
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal predict payload: %v", err)
	}
	url := fmt.Sprintf("%s/predict_for_best", s.config.InferenceGateway.BaseURL)
	client := newInferenceGatewayHTTPClient(s.config.InferenceGateway.Timeout)
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, nil, fmt.Errorf("call inference gateway predict: %v", err)
	}
	defer resp.Body.Close()
	body, err := readGatewayJSONResponse(resp, url, "decode inference gateway predict response")
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func (s *WatchlistService) triggerStaleMTFBestRefresh(item models.MTFBestPrediction) error {
	if s == nil || s.config == nil || strings.TrimSpace(s.config.InferenceGateway.BaseURL) == "" {
		return nil
	}
	if item.UpdatedAt.IsZero() || time.Since(item.UpdatedAt) <= mtfBestStaleRefreshAfter {
		return nil
	}

	stockType := item.StockType
	if stockType != 1 && stockType != 2 {
		stockType = inferLookupStockTypes(item.Symbol)[0]
	}
	request := &models.MTFPredictRequest{
		StockCode:      item.Symbol,
		StockType:      stockType,
		PredictionType: normalizedPredictionType(item.PredictionType),
		HorizonLen:     &item.HorizonLen,
		ContextLen:     &item.ContextLen,
		QueuePriority:  "background",
		RefreshReason:  "stale_180d",
	}
	if strings.TrimSpace(item.CovariateConfig) != "" && item.CovariateConfig != "{}" {
		var covariateConfig map[string]interface{}
		if err := json.Unmarshal([]byte(item.CovariateConfig), &covariateConfig); err == nil {
			request.CovariateConfig = covariateConfig
		}
	}
	if strings.TrimSpace(item.CovariateSignature) != "" {
		request.CovariateSignature = strings.TrimSpace(item.CovariateSignature)
	}
	if predictionTypeUsesCovariates(request.PredictionType) && request.CovariateConfig == nil {
		request.CovariateConfig = buildMarketCovariateConfig()
		preset := "market_cov_v1"
		request.CovariatePreset = &preset
	}

	_, _, err := s.TriggerMTFPredict(request)
	return err
}

func (s *WatchlistService) applyPredictOnceBestContinuation(req *models.MTFPredictRequest) {
	if s == nil || s.db == nil || req == nil {
		return
	}
	predictFromBestEnd := true
	if req.PredictFromBestEnd != nil {
		predictFromBestEnd = *req.PredictFromBestEnd
	}
	if !predictFromBestEnd || (req.StartDate != nil && strings.TrimSpace(*req.StartDate) != "") {
		return
	}
	horizonLen := intValueOrDefault(req.HorizonLen, 7)
	contextLen := intValueOrDefault(req.ContextLen, 2048)
	keys, err := s.GetMTFBestUniqueKeysByConfig(req.StockCode, horizonLen, contextLen, "")
	if err != nil || keys == nil {
		return
	}
	var uniqueKey string
	switch normalizeTrainPredictionType(req.PredictionType) {
	case "mtf-pro":
		uniqueKey = strings.TrimSpace(keys.MTFProUniqueKey)
	default:
		uniqueKey = strings.TrimSpace(keys.MTFLiteUniqueKey)
	}
	if uniqueKey == "" {
		return
	}
	best, err := s.GetMTFBestByUniqueKey(uniqueKey)
	if err != nil || best == nil || best.ValEndDate.IsZero() {
		return
	}
	startDate := best.ValEndDate.Format("2006-01-02")
	req.StartDate = &startDate
}

func (s *WatchlistService) TriggerMTFPredictOnce(req *models.MTFPredictRequest) (int, map[string]interface{}, error) {
	s.applyPredictOnceBestContinuation(req)
	payload := map[string]interface{}{
		"stock_code": req.StockCode,
	}
	if req.StockType != nil {
		payload["stock_type"] = req.StockType
	}
	if req.TimeStep != nil {
		payload["time_step"] = *req.TimeStep
	}
	if req.Years != nil {
		payload["years"] = *req.Years
	}
	if req.StartDate != nil && strings.TrimSpace(*req.StartDate) != "" {
		payload["start_date"] = strings.TrimSpace(*req.StartDate)
	}
	if req.EndDate != nil && strings.TrimSpace(*req.EndDate) != "" {
		payload["end_date"] = strings.TrimSpace(*req.EndDate)
	}
	if strings.TrimSpace(req.PredictionType) != "" {
		payload["prediction_type"] = strings.TrimSpace(req.PredictionType)
	}
	if req.HorizonLen != nil {
		payload["horizon_len"] = *req.HorizonLen
	}
	if req.ContextLen != nil {
		payload["context_len"] = *req.ContextLen
	}
	if req.UserID != nil {
		payload["user_id"] = *req.UserID
	}
	if req.ForceEnqueue != nil {
		payload["force_enqueue"] = *req.ForceEnqueue
	}
	if req.ForceRequeue != nil {
		payload["force_requeue"] = *req.ForceRequeue
	}
	bestMaxAgeDays := mtfPredictOnceBestMaxAgeDays
	if req.BestMaxAgeDays != nil && *req.BestMaxAgeDays > 0 {
		bestMaxAgeDays = *req.BestMaxAgeDays
	}
	payload["best_max_age_days"] = bestMaxAgeDays
	predictFromBestEnd := true
	if req.PredictFromBestEnd != nil {
		predictFromBestEnd = *req.PredictFromBestEnd
	}
	payload["predict_from_best_val_end"] = predictFromBestEnd
	chunkUntilLatest := true
	if req.ChunkUntilLatest != nil {
		chunkUntilLatest = *req.ChunkUntilLatest
	}
	payload["chunk_until_latest"] = chunkUntilLatest
	if req.CovariatePreset != nil && strings.TrimSpace(*req.CovariatePreset) != "" {
		payload["covariate_preset"] = strings.TrimSpace(*req.CovariatePreset)
	}
	if strings.TrimSpace(req.CovariateSignature) != "" {
		payload["covariate_signature"] = strings.TrimSpace(req.CovariateSignature)
	}
	if req.CovariateConfig != nil {
		payload["covariate_config"] = req.CovariateConfig
	}
	if req.Covariates != nil {
		payload["covariates"] = req.Covariates
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal predict once payload: %v", err)
	}
	url := fmt.Sprintf("%s/predict_once", s.config.InferenceGateway.BaseURL)
	client := newInferenceGatewayHTTPClient(s.config.InferenceGateway.Timeout)
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, nil, fmt.Errorf("call inference gateway predict once: %v", err)
	}
	defer resp.Body.Close()
	body, err := readGatewayJSONResponse(resp, url, "decode inference gateway predict once response")
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func (s *WatchlistService) GetMTFPredictOnceCached(req *models.MTFPredictRequest) (int, map[string]interface{}, error) {
	if s == nil || s.config == nil || strings.TrimSpace(s.config.PostgresHandler.BaseURL) == "" {
		return 0, nil, fmt.Errorf("postgres handler is not configured")
	}
	if req == nil {
		return http.StatusBadRequest, map[string]interface{}{
			"success": false,
			"message": "request is required",
		}, nil
	}

	stockCode := strings.TrimSpace(req.StockCode)
	horizonLen := intValueOrDefault(req.HorizonLen, 7)
	contextLen := intValueOrDefault(req.ContextLen, 2048)
	baseURL := strings.TrimRight(strings.TrimSpace(s.config.PostgresHandler.BaseURL), "/")
	query := url.Values{}
	query.Set("symbol", normalizeMTFSymbolReadKey(stockCode))
	query.Set("stock_type", strconv.Itoa(normalizeMTFPredictStockType(req.StockType)))
	query.Set("horizon_len", strconv.Itoa(horizonLen))
	query.Set("context_len", strconv.Itoa(contextLen))
	query.Set("prediction_type", normalizeTrainPredictionType(req.PredictionType))
	requestURL := fmt.Sprintf("%s/api/v1/save-predictions/mtf-direct/by-request?%s", baseURL, query.Encode())

	httpReq, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build postgres direct prediction request: %v", err)
	}
	if token := strings.TrimSpace(s.config.PostgresHandler.APIToken); token != "" {
		httpReq.Header.Set("X-Token", token)
	}

	timeout := s.config.PostgresHandler.Timeout
	if timeout <= 0 {
		timeout = 10
	}
	client := newInferenceGatewayHTTPClient(timeout)
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("query postgres direct prediction cache: %v", err)
	}
	defer resp.Body.Close()

	var decoded postgresHandlerDirectPredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("decode postgres direct prediction cache response: %v", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return http.StatusNotFound, directPredictionCacheNotFoundBody(stockCode, "未找到单次预测缓存", "prediction cache not found"), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(decoded.Error)
		if message == "" {
			message = strings.TrimSpace(decoded.Message)
		}
		if message == "" {
			message = fmt.Sprintf("postgres direct prediction cache returned status %d", resp.StatusCode)
		}
		return resp.StatusCode, map[string]interface{}{
			"success": false,
			"message": message,
			"error":   message,
		}, nil
	}
	if decoded.Data == nil {
		return http.StatusNotFound, directPredictionCacheNotFoundBody(stockCode, "未找到单次预测缓存", "prediction cache not found"), nil
	}
	if !isDirectPredictionCacheFresh(decoded.Data, time.Now().UTC()) {
		return http.StatusNotFound, directPredictionCacheNotFoundBody(stockCode, "单次预测缓存已过期", "prediction cache stale"), nil
	}
	data := slimDirectPredictionCacheData(decoded.Data)
	body := map[string]interface{}{
		"success":    true,
		"stock_code": stockCode,
		"message":    "单次预测缓存命中",
		"data":       data,
	}
	if gpuID := strings.TrimSpace(fmt.Sprint(data["gpu_id"])); gpuID != "" && gpuID != "<nil>" {
		body["gpu_id"] = gpuID
	}
	return http.StatusOK, body, nil
}

func directPredictionCacheNotFoundBody(stockCode, message, errorCode string) map[string]interface{} {
	return map[string]interface{}{
		"success":    false,
		"stock_code": stockCode,
		"message":    message,
		"error":      errorCode,
	}
}

func isDirectPredictionCacheFresh(data map[string]interface{}, now time.Time) bool {
	lastDate, ok := latestDirectPredictionFutureDate(data["future_dates"])
	if !ok {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return !lastDate.Before(today)
}

func latestDirectPredictionFutureDate(value interface{}) (time.Time, bool) {
	var rawDates []interface{}
	switch typed := value.(type) {
	case []interface{}:
		rawDates = typed
	case []string:
		rawDates = make([]interface{}, 0, len(typed))
		for _, item := range typed {
			rawDates = append(rawDates, item)
		}
	default:
		return time.Time{}, false
	}

	var latest time.Time
	for _, item := range rawDates {
		dateText := strings.TrimSpace(fmt.Sprint(item))
		if dateText == "" || dateText == "<nil>" {
			continue
		}
		parsed, err := time.Parse(directPredictionCacheDateLayout, dateText)
		if err != nil {
			continue
		}
		if latest.IsZero() || parsed.After(latest) {
			latest = parsed
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

func slimDirectPredictionCacheData(data map[string]interface{}) map[string]interface{} {
	allowedKeys := []string{
		"stock_code",
		"stock_type",
		"prediction_type",
		"context_len",
		"horizon_len",
		"request_end_date",
		"latest_data_date",
		"latest_close",
		"change_base_value",
		"change_base_date",
		"future_dates",
		"best_prediction_item",
		"best_prediction_values",
		"predicted_change_percent",
		"short_name",
		"gpu_id",
	}
	slim := make(map[string]interface{}, len(allowedKeys))
	for _, key := range allowedKeys {
		if value, ok := data[key]; ok {
			slim[key] = value
		}
	}
	for key, value := range data {
		if strings.HasPrefix(key, "adjust_raw_") {
			slim[key] = value
		}
	}
	return slim
}

func (s *WatchlistService) GetMTFJobStatus(jobID string) (int, map[string]interface{}, error) {
	url := fmt.Sprintf("%s/jobs/%s", s.config.InferenceGateway.BaseURL, jobID)
	client := newInferenceGatewayHTTPClient(s.config.InferenceGateway.Timeout)
	resp, err := client.Get(url)
	if err != nil {
		return 0, nil, fmt.Errorf("call inference gateway job status: %v", err)
	}
	defer resp.Body.Close()
	body, err := readGatewayJSONResponse(resp, url, "decode inference gateway job status response")
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, slimMTFJobStatusBody(body), nil
}

func slimMTFJobStatusBody(body map[string]interface{}) map[string]interface{} {
	allowedKeys := []string{
		"job_id",
		"status",
		"stock_code",
		"prediction_type",
		"covariate_signature",
		"current_stage",
		"error",
		"created_at",
		"started_at",
		"finished_at",
		"queue_position",
	}
	slim := make(map[string]interface{}, len(allowedKeys)+1)
	for _, key := range allowedKeys {
		value, ok := body[key]
		if !ok || isEmptyJobStatusValue(value) {
			continue
		}
		slim[key] = value
	}
	if result, ok := slimMTFJobResult(body["result"]); ok {
		slim["result"] = result
	}
	return slim
}

func isEmptyJobStatusValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	default:
		return false
	}
}

func slimMTFJobResult(value interface{}) (map[string]interface{}, bool) {
	result, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	allowedKeys := []string{
		"success",
		"message",
		"stock_code",
		"gpu_id",
		"error",
	}
	slim := make(map[string]interface{}, len(allowedKeys)+1)
	for _, key := range allowedKeys {
		item, exists := result[key]
		if !exists || isEmptyJobStatusValue(item) {
			continue
		}
		slim[key] = item
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		slim["data"] = slimDirectPredictionCacheData(data)
	}
	return slim, len(slim) > 0
}

// 保存MTF最佳分位预测结果到PG（UPSERT by unique_key）
func (s *WatchlistService) SaveMTFBest(req *models.SaveMTFBestRequest) error {
	// 解析日期字符串为DATE由SQL处理
	metricsJSON, err := json.Marshal(req.BestMetrics)
	if err != nil {
		return fmt.Errorf("failed to marshal best_metrics: %v", err)
	}
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal covariate_config: %v", err)
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		return fmt.Errorf("failed to marshal covariate_analysis: %v", err)
	}
	bestValuesJSON, err := marshalOptionalFloat64Array(req.BestPredictionValues, "best_prediction_values")
	if err != nil {
		return err
	}
	futureDatesJSON, err := marshalOptionalStringArray(req.FutureDates, "future_dates")
	if err != nil {
		return err
	}
	adjustRawBestValuesJSON, err := marshalOptionalFloat64Array(req.AdjustRawBestPredictionValues, "adjust_raw_best_prediction_values")
	if err != nil {
		return err
	}
	bestQuantileArg := bestPredictionQuantileArg(req)
	predictionType := normalizedPredictionType(req.PredictionType)
	var covSignatureArg interface{}
	if strings.TrimSpace(req.CovariateSignature) != "" {
		covSignatureArg = req.CovariateSignature
	}

	// MTF best 结果统一公开，便于不同用户复用历史走势。
	isPublic := 1

	_, err = s.db.Conn.Exec(`
        INSERT INTO mtf_best_predictions (
            unique_key, symbol, mtf_version, best_prediction_item, best_metrics,
            prediction_type, covariate_config, covariate_signature, covariate_analysis,
            is_public,
            train_start_date, train_end_date,
            test_start_date, test_end_date,
            val_start_date, val_end_date,
            context_len, horizon_len,
            best_prediction_values, future_dates, adjust_raw_best_prediction_values,
            best_prediction_quantile
        ) VALUES (
            $1, $2, $3, $4, $5::jsonb,
            $6, $7::jsonb, $8, $9::jsonb,
            $10,
            $11::date, $12::date,
            $13::date, $14::date,
            $15::date, $16::date,
            $17, $18,
            $19::jsonb, $20::jsonb, $21::jsonb,
            $22
        )
        ON CONFLICT (unique_key) DO UPDATE SET
            best_prediction_item = EXCLUDED.best_prediction_item,
            best_prediction_quantile = COALESCE(EXCLUDED.best_prediction_quantile, mtf_best_predictions.best_prediction_quantile),
            best_metrics = EXCLUDED.best_metrics,
            prediction_type = EXCLUDED.prediction_type,
            covariate_config = EXCLUDED.covariate_config,
            covariate_signature = EXCLUDED.covariate_signature,
            covariate_analysis = EXCLUDED.covariate_analysis,
            is_public = EXCLUDED.is_public,
            train_start_date = EXCLUDED.train_start_date,
            train_end_date = EXCLUDED.train_end_date,
            test_start_date = EXCLUDED.test_start_date,
            test_end_date = EXCLUDED.test_end_date,
            val_start_date = EXCLUDED.val_start_date,
            val_end_date = EXCLUDED.val_end_date,
            context_len = EXCLUDED.context_len,
            horizon_len = EXCLUDED.horizon_len,
            best_prediction_values = COALESCE(EXCLUDED.best_prediction_values, mtf_best_predictions.best_prediction_values),
            future_dates = COALESCE(EXCLUDED.future_dates, mtf_best_predictions.future_dates),
            adjust_raw_best_prediction_values = COALESCE(EXCLUDED.adjust_raw_best_prediction_values, mtf_best_predictions.adjust_raw_best_prediction_values),
            updated_at = CURRENT_TIMESTAMP
    `,
		req.UniqueKey, req.Symbol, req.MTFVersion, req.BestPredictionItem, string(metricsJSON),
		predictionType, covConfigJSON, covSignatureArg, covAnalysisJSON,
		isPublic,
		req.TrainStartDate, req.TrainEndDate,
		req.TestStartDate, req.TestEndDate,
		req.ValStartDate, req.ValEndDate,
		req.ContextLen, req.HorizonLen,
		bestValuesJSON, futureDatesJSON, adjustRawBestValuesJSON,
		bestQuantileArg,
	)

	if err != nil {
		return fmt.Errorf("failed to upsert mtf_best_predictions: %v", err)
	}
	return nil
}

// 按 unique_key 查询单条 MTF 最佳分位预测记录
func (s *WatchlistService) GetMTFBestByUniqueKey(uniqueKey string) (*models.MTFBestPrediction, error) {
	row := s.db.Conn.QueryRow(`
        SELECT 
            id, unique_key, symbol, mtf_version, best_prediction_item, best_metrics::text,
            prediction_type, COALESCE(covariate_config, '{}'::jsonb)::text,
            COALESCE(covariate_signature, ''),
            COALESCE(covariate_analysis, '{}'::jsonb)::text,
            is_public,
            train_start_date, train_end_date,
            test_start_date, test_end_date,
            val_start_date, val_end_date,
            context_len, horizon_len,
            created_at, updated_at
        FROM mtf_best_predictions
        WHERE unique_key = $1
        LIMIT 1
    `, uniqueKey)

	var item models.MTFBestPrediction
	err := row.Scan(
		&item.ID, &item.UniqueKey, &item.Symbol, &item.MTFVersion, &item.BestPredictionItem, &item.BestMetrics,
		&item.PredictionType,
		&item.CovariateConfig, &item.CovariateSignature, &item.CovariateAnalysis,
		&item.IsPublic,
		&item.TrainStartDate, &item.TrainEndDate,
		&item.TestStartDate, &item.TestEndDate,
		&item.ValStartDate, &item.ValEndDate,
		&item.ContextLen, &item.HorizonLen,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to get mtf_best_predictions by unique_key: %v", err)
	}

	return &item, nil
}

func (s *WatchlistService) GetMTFBestValueByUniqueKey(uniqueKey string) (*models.MTFBestValue, error) {
	row := s.db.Conn.QueryRow(`
        SELECT
            unique_key,
            best_prediction_item,
            best_prediction_quantile,
            COALESCE(best_prediction_values, 'null'::jsonb),
            COALESCE(future_dates, 'null'::jsonb),
            COALESCE(adjust_raw_best_prediction_values, 'null'::jsonb)
        FROM mtf_best_predictions
        WHERE unique_key = $1
        LIMIT 1
    `, uniqueKey)

	var result models.MTFBestValue
	var bestQuantile sql.NullFloat64
	var valuesJSON, datesJSON, adjustRawJSON []byte
	if err := row.Scan(&result.UniqueKey, &result.BestPredictionItem, &bestQuantile, &valuesJSON, &datesJSON, &adjustRawJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to get mtf best value by unique_key: %v", err)
	}
	if bestQuantile.Valid {
		value := bestQuantile.Float64
		result.BestPredictionQuantile = &value
	} else if value, ok := parseBestPredictionQuantile(result.BestPredictionItem); ok {
		result.BestPredictionQuantile = &value
	}

	values, valuesOK, err := decodeOptionalFloat64Array(valuesJSON, "best_prediction_values")
	if err != nil {
		return nil, err
	}
	dates, datesOK, err := decodeOptionalStringArray(datesJSON, "future_dates")
	if err != nil {
		return nil, err
	}
	adjustRawValues, _, err := decodeOptionalFloat64Array(adjustRawJSON, "adjust_raw_best_prediction_values")
	if err != nil {
		return nil, err
	}
	if valuesOK && datesOK {
		result.BestPredictionValues = values
		result.FutureDates = dates
		result.AdjustRawBestPredictionValues = adjustRawValues
		result.Source = "saved"
		return &result, nil
	}

	dates, values, predictedLatest, actualLatest, changePct, err := s.ListFuturePredictionsByUniqueKey(uniqueKey)
	if err != nil {
		return nil, err
	}
	result.BestPredictionValues = values
	result.FutureDates = dates
	result.PredictedLatest = predictedLatest
	result.ActualLatest = actualLatest
	result.PredictedChangePercent = changePct
	result.Source = "future_chunks"
	return &result, nil
}

func decodeOptionalFloat64Array(data []byte, field string) ([]float64, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 || string(bytes.TrimSpace(data)) == "null" {
		return nil, false, nil
	}
	var values []float64
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal %s: %v", field, err)
	}
	return values, len(values) > 0, nil
}

func decodeOptionalStringArray(data []byte, field string) ([]string, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 || string(bytes.TrimSpace(data)) == "null" {
		return nil, false, nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal %s: %v", field, err)
	}
	return values, len(values) > 0, nil
}

func (s *WatchlistService) GetMTFBestUniqueKeysByConfig(symbol string, horizonLen int, contextLen int, mtfVersion string) (*models.MTFBestUniqueKeysByConfig, error) {
	normalizedSymbol := normalizeMTFSymbolReadKey(symbol)
	canonicalSymbolExpr := mtfCanonicalSymbolExpr("symbol")

	rows, err := s.db.Conn.Query(fmt.Sprintf(`
        SELECT prediction_type, unique_key
        FROM (
            SELECT DISTINCT ON (prediction_type)
                prediction_type,
                unique_key,
                updated_at,
                id
            FROM mtf_best_predictions
            WHERE %s = $1
              AND horizon_len = $2
              AND context_len = $3
              AND ($4 = '' OR mtf_version = $4)
            ORDER BY prediction_type,
                     CASE WHEN %s = lower(trim(symbol)) THEN 0 ELSE 1 END,
                     updated_at DESC,
                     id DESC
        ) latest
    `, canonicalSymbolExpr, canonicalSymbolExpr), normalizedSymbol, horizonLen, contextLen, strings.TrimSpace(mtfVersion))
	if err != nil {
		return nil, fmt.Errorf("failed to query mtf_best_predictions by config: %v", err)
	}
	defer rows.Close()

	result := &models.MTFBestUniqueKeysByConfig{
		Symbol:           normalizedSymbol,
		MTFVersion:       strings.TrimSpace(mtfVersion),
		HorizonLen:       horizonLen,
		ContextLen:       contextLen,
		MTFLiteUniqueKey: "",
		MTFProUniqueKey:  "",
	}

	found := 0
	for rows.Next() {
		var predictionType string
		var uniqueKey string
		if err := rows.Scan(&predictionType, &uniqueKey); err != nil {
			return nil, fmt.Errorf("failed to scan mtf_best_predictions by config: %v", err)
		}
		switch normalizeTrainPredictionType(predictionType) {
		case "mtf-pro":
			result.MTFProUniqueKey = uniqueKey
			found++
		default:
			result.MTFLiteUniqueKey = uniqueKey
			found++
		}
	}
	if found == 0 {
		return nil, sql.ErrNoRows
	}

	return result, nil
}

// 按 unique_key 查询单条 MTF 回测结果
func (s *WatchlistService) GetMTFBacktestByUniqueKey(uniqueKey string) (map[string]interface{}, error) {
	row := s.db.Conn.QueryRow(`
        SELECT 
            unique_key, symbol, mtf_version, context_len, horizon_len,
            covariate_config, covariate_signature, covariate_analysis,
            used_quantile, buy_threshold_pct, sell_threshold_pct, trade_fee_rate, total_fees_paid, actual_total_return_pct,
            benchmark_return_pct, benchmark_annualized_return_pct, period_days,
            validation_start_date, validation_end_date, validation_benchmark_return_pct, validation_benchmark_annualized_return_pct, validation_period_days,
            position_control, predicted_change_stats, per_chunk_signals,
            equity_curve_values, equity_curve_pct, equity_curve_pct_gross, curve_dates, actual_end_prices, trades
        FROM mtf_backtests
        WHERE unique_key = $1
        LIMIT 1
    `, uniqueKey)

	var (
		uniqueKeyOut, symbol, mtfVersion                                                     string
		covariateConfig, covariateAnalysis                                                   []byte
		covariateSignature                                                                   sql.NullString
		contextLen, horizonLen, periodDays, validationPeriodDays                             int
		usedQuantile                                                                         string
		buyThresholdPct, sellThresholdPct, tradeFeeRate, totalFeesPaid, actualTotalReturnPct float64
		benchmarkReturnPct, benchmarkAnnualizedReturnPct                                     float64
		validationStartDate, validationEndDate                                               sql.NullTime
		validationBenchmarkReturnPct, validationBenchmarkAnnualizedReturnPct                 float64
		positionControl, predictedChangeStats, perChunkSignals                               []byte
		equityCurveValues, equityCurvePct, equityCurvePctGross                               []byte
		curveDates, actualEndPrices, trades                                                  []byte
	)

	err := row.Scan(
		&uniqueKeyOut, &symbol, &mtfVersion, &contextLen, &horizonLen,
		&covariateConfig, &covariateSignature, &covariateAnalysis,
		&usedQuantile, &buyThresholdPct, &sellThresholdPct, &tradeFeeRate, &totalFeesPaid, &actualTotalReturnPct,
		&benchmarkReturnPct, &benchmarkAnnualizedReturnPct, &periodDays,
		&validationStartDate, &validationEndDate, &validationBenchmarkReturnPct, &validationBenchmarkAnnualizedReturnPct, &validationPeriodDays,
		&positionControl, &predictedChangeStats, &perChunkSignals,
		&equityCurveValues, &equityCurvePct, &equityCurvePctGross, &curveDates, &actualEndPrices, &trades,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to get mtf_backtests by unique_key: %v", err)
	}

	out := map[string]interface{}{
		"unique_key":                      uniqueKeyOut,
		"symbol":                          symbol,
		"mtf_version":                     mtfVersion,
		"context_len":                     contextLen,
		"horizon_len":                     horizonLen,
		"covariate_analysis":              covariateAnalysis,
		"used_quantile":                   usedQuantile,
		"buy_threshold_pct":               buyThresholdPct,
		"sell_threshold_pct":              sellThresholdPct,
		"trade_fee_rate":                  tradeFeeRate,
		"total_fees_paid":                 totalFeesPaid,
		"actual_total_return_pct":         actualTotalReturnPct,
		"benchmark_return_pct":            benchmarkReturnPct,
		"benchmark_annualized_return_pct": benchmarkAnnualizedReturnPct,
		"period_days":                     periodDays,
		"validation_start_date": func() interface{} {
			if validationStartDate.Valid {
				return validationStartDate.Time.Format("2006-01-02")
			}
			return nil
		}(),
		"validation_end_date": func() interface{} {
			if validationEndDate.Valid {
				return validationEndDate.Time.Format("2006-01-02")
			}
			return nil
		}(),
		"validation_benchmark_return_pct":            validationBenchmarkReturnPct,
		"validation_benchmark_annualized_return_pct": validationBenchmarkAnnualizedReturnPct,
		"validation_period_days":                     validationPeriodDays,
		"position_control":                           positionControl,
		"predicted_change_stats":                     predictedChangeStats,
		"per_chunk_signals":                          perChunkSignals,
		"equity_curve_values":                        equityCurveValues,
		"equity_curve_pct":                           equityCurvePct,
		"equity_curve_pct_gross":                     equityCurvePctGross,
		"curve_dates":                                curveDates,
		"actual_end_prices":                          actualEndPrices,
		"trades":                                     trades,
	}
	if covariateSignature.Valid {
		out["covariate_signature"] = covariateSignature.String
	}
	s.applyCurrentBacktestReferenceMetrics(uniqueKeyOut, out)
	return out, nil
}

// 按用户ID查询其所有MTF最佳分位预测列表
func (s *WatchlistService) ListMTFBestByUserID(userID int) ([]models.MTFBestPrediction, error) {
	rows, err := s.db.Conn.Query(`
        SELECT 
            id, unique_key, symbol, mtf_version, best_prediction_item, best_metrics::text,
            prediction_type, COALESCE(covariate_config, '{}'::jsonb)::text,
            COALESCE(covariate_signature, ''),
            COALESCE(covariate_analysis, '{}'::jsonb)::text,
            is_public,
            train_start_date, train_end_date,
            test_start_date, test_end_date,
            val_start_date, val_end_date,
            context_len, horizon_len,
            created_at, updated_at
        FROM mtf_best_predictions
        WHERE EXISTS (
            SELECT 1 FROM mtf_best_validation_chunks vc
            WHERE vc.unique_key = mtf_best_predictions.unique_key
              AND vc.user_id = $1
        )
        ORDER BY updated_at DESC
    `, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query mtf_best_predictions by user_id: %v", err)
	}
	defer rows.Close()

	var results []models.MTFBestPrediction
	for rows.Next() {
		var item models.MTFBestPrediction
		if err := rows.Scan(
			&item.ID, &item.UniqueKey, &item.Symbol, &item.MTFVersion, &item.BestPredictionItem, &item.BestMetrics,
			&item.PredictionType,
			&item.CovariateConfig, &item.CovariateSignature, &item.CovariateAnalysis,
			&item.IsPublic,
			&item.TrainStartDate, &item.TrainEndDate,
			&item.TestStartDate, &item.TestEndDate,
			&item.ValStartDate, &item.ValEndDate,
			&item.ContextLen, &item.HorizonLen,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan mtf_best_predictions: %v", err)
		}
		results = append(results, item)
	}
	return results, nil
}

// 普通用户只查询公开数据；管理员可同时查询公开/非公开数据，并支持按 horizon_len / symbol 筛选
func (s *WatchlistService) listScopedMTFBest(horizonLen int, symbol string, userID *int, includePrivate bool) ([]models.MTFBestPrediction, error) {
	page, err := s.listScopedMTFBestPage(horizonLen, symbol, userID, includePrivate, 0, 0, 0)
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (s *WatchlistService) listScopedMTFBestPage(horizonLen int, symbol string, userID *int, includePrivate bool, limit int, offset int, stockType int) (MTFBestPage, error) {
	if limit < 0 {
		limit = 0
	}
	if offset < 0 {
		offset = 0
	}
	baseCanonicalSymbolExpr := mtfCanonicalSymbolExpr("symbol")
	rowCanonicalSymbolExpr := mtfCanonicalSymbolExpr("t.symbol")
	query := `
        WITH scoped_best AS (
            SELECT p.*
            FROM mtf_best_predictions p
            WHERE 1 = 1
    `
	var args []interface{}
	var conditions []string
	if includePrivate {
		// admins can access all public/private records
	} else if userID != nil {
		args = append(args, *userID)
		conditions = append(conditions, fmt.Sprintf(`(
			p.is_public = 1 OR EXISTS (
				SELECT 1
				FROM mtf_best_validation_chunks vc
				WHERE vc.unique_key = p.unique_key
				  AND vc.user_id = $%d
			)
		)`, len(args)))
	} else {
		conditions = append(conditions, "p.is_public = 1")
	}
	if horizonLen > 0 {
		args = append(args, horizonLen)
		conditions = append(conditions, fmt.Sprintf("p.horizon_len = $%d", len(args)))
	}
	if normalizedSymbol := normalizeMTFSymbolReadKey(symbol); normalizedSymbol != "" {
		args = append(args, normalizedSymbol)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", mtfCanonicalSymbolExpr("p.symbol"), len(args)))
	}
	if len(conditions) > 0 {
		query += ` AND ` + strings.Join(conditions, " AND ")
	}
	query += fmt.Sprintf(`
        ),
        public_groups AS (
            SELECT DISTINCT
                %s AS canonical_symbol,
                horizon_len,
                context_len,
                mtf_version
            FROM scoped_best
        ),
        watchlist_counts AS (
            SELECT
                %s AS canonical_symbol,
                COUNT(*)::int AS watchlist_count
            FROM user_watchlist
            GROUP BY %s
        ),
        validation_stock_types AS (
            SELECT
                unique_key,
                MIN(NULLIF(stock_type, 0))::int AS stock_type
            FROM mtf_best_validation_chunks
            GROUP BY unique_key
        ),
        ranked AS (
            SELECT
                t.id,
                t.unique_key,
                %s AS symbol,
                t.mtf_version,
                t.best_prediction_item,
                t.best_metrics::text AS best_metrics,
                t.prediction_type,
                COALESCE(t.covariate_config, '{}'::jsonb)::text AS covariate_config,
                COALESCE(t.covariate_signature, '') AS covariate_signature,
                COALESCE(t.covariate_analysis, '{}'::jsonb)::text AS covariate_analysis,
                t.is_public,
                t.train_start_date,
                t.train_end_date,
                t.test_start_date,
                t.test_end_date,
                t.val_start_date,
                t.val_end_date,
                t.context_len,
                t.horizon_len,
                t.created_at,
                t.updated_at,
                COALESCE(t.short_name, '') AS short_name,
	                COALESCE(vst.stock_type, 1)::int AS stock_type,
                COALESCE(wc.watchlist_count, 0)::int AS watchlist_count,
                ROW_NUMBER() OVER (
                    PARTITION BY %s, t.horizon_len, t.context_len, t.mtf_version, COALESCE(NULLIF(TRIM(t.prediction_type), ''), 'mtf-lite')
                    ORDER BY CASE WHEN %s = lower(trim(t.symbol)) THEN 0 ELSE 1 END,
                             t.updated_at DESC,
                             t.id DESC
                ) AS rn
            FROM scoped_best t
            INNER JOIN public_groups g
                ON g.canonical_symbol = %s
               AND g.horizon_len = t.horizon_len
               AND g.context_len = t.context_len
               AND g.mtf_version = t.mtf_version
            LEFT JOIN watchlist_counts wc
                ON wc.canonical_symbol = %s
            LEFT JOIN validation_stock_types vst
                ON vst.unique_key = t.unique_key
        ),
        filtered_ranked AS (
            SELECT *
            FROM ranked
            WHERE rn = 1
    `, baseCanonicalSymbolExpr, mtfCanonicalSymbolExpr("symbol"), mtfCanonicalSymbolExpr("symbol"), rowCanonicalSymbolExpr, rowCanonicalSymbolExpr, rowCanonicalSymbolExpr, rowCanonicalSymbolExpr, rowCanonicalSymbolExpr)
	if stockType > 0 {
		args = append(args, stockType)
		query += fmt.Sprintf(" AND stock_type = $%d", len(args))
	}
	query += `
        ),
        paged_symbols AS (
            SELECT
                symbol,
                MAX(watchlist_count)::int AS watchlist_count,
                MAX(created_at) AS latest_created_at,
                MAX(id) AS latest_id,
                COUNT(*) OVER() AS total_count
            FROM filtered_ranked
            GROUP BY symbol
            ORDER BY MAX(watchlist_count) DESC, MAX(created_at) DESC, MAX(id) DESC
    `
	if limit > 0 {
		args = append(args, limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if offset > 0 {
		args = append(args, offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}
	query += `
        )
        SELECT
            r.id, r.unique_key, r.symbol, r.mtf_version, r.best_prediction_item, r.best_metrics,
            r.prediction_type, r.covariate_config, r.covariate_signature, r.covariate_analysis,
            r.is_public,
            r.train_start_date, r.train_end_date,
            r.test_start_date, r.test_end_date,
            r.val_start_date, r.val_end_date,
            r.context_len, r.horizon_len,
            r.created_at, r.updated_at, r.short_name,
            r.stock_type, r.watchlist_count,
            ps.total_count
        FROM filtered_ranked r
        INNER JOIN paged_symbols ps ON ps.symbol = r.symbol
        ORDER BY ps.watchlist_count DESC, ps.latest_created_at DESC, ps.latest_id DESC,
                 r.symbol ASC, r.horizon_len ASC, r.context_len ASC, r.mtf_version ASC, r.prediction_type ASC, r.updated_at DESC
    `

	rows, err := s.db.Conn.Query(query, args...)
	if err != nil {
		return MTFBestPage{}, fmt.Errorf("failed to query public mtf_best_predictions: %v", err)
	}
	defer rows.Close()

	var results []models.MTFBestPrediction
	total := 0
	for rows.Next() {
		var item models.MTFBestPrediction
		if err := rows.Scan(
			&item.ID, &item.UniqueKey, &item.Symbol, &item.MTFVersion, &item.BestPredictionItem, &item.BestMetrics,
			&item.PredictionType,
			&item.CovariateConfig, &item.CovariateSignature, &item.CovariateAnalysis,
			&item.IsPublic,
			&item.TrainStartDate, &item.TrainEndDate,
			&item.TestStartDate, &item.TestEndDate,
			&item.ValStartDate, &item.ValEndDate,
			&item.ContextLen, &item.HorizonLen,
			&item.CreatedAt, &item.UpdatedAt, &item.ShortName,
			&item.StockType, &item.WatchlistCount, &total,
		); err != nil {
			return MTFBestPage{}, fmt.Errorf("failed to scan public mtf_best_predictions: %v", err)
		}
		if strings.TrimSpace(item.ShortName) == "" {
			item.ShortName = s.lookupDisplayName(item.Symbol)
		}
		results = append(results, item)
	}

	return MTFBestPage{Items: results, Total: total, Limit: limit, Offset: offset}, nil
}

func (s *WatchlistService) ListPublicMTFBest(horizonLen int, symbol string, includePrivate bool) ([]models.MTFBestPrediction, error) {
	return s.listScopedMTFBest(horizonLen, symbol, nil, includePrivate)
}

func (s *WatchlistService) ListPublicMTFBestPage(horizonLen int, symbol string, includePrivate bool, limit int, offset int) (MTFBestPage, error) {
	return s.listScopedMTFBestPage(horizonLen, symbol, nil, includePrivate, limit, offset, 0)
}

func (s *WatchlistService) ListPublicMTFBestPageByStockType(horizonLen int, symbol string, includePrivate bool, limit int, offset int, stockType int) (MTFBestPage, error) {
	return s.listScopedMTFBestPage(horizonLen, symbol, nil, includePrivate, limit, offset, stockType)
}

func (s *WatchlistService) ListAccessibleMTFBest(userID int, horizonLen int, symbol string, includePrivate bool) ([]models.MTFBestPrediction, error) {
	uid := userID
	items, err := s.listScopedMTFBest(horizonLen, symbol, &uid, includePrivate)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := s.triggerStaleMTFBestRefresh(item); err != nil {
			log.Printf("stale mtf best refresh enqueue failed: symbol=%s unique_key=%s err=%v", item.Symbol, item.UniqueKey, err)
		}
	}
	return items, nil
}

// 根据 unique_key 查询对应的验证集分块列表
func (s *WatchlistService) ListValidationChunksByUniqueKey(uniqueKey string) ([]models.SaveMTFValChunkRequest, error) {
	chunksByKey, err := s.ListValidationChunksByUniqueKeys([]string{uniqueKey})
	if err != nil {
		return nil, err
	}
	return chunksByKey[uniqueKey], nil
}

func (s *WatchlistService) ListValidationChunksByUniqueKeys(uniqueKeys []string) (map[string][]models.SaveMTFValChunkRequest, error) {
	keys := uniqueNonEmptyStrings(uniqueKeys)
	chunksByKey := make(map[string][]models.SaveMTFValChunkRequest, len(keys))
	for _, key := range keys {
		chunksByKey[key] = []models.SaveMTFValChunkRequest{}
	}
	if len(keys) == 0 {
		return chunksByKey, nil
	}

	rows, err := s.db.Conn.Query(`
        SELECT 
            unique_key, chunk_index, start_date::text, end_date::text, symbol,
            predictions, actual_values,
            COALESCE(predicted_change_percent, '{}'::jsonb),
            COALESCE(actual_change_percent, '[]'::jsonb),
            change_base_value,
            change_base_date::text,
            dates, COALESCE(prediction_type, 'mtf-lite'),
            covariate_config, covariate_signature, covariate_analysis,
            COALESCE(stock_type, 0),
            COALESCE(adjust_raw_chunks, 'null'::jsonb)
        FROM mtf_best_validation_chunks
        WHERE unique_key = ANY($1)
        ORDER BY unique_key ASC, chunk_index ASC
    `, pq.Array(keys))
	if err != nil {
		return nil, fmt.Errorf("failed to query validation chunks by unique_keys: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		req, err := scanMTFValidationChunk(rows)
		if err != nil {
			return nil, err
		}
		chunksByKey[req.UniqueKey] = append(chunksByKey[req.UniqueKey], req)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate validation chunks: %v", err)
	}
	return chunksByKey, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func scanMTFValidationChunk(rows *sql.Rows) (models.SaveMTFValChunkRequest, error) {
	var req models.SaveMTFValChunkRequest
	var predsJSON, actualJSON, predChangeJSON, actualChangeJSON, datesJSON []byte
	var changeBaseValue sql.NullFloat64
	var changeBaseDate sql.NullString
	var covConfigJSON, covAnalysisJSON, adjustRawJSON []byte
	var covSignature sql.NullString
	if err := rows.Scan(
		&req.UniqueKey, &req.ChunkIndex, &req.StartDate, &req.EndDate, &req.Symbol,
		&predsJSON, &actualJSON, &predChangeJSON, &actualChangeJSON, &changeBaseValue, &changeBaseDate, &datesJSON, &req.PredictionType,
		&covConfigJSON, &covSignature, &covAnalysisJSON, &req.StockType, &adjustRawJSON,
	); err != nil {
		return req, fmt.Errorf("failed to scan validation chunk: %v", err)
	}
	if err := unmarshalValidationChunkJSON(predsJSON, &req.Predictions, "predictions"); err != nil {
		return req, err
	}
	if err := unmarshalValidationChunkJSON(actualJSON, &req.Actual, "actual_values"); err != nil {
		return req, err
	}
	if err := unmarshalValidationChunkJSON(predChangeJSON, &req.PredictedChangePct, "predicted_change_percent"); err != nil {
		return req, err
	}
	if err := unmarshalValidationChunkJSON(actualChangeJSON, &req.ActualChangePct, "actual_change_percent"); err != nil {
		return req, err
	}
	if changeBaseValue.Valid {
		value := changeBaseValue.Float64
		req.ChangeBaseValue = &value
	}
	if changeBaseDate.Valid {
		value := changeBaseDate.String
		req.ChangeBaseDate = &value
	}
	if err := unmarshalValidationChunkJSON(datesJSON, &req.Dates, "dates"); err != nil {
		return req, err
	}
	if len(covConfigJSON) > 0 {
		if err := unmarshalValidationChunkJSON(covConfigJSON, &req.CovariateConfig, "covariate_config"); err != nil {
			return req, err
		}
	}
	if covSignature.Valid {
		req.CovariateSignature = covSignature.String
	}
	if len(covAnalysisJSON) > 0 {
		if err := unmarshalValidationChunkJSON(covAnalysisJSON, &req.CovariateAnalysis, "covariate_analysis"); err != nil {
			return req, err
		}
	}
	if len(adjustRawJSON) > 0 && string(adjustRawJSON) != "null" {
		var adjustRawChunks interface{}
		if err := unmarshalValidationChunkJSON(adjustRawJSON, &adjustRawChunks, "adjust_raw_chunks"); err != nil {
			return req, err
		}
		req.AdjustRawChunks = adjustRawChunks
	}
	return req, nil
}

func unmarshalValidationChunkJSON(data []byte, target interface{}, field string) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal %s: %v", field, err)
	}
	return nil
}

func (s *WatchlistService) GetLatestValidationChunkByUniqueKey(uniqueKey string) (map[string]interface{}, error) {
	row := s.db.Conn.QueryRow(`
        SELECT
            vc.unique_key,
            vc.chunk_index,
            vc.symbol,
            vc.start_date,
            vc.end_date,
            COALESCE(vc.prediction_type, 'mtf-lite'),
            COALESCE(vc.covariate_config, '{}'::jsonb),
            COALESCE(vc.covariate_signature, ''),
            COALESCE(vc.covariate_analysis, '{}'::jsonb),
            COALESCE(st.company_name, '') AS stock_name
        FROM mtf_best_validation_chunks vc
        LEFT JOIN stocks st ON st.symbol = vc.symbol
        WHERE vc.unique_key = $1
        ORDER BY vc.chunk_index DESC
        LIMIT 1
    `, uniqueKey)

	var (
		key            string
		chunkIdx       int
		symbol         string
		startDate      time.Time
		endDate        time.Time
		predictionType string
		covConfig      []byte
		covSignature   string
		covAnalysis    []byte
		stockName      string
	)

	if err := row.Scan(&key, &chunkIdx, &symbol, &startDate, &endDate, &predictionType, &covConfig, &covSignature, &covAnalysis, &stockName); err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to get latest validation chunk by unique_key: %v", err)
	}

	return map[string]interface{}{
		"unique_key":          key,
		"chunk_index":         chunkIdx,
		"symbol":              symbol,
		"prediction_type":     predictionType,
		"covariate_signature": covSignature,
		"covariate_analysis":  covAnalysis,
		"stock_name":          stockName,
		"stock_type":          1,
		"start_date":          startDate.Format("2006-01-02"),
		"end_date":            endDate.Format("2006-01-02"),
	}, nil
}

func (s *WatchlistService) ListFuturePredictionsByUniqueKey(uniqueKey string) ([]string, []float64, float64, float64, float64, error) {
	row := s.db.Conn.QueryRow(`SELECT best_prediction_item FROM mtf_best_predictions WHERE unique_key = $1 LIMIT 1`, uniqueKey)
	var bestItem string
	if err := row.Scan(&bestItem); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("failed to get best_prediction_item: %v", err)
	}

	rows, err := s.db.Conn.Query(`
        SELECT dates, predictions
        FROM mtf_best_validation_chunks
        WHERE unique_key = $1 AND start_date >= CURRENT_DATE + INTERVAL '1 day'
        ORDER BY chunk_index ASC
    `, uniqueKey)
	if err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("failed to query future validation chunks: %v", err)
	}
	defer rows.Close()

	var outDates []string
	var outPreds []float64
	var predictedLatest float64 = 0

	for rows.Next() {
		var datesJSON, predsJSON []byte
		if err := rows.Scan(&datesJSON, &predsJSON); err != nil {
			return nil, nil, 0, 0, 0, fmt.Errorf("failed to scan future chunk: %v", err)
		}
		var dates []string
		var preds map[string]interface{}
		if err := json.Unmarshal(datesJSON, &dates); err != nil {
			return nil, nil, 0, 0, 0, fmt.Errorf("failed to unmarshal dates: %v", err)
		}
		if err := json.Unmarshal(predsJSON, &preds); err != nil {
			return nil, nil, 0, 0, 0, fmt.Errorf("failed to unmarshal predictions: %v", err)
		}
		val, ok := preds[bestItem]
		if !ok {
			continue
		}
		arr, ok := val.([]interface{})
		if !ok {
			continue
		}
		maxLen := len(dates)
		if len(arr) < maxLen {
			maxLen = len(arr)
		}
		for i := 0; i < maxLen; i++ {
			var p float64
			switch v := arr[i].(type) {
			case float64:
				p = v
			case float32:
				p = float64(v)
			case int:
				p = float64(v)
			case int64:
				p = float64(v)
			case json.Number:
				if f, e := v.Float64(); e == nil {
					p = f
				} else {
					continue
				}
			default:
				continue
			}
			if p == 0 || math.IsNaN(p) || math.IsInf(p, 0) {
				continue
			}
			outDates = append(outDates, dates[i])
			outPreds = append(outPreds, p)
		}
	}

	// compute predicted latest price
	for i := len(outPreds) - 1; i >= 0; i-- {
		if outPreds[i] != 0 && !math.IsNaN(outPreds[i]) && !math.IsInf(outPreds[i], 0) {
			predictedLatest = outPreds[i]
			break
		}
	}

	// fetch latest actual price from chunks with start_date <= CURRENT_DATE
	var actualLatest float64 = 0
	pastRows, err := s.db.Conn.Query(`
        SELECT actual_values
        FROM mtf_best_validation_chunks
        WHERE unique_key = $1 AND start_date <= CURRENT_DATE
        ORDER BY chunk_index ASC
    `, uniqueKey)
	if err == nil {
		defer pastRows.Close()
		for pastRows.Next() {
			var actualJSON []byte
			if err := pastRows.Scan(&actualJSON); err != nil {
				continue
			}
			var actuals []float64
			// actual_values stored as JSON array of numbers
			if err := json.Unmarshal(actualJSON, &actuals); err != nil {
				continue
			}
			for i := len(actuals) - 1; i >= 0; i-- {
				a := actuals[i]
				if a != 0 && !math.IsNaN(a) && !math.IsInf(a, 0) {
					actualLatest = a
					break
				}
			}
		}
	}

	// compute change percent between predicted latest vs latest actual
	var changePercent float64 = 0
	if actualLatest > 0 && predictedLatest > 0 {
		changePercent = (predictedLatest - actualLatest) / actualLatest * 100
	}

	return outDates, outPreds, predictedLatest, actualLatest, changePercent, nil
}

// 保存验证集分块的预测与实际值（UPSERT by unique_key+chunk_index）
func (s *WatchlistService) SaveMTFValChunk(req *models.SaveMTFValChunkRequest) error {
	predsJSON, err := json.Marshal(req.Predictions)
	if err != nil {
		return fmt.Errorf("failed to marshal predictions: %v", err)
	}
	actualJSON, err := json.Marshal(req.Actual)
	if err != nil {
		return fmt.Errorf("failed to marshal actual_values: %v", err)
	}
	predictedChangeJSON := "{}"
	if req.PredictedChangePct != nil {
		b, err := json.Marshal(req.PredictedChangePct)
		if err != nil {
			return fmt.Errorf("failed to marshal predicted_change_percent: %v", err)
		}
		predictedChangeJSON = string(b)
	}
	actualChangeJSON := "[]"
	if req.ActualChangePct != nil {
		b, err := json.Marshal(req.ActualChangePct)
		if err != nil {
			return fmt.Errorf("failed to marshal actual_change_percent: %v", err)
		}
		actualChangeJSON = string(b)
	}
	datesJSON, err := json.Marshal(req.Dates)
	if err != nil {
		return fmt.Errorf("failed to marshal dates: %v", err)
	}
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal covariate_config: %v", err)
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		return fmt.Errorf("failed to marshal covariate_analysis: %v", err)
	}
	predictionType := normalizedPredictionType(req.PredictionType)
	var covSignatureArg interface{}
	if strings.TrimSpace(req.CovariateSignature) != "" {
		covSignatureArg = req.CovariateSignature
	}
	var stockTypeArg interface{}
	if req.StockType > 0 {
		stockTypeArg = req.StockType
	}

	// 处理可选的 user_id 指针
	var uidArg interface{}
	if req.UserID != nil {
		uidArg = *req.UserID
	} else {
		uidArg = nil
	}
	var changeBaseValueArg interface{}
	if req.ChangeBaseValue != nil {
		changeBaseValueArg = *req.ChangeBaseValue
	}
	var changeBaseDateArg interface{}
	if req.ChangeBaseDate != nil && strings.TrimSpace(*req.ChangeBaseDate) != "" {
		changeBaseDateArg = *req.ChangeBaseDate
	}
	var adjustRawChunksArg interface{}
	if req.AdjustRawChunks != nil {
		raw, err := json.Marshal(req.AdjustRawChunks)
		if err != nil {
			return fmt.Errorf("failed to marshal adjust_raw_chunks: %v", err)
		}
		if string(raw) != "null" {
			adjustRawChunksArg = string(raw)
		}
	}

	_, err = s.db.Conn.Exec(`
        INSERT INTO mtf_best_validation_chunks (
            unique_key, chunk_index, user_id, symbol, start_date, end_date,
            predictions, actual_values, predicted_change_percent, actual_change_percent, change_base_value, change_base_date, dates,
            prediction_type, covariate_config, covariate_signature, covariate_analysis, stock_type, adjust_raw_chunks
        ) VALUES (
            $1, $2, $3, $4, $5::date, $6::date,
            $7::jsonb, $8::jsonb, $9::jsonb, $10::jsonb, $11, $12::date, $13::jsonb,
            $14, $15::jsonb, $16, $17::jsonb, $18, $19::jsonb
        )
        ON CONFLICT (unique_key, chunk_index) DO UPDATE SET
            start_date = EXCLUDED.start_date,
            end_date = EXCLUDED.end_date,
            predictions = EXCLUDED.predictions,
            actual_values = EXCLUDED.actual_values,
            predicted_change_percent = EXCLUDED.predicted_change_percent,
            actual_change_percent = EXCLUDED.actual_change_percent,
            change_base_value = EXCLUDED.change_base_value,
            change_base_date = EXCLUDED.change_base_date,
            dates = EXCLUDED.dates,
            prediction_type = EXCLUDED.prediction_type,
            covariate_config = EXCLUDED.covariate_config,
            covariate_signature = EXCLUDED.covariate_signature,
            covariate_analysis = EXCLUDED.covariate_analysis,
            stock_type = EXCLUDED.stock_type,
            adjust_raw_chunks = EXCLUDED.adjust_raw_chunks,
            updated_at = CURRENT_TIMESTAMP
    `,
		req.UniqueKey, req.ChunkIndex, uidArg, req.Symbol, req.StartDate, req.EndDate,
		string(predsJSON), string(actualJSON), predictedChangeJSON, actualChangeJSON, changeBaseValueArg, changeBaseDateArg, string(datesJSON),
		predictionType, covConfigJSON, covSignatureArg, covAnalysisJSON, stockTypeArg, adjustRawChunksArg,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert mtf_best_validation_chunks: %v", err)
	}
	return nil
}

// 保存 MTF 回测结果到 PG（UPSERT by unique_key）
func (s *WatchlistService) SaveMTFBacktest(req *models.SaveMTFBacktestRequest) error {
	posJSON, err := json.Marshal(req.PositionControl)
	if err != nil {
		return fmt.Errorf("failed to marshal position_control: %v", err)
	}
	statsJSON, err := json.Marshal(req.PredictedChangeStats)
	if err != nil {
		return fmt.Errorf("failed to marshal predicted_change_stats: %v", err)
	}
	signalsJSON, err := json.Marshal(req.PerChunkSignals)
	if err != nil {
		return fmt.Errorf("failed to marshal per_chunk_signals: %v", err)
	}
	eqValsJSON, err := json.Marshal(req.EquityCurveValues)
	if err != nil {
		return fmt.Errorf("failed to marshal equity_curve_values: %v", err)
	}
	eqPctJSON, err := json.Marshal(req.EquityCurvePct)
	if err != nil {
		return fmt.Errorf("failed to marshal equity_curve_pct: %v", err)
	}
	eqPctGrossJSON, err := json.Marshal(req.EquityCurvePctGross)
	if err != nil {
		return fmt.Errorf("failed to marshal equity_curve_pct_gross: %v", err)
	}
	curveDatesJSON, err := json.Marshal(req.CurveDates)
	if err != nil {
		return fmt.Errorf("failed to marshal curve_dates: %v", err)
	}
	actualEndJSON, err := json.Marshal(req.ActualEndPrices)
	if err != nil {
		return fmt.Errorf("failed to marshal actual_end_prices: %v", err)
	}
	tradesJSON, err := json.Marshal(req.Trades)
	if err != nil {
		return fmt.Errorf("failed to marshal trades: %v", err)
	}
	covConfigJSON, err := marshalOptionalJSONObject(req.CovariateConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal covariate_config: %v", err)
	}
	covAnalysisJSON, err := marshalOptionalJSONObject(req.CovariateAnalysis)
	if err != nil {
		return fmt.Errorf("failed to marshal covariate_analysis: %v", err)
	}
	var covSignatureArg interface{}
	if strings.TrimSpace(req.CovariateSignature) != "" {
		covSignatureArg = req.CovariateSignature
	}

	var uidArg interface{}
	if req.UserID != nil {
		uidArg = *req.UserID
	} else {
		uidArg = nil
	}
	var spIDArg interface{}
	if req.StrategyParamsID != nil {
		spIDArg = *req.StrategyParamsID
	} else {
		var spID int
		if req.UserID != nil {
			row := s.db.Conn.QueryRow(`SELECT id FROM mtf_strategy_params WHERE unique_key = $1 AND user_id = $2 LIMIT 1`, req.UniqueKey, *req.UserID)
			if err := row.Scan(&spID); err == nil {
				spIDArg = spID
			} else {
				spIDArg = nil
			}
		} else {
			row := s.db.Conn.QueryRow(`SELECT id FROM mtf_strategy_params WHERE unique_key = $1 LIMIT 1`, req.UniqueKey)
			if err := row.Scan(&spID); err == nil {
				spIDArg = spID
			} else {
				spIDArg = nil
			}
		}
	}

	_, err = s.db.Conn.Exec(`
        INSERT INTO mtf_backtests (
            unique_key, user_id, strategy_params_id, symbol, mtf_version, context_len, horizon_len,
            covariate_config, covariate_signature, covariate_analysis,
            used_quantile, buy_threshold_pct, sell_threshold_pct, trade_fee_rate, total_fees_paid, actual_total_return_pct,
            benchmark_return_pct, benchmark_annualized_return_pct, period_days,
            validation_start_date, validation_end_date, validation_benchmark_return_pct, validation_benchmark_annualized_return_pct, validation_period_days,
            position_control, predicted_change_stats, per_chunk_signals,
            equity_curve_values, equity_curve_pct, equity_curve_pct_gross, curve_dates, actual_end_prices, trades
        ) VALUES (
            $1, $2, $3, $4, $5, $6, $7,
            $8::jsonb, $9, $10::jsonb,
            $11, $12, $13, $14, $15, $16,
            $17, $18, $19,
            $20::date, $21::date, $22, $23, $24,
            $25::jsonb, $26::jsonb, $27::jsonb,
            $28::jsonb, $29::jsonb, $30::jsonb, $31::jsonb, $32::jsonb, $33::jsonb
        )
        ON CONFLICT (unique_key) DO UPDATE SET
            user_id = EXCLUDED.user_id,
            strategy_params_id = EXCLUDED.strategy_params_id,
            symbol = EXCLUDED.symbol,
            mtf_version = EXCLUDED.mtf_version,
            context_len = EXCLUDED.context_len,
            horizon_len = EXCLUDED.horizon_len,
            covariate_config = EXCLUDED.covariate_config,
            covariate_signature = EXCLUDED.covariate_signature,
            covariate_analysis = EXCLUDED.covariate_analysis,
            used_quantile = EXCLUDED.used_quantile,
            buy_threshold_pct = EXCLUDED.buy_threshold_pct,
            sell_threshold_pct = EXCLUDED.sell_threshold_pct,
            trade_fee_rate = EXCLUDED.trade_fee_rate,
            total_fees_paid = EXCLUDED.total_fees_paid,
            actual_total_return_pct = EXCLUDED.actual_total_return_pct,
            benchmark_return_pct = EXCLUDED.benchmark_return_pct,
            benchmark_annualized_return_pct = EXCLUDED.benchmark_annualized_return_pct,
            period_days = EXCLUDED.period_days,
            validation_start_date = EXCLUDED.validation_start_date,
            validation_end_date = EXCLUDED.validation_end_date,
            validation_benchmark_return_pct = EXCLUDED.validation_benchmark_return_pct,
            validation_benchmark_annualized_return_pct = EXCLUDED.validation_benchmark_annualized_return_pct,
            validation_period_days = EXCLUDED.validation_period_days,
            position_control = EXCLUDED.position_control,
            predicted_change_stats = EXCLUDED.predicted_change_stats,
            per_chunk_signals = EXCLUDED.per_chunk_signals,
            equity_curve_values = EXCLUDED.equity_curve_values,
            equity_curve_pct = EXCLUDED.equity_curve_pct,
            equity_curve_pct_gross = EXCLUDED.equity_curve_pct_gross,
            curve_dates = EXCLUDED.curve_dates,
            actual_end_prices = EXCLUDED.actual_end_prices,
            trades = EXCLUDED.trades,
            updated_at = CURRENT_TIMESTAMP
        `,
		req.UniqueKey, uidArg, spIDArg, req.Symbol, req.MTFVersion, req.ContextLen, req.HorizonLen,
		covConfigJSON, covSignatureArg, covAnalysisJSON,
		req.UsedQuantile, req.BuyThresholdPct, req.SellThresholdPct, req.TradeFeeRate, req.TotalFeesPaid, req.ActualTotalReturnPct,
		req.BenchmarkReturnPct, req.BenchmarkAnnualizedReturnPct, req.PeriodDays,
		req.ValidationStartDate, req.ValidationEndDate, req.ValidationBenchmarkReturnPct, req.ValidationBenchmarkAnnualizedReturnPct, req.ValidationPeriodDays,
		string(posJSON), string(statsJSON), string(signalsJSON),
		string(eqValsJSON), string(eqPctJSON), string(eqPctGrossJSON), string(curveDatesJSON), string(actualEndJSON), string(tradesJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to upsert mtf_backtests: %v", err)
	}
	return nil
}

func (s *WatchlistService) SaveStrategyParams(req *models.SaveStrategyParamsRequest) error {
	var uidArg interface{}
	if req.UserID != nil {
		uidArg = *req.UserID
	} else {
		uidArg = nil
	}
	_, err := s.db.Conn.Exec(`
        INSERT INTO mtf_strategy_params (
            unique_key, user_id, name,
            buy_threshold_pct, sell_threshold_pct, initial_cash,
            enable_rebalance, max_position_pct, min_position_pct,
            slope_position_per_pct, rebalance_tolerance_pct,
            trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
        ) VALUES (
            $1, $2, $3,
            $4, $5, $6,
            $7, $8, $9,
            $10, $11,
            $12, $13, $14
        )
        ON CONFLICT (unique_key) DO UPDATE SET
            user_id = EXCLUDED.user_id,
            name = EXCLUDED.name,
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
            updated_at = CURRENT_TIMESTAMP
    `,
		req.UniqueKey, uidArg, req.Name,
		req.BuyThresholdPct, req.SellThresholdPct, req.InitialCash,
		req.EnableRebalance, req.MaxPositionPct, req.MinPositionPct,
		req.SlopePositionPerPct, req.RebalanceTolerancePct,
		req.TradeFeeRate, req.TakeProfitThresholdPct, req.TakeProfitSellFrac,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert mtf_strategy_params: %v", err)
	}
	return nil
}

func (s *WatchlistService) GetStrategyParamsByUniqueKey(uniqueKey string) (*models.StrategyParams, error) {
	row := s.db.Conn.QueryRow(`
        SELECT id, unique_key, user_id, name, is_public,
               buy_threshold_pct, sell_threshold_pct, initial_cash,
               enable_rebalance, max_position_pct, min_position_pct,
               slope_position_per_pct, rebalance_tolerance_pct,
               trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
        FROM mtf_strategy_params
        WHERE unique_key = $1
        LIMIT 1
    `, uniqueKey)
	var item models.StrategyParams
	var uid sql.NullInt64
	err := row.Scan(
		&item.ID, &item.UniqueKey, &uid, &item.Name, &item.IsPublic,
		&item.BuyThresholdPct, &item.SellThresholdPct, &item.InitialCash,
		&item.EnableRebalance, &item.MaxPositionPct, &item.MinPositionPct,
		&item.SlopePositionPerPct, &item.RebalanceTolerancePct,
		&item.TradeFeeRate, &item.TakeProfitThresholdPct, &item.TakeProfitSellFrac,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("failed to query strategy params by unique_key: %v", err)
	}
	if uid.Valid {
		v := int(uid.Int64)
		item.UserID = &v
	} else {
		item.UserID = nil
	}
	return &item, nil
}

// 获取用户的所有策略（包括系统预设策略）
func (s *WatchlistService) GetUserStrategies(userID int) ([]models.StrategyParams, error) {
	rows, err := s.db.Conn.Query(`
        SELECT id, unique_key, user_id, name, is_public,
               buy_threshold_pct, sell_threshold_pct, initial_cash,
               enable_rebalance, max_position_pct, min_position_pct,
               slope_position_per_pct, rebalance_tolerance_pct,
               trade_fee_rate, take_profit_threshold_pct, take_profit_sell_frac
        FROM mtf_strategy_params
        WHERE user_id = $1 OR is_public = 1
        ORDER BY is_public DESC, updated_at DESC
    `, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user strategies: %v", err)
	}
	defer rows.Close()

	var results []models.StrategyParams
	for rows.Next() {
		var item models.StrategyParams
		var uid sql.NullInt64
		err := rows.Scan(
			&item.ID, &item.UniqueKey, &uid, &item.Name, &item.IsPublic,
			&item.BuyThresholdPct, &item.SellThresholdPct, &item.InitialCash,
			&item.EnableRebalance, &item.MaxPositionPct, &item.MinPositionPct,
			&item.SlopePositionPerPct, &item.RebalanceTolerancePct,
			&item.TradeFeeRate, &item.TakeProfitThresholdPct, &item.TakeProfitSellFrac,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan strategy params: %v", err)
		}
		if uid.Valid {
			v := int(uid.Int64)
			item.UserID = &v
		} else {
			item.UserID = nil
		}
		results = append(results, item)
	}
	return results, nil
}
