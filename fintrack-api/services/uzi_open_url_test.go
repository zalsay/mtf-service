package services

import (
	"context"
	"strings"
	"testing"

	"fintrack-api/config"
)

func TestBuildReportOpenURLUsesSignedURLWhenAvailable(t *testing.T) {
	service := &UZIService{
		oss: &OSSService{
			config: &config.OSSConfig{
				Enabled:         true,
				Endpoint:        "oss-ap-southeast-1.aliyuncs.com",
				Region:          "ap-southeast-1",
				Bucket:          "uzi-report",
				AccessKeyID:     "ak",
				AccessKeySecret: "sk",
				Prefix:          "uzi-reports",
				PublicBaseURL:   "http://uzi-reports.meetlife.com.cn",
			},
		},
	}

	got, err := service.buildReportOpenURL(
		"unused-token",
		"https://uzi-report.oss-ap-southeast-1.aliyuncs.com/uzi-reports/6/demo.html?x-oss-signature=abc",
	)
	if err != nil {
		t.Fatalf("buildReportOpenURL() error = %v", err)
	}

	want := "https://uzi-report.oss-ap-southeast-1.aliyuncs.com/uzi-reports/6/demo.html?x-oss-signature=abc"
	if got != want {
		t.Fatalf("buildReportOpenURL() = %q, want %q", got, want)
	}
}

func TestBuildReportOpenURLFallsBackToTokenRoute(t *testing.T) {
	service := &UZIService{}

	got, err := service.buildReportOpenURL("demo-token", "")
	if err != nil {
		t.Fatalf("buildReportOpenURL() error = %v", err)
	}

	want := "/api/v1/uzi/report-open?token=demo-token"
	if got != want {
		t.Fatalf("buildReportOpenURL() = %q, want %q", got, want)
	}
}

func TestShouldBackfillManagedReportURL(t *testing.T) {
	service := &UZIService{
		oss: &OSSService{
			config: &config.OSSConfig{
				Enabled:         true,
				Endpoint:        "oss-ap-southeast-1.aliyuncs.com",
				Region:          "ap-southeast-1",
				Bucket:          "uzi-report",
				AccessKeyID:     "ak",
				AccessKeySecret: "sk",
				Prefix:          "uzi-reports",
				PublicBaseURL:   "http://uzi-reports.meetlife.com.cn",
			},
		},
	}

	if !service.shouldBackfillManagedReportURL("http://host.docker.internal:59011/reports/demo.html") {
		t.Fatal("shouldBackfillManagedReportURL() = false for legacy url, want true")
	}
	if service.shouldBackfillManagedReportURL("uzi-reports/1/demo.html") {
		t.Fatal("shouldBackfillManagedReportURL() = true for managed key, want false")
	}
}

func TestRewriteReportHTMLInjectsAlphaScoreUnitFix(t *testing.T) {
	service := &UZIService{}
	body := []byte(`<!doctype html><html><head><style>.score-giant{letter-spacing:-.06em}</style></head><body><div class="score-giant">57</div></body></html>`)

	rewritten, changed, err := service.rewriteReportHTMLAssetURLs(context.Background(), 1, "601766.SH_20260505/full-report-standalone.html", body)
	if err != nil {
		t.Fatalf("rewriteReportHTMLAssetURLs() error = %v", err)
	}
	if !changed {
		t.Fatal("rewriteReportHTMLAssetURLs() changed = false, want true")
	}

	output := string(rewritten)
	if !strings.Contains(output, reportAlphaScoreUnitFixMarker) {
		t.Fatalf("rewritten html missing %q marker", reportAlphaScoreUnitFixMarker)
	}
	if !strings.Contains(output, "letter-spacing: 0 !important") {
		t.Fatal("rewritten html missing isolated alpha score unit letter-spacing")
	}
}

func TestRewriteReportHTMLDoesNotDuplicateAlphaScoreUnitFix(t *testing.T) {
	service := &UZIService{}
	body := []byte(`<!doctype html><html><head><style>.score-giant{letter-spacing:-.06em}</style><style>` + reportAlphaScoreUnitFixCSS + `</style></head><body><div class="score-giant">57</div></body></html>`)

	rewritten, changed, err := service.rewriteReportHTMLAssetURLs(context.Background(), 1, "601766.SH_20260505/full-report-standalone.html", body)
	if err != nil {
		t.Fatalf("rewriteReportHTMLAssetURLs() error = %v", err)
	}
	if changed {
		t.Fatal("rewriteReportHTMLAssetURLs() changed = true, want false")
	}
	if got := strings.Count(string(rewritten), reportAlphaScoreUnitFixMarker); got != 1 {
		t.Fatalf("alpha score unit fix marker count = %d, want 1", got)
	}
}

func TestNormalizeUZIDepthDefaultsToMedium(t *testing.T) {
	if got := normalizeUZIDepth(nil, ""); got != "medium" {
		t.Fatalf("normalizeUZIDepth(nil, \"\") = %q, want medium", got)
	}

	deep := "deep"
	if got := normalizeUZIDepth(&deep, "lite"); got != "deep" {
		t.Fatalf("normalizeUZIDepth(&deep, lite) = %q, want deep", got)
	}

	invalid := "full"
	if got := normalizeUZIDepth(&invalid, "lite"); got != "medium" {
		t.Fatalf("normalizeUZIDepth(&invalid, lite) = %q, want medium", got)
	}
}
