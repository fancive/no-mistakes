package legacycleanup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeStateReader struct {
	state State
	err   error
}

func (f fakeStateReader) Read(context.Context, string) (State, error) { return f.state, f.err }

func cleanupGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupCleanupFixture(t *testing.T) (root, repo, gate, serviceFile string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, ".no-mistakes")
	repo = filepath.Join(base, "repo")
	gate = filepath.Join(root, "repos", "repo1.git")
	serviceFile = filepath.Join(base, "service.plist")
	for _, path := range []string{filepath.Join(root, "worktrees", "repo1", "run1"), gate, repo} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "worktrees", "repo1", "run1", "file"), []byte("managed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.sqlite"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("preserve: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.txt"), []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serviceFile, []byte("no-mistakes daemon run --root "+root), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupGit(t, repo, "init")
	cleanupGit(t, repo, "remote", "add", "origin", "https://example.com/repo.git")
	cleanupGit(t, repo, "remote", "add", "custom", "https://example.com/custom.git")
	cleanupGit(t, repo, "remote", "add", "no-mistakes", gate)
	return root, repo, gate, serviceFile
}

func TestPlanIsReadOnlyAndHashesExactOwnedTargets(t *testing.T) {
	root, repo, gate, serviceFile := setupCleanupFixture(t)
	service := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "failed"}},
		}},
		ProcessAlive: func(int) bool { return false },
		ServiceFiles: []string{serviceFile},
	})

	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Hash == "" || len(plan.Blockers) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	repoCanonical, err := canonicalPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{
		"worktree:" + filepath.Join(plan.Root, "worktrees", "repo1", "run1"),
		"gates:" + filepath.Join(plan.Root, "repos", "repo1.git"),
		"database:" + filepath.Join(plan.Root, "state.sqlite"),
		"service:" + serviceFile,
		"remote:" + repoCanonical + "#no-mistakes",
	}
	for _, want := range wants {
		if !containsTarget(plan.Targets, want) {
			t.Errorf("plan missing %q: %+v", want, plan.Targets)
		}
	}
	for _, path := range []string{filepath.Join(root, "worktrees"), gate, serviceFile, filepath.Join(root, "state.sqlite")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("plan mutated %s: %v", path, err)
		}
	}
}

func TestConfirmRevalidatesHashAndPreservesUnrelatedState(t *testing.T) {
	root, repo, gate, serviceFile := setupCleanupFixture(t)
	service := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "failed"}},
		}},
		ProcessAlive: func(int) bool { return false },
		ServiceFiles: []string{serviceFile},
	})
	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Confirm(context.Background(), plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Removed) != len(plan.Targets) {
		t.Fatalf("receipt = %+v, plan targets = %+v", receipt, plan.Targets)
	}
	for _, path := range []string{filepath.Join(root, "worktrees", "repo1", "run1"), gate, filepath.Join(root, "state.sqlite"), serviceFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("owned path not removed %s: %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(root, "config.yaml"), filepath.Join(root, "unrelated.txt")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated path removed %s: %v", path, err)
		}
	}
	if got := cleanupGit(t, repo, "remote"); strings.Contains(got, "no-mistakes") || !strings.Contains(got, "origin") || !strings.Contains(got, "custom") {
		t.Fatalf("remotes after cleanup:\n%s", got)
	}
}

func TestConfirmRefusesInventoryDrift(t *testing.T) {
	root, repo, _, serviceFile := setupCleanupFixture(t)
	service := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "failed"}},
		}},
		ProcessAlive: func(int) bool { return false },
		ServiceFiles: []string{serviceFile},
	})
	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worktrees", "repo1", "run1", "late"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Confirm(context.Background(), plan.Hash); err == nil || !strings.Contains(err.Error(), "plan hash changed") {
		t.Fatalf("Confirm error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "worktrees")); err != nil {
		t.Fatalf("drift refusal mutated worktrees: %v", err)
	}
}

