# No-mistakes Lean Guard Scope

## Status

Confirmed scope for replacing the existing autonomous pipeline with a synchronous,
deterministic shipping guard. This is a breaking product change; the legacy full pipeline
will be removed rather than retained behind a compatibility mode.

## Problem

The current product duplicates the user's established `ship`, hook, GitHub, iCode,
`ipipe-pull-branch`, CI, and deployment skills. It also introduces a second orchestrator:
daemon-managed bare gates, detached worktrees, AI review/fix loops, a SQLite run state,
branch custody, and post-run synchronization. That duplication makes intent drift and
workflow conflicts more likely than the defects the extra machinery is intended to prevent.

The replacement must keep the deterministic policies that are genuinely useful while
returning orchestration and code authoring to the main agent context.

## Goals

- Run synchronously in the caller's current checkout with no daemon, hidden worktree, or
  persistent run database.
- Validate an exact authored file list, reject sensitive paths and unrelated staged files,
  and enforce provider-specific author, commit-message, and Gerrit `Change-Id` rules.
- Execute only an explicitly configured changed-scope lint command; never ask an agent to
  discover, fix, document, review, or test code.
- Validate the attached branch, authoritative remote target, and provider-specific push
  route before committing or pushing.
- Execute deterministic commit and push commands only after the caller (`ship`) has obtained
  the required authorization.
- Keep GitHub delivery limited to a safe branch push: direct-main for the configured personal
  owner except conventional `origin=fork, upstream=parent` checkouts, and feature-branch
  push for forks and other owners. PR creation, CI monitoring, and merge remain outside
  no-mistakes.
- Keep the iCode delivery tail: verify an existing SCM_FLOW target, push the immutable commit
  to `refs/for/<branch>`, and observe machine checks/iPipe. With `icode.auto_submit: true`
  plus an explicit one-invocation capability bound to the exact HEAD and canonical policy
  hash, try self `+2`, submit, and prove `MERGED`; otherwise return pending before those
  writes. If self `+2` is unavailable in an authorized invocation, add configured reviewers
  and return a pending state.
- After an iCode self-merge, emit a structured `opera-deploy` handoff; do not execute or
  silently authorize the sandbox deployment.
- Preserve deterministic Chinese output support and the changed-file lint manifest contract.
- Offer an explicit, inspectable cleanup path for state created by older releases.

## Non-goals

- Intent inference from agent transcripts.
- AI review, testing, documentation, lint discovery, or automatic code repair.
- Automatic rebase, merge, stash, reset, force push, or SCM_FLOW branch creation.
- GitHub PR creation, review, CI monitoring, or merge.
- Reimplementing `ipipe-pull-branch`, `ci-pipeline`, GitHub skills, or `opera-deploy`.
- Retaining the old pipeline as `full`, `legacy`, or an opt-in compatibility mode.

## User-facing command contract

### `no-mistakes check`

Read-only. Resolve the current repository/provider and report a structured plan covering:

- exact file scope and sensitive-path findings;
- proposed commit message, author, and iCode hook/`Change-Id` requirements;
- configured changed-scope lint command and its manifest inputs (reported only; execution
  belongs to explicitly authorized `commit`);
- attached branch, remote target existence and freshness;
- the exact push destination (`refs/heads/<branch>` or `refs/for/<branch>`);
- blockers, with no degraded success on network/auth/fetch failures.

### `no-mistakes commit --file ... --message ...`

Re-run the applicable branch, scope, message, and lint checks, then use the existing
index-snapshot/rollback implementation to stage and commit exactly the named files. A failed
lint or changed assumption leaves HEAD and the index unchanged.

### `no-mistakes push [--expected-head SHA]`

Re-resolve HEAD, provider, branch, remote and target immediately before mutation. Push only
the exact verified commit. Literal remotes remain policy inputs; default-port GitHub SSH
traffic is bound to the same repository through `ssh.github.com:443`, while explicit custom
ports are left unchanged:

| Provider/mode | Push behavior | Tail behavior |
|---|---|---|
| GitHub direct-main | regular fast-forward push to the resolved default branch | return delivered SHA |
| GitHub fork or feature | regular non-force push to the same feature branch | return branch handoff for `ship` |
| iCode | regular push of immutable SHA to `refs/for/<SCM_FLOW branch>` | observe CR/checks; with config eligibility plus exact HEAD/policy capability, self `+2`/reviewers, submit, prove `MERGED`, emit deploy handoff |

Any non-fast-forward GitHub update blocks. Version 1 of the lean guard has no force-push mode.
A GitHub feature branch that tracks a differently named `origin/*` ref also blocks; the
caller must align the local/tracking branch names instead of letting the guard guess a PR target.
The iCode target `refs/heads/<branch>` must exist before commit and again before push; a local
branch name or tracking ref is not evidence.

### `no-mistakes legacy-cleanup --plan|--confirm <plan-hash>`

`--plan` inventories only state whose ownership can be proved from old no-mistakes metadata:
daemon service, bare gates, run worktrees, SQLite state, and registered `no-mistakes` remotes.
It emits exact paths/repos plus a canonical hash and changes nothing. `--confirm` revalidates
the same inventory/hash, refuses active or uncertain legacy runs, and removes only the proven
targets. Unrelated hooks, remotes, files, and user worktrees are preserved.

## Configuration

- Repository configuration keeps only the lint command and iCode delivery eligibility policy needed by
  the guard (`commands.lint`, reviewers, auto-submit) plus output language.
