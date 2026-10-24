package preferences

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
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
	DefaultRateLimitRetryBaseDelaySeconds  = 10
	DefaultRateLimitRetryIncrementSeconds  = 5
	MaxRateLimitRetrySettingSeconds        = 3600
	RateLimitRetryRecommendationAttempt    = 3
	minRateLimitRetryRecommendationSeconds = 10
	DefaultIdleSessionResourceReleaseHours = 72
	MaxIdleSessionResourceReleaseHours     = 2_562_047
)

type Store struct {
	mu   sync.RWMutex
	path string
	data UserPreferences
}

type UserPreferences struct {
	AgentPinnedModels               map[string][]string        `json:"agent_pinned_models,omitempty"`
	LauncherNodes                   []LauncherNode             `json:"launcher_nodes,omitempty"`
	Agents                          map[string]AgentDefaults   `json:"agents,omitempty"`
	LastAgent                       string                     `json:"last_agent,omitempty"`
	RateLimitRetry                  *RateLimitRetryPreferences `json:"rate_limit_retry,omitempty"`
	SessionNaming                   SessionNamingDefaults      `json:"session_naming,omitempty"`
	IdleSessionResourceReleaseHours int                        `json:"idle_session_resource_release_hours,omitempty"`
	NewProjectMetaLocation          string                     `json:"new_project_meta_location,omitempty"`
}

type RateLimitRetryPreferences struct {
	Enabled                     bool `json:"enabled"`
	BaseDelaySeconds            int  `json:"base_delay_seconds"`
	IncrementSeconds            int  `json:"increment_seconds"`
	RecommendedBaseDelaySeconds int  `json:"recommended_base_delay_seconds,omitempty"`
	LastRecoverySeconds         int  `json:"last_recovery_seconds,omitempty"`
	LastRetryCount              int  `json:"last_retry_count,omitempty"`
	LastProbeSeconds            int  `json:"last_probe_seconds,omitempty"`
	RecoverySampleCount         int  `json:"recovery_sample_count,omitempty"`
}

type RateLimitRetryPatch struct {
	Enabled          bool
	BaseDelaySeconds int
	IncrementSeconds int
}

type LauncherNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

func (s *Store) LauncherNodes() []LauncherNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]LauncherNode{}, s.data.LauncherNodes...)
}

