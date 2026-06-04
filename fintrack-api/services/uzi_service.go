package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"fintrack-api/config"
	"fintrack-api/database"
	"fintrack-api/models"

	"golang.org/x/net/html"
)

type UZIService struct {
	db         *database.DB
	config     *config.UZIServiceConfig
	oss        *OSSService
	openTokens *UZIOpenTokenService
	statusHub  *UZIStatusHub
}

type UZIUpstreamError struct {
	StatusCode int
	Body       map[string]interface{}
	Err        error
}

func (e *UZIUpstreamError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if message := firstNonEmpty(stringValue(e.Body["error"]), stringValue(e.Body["message"])); message != "" {
		return message
	}
	return fmt.Sprintf("uzi upstream returned status %d", e.StatusCode)
}

func NewUZIService(db *database.DB, cfg *config.Config) *UZIService {
	openTokenTTL := 45 * time.Second
	if cfg != nil && cfg.UZI.OpenTokenTTL > 0 {
		openTokenTTL = time.Duration(cfg.UZI.OpenTokenTTL) * time.Second
	}

	return &UZIService{
		db:         db,
		config:     &cfg.UZI,
		oss:        NewOSSService(cfg),
		openTokens: NewUZIOpenTokenService(openTokenTTL),
		statusHub:  NewUZIStatusHub(),
	}
}

func (s *UZIService) isConfigured() bool {
	return s != nil && s.config != nil && s.config.Enabled && strings.TrimSpace(s.config.BaseURL) != ""
}

func (s *UZIService) isQueueConfigured() bool {
	return s != nil && s.config != nil && s.config.Enabled && strings.TrimSpace(s.config.QueueBaseURL) != ""
}

func (s *UZIService) Health() (int, map[string]interface{}, error) {
	if !s.isConfigured() {
		return http.StatusServiceUnavailable, map[string]interface{}{
			"status": "disabled",
			"error":  "UZI service is disabled",
		}, nil
	}

	status, body, _, err := s.doJSONRequest(http.MethodGet, "/health", nil)
	return status, body, err
}

func (s *UZIService) OpenAnalyzeStream(req *models.UZIAnalyzeRequest, aiConfig *models.AIModelConfig) (*http.Response, error) {
	if !s.isConfigured() {
		return nil, errors.New("UZI service is not configured")
	}
	if !IsAIModelConfigReady(aiConfig) {
		return nil, errors.New(AIModelConfigRequiredMsg)
	}

	payload := map[string]interface{}{
		"ticker": strings.TrimSpace(req.Ticker),
		"ai_model": map[string]interface{}{
			"provider_name": strings.TrimSpace(aiConfig.ProviderName),
			"base_url":      strings.TrimRight(strings.TrimSpace(aiConfig.BaseURL), "/"),
			"api_key":       strings.TrimSpace(aiConfig.APIKey),
			"model_id":      strings.TrimSpace(aiConfig.ModelID),
		},
	}
	payload["depth"] = normalizeUZIDepth(req.Depth, "")
	if req.NoResume != nil {
		payload["no_resume"] = *req.NoResume
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal uzi analyze payload: %v", err)
	}

	resp, requestURL, err := s.doRawRequest(http.MethodPost, "/analyze", jsonData, "application/json")
	if err != nil {
		return nil, err
	}
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if resp.StatusCode != http.StatusOK || !strings.Contains(contentType, "text/event-stream") {
		defer resp.Body.Close()

		body, readErr := readGatewayJSONResponse(resp, requestURL, "decode uzi analyze response")
		if readErr != nil {
			return nil, &UZIUpstreamError{
				StatusCode: resp.StatusCode,
				Body:       map[string]interface{}{"error": readErr.Error()},
				Err:        readErr,
			}
		}
		return nil, &UZIUpstreamError{
			StatusCode: resp.StatusCode,
			Body:       body,
			Err:        errors.New(firstNonEmpty(stringValue(body["error"]), stringValue(body["message"]), fmt.Sprintf("uzi upstream returned status %d", resp.StatusCode))),
		}
	}

	return resp, nil
}

