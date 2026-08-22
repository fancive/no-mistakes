package delivery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/scm"
	"github.com/kunchenguid/no-mistakes/internal/scm/icode"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

func deliveryGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setupDeliveryRepo(t *testing.T, literalRemote string) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "init", "-b", "main")
	deliveryGit(t, root, "config", "user.name", "Test")
	deliveryGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "add", "main.go")
	deliveryGit(t, root, "commit", "-m", "feat: initial")
	deliveryGit(t, filepath.Dir(remote), "init", "--bare", remote)
	deliveryGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	deliveryGit(t, root, "remote", "add", "origin", literalRemote)
	deliveryGit(t, root, "config", "url."+remote+".insteadOf", literalRemote)
	deliveryGit(t, root, "push", "-u", "origin", "main")
	return root, remote
}

func commitDeliveryChange(t *testing.T, root, message string) string {
	t.Helper()
	file := filepath.Join(root, "main.go")
	existing, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(existing, []byte("// change\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "add", "main.go")
	deliveryGit(t, root, "commit", "-m", message)
	return deliveryGit(t, root, "rev-parse", "HEAD")
}

func authorizedICodePushRequest(t *testing.T, root string) types.PushRequest {
	t.Helper()
	cfg, err := config.LoadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	return types.PushRequest{
		ExpectedHead:     deliveryGit(t, root, "rev-parse", "HEAD"),
		AllowICodeSubmit: true,
		ICodeSubmitPolicyHash: cfg.ICodePolicyHash(
			icode.RepoPath(deliveryGit(t, root, "config", "--get", "remote.origin.url")),
			deliveryGit(t, root, "branch", "--show-current"),
		),
	}
}

func TestPushGitHubDirectMainUsesRegularExactSHAPush(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:fancive/example.git")
	head := commitDeliveryChange(t, root, "feat(cli): direct main")
	service := New(Options{Dir: root})

	result, err := service.Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardDelivered || result.TargetRef != "refs/heads/main" || result.SHA != head {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(strings.ToLower(result.NextAction), "pr") || strings.Contains(strings.ToLower(result.NextAction), "merge") {
		t.Fatalf("direct-main result claims nonexistent downstream work: %+v", result)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); got != head {
		t.Fatalf("remote main = %s, want %s", got, head)
	}
}

func TestPushGitHubFeatureCreatesOnlyFeatureBranch(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:someone/example.git")
	deliveryGit(t, root, "checkout", "-b", "feature")
	head := commitDeliveryChange(t, root, "feat(cli): feature push")
	mainBefore := deliveryGit(t, remote, "rev-parse", "refs/heads/main")
	service := New(Options{Dir: root})

	result, err := service.Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetRef != "refs/heads/feature" || result.Status != types.GuardDelivered {
		t.Fatalf("result = %+v", result)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/feature"); got != head {
		t.Fatalf("remote feature = %s, want %s", got, head)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("remote main changed: %s -> %s", mainBefore, got)
	}
}

func TestPushGitHubFanciveForkCheckoutCreatesOnlyFeatureBranch(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:fancive/example.git")
	const upstreamURL = "https://github.com/parent/example.git"
	deliveryGit(t, root, "remote", "add", "upstream", upstreamURL)
	deliveryGit(t, root, "config", "--add", "url."+remote+".insteadOf", upstreamURL)
	deliveryGit(t, root, "checkout", "-b", "feature")
	head := commitDeliveryChange(t, root, "feat(cli): fork feature push")
	mainBefore := deliveryGit(t, remote, "rev-parse", "refs/heads/main")

	result, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetRef != "refs/heads/feature" || result.Status != types.GuardDelivered {
		t.Fatalf("result = %+v", result)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/feature"); got != head {
		t.Fatalf("remote feature = %s, want %s", got, head)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("remote main changed: %s -> %s", mainBefore, got)
	}
}

func TestPushGitHubFanciveForkCheckoutUsesSSHOver443Endpoint(t *testing.T) {
	const (
		originURL        = "git@github.com:fancive/example.git"
		upstreamURL      = "git@github.com:parent/example.git"
		originEndpoint   = "ssh://git@ssh.github.com:443/fancive/example.git"
		upstreamEndpoint = "ssh://git@ssh.github.com:443/parent/example.git"
	)
	root, remote := setupDeliveryRepo(t, originURL)
	deliveryGit(t, root, "config", "--unset-all", "url."+remote+".insteadOf")
	deliveryGit(t, root, "config", "--add", "url."+remote+".insteadOf", originEndpoint)
	deliveryGit(t, root, "config", "--add", "url."+remote+".insteadOf", upstreamEndpoint)
	deliveryGit(t, root, "remote", "add", "upstream", upstreamURL)
	deliveryGit(t, root, "checkout", "-b", "feature")
	head := commitDeliveryChange(t, root, "feat(cli): fork feature push over 443")

	result, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetRef != "refs/heads/feature" || result.Status != types.GuardDelivered {
		t.Fatalf("result = %+v", result)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/feature"); got != head {
		t.Fatalf("remote feature = %s, want %s", got, head)
	}
}

func TestPushGitHubFanciveForkCheckoutBlocksMismatchedTrackingBranch(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:fancive/example.git")
	const upstreamURL = "https://github.com/parent/example.git"
	deliveryGit(t, root, "remote", "add", "upstream", upstreamURL)
	deliveryGit(t, root, "config", "--add", "url."+remote+".insteadOf", upstreamURL)
	deliveryGit(t, root, "checkout", "-b", "local-feature")
	head := deliveryGit(t, root, "rev-parse", "HEAD")
	deliveryGit(t, root, "update-ref", "refs/remotes/origin/pr-feature", head)
	deliveryGit(t, root, "branch", "--set-upstream-to=origin/pr-feature", "local-feature")
	mainBefore := deliveryGit(t, remote, "rev-parse", "refs/heads/main")

	_, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err == nil || !strings.Contains(err.Error(), "tracks origin/pr-feature") {
		t.Fatalf("push error = %v", err)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); got != mainBefore {
		t.Fatalf("remote main changed: %s -> %s", mainBefore, got)
	}
}

func TestPushRejectsUnexpectedHeadBeforeWritingRemote(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:fancive/example.git")
	commitDeliveryChange(t, root, "feat(cli): expected head")
	remoteBefore := deliveryGit(t, remote, "rev-parse", "refs/heads/main")

	_, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{ExpectedHead: strings.Repeat("0", 40)})
	if err == nil || !strings.Contains(err.Error(), "expected HEAD") {
		t.Fatalf("push error = %v", err)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); got != remoteBefore {
		t.Fatalf("remote changed despite expected-head mismatch: %s -> %s", remoteBefore, got)
	}
}

func TestPushGitHubNonFanciveDefaultBranchIsBlocked(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:someone/example.git")
	before := deliveryGit(t, remote, "rev-parse", "refs/heads/main")
	_, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err == nil || !strings.Contains(err.Error(), "feature branch") {
		t.Fatalf("push error = %v", err)
	}
	if after := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); after != before {
		t.Fatalf("remote main changed: %s -> %s", before, after)
	}
}

func TestPushGitHubFanciveFetchWithDifferentPushRepoIsNotDirectMain(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:fancive/example.git")
	pushLiteral := "git@github.com:someone/different.git"
	deliveryGit(t, root, "config", "remote.origin.pushurl", pushLiteral)
	deliveryGit(t, root, "config", "--add", "url."+remote+".insteadOf", pushLiteral)
	before := deliveryGit(t, remote, "rev-parse", "refs/heads/main")
	_, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err == nil || !strings.Contains(err.Error(), "feature branch") {
		t.Fatalf("push error = %v", err)
	}
	if after := deliveryGit(t, remote, "rev-parse", "refs/heads/main"); after != before {
		t.Fatalf("remote main changed: %s -> %s", before, after)
	}
}

func TestPushGitHubFeatureSupportsSingleForkPushURL(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:someone/example.git")
	pushLiteral := "git@github.com:fancive/example-fork.git"
	deliveryGit(t, root, "config", "remote.origin.pushurl", pushLiteral)
	deliveryGit(t, root, "config", "--add", "url."+remote+".insteadOf", pushLiteral)
	deliveryGit(t, root, "checkout", "-b", "feature")
	head := commitDeliveryChange(t, root, "feat(cli): fork push")
	result, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.TargetRef != "refs/heads/feature" || deliveryGit(t, remote, "rev-parse", "refs/heads/feature") != head {
		t.Fatalf("result = %+v", result)
	}
}

func TestPushGitHubRefusesNonFastForwardWithoutForce(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "git@github.com:someone/example.git")
	deliveryGit(t, root, "checkout", "-b", "feature")
	commitDeliveryChange(t, root, "feat(cli): local")
	deliveryGit(t, remote, "branch", "feature", "main")

	other := filepath.Join(t.TempDir(), "other")
	deliveryGit(t, filepath.Dir(other), "clone", remote, other)
	deliveryGit(t, other, "config", "user.name", "Other")
	deliveryGit(t, other, "config", "user.email", "other@example.com")
	deliveryGit(t, other, "checkout", "feature")
	if err := os.WriteFile(filepath.Join(other, "remote.txt"), []byte("remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, other, "add", "remote.txt")
	deliveryGit(t, other, "commit", "-m", "feat: remote")
	deliveryGit(t, other, "push", "origin", "feature")
	remoteBefore := deliveryGit(t, remote, "rev-parse", "refs/heads/feature")

	_, err := New(Options{Dir: root}).Push(context.Background(), types.PushRequest{})
	if err == nil || !strings.Contains(err.Error(), "non-fast-forward") {
		t.Fatalf("push error = %v", err)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/heads/feature"); got != remoteBefore {
		t.Fatalf("remote feature was overwritten: %s -> %s", remoteBefore, got)
	}
}

type fakeICodeHost struct {
	findResults      []*scm.PR
	findCalls        int
	states           []scm.PRState
	stateCalls       int
	checks           []scm.Check
	submission       scm.ReviewSubmission
	submitCalls      int
	available        error
	revisionMismatch bool
}

func (f *fakeICodeHost) Available(context.Context) error { return f.available }
func (f *fakeICodeHost) FindReview(context.Context, string) (*scm.PR, error) {
	index := f.findCalls
	f.findCalls++
	if index >= len(f.findResults) {
		index = len(f.findResults) - 1
	}
	if index < 0 {
		return nil, nil
	}
	return f.findResults[index], nil
}
func (f *fakeICodeHost) GetBoundPRState(_ context.Context, _ *scm.PR, _ string) (scm.PRState, error) {
	if f.revisionMismatch {
		return "", errors.New("current revision changed")
	}
	index := f.stateCalls
	f.stateCalls++
	if index >= len(f.states) {
		index = len(f.states) - 1
	}
	return f.states[index], nil
}
func (f *fakeICodeHost) GetChecks(context.Context, *scm.PR) ([]scm.Check, error) {
	return f.checks, nil
}
func (f *fakeICodeHost) EnsureSubmitted(context.Context, *scm.PR, string) (scm.ReviewSubmission, error) {
	f.submitCalls++
	return f.submission, nil
}

func TestPushICodeDrivesExistingTargetToMergedAndEmitsDeployHandoff(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("icode:\n  auto_submit: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	deliveryGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/not-advertised")
	head := commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	review := &scm.PR{Number: "7", URL: "https://icode.example/reviews/7"}
	host := &fakeICodeHost{
		findResults: []*scm.PR{nil, review},
		states:      []scm.PRState{scm.PRStateOpen, scm.PRStateMerged},
		checks:      []scm.Check{{Name: "iPipe", Bucket: scm.CheckBucketPass}},
		submission:  scm.ReviewSubmission{Submitted: true, Message: "accepted"},
	}
	service := New(Options{
		Dir:              root,
		ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil },
		PollInterval:     time.Millisecond,
		Timeout:          time.Second,
	})

	result, err := service.Push(context.Background(), authorizedICodePushRequest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardDelivered || result.SHA != head || result.ReviewURL != review.URL {
		t.Fatalf("result = %+v", result)
	}
	if result.DeployHandoff == nil || result.DeployHandoff.Skill != "opera-deploy" || result.DeployHandoff.Environment != "imeShahe" {
		t.Fatalf("deploy handoff = %+v", result.DeployHandoff)
	}
	if got := deliveryGit(t, remote, "rev-parse", "refs/for/server_BRANCH"); got != head {
		t.Fatalf("refs/for head = %s, want %s", got, head)
	}
}

func TestPushICodeRerunUsesMergedProviderTruthWithoutAnotherPush(t *testing.T) {
	root, remote := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	review := &scm.PR{Number: "7", URL: "https://icode.example/reviews/7"}
	host := &fakeICodeHost{findResults: []*scm.PR{review}, states: []scm.PRState{scm.PRStateMerged}}
	service := New(Options{
		Dir:              root,
		ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil },
	})

	result, err := service.Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardDelivered {
		t.Fatalf("result = %+v", result)
	}
	if _, err := exec.Command("git", "--git-dir="+remote, "rev-parse", "--verify", "refs/for/server_BRANCH").CombinedOutput(); err == nil {
		t.Fatal("rerun unexpectedly pushed a new refs/for patch set")
	}
}

func TestPushICodeReviewerFallbackReturnsPendingWithoutDeploy(t *testing.T) {
	root, _ := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("output_language: zh-CN\nicode:\n  auto_submit: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	review := &scm.PR{Number: "7", URL: "https://icode.example/reviews/7"}
	host := &fakeICodeHost{
		findResults: []*scm.PR{review},
		states:      []scm.PRState{scm.PRStateOpen},
		checks:      []scm.Check{{Name: "iPipe", Bucket: scm.CheckBucketPass}},
		submission:  scm.ReviewSubmission{Pending: true, Message: "waiting for external +2"},
	}
	service := New(Options{Dir: root, ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil }})

	result, err := service.Push(context.Background(), authorizedICodePushRequest(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardPending || result.OutputLanguage != "zh-CN" || result.DeployHandoff != nil || !strings.Contains(result.NextAction, "等待外部 +2") {
		t.Fatalf("result = %+v", result)
	}
}

func TestPushICodeAutoSubmitDefaultsToPendingWithoutWrite(t *testing.T) {
	root, _ := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	review := &scm.PR{Number: "7", URL: "https://icode.example/reviews/7"}
	host := &fakeICodeHost{
		findResults: []*scm.PR{review}, states: []scm.PRState{scm.PRStateOpen},
		checks: []scm.Check{{Name: "iPipe", Bucket: scm.CheckBucketPass}},
	}
	result, err := New(Options{Dir: root, ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil }}).Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardPending || host.submitCalls != 0 || !strings.Contains(result.NextAction, "enable icode.auto_submit") {
		t.Fatalf("result = %+v, submit calls = %d", result, host.submitCalls)
	}
}

func TestPushICodeBranchConfigCannotAuthorizeSubmitByItself(t *testing.T) {
	root, _ := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("icode:\n  auto_submit: true\n  reviewers: [reviewer1]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	review := &scm.PR{Number: "7", URL: "https://icode.example/reviews/7"}
	host := &fakeICodeHost{
		findResults: [](*scm.PR){review}, states: []scm.PRState{scm.PRStateOpen},
		checks:     []scm.Check{{Name: "iPipe", Bucket: scm.CheckBucketPass}},
		submission: scm.ReviewSubmission{Pending: true, Message: "waiting"},
	}

	result, err := New(Options{Dir: root, ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil }}).Push(context.Background(), types.PushRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.GuardPending || host.submitCalls != 0 {
		t.Fatalf("branch config authorized submit: result=%+v submitCalls=%d", result, host.submitCalls)
	}
}

func TestPushICodeRejectsMismatchedExplicitPolicyBinding(t *testing.T) {
	root, _ := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("icode:\n  auto_submit: true\n  reviewers: [reviewer1]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	host := &fakeICodeHost{}

	_, err := New(Options{Dir: root, ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil }}).Push(context.Background(), types.PushRequest{
		ExpectedHead:     deliveryGit(t, root, "rev-parse", "HEAD"),
		AllowICodeSubmit: true, ICodeSubmitPolicyHash: "stale-policy-hash",
	})
	if err == nil || !strings.Contains(err.Error(), "policy hash") {
		t.Fatalf("push error = %v", err)
	}
	if host.submitCalls != 0 {
		t.Fatalf("mismatched policy reached submit: %d", host.submitCalls)
	}
}

func TestPushICodeSubmitAuthorizationRequiresExpectedHead(t *testing.T) {
	root, _ := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("icode:\n  auto_submit: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	request := authorizedICodePushRequest(t, root)
	request.ExpectedHead = ""

	_, err := New(Options{Dir: root}).Push(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "requires --expected-head") {
		t.Fatalf("push error = %v", err)
	}
}

func TestPushICodeRefusesConcurrentPatchSetBeforeApproval(t *testing.T) {
	root, _ := setupDeliveryRepo(t, "ssh://icode.baidu.com:8235/baidu/inputmethod/server")
	deliveryGit(t, root, "checkout", "-b", "server_BRANCH")
	deliveryGit(t, root, "push", "origin", "server_BRANCH")
	commitDeliveryChange(t, root, "IMInput-1 [Task] 精简守卫\n\nChange-Id: I1111111111111111111111111111111111111111")
	review := &scm.PR{Number: "7", URL: "https://icode.example/reviews/7"}
	host := &fakeICodeHost{findResults: []*scm.PR{review}, revisionMismatch: true}
	service := New(Options{Dir: root, ICodeHostFactory: func(ICodeHostOptions) (ICodeHost, error) { return host, nil }})
	_, err := service.Push(context.Background(), types.PushRequest{})
	if err == nil || !strings.Contains(err.Error(), "current revision changed") {
		t.Fatalf("push error = %v", err)
	}
}
