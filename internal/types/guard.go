package types

// GuardSchemaVersion is the machine-readable contract shared by no-mistakes,
// ship, and hooks. Unknown versions must fail closed.
const GuardSchemaVersion = 1

// GuardStatus is the terminal state of one synchronous lean-guard command.
type GuardStatus string

const (
	GuardPassed    GuardStatus = "passed"
	GuardBlocked   GuardStatus = "blocked"
	GuardPending   GuardStatus = "pending"
	GuardDelivered GuardStatus = "delivered"
)

// CheckRequest describes the read-only authored-change checks.
type CheckRequest struct {
	Files   []string
	Message string
}

// CommitRequest describes one exact-scope commit transaction.
type CommitRequest struct {
	Files         []string
	Message       string
	Amend         bool
	AllowRepoLint bool
}

// PushRequest binds an automated caller to the exact HEAD and, when requested,
// to the inspected iCode submit policy. Empty fields retain safe manual push
// behavior: delivery may create/update the review, but it cannot submit it.
type PushRequest struct {
	ExpectedHead          string
	AllowICodeSubmit      bool
	ICodeSubmitPolicyHash string
}

// LegacyCleanupRequest selects the read-only plan or a hash-bound confirm.
type LegacyCleanupRequest struct {
	Plan        bool
	ConfirmHash string
}

// DeployHandoff is provider evidence for a later, separately authorized skill.
type DeployHandoff struct {
	Skill       string
	Environment string
}

// GuardResult is the stable response model rendered by every lean command.
// Fields that do not apply to a command remain empty and are omitted.
type GuardResult struct {
	Status                GuardStatus
	OutputLanguage        string
	ErrorCode             string
	Summary               string
	Provider              string
	Branch                string
	Head                  string
	TargetRef             string
	SHA                   string
	ReviewURL             string
	PlanHash              string
	CommitMessage         string
	CommitAuthor          string
	CommitMsgHook         string
	ChangeIDRequired      bool
	LintCommand           string
	LintFiles             []string
	LintAuthorized        bool
	LintRan               bool
	ICodeAutoSubmit       bool
	ICodeReviewers        []string
	ICodePolicyHash       string
	ICodeSubmitAuthorized bool
	Files                 []string
	Blockers              []string
	CleanupTargets        []string
	NextAction            string
	DeployHandoff         *DeployHandoff
}
