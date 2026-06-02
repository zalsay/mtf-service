package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type DSABridgeHandler struct {
	bridgeService *services.DSABridgeService
	authService   *services.AuthService
}

type bridgeEntryRequest struct {
	BridgeToken string `form:"bridge_token" json:"bridge_token" binding:"required"`
	ReturnTo    string `form:"return_to" json:"return_to"`
}

type bridgeConsumeRequest struct {
	BridgeToken string `json:"bridge_token" binding:"required"`
}

func NewDSABridgeHandler(bridgeService *services.DSABridgeService, authService *services.AuthService) *DSABridgeHandler {
	return &DSABridgeHandler{
		bridgeService: bridgeService,
		authService:   authService,
	}
}

func (h *DSABridgeHandler) Entry(c *gin.Context) {
	var req bridgeEntryRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	claims, err := h.bridgeService.VerifyBridgeToken(req.BridgeToken)
	if err != nil {
		h.writeBridgeError(c, err)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(renderBridgeBootstrapHTML(req.BridgeToken, claims.ReturnTo)))
}

func (h *DSABridgeHandler) Consume(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req bridgeConsumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	claims, err := h.bridgeService.VerifyBridgeToken(req.BridgeToken)
	if err != nil {
		h.writeBridgeError(c, err)
		return
	}

	userID, ok := userIDValue.(int)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid authenticated user"})
		return
	}

	if err := h.authService.BindDSAUser(userID, claims.Subject); err != nil {
		switch {
		case errors.Is(err, services.ErrDSABindingConflict):
			c.JSON(http.StatusConflict, gin.H{
				"error":   "fintrack_account_already_bound",
				"message": "Current FinTrack account is already bound to another daily_stock_analysis user",
			})
		case errors.Is(err, services.ErrDSAUserAlreadyBound):
			c.JSON(http.StatusConflict, gin.H{
				"error":   "dsa_user_already_bound",
				"message": "This daily_stock_analysis user is already bound to another FinTrack account",
			})
		default:
			log.Printf("DSA bridge consume error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to bind daily_stock_analysis user"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":                      "daily_stock_analysis bridge bound",
		"daily_stock_analysis_user_id": claims.Subject,
		"return_to":                    claims.ReturnTo,
	})
}

func (h *DSABridgeHandler) Status(c *gin.Context) {
	userValue, exists := c.Get("user")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	user, ok := userValue.(*models.User)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid authenticated user"})
		return
	}

	bound := user.DailyStockAnalysisUserID != nil && *user.DailyStockAnalysisUserID != ""
	c.JSON(http.StatusOK, gin.H{
		"enabled":                      h.bridgeService.IsEnabled(),
		"issuer":                       h.bridgeService.Issuer(),
		"current_user_bound":           bound,
		"daily_stock_analysis_user_id": user.DailyStockAnalysisUserID,
	})
}

func (h *DSABridgeHandler) Unbind(c *gin.Context) {
	userIDValue, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	userID, ok := userIDValue.(int)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid authenticated user"})
		return
	}

	if err := h.authService.UnbindDSAUser(userID); err != nil {
		log.Printf("DSA bridge unbind error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unbind daily_stock_analysis user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":                      "daily_stock_analysis bridge unbound",
		"current_user_bound":           false,
		"daily_stock_analysis_user_id": nil,
	})
}

func (h *DSABridgeHandler) writeBridgeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrBridgeNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dsa_bridge_unavailable"})
	case errors.Is(err, services.ErrExpiredBridgeToken):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "dsa_bridge_token_expired"})
	case errors.Is(err, services.ErrBridgeIssuerMismatch), errors.Is(err, services.ErrInvalidBridgeToken):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "dsa_bridge_token_invalid"})
	default:
		log.Printf("DSA bridge verify error: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "dsa_bridge_token_invalid"})
	}
}

func renderBridgeBootstrapHTML(bridgeToken string, returnTo string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>FinTrack Bridge</title>
</head>
<body style="font-family: sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: #08110d; color: #f2f5f3;">
  <div style="text-align: center;">
    <p>FinTrack bridge is preparing your login flow...</p>
  </div>
  <script>
    sessionStorage.setItem('fintrack_dsa_bridge_token', %s);
    sessionStorage.setItem('fintrack_dsa_return_to', %s);
    window.location.replace('/');
  </script>
</body>
</html>`, jsonQuoted(bridgeToken), jsonQuoted(returnTo))
}

func jsonQuoted(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
