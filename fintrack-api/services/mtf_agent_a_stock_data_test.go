package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fintrack-api/config"
)

func TestAStockDataProviderFetchesTencentQuote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/quote" {
			t.Fatalf("path = %s, want /quote", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "sh688017" {
			t.Fatalf("q = %s, want sh688017", got)
		}
		_, _ = w.Write([]byte(buildTencentQuoteLine("sh688017", []string{
			1:  "Lvdi Harmonic",
			3:  "224.12",
			4:  "215.01",
			5:  "214.10",
			31: "9.11",
			32: "4.24",
			33: "229.62",
			34: "214.10",
			37: "187040",
			38: "4.55",
			39: "300.45",
			43: "7.22",
			44: "410.88",
			45: "410.88",
			46: "11.51",
			47: "258.01",
			48: "172.01",
			49: "1.20",
			52: "314.76",
		})))
	}))
	defer server.Close()

	provider := newTestAStockDataProvider(server)
	result, err := provider.quote(context.Background(), "688017")
	if err != nil {
		t.Fatalf("quote error = %v", err)
	}
	if result["symbol"] != "688017" || result["name"] != "Lvdi Harmonic" {
		t.Fatalf("unexpected quote identity: %#v", result)
	}
	if result["price"] != 224.12 || result["pe_ttm"] != 300.45 || result["pb"] != 11.51 {
		t.Fatalf("unexpected valuation fields: %#v", result)
	}
}

func TestAStockDataProviderFetchesMoneyFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fflow/kline/get" {
			t.Fatalf("path = %s, want /fflow/kline/get", r.URL.Path)
		}
		if got := r.URL.Query().Get("secid"); got != "0.000858" {
			t.Fatalf("secid = %s, want 0.000858", got)
		}
		writeJSON(t, w, map[string]interface{}{
			"data": map[string]interface{}{
				"klines": []string{
					"2026-05-22 09:31,100,10,20,30,40,50",
					"2026-05-22 09:32,200,11,21,31,41,51",
				},
			},
		})
	}))
	defer server.Close()

	provider := newTestAStockDataProvider(server)
	result, err := provider.moneyFlow(context.Background(), "000858", 10)
	if err != nil {
		t.Fatalf("moneyFlow error = %v", err)
	}
	items := result["items"].([]map[string]interface{})
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if result["latest_main_net"] != 200.0 || result["total_main_net"] != 300.0 {
		t.Fatalf("unexpected money flow summary: %#v", result)
	}
}

func TestAStockDataProviderFetchesDragonTigerBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		report := r.URL.Query().Get("reportName")
		switch report {
		case "RPT_DAILYBILLBOARD_DETAILSNEW":
			if !strings.Contains(r.URL.Query().Get("filter"), `SECURITY_CODE="002475"`) {
				t.Fatalf("details filter = %s", r.URL.Query().Get("filter"))
			}
			writeEastmoneyRows(t, w, []map[string]interface{}{{
				"TRADE_DATE":        "2026-05-22 00:00:00",
				"EXPLANATION":       "日涨幅偏离值达7%",
				"BILLBOARD_NET_AMT": 12340000.0,
				"TURNOVERRATE":      12.34,
			}})
		case "RPT_BILLBOARD_DAILYDETAILSBUY":
			writeEastmoneyRows(t, w, []map[string]interface{}{{
				"OPERATEDEPT_NAME": "机构专用",
				"OPERATEDEPT_CODE": "0",
				"BUY":              20000000.0,
				"SELL":             1000000.0,
				"NET":              19000000.0,
			}})
		case "RPT_BILLBOARD_DAILYDETAILSSELL":
			writeEastmoneyRows(t, w, []map[string]interface{}{{
				"OPERATEDEPT_NAME": "某营业部",
				"OPERATEDEPT_CODE": "123",
				"BUY":              1000000.0,
				"SELL":             3000000.0,
				"NET":              -2000000.0,
			}})
		default:
			t.Fatalf("unexpected reportName = %s", report)
		}
	}))
	defer server.Close()

	provider := newTestAStockDataProvider(server)
	result, err := provider.dragonTigerBoard(context.Background(), "002475", "2026-05-22", 30, 10)
	if err != nil {
		t.Fatalf("dragonTigerBoard error = %v", err)
	}
	records := result["records"].([]map[string]interface{})
	if len(records) != 1 || records[0]["net_buy_wan"] != 1234.0 {
		t.Fatalf("unexpected records: %#v", records)
	}
	institution := result["institution"].(map[string]interface{})
	if institution["net_wan"] != 1900.0 {
		t.Fatalf("unexpected institution summary: %#v", institution)
	}
}

func TestExecuteAStockDataSkillRoutesToProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]interface{}{
			"data": map[string]interface{}{
				"klines": []string{"2026-05-22 09:31,100,10,20,30,40,50"},
			},
		})
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{})
	service.aStockDataProvider = newTestAStockDataProvider(server)

	result, err := service.ExecuteMTFAgentSkill(context.Background(), 7, "a_stock_data", map[string]interface{}{
		"intent": "money_flow",
		"symbol": "000858",
	})
	if err != nil {
		t.Fatalf("ExecuteMTFAgentSkill error = %v", err)
	}
	if result["status"] != "ok" || result["source"] != "eastmoney_push2" {
		t.Fatalf("unexpected skill result: %#v", result)
	}
	data := result["data"].(map[string]interface{})
	if data["latest_main_net"] != 100.0 {
		t.Fatalf("unexpected data: %#v", data)
	}
}

func newTestAStockDataProvider(server *httptest.Server) *aStockDataProvider {
	return &aStockDataProvider{
		client:               server.Client(),
		tencentQuoteURL:      server.URL + "/quote",
		eastmoneyFFlowURL:    server.URL + "/fflow/kline/get",
		eastmoneyDataURL:     server.URL + "/datacenter",
		eastmoneyIndustryURL: server.URL + "/industry",
	}
}

func buildTencentQuoteLine(code string, values []string) string {
	fields := make([]string, 89)
	for i, value := range values {
		fields[i] = value
	}
	return "v_" + code + "=\"" + strings.Join(fields, "~") + "\";"
}

func writeJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func writeEastmoneyRows(t *testing.T, w http.ResponseWriter, rows []map[string]interface{}) {
	t.Helper()
	writeJSON(t, w, map[string]interface{}{
		"result": map[string]interface{}{
			"data": rows,
		},
	})
}
