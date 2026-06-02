package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	aStockTencentQuoteURL      = "https://qt.gtimg.cn/"
	aStockEastmoneyFFlowURL    = "https://push2his.eastmoney.com/api/qt/stock/fflow/kline/get"
	aStockEastmoneyDataURL     = "https://datacenter-web.eastmoney.com/api/data/v1/get"
	aStockEastmoneyIndustryURL = "https://push2.eastmoney.com/api/qt/clist/get"
)

type aStockDataProvider struct {
	client               *http.Client
	tencentQuoteURL      string
	eastmoneyFFlowURL    string
	eastmoneyDataURL     string
	eastmoneyIndustryURL string
}

type aStockSkillQuery struct {
	Intent   string   `json:"intent"`
	Symbol   string   `json:"symbol,omitempty"`
	Symbols  []string `json:"symbols,omitempty"`
	Topic    string   `json:"topic,omitempty"`
	Period   string   `json:"period,omitempty"`
	Question string   `json:"question,omitempty"`
	Limit    int      `json:"limit"`
}

func (s *MTFAgentService) executeAStockDataSkill(ctx context.Context, args map[string]interface{}) map[string]interface{} {
	query := buildAStockSkillQuery(args)
	provider := s.ensureAStockDataProvider()

	data, source, err := provider.fetch(ctx, query)
	result := map[string]interface{}{
		"skill":  mtfAgentAStockSkillName,
		"intent": query.Intent,
		"query":  query,
	}
	if err != nil {
		result["status"] = aStockSkillStatus(err)
		result["message"] = err.Error()
		result["required_fields"] = requiredAStockFields(query.Intent)
		result["assistant_guidance"] = "当前工具没有返回可用数据时，请直接向用户说明数据源限制，并给出可执行的查询口径、关键字段和分析框架；不要重复调用同一个工具。"
		return result
	}
	result["status"] = "ok"
	result["source"] = source
	result["data"] = data
	result["required_fields"] = requiredAStockFields(query.Intent)
	result["missing_fields"] = missingAStockFields(query.Intent, data)
	return result
}

func buildAStockSkillQuery(args map[string]interface{}) aStockSkillQuery {
	return aStockSkillQuery{
		Intent:   skillStringArg(args, "intent"),
		Symbol:   skillStringArg(args, "symbol"),
		Symbols:  skillStringSliceArg(args, "symbols"),
		Topic:    skillStringArg(args, "topic"),
		Period:   skillStringArg(args, "period"),
		Question: skillStringArg(args, "question"),
		Limit:    skillIntArg(args, "limit", 10, 1, 50),
	}
}

func (s *MTFAgentService) ensureAStockDataProvider() *aStockDataProvider {
	if s != nil && s.aStockDataProvider != nil {
		return s.aStockDataProvider
	}
	client := http.DefaultClient
	if s != nil && s.client != nil {
		client = s.client
	}
	return &aStockDataProvider{
		client:               client,
		tencentQuoteURL:      aStockTencentQuoteURL,
		eastmoneyFFlowURL:    aStockEastmoneyFFlowURL,
		eastmoneyDataURL:     aStockEastmoneyDataURL,
		eastmoneyIndustryURL: aStockEastmoneyIndustryURL,
	}
}

func (p *aStockDataProvider) fetch(ctx context.Context, query aStockSkillQuery) (map[string]interface{}, string, error) {
	switch strings.TrimSpace(query.Intent) {
	case "valuation":
		data, err := p.quote(ctx, query.Symbol)
		return data, "tencent_quote", err
	case "money_flow":
		data, err := p.moneyFlow(ctx, query.Symbol, query.Limit)
		return data, "eastmoney_push2", err
	case "lhb":
		data, err := p.dragonTigerBoard(ctx, query.Symbol, query.Period, 30, query.Limit)
		return data, "eastmoney_datacenter", err
	case "market_lhb":
		data, err := p.marketDragonTigerBoard(ctx, query.Period, query.Limit)
		return data, "eastmoney_datacenter", err
	case "sector_rotation":
		data, err := p.sectorRotation(ctx, query.Limit)
		return data, "eastmoney_push2", err
	case "news_announcements":
		data, source, err := p.newsAnnouncements(ctx, query)
		return data, source, err
	default:
		return nil, "", fmt.Errorf("unsupported_intent: %s 暂未接入无落库实时 provider", query.Intent)
	}
}

