package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/models"
)

type MTFAgentService struct {
	db                 *database.DB
	config             *config.MTFAgentConfig
	client             *http.Client
	aStockDataProvider *aStockDataProvider
	skillExecutor      mtfAgentSkillExecutor
	messageJobRunner   mtfAgentMessageJobRunner
	jobs               map[string]*mtfAgentMessageJob
	jobsMu             sync.RWMutex
}

type mtfAgentMessageJobRunner func(context.Context, int, string, *models.AIModelConfig) (*models.MTFAgentMessageResponse, error)

type mtfAgentMessageJob struct {
	ID        string
	UserID    int
	Status    string
	Response  *models.MTFAgentMessageResponse
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type deepSeekRuntimeHTTPError struct {
	statusCode int
	body       string
}

func (e *deepSeekRuntimeHTTPError) Error() string {
	return fmt.Sprintf("deepseek runtime returned %d: %s", e.statusCode, e.body)
}

const (
	mtfAgentTurnPollAttempts = 120
	mtfAgentTurnPollInterval = 1 * time.Second
	mtfAgentNoContentText    = "MTF 智能体运行时没有返回有效内容；当前请求没有后台任务在继续处理，请稍后重试或检查 DeepSeek TUI 服务日志。"
	mtfAgentHistoryLimit     = 100
)

func NewMTFAgentService(db *database.DB, cfg *config.Config) *MTFAgentService {
	mtfConfig := &config.MTFAgentConfig{}
	if cfg != nil {
		mtfConfig = &cfg.MTFAgent
	}
	timeout := 120 * time.Second
	if mtfConfig.Timeout > 0 {
		timeout = time.Duration(mtfConfig.Timeout) * time.Second
	}
	return &MTFAgentService{
		db:     db,
		config: mtfConfig,
		client: &http.Client{Timeout: timeout},
		jobs:   map[string]*mtfAgentMessageJob{},
	}
}

func (s *MTFAgentService) isConfigured() bool {
	return s != nil && s.config != nil && s.config.Enabled && strings.TrimSpace(s.config.BaseURL) != ""
}

func (s *MTFAgentService) defaultModelID() string {
	if s != nil && s.config != nil && strings.TrimSpace(s.config.DefaultModel) != "" {
		return strings.TrimSpace(s.config.DefaultModel)
	}
	return RecommendedAIModelID
}

func RenderMTFAgentMemoryBlock(memories []models.MTFAgentMemory) string {
	if len(memories) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("用户长期偏好：\n")
	for _, memory := range memories {
		content := strings.TrimSpace(memory.Content)
		if content == "" {
			continue
		}
		builder.WriteString("- ")
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func BuildMTFAgentPrompt(userMessage string, memoryBlock string, ctx models.MTFAgentContextSummary) string {
	var builder strings.Builder
	builder.WriteString("你是 MTF 智能体。你提供投资研究辅助，不承诺收益，不替代用户决策。\n")
	builder.WriteString("回答要求：先给结论，再给关键依据和风险。使用简体中文。\n\n")
	builder.WriteString("固定 A 股数据 Skill：当用户询问个股估值、题材归因、研报检索、北向资金、概念板块、资金流向、龙虎榜、限售解禁、行业轮动、融资融券、大宗交易、股东户数、分红送转、新闻公告或批量对比时，按 A 股研究数据助理方式回答。\n")
	builder.WriteString("若当前上下文没有足够数据，先说明需要实时 A 股数据源支持，再给出可执行的查询口径、关键字段和后续分析框架；不要编造具体实时数值。\n\n")
	builder.WriteString("MTF 内置 skill 调用规则：当用户需要查看历史走势、历史预测趋势或研报详情，且当前上下文不足时，只输出一个 fenced JSON 调用，不要附加解释。\n")
	builder.WriteString("可用 skill：\n")
	builder.WriteString("- history_trends：查看历史走势和历史预测趋势，arguments 可包含 symbol、unique_key、prediction_type、horizon_len、limit、chunk_limit、point_limit。\n")
	builder.WriteString("- uzi_reports：查看 UZI 研报，arguments 可包含 ticker、limit。\n")
	builder.WriteString("调用格式：\n```mtf-skill\n{\"skill\":\"history_trends\",\"arguments\":{\"symbol\":\"601766.SH\"}}\n```\n\n")
	if strings.TrimSpace(memoryBlock) != "" {
		builder.WriteString(memoryBlock)
		builder.WriteString("\n\n")
	}
	builder.WriteString("MTF 当前上下文：\n")
	writeContextLine(&builder, "自选股", ctx.Watchlist)
	writeContextLine(&builder, "预测摘要", ctx.Prediction)
	writeContextLine(&builder, "UZI研报", ctx.UZIReports)
	builder.WriteString("\n用户问题：\n")
	builder.WriteString(strings.TrimSpace(userMessage))
	return builder.String()
}

func writeContextLine(builder *strings.Builder, label string, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		trimmed = "暂无"
	}
	builder.WriteString("- ")
	builder.WriteString(label)
	builder.WriteString("：")
	builder.WriteString(trimmed)
	builder.WriteString("\n")
}

func parseDeepSeekThreadID(body map[string]interface{}) string {
	for _, key := range []string{"id", "thread_id"} {
		if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if thread, ok := body["thread"].(map[string]interface{}); ok {
		if value, ok := thread["id"].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mtfRuntimeAIModelPayload(aiConfig *models.AIModelConfig) map[string]interface{} {
	if aiConfig == nil {
		return nil
	}
	return map[string]interface{}{
		"provider_name": strings.TrimSpace(aiConfig.ProviderName),
		"base_url":      strings.TrimRight(strings.TrimSpace(aiConfig.BaseURL), "/"),
		"api_key":       strings.TrimSpace(aiConfig.APIKey),
		"model_id":      strings.TrimSpace(aiConfig.ModelID),
	}
}

func (s *MTFAgentService) createDeepSeekThread(ctx context.Context, aiConfig *models.AIModelConfig) (string, error) {
	modelID := strings.TrimSpace(aiConfig.ModelID)
	aiModel := mtfRuntimeAIModelPayload(aiConfig)
	payload := map[string]interface{}{
		"model":         modelID,
		"mode":          "agent",
		"title":         "MTF Agent",
		"system_prompt": "你是 MTF 智能体，专注投资研究辅助。",
	}
	if aiModel != nil {
		payload["ai_model"] = aiModel
	}
	body, err := s.doRuntimeJSON(ctx, http.MethodPost, "/v1/threads", payload, aiConfig)
	if err != nil {
		return "", err
	}
	threadID := parseDeepSeekThreadID(body)
	if threadID == "" {
		return "", fmt.Errorf("deepseek runtime did not return thread id")
	}
	return threadID, nil
}

func (s *MTFAgentService) sendDeepSeekTurn(ctx context.Context, threadID string, prompt string, aiConfig *models.AIModelConfig) (string, error) {
	payload := map[string]interface{}{"prompt": prompt}
	if aiModel := mtfRuntimeAIModelPayload(aiConfig); aiModel != nil {
		payload["ai_model"] = aiModel
	}
	body, err := s.doRuntimeJSON(ctx, http.MethodPost, "/v1/threads/"+url.PathEscape(threadID)+"/turns", payload, aiConfig)
	if err != nil {
		return "", err
	}
	turnID := parseDeepSeekTurnID(body)
	if turnID == "" {
		return extractAssistantText(body), nil
	}
	return s.waitDeepSeekTurn(ctx, threadID, turnID, aiConfig)
}

func (s *MTFAgentService) sendDeepSeekTurnWithRecovery(ctx context.Context, threadID string, prompt string, aiConfig *models.AIModelConfig) (string, string, error) {
	assistantText, err := s.sendDeepSeekTurn(ctx, threadID, prompt, aiConfig)
	if err == nil {
		return threadID, assistantText, nil
	}
	if !isDeepSeekThreadNotFoundError(err) {
		return threadID, "", err
	}
	newThreadID, err := s.createDeepSeekThread(ctx, aiConfig)
	if err != nil {
		return "", "", err
	}
	assistantText, err = s.sendDeepSeekTurn(ctx, newThreadID, prompt, aiConfig)
	if err != nil {
		return newThreadID, "", err
	}
	return newThreadID, assistantText, nil
}

func isDeepSeekThreadNotFoundError(err error) bool {
	var runtimeErr *deepSeekRuntimeHTTPError
	if !errors.As(err, &runtimeErr) {
		return false
	}
	return runtimeErr.statusCode == http.StatusNotFound && strings.Contains(runtimeErr.body, "Thread not found")
}

func parseDeepSeekTurnID(body map[string]interface{}) string {
	if turn, ok := body["turn"].(map[string]interface{}); ok {
		if value, ok := turn["id"].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	for _, key := range []string{"id", "turn_id"} {
		if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *MTFAgentService) waitDeepSeekTurn(ctx context.Context, threadID string, turnID string, aiConfig *models.AIModelConfig) (string, error) {
	endpoint := "/v1/threads/" + url.PathEscape(threadID)
	for attempt := 0; attempt < mtfAgentTurnPollAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(mtfAgentTurnPollInterval):
			}
		}
		body, err := s.doRuntimeJSON(ctx, http.MethodGet, endpoint, nil, aiConfig)
		if err != nil {
			return "", err
		}
		status, runtimeErr := findDeepSeekTurnState(body, turnID)
		if strings.EqualFold(status, "failed") {
			if runtimeErr == "" {
				runtimeErr = "deepseek runtime turn failed"
			}
			return "", errors.New(runtimeErr)
		}
		if strings.EqualFold(status, "completed") {
			return extractAssistantText(body), nil
		}
	}
	return "", fmt.Errorf("deepseek runtime turn timed out")
}

func findDeepSeekTurnState(body map[string]interface{}, turnID string) (string, string) {
	if turn, ok := body["turn"].(map[string]interface{}); ok && stringField(turn, "id") == turnID {
		return stringField(turn, "status"), stringField(turn, "error")
	}
	if turns, ok := body["turns"].([]interface{}); ok {
		for _, item := range turns {
			turn, ok := item.(map[string]interface{})
			if ok && stringField(turn, "id") == turnID {
				return stringField(turn, "status"), stringField(turn, "error")
			}
		}
	}
	return "", ""
}

func (s *MTFAgentService) Session(ctx context.Context, userID int, aiConfig *models.AIModelConfig) (*models.MTFAgentSessionResponse, error) {
	memories, err := s.ListMemories(userID)
	if err != nil {
		return nil, err
	}
	session, err := s.getSession(userID)
	if err != nil {
		return nil, err
	}

	modelID := ""
	threadID := ""
	if session != nil {
		modelID = session.ModelID
		threadID = session.DeepSeekThreadID
	}
	if IsAIModelConfigReady(aiConfig) {
		modelID = strings.TrimSpace(aiConfig.ModelID)
	} else if strings.TrimSpace(modelID) == "" {
		modelID = s.defaultModelID()
	}

	_ = ctx
	return &models.MTFAgentSessionResponse{
		ThreadID:         threadID,
		ModelID:          modelID,
		RuntimeAvailable: s.isConfigured(),
		MemoryCount:      len(memories),
		HasAIModelConfig: IsAIModelConfigReady(aiConfig),
	}, nil
}

func (s *MTFAgentService) SendMessage(ctx context.Context, userID int, message string, aiConfig *models.AIModelConfig) (*models.MTFAgentMessageResponse, error) {
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
	if autoSkillContext := s.BuildMTFAgentAutoSkillContext(ctx, userID, cleanMessage); autoSkillContext != "" {
		prompt += "\n\n" + autoSkillContext
	}
	threadID, assistantText, err := s.sendMTFAgentTurnWithStandardTools(ctx, userID, threadID, prompt, aiConfig)
	if err != nil {
		return nil, err
	}
	assistantText = strings.TrimSpace(assistantText)
	if assistantText == "" {
		assistantText = mtfAgentNoContentText
	}
	if err := s.upsertSession(userID, threadID, modelID); err != nil {
		return nil, err
	}
	if err := s.saveMessage(userID, threadID, "user", cleanMessage); err != nil {
		return nil, err
	}
	if err := s.saveMessage(userID, threadID, "assistant", assistantText); err != nil {
		return nil, err
	}

	return &models.MTFAgentMessageResponse{
		ThreadID: threadID,
		Message: models.Message{
			Role:    "assistant",
			Content: assistantText,
		},
		Model: modelID,
	}, nil
}

func (s *MTFAgentService) Reset(ctx context.Context, userID int, aiConfig *models.AIModelConfig) (*models.MTFAgentResetResponse, error) {
	if !IsAIModelConfigReady(aiConfig) {
		return nil, errors.New(AIModelConfigRequiredMsg)
	}
	modelID := strings.TrimSpace(aiConfig.ModelID)
	threadID, err := s.createDeepSeekThread(ctx, aiConfig)
	if err != nil {
		return nil, err
	}
	if err := s.upsertSession(userID, threadID, modelID); err != nil {
		return nil, err
	}
	if err := s.ClearMessages(userID); err != nil {
		return nil, err
	}
	return &models.MTFAgentResetResponse{ThreadID: threadID}, nil
}

func (s *MTFAgentService) doRuntimeJSON(ctx context.Context, method string, endpoint string, payload interface{}, aiConfig *models.AIModelConfig) (map[string]interface{}, error) {
	if !s.isConfigured() {
		return nil, errors.New("MTF Agent runtime is not configured")
	}

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.config.BaseURL, "/")+endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(s.config.RuntimeToken); token != "" {
		req.Header.Set("X-Gateway-API-Token", token)
	}
	if IsAIModelConfigReady(aiConfig) {
		req.Header.Set("X-DeepSeek-API-Key", strings.TrimSpace(aiConfig.APIKey))
		if baseURL := strings.TrimSpace(aiConfig.BaseURL); baseURL != "" {
			req.Header.Set("X-DeepSeek-Base-URL", strings.TrimRight(baseURL, "/"))
		}
	}

	client := s.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, &deepSeekRuntimeHTTPError{statusCode: resp.StatusCode, body: string(raw)}
	}
	var decoded map[string]interface{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, fmt.Errorf("decode deepseek runtime response: %w", err)
		}
	} else {
		decoded = map[string]interface{}{}
	}
	return decoded, nil
}

func extractAssistantText(body map[string]interface{}) string {
	for _, key := range []string{"output", "message", "text"} {
		if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if items, ok := body["items"].([]interface{}); ok {
		for i := len(items) - 1; i >= 0; i-- {
			item, ok := items[i].(map[string]interface{})
			if !ok || !strings.Contains(stringField(item, "kind"), "assistant") {
				continue
			}
			for _, key := range []string{"detail", "content", "text", "summary"} {
				if value := stringField(item, key); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func stringField(values map[string]interface{}, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func (s *MTFAgentService) ListMemories(userID int) ([]models.MTFAgentMemory, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	rows, err := s.db.Conn.Query(`
		SELECT id, user_id, memory_type, content, source, confidence, created_at, updated_at
		FROM mtf_agent_memories
		WHERE user_id = $1
		ORDER BY updated_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query mtf memories: %w", err)
	}
	defer rows.Close()

	memories := make([]models.MTFAgentMemory, 0)
	for rows.Next() {
		var memory models.MTFAgentMemory
		if err := rows.Scan(
			&memory.ID,
			&memory.UserID,
			&memory.MemoryType,
			&memory.Content,
			&memory.Source,
			&memory.Confidence,
			&memory.CreatedAt,
			&memory.UpdatedAt,
		); err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	return memories, rows.Err()
}

func (s *MTFAgentService) ClearMemories(userID int) error {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return errors.New("database is not configured")
	}
	_, err := s.db.Conn.Exec(`DELETE FROM mtf_agent_memories WHERE user_id = $1`, userID)
	return err
}

func (s *MTFAgentService) ListMessages(userID int) (*models.MTFAgentMessagesResponse, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	session, err := s.getSession(userID)
	if err != nil {
		return nil, err
	}
	if session == nil || strings.TrimSpace(session.DeepSeekThreadID) == "" {
		return &models.MTFAgentMessagesResponse{Messages: []models.MTFAgentMessage{}}, nil
	}
	threadID := strings.TrimSpace(session.DeepSeekThreadID)
	rows, err := s.db.Conn.Query(`
		SELECT id, user_id, thread_id, role, content, created_at
		FROM (
			SELECT id, user_id, thread_id, role, content, created_at
			FROM mtf_agent_messages
			WHERE user_id = $1 AND thread_id = $2
			ORDER BY created_at DESC, id DESC
			LIMIT $3
		) recent
		ORDER BY created_at ASC, id ASC
	`, userID, threadID, mtfAgentHistoryLimit)
	if err != nil {
		return nil, fmt.Errorf("query mtf messages: %w", err)
	}
	defer rows.Close()

	messages := make([]models.MTFAgentMessage, 0)
	for rows.Next() {
		var message models.MTFAgentMessage
		if err := rows.Scan(
			&message.ID,
			&message.UserID,
			&message.ThreadID,
			&message.Role,
			&message.Content,
			&message.CreatedAt,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &models.MTFAgentMessagesResponse{ThreadID: threadID, Messages: messages}, nil
}

func (s *MTFAgentService) ClearMessages(userID int) error {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return errors.New("database is not configured")
	}
	_, err := s.db.Conn.Exec(`DELETE FROM mtf_agent_messages WHERE user_id = $1`, userID)
	return err
}

func (s *MTFAgentService) saveMessage(userID int, threadID string, role string, content string) error {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return errors.New("database is not configured")
	}
	trimmedThreadID := strings.TrimSpace(threadID)
	trimmedContent := strings.TrimSpace(content)
	if trimmedThreadID == "" || trimmedContent == "" {
		return nil
	}
	_, err := s.db.Conn.Exec(`
		INSERT INTO mtf_agent_messages (user_id, thread_id, role, content)
		VALUES ($1, $2, $3, $4)
	`, userID, trimmedThreadID, role, trimmedContent)
	return err
}

func (s *MTFAgentService) upsertSession(userID int, threadID string, modelID string) error {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return errors.New("database is not configured")
	}
	_, err := s.db.Conn.Exec(`
		INSERT INTO mtf_agent_sessions (user_id, deepseek_thread_id, model_id, last_used_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id) DO UPDATE SET
			deepseek_thread_id = EXCLUDED.deepseek_thread_id,
			model_id = EXCLUDED.model_id,
			last_used_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
	`, userID, threadID, modelID)
	return err
}

func (s *MTFAgentService) getSession(userID int) (*models.MTFAgentSession, error) {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return nil, errors.New("database is not configured")
	}
	var session models.MTFAgentSession
	err := s.db.Conn.QueryRow(`
		SELECT id, user_id, deepseek_thread_id, model_id, last_used_at, created_at, updated_at
		FROM mtf_agent_sessions
		WHERE user_id = $1
	`, userID).Scan(
		&session.ID,
		&session.UserID,
		&session.DeepSeekThreadID,
		&session.ModelID,
		&session.LastUsedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *MTFAgentService) BuildContextSummary(userID int) models.MTFAgentContextSummary {
	return models.MTFAgentContextSummary{
		Watchlist:  s.watchlistSummary(userID),
		Prediction: s.predictionSummary(userID),
		UZIReports: s.uziReportsSummary(userID),
	}
}

func (s *MTFAgentService) watchlistSummary(userID int) string {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return ""
	}
	rows, err := s.db.Conn.Query(`
		SELECT uw.symbol, COALESCE(st.company_name, ''), COALESCE(uw.notes, '')
		FROM user_watchlist uw
		LEFT JOIN stocks st ON LOWER(st.symbol) = LOWER(uw.symbol)
		WHERE uw.user_id = $1
		ORDER BY uw.added_at DESC
		LIMIT 20
	`, userID)
	if err != nil {
		log.Printf("mtf_agent watchlist summary query failed: %v", err)
		return ""
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var symbol, name, notes string
		if err := rows.Scan(&symbol, &name, &notes); err != nil {
			log.Printf("mtf_agent watchlist summary scan failed: %v", err)
			return ""
		}
		item := strings.TrimSpace(symbol)
		if strings.TrimSpace(name) != "" {
			item += " " + strings.TrimSpace(name)
		}
		if strings.TrimSpace(notes) != "" {
			item += "（" + strings.TrimSpace(notes) + "）"
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("mtf_agent watchlist summary rows failed: %v", err)
		return ""
	}
	return truncateForPrompt(strings.Join(items, "；"), 1200)
}

func (s *MTFAgentService) predictionSummary(userID int) string {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return ""
	}
	rows, err := s.db.Conn.Query(`
		SELECT p.symbol, COALESCE(p.short_name, ''), p.best_prediction_item, p.prediction_type, p.updated_at
		FROM timesfm_best_predictions p
		WHERE EXISTS (
			SELECT 1 FROM user_watchlist uw
			WHERE uw.user_id = $1 AND LOWER(uw.symbol) = LOWER(p.symbol)
		)
		ORDER BY p.updated_at DESC
		LIMIT 10
	`, userID)
	if err != nil {
		log.Printf("mtf_agent prediction summary query failed: %v", err)
		return ""
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var symbol, shortName, bestItem, predictionType string
		var updatedAt time.Time
		if err := rows.Scan(&symbol, &shortName, &bestItem, &predictionType, &updatedAt); err != nil {
			log.Printf("mtf_agent prediction summary scan failed: %v", err)
			return ""
		}
		label := strings.TrimSpace(symbol)
		if strings.TrimSpace(shortName) != "" {
			label += " " + strings.TrimSpace(shortName)
		}
		items = append(items, fmt.Sprintf("%s：%s/%s，更新于%s", label, bestItem, predictionType, updatedAt.Format("2006-01-02")))
	}
	if err := rows.Err(); err != nil {
		log.Printf("mtf_agent prediction summary rows failed: %v", err)
		return ""
	}
	return truncateForPrompt(strings.Join(items, "；"), 1200)
}

func (s *MTFAgentService) uziReportsSummary(userID int) string {
	if s == nil || s.db == nil || s.db.Conn == nil {
		return ""
	}
	rows, err := s.db.Conn.Query(`
		SELECT ticker, COALESCE(depth, ''), report_url, updated_at
		FROM uzi_reports
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC
		LIMIT 10
	`, userID)
	if err != nil {
		log.Printf("mtf_agent uzi reports summary query failed: %v", err)
		return ""
	}
	defer rows.Close()

	items := make([]string, 0)
	for rows.Next() {
		var ticker, depth, reportURL string
		var updatedAt time.Time
		if err := rows.Scan(&ticker, &depth, &reportURL, &updatedAt); err != nil {
			log.Printf("mtf_agent uzi reports summary scan failed: %v", err)
			return ""
		}
		item := fmt.Sprintf("%s %s研报（%s）", strings.TrimSpace(ticker), strings.TrimSpace(depth), updatedAt.Format("2006-01-02"))
		if strings.TrimSpace(reportURL) != "" {
			item += " " + strings.TrimSpace(reportURL)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("mtf_agent uzi reports summary rows failed: %v", err)
		return ""
	}
	return truncateForPrompt(strings.Join(items, "；"), 1200)
}

func truncateForPrompt(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "...(已截断)"
}