func (s *UZIService) EnqueueAnalyze(req *models.UZIAnalyzeRequest, aiConfig *models.AIModelConfig) (int, map[string]interface{}, error) {
	if !s.isQueueConfigured() {
		return 0, nil, errors.New("UZI gateway queue is not configured")
	}
	if !IsAIModelConfigReady(aiConfig) {
		return 0, nil, errors.New(AIModelConfigRequiredMsg)
	}

	payload := map[string]interface{}{
		"ticker": strings.TrimSpace(req.Ticker),
		"ai_model": map[string]interface{}{
			"provider_name": strings.TrimSpace(aiConfig.ProviderName),
			"base_url":      strings.TrimRight(strings.TrimSpace(aiConfig.BaseURL), "/"),
			"api_key":       strings.TrimSpace(aiConfig.APIKey),
			"model_id":      strings.TrimSpace(aiConfig.ModelID),
		},
	}
	payload["depth"] = normalizeUZIDepth(req.Depth, "medium")
	if req.NoResume != nil {
		payload["no_resume"] = *req.NoResume
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal uzi queue payload: %v", err)
	}
	status, body, _, err := s.doQueueJSONRequest(http.MethodPost, "/uzi/analyze", jsonData)
	if err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

func (s *UZIService) GetQueuedAnalyzeJob(jobID string) (int, map[string]interface{}, error) {
	if !s.isQueueConfigured() {
		return 0, nil, errors.New("UZI gateway queue is not configured")
	}
	cleanJobID := strings.TrimSpace(jobID)
	if cleanJobID == "" || strings.Contains(cleanJobID, "/") {
		return http.StatusBadRequest, map[string]interface{}{"error": "invalid job id"}, nil
	}
	status, body, _, err := s.doQueueJSONRequest(http.MethodGet, "/uzi/jobs/"+url.PathEscape(cleanJobID), nil)
	if err != nil {
		return 0, nil, err
	}
	return status, body, nil
}

func (s *UZIService) PersistAnalyzeResult(userID int, req *models.UZIAnalyzeRequest, body map[string]interface{}) (*models.UZIReportItem, error) {
	return s.persistAnalyzeResult(userID, req, body)
}

func (s *UZIService) TryStartAnalyze(userID int, ticker string) (models.UZIAnalyzeStatus, bool) {
	if s == nil || s.statusHub == nil {
		return models.UZIAnalyzeStatus{}, true
	}
	return s.statusHub.TryStart(userID, strings.TrimSpace(ticker))
}

func (s *UZIService) UpdateAnalyzeStatus(userID int, status models.UZIAnalyzeStatus) models.UZIAnalyzeStatus {
	if s == nil || s.statusHub == nil {
		return status
	}
	return s.statusHub.Update(userID, status)
}

func (s *UZIService) GetAnalyzeStatus(userID int) models.UZIAnalyzeStatus {
	if s == nil || s.statusHub == nil {
		return idleUZIAnalyzeStatus()
	}
	return s.statusHub.Get(userID)
}

func (s *UZIService) SubscribeAnalyzeStatus(userID int) (<-chan models.UZIAnalyzeStatus, func()) {
	if s == nil || s.statusHub == nil {
		hub := NewUZIStatusHub()
		return hub.Subscribe(userID)
	}
	return s.statusHub.Subscribe(userID)
}

func (s *UZIService) ListReports(userID int, ticker string) (int, *models.UZIReportListResponse, error) {
	rows, err := s.db.Conn.Query(`
		SELECT
			id,
			ticker,
			depth,
			status,
			directory_name,
			date_tag,
			report_relative_path,
			report_url,
			size_bytes,
			created_at,
			updated_at
		FROM uzi_reports
		WHERE user_id = $1
		  AND deleted_at IS NULL
		  AND ($2 = '' OR UPPER(ticker) = UPPER($2))
		ORDER BY updated_at DESC, id DESC
	`, userID, strings.TrimSpace(ticker))
	if err != nil {
		return 0, nil, fmt.Errorf("query uzi reports: %v", err)
	}
	defer rows.Close()

	items := make([]models.UZIReportItem, 0)
	for rows.Next() {
		var (
			item      models.UZIReportItem
			depth     sql.NullString
			createdAt time.Time
			updatedAt time.Time
		)
		if err := rows.Scan(
			&item.ID,
			&item.Ticker,
			&depth,
			&item.Status,
			&item.DirectoryName,
			&item.DateTag,
			&item.ReportRelativePath,
			&item.ReportURL,
			&item.SizeBytes,
			&createdAt,
			&updatedAt,
		); err != nil {
			return 0, nil, fmt.Errorf("scan uzi reports: %v", err)
		}
		if depth.Valid {
			item.Depth = stringPtr(depth.String)
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("iterate uzi reports: %v", err)
	}

	return http.StatusOK, &models.UZIReportListResponse{
		Items: items,
		Count: len(items),
	}, nil
}

func (s *UZIService) DeleteReport(userID int, relativePath string) (int, map[string]interface{}, error) {
	if !s.isConfigured() {
		return 0, nil, errors.New("UZI service is not configured")
	}

	cleanedPath, err := normalizeUZIReportPath(relativePath)
	if err != nil {
		return http.StatusBadRequest, map[string]interface{}{"error": err.Error()}, nil
	}

	reportRef, err := s.lookupActiveReportRef(userID, cleanedPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return http.StatusNotFound, map[string]interface{}{"error": "report not found"}, nil
		}
		return 0, nil, fmt.Errorf("find uzi report: %v", err)
	}

	query := url.Values{}
	query.Set("relative_path", cleanedPath)
	status, body, _, err := s.doJSONRequest(http.MethodDelete, "/reports-entry?"+query.Encode(), nil)
	if err != nil {
		return 0, nil, err
	}

	readErr := error(nil)
	if body == nil {
		readErr = errors.New("empty response body")
	}
	if readErr != nil {
		if status != http.StatusNotFound {
			return status, nil, readErr
		}
		body = map[string]interface{}{
			"success":           true,
			"deleted_path":      cleanedPath,
			"deleted_directory": path.Dir(cleanedPath),
			"warning":           "report file already missing in UZI runtime, database entry was still removed",
		}
	}

	if s.oss != nil && s.oss.IsManagedObjectKey(reportRef.ReportURL) {
		if deleteErr := s.oss.DeleteObject(context.Background(), reportRef.ReportURL); deleteErr != nil {
			if !isOSSObjectMissing(deleteErr) {
				return 0, nil, deleteErr
			}
			body["warning"] = "report object already missing in OSS, database entry was still removed"
		}
	}

	if err := s.softDeleteReport(reportRef.ID); err != nil {
		return 0, nil, fmt.Errorf("soft delete uzi report: %v", err)
	}
	return http.StatusOK, body, nil
}

func (s *UZIService) FetchReport(userID int, reportPath string) (*http.Response, error) {
	if !s.isConfigured() {
		return nil, errors.New("UZI service is not configured")
	}

	cleanedPath, err := normalizeUZIReportPath(reportPath)
	if err != nil {
		return nil, err
	}

	if _, err := s.lookupActiveReportRef(userID, cleanedPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("report not found")
		}
		return nil, fmt.Errorf("check report access: %v", err)
	}

	return s.fetchReportFromUZI(cleanedPath)
}

