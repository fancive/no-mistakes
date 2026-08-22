# Contributing

Create a feature branch, make a focused change, and run:

```sh
make fmt
make lint
make test
make e2e
```

The committed `skills/no-mistakes/SKILL.md` is generated from
`internal/skill/skill.go`. Edit the source and run `make skill`; `make lint` checks drift.

Use conventional commits for GitHub changes. Do not hand-edit `CHANGELOG.md` or
`.release-please-manifest.json`; release-please owns them.

No-mistakes itself performs only a regular push. Contributors create and manage the GitHub
PR through their normal GitHub workflow.
