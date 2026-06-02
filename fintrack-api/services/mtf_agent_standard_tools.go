package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"fintrack-api/models"
)

const mtfAgentAStockSkillName = "a_stock_data"

type mtfAgentRuntimeRequest struct {
	Prompt   string
	Messages []map[string]interface{}
	Tools    []map[string]interface{}
}

type mtfAgentRuntimeAssistant struct {
	Content          string
	ReasoningContent string
	ToolCalls        []mtfAgentRuntimeToolCall
}

type mtfAgentRuntimeToolCall struct {
	ID       string                      `json:"id"`
	Type     string                      `json:"type"`
	Function mtfAgentRuntimeToolFunction `json:"function"`
}

type mtfAgentRuntimeToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func buildMTFAgentStandardTools() []map[string]interface{} {
	return []map[string]interface{}{
		buildMTFAgentToolSchema(
			"history_trends",
			"查询 MTF 历史走势、历史预测趋势和验证分段数据。",
			map[string]interface{}{
				"symbol":          map[string]interface{}{"type": "string", "description": "股票代码，如 601766.SH 或 601766"},
				"unique_key":      map[string]interface{}{"type": "string", "description": "MTF prediction unique_key"},
				"prediction_type": map[string]interface{}{"type": "string", "enum": []string{"non_cov", "cov"}},
				"horizon_len":     map[string]interface{}{"type": "integer"},
				"limit":           map[string]interface{}{"type": "integer", "minimum": 1, "maximum": mtfAgentMaxHistoryLimit},
				"chunk_limit":     map[string]interface{}{"type": "integer", "minimum": 1, "maximum": mtfAgentMaxChunkLimit},
				"point_limit":     map[string]interface{}{"type": "integer", "minimum": 5, "maximum": mtfAgentMaxPointLimit},
			},
		),
		buildMTFAgentToolSchema(
			"uzi_reports",
			"查询用户已有 UZI 研报索引和报告摘要。",
			map[string]interface{}{
				"ticker": map[string]interface{}{"type": "string", "description": "股票代码，如 601766.SH"},
				"limit":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": mtfAgentMaxReportsLimit},
			},
		),
		buildMTFAgentToolSchema(
			mtfAgentAStockSkillName,
			"处理 A 股数据问题，包括估值、题材归因、研报检索、北向资金、概念板块、资金流向、龙虎榜、解禁、行业轮动、融资融券、大宗交易、股东户数、分红送转、新闻公告和批量对比。",
			map[string]interface{}{
				"intent": map[string]interface{}{
					"type": "string",
					"enum": []string{"valuation", "theme_attribution", "research_reports", "northbound_funds", "concept_boards", "money_flow", "lhb", "market_lhb", "unlock_warning", "sector_rotation", "margin_trading", "block_trade", "shareholder_count", "dividend", "news_announcements", "batch_compare"},
				},
				"symbol":   map[string]interface{}{"type": "string", "description": "单只股票代码或名称"},
				"symbols":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"topic":    map[string]interface{}{"type": "string", "description": "产业链、题材或研报主题"},
				"period":   map[string]interface{}{"type": "string", "description": "时间范围，如 today、3_months、recent"},
				"question": map[string]interface{}{"type": "string", "description": "用户原始问题"},
				"limit":    map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 50},
			},
		),
	}
}

func buildMTFAgentToolSchema(name string, description string, properties map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": description,
			"parameters": map[string]interface{}{
				"type":                 "object",
				"properties":           properties,
				"additionalProperties": false,
			},
		},
	}
}

