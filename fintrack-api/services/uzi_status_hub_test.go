package services

import (
	"testing"

	"fintrack-api/models"
)

func TestUZIStatusHubAllowsOnlyOneActiveJobPerUser(t *testing.T) {
	hub := NewUZIStatusHub()

	first, ok := hub.TryStart(7, "601766.SH")
	if !ok {
		t.Fatal("expected first analyze request to start")
	}
	if first.Status != UZIAnalyzeStatusRunning {
		t.Fatalf("expected running status, got %q", first.Status)
	}

	second, ok := hub.TryStart(7, "000001.SZ")
	if ok {
		t.Fatal("expected second analyze request for same user to be rejected")
	}
	if second.Ticker != "601766.SH" {
		t.Fatalf("expected active ticker to be returned, got %q", second.Ticker)
	}

	hub.Update(7, models.UZIAnalyzeStatus{Status: UZIAnalyzeStatusCompleted, Summary: "done"})
	third, ok := hub.TryStart(7, "000001.SZ")
	if !ok {
		t.Fatal("expected new analyze request after completion to start")
	}
	if third.Ticker != "000001.SZ" {
		t.Fatalf("expected new ticker, got %q", third.Ticker)
	}
}

func TestUZIStatusHubBroadcastsStatusUpdates(t *testing.T) {
	hub := NewUZIStatusHub()
	ch, unsubscribe := hub.Subscribe(3)
	defer unsubscribe()

	initial := <-ch
	if initial.Status != UZIAnalyzeStatusIdle {
		t.Fatalf("expected initial idle status, got %q", initial.Status)
	}

	hub.TryStart(3, "601766.SH")
	started := <-ch
	if started.Status != UZIAnalyzeStatusRunning {
		t.Fatalf("expected running broadcast, got %q", started.Status)
	}

	hub.Update(3, models.UZIAnalyzeStatus{Status: UZIAnalyzeStatusProcessing, Summary: "处理中"})
	processing := <-ch
	if processing.Status != UZIAnalyzeStatusProcessing {
		t.Fatalf("expected processing broadcast, got %q", processing.Status)
	}
}
