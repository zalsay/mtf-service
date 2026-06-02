package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"fintrack-api/config"
	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

// LLMHandler handles LLM-related HTTP requests
type LLMHandler struct {
	llmService           *services.LLMService
	config               *config.Config
	aiModelConfigService *services.AIModelConfigService
}

// NewLLMHandler creates a new LLM handler
func NewLLMHandler(llmService *services.LLMService, cfg *config.Config, aiModelConfigService *services.AIModelConfigService) *LLMHandler {
	return &LLMHandler{
		llmService:           llmService,
		config:               cfg,
		aiModelConfigService: aiModelConfigService,
	}
}

// Chat handles chat completion requests
// POST /api/v1/llm/chat
func (h *LLMHandler) Chat(c *gin.Context) {
	if h.llmService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "LLM service is not configured. Please set OPENAI_API_KEY in environment variables.",
		})
		return
	}

	// Parse request body
	var req models.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request format: " + err.Error(),
		})
		return
	}

	// Validate that we have at least one message
	if len(req.Messages) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "At least one message is required",
		})
		return
	}

	userID, _ := c.Get("user_id")
	aiConfig, err := h.aiModelConfigService.GetByUserID(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to load AI model config",
		})
		return
	}

	response, err := h.llmService.ChatWithAIModelConfig(&req, aiConfig)
	if err != nil {
		log.Printf("LLM chat error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process chat request: " + err.Error(),
		})
		return
	}

	go h.saveTokenUsage(userID.(int), response.Model, response)

	// Return successful response
	c.JSON(http.StatusOK, response)
}

// GetModels returns the list of available LLM models
// GET /api/v1/llm/models
func (h *LLMHandler) GetModels(c *gin.Context) {
	// Check if LLM service is available
	if h.llmService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "LLM service is not configured. Please set OPENAI_API_KEY in environment variables.",
		})
		return
	}

	// Get available models
	response, err := h.llmService.GetAvailableModels()
	if err != nil {
		log.Printf("Failed to get models: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to retrieve available models",
		})
		return
	}

	// Return successful response
	c.JSON(http.StatusOK, response)
}

// saveTokenUsage saves token usage to postgres-handler
func (h *LLMHandler) saveTokenUsage(userID int, model string, response *models.ChatResponse) {
	if h.config.PostgresHandler.BaseURL == "" || h.config.PostgresHandler.APIToken == "" {
		log.Println("Postgres handler not configured, skipping token usage tracking")
		return
	}

	// Prepare token usage data
	tokenUsage := map[string]interface{}{
		"user_id":           userID,
		"provider":          "openai",
		"model":             model,
		"prompt_tokens":     response.Usage.PromptTokens,
		"completion_tokens": response.Usage.CompletionTokens,
		"total_tokens":      response.Usage.TotalTokens,
		"request_time":      time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(tokenUsage)
	if err != nil {
		log.Printf("Failed to marshal token usage data: %v", err)
		return
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/api/v1/llm/token-usage", h.config.PostgresHandler.BaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Failed to create token usage request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", h.config.PostgresHandler.APIToken)

	// Send request with timeout
	client := &http.Client{
		Timeout: time.Duration(h.config.PostgresHandler.Timeout) * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to save token usage: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to save token usage, status code: %d", resp.StatusCode)
		return
	}

	log.Printf("Successfully saved token usage for user %d: %d tokens", userID, response.Usage.TotalTokens)
}