func (s *MTFAgentService) sendDeepSeekTurnWithTools(ctx context.Context, threadID string, request mtfAgentRuntimeRequest, aiConfig *models.AIModelConfig) (mtfAgentRuntimeAssistant, error) {
	payload := map[string]interface{}{}
	if strings.TrimSpace(request.Prompt) != "" {
		payload["prompt"] = strings.TrimSpace(request.Prompt)
	}
	if len(request.Messages) > 0 {
		payload["messages"] = request.Messages
	}
	if len(request.Tools) > 0 {
		payload["tools"] = request.Tools
		payload["tool_choice"] = "auto"
	}
	if aiModel := mtfRuntimeAIModelPayload(aiConfig); aiModel != nil {
		payload["ai_model"] = aiModel
	}
	body, err := s.doRuntimeJSON(ctx, "POST", "/v1/threads/"+url.PathEscape(threadID)+"/turns", payload, aiConfig)
	if err != nil {
		return mtfAgentRuntimeAssistant{}, err
	}
	assistant := parseMTFAgentRuntimeAssistant(body)
	if turnID := parseDeepSeekTurnID(body); turnID != "" {
		status, runtimeErr := findDeepSeekTurnState(body, turnID)
		if strings.EqualFold(status, "failed") {
			if runtimeErr == "" {
				runtimeErr = "deepseek runtime turn failed"
			}
			return mtfAgentRuntimeAssistant{}, errors.New(runtimeErr)
		}
		if len(assistant.ToolCalls) > 0 || strings.TrimSpace(assistant.Content) != "" || strings.EqualFold(status, "completed") {
			return assistant, nil
		}
		text, err := s.waitDeepSeekTurn(ctx, threadID, turnID, aiConfig)
		return mtfAgentRuntimeAssistant{Content: text}, err
	}
	return assistant, nil
}

func parseMTFAgentRuntimeAssistant(body map[string]interface{}) mtfAgentRuntimeAssistant {
	assistant := mtfAgentRuntimeAssistant{}
	for _, key := range []string{"message", "output", "text"} {
		if value := stringField(body, key); value != "" {
			assistant.Content = value
			break
		}
	}
	assistant.ReasoningContent = stringField(body, "reasoning_content")
	rawCalls, ok := body["tool_calls"].([]interface{})
	if !ok {
		if message, ok := body["message"].(map[string]interface{}); ok {
			assistant.Content = stringField(message, "content")
			assistant.ReasoningContent = stringField(message, "reasoning_content")
			rawCalls, _ = message["tool_calls"].([]interface{})
		}
	}
	for _, rawCall := range rawCalls {
		callMap, ok := rawCall.(map[string]interface{})
		if !ok {
			continue
		}
		functionMap, _ := callMap["function"].(map[string]interface{})
		call := mtfAgentRuntimeToolCall{
			ID:   stringField(callMap, "id"),
			Type: firstNonEmptyString(stringField(callMap, "type"), "function"),
			Function: mtfAgentRuntimeToolFunction{
				Name:      stringField(functionMap, "name"),
				Arguments: stringField(functionMap, "arguments"),
			},
		}
		if call.Function.Name != "" {
			assistant.ToolCalls = append(assistant.ToolCalls, call)
		}
	}
	if len(assistant.ToolCalls) == 0 && strings.TrimSpace(assistant.Content) == "" {
		assistant.Content = extractAssistantText(body)
	}
	return assistant
}

func (s *MTFAgentService) sendDeepSeekTurnWithToolsAndRecovery(ctx context.Context, threadID string, request mtfAgentRuntimeRequest, aiConfig *models.AIModelConfig) (string, mtfAgentRuntimeAssistant, error) {
	assistant, err := s.sendDeepSeekTurnWithTools(ctx, threadID, request, aiConfig)
	if err == nil {
		return threadID, assistant, nil
	}
	if !isDeepSeekThreadNotFoundError(err) {
		return threadID, mtfAgentRuntimeAssistant{}, err
	}
	newThreadID, err := s.createDeepSeekThread(ctx, aiConfig)
	if err != nil {
		return "", mtfAgentRuntimeAssistant{}, err
	}
	assistant, err = s.sendDeepSeekTurnWithTools(ctx, newThreadID, request, aiConfig)
	if err != nil {
		return newThreadID, mtfAgentRuntimeAssistant{}, err
	}
	return newThreadID, assistant, nil
}

