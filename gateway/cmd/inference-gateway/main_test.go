package main

import "testing"

func TestDailySyncSchedulesIncludesPrimaryAndExtraTimes(t *testing.T) {
	schedules := dailySyncSchedules(22, 0, "01:00,22:00,bad,09:30")

	if got := formatDailySyncSchedules(schedules); got != "22:00,01:00,09:30" {
		t.Fatalf("unexpected schedules: %s", got)
	}
}

func TestParseDailySyncTimeRejectsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "1", "24:00", "01:60", "aa:00"} {
		if schedule, ok := parseDailySyncTime(raw); ok {
			t.Fatalf("expected %q to be rejected, got %#v", raw, schedule)
		}
	}
}
