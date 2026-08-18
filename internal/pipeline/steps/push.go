package steps

import (
	"fmt"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/branchsync"
	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/pipeline"
	"github.com/kunchenguid/no-mistakes/internal/safeurl"
	"github.com/kunchenguid/no-mistakes/internal/types"
)

// PushStep force-pushes the worktree state to the configured push remote.
type PushStep struct{}

func (s *PushStep) Name() types.StepName { return types.StepPush }

func (s *PushStep) Execute(sctx *pipeline.StepContext) (*pipeline.StepOutcome, error) {
	if err := assertPipelineHeadContinuity(sctx, s.Name()); err != nil {
		return nil, err
	}
	ctx := sctx.Ctx
	newHeadSHA := ""
	if err := sctx.DB.SetRunPushActive(sctx.Run.ID, true); err != nil {
		return nil, err
	}
	defer func() { _ = sctx.DB.SetRunPushActive(sctx.Run.ID, false) }()

	// Run format command if configured (before committing, so changes are formatted)
	if fmtCmd := sctx.Config.Commands.Format; fmtCmd != "" {
		sctx.Log(fmt.Sprintf("running formatter: %s", fmtCmd))
		output, exitCode, err := runStepShellCommand(sctx, fmtCmd)
		if err != nil {
			sctx.Log(fmt.Sprintf("warning: format command failed: %v", err))
		} else if exitCode != 0 {
			sctx.Log(fmt.Sprintf("warning: format command exited with code %d: %s", exitCode, output))
		}
	}

	// Commit any uncommitted changes from agent fixes. Test evidence is
	// deliberately not among them: it is collected outside the worktree and
	// published to the orphan evidence branch (internal/evidence), so no
	// artifact ever enters the pushed branch or the default branch's history.
	status, _ := git.Run(ctx, sctx.WorkDir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		sctx.Log("committing agent changes...")
		if _, err := git.Run(ctx, sctx.WorkDir, "add", "-A"); err != nil {
			return nil, fmt.Errorf("stage agent changes: %w", err)
		}
		if err := commitPipelineChanges(sctx, "no-mistakes: apply agent fixes"); err != nil {
			return nil, fmt.Errorf("commit agent changes: %w", err)
		}
		headSHA, err := git.HeadSHA(ctx, sctx.WorkDir)
		if err != nil {
			return nil, fmt.Errorf("resolve head after commit: %w", err)
		}
		newHeadSHA = headSHA
	}

	sourceRef := normalizedBranchRef(sctx.Run.Branch)
	ref := sourceRef
	branch := strings.TrimPrefix(ref, "refs/heads/")
	directMain := isGitHubDirectMainRepo(sctx)
	if directMain {
		branch = strings.TrimSpace(sctx.Repo.DefaultBranch)
		if branch == "" {
			return nil, fmt.Errorf("direct-main delivery requires a resolved default branch")
		}
		ref = normalizedBranchRef(branch)
	}

	pushURL := resolvePushURL(sctx)
	pushTarget := "upstream"
	usingFork := strings.TrimSpace(sctx.Repo.ForkURL) != ""
	if directMain {
		pushURL = resolveUpstreamURL(sctx)
		pushTarget = "direct-main"
		usingFork = false
		sctx.Log(fmt.Sprintf("pushing directly to default branch %s (%s)...", safeurl.Redact(pushURL), ref))
	} else if usingFork {
		pushTarget = "fork"
		sctx.Log(fmt.Sprintf("pushing to fork %s (%s)...", safeurl.Redact(pushURL), ref))
	} else {
		sctx.Log(fmt.Sprintf("pushing to %s (%s)...", safeurl.Redact(pushURL), ref))
	}

	headBeingPushed, err := git.HeadSHA(ctx, sctx.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("resolve head before push: %w", err)
	}
	if err := assertReviewApprovedPushHead(sctx, headBeingPushed); err != nil {
		return nil, err
	}
	if isICodeRepo(sctx) {
		review, err := pushICodeReviewedHead(sctx, headBeingPushed)
		if err != nil {
			return nil, err
		}
		if newHeadSHA != "" {
			if _, err := git.Run(ctx, sctx.WorkDir, "update-ref", normalizedBranchRef(sctx.Run.Branch), newHeadSHA); err != nil {
				return nil, fmt.Errorf("update local branch ref: %w", err)
			}
		}
		if headBeingPushed != sctx.Run.HeadSHA {
			sctx.Run.HeadSHA = headBeingPushed
			if err := sctx.DB.UpdateRunHeadSHA(sctx.Run.ID, headBeingPushed); err != nil {
				return nil, err
			}
		}
		sctx.Log(fmt.Sprintf("pushed iCode review successfully: %s", review.URL))
		return &pipeline.StepOutcome{PRURL: review.URL}, nil
	}

	if directMain {
		// Personal-repository direct delivery is intentionally stricter than the
		// branch/PR path: the default branch may only move by fast-forward and is
		// never force-pushed. A concurrent remote update therefore fails closed.
		remoteHead, err := git.LsRemote(ctx, sctx.WorkDir, pushURL, ref)
		if err != nil {
			return nil, fmt.Errorf("inspect direct-main target: %w", err)
		}
		if strings.TrimSpace(remoteHead) == "" {
			return nil, fmt.Errorf("direct-main target %s does not exist", ref)
		}
		if remoteHead != headBeingPushed {
			if _, err := git.Run(ctx, sctx.WorkDir, "merge-base", "--is-ancestor", remoteHead, headBeingPushed); err != nil {
				return nil, fmt.Errorf("direct-main non-fast-forward: remote %s is not an ancestor of reviewed head %s", shortObjectID(remoteHead), shortObjectID(headBeingPushed))
			}
			if err := git.PushCommit(ctx, sctx.WorkDir, pushURL, headBeingPushed, ref, "", false); err != nil {
				return nil, fmt.Errorf("push to %s: %w", pushTarget, err)
			}
		}
	} else {
		// Decide whether force-pushing would discard commits the pipeline never saw.
		// The lease is anchored to the remote-tracking ref the rebase step freshly
		// fetched (the exact commit this branch was rebased against), so a push that
		// would clobber an out-of-band or stale-mirror commit fails loudly instead
		// of silently dropping it. A bare --force-with-lease offers no protection
		// when pushing to a URL (no remote-tracking refs), so the anchor is explicit.
		lastSeen := lastFetchedBranchTip(ctx, sctx.WorkDir, branch, usingFork)
		gitRun := func(args ...string) (string, error) { return git.Run(ctx, sctx.WorkDir, args...) }
		decision, err := resolveForcePushDecision(gitRun, pushURL, ref, headBeingPushed, lastSeen, sctx.Run.BaseSHA)
		if err != nil {
			return nil, fmt.Errorf("push to %s: %w", pushTarget, err)
		}
		switch {
		case decision.newBranch:
			// New branch: regular push (no force needed).
			if err := git.PushCommit(ctx, sctx.WorkDir, pushURL, headBeingPushed, ref, "", false); err != nil {
				return nil, fmt.Errorf("push to %s: %w", pushTarget, err)
			}
		case decision.upToDate:
			// Remote already at this exact head. This freshly verified equality is a
			// successful binding even though no objects needed to move.
		default:
			// Existing branch: force-with-lease anchored to the verified remote head.
			if err := git.PushCommit(ctx, sctx.WorkDir, pushURL, headBeingPushed, ref, decision.remoteSHA, true); err != nil {
				return nil, fmt.Errorf("push to %s: %w", pushTarget, err)
			}
		}
	}
	verifiedRemote, err := git.LsRemote(ctx, sctx.WorkDir, pushURL, ref)
	if err != nil || verifiedRemote != headBeingPushed {
		if err != nil {
			return nil, fmt.Errorf("verify successful push to %s: %w", pushTarget, err)
		}
		return nil, fmt.Errorf("verify successful push to %s: remote head %s does not equal pushed head %s", pushTarget, verifiedRemote, headBeingPushed)
	}
	if err := sctx.DB.UpdateRunPushBinding(sctx.Run.ID, db.PushBinding{
		HeadSHA:           headBeingPushed,
		TargetKind:        pushTarget,
		TargetFingerprint: branchsync.TargetFingerprint(pushURL),
		Ref:               ref,
	}); err != nil {
		return nil, err
	}

	if newHeadSHA != "" {
		if _, err := git.Run(ctx, sctx.WorkDir, "update-ref", sourceRef, newHeadSHA); err != nil {
			return nil, fmt.Errorf("update local branch ref: %w", err)
		}
	}

	// Persist the immutable source that was verified and delivered, never a
	// fresh read of mutable worktree HEAD after the push.
	if headBeingPushed != sctx.Run.HeadSHA {
		sctx.Run.HeadSHA = headBeingPushed
		if err := sctx.DB.UpdateRunHeadSHA(sctx.Run.ID, headBeingPushed); err != nil {
			return nil, err
		}
	}

	if directMain {
		sctx.Log(fmt.Sprintf("pushed directly to default branch %s successfully", branch))
	} else {
		sctx.Log("pushed successfully")
	}
	return &pipeline.StepOutcome{}, nil
}

