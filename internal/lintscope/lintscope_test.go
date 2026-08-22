package lintscope

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func initLintRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	gitLintCommand(t, root, "init")
	gitLintCommand(t, root, "config", "user.name", "Lint Test")
	gitLintCommand(t, root, "config", "user.email", "lint@example.com")
	return root
}

func gitLintCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func TestPrepareKeepsTrackedDeletionInNULManifest(t *testing.T) {
	root := initLintRepo(t)
	for _, name := range []string{"b.go", "deleted.go", "a.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitLintCommand(t, root, "add", "--", "a.go", "b.go", "deleted.go")
	gitLintCommand(t, root, "commit", "-m", "initial")
	if err := os.Remove(filepath.Join(root, "deleted.go")); err != nil {
		t.Fatal(err)
	}

	prepared, cleanup, err := Prepare(Options{
		Dir:     root,
		Command: "lint-command",
		Files:   []string{"b.go", "deleted.go", "a.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	payload, err := os.ReadFile(prepared.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), "a.go\x00b.go\x00deleted.go\x00"; got != want {
		t.Fatalf("manifest = %q, want %q", got, want)
	}
	if got, want := prepared.Files, []string{"a.go", "b.go", "deleted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestPrepareKeepsPreStagedDeletionInNULManifest(t *testing.T) {
	root := initLintRepo(t)
	if err := os.WriteFile(filepath.Join(root, "deleted.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitLintCommand(t, root, "add", "--", "deleted.go")
	gitLintCommand(t, root, "commit", "-m", "initial")
	gitLintCommand(t, root, "rm", "--", "deleted.go")

	prepared, cleanup, err := Prepare(Options{
		Dir:     root,
		Command: "lint-command",
		Files:   []string{"deleted.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	payload, err := os.ReadFile(prepared.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), "deleted.go\x00"; got != want {
		t.Fatalf("manifest = %q, want %q", got, want)
	}
}

func TestPrepareRejectsNonexistentUntrackedPath(t *testing.T) {
	root := initLintRepo(t)

	_, _, err := Prepare(Options{
		Dir:     root,
		Command: "lint-command",
		Files:   []string{"never-tracked.go"},
	})
	if err == nil || !strings.Contains(err.Error(), "not a tracked deletion") {
		t.Fatalf("Prepare error = %v, want nonexistent untracked path refusal", err)
	}
}

func TestPrepareWritesSortedNULManifestAndOverridesEnvironment(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b.go", "a.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("package p\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prepared, cleanup, err := Prepare(Options{
		Dir:     root,
		Command: "lint-command",
		Files:   []string{"b.go", "a.go"},
		BaseSHA: "base",
		HeadSHA: "head",
		Env:     []string{"KEEP=yes", ChangedFilesFileEnv + "=stale"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	payload, err := os.ReadFile(prepared.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), "a.go\x00b.go\x00"; got != want {
		t.Fatalf("manifest = %q, want %q", got, want)
	}
	if got, want := prepared.Files, []string{"a.go", "b.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	env := append([]string(nil), prepared.Env...)
	sort.Strings(env)
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"KEEP=yes", BaseSHAEnv + "=base", HeadSHAEnv + "=head",
		ChangedFilesFileEnv + "=" + prepared.ManifestPath, ScopeEnv + "=changed",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("environment missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, ChangedFilesFileEnv+"=stale") {
		t.Fatalf("stale environment survived:\n%s", joined)
	}
}

func TestPrepareRejectsPathsOutsideRepository(t *testing.T) {
	_, _, err := Prepare(Options{Dir: t.TempDir(), Command: "lint", Files: []string{"../outside"}})
	if err == nil || !strings.Contains(err.Error(), "inside the repository") {
		t.Fatalf("Prepare error = %v", err)
	}
}
