// Package delivery owns synchronous, stateless provider push and iCode review
// completion. It never creates branches, force-pushes, or edits code.
package delivery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/commitpolicy"
	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/scm"
	githubscm "github.com/kunchenguid/no-mistakes/internal/scm/github"
	"github.com/kunchenguid/no-mistakes/internal/scm/icode"
	"github.com/kunchenguid/no-mistakes/internal/shellenv"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

const (
	defaultICodePollInterval = 2 * time.Second
	defaultICodeTimeout      = 30 * time.Minute
)

// ICodeHost is the provider truth needed by lean synchronous delivery.
type ICodeHost interface {
	Available(context.Context) error
	FindReview(context.Context, string) (*scm.PR, error)
	GetBoundPRState(context.Context, *scm.PR, string) (scm.PRState, error)
	GetChecks(context.Context, *scm.PR) ([]scm.Check, error)
	EnsureSubmitted(context.Context, *scm.PR, string) (scm.ReviewSubmission, error)
}

// ICodeHostOptions are the immutable inputs for one provider session.
type ICodeHostOptions struct {
	WorkDir    string
	RemoteURL  string
	RepoPath   string
	Branch     string
	HeadSHA    string
	Reviewers  []string
	AutoSubmit bool
}

// ICodeHostFactory creates the provider adapter after all local facts resolve.
type ICodeHostFactory func(ICodeHostOptions) (ICodeHost, error)

// Options configures delivery and deterministic test timing.
type Options struct {
	Dir              string
	ICodeHostFactory ICodeHostFactory
	PollInterval     time.Duration
	Timeout          time.Duration
}

// Service carries no durable run state. Reruns reconstruct provider state.
type Service struct {
	dir          string
	icodeFactory ICodeHostFactory
	pollInterval time.Duration
	timeout      time.Duration
}

// New constructs a synchronous delivery service.
func New(options Options) *Service {
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		dir = "."
	}
	factory := options.ICodeHostFactory
	if factory == nil {
		factory = defaultICodeHostFactory
	}
	poll := options.PollInterval
	if poll <= 0 {
		poll = defaultICodePollInterval
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultICodeTimeout
	}
	return &Service{dir: dir, icodeFactory: factory, pollInterval: poll, timeout: timeout}
}

type pushPlan struct {
	root                  string
	remoteURL             string
	pushURL               string
	provider              scm.Provider
	branch                string
	head                  string
	defaultBranch         string
	targetRef             string
	directMain            bool
	repoConfig            *config.RepoConfig
	language              string
	icodePolicyHash       string
	icodeSubmitAuthorized bool
}

// Push re-resolves every mutable assumption and dispatches provider delivery.
func (s *Service) Push(ctx context.Context, request types.PushRequest) (types.GuardResult, error) {
	plan, err := s.inspect(ctx)
	if err != nil {
		return types.GuardResult{}, err
	}
	plan, err = authorizePushRequest(plan, request)
	if err != nil {
		return types.GuardResult{}, err
	}
	switch plan.provider {
	case scm.ProviderGitHub:
		return s.pushGitHub(ctx, plan)
	case scm.ProviderICode:
		return s.pushICode(ctx, plan)
	default:
		return types.GuardResult{}, fmt.Errorf("provider %q is not supported by lean delivery", plan.provider)
	}
}

func authorizePushRequest(plan pushPlan, request types.PushRequest) (pushPlan, error) {
	expectedHead := strings.TrimSpace(request.ExpectedHead)
	if expectedHead != "" && !strings.EqualFold(expectedHead, plan.head) {
		return pushPlan{}, fmt.Errorf("expected HEAD %s but current HEAD is %s", expectedHead, plan.head)
	}
	policyHash := strings.TrimSpace(request.ICodeSubmitPolicyHash)
	if plan.provider != scm.ProviderICode {
		if request.AllowICodeSubmit || policyHash != "" {
			return pushPlan{}, fmt.Errorf("iCode submit authorization is valid only for an iCode origin")
		}
		return plan, nil
	}
	if !request.AllowICodeSubmit {
		if policyHash != "" {
			return pushPlan{}, fmt.Errorf("iCode policy hash requires --allow-icode-submit")
		}
		return plan, nil
	}
	if expectedHead == "" {
		return pushPlan{}, fmt.Errorf("iCode submit authorization requires --expected-head")
	}
	if !plan.repoConfig.ICodeAutoSubmit() {
		return pushPlan{}, fmt.Errorf("repository policy does not enable iCode auto_submit")
	}
	if policyHash == "" || !strings.EqualFold(policyHash, plan.icodePolicyHash) {
		return pushPlan{}, fmt.Errorf("iCode policy hash does not match the policy reported by no-mistakes check")
	}
	plan.icodeSubmitAuthorized = true
	return plan, nil
}

