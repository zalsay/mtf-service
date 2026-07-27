package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-functions/internal/models"

	_ "modernc.org/sqlite"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    request_key TEXT NOT NULL DEFAULT '',
    payload BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_status_idx ON jobs(status);

CREATE TABLE IF NOT EXISTS request_keys (
    request_key TEXT PRIMARY KEY,
    job_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS queue (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS queue_order_idx ON queue(priority, sequence);
`

// SQLiteStore persists scheduler state in a local SQLite database. The
// connection pool is intentionally limited to one connection so :memory: and
// file-backed databases have identical locking behavior.
type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("sqlite path is empty")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if parent := filepath.Dir(path); parent != "." {
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return nil, fmt.Errorf("create sqlite directory: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db}
	if err := store.initialize(context.Background(), path); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) initialize(ctx context.Context, path string) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping sqlite database: %w", err)
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file::memory:") {
		if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
			return fmt.Errorf("enable sqlite WAL: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
		return fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, sqliteSchema); err != nil {
		return fmt.Errorf("initialize sqlite schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("sqlite store is not configured")
	}
	return s.db.PingContext(ctx)
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func marshalJob(job *models.Job) ([]byte, error) {
	if job == nil {
		return nil, errors.New("job is nil")
	}
	return json.Marshal(job)
}

func jobCreatedAt(job *models.Job) int64 {
	if job == nil || job.CreatedAt.IsZero() {
		return 0
	}
	return job.CreatedAt.UTC().UnixNano()
}

func sqliteQueuePriority(job *models.Job) int {
	if job != nil && job.QueuePriority == models.JobQueuePriorityBackground {
		return 1
	}
	return 0
}

func upsertSQLiteJob(ctx context.Context, exec sqlExecer, job *models.Job, payload []byte) error {
	_, err := exec.ExecContext(ctx, `
INSERT INTO jobs (id, status, created_at, request_key, payload)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    status = excluded.status,
    created_at = excluded.created_at,
    request_key = excluded.request_key,
    payload = excluded.payload
`, job.ID, job.Status, jobCreatedAt(job), job.RequestKey, payload)
	return err
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func decodeSQLiteJob(payload []byte) (*models.Job, error) {
	var job models.Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *SQLiteStore) SaveJob(ctx context.Context, job *models.Job) error {
	payload, err := marshalJob(job)
	if err != nil {
		return err
	}
	if err := upsertSQLiteJob(ctx, s.db, job, payload); err != nil {
		return fmt.Errorf("save sqlite job %s: %w", job.ID, err)
	}
	return nil
}

func (s *SQLiteStore) EnqueueJob(ctx context.Context, job *models.Job) error {
	payload, err := marshalJob(job)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}
	if err := upsertSQLiteJob(ctx, tx, job, payload); err != nil {
		return rollback(fmt.Errorf("save sqlite job %s: %w", job.ID, err))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO queue (job_id, priority) VALUES (?, ?)`, job.ID, sqliteQueuePriority(job)); err != nil {
		return rollback(fmt.Errorf("enqueue sqlite job %s: %w", job.ID, err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite enqueue %s: %w", job.ID, err)
	}
	return nil
}

func (s *SQLiteStore) EnqueueJobIfAbsent(ctx context.Context, job *models.Job) (bool, error) {
	payload, err := marshalJob(job)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	rollback := func(cause error) (bool, error) {
		_ = tx.Rollback()
		return false, cause
	}

	if job.RequestKey != "" {
		result, err := tx.ExecContext(ctx, `
INSERT INTO request_keys (request_key, job_id) VALUES (?, ?)
ON CONFLICT(request_key) DO NOTHING
`, job.RequestKey, job.ID)
		if err != nil {
			return rollback(err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return rollback(err)
		}
		if rows != 1 {
			_ = tx.Rollback()
			return false, nil
		}
	}

	if err := upsertSQLiteJob(ctx, tx, job, payload); err != nil {
		return rollback(fmt.Errorf("save sqlite job %s: %w", job.ID, err))
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO queue (job_id, priority) VALUES (?, ?)`, job.ID, sqliteQueuePriority(job)); err != nil {
		return rollback(fmt.Errorf("enqueue sqlite job %s: %w", job.ID, err))
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit sqlite enqueue %s: %w", job.ID, err)
	}
	return true, nil
}

func (s *SQLiteStore) ClaimRequestKey(ctx context.Context, requestKey, jobID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
INSERT INTO request_keys (request_key, job_id) VALUES (?, ?)
ON CONFLICT(request_key) DO NOTHING
`, requestKey, jobID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (s *SQLiteStore) GetRequestKeyJobID(ctx context.Context, requestKey string) (string, error) {
	var jobID string
	err := s.db.QueryRowContext(ctx, `SELECT job_id FROM request_keys WHERE request_key = ?`, requestKey).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return jobID, err
}

func (s *SQLiteStore) DeleteRequestKey(ctx context.Context, requestKey string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM request_keys WHERE request_key = ?`, requestKey)
	return err
}

func (s *SQLiteStore) DeleteRequestKeyIfMatches(ctx context.Context, requestKey, jobID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM request_keys
WHERE request_key = ? AND job_id = ?
`, requestKey, jobID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (s *SQLiteStore) GetJob(ctx context.Context, id string) (*models.Job, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM jobs WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeSQLiteJob(payload)
}

func (s *SQLiteStore) LoadAllJobs(ctx context.Context) ([]*models.Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM jobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]*models.Job, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		job, err := decodeSQLiteJob(payload)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *SQLiteStore) PopQueuedJob(ctx context.Context) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	var sequence int64
	var jobID string
	err = tx.QueryRowContext(ctx, `
SELECT sequence, job_id
FROM queue
ORDER BY priority ASC, sequence ASC
LIMIT 1
`).Scan(&sequence, &jobID)
	if errors.Is(err, sql.ErrNoRows) {
		_ = tx.Rollback()
		return "", nil
	}
	if err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM queue WHERE sequence = ?`, sequence); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return jobID, nil
}

func (s *SQLiteStore) QueueDepth(ctx context.Context) (int, error) {
	var depth int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM queue`).Scan(&depth)
	return depth, err
}

func (s *SQLiteStore) QueuePosition(ctx context.Context, jobID string) (int, error) {
	var priority int
	var sequence int64
	err := s.db.QueryRowContext(ctx, `
SELECT priority, sequence FROM queue
WHERE job_id = ?
ORDER BY priority ASC, sequence ASC
LIMIT 1
`, jobID).Scan(&priority, &sequence)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var position int
	err = s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM queue
WHERE priority < ? OR (priority = ? AND sequence <= ?)
`, priority, priority, sequence).Scan(&position)
	return position, err
}

func (s *SQLiteStore) RecoverQueue(ctx context.Context) error {
	jobs, err := s.LoadAllJobs(ctx)
	if err != nil {
		return err
	}
	recoverable, normalized := normalizeRecoveredJobs(jobs)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM queue`); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM request_keys`); err != nil {
		return rollback(err)
	}
	for _, job := range normalized {
		payload, marshalErr := marshalJob(job)
		if marshalErr != nil {
			return rollback(marshalErr)
		}
		if err := upsertSQLiteJob(ctx, tx, job, payload); err != nil {
			return rollback(err)
		}
		if job.RequestKey != "" {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO request_keys (request_key, job_id) VALUES (?, ?)
ON CONFLICT(request_key) DO UPDATE SET job_id = excluded.job_id
`, job.RequestKey, job.ID); err != nil {
				return rollback(err)
			}
		}
	}
	for _, job := range recoverable {
		if _, err := tx.ExecContext(ctx, `INSERT INTO queue (job_id, priority) VALUES (?, ?)`, job.ID, sqliteQueuePriority(job)); err != nil {
			return rollback(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) StatusCounts(ctx context.Context) (map[string]int, error) {
	counts := emptyStatusCounts()
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM jobs GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}
