package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/config"
	"github.com/kunchenguid/no-mistakes/internal/git"
	toon "github.com/toon-format/toon-go"

	"github.com/kunchenguid/no-mistakes/internal/buildinfo"
	"github.com/kunchenguid/no-mistakes/internal/types"
	"github.com/spf13/cobra"
)

type leanService interface {
	Check(context.Context, types.CheckRequest) (types.GuardResult, error)
	Commit(context.Context, types.CommitRequest) (types.GuardResult, error)
	Push(context.Context, types.PushRequest) (types.GuardResult, error)
	LegacyCleanup(context.Context, types.LegacyCleanupRequest) (types.GuardResult, error)
}

func newLeanRootCmd(service leanService) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "no-mistakes",
		Short:         "Deterministic commit, lint, branch, and push guard",
		Version:       buildinfo.String(),
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	cmd.AddCommand(newLeanCheckCmd(service))
	cmd.AddCommand(newLeanCommitCmd(service))
	cmd.AddCommand(newLeanPushCmd(service))
	cmd.AddCommand(newLeanLegacyCleanupCmd(service))
	for _, name := range []string{"init", "eject", "daemon", "attach", "rerun", "status", "sync", "runs", "stats", "eval", "axi"} {
		cmd.AddCommand(newRemovedLeanCommand(name))
	}
	return cmd
}

func newLeanCheckCmd(service leanService) *cobra.Command {
	var files []string
	var message string
	cmd := &cobra.Command{
		Use:           "check",
		Short:         "Read-only commit, lint, branch, and push-plan checks",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := service.Check(cmd.Context(), types.CheckRequest{
				Files: append([]string(nil), files...), Message: message,
			})
			return renderLeanResult(cmd, "check", result, err)
		},
	}
	cmd.Flags().StringArrayVar(&files, "file", nil, "repository-relative file to check; repeat for every authored file")
	cmd.Flags().StringVarP(&message, "message", "m", "", "provider-compliant commit message to check")
	return cmd
}

func newLeanCommitCmd(service leanService) *cobra.Command {
	var files []string
	var message string
	var amend bool
	var allowRepoLint bool
	cmd := &cobra.Command{
		Use:           "commit",
		Short:         "Lint and commit an exact file list under provider policy",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(files) == 0 {
				return renderLeanResult(cmd, "commit", types.GuardResult{}, fmt.Errorf("at least one --file is required"))
			}
			if !amend && strings.TrimSpace(message) == "" {
				return renderLeanResult(cmd, "commit", types.GuardResult{}, fmt.Errorf("--message is required"))
			}
			result, err := service.Commit(cmd.Context(), types.CommitRequest{
				Files: append([]string(nil), files...), Message: message, Amend: amend, AllowRepoLint: allowRepoLint,
			})
			return renderLeanResult(cmd, "commit", result, err)
		},
	}
	cmd.Flags().StringArrayVar(&files, "file", nil, "repository-relative file to commit; repeat for every authored file")
	cmd.Flags().StringVarP(&message, "message", "m", "", "complete provider-compliant commit message")
	cmd.Flags().BoolVar(&amend, "amend", false, "amend the current commit under the same exact-scope rules")
	cmd.Flags().BoolVar(&allowRepoLint, "allow-repo-lint", false, "authorize the exact repository lint command printed by check")
	return cmd
}

func newLeanPushCmd(service leanService) *cobra.Command {
	var expectedHead string
	var allowICodeSubmit bool
	var icodePolicyHash string
	cmd := &cobra.Command{
		Use:           "push",
		Short:         "Safely push the exact current HEAD using provider policy",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, err := service.Push(cmd.Context(), types.PushRequest{
				ExpectedHead: expectedHead, AllowICodeSubmit: allowICodeSubmit, ICodeSubmitPolicyHash: icodePolicyHash,
			})
			return renderLeanResult(cmd, "push", result, err)
		},
	}
	cmd.Flags().StringVar(&expectedHead, "expected-head", "", "refuse to push unless current HEAD equals this full commit SHA")
	cmd.Flags().BoolVar(&allowICodeSubmit, "allow-icode-submit", false, "explicitly authorize configured iCode reviewer, +2, and submit writes")
	cmd.Flags().StringVar(&icodePolicyHash, "icode-policy-hash", "", "exact iCode policy hash reported by no-mistakes check")
	return cmd
}

