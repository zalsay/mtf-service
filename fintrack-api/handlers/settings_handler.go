package handlers

import (
	"net/http"
	"strings"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type SettingsHandler struct {
	aiModelConfigService *services.AIModelConfigService
}

func NewSettingsHandler(aiModelConfigService *services.AIModelConfigService) *SettingsHandler {
	return &SettingsHandler{aiModelConfigService: aiModelConfigService}
}

func (h *SettingsHandler) GetAIModelConfig(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	response, err := h.aiModelConfigService.GetResponseByUserID(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get AI model config"})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *SettingsHandler) UpdateAIModelConfig(c *gin.Context) {
	userID, ok := getAuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req models.AIModelConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	response, err := h.aiModelConfigService.Upsert(userID, req)
	if err != nil {
		if isAIModelConfigValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save AI model config"})
		return
	}

	c.JSON(http.StatusOK, response)
}

func getAuthenticatedUserID(c *gin.Context) (int, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	id, ok := userID.(int)
	return id, ok
}

func isAIModelConfigValidationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "base_url") ||
		strings.Contains(message, "api_key") ||
		strings.Contains(message, "model_id")
}
