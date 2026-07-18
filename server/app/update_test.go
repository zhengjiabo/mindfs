package app

import (
	"context"
	"testing"
	"time"

	"mindfs/server/internal/update"
)

type fakeUpdateInstaller struct {
	status update.Status
}

func (f *fakeUpdateInstaller) AddListener(func(update.Status)) {}

func (f *fakeUpdateInstaller) InstallLatest(context.Context) (update.Status, error) {
	return f.status, nil
}

func TestUpdateNowUsesResolvedUpdateRepo(t *testing.T) {
	t.Setenv(updateRepoEnvKey, " zhengjiabo/mindfs ")
	original := newUpdateInstaller
	t.Cleanup(func() {
		newUpdateInstaller = original
	})

	gotRepo := ""
	newUpdateInstaller = func(repo, currentVersion, executable string, args []string, interval time.Duration) updateInstaller {
		gotRepo = repo
		return &fakeUpdateInstaller{status: update.Status{
			CurrentVersion: currentVersion,
			LatestVersion:  currentVersion,
			Status:         "up_to_date",
		}}
	}

	if _, err := UpdateNow(context.Background(), UpdateOptions{Version: "v0.4.2"}); err != nil {
		t.Fatalf("UpdateNow() error = %v", err)
	}
	if gotRepo != "zhengjiabo/mindfs" {
		t.Fatalf("update repo = %q, want %q", gotRepo, "zhengjiabo/mindfs")
	}
}
