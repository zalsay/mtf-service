package queue

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"ai-functions/internal/backend"
	"ai-functions/internal/models"
	"ai-functions/internal/store"
)

type backendState struct {
	endpoint backend.Endpoint
	inFlight int
}

type Scheduler struct {
	client   *backend.Client
	store    *store.RedisStore
	mu       sync.Mutex
	cond     *sync.Cond
	backends []*backendState
}

type EnqueueResult struct {
	Job           *models.Job
	Reused        bool
	QueuePosition int
}

func NewScheduler(client *backend.Client, redisStore *store.RedisStore, endpoints []backend.Endpoint) *Scheduler {
	states := make([]*backendState, 0, len(endpoints))
	for _, endpoint := range endpoints {
		states = append(states, &backendState{endpoint: endpoint})
	}
	scheduler := &Scheduler{
		client:   client,
		store:    redisStore,
		backends: states,
	}
	scheduler.cond = sync.NewCond(&scheduler.mu)
	return scheduler
}

func (s *Scheduler) Recover(ctx context.Context) error {
	return s.store.RecoverQueue(ctx)
}

func (s *Scheduler) Start(ctx context.Context) {
	go s.dispatchLoop(ctx)
}

func (s *Scheduler) Enqueue(ctx context.Context, payload []byte, request models.InferenceRequest, targetPath string) (*EnqueueResult, error) {
	requestKey, err := request.RequestKey()
	if err != nil {
		return nil, err
	}
	if targetPath == "" {
		targetPath = "/internal/predict_for_best_sync"
	}
	requestKey = targetPath + ":" + requestKey

	for {
		if existingJobID, err := s.store.GetRequestKeyJobID(ctx, requestKey); err != nil {
			return nil, err
		} else if existingJobID != "" {
			if request.ForceEnqueueEnabled() {
				if err := s.store.DeleteRequestKey(ctx, requestKey); err != nil {
					return nil, err
				}
				continue
			}
			existingJob, queuePosition, ok, getErr := s.GetJob(ctx, existingJobID)
			if getErr != nil {
				return nil, getErr
			}
			if ok && existingJob != nil {
				if existingJob.Status != models.JobFailed {
					return &EnqueueResult{
						Job:           existingJob,
						Reused:        true,
						QueuePosition: queuePosition,
					}, nil
				}
			}
			if err := s.store.DeleteRequestKey(ctx, requestKey); err != nil {
				return nil, err
			}
			continue
		}

		job := &models.Job{
			ID:                 newJobID(),
			JobKind:            models.JobKindInference,
			Status:             models.JobQueued,
			StockCode:          request.StockCode,
			PredictionType:     request.PredictionType(),
			CovariateSignature: request.CovariateSignature(),
			CurrentStage:       models.BackendRoleMain,
			TargetPath:         targetPath,
			RequestKey:         requestKey,
			RequestBody:        append([]byte(nil), payload...),
			ForceEnqueue:       request.ForceEnqueueEnabled(),
			QueuePriority:      normalizedQueuePriority(request),
			CreatedAt:          time.Now().UTC(),
		}

		claimed, err := s.store.ClaimRequestKey(ctx, requestKey, job.ID)
		if err != nil {
			return nil, err
		}
		if !claimed {
			continue
		}

		if err := s.store.EnqueueJob(ctx, job); err != nil {
			_ = s.store.DeleteRequestKey(ctx, requestKey)
			return nil, err
		}

		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
		return &EnqueueResult{
			Job:    cloneJob(job),
			Reused: false,
		}, nil
	}
}

