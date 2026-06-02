package services

import (
	"errors"
	"path"
	"strings"
)

func normalizeUZIReportPath(raw string) (string, error) {
	cleanedPath := strings.TrimPrefix(path.Clean("/"+strings.TrimSpace(raw)), "/")
	if cleanedPath == "." || cleanedPath == "" {
		return "", errors.New("report path is required")
	}
	if strings.Contains(cleanedPath, "..") {
		return "", errors.New("invalid report path")
	}
	return cleanedPath, nil
}

func reportDirectory(relativePath string) string {
	cleaned := strings.TrimSpace(relativePath)
	if cleaned == "" {
		return ""
	}
	dir := path.Dir(cleaned)
	if dir == "." {
		return ""
	}
	return strings.TrimPrefix(dir, "/")
}
