// Package icode implements the synchronous Baidu iCode review transaction.
package icode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/scm"
)

// CmdFactory builds an icode-cli command in the caller's checkout.
type CmdFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// Host talks to Baidu iCode through icode-cli's JSON API.
type Host struct {
	cmd          CmdFactory
	cliAvailable func() bool
	repo         string
	headSHA      string
	reviewers    string
	reviewersSet bool
	submitted    bool
	autoSubmit   bool
}

// Options controls trusted iCode write behavior.
type Options struct {
	Reviewers  string
	AutoSubmit bool
}

// New builds an iCode Host for repo. headSHA is the immutable revision pushed
// by lean delivery and disambiguates reviews on the same target branch.
func New(cmd CmdFactory, cliAvailable func() bool, repo, headSHA string, options ...Options) *Host {
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}
	return &Host{
		cmd:          cmd,
		cliAvailable: cliAvailable,
		repo:         strings.Trim(strings.TrimSpace(repo), "/"),
		headSHA:      strings.TrimSpace(headSHA),
		reviewers:    strings.TrimSpace(opts.Reviewers),
		autoSubmit:   opts.AutoSubmit,
	}
}

// RepoPath extracts the repository path from iCode HTTPS, ssh://, or scp-like
// remotes. The result keeps all namespace segments, e.g. baidu/inputmethod/v5api.
func RepoPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var repoPath string
	if strings.Contains(raw, "://") {
		if parsed, err := url.Parse(raw); err == nil {
			repoPath = parsed.Path
		}
	} else if colon := strings.Index(raw, ":"); colon >= 0 {
		repoPath = raw[colon+1:]
	}
	repoPath = strings.Trim(repoPath, "/")
	return strings.TrimSuffix(repoPath, ".git")
}

func (h *Host) Available(ctx context.Context) error {
	if h.cliAvailable != nil && !h.cliAvailable() {
		return errors.New("icode-cli is not installed")
	}
	if h.cmd == nil {
		return errors.New("icode-cli command runner is unavailable")
	}
	if h.repo == "" {
		return errors.New("iCode repository path is empty")
	}
	var payload json.RawMessage
	if err := h.runAPI(ctx, &payload, "get_submit_settings", "--repo", h.repo, "--check-permission"); err != nil {
		return fmt.Errorf("icode-cli is unavailable or unauthenticated: %w", err)
	}
	return nil
}

type reviewListEnvelope struct {
	Changes []reviewListItem `json:"changes"`
}

type reviewListItem struct {
	Number          json.Number `json:"_number"`
	Branch          string      `json:"branch"`
	CurrentRevision string      `json:"current_revision"`
}

func (h *Host) FindPR(ctx context.Context, branch, _ string) (*scm.PR, error) {
	return h.findReviewByStatus(ctx, branch, "NEW")
}

// FindReview resolves the current revision across open and merged reviews.
// Lean delivery uses provider truth here to resume idempotently without a run
// database and to avoid pushing another patch set after MERGED.
func (h *Host) FindReview(ctx context.Context, branch string) (*scm.PR, error) {
	for _, status := range []string{"NEW", "SUBMITTED", "MERGED"} {
		review, err := h.findReviewByStatus(ctx, branch, status)
		if err != nil {
			return nil, err
		}
		if review != nil {
			return review, nil
		}
	}
	return nil, nil
}

func (h *Host) findReviewByStatus(ctx context.Context, branch, status string) (*scm.PR, error) {
	var data reviewListEnvelope
	if err := h.runAPI(ctx, &data, "get_repo_reviews", "--repo", h.repo, "--status", status); err != nil {
		return nil, err
	}
	branch = strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	for _, change := range data.Changes {
		if h.headSHA != "" && !strings.EqualFold(strings.TrimSpace(change.CurrentRevision), h.headSHA) {
			continue
		}
		if branch != "" && strings.TrimSpace(change.Branch) != "" && change.Branch != branch {
			continue
		}
		number := change.Number.String()
		if number == "" {
			continue
		}
		return &scm.PR{Number: number, URL: ReviewURL(h.repo, number)}, nil
	}
	return nil, nil
}

type reviewInfo struct {
	Number          json.Number `json:"_number"`
	Status          string      `json:"status"`
	CurrentRevision string      `json:"current_revision"`
	Labels          map[string]struct {
		All []struct {
			Value any `json:"value"`
		} `json:"all"`
	} `json:"labels"`
}

func (h *Host) GetPRState(ctx context.Context, pr *scm.PR) (scm.PRState, error) {
	var data reviewInfo
	if err := h.runAPI(ctx, &data, "get_review_info", "--change-number", pr.Number); err != nil {
		return "", err
	}
	return normalizeReviewState(data.Status)
}

// GetBoundPRState reads revision and lifecycle from one provider response and
// refuses to act on a concurrent patch set.
func (h *Host) GetBoundPRState(ctx context.Context, pr *scm.PR, expectedRevision string) (scm.PRState, error) {
	var data reviewInfo
	if err := h.runAPI(ctx, &data, "get_review_info", "--change-number", pr.Number); err != nil {
		return "", err
	}
	if !strings.EqualFold(strings.TrimSpace(data.CurrentRevision), strings.TrimSpace(expectedRevision)) {
		return "", fmt.Errorf("iCode review current revision %q does not match exact HEAD %q", data.CurrentRevision, expectedRevision)
	}
	return normalizeReviewState(data.Status)
}

