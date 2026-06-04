package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"fintrack-api/models"
)

const (
	mtfAgentMaxSkillCalls        = 2
	mtfAgentDefaultHistoryLimit  = 3
	mtfAgentMaxHistoryLimit      = 5
	mtfAgentDefaultChunkLimit    = 3
	mtfAgentMaxChunkLimit        = 6
	mtfAgentDefaultPointLimit    = 30
	mtfAgentMaxPointLimit        = 80
	mtfAgentDefaultReportsLimit  = 5
	mtfAgentMaxReportsLimit      = 10
	mtfAgentSkillResultCharLimit = 12000
)

type mtfAgentSkillExecutor func(context.Context, int, mtfAgentSkillCall) (map[string]interface{}, error)

type mtfAgentSkillCall struct {
	Skill     string                 `json:"skill"`
	Arguments map[string]interface{} `json:"arguments"`
}

func extractMTFAgentSkillCall(text string) (mtfAgentSkillCall, bool, error) {
	raw, ok := extractFencedBlock(text, "mtf-skill")
	if !ok {
		return mtfAgentSkillCall{}, false, nil
	}
	var call mtfAgentSkillCall
	if err := json.Unmarshal([]byte(raw), &call); err != nil {
		return mtfAgentSkillCall{}, true, fmt.Errorf("decode MTF Agent skill call: %w", err)
	}
	call.Skill = strings.TrimSpace(call.Skill)
	if call.Skill == "" {
		return mtfAgentSkillCall{}, true, errors.New("MTF Agent skill call missing skill")
	}
	if call.Arguments == nil {
		call.Arguments = map[string]interface{}{}
	}
	return call, true, nil
}

func extractFencedBlock(text string, language string) (string, bool) {
	needle := "```" + language
	start := strings.Index(text, needle)
	if start < 0 {
		return "", false
	}
	blockStart := start + len(needle)
	if blockStart < len(text) && text[blockStart] == '\r' {
		blockStart++
	}
	if blockStart < len(text) && text[blockStart] == '\n' {
		blockStart++
	}
	end := strings.Index(text[blockStart:], "```")
	if end < 0 {
		return "", false
	}
	return strings.TrimSpace(text[blockStart : blockStart+end]), true
}

func (s *MTFAgentService) sendMTFAgentTurnWithSkills(ctx context.Context, userID int, threadID string, prompt string, aiConfig *models.AIModelConfig) (string, string, error) {
	currentPrompt := prompt
	currentThreadID := threadID
	for attempt := 0; attempt <= mtfAgentMaxSkillCalls; attempt++ {
		nextThreadID, assistantText, err := s.sendDeepSeekTurnWithRecovery(ctx, currentThreadID, currentPrompt, aiConfig)
		if err != nil {
			return currentThreadID, "", err
		}
		currentThreadID = nextThreadID

		call, ok, err := extractMTFAgentSkillCall(assistantText)
		if err != nil {
			return currentThreadID, "", err
		}
		if !ok {
			return currentThreadID, assistantText, nil
		}
		if attempt == mtfAgentMaxSkillCalls {
			return currentThreadID, "", errors.New("MTF Agent skill call limit exceeded")
		}

		result, err := s.runMTFAgentSkill(ctx, userID, call)
		if err != nil {
			return currentThreadID, "", err
		}
		currentPrompt = buildMTFAgentSkillResultPrompt(prompt, call, result)
	}
	return currentThreadID, "", errors.New("MTF Agent skill call loop exited unexpectedly")
}

func (s *MTFAgentService) runMTFAgentSkill(ctx context.Context, userID int, call mtfAgentSkillCall) (map[string]interface{}, error) {
	if s != nil && s.skillExecutor != nil {
		return s.skillExecutor(ctx, userID, call)
	}
	return s.ExecuteMTFAgentSkill(ctx, userID, call.Skill, call.Arguments)
}