func (s *Scheduler) EnqueueUZI(ctx context.Context, payload []byte, request models.UZIAnalyzeRequest, targetPath string) (*EnqueueResult, error) {
	requestKey, err := request.RequestKey()
	if err != nil {
		return nil, err
	}
	if targetPath == "" {
		targetPath = "/internal/analyze_sync"
	}
	requestKey = targetPath + ":" + requestKey

	for {
		if existingJobID, err := s.store.GetRequestKeyJobID(ctx, requestKey); err != nil {
			return nil, err
		} else if existingJobID != "" {
			if request.ForceEnqueueEnabled() {
				if err := s.store.DeleteRequestKey(ctx, requestKey); err != nil {
					return nil, err
				}
				continue
			}
			existingJob, queuePosition, ok, getErr := s.GetJob(ctx, existingJobID)
			if getErr != nil {
				return nil, getErr
			}
			if ok && existingJob != nil {
				if existingJob.Status != models.JobFailed {
					return &EnqueueResult{
						Job:           existingJob,
						Reused:        true,
						QueuePosition: queuePosition,
					}, nil
				}
			}
			if err := s.store.DeleteRequestKey(ctx, requestKey); err != nil {
				return nil, err
			}
			continue
		}

		job := &models.Job{
			ID:           newJobID(),
			JobKind:      models.JobKindUZI,
			Status:       models.JobQueued,
			StockCode:    strings.TrimSpace(request.Ticker),
			CurrentStage: models.BackendRoleUZI,
			TargetPath:   targetPath,
			RequestKey:   requestKey,
			RequestBody:  append([]byte(nil), payload...),
			ForceEnqueue: request.ForceEnqueueEnabled(),
			CreatedAt:    time.Now().UTC(),
		}

		claimed, err := s.store.ClaimRequestKey(ctx, requestKey, job.ID)
		if err != nil {
			return nil, err
		}
		if !claimed {
			continue
		}

		if err := s.store.EnqueueJob(ctx, job); err != nil {
			_ = s.store.DeleteRequestKey(ctx, requestKey)
			return nil, err
		}

		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
		return &EnqueueResult{
			Job:    cloneJob(job),
			Reused: false,
		}, nil
	}
}

func normalizedQueuePriority(request models.InferenceRequest) string {
	if request.BackgroundPriorityEnabled() {
		return models.JobQueuePriorityBackground
	}
	return ""
}

func (s *Scheduler) GetJob(ctx context.Context, id string) (*models.Job, int, bool, error) {
	job, err := s.store.GetJob(ctx, id)
	if err != nil {
		return nil, 0, false, err
	}
	if job == nil {
		return nil, 0, false, nil
	}
	queuePosition, err := s.store.QueuePosition(ctx, id)
	if err != nil {
		return nil, 0, false, err
	}
	return cloneJob(job), queuePosition, true, nil
}

