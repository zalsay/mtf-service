package handlers

import (
	"errors"
	"net/http"

	"fintrack-api/models"
	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

func (h *UZIHandler) CreateReportOpenToken(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.UZIReportOpenTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.uziService.CreateReportOpenToken(userID.(int), req.RelativePath)
	if err != nil {
		var httpErr *services.HTTPError
		if errors.As(err, &httpErr) {
			c.JSON(httpErr.StatusCode, gin.H{"error": httpErr.Message})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *UZIHandler) OpenReportWithToken(c *gin.Context) {
	redirectURL, upstreamResp, err := h.uziService.ResolveReportOpen(c.Query("token"))
	if err != nil {
		var httpErr *services.HTTPError
		if errors.As(err, &httpErr) {
			c.JSON(httpErr.StatusCode, gin.H{"error": httpErr.Message})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	if redirectURL != "" {
		c.Redirect(http.StatusFound, redirectURL)
		return
	}

	if upstreamResp == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}
	defer upstreamResp.Body.Close()

	contentType := upstreamResp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}

	extraHeaders := map[string]string{}
	if cacheControl := upstreamResp.Header.Get("Cache-Control"); cacheControl != "" {
		extraHeaders["Cache-Control"] = cacheControl
	}
	c.Status(upstreamResp.StatusCode)
	c.DataFromReader(upstreamResp.StatusCode, upstreamResp.ContentLength, contentType, upstreamResp.Body, extraHeaders)
}
