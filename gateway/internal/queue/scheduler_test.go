package queue

import (
	"encoding/json"
	"testing"

	"ai-functions/internal/backend"
	"ai-functions/internal/models"
)

func TestBackendMatchesJobForMainRole(t *testing.T) {
	xpuEndpoint := backend.Endpoint{
		Name:           "xpu",
		Role:           models.BackendRoleMain,
		SupportsCov:    false,
		SupportsNonCov: true,
	}
	rocmEndpoint := backend.Endpoint{
		Name:              "rocm",
		Role:              models.BackendRoleMain,
		SupportsCov:       true,
		SupportsDirectCov: true,
		SupportsNonCov:    false,
	}

	if !backendMatchesJob(xpuEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFLite, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected xpu endpoint to accept mtf-lite main-stage job")
	}
	if backendMatchesJob(xpuEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFPro, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected xpu endpoint to reject mtf-pro main-stage job")
	}
	if !backendMatchesJob(rocmEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFPro, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected rocm endpoint to accept mtf-pro main-stage job")
	}
	if backendMatchesJob(rocmEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFLite, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected rocm endpoint to reject mtf-lite main-stage job")
	}
}

func TestBackendMatchesJobRejectsCovOnNonCovOnlyMainEndpoint(t *testing.T) {
	xpuEndpoint := backend.Endpoint{
		Name:           "xpu",
		Role:           models.BackendRoleMain,
		SupportsCov:    false,
		SupportsNonCov: true,
	}

	if backendMatchesJob(xpuEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFPro, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected xpu endpoint to reject mtf-pro main-stage job")
	}
	if !backendMatchesJob(xpuEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFLite, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected xpu endpoint to accept mtf-lite main-stage job")
	}
}

func TestBackendMatchesJobRejectsWrongRoleOrPredictionType(t *testing.T) {
	xregEndpoint := backend.Endpoint{
		Name:           "cpu-xreg",
		Role:           models.BackendRoleXReg,
		SupportsCov:    true,
		SupportsNonCov: false,
	}

	if backendMatchesJob(xregEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFPro, CurrentStage: models.BackendRoleMain}) {
		t.Fatalf("expected xreg endpoint to reject main-stage job")
	}
	if backendMatchesJob(xregEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFLite, CurrentStage: models.BackendRoleXReg}) {
		t.Fatalf("expected xreg endpoint to reject mtf-lite xreg-stage job")
	}
	if !backendMatchesJob(xregEndpoint, &models.Job{PredictionType: models.PredictionTypeMTFPro, CurrentStage: models.BackendRoleXReg}) {
		t.Fatalf("expected xreg endpoint to accept cov xreg-stage job")
	}
}

func TestBackendMatchesJobRoutesUZIOnlyToUZIBackend(t *testing.T) {
	uziEndpoint := backend.Endpoint{
		Name:        "uzi",
		Role:        models.BackendRoleUZI,
		SupportsUZI: true,
	}
	xpuEndpoint := backend.Endpoint{
		Name:           "xpu",
		Role:           models.BackendRoleMain,
		SupportsNonCov: true,
	}
	job := &models.Job{
		JobKind:      models.JobKindUZI,
		CurrentStage: models.BackendRoleUZI,
	}

	if !backendMatchesJob(uziEndpoint, job) {
		t.Fatalf("expected uzi endpoint to accept uzi job")
	}
	if backendMatchesJob(xpuEndpoint, job) {
		t.Fatalf("expected main inference endpoint to reject uzi job")
	}
}

func TestShouldOrchestrateCovJob(t *testing.T) {
	schedulerWithXReg := NewScheduler(
		nil,
		nil,
		[]backend.Endpoint{
			{
				Name:              "rocm",
				Role:              models.BackendRoleMain,
				SupportsCov:       true,
				SupportsDirectCov: true,
				SupportsNonCov:    false,
			},
			{
				Name:           "cpu-xreg",
				Role:           models.BackendRoleXReg,
				SupportsCov:    true,
				SupportsNonCov: false,
			},
		},
	)
	schedulerWithoutXReg := NewScheduler(
		nil,
		nil,
		[]backend.Endpoint{
			{
				Name:           "xpu",
				Role:           models.BackendRoleMain,
				SupportsCov:    false,
				SupportsNonCov: true,
			},
		},
	)

	if schedulerWithXReg.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_once_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:              "rocm",
		Role:              models.BackendRoleMain,
		SupportsCov:       true,
		SupportsDirectCov: true,
	}) {
		t.Fatalf("expected direct mtf-pro backend to bypass staged orchestration")
	}
	if schedulerWithXReg.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_for_best_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:              "rocm",
		Role:              models.BackendRoleMain,
		SupportsCov:       true,
		SupportsDirectCov: true,
	}) {
		t.Fatalf("expected direct mtf-pro backend to bypass staged orchestration for predict_for_best")
	}
	if schedulerWithoutXReg.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_once_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:           "xpu",
		Role:           models.BackendRoleMain,
		SupportsCov:    true,
		SupportsNonCov: true,
	}) {
		t.Fatalf("expected mtf-pro predict_once job to stay single-stage without xreg backend")
	}
	if schedulerWithXReg.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_once_sync",
		PredictionType: models.PredictionTypeMTFLite,
	}, backend.Endpoint{
		Name:           "xpu",
		Role:           models.BackendRoleMain,
		SupportsCov:    true,
		SupportsNonCov: true,
	}) {
		t.Fatalf("expected mtf-lite job to skip staged orchestration")
	}
	if schedulerWithXReg.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_once_cached_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:           "xpu",
		Role:           models.BackendRoleMain,
		SupportsCov:    true,
		SupportsNonCov: true,
	}) {
		t.Fatalf("expected unsupported mtf-pro target to skip staged orchestration")
	}
}

