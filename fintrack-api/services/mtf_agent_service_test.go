package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fintrack-api/config"
	"fintrack-api/models"
)

func TestMTFAgentConfigDefaults(t *testing.T) {
	cfg := config.Config{}
	if cfg.MTFAgent.BaseURL != "" {
		t.Fatalf("zero config BaseURL = %q, want empty", cfg.MTFAgent.BaseURL)
	}
}

func TestMTFAgentDefaultModel(t *testing.T) {
	service := NewMTFAgentService(nil, &config.Config{})
	if got := service.defaultModelID(); got != RecommendedAIModelID {
		t.Fatalf("defaultModelID = %q, want %q", got, RecommendedAIModelID)
	}

	service = NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{DefaultModel: "custom-model"},
	})
	if got := service.defaultModelID(); got != "custom-model" {
		t.Fatalf("defaultModelID = %q, want custom-model", got)
	}
}

func TestRenderMTFAgentMemoryBlock(t *testing.T) {
	memories := []models.MTFAgentMemory{
		{MemoryType: "risk_preference", Content: "偏好低波动现金流资产"},
		{MemoryType: "output_style", Content: "回答先给结论"},
	}

	block := RenderMTFAgentMemoryBlock(memories)

	for _, want := range []string{"用户长期偏好", "偏好低波动现金流资产", "回答先给结论"} {
		if !strings.Contains(block, want) {
			t.Fatalf("memory block missing %q: %s", want, block)
		}
	}
}

func TestParseDeepSeekThreadID(t *testing.T) {
	body := map[string]interface{}{"id": "thr_123"}
	if got := parseDeepSeekThreadID(body); got != "thr_123" {
		t.Fatalf("parseDeepSeekThreadID id = %q, want thr_123", got)
	}

	body = map[string]interface{}{"thread": map[string]interface{}{"id": "thr_nested"}}
	if got := parseDeepSeekThreadID(body); got != "thr_nested" {
		t.Fatalf("parseDeepSeekThreadID nested = %q, want thr_nested", got)
	}
}

func TestMTFAgentRuntimeUsesGatewayBasePathAndUserAPIKey(t *testing.T) {
	var gotPath string
	var gotGatewayToken string
	var gotDeepSeekAPIKey string
	var gotDeepSeekBaseURL string
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotGatewayToken = r.Header.Get("X-Gateway-API-Token")
		gotDeepSeekAPIKey = r.Header.Get("X-DeepSeek-API-Key")
		gotDeepSeekBaseURL = r.Header.Get("X-DeepSeek-Base-URL")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "thr_gateway"})
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled:      true,
			BaseURL:      server.URL + "/deepseek-tui",
			RuntimeToken: "proxy-token",
		},
	})

	aiConfig := &models.AIModelConfig{
		BaseURL: "https://api.deepseek.com",
		APIKey:  "user-api-key",
		ModelID: "deepseek-v4-pro",
	}
	threadID, err := service.createDeepSeekThread(context.Background(), aiConfig)
	if err != nil {
		t.Fatalf("createDeepSeekThread error = %v", err)
	}
	if threadID != "thr_gateway" {
		t.Fatalf("threadID = %q, want thr_gateway", threadID)
	}
	if gotPath != "/deepseek-tui/v1/threads" {
		t.Fatalf("runtime path = %q, want /deepseek-tui/v1/threads", gotPath)
	}
	if gotGatewayToken != "proxy-token" {
		t.Fatalf("X-Gateway-API-Token = %q, want proxy-token", gotGatewayToken)
	}
	if gotDeepSeekAPIKey != "user-api-key" {
		t.Fatalf("X-DeepSeek-API-Key = %q, want user-api-key", gotDeepSeekAPIKey)
	}
	if gotDeepSeekBaseURL != "https://api.deepseek.com" {
		t.Fatalf("X-DeepSeek-Base-URL = %q, want https://api.deepseek.com", gotDeepSeekBaseURL)
	}
	if prompt, _ := gotPayload["system_prompt"].(string); strings.Contains(prompt, "FinTrack") || !strings.Contains(prompt, "MTF") {
		t.Fatalf("system_prompt should mention MTF instead of FinTrack: %q", prompt)
	}
}

