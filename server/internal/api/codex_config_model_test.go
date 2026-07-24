package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchTopLevelTOMLStringKeyReplace(t *testing.T) {
	input := "model = \"old\"\nmodel_provider = \"my\"\n\n[projects.\"/tmp\"]\ntrust_level = \"trusted\"\n"
	next, prev, changed, err := patchTopLevelTOMLStringKey(input, "model", "new-model")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if prev != "old" {
		t.Fatalf("prev=%q", prev)
	}
	if !strings.Contains(next, `model = "new-model"`) {
		t.Fatalf("next=%q", next)
	}
	if !strings.Contains(next, `model_provider = "my"`) {
		t.Fatalf("lost provider: %q", next)
	}
	if !strings.Contains(next, `[projects."/tmp"]`) {
		t.Fatalf("lost section: %q", next)
	}
}

func TestPatchTopLevelTOMLStringKeyIgnoresSectionModel(t *testing.T) {
	input := "model_provider = \"my\"\n\n[tui.model_availability_nux]\nmodel = \"nux-model\"\n"
	next, prev, changed, err := patchTopLevelTOMLStringKey(input, "model", "top-model")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if prev != "" {
		t.Fatalf("prev=%q", prev)
	}
	if !strings.Contains(next, `model = "top-model"`) {
		t.Fatalf("missing top model: %q", next)
	}
	// section model preserved
	if !strings.Contains(next, `model = "nux-model"`) {
		t.Fatalf("section model lost: %q", next)
	}
	// top model must appear before section
	topIdx := strings.Index(next, `model = "top-model"`)
	secIdx := strings.Index(next, `[tui.model_availability_nux]`)
	if topIdx < 0 || secIdx < 0 || topIdx > secIdx {
		t.Fatalf("order wrong: %q", next)
	}
}

func TestPatchTopLevelTOMLStringKeyNoopSameValue(t *testing.T) {
	input := "model = \"same\"\n"
	next, prev, changed, err := patchTopLevelTOMLStringKey(input, "model", "same")
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change")
	}
	if prev != "same" {
		t.Fatalf("prev=%q", prev)
	}
	if next != input {
		t.Fatalf("content mutated: %q", next)
	}
}

func TestPatchTopLevelTOMLStringKeyCRLF(t *testing.T) {
	input := "model = \"old\"\r\nmodel_provider = \"my\"\r\n"
	next, _, changed, err := patchTopLevelTOMLStringKey(input, "model", "new")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if !strings.Contains(next, "\r\n") {
		t.Fatalf("expected CRLF preserved: %q", next)
	}
	if strings.Contains(next, "model = \"old\"") {
		t.Fatalf("old model remains: %q", next)
	}
}

func TestPatchTopLevelTOMLStringKeyPreservesComments(t *testing.T) {
	input := "# header\nmodel_provider = \"my\"\n# keep\n\n[section]\nx = 1\n"
	next, _, changed, err := patchTopLevelTOMLStringKey(input, "model", "x")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if !strings.Contains(next, "# header") || !strings.Contains(next, "# keep") {
		t.Fatalf("comments lost: %q", next)
	}
}

func TestNormalizeCodexConfigModel(t *testing.T) {
	if _, err := normalizeCodexConfigModel("  "); err == nil {
		t.Fatal("expected empty error")
	}
	if _, err := normalizeCodexConfigModel("a\nb"); err == nil {
		t.Fatal("expected newline error")
	}
	long := strings.Repeat("m", codexConfigModelMaxLen+1)
	if _, err := normalizeCodexConfigModel(long); err == nil {
		t.Fatal("expected length error")
	}
	got, err := normalizeCodexConfigModel("  gpt-5.4  ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt-5.4" {
		t.Fatalf("got=%q", got)
	}
}

func TestSetCodexConfigModelWritesCODEXHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("model = \"old\"\nmodel_provider = \"my\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev, model, changed, err := setCodexConfigModel("fresh-model")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || prev != "old" || model != "fresh-model" {
		t.Fatalf("prev=%q model=%q changed=%t", prev, model, changed)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `model = "fresh-model"`) {
		t.Fatalf("file=%q", text)
	}
	if !strings.Contains(text, `model_provider = "my"`) {
		t.Fatalf("provider lost: %q", text)
	}

	// no-op
	prev, model, changed, err = setCodexConfigModel("fresh-model")
	if err != nil {
		t.Fatal(err)
	}
	if changed || prev != "fresh-model" || model != "fresh-model" {
		t.Fatalf("noop prev=%q model=%q changed=%t", prev, model, changed)
	}
}

func TestSetCodexConfigModelCreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	prev, model, changed, err := setCodexConfigModel("brand-new")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || prev != "" || model != "brand-new" {
		t.Fatalf("prev=%q model=%q changed=%t", prev, model, changed)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `model = "brand-new"`) {
		t.Fatalf("file=%q", raw)
	}
}

func TestPatchTopLevelEscapesQuotes(t *testing.T) {
	next, _, changed, err := patchTopLevelTOMLStringKey("model = \"a\"\n", "model", "foo\"bar")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("want change")
	}
	if !strings.Contains(next, "model = \"foo\\\"bar\"") {
		t.Fatalf("next=%q", next)
	}
}

func TestPatchTopLevelSingleQuoted(t *testing.T) {
	next, prev, changed, err := patchTopLevelTOMLStringKey("model = 'old'\n", "model", "new")
	if err != nil {
		t.Fatal(err)
	}
	if !changed || prev != "old" {
		t.Fatalf("prev=%q changed=%v", prev, changed)
	}
	if !strings.Contains(next, "model = \"new\"") {
		t.Fatalf("%q", next)
	}
}
