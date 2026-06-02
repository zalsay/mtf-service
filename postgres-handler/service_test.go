package main

import "testing"

func TestNormalizeStockSymbol(t *testing.T) {
	cases := map[string]string{
		"sh000001": "000001",
		"SZ159915": "159915",
		"bj430047": "430047",
		"000300":   "000300",
		"":         "",
	}

	for input, expected := range cases {
		if got := normalizeStockSymbol(input); got != expected {
			t.Fatalf("normalizeStockSymbol(%q) = %q, want %q", input, got, expected)
		}
	}
}