func (s *Store) UpdateLauncherNodes(nodes []LauncherNode) error {
	if len(nodes) > 1000 {
		return errors.New("too many nodes")
	}
	next := append([]LauncherNode{}, nodes...)
	ids, urls := map[string]bool{}, map[string]bool{}
	for i := range next {
		n := &next[i]
		n.ID, n.Name, n.URL = strings.TrimSpace(n.ID), strings.TrimSpace(n.Name), strings.TrimSpace(n.URL)
		u, err := url.Parse(n.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || n.ID == "" || n.Name == "" || n.CreatedAt == "" {
			return errors.New("invalid node: name, id, creation time and HTTP(S) URL are required")
		}
		if ids[n.ID] || urls[n.URL] {
			return errors.New("duplicate node")
		}
		ids[n.ID], urls[n.URL] = true, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.LauncherNodes
	s.data.LauncherNodes = next
	if err := s.saveLocked(); err != nil {
		s.data.LauncherNodes = previous
		return err
	}
	return nil
}

func (s *Store) NewProjectMetaLocation() string {
	if s == nil {
		return "project"
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.NewProjectMetaLocation == "home" {
		return "home"
	}
	return "project"
}

func (s *Store) UpdateNewProjectMetaLocation(location string) error {
	if s == nil {
		return nil
	}
	location = strings.TrimSpace(location)
	if location != "project" && location != "home" {
		return errors.New("invalid new project metadata location")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.NewProjectMetaLocation == location {
		return nil
	}
	s.data.NewProjectMetaLocation = location
	return s.saveLocked()
}

func (s *Store) IdleSessionResourceReleaseHours() int {
	if s == nil {
		return DefaultIdleSessionResourceReleaseHours
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.data.IdleSessionResourceReleaseHours <= 0 {
		return DefaultIdleSessionResourceReleaseHours
	}
	return s.data.IdleSessionResourceReleaseHours
}

func (s *Store) UpdateIdleSessionResourceReleaseHours(hours int) error {
	if s == nil {
		return nil
	}
	if hours <= 0 || hours > MaxIdleSessionResourceReleaseHours {
		return errors.New("idle session resource release hours are out of range")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.IdleSessionResourceReleaseHours == hours {
		return nil
	}
	s.data.IdleSessionResourceReleaseHours = hours
	return s.saveLocked()
}

type SessionNamingDefaults struct {
	Agent    string `json:"agent,omitempty"`
	Model    string `json:"model,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
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
	next := *s.data.RateLimitRetry
	next.RecommendedBaseDelaySeconds = recommendRateLimitRetryBaseDelay(
		next.LastRecoverySeconds,
		next.LastProbeSeconds,
		next.IncrementSeconds,
	)
	return next
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
	next.RecommendedBaseDelaySeconds = recommendRateLimitRetryBaseDelay(
		next.LastRecoverySeconds,
		next.LastProbeSeconds,
		next.IncrementSeconds,
	)
	if s.data.RateLimitRetry != nil && *s.data.RateLimitRetry == next {
		return nil
	}
	s.data.RateLimitRetry = &next
	return s.saveLocked()
}

func recommendRateLimitRetryBaseDelay(elapsedSeconds, probeSeconds, incrementSeconds int) int {
	if elapsedSeconds <= 0 {
		return 0
	}
	if probeSeconds < 0 {
		probeSeconds = 0
	}
	if incrementSeconds < 0 {
		incrementSeconds = 0
	}

	attempts := int64(RateLimitRetryRecommendationAttempt)
	incrementSteps := attempts * (attempts - 1) / 2
	priorProbeCount := attempts - 1
	remaining := int64(elapsedSeconds) -
		incrementSteps*int64(incrementSeconds) -
		priorProbeCount*int64(probeSeconds)
	recommended := remaining / attempts
	if recommended < minRateLimitRetryRecommendationSeconds {
		return minRateLimitRetryRecommendationSeconds
	}
	if recommended > MaxRateLimitRetrySettingSeconds {
		return MaxRateLimitRetrySettingSeconds
	}
	return int(recommended)
}

func (s *Store) RecordRateLimitRecovery(elapsed time.Duration, retryCount int, typicalProbeDuration time.Duration) error {
	if s == nil || elapsed <= 0 || retryCount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	next := DefaultRateLimitRetryPreferences()
	if s.data.RateLimitRetry != nil {
		next = *s.data.RateLimitRetry
	}
	next.LastRecoverySeconds = int(math.Ceil(elapsed.Seconds()))
	next.LastRetryCount = retryCount
	if typicalProbeDuration > 0 {
		next.LastProbeSeconds = int(math.Ceil(typicalProbeDuration.Seconds()))
	} else {
		next.LastProbeSeconds = 0
	}
	next.RecommendedBaseDelaySeconds = recommendRateLimitRetryBaseDelay(
		next.LastRecoverySeconds,
		next.LastProbeSeconds,
		next.IncrementSeconds,
	)
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
	if s.data.Agents[agentName] == next && s.data.LastAgent == agentName {
		return false, nil
	}
	previousDefaults, existed := s.data.Agents[agentName]
	previousAgent := s.data.LastAgent
	s.data.Agents[agentName] = next
	s.data.LastAgent = agentName
	if err := s.saveLocked(); err != nil {
		if existed {
			s.data.Agents[agentName] = previousDefaults
		} else {
			delete(s.data.Agents, agentName)
		}
		s.data.LastAgent = previousAgent
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

// AgentLastConfigSelections returns a snapshot of every agent's last manual
// config selection keyed by agent name. Callers must treat the result as
// read-only.
func (s *Store) AgentLastConfigSelections() map[string]LastConfigSelection {
	out := map[string]LastConfigSelection{}
	if s == nil {
		return out
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, defaults := range s.data.Agents {
		if defaults.LastConfigSelection != nil {
			out[strings.TrimSpace(name)] = *defaults.LastConfigSelection
		}
	}
	return out
}

func (s *Store) SessionNamingDefaults() SessionNamingDefaults {
	if s == nil {
		return SessionNamingDefaults{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.SessionNaming
}

func (s *Store) UpdateSessionNamingDefaults(agentName, model string, disabled bool) error {
	if s == nil {
		return nil
	}
	next := SessionNamingDefaults{
		Agent:    strings.TrimSpace(agentName),
		Model:    strings.TrimSpace(model),
		Disabled: disabled,
	}
	if next.Agent == "" && !next.Disabled {
		return errors.New("session naming agent is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.SessionNaming == next {
		return nil
	}
	s.data.SessionNaming = next
	return s.saveLocked()
}

func (s *Store) ApplyAgentDefaults(statuses []agent.Status) []agent.Status {
	if s == nil || len(statuses) == 0 {
		return statuses
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.data.Agents) == 0 && s.data.LastAgent == "" && len(s.data.AgentPinnedModels) == 0 {
		return statuses
	}
	out := append([]agent.Status(nil), statuses...)
	for i := range out {
		name := strings.TrimSpace(out[i].Name)
		out[i].PinnedModels = append([]string{}, s.data.AgentPinnedModels[name]...)
		out[i].LastUsed = s.data.LastAgent == name
		defaults := s.data.Agents[name]
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
