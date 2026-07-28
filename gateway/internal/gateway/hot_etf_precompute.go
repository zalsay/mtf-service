package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-functions/internal/models"
	"ai-functions/internal/queue"

	"golang.org/x/net/html"
)

const (
	defaultHotETFSourceURL         = "https://ai.meetlife.top/hot-etf/latest"
	defaultHotETFPrecomputeHorizon = 8
	defaultHotETFJobTimeout        = 6 * time.Hour
	defaultHotETFPollInterval      = 10 * time.Second
)

var hotETFCodePattern = regexp.MustCompile(`\b([15]\d{5})\b`)

type HotETFPrecomputeOptions struct {
	SourceURL       string
	PostgresBaseURL string
	PostgresToken   string
	HistoryBaseURL  string
	Location        *time.Location
	ScheduleHour    int
	ScheduleMinute  int
	HorizonLen      int
	ContextLen      int
	MaxConcurrency  int
	JobTimeout      time.Duration
	PollInterval    time.Duration
}

type HotETFPrecomputer struct {
	client          *http.Client
	sourceURL       string
	postgresBaseURL string
	postgresToken   string
	historyBaseURL  string
	location        *time.Location
	scheduleHour    int
	scheduleMinute  int
	horizonLen      int
	contextLen      int
	maxConcurrency  int
	jobTimeout      time.Duration
	pollInterval    time.Duration
	scheduler       *queue.Scheduler
	runMu           sync.Mutex
}

type hotETFPrecomputeResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type hotETFPrecomputeItem struct {
	Code      string
	StockType int
}

func NewHotETFPrecomputer(scheduler *queue.Scheduler, options HotETFPrecomputeOptions) *HotETFPrecomputer {
	location := options.Location
	if location == nil {
		location = shanghaiLocation
	}
	sourceURL := strings.TrimSpace(options.SourceURL)
	if sourceURL == "" {
		sourceURL = defaultHotETFSourceURL
	}
	if options.HorizonLen <= 0 {
		options.HorizonLen = defaultHotETFPrecomputeHorizon
	}
	if options.ContextLen <= 0 {
		options.ContextLen = 2048
	}
	if options.MaxConcurrency <= 0 {
		options.MaxConcurrency = 2
	}
	if options.JobTimeout <= 0 {
		options.JobTimeout = defaultHotETFJobTimeout
	}
	if options.PollInterval <= 0 {
		options.PollInterval = defaultHotETFPollInterval
	}
	return &HotETFPrecomputer{
		client:          &http.Client{Timeout: 2 * time.Minute},
		sourceURL:       sourceURL,
		postgresBaseURL: strings.TrimRight(strings.TrimSpace(options.PostgresBaseURL), "/"),
		postgresToken:   strings.TrimSpace(options.PostgresToken),
		historyBaseURL:  strings.TrimRight(strings.TrimSpace(options.HistoryBaseURL), "/"),
		location:        location,
		scheduleHour:    options.ScheduleHour,
		scheduleMinute:  options.ScheduleMinute,
		horizonLen:      options.HorizonLen,
		contextLen:      options.ContextLen,
		maxConcurrency:  options.MaxConcurrency,
		jobTimeout:      options.JobTimeout,
		pollInterval:    options.PollInterval,
		scheduler:       scheduler,
	}
}

func (s *HotETFPrecomputer) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *HotETFPrecomputer) loop(ctx context.Context) {
	for {
		now := time.Now().In(s.location)
		nextRun := s.nextRunAfter(now)
		log.Printf("hot ETF precompute scheduled at %s", nextRun.Format(time.RFC3339))

		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := s.RunOnce(ctx); err != nil {
			log.Printf("hot ETF precompute run failed: %v", err)
		}
	}
}