func (s *Service) inspect(ctx context.Context) (pushPlan, error) {
	root, err := git.FindGitRoot(s.dir)
	if err != nil {
		return pushPlan{}, fmt.Errorf("find git root: %w", err)
	}
	branch, err := git.CurrentBranch(ctx, root)
	if err != nil || branch == "" || branch == "HEAD" {
		return pushPlan{}, fmt.Errorf("push requires an attached branch")
	}
	head, err := git.HeadSHA(ctx, root)
	if err != nil {
		return pushPlan{}, fmt.Errorf("resolve exact push HEAD: %w", err)
	}
	endpoint, err := git.ResolveOriginEndpoint(ctx, root)
	if err != nil {
		return pushPlan{}, err
	}
	remoteURL := endpoint.FetchLiteral
	provider := scm.DetectProviderContext(ctx, remoteURL)
	if provider != scm.ProviderGitHub && provider != scm.ProviderICode {
		return pushPlan{}, fmt.Errorf("provider %q is not supported by lean delivery", provider)
	}
	if pushProvider := scm.DetectProviderContext(ctx, endpoint.PushLiteral); pushProvider != provider {
		return pushPlan{}, fmt.Errorf("origin push URL provider %q does not match fetch provider %q", pushProvider, provider)
	}
	if provider == scm.ProviderICode && icode.RepoPath(endpoint.PushLiteral) != icode.RepoPath(remoteURL) {
		return pushPlan{}, fmt.Errorf("iCode origin push URL targets a different repository")
	}
	defaultBranch := ""
	if provider == scm.ProviderGitHub {
		defaultBranch, err = strictDefaultBranch(ctx, root)
		if err != nil {
			return pushPlan{}, err
		}
	}
	directMain := provider == scm.ProviderGitHub && githubscm.DirectMainRemote(remoteURL) && githubscm.SameRepository(endpoint.PushLiteral, remoteURL)
	if provider == scm.ProviderGitHub && !directMain && branch == defaultBranch {
		return pushPlan{}, fmt.Errorf("GitHub repositories outside fancive/* must use an attached feature branch, not %s", defaultBranch)
	}
	targetBranch := branch
	if directMain {
		targetBranch = defaultBranch
	}
	targetRef := "refs/heads/" + targetBranch
	if provider == scm.ProviderICode {
		targetRef = "refs/for/" + branch
	}
	repoConfig, err := config.LoadRepo(root)
	if err != nil {
		return pushPlan{}, err
	}
	icodePolicyHash := ""
	if provider == scm.ProviderICode {
		icodePolicyHash = repoConfig.ICodePolicyHash(icode.RepoPath(remoteURL), branch)
	}
	return pushPlan{
		root: root, remoteURL: remoteURL, pushURL: endpoint.PushEffective,
		provider: provider, branch: branch, head: head,
		defaultBranch: defaultBranch, targetRef: targetRef, directMain: directMain, repoConfig: repoConfig,
		language: repoConfig.Language(), icodePolicyHash: icodePolicyHash,
	}, nil
}

