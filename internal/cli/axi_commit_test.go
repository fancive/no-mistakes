package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAxiCommitCreatesExactScopeCommitWithoutInitialization(t *testing.T) {
	dir := setupTestRepo(t)
	run(t, dir, "git", "remote", "set-url", "origin", "git@github.com:fancive/example.git")
	if err := os.WriteFile(filepath.Join(dir, "selected.txt"), []byte("selected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCmd("axi", "commit", "--file", "selected.txt", "--message", "feat(commit): expose exact staging")
	if err != nil {
		t.Fatalf("axi commit failed: %v\n%s", err, out)
	}
	for _, want := range []string{"committed: true", "provider: github", "branch:", "selected.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if got := gitOutput(t, dir, "show", "-s", "--format=%an <%ae>", "HEAD"); got != "fancivez <fancive@gmail.com>" {
		t.Fatalf("author = %q", got)
	}
	if got := gitOutput(t, dir, "status", "--short"); got != "?? unrelated.txt" {
		t.Fatalf("unrelated state = %q", got)
	}
}

func TestAxiCommitAmendsExistingICodeCommitWithoutMessage(t *testing.T) {
	dir := setupTestRepo(t)
	run(t, dir, "git", "remote", "set-url", "origin", "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	run(t, dir, "git", "config", "user.name", "Repository User")
	run(t, dir, "git", "config", "user.email", "repository@example.com")
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	script := "#!/bin/sh\nprintf '\\nChange-Id: I1111111111111111111111111111111111111111\\n' >> \"$1\"\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "selected.txt"), []byte("selected v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "selected.txt")
	run(t, dir, "git", "commit", "-m", "IMInput-10207 [Story] 初始提交")
	if err := os.WriteFile(filepath.Join(dir, "selected.txt"), []byte("selected v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCmd("axi", "commit", "--amend", "--file", "selected.txt")
	if err != nil {
		t.Fatalf("axi commit --amend failed: %v\n%s", err, out)
	}
	for _, want := range []string{"committed: true", "amended: true", "provider: icode", "selected.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	message := gitOutput(t, dir, "show", "-s", "--format=%B", "HEAD")
	if !strings.Contains(message, "IMInput-10207 [Story] 初始提交") || !strings.Contains(message, "Change-Id: I1111111111111111111111111111111111111111") {
		t.Fatalf("amended message lost subject or Change-Id:\n%s", message)
	}
}

func TestAxiCommitRequiresMessageAndExplicitFiles(t *testing.T) {
	setupTestRepo(t)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"axi", "commit", "--file", "a.txt"}, "--message is required"},
		{[]string{"axi", "commit", "--message", "chore: test"}, "at least one --file is required"},
	}
	for _, tc := range tests {
		out, err := executeCmd(tc.args...)
		if err == nil || !strings.Contains(out, tc.want) {
			t.Errorf("args %v: expected %q error, got err=%v output=%s", tc.args, tc.want, err, out)
		}
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
