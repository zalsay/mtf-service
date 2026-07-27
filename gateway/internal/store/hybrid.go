package store

import (
	"context"
	"errors"
	"sort"

	"ai-functions/internal/models"
)

// HybridStore keeps a hot in-memory view and writes every mutation through to
// SQLite. Reads used by scheduling and status endpoints stay in memory, while
// the SQLite database remains the durable source across restarts.
type HybridStore struct {
	cache   *MemoryStore
	durable *SQLiteStore
}

func NewHybridStore(durable *SQLiteStore) (*HybridStore, error) {
	if durable == nil {
		return nil, errors.New("hybrid store requires sqlite store")
	}
	store := &HybridStore{
		cache:   NewMemoryStore(),
		durable: durable,
	}
	if err := store.hydrate(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *HybridStore) hydrate(ctx context.Context) error {
	jobs, err := s.durable.LoadAllJobs(ctx)
	if err != nil {
		return err
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		if jobs[i].CreatedAt.Equal(jobs[j].CreatedAt) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return s.cache.replaceJobs(jobs)
}

func (s *HybridStore) Ping(ctx context.Context) error {
	return s.durable.Ping(ctx)
}

func (s *HybridStore) Close() error {
	_ = s.cache.Close()
	return s.durable.Close()
}

func (s *HybridStore) SaveJob(ctx context.Context, job *models.Job) error {
	if err := s.durable.SaveJob(ctx, job); err != nil {
		return err
	}
	return s.cache.SaveJob(ctx, job)
}

func (s *HybridStore) EnqueueJob(ctx context.Context, job *models.Job) error {
	if err := s.durable.EnqueueJob(ctx, job); err != nil {
		return err
	}
	return s.cache.EnqueueJob(ctx, job)
}

func (s *HybridStore) EnqueueJobIfAbsent(ctx context.Context, job *models.Job) (bool, error) {
	created, err := s.durable.EnqueueJobIfAbsent(ctx, job)
	if err != nil || !created {
		return created, err
	}
	if err := s.cache.enqueueJobAndBindRequestKey(job); err != nil {
		return false, err
	}
	return true, nil
}

func (s *HybridStore) ClaimRequestKey(ctx context.Context, requestKey, jobID string) (bool, error) {
	claimed, err := s.durable.ClaimRequestKey(ctx, requestKey, jobID)
	if err != nil {
		return false, err
	}
	if claimed {
		return true, s.cache.setRequestKey(requestKey, jobID)
	}
	// Keep the cache correct if the durable claim already existed but the
	// process-local view had missed it after an earlier partial operation.
	existingJobID, err := s.durable.GetRequestKeyJobID(ctx, requestKey)
	if err != nil {
		return false, err
	}
	if existingJobID != "" {
		if err := s.cache.setRequestKey(requestKey, existingJobID); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (s *HybridStore) GetRequestKeyJobID(ctx context.Context, requestKey string) (string, error) {
	jobID, err := s.cache.GetRequestKeyJobID(ctx, requestKey)
	if err != nil {
		return "", err
	}
	if jobID != "" {
		return jobID, nil
	}
	jobID, err = s.durable.GetRequestKeyJobID(ctx, requestKey)
	if err != nil {
		return "", err
	}
	if jobID != "" {
		if err := s.cache.setRequestKey(requestKey, jobID); err != nil {
			return "", err
		}
	}
	return jobID, nil
}

func (s *HybridStore) DeleteRequestKey(ctx context.Context, requestKey string) error {
	if err := s.durable.DeleteRequestKey(ctx, requestKey); err != nil {
		return err
	}
	return s.cache.DeleteRequestKey(ctx, requestKey)
}

func (s *HybridStore) DeleteRequestKeyIfMatches(ctx context.Context, requestKey, jobID string) (bool, error) {
	deleted, err := s.durable.DeleteRequestKeyIfMatches(ctx, requestKey, jobID)
	if err != nil || !deleted {
		return deleted, err
	}
	return s.cache.deleteRequestKeyIfMatches(requestKey, jobID)
}

func (s *HybridStore) GetJob(ctx context.Context, id string) (*models.Job, error) {
	job, err := s.cache.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job != nil {
		return job, nil
	}
	job, err = s.durable.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job != nil {
		if err := s.cache.SaveJob(ctx, job); err != nil {
			return nil, err
		}
	}
	return job, nil
}

func (s *HybridStore) LoadAllJobs(ctx context.Context) ([]*models.Job, error) {
	jobs, err := s.cache.LoadAllJobs(ctx)
	if err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		return jobs, nil
	}
	jobs, err = s.durable.LoadAllJobs(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.cache.replaceJobs(jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *HybridStore) PopQueuedJob(ctx context.Context) (string, error) {
	jobID, err := s.durable.PopQueuedJob(ctx)
	if err != nil || jobID == "" {
		return jobID, err
	}
	if _, err := s.cache.PopQueuedJob(ctx); err != nil {
		return "", err
	}
	return jobID, nil
}

func (s *HybridStore) QueueDepth(ctx context.Context) (int, error) {
	return s.cache.QueueDepth(ctx)
}

func (s *HybridStore) QueuePosition(ctx context.Context, jobID string) (int, error) {
	return s.cache.QueuePosition(ctx, jobID)
}

func (s *HybridStore) RecoverQueue(ctx context.Context) error {
	if err := s.durable.RecoverQueue(ctx); err != nil {
		return err
	}
	return s.hydrate(ctx)
}

func (s *HybridStore) StatusCounts(ctx context.Context) (map[string]int, error) {
	return s.cache.StatusCounts(ctx)
}
