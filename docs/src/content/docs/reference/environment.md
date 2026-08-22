---
title: Environment Variables
---

The configured lint command receives:

| Variable | Meaning |
|---|---|
| `NO_MISTAKES_BASE_SHA` | Resolved remote baseline |
| `NO_MISTAKES_HEAD_SHA` | HEAD checked before lint |
| `NO_MISTAKES_CHANGED_FILES_FILE` | Temporary NUL-delimited exact-file manifest |
| `NO_MISTAKES_LINT_SCOPE` | Always `changed` |

Legacy cleanup reads `NM_HOME` only to locate old state. It does not create new state there.
