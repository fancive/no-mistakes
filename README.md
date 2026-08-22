# no-mistakes

`no-mistakes` is a synchronous, stateless guard for the narrow part of shipping that
benefits from deterministic enforcement:

- exact authored-file scope and sensitive-path checks;
- GitHub/iCode commit message, author, and Gerrit `Change-Id` policy;
- one explicitly configured changed-file lint command;
- attached-branch and live remote-target checks;
- regular, non-force exact-SHA GitHub or iCode delivery.

It is not an AI reviewer or an automatic repair loop. It has no daemon, hidden worktree,
run database, rebase/sync engine, TUI, PR creator, or GitHub CI monitor. An outer workflow
such as `$ship` owns intent, authorization, review, tests, CI, merge, and deployment.

## Commands

```sh
no-mistakes check --file internal/example.go --message "feat(core): add guard"
no-mistakes commit --file internal/example.go --message "feat(core): add guard" --allow-repo-lint
no-mistakes push --expected-head <commit-sha>
```

Every command emits versioned TOON. `check` is read-only. `commit` stages exactly the
repeated `--file` values and restores the index/HEAD on failure. `push` re-resolves the
current branch, immutable HEAD, and live remote immediately before a regular push.

For iCode, the SCM_FLOW target branch must already exist. Missing targets block with an
instruction to invoke `$ipipe-pull-branch`; no-mistakes never creates them. The iCode tail
retains checks, self `+2` or reviewer fallback, submit, `MERGED` proof, and an optional
`opera-deploy`/`imeShahe` handoff. Repository configuration alone cannot authorize
reviewer, `+2`, or submit writes.

## Configuration

```yaml
output_language: zh-CN
commands:
  lint: bash scripts/lint-changed.sh
icode:
  auto_submit: true
  reviewers: [alice, bob]
```

`check` prints repository lint without running it. `commit` runs it only with the explicit
`--allow-repo-lint` flag and supplies a NUL-delimited changed-file manifest.

`icode.auto_submit` defaults to `false`. Setting it to `true` only makes that reviewed
policy eligible for a one-invocation authorization. `check` reports `icode_policy_hash`;
the caller must bind both the committed SHA and that policy explicitly:

```sh
no-mistakes push --expected-head <commit-sha> \
  --allow-icode-submit --icode-policy-hash <hash-from-check>
```

Without those flags, iCode may push the review and observe checks but returns pending before
reviewer, `+2`, or submit writes. `icode-cli` has no revision-CAS write, so authorized writes
still use immediate revision checks and require provider `MERGED` proof.

## Legacy cleanup

Older releases may have left daemon, database, gate, or managed-worktree state under
`~/.no-mistakes`. Cleanup is explicit and hash-bound:

```sh
no-mistakes legacy-cleanup --plan
no-mistakes legacy-cleanup --confirm <plan-hash>
```

Confirmation re-inventories everything and refuses active or uncertain state. It preserves
unrelated files, hooks, remotes, configuration, and user worktrees.

Stop the old daemon with the old binary before upgrading; the lean cleaner deliberately
does not kill an active process on inference alone.

Build and verify with `make build`, `make lint`, `make test`, and `make e2e`.

[中文说明](README.zh-CN.md) · [Documentation](https://kunchenguid.github.io/no-mistakes/)
