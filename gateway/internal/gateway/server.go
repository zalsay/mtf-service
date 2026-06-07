package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-functions/internal/models"
	"ai-functions/internal/queue"
)

var shanghaiLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return loc
}()

type Server struct {
	scheduler         *queue.Scheduler
	mux               *http.ServeMux
	deepSeekTUIProxy  http.Handler
	deepSeekTUIToken  string
	deepSeekTUIPrefix string
	timeEstimator     *inferenceTimeEstimator
}

type ServerOptions struct {
	DeepSeekTUIBackendURL      string
	DeepSeekTUIProxyToken      string
	DeepSeekTUIProxyPath       string
	DeepSeekTUIAuthConfigPath  string
	InferenceTimeBenchmarkPath string
}

type inferenceTimeEstimator struct {
	entries map[string]inferenceTimeEstimate
}

type inferenceTimeEstimate struct {
	ContextLen                int     `json:"context_len"`
	HorizonLen                int     `json:"horizon_len"`
	Backend                   string  `json:"backend,omitempty"`
	PredictionType            string  `json:"prediction_type,omitempty"`
	EstimatedInferenceTimeSec float64 `json:"estimated_inference_time_sec"`
	HTTPElapsedSec            float64 `json:"http_elapsed_sec,omitempty"`
	TotalChunks               int     `json:"total_chunks,omitempty"`
	ValidationChunks          int     `json:"validation_chunks,omitempty"`
	Source                    string  `json:"source,omitempty"`
}

type inferenceTimeBenchmarkFile struct {
	GeneratedAt    string                       `json:"generated_at"`
	Backend        string                       `json:"backend"`
	PredictionType string                       `json:"prediction_type"`
	Results        []inferenceTimeEstimate      `json:"results"`
	Benchmarks     []inferenceTimeBenchmarkFile `json:"benchmarks"`
}

func NewServer(scheduler *queue.Scheduler) http.Handler {
	return NewServerWithOptions(scheduler, ServerOptions{})
}

func NewServerWithOptions(scheduler *queue.Scheduler, options ServerOptions) http.Handler {
	server := &Server{
		scheduler:         scheduler,
		mux:               http.NewServeMux(),
		deepSeekTUIToken:  strings.TrimSpace(options.DeepSeekTUIProxyToken),
		deepSeekTUIPrefix: normalizeProxyPrefix(options.DeepSeekTUIProxyPath, "/deepseek-tui"),
	}
	if benchmarkPath := strings.TrimSpace(options.InferenceTimeBenchmarkPath); benchmarkPath != "" {
		estimator, err := loadInferenceTimeEstimator(benchmarkPath)
		if err != nil {
			log.Printf("load inference time benchmark failed: path=%s error=%v", benchmarkPath, err)
		} else {
			server.timeEstimator = estimator
			log.Printf("loaded inference time benchmark: path=%s entries=%d", benchmarkPath, len(estimator.entries))
		}
	}
	if backendURL := strings.TrimSpace(options.DeepSeekTUIBackendURL); backendURL != "" {
		if proxy, err := newDeepSeekTUIProxy(backendURL, server.deepSeekTUIPrefix, options.DeepSeekTUIAuthConfigPath); err == nil {
			server.deepSeekTUIProxy = proxy
		}
	}
	server.routes()
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleRoot)
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/predict_for_best", s.handlePredictForBest)
	s.mux.HandleFunc("/predict_once", s.handlePredictOnce)
	s.mux.HandleFunc("/predict_once_cached", s.handlePredictOnceCached)
	s.mux.HandleFunc("/jobs/", s.handleJob)
	s.mux.HandleFunc("/uzi/analyze", s.handleUZIAnalyze)
	s.mux.HandleFunc("/uzi/jobs/", s.handleUZIJob)
	if s.deepSeekTUIProxy != nil {
		s.mux.HandleFunc(s.deepSeekTUIPrefix, s.handleDeepSeekTUIProxy)
		s.mux.HandleFunc(s.deepSeekTUIPrefix+"/", s.handleDeepSeekTUIProxy)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	routes := map[string]string{
		"health":              "/health",
		"predict_for_best":    "/predict_for_best",
		"predict_once":        "/predict_once",
		"predict_once_cached": "/predict_once_cached",
		"job_status":          "/jobs/{job_id}",
		"uzi_analyze":         "/uzi/analyze",
		"uzi_job_status":      "/uzi/jobs/{job_id}",
	}
	if s.deepSeekTUIProxy != nil {
		routes["deepseek_tui_proxy"] = s.deepSeekTUIPrefix + "/*"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "ai-functions-inference-gateway",
		"routes":  routes,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if s.scheduler == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "healthy",
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	snapshot, err := s.scheduler.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"scheduler": snapshot,
	})
}