func (s *HotETFPrecomputer) nextRunAfter(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), s.scheduleHour, s.scheduleMinute, 0, 0, s.location)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func (s *HotETFPrecomputer) RunOnce(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.scheduler == nil {
		return fmt.Errorf("gateway scheduler is not configured")
	}
	if s.postgresBaseURL == "" {
		return fmt.Errorf("postgres handler URL is not configured")
	}

	predictDate, err := s.resolvePredictionDate(ctx)
	if err != nil {
		return err
	}
	items, err := s.fetchHotETF(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("hot ETF source returned no ETF symbols")
	}
	items = append(items, hotETFPrecomputeItem{Code: "000001", StockType: 3})

	log.Printf("hot ETF precompute started: date=%s symbols=%d requested_context=%d horizon=%d", predictDate, len(items), s.contextLen, s.horizonLen)
	sem := make(chan struct{}, s.maxConcurrency)
	results := make(chan error, len(items))
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				results <- ctx.Err()
			case sem <- struct{}{}:
				defer func() { <-sem }()
				results <- s.precomputeSymbol(ctx, item.Code, item.StockType, predictDate)
			}
		}()
	}
	wg.Wait()
	close(results)

	failed := 0
	for err := range results {
		if err != nil {
			failed++
			log.Printf("hot ETF precompute symbol failed: %v", err)
		}
	}
	log.Printf("hot ETF precompute finished: date=%s symbols=%d failed=%d", predictDate, len(items), failed)
	if failed > 0 {
		return fmt.Errorf("hot ETF precompute failed for %d/%d symbols", failed, len(items))
	}
	return nil
}

func (s *HotETFPrecomputer) precomputeSymbol(ctx context.Context, symbol string, stockType int, predictDate string) error {
	best, err := s.lookupBest(ctx, symbol, stockType)
	if err != nil {
		return fmt.Errorf("%s lookup best: %w", symbol, err)
	}
	if best.UniqueKey == "" {
		job, err := s.enqueue(ctx, symbol, stockType, s.contextLen, "", "/internal/predict_for_best_sync")
		if err != nil {
			return fmt.Errorf("%s enqueue train: %w", symbol, err)
		}
		log.Printf("hot ETF precompute train queued: symbol=%s requested_context=%d job=%s reused=%t", symbol, s.contextLen, job.Job.ID, job.Reused)
		if err := s.waitForJob(ctx, job.Job.ID); err != nil {
			return fmt.Errorf("%s train job=%s: %w", symbol, job.Job.ID, err)
		}
		best, err = s.lookupBest(ctx, symbol, stockType)
		if err != nil {
			return fmt.Errorf("%s lookup trained best: %w", symbol, err)
		}
		if best.UniqueKey == "" {
			return fmt.Errorf("%s train succeeded but best is still missing", symbol)
		}
	}
	if best.ContextLen <= 0 {
		return fmt.Errorf("%s best has no selected context length", symbol)
	}

	found, err := s.lookupFuture(ctx, symbol, stockType, best.ContextLen, predictDate)
	if err != nil {
		return fmt.Errorf("%s context=%d lookup future: %w", symbol, best.ContextLen, err)
	}
	if found {
		log.Printf("hot ETF precompute cache hit: symbol=%s context=%d date=%s", symbol, best.ContextLen, predictDate)
		return nil
	}

	job, err := s.enqueue(ctx, symbol, stockType, best.ContextLen, predictDate, "/internal/predict_once_sync")
	if err != nil {
		return fmt.Errorf("%s context=%d enqueue future: %w", symbol, best.ContextLen, err)
	}
	log.Printf("hot ETF precompute future queued: symbol=%s context=%d date=%s job=%s reused=%t", symbol, best.ContextLen, predictDate, job.Job.ID, job.Reused)
	if err := s.waitForJob(ctx, job.Job.ID); err != nil {
		return fmt.Errorf("%s context=%d future job=%s: %w", symbol, best.ContextLen, job.Job.ID, err)
	}
	found, err = s.lookupFuture(ctx, symbol, stockType, best.ContextLen, predictDate)
	if err != nil {
		return fmt.Errorf("%s context=%d verify future: %w", symbol, best.ContextLen, err)
	}
	if !found {
		return fmt.Errorf("%s context=%d future job succeeded but date %s is absent", symbol, best.ContextLen, predictDate)
	}
	return nil
}

