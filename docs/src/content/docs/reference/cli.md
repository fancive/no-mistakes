---
title: CLI Commands
---

| Command | Mutation | Contract |
|---|---:|---|
| `check` | No | Scope, commit, lint, branch and target plan |
| `commit` | Yes | Exact file list, optional authorized lint, rollback on failure |
| `push --expected-head SHA` | Yes | Refuse drift, then regular exact-SHA push and provider verification |
| `legacy-cleanup --plan` | No | Canonical owned-state inventory and hash |
| `legacy-cleanup --confirm HASH` | Yes | Revalidated hash-bound cleanup |

All responses use schema version 1. `output_language`, `summary`, and stable `error_code`
make Chinese and English blockers machine-consumable while `blockers` preserves exact
diagnostics. Removed autonomous commands fail with migration guidance.

iCode reviewer, `+2`, and submit writes additionally require
`--allow-icode-submit --icode-policy-hash HASH`. Both the hash and `SHA` must match the
fields from the reviewed plan/current commit; configuration alone is never authority.