func newLeanLegacyCleanupCmd(service leanService) *cobra.Command {
	var plan bool
	var confirmHash string
	cmd := &cobra.Command{
		Use:           "legacy-cleanup",
		Short:         "Plan or confirm cleanup of proven legacy managed state",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if plan == (strings.TrimSpace(confirmHash) != "") {
				return renderLeanResult(cmd, "legacy-cleanup", types.GuardResult{}, fmt.Errorf("choose exactly one of --plan or --confirm <plan-hash>"))
			}
			result, err := service.LegacyCleanup(cmd.Context(), types.LegacyCleanupRequest{Plan: plan, ConfirmHash: confirmHash})
			return renderLeanResult(cmd, "legacy-cleanup", result, err)
		},
	}
	cmd.Flags().BoolVar(&plan, "plan", false, "print a read-only canonical cleanup inventory")
	cmd.Flags().StringVar(&confirmHash, "confirm", "", "apply only the freshly revalidated cleanup plan with this hash")
	return cmd
}

func newRemovedLeanCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              "Removed autonomous-pipeline command",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return renderLeanResult(cmd, name, types.GuardResult{}, fmt.Errorf("command %q was removed by the stateless lean-guard migration", name))
		},
	}
}

func renderLeanResult(cmd *cobra.Command, command string, result types.GuardResult, err error) error {
	if result.OutputLanguage == "" {
		result.OutputLanguage = currentOutputLanguage()
	}
	if err != nil {
		result.Status = types.GuardBlocked
		result.ErrorCode = classifyLeanError(err.Error())
		result.Summary = leanErrorSummary(result.OutputLanguage, result.ErrorCode)
		result.Blockers = append(result.Blockers, err.Error())
		if result.NextAction == "" {
			if result.OutputLanguage == "zh-CN" {
				result.NextAction = "运行 no-mistakes check，并按 blockers 处理"
			} else {
				result.NextAction = "run no-mistakes check and follow its blockers"
			}
		}
	}
	if result.Status == "" {
		result.Status = types.GuardPassed
	}
	if result.Status == types.GuardBlocked && result.ErrorCode == "" {
		if command == "legacy-cleanup" {
			result.ErrorCode = "legacy_cleanup_blocked"
		} else {
			result.ErrorCode = "operation_blocked"
		}
	}
	if result.Summary == "" {
		if result.Status == types.GuardBlocked {
			result.Summary = leanErrorSummary(result.OutputLanguage, result.ErrorCode)
		} else {
			result.Summary = leanSuccessSummary(result.OutputLanguage, command, result.Status)
		}
	}
	fields := []toon.Field{
		{Key: "schema_version", Value: types.GuardSchemaVersion},
		{Key: "command", Value: command},
		{Key: "status", Value: string(result.Status)},
	}
	fields = appendLeanField(fields, "output_language", result.OutputLanguage)
	fields = appendLeanField(fields, "error_code", result.ErrorCode)
	fields = appendLeanField(fields, "summary", result.Summary)
	fields = appendLeanField(fields, "provider", result.Provider)
	fields = appendLeanField(fields, "branch", result.Branch)
	fields = appendLeanField(fields, "head", result.Head)
	fields = appendLeanField(fields, "target_ref", result.TargetRef)
	fields = appendLeanField(fields, "sha", result.SHA)
	fields = appendLeanField(fields, "review_url", result.ReviewURL)
	fields = appendLeanField(fields, "plan_hash", result.PlanHash)
	fields = appendLeanField(fields, "commit_message", result.CommitMessage)
	fields = appendLeanField(fields, "commit_author", result.CommitAuthor)
	fields = appendLeanField(fields, "commit_msg_hook", result.CommitMsgHook)
	if result.ChangeIDRequired {
		fields = append(fields, toon.Field{Key: "change_id_required", Value: true})
	}
	fields = appendLeanField(fields, "lint_command", result.LintCommand)
	if len(result.LintFiles) > 0 {
		fields = append(fields, toon.Field{Key: "lint_files", Value: result.LintFiles})
	}
	if result.LintCommand != "" {
		fields = append(fields,
			toon.Field{Key: "lint_authorized", Value: result.LintAuthorized},
			toon.Field{Key: "lint_ran", Value: result.LintRan},
		)
	}
	if result.Provider == "icode" || result.ICodePolicyHash != "" {
		fields = append(fields,
			toon.Field{Key: "icode_auto_submit", Value: result.ICodeAutoSubmit},
			toon.Field{Key: "icode_submit_authorized", Value: result.ICodeSubmitAuthorized},
		)
		if len(result.ICodeReviewers) > 0 {
			fields = append(fields, toon.Field{Key: "icode_reviewers", Value: result.ICodeReviewers})
		}
		fields = appendLeanField(fields, "icode_policy_hash", result.ICodePolicyHash)
	}
	if len(result.Files) > 0 {
		fields = append(fields, toon.Field{Key: "files", Value: result.Files})
	}
	if len(result.Blockers) > 0 {
		fields = append(fields, toon.Field{Key: "blockers", Value: result.Blockers})
	}
	if len(result.CleanupTargets) > 0 {
		fields = append(fields, toon.Field{Key: "cleanup_targets", Value: result.CleanupTargets})
	}
	if result.DeployHandoff != nil {
		fields = append(fields, toon.Field{Key: "deploy_handoff", Value: toon.NewObject(
			toon.Field{Key: "skill", Value: result.DeployHandoff.Skill},
			toon.Field{Key: "environment", Value: result.DeployHandoff.Environment},
		)})
	}
	fields = appendLeanField(fields, "next_action", result.NextAction)
	emitLeanDoc(cmd, fields...)
	if err != nil || result.Status == types.GuardBlocked {
		return &exitError{code: 1}
	}
	return nil
}

