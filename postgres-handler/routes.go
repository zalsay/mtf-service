package main

import (
	"bytes"
	cgzip "compress/gzip"
	"io"
	"log"
	"net/http"
	"strings"

	gzing "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func RegisterRoutes(r *gin.Engine, handler *DatabaseHandler, apiToken string) {
	r.Use(gzing.Gzip(gzing.DefaultCompression))
	r.Use(RequestGzipDecodeMiddleware())
	r.Use(TokenAuthMiddleware(apiToken))

	api := r.Group("/api/v1")
	{
		api.POST("/stock-data", handler.insertStockDataHandler)
		api.POST("/stock-data/batch", handler.batchInsertStockDataHandler)
		api.GET("/stock-data/symbols", handler.listStockSymbolsHandler)
		api.POST("/stock-data/:symbol", handler.getStockDataHandler)
		api.POST("/stock-data/:symbol/range", handler.getStockDataByDateRangeHandler)

		api.POST("/history", handler.tickflowHistoryHandler)
		api.GET("/trading-day", handler.tickflowTradingDayHandler)
		api.GET("/instruments", handler.tickflowInstrumentsHandler)

		api.POST("/etf/daily", handler.insertEtfDailyHandler)
		api.POST("/etf/daily/batch", handler.batchInsertEtfDailyHandler)

		api.POST("/etf/daily/:code", handler.getEtfDailyHandler)
		api.POST("/etf/daily/:code/range", handler.getEtfDailyByDateRangeHandler)

		api.POST("/index/info", handler.insertIndexInfoHandler)
		api.POST("/index/info/batch", handler.batchInsertIndexInfoHandler)
		api.POST("/index/daily", handler.insertIndexDailyHandler)
		api.POST("/index/daily/batch", handler.batchInsertIndexDailyHandler)
		api.POST("/index/daily/:code", handler.getIndexDailyHandler)
		api.POST("/index/daily/:code/range", handler.getIndexDailyByDateRangeHandler)

		api.POST("/mtf/forecast/batch", handler.batchInsertMTFForecastHandler)
		api.POST("/mtf/forecast/query", handler.getMTFForecastBySymbolVersionHorizon)

		api.POST("/stock/comment/daily/batch", handler.batchInsertAStockCommentDailyHandler)
		api.POST("/stock/comment/daily/search/name", handler.getAStockCommentDailyByNameHandler)
		api.POST("/stock/comment/daily/search/code", handler.getAStockCommentDailyByCodeHandler)

		// 同步 fintrack-api 路由：保存 MTF 最佳分位、验证块、查询以及回测
		api.POST("/save-predictions/mtf-best", handler.saveMTFBestHandler)
		api.POST("/save-predictions/mtf-best/val-chunk", handler.saveMTFValChunkHandler)
		api.POST("/save-predictions/mtf-best/val-chunks", handler.saveMTFValidationChunksHandler)
		api.GET("/save-predictions/mtf-best/by-unique", handler.getMTFBestByUniqueKeyHandler)
		api.GET("/save-predictions/mtf-best/by-config", handler.getMTFBestKeysByConfigHandler)
		api.GET("/save-predictions/mtf-best/val-chunk/latest", handler.getLatestMTFValChunkHandler)
		api.GET("/save-predictions/mtf-best/val-chunk/list", handler.getMTFValChunkListHandler)
		api.POST("/save-predictions/mtf-direct", handler.saveMTFDirectHandler)
		api.GET("/save-predictions/mtf-direct/by-request", handler.getMTFDirectByRequestHandler)

		api.POST("/save-predictions/backtest", handler.saveMTFBacktestHandler)

		api.POST("/strategy/params", handler.saveStrategyParamsHandler)
		api.GET("/strategy/params/by-user-unique", handler.getStrategyParamsByUniqueKeyHandler)
		api.GET("/strategy/params/by-user", handler.getStrategyParamsByUserHandler)

		// LLM Token Usage routes
		api.POST("/llm/token-usage", handler.saveLlmTokenUsageHandler)
		api.GET("/llm/token-usage/:user_id", handler.getLlmTokenUsageByUserHandler)
		api.GET("/llm/token-usage/:user_id/stats", handler.getLlmTokenUsageStatsHandler)
	}

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	log.Printf("API endpoints:")
	log.Printf("  POST /api/v1/stock-data - Insert single stock data")
	log.Printf("  POST /api/v1/stock-data/batch - Batch insert stock data")
	log.Printf("  GET /api/v1/stock-data/symbols - List distinct stock symbols with latest dates")
	log.Printf("  POST /api/v1/stock-data/:symbol - Get stock data (JSON body: {type, limit, offset})")
	log.Printf("  POST /api/v1/stock-data/:symbol/range - Get stock data by date range (JSON body: {type, start_date, end_date})")
	log.Printf("  POST /api/v1/history - Fetch market history from provider chain")
	log.Printf("  GET /api/v1/trading-day - Check trading day with provider chain")
	log.Printf("  GET /api/v1/instruments - Resolve instrument names from DB/TickFlow")
	log.Printf("  POST /api/v1/etf/daily - Upsert single ETF daily data")
	log.Printf("  POST /api/v1/etf/daily/batch - Batch upsert ETF daily data")
	log.Printf("  POST /api/v1/etf/daily/:code - Query ETF daily data (JSON body: {limit, offset})")
	log.Printf("  POST /api/v1/etf/daily/:code/range - Query ETF daily data by date range (JSON body: {start_date, end_date})")
	log.Printf("  POST /api/v1/index/info - Upsert single index info")
	log.Printf("  POST /api/v1/index/info/batch - Batch upsert index info")
	log.Printf("  POST /api/v1/index/daily - Upsert single index daily data")
	log.Printf("  POST /api/v1/index/daily/batch - Batch upsert index daily data")
	log.Printf("  POST /api/v1/index/daily/:code - Query index daily data (JSON body: {limit, offset})")
	log.Printf("  POST /api/v1/index/daily/:code/range - Query index daily data by date range (JSON body: {start_date, end_date})")
	log.Printf("  POST /api/v1/stock/comment/daily/batch - Batch upsert A-stock comment daily metrics")
	log.Printf("  POST /api/v1/stock/comment/daily/search - Query A-stock comment daily by name (JSON body: {name, limit, offset})")
	log.Printf("  POST /api/v1/mtf/forecast/batch - Batch insert MTF forecast")
	log.Printf("  GET  /health - Health check")
}

func TokenAuthMiddleware(expectedToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		token := c.GetHeader("X-Token")
		if token == "" {
			auth := c.GetHeader("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if token == "" || token != expectedToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func RequestGzipDecodeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.EqualFold(c.GetHeader("Content-Encoding"), "gzip") {
			if gzReader, err := cgzip.NewReader(c.Request.Body); err == nil {
				defer gzReader.Close()
				if body, err := io.ReadAll(gzReader); err == nil {
					c.Request.Body = io.NopCloser(bytes.NewReader(body))
					c.Request.Header.Del("Content-Encoding")
				}
			}
		}
		c.Next()
	}
}
