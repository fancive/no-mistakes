package commitprep

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/scm"
)

func initRepo(t *testing.T, remote string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "Repository User")
	gitCmd(t, dir, "config", "user.email", "repository@example.com")
	write(t, dir, "README.md", "initial\n")
	gitCmd(t, dir, "add", "README.md")
	gitCmd(t, dir, "commit", "-m", "initial")
	gitCmd(t, dir, "remote", "add", "origin", remote)
	return dir
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, path, contents string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installChangeIDHook(t *testing.T, dir string) {
	t.Helper()
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	script := "#!/bin/sh\nprintf '\\nChange-Id: I1111111111111111111111111111111111111111\\n' >> \"$1\"\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCommitGitHubStagesOnlyExplicitFilesAndSetsAuthor(t *testing.T) {
	dir := initRepo(t, "git@github.com:fancive/example.git")
	write(t, dir, "selected.txt", "selected\n")
	write(t, dir, "unrelated.txt", "unrelated\n")

	result, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"selected.txt"},
		Message: "feat(commit): add exact staging",
	})
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if result.Provider != scm.ProviderGitHub || len(result.Files) != 1 || result.Files[0] != "selected.txt" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := gitCmd(t, dir, "show", "-s", "--format=%an <%ae>", "HEAD"); got != "fancivez <fancive@gmail.com>" {
		t.Fatalf("author = %q", got)
	}
	if got := gitCmd(t, dir, "show", "--format=", "--name-only", "HEAD"); got != "selected.txt" {
		t.Fatalf("committed files = %q", got)
	}
	if got := gitCmd(t, dir, "status", "--short"); got != "?? unrelated.txt" {
		t.Fatalf("unrelated worktree state = %q", got)
	}
}

func TestCommitRefusesPreStagedFileOutsideExplicitListWithoutMutatingIndex(t *testing.T) {
	dir := initRepo(t, "git@github.com:fancive/example.git")
	write(t, dir, "selected.txt", "selected\n")
	write(t, dir, "already-staged.txt", "staged\n")
	gitCmd(t, dir, "add", "already-staged.txt")
	before := gitCmd(t, dir, "rev-parse", "HEAD")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"selected.txt"},
		Message: "feat(commit): reject extra staging",
	})
	if err == nil || !strings.Contains(err.Error(), "already-staged.txt") {
		t.Fatalf("expected precise staging error, got %v", err)
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "already-staged.txt" {
		t.Fatalf("index changed: %q", got)
	}
}

func TestCommitRefusesDetachedHEADBeforeStaging(t *testing.T) {
	dir := initRepo(t, "git@github.com:fancive/example.git")
	gitCmd(t, dir, "checkout", "--detach")
	write(t, dir, "selected.txt", "selected\n")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"selected.txt"},
		Message: "feat(commit): reject detached head",
	})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("expected detached HEAD error, got %v", err)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("detached refusal mutated index: %q", got)
	}
}

func TestCommitICodeValidatesMessageAndRequiresChangeIDHookBeforeStaging(t *testing.T) {
	dir := initRepo(t, "ssh://icode.baidu.com/baidu/inputmethod/v5api")
	write(t, dir, "switch.php", "<?php return 1;\n")
	before := gitCmd(t, dir, "rev-parse", "HEAD")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Message: "feat(v5api): add resource loop switch",
	})
	if err == nil || !strings.Contains(err.Error(), "iCafe") {
		t.Fatalf("expected iCafe message error, got %v", err)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("invalid message mutated index: %q", got)
	}

	_, err = Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Message: "IMInput-10207 [Story] 增加资源闭环开关",
	})
	if err == nil || !strings.Contains(err.Error(), "commit-msg hook") {
		t.Fatalf("expected commit-msg hook error, got %v", err)
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("missing hook mutated index: %q", got)
	}
}

func TestCommitICodePreservesLocalAuthorAndVerifiesChangeID(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	installChangeIDHook(t, dir)
	write(t, dir, "switch.php", "<?php return 1;\n")

	result, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Message: "IMInput-10207 [Story] 增加资源闭环开关",
	})
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if result.Provider != scm.ProviderICode {
		t.Fatalf("provider = %s", result.Provider)
	}
	if got := gitCmd(t, dir, "show", "-s", "--format=%an <%ae>", "HEAD"); got != "Repository User <repository@example.com>" {
		t.Fatalf("author = %q", got)
	}
	message := gitCmd(t, dir, "show", "-s", "--format=%B", "HEAD")
	if !strings.Contains(message, "Change-Id: I1111111111111111111111111111111111111111") {
		t.Fatalf("commit message missing Change-Id:\n%s", message)
	}
}

