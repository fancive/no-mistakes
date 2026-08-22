---
title: Repo Config
---

`.no-mistakes.yaml` accepts only:

| Key | Values | Purpose |
|---|---|---|
| `output_language` | `en`, `zh-CN` | User-facing structured messages |
| `commands.lint` | shell command | Repository lint, explicitly authorized at invocation |
| `icode.auto_submit` | boolean, default `false` | Make this policy eligible for explicit push-time authorization |
| `icode.reviewers` | account list | Reviewer set bound into `icode_policy_hash` |

Candidate-checkout configuration is declarative, not an external-write capability. `check`
reports the canonical policy/hash; the caller must separately authorize it on `push`.
