package preferences

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mindfs/server/internal/agent"
)

func TestRateLimitRetryDefaults(t *testing.T) {
	store := &Store{}
	got := store.GetRateLimitRetry()
	if !got.Enabled || got.BaseDelaySeconds != 10 || got.IncrementSeconds != 5 {
		t.Fatalf("defaults = %#v, want enabled with 10s base and 5s increment", got)
	}
}

func TestUpdateRateLimitRetryPreservesRecoveryStats(t *testing.T) {
	store := &Store{
		path: filepath.Join(t.TempDir(), preferencesFileName),
		data: UserPreferences{RateLimitRetry: &RateLimitRetryPreferences{
			Enabled:                     true,
			BaseDelaySeconds:            10,
			IncrementSeconds:            5,
			RecommendedBaseDelaySeconds: 30,
			LastRecoverySeconds:         27,
			LastRetryCount:              2,
			LastProbeSeconds:            3,
			RecoverySampleCount:         1,
		}},
	}
	if err := store.UpdateRateLimitRetry(RateLimitRetryPatch{
		Enabled:          false,
		BaseDelaySeconds: 20,
		IncrementSeconds: 0,
	}); err != nil {
		t.Fatalf("UpdateRateLimitRetry: %v", err)
	}
	got := store.GetRateLimitRetry()
	if got.Enabled || got.BaseDelaySeconds != 20 || got.IncrementSeconds != 0 {
		t.Fatalf("updated settings = %#v", got)
	}
	if got.RecommendedBaseDelaySeconds != 10 || got.LastRecoverySeconds != 27 || got.LastRetryCount != 2 || got.LastProbeSeconds != 3 || got.RecoverySampleCount != 1 {
		t.Fatalf("recovery stats were not preserved: %#v", got)
	}
}

func TestRecordRateLimitRecoveryTargetsThirdAttemptAtRecoveryWindow(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), preferencesFileName)}
	if err := store.RecordRateLimitRecovery(62*time.Second, 2, 5*time.Second); err != nil {
		t.Fatalf("RecordRateLimitRecovery: %v", err)
	}
	got := store.GetRateLimitRetry()
	if got.RecommendedBaseDelaySeconds != 12 || got.LastRecoverySeconds != 62 || got.LastRetryCount != 2 || got.LastProbeSeconds != 5 || got.RecoverySampleCount != 1 {
		t.Fatalf("recorded recovery = %#v", got)
	}
}

func TestRateLimitRetryRecommendationMigratesLegacySample(t *testing.T) {
	store := &Store{data: UserPreferences{RateLimitRetry: &RateLimitRetryPreferences{
		Enabled:                     true,
		BaseDelaySeconds:            10,
		IncrementSeconds:            5,
		RecommendedBaseDelaySeconds: 60,
		LastRecoverySeconds:         62,
		LastRetryCount:              2,
	}}}
	got := store.GetRateLimitRetry()
	if got.RecommendedBaseDelaySeconds != 15 {
		t.Fatalf("legacy recommendation = %d, want 15", got.RecommendedBaseDelaySeconds)
	}
}

func TestUpdateRateLimitRetryRecomputesRecommendationForIncrement(t *testing.T) {
	store := &Store{
		path: filepath.Join(t.TempDir(), preferencesFileName),
		data: UserPreferences{RateLimitRetry: &RateLimitRetryPreferences{
			Enabled: true, BaseDelaySeconds: 10, IncrementSeconds: 5,
			LastRecoverySeconds: 62, LastRetryCount: 2, LastProbeSeconds: 5,
		}},
	}
	if err := store.UpdateRateLimitRetry(RateLimitRetryPatch{Enabled: true, BaseDelaySeconds: 10, IncrementSeconds: 0}); err != nil {
		t.Fatalf("UpdateRateLimitRetry: %v", err)
	}
	if got := store.GetRateLimitRetry().RecommendedBaseDelaySeconds; got != 17 {
		t.Fatalf("recommendation = %d, want 17", got)
	}
}

func TestRateLimitRetryRecommendationHasSafeBounds(t *testing.T) {
	if got := recommendRateLimitRetryBaseDelay(20, 20, 20); got != 10 {
		t.Fatalf("minimum recommendation = %d, want 10", got)
	}
	if got := recommendRateLimitRetryBaseDelay(100000, 0, 0); got != 3600 {
		t.Fatalf("maximum recommendation = %d, want 3600", got)
	}
}

func TestUpdateRateLimitRetryValidatesRange(t *testing.T) {
	store := &Store{}
	if err := store.UpdateRateLimitRetry(RateLimitRetryPatch{Enabled: true, BaseDelaySeconds: 0, IncrementSeconds: 5}); err == nil {
		t.Fatal("expected zero base delay to fail validation")
	}
	if err := store.UpdateRateLimitRetry(RateLimitRetryPatch{Enabled: true, BaseDelaySeconds: 10, IncrementSeconds: -1}); err == nil {
		t.Fatal("expected negative increment to fail validation")
	}
}

func TestUpdateAgentDefaultsClearsEmptyModel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, preferencesFileName)
	store := &Store{
		path: path,
		data: UserPreferences{Agents: map[string]AgentDefaults{
			"codex": {Model: "gpt-5.6-sol", Effort: "high"},
		}},
	}

	model := ""
	changed, err := store.UpdateAgentDefaultsIfChanged("codex", AgentDefaultsPatch{
		Model:  &model,
		Effort: "high",
	})
	if err != nil {
		t.Fatalf("UpdateAgentDefaultsIfChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected model clear to change preferences")
	}
	if got := store.data.Agents["codex"].Model; got != "" {
		t.Fatalf("model = %q, want empty", got)
	}
	if got := store.data.Agents["codex"].Effort; got != "high" {
		t.Fatalf("effort = %q, want high", got)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preferences: %v", err)
	}
	var data UserPreferences
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal preferences: %v", err)
	}
	if got := data.Agents["codex"].Model; got != "" {
		t.Fatalf("persisted model = %q, want empty", got)
	}
}

