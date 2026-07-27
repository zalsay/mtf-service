package store

import (
	"context"

	"ai-functions/internal/models"
)

// Store is the persistence contract used by the gateway scheduler.
// Implementations may be durable (Redis/SQLite) or process-local (memory).
type Store interface {
	Ping(context.Context) error
	Close() error

	SaveJob(context.Context, *models.Job) error
	EnqueueJob(context.Context, *models.Job) error
	// EnqueueJobIfAbsent atomically binds the request key, stores the job, and
	// adds it to the queue. It returns false when the request key already exists.
	EnqueueJobIfAbsent(context.Context, *models.Job) (bool, error)
	ClaimRequestKey(context.Context, string, string) (bool, error)
	GetRequestKeyJobID(context.Context, string) (string, error)
	DeleteRequestKey(context.Context, string) error
	DeleteRequestKeyIfMatches(context.Context, string, string) (bool, error)
	GetJob(context.Context, string) (*models.Job, error)
	LoadAllJobs(context.Context) ([]*models.Job, error)
	PopQueuedJob(context.Context) (string, error)
	QueueDepth(context.Context) (int, error)
	QueuePosition(context.Context, string) (int, error)
	RecoverQueue(context.Context) error
	StatusCounts(context.Context) (map[string]int, error)
}