func TestConfirmRefusesSameSizeContentDriftWithRestoredMtime(t *testing.T) {
	root, repo, _, serviceFile := setupCleanupFixture(t)
	service := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "failed"}},
		}},
		ProcessAlive: func(int) bool { return false },
		ServiceFiles: []string{serviceFile},
	})
	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	managedFile := filepath.Join(root, "worktrees", "repo1", "run1", "file")
	before, err := os.Stat(managedFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedFile, []byte("changed"), before.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(managedFile, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Confirm(context.Background(), plan.Hash); err == nil || !strings.Contains(err.Error(), "plan hash changed") {
		t.Fatalf("Confirm error = %v", err)
	}
	if got, err := os.ReadFile(managedFile); err != nil || string(got) != "changed" {
		t.Fatalf("drift refusal mutated managed file: content=%q err=%v", got, err)
	}
}

func TestServiceFingerprintIsCheckedBeforeUnregister(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".no-mistakes")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	serviceFile := filepath.Join(t.TempDir(), "service.plist")
	if err := os.WriteFile(serviceFile, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := fingerprintPath(serviceFile)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	service := New(Options{
		Root: root, Reader: fakeStateReader{}, ProcessAlive: func(int) bool { return false },
		ServiceFiles: []string{}, ServiceRemover: func(context.Context, string) error { called++; return nil },
	})
	if err := os.WriteFile(serviceFile, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = service.applyTarget(context.Background(), root, Target{Kind: "service", Path: serviceFile, Display: serviceFile, Fingerprint: fingerprint})
	if err == nil || called != 0 {
		t.Fatalf("applyTarget error = %v, remover calls = %d", err, called)
	}
}

func TestPlanRefusesBroadHomeRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(Options{Root: home, Reader: fakeStateReader{}, ProcessAlive: func(int) bool { return false }}).Plan(context.Background())
	if err == nil || !strings.Contains(err.Error(), "broad legacy root") {
		t.Fatalf("Plan error = %v", err)
	}
}

func TestPlanBlocksUnownedWorktreeEntries(t *testing.T) {
	root, repo, _, serviceFile := setupCleanupFixture(t)
	rogue := filepath.Join(root, "worktrees", "repo1", "user-worktree")
	if err := os.MkdirAll(rogue, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "failed"}},
		}},
		ProcessAlive: func(int) bool { return false }, ServiceFiles: []string{serviceFile},
	}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), "user-worktree") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
}

func TestPlanBlocksUnknownRunStatus(t *testing.T) {
	root, repo, _, serviceFile := setupCleanupFixture(t)
	plan, err := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			Runs:         []Run{{ID: "run1", RepoID: "repo1", Status: "mystery"}},
		}},
		ProcessAlive: func(int) bool { return false }, ServiceFiles: []string{serviceFile},
	}).Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Blockers, "\n"), "not proven terminal") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
}

func TestPlanBlocksActiveOrUncertainLegacyState(t *testing.T) {
	root, repo, _, serviceFile := setupCleanupFixture(t)
	if err := os.WriteFile(filepath.Join(root, "daemon.pid"), []byte(`{"pid":4242}`), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(Options{
		Root: root,
		Reader: fakeStateReader{state: State{
			Repositories: []Repository{{ID: "repo1", WorkingPath: repo}},
			ActiveRuns:   []string{"run-active"},
		}},
		ProcessAlive: func(pid int) bool { return pid == 4242 },
		ServiceFiles: []string{serviceFile},
	})
	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Blockers, "\n")
	for _, want := range []string{"daemon process 4242", "run-active"} {
		if !strings.Contains(joined, want) {
			t.Errorf("blockers missing %q: %v", want, plan.Blockers)
		}
	}
	if _, err := service.Confirm(context.Background(), plan.Hash); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("Confirm error = %v", err)
	}
}

func TestPlanRefusesSymlinkedManagedRoot(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "worktrees")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	service := New(Options{Root: root, Reader: fakeStateReader{}, ProcessAlive: func(int) bool { return false }})
	plan, err := service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(plan.Blockers, "\n"); !strings.Contains(joined, "symlink") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
	if _, err := service.Confirm(context.Background(), plan.Hash); err == nil {
		t.Fatal("Confirm accepted symlinked managed root")
	}
}

func containsTarget(targets []Target, want string) bool {
	for _, target := range targets {
		if target.Kind+":"+target.Display == want {
			return true
		}
	}
	return false
}
