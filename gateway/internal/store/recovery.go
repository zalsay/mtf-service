package store

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"ai-functions/internal/models"
)

func normalizeRecoveredJobs(jobs []*models.Job) (recoverable []*models.Job, changed []*models.Job) {
	sort.SliceStable(jobs, func(i, j int) bool {
		left := jobs[i].CreatedAt
		right := jobs[j].CreatedAt
		if left.Equal(right) {
			return jobs[i].ID < jobs[j].ID
		}
		return left.Before(right)
	})

	recoverable = make([]*models.Job, 0)
	changed = make([]*models.Job, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		if job.RequestKey == "" && len(job.RequestBody) > 0 {
			requestKey, keyErr := models.RequestKeyFromBody(job.RequestBody)
			if keyErr == nil {
				job.RequestKey = requestKey
			}
		}
		if job.PredictionType == "" && len(job.RequestBody) > 0 {
			var request models.InferenceRequest
			decoder := json.NewDecoder(bytes.NewReader(job.RequestBody))
			decoder.UseNumber()
			if err := decoder.Decode(&request); err == nil {
				job.PredictionType = request.PredictionType()
				job.CovariateSignature = request.CovariateSignature()
			}
		}
		if job.CurrentStage == "" {
			if job.JobKind == models.JobKindUZI {
				job.CurrentStage = models.BackendRoleUZI
			} else {
				job.CurrentStage = models.BackendRoleMain
			}
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
		}
		changed = append(changed, job)
	}
	return recoverable, changed
}