func buildMTFAgentSkillResultPrompt(originalPrompt string, call mtfAgentSkillCall, result map[string]interface{}) string {
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"error":"marshal skill result: %s"}`, err.Error()))
	}
	resultText := truncateForPrompt(string(raw), mtfAgentSkillResultCharLimit)
	callRaw, _ := json.Marshal(call)

	var builder strings.Builder
	builder.WriteString("上一步你请求调用 MTF 内置 skill。以下是内部 skill 返回结果，请基于这些数据回答原始用户问题。\n")
	builder.WriteString("除非还必须查询另一个 skill，否则不要再输出 mtf-skill 代码块。\n\n")
	builder.WriteString("原始提示：\n")
	builder.WriteString(originalPrompt)
	builder.WriteString("\n\nskill 调用：\n")
	builder.WriteString(string(callRaw))
	builder.WriteString("\n\n内部 skill 返回结果：\n")
	builder.WriteString(resultText)
	return builder.String()
}

func (s *MTFAgentService) ExecuteMTFAgentSkill(ctx context.Context, userID int, skill string, args map[string]interface{}) (map[string]interface{}, error) {
	switch strings.TrimSpace(skill) {
	case mtfAgentAStockSkillName:
		return s.executeAStockDataSkill(ctx, args), nil
	case "history_trends":
		if s == nil || s.db == nil || s.db.Conn == nil {
			return nil, errors.New("database is not configured")
		}
		return s.executeHistoryTrendsSkill(ctx, userID, args)
	case "uzi_reports":
		if s == nil || s.db == nil || s.db.Conn == nil {
			return nil, errors.New("database is not configured")
		}
		return s.executeUZIReportsSkill(ctx, userID, args)
	default:
		return nil, fmt.Errorf("unknown MTF Agent skill: %s", skill)
	}
}

func requiredAStockFields(intent string) []string {
	switch strings.TrimSpace(intent) {
	case "valuation":
		return []string{"latest_price", "market_cap", "pe_ttm", "peg", "net_profit_growth", "consensus_eps"}
	case "theme_attribution", "concept_boards":
		return []string{"concept_name", "constituents", "price_change_percent", "reason"}
	case "northbound_funds":
		return []string{"trade_date", "northbound_net_inflow", "sh_connect", "sz_connect"}
	case "money_flow", "sector_rotation":
		return []string{"trade_date", "main_net_inflow", "sector", "amount", "rank"}
	case "lhb", "market_lhb":
		return []string{"trade_date", "symbol", "net_buy_amount", "broker_seat", "buy_amount", "sell_amount"}
	case "unlock_warning":
		return []string{"unlock_date", "unlock_shares", "unlock_market_value", "unlock_ratio"}
	case "margin_trading":
		return []string{"trade_date", "financing_balance", "financing_buy", "securities_lending_balance"}
	case "block_trade":
		return []string{"trade_date", "price", "volume", "premium_discount_percent", "buyer", "seller"}
	case "shareholder_count":
		return []string{"report_date", "shareholder_count", "change_percent", "avg_holding"}
	case "dividend":
		return []string{"ex_dividend_date", "cash_dividend", "bonus_share", "dividend_yield"}
	case "news_announcements", "research_reports":
		return []string{"published_at", "title", "source", "url", "summary"}
	case "batch_compare":
		return []string{"symbol", "pe_ttm", "peg", "market_cap", "profit_growth", "industry"}
	default:
		return []string{"symbol", "trade_date", "source", "value"}
	}
}

func (s *MTFAgentService) executeHistoryTrendsSkill(ctx context.Context, userID int, args map[string]interface{}) (map[string]interface{}, error) {
	query := historyTrendSkillQuery{
		Symbol:         skillStringArg(args, "symbol"),
		UniqueKey:      skillStringArg(args, "unique_key"),
		PredictionType: skillStringArg(args, "prediction_type"),
		HorizonLen:     skillIntArg(args, "horizon_len", 0, 0, 10000),
		Limit:          skillIntArg(args, "limit", mtfAgentDefaultHistoryLimit, 1, mtfAgentMaxHistoryLimit),
		ChunkLimit:     skillIntArg(args, "chunk_limit", mtfAgentDefaultChunkLimit, 1, mtfAgentMaxChunkLimit),
		PointLimit:     skillIntArg(args, "point_limit", mtfAgentDefaultPointLimit, 5, mtfAgentMaxPointLimit),
	}
	items, err := s.queryHistoryTrendItems(ctx, userID, query)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"skill": "history_trends",
		"query": query,
		"count": len(items),
		"items": items,
	}, nil
}

type historyTrendSkillQuery struct {
	Symbol         string `json:"symbol,omitempty"`
	UniqueKey      string `json:"unique_key,omitempty"`
	PredictionType string `json:"prediction_type,omitempty"`
	HorizonLen     int    `json:"horizon_len,omitempty"`
	Limit          int    `json:"limit"`
	ChunkLimit     int    `json:"chunk_limit"`
	PointLimit     int    `json:"point_limit"`
}

func normalizeOptionalHistoryPredictionType(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return normalizeTrainPredictionType(value)
}

func (s *MTFAgentService) queryHistoryTrendItems(ctx context.Context, userID int, query historyTrendSkillQuery) ([]map[string]interface{}, error) {
	canonicalSymbol := normalizeMTFSymbolReadKey(query.Symbol)
	canonicalExpr := mtfCanonicalSymbolExpr("p.symbol")
	sqlText := fmt.Sprintf(`
		SELECT
			p.unique_key,
			p.symbol,
			COALESCE(p.short_name, ''),
			p.mtf_version,
			p.best_prediction_item,
			COALESCE(NULLIF(TRIM(p.prediction_type), ''), 'mtf-lite'),
			p.context_len,
			p.horizon_len,
			p.train_start_date,
			p.train_end_date,
			p.val_start_date,
			p.val_end_date,
			p.updated_at
		FROM mtf_best_predictions p
		WHERE ($1 = '' OR p.unique_key = $1)
		  AND ($2 = '' OR %s = $2)
		  AND ($3 = '' OR COALESCE(NULLIF(TRIM(p.prediction_type), ''), 'mtf-lite') = $3)
		  AND ($4 = 0 OR p.horizon_len = $4)
		  AND (
		    p.is_public = 1 OR EXISTS (
		      SELECT 1 FROM mtf_best_validation_chunks vc
		      WHERE vc.unique_key = p.unique_key AND vc.user_id = $5
		    )
		  )
		ORDER BY p.updated_at DESC, p.id DESC
		LIMIT $6
	`, canonicalExpr)
	rows, err := s.db.Conn.QueryContext(ctx, sqlText, strings.TrimSpace(query.UniqueKey), canonicalSymbol, normalizeOptionalHistoryPredictionType(query.PredictionType), query.HorizonLen, userID, query.Limit)
	if err != nil {
		return nil, fmt.Errorf("query history trend predictions: %w", err)
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var uniqueKey, symbol, shortName, mtfVersion, bestItem, predictionType string
		var contextLen, horizonLen int
		var trainStart, trainEnd, valStart, valEnd, updatedAt time.Time
		if err := rows.Scan(&uniqueKey, &symbol, &shortName, &mtfVersion, &bestItem, &predictionType, &contextLen, &horizonLen, &trainStart, &trainEnd, &valStart, &valEnd, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan history trend prediction: %w", err)
		}
		chunks, err := s.queryHistoryTrendChunks(ctx, uniqueKey, bestItem, query.ChunkLimit, query.PointLimit)
		if err != nil {
			return nil, err
		}
		items = append(items, map[string]interface{}{
			"unique_key":           uniqueKey,
			"symbol":               symbol,
			"short_name":           shortName,
			"mtf_version":          mtfVersion,
			"best_prediction_item": bestItem,
			"prediction_type":      predictionType,
			"context_len":          contextLen,
			"horizon_len":          horizonLen,
			"train_range":          []string{trainStart.Format("2006-01-02"), trainEnd.Format("2006-01-02")},
			"validation_range":     []string{valStart.Format("2006-01-02"), valEnd.Format("2006-01-02")},
			"updated_at":           updatedAt.Format(time.RFC3339),
			"chunks":               chunks,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history trend predictions: %w", err)
	}
	return items, nil
}

func (s *MTFAgentService) queryHistoryTrendChunks(ctx context.Context, uniqueKey string, bestItem string, chunkLimit int, pointLimit int) ([]map[string]interface{}, error) {
	rows, err := s.db.Conn.QueryContext(ctx, `
		SELECT chunk_index, start_date::text, end_date::text, predictions, actual_values,
		       COALESCE(predicted_change_percent, '{}'::jsonb),
		       COALESCE(actual_change_percent, '[]'::jsonb),
		       dates
		FROM mtf_best_validation_chunks
		WHERE unique_key = $1
		ORDER BY chunk_index DESC
		LIMIT $2
	`, uniqueKey, chunkLimit)
	if err != nil {
		return nil, fmt.Errorf("query history trend chunks: %w", err)
	}
	defer rows.Close()

	chunks := make([]map[string]interface{}, 0)
	for rows.Next() {
		var chunkIndex int
		var startDate, endDate string
		var predictionsJSON, actualJSON, predChangeJSON, actualChangeJSON, datesJSON []byte
		if err := rows.Scan(&chunkIndex, &startDate, &endDate, &predictionsJSON, &actualJSON, &predChangeJSON, &actualChangeJSON, &datesJSON); err != nil {
			return nil, fmt.Errorf("scan history trend chunk: %w", err)
		}

		var predictions map[string]interface{}
		var predictedChange map[string]interface{}
		var actualValues []float64
		var actualChange []float64
		var dates []string
		if err := json.Unmarshal(predictionsJSON, &predictions); err != nil {
			return nil, fmt.Errorf("decode history trend predictions: %w", err)
		}
		if err := json.Unmarshal(actualJSON, &actualValues); err != nil {
			return nil, fmt.Errorf("decode history trend actual values: %w", err)
		}
		if err := json.Unmarshal(predChangeJSON, &predictedChange); err != nil {
			return nil, fmt.Errorf("decode history trend predicted change: %w", err)
		}
		if err := json.Unmarshal(actualChangeJSON, &actualChange); err != nil {
			return nil, fmt.Errorf("decode history trend actual change: %w", err)
		}
		if err := json.Unmarshal(datesJSON, &dates); err != nil {
			return nil, fmt.Errorf("decode history trend dates: %w", err)
		}

		predictedValues := toMTFFloatSlice(predictions[bestItem])
		predictedChangeValues := toMTFFloatSlice(predictedChange[bestItem])
		points := buildHistoryTrendPoints(dates, actualValues, predictedValues, actualChange, predictedChangeValues, pointLimit)
		chunks = append(chunks, map[string]interface{}{
			"chunk_index": chunkIndex,
			"range":       []string{startDate, endDate},
			"point_count": len(points),
			"points":      points,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history trend chunks: %w", err)
	}
	return chunks, nil
}

func buildHistoryTrendPoints(dates []string, actual []float64, predicted []float64, actualChange []float64, predictedChange []float64, pointLimit int) []map[string]interface{} {
	maxLen := minInt(len(dates), len(actual), len(predicted))
	start := 0
	if maxLen > pointLimit {
		start = maxLen - pointLimit
	}
	points := make([]map[string]interface{}, 0, maxLen-start)
	for i := start; i < maxLen; i++ {
		a := actual[i]
		p := predicted[i]
		if a == 0 || p == 0 || math.IsNaN(a) || math.IsNaN(p) || math.IsInf(a, 0) || math.IsInf(p, 0) {
			continue
		}
		point := map[string]interface{}{
			"date":      dates[i],
			"actual":    a,
			"predicted": p,
		}
		if i < len(actualChange) {
			point["actual_change_percent"] = actualChange[i]
		}
		if i < len(predictedChange) {
			point["predicted_change_percent"] = predictedChange[i]
		}
		points = append(points, point)
	}
	return points
}

func (s *MTFAgentService) executeUZIReportsSkill(ctx context.Context, userID int, args map[string]interface{}) (map[string]interface{}, error) {
	ticker := skillStringArg(args, "ticker")
	limit := skillIntArg(args, "limit", mtfAgentDefaultReportsLimit, 1, mtfAgentMaxReportsLimit)
	canonicalTicker := normalizeMTFSymbolReadKey(ticker)
	rows, err := s.db.Conn.QueryContext(ctx, fmt.Sprintf(`
		SELECT ticker, COALESCE(depth, ''), status, report_relative_path, report_url,
		       size_bytes, COALESCE(stdout_tail, ''), updated_at
		FROM uzi_reports
		WHERE user_id = $1
		  AND deleted_at IS NULL
		  AND ($2 = '' OR LOWER(ticker) = LOWER($2) OR %s = $3)
		ORDER BY updated_at DESC, id DESC
		LIMIT $4
	`, mtfCanonicalSymbolExpr("ticker")), userID, strings.TrimSpace(ticker), canonicalTicker, limit)
	if err != nil {
		return nil, fmt.Errorf("query UZI reports: %w", err)
	}
	defer rows.Close()

	items := make([]map[string]interface{}, 0)
	for rows.Next() {
		var reportTicker, depth, status, relativePath, reportURL, stdoutTail string
		var sizeBytes int64
		var updatedAt time.Time
		if err := rows.Scan(&reportTicker, &depth, &status, &relativePath, &reportURL, &sizeBytes, &stdoutTail, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan UZI report: %w", err)
		}
		items = append(items, map[string]interface{}{
			"ticker":               reportTicker,
			"depth":                depth,
			"status":               status,
			"report_relative_path": relativePath,
			"report_url":           reportURL,
			"size_bytes":           sizeBytes,
			"stdout_tail":          truncateForPrompt(stdoutTail, 800),
			"updated_at":           updatedAt.Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate UZI reports: %w", err)
	}
	return map[string]interface{}{
		"skill": "uzi_reports",
		"query": map[string]interface{}{
			"ticker": ticker,
			"limit":  limit,
		},
		"count": len(items),
		"items": items,
	}, nil
}

func skillStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func skillStringSliceArg(args map[string]interface{}, key string) []string {
	if args == nil || args[key] == nil {
		return []string{}
	}
	rawItems, ok := args[key].([]interface{})
	if !ok {
		if single := skillStringArg(args, key); single != "" {
			return []string{single}
		}
		return []string{}
	}
	items := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item := strings.TrimSpace(fmt.Sprint(rawItem))
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func skillIntArg(args map[string]interface{}, key string, fallback int, minValue int, maxValue int) int {
	if args == nil {
		return fallback
	}
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	var parsed int
	switch typed := value.(type) {
	case int:
		parsed = typed
	case int64:
		parsed = int(typed)
	case float64:
		parsed = int(typed)
	case json.Number:
		n, err := typed.Int64()
		if err != nil {
			return fallback
		}
		parsed = int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		parsed = n
	default:
		return fallback
	}
	if minValue > 0 && parsed < minValue {
		return minValue
	}
	if maxValue > 0 && parsed > maxValue {
		return maxValue
	}
	return parsed
}

func toMTFFloatSlice(val interface{}) []float64 {
	values := make([]float64, 0)
	switch typed := val.(type) {
	case []float64:
		return typed
	case []interface{}:
		for _, item := range typed {
			switch v := item.(type) {
			case float64:
				values = append(values, v)
			case int:
				values = append(values, float64(v))
			case json.Number:
				if f, err := v.Float64(); err == nil {
					values = append(values, f)
				}
			}
		}
	}
	return values
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	minValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
	}
	return minValue
}