func TestReadMTFAgentRuntimeSSEForwardsDeltaAndError(t *testing.T) {
	var chunks []string
	stream := strings.NewReader(strings.Join([]string{
		"event: delta",
		`data: {"text":"你"}`,
		"",
		"event: delta",
		`data: {"text":"好"}`,
		"",
	}, "\n"))

	err := readMTFAgentRuntimeSSE(stream, func(text string) {
		chunks = append(chunks, text)
	})
	if err != nil {
		t.Fatalf("readMTFAgentRuntimeSSE delta error = %v", err)
	}
	if got := strings.Join(chunks, ""); got != "你好" {
		t.Fatalf("stream chunks = %q, want 你好", got)
	}

	err = readMTFAgentRuntimeSSE(strings.NewReader("event: error\ndata: {\"error\":\"runtime failed\"}\n\n"), func(string) {})
	if err == nil || !strings.Contains(err.Error(), "runtime failed") {
		t.Fatalf("readMTFAgentRuntimeSSE error = %v, want runtime failed", err)
	}
}

func TestMTFAgentTurnSendsPromptPayload(t *testing.T) {
	var gotPath string
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "ok"})
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
	})
	aiConfig := &models.AIModelConfig{
		BaseURL: "https://api.deepseek.com",
		APIKey:  "user-api-key",
		ModelID: "deepseek-v4-pro",
	}

	assistantText, err := service.sendDeepSeekTurn(context.Background(), "thr_123", "hello", aiConfig)
	if err != nil {
		t.Fatalf("sendDeepSeekTurn error = %v", err)
	}
	if assistantText != "ok" {
		t.Fatalf("assistantText = %q, want ok", assistantText)
	}
	if gotPath != "/deepseek-tui/v1/threads/thr_123/turns" {
		t.Fatalf("runtime path = %q, want /deepseek-tui/v1/threads/thr_123/turns", gotPath)
	}
	if gotPayload["prompt"] != "hello" {
		t.Fatalf("prompt payload = %q, want hello", gotPayload["prompt"])
	}
	if _, exists := gotPayload["message"]; exists {
		t.Fatalf("turn payload should not include message field: %#v", gotPayload)
	}
}

func TestMTFAgentTurnSendsStandardToolsPayload(t *testing.T) {
	var gotPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "ok"})
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
	})
	aiConfig := &models.AIModelConfig{
		BaseURL: "https://api.deepseek.com",
		APIKey:  "user-api-key",
		ModelID: "deepseek-v4-pro",
	}

	assistant, err := service.sendDeepSeekTurnWithTools(context.Background(), "thr_123", mtfAgentRuntimeRequest{
		Prompt: "帮我估一下 688017",
		Tools:  buildMTFAgentStandardTools(),
	}, aiConfig)
	if err != nil {
		t.Fatalf("sendDeepSeekTurnWithTools error = %v", err)
	}
	if assistant.Content != "ok" {
		t.Fatalf("assistant content = %q, want ok", assistant.Content)
	}
	if gotPayload["prompt"] != "帮我估一下 688017" {
		t.Fatalf("prompt payload = %q, want user prompt", gotPayload["prompt"])
	}
	if gotPayload["tool_choice"] != "auto" {
		t.Fatalf("tool_choice = %v, want auto", gotPayload["tool_choice"])
	}
	tools, ok := gotPayload["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("tools payload missing: %#v", gotPayload["tools"])
	}
	if !runtimeToolsContain(tools, "a_stock_data") {
		t.Fatalf("tools payload missing a_stock_data: %#v", tools)
	}
}