func (s *Service) pushGitHub(ctx context.Context, plan pushPlan) (types.GuardResult, error) {
	plan, err := s.revalidate(ctx, plan)
	if err != nil {
		return types.GuardResult{}, err
	}
	remoteHead, err := git.LsRemoteEndpoint(ctx, plan.root, plan.pushURL, plan.targetRef)
	if err != nil {
		return types.GuardResult{}, fmt.Errorf("inspect GitHub target %s: %w", plan.targetRef, err)
	}
	if plan.directMain && remoteHead == "" {
		return types.GuardResult{}, fmt.Errorf("direct-main target %s does not exist", plan.targetRef)
	}
	if remoteHead != "" && !strings.EqualFold(remoteHead, plan.head) {
		if err := git.FetchEndpoint(ctx, plan.root, plan.pushURL, plan.targetRef); err != nil {
			return types.GuardResult{}, fmt.Errorf("fetch GitHub target %s: %w", plan.targetRef, err)
		}
		if _, err := git.Run(ctx, plan.root, "merge-base", "--is-ancestor", remoteHead, plan.head); err != nil {
			return types.GuardResult{}, fmt.Errorf("non-fast-forward GitHub push refused for %s", plan.targetRef)
		}
	}
	if !strings.EqualFold(remoteHead, plan.head) {
		plan, err = s.revalidate(ctx, plan)
		if err != nil {
			return types.GuardResult{}, err
		}
		freshRemoteHead, err := git.LsRemoteEndpoint(ctx, plan.root, plan.pushURL, plan.targetRef)
		if err != nil {
			return types.GuardResult{}, fmt.Errorf("recheck GitHub target %s: %w", plan.targetRef, err)
		}
		if plan.directMain && freshRemoteHead == "" {
			return types.GuardResult{}, fmt.Errorf("direct-main target %s disappeared before push", plan.targetRef)
		}
		if freshRemoteHead != "" && !strings.EqualFold(freshRemoteHead, plan.head) {
			if err := git.FetchEndpoint(ctx, plan.root, plan.pushURL, plan.targetRef); err != nil {
				return types.GuardResult{}, fmt.Errorf("recheck GitHub target %s: %w", plan.targetRef, err)
			}
			if _, err := git.Run(ctx, plan.root, "merge-base", "--is-ancestor", freshRemoteHead, plan.head); err != nil {
				return types.GuardResult{}, fmt.Errorf("non-fast-forward GitHub push refused for %s", plan.targetRef)
			}
		}
		if err := git.PushCommitEndpoint(ctx, plan.root, plan.pushURL, plan.head, plan.targetRef); err != nil {
			return types.GuardResult{}, fmt.Errorf("push exact HEAD to GitHub %s: %w", plan.targetRef, err)
		}
	}
	verified, err := git.LsRemoteEndpoint(ctx, plan.root, plan.pushURL, plan.targetRef)
	if err != nil || !strings.EqualFold(verified, plan.head) {
		if err != nil {
			return types.GuardResult{}, fmt.Errorf("verify GitHub push %s: %w", plan.targetRef, err)
		}
		return types.GuardResult{}, fmt.Errorf("verify GitHub push %s: remote head %s does not equal %s", plan.targetRef, verified, plan.head)
	}
	nextAction := localized(plan.language,
		"return to the owning workflow for any PR, CI, or merge steps",
		"返回主交付流程处理后续 PR、CI 或合入步骤")
	if plan.directMain {
		nextAction = localized(plan.language,
			"delivery complete; exact HEAD is on the default branch",
			"交付完成；精确 HEAD 已位于默认分支")
	}
	return types.GuardResult{
		Status: types.GuardDelivered, OutputLanguage: plan.language, Provider: string(plan.provider), Branch: plan.branch,
		Head: plan.head, SHA: plan.head, TargetRef: plan.targetRef,
		NextAction: nextAction,
	}, nil
}

