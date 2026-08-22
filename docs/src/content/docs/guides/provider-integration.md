---
title: Provider Integration
---

For `fancive/*` GitHub repositories, `push` sends the exact HEAD to the resolved default
branch with a regular fast-forward push. Other GitHub repositories receive only a regular
feature-branch push; PR, CI, review, and merge remain with the caller.

iCode requires an existing `refs/heads/<SCM_FLOW branch>` and a valid Gerrit
`Change-Id`. It pushes the immutable SHA to `refs/for/<branch>`, observes checks, tries
self `+2`, falls back to configured reviewers, submits, and accepts only `MERGED` as
delivered only when `icode.auto_submit: true` is paired with `--expected-head`,
`--allow-icode-submit`, and the exact `--icode-policy-hash` reported by `check`. Configuration
alone returns pending before those writes because `icode-cli` exposes change-number writes
but no revision-CAS flag. Missing targets instruct the caller to run `$ipipe-pull-branch`; no-mistakes
never creates SCM_FLOW branches. Non-main merge results include an `opera-deploy`
`imeShahe` handoff, not deployment authorization.
