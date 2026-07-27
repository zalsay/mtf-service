//go:build ignore

package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

type watchlistSymbol struct {
	Symbol        string `json:"symbol"`
	StockType     int    `json:"stock_type"`
	WatchlistRows int    `json:"watchlist_rows"`
	SourceSymbols string `json:"source_symbols"`
}

type exportSummary struct {
	GeneratedAt        string `json:"generated_at"`
	TotalRows          int    `json:"total_watchlist_rows"`
	UniqueSymbols      int    `json:"unique_symbols"`
	UniqueStockSymbols int    `json:"unique_stock_symbols"`
	UniqueETFSymbols   int    `json:"unique_etf_symbols"`
	OutputDirectory    string `json:"output_directory"`
}

func main() {
	envFile := flag.String("env-file", ".env", "PostgreSQL environment file")
	outputDir := flag.String("output-dir", "./watchlist_symbols", "directory for exported files")
	flag.Parse()

	if err := godotenv.Load(*envFile); err != nil {
		fmt.Fprintf(os.Stderr, "warning: load env file %s: %v\n", *envFile, err)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		getenv("DB_HOST", "localhost"),
		getenv("DB_PORT", "5432"),
		getenv("DB_USER", "postgres"),
		getenv("DB_PASSWORD", ""),
		getenv("DB_NAME", "fintrack"),
		getenv("DB_SSLMODE", "disable"),
	)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fatal("connect PostgreSQL", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT normalized_symbol AS symbol,
		       stock_type,
		       COUNT(*)::int AS watchlist_rows,
		       string_agg(DISTINCT raw_symbol, ',' ORDER BY raw_symbol) AS source_symbols
		FROM (
			SELECT trim(symbol) AS raw_symbol,
			       CASE
					WHEN char_length(trim(symbol)) > 2
					 AND lower(left(trim(symbol), 2)) IN ('sh', 'sz', 'bj')
					THEN substring(trim(symbol) FROM 3)
					ELSE trim(symbol)
				END AS normalized_symbol,
			       stock_type
			FROM user_watchlist
			WHERE symbol IS NOT NULL AND trim(symbol) <> '' AND stock_type > 0
		) normalized_rows
		GROUP BY normalized_symbol, stock_type
		ORDER BY stock_type ASC, normalized_symbol ASC`)
	if err != nil {
		fatal("query user_watchlist", err)
	}
	defer rows.Close()

	items := make([]watchlistSymbol, 0)
	totalRows := 0
	stockSymbols := 0
	etfSymbols := 0
	for rows.Next() {
		var item watchlistSymbol
		if err := rows.Scan(&item.Symbol, &item.StockType, &item.WatchlistRows, &item.SourceSymbols); err != nil {
			fatal("scan user_watchlist", err)
		}
		items = append(items, item)
		totalRows += item.WatchlistRows
		switch item.StockType {
		case 1:
			stockSymbols++
		case 2:
			etfSymbols++
		}
	}
	if err := rows.Err(); err != nil {
		fatal("read user_watchlist rows", err)
	}

	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		fatal("create output directory", err)
	}

	if err := writeJSON(filepath.Join(*outputDir, "watchlist_symbols.json"), items); err != nil {
		fatal("write JSON export", err)
	}
	if err := writeCSV(filepath.Join(*outputDir, "watchlist_symbols.csv"), items); err != nil {
		fatal("write CSV export", err)
	}
	summary := exportSummary{
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		TotalRows:          totalRows,
		UniqueSymbols:      len(items),
		UniqueStockSymbols: stockSymbols,
		UniqueETFSymbols:   etfSymbols,
		OutputDirectory:    *outputDir,
	}
	if err := writeJSON(filepath.Join(*outputDir, "summary.json"), summary); err != nil {
		fatal("write summary", err)
	}

	fmt.Printf("watchlist rows: %d\n", summary.TotalRows)
	fmt.Printf("unique symbols: %d (stock=%d etf=%d)\n", summary.UniqueSymbols, summary.UniqueStockSymbols, summary.UniqueETFSymbols)
	fmt.Printf("output directory: %s\n", *outputDir)
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func writeCSV(path string, items []watchlistSymbol) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if err := writer.Write([]string{"symbol", "stock_type", "watchlist_rows", "source_symbols"}); err != nil {
		return err
	}
	for _, item := range items {
		if err := writer.Write([]string{
			item.Symbol,
			strconv.Itoa(item.StockType),
			strconv.Itoa(item.WatchlistRows),
			item.SourceSymbols,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func fatal(action string, err error) {
	fmt.Fprintf(os.Stderr, "%s failed: %v\n", action, err)
	os.Exit(1)
}