func (s *Server) handleDeepSeekTUIProxy(w http.ResponseWriter, r *http.Request) {
	if s.deepSeekTUIProxy == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "deepseek tui proxy is not configured"})
		return
	}
	if s.deepSeekTUIToken != "" && !proxyTokenMatches(r, s.deepSeekTUIToken) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="deepseek-tui"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	s.deepSeekTUIProxy.ServeHTTP(w, r)
}

func (s *Server) handlePredictForBest(w http.ResponseWriter, r *http.Request) {
	s.handleInferenceRequest(w, r, "/internal/predict_for_best_sync", "job accepted")
}

func (s *Server) handlePredictOnce(w http.ResponseWriter, r *http.Request) {
	s.handleInferenceRequest(w, r, "/internal/predict_once_sync", "job accepted")
}

func (s *Server) handlePredictOnceCached(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read request body failed"})
		return
	}
	defer r.Body.Close()

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	body, request, err = normalizeInferencePayload(body, request, "/internal/predict_once_cached_sync")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	request.StockCode = strings.TrimSpace(request.StockCode)
	if request.StockCode == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stock_code is required"})
		return
	}

	statusCode, responseBody, err := s.scheduler.CallBackendSync(r.Context(), "/internal/predict_once_cached_sync", body)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(responseBody)
}

func (s *Server) handleInferenceRequest(w http.ResponseWriter, r *http.Request, targetPath string, message string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read request body failed"})
		return
	}
	defer r.Body.Close()

	var request models.InferenceRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	body, request, err = normalizeInferencePayload(body, request, targetPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	request.StockCode = strings.TrimSpace(request.StockCode)
	if request.StockCode == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "stock_code is required"})
		return
	}

	result, err := s.scheduler.Enqueue(r.Context(), body, request, targetPath)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	job := result.Job
	snapshot, err := s.scheduler.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	response := map[string]any{
		"success":             true,
		"message":             message,
		"reused":              result.Reused,
		"force_enqueue":       job.ForceEnqueue,
		"job_id":              job.ID,
		"status":              job.Status,
		"stock_code":          job.StockCode,
		"prediction_type":     job.PredictionType,
		"covariate_signature": effectiveJobCovariateSignature(job),
		"current_stage":       job.CurrentStage,
		"target_path":         job.TargetPath,
		"request_key":         job.RequestKey,
		"created_at":          job.CreatedAt,
		"status_url":          "/jobs/" + job.ID,
		"queue_status":        snapshot,
	}
	if estimate, ok := s.estimateInferenceTime(request, targetPath); ok {
		response["estimated_inference_time_sec"] = estimate.EstimatedInferenceTimeSec
		response["estimated_inference_time_source"] = estimate.Source
	} else {
		response["estimated_inference_time_sec"] = nil
	}
	if result.Reused {
		response["message"] = "existing job reused"
	}
	if result.QueuePosition > 0 {
		response["queue_position"] = result.QueuePosition
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) handleUZIAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read request body failed"})
		return
	}
	defer r.Body.Close()

	var request models.UZIAnalyzeRequest
	if err := json.Unmarshal(body, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}
	body, request, err = normalizeUZIAnalyzePayload(body, request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if strings.TrimSpace(request.Ticker) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ticker is required"})
		return
	}

	result, err := s.scheduler.EnqueueUZI(r.Context(), body, request, "/internal/analyze_sync")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	job := result.Job
	snapshot, err := s.scheduler.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	response := map[string]any{
		"success":       true,
		"message":       "uzi job accepted",
		"reused":        result.Reused,
		"force_enqueue": job.ForceEnqueue,
		"job_id":        job.ID,
		"job_kind":      job.JobKind,
		"status":        job.Status,
		"ticker":        job.StockCode,
		"current_stage": job.CurrentStage,
		"target_path":   job.TargetPath,
		"request_key":   job.RequestKey,
		"created_at":    job.CreatedAt,
		"status_url":    "/uzi/jobs/" + job.ID,
		"queue_status":  snapshot,
	}
	if result.Reused {
		response["message"] = "existing uzi job reused"
	}
	if result.QueuePosition > 0 {
		response["queue_position"] = result.QueuePosition
	}
	writeJSON(w, http.StatusAccepted, response)
}

