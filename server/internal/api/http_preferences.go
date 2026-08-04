package api

import (
	"encoding/json"
	"io"
	"net/http"

	"mindfs/server/internal/preferences"
)

type rateLimitRetryPreferencesInput struct {
	Enabled          bool `json:"enabled"`
	BaseDelaySeconds int  `json:"base_delay_seconds"`
	IncrementSeconds int  `json:"increment_seconds"`
}

func (h *HTTPHandler) handleRateLimitRetryPreferencesGet(w http.ResponseWriter, _ *http.Request) {
	if h == nil || h.AppContext == nil || h.AppContext.GetPreferences() == nil {
		respondJSON(w, http.StatusOK, preferences.DefaultRateLimitRetryPreferences())
		return
	}
	respondJSON(w, http.StatusOK, h.AppContext.GetPreferences().GetRateLimitRetry())
}

func (h *HTTPHandler) handleRateLimitRetryPreferencesPut(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.AppContext == nil || h.AppContext.GetPreferences() == nil {
		respondError(w, http.StatusServiceUnavailable, errInvalidRequest("preferences not configured"))
		return
	}
	var input rateLimitRetryPreferencesInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest("invalid preferences payload"))
		return
	}
	store := h.AppContext.GetPreferences()
	if err := store.UpdateRateLimitRetry(preferences.RateLimitRetryPatch{
		Enabled:          input.Enabled,
		BaseDelaySeconds: input.BaseDelaySeconds,
		IncrementSeconds: input.IncrementSeconds,
	}); err != nil {
		respondError(w, http.StatusBadRequest, errInvalidRequest(err.Error()))
		return
	}
	respondJSON(w, http.StatusOK, store.GetRateLimitRetry())
}