func TestCommitAmendsICodeCommitAndPreservesChangeID(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	installChangeIDHook(t, dir)
	gitCmd(t, dir, "commit", "--amend", "-m", "IMInput-10207 [Story] 初始提交\n\nChange-Id: I1111111111111111111111111111111111111111")
	write(t, dir, "switch.php", "<?php return 2;\n")

	beforeCount := gitCmd(t, dir, "rev-list", "--count", "HEAD")
	beforeHead := gitCmd(t, dir, "rev-parse", "HEAD")
	result, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Amend:   true,
		Message: "",
	})
	if err != nil {
		t.Fatalf("Commit amend failed: %v", err)
	}
	if !result.Amended {
		t.Fatal("result did not report amend mode")
	}
	afterCount := gitCmd(t, dir, "rev-list", "--count", "HEAD")
	afterHead := gitCmd(t, dir, "rev-parse", "HEAD")
	if afterCount != beforeCount {
		t.Fatalf("commit count = %s, want unchanged %s", afterCount, beforeCount)
	}
	if afterHead == beforeHead {
		t.Fatal("amend did not advance HEAD")
	}
	message := gitCmd(t, dir, "show", "-s", "--format=%B", "HEAD")
	if !strings.Contains(message, "IMInput-10207 [Story] 初始提交") || !strings.Contains(message, "Change-Id: I1111111111111111111111111111111111111111") {
		t.Fatalf("amended message lost subject or Change-Id: %q", message)
	}
}

func TestCommitAmendICodeWithNewMessagePreservesCurrentChangeID(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	installChangeIDHook(t, dir)
	gitCmd(t, dir, "commit", "--amend", "-m", "IMInput-10207 [Story] 初始提交\n\nChange-Id: I1111111111111111111111111111111111111111")
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nif ! grep -qE '^Change-Id: I[0-9a-f]{40}$' \"$1\"; then printf '\\nChange-Id: I2222222222222222222222222222222222222222\\n' >> \"$1\"; fi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "switch.php", "<?php return 2;\n")

	beforeCount := gitCmd(t, dir, "rev-list", "--count", "HEAD")
	result, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Amend:   true,
		Message: "IMInput-10207 [Story] 更新实现",
	})
	if err != nil {
		t.Fatalf("Commit amend failed: %v", err)
	}
	if !result.Amended {
		t.Fatal("result did not report amend mode")
	}
	if got := gitCmd(t, dir, "rev-list", "--count", "HEAD"); got != beforeCount {
		t.Fatalf("commit count = %s, want unchanged %s", got, beforeCount)
	}
	message := gitCmd(t, dir, "show", "-s", "--format=%B", "HEAD")
	if !strings.Contains(message, "IMInput-10207 [Story] 更新实现") {
		t.Fatalf("amended message lost the new subject: %q", message)
	}
	if !strings.Contains(message, "Change-Id: I1111111111111111111111111111111111111111") {
		t.Fatalf("amended message lost the current Change-Id: %q", message)
	}
	if strings.Contains(message, "2222222222222222222222222222222222222222") {
		t.Fatalf("amended message minted a fresh Change-Id: %q", message)
	}
}

func TestCommitAmendICodeWithNewMessageRejectsDifferentChangeID(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	installChangeIDHook(t, dir)
	gitCmd(t, dir, "commit", "--amend", "-m", "IMInput-10207 [Story] 初始提交\n\nChange-Id: I1111111111111111111111111111111111111111")
	write(t, dir, "switch.php", "<?php return 2;\n")
	before := gitCmd(t, dir, "rev-parse", "HEAD")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Amend:   true,
		Message: "IMInput-10207 [Story] 更新实现\n\nChange-Id: I3333333333333333333333333333333333333333",
	})
	if err == nil || !strings.Contains(err.Error(), "changes the current Change-Id") {
		t.Fatalf("expected Change-Id preservation error, got %v", err)
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("rejected message mutated index: %q", got)
	}
}

