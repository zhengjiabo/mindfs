package usecase

import "testing"

func TestSuggestSessionExpectedNameDefaultsToFallback(t *testing.T) {
	in := SuggestSessionNameInput{}
	if got := suggestSessionExpectedName(in, "fallback title"); got != "fallback title" {
		t.Fatalf("suggestSessionExpectedName() = %q", got)
	}
}

func TestSuggestSessionExpectedNameUsesExplicitName(t *testing.T) {
	in := SuggestSessionNameInput{ExpectedName: "#14 · 安装 image-to-code 技能"}
	if got := suggestSessionExpectedName(in, "fallback title"); got != in.ExpectedName {
		t.Fatalf("suggestSessionExpectedName() = %q", got)
	}
}

func TestComposeSuggestedSessionNameKeepsPrefix(t *testing.T) {
	if got := composeSuggestedSessionName("#14 · ", "\"安装 image-to-code 技能。\""); got != "#14 · 安装 image-to-code 技能" {
		t.Fatalf("composeSuggestedSessionName() = %q", got)
	}
}

func TestComposeSuggestedSessionNameRejectsEmptyTitle(t *testing.T) {
	if got := composeSuggestedSessionName("#14 · ", "  "); got != "" {
		t.Fatalf("composeSuggestedSessionName() = %q", got)
	}
}
