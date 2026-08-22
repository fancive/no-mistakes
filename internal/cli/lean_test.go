package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/types"
)

type fakeLeanService struct {
	checkResult   types.GuardResult
	commitResult  types.GuardResult
	pushResult    types.GuardResult
	cleanupResult types.GuardResult
	err           error
	calls         []string
	checkRequest  types.CheckRequest
	commitRequest types.CommitRequest
	pushRequest   types.PushRequest
	cleanup       types.LegacyCleanupRequest
}

func (f *fakeLeanService) Check(_ context.Context, request types.CheckRequest) (types.GuardResult, error) {
	f.calls = append(f.calls, "check")
	f.checkRequest = request
	return f.checkResult, f.err
}

func (f *fakeLeanService) Commit(_ context.Context, request types.CommitRequest) (types.GuardResult, error) {
	f.calls = append(f.calls, "commit")
	f.commitRequest = request
	return f.commitResult, f.err
}

func (f *fakeLeanService) Push(_ context.Context, request types.PushRequest) (types.GuardResult, error) {
	f.calls = append(f.calls, "push")
	f.pushRequest = request
	return f.pushResult, f.err
}

func (f *fakeLeanService) LegacyCleanup(_ context.Context, request types.LegacyCleanupRequest) (types.GuardResult, error) {
	f.calls = append(f.calls, "legacy-cleanup")
	f.cleanup = request
	return f.cleanupResult, f.err
}

func executeLean(t *testing.T, service leanService, args ...string) (string, error) {
	t.Helper()
	cmd := newLeanRootCmd(service)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return output.String(), err
}

func assertLeanFields(t *testing.T, output string, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if !strings.Contains(output, field) {
			t.Errorf("output missing %q:\n%s", field, output)
		}
	}
}

func TestLeanCheckRendersVersionedReadOnlyPlan(t *testing.T) {
	service := &fakeLeanService{checkResult: types.GuardResult{
		Status:         types.GuardPassed,
		OutputLanguage: "zh-CN",
		Provider:       "github",
		Branch:         "feature",
		Head:           "abc123",
		TargetRef:      "refs/heads/feature",
		CommitMessage:  "feat(cli): lean guard",
		CommitAuthor:   "fancivez <fancive@gmail.com>",
		Files:          []string{"a.go", "b.go"},
		NextAction:     "运行 no-mistakes commit",
	}}

	output, err := executeLean(t, service, "check", "--file", "a.go", "--file", "b.go", "--message", "feat(cli): lean guard")
	if err != nil {
		t.Fatalf("check returned error: %v\n%s", err, output)
	}
	assertLeanFields(t, output,
		"schema_version: 1", "command: check", "status: passed", "provider: github",
		"output_language: zh-CN", "branch: feature", "head: abc123", "target_ref: refs/heads/feature",
		"commit_message: \"feat(cli): lean guard\"", "commit_author: fancivez <fancive@gmail.com>", "next_action: 运行 no-mistakes commit",
	)
	if got := strings.Join(service.checkRequest.Files, ","); got != "a.go,b.go" || service.checkRequest.Message != "feat(cli): lean guard" {
		t.Fatalf("check request = %+v", service.checkRequest)
	}
}

func TestLeanCommitRendersCommittedSHAAndPassesAuthorization(t *testing.T) {
	service := &fakeLeanService{commitResult: types.GuardResult{
		Status:           types.GuardPassed,
		Provider:         "icode",
		Branch:           "server_BRANCH",
		Head:             "before",
		SHA:              "deadbeef",
		Files:            []string{"main.go"},
		CommitMessage:    "IMInput-1 [Task] 精简守卫",
		CommitAuthor:     "local_git_config",
		CommitMsgHook:    "verified",
		ChangeIDRequired: true,
	}}

	output, err := executeLean(t, service, "commit", "--file", "main.go", "--message", "IMInput-1 [Task] 精简守卫", "--allow-repo-lint")
	if err != nil {
		t.Fatalf("commit returned error: %v\n%s", err, output)
	}
	assertLeanFields(t, output, "schema_version: 1", "command: commit", "status: passed", "sha: deadbeef",
		"commit_author: local_git_config", "commit_msg_hook: verified", "change_id_required: true")
	if !service.commitRequest.AllowRepoLint || len(service.commitRequest.Files) != 1 {
		t.Fatalf("commit request = %+v", service.commitRequest)
	}
}

func TestLeanPushRendersICodePendingAndDeployHandoff(t *testing.T) {
	service := &fakeLeanService{pushResult: types.GuardResult{
		Status:          types.GuardPending,
		Provider:        "icode",
		Branch:          "server_BRANCH",
		Head:            "deadbeef",
		TargetRef:       "refs/for/server_BRANCH",
		ReviewURL:       "https://icode.example/review/7",
		ICodeAutoSubmit: true,
		ICodeReviewers:  []string{"alice", "bob"},
		ICodePolicyHash: "sha256:policy",
		DeployHandoff: &types.DeployHandoff{
			Skill:       "opera-deploy",
			Environment: "imeShahe",
		},
	}}

	output, err := executeLean(t, service, "push")
	if err != nil {
		t.Fatalf("push returned error: %v\n%s", err, output)
	}
	assertLeanFields(t, output,
		"schema_version: 1", "command: push", "status: pending", "target_ref: refs/for/server_BRANCH",
		`review_url: "https://icode.example/review/7"`, "icode_auto_submit: true", "icode_submit_authorized: false",
		"icode_reviewers[2]:", `icode_policy_hash: "sha256:policy"`, "skill: opera-deploy", "environment: imeShahe",
	)
}

