package config

import "testing"

func TestLoadConfigUsesInferenceGatewayEnv(t *testing.T) {
	t.Setenv("INFERENCE_GATEWAY_URL", "http://gateway.local:59010")
	t.Setenv("INFERENCE_GATEWAY_TIMEOUT", "77")
	t.Setenv("PYTHON_SERVICE_URL", "http://legacy.local:59010")
	t.Setenv("PYTHON_SERVICE_TIMEOUT", "11")
	t.Setenv("UZI_GATEWAY_URL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.InferenceGateway.BaseURL != "http://gateway.local:59010" {
		t.Fatalf("InferenceGateway.BaseURL = %q", cfg.InferenceGateway.BaseURL)
	}
	if cfg.InferenceGateway.Timeout != 77 {
		t.Fatalf("InferenceGateway.Timeout = %d", cfg.InferenceGateway.Timeout)
	}
	if cfg.UZI.QueueBaseURL != "http://gateway.local:59010" {
		t.Fatalf("UZI.QueueBaseURL = %q", cfg.UZI.QueueBaseURL)
	}
}

func TestLoadConfigAcceptsLegacyPythonServiceGatewayEnv(t *testing.T) {
	t.Setenv("INFERENCE_GATEWAY_URL", "")
	t.Setenv("INFERENCE_GATEWAY_TIMEOUT", "")
	t.Setenv("PYTHON_SERVICE_URL", "http://legacy.local:59010")
	t.Setenv("PYTHON_SERVICE_TIMEOUT", "66")
	t.Setenv("UZI_GATEWAY_URL", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.InferenceGateway.BaseURL != "http://legacy.local:59010" {
		t.Fatalf("InferenceGateway.BaseURL = %q", cfg.InferenceGateway.BaseURL)
	}
	if cfg.InferenceGateway.Timeout != 66 {
		t.Fatalf("InferenceGateway.Timeout = %d", cfg.InferenceGateway.Timeout)
	}
	if cfg.UZI.QueueBaseURL != "http://legacy.local:59010" {
		t.Fatalf("UZI.QueueBaseURL = %q", cfg.UZI.QueueBaseURL)
	}
}

func TestLoadConfigUsesUnifiedMTFServiceToken(t *testing.T) {
	t.Setenv("MTF_SERVICE_TOKEN", "shared-service-token")
	t.Setenv("POSTGRES_HANDLER_TOKEN", "legacy-postgres-token")
	t.Setenv("MTF_AGENT_RUNTIME_TOKEN", "legacy-runtime-token")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.PostgresHandler.APIToken != "shared-service-token" {
		t.Fatalf("PostgresHandler.APIToken = %q", cfg.PostgresHandler.APIToken)
	}
	if cfg.MTFAgent.RuntimeToken != "shared-service-token" {
		t.Fatalf("MTFAgent.RuntimeToken = %q", cfg.MTFAgent.RuntimeToken)
	}
}

func TestLoadConfigAcceptsLegacyMTFServiceTokenAliases(t *testing.T) {
	t.Setenv("MTF_SERVICE_TOKEN", "")
	t.Setenv("POSTGRES_HANDLER_TOKEN", "legacy-postgres-token")
	t.Setenv("MTF_AGENT_RUNTIME_TOKEN", "")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.PostgresHandler.APIToken != "legacy-postgres-token" {
		t.Fatalf("PostgresHandler.APIToken = %q", cfg.PostgresHandler.APIToken)
	}
	if cfg.MTFAgent.RuntimeToken != "legacy-postgres-token" {
		t.Fatalf("MTFAgent.RuntimeToken = %q", cfg.MTFAgent.RuntimeToken)
	}
}
