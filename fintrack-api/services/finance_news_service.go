package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	financeNewsColumnURL       = "https://np-listapi.eastmoney.com/comm/web/getNewsByColumns"
	financeNewsStockSearchURL  = "https://search-api-web.eastmoney.com/search/jsonp"
	financeNewsAnnouncementURL = "https://np-anotice-stock.eastmoney.com/api/security/ann"
	financeNewsLHBURL          = "https://datacenter-web.eastmoney.com/api/data/v1/get"
	financeNewsHotETFURL       = "https://etf.imlam.com/"
)

var hotETFRowPattern = regexp.MustCompile(`(?m)([^\s\d][^\n]*?)\s+(\d{6})\s+([+-]?\d+(?:\.\d+)?)\s+([+-]?\d+(?:\.\d+)?)\s+([+-]?\d+(?:\.\d+)?)\s+([+-]?\d+(?:\.\d+)?)\s+([+-]?\d+(?:\.\d+)?)\s+([+-]?\d+(?:\.\d+)?)\s+([^\s]+)`)

type FinanceNewsService struct {
	client          *http.Client
	columnNewsURL   string
	stockSearchURL  string
	announcementURL string
	lhbURL          string
	hotETFURL       string
	hotETFCacheDir  string
}

type FinanceNewsQuery struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol,omitempty"`
	Keyword  string `json:"keyword,omitempty"`
	Limit    int    `json:"limit"`
	Page     int    `json:"page"`
}

type FinanceNewsResponse struct {
	Status   string            `json:"status"`
	Source   string            `json:"source"`
	Category string            `json:"category"`
	Query    FinanceNewsQuery  `json:"query"`
	Count    int               `json:"count"`
	Items    []FinanceNewsItem `json:"items"`
}

type FinanceNewsItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Source      string `json:"source,omitempty"`
	URL         string `json:"url,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	StockName   string `json:"stock_name,omitempty"`
	Category    string `json:"category,omitempty"`
	ETFRPS      string `json:"etf_rps,omitempty"`
	ETFMonth    string `json:"etf_month,omitempty"`
	ETFWeek     string `json:"etf_week,omitempty"`
	ETFDay      string `json:"etf_day,omitempty"`
	ETFStopLoss string `json:"etf_stop_loss,omitempty"`
	ETFScore    string `json:"etf_score,omitempty"`
	ETFStatus   string `json:"etf_status,omitempty"`
}

func NewFinanceNewsService(client *http.Client) *FinanceNewsService {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &FinanceNewsService{
		client:          client,
		columnNewsURL:   financeNewsColumnURL,
		stockSearchURL:  financeNewsStockSearchURL,
		announcementURL: financeNewsAnnouncementURL,
		lhbURL:          financeNewsLHBURL,
		hotETFURL:       hotETFConfiguredURL(),
		hotETFCacheDir:  hotETFConfiguredCacheDir(),
	}
}

func (s *FinanceNewsService) List(ctx context.Context, query FinanceNewsQuery) (FinanceNewsResponse, error) {
	query = normalizeFinanceNewsQuery(query)
	switch query.Category {
	case "stock":
		return s.listStockNews(ctx, query)
	case "announcements":
		return s.listAnnouncements(ctx, query)
	case "lhb":
		return s.listDragonTigerBoard(ctx, query)
	case "hot_etf":
		return s.listHotETF(ctx, query)
	case "market", "global":
		return s.listColumnNews(ctx, query)
	default:
		return FinanceNewsResponse{}, fmt.Errorf("unsupported finance news category: %s", query.Category)
	}
}

func (s *FinanceNewsService) listHotETF(ctx context.Context, query FinanceNewsQuery) (FinanceNewsResponse, error) {
	items, err := s.hotETFItems(ctx)
	if err != nil {
		return FinanceNewsResponse{}, err
	}
	filtered := make([]FinanceNewsItem, 0, len(items))
	for _, item := range items {
		if query.Keyword != "" && !financeNewsContains(item.Title+item.Summary+item.Symbol+item.StockName+item.ETFStatus, query.Keyword) {
			continue
		}
		filtered = append(filtered, item)
	}
	return financeNewsResponse(query, "imlam_etf", paginateFinanceNewsItems(filtered, query.Page, query.Limit)), nil
}

func (s *FinanceNewsService) hotETFItems(ctx context.Context) ([]FinanceNewsItem, error) {
	cachePath := s.hotETFCachePath(time.Now())
	if items, ok := readHotETFCache(cachePath); ok {
		return items, nil
	}
	body, err := s.getText(ctx, s.hotETFURL, nil)
	if err != nil {
		return nil, err
	}
	items := hotETFItemsFromHTML(body)
	_ = writeHotETFCache(cachePath, items)
	return items, nil
}

func (s *FinanceNewsService) hotETFCachePath(now time.Time) string {
	if s.hotETFCacheDir == "" {
		return ""
	}
	return filepath.Join(s.hotETFCacheDir, fmt.Sprintf("hot_etf_%s.json", now.Format("2006-01-02")))
}

func hotETFItemsFromHTML(body string) []FinanceNewsItem {
	text := normalizeHotETFText(body)
	matches := hotETFRowPattern.FindAllStringSubmatch(text, -1)
	items := make([]FinanceNewsItem, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		name := strings.TrimSpace(match[1])
		code := strings.TrimSpace(match[2])
		if code == "" || name == "" || seen[code] {
			continue
		}
		seen[code] = true
		item := FinanceNewsItem{
			ID:          "hot-etf-" + code,
			Title:       strings.TrimSpace(fmt.Sprintf("%s %s", code, name)),
			Summary:     fmt.Sprintf("RPS %s，月评分 %s，周评分 %s，日评分 %s，防守止损 %s，加权总分 %s，状态 %s。", match[3], match[4], match[5], match[6], match[7], match[8], match[9]),
			Source:      "ETF imlam",
			URL:         financeNewsHotETFURL,
			Symbol:      code,
			StockName:   name,
			Category:    "hot_etf",
			ETFRPS:      match[3],
			ETFMonth:    match[4],
			ETFWeek:     match[5],
			ETFDay:      match[6],
			ETFStopLoss: match[7],
			ETFScore:    match[8],
			ETFStatus:   match[9],
		}
		items = append(items, item)
	}
	return items
}

func readHotETFCache(path string) ([]FinanceNewsItem, bool) {
	if path == "" {
		return nil, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var payload struct {
		Items []FinanceNewsItem `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload.Items, true
}