func TestUpdateAgentDefaultsLeavesModelWhenPatchOmitsIt(t *testing.T) {
	store := &Store{
		path: filepath.Join(t.TempDir(), preferencesFileName),
		data: UserPreferences{Agents: map[string]AgentDefaults{
			"codex": {Model: "gpt-5.6-sol", Effort: "medium"},
		}},
	}

	changed, err := store.UpdateAgentDefaultsIfChanged("codex", AgentDefaultsPatch{Effort: "high"})
	if err != nil {
		t.Fatalf("UpdateAgentDefaultsIfChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected effort update to change preferences")
	}
	if got := store.data.Agents["codex"].Model; got != "gpt-5.6-sol" {
		t.Fatalf("model = %q, want gpt-5.6-sol", got)
	}
}

func TestApplyAgentDefaultsCodexEmptyModelFollowsConfig(t *testing.T) {
	store := &Store{
		data: UserPreferences{Agents: map[string]AgentDefaults{
			"codex":  {Effort: "high"},
			"claude": {Model: "sonnet[1m]"},
		}},
	}
	statuses := []agent.Status{
		{Name: "codex", DefaultModelID: "grok-4.5", CurrentModelID: "grok-4.5"},
		{Name: "claude", DefaultModelID: "opus", CurrentModelID: "opus"},
	}
	out := store.ApplyAgentDefaults(statuses)
	if out[0].DefaultModelID != "" {
		t.Fatalf("codex DefaultModelID = %q, want empty follow-config", out[0].DefaultModelID)
	}
	if out[1].DefaultModelID != "sonnet[1m]" {
		t.Fatalf("claude DefaultModelID = %q, want sonnet[1m]", out[1].DefaultModelID)
	}
}

func TestApplyAgentDefaultsCodexExplicitModelOverride(t *testing.T) {
	store := &Store{
		data: UserPreferences{Agents: map[string]AgentDefaults{
			"codex": {Model: "gpt-5.6-sol"},
		}},
	}
	statuses := []agent.Status{
		{Name: "codex", DefaultModelID: "grok-4.5", CurrentModelID: "grok-4.5"},
	}
	out := store.ApplyAgentDefaults(statuses)
	if out[0].DefaultModelID != "gpt-5.6-sol" {
		t.Fatalf("codex DefaultModelID = %q, want gpt-5.6-sol", out[0].DefaultModelID)
	}
}

func TestSessionNamingDefaultsPersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), preferencesFileName)
	store := &Store{path: path, data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if err := store.UpdateSessionNamingDefaults(" codex ", " gpt-5.4 "); err != nil {
		t.Fatalf("UpdateSessionNamingDefaults: %v", err)
	}
	if got := store.SessionNamingDefaults(); got != (SessionNamingDefaults{Agent: "codex", Model: "gpt-5.4"}) {
		t.Fatalf("SessionNamingDefaults = %#v", got)
	}
	reloaded := &Store{path: path, data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if err := reloaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := reloaded.SessionNamingDefaults(); got != (SessionNamingDefaults{Agent: "codex", Model: "gpt-5.4"}) {
		t.Fatalf("reloaded SessionNamingDefaults = %#v", got)
	}
}

func TestSessionNamingDefaultsAllowDefaultModel(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), preferencesFileName), data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if err := store.UpdateSessionNamingDefaults("codex", ""); err != nil {
		t.Fatalf("UpdateSessionNamingDefaults with default model: %v", err)
	}
	if got := store.SessionNamingDefaults(); got != (SessionNamingDefaults{Agent: "codex"}) {
		t.Fatalf("SessionNamingDefaults = %#v", got)
	}
	if err := store.UpdateSessionNamingDefaults("", "gpt-5.4"); err == nil {
		t.Fatal("UpdateSessionNamingDefaults without agent succeeded")
	}
}

func TestIdleSessionResourceReleaseHoursDefaultAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), preferencesFileName)
	store := &Store{path: path, data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if got := store.IdleSessionResourceReleaseHours(); got != 72 {
		t.Fatalf("default hours = %d, want 72", got)
	}
	if err := store.UpdateIdleSessionResourceReleaseHours(24); err != nil {
		t.Fatalf("UpdateIdleSessionResourceReleaseHours: %v", err)
	}
	reloaded := &Store{path: path, data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if err := reloaded.load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := reloaded.IdleSessionResourceReleaseHours(); got != 24 {
		t.Fatalf("reloaded hours = %d, want 24", got)
	}
	if err := store.UpdateIdleSessionResourceReleaseHours(0); err == nil {
		t.Fatal("zero hours unexpectedly accepted")
	}
}

func TestNewProjectMetaLocationDefaultAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), preferencesFileName)
	store := &Store{path: path, data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if got := store.NewProjectMetaLocation(); got != "project" {
		t.Fatalf("default location = %q", got)
	}
	if err := store.UpdateNewProjectMetaLocation("home"); err != nil {
		t.Fatal(err)
	}
	reloaded := &Store{path: path, data: UserPreferences{Agents: map[string]AgentDefaults{}}}
	if err := reloaded.load(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.NewProjectMetaLocation(); got != "home" {
		t.Fatalf("reloaded location = %q", got)
	}
	if err := store.UpdateNewProjectMetaLocation("other"); err == nil {
		t.Fatal("invalid location unexpectedly accepted")
	}
}