func loadInferenceTimeEstimator(path string) (*inferenceTimeEstimator, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var benchmark inferenceTimeBenchmarkFile
	if err := json.Unmarshal(raw, &benchmark); err != nil {
		return nil, err
	}
	estimator := &inferenceTimeEstimator{entries: map[string]inferenceTimeEstimate{}}
	if len(benchmark.Benchmarks) > 0 {
		for _, nested := range benchmark.Benchmarks {
			addInferenceTimeBenchmarkEntries(estimator, nested)
		}
	} else {
		addInferenceTimeBenchmarkEntries(estimator, benchmark)
	}
	return estimator, nil
}

func addInferenceTimeBenchmarkEntries(estimator *inferenceTimeEstimator, benchmark inferenceTimeBenchmarkFile) {
	backend := strings.TrimSpace(benchmark.Backend)
	predictionType := models.NormalizePredictionType(benchmark.PredictionType)
	if predictionType == "" {
		predictionType = models.PredictionTypeMTFLite
	}
	sourceParts := []string{}
	if backend != "" {
		sourceParts = append(sourceParts, backend)
	}
	sourceParts = append(sourceParts, predictionType)
	if benchmark.GeneratedAt != "" {
		sourceParts = append(sourceParts, benchmark.GeneratedAt)
	}
	source := strings.Join(sourceParts, ":")
	if source == "" {
		source = "benchmark"
	}
	for _, entry := range benchmark.Results {
		if entry.ContextLen <= 0 || entry.HorizonLen <= 0 || entry.EstimatedInferenceTimeSec <= 0 {
			continue
		}
		if strings.TrimSpace(entry.Backend) == "" {
			entry.Backend = backend
		}
		if strings.TrimSpace(entry.PredictionType) == "" {
			entry.PredictionType = predictionType
		} else {
			entry.PredictionType = models.NormalizePredictionType(entry.PredictionType)
		}
		entry.Source = source
		estimator.entries[inferenceTimeKey(entry.PredictionType, entry.ContextLen, entry.HorizonLen)] = entry
	}
}

func (s *Server) estimateInferenceTime(request models.InferenceRequest, targetPath string) (inferenceTimeEstimate, bool) {
	if s.timeEstimator == nil {
		return inferenceTimeEstimate{}, false
	}
	if targetPath != "/internal/predict_for_best_sync" {
		return inferenceTimeEstimate{}, false
	}
	predictionType := request.PredictionType()
	if predictionType == "" {
		return inferenceTimeEstimate{}, false
	}
	contextLen := normalizeRequestInt(request.ContextLen, 2048)
	horizonLen := normalizeRequestInt(request.HorizonLen, 7)
	estimate, ok := s.timeEstimator.entries[inferenceTimeKey(predictionType, contextLen, horizonLen)]
	return estimate, ok
}

func inferenceTimeKey(predictionType string, contextLen int, horizonLen int) string {
	return predictionType + ":" + strconv.Itoa(contextLen) + ":" + strconv.Itoa(horizonLen)
}

func normalizeRequestInt(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return typed
		}
	case int64:
		if typed > 0 {
			return int(typed)
		}
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	s.writeJobResponse(w, r, "/jobs/")
}

func (s *Server) handleUZIJob(w http.ResponseWriter, r *http.Request) {
	s.writeJobResponse(w, r, "/uzi/jobs/")
}

