package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var aStockDigitRe = regexp.MustCompile(`\d{6}`)

type eastmoneyKLineResponse struct {
	Data struct {
		Name   string   `json:"name"`
		KLines []string `json:"klines"`
	} `json:"data"`
}

type eastmoneyDataCenterResponse struct {
	Result struct {
		Data []map[string]interface{} `json:"data"`
	} `json:"result"`
}

type eastmoneyClistResponse struct {
	Data struct {
		Diff []struct {
			F12  string  `json:"f12"`
			F14  string  `json:"f14"`
			F3   float64 `json:"f3"`
			F62  float64 `json:"f62"`
			F184 float64 `json:"f184"`
		} `json:"diff"`
	} `json:"data"`
}

func normalizeAStockCode(input string) (string, bool) {
	match := aStockDigitRe.FindString(strings.TrimSpace(input))
	if len(match) != 6 {
		return "", false
	}
	return match, true
}

func tencentQuoteCode(code string) string {
	return aStockMarketPrefix(code) + code
}

func aStockEastmoneySecID(code string) string {
	if strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
		return "1." + code
	}
	return "0." + code
}

func aStockMarketPrefix(code string) string {
	if strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
		return "sh"
	}
	if strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") {
		return "bj"
	}
	return "sz"
}

func parseTencentQuoteFields(body string) ([]string, error) {
	start := strings.Index(body, "\"")
	end := strings.LastIndex(body, "\"")
	if start < 0 || end <= start {
		return nil, errors.New("provider_error: invalid Tencent quote response")
	}
	fields := strings.Split(body[start+1:end], "~")
	if len(fields) < 53 || strings.TrimSpace(fields[3]) == "" {
		return nil, errors.New("not_found: 腾讯行情未返回有效报价")
	}
	return fields, nil
}

func normalizeMoneyFlow(code string, name string, klines []string) map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(klines))
	totalMain := 0.0
	latestMain := 0.0
	for _, line := range klines {
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		mainNet := parseFloat(parts[1])
		totalMain += mainNet
		latestMain = mainNet
		items = append(items, map[string]interface{}{
			"trade_date":            dateOnly(parts[0]),
			"trade_time":            parts[0],
			"main_net":              mainNet,
			"main_net_inflow":       mainNet,
			"small_order_net":       parseFloat(parts[2]),
			"medium_order_net":      parseFloat(parts[3]),
			"large_order_net":       parseFloat(parts[4]),
			"super_large_order_net": parseFloat(parts[5]),
		})
	}
	return map[string]interface{}{
		"symbol":          code,
		"name":            name,
		"trade_date":      latestTradeDate(items),
		"latest_main_net": latestMain,
		"main_net_inflow": latestMain,
		"total_main_net":  totalMain,
		"items":           items,
	}
}

func normalizeLHBRecords(rows []map[string]interface{}) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]interface{}{
			"trade_date":      dateOnly(mapString(row, "TRADE_DATE")),
			"symbol":          mapString(row, "SECURITY_CODE"),
			"name":            mapString(row, "SECURITY_NAME_ABBR"),
			"reason":          firstNonEmptyString(mapString(row, "EXPLANATION"), mapString(row, "EXPLAIN")),
			"close_price":     mapFloat(row, "CLOSE_PRICE"),
			"change_percent":  mapFloat(row, "CHANGE_RATE"),
			"turnover_rate":   mapFloat(row, "TURNOVERRATE"),
			"net_buy_amount":  mapFloat(row, "BILLBOARD_NET_AMT"),
			"buy_amount":      mapFloat(row, "BILLBOARD_BUY_AMT"),
			"sell_amount":     mapFloat(row, "BILLBOARD_SELL_AMT"),
			"net_buy_wan":     toWan(mapFloat(row, "BILLBOARD_NET_AMT")),
			"buy_amount_wan":  toWan(mapFloat(row, "BILLBOARD_BUY_AMT")),
			"sell_amount_wan": toWan(mapFloat(row, "BILLBOARD_SELL_AMT")),
		})
	}
	return items
}

func normalizeLHBSeats(rows []map[string]interface{}) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]interface{}{
			"trade_date":  dateOnly(mapString(row, "TRADE_DATE")),
			"broker_seat": mapString(row, "OPERATEDEPT_NAME"),
			"broker_code": mapString(row, "OPERATEDEPT_CODE"),
			"buy_amount":  mapFloat(row, "BUY"),
			"sell_amount": mapFloat(row, "SELL"),
			"net_amount":  mapFloat(row, "NET"),
			"buy_wan":     toWan(mapFloat(row, "BUY")),
			"sell_wan":    toWan(mapFloat(row, "SELL")),
			"net_wan":     toWan(mapFloat(row, "NET")),
			"reason":      mapString(row, "EXPLANATION"),
		})
	}
	return items
}

func summarizeInstitutionSeats(rows []map[string]interface{}) map[string]interface{} {
	count := 0
	net := 0.0
	for _, row := range rows {
		if strings.Contains(mapString(row, "OPERATEDEPT_NAME"), "机构") {
			count++
			net += mapFloat(row, "NET")
		}
	}
	return map[string]interface{}{"count": count, "net_wan": toWan(net)}
}

func lhbFilter(code string, period string) string {
	filters := []string{fmt.Sprintf(`(SECURITY_CODE="%s")`, code)}
	if date := eastmoneyTradeDateFilter(period); date != "" {
		filters = append(filters, fmt.Sprintf(`(TRADE_DATE>='%s')`, date))
	}
	return strings.Join(filters, " and ")
}

func periodLHBFilter(period string) string {
	if date := eastmoneyTradeDateFilter(period); date != "" {
		return fmt.Sprintf(`(TRADE_DATE>='%s')`, date)
	}
	return ""
}

func eastmoneyTradeDateFilter(period string) string {
	period = strings.TrimSpace(strings.ToLower(period))
	if period == "" || period == "recent" {
		return ""
	}
	if period == "today" {
		return time.Now().Format("2006-01-02")
	}
	return period
}

func appendQuery(rawURL string, params url.Values) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := u.Query()
	for key, values := range params {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func aStockSkillStatus(err error) string {
	message := err.Error()
	switch {
	case strings.HasPrefix(message, "unsupported_intent:"):
		return "unsupported_intent"
	case strings.HasPrefix(message, "missing_symbol:"):
		return "missing_symbol"
	case strings.HasPrefix(message, "not_found:"):
		return "not_found"
	default:
		return "provider_error"
	}
}

func tencentField(fields []string, index int) string {
	if index >= 0 && index < len(fields) {
		return strings.TrimSpace(fields[index])
	}
	return ""
}

func tencentFloat(fields []string, index int) float64 {
	return parseFloat(tencentField(fields, index))
}

func mapString(row map[string]interface{}, key string) string {
	if value, ok := row[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func mapFloat(row map[string]interface{}, key string) float64 {
	switch value := row[key].(type) {
	case float64:
		return value
	case json.Number:
		num, _ := value.Float64()
		return num
	case string:
		return parseFloat(value)
	default:
		return 0
	}
}

func parseFloat(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return value
}

func toWan(value float64) float64 {
	return math.Round(value/10000*100) / 100
}

func dateOnly(value string) string {
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}

func aStockMaxInt(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
