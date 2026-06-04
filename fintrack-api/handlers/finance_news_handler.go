package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"fintrack-api/services"

	"github.com/gin-gonic/gin"
)

type FinanceNewsHandler struct {
	service *services.FinanceNewsService
}

func NewFinanceNewsHandler(service *services.FinanceNewsService) *FinanceNewsHandler {
	return &FinanceNewsHandler{service: service}
}

func (h *FinanceNewsHandler) List(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "finance news service is not configured"})
		return
	}
	query := services.FinanceNewsQuery{
		Category: strings.TrimSpace(c.Query("category")),
		Symbol:   strings.TrimSpace(c.Query("symbol")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Limit:    queryInt(c, "limit"),
		Page:     queryInt(c, "page"),
	}
	result, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *FinanceNewsHandler) HotETF(c *gin.Context) {
	if h == nil || h.service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "finance news service is not configured"})
		return
	}
	result, err := h.service.ListHotETF(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func queryInt(c *gin.Context, key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return 0
	}
	return value
}
