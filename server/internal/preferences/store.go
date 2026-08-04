package preferences

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mindfs/server/internal/agent"
	"mindfs/server/internal/config"
)

const preferencesFileName = "preferences.json"

const (
	DefaultRateLimitRetryBaseDelaySeconds = 10
	DefaultRateLimitRetryIncrementSeconds = 5
	MaxRateLimitRetrySettingSeconds       = 3600
)

type Store struct {
	mu   sync.RWMutex
	path string
	data UserPreferences
}

type UserPreferences struct {
	Agents         map[string]AgentDefaults   `json:"agents,omitempty"`
	RateLimitRetry *RateLimitRetryPreferences `json:"rate_limit_retry,omitempty"`
}

type RateLimitRetryPreferences struct {
	Enabled                     bool `json:"enabled"`
	BaseDelaySeconds            int  `json:"base_delay_seconds"`
	IncrementSeconds            int  `json:"increment_seconds"`
	RecommendedBaseDelaySeconds int  `json:"recommended_base_delay_seconds,omitempty"`
	LastRecoverySeconds         int  `json:"last_recovery_seconds,omitempty"`
	LastRetryCount              int  `json:"last_retry_count,omitempty"`
	RecoverySampleCount         int  `json:"recovery_sample_count,omitempty"`
}

type RateLimitRetryPatch struct {
	Enabled          bool
	BaseDelaySeconds int
	IncrementSeconds int
}

type AgentDefaults struct {
	Model               string               `json:"model,omitempty"`
	Effort              string               `json:"effort,omitempty"`
	FastService         string               `json:"fast_service,omitempty"`
	LastConfigSelection *LastConfigSelection `json:"last_config_selection,omitempty"`
}

type LastConfigSelection struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type AgentDefaultsPatch struct {
	Model       *string
	Effort      string
	FastService string
}

func DefaultRateLimitRetryPreferences() RateLimitRetryPreferences {
	return RateLimitRetryPreferences{
		Enabled:          true,
		BaseDelaySeconds: DefaultRateLimitRetryBaseDelaySeconds,
		IncrementSeconds: DefaultRateLimitRetryIncrementSeconds,
	}
}

func validateRateLimitRetryPatch(patch RateLimitRetryPatch) error {
	if patch.BaseDelaySeconds < 1 || patch.BaseDelaySeconds > MaxRateLimitRetrySettingSeconds {
		return errors.New("base delay must be between 1 and 3600 seconds")
	}
	if patch.IncrementSeconds < 0 || patch.IncrementSeconds > MaxRateLimitRetrySettingSeconds {
		return errors.New("increment must be between 0 and 3600 seconds")
	}
	return nil
}

