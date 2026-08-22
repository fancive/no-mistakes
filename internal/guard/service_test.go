package guard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/lintscope"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

type fakeLintRunner struct {
	calls   int
	options lintscope.Options
	err     error
	run     func(lintscope.Options) error
}

func (f *fakeLintRunner) Run(_ context.Context, options lintscope.Options) (lintscope.Result, error) {
	f.calls++
	f.options = options
	if f.run != nil {
		if err := f.run(options); err != nil {
			return lintscope.Result{}, err
		}
	}
	return lintscope.Result{Command: options.Command, Files: append([]string(nil), options.Files...)}, f.err
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupGuardRepo(t *testing.T, literalRemote string) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "init", "-b", "main")
	gitCmd(t, root, "config", "user.name", "Test")
	gitCmd(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", "main.go")
	gitCmd(t, root, "commit", "-m", "feat: initial")
	gitCmd(t, filepath.Dir(remote), "init", "--bare", remote)
	gitCmd(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	gitCmd(t, root, "remote", "add", "origin", literalRemote)
	gitCmd(t, root, "config", "url."+remote+".insteadOf", literalRemote)
	gitCmd(t, root, "push", "-u", "origin", "main")
	return root, remote
}

func writeLintConfig(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("commands:\n  lint: run-project-lint\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckIsReadOnlyAndReportsExactFilesAndConfiguredLint(t *testing.T) {
	root, _ := setupGuardRepo(t, "git@github.com:fancive/example.git")
	writeLintConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeHead := gitCmd(t, root, "rev-parse", "HEAD")
	beforeIndex := gitCmd(t, root, "diff", "--cached", "--binary")
	runner := &fakeLintRunner{}
	service := New(Options{Dir: root, Lint: runner})

	result, err := service.Check(context.Background(), types.CheckRequest{
		Files: []string{"main.go"}, Message: "feat(cli): lean guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardPassed || result.Provider != "github" || result.TargetRef != "refs/heads/main" {
		t.Fatalf("result = %+v", result)
	}
	if result.CommitMessage != "feat(cli): lean guard" || result.CommitAuthor != "fancivez <fancive@gmail.com>" || result.ChangeIDRequired {
		t.Fatalf("commit plan = %+v", result)
	}
	if runner.calls != 0 || result.LintCommand != "run-project-lint" || !reflect.DeepEqual(result.LintFiles, []string{"main.go"}) {
		t.Fatalf("check ran lint or lost plan: calls=%d result=%+v", runner.calls, result)
	}
	if got := gitCmd(t, root, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := gitCmd(t, root, "diff", "--cached", "--binary"); got != beforeIndex {
		t.Fatalf("index changed:\n%s", got)
	}
}

func TestMissingICodeTargetBlocksBeforeLintOrCommit(t *testing.T) {
	root, _ := setupGuardRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	gitCmd(t, root, "checkout", "-b", "server_BRANCH")
	writeLintConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeLintRunner{}
	service := New(Options{Dir: root, Lint: runner})

	_, err := service.Check(context.Background(), types.CheckRequest{
		Files: []string{"main.go"}, Message: "IMInput-1 [Task] 精简守卫",
	})
	if err == nil || !strings.Contains(err.Error(), "refs/heads/server_BRANCH") {
		t.Fatalf("check error = %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("lint ran %d times before target proof", runner.calls)
	}

	beforeHead := gitCmd(t, root, "rev-parse", "HEAD")
	_, err = service.Commit(context.Background(), types.CommitRequest{
		Files: []string{"main.go"}, Message: "IMInput-1 [Task] 精简守卫", AllowRepoLint: true,
	})
	if err == nil || !strings.Contains(err.Error(), "refs/heads/server_BRANCH") {
		t.Fatalf("commit error = %v", err)
	}
	if got := gitCmd(t, root, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
}

func TestICodeBranchOnlyCheckDoesNotClaimHookWasVerified(t *testing.T) {
	root, remote := setupGuardRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	gitCmd(t, root, "checkout", "-b", "server_BRANCH")
	gitCmd(t, root, "push", "origin", "server_BRANCH")
	gitCmd(t, remote, "symbolic-ref", "HEAD", "refs/heads/not-advertised")
	result, err := New(Options{Dir: root}).Check(context.Background(), types.CheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitMsgHook != "required" || !result.ChangeIDRequired {
		t.Fatalf("commit plan = %+v", result)
	}
}

func TestICodeCheckExposesSubmitPolicyForExplicitCallerBinding(t *testing.T) {
	root, remote := setupGuardRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("icode:\n  auto_submit: true\n  reviewers: [alice, bob]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "checkout", "-b", "server_BRANCH")
	gitCmd(t, root, "push", "origin", "server_BRANCH")
	gitCmd(t, remote, "symbolic-ref", "HEAD", "refs/heads/not-advertised")

	result, err := New(Options{Dir: root}).Check(context.Background(), types.CheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ICodeAutoSubmit || result.ICodePolicyHash == "" || !reflect.DeepEqual(result.ICodeReviewers, []string{"alice", "bob"}) {
		t.Fatalf("iCode policy = %+v", result)
	}
	if result.ICodeSubmitAuthorized {
		t.Fatalf("read-only check claimed submit authorization: %+v", result)
	}
}

func TestNonFanciveGitHubDefaultBranchIsBlocked(t *testing.T) {
	root, _ := setupGuardRepo(t, "git@github.com:someone/example.git")
	_, err := New(Options{Dir: root}).Check(context.Background(), types.CheckRequest{})
	if err == nil || !strings.Contains(err.Error(), "feature branch") {
		t.Fatalf("check error = %v", err)
	}
}

func TestFanciveForkCheckoutUsesFeaturePushTarget(t *testing.T) {
	root, remote := setupGuardRepo(t, "git@github.com:fancive/example.git")
	const upstreamURL = "https://github.com/parent/example.git"
	gitCmd(t, root, "remote", "add", "upstream", upstreamURL)
	gitCmd(t, root, "config", "--add", "url."+remote+".insteadOf", upstreamURL)
	gitCmd(t, root, "checkout", "-b", "feature")

	result, err := New(Options{Dir: root}).Check(context.Background(), types.CheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetRef != "refs/heads/feature" || result.Branch != "feature" {
		t.Fatalf("fork checkout result = %+v", result)
	}
}

func TestFanciveForkCheckoutUsesGitHubSSHOver443Endpoints(t *testing.T) {
	const (
		originURL        = "git@github.com:fancive/example.git"
		upstreamURL      = "git@github.com:parent/example.git"
		originEndpoint   = "ssh://git@ssh.github.com:443/fancive/example.git"
		upstreamEndpoint = "ssh://git@ssh.github.com:443/parent/example.git"
	)
	root, remote := setupGuardRepo(t, originURL)
	gitCmd(t, root, "config", "--unset-all", "url."+remote+".insteadOf")
	gitCmd(t, root, "config", "--add", "url."+remote+".insteadOf", originEndpoint)
	gitCmd(t, root, "config", "--add", "url."+remote+".insteadOf", upstreamEndpoint)
	gitCmd(t, root, "remote", "add", "upstream", upstreamURL)
	gitCmd(t, root, "checkout", "-b", "feature")

	result, err := New(Options{Dir: root}).Check(context.Background(), types.CheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetRef != "refs/heads/feature" || result.Branch != "feature" {
		t.Fatalf("fork checkout result = %+v", result)
	}
}

func TestFanciveForkCheckoutBlocksMismatchedTrackingBranch(t *testing.T) {
	root, remote := setupGuardRepo(t, "git@github.com:fancive/example.git")
	const upstreamURL = "https://github.com/parent/example.git"
	gitCmd(t, root, "remote", "add", "upstream", upstreamURL)
	gitCmd(t, root, "config", "--add", "url."+remote+".insteadOf", upstreamURL)
	gitCmd(t, root, "checkout", "-b", "local-feature")
	head := gitCmd(t, root, "rev-parse", "HEAD")
	gitCmd(t, root, "update-ref", "refs/remotes/origin/pr-feature", head)
	gitCmd(t, root, "branch", "--set-upstream-to=origin/pr-feature", "local-feature")

	_, err := New(Options{Dir: root}).Check(context.Background(), types.CheckRequest{})
	if err == nil || !strings.Contains(err.Error(), "tracks origin/pr-feature") {
		t.Fatalf("check error = %v", err)
	}
}

func TestLintFailureLeavesHeadAndIndexUnchanged(t *testing.T) {
	root, _ := setupGuardRepo(t, "git@github.com:fancive/example.git")
	writeLintConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", "main.go")
	beforeHead := gitCmd(t, root, "rev-parse", "HEAD")
	beforeIndex := gitCmd(t, root, "diff", "--cached", "--binary")
	runner := &fakeLintRunner{err: errors.New("lint failed")}
	service := New(Options{Dir: root, Lint: runner})

	_, err := service.Commit(context.Background(), types.CommitRequest{
		Files: []string{"main.go"}, Message: "feat(cli): lean guard", AllowRepoLint: true,
	})
	if err == nil || !strings.Contains(err.Error(), "lint failed") {
		t.Fatalf("commit error = %v", err)
	}
	if got := gitCmd(t, root, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("HEAD changed: %s -> %s", beforeHead, got)
	}
	if got := gitCmd(t, root, "diff", "--cached", "--binary"); got != beforeIndex {
		t.Fatalf("index changed after lint failure:\n%s", got)
	}
}

func TestCommitRequiresExplicitRepositoryLintAuthorization(t *testing.T) {
	root, _ := setupGuardRepo(t, "git@github.com:fancive/example.git")
	writeLintConfig(t, root)
	service := New(Options{Dir: root, Lint: &fakeLintRunner{}})
	_, err := service.Commit(context.Background(), types.CommitRequest{
		Files: []string{"main.go"}, Message: "feat(cli): lean guard",
	})
	if err == nil || !strings.Contains(err.Error(), "--allow-repo-lint") {
		t.Fatalf("commit error = %v", err)
	}
}

func TestLintMutationBlocksCommitInsteadOfSilentlyShippingIt(t *testing.T) {
	root, _ := setupGuardRepo(t, "git@github.com:fancive/example.git")
	writeLintConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// authored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeHead := gitCmd(t, root, "rev-parse", "HEAD")
	runner := &fakeLintRunner{run: func(options lintscope.Options) error {
		return os.WriteFile(filepath.Join(options.Dir, "main.go"), []byte("package main\n// silently rewritten\n"), 0o644)
	}}
	_, err := New(Options{Dir: root, Lint: runner}).Commit(context.Background(), types.CommitRequest{
		Files: []string{"main.go"}, Message: "feat(cli): lean guard", AllowRepoLint: true,
	})
	if err == nil || !strings.Contains(err.Error(), "lint modified") {
		t.Fatalf("commit error = %v", err)
	}
	if got := gitCmd(t, root, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("lint mutation was committed: %s -> %s", beforeHead, got)
	}
}

func TestRemoteAdvanceDuringLintBlocksCommit(t *testing.T) {
	root, remote := setupGuardRepo(t, "git@github.com:fancive/example.git")
	writeLintConfig(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// authored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeHead := gitCmd(t, root, "rev-parse", "HEAD")
	runner := &fakeLintRunner{run: func(lintscope.Options) error {
		other := filepath.Join(t.TempDir(), "other")
		gitCmd(t, filepath.Dir(other), "clone", remote, other)
		gitCmd(t, other, "config", "user.name", "Other")
		gitCmd(t, other, "config", "user.email", "other@example.com")
		if err := os.WriteFile(filepath.Join(other, "remote.txt"), []byte("advance\n"), 0o644); err != nil {
			return err
		}
		gitCmd(t, other, "add", "remote.txt")
		gitCmd(t, other, "commit", "-m", "feat: advance remote")
		gitCmd(t, other, "push", "origin", "main")
		return nil
	}}
	_, err := New(Options{Dir: root, Lint: runner}).Commit(context.Background(), types.CommitRequest{
		Files: []string{"main.go"}, Message: "feat(cli): lean guard", AllowRepoLint: true,
	})
	if err == nil || !strings.Contains(err.Error(), "changed during lint") {
		t.Fatalf("commit error = %v", err)
	}
	if got := gitCmd(t, root, "rev-parse", "HEAD"); got != beforeHead {
		t.Fatalf("commit was created after remote advance: %s -> %s", beforeHead, got)
	}
}

func TestCheckRendersChineseNextActionWhenConfigured(t *testing.T) {
	root, _ := setupGuardRepo(t, "git@github.com:fancive/example.git")
	configBody := "output_language: zh-CN\ncommands:\n  lint: run-project-lint\n"
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := New(Options{Dir: root, Lint: &fakeLintRunner{}}).Check(context.Background(), types.CheckRequest{
		Files: []string{"main.go"}, Message: "feat(cli): lean guard",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.OutputLanguage != "zh-CN" || !strings.Contains(result.NextAction, "运行") {
		t.Fatalf("Chinese result = %+v", result)
	}
}
