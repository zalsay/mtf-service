package services

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var (
	hotETFCodeRe     = regexp.MustCompile(`\b(\d{6})\b`)
	hotETFRiskRPSRe  = regexp.MustCompile(`风险RPS\s*([+-]?\d+(?:\.\d+)?)`)
	hotETFPriorityRe = regexp.MustCompile(`雷达优先级\s*([+-]?\d+(?:\.\d+)?)\s*·\s*([A-Z])?`)
	hotETFNumberRe   = regexp.MustCompile(`[+-]?\d+(?:\.\d+)?`)
)

func parseHotETFHTML(body string) ([]HotETFItem, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("provider_error: parse hot ETF html: %w", err)
	}
	table := findElementByID(doc, "radarTable")
	if table == nil {
		return nil, fmt.Errorf("provider_error: hot ETF table not found")
	}
	rows := collectTableRows(table)
	items := make([]HotETFItem, 0, len(rows))
	for _, row := range rows {
		cells := collectTableCells(row)
		if len(cells) < 8 {
			continue
		}
		item := HotETFItem{
			Name:          hotETFName(cells[0]),
			Code:          firstSubmatch(hotETFCodeRe, cells[0]),
			RiskRPS:       parseFirstFloat(firstSubmatch(hotETFRiskRPSRe, cells[0])),
			RadarPriority: parseFirstFloat(firstSubmatch(hotETFPriorityRe, cells[0])),
			Grade:         hotETFGrade(cells[0]),
			Trend:         strings.TrimSpace(cells[1]),
			Month:         hotETFSignal(cells[2]),
			Week:          hotETFSignal(cells[3]),
			Day:           hotETFSignal(cells[4]),
			StopPrice:     hotETFStopPrice(cells[5]),
			StopDistance:  hotETFStopDistance(cells[5]),
			TotalScore:    parseFirstFloat(cells[6]),
			Status:        strings.TrimSpace(cells[7]),
		}
		if item.Code == "" || item.Name == "" {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func findElementByID(node *html.Node, id string) *html.Node {
	if node == nil {
		return nil
	}
	for _, attr := range node.Attr {
		if attr.Key == "id" && attr.Val == id {
			return node
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func collectTableRows(node *html.Node) []*html.Node {
	var rows []*html.Node
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.ElementNode && current.Data == "tr" {
			rows = append(rows, current)
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return rows
}

func collectTableCells(row *html.Node) []string {
	var cells []string
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "td" {
			cells = append(cells, normalizeHTMLText(child))
		}
	}
	return cells
}

func normalizeHTMLText(node *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			text := strings.TrimSpace(current.Data)
			if text != "" {
				parts = append(parts, text)
			}
		}
		if current.Type == html.ElementNode && current.Data == "title" {
			text := textContent(current)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(parts, " ")
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func hotETFName(value string) string {
	if code := hotETFCodeRe.FindStringIndex(value); code != nil {
		return strings.TrimSpace(value[:code[0]])
	}
	return strings.TrimSpace(value)
}

func hotETFGrade(value string) string {
	matches := hotETFPriorityRe.FindStringSubmatch(value)
	if len(matches) >= 3 {
		return strings.TrimSpace(matches[2])
	}
	return ""
}

func hotETFSignal(value string) HotETFSignal {
	scoreText := hotETFNumberRe.FindString(value)
	text := strings.TrimSpace(strings.TrimPrefix(value, scoreText))
	return HotETFSignal{Score: parseFirstFloat(scoreText), Text: text}
}

func hotETFStopPrice(value string) string {
	if idx := strings.Index(value, "参考止损"); idx >= 0 {
		rest := strings.TrimSpace(strings.TrimPrefix(value[idx:], "参考止损"))
		return hotETFNumberRe.FindString(rest)
	}
	return ""
}

func hotETFStopDistance(value string) string {
	matches := regexp.MustCompile(`[+-]?\d+(?:\.\d+)?%`).FindAllString(value, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func firstSubmatch(re *regexp.Regexp, value string) string {
	matches := re.FindStringSubmatch(value)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

func parseFirstFloat(value string) float64 {
	text := hotETFNumberRe.FindString(value)
	if text == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}
	return parsed
}
