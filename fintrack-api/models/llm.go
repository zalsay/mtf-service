package models

import "time"

// Message represents a chat message with role and content
type Message struct {
	Role    string `json:"role" binding:"required"`    // "system", "user", or "assistant"
	Content string `json:"content" binding:"required"` // Message content
}

// ChatRequest represents a request to the LLM chat endpoint
type ChatRequest struct {
	Model            string    `json:"model" binding:"required"`          // Model name (e.g., "gpt-3.5-turbo", "gpt-4")
	Messages         []Message `json:"messages" binding:"required,min=1"` // Conversation messages
	MaxContextRounds int       `json:"max_context_rounds,omitempty"`      // Override default context rounds
	Temperature      *float32  `json:"temperature,omitempty"`             // Sampling temperature 0-2
	MaxTokens        *int      `json:"max_tokens,omitempty"`              // Maximum tokens to generate
}

// ChatResponse represents a response from the LLM chat endpoint
type ChatResponse struct {
	Message      Message `json:"message"`       // AI response message
	Model        string  `json:"model"`         // Model used
	FinishReason string  `json:"finish_reason"` // Reason for completion
	Usage        Usage   `json:"usage"`         // Token usage information
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelInfo represents information about an available LLM model
type ModelInfo struct {
	ID          string `json:"id"`          // Model ID
	Name        string `json:"name"`        // Display name
	Description string `json:"description"` // Model description
	MaxTokens   int    `json:"max_tokens"`  // Maximum context tokens
}

// ModelsResponse represents the response for available models
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

type AIModelConfig struct {
	ID           int       `json:"id" db:"id"`
	UserID       int       `json:"user_id" db:"user_id"`
	ProviderName string    `json:"provider_name" db:"provider_name"`
	BaseURL      string    `json:"base_url" db:"base_url"`
	APIKey       string    `json:"-" db:"api_key"`
	ModelID      string    `json:"model_id" db:"model_id"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

type AIModelConfigRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	ModelID string `json:"model_id"`
}

type AIModelConfigResponse struct {
	ID            int       `json:"id,omitempty"`
	ProviderName  string    `json:"provider_name"`
	DisplayName   string    `json:"display_name"`
	BaseURL       string    `json:"base_url"`
	APIKey        string    `json:"api_key,omitempty"`
	APIKeyMasked  string    `json:"api_key_masked,omitempty"`
	HasAPIKey     bool      `json:"has_api_key"`
	ModelID       string    `json:"model_id"`
	IsRecommended bool      `json:"is_recommended"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}