func (s *UZIService) CreateReportOpenToken(userID int, relativePath string) (*models.UZIReportOpenTokenResponse, error) {
	cleanedPath, err := normalizeUZIReportPath(relativePath)
	if err != nil {
		return nil, &HTTPError{StatusCode: http.StatusBadRequest, Message: err.Error()}
	}

	record, err := s.lookupActiveReportRef(userID, cleanedPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &HTTPError{StatusCode: http.StatusNotFound, Message: "report not found"}
		}
		return nil, fmt.Errorf("lookup uzi report: %w", err)
	}

	if s.shouldBackfillManagedReportURL(record.ReportURL) {
		managedReportURL, err := s.backfillManagedReportURL(userID, cleanedPath, record)
		if err != nil {
			return nil, fmt.Errorf("backfill managed report url: %w", err)
		}
		record.ReportURL = managedReportURL
	}

	token, expiresAt, err := s.openTokens.Create(userID, cleanedPath)
	if err != nil {
		return nil, fmt.Errorf("create report open token: %w", err)
	}

	expiresIn := int(time.Until(expiresAt).Seconds())
	if expiresIn < 1 {
		expiresIn = 1
	}

	var signedURL string
	if s.oss != nil && s.oss.IsManagedObjectKey(record.ReportURL) {
		objectKeyForOpen := record.ReportURL
		if strings.Contains(strings.ToLower(path.Ext(cleanedPath)), "html") {
			objectKeyForOpen, err = s.prepareSignedReportHTMLObject(context.Background(), userID, cleanedPath, record.ReportURL, token)
			if err != nil {
				return nil, fmt.Errorf("prepare signed report html: %w", err)
			}
		}

		signedURL, err = s.presignedReportObjectURL(context.Background(), objectKeyForOpen)
		if err != nil {
			return nil, fmt.Errorf("presign report url: %w", err)
		}
	}

	openURL, err := s.buildReportOpenURL(token, signedURL)
	if err != nil {
		return nil, err
	}

	response := &models.UZIReportOpenTokenResponse{
		OpenToken: token,
		ExpiresIn: expiresIn,
		OpenURL:   openURL,
	}

	log.Printf(
		"[uzi] reports-open-token user_id=%d relative_path=%q cleaned_path=%q report_url=%q has_public_base=%t signed_url_present=%t expires_in=%d",
		userID,
		relativePath,
		cleanedPath,
		record.ReportURL,
		s.oss != nil && s.oss.HasPublicBaseURL(),
		strings.TrimSpace(signedURL) != "",
		response.ExpiresIn,
	)

	return response, nil
}

