package preferences

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mindfs/server/internal/agent"
)

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