func assertReviewApprovedPushHead(sctx *pipeline.StepContext, proposedHead string) error {
	run, err := sctx.DB.GetRun(sctx.Run.ID)
	if err != nil {
		return fmt.Errorf("load durable review approval before push: %w", err)
	}
	if run == nil || run.ReviewApprovedHeadSHA == nil || strings.TrimSpace(*run.ReviewApprovedHeadSHA) == "" {
		return fmt.Errorf("refusing to push: run has no durably recorded review-approved head")
	}
	approvedHead := strings.TrimSpace(*run.ReviewApprovedHeadSHA)
	if !isFullGitObjectID(approvedHead) {
		return fmt.Errorf("refusing to push: durable review-approved head is malformed")
	}
	resolved, err := git.Run(sctx.Ctx, sctx.WorkDir, "rev-parse", "--verify", approvedHead+"^{commit}")
	if err != nil || !strings.EqualFold(strings.TrimSpace(resolved), approvedHead) {
		return fmt.Errorf("refusing to push: durable review-approved head is unreachable")
	}
	if proposedHead != approvedHead {
		if _, err := git.Run(sctx.Ctx, sctx.WorkDir, "merge-base", "--is-ancestor", approvedHead, proposedHead); err != nil {
			return fmt.Errorf("refusing to push: proposed head %s violates continuity with review-approved head %s (it is not an equal or descendant commit)", shortObjectID(proposedHead), shortObjectID(approvedHead))
		}
	}
	return nil
}

func isFullGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func shortObjectID(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
