# Vision

No-mistakes should be a small, auditable policy executor beneath the user's existing
shipping workflow.

Its invariants are deliberately narrow:

- never include files outside the exact authorized scope;
- never silently execute repository code;
- never infer success from missing or failed remote evidence;
- never force-push, auto-rebase, or create provider branches;
- bind mutations to the current checkout and an immutable commit;
- preserve user state on failure;
- leave intent, code changes, review, CI, merge, and deployment to their owning workflows.

Provider-specific deterministic transactions may stay together when splitting them would
lose correctness. That is why iCode delivery includes `refs/for`, checks, approval, submit,
and `MERGED` verification, while GitHub delivery stops after a safe push.

The best failure is explicit custody returned to the caller with structured evidence and a
bounded next action.
