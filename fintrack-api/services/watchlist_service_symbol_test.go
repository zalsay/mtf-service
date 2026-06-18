package services

import (
	"testing"
	"time"
)

func TestNormalizeMTFSymbolReadKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "sz prefixed stock", input: "sz000001", want: "000001"},
		{name: "sh prefixed etf", input: "SH510050", want: "510050"},
		{name: "plain digits", input: "000001", want: "000001"},
		{name: "trim whitespace", input: "  sz159919  ", want: "159919"},
		{name: "non digit symbol falls back to lowercase", input: "AAPL", want: "aapl"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeMTFSymbolReadKey(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeMTFSymbolReadKey(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestUniqueNonEmptyStringsPreservesFirstSeenOrder(t *testing.T) {
	got := uniqueNonEmptyStrings([]string{" key-a ", "", "key-b", "key-a", "  ", "key-c"})
	want := []string{"key-a", "key-b", "key-c"}
	if len(got) != len(want) {
		t.Fatalf("uniqueNonEmptyStrings length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uniqueNonEmptyStrings[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func TestNormalizeMarketQuoteCode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain digits", input: "601766", want: "601766"},
		{name: "sh prefix", input: "sh601766", want: "601766"},
		{name: "uppercase sz prefix", input: "SZ000001", want: "000001"},
		{name: "dot suffix", input: "601766.SH", want: "601766"},
		{name: "etf prefix", input: "SH510050", want: "510050"},
		{name: "non digit fallback", input: " BABA ", want: "baba"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeMarketQuoteCode(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeMarketQuoteCode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSymbolNameLookupCandidates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "plain sh etf code", input: "510050", want: []string{"510050", "sh510050", "sz510050"}},
		{name: "prefixed sh etf code", input: "SH510050", want: []string{"sh510050", "510050", "sz510050"}},
		{name: "prefixed sz etf code", input: "sz159919", want: []string{"sz159919", "159919", "sh159919"}},
		{name: "non digit symbol", input: " BABA ", want: []string{"baba"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := symbolNameLookupCandidates(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("symbolNameLookupCandidates(%q) length = %d, want %d: %#v", tc.input, len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("symbolNameLookupCandidates(%q)[%d] = %q, want %q; all=%#v", tc.input, i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

func TestInferLookupStockTypes(t *testing.T) {
	tests := []struct {
		name   string
		symbol string
		want   []int
	}{
		{name: "sh etf code prefers etf", symbol: "510050", want: []int{2, 1}},
		{name: "sz etf code prefers etf", symbol: "159919", want: []int{2, 1}},
		{name: "stock code prefers stock", symbol: "601766", want: []int{1, 2}},
		{name: "plain stock code prefers stock", symbol: "000001", want: []int{1, 2}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := inferLookupStockTypes(tc.symbol)
			if len(got) != len(tc.want) {
				t.Fatalf("inferLookupStockTypes(%q) length = %d, want %d", tc.symbol, len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("inferLookupStockTypes(%q)[%d] = %d, want %d", tc.symbol, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestCanonicalWatchlistSymbol(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		stockType int
		want      string
	}{
		{name: "sh stock from plain code", input: "600246", stockType: 1, want: "sh600246"},
		{name: "sh stock from uppercase prefixed code", input: "SH600246", stockType: 1, want: "sh600246"},
		{name: "sz stock from plain code", input: "000001", stockType: 1, want: "sz000001"},
		{name: "sh etf from plain code", input: "510300", stockType: 2, want: "sh510300"},
		{name: "sz etf from plain code", input: "159919", stockType: 2, want: "sz159919"},
		{name: "non digit fallback", input: " BABA ", stockType: 1, want: "baba"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := canonicalWatchlistSymbol(tc.input, tc.stockType)
			if got != tc.want {
				t.Fatalf("canonicalWatchlistSymbol(%q, %d) = %q, want %q", tc.input, tc.stockType, got, tc.want)
			}
		})
	}
}

func TestLatestPreviousTradingQuoteFromKlinesSkipsCurrentDate(t *testing.T) {
	loc := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 4, 30, 9, 0, 0, 0, loc)
	quote, err := latestPreviousTradingQuoteFromKlines("sh601766", []string{
		"2026-04-28,6.30,6.31,6.32,6.25,1,1,1,0.16,0.01,0.40",
		"2026-04-29,6.31,6.34,6.35,6.29,1,1,1,0.48,0.03,0.44",
		"2026-04-30,6.34,6.40,6.41,6.33,1,1,1,0.95,0.06,0.50",
	}, now)
	if err != nil {
		t.Fatalf("expected previous trading quote, got error: %v", err)
	}
	if quote.TradingDate == nil || *quote.TradingDate != "2026-04-29" {
		t.Fatalf("expected 2026-04-29, got %#v", quote.TradingDate)
	}
	if quote.LatestPrice == nil || *quote.LatestPrice != 6.34 {
		t.Fatalf("unexpected latest price: %#v", quote.LatestPrice)
	}
	if quote.ChangePercent == nil || *quote.ChangePercent != 0.48 {
		t.Fatalf("unexpected change percent: %#v", quote.ChangePercent)
	}
	if quote.TurnoverRate == nil || *quote.TurnoverRate != 0.0044 {
		t.Fatalf("unexpected turnover rate: %#v", quote.TurnoverRate)
	}
}

func TestEastmoneySecIDInfersMarket(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		symbol string
		want   string
	}{
		{name: "sh prefix", code: "601766", symbol: "sh601766", want: "1.601766"},
		{name: "sz prefix", code: "000001", symbol: "sz000001", want: "0.000001"},
		{name: "sh etf digits", code: "510050", symbol: "510050", want: "1.510050"},
		{name: "sz etf digits", code: "159919", symbol: "159919", want: "0.159919"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := eastmoneySecID(tc.code, tc.symbol)
			if got != tc.want {
				t.Fatalf("eastmoneySecID(%q, %q) = %q, want %q", tc.code, tc.symbol, got, tc.want)
			}
		})
	}
}

func TestDefaultMarketEndDateUsesYesterdayInChinaTime(t *testing.T) {
	original := chinaNowFunc
	chinaNowFunc = func() time.Time {
		return time.Date(2026, 6, 11, 10, 0, 0, 0, chinaLocation())
	}
	defer func() {
		chinaNowFunc = original
	}()

	if got := defaultMarketEndDate(); got != "2026-06-10" {
		t.Fatalf("defaultMarketEndDate() = %q, want 2026-06-10", got)
	}
}
