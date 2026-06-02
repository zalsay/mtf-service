package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"fintrack-api/models"
)

type MTFAgentStreamEvent struct {
	Type     string
	Text     string
	Response *models.MTFAgentMessageResponse
	Error    string
}

func (s *MTFAgentService) StreamMessage(ctx context.Context, userID int, message string, aiConfig *models.AIModelConfig) (<-chan MTFAgentStreamEvent, error) {
	if !IsAIModelConfigReady(aiConfig) {
		return nil, errors.New(AIModelConfigRequiredMsg)
	}
	cleanMessage := strings.TrimSpace(message)
	if cleanMessage == "" {
		return nil, errors.New("message is required")
	}

	session, err := s.getSession(userID)
	if err != nil {
		return nil, err
	}
	modelID := strings.TrimSpace(aiConfig.ModelID)
	threadID := ""
	if session != nil {
		threadID = strings.TrimSpace(session.DeepSeekThreadID)
	}
	if threadID == "" {
		threadID, err = s.createDeepSeekThread(ctx, aiConfig)
		if err != nil {
			return nil, err
		}
		if err := s.upsertSession(userID, threadID, modelID); err != nil {
			return nil, err
		}
	}

	memories, err := s.ListMemories(userID)
	if err != nil {
		return nil, err
	}
	prompt := BuildMTFAgentPrompt(cleanMessage, RenderMTFAgentMemoryBlock(memories), s.BuildContextSummary(userID))
	if recent := s.recentMessagePromptBlock(userID); recent != "" {
		prompt += "\n\n最近对话：\n" + recent
	}
	if autoSkillContext := s.BuildMTFAgentAutoSkillContext(ctx, userID, cleanMessage); autoSkillContext != "" {
		prompt += "\n\n" + autoSkillContext
	}

	events := make(chan MTFAgentStreamEvent, 16)
	go func() {
		defer close(events)
		nextThreadID, assistantText, err := s.sendMTFAgentTurnWithStandardTools(ctx, userID, threadID, prompt, aiConfig)
		if err != nil {
			events <- MTFAgentStreamEvent{Type: "error", Error: err.Error()}
			return
		}
		assistantText = strings.TrimSpace(assistantText)
		if assistantText == "" {
			assistantText = mtfAgentNoContentText
		}
		events <- MTFAgentStreamEvent{Type: "delta", Text: assistantText}
		if err := s.upsertSession(userID, nextThreadID, modelID); err != nil {
			events <- MTFAgentStreamEvent{Type: "error", Error: err.Error()}
			return
		}
		if err := s.saveMessage(userID, nextThreadID, "user", cleanMessage); err != nil {
			events <- MTFAgentStreamEvent{Type: "error", Error: err.Error()}
			return
		}
		if err := s.saveMessage(userID, nextThreadID, "assistant", assistantText); err != nil {
			events <- MTFAgentStreamEvent{Type: "error", Error: err.Error()}
			return
		}
		events <- MTFAgentStreamEvent{
			Type: "done",
			Response: &models.MTFAgentMessageResponse{
				ThreadID: nextThreadID,
				Message:  models.Message{Role: "assistant", Content: assistantText},
				Model:    modelID,
			},
		}
	}()
	return events, nil
}

func (s *MTFAgentService) streamDeepSeekPrompt(ctx context.Context, prompt string, aiConfig *models.AIModelConfig, onDelta func(string)) error {
	if !s.isConfigured() {
		return errors.New("MTF Agent runtime is not configured")
	}
	payload := map[string]interface{}{"prompt": prompt}
	if aiModel := mtfRuntimeAIModelPayload(aiConfig); aiModel != nil {
		payload["ai_model"] = aiModel
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.config.BaseURL, "/")+"/v1/stream", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if token := strings.TrimSpace(s.config.RuntimeToken); token != "" {
		req.Header.Set("X-Gateway-API-Token", token)
	}
	if IsAIModelConfigReady(aiConfig) {
		req.Header.Set("X-DeepSeek-API-Key", strings.TrimSpace(aiConfig.APIKey))
		if baseURL := strings.TrimSpace(aiConfig.BaseURL); baseURL != "" {
			req.Header.Set("X-DeepSeek-Base-URL", strings.TrimRight(baseURL, "/"))
		}
	}

	streamClient := *s.client
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return &deepSeekRuntimeHTTPError{statusCode: resp.StatusCode, body: string(rawBody)}
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "text/event-stream") {
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		return fmt.Errorf("deepseek runtime stream returned non-SSE response: %s", string(rawBody))
	}
	return readMTFAgentRuntimeSSE(resp.Body, onDelta)
}

func readMTFAgentRuntimeSSE(reader io.Reader, onDelta func(string)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	eventName := "message"
	dataLines := make([]string, 0)
	flush := func() error {
		if len(dataLines) == 0 {
			eventName = "message"
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		currentEvent := eventName
		eventName = "message"
		return handleMTFAgentRuntimeSSEBlock(currentEvent, data, onDelta)
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func handleMTFAgentRuntimeSSEBlock(eventName string, data string, onDelta func(string)) error {
	if eventName == "done" || strings.TrimSpace(data) == "[DONE]" {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return nil
	}
	if eventName == "error" {
		return errors.New(firstNonEmptyString(stringField(payload, "error"), stringField(payload, "message"), "deepseek runtime stream failed"))
	}
	if text := stringField(payload, "text"); text != "" {
		onDelta(text)
		return nil
	}
	if text := stringField(payload, "delta"); text != "" {
		onDelta(text)
		return nil
	}
	return nil
}

func (s *MTFAgentService) recentMessagePromptBlock(userID int) string {
	response, err := s.ListMessages(userID)
	if err != nil || response == nil || len(response.Messages) == 0 {
		return ""
	}
	start := len(response.Messages) - 8
	if start < 0 {
		start = 0
	}
	lines := make([]string, 0, len(response.Messages)-start)
	for _, message := range response.Messages[start:] {
		content := truncateForPrompt(message.Content, 800)
		if strings.TrimSpace(content) == "" {
			continue
		}
		role := "用户"
		if message.Role == "assistant" {
			role = "助手"
		}
		lines = append(lines, role+"："+content)
	}
	return strings.Join(lines, "\n")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func deepSeekStreamURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/stream"
}

func escapeRuntimeThreadID(threadID string) string {
	return url.PathEscape(threadID)
}