func (s *Server) writeJobResponse(w http.ResponseWriter, r *http.Request, prefix string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, prefix)
	if jobID == "" || strings.Contains(jobID, "/") {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}

	job, queuePosition, ok, err := s.scheduler.GetJob(r.Context(), jobID)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}

	response := map[string]any{
		"job_id":              job.ID,
		"job_kind":            job.JobKind,
		"status":              job.Status,
		"force_enqueue":       job.ForceEnqueue,
		"stock_code":          job.StockCode,
		"ticker":              job.StockCode,
		"prediction_type":     job.PredictionType,
		"covariate_signature": effectiveJobCovariateSignature(job),
		"current_stage":       job.CurrentStage,
		"target_path":         job.TargetPath,
		"request_key":         job.RequestKey,
		"backend":             job.Backend,
		"upstream_status":     job.UpstreamStatus,
		"error":               job.Error,
		"created_at":          job.CreatedAt,
		"started_at":          job.StartedAt,
		"finished_at":         job.FinishedAt,
	}
	if queuePosition > 0 {
		response["queue_position"] = queuePosition
	}
	if len(job.ResultBody) > 0 {
		var result any
		if err := json.Unmarshal(job.ResultBody, &result); err == nil {
			response["result"] = result
		} else {
			response["result_raw"] = string(job.ResultBody)
		}
	}
	writeJSON(w, http.StatusOK, response)
}

type deepSeekTUIProxyHandler struct {
	target         *url.URL
	prefix         string
	proxy          *httputil.ReverseProxy
	authConfigPath string
	client         *http.Client
	mutex          sync.Mutex
}

func newDeepSeekTUIProxy(rawBackendURL string, prefix string, authConfigPath string) (*deepSeekTUIProxyHandler, error) {
	target, err := url.Parse(strings.TrimRight(rawBackendURL, "/"))
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		deepSeekAPIKey := strings.TrimSpace(req.Header.Get("X-DeepSeek-API-Key"))
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = stripProxyPrefix(req.URL.Path, prefix)
		req.URL.RawPath = ""
		req.Host = target.Host
		req.Header.Del("Authorization")
		req.Header.Del("X-API-Key")
		req.Header.Del("X-Gateway-API-Token")
		req.Header.Del("X-DeepSeek-API-Key")
		if deepSeekAPIKey != "" {
			req.Header.Set("Authorization", "Bearer "+deepSeekAPIKey)
		}
		req.Header.Set("X-Forwarded-Prefix", prefix)
	}
	proxy.FlushInterval = 100 * time.Millisecond
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": "deepseek tui proxy upstream unavailable",
		})
	}
	return &deepSeekTUIProxyHandler{
		target:         target,
		prefix:         prefix,
		proxy:          proxy,
		authConfigPath: strings.TrimSpace(authConfigPath),
		client:         &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (p *deepSeekTUIProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	deepSeekAPIKey := strings.TrimSpace(r.Header.Get("X-DeepSeek-API-Key"))
	if p.authConfigPath == "" || deepSeekAPIKey == "" {
		p.proxy.ServeHTTP(w, r)
		return
	}

	p.mutex.Lock()
	defer p.mutex.Unlock()

	if err := writeDeepSeekTUIAuthConfig(p.authConfigPath, deepSeekAPIKey, r.Header.Get("X-DeepSeek-Base-URL")); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "write deepseek tui auth config failed"})
		return
	}
	if isDeepSeekTUITurnCreate(r, p.prefix) {
		p.forwardTurnAndWait(w, r)
		return
	}
	p.proxy.ServeHTTP(w, r)
}

func writeDeepSeekTUIAuthConfig(configPath string, apiKey string, baseURL string) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	var builder strings.Builder
	builder.WriteString("api_key = ")
	builder.WriteString(tomlString(apiKey))
	builder.WriteString("\n")
	if trimmed := strings.TrimSpace(baseURL); trimmed != "" {
		builder.WriteString("base_url = ")
		builder.WriteString(tomlString(trimmed))
		builder.WriteString("\n")
	}
	return os.WriteFile(configPath, []byte(builder.String()), 0o600)
}