func (s *HotETFPrecomputer) enqueue(ctx context.Context, symbol string, stockType int, contextLen int, predictDate string, targetPath string) (*queue.EnqueueResult, error) {
	request := models.InferenceRequest{
		StockCode:           symbol,
		StockType:           stockType,
		Years:               15,
		HorizonLen:          s.horizonLen,
		ContextLen:          contextLen,
		PredictionTypeValue: models.PredictionTypeMTFPro,
		CovariatePreset:     models.CovariatePresetMarketV1,
		CovariateConfig: map[string]any{
			"enabled":   true,
			"xreg_mode": models.XRegModeMTFPlusXReg,
		},
		QueuePriority: "background",
		RefreshReason: "hot_etf_precompute",
	}
	if predictDate != "" {
		request.PredictDate = predictDate
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	payload, normalized, err := normalizeInferencePayload(raw, request, targetPath)
	if err != nil {
		return nil, err
	}
	return s.scheduler.Enqueue(ctx, payload, normalized, targetPath)
}

func (s *HotETFPrecomputer) waitForJob(ctx context.Context, jobID string) error {
	waitCtx, cancel := context.WithTimeout(ctx, s.jobTimeout)
	defer cancel()
	for {
		job, _, ok, err := s.scheduler.GetJob(waitCtx, jobID)
		if err != nil {
			return err
		}
		if !ok || job == nil {
			return fmt.Errorf("job not found")
		}
		switch job.Status {
		case models.JobSucceeded:
			return nil
		case models.JobFailed:
			if job.Error != "" {
				return fmt.Errorf("%s", job.Error)
			}
			return fmt.Errorf("job failed")
		}
		timer := time.NewTimer(s.pollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return waitCtx.Err()
		case <-timer.C:
		}
	}
}

type hotETFBestSelection struct {
	UniqueKey  string
	ContextLen int
}

func (s *HotETFPrecomputer) lookupBest(ctx context.Context, symbol string, stockType int) (hotETFBestSelection, error) {
	query := url.Values{
		"symbol":      []string{symbol},
		"stock_type":  []string{strconv.Itoa(stockType)},
		"horizon_len": []string{strconv.Itoa(s.horizonLen)},
	}
	status, data, err := s.getPostgresData(ctx, "/api/v1/save-predictions/mtf-best/by-config", query)
	if err != nil {
		return hotETFBestSelection{}, err
	}
	if status == http.StatusNotFound {
		return hotETFBestSelection{}, nil
	}
	return hotETFBestSelection{
		UniqueKey:  strings.TrimSpace(hotETFStringValue(data["mtf_pro_unique_key"])),
		ContextLen: hotETFIntValue(data["mtf_pro_context_len"], hotETFIntValue(data["context_len"], 0)),
	}, nil
}

func (s *HotETFPrecomputer) lookupFuture(ctx context.Context, symbol string, stockType int, contextLen int, predictDate string) (bool, error) {
	query := url.Values{
		"symbol":          []string{symbol},
		"stock_type":      []string{strconv.Itoa(stockType)},
		"horizon_len":     []string{strconv.Itoa(s.horizonLen)},
		"context_len":     []string{strconv.Itoa(contextLen)},
		"predict_date":    []string{predictDate},
		"prediction_type": []string{models.PredictionTypeMTFPro},
	}
	status, data, err := s.getPostgresData(ctx, "/api/v1/save-predictions/mtf-direct/by-request", query)
	if err != nil {
		return false, err
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	dates, ok := data["future_dates"].([]any)
	if !ok {
		if values, ok := data["future_dates"].([]string); ok {
			for _, value := range values {
				if strings.TrimSpace(value) == predictDate {
					return true, nil
				}
			}
		}
		return false, nil
	}
	for _, value := range dates {
		if strings.TrimSpace(fmt.Sprint(value)) == predictDate {
			return true, nil
		}
	}
	return false, nil
}

func (s *HotETFPrecomputer) getPostgresData(ctx context.Context, path string, query url.Values) (int, map[string]any, error) {
	requestURL := s.postgresBaseURL + path
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Token", s.postgresToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var envelope hotETFPrecomputeResponse
	if len(body) > 0 {
		if err := json.Unmarshal(body, &envelope); err != nil {
			return resp.StatusCode, nil, fmt.Errorf("decode postgres response: %w", err)
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		return resp.StatusCode, nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil, fmt.Errorf("postgres handler returned status %d", resp.StatusCode)
	}
	data := map[string]any{}
	if len(envelope.Data) > 0 && string(envelope.Data) != "null" {
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return resp.StatusCode, nil, fmt.Errorf("decode postgres data: %w", err)
		}
	}
	return resp.StatusCode, data, nil
}

func (s *HotETFPrecomputer) fetchHotETF(ctx context.Context) ([]hotETFPrecomputeItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.sourceURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch hot ETF source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch hot ETF source returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse hot ETF source: %w", err)
	}
	table := findHTMLByID(doc, "radarTable")
	if table == nil {
		table = findHTMLByID(doc, "radar")
	}
	if table == nil {
		return nil, fmt.Errorf("hot ETF source table not found")
	}
	seen := map[string]bool{}
	items := make([]hotETFPrecomputeItem, 0)
	for _, row := range collectHTMLRows(table) {
		cells := collectHTMLCells(row)
		if len(cells) == 0 {
			continue
		}
		matches := hotETFCodePattern.FindStringSubmatch(cells[0])
		if len(matches) < 2 || seen[matches[1]] {
			continue
		}
		seen[matches[1]] = true
		items = append(items, hotETFPrecomputeItem{Code: matches[1], StockType: 2})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })
	return items, nil
}

