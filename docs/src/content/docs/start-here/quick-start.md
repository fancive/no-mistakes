---
title: Quick Start
---

```sh
no-mistakes check --file internal/example.go --message "feat(core): add guard"
no-mistakes commit --file internal/example.go --message "feat(core): add guard" --allow-repo-lint
no-mistakes push --expected-head <commit-sha>
```

Repeat `--file` for every task-owned path. Inspect the exact lint command printed by
`check` before authorizing it. `push` always resolves the current HEAD and live remote
again and never force-pushes. For an authorized iCode submit tail, also pass
`--allow-icode-submit --icode-policy-hash <hash-from-check>`; configuration alone stops
pending before reviewer, `+2`, or submit writes.
