package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSyncSymbolRangeFetchesLatestDateGapInOneRequest(t *testing.T) {
	var historyPayload struct {
		Symbol    string `json:"symbol"`
		StockType int    `json:"stock_type"`
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
		Adjust    string `json:"adjust"`
	}
	historyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/history" {
			t.Fatalf("unexpected history path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&historyPayload); err != nil {
			t.Fatalf("decode history payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(historyResponse{
			Code:      http.StatusOK,
			Symbol:    historyPayload.Symbol,
			StockType: historyPayload.StockType,
			Provider:  "test",
			Rows:      5,
			Data: []historyRecord{
				{Datetime: "2026-04-27", Open: 9, Close: 9, High: 9, Low: 9},
				{Datetime: "2026-04-28", Open: 10, Close: 10, High: 10, Low: 10},
				{Datetime: "2026-04-29T00:00:00Z", Open: 11, Close: 11, High: 11, Low: 11},
				{Datetime: "2026-04-30", Open: 12, Close: 12, High: 12, Low: 12},
				{Datetime: "2026-05-01", Open: 13, Close: 13, High: 13, Low: 13},
			},
		})
	}))
	defer historyServer.Close()

	var inserted []stockBatchRecord
	postgresServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stock-data/batch" {
			t.Fatalf("unexpected postgres path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Token"); got != "token" {
			t.Fatalf("expected X-Token header, got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&inserted); err != nil {
			t.Fatalf("decode inserted records: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": http.StatusOK})
	}))
	defer postgresServer.Close()

	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	syncer := NewDailyStockSyncer(postgresServer.URL, historyServer.URL, "token", location, 22, 0, 4, 0)

	rows, err := syncer.syncSymbolRange(
		context.Background(),
		stockSymbolMeta{Symbol: "sh510050", StockType: 2, LatestDate: "2026-04-27"},
		"20260430",
		"2026-04-30",
	)
	if err != nil {
		t.Fatalf("syncSymbolRange() error: %v", err)
	}
	if rows != 3 {
		t.Fatalf("expected 3 inserted rows, got %d", rows)
	}
	if historyPayload.StartDate != "20260428" || historyPayload.EndDate != "20260430" {
		t.Fatalf("expected range 20260428~20260430, got %s~%s", historyPayload.StartDate, historyPayload.EndDate)
	}
	if historyPayload.Symbol != "sh510050" || historyPayload.StockType != 2 || historyPayload.Adjust != "forward_additive" {
		t.Fatalf("unexpected history payload: %#v", historyPayload)
	}
	if len(inserted) != 3 {
		t.Fatalf("expected 3 inserted records, got %d", len(inserted))
	}
	if inserted[0].DateStr != "2026-04-28" || inserted[1].DateStr != "2026-04-29" || inserted[2].DateStr != "2026-04-30" {
		t.Fatalf("unexpected inserted dates: %#v", inserted)
	}
	if inserted[0].Symbol != "510050" || inserted[0].Type != 2 {
		t.Fatalf("expected normalized storage symbol and type, got %#v", inserted[0])
	}
}

func TestSyncSymbolRangeSkipsFutureLatestDate(t *testing.T) {
	syncer := NewDailyStockSyncer("http://postgres.invalid", "http://history.invalid", "token", time.UTC, 22, 0, 4, 0)

	rows, err := syncer.syncSymbolRange(
		context.Background(),
		stockSymbolMeta{Symbol: "600000", StockType: 1, LatestDate: "2026-05-01"},
		"20260430",
		"2026-04-30",
	)
	if err != nil {
		t.Fatalf("syncSymbolRange() error: %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected no rows for future latest date, got %d", rows)
	}
}

func TestSyncSymbolRangeUsesLookbackWindow(t *testing.T) {
	var historyPayload struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	historyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&historyPayload); err != nil {
			t.Fatalf("decode history payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(historyResponse{
			Code: http.StatusOK,
			Data: []historyRecord{
				{Datetime: "2026-04-25", Open: 1, Close: 1, High: 1, Low: 1},
				{Datetime: "2026-04-30", Open: 2, Close: 2, High: 2, Low: 2},
			},
		})
	}))
	defer historyServer.Close()

	postgresServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": http.StatusOK})
	}))
	defer postgresServer.Close()

	syncer := NewDailyStockSyncer(postgresServer.URL, historyServer.URL, "token", time.UTC, 22, 0, 4, 5)
	rows, err := syncer.syncSymbolRange(
		context.Background(),
		stockSymbolMeta{Symbol: "510050", StockType: 2, LatestDate: "2026-04-29"},
		"20260430",
		"2026-04-30",
	)
	if err != nil {
		t.Fatalf("syncSymbolRange() error: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows from lookback window, got %d", rows)
	}
	if historyPayload.StartDate != "20260425" || historyPayload.EndDate != "20260430" {
		t.Fatalf("expected lookback range 20260425~20260430, got %s~%s", historyPayload.StartDate, historyPayload.EndDate)
	}
}
