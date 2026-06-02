package backend

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultRequestGzipMinBytes = 64 * 1024

type Endpoint struct {
	Name              string
	Role              string
	URL               string
	Capacity          int
	SupportsCov       bool
	SupportsDirectCov bool
	SupportsNonCov    bool
	SupportsUZI       bool
}

type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

func requestGzipMinBytes() int {
	raw := strings.TrimSpace(os.Getenv("AI_BACKEND_REQUEST_GZIP_MIN_BYTES"))
	if raw == "" {
		return defaultRequestGzipMinBytes
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultRequestGzipMinBytes
	}
	return value
}

func maybeCompressPayload(payload []byte) ([]byte, bool, error) {
	if len(payload) < requestGzipMinBytes() {
		return payload, false, nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		_ = zw.Close()
		return nil, false, err
	}
	if err := zw.Close(); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

func (c *Client) Submit(ctx context.Context, endpoint Endpoint, targetPath string, payload []byte) (int, []byte, error) {
	if strings.TrimSpace(targetPath) == "" {
		targetPath = "/internal/predict_for_best_sync"
	}
	url := strings.TrimRight(endpoint.URL, "/") + targetPath
	requestBody, compressed, err := maybeCompressPayload(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("compress backend request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(requestBody))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if compressed {
		req.Header.Set("Content-Encoding", "gzip")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("call backend %s: %w", endpoint.Name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read backend response: %w", err)
	}
	return resp.StatusCode, body, nil
}
