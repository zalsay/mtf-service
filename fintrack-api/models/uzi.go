package models

import "time"

type UZIAnalyzeRequest struct {
	Ticker   string  `json:"ticker" binding:"required"`
	Depth    *string `json:"depth,omitempty"`
	NoResume *bool   `json:"no_resume,omitempty"`
}

type UZIReportRecord struct {
	ID                 int        `json:"id" db:"id"`
	UserID             int        `json:"user_id" db:"user_id"`
	Ticker             string     `json:"ticker" db:"ticker"`
	Depth              *string    `json:"depth,omitempty" db:"depth"`
	Status             string     `json:"status" db:"status"`
	DirectoryName      string     `json:"directory_name" db:"directory_name"`
	DateTag            string     `json:"date_tag" db:"date_tag"`
	ReportRelativePath string     `json:"report_relative_path" db:"report_relative_path"`
	ReportURL          string     `json:"report_url" db:"report_url"`
	SizeBytes          int64      `json:"size_bytes" db:"size_bytes"`
	ExitCode           *int       `json:"exit_code,omitempty" db:"exit_code"`
	DurationSeconds    *float64   `json:"duration_seconds,omitempty" db:"duration_seconds"`
	StdoutTail         *string    `json:"stdout_tail,omitempty" db:"stdout_tail"`
	StderrTail         *string    `json:"stderr_tail,omitempty" db:"stderr_tail"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
	DeletedAt          *time.Time `json:"deleted_at,omitempty" db:"deleted_at"`
}

type UZIReportItem struct {
	ID                 int     `json:"id,omitempty"`
	Ticker             string  `json:"ticker"`
	Depth              *string `json:"depth,omitempty"`
	Status             string  `json:"status,omitempty"`
	DirectoryName      string  `json:"directory_name"`
	DateTag            string  `json:"date_tag"`
	ReportRelativePath string  `json:"report_relative_path"`
	ReportURL          string  `json:"report_url"`
	SizeBytes          int64   `json:"size_bytes"`
	UpdatedAt          string  `json:"updated_at"`
	CreatedAt          string  `json:"created_at,omitempty"`
}

type UZIReportListResponse struct {
	Items []UZIReportItem `json:"items"`
	Count int             `json:"count"`
}

type UZIAnalyzeStatus struct {
	Status    string         `json:"status"`
	JobID     string         `json:"job_id,omitempty"`
	Ticker    string         `json:"ticker,omitempty"`
	Stage     string         `json:"stage,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Report    *UZIReportItem `json:"report,omitempty"`
	StartedAt string         `json:"started_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

type UZIAnalyzeQueueResponse struct {
	Success       bool                   `json:"success"`
	Message       string                 `json:"message,omitempty"`
	Reused        bool                   `json:"reused,omitempty"`
	ForceEnqueue  bool                   `json:"force_enqueue,omitempty"`
	JobID         string                 `json:"job_id"`
	JobKind       string                 `json:"job_kind,omitempty"`
	Status        string                 `json:"status"`
	Ticker        string                 `json:"ticker,omitempty"`
	CurrentStage  string                 `json:"current_stage,omitempty"`
	TargetPath    string                 `json:"target_path,omitempty"`
	RequestKey    string                 `json:"request_key,omitempty"`
	CreatedAt     string                 `json:"created_at,omitempty"`
	StatusURL     string                 `json:"status_url,omitempty"`
	QueuePosition int                    `json:"queue_position,omitempty"`
	QueueStatus   map[string]interface{} `json:"queue_status,omitempty"`
}

type UZIAnalyzeJobStatusResponse struct {
	JobID          string                 `json:"job_id"`
	JobKind        string                 `json:"job_kind,omitempty"`
	Status         string                 `json:"status"`
	ForceEnqueue   bool                   `json:"force_enqueue,omitempty"`
	Ticker         string                 `json:"ticker,omitempty"`
	StockCode      string                 `json:"stock_code,omitempty"`
	CurrentStage   string                 `json:"current_stage,omitempty"`
	TargetPath     string                 `json:"target_path,omitempty"`
	Backend        string                 `json:"backend,omitempty"`
	UpstreamStatus int                    `json:"upstream_status,omitempty"`
	Error          string                 `json:"error,omitempty"`
	QueuePosition  int                    `json:"queue_position,omitempty"`
	CreatedAt      string                 `json:"created_at,omitempty"`
	StartedAt      *string                `json:"started_at,omitempty"`
	FinishedAt     *string                `json:"finished_at,omitempty"`
	Result         map[string]interface{} `json:"result,omitempty"`
	Report         *UZIReportItem         `json:"report,omitempty"`
}
