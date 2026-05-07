package update

import "testing"

func TestParseReleaseNotesVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "tag heading", text: "# MindFS v0.2.3\n\n## Fixes\n", want: "v0.2.3"},
		{name: "version without prefix", text: "# MindFS 0.2.3\n", want: "0.2.3"},
		{name: "invalid heading", text: "# Latest v0.2.3\n", want: ""},
		{name: "empty", text: "", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseReleaseNotesVersion(tt.text)
			if got != tt.want {
				t.Fatalf("parseReleaseNotesVersion(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{name: "higher patch", latest: "0.1.1", current: "0.1.0", want: true},
		{name: "lower patch", latest: "0.1.0", current: "0.1.1", want: false},
		{name: "same version", latest: "0.1.0", current: "0.1.0", want: false},
		{name: "prefixed tag", latest: "v0.2.0", current: "0.1.9", want: true},
		{name: "git describe current", latest: "0.1.0", current: "v0.1.0-2-gabc123", want: false},
		{name: "invalid current treated as older", latest: "0.1.0", current: "dev", want: true},
		{name: "invalid latest ignored", latest: "dev", current: "0.1.0", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isNewerVersion(tt.latest, tt.current)
			if got != tt.want {
				t.Fatalf("isNewerVersion(%q, %q) = %t, want %t", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}