func normalizeReviewState(status string) (scm.PRState, error) {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "NEW", "OPEN", "SUBMITTED":
		return scm.PRStateOpen, nil
	case "MERGED":
		return scm.PRStateMerged, nil
	case "ABANDONED", "CLOSED":
		return scm.PRStateClosed, nil
	default:
		return "", fmt.Errorf("unrecognized iCode review status %q", status)
	}
}

type machineCheckData struct {
	Style struct {
		Operations []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"operations"`
	} `json:"style"`
	Pipeline struct {
		HasPipelines    bool   `json:"hasPipelines"`
		Status          string `json:"status"`
		ChangePipelines []struct {
			Name        string `json:"pipelineName"`
			Result      string `json:"result"`
			Completed   bool   `json:"completed"`
			Success     bool   `json:"success"`
			PipelineURL string `json:"pipelineUrl"`
		} `json:"changePipelines"`
	} `json:"pipeline"`
}

func (h *Host) GetChecks(ctx context.Context, pr *scm.PR) ([]scm.Check, error) {
	var data machineCheckData
	if err := h.runAPI(ctx, &data, "get_machine_check", "--change-number", pr.Number); err != nil {
		return nil, err
	}
	checks := make([]scm.Check, 0, len(data.Style.Operations)+len(data.Pipeline.ChangePipelines))
	for _, operation := range data.Style.Operations {
		name := strings.TrimSpace(operation.Name)
		if name == "" {
			name = "iCode machine check"
		}
		checks = append(checks, scm.Check{Name: name, Bucket: checkBucket(operation.Status), State: operation.Status})
	}
	for _, item := range data.Pipeline.ChangePipelines {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "iPipe"
		}
		state := strings.TrimSpace(item.Result)
		if state == "" {
			switch {
			case item.Completed && item.Success:
				state = "SUCCESS"
			case item.Completed:
				state = "FAILED"
			default:
				state = data.Pipeline.Status
			}
		}
		checks = append(checks, scm.Check{Name: name, Bucket: checkBucket(state), State: state, Link: item.PipelineURL})
	}
	if data.Pipeline.HasPipelines && len(data.Pipeline.ChangePipelines) == 0 {
		checks = append(checks, scm.Check{Name: "iPipe", Bucket: checkBucket(data.Pipeline.Status), State: data.Pipeline.Status})
	}
	return checks, nil
}

var (
	noScorePermissionPattern = regexp.MustCompile(`(?i)无合入权限|无权限|无法对自己|permission|forbidden|not allowed|denied`)
	pendingSubmitPattern     = regexp.MustCompile(`(?i)not approved|not submittable|missing.*approval|need.*\+2|code-review|label|review-required|未.*\+2|没有.*\+2|评审.*不足|未通过.*评审|持续集成|pipeline|ci|流水线|检查.*失败|未执行|执行失败`)
	duplicateReviewerPattern = regexp.MustCompile(`(?i)already|duplicate|exists|已存在|重复`)
)

// EnsureSubmitted applies the existing iCode delivery policy: try self +2,
// fall back to configured reviewers when self-scoring is unavailable, and
// submit once approvals and CI permit it. Repeated calls are idempotent for one
// Host instance and GetPRState still confirms the terminal MERGED state.
func (h *Host) EnsureSubmitted(ctx context.Context, pr *scm.PR, expectedRevision string) (scm.ReviewSubmission, error) {
	if !h.autoSubmit {
		return scm.ReviewSubmission{Pending: true, Message: "automatic iCode submit is disabled by trusted repository policy"}, nil
	}
	if h.submitted {
		return scm.ReviewSubmission{Submitted: true, Message: "submit already accepted"}, nil
	}
	var info reviewInfo
	if err := h.runAPI(ctx, &info, "get_review_info", "--change-number", pr.Number); err != nil {
		return scm.ReviewSubmission{}, err
	}
	if err := requireExpectedRevision(info, expectedRevision); err != nil {
		return scm.ReviewSubmission{}, err
	}
	if strings.EqualFold(strings.TrimSpace(info.Status), "MERGED") {
		h.submitted = true
		return scm.ReviewSubmission{Submitted: true, Message: "review already merged"}, nil
	}
	maxVote, minVote := codeReviewVoteRange(info)
	if minVote <= -2 {
		return scm.ReviewSubmission{}, errors.New("iCode review has a -2 vote and cannot be submitted")
	}
	if maxVote < 2 {
		err := h.runAPI(ctx, nil, "set_review_score", "--repo", h.repo, "--change-number", pr.Number, "--score", "2")
		if err != nil {
			if !noScorePermissionPattern.MatchString(err.Error()) {
				return scm.ReviewSubmission{}, fmt.Errorf("set iCode +2: %w", err)
			}
			if h.reviewers != "" && !h.reviewersSet {
				if err := h.recheckRevision(ctx, pr, expectedRevision); err != nil {
					return scm.ReviewSubmission{}, err
				}
				if addErr := h.runAPI(ctx, nil, "add_reviewers", "--change-number", pr.Number, "--reviewers", h.reviewers); addErr != nil && !duplicateReviewerPattern.MatchString(addErr.Error()) {
					return scm.ReviewSubmission{}, fmt.Errorf("add iCode reviewers: %w", addErr)
				}
				h.reviewersSet = true
			}
		}
	}

	if err := h.recheckRevision(ctx, pr, expectedRevision); err != nil {
		return scm.ReviewSubmission{}, err
	}
	if err := h.runAPI(ctx, nil, "submit_review", "--repo", h.repo, "--change-number", pr.Number); err != nil {
		if pendingSubmitPattern.MatchString(err.Error()) {
			return scm.ReviewSubmission{Pending: true, Message: err.Error()}, nil
		}
		return scm.ReviewSubmission{}, fmt.Errorf("submit iCode review: %w", err)
	}
	h.submitted = true
	return scm.ReviewSubmission{Submitted: true, Message: "iCode accepted submit"}, nil
}

func requireExpectedRevision(info reviewInfo, expected string) error {
	if !strings.EqualFold(strings.TrimSpace(info.CurrentRevision), strings.TrimSpace(expected)) {
		return fmt.Errorf("iCode review current revision %q does not match exact HEAD %q", info.CurrentRevision, expected)
	}
	return nil
}

func (h *Host) recheckRevision(ctx context.Context, pr *scm.PR, expected string) error {
	var info reviewInfo
	if err := h.runAPI(ctx, &info, "get_review_info", "--change-number", pr.Number); err != nil {
		return err
	}
	return requireExpectedRevision(info, expected)
}

func codeReviewVoteRange(info reviewInfo) (maxVote, minVote int) {
	first := true
	for name, label := range info.Labels {
		normalized := strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(name))
		if normalized != "codereview" {
			continue
		}
		for _, vote := range label.All {
			value := intValue(vote.Value)
			if first || value > maxVote {
				maxVote = value
			}
			if first || value < minVote {
				minVote = value
			}
			first = false
		}
	}
	return maxVote, minVote
}

func intValue(raw any) int {
	switch value := raw.(type) {
	case json.Number:
		n, _ := strconv.Atoi(value.String())
		return n
	case float64:
		return int(value)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(value))
		return n
	default:
		return 0
	}
}

// ReviewURL returns the canonical browser URL for an iCode review.
func ReviewURL(repo, number string) string {
	return "https://console.cloud.baidu-int.com/devops/icode/repos/" + strings.Trim(repo, "/") + "/reviews/" + strings.TrimSpace(number) + "/"
}

func checkBucket(raw string) scm.CheckBucket {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "SUCCESS", "SUCCEEDED", "PASS", "PASSED", "OK":
		return scm.CheckBucketPass
	case "FAIL", "FAILED", "FAILURE", "ERROR":
		return scm.CheckBucketFail
	case "CANCEL", "CANCELED", "CANCELLED", "STOPPED":
		return scm.CheckBucketCancel
	case "SKIP", "SKIPPED", "NOT_REQUIRED":
		return scm.CheckBucketSkip
	default:
		return scm.CheckBucketPending
	}
}

type apiEnvelope struct {
	Status  any             `json:"status"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (h *Host) runAPI(ctx context.Context, dest any, command string, args ...string) error {
	cmdArgs := append([]string{"api", command}, args...)
	cmdArgs = append(cmdArgs, "-o", "json")
	cmd := h.cmd(ctx, "icode-cli", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("icode-cli api %s: %s: %w", command, strings.TrimSpace(string(out)), err)
	}
	trimmed := trimToJSON(out)
	if len(trimmed) == 0 {
		return fmt.Errorf("icode-cli api %s returned no JSON", command)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var envelope apiEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return fmt.Errorf("icode-cli api %s returned invalid JSON: %w", command, err)
	}
	if !apiStatusOK(envelope.Status) {
		return fmt.Errorf("icode-cli api %s failed: %s", command, strings.TrimSpace(envelope.Message))
	}
	if dest == nil || len(envelope.Data) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Data), []byte("null")) {
		return nil
	}
	decoder = json.NewDecoder(bytes.NewReader(envelope.Data))
	decoder.UseNumber()
	if err := decoder.Decode(dest); err != nil {
		return fmt.Errorf("icode-cli api %s returned invalid data: %w", command, err)
	}
	return nil
}

func apiStatusOK(status any) bool {
	switch value := status.(type) {
	case string:
		normalized := strings.ToUpper(strings.TrimSpace(value))
		return normalized == "OK" || normalized == "SUCCESS" || normalized == "0" || normalized == "200"
	case json.Number:
		n, err := strconv.Atoi(value.String())
		return err == nil && (n == 0 || n == 200)
	case float64:
		return value == 0 || value == 200
	default:
		return false
	}
}

func trimToJSON(out []byte) []byte {
	for i, b := range out {
		if b == '{' || b == '[' {
			return out[i:]
		}
	}
	return nil
}
