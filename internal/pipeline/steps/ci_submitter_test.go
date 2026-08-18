package steps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/scm"
)

func TestCIStepUsesProviderSubmitterAfterChecksPass(t *testing.T) {
	t.Parallel()

	dir, baseSHA, headSHA := setupGitRepo(t)
	sctx := newTestContextWithDBRecords(t, &mockAgent{name: "codex"}, dir, baseSHA, headSHA, config.Commands{})
	prURL := "https://console.cloud.baidu-int.com/devops/icode/repos/baidu/inputmethod/v5api/reviews/123/"
	if err := sctx.DB.UpdateRunPRURL(sctx.Run.ID, prURL); err != nil {
		t.Fatal(err)
	}
	sctx.Run.PRURL = &prURL
	host := &submitterTestHost{
		states:       []scm.PRState{scm.PRStateOpen, scm.PRStateMerged},
		submitResult: scm.ReviewSubmission{Submitted: true, Message: "accepted"},
	}
	step := &CIStep{
		hostOverride:         host,
		pollIntervalOverride: time.Millisecond,
		waitForNextPoll:      func(context.Context, time.Duration) error { return nil },
		baseBranchTip:        func(context.Context) (string, bool) { return baseSHA, true },
	}

	outcome, err := step.Execute(sctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if outcome == nil || outcome.NeedsApproval {
		t.Fatalf("Execute() outcome = %+v", outcome)
	}
	if host.submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1", host.submitCalls)
	}
	run, err := sctx.DB.GetRun(sctx.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.PRState == nil || *run.PRState != "merged" {
		t.Fatalf("persisted PR state = %v, want merged", run.PRState)
	}
}

func TestCIStepParksWhenProviderSubmitFails(t *testing.T) {
	t.Parallel()

	dir, baseSHA, headSHA := setupGitRepo(t)
	sctx := newTestContextWithDBRecords(t, &mockAgent{name: "codex"}, dir, baseSHA, headSHA, config.Commands{})
	prURL := "https://console.cloud.baidu-int.com/devops/icode/repos/baidu/inputmethod/v5api/reviews/123/"
	if err := sctx.DB.UpdateRunPRURL(sctx.Run.ID, prURL); err != nil {
		t.Fatal(err)
	}
	sctx.Run.PRURL = &prURL
	host := &submitterTestHost{
		states:    []scm.PRState{scm.PRStateOpen},
		submitErr: errors.New("permission denied"),
	}
	step := &CIStep{
		hostOverride:  host,
		baseBranchTip: func(context.Context) (string, bool) { return baseSHA, true },
	}

	outcome, err := step.Execute(sctx)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if outcome == nil || !outcome.NeedsApproval || !strings.Contains(outcome.Findings, "permission denied") {
		t.Fatalf("Execute() outcome = %+v, want approval gate", outcome)
	}
}

type submitterTestHost struct {
	states       []scm.PRState
	stateCalls   int
	submitResult scm.ReviewSubmission
	submitErr    error
	submitCalls  int
}

func (h *submitterTestHost) Provider() scm.Provider { return scm.ProviderICode }
func (h *submitterTestHost) Capabilities() scm.Capabilities {
	return scm.Capabilities{}
}
func (h *submitterTestHost) Available(context.Context) error { return nil }
func (h *submitterTestHost) FindPR(context.Context, string, string) (*scm.PR, error) {
	return nil, nil
}
func (h *submitterTestHost) CreatePR(context.Context, string, string, scm.PRContent) (*scm.PR, error) {
	return nil, nil
}
func (h *submitterTestHost) UpdatePR(_ context.Context, pr *scm.PR, _ scm.PRContent) (*scm.PR, error) {
	return pr, nil
}
func (h *submitterTestHost) GetPRState(context.Context, *scm.PR) (scm.PRState, error) {
	if len(h.states) == 0 {
		return scm.PRStateOpen, nil
	}
	idx := h.stateCalls
	if idx >= len(h.states) {
		idx = len(h.states) - 1
	}
	h.stateCalls++
	return h.states[idx], nil
}
func (h *submitterTestHost) GetChecks(context.Context, *scm.PR) ([]scm.Check, error) {
	return []scm.Check{{Name: "iPipe", Bucket: scm.CheckBucketPass}}, nil
}
func (h *submitterTestHost) GetMergeableState(context.Context, *scm.PR) (scm.MergeableState, error) {
	return "", scm.ErrUnsupported
}
func (h *submitterTestHost) FetchFailedCheckLogs(context.Context, *scm.PR, string, string, []string) (string, error) {
	return "", scm.ErrUnsupported
}
func (h *submitterTestHost) EnsureSubmitted(context.Context, *scm.PR) (scm.ReviewSubmission, error) {
	h.submitCalls++
	return h.submitResult, h.submitErr
}