func (p *aStockDataProvider) quote(ctx context.Context, symbol string) (map[string]interface{}, error) {
	code, ok := normalizeAStockCode(symbol)
	if !ok {
		return nil, errors.New("missing_symbol: 请提供 6 位 A 股代码")
	}
	body, err := p.getText(ctx, p.tencentQuoteURL, url.Values{"q": []string{tencentQuoteCode(code)}})
	if err != nil {
		return nil, err
	}
	fields, err := parseTencentQuoteFields(body)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"latest_price":           tencentFloat(fields, 3),
		"symbol":                 code,
		"name":                   tencentField(fields, 1),
		"price":                  tencentFloat(fields, 3),
		"previous_close":         tencentFloat(fields, 4),
		"open":                   tencentFloat(fields, 5),
		"change":                 tencentFloat(fields, 31),
		"change_percent":         tencentFloat(fields, 32),
		"high":                   tencentFloat(fields, 33),
		"low":                    tencentFloat(fields, 34),
		"volume_lot":             tencentFloat(fields, 37),
		"turnover_yi":            tencentFloat(fields, 38),
		"pe_ttm":                 tencentFloat(fields, 39),
		"amplitude_percent":      tencentFloat(fields, 43),
		"circulating_market_cap": tencentFloat(fields, 44),
		"market_cap":             tencentFloat(fields, 45),
		"pb":                     tencentFloat(fields, 46),
		"limit_up":               tencentFloat(fields, 47),
		"limit_down":             tencentFloat(fields, 48),
		"turnover_rate_percent":  tencentFloat(fields, 49),
		"dynamic_pe":             tencentFloat(fields, 52),
	}, nil
}

func (p *aStockDataProvider) moneyFlow(ctx context.Context, symbol string, limit int) (map[string]interface{}, error) {
	code, ok := normalizeAStockCode(symbol)
	if !ok {
		return nil, errors.New("missing_symbol: 请提供 6 位 A 股代码")
	}
	params := url.Values{
		"secid":   []string{aStockEastmoneySecID(code)},
		"fields1": []string{"f1,f2,f3"},
		"fields2": []string{"f51,f52,f53,f54,f55,f56,f57"},
		"klt":     []string{"101"},
		"lmt":     []string{fmt.Sprintf("%d", aStockMaxInt(limit, 1))},
	}
	var payload eastmoneyKLineResponse
	if err := p.getJSON(ctx, p.eastmoneyFFlowURL, params, &payload); err != nil {
		return nil, err
	}
	return normalizeMoneyFlow(code, payload.Data.Name, payload.Data.KLines), nil
}

func (p *aStockDataProvider) dragonTigerBoard(ctx context.Context, symbol string, period string, days int, limit int) (map[string]interface{}, error) {
	code, ok := normalizeAStockCode(symbol)
	if !ok {
		return nil, errors.New("missing_symbol: 请提供 6 位 A 股代码")
	}
	records, err := p.eastmoneyRows(ctx, "RPT_DAILYBILLBOARD_DETAILSNEW", lhbFilter(code, period), "TRADE_DATE", "-1", limit)
	if err != nil {
		return nil, err
	}
	buy, _ := p.eastmoneyRows(ctx, "RPT_BILLBOARD_DAILYDETAILSBUY", lhbFilter(code, period), "BUY", "-1", limit)
	sell, _ := p.eastmoneyRows(ctx, "RPT_BILLBOARD_DAILYDETAILSSELL", lhbFilter(code, period), "SELL", "-1", limit)
	return map[string]interface{}{
		"symbol":      code,
		"records":     normalizeLHBRecords(records),
		"buy_seats":   normalizeLHBSeats(buy),
		"sell_seats":  normalizeLHBSeats(sell),
		"institution": summarizeInstitutionSeats(append(buy, sell...)),
	}, nil
}

