package models

import "time"

type OpenAPIKeyCreateRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Name     string `json:"name"`
}

type OpenAPIKeyCreateResponse struct {
	APIKey    string     `json:"api_key"`
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type OpenAPIKeyTempTokenRequest struct {
	Token string `json:"token" binding:"required"`
	Name  string `json:"name"`
}

type OpenAPIKeyTempTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

type OpenAPIKeyFromTokenResponse struct {
	APIKey         string     `json:"api_key,omitempty"`
	KeyID          int        `json:"key_id,omitempty"`
	Name           string     `json:"name"`
	Scopes         []string   `json:"scopes"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	HasExistingKey bool       `json:"has_existing_key"`
}

type OpenAPIKeyRecord struct {
	ID        int
	UserID    int
	Name      string
	Scopes    []string
	Status    string
	ExpiresAt *time.Time
}

type OpenAPIMTFPredictOnceRequest struct {
	MTFPredictRequest
	PreferCache bool `json:"prefer_cache,omitempty"`
}

type OpenAPIEnvelope struct {
	RequestID string      `json:"request_id"`
	Status    string      `json:"status"`
	Data      interface{} `json:"data,omitempty"`
	Error     interface{} `json:"error,omitempty"`
}

type OpenAPIErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}
