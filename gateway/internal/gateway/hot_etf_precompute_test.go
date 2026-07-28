package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-functions/internal/backend"
	"ai-functions/internal/queue"
	"ai-functions/internal/store"
)

func TestHotETFPrecomputerParsesRadarTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
			<html><body>
			<table id="radar"><tbody>
			<tr><th>标的</th></tr>
			<tr><td>华夏沪深300ETF 510300</td><td>上升</td></tr>
			<tr><td>南方中证500ETF 510500</td><td>上升</td></tr>
			<tr><td>股票 600000</td><td>忽略</td></tr>
			<tr><td>华夏沪深300ETF 510300</td><td>重复</td></tr>
			</tbody></table>
			</body></html>`))
	}))
	defer server.Close()

	precomputer := NewHotETFPrecomputer(nil, HotETFPrecomputeOptions{SourceURL: server.URL})
	items, err := precomputer.fetchHotETF(context.Background())
	if err != nil {
		t.Fatalf("fetchHotETF() error = %v", err)
	}
	if len(items) != 2 || items[0].Code != "510300" || items[0].StockType != 2 || items[1].Code != "510500" || items[1].StockType != 2 {
		t.Fatalf("unexpected parsed ETF items: %#v", items)
	}
}

func TestHotETFPrecomputerNextRunAfter(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	precomputer := NewHotETFPrecomputer(nil, HotETFPrecomputeOptions{
		Location:       location,
		ScheduleHour:   0,
		ScheduleMinute: 5,
	})
	before := time.Date(2026, 7, 28, 0, 4, 59, 0, location)
	if got := precomputer.nextRunAfter(before); !got.Equal(time.Date(2026, 7, 28, 0, 5, 0, 0, location)) {
		t.Fatalf("next run before schedule = %s", got)
	}
	after := time.Date(2026, 7, 28, 0, 5, 0, 0, location)
	if got := precomputer.nextRunAfter(after); !got.Equal(time.Date(2026, 7, 29, 0, 5, 0, 0, location)) {
		t.Fatalf("next run after schedule = %s", got)
	}
}

func TestHotETFPrecomputerSkipsExistingBestAndFuture(t *testing.T) {
	postgres := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") != "token" {
			t.Fatalf("X-Token = %q, want token", r.Header.Get("X-Token"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/save-predictions/mtf-best/by-config":
			if got := r.URL.Query().Get("context_len"); got != "" {
				t.Errorf("best context_len = %q, want omitted", got)
			}
			wantStockType := "2"
			if r.URL.Query().Get("symbol") == "000001" {
				wantStockType = "3"
			}
			if got := r.URL.Query().Get("stock_type"); got != wantStockType {
				t.Errorf("best stock_type = %q, want %s", got, wantStockType)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{
					"mtf_pro_unique_key":  "510300_best_hlen_8_clen_512_mtf-pro",
					"mtf_pro_context_len": 512,
				},
			})
		case "/api/v1/save-predictions/mtf-direct/by-request":
			if got := r.URL.Query().Get("context_len"); got != "512" {
				t.Errorf("future context_len = %q, want 512", got)
			}
			wantStockType := "2"
			if r.URL.Query().Get("symbol") == "000001" {
				wantStockType = "3"
			}
			if got := r.URL.Query().Get("stock_type"); got != wantStockType {
				t.Errorf("future stock_type = %q, want %s", got, wantStockType)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{"future_dates": []string{"2026-07-29", "2026-07-30"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer postgres.Close()

	jobStore := store.NewMemoryStore()
	scheduler := queue.NewScheduler(backend.NewClient(time.Second), jobStore, nil)
	precomputer := NewHotETFPrecomputer(scheduler, HotETFPrecomputeOptions{
		PostgresBaseURL: postgres.URL,
		PostgresToken:   "token",
		HorizonLen:      8,
		ContextLen:      2048,
	})
	if err := precomputer.precomputeSymbol(context.Background(), "510300", 2, "2026-07-29"); err != nil {
		t.Fatalf("precomputeSymbol() error = %v", err)
	}
	if err := precomputer.precomputeSymbol(context.Background(), "000001", 3, "2026-07-29"); err != nil {
		t.Fatalf("precomputeSymbol() index error = %v", err)
	}
	snapshot, err := scheduler.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("scheduler snapshot error = %v", err)
	}
	if snapshot.QueueDepth != 0 {
		t.Fatalf("queue depth = %d, want 0", snapshot.QueueDepth)
	}
}
