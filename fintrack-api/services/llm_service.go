package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"fintrack-api/config"
	"fintrack-api/models"

	openai "github.com/sashabaranov/go-openai"
)

// LLMService handles LLM chat operations
type LLMService struct {
	client           *openai.Client
	config           *config.LLMConfig
	maxContextRounds int
}

// NewLLMService creates a new LLM service instance
func NewLLMService(cfg *config.Config) *LLMService {
	var client *openai.Client
	if cfg.LLM.APIKey != "" {
		client = newOpenAIClient(cfg.LLM.APIKey, cfg.LLM.BaseURL)
	}

	return &LLMService{
		client:           client,
		config:           &cfg.LLM,
		maxContextRounds: cfg.LLM.MaxContextRounds,
	}
}

// Chat sends a chat request to the LLM and returns the response
func (s *LLMService) Chat(req *models.ChatRequest) (*models.ChatResponse, error) {
	if s.client == nil {
		return nil, errors.New("LLM service is not configured (missing API key)")
	}
	return s.chatWithClient(s.client, req, s.config.Timeout)
}

func (s *LLMService) ChatWithAIModelConfig(req *models.ChatRequest, aiConfig *models.AIModelConfig) (*models.ChatResponse, error) {
	if aiConfig == nil || aiConfig.APIKey == "" {
		return s.Chat(req)
	}

	overrideReq := *req
	overrideReq.Model = aiConfig.ModelID

	client := newOpenAIClient(aiConfig.APIKey, aiConfig.BaseURL)
	return s.chatWithClient(client, &overrideReq, s.config.Timeout)
}

func (s *LLMService) chatWithClient(client *openai.Client, req *models.ChatRequest, timeoutSeconds int) (*models.ChatResponse, error) {
	// Limit context to max rounds (default 3)
	maxContextRounds := s.maxContextRounds
	if req.MaxContextRounds > 0 {
		maxContextRounds = req.MaxContextRounds
	}

	// Trim messages to keep only the last N rounds
	messages := s.trimMessages(req.Messages, maxContextRounds)

	// Convert our messages to OpenAI format
	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, msg := range messages {
		openaiMessages[i] = openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Build the request
	chatReq := openai.ChatCompletionRequest{
		Model:    req.Model,
		Messages: openaiMessages,
	}

	// Set optional parameters
	if req.Temperature != nil {
		chatReq.Temperature = *req.Temperature
	}
	if req.MaxTokens != nil {
		chatReq.MaxTokens = *req.MaxTokens
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Call OpenAI API
	resp, err := client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat completion: %w", err)
	}

	// Check if we have a valid response
	if len(resp.Choices) == 0 {
		return nil, errors.New("no response from LLM")
	}

	// Build our response
	response := &models.ChatResponse{
		Message: models.Message{
			Role:    resp.Choices[0].Message.Role,
			Content: resp.Choices[0].Message.Content,
		},
		Model:        resp.Model,
		FinishReason: string(resp.Choices[0].FinishReason),
		Usage: models.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	return response, nil
}

func newOpenAIClient(apiKey string, baseURL string) *openai.Client {
	clientConfig := openai.DefaultConfig(apiKey)
	if baseURL != "" && baseURL != "https://api.openai.com/v1" {
		clientConfig.BaseURL = baseURL
	}
	return openai.NewClientWithConfig(clientConfig)
}

// GetAvailableModels returns a list of available models from OpenAI API
func (s *LLMService) GetAvailableModels() (*models.ModelsResponse, error) {
	if s.client == nil {
		return nil, errors.New("LLM service is not configured (missing API key)")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.Timeout)*time.Second)
	defer cancel()

	// Fetch models from OpenAI API
	modelsList, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models from OpenAI API: %w", err)
	}

	// Filter and convert to our model format
	// We only want to show GPT models that are suitable for chat
	var modelInfoList []models.ModelInfo
	for _, m := range modelsList.Models {
		// Only include GPT models (skip embedding, moderation, etc.)
		if !s.isChatModel(m.ID) {
			continue
		}

		modelInfo := models.ModelInfo{
			ID:          m.ID,
			Name:        s.formatModelName(m.ID),
			Description: s.getModelDescription(m.ID),
			MaxTokens:   s.getModelMaxTokens(m.ID),
		}
		modelInfoList = append(modelInfoList, modelInfo)
	}

	return &models.ModelsResponse{
		Models: modelInfoList,
	}, nil
}

// isChatModel checks if the model is suitable for chat
func (s *LLMService) isChatModel(modelID string) bool {
	// Include GPT models for chat
	chatPrefixes := []string{"gpt-4", "gpt-3.5-turbo"}
	for _, prefix := range chatPrefixes {
		if len(modelID) >= len(prefix) && modelID[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

// formatModelName creates a friendly display name from model ID
func (s *LLMService) formatModelName(modelID string) string {
	// Simple formatting logic
	switch {
	case modelID == "gpt-4":
		return "GPT-4"
	case modelID == "gpt-4-turbo" || modelID == "gpt-4-turbo-preview":
		return "GPT-4 Turbo"
	case modelID == "gpt-3.5-turbo":
		return "GPT-3.5 Turbo"
	case modelID == "gpt-3.5-turbo-16k":
		return "GPT-3.5 Turbo 16K"
	default:
		return modelID
	}
}

// getModelDescription returns a description for the model
func (s *LLMService) getModelDescription(modelID string) string {
	switch {
	case modelID == "gpt-4":
		return "Most capable model, best for complex tasks"
	case modelID == "gpt-4-turbo" || modelID == "gpt-4-turbo-preview":
		return "Latest GPT-4 Turbo model with improved performance"
	case modelID == "gpt-3.5-turbo":
		return "Fast and efficient for most tasks"
	case modelID == "gpt-3.5-turbo-16k":
		return "Extended context version of GPT-3.5 Turbo"
	default:
		return "OpenAI language model"
	}
}

// getModelMaxTokens returns the maximum tokens for the model
func (s *LLMService) getModelMaxTokens(modelID string) int {
	switch {
	case modelID == "gpt-4":
		return 8192
	case modelID == "gpt-4-turbo" || modelID == "gpt-4-turbo-preview":
		return 128000
	case modelID == "gpt-3.5-turbo":
		return 4096
	case modelID == "gpt-3.5-turbo-16k":
		return 16384
	default:
		return 4096 // default fallback
	}
}

// trimMessages keeps only the last N rounds of conversation
// A round consists of a user message and an assistant response
func (s *LLMService) trimMessages(messages []models.Message, maxRounds int) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	// Separate system messages from conversation
	var systemMessages []models.Message
	var conversationMessages []models.Message

	for _, msg := range messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		} else {
			conversationMessages = append(conversationMessages, msg)
		}
	}

	// Calculate how many messages to keep (maxRounds * 2, since each round has user + assistant)
	maxMessages := maxRounds * 2
	var trimmedConversation []models.Message

	if len(conversationMessages) <= maxMessages {
		// If we have fewer messages than the limit, keep all
		trimmedConversation = conversationMessages
	} else {
		// Keep only the last maxMessages
		startIndex := len(conversationMessages) - maxMessages
		trimmedConversation = conversationMessages[startIndex:]
	}

	// Combine system messages with trimmed conversation
	result := make([]models.Message, 0, len(systemMessages)+len(trimmedConversation))
	result = append(result, systemMessages...)
	result = append(result, trimmedConversation...)

	return result
}
