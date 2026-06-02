package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"fintrack-api/models"
)

const (
	mtfAgentJobStatusQueued    = "queued"
	mtfAgentJobStatusRunning   = "running"
	mtfAgentJobStatusCompleted = "completed"
	mtfAgentJobStatusFailed    = "failed"
)

func (s *MTFAgentService) StartMessageJob(ctx context.Context, userID int, message string, aiConfig *models.AIModelConfig) (*models.MTFAgentMessageJobResponse, error) {
	if !IsAIModelConfigReady(aiConfig) {
		return nil, errors.New(AIModelConfigRequiredMsg)
	}
	cleanMessage := strings.TrimSpace(message)
	if cleanMessage == "" {
		return nil, errors.New("message is required")
	}
	if s == nil {
		return nil, errors.New("MTF Agent service is not configured")
	}
	s.ensureJobStore()

	jobID := newMTFAgentJobID()
	now := time.Now().UTC()
	job := &mtfAgentMessageJob{
		ID:        jobID,
		UserID:    userID,
		Status:    mtfAgentJobStatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.jobsMu.Lock()
	s.jobs[jobID] = job
	s.jobsMu.Unlock()

	copiedAIConfig := *aiConfig
	go s.runMessageJob(context.Background(), userID, cleanMessage, &copiedAIConfig, jobID)
	_ = ctx
	return &models.MTFAgentMessageJobResponse{JobID: jobID, Status: mtfAgentJobStatusQueued}, nil
}

func (s *MTFAgentService) GetMessageJob(userID int, jobID string) (*models.MTFAgentMessageJobStatusResponse, error) {
	if s == nil {
		return nil, errors.New("MTF Agent service is not configured")
	}
	s.jobsMu.RLock()
	job := s.jobs[strings.TrimSpace(jobID)]
	s.jobsMu.RUnlock()
	if job == nil || job.UserID != userID {
		return nil, errors.New("MTF Agent job not found")
	}
	return &models.MTFAgentMessageJobStatusResponse{
		JobID:    job.ID,
		Status:   job.Status,
		Response: job.Response,
		Error:    job.Error,
	}, nil
}

func (s *MTFAgentService) runMessageJob(ctx context.Context, userID int, message string, aiConfig *models.AIModelConfig, jobID string) {
	s.updateMessageJob(jobID, func(job *mtfAgentMessageJob) {
		job.Status = mtfAgentJobStatusRunning
	})

	runner := s.messageJobRunner
	if runner == nil {
		runner = s.SendMessage
	}
	response, err := runner(ctx, userID, message, aiConfig)
	if err != nil {
		s.updateMessageJob(jobID, func(job *mtfAgentMessageJob) {
			job.Status = mtfAgentJobStatusFailed
			job.Error = err.Error()
		})
		return
	}
	s.updateMessageJob(jobID, func(job *mtfAgentMessageJob) {
		job.Status = mtfAgentJobStatusCompleted
		job.Response = response
	})
}

func (s *MTFAgentService) updateMessageJob(jobID string, update func(*mtfAgentMessageJob)) {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	job := s.jobs[jobID]
	if job == nil {
		return
	}
	update(job)
	job.UpdatedAt = time.Now().UTC()
}

func (s *MTFAgentService) ensureJobStore() {
	s.jobsMu.Lock()
	defer s.jobsMu.Unlock()
	if s.jobs == nil {
		s.jobs = map[string]*mtfAgentMessageJob{}
	}
}

func newMTFAgentJobID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "job_" + strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
	}
	return "job_" + hex.EncodeToString(raw[:])
}
