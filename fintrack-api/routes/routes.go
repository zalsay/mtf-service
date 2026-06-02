package routes

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"time"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/handlers"
	"fintrack-api/services"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config, db *database.DB) *gin.Engine {
	// 设置Gin模式
	if cfg.Server.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// 支持请求体gzip解压
	router.Use(func(c *gin.Context) {
		if strings.EqualFold(c.GetHeader("Content-Encoding"), "gzip") {
			if gzReader, err := gzip.NewReader(c.Request.Body); err == nil {
				defer gzReader.Close()
				if body, err := io.ReadAll(gzReader); err == nil {
					c.Request.Body = io.NopCloser(bytes.NewReader(body))
					c.Request.Header.Del("Content-Encoding")
				}
			}
		}
		c.Next()
	})

	// 响应内容gzip压缩（不依赖外部包）
	router.Use(GzipResponseMiddleware())

	corsConfig := cors.Config{
		AllowMethods:     cfg.CORS.AllowedMethods,
		AllowHeaders:     cfg.CORS.AllowedHeaders,
		ExposeHeaders:    []string{"Content-Length", "X-Captcha-Verify-Code"},
		AllowCredentials: cfg.CORS.AllowCredentials,
		MaxAge:           12 * time.Hour,
	}
	if allowsAllOrigins(cfg.CORS.AllowedOrigins) {
		corsConfig.AllowOriginFunc = func(string) bool { return true }
	} else {
		corsConfig.AllowOrigins = cfg.CORS.AllowedOrigins
	}
	router.Use(cors.New(corsConfig))

	// 初始化服务
	authService := services.NewAuthService(db)
	watchlistService := services.NewWatchlistService(db, cfg)
	uziService := services.NewUZIService(db, cfg)
	aiModelConfigService := services.NewAIModelConfigService(db)
	llmService := services.NewLLMService(cfg)
	adminService := services.NewAdminService(db, cfg)
	mtfAgentService := services.NewMTFAgentService(db, cfg)
	financeNewsService := services.NewFinanceNewsService(nil)

	// 初始化处理器
	authHandler := handlers.NewAuthHandler(authService)
	watchlistHandler := handlers.NewWatchlistHandler(watchlistService)
	uziHandler := handlers.NewUZIHandler(uziService, aiModelConfigService)
	settingsHandler := handlers.NewSettingsHandler(aiModelConfigService)
	llmHandler := handlers.NewLLMHandler(llmService, cfg, aiModelConfigService)
	adminHandler := handlers.NewAdminHandler(adminService)
	mtfAgentHandler := handlers.NewMTFAgentHandler(mtfAgentService, aiModelConfigService)
	financeNewsHandler := handlers.NewFinanceNewsHandler(financeNewsService)

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"message": "FinTrack API is running",
		})
	})

	// API版本组
	v1 := router.Group("/api/v1")
	{
		// 认证路由
		auth := v1.Group("/auth")
		{
			auth.POST("/register", authHandler.Register)
			auth.POST("/login", authHandler.Login)
			auth.GET("/status", authHandler.GetStatus)
			auth.GET("/profile", authHandler.AuthMiddleware(), authHandler.GetProfile)
			auth.POST("/logout", authHandler.AuthMiddleware(), authHandler.Logout)
			auth.PUT("/membership", authHandler.AuthMiddleware(), authHandler.UpdateMembership)
			auth.POST("/redeem-invite", authHandler.AuthMiddleware(), authHandler.RedeemInvite)
		}

		admin := v1.Group("/admin")
		admin.Use(authHandler.AuthMiddleware(), authHandler.AdminMiddleware())
		{
			admin.GET("/status", adminHandler.GetStatus)
			admin.GET("/gateway-queue", adminHandler.GetGatewayQueueStatus)
			admin.GET("/invite-codes", adminHandler.ListInviteCodes)
			admin.POST("/invite-codes", adminHandler.CreateInviteCode)
			admin.PATCH("/invite-codes/:id/active", adminHandler.SetInviteCodeActive)
			admin.GET("/system-strategies", adminHandler.ListSystemStrategies)
			admin.POST("/system-strategies", adminHandler.SaveSystemStrategy)
		}

		// Watchlist路由
		watchlist := v1.Group("/watchlist")
		watchlist.Use(authHandler.AuthMiddleware())
		{
			watchlist.POST("", watchlistHandler.AddToWatchlist)
			watchlist.GET("", watchlistHandler.GetWatchlist)
			watchlist.DELETE("/:id", watchlistHandler.RemoveFromWatchlist)
			watchlist.PUT("/:id", watchlistHandler.UpdateWatchlistItem)
			watchlist.POST("/bind", watchlistHandler.BindStrategy)
		}

		quotes := v1.Group("/quotes")
		quotes.Use(authHandler.AuthMiddleware())
		{
			quotes.POST("/batch-latest", watchlistHandler.GetBatchLatestQuotes)
		}

		getPredictions := v1.Group("/get-predictions")
		{
			// 需要鉴权，按当前登录用户查询其关联的best列表
			getPredictions.GET("/mtf-best", authHandler.AuthMiddleware(), watchlistHandler.ListTimesfmBestByUser)
			// 匿名时只返回公开数据；管理员携带有效 token 时可同时查询公开/非公开数据
			getPredictions.GET("/mtf-best/public", authHandler.OptionalAuthMiddleware(), watchlistHandler.ListPublicTimesfmBestWithValidation)
			// 当前登录用户可查询“自己可访问”的 best + validation chunks；管理员可看任意私有/公开数据
			getPredictions.GET("/mtf-best/accessible", authHandler.AuthMiddleware(), watchlistHandler.ListAccessibleTimesfmBestWithValidation)
			getPredictions.GET("/mtf-best/future", watchlistHandler.GetFuturePredictions)
		}

		savePredictions := v1.Group("/save-predictions")
		{
			savePredictions.POST("/mtf-best", watchlistHandler.SaveTimesfmBest)
			savePredictions.GET("/mtf-best/by-unique", watchlistHandler.GetTimesfmBestByUniqueKey)
			savePredictions.GET("/mtf-best/by-config", watchlistHandler.GetTimesfmBestUniqueKeysByConfig)
			savePredictions.POST("/mtf-best/val-chunk", watchlistHandler.SaveTimesfmValChunk)
			savePredictions.GET("/mtf-best/val-chunk/latest", watchlistHandler.GetLatestValidationChunk)
			savePredictions.POST("/backtest", watchlistHandler.SaveTimesfmBacktest)
		}

		// TimesFM 推理与回测代理路由
		timesfm := v1.Group("/mtf")
		{
			timesfm.POST("/predict", watchlistHandler.TriggerTimesfmPredict)
			timesfm.POST("/predict-best", authHandler.AuthMiddleware(), watchlistHandler.TriggerTimesfmPredictBestAuthorized)
			timesfm.POST("/predict-once", authHandler.AuthMiddleware(), watchlistHandler.TriggerTimesfmPredictOnce)
			timesfm.POST("/predict-once/cached", authHandler.AuthMiddleware(), watchlistHandler.GetTimesfmPredictOnceCached)
			timesfm.GET("/jobs/:jobID", authHandler.AuthMiddleware(), watchlistHandler.GetTimesfmJobStatus)
			timesfm.POST("/backtest", authHandler.AuthMiddleware(), watchlistHandler.RunTimesfmBacktestProxy)
			timesfm.GET("/backtest/by-unique", authHandler.AuthMiddleware(), watchlistHandler.GetTimesfmBacktestByUniqueKey)
		}

		strategy := v1.Group("/strategy")
		{
			strategy.POST("/params", authHandler.AuthMiddleware(), watchlistHandler.SaveStrategyParams)
			strategy.GET("/params/by-unique", watchlistHandler.GetStrategyParamsByUniqueKey)
			strategy.GET("/list", authHandler.AuthMiddleware(), watchlistHandler.GetUserStrategies)
		}

		// LLM 路由 (需要鉴权)
		llm := v1.Group("/llm")
		llm.Use(authHandler.AuthMiddleware())
		{
			llm.POST("/chat", llmHandler.Chat)
			llm.GET("/models", llmHandler.GetModels)
		}

		settings := v1.Group("/settings")
		settings.Use(authHandler.AuthMiddleware())
		{
			settings.GET("/ai-model", settingsHandler.GetAIModelConfig)
			settings.PUT("/ai-model", settingsHandler.UpdateAIModelConfig)
		}

		mtfAgent := v1.Group("/mtf-agent")
		mtfAgent.Use(authHandler.AuthMiddleware())
		{
			mtfAgent.GET("/session", mtfAgentHandler.Session)
			mtfAgent.GET("/messages", mtfAgentHandler.ListMessages)
			mtfAgent.POST("/messages", mtfAgentHandler.SendMessage)
			mtfAgent.POST("/messages/stream", mtfAgentHandler.SendMessageStream)
			mtfAgent.POST("/messages/jobs", mtfAgentHandler.StartMessageJob)
			mtfAgent.GET("/messages/jobs/:jobID", mtfAgentHandler.GetMessageJob)
			mtfAgent.POST("/reset", mtfAgentHandler.Reset)
			mtfAgent.GET("/memory", mtfAgentHandler.ListMemories)
			mtfAgent.DELETE("/memory", mtfAgentHandler.ClearMemories)
			mtfAgent.GET("/skills/history-trends", mtfAgentHandler.HistoryTrendsSkill)
			mtfAgent.GET("/skills/uzi-reports", mtfAgentHandler.UZIReportsSkill)
		}

		financeNews := v1.Group("/finance-news")
		{
			financeNews.GET("", financeNewsHandler.List)
		}

		v1.GET("/uzi/health", uziHandler.Health)
		v1.GET("/uzi/report-open", uziHandler.OpenReportWithToken)

		uzi := v1.Group("/uzi")
		uzi.Use(authHandler.AuthMiddleware())
		{
			uzi.POST("/analyze", uziHandler.Analyze)
			uzi.GET("/jobs/:jobID", uziHandler.GetAnalyzeJobStatus)
			uzi.GET("/status", uziHandler.GetAnalyzeStatus)
			uzi.GET("/status/ws", uziHandler.AnalyzeStatusWebSocket)
			uzi.GET("/reports-index", uziHandler.ListReports)
			uzi.POST("/reports-open-token", uziHandler.CreateReportOpenToken)
			uzi.DELETE("/reports-entry", uziHandler.DeleteReport)
			uzi.GET("/reports/*path", uziHandler.GetReport)
		}

		// 股票相关路由（预留）
		stocks := v1.Group("/stocks")
		{
			stocks.GET("", func(c *gin.Context) {
				c.JSON(200, gin.H{"message": "Stocks endpoint - coming soon"})
			})
			stocks.GET("/:symbol", func(c *gin.Context) {
				c.JSON(200, gin.H{"message": "Stock detail endpoint - coming soon"})
			})
			stocks.GET("/lookup", watchlistHandler.LookupStockName)
		}
	}

	return router
}

func allowsAllOrigins(origins []string) bool {
	if len(origins) == 0 {
		return true
	}
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}
	return false
}

// gzip Writer包装，拦截写出并压缩
type gzipWriter struct {
	gin.ResponseWriter
	writer io.Writer
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	return g.writer.Write(data)
}

// 中间件：按客户端Accept-Encoding支持gzip时压缩响应
func GzipResponseMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isSSERequest(c) {
			c.Next()
			return
		}
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}
		c.Header("Content-Encoding", "gzip")
		gz := gzip.NewWriter(c.Writer)
		defer gz.Close()
		c.Writer = &gzipWriter{ResponseWriter: c.Writer, writer: gz}
		c.Next()
	}
}

func isSSERequest(c *gin.Context) bool {
	accept := strings.ToLower(c.GetHeader("Accept"))
	if strings.Contains(accept, "text/event-stream") {
		return true
	}
	return strings.HasSuffix(c.Request.URL.Path, "/mtf-agent/messages/stream")
}