func TestSendMTFAgentTurnWithStandardToolsExecutesToolCalls(t *testing.T) {
	requests := make([]map[string]interface{}, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requests = append(requests, payload)
		switch len(requests) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"message":           "",
				"reasoning_content": "需要调用 A 股数据工具。",
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "a_stock_data",
							"arguments": `{"intent":"valuation","symbol":"688017","question":"帮我估一下 688017"}`,
						},
					},
				},
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "688017 估值需要实时数据源。"})
		default:
			t.Fatalf("unexpected runtime request count %d", len(requests))
		}
	}))
	defer server.Close()

	var calls []mtfAgentSkillCall
	service := &MTFAgentService{
		config: &config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
		skillExecutor: func(ctx context.Context, userID int, call mtfAgentSkillCall) (map[string]interface{}, error) {
			calls = append(calls, call)
			return map[string]interface{}{
				"skill":  call.Skill,
				"symbol": call.Arguments["symbol"],
				"status": "requires_realtime_data_source",
			}, nil
		},
	}
	aiConfig := &models.AIModelConfig{BaseURL: "https://api.deepseek.com", APIKey: "user-api-key", ModelID: "deepseek-v4-pro"}

	threadID, assistantText, err := service.sendMTFAgentTurnWithStandardTools(context.Background(), 7, "thr_123", "帮我估一下 688017", aiConfig)
	if err != nil {
		t.Fatalf("sendMTFAgentTurnWithStandardTools error = %v", err)
	}
	if threadID != "thr_123" {
		t.Fatalf("threadID = %q, want thr_123", threadID)
	}
	if assistantText != "688017 估值需要实时数据源。" {
		t.Fatalf("assistantText = %q", assistantText)
	}
	if len(calls) != 1 || calls[0].Skill != "a_stock_data" {
		t.Fatalf("skill calls = %#v, want a_stock_data", calls)
	}
	messages, ok := requests[1]["messages"].([]interface{})
	if !ok || len(messages) < 3 {
		t.Fatalf("second request missing standard messages: %#v", requests[1]["messages"])
	}
	if !runtimeMessagesContainRole(messages, "tool") {
		t.Fatalf("second request should include tool result message: %#v", messages)
	}
	if !runtimeAssistantMessageContains(messages, "reasoning_content", "需要调用 A 股数据工具。") {
		t.Fatalf("second request should preserve assistant reasoning_content: %#v", messages)
	}
}

func TestSendMTFAgentTurnWithStandardToolsUsesSynchronousToolCalls(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if len(requests) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turn":              map[string]string{"id": "turn_123", "status": "completed"},
				"message":           "",
				"reasoning_content": "需要调用 A 股数据工具。",
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "a_stock_data",
							"arguments": `{"intent":"valuation","symbol":"688017"}`,
						},
					},
				},
			})
			return
		}
		if len(requests) == 2 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turn":    map[string]string{"id": "turn_456", "status": "completed"},
				"message": "688017 估值需要实时数据源。",
			})
			return
		}
		t.Fatalf("unexpected runtime request %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	var calls []mtfAgentSkillCall
	service := &MTFAgentService{
		config: &config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
		skillExecutor: func(ctx context.Context, userID int, call mtfAgentSkillCall) (map[string]interface{}, error) {
			calls = append(calls, call)
			return map[string]interface{}{"status": "ok"}, nil
		},
	}
	aiConfig := &models.AIModelConfig{BaseURL: "https://api.deepseek.com", APIKey: "user-api-key", ModelID: "deepseek-v4-pro"}

	_, assistantText, err := service.sendMTFAgentTurnWithStandardTools(context.Background(), 7, "thr_123", "帮我估一下 688017", aiConfig)
	if err != nil {
		t.Fatalf("sendMTFAgentTurnWithStandardTools error = %v", err)
	}
	if assistantText != "688017 估值需要实时数据源。" {
		t.Fatalf("assistantText = %q", assistantText)
	}
	if len(calls) != 1 || calls[0].Skill != "a_stock_data" {
		t.Fatalf("skill calls = %#v, want a_stock_data", calls)
	}
	if len(requests) != 2 {
		t.Fatalf("runtime requests = %#v, want exactly two POST turns without polling", requests)
	}
}