func TestShouldOrchestrateCovJobWhenOnlyStagedCovBackendExists(t *testing.T) {
	scheduler := NewScheduler(
		nil,
		nil,
		[]backend.Endpoint{
			{
				Name:              "xpu",
				Role:              models.BackendRoleMain,
				SupportsCov:       true,
				SupportsDirectCov: false,
				SupportsNonCov:    true,
			},
			{
				Name:           "cpu-xreg",
				Role:           models.BackendRoleXReg,
				SupportsCov:    true,
				SupportsNonCov: false,
			},
		},
	)

	if !scheduler.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_for_best_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:              "xpu",
		Role:              models.BackendRoleMain,
		SupportsCov:       true,
		SupportsDirectCov: false,
		SupportsNonCov:    true,
	}) {
		t.Fatalf("expected staged mtf-pro backend to use orchestration when no direct cov backend exists")
	}
}

func TestShouldOrchestrateCovJobWhenSelectedBackendIsStagedEvenIfDirectFallbackExists(t *testing.T) {
	scheduler := NewScheduler(
		nil,
		nil,
		[]backend.Endpoint{
			{
				Name:              "xpu",
				Role:              models.BackendRoleMain,
				SupportsCov:       true,
				SupportsDirectCov: false,
				SupportsNonCov:    true,
			},
			{
				Name:              "rocm",
				Role:              models.BackendRoleMain,
				SupportsCov:       true,
				SupportsDirectCov: true,
				SupportsNonCov:    false,
			},
			{
				Name:           "cpu-xreg",
				Role:           models.BackendRoleXReg,
				SupportsCov:    true,
				SupportsNonCov: false,
			},
		},
	)

	if !scheduler.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_for_best_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:              "xpu",
		Role:              models.BackendRoleMain,
		SupportsCov:       true,
		SupportsDirectCov: false,
		SupportsNonCov:    true,
	}) {
		t.Fatalf("expected selected staged mtf-pro backend to orchestrate even when direct fallback exists")
	}
	if scheduler.shouldOrchestrateCovJob(&models.Job{
		TargetPath:     "/internal/predict_for_best_sync",
		PredictionType: models.PredictionTypeMTFPro,
	}, backend.Endpoint{
		Name:              "rocm",
		Role:              models.BackendRoleMain,
		SupportsCov:       true,
		SupportsDirectCov: true,
		SupportsNonCov:    false,
	}) {
		t.Fatalf("expected selected direct mtf-pro backend to skip orchestration")
	}
}

func TestSelectBackendForCovPrefersXpuSplitAndFallsBackToRocm(t *testing.T) {
	scheduler := NewScheduler(
		nil,
		nil,
		[]backend.Endpoint{
			{
				Name:              "xpu",
				Role:              models.BackendRoleMain,
				SupportsCov:       true,
				SupportsDirectCov: false,
				SupportsNonCov:    true,
				Capacity:          1,
			},
			{
				Name:              "rocm",
				Role:              models.BackendRoleMain,
				SupportsCov:       true,
				SupportsDirectCov: true,
				SupportsNonCov:    false,
				Capacity:          1,
			},
		},
	)

	job := &models.Job{
		PredictionType: models.PredictionTypeMTFPro,
		CurrentStage:   models.BackendRoleMain,
	}
	selected := scheduler.selectBackendForJobLocked(job)
	if selected == nil || selected.endpoint.Name != "xpu" {
		t.Fatalf("expected xpu to be selected first for cov split mode, got %#v", selected)
	}

	selected.inFlight = selected.endpoint.Capacity
	fallback := scheduler.selectBackendForJobLocked(job)
	if fallback == nil || fallback.endpoint.Name != "rocm" {
		t.Fatalf("expected rocm to be selected as fallback when xpu is saturated, got %#v", fallback)
	}
}

func TestParseDirectPredictionStageResponse(t *testing.T) {
	tests := []struct {
		name        string
		body        map[string]any
		wantStage   string
		wantPayload string
		wantErr     string
	}{
		{
			name:        "main stage payload",
			body:        map[string]any{"success": true, "stage": "main", "payload": map[string]any{"foo": "bar"}},
			wantStage:   "main",
			wantPayload: `{"foo":"bar"}`,
		},
		{
			name:      "complete stage explicit",
			body:      map[string]any{"success": true, "stage": "complete", "result": map[string]any{"ok": true}},
			wantStage: "complete",
		},
		{
			name:      "complete stage implicit",
			body:      map[string]any{"success": true, "data": map[string]any{"ok": true}},
			wantStage: "complete",
		},
		{
			name:    "failure payload surfaces error",
			body:    map[string]any{"success": false, "error": "stage failed"},
			wantErr: "stage failed",
		},
		{
			name:    "unsupported stage rejected",
			body:    map[string]any{"success": true, "stage": "weird"},
			wantErr: "unsupported stage response: weird",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatalf("marshal test body: %v", err)
			}

			stage, payload, parseErr := parseDirectPredictionStageResponse(body)
			if tc.wantErr != "" {
				if parseErr == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErr)
				}
				if parseErr.Error() != tc.wantErr {
					t.Fatalf("expected error %q, got %q", tc.wantErr, parseErr.Error())
				}
				return
			}
			if parseErr != nil {
				t.Fatalf("unexpected parse error: %v", parseErr)
			}
			if stage != tc.wantStage {
				t.Fatalf("expected stage %q, got %q", tc.wantStage, stage)
			}
			if tc.wantPayload != "" && string(payload) != tc.wantPayload {
				t.Fatalf("expected payload %s, got %s", tc.wantPayload, string(payload))
			}
		})
	}
}
