package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ziuus/goroute/pkg/quota"
	"github.com/ziuus/goroute/pkg/router"
	"github.com/ziuus/goroute/pkg/storage"
)

func TestNewEndpoints(t *testing.T) {
	r := router.NewRouter()
	limiter := quota.NewLimiter()
	srv := NewServer(r, limiter, false)

	handler := srv.Handler()

	// 1. GET /api/oauth/cliproxy-import
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/cliproxy-import", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("cliproxy-import GET failed with status %d: %s", w.Code, w.Body.String())
	}

	// 2. POST /api/usage/budget
	bReq := storage.DomainBudget{
		APIKeyID:         "test-api-key",
		DailyLimitUSD:    10.0,
		WeeklyLimitUSD:   50.0,
		MonthlyLimitUSD:  200.0,
		WarningThreshold: 0.8,
		ResetInterval:    "daily",
	}
	body, _ := json.Marshal(bReq)
	req = httptest.NewRequest(http.MethodPost, "/api/usage/budget", bytes.NewReader(body))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("usage budget POST failed with status %d: %s", w.Code, w.Body.String())
	}

	// 3. GET /api/usage/budget?apiKeyId=test-api-key
	req = httptest.NewRequest(http.MethodGet, "/api/usage/budget?apiKeyId=test-api-key", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("usage budget GET failed with status %d: %s", w.Code, w.Body.String())
	}

	// 4. GET /api/usage/budget/bulk
	req = httptest.NewRequest(http.MethodGet, "/api/usage/budget/bulk", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("usage budget bulk GET failed with status %d: %s", w.Code, w.Body.String())
	}

	// 5. GET & POST /api/admin/concurrency
	req = httptest.NewRequest(http.MethodGet, "/api/admin/concurrency", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("admin concurrency GET failed: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/admin/concurrency?action=reset-semaphores", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("admin concurrency POST failed: %d", w.Code)
	}

	// 6. POST /api/webhooks/validate-url
	vBody := []byte(`{"url":"https://api.github.com/webhook"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/validate-url", bytes.NewReader(vBody))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("webhook validate url POST failed: %d", w.Code)
	}

	// 7. GET /api/auth/status
	req = httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("auth status GET failed: %d", w.Code)
	}

	// 8. GET /api/version-manager/status
	req = httptest.NewRequest(http.MethodGet, "/api/version-manager/status", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("version manager status GET failed: %d", w.Code)
	}
}
