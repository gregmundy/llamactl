package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestHandler(apiKey string) http.Handler {
	state := NewState()
	state.Update("a", Sample{State: "idle"})
	return NewHandler(state, nil, apiKey, func() time.Time { return time.Date(2026, 5, 16, 14, 0, 0, 0, time.UTC) })
}

func TestHandler_HealthNoAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != 200 {
		t.Errorf("Code = %d", rec.Code)
	}
}

func TestHandler_TelemetryRequiresAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil))
	if rec.Code != 401 {
		t.Errorf("Code = %d, want 401", rec.Code)
	}
}

func TestHandler_TelemetryWrongToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("Code = %d, want 401", rec.Code)
	}
}

func TestHandler_TelemetryRightToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil)
	req.Header.Set("Authorization", "Bearer sk-xxx")
	rec := httptest.NewRecorder()
	newTestHandler("sk-xxx").ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("Code = %d, want 200", rec.Code)
	}
	var resp TelemetryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.GeneratedAt == "" {
		t.Error("GeneratedAt missing")
	}
}

func TestHandler_NoAPIKeyMeansNoAuth(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/telemetry", nil))
	if rec.Code != 200 {
		t.Errorf("Code = %d, want 200 (no apikey configured)", rec.Code)
	}
}

func TestHandler_404On405(t *testing.T) {
	rec := httptest.NewRecorder()
	newTestHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/wat", nil))
	if rec.Code != 404 {
		t.Errorf("404 case: Code = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	newTestHandler("").ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/telemetry", strings.NewReader("")))
	if rec.Code != 405 {
		t.Errorf("405 case: Code = %d", rec.Code)
	}
}