func (s *Service) pushICode(ctx context.Context, plan pushPlan) (types.GuardResult, error) {
	targetHeadRef := "refs/heads/" + plan.branch
	targetHead, err := git.LsRemoteEndpoint(ctx, plan.root, plan.pushURL, targetHeadRef)
	if err != nil {
		return types.GuardResult{}, fmt.Errorf("verify iCode target %s: %w", targetHeadRef, err)
	}
	if strings.TrimSpace(targetHead) == "" {
		return types.GuardResult{}, fmt.Errorf("iCode target %s does not exist; run $ipipe-pull-branch explicitly", targetHeadRef)
	}
	message, err := git.Run(ctx, plan.root, "show", "-s", "--format=%B", plan.head)
	if err != nil {
		return types.GuardResult{}, fmt.Errorf("read iCode HEAD message: %w", err)
	}
	if !commitpolicy.HasValidICodeChangeID(message) {
		return types.GuardResult{}, fmt.Errorf("iCode HEAD is missing a valid Change-Id footer")
	}
	repoPath := icode.RepoPath(plan.remoteURL)
	if repoPath == "" {
		return types.GuardResult{}, fmt.Errorf("resolve iCode repository path from origin")
	}
	reviewers := []string(nil)
	if plan.icodeSubmitAuthorized {
		reviewers = append(reviewers, plan.repoConfig.ICodeReviewers()...)
	}
	host, err := s.icodeFactory(ICodeHostOptions{
		WorkDir: plan.root, RemoteURL: plan.remoteURL, RepoPath: repoPath,
		Branch: plan.branch, HeadSHA: plan.head,
		Reviewers:  reviewers,
		AutoSubmit: plan.icodeSubmitAuthorized,
	})
	if err != nil {
		return types.GuardResult{}, err
	}
	if err := host.Available(ctx); err != nil {
		return types.GuardResult{}, fmt.Errorf("iCode delivery unavailable: %w", err)
	}
	review, err := host.FindReview(ctx, plan.branch)
	if err != nil {
		return types.GuardResult{}, fmt.Errorf("find existing iCode review: %w", err)
	}
	if review == nil {
		plan, err = s.revalidate(ctx, plan)
		if err != nil {
			return types.GuardResult{}, err
		}
		if targetHead, err = git.LsRemoteEndpoint(ctx, plan.root, plan.pushURL, targetHeadRef); err != nil || strings.TrimSpace(targetHead) == "" {
			if err != nil {
				return types.GuardResult{}, fmt.Errorf("recheck iCode target %s: %w", targetHeadRef, err)
			}
			return types.GuardResult{}, fmt.Errorf("iCode target %s disappeared; run $ipipe-pull-branch explicitly", targetHeadRef)
		}
		if err := git.PushCommitEndpoint(ctx, plan.root, plan.pushURL, plan.head, plan.targetRef); err != nil {
			return types.GuardResult{}, fmt.Errorf("push iCode review %s: %w", plan.targetRef, err)
		}
		for attempt := 0; attempt < 5 && review == nil; attempt++ {
			review, err = host.FindReview(ctx, plan.branch)
			if err != nil {
				return types.GuardResult{}, fmt.Errorf("verify iCode review push: %w", err)
			}
			if review == nil && attempt < 4 {
				if err := wait(ctx, s.pollInterval); err != nil {
					return types.GuardResult{}, err
				}
			}
		}
	}
	if review == nil || strings.TrimSpace(review.URL) == "" {
		return types.GuardResult{}, fmt.Errorf("iCode did not expose a review for exact HEAD %s", plan.head)
	}
	return s.driveICode(ctx, plan, host, review)
}

func (s *Service) revalidate(ctx context.Context, expected pushPlan) (pushPlan, error) {
	fresh, err := s.inspect(ctx)
	if err != nil {
		return pushPlan{}, fmt.Errorf("revalidate delivery assumptions: %w", err)
	}
	if fresh.root != expected.root || fresh.remoteURL != expected.remoteURL || fresh.pushURL != expected.pushURL ||
		fresh.provider != expected.provider || fresh.branch != expected.branch || fresh.head != expected.head ||
		fresh.defaultBranch != expected.defaultBranch || fresh.targetRef != expected.targetRef || fresh.directMain != expected.directMain ||
		fresh.repoConfig.Language() != expected.repoConfig.Language() || fresh.icodePolicyHash != expected.icodePolicyHash {
		return pushPlan{}, fmt.Errorf("repository branch, HEAD, origin, push destination, target, or delivery policy changed; rerun no-mistakes push")
	}
	fresh.icodeSubmitAuthorized = expected.icodeSubmitAuthorized
	return fresh, nil
}

