package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"usqay-print-server/internal/config"
	"usqay-print-server/internal/ws"
)

func TestAuthMiddleware(t *testing.T) {
	cfg := &config.Config{
		InternalToken: "secret123",
	}

	handler := authMiddleware(cfg, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Case 1: Missing Token
	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %v", rr.Code)
	}

	// Case 2: Wrong Token
	req = httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("X-Internal-Token", "wrong_token")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %v", rr.Code)
	}

	// Case 3: Correct Token
	req = httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("X-Internal-Token", "secret123")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %v", rr.Code)
	}
}

func TestAPIRoutes(t *testing.T) {
	cfg := &config.Config{
		Port:          8080,
		InternalToken: "secret123",
	}
	hub := ws.NewHub(cfg)
	router := buildRouter(hub, cfg)

	// Test GET /api/v1/agents (Empty initially)
	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("X-Internal-Token", "secret123")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %v", rr.Code)
	}

	var agents []ws.AgentInfo
	if err := json.Unmarshal(rr.Body.Bytes(), &agents); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %v", len(agents))
	}

	// Test GET /api/v1/agents/caja-01/status
	req = httptest.NewRequest("GET", "/api/v1/agents/caja-01/status?business_id=empresa-01", nil)
	req.Header.Set("X-Internal-Token", "secret123")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %v", rr.Code)
	}

	var status ws.AgentStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if status.Online {
		t.Errorf("expected status.Online to be false")
	}
	if status.PendingJobs != 0 {
		t.Errorf("expected 0 pending jobs, got %v", status.PendingJobs)
	}

	// Test POST /api/v1/jobs
	jobReq := LaravelJobRequest{
		JobID:         42,
		BusinessID:    "empresa-01",
		TerminalID:    "caja-01",
		Tipo:          "RED",
		DocumentoSlug: "COMANDA",
		Payload:       json.RawMessage(`{"items": []}`),
		ExpiraEn:      300,
	}
	bodyBytes, _ := json.Marshal(jobReq)
	req = httptest.NewRequest("POST", "/api/v1/jobs", bytes.NewBuffer(bodyBytes))
	req.Header.Set("X-Internal-Token", "secret123")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %v", rr.Code)
	}

	var jobResp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &jobResp); err != nil {
		t.Fatalf("failed to decode job response: %v", err)
	}
	if jobResp["job_id"] != "42" {
		t.Errorf("expected job_id '42', got %v", jobResp["job_id"])
	}
	if jobResp["estado"] != "pendiente" {
		t.Errorf("expected estado 'pendiente', got %v", jobResp["estado"])
	}

	// Check pending jobs count for caja-01
	status = hub.GetAgentStatus("empresa-01", "caja-01")
	if status.PendingJobs != 1 {
		t.Errorf("expected 1 pending job, got %v", status.PendingJobs)
	}

	// Test PUT /api/v1/jobs/42/status
	statusUpdate := struct {
		Estado string `json:"estado"`
	}{
		Estado: "IMPRESO",
	}
	bodyBytes, _ = json.Marshal(statusUpdate)
	req = httptest.NewRequest("PUT", "/api/v1/jobs/42/status", bytes.NewBuffer(bodyBytes))
	req.Header.Set("X-Internal-Token", "secret123")
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %v", rr.Code)
	}

	// Check pending jobs count after print
	status = hub.GetAgentStatus("empresa-01", "caja-01")
	if status.PendingJobs != 0 {
		t.Errorf("expected 0 pending jobs after printing, got %v", status.PendingJobs)
	}
}

func TestParseStringID(t *testing.T) {
	tests := []struct {
		input    any
		expected string
	}{
		{"123", "123"},
		{123, "123"},
		{int64(123), "123"},
		{123.0, "123"},
		{nil, ""},
	}

	for _, tc := range tests {
		got := parseStringID(tc.input)
		if got != tc.expected {
			t.Errorf("parseStringID(%v) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}
