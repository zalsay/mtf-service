package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinanceNewsServiceListsColumnNews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/columns" {
			t.Fatalf("path = %s, want /columns", r.URL.Path)
		}
		if got := r.URL.Query().Get("column"); got != "350" {
			t.Fatalf("column = %s, want 350", got)
		}
		writeJSON(t, w, map[string]interface{}{
			"code":    "1",
			"message": "success",
			"data": map[string]interface{}{
				"list": []map[string]interface{}{{
					"code":      "202605230001",
					"title":     "央行公开市场净投放",
					"summary":   "公开市场操作保持流动性合理充裕。",
					"showTime":  "2026-05-23 09:00:00",
					"mediaName": "东方财富网",
					"url":       "https://finance.eastmoney.com/a/202605230001.html",
				}},
			},
		})
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	result, err := service.List(context.Background(), FinanceNewsQuery{Category: "market", Limit: 10, Page: 1})
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if result.Source != "eastmoney_columns" || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Items[0].Title != "央行公开市场净投放" || result.Items[0].PublishedAt != "2026-05-23 09:00:00" {
		t.Fatalf("unexpected item: %#v", result.Items[0])
	}
}

func TestFinanceNewsServiceSearchesStockNews(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %s, want /search", r.URL.Path)
		}
		if !strings.Contains(r.URL.Query().Get("param"), "688017") {
			t.Fatalf("param should contain symbol: %s", r.URL.Query().Get("param"))
		}
		_, _ = w.Write([]byte(`jQuery1124({"code":0,"result":{"cmsArticleWebOld":[{"code":"1","title":"绿的谐波发布一季报","content":"净利润同比增长。","date":"2026-04-23 09:34:06","mediaName":"界面新闻","url":"https://finance.eastmoney.com/a/1.html"}]}})`))
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	result, err := service.List(context.Background(), FinanceNewsQuery{Category: "stock", Symbol: "688017", Limit: 5, Page: 1})
	if err != nil {
		t.Fatalf("List stock error = %v", err)
	}
	if result.Source != "eastmoney_search" || result.Items[0].Summary != "净利润同比增长。" {
		t.Fatalf("unexpected stock news result: %#v", result)
	}
}

func TestFinanceNewsServiceListsAnnouncements(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ann" {
			t.Fatalf("path = %s, want /ann", r.URL.Path)
		}
		if got := r.URL.Query().Get("stock_list"); got != "688017.SH" {
			t.Fatalf("stock_list = %s, want 688017.SH", got)
		}
		_, _ = w.Write([]byte(`jQuery1123({"data":{"list":[{"art_code":"AN1","title":"绿的谐波:年度报告","display_time":"2026-04-23 10:00:00","notice_date":"2026-04-23 00:00:00","columns":[{"column_name":"年度报告"}],"codes":[{"short_name":"绿的谐波","stock_code":"688017"}]}],"total_hits":1},"success":1})`))
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	result, err := service.List(context.Background(), FinanceNewsQuery{Category: "announcements", Symbol: "688017", Limit: 5, Page: 1})
	if err != nil {
		t.Fatalf("List announcements error = %v", err)
	}
	if result.Source != "eastmoney_announcements" || result.Count != 1 {
		t.Fatalf("unexpected announcement result: %#v", result)
	}
	if result.Items[0].Category != "年度报告" || result.Items[0].Symbol != "688017" {
		t.Fatalf("unexpected announcement item: %#v", result.Items[0])
	}
}

func TestFinanceNewsServiceListsDragonTigerBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lhb" {
			t.Fatalf("path = %s, want /lhb", r.URL.Path)
		}
		query := r.URL.Query()
		if got := query.Get("reportName"); got != "RPT_DAILYBILLBOARD_DETAILSNEW" {
			t.Fatalf("reportName = %s, want RPT_DAILYBILLBOARD_DETAILSNEW", got)
		}
		if got := query.Get("pageNumber"); got != "2" {
			t.Fatalf("pageNumber = %s, want 2", got)
		}
		if got := query.Get("pageSize"); got != "5" {
			t.Fatalf("pageSize = %s, want 5", got)
		}
		if got := query.Get("sortColumns"); got != "TRADE_DATE,BILLBOARD_NET_AMT" {
			t.Fatalf("sortColumns = %s, want TRADE_DATE,BILLBOARD_NET_AMT", got)
		}
		writeJSON(t, w, map[string]interface{}{
			"result": map[string]interface{}{
				"data": []map[string]interface{}{{
					"TRADE_DATE":         "2026-05-22 00:00:00",
					"SECURITY_CODE":      "688017",
					"SECURITY_NAME_ABBR": "绿的谐波",
					"EXPLANATION":        "有价格涨跌幅限制的日收盘价格涨幅达到15%的前五只证券",
					"CLOSE_PRICE":        128.34,
					"CHANGE_RATE":        18.52,
					"TURNOVERRATE":       12.35,
					"BILLBOARD_NET_AMT":  12345678.0,
					"BILLBOARD_BUY_AMT":  23456789.0,
					"BILLBOARD_SELL_AMT": 11111111.0,
				}},
			},
		})
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	result, err := service.List(context.Background(), FinanceNewsQuery{Category: "lhb", Keyword: "绿的", Limit: 5, Page: 2})
	if err != nil {
		t.Fatalf("List lhb error = %v", err)
	}
	if result.Source != "eastmoney_lhb" || result.Count != 1 {
		t.Fatalf("unexpected lhb result: %#v", result)
	}
	item := result.Items[0]
	if item.Title != "688017 绿的谐波 龙虎榜" || item.Category != "lhb" {
		t.Fatalf("unexpected lhb item title/category: %#v", item)
	}
	if item.Source != "东方财富龙虎榜" || item.Symbol != "688017" || item.StockName != "绿的谐波" {
		t.Fatalf("unexpected lhb item source/symbol: %#v", item)
	}
	if !strings.Contains(item.Summary, "净买入 1234.57 万元") {
		t.Fatalf("summary should contain normalized net buy amount: %s", item.Summary)
	}
}

func TestFinanceNewsServiceListsHotETFRecommendations(t *testing.T) {
	page := `
		<html><body>
			<h2>标的评分明细</h2>
			<div>沪深300ETF 510300 91.50 88.00 92.00 85.00 3.870 89.25 观察</div>
			<div>人工智能ETF 159819 96.20 94.00 97.00 91.00 0.982 95.40 强势</div>
		</body></html>
	`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hot-etf" {
			t.Fatalf("path = %s, want /hot-etf", r.URL.Path)
		}
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	result, err := service.List(context.Background(), FinanceNewsQuery{Category: "hot_etf", Keyword: "人工", Limit: 10, Page: 1})
	if err != nil {
		t.Fatalf("List hot_etf error = %v", err)
	}
	if result.Source != "imlam_etf" || result.Category != "hot_etf" || result.Count != 1 {
		t.Fatalf("unexpected hot ETF result: %#v", result)
	}
	item := result.Items[0]
	if item.Symbol != "159819" || item.StockName != "人工智能ETF" || item.Category != "hot_etf" {
		t.Fatalf("unexpected hot ETF item identity: %#v", item)
	}
	if item.ETFStatus != "强势" || item.ETFRPS != "96.20" || item.ETFStopLoss != "0.982" || item.ETFScore != "95.40" {
		t.Fatalf("unexpected hot ETF scores: %#v", item)
	}
	if !strings.Contains(item.Summary, "RPS 96.20") || !strings.Contains(item.Summary, "加权总分 95.40") {
		t.Fatalf("summary should contain normalized score details: %s", item.Summary)
	}
}

func TestFinanceNewsServiceCachesHotETFRecommendationsForCurrentDay(t *testing.T) {
	hits := 0
	page := `<html><body><div>人工智能ETF 159819 96.20 94.00 97.00 91.00 0.982 95.40 强势</div></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits > 1 {
			t.Fatalf("hot ETF snapshot should be fetched once per day, got %d hits", hits)
		}
		_, _ = w.Write([]byte(page))
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	service.hotETFURL = server.URL
	service.hotETFCacheDir = t.TempDir()

	for i := 0; i < 2; i++ {
		result, err := service.List(context.Background(), FinanceNewsQuery{Category: "hot_etf", Limit: 10, Page: 1})
		if err != nil {
			t.Fatalf("List hot_etf #%d error = %v", i+1, err)
		}
		if result.Count != 1 || result.Items[0].Symbol != "159819" {
			t.Fatalf("unexpected cached hot ETF result #%d: %#v", i+1, result)
		}
	}

	files, err := filepath.Glob(filepath.Join(service.hotETFCacheDir, "hot_etf_*.json"))
	if err != nil {
		t.Fatalf("glob cache files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("cache files count = %d, want 1", len(files))
	}
	if info, err := os.Stat(files[0]); err != nil || info.Size() == 0 {
		t.Fatalf("cache file should exist with content, info=%#v err=%v", info, err)
	}
}

func newTestFinanceNewsService(server *httptest.Server) *FinanceNewsService {
	return &FinanceNewsService{
		client:          server.Client(),
		columnNewsURL:   server.URL + "/columns",
		stockSearchURL:  server.URL + "/search",
		announcementURL: server.URL + "/ann",
		lhbURL:          server.URL + "/lhb",
		hotETFURL:       server.URL + "/hot-etf",
	}
}
