package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ai-functions/internal/backend"
	"ai-functions/internal/gateway"
	"ai-functions/internal/models"
	"ai-functions/internal/queue"
	"ai-functions/internal/store"
)

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvWithAliases(keys []string, fallback string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getenvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func getenvCovRouteMode(key string, fallback string) string {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch raw {
	case "":
		return fallback
	case "xpu_split", "rocm_direct":
		return raw
	default:
		return fallback
	}
}

type dailySyncSchedule struct {
	Hour   int
	Minute int
}

func parseDailySyncTime(raw string) (dailySyncSchedule, bool) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 {
		return dailySyncSchedule{}, false
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return dailySyncSchedule{}, false
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return dailySyncSchedule{}, false
	}
	return dailySyncSchedule{Hour: hour, Minute: minute}, true
}

func dailySyncSchedules(primaryHour int, primaryMinute int, extraTimes string) []dailySyncSchedule {
	schedules := []dailySyncSchedule{{Hour: primaryHour, Minute: primaryMinute}}
	seen := map[string]bool{fmtSchedule(primaryHour, primaryMinute): true}
	for _, item := range strings.Split(extraTimes, ",") {
		schedule, ok := parseDailySyncTime(item)
		if !ok {
			if strings.TrimSpace(item) != "" {
				log.Printf("invalid daily stock sync extra time ignored: %q", item)
			}
			continue
		}
		key := fmtSchedule(schedule.Hour, schedule.Minute)
		if seen[key] {
			continue
		}
		seen[key] = true
		schedules = append(schedules, schedule)
	}
	return schedules
}

func fmtSchedule(hour int, minute int) string {
	return strconv.Itoa(hour) + ":" + strconv.Itoa(minute)
}

func formatDailySyncSchedules(schedules []dailySyncSchedule) string {
	parts := make([]string, 0, len(schedules))
	for _, schedule := range schedules {
		parts = append(parts, fmt.Sprintf("%02d:%02d", schedule.Hour, schedule.Minute))
	}
	return strings.Join(parts, ",")
}

