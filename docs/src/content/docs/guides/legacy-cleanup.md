---
title: Legacy Cleanup
---

Old daemon/gate/worktree state is never removed automatically.

```sh
no-mistakes legacy-cleanup --plan
no-mistakes legacy-cleanup --confirm <plan-hash>
```

The plan is read-only. Confirmation re-inventories all targets and requires the same hash.
Active runs, live daemon ownership, unreadable state, symlink escapes, and changed evidence
block cleanup. User worktrees, unrelated files/hooks/remotes, `origin`, and configuration
are preserved. Confirmed cleanup unregisters owned launchd/systemd definitions or Windows
scheduled tasks before deleting their definitions.
