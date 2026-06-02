package services

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
)

var (
	htmlTagPattern      = regexp.MustCompile(`<[^>]+>`)
	hotETFHeaderPattern = regexp.MustCompile(`(?i)(标的|名称|代码|rps|月评分|周评分|日评分|防守止损|加权总分|状态|signal|score)`)
	stockCodePattern    = regexp.MustCompile(`\d{6}`)
)

type financeNewsColumnPayload struct {
	Data struct {
		List []struct {
			Code      string `json:"code"`
			Title     string `json:"title"`
			Summary   string `json:"summary"`
			ShowTime  string `json:"showTime"`
			MediaName string `json:"mediaName"`
			URL       string `json:"url"`
			UniqueURL string `json:"uniqueUrl"`
		} `json:"list"`
	} `json:"data"`
}

type financeNewsSearchPayload struct {
	Result struct {
		CMSArticleWebOld []struct {
			Code      string `json:"code"`
			Title     string `json:"title"`
			Content   string `json:"content"`
			Date      string `json:"date"`
			MediaName string `json:"mediaName"`
			URL       string `json:"url"`
		} `json:"cmsArticleWebOld"`
	} `json:"result"`
}

type financeNewsAnnouncementPayload struct {
	Data struct {
		List []struct {
			ArtCode     string `json:"art_code"`
			Title       string `json:"title"`
			TitleCH     string `json:"title_ch"`
			DisplayTime string `json:"display_time"`
			NoticeDate  string `json:"notice_date"`
			Codes       []struct {
				StockCode string `json:"stock_code"`
				ShortName string `json:"short_name"`
			} `json:"codes"`
			Columns []struct {
				ColumnName string `json:"column_name"`
			} `json:"columns"`
		} `json:"list"`
	} `json:"data"`
}

func financeNewsColumn(category string) string {
	switch category {
	case "global":
		return "349"
	default:
		return "350"
	}
}

func financeNewsResponse(query FinanceNewsQuery, source string, items []FinanceNewsItem) FinanceNewsResponse {
	return FinanceNewsResponse{
		Status:   "ok",
		Source:   source,
		Category: query.Category,
		Query:    query,
		Count:    len(items),
		Items:    items,
	}
}

func buildEastmoneySearchParam(keyword string, page int, limit int) string {
	payload := map[string]interface{}{
		"uid":           "",
		"keyword":       keyword,
		"type":          []string{"cmsArticleWebOld"},
		"client":        "web",
		"clientType":    "web",
		"clientVersion": "curr",
		"param": map[string]interface{}{
			"cmsArticleWebOld": map[string]interface{}{
				"searchScope": "default",
				"sort":        "default",
				"pageIndex":   page,
				"pageSize":    limit,
				"preTag":      "",
				"postTag":     "",
			},
		},
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func decodeJSONOrJSONP(body string, target interface{}) error {
	raw := strings.TrimSpace(body)
	if start := strings.Index(raw, "("); start >= 0 {
		if end := strings.LastIndex(raw, ")"); end > start {
			raw = raw[start+1 : end]
		}
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("provider_error: decode finance news: %w", err)
	}
	return nil
}

func announcementStockListCode(code string) string {
	suffix := "SZ"
	if strings.HasPrefix(code, "6") || strings.HasPrefix(code, "9") {
		suffix = "SH"
	} else if strings.HasPrefix(code, "8") || strings.HasPrefix(code, "4") {
		suffix = "BJ"
	}
	return code + "." + suffix
}

func firstAnnouncementCode(codes []struct {
	StockCode string `json:"stock_code"`
	ShortName string `json:"short_name"`
}) string {
	if len(codes) == 0 {
		return ""
	}
	return codes[0].StockCode
}

func firstAnnouncementName(codes []struct {
	StockCode string `json:"stock_code"`
	ShortName string `json:"short_name"`
}) string {
	if len(codes) == 0 {
		return ""
	}
	return codes[0].ShortName
}

func firstAnnouncementColumn(columns []struct {
	ColumnName string `json:"column_name"`
}) string {
	if len(columns) == 0 {
		return ""
	}
	return columns[0].ColumnName
}

func financeNewsContains(value string, keyword string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(keyword))
}

func financeNewsLHBItem(row map[string]interface{}) FinanceNewsItem {
	code := mapString(row, "SECURITY_CODE")
	name := mapString(row, "SECURITY_NAME_ABBR")
	tradeDate := dateOnly(mapString(row, "TRADE_DATE"))
	reason := firstNonEmptyString(mapString(row, "EXPLANATION"), mapString(row, "EXPLAIN"))
	netWan := toWan(mapFloat(row, "BILLBOARD_NET_AMT"))
	buyWan := toWan(mapFloat(row, "BILLBOARD_BUY_AMT"))
	sellWan := toWan(mapFloat(row, "BILLBOARD_SELL_AMT"))
	changeRate := mapFloat(row, "CHANGE_RATE")
	turnoverRate := mapFloat(row, "TURNOVERRATE")

	title := strings.TrimSpace(fmt.Sprintf("%s %s 龙虎榜", code, name))
	summary := fmt.Sprintf("净买入 %.2f 万元，买入 %.2f 万元，卖出 %.2f 万元；涨跌幅 %.2f%%，换手率 %.2f%%。", netWan, buyWan, sellWan, changeRate, turnoverRate)
	if reason != "" {
		summary += reason
	}

	return FinanceNewsItem{
		ID:          strings.Trim(strings.Join([]string{code, tradeDate, reason}, "-"), "-"),
		Title:       title,
		Summary:     summary,
		PublishedAt: tradeDate,
		Source:      "东方财富龙虎榜",
		URL:         "https://data.eastmoney.com/stock/lhb.html",
		Symbol:      code,
		StockName:   name,
		Category:    "lhb",
	}
}

func normalizeHotETFText(body string) string {
	text := body
	text = strings.ReplaceAll(text, "\r", "\n")
	text = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(text, "\n")
	text = regexp.MustCompile(`(?i)</(tr|div|p|li|section|article|h[1-6])>`).ReplaceAllString(text, "\n")
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)

	lines := strings.Split(text, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.Join(strings.Fields(line), " ")
		if clean == "" || hotETFHeaderPattern.MatchString(clean) && !stockCodePattern.MatchString(clean) {
			continue
		}
		normalized = append(normalized, clean)
	}
	return strings.Join(normalized, "\n")
}

func readHTTPText(reader io.Reader) (string, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("provider_error: read finance news: %w", err)
	}
	return string(body), nil
}
