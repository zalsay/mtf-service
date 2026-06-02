package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"ai-functions/internal/models"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	client             *redis.Client
	jobsKey            string
	queueKey           string
	backgroundQueueKey string
	requestKeysKey     string
}

func NewRedisStore(addr, password string, db int, prefix string) *RedisStore {
	if prefix == "" {
		prefix = "ai-functions"
	}
	return &RedisStore{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		jobsKey:            prefix + ":jobs",
		queueKey:           prefix + ":queue",
		backgroundQueueKey: prefix + ":queue:background",
		requestKeysKey:     prefix + ":request-keys",
	}
}

func (s *RedisStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStore) Close() error {
	return s.client.Close()
}

func (s *RedisStore) SaveJob(ctx context.Context, job *models.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	return s.client.HSet(ctx, s.jobsKey, job.ID, payload).Err()
}

func (s *RedisStore) EnqueueJob(ctx context.Context, job *models.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, s.jobsKey, job.ID, payload)
	pipe.RPush(ctx, s.queueKeyForJob(job), job.ID)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) EnqueueJobAndBindRequestKey(ctx context.Context, job *models.Job) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, s.jobsKey, job.ID, payload)
	pipe.RPush(ctx, s.queueKeyForJob(job), job.ID)
	if job.RequestKey != "" {
		pipe.HSet(ctx, s.requestKeysKey, job.RequestKey, job.ID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) queueKeyForJob(job *models.Job) string {
	if job != nil && job.QueuePriority == models.JobQueuePriorityBackground {
		return s.backgroundQueueKey
	}
	return s.queueKey
}

func (s *RedisStore) ClaimRequestKey(ctx context.Context, requestKey, jobID string) (bool, error) {
	return s.client.HSetNX(ctx, s.requestKeysKey, requestKey, jobID).Result()
}

func (s *RedisStore) GetRequestKeyJobID(ctx context.Context, requestKey string) (string, error) {
	value, err := s.client.HGet(ctx, s.requestKeysKey, requestKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func (s *RedisStore) DeleteRequestKey(ctx context.Context, requestKey string) error {
	return s.client.HDel(ctx, s.requestKeysKey, requestKey).Err()
}

func (s *RedisStore) GetJob(ctx context.Context, id string) (*models.Job, error) {
	value, err := s.client.HGet(ctx, s.jobsKey, id).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var job models.Job
	if err := json.Unmarshal([]byte(value), &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *RedisStore) LoadAllJobs(ctx context.Context) ([]*models.Job, error) {
	values, err := s.client.HGetAll(ctx, s.jobsKey).Result()
	if err != nil {
		return nil, err
	}
	jobs := make([]*models.Job, 0, len(values))
	for _, raw := range values {
		var job models.Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, &job)
	}
	return jobs, nil
}

func (s *RedisStore) PopQueuedJob(ctx context.Context) (string, error) {
	for _, queueKey := range []string{s.queueKey, s.backgroundQueueKey} {
		value, err := s.client.LPop(ctx, queueKey).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return "", err
		}
		return value, nil
	}
	return "", nil
}

func (s *RedisStore) QueueDepth(ctx context.Context) (int, error) {
	values, err := s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.LLen(ctx, s.queueKey)
		pipe.LLen(ctx, s.backgroundQueueKey)
		return nil
	})
	if err != nil {
		return 0, err
	}
	total := 0
	for _, cmd := range values {
		if intCmd, ok := cmd.(*redis.IntCmd); ok {
			total += int(intCmd.Val())
		}
	}
	return total, nil
}

func (s *RedisStore) QueuePosition(ctx context.Context, jobID string) (int, error) {
	normalValues, err := s.client.LRange(ctx, s.queueKey, 0, -1).Result()
	if err != nil {
		return 0, err
	}
	for index, value := range normalValues {
		if value == jobID {
			return index + 1, nil
		}
	}
	backgroundValues, err := s.client.LRange(ctx, s.backgroundQueueKey, 0, -1).Result()
	if err != nil {
		return 0, err
	}
	offset := len(normalValues)
	for index, value := range backgroundValues {
		if value == jobID {
			return offset + index + 1, nil
		}
	}
	return 0, nil
}

func (s *RedisStore) RecoverQueue(ctx context.Context) error {
	jobs, err := s.LoadAllJobs(ctx)
	if err != nil {
		return err
	}

	sort.SliceStable(jobs, func(i, j int) bool {
		left := jobs[i].CreatedAt
		right := jobs[j].CreatedAt
		if left.Equal(right) {
			return jobs[i].ID < jobs[j].ID
		}
		return left.Before(right)
	})

	recoverable := make([]*models.Job, 0)
	for _, job := range jobs {
		if job.RequestKey == "" && len(job.RequestBody) > 0 {
			requestKey, keyErr := models.RequestKeyFromBody(job.RequestBody)
			if keyErr == nil {
				job.RequestKey = requestKey
			}
		}
		if job.PredictionType == "" && len(job.RequestBody) > 0 {
			var request models.InferenceRequest
			decoder := json.NewDecoder(strings.NewReader(string(job.RequestBody)))
			decoder.UseNumber()
			if err := decoder.Decode(&request); err == nil {
				job.PredictionType = request.PredictionType()
				job.CovariateSignature = request.CovariateSignature()
			}
		}
		if job.CurrentStage == "" {
			job.CurrentStage = models.BackendRoleMain
		}
		if job.RequestKey != "" && job.TargetPath != "" && !strings.HasPrefix(job.RequestKey, "/internal/") {
			job.RequestKey = job.TargetPath + ":" + job.RequestKey
		}
		switch job.Status {
		case models.JobQueued:
			job.Backend = ""
			job.UpstreamStatus = 0
			job.Error = ""
			job.StartedAt = nil
			job.FinishedAt = nil
			recoverable = append(recoverable, job)
		case models.JobRunning:
			now := time.Now().UTC()
			job.Status = models.JobFailed
			job.Backend = ""
			job.UpstreamStatus = 0
			job.Error = "gateway restarted before upstream completion"
			job.FinishedAt = &now
			if err := s.SaveJob(ctx, job); err != nil {
				return err
			}
		}
	}

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, s.queueKey)
	pipe.Del(ctx, s.backgroundQueueKey)
	pipe.Del(ctx, s.requestKeysKey)
	for _, job := range jobs {
		if job.RequestKey == "" {
			continue
		}
		payload, err := json.Marshal(job)
		if err != nil {
			return err
		}
		pipe.HSet(ctx, s.jobsKey, job.ID, payload)
		pipe.HSet(ctx, s.requestKeysKey, job.RequestKey, job.ID)
	}
	for _, job := range recoverable {
		payload, err := json.Marshal(job)
		if err != nil {
			return err
		}
		pipe.HSet(ctx, s.jobsKey, job.ID, payload)
		pipe.RPush(ctx, s.queueKeyForJob(job), job.ID)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStore) StatusCounts(ctx context.Context) (map[string]int, error) {
	jobs, err := s.LoadAllJobs(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{
		string(models.JobQueued):    0,
		string(models.JobRunning):   0,
		string(models.JobSucceeded): 0,
		string(models.JobFailed):    0,
	}
	for _, job := range jobs {
		counts[string(job.Status)]++
	}
	return counts, nil
}

func ParseRedisDB(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func NormalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}
