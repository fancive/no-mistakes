# Agent Instructions

This repository implements a synchronous, stateless commit/lint/branch/push guard. Do not
reintroduce an agent loop, daemon, database, gate remote, managed worktree, automatic code
repair, rebase/sync custody, TUI, GitHub PR/CI/merge automation, or silent SCM_FLOW creation.

## Architecture

- `internal/guard`: read-only planning and exact commit orchestration.
- `internal/commitpolicy`, `internal/commitprep`: provider message/author policy, exact
  staging, index/HEAD rollback, and iCode `Change-Id` checks.
- `internal/lintscope`: explicitly configured lint with a NUL-delimited exact-file manifest.
- `internal/delivery`: regular exact-SHA GitHub push and the synchronous iCode transaction.
- `internal/legacycleanup`: read-only inventory plus hash-bound cleanup of proven old state.
- `internal/cli`, `internal/types`: schema-versioned TOON command contract.

## Invariants

- Re-resolve branch, HEAD, literal `origin`, remote default branch, and target at each
  mutation boundary. Network/auth/fetch ambiguity blocks.
- `commit` must stage exactly repeated `--file` values; never add a broad staging fallback.
- Repository lint runs only after the caller explicitly authorizes the exact printed command.
- GitHub push is regular and non-force. `fancive/*` uses direct-main; other owners use the
  attached feature branch and return PR/CI/merge responsibility to the caller.
- iCode requires existing `refs/heads/<SCM_FLOW branch>` before commit and push. Missing
  targets point to `$ipipe-pull-branch`; no-mistakes never creates them.
- iCode delivery accepts only `MERGED` as terminal success. Deployment is a structured
  `opera-deploy` handoff, never an implicit authorization.
- Legacy cleanup never runs automatically. Confirm must recreate the exact plan/hash and
  preserve unrelated hooks, remotes, files, configuration, and user worktrees.

## Development

Write behavior tests with each change. Run `gofmt -w .`, `make lint`, `go test -race ./...`,
`go build ./cmd/no-mistakes`, and `make e2e` before claiming completion.

`skills/no-mistakes/SKILL.md` is generated from `internal/skill/skill.go`; edit the source
and run `make skill`.
