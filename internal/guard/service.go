// Package guard owns synchronous, stateless repository checks and exact
// authored commits. Provider delivery is implemented separately.
package guard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/commitpolicy"
	"github.com/kunchenguid/no-mistakes/internal/commitprep"
	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/lintscope"
	"github.com/kunchenguid/no-mistakes/internal/scm"
	githubscm "github.com/kunchenguid/no-mistakes/internal/scm/github"
	"github.com/kunchenguid/no-mistakes/internal/scm/icode"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// LintRunner is the deterministic configured-command boundary.
type LintRunner interface {
	Run(context.Context, lintscope.Options) (lintscope.Result, error)
}

// Options configures a synchronous guard service.
type Options struct {
	Dir  string
	Lint LintRunner
}

// Service is stateless; every method re-resolves mutable Git/provider facts.
type Service struct {
	dir  string
	lint LintRunner
}

// New constructs a guard for Dir, defaulting to the real lint runner.
func New(options Options) *Service {
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		dir = "."
	}
	runner := options.Lint
	if runner == nil {
		runner = lintscope.Runner{}
	}
	return &Service{dir: dir, lint: runner}
}

type repositoryPlan struct {
	root            string
	remoteURL       string
	pushURL         string
	provider        scm.Provider
	branch          string
	head            string
	defaultName     string
	baseSHA         string
	targetSHA       string
	targetRef       string
	lintCommand     string
	language        string
	icodeAutoSubmit bool
	icodeReviewers  []string
	icodePolicyHash string
}

// Check validates without staging or committing. The configured lint command
// runs only when the caller explicitly authorizes it.
func (s *Service) Check(ctx context.Context, request types.CheckRequest) (types.GuardResult, error) {
	plan, err := s.inspect(ctx)
	if err != nil {
		return types.GuardResult{}, err
	}
	if (len(request.Files) == 0) != (strings.TrimSpace(request.Message) == "") {
		return types.GuardResult{}, fmt.Errorf("--file and --message must be supplied together for authored-commit checks")
	}
	files := append([]string(nil), request.Files...)
	validatedMessage := ""
	if len(files) > 0 {
		validation, err := commitprep.Validate(ctx, commitprep.Options{Dir: plan.root, Files: files, Message: request.Message})
		if err != nil {
			return types.GuardResult{}, err
		}
		files = validation.Files
		validatedMessage = validation.Message
	}
	result := plan.result()
	result.Files = files
	result.CommitAuthor, result.CommitMsgHook, result.ChangeIDRequired = commitPlan(plan.provider, len(files) > 0)
	if len(files) > 0 {
		result.CommitMessage = validatedMessage
	}
	result.LintCommand = plan.lintCommand
	result.LintFiles = append([]string(nil), files...)
	if len(files) > 0 {
		result.NextAction = localized(plan.language,
			"run no-mistakes commit with the same exact file list",
			"使用同一精确文件列表运行 no-mistakes commit")
	}
	return result, nil
}

// Commit revalidates branch, provider, target, exact scope, message and lint,
// then delegates the exact index transaction to commitprep.
func (s *Service) Commit(ctx context.Context, request types.CommitRequest) (types.GuardResult, error) {
	plan, err := s.inspect(ctx)
	if err != nil {
		return types.GuardResult{}, err
	}
	validation, err := commitprep.Validate(ctx, commitprep.Options{
		Dir: plan.root, Files: request.Files, Message: request.Message, Amend: request.Amend,
	})
	if err != nil {
		return types.GuardResult{}, err
	}
	if plan.lintCommand != "" && !request.AllowRepoLint {
		return types.GuardResult{}, fmt.Errorf("repository lint command requires explicit --allow-repo-lint authorization: %s", plan.lintCommand)
	}
	index, err := commitprep.CaptureIndex(ctx, plan.root)
	if err != nil {
		return types.GuardResult{}, fmt.Errorf("snapshot index before lint: %w", err)
	}
	if plan.lintCommand != "" {
		if _, err := s.runLint(ctx, plan, validation.Files); err != nil {
			if restoreErr := index.Restore(); restoreErr != nil {
				return types.GuardResult{}, fmt.Errorf("%v; restore index after lint failure: %w", err, restoreErr)
			}
			return types.GuardResult{}, err
		}
		if err := index.Restore(); err != nil {
			return types.GuardResult{}, fmt.Errorf("restore index after lint: %w", err)
		}
	}
	fresh, err := s.inspect(ctx)
	if err != nil {
		return types.GuardResult{}, fmt.Errorf("revalidate repository after lint: %w", err)
	}
	if fresh.root != plan.root || fresh.remoteURL != plan.remoteURL || fresh.pushURL != plan.pushURL || fresh.provider != plan.provider ||
		fresh.branch != plan.branch || fresh.head != plan.head || fresh.defaultName != plan.defaultName || fresh.baseSHA != plan.baseSHA ||
		fresh.targetSHA != plan.targetSHA || fresh.targetRef != plan.targetRef || fresh.lintCommand != plan.lintCommand || fresh.language != plan.language {
		return types.GuardResult{}, fmt.Errorf("repository branch, HEAD, provider, or push target changed during lint; rerun check")
	}
	if fresh.icodePolicyHash != plan.icodePolicyHash {
		return types.GuardResult{}, fmt.Errorf("repository iCode delivery policy changed during lint; rerun check")
	}
	commitResult, err := commitprep.Commit(ctx, commitprep.Options{
		Dir: plan.root, Files: validation.Files, Message: validation.Message, Amend: request.Amend,
	})
	if err != nil {
		return types.GuardResult{}, err
	}
	result := fresh.result()
	result.SHA = commitResult.SHA
	result.Files = commitResult.Files
	result.CommitMessage = validation.Message
	result.CommitAuthor, result.CommitMsgHook, result.ChangeIDRequired = commitPlan(plan.provider, true)
	result.LintCommand = plan.lintCommand
	result.LintFiles = append([]string(nil), validation.Files...)
	result.LintAuthorized = request.AllowRepoLint
	result.LintRan = plan.lintCommand != ""
	result.NextAction = localized(plan.language, "run no-mistakes push", "运行 no-mistakes push")
	return result, nil
}