func TestExecuteAStockDataSkillDoesNotRequireDatabase(t *testing.T) {
	service := NewMTFAgentService(nil, &config.Config{})

	result, err := service.ExecuteMTFAgentSkill(context.Background(), 7, "a_stock_data", map[string]interface{}{
		"intent":   "research_reports",
		"symbol":   "688017",
		"question": "帮我估一下 688017",
	})
	if err != nil {
		t.Fatalf("ExecuteMTFAgentSkill a_stock_data error = %v", err)
	}
	if result["skill"] != "a_stock_data" {
		t.Fatalf("skill = %v, want a_stock_data", result["skill"])
	}
	if result["status"] != "unsupported_intent" {
		t.Fatalf("status = %v, want unsupported_intent", result["status"])
	}
	fields, ok := result["required_fields"].([]string)
	if !ok || !containsString(fields, "source") {
		t.Fatalf("required_fields = %#v, want source", result["required_fields"])
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func runtimeToolsContain(tools []interface{}, name string) bool {
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		fn, ok := toolMap["function"].(map[string]interface{})
		if ok && fn["name"] == name {
			return true
		}
	}
	return false
}

func runtimeMessagesContainRole(messages []interface{}, role string) bool {
	for _, message := range messages {
		messageMap, ok := message.(map[string]interface{})
		if ok && messageMap["role"] == role {
			return true
		}
	}
	return false
}

func runtimeAssistantMessageContains(messages []interface{}, key string, value string) bool {
	for _, message := range messages {
		messageMap, ok := message.(map[string]interface{})
		if !ok || messageMap["role"] != "assistant" {
			continue
		}
		if messageMap[key] == value {
			return true
		}
	}
	return false
}

func TestMTFAgentTurnPollsCompletedThreadItems(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/deepseek-tui/v1/threads/thr_123/turns":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turn": map[string]string{"id": "turn_123", "status": "in_progress"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/deepseek-tui/v1/threads/thr_123":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turns": []map[string]interface{}{
					{"id": "turn_123", "status": "completed"},
				},
				"items": []map[string]interface{}{
					{"turn_id": "turn_123", "kind": "assistant_message", "status": "completed", "detail": "完成回答"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
	})
	aiConfig := &models.AIModelConfig{BaseURL: "https://api.deepseek.com", APIKey: "user-api-key", ModelID: "deepseek-v4-pro"}

	assistantText, err := service.sendDeepSeekTurn(context.Background(), "thr_123", "hello", aiConfig)
	if err != nil {
		t.Fatalf("sendDeepSeekTurn error = %v", err)
	}
	if assistantText != "完成回答" {
		t.Fatalf("assistantText = %q, want 完成回答", assistantText)
	}
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2: %#v", len(requests), requests)
	}
}

func TestExtractAssistantTextReturnsEmptyWhenRuntimeHasNoAssistantContent(t *testing.T) {
	text := extractAssistantText(map[string]interface{}{
		"turns": []map[string]interface{}{
			{"id": "turn_123", "status": "completed"},
		},
	})
	if text != "" {
		t.Fatalf("extractAssistantText = %q, want empty text", text)
	}
	if strings.Contains(mtfAgentNoContentText, "已提交") || !strings.Contains(mtfAgentNoContentText, "没有后台任务") {
		t.Fatalf("no-content fallback should not imply queued background work: %q", mtfAgentNoContentText)
	}
}

func TestMTFAgentTurnReturnsRuntimeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/deepseek-tui/v1/threads/thr_123/turns":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turn": map[string]string{"id": "turn_123", "status": "in_progress"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/deepseek-tui/v1/threads/thr_123":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"turns": []map[string]interface{}{
					{"id": "turn_123", "status": "failed", "error": "DeepSeek API key not found"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
	})
	aiConfig := &models.AIModelConfig{BaseURL: "https://api.deepseek.com", APIKey: "user-api-key", ModelID: "deepseek-v4-pro"}

	_, err := service.sendDeepSeekTurn(context.Background(), "thr_123", "hello", aiConfig)
	if err == nil || !strings.Contains(err.Error(), "DeepSeek API key not found") {
		t.Fatalf("sendDeepSeekTurn error = %v, want DeepSeek API key not found", err)
	}
}

