package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// GitInfo is best-effort git context for a working directory. Zero values
// mean "not a repo / git unavailable" — never an error.
type GitInfo struct {
	Branch string
	Commit string
	Remote string
}

// gitCmdTimeout bounds each git invocation so a hung git (e.g. fsmonitor,
// network-mounted repo) can never stall a hook.
const gitCmdTimeout = 1 * time.Second

// gitOutput runs one git command against dir and returns its trimmed stdout,
// or "" on any failure.
func gitOutput(ctx context.Context, dir string, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, gitCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ReadGitInfo collects branch, short commit, and origin URL for cwd.
// Strictly best-effort: partial results are fine, failures are silent.
func ReadGitInfo(ctx context.Context, cwd string) GitInfo {
	if cwd == "" {
		return GitInfo{}
	}
	return GitInfo{
		Branch: gitOutput(ctx, cwd, "rev-parse", "--abbrev-ref", "HEAD"),
		Commit: gitOutput(ctx, cwd, "rev-parse", "--short", "HEAD"),
		Remote: gitOutput(ctx, cwd, "remote", "get-url", "origin"),
	}
}