func (s *Service) runLint(ctx context.Context, plan repositoryPlan, files []string) (lintscope.Result, error) {
	before, err := captureLintMutationSnapshot(ctx, plan.root, files)
	if err != nil {
		return lintscope.Result{}, fmt.Errorf("snapshot worktree before lint: %w", err)
	}
	result, runErr := s.lint.Run(ctx, lintscope.Options{
		Dir: plan.root, Command: plan.lintCommand, Files: files,
		BaseSHA: plan.baseSHA, HeadSHA: plan.head,
	})
	if runErr != nil {
		return result, runErr
	}
	after, err := captureLintMutationSnapshot(ctx, plan.root, files)
	if err != nil {
		return result, fmt.Errorf("snapshot worktree after lint: %w", err)
	}
	if !before.equal(after) {
		return result, fmt.Errorf("repository lint modified authored or tracked worktree content; inspect the changes and rerun check")
	}
	return result, nil
}

type lintMutationSnapshot struct {
	status     []byte
	fileHashes map[string][32]byte
}

func captureLintMutationSnapshot(ctx context.Context, root string, files []string) (lintMutationSnapshot, error) {
	status, err := git.RunRaw(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return lintMutationSnapshot{}, err
	}
	hashes := make(map[string][32]byte, len(files))
	for _, name := range files {
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil && !os.IsNotExist(err) {
			return lintMutationSnapshot{}, err
		}
		if os.IsNotExist(err) {
			payload = []byte("\x00no-mistakes-missing-file\x00")
		}
		hashes[name] = sha256.Sum256(payload)
	}
	return lintMutationSnapshot{status: status, fileHashes: hashes}, nil
}

func (s lintMutationSnapshot) equal(other lintMutationSnapshot) bool {
	if string(s.status) != string(other.status) || len(s.fileHashes) != len(other.fileHashes) {
		return false
	}
	for name, hash := range s.fileHashes {
		if other.fileHashes[name] != hash {
			return false
		}
	}
	return true
}

