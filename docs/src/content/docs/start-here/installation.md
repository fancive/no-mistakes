---
title: Installation
---

Build or install the single binary:

```sh
make install
no-mistakes --version
```

Installation does not start a service or modify repository remotes/hooks. Run
`make skill` when developing this repository to regenerate the committed agent skill.

When upgrading from the autonomous-pipeline release, run the old binary's
`no-mistakes daemon stop` before replacing it. Then use `legacy-cleanup --plan` and the
hash-bound confirm flow. If the old binary was already replaced while its daemon is still
active, reinstall that old version long enough to stop it; the lean cleanup intentionally
refuses to kill an active or ambiguously owned process.