func tomlString(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(value)
	return `"` + escaped + `"`
}

func isDeepSeekTUITurnCreate(r *http.Request, prefix string) bool {
	if r.Method != http.MethodPost {
		return false
	}
	path := stripProxyPrefix(r.URL.Path, prefix)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 4 && parts[0] == "v1" && parts[1] == "threads" && parts[3] == "turns"
}

func (p *deepSeekTUIProxyHandler) forwardTurnAndWait(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read request body failed"})
		return
	}
	_ = r.Body.Close()

	upstreamResp, upstreamBody, err := p.doBackendRequest(r.Context(), r.Method, stripProxyPrefix(r.URL.Path, p.prefix), r.URL.RawQuery, body, r.Header.Get("Content-Type"), r.Header.Get("X-DeepSeek-API-Key"))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "deepseek tui proxy upstream unavailable"})
		return
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode < http.StatusBadRequest {
		threadID, turnID := parseDeepSeekTUITurnCreateResponse(stripProxyPrefix(r.URL.Path, p.prefix), upstreamBody)
		if threadID != "" && turnID != "" {
			_ = p.waitDeepSeekTUITurn(r.Context(), threadID, turnID, r.Header.Get("X-DeepSeek-API-Key"))
		}
	}

	copyResponseHeaders(w.Header(), upstreamResp.Header)
	w.WriteHeader(upstreamResp.StatusCode)
	_, _ = w.Write(upstreamBody)
}

func (p *deepSeekTUIProxyHandler) doBackendRequest(ctx context.Context, method string, path string, rawQuery string, body []byte, contentType string, deepSeekAPIKey string) (*http.Response, []byte, error) {
	target := *p.target
	target.Path = path
	target.RawQuery = rawQuery
	req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(contentType) != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if apiKey := strings.TrimSpace(deepSeekAPIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("X-Forwarded-Prefix", p.prefix)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, err
	}
	return resp, raw, nil
}

func parseDeepSeekTUITurnCreateResponse(path string, body []byte) (string, string) {
	threadID := deepSeekTUIThreadIDFromTurnPath(path)
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return threadID, ""
	}
	if turn, ok := decoded["turn"].(map[string]any); ok {
		if id, ok := turn["id"].(string); ok {
			return threadID, strings.TrimSpace(id)
		}
	}
	if id, ok := decoded["turn_id"].(string); ok {
		return threadID, strings.TrimSpace(id)
	}
	if id, ok := decoded["id"].(string); ok {
		return threadID, strings.TrimSpace(id)
	}
	return threadID, ""
}

func deepSeekTUIThreadIDFromTurnPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 4 && parts[0] == "v1" && parts[1] == "threads" && parts[3] == "turns" {
		value, err := url.PathUnescape(parts[2])
		if err == nil {
			return strings.TrimSpace(value)
		}
		return strings.TrimSpace(parts[2])
	}
	return ""
}

func (p *deepSeekTUIProxyHandler) waitDeepSeekTUITurn(ctx context.Context, threadID string, turnID string, deepSeekAPIKey string) error {
	path := "/v1/threads/" + url.PathEscape(threadID)
	for attempt := 0; attempt < 900; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		resp, body, err := p.doBackendRequest(ctx, http.MethodGet, path, "", nil, "", deepSeekAPIKey)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= http.StatusBadRequest {
			return fmt.Errorf("deepseek tui turn poll returned %d", resp.StatusCode)
		}
		status := deepSeekTUITurnStatus(body, turnID)
		if status == "completed" || status == "failed" || status == "cancelled" {
			return nil
		}
	}
	return fmt.Errorf("deepseek tui turn timed out")
}

func deepSeekTUITurnStatus(body []byte, turnID string) string {
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ""
	}
	if turn, ok := decoded["turn"].(map[string]any); ok && stringMapValue(turn, "id") == turnID {
		return strings.ToLower(stringMapValue(turn, "status"))
	}
	if turns, ok := decoded["turns"].([]any); ok {
		for _, item := range turns {
			turn, ok := item.(map[string]any)
			if ok && stringMapValue(turn, "id") == turnID {
				return strings.ToLower(stringMapValue(turn, "status"))
			}
		}
	}
	return ""
}

