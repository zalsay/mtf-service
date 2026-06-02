package services

import (
	"sync"
	"time"

	"fintrack-api/models"
)

const (
	UZIAnalyzeStatusIdle       = "idle"
	UZIAnalyzeStatusRunning    = "running"
	UZIAnalyzeStatusProcessing = "processing"
	UZIAnalyzeStatusCompleted  = "completed"
	UZIAnalyzeStatusFailed     = "failed"
)

type UZIStatusHub struct {
	mu          sync.RWMutex
	statuses    map[int]models.UZIAnalyzeStatus
	subscribers map[int]map[chan models.UZIAnalyzeStatus]struct{}
}

func NewUZIStatusHub() *UZIStatusHub {
	return &UZIStatusHub{
		statuses:    make(map[int]models.UZIAnalyzeStatus),
		subscribers: make(map[int]map[chan models.UZIAnalyzeStatus]struct{}),
	}
}

func (h *UZIStatusHub) TryStart(userID int, ticker string) (models.UZIAnalyzeStatus, bool) {
	if h == nil {
		return models.UZIAnalyzeStatus{}, false
	}

	now := time.Now().UTC().Format(time.RFC3339)
	h.mu.Lock()
	if existing, ok := h.statuses[userID]; ok && isUZIAnalyzeStatusActive(existing.Status) {
		h.mu.Unlock()
		return existing, false
	}

	status := models.UZIAnalyzeStatus{
		Status:    UZIAnalyzeStatusRunning,
		Ticker:    ticker,
		Stage:     "bootstrap",
		Summary:   "生成中",
		StartedAt: now,
		UpdatedAt: now,
	}
	h.statuses[userID] = status
	subscribers := h.copySubscribersLocked(userID)
	h.mu.Unlock()

	broadcastUZIStatus(subscribers, status)
	return status, true
}

func (h *UZIStatusHub) Update(userID int, status models.UZIAnalyzeStatus) models.UZIAnalyzeStatus {
	if h == nil {
		return status
	}

	h.mu.Lock()
	current := h.statuses[userID]
	next := mergeUZIAnalyzeStatus(current, status)
	h.statuses[userID] = next
	subscribers := h.copySubscribersLocked(userID)
	h.mu.Unlock()

	broadcastUZIStatus(subscribers, next)
	return next
}

func (h *UZIStatusHub) Get(userID int) models.UZIAnalyzeStatus {
	if h == nil {
		return idleUZIAnalyzeStatus()
	}

	h.mu.RLock()
	status, ok := h.statuses[userID]
	h.mu.RUnlock()
	if !ok {
		return idleUZIAnalyzeStatus()
	}
	return status
}

func (h *UZIStatusHub) Subscribe(userID int) (<-chan models.UZIAnalyzeStatus, func()) {
	ch := make(chan models.UZIAnalyzeStatus, 8)
	if h == nil {
		ch <- idleUZIAnalyzeStatus()
		return ch, func() { close(ch) }
	}

	h.mu.Lock()
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[chan models.UZIAnalyzeStatus]struct{})
	}
	h.subscribers[userID][ch] = struct{}{}
	initial := h.statuses[userID]
	if initial.Status == "" {
		initial = idleUZIAnalyzeStatus()
	}
	h.mu.Unlock()

	ch <- initial
	return ch, func() {
		h.mu.Lock()
		if subscribers := h.subscribers[userID]; subscribers != nil {
			delete(subscribers, ch)
			if len(subscribers) == 0 {
				delete(h.subscribers, userID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
}

func (h *UZIStatusHub) copySubscribersLocked(userID int) []chan models.UZIAnalyzeStatus {
	subscribers := h.subscribers[userID]
	if len(subscribers) == 0 {
		return nil
	}
	copied := make([]chan models.UZIAnalyzeStatus, 0, len(subscribers))
	for ch := range subscribers {
		copied = append(copied, ch)
	}
	return copied
}

func mergeUZIAnalyzeStatus(current models.UZIAnalyzeStatus, patch models.UZIAnalyzeStatus) models.UZIAnalyzeStatus {
	now := time.Now().UTC().Format(time.RFC3339)
	if current.Status == "" {
		current = idleUZIAnalyzeStatus()
	}
	if patch.Status != "" {
		current.Status = patch.Status
	}
	if patch.JobID != "" {
		current.JobID = patch.JobID
	}
	if patch.Ticker != "" {
		current.Ticker = patch.Ticker
	}
	if patch.Stage != "" {
		current.Stage = patch.Stage
	}
	if patch.Summary != "" {
		current.Summary = patch.Summary
	}
	if patch.Report != nil {
		current.Report = patch.Report
	}
	if patch.StartedAt != "" {
		current.StartedAt = patch.StartedAt
	}
	current.UpdatedAt = now
	return current
}

func broadcastUZIStatus(subscribers []chan models.UZIAnalyzeStatus, status models.UZIAnalyzeStatus) {
	for _, ch := range subscribers {
		select {
		case ch <- status:
		default:
		}
	}
}

func idleUZIAnalyzeStatus() models.UZIAnalyzeStatus {
	return models.UZIAnalyzeStatus{
		Status:    UZIAnalyzeStatusIdle,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func isUZIAnalyzeStatusActive(status string) bool {
	return status == UZIAnalyzeStatusRunning || status == UZIAnalyzeStatusProcessing
}