func (s *HotETFPrecomputer) resolvePredictionDate(ctx context.Context) (string, error) {
	now := time.Now().In(s.location)
	if s.historyBaseURL == "" {
		return now.Format("2006-01-02"), nil
	}
	for offset := 0; offset <= 14; offset++ {
		candidate := now.AddDate(0, 0, offset)
		date := candidate.Format("20060102")
		query := url.Values{"date": []string{date}}
		status, data, err := s.getHistoryData(ctx, "/api/v1/trading-day", query)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK && boolValue(data["is_trading_day"]) {
			return candidate.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("no trading day found within 14 days")
}

func (s *HotETFPrecomputer) getHistoryData(ctx context.Context, path string, query url.Values) (int, map[string]any, error) {
	requestURL := s.historyBaseURL + path + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("X-Token", s.postgresToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	data := map[string]any{}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &data); err != nil {
			return resp.StatusCode, nil, err
		}
	}
	return resp.StatusCode, data, nil
}

func findHTMLByID(node *html.Node, id string) *html.Node {
	if node == nil {
		return nil
	}
	for _, attr := range node.Attr {
		if attr.Key == "id" && attr.Val == id {
			return node
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findHTMLByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func collectHTMLRows(node *html.Node) []*html.Node {
	rows := make([]*html.Node, 0)
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.ElementNode && current.Data == "tr" {
			rows = append(rows, current)
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return rows
}

func collectHTMLCells(row *html.Node) []string {
	cells := make([]string, 0)
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "td" {
			cells = append(cells, strings.Join(strings.Fields(htmlText(child)), " "))
		}
	}
	return cells
}

func htmlText(node *html.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == html.TextNode {
		return node.Data
	}
	var builder strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		builder.WriteString(htmlText(child))
		builder.WriteByte(' ')
	}
	return builder.String()
}

func hotETFStringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1"
	default:
		return false
	}
}

func hotETFIntValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}
