package gitview

import (
	"context"
	"os/exec"
)

// Large repositories can make a single `git diff` process use well over 100 MB.
// Related-file views request diffs for many files at once, so an unbounded
// number of git children can exhaust a small host's memory. Keep a process-wide
// cap around commands that materialize repository data.
const maxConcurrentGitCommands = 4

var gitCommandSlots = make(chan struct{}, maxConcurrentGitCommands)

func acquireGitCommandSlot(ctx context.Context) (func(), error) {
	select {
	case gitCommandSlots <- struct{}{}:
		return func() { <-gitCommandSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func newGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	configureGitCommand(cmd)
	return cmd
}

const (
	windowsGitCreateNewProcessGroup  = uint32(0x00000200)
	windowsGitCreateDefaultErrorMode = uint32(0x04000000)
	windowsGitCreateNoWindow         = uint32(0x08000000)
)

func windowsGitCreationFlags() uint32 {
	return windowsGitCreateNewProcessGroup | windowsGitCreateDefaultErrorMode | windowsGitCreateNoWindow
}
