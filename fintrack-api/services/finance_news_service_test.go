package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestFinanceNewsServiceListsHotETFDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hot-etf" {
			t.Fatalf("path = %s, want /hot-etf", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`
			<table id="radarTable">
				<thead>
					<tr><th>标的/雷达优先级</th><th>趋势</th><th>月线</th><th>周线</th><th>日线</th><th>参考止损</th><th>总分</th><th>状态</th></tr>
				</thead>
				<tbody>
					<tr>
						<td><div><span>华夏中证机器人ETF</span><span>562500 · 风险RPS 83</span><span>雷达优先级 77.7 · A</span></div></td>
						<td><svg><title>2026-05-19~2026-06-03: +12.3→+18.8 (+2.4)</title></svg></td>
						<td><span>+4.0</span><span>月线多头结构</span></td>
						<td><span>+6.0</span><span>极强多</span></td>
						<td><span>+4.5</span><span>突破上轨</span></td>
						<td><div>参考止损 <strong>1.013</strong></div><div>2.5%</div></td>
						<td><span>18.8</span></td>
						<td><span>强势波段多头</span><span>主升</span></td>
					</tr>
				</tbody>
			</table>
		`))
	}))
	defer server.Close()

	service := newTestFinanceNewsService(server)
	result, err := service.ListHotETF(context.Background())
	if err != nil {
		t.Fatalf("ListHotETF error = %v", err)
	}
	if result.Source != "meetlife_hot_etf" || result.Count != 1 {
		t.Fatalf("unexpected hot ETF result: %#v", result)
	}
	item := result.Items[0]
	if item.Code != "562500" || item.Name != "华夏中证机器人ETF" {
		t.Fatalf("unexpected code/name: %#v", item)
	}
	if item.RadarPriority != 77.7 || item.RiskRPS != 83 || item.Grade != "A" {
		t.Fatalf("unexpected radar fields: %#v", item)
	}
	if item.TotalScore != 18.8 || item.StopPrice != "1.013" || item.StopDistance != "2.5%" {
		t.Fatalf("unexpected score/stop fields: %#v", item)
	}
	if item.Month.Score != 4 || item.Week.Score != 6 || item.Day.Score != 4.5 {
		t.Fatalf("unexpected signal scores: %#v", item)
	}
	if item.Status != "强势波段多头 主升" {
		t.Fatalf("unexpected status: %q", item.Status)
	}
	if !strings.Contains(item.Trend, "+12.3") {
		t.Fatalf("unexpected trend: %q", item.Trend)
	}
}

func TestFinanceNewsServiceUsesLocalHotETFHTMLBeforeCacheExpires(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "hot-etf", "latest.html")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(hotETFTestHTML("华夏本地ETF", "510300")), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	localTime := time.Now().Add(-23 * time.Hour)
	if err := os.Chtimes(cachePath, localTime, localTime); err != nil {
		t.Fatalf("set cache time: %v", err)
	}

	service := newTestFinanceNewsService(server)
	service.hotETFCachePath = cachePath
	result, err := service.ListHotETF(context.Background())
	if err != nil {
		t.Fatalf("ListHotETF error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("remote requests = %d, want 0", requests)
	}
	if result.Count != 1 || result.Items[0].Code != "510300" || result.Items[0].Name != "华夏本地ETF" {
		t.Fatalf("unexpected cached hot ETF result: %#v", result)
	}
}

func TestFinanceNewsServiceUsesExpiredLocalHotETFHTMLWhenRemoteIsNotNewer(t *testing.T) {
	requests := 0
	localTime := time.Now().Add(-25 * time.Hour).Truncate(time.Second)
	remoteTime := localTime.Add(-time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Last-Modified", remoteTime.Format(http.TimeFormat))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(hotETFTestHTML("华夏远端ETF", "159919")))
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "hot-etf", "latest.html")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(hotETFTestHTML("华夏本地ETF", "510300")), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	if err := os.Chtimes(cachePath, localTime, localTime); err != nil {
		t.Fatalf("set cache time: %v", err)
	}

	service := newTestFinanceNewsService(server)
	service.hotETFCachePath = cachePath
	result, err := service.ListHotETF(context.Background())
	if err != nil {
		t.Fatalf("ListHotETF error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("remote requests = %d, want 1", requests)
	}
	if result.Count != 1 || result.Items[0].Code != "510300" || result.Items[0].Name != "华夏本地ETF" {
		t.Fatalf("unexpected cached hot ETF result: %#v", result)
	}
}

func TestFinanceNewsServiceRefreshesHotETFHTMLWhenRemoteIsNewer(t *testing.T) {
	requests := 0
	remoteTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Last-Modified", remoteTime.Format(http.TimeFormat))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(hotETFTestHTML("华夏远端ETF", "159919")))
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "hot-etf", "latest.html")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(hotETFTestHTML("华夏本地ETF", "510300")), 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	localTime := time.Now().Add(-25 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(cachePath, localTime, localTime); err != nil {
		t.Fatalf("set cache time: %v", err)
	}

	service := newTestFinanceNewsService(server)
	service.hotETFCachePath = cachePath
	result, err := service.ListHotETF(context.Background())
	if err != nil {
		t.Fatalf("ListHotETF error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("remote requests = %d, want 1", requests)
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read refreshed cache: %v", err)
	}
	if !strings.Contains(string(raw), "华夏远端ETF") {
		t.Fatalf("cache body missing remote html: %s", string(raw))
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("stat refreshed cache: %v", err)
	}
	if !info.ModTime().Equal(remoteTime) {
		t.Fatalf("cache mtime = %v, want %v", info.ModTime(), remoteTime)
	}
	if result.Count != 1 || result.Items[0].Code != "159919" || result.Items[0].Name != "华夏远端ETF" {
		t.Fatalf("unexpected remote hot ETF result: %#v", result)
	}
}

func TestFinanceNewsServiceWritesHotETFHTMLCacheAfterRemoteFetch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(hotETFTestHTML("华夏远端ETF", "159919")))
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "hot-etf", "latest.html")
	service := newTestFinanceNewsService(server)
	service.hotETFCachePath = cachePath
	result, err := service.ListHotETF(context.Background())
	if err != nil {
		t.Fatalf("ListHotETF error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("remote requests = %d, want 1", requests)
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read written cache: %v", err)
	}
	if !strings.Contains(string(raw), "华夏远端ETF") {
		t.Fatalf("cache body missing remote html: %s", string(raw))
	}
	if result.Count != 1 || result.Items[0].Code != "159919" {
		t.Fatalf("unexpected remote hot ETF result: %#v", result)
	}
}

func hotETFTestHTML(name string, code string) string {
	return `
		<table id="radarTable">
			<tbody>
				<tr>
					<td><span>` + name + `</span><span>` + code + ` · 风险RPS 83</span><span>雷达优先级 77.7 · A</span></td>
					<td><svg><title>2026-05-19~2026-06-03: +12.3→+18.8 (+2.4)</title></svg></td>
					<td><span>+4.0</span><span>月线多头结构</span></td>
					<td><span>+6.0</span><span>极强多</span></td>
					<td><span>+4.5</span><span>突破上轨</span></td>
					<td><div>参考止损 <strong>1.013</strong></div><div>2.5%</div></td>
					<td><span>18.8</span></td>
					<td><span>强势波段多头</span></td>
				</tr>
			</tbody>
		</table>`
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
