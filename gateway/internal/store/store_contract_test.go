package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ai-functions/internal/models"
)

var (
	_ Store = (*MemoryStore)(nil)
	_ Store = (*SQLiteStore)(nil)
	_ Store = (*HybridStore)(nil)
	_ Store = (*RedisStore)(nil)
)

func TestMemoryStoreQueueAndRequestKeySemantics(t *testing.T) {
	testQueueAndRequestKeySemantics(t, NewMemoryStore())
}

func TestSQLiteStoreQueueAndRequestKeySemantics(t *testing.T) {
	database, err := NewSQLiteStore(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	testQueueAndRequestKeySemantics(t, database)
}

func TestHybridStoreQueueAndRequestKeySemantics(t *testing.T) {
	database, err := NewSQLiteStore(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	hybrid, err := NewHybridStore(database)
	if err != nil {
		t.Fatalf("NewHybridStore() error = %v", err)
	}
	testQueueAndRequestKeySemantics(t, hybrid)
}

func TestMemoryStoreClaimRequestKeyIsAtomic(t *testing.T) {
	store := NewMemoryStore()
	const attempts = 32
	claimed := make(chan bool, attempts)
	var waitGroup sync.WaitGroup
	for index := 0; index < attempts; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			ok, err := store.ClaimRequestKey(context.Background(), "same-request", fmt.Sprintf("job-%d", index))
			if err != nil {
				t.Errorf("ClaimRequestKey() error = %v", err)
			}
			claimed <- ok
		}(index)
	}
	waitGroup.Wait()
	close(claimed)

	wins := 0
	for ok := range claimed {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("ClaimRequestKey() successes = %d, want 1", wins)
	}
}

func TestMemoryStoreEnqueueJobIfAbsentIsAtomic(t *testing.T) {
	testEnqueueJobIfAbsentIsAtomic(t, NewMemoryStore())
}

func TestSQLiteStoreEnqueueJobIfAbsentIsAtomic(t *testing.T) {
	database, err := NewSQLiteStore(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	testEnqueueJobIfAbsentIsAtomic(t, database)
}

func TestHybridStoreEnqueueJobIfAbsentIsAtomic(t *testing.T) {
	database, err := NewSQLiteStore(filepath.Join(t.TempDir(), "gateway.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	hybrid, err := NewHybridStore(database)
	if err != nil {
		t.Fatalf("NewHybridStore() error = %v", err)
	}
	testEnqueueJobIfAbsentIsAtomic(t, hybrid)
}

func TestSQLiteStorePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	first, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("first NewSQLiteStore() error = %v", err)
	}
	job := testJob("persisted", models.JobQueued, "")
	if ok, err := first.ClaimRequestKey(context.Background(), job.RequestKey, job.ID); err != nil || !ok {
		t.Fatalf("ClaimRequestKey() = %t, %v", ok, err)
	}
	if err := first.EnqueueJob(context.Background(), job); err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	second, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("second NewSQLiteStore() error = %v", err)
	}
	defer second.Close()
	got, err := second.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if got == nil || got.ID != job.ID {
		t.Fatalf("GetJob() = %#v, want persisted job", got)
	}
	depth, err := second.QueueDepth(context.Background())
	if err != nil || depth != 1 {
		t.Fatalf("QueueDepth() = %d, %v; want 1", depth, err)
	}
	if err := second.RecoverQueue(context.Background()); err != nil {
		t.Fatalf("RecoverQueue() error = %v", err)
	}
	if got, err := second.PopQueuedJob(context.Background()); err != nil || got != job.ID {
		t.Fatalf("PopQueuedJob() = %q, %v; want %q", got, err, job.ID)
	}
}

func TestHybridStoreHydratesMemoryFromSQLiteAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.db")
	firstDatabase, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("first NewSQLiteStore() error = %v", err)
	}
	first, err := NewHybridStore(firstDatabase)
	if err != nil {
		t.Fatalf("first NewHybridStore() error = %v", err)
	}
	job := testJob("hybrid-persisted", models.JobQueued, "")
	if ok, err := first.ClaimRequestKey(context.Background(), job.RequestKey, job.ID); err != nil || !ok {
		t.Fatalf("ClaimRequestKey() = %t, %v", ok, err)
	}
	if err := first.EnqueueJob(context.Background(), job); err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	secondDatabase, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("second NewSQLiteStore() error = %v", err)
	}
	second, err := NewHybridStore(secondDatabase)
	if err != nil {
		t.Fatalf("second NewHybridStore() error = %v", err)
	}
	defer second.Close()
	got, err := second.GetJob(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if got == nil || got.ID != job.ID {
		t.Fatalf("GetJob() = %#v, want hydrated job", got)
	}
	if depth, err := second.QueueDepth(context.Background()); err != nil || depth != 1 {
		t.Fatalf("QueueDepth() = %d, %v; want 1", depth, err)
	}
}

