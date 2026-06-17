package main

import (
	"testing"
	"time"
)

func TestNormalizeEastmoneyKlines(t *testing.T) {
	rows := []string{
		"2026-05-06,1365.10,1375.00,1379.00,1360.05,47806,6550750940.00,1.37,-0.71,-9.79,0.38",
	}

	records, err := normalizeEastmoneyKlines(rows, "贵州茅台", "600519.SH")
	if err != nil {
		t.Fatalf("normalizeEastmoneyKlines returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	record := records[0]
	if record.TradeDate != "2026-05-06" {
		t.Fatalf("TradeDate = %q, want 2026-05-06", record.TradeDate)
	}
	if record.Open != 1365.10 || record.Close != 1375.00 || record.High != 1379.00 || record.Low != 1360.05 {
		t.Fatalf("unexpected OHLC: %#v", record)
	}
	if record.Volume != 47806 {
		t.Fatalf("Volume = %d, want 47806", record.Volume)
	}
	if record.PercentageChange != -0.71 || record.AmountChange != -9.79 || record.TurnoverRate != 0.38 {
		t.Fatalf("unexpected change fields: %#v", record)
	}
	if record.Name != "贵州茅台" || record.Symbol != "600519.SH" {
		t.Fatalf("unexpected identity fields: %#v", record)
	}
}

func TestNormalizeBaiduKlinesFiltersDateRange(t *testing.T) {
	marketData := "1777939200,2026-05-06,1365.10,1375.00,4780600,1379.00,1360.05,6550750940.00,-9.79,-0.71,0.38,1384.79;" +
		"1778025600,2026-05-07,1375.00,1371.05,4046100,1388.00,1370.01,5573286315.00,-3.95,-0.29,0.32,1375.00"
	startDate := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)

	records, err := normalizeBaiduKlines(marketData, "600519.SH", startDate, endDate)
	if err != nil {
		t.Fatalf("normalizeBaiduKlines returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	record := records[0]
	if record.TradeDate != "2026-05-07" {
		t.Fatalf("TradeDate = %q, want 2026-05-07", record.TradeDate)
	}
	if record.Open != 1375.00 || record.Close != 1371.05 || record.High != 1388.00 || record.Low != 1370.01 {
		t.Fatalf("unexpected OHLC: %#v", record)
	}
	if record.Volume != 4046100 {
		t.Fatalf("Volume = %d, want 4046100", record.Volume)
	}
	if record.PercentageChange != -0.29 || record.AmountChange != -3.95 || record.TurnoverRate != 0.32 {
		t.Fatalf("unexpected change fields: %#v", record)
	}
}

func TestHistoryProviderOrder(t *testing.T) {
	t.Setenv("HISTORY_PROVIDER_ORDER", "baidu, tickflow, daily, eastmoney, baidu")

	got := historyProviderOrder()
	want := []string{baiduProviderName, tickflowProviderName, dailyProviderName, eastmoneyProviderName}
	if len(got) != len(want) {
		t.Fatalf("historyProviderOrder len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("historyProviderOrder[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEastmoneySecIDAndFQT(t *testing.T) {
	cases := map[string]string{
		"600519.SH": "1.600519",
		"sh600519":  "1.600519",
		"000001.SZ": "0.000001",
		"sz000001":  "0.000001",
	}
	for input, expected := range cases {
		got, err := eastmoneySecID(input, 1)
		if err != nil {
			t.Fatalf("eastmoneySecID(%q) returned error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("eastmoneySecID(%q) = %q, want %q", input, got, expected)
		}
	}
	if eastmoneyFQT("qfq") != "1" || eastmoneyFQT("hfq") != "2" || eastmoneyFQT("none") != "0" {
		t.Fatalf("unexpected eastmoneyFQT mapping")
	}
}

func TestDailyAdjustMapping(t *testing.T) {
	if dailyAdjust("qfq") != "qfq" {
		t.Fatalf("dailyAdjust(qfq) = %q, want qfq", dailyAdjust("qfq"))
	}
	if dailyAdjust("forward_additive") != "qfq" {
		t.Fatalf("dailyAdjust(forward_additive) = %q, want qfq", dailyAdjust("forward_additive"))
	}
	if dailyAdjust("hfq") != "hfq" {
		t.Fatalf("dailyAdjust(hfq) = %q, want hfq", dailyAdjust("hfq"))
	}
	if dailyAdjust("none") != "none" {
		t.Fatalf("dailyAdjust(none) = %q, want none", dailyAdjust("none"))
	}
}

func TestDefaultHistoryProviderOrderDoesNotRequireEnv(t *testing.T) {
	t.Setenv("HISTORY_PROVIDER_ORDER", "")

	got := historyProviderOrder()
	want := []string{tickflowProviderName, dailyProviderName, eastmoneyProviderName, baiduProviderName}
	if len(got) != len(want) {
		t.Fatalf("historyProviderOrder len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("historyProviderOrder[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeTickflowInstrumentPromotesListingDate(t *testing.T) {
	got := normalizeTickflowInstrument(TickflowInstrument{
		Symbol:   " 515880.SH ",
		Exchange: " SH ",
		Name:     " 通信ETF国泰 ",
		Region:   " CN ",
		Type:     " etf ",
		Ext: map[string]any{
			"listing_date": "2019-09-06",
		},
	})

	if got.Symbol != "515880.SH" {
		t.Fatalf("Symbol = %q, want 515880.SH", got.Symbol)
	}
	if got.Code != "515880" {
		t.Fatalf("Code = %q, want 515880", got.Code)
	}
	if got.Exchange != "SH" || got.Region != "CN" || got.Type != "etf" {
		t.Fatalf("unexpected metadata: %#v", got)
	}
	if got.Name != "通信ETF国泰" {
		t.Fatalf("Name = %q, want 通信ETF国泰", got.Name)
	}
	if got.ListingDate != "2019-09-06" {
		t.Fatalf("ListingDate = %q, want 2019-09-06", got.ListingDate)
	}
}