func (s *Service) driveICode(ctx context.Context, plan pushPlan, host ICodeHost, review *scm.PR) (types.GuardResult, error) {
	deadline := time.Now().Add(s.timeout)
	baseResult := types.GuardResult{
		OutputLanguage: plan.language, Provider: string(plan.provider), Branch: plan.branch, Head: plan.head, SHA: plan.head,
		TargetRef: plan.targetRef, ReviewURL: review.URL,
		ICodeAutoSubmit:       plan.repoConfig.ICodeAutoSubmit(),
		ICodeReviewers:        append([]string(nil), plan.repoConfig.ICodeReviewers()...),
		ICodePolicyHash:       plan.icodePolicyHash,
		ICodeSubmitAuthorized: plan.icodeSubmitAuthorized,
	}
	for {
		state, err := host.GetBoundPRState(ctx, review, plan.head)
		if err != nil {
			return types.GuardResult{}, fmt.Errorf("read iCode review state: %w", err)
		}
		switch state {
		case scm.PRStateMerged:
			baseResult.Status = types.GuardDelivered
			if plan.branch != "main" && plan.branch != "master" {
				baseResult.DeployHandoff = &types.DeployHandoff{Skill: "opera-deploy", Environment: "imeShahe"}
			}
			return baseResult, nil
		case scm.PRStateClosed:
			return types.GuardResult{}, fmt.Errorf("iCode review %s is closed without merge", review.URL)
		}

		checks, err := host.GetChecks(ctx, review)
		if err != nil {
			return types.GuardResult{}, fmt.Errorf("read iCode checks: %w", err)
		}
		ready, failed := classifyChecks(checks)
		if failed != "" {
			return types.GuardResult{}, fmt.Errorf("iCode check failed: %s", failed)
		}
		if ready {
			if !plan.icodeSubmitAuthorized {
				baseResult.Status = types.GuardPending
				if plan.repoConfig.ICodeAutoSubmit() {
					baseResult.NextAction = localized(plan.language,
						"iCode checks passed; complete +2/submit externally or rerun push with explicit authorization bound to this policy hash",
						"iCode 检查已通过；请在外部完成 +2/submit，或使用绑定当前策略哈希的显式授权重新运行 push")
				} else {
					baseResult.NextAction = localized(plan.language,
						"iCode checks passed; enable icode.auto_submit in reviewed repository policy or complete +2/submit externally, then rerun no-mistakes check",
						"iCode 检查已通过；请在已评审的仓库策略中启用 icode.auto_submit，或在外部完成 +2/submit，然后重新运行 no-mistakes check")
				}
				return baseResult, nil
			}
			if _, err := s.revalidate(ctx, plan); err != nil {
				return types.GuardResult{}, err
			}
			if _, err := host.GetBoundPRState(ctx, review, plan.head); err != nil {
				return types.GuardResult{}, fmt.Errorf("revalidate iCode patch set before submit: %w", err)
			}
			submission, err := host.EnsureSubmitted(ctx, review, plan.head)
			if err != nil {
				return types.GuardResult{}, fmt.Errorf("submit iCode review: %w", err)
			}
			if submission.Pending {
				baseResult.Status = types.GuardPending
				baseResult.NextAction = strings.TrimSpace(submission.Message)
				if plan.language == "zh-CN" {
					baseResult.NextAction = "等待外部 +2 或平台前置条件满足后，重新运行 no-mistakes push"
				} else if baseResult.NextAction == "" {
					baseResult.NextAction = localized(plan.language,
						"wait for external +2 or provider prerequisites, then rerun no-mistakes push",
						"等待外部 +2 或平台前置条件满足后，重新运行 no-mistakes push")
				}
				return baseResult, nil
			}
		}
		if time.Now().After(deadline) {
			baseResult.Status = types.GuardPending
			baseResult.NextAction = localized(plan.language,
				"iCode checks or merge are still pending; rerun no-mistakes push",
				"iCode 检查或合入仍在等待中，请重新运行 no-mistakes push")
			return baseResult, nil
		}
		if err := wait(ctx, s.pollInterval); err != nil {
			return types.GuardResult{}, err
		}
	}
}

func localized(language, english, chinese string) string {
	if language == "zh-CN" {
		return chinese
	}
	return english
}

func classifyChecks(checks []scm.Check) (ready bool, failed string) {
	if len(checks) == 0 {
		return false, ""
	}
	for _, check := range checks {
		switch check.Bucket {
		case scm.CheckBucketFail, scm.CheckBucketCancel:
			return false, check.Name + " (" + check.State + ")"
		case scm.CheckBucketPending:
			ready = false
			continue
		case scm.CheckBucketPass, scm.CheckBucketSkip:
		default:
			ready = false
			continue
		}
		if !ready && check.Bucket != scm.CheckBucketPass && check.Bucket != scm.CheckBucketSkip {
			continue
		}
	}
	for _, check := range checks {
		if check.Bucket != scm.CheckBucketPass && check.Bucket != scm.CheckBucketSkip {
			return false, ""
		}
	}
	return true, ""
}

func defaultICodeHostFactory(options ICodeHostOptions) (ICodeHost, error) {
	cmdFactory := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = options.WorkDir
		cmd.Env = os.Environ()
		shellenv.ConfigureShellCommand(cmd)
		return cmd
	}
	return icode.New(cmdFactory, func() bool {
		_, err := exec.LookPath("icode-cli")
		return err == nil
	}, options.RepoPath, options.HeadSHA, icode.Options{
		Reviewers: strings.Join(options.Reviewers, ","), AutoSubmit: options.AutoSubmit,
	}), nil
}

func strictDefaultBranch(ctx context.Context, root string) (string, error) {
	out, err := git.Run(ctx, root, "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve remote default branch: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "ref: refs/heads/") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return strings.TrimPrefix(fields[1], "refs/heads/"), nil
		}
	}
	return "", fmt.Errorf("remote HEAD does not identify a default branch")
}

func wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
