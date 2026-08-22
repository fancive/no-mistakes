---
title: Introduction
---

No-mistakes is the deterministic executor beneath an external shipping workflow. The
caller owns intent, authorization, testing, review, PR/CR decisions, CI, and deployment.
No-mistakes owns four synchronous operations: `check`, `commit`, `push`, and explicit
legacy cleanup.

Every response is versioned TOON beginning with `schema_version: 1`. Failures in remote,
authentication, branch, or provider evidence block instead of degrading to success.