func stringMapValue(values map[string]any, key string) string {
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		if lower == "connection" || lower == "transfer-encoding" || lower == "content-length" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func normalizeProxyPrefix(value string, fallback string) string {
	prefix := "/" + strings.Trim(strings.TrimSpace(value), "/")
	if prefix == "/" {
		prefix = fallback
	}
	if prefix == "" {
		prefix = fallback
	}
	return strings.TrimRight(prefix, "/")
}

func stripProxyPrefix(path string, prefix string) string {
	if path == prefix {
		return "/"
	}
	stripped := strings.TrimPrefix(path, prefix+"/")
	if stripped == path {
		return path
	}
	return "/" + stripped
}

func proxyTokenMatches(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return true
	}
	if token := strings.TrimSpace(r.Header.Get("X-API-Key")); token != "" {
		return token == expected
	}
	if token := strings.TrimSpace(r.Header.Get("X-Gateway-API-Token")); token != "" {
		return token == expected
	}
	const bearerPrefix = "bearer "
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) >= len(bearerPrefix) && strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
		return strings.TrimSpace(auth[len(bearerPrefix):]) == expected
	}
	return false
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func effectiveJobCovariateSignature(job *models.Job) string {
	if job == nil {
		return ""
	}
	if job.CovariateSignature != "" {
		return job.CovariateSignature
	}
	if len(job.ResultBody) == 0 {
		return ""
	}

	var payload struct {
		CovariateSignature string `json:"covariate_signature"`
		Data               struct {
			CovariateSignature string `json:"covariate_signature"`
			OverallMetrics     struct {
				CovariateSignature string `json:"covariate_signature"`
			} `json:"overall_metrics"`
		} `json:"data"`
	}
	if err := json.Unmarshal(job.ResultBody, &payload); err != nil {
		return job.CovariateSignature
	}
	if payload.Data.OverallMetrics.CovariateSignature != "" {
		return payload.Data.OverallMetrics.CovariateSignature
	}
	if payload.Data.CovariateSignature != "" {
		return payload.Data.CovariateSignature
	}
	if payload.CovariateSignature != "" {
		return payload.CovariateSignature
	}
	return job.CovariateSignature
}

func normalizeInferencePayload(body []byte, request models.InferenceRequest, targetPath string) ([]byte, models.InferenceRequest, error) {
	normalized := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &normalized); err != nil {
			return nil, request, err
		}
	}

	request.StockCode = strings.TrimSpace(request.StockCode)
	normalized["stock_code"] = request.StockCode

	delete(normalized, "mtf_version")

	request.ForceEnqueue = models.NormalizeForceEnqueueValue(request.ForceEnqueue)
	if request.ForceEnqueueEnabled() {
		normalized["force_enqueue"] = true
	} else {
		delete(normalized, "force_enqueue")
	}

	covariateConfig, covariatePreset := models.CanonicalizeCovariateRouting(
		func() any {
			if request.CovariateConfig != nil {
				return request.CovariateConfig
			}
			return request.Covariates
		}(),
		request.CovariatePreset,
	)
	request.CovariateConfig = covariateConfig
	request.Covariates = nil
	request.CovariatePreset = covariatePreset
	delete(normalized, "covariates")
	if covariateConfig != nil {
		normalized["covariate_config"] = covariateConfig
	}
	if covariatePreset != "" {
		normalized["covariate_preset"] = covariatePreset
	}
	if covariateSignature := request.CovariateSignature(); covariateSignature != "" {
		normalized["covariate_signature"] = covariateSignature
	} else {
		delete(normalized, "covariate_signature")
	}
	request.PredictionTypeValue = request.PredictionType()
	normalized["prediction_type"] = request.PredictionTypeValue

	if targetPath == "/internal/predict_once_sync" || targetPath == "/internal/predict_once_cached_sync" {
		if _, exists := normalized["best_max_age_days"]; !exists {
			normalized["best_max_age_days"] = 180
			request.BestMaxAgeDays = 180
		}
		if _, exists := normalized["predict_from_best_val_end"]; !exists {
			normalized["predict_from_best_val_end"] = true
			request.PredictFromBestValEnd = true
		}
		if _, exists := normalized["chunk_until_latest"]; !exists {
			normalized["chunk_until_latest"] = true
			request.ChunkUntilLatest = true
		}
	}

	endDateFallback := time.Now().In(shanghaiLocation)
	if targetPath == "/internal/predict_for_best_sync" {
		endDateFallback = time.Time{}
	}
	endDate := modelsNormalizeDateOrDefault(request.EndDate, endDateFallback)
	if endDate != "" {
		normalized["end_date"] = endDate
		request.EndDate = endDate
	} else {
		delete(normalized, "end_date")
		request.EndDate = nil
	}

	startDate := modelsNormalizeDateOrDefault(request.StartDate, time.Time{})
	if startDate != "" {
		normalized["start_date"] = startDate
		request.StartDate = startDate
	} else {
		delete(normalized, "start_date")
		request.StartDate = nil
		if targetPath != "/internal/predict_for_best_sync" {
			years := normalizeYears(request.Years)
			if years > 0 && endDate != "" {
				startDate = subtractYearsYYYYMMDD(endDate, years)
				normalized["start_date"] = startDate
				request.StartDate = startDate
			}
		}
	}

	updatedBody, err := json.Marshal(normalized)
	if err != nil {
		return nil, request, err
	}
	return updatedBody, request, nil
}