func (s *UZIService) buildReportOpenURL(token string, signedURL string) (string, error) {
	if strings.TrimSpace(signedURL) != "" {
		return strings.TrimSpace(signedURL), nil
	}
	return "/api/v1/uzi/report-open?token=" + url.QueryEscape(strings.TrimSpace(token)), nil
}

func (s *UZIService) shouldBackfillManagedReportURL(reportURL string) bool {
	return s != nil &&
		s.oss != nil &&
		s.oss.Enabled() &&
		strings.TrimSpace(reportURL) != "" &&
		!s.oss.IsManagedObjectKey(reportURL)
}

func (s *UZIService) backfillManagedReportURL(userID int, relativePath string, record *uziActiveReportRef) (string, error) {
	reportBody, contentType, err := s.fetchReportContent(relativePath)
	if err != nil {
		return "", fmt.Errorf("fetch legacy report content from uzi: %w", err)
	}

	if strings.Contains(strings.ToLower(contentType), "text/html") {
		if _, err := s.uploadReferencedReportAssets(context.Background(), userID, relativePath, reportBody); err != nil {
			return "", fmt.Errorf("upload legacy report assets to oss: %w", err)
		}
	}

	objectKey, _, err := s.oss.UploadHTML(context.Background(), userID, relativePath, reportBody, contentType)
	if err != nil {
		return "", fmt.Errorf("upload legacy report to oss: %w", err)
	}

	if err := s.updateReportURL(record.ID, objectKey); err != nil {
		return "", fmt.Errorf("update report_url to managed object key: %w", err)
	}

	log.Printf(
		"[uzi] reports-open-token backfilled legacy report id=%d user_id=%d relative_path=%q old_report_url=%q new_report_url=%q",
		record.ID,
		userID,
		relativePath,
		record.ReportURL,
		objectKey,
	)

	return objectKey, nil
}

func (s *UZIService) ResolveReportOpen(token string) (string, *http.Response, error) {
	userID, relativePath, err := s.openTokens.Consume(strings.TrimSpace(token))
	if err != nil {
		if errors.Is(err, ErrUZIOpenTokenExpired) {
			return "", nil, &HTTPError{StatusCode: http.StatusGone, Message: "report open token has expired"}
		}
		if errors.Is(err, ErrUZIOpenTokenInvalid) {
			return "", nil, &HTTPError{StatusCode: http.StatusUnauthorized, Message: "report open token is invalid"}
		}
		return "", nil, fmt.Errorf("consume report open token: %w", err)
	}

	record, err := s.lookupActiveReportRef(userID, relativePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, &HTTPError{StatusCode: http.StatusNotFound, Message: "report not found"}
		}
		return "", nil, fmt.Errorf("lookup report by open token: %w", err)
	}

	if s.oss != nil && s.oss.IsManagedObjectKey(record.ReportURL) {
		objectKeyForOpen := record.ReportURL
		if strings.Contains(strings.ToLower(path.Ext(relativePath)), "html") {
			objectKeyForOpen, err = s.prepareSignedReportHTMLObject(context.Background(), userID, relativePath, record.ReportURL, strings.TrimSpace(token))
			if err != nil {
				return "", nil, fmt.Errorf("prepare signed report html: %w", err)
			}
		}

		signedURL, err := s.presignedReportObjectURL(context.Background(), objectKeyForOpen)
		if err != nil {
			return "", nil, fmt.Errorf("presign report url: %w", err)
		}
		return signedURL, nil, nil
	}

	resp, err := s.fetchReportFromUZI(relativePath)
	if err != nil {
		return "", nil, err
	}
	return "", resp, nil
}

func (s *UZIService) presignedReportObjectURL(ctx context.Context, objectKey string) (string, error) {
	signedURL, err := s.oss.PresignGetObjectURL(ctx, objectKey)
	if err != nil {
		return "", err
	}
	if s.oss.HasPublicBaseURL() {
		return s.oss.RewriteSignedURLToPublicBase(signedURL)
	}
	return signedURL, nil
}

