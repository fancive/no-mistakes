//go:build !windows

package legacycleanup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanBlocksIntermediateWorktreeSymlinkEscape(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".no-mistakes")
	external := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(external, "run1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "worktrees", "repo1")); err != nil {
		t.Fatal(err)
	}
	plan, err := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: t.TempDir()}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "failed"}},
		}},
		ProcessAlive: func(int) bool { return false }, ServiceFiles: []string{},
	}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Blockers, "\n")
	if !strings.Contains(joined, "symlink") && !strings.Contains(joined, "physically escapes") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
}