func (s *Scheduler) Snapshot(ctx context.Context) (models.SchedulerSnapshot, error) {
	queueDepth, err := s.store.QueueDepth(ctx)
	if err != nil {
		return models.SchedulerSnapshot{}, err
	}
	statusCounts, err := s.store.StatusCounts(ctx)
	if err != nil {
		return models.SchedulerSnapshot{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	backends := make([]models.BackendSnapshot, 0, len(s.backends))
	for _, backendState := range s.backends {
		available := backendState.endpoint.Capacity - backendState.inFlight
		if available < 0 {
			available = 0
		}
		backends = append(backends, models.BackendSnapshot{
			Name:              backendState.endpoint.Name,
			Role:              backendState.endpoint.Role,
			URL:               backendState.endpoint.URL,
			Capacity:          backendState.endpoint.Capacity,
			InFlight:          backendState.inFlight,
			Available:         available,
			SupportsCov:       backendState.endpoint.SupportsCov,
			SupportsDirectCov: backendState.endpoint.SupportsDirectCov,
			SupportsNonCov:    backendState.endpoint.SupportsNonCov,
			SupportsUZI:       backendState.endpoint.SupportsUZI,
		})
	}

	return models.SchedulerSnapshot{
		QueueDepth: queueDepth,
		Backends:   backends,
		Jobs:       statusCounts,
	}, nil
}

func (s *Scheduler) CallBackendSync(ctx context.Context, targetPath string, payload []byte) (int, []byte, error) {
	s.mu.Lock()
	endpoints := make([]backend.Endpoint, 0, len(s.backends))
	for _, backendState := range s.backends {
		endpoints = append(endpoints, backendState.endpoint)
	}
	s.mu.Unlock()

	var lastErr error
	for _, endpoint := range endpoints {
		if !backendSupportsTargetPath(endpoint, targetPath) {
			continue
		}
		statusCode, body, err := s.client.Submit(ctx, endpoint, targetPath, append([]byte(nil), payload...))
		if err == nil {
			return statusCode, body, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return 0, nil, lastErr
	}
	return 0, nil, fmt.Errorf("no backend configured")
}

func backendSupportsTargetPath(endpoint backend.Endpoint, targetPath string) bool {
	if strings.HasPrefix(targetPath, "/internal/analyze") {
		return endpoint.SupportsUZI
	}
	if strings.HasPrefix(targetPath, "/internal/predict") {
		return !endpoint.SupportsUZI
	}
	return true
}

func (s *Scheduler) dispatchLoop(ctx context.Context) {
	for {
		s.mu.Lock()
		for !s.hasCapacityLocked() || !s.hasQueuedWork(ctx) {
			s.cond.Wait()
			if ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
		}

		jobID, err := s.store.PopQueuedJob(ctx)
		if err != nil || jobID == "" {
			s.mu.Unlock()
			continue
		}

		job, err := s.store.GetJob(ctx, jobID)
		if err != nil || job == nil {
			s.mu.Unlock()
			continue
		}

		s.ensureJobClassificationLocked(ctx, job)
		backendState := s.selectBackendForJobLocked(job)
		if backendState == nil {
			s.mu.Unlock()
			_ = s.store.EnqueueJob(ctx, job)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		now := time.Now().UTC()
		job.Status = models.JobRunning
		job.StartedAt = &now
		job.Backend = backendState.endpoint.Name
		if err := s.store.SaveJob(ctx, job); err != nil {
			s.mu.Unlock()
			continue
		}

		backendState.inFlight++
		s.mu.Unlock()

		go s.runJob(ctx, jobID, backendState)
	}
}

func (s *Scheduler) runJob(ctx context.Context, jobID string, backendState *backendState) {
	job, err := s.store.GetJob(ctx, jobID)
	if err != nil || job == nil {
		s.finishBackendSlot(backendState)
		return
	}
	if s.shouldOrchestrateCovJob(job, backendState.endpoint) {
		s.runOrchestratedCovJob(ctx, job, backendState)
		return
	}

	statusCode, body, submitErr := s.client.Submit(
		ctx,
		backendState.endpoint,
		job.TargetPath,
		append([]byte(nil), job.RequestBody...),
	)

	job, err = s.store.GetJob(ctx, jobID)
	if err == nil && job != nil {
		now := time.Now().UTC()
		job.FinishedAt = &now
		job.UpstreamStatus = statusCode
		if len(body) > 0 {
			job.ResultBody = append([]byte(nil), body...)
		}
		if submitErr != nil {
			job.Status = models.JobFailed
			job.Error = submitErr.Error()
		} else {
			job.Status, job.Error = classifyUpstream(statusCode, body)
		}
		_ = s.store.SaveJob(ctx, job)
	}

	s.finishBackendSlot(backendState)
}

func (s *Scheduler) shouldOrchestrateCovJob(job *models.Job, endpoint backend.Endpoint) bool {
	if job == nil {
		return false
	}
	if !models.PredictionTypeUsesCovariates(job.PredictionType) {
		return false
	}
	if endpoint.SupportsDirectCov {
		return false
	}
	if _, _, ok := covOrchestrationPaths(job); !ok {
		return false
	}
	return s.hasBackendRole(models.BackendRoleXReg)
}

func covOrchestrationPaths(job *models.Job) (string, string, bool) {
	if job == nil {
		return "", "", false
	}
	switch job.TargetPath {
	case "/internal/predict_once_sync":
		return "/internal/predict_once_main_sync", "/internal/predict_once_cov_finalize_sync", true
	case "/internal/predict_for_best_sync":
		return "/internal/predict_for_best_main_sync", "/internal/predict_for_best_cov_finalize_sync", true
	default:
		return "", "", false
	}
}

func (s *Scheduler) runOrchestratedCovJob(ctx context.Context, job *models.Job, mainBackend *backendState) {
	mainReleased := false
	releaseMain := func() {
		if !mainReleased {
			s.finishBackendSlot(mainBackend)
			mainReleased = true
		}
	}

	mainPath, finalizePath, ok := covOrchestrationPaths(job)
	if !ok {
		releaseMain()
		s.completeJob(job.ID, 0, nil, fmt.Errorf("unsupported mtf-pro orchestration target: %s", job.TargetPath))
		return
	}

	statusCode, body, submitErr := s.client.Submit(
		ctx,
		mainBackend.endpoint,
		mainPath,
		append([]byte(nil), job.RequestBody...),
	)
	if submitErr != nil {
		releaseMain()
		s.completeJob(job.ID, statusCode, body, submitErr)
		return
	}

	stageName, stagePayload, err := parsePredictionStageResponse(body)
	if err != nil {
		releaseMain()
		s.completeJob(job.ID, statusCode, body, err)
		return
	}
	if stageName == "complete" {
		releaseMain()
		s.completeJob(job.ID, statusCode, body, nil)
		return
	}

	job, err = s.store.GetJob(ctx, job.ID)
	if err != nil || job == nil {
		releaseMain()
		return
	}
	job.CurrentStage = models.BackendRoleXReg
	job.Backend = ""
	_ = s.store.SaveJob(ctx, job)
	releaseMain()

	xregBackend := s.acquireBackendForStage(ctx, models.BackendRoleXReg, job)
	if xregBackend == nil {
		s.completeJob(job.ID, 0, nil, fmt.Errorf("no xreg backend available for mtf-pro finalize stage"))
		return
	}
	defer s.finishBackendSlot(xregBackend)

	job, err = s.store.GetJob(ctx, job.ID)
	if err == nil && job != nil {
		job.Backend = xregBackend.endpoint.Name
		_ = s.store.SaveJob(ctx, job)
	}

	finalizeStatusCode, finalizeBody, finalizeErr := s.client.Submit(
		ctx,
		xregBackend.endpoint,
		finalizePath,
		stagePayload,
	)
	if finalizeErr != nil {
		s.completeJob(job.ID, finalizeStatusCode, finalizeBody, finalizeErr)
		return
	}

	stageName, _, err = parsePredictionStageResponse(finalizeBody)
	if err != nil {
		s.completeJob(job.ID, finalizeStatusCode, finalizeBody, err)
		return
	}
	if stageName != "complete" {
		s.completeJob(job.ID, finalizeStatusCode, finalizeBody, fmt.Errorf("mtf-pro finalize stage returned no final result"))
		return
	}
	s.completeJob(job.ID, finalizeStatusCode, finalizeBody, nil)
}

func (s *Scheduler) finishBackendSlot(backendState *backendState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	backendState.inFlight--
	if backendState.inFlight < 0 {
		backendState.inFlight = 0
	}
	s.cond.Broadcast()
}

func (s *Scheduler) hasQueuedWork(ctx context.Context) bool {
	queueDepth, err := s.store.QueueDepth(ctx)
	if err != nil {
		return false
	}
	return queueDepth > 0
}

func (s *Scheduler) hasCapacityLocked() bool {
	for _, backendState := range s.backends {
		if backendState.inFlight < backendState.endpoint.Capacity {
			return true
		}
	}
	return false
}

func (s *Scheduler) selectBackendForJobLocked(job *models.Job) *backendState {
	for _, backendState := range s.backends {
		if backendState.inFlight < backendState.endpoint.Capacity && backendMatchesJob(backendState.endpoint, job) {
			return backendState
		}
	}
	return nil
}

func (s *Scheduler) ensureJobClassificationLocked(ctx context.Context, job *models.Job) {
	if job == nil {
		return
	}
	if job.JobKind == models.JobKindUZI {
		changed := false
		if job.CurrentStage == "" {
			job.CurrentStage = models.BackendRoleUZI
			changed = true
		}
		if changed {
			_ = s.store.SaveJob(ctx, job)
		}
		return
	}
	changed := false
	if job.PredictionType == "" && len(job.RequestBody) > 0 {
		var request models.InferenceRequest
		decoder := json.NewDecoder(bytes.NewReader(job.RequestBody))
		decoder.UseNumber()
		if err := decoder.Decode(&request); err == nil {
			job.PredictionType = request.PredictionType()
			job.CovariateSignature = request.CovariateSignature()
			changed = true
		}
	}
	if job.CurrentStage == "" {
		job.CurrentStage = models.BackendRoleMain
		changed = true
	}
	if changed {
		_ = s.store.SaveJob(ctx, job)
	}
}

func backendMatchesJob(endpoint backend.Endpoint, job *models.Job) bool {
	if job != nil && job.JobKind == models.JobKindUZI {
		endpointRole := endpoint.Role
		if endpointRole == "" {
			endpointRole = models.BackendRoleMain
		}
		return endpointRole == models.BackendRoleUZI && endpoint.SupportsUZI
	}

	requiredRole := job.CurrentStage
	if requiredRole == "" {
		requiredRole = models.BackendRoleMain
	}
	endpointRole := endpoint.Role
	if endpointRole == "" {
		endpointRole = models.BackendRoleMain
	}
	if endpointRole != requiredRole {
		return false
	}

	predictionType := models.NormalizePredictionType(job.PredictionType)
	if predictionType == "" {
		predictionType = models.PredictionTypeMTFLite
	}
	switch predictionType {
	case models.PredictionTypeMTFPro:
		return endpoint.SupportsCov
	case models.PredictionTypeMTFLite:
		return endpoint.SupportsNonCov
	default:
		return endpoint.SupportsCov || endpoint.SupportsNonCov
	}
}

func (s *Scheduler) hasBackendRole(role string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, backendState := range s.backends {
		endpointRole := backendState.endpoint.Role
		if endpointRole == "" {
			endpointRole = models.BackendRoleMain
		}
		if endpointRole == role {
			return true
		}
	}
	return false
}

func (s *Scheduler) acquireBackendForStage(ctx context.Context, stage string, job *models.Job) *backendState {
	for {
		if ctx.Err() != nil {
			return nil
		}
		s.mu.Lock()
		candidate := cloneJob(job)
		candidate.CurrentStage = stage
		backendState := s.selectBackendForJobLocked(candidate)
		if backendState != nil {
			backendState.inFlight++
			s.mu.Unlock()
			return backendState
		}
		s.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
}

func (s *Scheduler) completeJob(jobID string, statusCode int, body []byte, submitErr error) {
	job, err := s.store.GetJob(context.Background(), jobID)
	if err != nil || job == nil {
		return
	}
	now := time.Now().UTC()
	job.FinishedAt = &now
	job.UpstreamStatus = statusCode
	job.CurrentStage = ""
	if len(body) > 0 {
		job.ResultBody = append([]byte(nil), body...)
	}
	if submitErr != nil {
		job.Status = models.JobFailed
		job.Error = submitErr.Error()
	} else {
		job.Status, job.Error = classifyUpstream(statusCode, body)
	}
	_ = s.store.SaveJob(context.Background(), job)
}

func parsePredictionStageResponse(body []byte) (string, []byte, error) {
	var payload struct {
		Success bool            `json:"success"`
		Stage   string          `json:"stage"`
		Result  json.RawMessage `json:"result"`
		Payload json.RawMessage `json:"payload"`
		Error   string          `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", nil, fmt.Errorf("parse stage response: %w", err)
	}
	if !payload.Success {
		message := strings.TrimSpace(payload.Error)
		if message == "" {
			message = "stage response returned success=false"
		}
		return "", nil, fmt.Errorf(message)
	}
	switch payload.Stage {
	case "complete":
		return "complete", nil, nil
	case "main":
		if len(payload.Payload) == 0 {
			return "", nil, fmt.Errorf("main stage response missing payload")
		}
		return "main", append([]byte(nil), payload.Payload...), nil
	case "":
		return "complete", nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported stage response: %s", payload.Stage)
	}
}

func classifyUpstream(statusCode int, body []byte) (models.JobStatus, string) {
	if statusCode < 200 || statusCode >= 300 {
		return models.JobFailed, fmt.Sprintf("upstream returned status %d", statusCode)
	}

	var payload struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return models.JobSucceeded, ""
	}
	if payload.Success {
		return models.JobSucceeded, ""
	}
	if payload.Error != "" {
		return models.JobFailed, payload.Error
	}
	return models.JobFailed, "upstream reported unsuccessful result"
}

func cloneJob(job *models.Job) *models.Job {
	if job == nil {
		return nil
	}
	clone := *job
	if job.RequestBody != nil {
		clone.RequestBody = append([]byte(nil), job.RequestBody...)
	}
	if job.ResultBody != nil {
		clone.ResultBody = append([]byte(nil), job.ResultBody...)
	}
	return &clone
}

func newJobID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return "job-" + hex.EncodeToString(buf)
}