func (s *UZIService) fetchReportFromUZI(cleanedPath string) (*http.Response, error) {
	resp, _, err := s.doRawRequest(http.MethodGet, "/reports/"+escapeURLPath(cleanedPath), nil, "")
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *UZIService) fetchReportFromSignedURL(reportURL string) (*http.Response, error) {
	client := newInferenceGatewayHTTPClient(s.config.Timeout)

	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(reportURL), nil)
	if err != nil {
		return nil, fmt.Errorf("create oss get request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch oss report: %v", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("fetch oss report failed with status %d and unreadable body: %v", resp.StatusCode, readErr)
		}
		return nil, fmt.Errorf("fetch oss report failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp, nil
}

func (s *UZIService) doJSONRequest(method string, endpoint string, payload []byte) (int, map[string]interface{}, string, error) {
	resp, requestURL, err := s.doRawRequest(method, endpoint, payload, "application/json")
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()

	body, readErr := readGatewayJSONResponse(resp, requestURL, "decode uzi response")
	if readErr != nil {
		return resp.StatusCode, nil, requestURL, readErr
	}
	return resp.StatusCode, body, requestURL, nil
}

func (s *UZIService) doQueueJSONRequest(method string, endpoint string, payload []byte) (int, map[string]interface{}, string, error) {
	resp, requestURL, err := s.doRawQueueRequest(method, endpoint, payload, "application/json")
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()

	body, readErr := readGatewayJSONResponse(resp, requestURL, "decode uzi queue response")
	if readErr != nil {
		return resp.StatusCode, nil, requestURL, readErr
	}
	return resp.StatusCode, body, requestURL, nil
}

func (s *UZIService) doRawRequest(method string, endpoint string, payload []byte, contentType string) (*http.Response, string, error) {
	client := newInferenceGatewayHTTPClient(s.config.Timeout)
	attemptErrors := make([]string, 0)

	for _, baseURL := range s.baseURLCandidates() {
		requestURL := strings.TrimRight(baseURL, "/") + endpoint
		req, err := http.NewRequest(method, requestURL, bytes.NewReader(payload))
		if err != nil {
			return nil, "", fmt.Errorf("create uzi %s request: %v", strings.ToLower(method), err)
		}
		if payload != nil && contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}

		resp, err := client.Do(req)
		if err == nil {
			return resp, requestURL, nil
		}
		attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", requestURL, err))
	}

	return nil, "", fmt.Errorf("call uzi %s failed: %s", strings.ToLower(method), strings.Join(attemptErrors, " | "))
}

func (s *UZIService) doRawQueueRequest(method string, endpoint string, payload []byte, contentType string) (*http.Response, string, error) {
	client := newInferenceGatewayHTTPClient(s.config.Timeout)
	attemptErrors := make([]string, 0)

	for _, baseURL := range s.queueBaseURLCandidates() {
		requestURL := strings.TrimRight(baseURL, "/") + endpoint
		req, err := http.NewRequest(method, requestURL, bytes.NewReader(payload))
		if err != nil {
			return nil, "", fmt.Errorf("create uzi queue %s request: %v", strings.ToLower(method), err)
		}
		if payload != nil && contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}

		resp, err := client.Do(req)
		if err == nil {
			return resp, requestURL, nil
		}
		attemptErrors = append(attemptErrors, fmt.Sprintf("%s: %v", requestURL, err))
	}

	return nil, "", fmt.Errorf("call uzi queue %s failed: %s", strings.ToLower(method), strings.Join(attemptErrors, " | "))
}

func (s *UZIService) baseURLCandidates() []string {
	primary := strings.TrimSpace(s.config.BaseURL)
	if primary == "" {
		return nil
	}

	values := []string{primary}
	parsed, err := url.Parse(primary)
	if err != nil {
		return values
	}

	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}

	addCandidate := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range values {
			if strings.EqualFold(existing, raw) {
				return
			}
		}
		values = append(values, raw)
	}

	switch strings.ToLower(parsed.Hostname()) {
	case "host.docker.internal", "127.0.0.1", "localhost":
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59011", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59011", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59011", scheme))
		addCandidate(fmt.Sprintf("%s://ai-functions-uzi:9011", scheme))
	case "ai-functions-uzi":
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59011", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59011", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59011", scheme))
	default:
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59011", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59011", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59011", scheme))
		addCandidate(fmt.Sprintf("%s://ai-functions-uzi:9011", scheme))
	}

	return values
}

func (s *UZIService) queueBaseURLCandidates() []string {
	primary := strings.TrimSpace(s.config.QueueBaseURL)
	if primary == "" {
		return nil
	}

	values := []string{primary}
	parsed, err := url.Parse(primary)
	if err != nil {
		return values
	}

	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}

	addCandidate := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range values {
			if strings.EqualFold(existing, raw) {
				return
			}
		}
		values = append(values, raw)
	}

	switch strings.ToLower(parsed.Hostname()) {
	case "host.docker.internal", "127.0.0.1", "localhost":
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59010", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59010", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59010", scheme))
		addCandidate(fmt.Sprintf("%s://ai-functions-gateway:9010", scheme))
	case "ai-functions-gateway":
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59010", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59010", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59010", scheme))
	default:
		addCandidate(fmt.Sprintf("%s://127.0.0.1:59010", scheme))
		addCandidate(fmt.Sprintf("%s://localhost:59010", scheme))
		addCandidate(fmt.Sprintf("%s://host.docker.internal:59010", scheme))
		addCandidate(fmt.Sprintf("%s://ai-functions-gateway:9010", scheme))
	}

	return values
}

