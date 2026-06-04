package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"fintrack-api/models"
)

type PaidPredictOnceRecord struct {
	ResourceID string
	OrderID    string
	Credential string
	Request    *models.MTFPredictRequest
}

type PaidPredictOnceRun struct {
	ShouldRun  bool
	InProgress bool
	Status     int
	Body       map[string]interface{}
}

func (s *WatchlistService) BeginPaidPredictOnce(ctx context.Context, record PaidPredictOnceRecord) (*PaidPredictOnceRun, error) {
	if s.db == nil || s.db.Conn == nil {
		return nil, fmt.Errorf("database is required for paid predict once")
	}
	resourceID := strings.TrimSpace(record.ResourceID)
	if resourceID == "" {
		resourceID = "mtf.predict.once"
	}
	orderID := strings.TrimSpace(record.OrderID)
	if orderID == "" {
		orderID = credentialReference(record.Credential)
	}
	signature, err := paidPredictOnceRequestSignature(record.Request)
	if err != nil {
		return nil, err
	}
	credentialHash := sha256Hex(strings.TrimSpace(record.Credential))
	periodKey := time.Now().UTC().Format("2006-01-02")

	result, err := s.db.Conn.ExecContext(ctx, `
		INSERT INTO ai_payment_records (
			resource_id, order_id, credential_hash, request_signature, period_key,
			payment_status, fulfillment_status, paid_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, 'paid', 'processing', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (resource_id, order_id, request_signature, period_key) DO NOTHING
	`, resourceID, orderID, credentialHash, signature, periodKey)
	if err != nil {
		return nil, fmt.Errorf("create paid prediction record: %v", err)
	}

	var status string
	var responseStatus sql.NullInt64
	var responseBody sql.NullString
	err = s.db.Conn.QueryRowContext(ctx, `
		SELECT fulfillment_status, response_status, response_body::text
		FROM ai_payment_records
		WHERE resource_id = $1 AND order_id = $2 AND request_signature = $3 AND period_key = $4
		LIMIT 1
	`, resourceID, orderID, signature, periodKey).Scan(&status, &responseStatus, &responseBody)
	if err != nil {
		return nil, fmt.Errorf("load paid prediction record: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 1 {
		return &PaidPredictOnceRun{ShouldRun: true}, nil
	}
	if status == "fulfilled" && responseStatus.Valid && responseBody.Valid {
		var body map[string]interface{}
		if err := json.Unmarshal([]byte(responseBody.String), &body); err != nil {
			return nil, fmt.Errorf("decode stored paid prediction response: %v", err)
		}
		return &PaidPredictOnceRun{Status: int(responseStatus.Int64), Body: body}, nil
	}
	return &PaidPredictOnceRun{InProgress: true}, nil
}

func (s *WatchlistService) CompletePaidPredictOnce(ctx context.Context, record PaidPredictOnceRecord, status int, body map[string]interface{}) error {
	if s.db == nil || s.db.Conn == nil {
		return fmt.Errorf("database is required for paid predict once")
	}
	resourceID := strings.TrimSpace(record.ResourceID)
	if resourceID == "" {
		resourceID = "mtf.predict.once"
	}
	orderID := strings.TrimSpace(record.OrderID)
	if orderID == "" {
		orderID = credentialReference(record.Credential)
	}
	signature, err := paidPredictOnceRequestSignature(record.Request)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal paid prediction response: %v", err)
	}
	periodKey := time.Now().UTC().Format("2006-01-02")
	_, err = s.db.Conn.ExecContext(ctx, `
		UPDATE ai_payment_records
		SET fulfillment_status = 'fulfilled',
			response_status = $5,
			response_body = $6::jsonb,
			fulfilled_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE resource_id = $1 AND order_id = $2 AND request_signature = $3 AND period_key = $4
	`, resourceID, orderID, signature, periodKey, status, string(raw))
	if err != nil {
		return fmt.Errorf("complete paid prediction record: %v", err)
	}
	return nil
}

func paidPredictOnceRequestSignature(req *models.MTFPredictRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("request is required")
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal paid prediction request: %v", err)
	}
	return sha256Hex(string(raw)), nil
}

func credentialReference(credential string) string {
	hash := sha256Hex(strings.TrimSpace(credential))
	if hash == "" {
		return "missing-order"
	}
	return "credential:" + hash
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
