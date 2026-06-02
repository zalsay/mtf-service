package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService *services.AuthService
}

func NewAuthHandler(authService *services.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.authService.Register(&req)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if isRegisterInviteError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		log.Printf("Register error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register user"})
		return
	}

	c.JSON(http.StatusCreated, response)
}

func isRegisterInviteError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "invite code not found") ||
		strings.Contains(message, "invite code is disabled") ||
		strings.Contains(message, "invite code is required") ||
		strings.Contains(message, "invite code usage limit reached")
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.authService.Login(&req)
	if err != nil {
		if strings.Contains(err.Error(), "invalid email or password") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
			return
		}
		log.Printf("Login error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to login"})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AuthHandler) GetProfile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	profile, err := h.authService.GetUserProfile(userID.(int))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user profile"})
		return
	}

	c.JSON(http.StatusOK, profile)
}

func (h *AuthHandler) GetStatus(c *gin.Context) {
	token := extractBearerToken(c.GetHeader("Authorization"))
	if token == "" {
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
		return
	}

	user, err := h.authService.ValidateSession(token)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"authenticated": false})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"authenticated": true,
		"user": gin.H{
			"id":                    user.ID,
			"email":                 user.Email,
			"username":              user.Username,
			"is_premium":            user.IsPremium,
			"is_admin":              user.IsAdmin,
			"membership_level":      user.MembershipLevel,
			"membership_expires_at": user.MembershipExpiresAt,
		},
	})
}

type updateMembershipRequest struct {
	MembershipLevel     int        `json:"membership_level"`
	MembershipExpiresAt *time.Time `json:"membership_expires_at"`
}

func (h *AuthHandler) UpdateMembership(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	var req updateMembershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.authService.UpdateMembershipLevel(userID.(int), req.MembershipLevel, req.MembershipExpiresAt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":               "membership updated",
		"membership_level":      req.MembershipLevel,
		"membership_expires_at": req.MembershipExpiresAt,
	})
}

func (h *AuthHandler) RedeemInvite(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	var req models.RedeemInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := h.authService.RedeemMembershipInvite(userID.(int), req.Code)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	token := extractBearerToken(c.GetHeader("Authorization"))
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Authorization header required"})
		return
	}

	err := h.authService.Logout(userID.(int), token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// Middleware for JWT authentication
func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearerToken(c.GetHeader("Authorization"))
		if token == "" && strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
			token = strings.TrimSpace(c.Query("token"))
		}
		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		user, err := h.authService.ValidateSession(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
			c.Abort()
			return
		}

		// Set user information in context
		c.Set("user_id", user.ID)
		c.Set("user_email", user.Email)
		c.Set("is_admin", user.IsAdmin)
		c.Set("user", user)
		c.Next()
	}
}

func (h *AuthHandler) OptionalAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.Next()
			return
		}

		user, err := h.authService.ValidateSession(token)
		if err == nil {
			c.Set("user_id", user.ID)
			c.Set("user_email", user.Email)
			c.Set("is_admin", user.IsAdmin)
			c.Set("user", user)
		}

		c.Next()
	}
}

func (h *AuthHandler) AdminMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		isAdmin, _ := c.Get("is_admin")
		if ok, _ := isAdmin.(bool); !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "Admin permission required"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Optional middleware for premium users only
func (h *AuthHandler) PremiumMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, exists := c.Get("user")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			c.Abort()
			return
		}

		if !user.(*models.User).IsPremium {
			c.JSON(http.StatusForbidden, gin.H{"error": "Premium subscription required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Health check endpoint
func (h *AuthHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"service":   "fintrack-api",
		"timestamp": strconv.FormatInt(c.Request.Context().Value("timestamp").(int64), 10),
	})
}

func extractBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return header
}
