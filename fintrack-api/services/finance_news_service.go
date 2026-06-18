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
	"strings"
	"time"
)

const (
	financeNewsColumnURL       = "https://np-listapi.eastmoney.com/comm/web/getNewsByColumns"
	financeNewsStockSearchURL  = "https://search-api-web.eastmoney.com/search/jsonp"
	financeNewsAnnouncementURL = "https://np-anotice-stock.eastmoney.com/api/security/ann"
	financeNewsLHBURL          = "https://datacenter-web.eastmoney.com/api/data/v1/get"
	financeNewsHotETFURL       = "https://ai.meetlife.top/hot-etf/latest"
	financeNewsHotETFCachePath = "data/hot-etf/latest.html"
	hotETFCacheMaxAge          = 24 * time.Hour
)

type FinanceNewsService struct {
	client          *http.Client
	columnNewsURL   string
	stockSearchURL  string
	announcementURL string
	lhbURL          string
	hotETFURL       string
	hotETFCachePath string
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
}

type HotETFResponse struct {
	Status string       `json:"status"`
	Source string       `json:"source"`
	Count  int          `json:"count"`
	Items  []HotETFItem `json:"items"`
}

type HotETFItem struct {
	Code          string       `json:"code"`
	Name          string       `json:"name"`
	RiskRPS       float64      `json:"risk_rps"`
	RadarPriority float64      `json:"radar_priority"`
	Grade         string       `json:"grade,omitempty"`
	Trend         string       `json:"trend,omitempty"`
	Month         HotETFSignal `json:"month"`
	Week          HotETFSignal `json:"week"`
	Day           HotETFSignal `json:"day"`
	StopPrice     string       `json:"stop_price,omitempty"`
	StopDistance  string       `json:"stop_distance,omitempty"`
	TotalScore    float64      `json:"total_score"`
	Status        string       `json:"status"`
}

type HotETFSignal struct {
	Score float64 `json:"score"`
	Text  string  `json:"text"`
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
		hotETFURL:       financeNewsHotETFURL,
		hotETFCachePath: financeNewsHotETFCachePath,
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
	case "market", "global":
		return s.listColumnNews(ctx, query)
	default:
		return FinanceNewsResponse{}, fmt.Errorf("unsupported finance news category: %s", query.Category)
	}
}

func (s *FinanceNewsService) ListHotETF(ctx context.Context) (HotETFResponse, error) {
	body, err := s.loadHotETFHTML(ctx)
	if err != nil {
		return HotETFResponse{}, err
	}
	items, err := parseHotETFHTML(body)
	if err != nil {
		return HotETFResponse{}, err
	}
	return HotETFResponse{
		Status: "ok",
		Source: "meetlife_hot_etf",
		Count:  len(items),
		Items:  items,
	}, nil
}

func (s *FinanceNewsService) loadHotETFHTML(ctx context.Context) (string, error) {
	cachePath := strings.TrimSpace(s.hotETFCachePath)
	if cachePath == "" {
		cachePath = financeNewsHotETFCachePath
	}
	var cachedBody string
	var cacheModTime time.Time
	if raw, err := os.ReadFile(cachePath); err == nil && len(raw) > 0 {
		cachedBody = string(raw)
		if info, statErr := os.Stat(cachePath); statErr == nil {
			cacheModTime = info.ModTime()
		}
	}
	if cachedBody != "" && !cacheModTime.IsZero() && time.Since(cacheModTime) < hotETFCacheMaxAge {
		return cachedBody, nil
	}
	body, remoteModTime, err := s.getHotETFHTML(ctx)
	if err != nil {
		if cachedBody != "" {
			return cachedBody, nil
		}
		return "", err
	}
	if cachedBody != "" && !remoteModTime.IsZero() && !remoteModTime.After(cacheModTime) {
		return cachedBody, nil
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", fmt.Errorf("cache hot ETF html: create dir: %w", err)
	}
	if err := os.WriteFile(cachePath, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("cache hot ETF html: write file: %w", err)
	}
	if !remoteModTime.IsZero() {
		if err := os.Chtimes(cachePath, remoteModTime, remoteModTime); err != nil {
			return "", fmt.Errorf("cache hot ETF html: update time: %w", err)
		}
	}
	return body, nil
}

func (s *FinanceNewsService) getHotETFHTML(ctx context.Context) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.hotETFURL, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("provider_error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("provider_error: upstream returned %d", resp.StatusCode)
	}
	modTime, _ := http.ParseTime(resp.Header.Get("Last-Modified"))
	body, err := readHTTPText(resp.Body)
	return body, modTime, err
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