func (s *Service) inspect(ctx context.Context) (repositoryPlan, error) {
	root, err := git.FindGitRoot(s.dir)
	if err != nil {
		return repositoryPlan{}, fmt.Errorf("find git root: %w", err)
	}
	branch, err := git.CurrentBranch(ctx, root)
	if err != nil || branch == "" || branch == "HEAD" {
		return repositoryPlan{}, fmt.Errorf("synchronization requires an attached branch")
	}
	head, err := git.HeadSHA(ctx, root)
	if err != nil {
		return repositoryPlan{}, fmt.Errorf("resolve HEAD: %w", err)
	}
	endpoint, err := git.ResolveOriginEndpoint(ctx, root)
	if err != nil {
		return repositoryPlan{}, err
	}
	provider := scm.DetectProviderContext(ctx, endpoint.FetchLiteral)
	if provider != scm.ProviderGitHub && provider != scm.ProviderICode {
		return repositoryPlan{}, fmt.Errorf("provider %q is not supported by the lean guard", provider)
	}
	if pushProvider := scm.DetectProviderContext(ctx, endpoint.PushLiteral); pushProvider != provider {
		return repositoryPlan{}, fmt.Errorf("origin push URL provider %q does not match fetch provider %q", pushProvider, provider)
	}
	if provider == scm.ProviderICode && icode.RepoPath(endpoint.PushLiteral) != icode.RepoPath(endpoint.FetchLiteral) {
		return repositoryPlan{}, fmt.Errorf("iCode origin push URL targets a different repository")
	}
	defaultBranch := ""
	targetBranch := branch
	if provider == scm.ProviderGitHub {
		defaultBranch, err = strictDefaultBranch(ctx, root)
		if err != nil {
			return repositoryPlan{}, err
		}
		directMain := githubscm.DirectMainRemote(endpoint.FetchLiteral) && githubscm.SameRepository(endpoint.PushLiteral, endpoint.FetchLiteral)
		if directMain {
			targetBranch = defaultBranch
		} else if branch == defaultBranch {
			return repositoryPlan{}, fmt.Errorf("GitHub repositories outside fancive/* must use an attached feature branch, not %s", defaultBranch)
		}
	}
	targetRef := "refs/heads/" + targetBranch
	remoteTarget, err := git.LsRemoteEndpoint(ctx, root, endpoint.PushEffective, targetRef)
	if err != nil {
		return repositoryPlan{}, fmt.Errorf("verify remote target %s: %w", targetRef, err)
	}
	if (provider == scm.ProviderICode || targetBranch == defaultBranch) && strings.TrimSpace(remoteTarget) == "" {
		return repositoryPlan{}, fmt.Errorf("remote target %s does not exist", targetRef)
	}
	baseSHA := remoteTarget
	if provider == scm.ProviderICode {
		targetRef = "refs/for/" + branch
	} else if targetBranch != defaultBranch || endpoint.PushLiteral != endpoint.FetchLiteral {
		baseSHA, err = git.LsRemote(ctx, root, "origin", "refs/heads/"+defaultBranch)
		if err != nil || strings.TrimSpace(baseSHA) == "" {
			if err != nil {
				return repositoryPlan{}, fmt.Errorf("resolve remote default branch refs/heads/%s: %w", defaultBranch, err)
			}
			return repositoryPlan{}, fmt.Errorf("remote default branch refs/heads/%s does not exist", defaultBranch)
		}
	}
	repoConfig, err := config.LoadRepo(root)
	if err != nil {
		return repositoryPlan{}, err
	}
	icodeReviewers := []string(nil)
	icodePolicyHash := ""
	if provider == scm.ProviderICode {
		icodeReviewers = repoConfig.ICodeReviewers()
		icodePolicyHash = repoConfig.ICodePolicyHash(icode.RepoPath(endpoint.FetchLiteral), branch)
	}
	return repositoryPlan{
		root: root, remoteURL: endpoint.FetchLiteral, pushURL: endpoint.PushEffective,
		provider: provider, branch: branch, head: head,
		defaultName: defaultBranch, baseSHA: baseSHA, targetSHA: remoteTarget, targetRef: targetRef,
		lintCommand: strings.TrimSpace(repoConfig.Commands.Lint), language: repoConfig.Language(),
		icodeAutoSubmit: repoConfig.ICodeAutoSubmit(),
		icodeReviewers:  icodeReviewers,
		icodePolicyHash: icodePolicyHash,
	}, nil
}

func (p repositoryPlan) result() types.GuardResult {
	result := types.GuardResult{
		Status: types.GuardPassed, OutputLanguage: p.language, Provider: string(p.provider), Branch: p.branch,
		Head: p.head, TargetRef: p.targetRef,
	}
	if p.provider == scm.ProviderICode {
		result.ICodeAutoSubmit = p.icodeAutoSubmit
		result.ICodeReviewers = append([]string(nil), p.icodeReviewers...)
		result.ICodePolicyHash = p.icodePolicyHash
	}
	return result
}

func localized(language, english, chinese string) string {
	if language == "zh-CN" {
		return chinese
	}
	return english
}

func commitPlan(provider scm.Provider, hookVerified bool) (author, hook string, changeIDRequired bool) {
	if identity, ok := commitpolicy.AuthorFor(provider); ok {
		author = identity.Name + " <" + identity.Email + ">"
	} else {
		author = "local_git_config"
	}
	if provider == scm.ProviderICode {
		hook = "required"
		if hookVerified {
			hook = "verified"
		}
		changeIDRequired = true
	}
	return author, hook, changeIDRequired
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
