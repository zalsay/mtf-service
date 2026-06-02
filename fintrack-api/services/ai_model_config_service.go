package services

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"fintrack-api/database"
	"fintrack-api/models"
)

const (
	RecommendedAIProviderName = "DeepSeek"
	RecommendedAIBaseURL      = "https://api.deepseek.com"
	RecommendedAIModelID      = "deepseek-v4-pro"
	RecommendedAIDisplayName  = "DeepSeek v4 pro"
	AIModelConfigRequiredMsg  = "请先在设置中配置 AI 模型"
)

type AIModelConfigService struct {
	db *database.DB
}

func NewAIModelConfigService(db *database.DB) *AIModelConfigService {
	return &AIModelConfigService{db: db}
}

func (s *AIModelConfigService) GetByUserID(userID int) (*models.AIModelConfig, error) {
	var config models.AIModelConfig
	err := s.db.Conn.QueryRow(`
		SELECT id, user_id, provider_name, base_url, api_key, model_id, created_at, updated_at
		FROM user_ai_model_configs
		WHERE user_id = $1
	`, userID).Scan(
		&config.ID,
		&config.UserID,
		&config.ProviderName,
		&config.BaseURL,
		&config.APIKey,
		&config.ModelID,
		&config.CreatedAt,
		&config.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get ai model config: %w", err)
	}
	return &config, nil
}

func (s *AIModelConfigService) GetResponseByUserID(userID int) (*models.AIModelConfigResponse, error) {
	config, err := s.GetByUserID(userID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return DefaultAIModelConfigResponse(), nil
	}
	response := BuildAIModelConfigResponse(config)
	return &response, nil
}

func (s *AIModelConfigService) Upsert(userID int, req models.AIModelConfigRequest) (*models.AIModelConfigResponse, error) {
	existing, err := s.GetByUserID(userID)
	if err != nil {
		return nil, err
	}

	existingKey := ""
	if existing != nil {
		existingKey = existing.APIKey
	}

	normalized, err := NormalizeAIModelConfigUpdate(req, existingKey)
	if err != nil {
		return nil, err
	}

	var saved models.AIModelConfig
	err = s.db.Conn.QueryRow(`
		INSERT INTO user_ai_model_configs (user_id, provider_name, base_url, api_key, model_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			provider_name = EXCLUDED.provider_name,
			base_url = EXCLUDED.base_url,
			api_key = EXCLUDED.api_key,
			model_id = EXCLUDED.model_id,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, user_id, provider_name, base_url, api_key, model_id, created_at, updated_at
	`, userID, normalized.ProviderName, normalized.BaseURL, normalized.APIKey, normalized.ModelID).Scan(
		&saved.ID,
		&saved.UserID,
		&saved.ProviderName,
		&saved.BaseURL,
		&saved.APIKey,
		&saved.ModelID,
		&saved.CreatedAt,
		&saved.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to save ai model config: %w", err)
	}

	response := BuildAIModelConfigResponse(&saved)
	return &response, nil
}

func DefaultAIModelConfigResponse() *models.AIModelConfigResponse {
	return &models.AIModelConfigResponse{
		ProviderName:  RecommendedAIProviderName,
		DisplayName:   RecommendedAIDisplayName,
		BaseURL:       RecommendedAIBaseURL,
		ModelID:       RecommendedAIModelID,
		IsRecommended: true,
		HasAPIKey:     false,
	}
}

func BuildAIModelConfigResponse(config *models.AIModelConfig) models.AIModelConfigResponse {
	if config == nil {
		return *DefaultAIModelConfigResponse()
	}

	return models.AIModelConfigResponse{
		ID:            config.ID,
		ProviderName:  config.ProviderName,
		DisplayName:   getAIModelDisplayName(config.ProviderName, config.ModelID),
		BaseURL:       config.BaseURL,
		APIKeyMasked:  MaskAPIKey(config.APIKey),
		HasAPIKey:     strings.TrimSpace(config.APIKey) != "",
		ModelID:       config.ModelID,
		IsRecommended: isRecommendedAIModelConfig(config.BaseURL, config.ModelID),
		CreatedAt:     config.CreatedAt,
		UpdatedAt:     config.UpdatedAt,
	}
}

func IsAIModelConfigReady(config *models.AIModelConfig) bool {
	return config != nil &&
		strings.TrimSpace(config.BaseURL) != "" &&
		strings.TrimSpace(config.APIKey) != "" &&
		strings.TrimSpace(config.ModelID) != ""
}

func NormalizeAIModelConfigUpdate(req models.AIModelConfigRequest, existingAPIKey string) (*models.AIModelConfig, error) {
	baseURL := strings.TrimSpace(req.BaseURL)
	apiKey := strings.TrimSpace(req.APIKey)
	modelID := strings.TrimSpace(req.ModelID)

	if baseURL == "" {
		return nil, errors.New("base_url is required")
	}
	if _, err := parseHTTPURL(baseURL); err != nil {
		return nil, fmt.Errorf("base_url is invalid: %w", err)
	}
	if modelID == "" {
		return nil, errors.New("model_id is required")
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(existingAPIKey)
	}
	if apiKey == "" {
		return nil, errors.New("api_key is required")
	}

	return &models.AIModelConfig{
		ProviderName: RecommendedAIProviderName,
		BaseURL:      strings.TrimRight(baseURL, "/"),
		APIKey:       apiKey,
		ModelID:      modelID,
	}, nil
}

func MaskAPIKey(apiKey string) string {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return strings.Repeat("*", len(trimmed))
	}
	return trimmed[:4] + "********" + trimmed[len(trimmed)-4:]
}

func parseHTTPURL(rawURL string) (*url.URL, error) {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("scheme must be http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("host is required")
	}
	return parsed, nil
}

func getAIModelDisplayName(providerName string, modelID string) string {
	if isRecommendedAIModelConfig(RecommendedAIBaseURL, modelID) {
		return RecommendedAIDisplayName
	}
	if strings.TrimSpace(providerName) != "" {
		return strings.TrimSpace(providerName)
	}
	return modelID
}

func isRecommendedAIModelConfig(baseURL string, modelID string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(baseURL), "/"), RecommendedAIBaseURL) &&
		strings.EqualFold(strings.TrimSpace(modelID), RecommendedAIModelID)
}
