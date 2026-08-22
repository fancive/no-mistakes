---
title: Troubleshooting
---

- Missing iCode target: run `$ipipe-pull-branch` explicitly, then rerun `check`.
- Lint not run: inspect `lint_command`, then pass `--allow-repo-lint` if authorized.
- Non-fast-forward: update the checkout through the owning workflow; force push is absent.
- `init`, `daemon`, `axi`, or `sync` errors: these autonomous-pipeline commands were removed.
- Cleanup hash mismatch: run `legacy-cleanup --plan` again and review the new inventory.