func currentOutputLanguage() string {
	root, err := git.FindGitRoot(".")
	if err != nil {
		return "en"
	}
	return config.DetectOutputLanguage(root)
}

func classifyLeanError(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "non-fast-forward"):
		return "non_fast_forward"
	case strings.Contains(lower, "remote"), strings.Contains(lower, "origin"), strings.Contains(lower, "refs/heads"), strings.Contains(lower, "push url"):
		return "remote_unverified"
	case strings.Contains(lower, "branch"), strings.Contains(lower, "detached"):
		return "branch_policy"
	case strings.Contains(lower, "lint"):
		return "lint_failed"
	case strings.Contains(lower, "commit"), strings.Contains(lower, "change-id"), strings.Contains(lower, "message"), strings.Contains(lower, "staged"):
		return "commit_policy"
	case strings.Contains(lower, "cleanup"), strings.Contains(lower, "daemon"), strings.Contains(lower, "worktree"):
		return "legacy_cleanup_blocked"
	default:
		return "operation_blocked"
	}
}

func leanErrorSummary(language, code string) string {
	if language != "zh-CN" {
		return "operation blocked; inspect blockers"
	}
	switch code {
	case "non_fast_forward":
		return "远端存在非快进更新，已拒绝推送"
	case "remote_unverified":
		return "无法验证远端目标，操作已阻塞"
	case "branch_policy":
		return "当前分支不符合交付规则"
	case "lint_failed":
		return "Lint 未通过，未创建提交"
	case "commit_policy":
		return "提交范围或提交信息不符合规则"
	case "legacy_cleanup_blocked":
		return "旧状态清理条件未满足"
	default:
		return "操作已阻塞，请查看 blockers"
	}
}

func leanSuccessSummary(language, command string, status types.GuardStatus) string {
	if language != "zh-CN" {
		return command + " " + string(status)
	}
	switch {
	case command == "check":
		return "检查通过"
	case command == "commit":
		return "精确范围提交已创建"
	case command == "push" && status == types.GuardPending:
		return "交付仍在等待平台条件"
	case command == "push":
		return "安全推送已完成"
	case command == "legacy-cleanup":
		return "旧状态清理操作已完成"
	default:
		return "操作完成"
	}
}

func emitLeanDoc(cmd *cobra.Command, fields ...toon.Field) {
	out, err := toon.MarshalString(toon.NewObject(fields...))
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "schema_version: %d\nstatus: blocked\nblockers[1]: encode output: %s\n", types.GuardSchemaVersion, err)
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), out)
}

func appendLeanField(fields []toon.Field, key, value string) []toon.Field {
	if strings.TrimSpace(value) == "" {
		return fields
	}
	return append(fields, toon.Field{Key: key, Value: value})
}
