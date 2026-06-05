package services

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"fintrack-api/config"
)

func TestResolveInviteMaxUses(t *testing.T) {
	cases := []struct {
		name    string
		input   *int
		want    int
		wantErr bool
	}{
		{name: "default", input: nil, want: DefaultInviteMaxUses},
		{name: "custom", input: intPtr(12), want: 12},
		{name: "zero", input: intPtr(0), wantErr: true},
		{name: "negative", input: intPtr(-1), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveInviteMaxUses(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveInviteMaxUses() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGetGatewayQueueStatusParsesHealthSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q, want /health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "healthy",
			"timestamp": "2026-05-26T00:00:00Z",
			"scheduler": {
				"queue_depth": 3,
				"jobs": {"queued": 2, "running": 1},
				"backends": [{
					"name": "xpu",
					"role": "main",
					"url": "http://xpu:9000",
					"capacity": 2,
					"in_flight": 1,
					"available": 1,
					"supports_mtf_pro": true,
					"supports_mtf_lite": true
				}]
			}
		}`))
	}))
	defer server.Close()

	service := NewAdminService(nil, &config.Config{
		InferenceGateway: config.InferenceGatewayConfig{BaseURL: server.URL, Timeout: 2},
		UZI:              config.UZIServiceConfig{QueueBaseURL: server.URL},
	})

	status, err := service.GetGatewayQueueStatus()
	if err != nil {
		t.Fatalf("GetGatewayQueueStatus() error = %v", err)
	}
	if !status.Reachable {
		t.Fatal("expected gateway to be reachable")
	}
	if status.Status != "healthy" {
		t.Fatalf("status = %q, want healthy", status.Status)
	}
	if status.QueueDepth != 3 {
		t.Fatalf("queue depth = %d, want 3", status.QueueDepth)
	}
	if status.Jobs["queued"] != 2 || status.Jobs["running"] != 1 {
		t.Fatalf("jobs = %#v, want queued=2 running=1", status.Jobs)
	}
	if len(status.Backends) != 1 || status.Backends[0].Name != "xpu" || status.Backends[0].InFlight != 1 {
		t.Fatalf("backends = %#v, want one xpu backend with in_flight=1", status.Backends)
	}
	if !status.Backends[0].SupportsMTFPro || !status.Backends[0].SupportsMTFLite {
		t.Fatalf("backend capabilities = %#v, want mtf-pro and mtf-lite", status.Backends[0])
	}
}

func TestGetGatewayQueueStatusReturnsUnconfiguredSnapshot(t *testing.T) {
	service := NewAdminService(nil)

	status, err := service.GetGatewayQueueStatus()
	if err != nil {
		t.Fatalf("GetGatewayQueueStatus() error = %v", err)
	}
	if status.Reachable {
		t.Fatal("expected gateway to be unreachable when config is missing")
	}
	if status.Status != "unconfigured" {
		t.Fatalf("status = %q, want unconfigured", status.Status)
	}
	if status.Error == "" || status.CheckedPath != "/health" {
		t.Fatalf("snapshot = %#v, want error and checked path", status)
	}
}

func intPtr(value int) *int {
	return &value
}