func writeHotETFCache(path string, items []FinanceNewsItem) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	payload := struct {
		FetchedAt string            `json:"fetched_at"`
		Source    string            `json:"source"`
		Items     []FinanceNewsItem `json:"items"`
	}{
		FetchedAt: time.Now().Format(time.RFC3339),
		Source:    "imlam_etf_snapshot",
		Items:     items,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func hotETFConfiguredURL() string {
	if value := strings.TrimSpace(os.Getenv("HOT_ETF_SNAPSHOT_URL")); value != "" {
		return value
	}
	return financeNewsHotETFURL
}

func hotETFConfiguredCacheDir() string {
	if value := strings.TrimSpace(os.Getenv("HOT_ETF_CACHE_DIR")); value != "" {
		return value
	}
	return filepath.Join(os.TempDir(), "fintrack-hot-etf-cache")
}

func paginateFinanceNewsItems(items []FinanceNewsItem, page int, limit int) []FinanceNewsItem {
	start := (page - 1) * limit
	if start >= len(items) {
		return []FinanceNewsItem{}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func normalizeFinanceNewsQuery(query FinanceNewsQuery) FinanceNewsQuery {
	query.Category = strings.TrimSpace(strings.ToLower(query.Category))
	if query.Category == "" {
		query.Category = "market"
	}
	query.Symbol = strings.TrimSpace(query.Symbol)
	query.Keyword = strings.TrimSpace(query.Keyword)
	if query.Limit <= 0 || query.Limit > 50 {
		query.Limit = 20
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	return query
}

func (s *FinanceNewsService) listColumnNews(ctx context.Context, query FinanceNewsQuery) (FinanceNewsResponse, error) {
	params := url.Values{
		"client":     []string{"web"},
		"biz":        []string{"web_news_col"},
		"column":     []string{financeNewsColumn(query.Category)},
		"page_index": []string{fmt.Sprintf("%d", query.Page)},
		"page_size":  []string{fmt.Sprintf("%d", query.Limit)},
		"req_trace":  []string{fmt.Sprintf("%d", time.Now().UnixNano())},
	}
	var payload financeNewsColumnPayload
	if err := s.getJSON(ctx, s.columnNewsURL, params, &payload); err != nil {
		return FinanceNewsResponse{}, err
	}
	items := make([]FinanceNewsItem, 0, len(payload.Data.List))
	for _, row := range payload.Data.List {
		if query.Keyword != "" && !financeNewsContains(row.Title+row.Summary, query.Keyword) {
			continue
		}
		items = append(items, FinanceNewsItem{
			ID:          row.Code,
			Title:       row.Title,
			Summary:     row.Summary,
			PublishedAt: row.ShowTime,
			Source:      row.MediaName,
			URL:         firstNonEmptyString(row.UniqueURL, row.URL),
			Category:    query.Category,
		})
	}
	return financeNewsResponse(query, "eastmoney_columns", items), nil
}

func (s *FinanceNewsService) listStockNews(ctx context.Context, query FinanceNewsQuery) (FinanceNewsResponse, error) {
	keyword := firstNonEmptyString(query.Symbol, query.Keyword)
	if strings.TrimSpace(keyword) == "" {
		return FinanceNewsResponse{}, errors.New("symbol or keyword is required for stock news")
	}
	params := url.Values{
		"cb":    []string{"jQuery1124"},
		"param": []string{buildEastmoneySearchParam(keyword, query.Page, query.Limit)},
	}
	body, err := s.getText(ctx, s.stockSearchURL, params)
	if err != nil {
		return FinanceNewsResponse{}, err
	}
	var payload financeNewsSearchPayload
	if err := decodeJSONOrJSONP(body, &payload); err != nil {
		return FinanceNewsResponse{}, err
	}
	rows := payload.Result.CMSArticleWebOld
	items := make([]FinanceNewsItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, FinanceNewsItem{
			ID:          row.Code,
			Title:       row.Title,
			Summary:     row.Content,
			PublishedAt: row.Date,
			Source:      row.MediaName,
			URL:         row.URL,
			Symbol:      query.Symbol,
			Category:    "stock",
		})
	}
	return financeNewsResponse(query, "eastmoney_search", items), nil
}

func (s *FinanceNewsService) listAnnouncements(ctx context.Context, query FinanceNewsQuery) (FinanceNewsResponse, error) {
	params := url.Values{
		"sr":            []string{"-1"},
		"page_size":     []string{fmt.Sprintf("%d", query.Limit)},
		"page_index":    []string{fmt.Sprintf("%d", query.Page)},
		"ann_type":      []string{"A"},
		"client_source": []string{"web"},
		"f_node":        []string{"0"},
		"s_node":        []string{"0"},
	}
	if code, ok := normalizeAStockCode(query.Symbol); ok {
		params.Set("stock_list", announcementStockListCode(code))
	}
	body, err := s.getText(ctx, s.announcementURL, params)
	if err != nil {
		return FinanceNewsResponse{}, err
	}
	var payload financeNewsAnnouncementPayload
	if err := decodeJSONOrJSONP(body, &payload); err != nil {
		return FinanceNewsResponse{}, err
	}
	items := make([]FinanceNewsItem, 0, len(payload.Data.List))
	for _, row := range payload.Data.List {
		items = append(items, FinanceNewsItem{
			ID:          row.ArtCode,
			Title:       firstNonEmptyString(row.Title, row.TitleCH),
			PublishedAt: firstNonEmptyString(row.DisplayTime, row.NoticeDate),
			URL:         fmt.Sprintf("https://data.eastmoney.com/notices/detail/%s.html", row.ArtCode),
			Symbol:      firstAnnouncementCode(row.Codes),
			StockName:   firstAnnouncementName(row.Codes),
			Category:    firstAnnouncementColumn(row.Columns),
		})
	}
	return financeNewsResponse(query, "eastmoney_announcements", items), nil
}

func (s *FinanceNewsService) listDragonTigerBoard(ctx context.Context, query FinanceNewsQuery) (FinanceNewsResponse, error) {
	params := url.Values{
		"reportName":  []string{"RPT_DAILYBILLBOARD_DETAILSNEW"},
		"columns":     []string{"ALL"},
		"pageNumber":  []string{fmt.Sprintf("%d", query.Page)},
		"pageSize":    []string{fmt.Sprintf("%d", query.Limit)},
		"sortColumns": []string{"TRADE_DATE,BILLBOARD_NET_AMT"},
		"sortTypes":   []string{"-1,-1"},
		"source":      []string{"WEB"},
		"client":      []string{"WEB"},
	}
	var payload eastmoneyDataCenterResponse
	if err := s.getJSON(ctx, s.lhbURL, params, &payload); err != nil {
		return FinanceNewsResponse{}, err
	}
	items := make([]FinanceNewsItem, 0, len(payload.Result.Data))
	for _, row := range payload.Result.Data {
		item := financeNewsLHBItem(row)
		if query.Keyword != "" && !financeNewsContains(item.Title+item.Summary+item.Symbol+item.StockName, query.Keyword) {
			continue
		}
		items = append(items, item)
	}
	return financeNewsResponse(query, "eastmoney_lhb", items), nil
}

func (s *FinanceNewsService) getJSON(ctx context.Context, rawURL string, params url.Values, target interface{}) error {
	body, err := s.getText(ctx, rawURL, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), target); err != nil {
		return fmt.Errorf("provider_error: decode finance news: %w", err)
	}
	return nil
}

func (s *FinanceNewsService) getText(ctx context.Context, rawURL string, params url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appendQuery(rawURL, params), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; FinTrack/1.0; +https://etf.imlam.com/)")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("provider_error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider_error: upstream returned %d", resp.StatusCode)
	}
	return readHTTPText(resp.Body)
}

func (s *FinanceNewsService) httpClient() *http.Client {
	if s != nil && s.client != nil {
		return s.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}
