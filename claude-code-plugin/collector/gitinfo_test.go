package main

import (
	"context"
	"testing"
)

// TestReadGitInfo runs against this repo itself (a real git checkout) and a
// non-repo temp dir.
func TestReadGitInfo(t *testing.T) {
	ctx := context.Background()

	gi := ReadGitInfo(ctx, ".")
	if gi.Branch == "" || gi.Commit == "" {
		t.Errorf("expected branch+commit in a real repo, got %+v", gi)
	}

	empty := ReadGitInfo(ctx, t.TempDir())
	if empty.Branch != "" || empty.Commit != "" {
		t.Errorf("non-repo dir should yield zero GitInfo, got %+v", empty)
	}

	if got := ReadGitInfo(ctx, ""); got != (GitInfo{}) {
		t.Errorf("empty cwd should yield zero GitInfo, got %+v", got)
	}
}
