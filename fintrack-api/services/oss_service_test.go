package services

import (
	"testing"

	"fintrack-api/config"
)

func TestOSSServiceBuildObjectKey(t *testing.T) {
	service := &OSSService{
		config: &config.OSSConfig{
			Enabled:         true,
			Prefix:          "uzi-reports",
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			Region:          "cn-hangzhou",
			Bucket:          "demo-bucket",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
		},
	}

	objectKey, err := service.BuildObjectKey(18, "daily/601766.SH/report.html")
	if err != nil {
		t.Fatalf("BuildObjectKey() error = %v", err)
	}

	want := "uzi-reports/18/daily/601766.SH/report.html"
	if objectKey != want {
		t.Fatalf("BuildObjectKey() = %q, want %q", objectKey, want)
	}
}

func TestOSSServiceManagedObjectKey(t *testing.T) {
	service := &OSSService{
		config: &config.OSSConfig{
			Enabled:         true,
			Prefix:          "uzi-reports",
			Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
			Region:          "cn-hangzhou",
			Bucket:          "demo-bucket",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
		},
	}

	if !service.IsManagedObjectKey("uzi-reports/18/demo/report.html") {
		t.Fatalf("IsManagedObjectKey() = false, want true")
	}
	if service.IsManagedObjectKey("https://example.com/report.html") {
		t.Fatalf("IsManagedObjectKey() = true for external url, want false")
	}
}

func TestOSSServiceRewriteSignedURLToPublicBase(t *testing.T) {
	service := &OSSService{
		config: &config.OSSConfig{
			Enabled:         true,
			Prefix:          "uzi-reports",
			Endpoint:        "oss-ap-southeast-1.aliyuncs.com",
			Region:          "ap-southeast-1",
			Bucket:          "uzi-report",
			AccessKeyID:     "ak",
			AccessKeySecret: "sk",
			PublicBaseURL:   "http://uzi-reports.meetlife.com.cn",
		},
	}

	signedURL := "https://uzi-report.oss-ap-southeast-1.aliyuncs.com/uzi-reports/6/demo.html?x-oss-signature=abc"

	got, err := service.RewriteSignedURLToPublicBase(signedURL)
	if err != nil {
		t.Fatalf("RewriteSignedURLToPublicBase() error = %v", err)
	}

	want := "http://uzi-reports.meetlife.com.cn/uzi-reports/6/demo.html?x-oss-signature=abc"
	if got != want {
		t.Fatalf("RewriteSignedURLToPublicBase() = %q, want %q", got, want)
	}
}