func TestLeanPushPassesExactHeadAndExplicitICodeSubmitCapability(t *testing.T) {
	service := &fakeLeanService{pushResult: types.GuardResult{Status: types.GuardPending}}
	output, err := executeLean(t, service, "push", "--expected-head", "abc123", "--allow-icode-submit", "--icode-policy-hash", "policy123")
	if err != nil {
		t.Fatalf("push returned error: %v\n%s", err, output)
	}
	if service.pushRequest.ExpectedHead != "abc123" || !service.pushRequest.AllowICodeSubmit || service.pushRequest.ICodeSubmitPolicyHash != "policy123" {
		t.Fatalf("push request = %+v", service.pushRequest)
	}
}

func TestLeanLegacyCleanupRequiresOneExplicitMode(t *testing.T) {
	service := &fakeLeanService{cleanupResult: types.GuardResult{
		Status:         types.GuardPassed,
		PlanHash:       "plan123",
		CleanupTargets: []string{"worktree:/tmp/owned"},
	}}

	output, err := executeLean(t, service, "legacy-cleanup", "--plan")
	if err != nil {
		t.Fatalf("cleanup plan returned error: %v\n%s", err, output)
	}
	assertLeanFields(t, output, "command: legacy-cleanup", "status: passed", "plan_hash: plan123", "worktree:/tmp/owned")
	if !service.cleanup.Plan {
		t.Fatalf("cleanup request = %+v", service.cleanup)
	}

	output, err = executeLean(t, service, "legacy-cleanup")
	if err == nil {
		t.Fatalf("cleanup without mode succeeded:\n%s", output)
	}
	assertLeanFields(t, output, "status: blocked", "choose exactly one of --plan or --confirm")
}

func TestLeanLegacyCleanupBlockersUseBlockedChineseSummary(t *testing.T) {
	service := &fakeLeanService{cleanupResult: types.GuardResult{
		Status: types.GuardBlocked, OutputLanguage: "zh-CN", Blockers: []string{"legacy daemon is active"},
	}}
	output, err := executeLean(t, service, "legacy-cleanup", "--plan")
	if err == nil {
		t.Fatalf("blocked plan returned success:\n%s", output)
	}
	assertLeanFields(t, output, "status: blocked", "error_code: legacy_cleanup_blocked", "旧状态清理条件未满足")
	if strings.Contains(output, "清理操作已完成") {
		t.Fatalf("blocked cleanup claims completion:\n%s", output)
	}
}

func TestLeanServiceErrorsAreStructuredBlockers(t *testing.T) {
	commands := [][]string{
		{"check"},
		{"commit", "--file", "x", "--message", "feat: x"},
		{"push"},
		{"legacy-cleanup", "--plan"},
	}
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			service := &fakeLeanService{err: errors.New("remote unavailable"), checkResult: types.GuardResult{OutputLanguage: "zh-CN"}, commitResult: types.GuardResult{OutputLanguage: "zh-CN"}, pushResult: types.GuardResult{OutputLanguage: "zh-CN"}, cleanupResult: types.GuardResult{OutputLanguage: "zh-CN"}}
			output, err := executeLean(t, service, args...)
			if err == nil {
				t.Fatalf("%s succeeded:\n%s", args[0], output)
			}
			assertLeanFields(t, output, "schema_version: 1", "command: "+args[0], "status: blocked", "output_language: zh-CN", "error_code: remote_unverified", "无法验证远端目标", "remote unavailable")
		})
	}
}

func TestLeanLintFailureCannotBeMisclassifiedByCapturedTestOutput(t *testing.T) {
	service := &fakeLeanService{
		err:          errors.New("repository lint failed with exit code 2: test output mentions origin and refs/heads/main"),
		commitResult: types.GuardResult{OutputLanguage: "zh-CN"},
	}
	output, err := executeLean(t, service, "commit", "--file", "x", "--message", "feat: x")
	if err == nil {
		t.Fatalf("lint failure succeeded:\n%s", output)
	}
	assertLeanFields(t, output,
		"schema_version: 1", "command: commit", "status: blocked",
		"error_code: lint_failed", "Lint 未通过，未创建提交",
	)
	if strings.Contains(output, "error_code: remote_unverified") {
		t.Fatalf("captured test output changed lint classification:\n%s", output)
	}
}

func TestLeanRemovedCommandsReturnMigrationGuidanceWithoutCallingService(t *testing.T) {
	for _, name := range []string{"init", "eject", "daemon", "attach", "rerun", "status", "sync", "runs", "stats", "eval", "axi"} {
		t.Run(name, func(t *testing.T) {
			service := &fakeLeanService{}
			output, err := executeLean(t, service, name)
			if err == nil {
				t.Fatalf("removed command %s succeeded:\n%s", name, output)
			}
			assertLeanFields(t, output,
				"schema_version: 1", "command: "+name, "status: blocked",
				"removed by the stateless lean-guard migration", "no-mistakes check",
			)
			if len(service.calls) != 0 {
				t.Fatalf("removed command called service: %v", service.calls)
			}
		})
	}
}
