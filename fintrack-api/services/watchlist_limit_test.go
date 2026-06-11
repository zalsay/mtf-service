package services

import (
	"testing"
	"time"

	"fintrack-api/models"
)

func TestWatchlistLimitForMembershipLevel(t *testing.T) {
	tests := []struct {
		level int
		want  int
	}{
		{level: 0, want: 3},
		{level: 1, want: 30},
		{level: 2, want: 30},
		{level: 3, want: 30},
		{level: 99, want: 30},
	}

	for _, tc := range tests {
		got := watchlistLimitForMembershipLevel(tc.level)
		if got != tc.want {
			t.Fatalf("watchlistLimitForMembershipLevel(%d) = %d, want %d", tc.level, got, tc.want)
		}
	}
}

func TestApplyWatchlistOverflowMarksNewestItemsOverLimit(t *testing.T) {
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	items := []models.WatchlistItem{
		{ID: 3, AddedAt: base.Add(2 * time.Hour)},
		{ID: 1, AddedAt: base},
		{ID: 4, AddedAt: base.Add(3 * time.Hour)},
		{ID: 2, AddedAt: base.Add(time.Hour)},
	}

	applyWatchlistOverflow(items, 2)

	byID := map[int]models.WatchlistItem{}
	for _, item := range items {
		byID[item.ID] = item
	}

	for _, id := range []int{1, 2} {
		if byID[id].IsOverLimit {
			t.Fatalf("item %d should be active", id)
		}
		if byID[id].WatchlistLimit != 2 {
			t.Fatalf("item %d limit = %d, want 2", id, byID[id].WatchlistLimit)
		}
	}
	for _, id := range []int{3, 4} {
		if !byID[id].IsOverLimit {
			t.Fatalf("item %d should be over limit", id)
		}
		if byID[id].WatchlistLimit != 2 {
			t.Fatalf("item %d limit = %d, want 2", id, byID[id].WatchlistLimit)
		}
	}
}

func TestNewWatchlistLimitExceededErrorIncludesLimitAndCount(t *testing.T) {
	err := newWatchlistLimitExceededError(3, 3)

	if err.Limit != 3 {
		t.Fatalf("Limit = %d, want 3", err.Limit)
	}
	if err.Count != 3 {
		t.Fatalf("Count = %d, want 3", err.Count)
	}
	if err.Error() != "watchlist limit exceeded" {
		t.Fatalf("Error() = %q, want watchlist limit exceeded", err.Error())
	}
}
