package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/branchsync"
	"github.com/kunchenguid/no-mistakes/internal/commitpolicy"
	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/pipeline"
	"github.com/kunchenguid/no-mistakes/internal/safeurl"
	"github.com/kunchenguid/no-mistakes/internal/scm"
)

func isICodeRepo(sctx *pipeline.StepContext) bool {
	return sctx != nil && sctx.Repo != nil && scm.DetectProviderContext(sctx.Ctx, sctx.Repo.UpstreamURL) == scm.ProviderICode
}

// commitPipelineChanges keeps iCode on one Gerrit review by amending the
// current Change-Id-bearing tip. Other providers retain ordinary fix commits.
func commitPipelineChanges(sctx *pipeline.StepContext, message string) error {
	if !isICodeRepo(sctx) {
		_, err := stepGitRun(sctx, "commit", "-m", message)
		return err
	}
	if err := requireICodeTipChangeID(sctx); err != nil {
		return err
	}
	_, err := stepGitRun(sctx, "commit", "--amend", "--no-edit")
	return err
}

func requireICodeTipChangeID(sctx *pipeline.StepContext) error {
	message, err := stepGitRun(sctx, "log", "-1", "--format=%B")
	if err != nil {
		return fmt.Errorf("read iCode commit message: %w", err)
	}
	if !commitpolicy.HasValidICodeChangeID(message) {
		return fmt.Errorf("iCode commit is missing a valid Change-Id footer; install the Gerrit commit-msg hook and amend before validation")
	}
	return nil
}

func icodeReviewRef(branch string) (string, error) {
	branch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if branch == "" || strings.HasPrefix(branch, "refs/") || strings.ContainsAny(branch, "\r\n") {
		return "", fmt.Errorf("invalid iCode target branch %q", branch)
	}
	return "refs/for/" + branch, nil
}

// pushICodeReviewedHead delivers an immutable commit to Gerrit's refs/for
// namespace and verifies success through iCode's review API. refs/for is not a
// persistent remote ref, so ls-remote cannot serve as delivery evidence.
func pushICodeReviewedHead(sctx *pipeline.StepContext, headSHA string) (*scm.PR, error) {
	if err := requireICodeTipChangeID(sctx); err != nil {
		return nil, err
	}
	ref, err := icodeReviewRef(sctx.Run.Branch)
	if err != nil {
		return nil, err
	}
	pushURL := resolvePushURL(sctx)
	sctx.Log(fmt.Sprintf("pushing iCode review to %s (%s)...", safeurl.Redact(pushURL), ref))
	if _, err := stepGitRun(sctx, "push", pushURL, headSHA+":"+ref); err != nil {
		return nil, fmt.Errorf("push iCode review: %w", err)
	}

	host, skipReason := buildHost(sctx, scm.ProviderICode)
	if host == nil {
		return nil, fmt.Errorf("verify iCode review push: %s", skipReason)
	}
	if err := host.Available(sctx.Ctx); err != nil {
		return nil, fmt.Errorf("verify iCode review push: %w", err)
	}
	branch := strings.TrimPrefix(strings.TrimSpace(sctx.Run.Branch), "refs/heads/")
	var review *scm.PR
	for attempt := 0; attempt < 5; attempt++ {
		review, err = host.FindPR(sctx.Ctx, branch, branch)
		if err != nil {
			return nil, fmt.Errorf("verify iCode review push: %w", err)
		}
		if review != nil {
			break
		}
		select {
		case <-sctx.Ctx.Done():
			return nil, sctx.Ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if review == nil || strings.TrimSpace(review.URL) == "" {
		return nil, fmt.Errorf("pushed %s but iCode did not expose a review for head %s", ref, shortObjectID(headSHA))
	}

	if err := sctx.DB.UpdateRunPushBinding(sctx.Run.ID, db.PushBinding{
		HeadSHA:           headSHA,
		TargetKind:        "icode",
		TargetFingerprint: branchsync.TargetFingerprint(pushURL),
		Ref:               ref,
	}); err != nil {
		return nil, err
	}
	if err := sctx.DB.UpdateRunPRURL(sctx.Run.ID, review.URL); err != nil {
		return nil, err
	}
	sctx.Run.PRURL = &review.URL
	return review, nil
}

func icodeRebaseTargets(branch, defaultBranch, branchTarget string) []string {
	branch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if branch == "" || branch == strings.TrimSpace(defaultBranch) {
		return nil
	}
	return []string{branchTarget}
}

// pipelineBaseBranch is the comparison/rebase base for this delivery model.
// Pull-request providers compare feature branches with the repository default;
// iCode reviews target the current release branch through refs/for.
func pipelineBaseBranch(sctx *pipeline.StepContext) string {
	if isICodeRepo(sctx) && sctx != nil && sctx.Run != nil {
		if branch := strings.TrimPrefix(strings.TrimSpace(sctx.Run.Branch), "refs/heads/"); branch != "" {
			return branch
		}
	}
	if sctx != nil && sctx.Repo != nil {
		return sctx.Repo.DefaultBranch
	}
	return ""
}

func resolvePipelineBaseSHA(ctx context.Context, sctx *pipeline.StepContext) string {
	return resolveBranchBaseSHA(ctx, sctx.WorkDir, sctx.Run.BaseSHA, pipelineBaseBranch(sctx))
}