func (s *Store) GetRateLimitRetry() RateLimitRetryPreferences {
	if s == nil {
		return DefaultRateLimitRetryPreferences()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.RateLimitRetry == nil {
		return DefaultRateLimitRetryPreferences()
	}
	return *s.data.RateLimitRetry
}

func (s *Store) UpdateRateLimitRetry(patch RateLimitRetryPatch) error {
	if s == nil {
		return nil
	}
	if err := validateRateLimitRetryPatch(patch); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	next := DefaultRateLimitRetryPreferences()
	if s.data.RateLimitRetry != nil {
		next = *s.data.RateLimitRetry
	}
	next.Enabled = patch.Enabled
	next.BaseDelaySeconds = patch.BaseDelaySeconds
	next.IncrementSeconds = patch.IncrementSeconds
	if s.data.RateLimitRetry != nil && *s.data.RateLimitRetry == next {
		return nil
	}
	s.data.RateLimitRetry = &next
	return s.saveLocked()
}

func (s *Store) RecordRateLimitRecovery(elapsed time.Duration, retryCount int) error {
	if s == nil || elapsed <= 0 || retryCount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	next := DefaultRateLimitRetryPreferences()
	if s.data.RateLimitRetry != nil {
		next = *s.data.RateLimitRetry
	}
	elapsedSeconds := int(math.Ceil(elapsed.Seconds()))
	recommended := int(math.Round(elapsed.Seconds()/10.0)) * 10
	if recommended < 10 {
		recommended = 10
	}
	if recommended > MaxRateLimitRetrySettingSeconds {
		recommended = MaxRateLimitRetrySettingSeconds
	}
	next.RecommendedBaseDelaySeconds = recommended
	next.LastRecoverySeconds = elapsedSeconds
	next.LastRetryCount = retryCount
	next.RecoverySampleCount++
	s.data.RateLimitRetry = &next
	return s.saveLocked()
}

func NewStore() (*Store, error) {
	configDir, err := config.MindFSConfigDir()
	if err != nil {
		return nil, err
	}
	store := &Store{
		path: filepath.Join(configDir, preferencesFileName),
		data: UserPreferences{Agents: map[string]AgentDefaults{}},
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	var data UserPreferences
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}
	if data.Agents == nil {
		data.Agents = map[string]AgentDefaults{}
	}
	s.data = data
	return nil
}

func (s *Store) UpdateAgentDefaultsIfChanged(agentName string, patch AgentDefaultsPatch) (bool, error) {
	if s == nil {
		return false, nil
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return false, nil
	}
	patch.Effort = strings.TrimSpace(patch.Effort)
	patch.FastService = strings.TrimSpace(patch.FastService)
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.Agents == nil {
		s.data.Agents = map[string]AgentDefaults{}
	}
	next := s.data.Agents[agentName]
	// Empty model means follow config for Codex and clears any previous
	// explicit override. Other agents keep non-empty-only updates.
	if patch.Model != nil {
		model := strings.TrimSpace(*patch.Model)
		if model != "" || agentName == "codex" {
			next.Model = model
		}
	}
	if patch.Effort != "" {
		next.Effort = patch.Effort
	}
	if patch.FastService != "" {
		next.FastService = patch.FastService
	}
	if s.data.Agents[agentName] == next {
		return false, nil
	}
	s.data.Agents[agentName] = next
	if err := s.saveLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) UpdateAgentLastConfigSelection(agentName string, selection LastConfigSelection) error {
	if s == nil {
		return nil
	}
	agentName = strings.TrimSpace(agentName)
	selection.Type = strings.TrimSpace(selection.Type)
	selection.ID = strings.TrimSpace(selection.ID)
	selection.Name = strings.TrimSpace(selection.Name)
	if agentName == "" || selection.Type == "" || selection.ID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data.Agents == nil {
		s.data.Agents = map[string]AgentDefaults{}
	}
	next := s.data.Agents[agentName]
	if next.LastConfigSelection != nil && *next.LastConfigSelection == selection {
		return nil
	}
	next.LastConfigSelection = &selection
	s.data.Agents[agentName] = next
	return s.saveLocked()
}

func (s *Store) ApplyAgentDefaults(statuses []agent.Status) []agent.Status {
	if s == nil || len(statuses) == 0 {
		return statuses
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.data.Agents) == 0 {
		return statuses
	}
	out := append([]agent.Status(nil), statuses...)
	for i := range out {
		defaults, hasDefaults := s.data.Agents[strings.TrimSpace(out[i].Name)]
		if !hasDefaults {
			continue
		}
		// Codex follows ~/.codex/config.toml unless the user explicitly
		// selected a model (stored as a non-empty preference).
		if strings.TrimSpace(out[i].Name) == "codex" {
			out[i].DefaultModelID = strings.TrimSpace(defaults.Model)
		} else if defaults.Model != "" {
			out[i].DefaultModelID = defaults.Model
		}
		if defaults.Effort != "" {
			out[i].DefaultEffort = defaults.Effort
		}
		if defaults.FastService != "" {
			out[i].DefaultFastService = defaults.FastService
		}
		if defaults.LastConfigSelection != nil {
			out[i].LastConfigSelection = *defaults.LastConfigSelection
		}
	}
	return out
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(s.path)
		if retryErr := os.Rename(tmp, s.path); retryErr != nil {
			return err
		}
	}
	return nil
}
