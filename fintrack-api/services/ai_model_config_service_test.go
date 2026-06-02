package services

import (
	"testing"

	"fintrack-api/models"
)

func TestBuildAIModelConfigResponseMasksAPIKey(t *testing.T) {
	config := &models.AIModelConfig{
		ID:      7,
		UserID:  12,
		BaseURL: "https://api.deepseek.com",
		APIKey:  "sk-1234567890abcdef",
		ModelID: "deepseek-v4-pro",
	}

	response := BuildAIModelConfigResponse(config)

	if response.APIKey != "" {
		t.Fatal("expected raw api key to be omitted from response")
	}
	if !response.HasAPIKey {
		t.Fatal("expected has_api_key to be true")
	}
	if response.APIKeyMasked != "sk-1********cdef" {
		t.Fatalf("unexpected masked key: %q", response.APIKeyMasked)
	}
	if !response.IsRecommended {
		t.Fatal("expected default DeepSeek v4 pro config to be marked recommended")
	}
}

func TestNormalizeAIModelConfigUpdatePreservesExistingAPIKey(t *testing.T) {
	req := models.AIModelConfigRequest{
		BaseURL: " https://api.deepseek.com ",
		APIKey:  "   ",
		ModelID: " deepseek-v4-pro ",
	}

	normalized, err := NormalizeAIModelConfigUpdate(req, "sk-existing")
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if normalized.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("unexpected base_url: %q", normalized.BaseURL)
	}
	if normalized.APIKey != "sk-existing" {
		t.Fatalf("expected existing api key to be preserved, got %q", normalized.APIKey)
	}
	if normalized.ModelID != "deepseek-v4-pro" {
		t.Fatalf("unexpected model_id: %q", normalized.ModelID)
	}
}

func TestIsAIModelConfigReady(t *testing.T) {
	if IsAIModelConfigReady(nil) {
		t.Fatal("expected nil config to be not ready")
	}

	if IsAIModelConfigReady(&models.AIModelConfig{
		BaseURL: "https://api.deepseek.com",
		APIKey:  "",
		ModelID: "deepseek-v4-pro",
	}) {
		t.Fatal("expected config without api key to be not ready")
	}

	if !IsAIModelConfigReady(&models.AIModelConfig{
		BaseURL: "https://api.deepseek.com",
		APIKey:  "sk-test",
		ModelID: "deepseek-v4-pro",
	}) {
		t.Fatal("expected complete config to be ready")
	}
}

func TestNormalizeAIModelConfigUpdateRejectsMissingRequiredFields(t *testing.T) {
	_, err := NormalizeAIModelConfigUpdate(models.AIModelConfigRequest{
		BaseURL: "",
		APIKey:  "",
		ModelID: "deepseek-v4-pro",
	}, "")

	if err == nil {
		t.Fatal("expected missing base_url/api_key to be rejected")
	}
}