func TestMTFAgentTurnRecoversMissingRuntimeThreadOnce(t *testing.T) {
	requests := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/deepseek-tui/v1/threads/thr_missing/turns":
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "Thread not found: thr_missing",
					"status":  http.StatusNotFound,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/deepseek-tui/v1/threads":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "thr_recovered"})
		case r.Method == http.MethodPost && r.URL.Path == "/deepseek-tui/v1/threads/thr_recovered/turns":
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "恢复后的回答"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL + "/deepseek-tui",
		},
	})
	aiConfig := &models.AIModelConfig{BaseURL: "https://api.deepseek.com", APIKey: "user-api-key", ModelID: "deepseek-v4-pro"}

	threadID, assistantText, err := service.sendDeepSeekTurnWithRecovery(context.Background(), "thr_missing", "hello", aiConfig)
	if err != nil {
		t.Fatalf("sendDeepSeekTurnWithRecovery error = %v", err)
	}
	if threadID != "thr_recovered" {
		t.Fatalf("threadID = %q, want thr_recovered", threadID)
	}
	if assistantText != "恢复后的回答" {
		t.Fatalf("assistantText = %q, want 恢复后的回答", assistantText)
	}
	wantRequests := []string{
		"POST /deepseek-tui/v1/threads/thr_missing/turns",
		"POST /deepseek-tui/v1/threads",
		"POST /deepseek-tui/v1/threads/thr_recovered/turns",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
}

func TestMTFAgentMessageRequiresUserAIModelConfig(t *testing.T) {
	service := NewMTFAgentService(nil, &config.Config{
		MTFAgent: config.MTFAgentConfig{
			Enabled: true,
			BaseURL: "http://runtime.local/deepseek-tui",
		},
	})

	_, err := service.SendMessage(context.Background(), 1, "hello", nil)
	if err == nil || err.Error() != AIModelConfigRequiredMsg {
		t.Fatalf("SendMessage error = %v, want %q", err, AIModelConfigRequiredMsg)
	}

	_, err = service.Reset(context.Background(), 1, &models.AIModelConfig{ModelID: "deepseek-v4-pro"})
	if err == nil || err.Error() != AIModelConfigRequiredMsg {
		t.Fatalf("Reset error = %v, want %q", err, AIModelConfigRequiredMsg)
	}
}

