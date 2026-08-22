# ADR: Replace the Autonomous Pipeline with a Stateless Lean Guard

- Status: Proposed
- Scope: `docs/no-mistakes-lean-guard-scope.md`
- Decision owner: repository maintainer

## Context

No-mistakes currently combines deterministic Git policy with an autonomous agent pipeline,
daemon, bare gate repositories, hidden worktrees, SQLite run state, branch custody, review and
fix loops, provider delivery, and CI monitoring. The maintainer already has `ship`, hooks,
GitHub/iCode skills, SCM_FLOW branch creation, CI diagnostics, and deployment skills. Running
another orchestrator underneath those workflows creates duplicated decisions, hidden code
changes, stale local branches, and incompatible completion states.

The product will become a synchronous command-line guard. The old full pipeline is removed,
not deprecated behind a compatibility flag.

## Decision

Retain only four synchronous capabilities:

1. `check`: read-only scope, message, lint, branch, remote and push-plan validation.
2. `commit`: revalidate and create an exact provider-compliant commit.
3. `push`: revalidate and deliver an exact immutable HEAD using the provider-safe ref.
4. `legacy-cleanup`: produce and then explicitly apply a hash-bound cleanup plan.

`ship` remains the orchestration and authorization owner. No-mistakes contains no model
adapter, daemon, managed worktree, run database, background monitor, branch custody, local
sync, automatic rebase, or automatic code modification.

GitHub delivery ends after a normal non-force push. iCode retains the provider-specific CR
tail because `refs/for`, checks, self `+2`, reviewer fallback, submit and `MERGED` are one
idempotent Gerrit delivery transaction. A self-merged result emits an `opera-deploy` handoff;
deployment remains a separate explicitly confirmed skill action.

## Component boundaries

```text
CLI (Cobra + versioned structured output)
  ├── guard: repository/branch/scope/message/lint planning
  ├── commitprep + commitpolicy: exact commit and rollback
  ├── delivery/github: non-force exact-SHA push
  ├── delivery/icode: target proof, refs/for, checks, +2, submit, MERGED
  ├── lintscope: NUL-delimited changed-file manifest
  └── legacycleanup: plan/hash/confirm for old managed state
```

The existing `internal/git`, `internal/safeurl`, commit policy/preparation, provider parsing,
and iCode API behavior are reused after their pipeline/DB dependencies are removed. Agent,
pipeline, daemon, branchsync, run DB, TUI, eval, evidence, and session packages are deleted.

## Command transaction model

Every mutating command is self-contained. A previous `check` is useful evidence but never a
capability token and never authorizes mutation.

### Commit

1. Resolve the exact repository root, attached branch, origin URL and provider.
2. Prove provider branch prerequisites. For iCode, query
   `refs/heads/<SCM_FLOW-branch>` from the remote; missing, auth, network and timeout results
   all block before lint or index mutation.
3. Normalize and validate every `--file`; reject directories, traversal, duplicates,
   sensitive paths and already-staged paths outside the list.
4. Validate the proposed message/author and, for iCode, the executable `commit-msg` hook.
5. Resolve and visibly report the changed-scope lint command. Execute it only under the
   configuration trust decision below.
6. Re-read branch, HEAD, index and explicit file scope after lint. Any new unrelated change
   blocks.
7. Snapshot the index, stage exactly the named files, verify exact equality, commit, and
   roll back HEAD/index on provider postcondition failure.

### Push

1. Re-resolve repository, provider, attached branch, HEAD, message and remote; do not reuse a
   prior check snapshot.
2. Bind the operation to the immutable resolved HEAD SHA and exact target ref.
3. Read the live remote immediately before mutation.
4. GitHub direct-main and feature delivery use a regular push only. A server-side concurrent
   update or non-fast-forward relationship is rejected by Git; there is no force mode.
5. iCode re-proves the target `refs/heads/<branch>`, then pushes
   `<immutable-SHA>:refs/for/<branch>`. It never updates or creates the target branch.
6. Verify provider truth after mutation instead of inferring success from process exit.

