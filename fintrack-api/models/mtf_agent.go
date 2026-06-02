package models

import "time"

type MTFAgentSession struct {
	ID               int        `json:"id" db:"id"`
	UserID           int        `json:"user_id" db:"user_id"`
	DeepSeekThreadID string     `json:"deepseek_thread_id" db:"deepseek_thread_id"`
	ModelID          string     `json:"model_id" db:"model_id"`
	LastUsedAt       *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

type MTFAgentMemory struct {
	ID         int       `json:"id" db:"id"`
	UserID     int       `json:"user_id" db:"user_id"`
	MemoryType string    `json:"memory_type" db:"memory_type"`
	Content    string    `json:"content" db:"content"`
	Source     string    `json:"source" db:"source"`
	Confidence float64   `json:"confidence" db:"confidence"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

type MTFAgentMessage struct {
	ID        int       `json:"id" db:"id"`
	UserID    int       `json:"user_id" db:"user_id"`
	ThreadID  string    `json:"thread_id" db:"thread_id"`
	Role      string    `json:"role" db:"role"`
	Content   string    `json:"content" db:"content"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type MTFAgentSessionResponse struct {
	ThreadID         string `json:"thread_id,omitempty"`
	ModelID          string `json:"model_id,omitempty"`
	RuntimeAvailable bool   `json:"runtime_available"`
	MemoryCount      int    `json:"memory_count"`
	HasAIModelConfig bool   `json:"has_ai_model_config"`
}

type MTFAgentMessageRequest struct {
	Message string `json:"message" binding:"required"`
}

type MTFAgentMessageResponse struct {
	ThreadID string  `json:"thread_id"`
	Message  Message `json:"message"`
	Model    string  `json:"model,omitempty"`
}

type MTFAgentMessageJobResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type MTFAgentMessageJobStatusResponse struct {
	JobID    string                   `json:"job_id"`
	Status   string                   `json:"status"`
	Response *MTFAgentMessageResponse `json:"response,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

type MTFAgentMessagesResponse struct {
	ThreadID string            `json:"thread_id,omitempty"`
	Messages []MTFAgentMessage `json:"messages"`
}

type MTFAgentResetResponse struct {
	ThreadID string `json:"thread_id"`
}

type MTFAgentContextSummary struct {
	Watchlist  string
	Prediction string
	UZIReports string
}