- Code-executing configuration must retain a trust boundary after the daemon/default-branch
  loader is deleted. The ADR must choose a bootstrap and changed-config policy that does not
  execute an untrusted branch's shell command silently.
- Agent, auto-fix, intent, test-evidence, document, CI repair, session, worktree, daemon, and
  eval configuration is removed.

## Existing code to reuse

- `internal/commitpolicy` and `internal/commitprep`: exact staging, message/author policy,
  index rollback, Gerrit hook verification.
- `internal/git`, `internal/safeurl`, and provider detection in `internal/scm`.
- The NUL-delimited changed-file manifest contract currently implemented by
  `internal/pipeline/steps/lint_scope.go`, extracted into a pipeline-independent package.
- `internal/scm/icode`: review lookup, status, self `+2`, reviewer fallback, and submit
  semantics, extracted behind a synchronous delivery service.
- Direct-main and remote-clobber checks from the existing push step, without its DB,
  review-binding, worktree, auto-format, auto-commit, or force-push dependencies.

## Code to remove

- `internal/agent`, autonomous `internal/pipeline`, `internal/daemon`, `internal/branchsync`,
  run/eval/statistics database code, TUI, evidence publication, session reuse, and their CLI
  commands/tests/docs.
- `init`, `eject`, `attach`, `rerun`, `status`, `sync`, `runs`, `stats`, `eval`, daemon, and
  AXI run/respond/log/abort surfaces. The useful exact commit operation moves to the lean
  top-level command surface.
- Dependencies that become unreachable, including Bubble Tea UI and SQLite packages.

## Options considered

1. **Extract the deterministic core, then delete the autonomous subsystems (recommended).**
   Reuses mature Git/commit/iCode safety code and its behavioral tests while establishing a
   new synchronous service boundary. Large deletion, but lower correctness risk than a full
   rewrite.
2. **Rewrite a new minimal binary from scratch.** Produces the cleanest initial layout, but
   risks regressing subtle commit rollback, Gerrit, remote-clobber, and iCode idempotency
   behavior already covered by tests.
3. **Keep the current pipeline behind an explicit `full` mode.** Easiest migration, but
   rejected because daemon/worktree/custody/agent complexity and maintenance remain.

## Recommended architecture

Keep Cobra and the small deterministic support packages. Add a synchronous guard layer that
owns immutable `CheckPlan`, `CommitResult`, `PushResult`, and `ICodeDeliveryResult` values.
CLI commands render those values as stable structured output. Provider delivery is composed
from small services rather than the generic pipeline `StepContext`/DB. The main `ship` skill
remains the only orchestrator.

No operation may rely on a previous `check` result without revalidating mutable assumptions.
Commit uses index rollback; push binds to an exact HEAD and remote observation; cleanup binds
to an exact hashed inventory.

## Migration and compatibility

- This release intentionally removes old commands; invocations fail with concise migration
  guidance pointing to `check`, `commit`, `push`, or `legacy-cleanup`.
- A running legacy daemon must be stopped and proven inactive before cleanup. Unknown or
  active custody state blocks deletion rather than being guessed safe.
- Legacy worktree-cleanup and Chinese-output patches currently uncommitted in this checkout
  are not carried mechanically: output language and exact cleanup planning survive in the
  new services; daemon/worktree-specific implementations are deleted.
- The iCode missing-target regression survives as a branch-check behavior test, not a rebase
  test. Fetch failure remains fatal and can never become an empty diff.

## Acceptance criteria

- The binary has no daemon process, SQLite runtime, managed gate, managed worktree, agent
  invocation, branch custody, or sync command.
- `check` is read-only and reports exact provider, branch, lint and push plans in Chinese when
  configured.
- `commit` refuses extra staged/sensitive files, invalid messages/authors/hooks, missing iCode
  targets, and failed lint without changing HEAD or the index.
- `push` refuses detached branches, absent/remotely unverifiable targets, stale/non-fast-forward
  GitHub updates, missing iCode `Change-Id`, and unknown delivery results.
- GitHub push never creates or merges a PR and never monitors CI.
- iCode push verifies the CR and checks; explicitly capability-bound auto-submit handles self
  `+2` or reviewer fallback, submits only after checks, proves `MERGED`, and emits but does
  not execute the deployment handoff. Candidate config alone never authorizes those writes.
- Legacy cleanup requires a matching plan hash and leaves unrelated repositories and files
  untouched.
- The removed packages and dependencies are absent; `go test -race ./...`, lint, build, and
  behavior-level CLI/e2e tests pass.

## Implementation tasks

1. Define and behavior-test the lean CLI/result schemas and migration errors.
2. Extract synchronous scope/message/lint/branch checks and wire exact commit execution.
3. Extract safe GitHub/iCode push plus the iCode submit/MERGED/deploy-handoff tail.
4. Implement hashed, explicit legacy cleanup with active-state and ownership safeguards.
5. Remove autonomous subsystems, obsolete configuration/dependencies/docs, regenerate the
   user skill, and finish CLI/e2e migration tests.

## Design risks requiring an ADR

- Proving exact ownership and inactivity before destructive legacy cleanup.
- Preserving iCode delivery idempotency and recoverability without SQLite or a daemon.
- Preventing check-to-commit/push races while remaining synchronous and stateless.
- Replacing the trusted-default-branch command loader without executing changed or untrusted
  lint configuration silently.
- Versioning structured output so `ship` and hooks fail closed during the cross-repository
  migration.
