package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"alipay-service/internal/payment"
)

type Config struct {
	MerchantID   string
	MerchantName string
	ResourceID   string
	ResourceName string
	AmountCents  int
	Currency     string
}

type Server struct {
	mux      *http.ServeMux
	cfg      Config
	verifier payment.Verifier
}

func New(cfg Config, verifier payment.Verifier) http.Handler {
	s := &Server{
		mux:      http.NewServeMux(),
		cfg:      cfg,
		verifier: verifier,
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /api/v1/payments/verify", s.handleVerifyPayment)
	s.mux.HandleFunc("POST /api/v1/dev/credentials", s.handleDevCredential)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "alipay-service",
	})
}

func (s *Server) handleDevCredential(w http.ResponseWriter, r *http.Request) {
	local, ok := s.verifier.(*payment.LocalVerifier)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dev credentials are only available in local mode"})
		return
	}
	var req struct {
		OrderID    string `json:"order_id"`
		ResourceID string `json:"resource_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(req.ResourceID) == "" {
		req.ResourceID = s.cfg.ResourceID
	}
	if strings.TrimSpace(req.OrderID) == "" {
		req.OrderID = "dev-order"
	}
	credential, err := local.IssueDevCredential(req.OrderID, req.ResourceID, time.Now())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"credential":  credential,
		"resource_id": req.ResourceID,
		"order_id":    req.OrderID,
	})
}

func (s *Server) handleVerifyPayment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Credential string `json:"credential"`
		ResourceID string `json:"resource_id"`
		OrderID    string `json:"order_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	result, err := s.verifier.Verify(r.Context(), strings.TrimSpace(req.Credential), payment.VerifyRequest{
		ResourceID: req.ResourceID,
		OrderID:    req.OrderID,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
