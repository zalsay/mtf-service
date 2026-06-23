package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type Level1DailySyncer struct {
	client         *http.Client
	level1BaseURL  string
	historyBaseURL string
	level1Token    string
	historyToken   string
	location       *time.Location
	scheduleHour   int
	scheduleMinute int
	concurrent     int
}

type level1TriggerResponse struct {
	Date     string `json:"date"`
	Mode     string `json:"mode"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

func NewLevel1DailySyncer(
	level1BaseURL string,
	historyBaseURL string,
	apiToken string,
	location *time.Location,
	scheduleHour int,
	scheduleMinute int,
	concurrent int,
) *Level1DailySyncer {
	return NewLevel1DailySyncerWithTokens(level1BaseURL, historyBaseURL, apiToken, apiToken, location, scheduleHour, scheduleMinute, concurrent)
}

func NewLevel1DailySyncerWithTokens(
	level1BaseURL string,
	historyBaseURL string,
	level1Token string,
	historyToken string,
	location *time.Location,
	scheduleHour int,
	scheduleMinute int,
	concurrent int,
) *Level1DailySyncer {
	if location == nil {
		location = time.Local
	}
	if concurrent <= 0 {
		concurrent = 50
	}
	return &Level1DailySyncer{
		client: &http.Client{
			Timeout: 3 * time.Hour,
		},
		level1BaseURL:  strings.TrimRight(level1BaseURL, "/"),
		historyBaseURL: strings.TrimRight(historyBaseURL, "/"),
		level1Token:    level1Token,
		historyToken:   historyToken,
		location:       location,
		scheduleHour:   scheduleHour,
		scheduleMinute: scheduleMinute,
		concurrent:     concurrent,
	}
}

func (s *Level1DailySyncer) Start(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Level1DailySyncer) loop(ctx context.Context) {
	for {
		now := time.Now().In(s.location)
		nextRun := s.nextRunAfter(now)
		wait := time.Until(nextRun)
		log.Printf("level1 daily stock sync scheduled at %s", nextRun.Format(time.RFC3339))

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := s.RunOnce(ctx); err != nil {
			log.Printf("level1 daily stock sync run failed: %v", err)
		}
	}
}

func (s *Level1DailySyncer) RunOnce(ctx context.Context) error {
	now := time.Now().In(s.location)
	target := now.AddDate(0, 0, -1)
	targetDate := target.Format("20060102")
	targetDateDashed := target.Format("2006-01-02")

	if s.historyBaseURL != "" {
		isTradingDay, err := s.isTradingDay(ctx, targetDate)
		if err != nil {
			return err
		}
		if !isTradingDay {
			log.Printf("level1 daily stock sync skipped: %s is not a trading day", targetDateDashed)
			return nil
		}
	}

	response, err := s.triggerDaily(ctx, targetDate)
	if err != nil {
		return err
	}
	log.Printf(
		"level1 daily stock sync finished: date=%s mode=%s exit_code=%d",
		response.Date,
		response.Mode,
		response.ExitCode,
	)
	return nil
}

func (s *Level1DailySyncer) nextRunAfter(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), s.scheduleHour, s.scheduleMinute, 0, 0, s.location)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func (s *Level1DailySyncer) isTradingDay(ctx context.Context, date string) (bool, error) {
	url := s.historyBaseURL + "/api/v1/trading-day?date=" + date
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Token", s.historyToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("trading day check failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed tradingDayResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, err
	}
	return parsed.IsTradingDay, nil
}

func (s *Level1DailySyncer) triggerDaily(ctx context.Context, date string) (*level1TriggerResponse, error) {
	payload := map[string]any{
		"date":       date,
		"concurrent": s.concurrent,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.level1BaseURL+"/daily", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.level1Token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("level1 daily trigger failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed level1TriggerResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed.ExitCode != 0 {
		return nil, fmt.Errorf("level1 daily trigger exited with %d: %s", parsed.ExitCode, strings.TrimSpace(parsed.Output))
	}
	return &parsed, nil
}