func TestMTFAgentRuntimePayloadIncludesUserAIModelConfig(t *testing.T) {
	var threadPayload map[string]interface{}
	var turnPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/threads":
			if err := json.NewDecoder(r.Body).Decode(&threadPayload); err != nil {
				t.Fatalf("decode thread payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "thr_123"})
		case "/v1/threads/thr_123/turns":
			if err := json.NewDecoder(r.Body).Decode(&turnPayload); err != nil {
				t.Fatalf("decode turn payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "ok"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	service := &MTFAgentService{
		config: &config.MTFAgentConfig{
			Enabled: true,
			BaseURL: server.URL,
		},
		client: server.Client(),
	}
	aiConfig := &models.AIModelConfig{
		ProviderName: "DeepSeek",
		BaseURL:      "https://api.deepseek.com/",
		APIKey:       "sk-user-key",
		ModelID:      "deepseek-chat",
	}

	threadID, err := service.createDeepSeekThread(context.Background(), aiConfig)
	if err != nil {
		t.Fatalf("createDeepSeekThread error: %v", err)
	}
	if threadID != "thr_123" {
		t.Fatalf("threadID = %q, want thr_123", threadID)
	}
	if _, err := service.sendDeepSeekTurn(context.Background(), threadID, "hello", aiConfig); err != nil {
		t.Fatalf("sendDeepSeekTurn error: %v", err)
	}

	assertRuntimeAIModelPayload(t, threadPayload, "deepseek-chat")
	assertRuntimeAIModelPayload(t, turnPayload, "deepseek-chat")
	if got := turnPayload["prompt"]; got != "hello" {
		t.Fatalf("turn prompt = %v, want hello", got)
	}
	if _, ok := turnPayload["message"]; ok {
		t.Fatalf("turn payload should use prompt, got message field")
	}
}

func assertRuntimeAIModelPayload(t *testing.T, payload map[string]interface{}, wantModel string) {
	t.Helper()
	aiModel, ok := payload["ai_model"].(map[string]interface{})
	if !ok {
		t.Fatalf("payload missing ai_model: %#v", payload)
	}
	if got := aiModel["api_key"]; got != "sk-user-key" {
		t.Fatalf("ai_model.api_key = %v, want user key", got)
	}
	if got := aiModel["base_url"]; got != "https://api.deepseek.com" {
		t.Fatalf("ai_model.base_url = %v, want normalized base URL", got)
	}
	if got := aiModel["model_id"]; got != wantModel {
		t.Fatalf("ai_model.model_id = %v, want %s", got, wantModel)
	}
}

func TestBuildMTFAgentPromptIncludesContextAndMemory(t *testing.T) {
	prompt := BuildMTFAgentPrompt(
		"帮我分析自选股",
		"用户长期偏好：\n- 回答先给结论",
		models.MTFAgentContextSummary{
			Watchlist:  "自选股：601766.SH 中国中车",
			UZIReports: "最近研报：601766.SH 标准研报",
		},
	)

	for _, want := range []string{"帮我分析自选股", "用户长期偏好", "自选股：601766.SH", "最近研报", "MTF 当前上下文"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "FinTrack") {
		t.Fatalf("prompt should not mention FinTrack: %s", prompt)
	}
}

func TestBuildMTFAgentPromptIncludesSkillInstructions(t *testing.T) {
	prompt := BuildMTFAgentPrompt(
		"查看 601766 的历史走势和研报",
		"",
		models.MTFAgentContextSummary{},
	)

	for _, want := range []string{"固定 A 股数据 Skill", "个股估值", "北向资金", "MTF 内置 skill", "history_trends", "uzi_reports", "```mtf-skill"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing skill instruction %q: %s", want, prompt)
		}
	}
}

func TestExtractMTFAgentSkillCallFromFencedJSON(t *testing.T) {
	text := "```mtf-skill\n{\"skill\":\"history_trends\",\"arguments\":{\"symbol\":\"601766.SH\",\"limit\":2}}\n```"

	call, ok, err := extractMTFAgentSkillCall(text)
	if err != nil {
		t.Fatalf("extractMTFAgentSkillCall error = %v", err)
	}
	if !ok {
		t.Fatal("extractMTFAgentSkillCall ok = false, want true")
	}
	if call.Skill != "history_trends" {
		t.Fatalf("skill = %q, want history_trends", call.Skill)
	}
	if got := call.Arguments["symbol"]; got != "601766.SH" {
		t.Fatalf("arguments.symbol = %v, want 601766.SH", got)
	}
}

func TestSendMTFAgentTurnWithSkillsCallsRuntimeTwice(t *testing.T) {
	requests := make([]map[string]interface{}, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		requests = append(requests, payload)
		switch len(requests) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message": "```mtf-skill\n{\"skill\":\"uzi_reports\",\"arguments\":{\"ticker\":\"601766.SH\"}}\n```",
			})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "601766.SH 最近有 1 篇标准研报。"})
		default:
			t.Fatalf("unexpected runtime request count %d", len(requests))
		}
	}))
	defer server.Close()

	service := &MTFAgentService{
		config: &config.MTFAgentConfig{Enabled: true, BaseURL: server.URL},
		client: server.Client(),
		skillExecutor: func(ctx context.Context, userID int, call mtfAgentSkillCall) (map[string]interface{}, error) {
			if userID != 7 {
				t.Fatalf("userID = %d, want 7", userID)
			}
			if call.Skill != "uzi_reports" {
				t.Fatalf("skill = %q, want uzi_reports", call.Skill)
			}
			return map[string]interface{}{
				"skill": "uzi_reports",
				"count": 1,
				"items": []map[string]interface{}{{"ticker": "601766.SH", "depth": "standard"}},
			}, nil
		},
	}
	aiConfig := &models.AIModelConfig{BaseURL: "https://api.deepseek.com", APIKey: "user-api-key", ModelID: "deepseek-v4-pro"}

	threadID, assistantText, err := service.sendMTFAgentTurnWithSkills(context.Background(), 7, "thr_123", "原始提示", aiConfig)
	if err != nil {
		t.Fatalf("sendMTFAgentTurnWithSkills error = %v", err)
	}
	if threadID != "thr_123" {
		t.Fatalf("threadID = %q, want thr_123", threadID)
	}
	if assistantText != "601766.SH 最近有 1 篇标准研报。" {
		t.Fatalf("assistantText = %q", assistantText)
	}
	if len(requests) != 2 {
		t.Fatalf("runtime request count = %d, want 2", len(requests))
	}
	secondPrompt, _ := requests[1]["prompt"].(string)
	for _, want := range []string{"内部 skill 返回结果", "uzi_reports", "601766.SH"} {
		if !strings.Contains(secondPrompt, want) {
			t.Fatalf("second prompt missing %q: %s", want, secondPrompt)
		}
	}
}