func testQueueAndRequestKeySemantics(t *testing.T, jobStore Store) {
	t.Helper()
	defer jobStore.Close()
	ctx := context.Background()
	normal := testJob("normal", models.JobQueued, "")
	background := testJob("background", models.JobQueued, models.JobQueuePriorityBackground)
	for _, job := range []*models.Job{normal, background} {
		if claimed, err := jobStore.ClaimRequestKey(ctx, job.RequestKey, job.ID); err != nil || !claimed {
			t.Fatalf("ClaimRequestKey(%q) = %t, %v", job.ID, claimed, err)
		}
		if err := jobStore.EnqueueJob(ctx, job); err != nil {
			t.Fatalf("EnqueueJob(%q) error = %v", job.ID, err)
		}
	}
	if depth, err := jobStore.QueueDepth(ctx); err != nil || depth != 2 {
		t.Fatalf("QueueDepth() = %d, %v; want 2", depth, err)
	}
	if position, err := jobStore.QueuePosition(ctx, normal.ID); err != nil || position != 1 {
		t.Fatalf("normal QueuePosition() = %d, %v; want 1", position, err)
	}
	if position, err := jobStore.QueuePosition(ctx, background.ID); err != nil || position != 2 {
		t.Fatalf("background QueuePosition() = %d, %v; want 2", position, err)
	}
	if got, err := jobStore.PopQueuedJob(ctx); err != nil || got != normal.ID {
		t.Fatalf("first PopQueuedJob() = %q, %v; want %q", got, err, normal.ID)
	}
	if got, err := jobStore.PopQueuedJob(ctx); err != nil || got != background.ID {
		t.Fatalf("second PopQueuedJob() = %q, %v; want %q", got, err, background.ID)
	}
}

func testEnqueueJobIfAbsentIsAtomic(t *testing.T, jobStore Store) {
	t.Helper()
	defer jobStore.Close()
	ctx := context.Background()
	const attempts = 64
	start := make(chan struct{})
	created := make(chan bool, attempts)
	var waitGroup sync.WaitGroup
	for index := 0; index < attempts; index++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			<-start
			job := testJob(fmt.Sprintf("atomic-%d", index), models.JobQueued, "")
			job.RequestKey = "same-request"
			ok, err := jobStore.EnqueueJobIfAbsent(ctx, job)
			if err != nil {
				t.Errorf("EnqueueJobIfAbsent() error = %v", err)
			}
			created <- ok
		}(index)
	}
	close(start)
	waitGroup.Wait()
	close(created)

	wins := 0
	for ok := range created {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("EnqueueJobIfAbsent() successes = %d, want 1", wins)
	}
	if depth, err := jobStore.QueueDepth(ctx); err != nil || depth != 1 {
		t.Fatalf("QueueDepth() = %d, %v; want 1", depth, err)
	}
	jobs, err := jobStore.LoadAllJobs(ctx)
	if err != nil {
		t.Fatalf("LoadAllJobs() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("LoadAllJobs() returned %d jobs, want 1", len(jobs))
	}
	jobID, err := jobStore.GetRequestKeyJobID(ctx, "same-request")
	if err != nil {
		t.Fatalf("GetRequestKeyJobID() error = %v", err)
	}
	if jobID == "" || jobs[0].ID != jobID {
		t.Fatalf("request key job = %q, stored job = %q", jobID, jobs[0].ID)
	}
}

func TestMemoryStoreRecoverQueueMarksRunningJobsFailed(t *testing.T) {
	jobStore := NewMemoryStore()
	defer jobStore.Close()
	ctx := context.Background()
	queued := testJob("queued-recovery", models.JobQueued, "")
	running := testJob("running-recovery", models.JobRunning, "")
	if ok, err := jobStore.ClaimRequestKey(ctx, queued.RequestKey, queued.ID); err != nil || !ok {
		t.Fatalf("ClaimRequestKey() = %t, %v", ok, err)
	}
	if err := jobStore.EnqueueJob(ctx, queued); err != nil {
		t.Fatalf("EnqueueJob() error = %v", err)
	}
	if err := jobStore.SaveJob(ctx, running); err != nil {
		t.Fatalf("SaveJob() error = %v", err)
	}
	if err := jobStore.RecoverQueue(ctx); err != nil {
		t.Fatalf("RecoverQueue() error = %v", err)
	}
	if depth, err := jobStore.QueueDepth(ctx); err != nil || depth != 1 {
		t.Fatalf("QueueDepth() = %d, %v; want 1", depth, err)
	}
	got, err := jobStore.GetJob(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if got == nil || got.Status != models.JobFailed {
		t.Fatalf("running job after recovery = %#v, want failed", got)
	}
	if jobID, err := jobStore.GetRequestKeyJobID(ctx, queued.RequestKey); err != nil || jobID != queued.ID {
		t.Fatalf("recovered request key = %q, %v; want %q", jobID, err, queued.ID)
	}
}

func testJob(id string, status models.JobStatus, priority string) *models.Job {
	return &models.Job{
		ID:            id,
		Status:        status,
		StockCode:     "000001",
		TargetPath:    "/internal/predict_once_sync",
		RequestKey:    "/internal/predict_once_sync:request-" + id,
		RequestBody:   []byte(`{"stock_code":"000001"}`),
		QueuePriority: priority,
		CreatedAt:     time.Unix(1700000000, 0).UTC(),
	}
}
