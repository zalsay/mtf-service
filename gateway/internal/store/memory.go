package store

import (
	"context"
	"errors"
	"sync"

	"ai-functions/internal/models"
)

var errMemoryStoreClosed = errors.New("memory store is closed")

// MemoryStore keeps gateway jobs in process memory. It is intended for local
// or single-process deployments where losing queued jobs on restart is okay.
type MemoryStore struct {
	mu              sync.Mutex
	closed          bool
	jobs            map[string]*models.Job
	requestKeys     map[string]string
	normalQueue     []string
	backgroundQueue []string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:        make(map[string]*models.Job),
		requestKeys: make(map[string]string),
	}
}

func (s *MemoryStore) Ping(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errMemoryStoreClosed
	}
	return nil
}

func (s *MemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *MemoryStore) checkLocked() error {
	if s.closed {
		return errMemoryStoreClosed
	}
	return nil
}

func (s *MemoryStore) SaveJob(_ context.Context, job *models.Job) error {
	if job == nil {
		return errors.New("job is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	s.jobs[job.ID] = cloneJob(job)
	return nil
}

func (s *MemoryStore) EnqueueJob(_ context.Context, job *models.Job) error {
	if job == nil {
		return errors.New("job is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	return s.enqueueJobLocked(job, false)
}

func (s *MemoryStore) EnqueueJobIfAbsent(_ context.Context, job *models.Job) (bool, error) {
	if job == nil {
		return false, errors.New("job is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return false, err
	}
	if job.RequestKey != "" {
		if _, exists := s.requestKeys[job.RequestKey]; exists {
			return false, nil
		}
	}
	if err := s.enqueueJobLocked(job, true); err != nil {
		return false, err
	}
	return true, nil
}

func (s *MemoryStore) enqueueJobAndBindRequestKey(job *models.Job) error {
	if job == nil {
		return errors.New("job is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	return s.enqueueJobLocked(job, true)
}

func (s *MemoryStore) enqueueJobLocked(job *models.Job, bindRequestKey bool) error {
	s.jobs[job.ID] = cloneJob(job)
	if bindRequestKey && job.RequestKey != "" {
		s.requestKeys[job.RequestKey] = job.ID
	}
	if job.QueuePriority == models.JobQueuePriorityBackground {
		s.backgroundQueue = append(s.backgroundQueue, job.ID)
	} else {
		s.normalQueue = append(s.normalQueue, job.ID)
	}
	return nil
}

func (s *MemoryStore) ClaimRequestKey(_ context.Context, requestKey, jobID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return false, err
	}
	if _, exists := s.requestKeys[requestKey]; exists {
		return false, nil
	}
	s.requestKeys[requestKey] = jobID
	return true, nil
}

func (s *MemoryStore) setRequestKey(requestKey, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	s.requestKeys[requestKey] = jobID
	return nil
}

func (s *MemoryStore) GetRequestKeyJobID(_ context.Context, requestKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return "", err
	}
	return s.requestKeys[requestKey], nil
}

func (s *MemoryStore) DeleteRequestKey(_ context.Context, requestKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	delete(s.requestKeys, requestKey)
	return nil
}

func (s *MemoryStore) DeleteRequestKeyIfMatches(_ context.Context, requestKey, jobID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return false, err
	}
	return s.deleteRequestKeyIfMatchesLocked(requestKey, jobID), nil
}

func (s *MemoryStore) deleteRequestKeyIfMatches(requestKey, jobID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return false, err
	}
	return s.deleteRequestKeyIfMatchesLocked(requestKey, jobID), nil
}

func (s *MemoryStore) deleteRequestKeyIfMatchesLocked(requestKey, jobID string) bool {
	if currentJobID, exists := s.requestKeys[requestKey]; !exists || currentJobID != jobID {
		return false
	}
	delete(s.requestKeys, requestKey)
	return true
}

func (s *MemoryStore) GetJob(_ context.Context, id string) (*models.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return nil, err
	}
	return cloneJob(s.jobs[id]), nil
}

func (s *MemoryStore) LoadAllJobs(_ context.Context) ([]*models.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return nil, err
	}
	jobs := make([]*models.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, cloneJob(job))
	}
	return jobs, nil
}

func (s *MemoryStore) PopQueuedJob(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return "", err
	}
	if len(s.normalQueue) > 0 {
		jobID := s.normalQueue[0]
		s.normalQueue = s.normalQueue[1:]
		return jobID, nil
	}
	if len(s.backgroundQueue) > 0 {
		jobID := s.backgroundQueue[0]
		s.backgroundQueue = s.backgroundQueue[1:]
		return jobID, nil
	}
	return "", nil
}

func (s *MemoryStore) QueueDepth(_ context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return 0, err
	}
	return len(s.normalQueue) + len(s.backgroundQueue), nil
}

func (s *MemoryStore) QueuePosition(_ context.Context, jobID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return 0, err
	}
	for index, queuedID := range s.normalQueue {
		if queuedID == jobID {
			return index + 1, nil
		}
	}
	offset := len(s.normalQueue)
	for index, queuedID := range s.backgroundQueue {
		if queuedID == jobID {
			return offset + index + 1, nil
		}
	}
	return 0, nil
}

func (s *MemoryStore) RecoverQueue(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	jJobList := make([]*models.Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jJobList = append(jJobList, cloneJob(job))
	}
	recoverable, normalized := normalizeRecoveredJobs(jJobList)
	s.normalQueue = nil
	s.backgroundQueue = nil
	s.requestKeys = make(map[string]string)
	for _, job := range normalized {
		s.jobs[job.ID] = cloneJob(job)
		if job.RequestKey != "" {
			s.requestKeys[job.RequestKey] = job.ID
		}
	}
	for _, job := range recoverable {
		if job.QueuePriority == models.JobQueuePriorityBackground {
			s.backgroundQueue = append(s.backgroundQueue, job.ID)
		} else {
			s.normalQueue = append(s.normalQueue, job.ID)
		}
	}
	return nil
}

func (s *MemoryStore) StatusCounts(_ context.Context) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return nil, err
	}
	counts := emptyStatusCounts()
	for _, job := range s.jobs {
		counts[string(job.Status)]++
	}
	return counts, nil
}

func (s *MemoryStore) replaceJobs(jobs []*models.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkLocked(); err != nil {
		return err
	}
	s.jobs = make(map[string]*models.Job, len(jobs))
	s.requestKeys = make(map[string]string)
	s.normalQueue = nil
	s.backgroundQueue = nil
	for _, job := range jobs {
		if job == nil {
			continue
		}
		s.jobs[job.ID] = cloneJob(job)
		if job.RequestKey != "" {
			s.requestKeys[job.RequestKey] = job.ID
		}
		if job.Status != models.JobQueued {
			continue
		}
		if job.QueuePriority == models.JobQueuePriorityBackground {
			s.backgroundQueue = append(s.backgroundQueue, job.ID)
		} else {
			s.normalQueue = append(s.normalQueue, job.ID)
		}
	}
	return nil
}

func emptyStatusCounts() map[string]int {
	return map[string]int{
		string(models.JobQueued):    0,
		string(models.JobRunning):   0,
		string(models.JobSucceeded): 0,
		string(models.JobFailed):    0,
	}
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
