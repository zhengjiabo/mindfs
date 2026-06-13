package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUpdateRepoUsesDefaultWhenEnvUnset(t *testing.T) {
	t.Setenv(updateRepoEnvKey, "")
	old := defaultUpdateRepo
	defaultUpdateRepo = " a9gent/mindfs "
	t.Cleanup(func() {
		defaultUpdateRepo = old
	})
	if got := resolveUpdateRepo(); got != "a9gent/mindfs" {
		t.Fatalf("resolveUpdateRepo() = %q, want %q", got, "a9gent/mindfs")
	}
}

func TestResolveUpdateRepoPrefersEnv(t *testing.T) {
	t.Setenv(updateRepoEnvKey, " zhengjiabo/mindfs ")
	if got := resolveUpdateRepo(); got != "zhengjiabo/mindfs" {
		t.Fatalf("resolveUpdateRepo() = %q, want %q", got, "zhengjiabo/mindfs")
	}
}

func TestResolveStaticDirFromExecutablePrefersBuiltWebDist(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	builtWeb := filepath.Join(exeDir, "web", "dist")
	releaseWeb := filepath.Join(exeDir, "web")
	installedWeb := filepath.Join(root, "share", "mindfs", "web")
	writeFrontendAssets(t, builtWeb)
	writeFrontendAssets(t, releaseWeb)
	writeFrontendAssets(t, installedWeb)

	got := resolveStaticDirFromExecutable(filepath.Join(exeDir, "mindfs.exe"))
	if got != builtWeb {
		t.Fatalf("resolveStaticDirFromExecutable() = %q, want %q", got, builtWeb)
	}
}

func TestResolveStaticDirFromExecutableFallsBackToReleaseArchiveLayout(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	releaseWeb := filepath.Join(exeDir, "web")
	installedWeb := filepath.Join(root, "share", "mindfs", "web")
	writeFrontendAssets(t, releaseWeb)
	writeFrontendAssets(t, installedWeb)

	got := resolveStaticDirFromExecutable(filepath.Join(exeDir, "mindfs.exe"))
	if got != releaseWeb {
		t.Fatalf("resolveStaticDirFromExecutable() = %q, want %q", got, releaseWeb)
	}
}

func TestResolveStaticDirFromExecutableFallsBackToInstalledLayout(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	installedWeb := filepath.Join(root, "share", "mindfs", "web")
	writeFrontendAssets(t, installedWeb)

	got := resolveStaticDirFromExecutable(filepath.Join(exeDir, "mindfs.exe"))
	if got != installedWeb {
		t.Fatalf("resolveStaticDirFromExecutable() = %q, want %q", got, installedWeb)
	}
}

func TestResolveStaticDirFromExecutableUsesBuiltWebDistWhenSourceWebIsPresent(t *testing.T) {
	root := t.TempDir()
	sourceWeb := filepath.Join(root, "web")
	builtWeb := filepath.Join(sourceWeb, "dist")
	mkdirAll(t, sourceWeb)
	if err := os.WriteFile(filepath.Join(sourceWeb, "index.html"), []byte("source"), 0o644); err != nil {
		t.Fatalf("write source index: %v", err)
	}
	writeFrontendAssets(t, builtWeb)

	got := resolveStaticDirFromExecutable(filepath.Join(root, "mindfs"))
	if got != builtWeb {
		t.Fatalf("resolveStaticDirFromExecutable() = %q, want %q", got, builtWeb)
	}
}

func writeFrontendAssets(t *testing.T, path string) {
	t.Helper()
	mkdirAll(t, path)
	for _, name := range []string{"index.html", "favicon.svg"} {
		if err := os.WriteFile(filepath.Join(path, name), []byte(name), 0o644); err != nil {
			t.Fatalf("write %s: %v", filepath.Join(path, name), err)
		}
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