func (s *MTFAgentService) sendMTFAgentTurnWithStandardTools(ctx context.Context, userID int, threadID string, prompt string, aiConfig *models.AIModelConfig) (string, string, error) {
	tools := buildMTFAgentStandardTools()
	currentThreadID := threadID
	messages := []map[string]interface{}{{"role": "user", "content": prompt}}
	request := mtfAgentRuntimeRequest{Prompt: prompt, Tools: tools}

	for attempt := 0; attempt <= mtfAgentMaxSkillCalls; attempt++ {
		nextThreadID, assistant, err := s.sendDeepSeekTurnWithToolsAndRecovery(ctx, currentThreadID, request, aiConfig)
		if err != nil {
			return currentThreadID, "", err
		}
		currentThreadID = nextThreadID
		if len(assistant.ToolCalls) == 0 {
			call, ok, err := extractMTFAgentSkillCall(assistant.Content)
			if err != nil || !ok {
				return currentThreadID, assistant.Content, err
			}
			result, err := s.runMTFAgentSkill(ctx, userID, call)
			if err != nil {
				return currentThreadID, "", err
			}
			request = mtfAgentRuntimeRequest{
				Prompt: buildMTFAgentSkillResultPrompt(prompt, call, result),
				Tools:  tools,
			}
			continue
		}
		if attempt == mtfAgentMaxSkillCalls {
			return currentThreadID, "", errors.New("MTF Agent standard tool call limit exceeded")
		}

		messages = append(messages, buildMTFAgentAssistantToolCallMessage(assistant))
		for _, toolCall := range assistant.ToolCalls {
			call, err := mtfAgentSkillCallFromRuntimeToolCall(toolCall)
			if err != nil {
				return currentThreadID, "", err
			}
			result, err := s.runMTFAgentSkill(ctx, userID, call)
			if err != nil {
				return currentThreadID, "", err
			}
			messages = append(messages, buildMTFAgentToolResultMessage(toolCall, result))
		}
		request = mtfAgentRuntimeRequest{Messages: messages, Tools: tools}
	}
	return currentThreadID, "", errors.New("MTF Agent standard tool call loop exited unexpectedly")
}

func buildMTFAgentAssistantToolCallMessage(assistant mtfAgentRuntimeAssistant) map[string]interface{} {
	rawCalls := make([]map[string]interface{}, 0, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		rawCalls = append(rawCalls, map[string]interface{}{
			"id":   call.ID,
			"type": firstNonEmptyString(call.Type, "function"),
			"function": map[string]interface{}{
				"name":      call.Function.Name,
				"arguments": call.Function.Arguments,
			},
		})
	}
	message := map[string]interface{}{
		"role":       "assistant",
		"content":    assistant.Content,
		"tool_calls": rawCalls,
	}
	if strings.TrimSpace(assistant.ReasoningContent) != "" {
		message["reasoning_content"] = assistant.ReasoningContent
	}
	return message
}

func buildMTFAgentToolResultMessage(toolCall mtfAgentRuntimeToolCall, result map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(result)
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"error":"marshal skill result: %s"}`, err.Error()))
	}
	return map[string]interface{}{
		"role":         "tool",
		"tool_call_id": toolCall.ID,
		"content":      truncateForPrompt(string(raw), mtfAgentSkillResultCharLimit),
	}
}

func mtfAgentSkillCallFromRuntimeToolCall(toolCall mtfAgentRuntimeToolCall) (mtfAgentSkillCall, error) {
	name := strings.TrimSpace(toolCall.Function.Name)
	if name == "" {
		return mtfAgentSkillCall{}, errors.New("MTF Agent tool call missing function name")
	}
	args := map[string]interface{}{}
	rawArgs := strings.TrimSpace(toolCall.Function.Arguments)
	if rawArgs != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return mtfAgentSkillCall{}, fmt.Errorf("decode MTF Agent tool call arguments: %w", err)
		}
	}
	return mtfAgentSkillCall{Skill: name, Arguments: args}, nil
}
