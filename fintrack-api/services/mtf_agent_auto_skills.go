package services

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var mtfAgentSymbolPattern = regexp.MustCompile(`(?i)\b(?:sh|sz)?\d{6}(?:\.(?:sh|sz))?\b`)

func (s *MTFAgentService) BuildMTFAgentAutoSkillContext(ctx context.Context, userID int, message string) string {
	results := s.collectMTFAgentAutoSkillResults(ctx, userID, message)
	if len(results) == 0 {
		return ""
	}
	raw, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Sprintf("MTF 内置 skill 已自动预取，但序列化失败：%s", err.Error())
	}
	return "MTF 内置 skill 已自动预取。优先使用以下内部数据回答，避免再次请求相同 skill：\n" +
		truncateForPrompt(string(raw), mtfAgentSkillResultCharLimit)
}

func (s *MTFAgentService) collectMTFAgentAutoSkillResults(ctx context.Context, userID int, message string) []map[string]interface{} {
	symbol := extractMTFAgentSymbol(message)
	if symbol == "" {
		return nil
	}

	calls := autoSkillCallsForMessage(message, symbol)
	if len(calls) == 0 {
		return nil
	}

	results := make([]map[string]interface{}, 0, len(calls))
	for _, call := range calls {
		result, err := s.runMTFAgentSkill(ctx, userID, call)
		if err != nil {
			results = append(results, map[string]interface{}{
				"skill": call.Skill,
				"error": err.Error(),
			})
			continue
		}
		results = append(results, result)
	}
	return results
}

func autoSkillCallsForMessage(message string, symbol string) []mtfAgentSkillCall {
	lower := strings.ToLower(message)
	calls := make([]mtfAgentSkillCall, 0, 2)
	if containsAny(lower, "走势", "趋势", "历史", "预测") {
		calls = append(calls, mtfAgentSkillCall{
			Skill: "history_trends",
			Arguments: map[string]interface{}{
				"symbol":      symbol,
				"limit":       1,
				"chunk_limit": 2,
				"point_limit": 24,
			},
		})
	}
	if containsAny(lower, "研报", "报告", "研究") {
		calls = append(calls, mtfAgentSkillCall{
			Skill: "uzi_reports",
			Arguments: map[string]interface{}{
				"ticker": symbol,
				"limit":  3,
			},
		})
	}
	return calls
}

func extractMTFAgentSymbol(message string) string {
	match := mtfAgentSymbolPattern.FindString(message)
	return strings.TrimSpace(match)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