func (p *aStockDataProvider) marketDragonTigerBoard(ctx context.Context, period string, limit int) (map[string]interface{}, error) {
	rows, err := p.eastmoneyRows(ctx, "RPT_DAILYBILLBOARD_DETAILSNEW", periodLHBFilter(period), "BILLBOARD_NET_AMT", "-1", limit)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"items": normalizeLHBRecords(rows)}, nil
}

func (p *aStockDataProvider) sectorRotation(ctx context.Context, limit int) (map[string]interface{}, error) {
	params := url.Values{
		"pn":     []string{"1"},
		"pz":     []string{fmt.Sprintf("%d", aStockMaxInt(limit, 1))},
		"po":     []string{"1"},
		"np":     []string{"1"},
		"fltt":   []string{"2"},
		"invt":   []string{"2"},
		"fs":     []string{"m:90+t:2"},
		"fields": []string{"f12,f14,f3,f62,f184"},
	}
	var payload eastmoneyClistResponse
	if err := p.getJSON(ctx, p.eastmoneyIndustryURL, params, &payload); err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(payload.Data.Diff))
	for _, row := range payload.Data.Diff {
		items = append(items, map[string]interface{}{
			"board_code":            row.F12,
			"board_name":            row.F14,
			"change_percent":        row.F3,
			"main_net_inflow":       row.F62,
			"main_net_inflow_ratio": row.F184,
		})
	}
	return map[string]interface{}{"items": items}, nil
}

func (p *aStockDataProvider) newsAnnouncements(ctx context.Context, query aStockSkillQuery) (map[string]interface{}, string, error) {
	category := "market"
	if query.Symbol != "" || query.Topic != "" {
		category = "stock"
	}
	service := NewFinanceNewsService(p.httpClient())
	result, err := service.List(ctx, FinanceNewsQuery{
		Category: category,
		Symbol:   query.Symbol,
		Keyword:  firstNonEmptyString(query.Topic, query.Question),
		Limit:    query.Limit,
		Page:     1,
	})
	if err != nil {
		return nil, "", err
	}
	return map[string]interface{}{
		"items": result.Items,
		"count": result.Count,
	}, result.Source, nil
}

func (p *aStockDataProvider) eastmoneyRows(ctx context.Context, report string, filter string, sortColumn string, sortType string, limit int) ([]map[string]interface{}, error) {
	params := url.Values{
		"reportName":  []string{report},
		"columns":     []string{"ALL"},
		"pageNumber":  []string{"1"},
		"pageSize":    []string{fmt.Sprintf("%d", aStockMaxInt(limit, 1))},
		"sortColumns": []string{sortColumn},
		"sortTypes":   []string{sortType},
		"source":      []string{"WEB"},
		"client":      []string{"WEB"},
	}
	if strings.TrimSpace(filter) != "" {
		params.Set("filter", filter)
	}
	var payload eastmoneyDataCenterResponse
	if err := p.getJSON(ctx, p.eastmoneyDataURL, params, &payload); err != nil {
		return nil, err
	}
	return payload.Result.Data, nil
}

func (p *aStockDataProvider) getText(ctx context.Context, rawURL string, params url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appendQuery(rawURL, params), nil)
	if err != nil {
		return "", err
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("provider_error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider_error: upstream returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return "", fmt.Errorf("provider_error: read response: %w", err)
	}
	return string(body), nil
}

func (p *aStockDataProvider) getJSON(ctx context.Context, rawURL string, params url.Values, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appendQuery(rawURL, params), nil)
	if err != nil {
		return err
	}
	resp, err := p.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("provider_error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("provider_error: upstream returned %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("provider_error: decode response: %w", err)
	}
	return nil
}

func (p *aStockDataProvider) httpClient() *http.Client {
	if p != nil && p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}