func main() {
	port := getenv("SERVICE_PORT", "9010")
	xpuURL := getenv("XPU_BACKEND_URL", "http://ai-functions-xpu:9008")
	rocmURL := getenv("ROCM_BACKEND_URL", "http://ai-functions-rocm:9009")
	cpuXregURL := strings.TrimSpace(os.Getenv("CPU_XREG_BACKEND_URL"))
	uziURL := strings.TrimSpace(getenv("UZI_BACKEND_URL", "http://ai-functions-uzi:9011"))
	apiToken := getenvWithAliases([]string{"MTF_SERVICE_TOKEN", "GATEWAY_API_TOKEN"}, "fintrack-dev-token")
	deepSeekTUIBackendURL := strings.TrimSpace(os.Getenv("DEEPSEEK_TUI_BACKEND_URL"))
	deepSeekTUIProxyToken := getenvWithAliases([]string{"MTF_SERVICE_TOKEN", "DEEPSEEK_TUI_PROXY_TOKEN", "GATEWAY_API_TOKEN"}, apiToken)
	deepSeekTUIProxyPath := getenv("DEEPSEEK_TUI_PROXY_PATH", "/deepseek-tui")
	deepSeekTUIAuthConfigPath := strings.TrimSpace(os.Getenv("DEEPSEEK_TUI_AUTH_CONFIG_PATH"))
	inferenceTimeBenchmarkPath := getenv("INFERENCE_TIME_BENCHMARK_PATH", "/app/config/inference_time_benchmarks.json")
	covRouteMode := getenvCovRouteMode("GATEWAY_COV_ROUTE_MODE", "xpu_split")
	xpuConcurrency := getenvInt("XPU_CONCURRENCY", 2)
	rocmConcurrency := getenvInt("ROCM_CONCURRENCY", 1)
	cpuXregConcurrency := getenvInt("CPU_XREG_CONCURRENCY", 0)
	uziConcurrency := getenvInt("UZI_CONCURRENCY", 10)
	redisAddr := getenv("REDIS_ADDR", "ai-functions-redis:6379")
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDB := store.ParseRedisDB(os.Getenv("REDIS_DB"), 0)
	redisPrefix := getenv("REDIS_PREFIX", "ai-functions")
	postgresHandlerURL := getenv("POSTGRES_HANDLER_URL", "http://ai-functions-postgres-handler:58004")
	postgresHandlerToken := getenvWithAliases([]string{"POSTGRES_HANDLER_TOKEN", "HISTORY_SERVICE_TOKEN", "API_TOKEN"}, "fintrack-dev-token")
	historyServiceURL := getenv("HISTORY_SERVICE_URL", getenv("AKSHARE_SERVICE_URL", postgresHandlerURL))
	dailyStockSyncEnabled := getenvBool("DAILY_STOCK_SYNC_ENABLED", true)
	dailyStockSyncMode := strings.TrimSpace(strings.ToLower(os.Getenv("DAILY_STOCK_SYNC_MODE")))
	dailyStockSyncHour := getenvInt("DAILY_STOCK_SYNC_HOUR", 22)
	dailyStockSyncMinute := getenvInt("DAILY_STOCK_SYNC_MINUTE", 0)
	dailyStockSyncExtraTimes := getenv("DAILY_STOCK_SYNC_EXTRA_TIMES", "")
	dailyStockSyncMaxConcurrency := getenvInt("DAILY_STOCK_SYNC_MAX_CONCURRENCY", 4)
	dailyStockSyncLookbackDays := getenvInt("DAILY_STOCK_SYNC_LOOKBACK_DAYS", 0)
	dailyStockSyncTimezone := getenv("DAILY_STOCK_SYNC_TIMEZONE", "Asia/Shanghai")
	level1DailyURL := strings.TrimSpace(os.Getenv("A_STOCK_DAILY_URL"))
	level1DailyConcurrent := getenvInt("A_STOCK_DAILY_CONCURRENT", 50)
	if dailyStockSyncMode == "" {
		if level1DailyURL != "" {
			dailyStockSyncMode = "level1"
		} else {
			dailyStockSyncMode = "history"
		}
	}
	if dailyStockSyncMode == "level1" && level1DailyURL == "" {
		level1DailyURL = "http://a-stock-daily:8080"
	}

	location, err := time.LoadLocation(dailyStockSyncTimezone)
	if err != nil {
		log.Printf("invalid DAILY_STOCK_SYNC_TIMEZONE=%q, fallback to Local: %v", dailyStockSyncTimezone, err)
		location = time.Local
	}

	client := backend.NewClient(2 * time.Hour)
	redisStore := store.NewRedisStore(redisAddr, redisPassword, redisDB, redisPrefix)
	defer func() {
		if err := redisStore.Close(); err != nil {
			log.Printf("redis close error: %v", err)
		}
	}()
	if err := redisStore.Ping(context.Background()); err != nil {
		log.Fatalf("redis unavailable: %v", err)
	}

	endpoints := []backend.Endpoint{
		{
			Name:              "xpu",
			Role:              models.BackendRoleMain,
			URL:               xpuURL,
			Capacity:          xpuConcurrency,
			SupportsCov:       covRouteMode == "xpu_split",
			SupportsDirectCov: false,
			SupportsNonCov:    false,
		},
		{
			Name:              "rocm",
			Role:              models.BackendRoleMain,
			URL:               rocmURL,
			Capacity:          rocmConcurrency,
			SupportsCov:       true,
			SupportsDirectCov: true,
			SupportsNonCov:    true,
		},
	}
	if cpuXregURL != "" && cpuXregConcurrency > 0 {
		endpoints = append(endpoints, backend.Endpoint{
			Name:              "cpu-xreg",
			Role:              models.BackendRoleXReg,
			URL:               cpuXregURL,
			Capacity:          cpuXregConcurrency,
			SupportsCov:       true,
			SupportsDirectCov: false,
			SupportsNonCov:    false,
		})
	}
	if uziURL != "" && uziConcurrency > 0 {
		endpoints = append(endpoints, backend.Endpoint{
			Name:        "uzi",
			Role:        models.BackendRoleUZI,
			URL:         uziURL,
			Capacity:    uziConcurrency,
			SupportsUZI: true,
		})
	}
	scheduler := queue.NewScheduler(client, redisStore, endpoints)
	log.Printf("gateway mtf-pro route mode: %s", covRouteMode)
	log.Printf("gateway uzi queue: backend=%s concurrency=%d", uziURL, uziConcurrency)
	if deepSeekTUIBackendURL != "" {
		log.Printf("gateway deepseek tui proxy: path=%s backend=%s token_configured=%t", deepSeekTUIProxyPath, deepSeekTUIBackendURL, deepSeekTUIProxyToken != "")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := scheduler.Recover(ctx); err != nil {
		log.Fatalf("scheduler recover failed: %v", err)
	}
	scheduler.Start(ctx)

	if dailyStockSyncEnabled {
		schedules := dailySyncSchedules(dailyStockSyncHour, dailyStockSyncMinute, dailyStockSyncExtraTimes)
		for _, schedule := range schedules {
			if dailyStockSyncMode == "level1" {
				dailySyncer := gateway.NewLevel1DailySyncer(
					level1DailyURL,
					historyServiceURL,
					apiToken,
					location,
					schedule.Hour,
					schedule.Minute,
					level1DailyConcurrent,
				)
				dailySyncer.Start(ctx)
			} else {
				dailySyncer := gateway.NewDailyStockSyncer(
					postgresHandlerURL,
					historyServiceURL,
					apiToken,
					location,
					schedule.Hour,
					schedule.Minute,
					dailyStockSyncMaxConcurrency,
					dailyStockSyncLookbackDays,
				)
				dailySyncer.Start(ctx)
			}
		}
		if dailyStockSyncMode == "level1" {
			log.Printf(
				"daily stock sync enabled: mode=level1 level1=%s history=%s schedules=%s tz=%s concurrent=%d",
				level1DailyURL,
				historyServiceURL,
				formatDailySyncSchedules(schedules),
				location.String(),
				level1DailyConcurrent,
			)
		} else {
			log.Printf(
				"daily stock sync enabled: mode=history postgres=%s history=%s schedules=%s tz=%s concurrency=%d lookback_days=%d",
				postgresHandlerURL,
				historyServiceURL,
				formatDailySyncSchedules(schedules),
				location.String(),
				dailyStockSyncMaxConcurrency,
				dailyStockSyncLookbackDays,
			)
		}
	} else {
		log.Printf("daily stock sync disabled")
	}

	server := &http.Server{
		Addr: ":" + port,
		Handler: gateway.NewServerWithOptions(scheduler, gateway.ServerOptions{
			DeepSeekTUIBackendURL:      deepSeekTUIBackendURL,
			DeepSeekTUIProxyToken:      deepSeekTUIProxyToken,
			DeepSeekTUIProxyPath:       deepSeekTUIProxyPath,
			DeepSeekTUIAuthConfigPath:  deepSeekTUIAuthConfigPath,
			InferenceTimeBenchmarkPath: inferenceTimeBenchmarkPath,
			PostgresHandlerURL:         postgresHandlerURL,
			PostgresHandlerToken:       postgresHandlerToken,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("starting inference gateway on :%s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("gateway exited with error: %v", err)
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
