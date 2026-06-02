package models

type AdminGatewayBackendStatus struct {
	Name              string `json:"name"`
	Role              string `json:"role,omitempty"`
	URL               string `json:"url"`
	Capacity          int    `json:"capacity"`
	InFlight          int    `json:"in_flight"`
	Available         int    `json:"available"`
	SupportsCov       bool   `json:"supports_cov,omitempty"`
	SupportsDirectCov bool   `json:"supports_direct_cov,omitempty"`
	SupportsNonCov    bool   `json:"supports_non_cov,omitempty"`
	SupportsUZI       bool   `json:"supports_uzi,omitempty"`
}

type AdminGatewayQueueStatus struct {
	Reachable   bool                        `json:"reachable"`
	Status      string                      `json:"status"`
	Timestamp   string                      `json:"timestamp,omitempty"`
	SourceURL   string                      `json:"source_url,omitempty"`
	QueueDepth  int                         `json:"queue_depth"`
	Jobs        map[string]int              `json:"jobs"`
	Backends    []AdminGatewayBackendStatus `json:"backends"`
	Error       string                      `json:"error,omitempty"`
	CheckedPath string                      `json:"checked_path"`
}
