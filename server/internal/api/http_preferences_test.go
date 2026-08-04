package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"mindfs/server/internal/preferences"
)

func TestRateLimitRetryPreferencesHandlers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	store, err := preferences.NewStore()
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	handler := &HTTPHandler{AppContext: &AppContext{Prefs: store}}

	get := httptest.NewRecorder()
	handler.handleRateLimitRetryPreferencesGet(get, httptest.NewRequest(http.MethodGet, "/api/preferences/rate-limit-retry", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", get.Code, get.Body.String())
	}
	var defaults preferences.RateLimitRetryPreferences
	if err := json.Unmarshal(get.Body.Bytes(), &defaults); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if !defaults.Enabled || defaults.BaseDelaySeconds != 10 || defaults.IncrementSeconds != 5 {
		t.Fatalf("GET defaults = %#v", defaults)
	}

	put := httptest.NewRecorder()
	handler.handleRateLimitRetryPreferencesPut(put, httptest.NewRequest(
		http.MethodPut,
		"/api/preferences/rate-limit-retry",
		bytes.NewBufferString(`{"enabled":false,"base_delay_seconds":25,"increment_seconds":0}`),
	))
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", put.Code, put.Body.String())
	}
	got := store.GetRateLimitRetry()
	if got.Enabled || got.BaseDelaySeconds != 25 || got.IncrementSeconds != 0 {
		t.Fatalf("stored preferences = %#v", got)
	}

	invalid := httptest.NewRecorder()
	handler.handleRateLimitRetryPreferencesPut(invalid, httptest.NewRequest(
		http.MethodPut,
		"/api/preferences/rate-limit-retry",
		bytes.NewBufferString(`{"enabled":true,"base_delay_seconds":0,"increment_seconds":5}`),
	))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid PUT status = %d, body=%s", invalid.Code, invalid.Body.String())
	}
}