func (s *UZIService) persistAnalyzeResult(userID int, req *models.UZIAnalyzeRequest, body map[string]interface{}) (*models.UZIReportItem, error) {
	statusText := strings.ToLower(strings.TrimSpace(stringValue(body["status"])))
	if statusText == "" || (statusText != "succeeded" && statusText != "partial_success") {
		return nil, nil
	}

	reportMap, _ := body["report"].(map[string]interface{})
	reportRelativePath := strings.TrimSpace(stringValue(body["report_relative_path"]))
	if reportRelativePath == "" {
		reportRelativePath = strings.TrimSpace(stringValue(reportMap["report_relative_path"]))
	}
	if reportRelativePath == "" {
		return nil, nil
	}

	item := models.UZIReportItem{
		Ticker:             firstNonEmpty(stringValue(reportMap["ticker"]), strings.TrimSpace(req.Ticker)),
		DirectoryName:      stringValue(reportMap["directory_name"]),
		DateTag:            stringValue(reportMap["date_tag"]),
		ReportRelativePath: reportRelativePath,
		ReportURL:          firstNonEmpty(stringValue(body["report_url"]), stringValue(reportMap["report_url"])),
		SizeBytes:          int64Value(reportMap["size_bytes"]),
	}
	if item.Ticker == "" {
		item.Ticker = strings.TrimSpace(req.Ticker)
	}
	if item.DirectoryName == "" {
		item.DirectoryName = path.Dir(reportRelativePath)
	}
	if item.DateTag == "" {
		item.DateTag = inferDateTag(item.DirectoryName)
	}
	now := time.Now().UTC()
	item.CreatedAt = now.Format(time.RFC3339)
	item.UpdatedAt = firstNonEmpty(stringValue(reportMap["updated_at"]), now.Format(time.RFC3339))

	if s.oss != nil && s.oss.Enabled() {
		reportBody, contentType, err := s.fetchReportContent(reportRelativePath)
		if err != nil {
			return nil, fmt.Errorf("fetch report content from uzi: %v", err)
		}

		if strings.Contains(strings.ToLower(contentType), "text/html") {
			if _, err := s.uploadReferencedReportAssets(context.Background(), userID, reportRelativePath, reportBody); err != nil {
				return nil, fmt.Errorf("upload report assets to oss: %v", err)
			}
		}

		objectKey, sizeBytes, err := s.oss.UploadHTML(context.Background(), userID, reportRelativePath, reportBody, contentType)
		if err != nil {
			return nil, fmt.Errorf("upload report to oss: %v", err)
		}

		item.ReportURL = objectKey
		if item.SizeBytes <= 0 {
			item.SizeBytes = sizeBytes
		}
	}

	record, err := s.upsertReport(userID, req, body, &item)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func (s *UZIService) upsertReport(userID int, req *models.UZIAnalyzeRequest, body map[string]interface{}, item *models.UZIReportItem) (*models.UZIReportItem, error) {
	var (
		id        int
		createdAt time.Time
		updatedAt time.Time
		depth     sql.NullString
	)

	depthValue := normalizeUZIDepth(req.Depth, stringValue(body["depth"]))

	err := s.db.Conn.QueryRow(`
		INSERT INTO uzi_reports (
			user_id,
			ticker,
			depth,
			status,
			directory_name,
			date_tag,
			report_relative_path,
			report_url,
			size_bytes,
			exit_code,
			duration_seconds,
			stdout_tail,
			stderr_tail,
			created_at,
			updated_at,
			deleted_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, NULL
		)
		ON CONFLICT (user_id, report_relative_path) WHERE deleted_at IS NULL
		DO UPDATE SET
			ticker = EXCLUDED.ticker,
			depth = EXCLUDED.depth,
			status = EXCLUDED.status,
			directory_name = EXCLUDED.directory_name,
			date_tag = EXCLUDED.date_tag,
			report_url = EXCLUDED.report_url,
			size_bytes = EXCLUDED.size_bytes,
			exit_code = EXCLUDED.exit_code,
			duration_seconds = EXCLUDED.duration_seconds,
			stdout_tail = EXCLUDED.stdout_tail,
			stderr_tail = EXCLUDED.stderr_tail,
			updated_at = CURRENT_TIMESTAMP,
			deleted_at = NULL
		RETURNING id, depth, created_at, updated_at
	`,
		userID,
		item.Ticker,
		depthValue,
		firstNonEmpty(stringValue(body["status"]), "succeeded"),
		item.DirectoryName,
		item.DateTag,
		item.ReportRelativePath,
		item.ReportURL,
		item.SizeBytes,
		intValue(body["exit_code"]),
		float64Value(body["duration_seconds"]),
		nullableStringValue(body["stdout_tail"]),
		nullableStringValue(body["stderr_tail"]),
	).Scan(&id, &depth, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert uzi report: %v", err)
	}

	item.ID = id
	if depth.Valid {
		item.Depth = stringPtr(depth.String)
	}
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	item.Status = firstNonEmpty(stringValue(body["status"]), "succeeded")
	return item, nil
}

type uziActiveReportRef struct {
	ID        int
	ReportURL string
}

func (s *UZIService) lookupActiveReportRef(userID int, relativePath string) (*uziActiveReportRef, error) {
	ref := &uziActiveReportRef{}
	err := s.db.Conn.QueryRow(`
		SELECT id, report_url
		FROM uzi_reports
		WHERE user_id = $1
		  AND report_relative_path = $2
		  AND deleted_at IS NULL
		ORDER BY updated_at DESC, id DESC
		LIMIT 1
	`, userID, relativePath).Scan(&ref.ID, &ref.ReportURL)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func (s *UZIService) updateReportURL(id int, reportURL string) error {
	_, err := s.db.Conn.Exec(`
		UPDATE uzi_reports
		SET report_url = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id, reportURL)
	return err
}

func (s *UZIService) softDeleteReport(id int) error {
	_, err := s.db.Conn.Exec(`
		UPDATE uzi_reports
		SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
	`, id)
	return err
}

func (s *UZIService) uploadReferencedReportAssets(ctx context.Context, userID int, reportRelativePath string, htmlBody []byte) (int, error) {
	if s == nil || s.oss == nil || !s.oss.Enabled() {
		return 0, nil
	}

	assetPaths, err := localReportAssetPaths(reportRelativePath, htmlBody)
	if err != nil {
		return 0, err
	}

	uploaded := 0
	for _, assetPath := range assetPaths {
		body, contentType, err := s.fetchReportAssetContent(assetPath)
		if err != nil {
			return uploaded, fmt.Errorf("fetch report asset %q: %w", assetPath, err)
		}
		if _, _, err := s.oss.UploadObject(ctx, userID, assetPath, body, contentType, "private, max-age=86400"); err != nil {
			return uploaded, fmt.Errorf("upload report asset %q: %w", assetPath, err)
		}
		uploaded++
	}

	return uploaded, nil
}

func (s *UZIService) prepareSignedReportHTMLObject(ctx context.Context, userID int, reportRelativePath string, objectKey string, token string) (string, error) {
	if s == nil || s.oss == nil || !s.oss.Enabled() {
		return objectKey, nil
	}

	body, contentType, err := s.oss.GetObject(ctx, objectKey)
	if err != nil {
		return "", err
	}
	if !strings.Contains(strings.ToLower(contentType), "text/html") {
		return objectKey, nil
	}

	rewritten, changed, err := s.rewriteReportHTMLAssetURLs(ctx, userID, reportRelativePath, body)
	if err != nil {
		return "", err
	}
	if !changed {
		return objectKey, nil
	}

	tempRelativePath := path.Join(".open", strings.TrimSpace(token), path.Base(reportRelativePath))
	tempObjectKey, _, err := s.oss.UploadHTML(ctx, userID, tempRelativePath, rewritten, contentType)
	if err != nil {
		return "", err
	}
	return tempObjectKey, nil
}

func (s *UZIService) rewriteReportHTMLAssetURLs(ctx context.Context, userID int, reportRelativePath string, body []byte) ([]byte, bool, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("parse report html: %w", err)
	}

	changed := false
	changed = injectReportAlphaScoreUnitFix(doc) || changed
	var walk func(*html.Node) error
	walk = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			for idx := range n.Attr {
				key := strings.ToLower(n.Attr[idx].Key)
				if key != "src" && key != "href" {
					continue
				}
				assetPath, ok := resolveLocalReportAssetPath(reportRelativePath, n.Attr[idx].Val)
				if !ok {
					continue
				}
				objectKey, err := s.oss.BuildObjectKey(userID, assetPath)
				if err != nil {
					return err
				}
				signedURL, err := s.presignedReportObjectURL(ctx, objectKey)
				if err != nil {
					return fmt.Errorf("presign report asset %q: %w", assetPath, err)
				}
				n.Attr[idx].Val = signedURL
				changed = true
			}
		}

		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(doc); err != nil {
		return nil, false, err
	}
	if !changed {
		return body, false, nil
	}

	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return nil, false, fmt.Errorf("render signed report html: %w", err)
	}
	return out.Bytes(), true, nil
}

const reportAlphaScoreUnitFixMarker = "fintrack-alpha-score-unit-fix"

const reportAlphaScoreUnitFixCSS = `
/* fintrack-alpha-score-unit-fix: isolate /100 from giant score letter spacing. */
.score-giant::after {
  font-family: 'Fira Code', 'SFMono-Regular', Consolas, Menlo, monospace !important;
  letter-spacing: 0 !important;
  font-variant-numeric: tabular-nums !important;
  font-feature-settings: "tnum" 1, "kern" 1 !important;
  white-space: nowrap !important;
  display: inline-block !important;
  min-width: 4.2ch !important;
  line-height: 1 !important;
  text-align: left !important;
}
@media (max-width: 720px) {
  .score-giant::after {
    letter-spacing: 0 !important;
    min-width: 4.2ch !important;
  }
}
`

func injectReportAlphaScoreUnitFix(doc *html.Node) bool {
	if doc == nil || reportHTMLContains(doc, reportAlphaScoreUnitFixMarker) || !reportHTMLContains(doc, ".score-giant") {
		return false
	}

	head := findFirstHTMLElement(doc, "head")
	if head == nil {
		return false
	}

	style := &html.Node{
		Type: html.ElementNode,
		Data: "style",
	}
	style.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: reportAlphaScoreUnitFixCSS,
	})
	head.AppendChild(style)
	return true
}

func reportHTMLContains(n *html.Node, needle string) bool {
	if n == nil || needle == "" {
		return false
	}
	if strings.Contains(n.Data, needle) {
		return true
	}
	for _, attr := range n.Attr {
		if strings.Contains(attr.Val, needle) {
			return true
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if reportHTMLContains(child, needle) {
			return true
		}
	}
	return false
}

func findFirstHTMLElement(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && strings.EqualFold(n.Data, tag) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirstHTMLElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}

func normalizeUZIDepth(depth *string, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(fallback))
	if depth != nil && strings.TrimSpace(*depth) != "" {
		value = strings.ToLower(strings.TrimSpace(*depth))
	}
	switch value {
	case "lite", "medium", "deep":
		return value
	default:
		return "medium"
	}
}

func localReportAssetPaths(reportRelativePath string, body []byte) ([]string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse report html assets: %w", err)
	}

	seen := make(map[string]struct{})
	var paths []string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, attr := range n.Attr {
				key := strings.ToLower(attr.Key)
				if key != "src" && key != "href" {
					continue
				}
				assetPath, ok := resolveLocalReportAssetPath(reportRelativePath, attr.Val)
				if !ok {
					continue
				}
				if _, exists := seen[assetPath]; exists {
					continue
				}
				seen[assetPath] = struct{}{}
				paths = append(paths, assetPath)
			}
		}

		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	return paths, nil
}

func resolveLocalReportAssetPath(reportRelativePath string, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return "", false
	}

	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "javascript:") ||
		strings.HasPrefix(raw, "//") {
		return "", false
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Path == "" || strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}

	reportDir := reportDirectory(reportRelativePath)
	if reportDir == "" {
		return "", false
	}

	cleaned := strings.TrimPrefix(path.Clean(path.Join(reportDir, parsed.Path)), "/")
	if cleaned == "" || cleaned == "." || strings.Contains(cleaned, "..") {
		return "", false
	}
	if cleaned == strings.TrimPrefix(path.Clean(reportRelativePath), "/") {
		return "", false
	}
	if cleaned != reportDir && !strings.HasPrefix(cleaned, reportDir+"/") {
		return "", false
	}
	return cleaned, true
}

func (s *UZIService) fetchReportAssetContent(assetPath string) ([]byte, string, error) {
	resp, err := s.fetchReportFromUZI(assetPath)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("uzi returned status %d while fetching report asset", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read uzi report asset body: %v", err)
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = mime.TypeByExtension(path.Ext(assetPath))
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	return body, contentType, nil
}

func (s *UZIService) fetchReportContent(reportPath string) ([]byte, string, error) {
	resp, err := s.fetchReportFromUZI(reportPath)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("uzi returned status %d while fetching report", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read uzi report body: %v", err)
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/html; charset=utf-8"
	}
	return body, contentType, nil
}

func escapeURLPath(raw string) string {
	parts := strings.Split(raw, "/")
	for idx, part := range parts {
		parts[idx] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func inferDateTag(directoryName string) string {
	parts := strings.Split(strings.TrimSpace(directoryName), "_")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func nullableStringValue(value interface{}) interface{} {
	if text := strings.TrimSpace(stringValue(value)); text != "" {
		return text
	}
	return nil
}

func int64Value(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func intValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return int(parsed)
		}
	}
	return nil
}

func float64Value(value interface{}) interface{} {
	switch typed := value.(type) {
	case float32:
		return float64(typed)
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
	}
	return nil
}

func stringPtr(value string) *string {
	copied := value
	return &copied
}