func normalizeUZIAnalyzePayload(body []byte, request models.UZIAnalyzeRequest) ([]byte, models.UZIAnalyzeRequest, error) {
	normalized := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &normalized); err != nil {
			return nil, request, err
		}
	}

	request.Ticker = strings.ToUpper(strings.TrimSpace(request.Ticker))
	normalized["ticker"] = request.Ticker

	request.Depth = normalizeUZIDepthValue(request.Depth)
	normalized["depth"] = request.Depth

	noResume := models.NormalizeForceEnqueueValue(request.NoResume)
	if noResume {
		normalized["no_resume"] = true
		request.NoResume = true
	} else {
		delete(normalized, "no_resume")
		request.NoResume = false
	}

	request.ForceEnqueue = models.NormalizeForceEnqueueValue(request.ForceEnqueue)
	if request.ForceEnqueueEnabled() {
		normalized["force_enqueue"] = true
	} else {
		delete(normalized, "force_enqueue")
	}

	if aiModel, ok := normalized["ai_model"].(map[string]any); ok {
		request.AIModel = aiModel
	}

	updatedBody, err := json.Marshal(normalized)
	if err != nil {
		return nil, request, err
	}
	return updatedBody, request, nil
}

func normalizeUZIDepthValue(value string) string {
	switch strings.TrimSpace(value) {
	case "lite", "medium", "deep":
		return strings.TrimSpace(value)
	default:
		return "medium"
	}
}

func modelsNormalizeDateOrDefault(value any, fallback time.Time) string {
	raw := strings.TrimSpace(stringValue(value))
	if raw != "" {
		layouts := []string{"20060102", "2006-01-02", time.RFC3339, "2006-01-02 15:04:05"}
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed.Format("20060102")
			}
		}
		return raw
	}
	if fallback.IsZero() {
		return ""
	}
	return fallback.Format("20060102")
}

func subtractYearsYYYYMMDD(dateStr string, years int) string {
	parsed, err := time.ParseInLocation("20060102", dateStr, shanghaiLocation)
	if err != nil {
		return ""
	}
	result := parsed.AddDate(-years, 0, 0)
	return result.Format("20060102")
}

func normalizeYears(value any) int {
	switch typed := value.(type) {
	case nil:
		return 15
	case json.Number:
		if intValue, err := typed.Int64(); err == nil {
			return int(intValue)
		}
		if floatValue, err := typed.Float64(); err == nil {
			return int(floatValue)
		}
		return 15
	case int:
		return typed
	case int8:
		return int(typed)
	case int16:
		return int(typed)
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case uint8:
		return int(typed)
	case uint16:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case string:
		years, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return years
		}
		return 15
	default:
		return 15
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}