func TestBuildMTFAgentAutoSkillContextPrefetchesHistoryAndReports(t *testing.T) {
	calls := make([]mtfAgentSkillCall, 0)
	service := &MTFAgentService{
		skillExecutor: func(ctx context.Context, userID int, call mtfAgentSkillCall) (map[string]interface{}, error) {
			if userID != 7 {
				t.Fatalf("userID = %d, want 7", userID)
			}
			calls = append(calls, call)
			return map[string]interface{}{
				"skill": call.Skill,
				"query": call.Arguments,
				"count": 1,
			}, nil
		},
	}

	contextText := service.BuildMTFAgentAutoSkillContext(context.Background(), 7, "查看 601766 的历史走势和研报")

	if len(calls) != 2 {
		t.Fatalf("skill call count = %d, want 2: %#v", len(calls), calls)
	}
	if calls[0].Skill != "history_trends" || calls[1].Skill != "uzi_reports" {
		t.Fatalf("skill calls = %#v, want history_trends then uzi_reports", calls)
	}
	for _, call := range calls {
		if got := call.Arguments["symbol"]; call.Skill == "history_trends" && got != "601766" {
			t.Fatalf("history symbol = %v, want 601766", got)
		}
		if got := call.Arguments["ticker"]; call.Skill == "uzi_reports" && got != "601766" {
			t.Fatalf("reports ticker = %v, want 601766", got)
		}
	}
	for _, want := range []string{"已自动预取", "history_trends", "uzi_reports"} {
		if !strings.Contains(contextText, want) {
			t.Fatalf("auto skill context missing %q: %s", want, contextText)
		}
	}
}

func TestMTFAgentMessageJobRunsInBackground(t *testing.T) {
	service := NewMTFAgentService(nil, &config.Config{})
	service.messageJobRunner = func(ctx context.Context, userID int, message string, aiConfig *models.AIModelConfig) (*models.MTFAgentMessageResponse, error) {
		if userID != 7 {
			t.Fatalf("userID = %d, want 7", userID)
		}
		if message != "查看 601766 的历史走势和研报" {
			t.Fatalf("message = %q", message)
		}
		return &models.MTFAgentMessageResponse{
			ThreadID: "thr_job",
			Message:  models.Message{Role: "assistant", Content: "DeepSeek 生成结果"},
			Model:    "deepseek-v4-pro",
		}, nil
	}

	job, err := service.StartMessageJob(context.Background(), 7, "查看 601766 的历史走势和研报", &models.AIModelConfig{APIKey: "key", BaseURL: "https://api.deepseek.com", ModelID: "deepseek-v4-pro"})
	if err != nil {
		t.Fatalf("StartMessageJob error = %v", err)
	}
	if job.JobID == "" {
		t.Fatal("job id is empty")
	}

	var status *models.MTFAgentMessageJobStatusResponse
	for i := 0; i < 20; i++ {
		status, err = service.GetMessageJob(7, job.JobID)
		if err != nil {
			t.Fatalf("GetMessageJob error = %v", err)
		}
		if status.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status == nil || status.Status != "completed" {
		t.Fatalf("job status = %#v, want completed", status)
	}
	if status.Response == nil || status.Response.Message.Content != "DeepSeek 生成结果" {
		t.Fatalf("job response = %#v", status.Response)
	}
}

func TestTruncateForPrompt(t *testing.T) {
	input := strings.Repeat("甲", 100)
	got := truncateForPrompt(input, 10)
	if len([]rune(got)) > 20 {
		t.Fatalf("truncateForPrompt too long: %q", got)
	}
	if !strings.Contains(got, "截断") {
		t.Fatalf("truncateForPrompt should mark truncation: %q", got)
	}
}
