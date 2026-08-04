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
	if got.RecommendedBaseDelaySeconds != 30 || got.LastRecoverySeconds != 27 || got.LastRetryCount != 2 || got.RecoverySampleCount != 1 {
		t.Fatalf("recovery stats were not preserved: %#v", got)
	}
}

func TestRecordRateLimitRecoveryRoundsRecommendationToNearestTenSeconds(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), preferencesFileName)}
	if err := store.RecordRateLimitRecovery(26*time.Second, 2); err != nil {
		t.Fatalf("RecordRateLimitRecovery: %v", err)
	}
	got := store.GetRateLimitRetry()
	if got.RecommendedBaseDelaySeconds != 30 || got.LastRecoverySeconds != 26 || got.LastRetryCount != 2 || got.RecoverySampleCount != 1 {
		t.Fatalf("recorded recovery = %#v", got)
	}
}

func TestRecordRateLimitRecoveryRoundsDownBelowMidpoint(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), preferencesFileName)}
	if err := store.RecordRateLimitRecovery(24*time.Second+100*time.Millisecond, 1); err != nil {
		t.Fatalf("RecordRateLimitRecovery: %v", err)
	}
	got := store.GetRateLimitRetry()
	if got.RecommendedBaseDelaySeconds != 20 || got.LastRecoverySeconds != 25 {
		t.Fatalf("recorded recovery = %#v", got)
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
