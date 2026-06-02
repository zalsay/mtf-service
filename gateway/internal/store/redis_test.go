package store

import (
	"testing"

	"ai-functions/internal/models"
)

func TestQueueKeyForJobUsesBackgroundQueue(t *testing.T) {
	store := NewRedisStore("127.0.0.1:6379", "", 0, "test-prefix")

	if got := store.queueKeyForJob(&models.Job{}); got != store.queueKey {
		t.Fatalf("normal queue key = %q, want %q", got, store.queueKey)
	}
	if got := store.queueKeyForJob(&models.Job{QueuePriority: models.JobQueuePriorityBackground}); got != store.backgroundQueueKey {
		t.Fatalf("background queue key = %q, want %q", got, store.backgroundQueueKey)
	}
}
