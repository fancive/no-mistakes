---
title: Configuration
---

The candidate checkout may declare one lint command and provider presentation policy.
Because the lint command is repository-controlled, `check` only reports it by default;
execution requires `--allow-repo-lint`.

```yaml
output_language: zh-CN
commands:
  lint: bash scripts/lint-changed.sh
icode:
  auto_submit: true
  reviewers: [alice, bob]
```

Unknown and removed keys are rejected.

Lint is expected to be read-only. If it rewrites authored or tracked content, the guard
blocks rather than silently including that rewrite in the commit.

`icode.auto_submit: true` is eligibility, not authority. `check` canonicalizes the reviewer
set and reports an `icode_policy_hash` bound to repository and branch. Reviewer, `+2`, and
submit writes require a one-invocation `push` capability with both that hash and the exact
committed HEAD. A candidate branch cannot authorize those writes by changing this file.