func TestCommitAmendICodeRollsBackWhenHookReplacesChangeID(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	installChangeIDHook(t, dir)
	gitCmd(t, dir, "commit", "--amend", "-m", "IMInput-10207 [Story] 初始提交\n\nChange-Id: I1111111111111111111111111111111111111111")
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	script := "#!/bin/sh\ngrep -v '^Change-Id:' \"$1\" > \"$1.tmp\" && mv \"$1.tmp\" \"$1\"\nprintf '\\nChange-Id: I4444444444444444444444444444444444444444\\n' >> \"$1\"\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "switch.php", "<?php return 2;\n")
	before := gitCmd(t, dir, "rev-parse", "HEAD")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Amend:   true,
		Message: "IMInput-10207 [Story] 更新实现",
	})
	if err == nil || !strings.Contains(err.Error(), "changed the Change-Id footer") {
		t.Fatalf("expected Change-Id preservation verification failure, got %v", err)
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("rollback did not restore original index: %q", got)
	}
	if got := gitCmd(t, dir, "status", "--short"); got != "?? switch.php" {
		t.Fatalf("worktree change was not preserved: %q", got)
	}
}

func TestCommitRestoresOriginalIndexWhenCommitHookRejects(t *testing.T) {
	dir := initRepo(t, "git@github.com:fancive/example.git")
	write(t, dir, "selected.txt", "selected\n")
	gitCmd(t, dir, "add", "selected.txt")
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := gitCmd(t, dir, "rev-parse", "HEAD")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"selected.txt"},
		Message: "feat(commit): preserve the index on failure",
	})
	if err == nil || !strings.Contains(err.Error(), "create commit") {
		t.Fatalf("expected commit hook failure, got %v", err)
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "selected.txt" {
		t.Fatalf("original staged state was not restored: %q", got)
	}
}

func TestCommitRollsBackICodeCommitWhenHookAddsNoChangeID(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	hook := filepath.Join(dir, ".git", "hooks", "commit-msg")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "switch.php", "<?php return 1;\n")
	before := gitCmd(t, dir, "rev-parse", "HEAD")

	_, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"switch.php"},
		Message: "IMInput-10207 [Story] 增加资源闭环开关",
	})
	if err == nil || !strings.Contains(err.Error(), "did not add a valid Change-Id") {
		t.Fatalf("expected Change-Id verification failure, got %v", err)
	}
	if got := gitCmd(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatalf("HEAD changed from %s to %s", before, got)
	}
	if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
		t.Fatalf("rollback did not restore original index: %q", got)
	}
	if got := gitCmd(t, dir, "status", "--short"); got != "?? switch.php" {
		t.Fatalf("worktree change was not preserved: %q", got)
	}
}

func TestCommitRejectsSensitivePaths(t *testing.T) {
	tests := []struct {
		name     string
		remote   string
		path     string
		message  string
		wantText string
	}{
		{"secret", "git@github.com:fancive/example.git", ".env", "chore: update environment", "sensitive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := initRepo(t, tc.remote)
			installChangeIDHook(t, dir)
			write(t, dir, tc.path, "value\n")
			_, err := Commit(context.Background(), Options{Dir: dir, Files: []string{tc.path}, Message: tc.message})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantText)) {
				t.Fatalf("expected %q error, got %v", tc.wantText, err)
			}
			if got := gitCmd(t, dir, "diff", "--cached", "--name-only"); got != "" {
				t.Fatalf("rejected path mutated index: %q", got)
			}
		})
	}
}

func TestCommitAllowsICodeDependencyFiles(t *testing.T) {
	dir := initRepo(t, "git@icode.baidu.com:baidu/inputmethod/v5api.git")
	installChangeIDHook(t, dir)
	write(t, dir, "go.mod", "module example.com/dependency-update\n")
	write(t, dir, "go.sum", "example.com/dependency v1.0.0 h1:test\n")

	result, err := Commit(context.Background(), Options{
		Dir:     dir,
		Files:   []string{"go.mod", "go.sum"},
		Message: "IMInput-10207 [Task] 更新依赖校验",
	})
	if err != nil {
		t.Fatalf("commit iCode dependency files: %v", err)
	}
	if got, want := strings.Join(result.Files, ","), "go.mod,go.sum"; got != want {
		t.Fatalf("committed files = %q, want %q", got, want)
	}
}