This model prevents check-to-commit/push races while remaining synchronous and stateless:
mutable assumptions are sampled again at the mutation boundary, Git index rollback protects
commit failures, regular receive rules protect GitHub refs, and Gerrit review refs avoid
overwriting iCode target history.

## Legacy cleanup safety

Legacy cleanup must prove exact ownership and daemon inactivity before deleting gates,
worktrees, database state or remotes.

`legacy-cleanup --plan` is read-only and builds a canonical inventory from the old
`NM_HOME` layout and database when readable. Every target records its canonical absolute
path, kind, repository identity, and ownership evidence. Eligible paths must be exact
children of the known old managed roots; symlink escapes, unexpected depth, missing metadata,
or ambiguous repositories are blockers rather than cleanup candidates.

The plan also inventories the legacy service/PID/lock and active run rows. Any responsive
daemon, live authenticated PID, pending/running run, or worktree whose run status is unknown
makes the plan non-applicable. The new CLI retains only the minimal platform-specific service
inspection/removal code required for this migration; it does not retain daemon execution.

The canonical JSON inventory is hashed. `--confirm <hash>` repeats discovery from scratch and
requires byte-equivalent canonical inventory before mutation. It stops/removes only a proven
inactive legacy service, removes exact managed gate/worktree/database paths, and removes a
repository's `no-mistakes` remote only when its canonical URL equals the planned owned gate.
Unrelated Git hooks, origin/fork remotes, global hooks, user worktrees, repositories, and
configuration files are never removed. Partial failure is reported per target and is safe to
re-plan; cleanup does not claim transactional deletion across repositories.

No automatic first-run cleanup is allowed. Until the user confirms a plan, legacy state is
left intact, which also preserves rollback to the old binary.

## Stateless iCode idempotency and recovery

iCode checks, reviewer fallback, submit and MERGED observation must remain idempotent and
recoverable without SQLite or a daemon.

The durable identities are provider-owned:

- repository path;
- SCM_FLOW target branch;
- Gerrit `Change-Id` on the immutable commit;
- current commit SHA/patch set;
- iCode review number and status returned by `icode-cli`.

Before pushing, delivery queries provider truth for the same repository, target and
`Change-Id`/head. If the patch set is already visible, it reuses the review instead of
creating a duplicate push. Re-running after interruption repeats read operations and then
idempotent transitions:

- `MERGED` returns success immediately;
- an existing `+2` is not re-scored;
- during an explicitly authorized submit invocation, lack of self-score permission adds
  configured reviewers at most once and
  treats provider duplicate-reviewer responses as already satisfied;
- submit is attempted only when checks/approval make the review submittable;
- pending external review or checks returns a structured non-success/pending result;
- unknown, timeout, auth, and malformed provider responses fail closed.

There is no background continuation. A synchronous command may wait within a bounded timeout;
after timeout or interruption, `$ship` reruns `no-mistakes push`, which reconstructs state
from iCode. `MERGED` is the only terminal delivery success. The deploy handoff contains the
repo, source branch, review URL/number and merge evidence, but no deployment confirmation
token.

## Configuration trust after removing the daemon

Removing the pinned trusted-config loader must not allow a changed or untrusted branch to
execute an arbitrary lint command silently.

The lean guard separates declarative repository defaults from command authorization:

- `.no-mistakes.yaml` may declare `commands.lint`, iCode reviewers/auto-submit and output
  language.
- `check` always renders the exact lint command and its configuration source and never runs
  it. This keeps the plan command strictly read-only.
- `commit` requires `--allow-repo-lint` when the command comes from the candidate checkout.
  `$ship` may pass this flag only after its visible preflight surfaced the exact command as
  the mandatory lint plan. A direct human invocation gets the same explicit boundary.
- A future trusted-config cache is out of scope; no local registration/database is recreated.
- Non-command settings cannot grant extra external-write authority. iCode auto-submit remains
  effective only when the invoking `$ship` action supplies the exact committed HEAD, an
  explicit one-invocation capability, and the canonical policy hash reported by `check`.

`icode-cli` currently exposes `set_review_score` and `submit_review` only by change number,
not by an expected revision/CAS token. Therefore `icode.auto_submit` defaults to false. An
explicit `true` makes the policy eligible but is not authority. The requested +2/submit tail
also requires `--expected-head`, `--allow-icode-submit`, and the exact
`--icode-policy-hash`. It uses immediate revision checks before every write and bound
`MERGED` verification after it, but accepts the provider's unavoidable read-then-write race.
Unknown or changed revisions never report delivered.

This is intentionally explicit rather than pretending a branch-controlled shell command is
safe. Hooks that already run a repository script name that script directly; they may use
`no-mistakes check` for the remaining Git policy checks without authorizing another command.

## Structured output and mixed-version safety

The structured CLI output and ship/hook migration must be versioned so mixed old/new
installations fail closed instead of using the wrong delivery path.

Every machine-readable response begins with:

```text
schema_version: 1
command: check|commit|push|legacy-cleanup
status: passed|blocked|pending|delivered
```

Command-specific records include provider, branch, HEAD, exact target ref, lint plan, commit
SHA, canonical iCode policy/hash, review identity, blockers and a bounded `next_action`. `$ship` requires
`schema_version: 1` and the expected command/status fields. The removed `axi run`, custody,
and pipeline fields are never mapped heuristically. An old binary, unknown schema, missing
field, or legacy blocker produces a hard compatibility error with installation guidance.

The no-mistakes-core Campaign child lands first. The dependent addons child updates `$ship`
and hooks only after the new schema and behavior tests exist, preventing the adapter from
guessing an unfinished interface.

## Existing uncommitted changes

The working branch contains changes for output language, changed-scope lint, managed
worktree cleanup, and iCode missing-target rebase behavior. They are treated as source
material:

- keep the output-language contract in deterministic rendering;
- extract the changed-scope lint manifest into the guard;
- replace periodic managed-worktree cleanup with explicit legacy cleanup;
- replace the rebase regression with branch-check/commit/push regressions;
- delete agent, daemon, rebase and pipeline wrappers that no longer exist.

No prior patch is preserved merely to minimize the diff.

## Failure semantics

- Read-only check failure: `blocked`, no mutation.
- Lint or commit validation failure: `blocked`, HEAD/index restored or untouched.
- GitHub remote changed/non-fast-forward: `blocked`, no force fallback.
- iCode target/auth/check/submit ambiguity: `blocked` or `pending`, never delivered.
- Reviewer fallback: `pending` with review identity and next action.
- Legacy cleanup inventory drift: refuse confirmation and require a new plan.
- Partial cleanup: list succeeded/failed exact targets; rerun plan before any retry.

## Consequences

### Positive

- One orchestrator (`ship`) and one deterministic policy executor.
- No hidden code changes or post-pipeline local synchronization.
- Dramatically smaller runtime/dependency/process surface.
- Provider-specific iCode delivery remains available without a generic AI pipeline.
- Every destructive legacy action is explicit and reviewable.

### Negative

- Breaking CLI release with no full-pipeline compatibility mode.
- No built-in AI review/test/docs safety net; project skills and CI own those checks.
- No background iCode continuation; interrupted delivery is resumed by re-running from
  provider truth.
- Repository lint command execution needs an explicit authorization flag.
- Large deletion requires broad compile/test/doc cleanup.

## Rollback

Before confirmed legacy cleanup, reinstalling the previous binary restores access to the old
state. After cleanup, source repositories and origin refs remain intact, but old gate/run
history is intentionally gone; rollback recreates gates with the old `init` rather than
restoring deleted state. The release notes must make this boundary explicit.

## Verification obligations

- Behavior tests for exact commit rollback, lint scope and config authorization.
- Real temporary Git remotes for direct-main, feature, concurrent-update and non-fast-forward
  push behavior.
- iCode command-fixture tests for existing/new patch set, checks pending/failing, self `+2`,
  reviewer fallback, submit, timeout and already `MERGED` reruns.
- Legacy filesystem/service/repository fixtures proving plan read-only behavior, hash drift
  refusal, active-state refusal, path containment and unrelated-state preservation.
- Mixed-schema `$ship` integration tests in the dependent repository.
- `gofmt`, lint, `go test -race ./...`, build, and behavior-level CLI/e2e checks.
