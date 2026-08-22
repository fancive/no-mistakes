// Package skill owns the generated no-mistakes agent skill.
package skill

import "strings"

const Name = "no-mistakes"

const Description = "Deterministically check and execute exact-scope commits, configured changed-file lint, branch/remote policy, and safe GitHub or iCode pushes. Use when ship delegates its commit or push guard, the user asks to validate commit/branch/push policy, or invokes /no-mistakes."

func Markdown() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + Name + "\n")
	b.WriteString("description: " + Description + "\n")
	b.WriteString("user-invocable: true\n")
	b.WriteString("---\n")
	b.WriteString(body)
	return b.String()
}

const body = `
# no-mistakes

No-mistakes is a synchronous, stateless shipping guard. It is not an AI reviewer,
test/document agent, automatic fixer, rebase engine, daemon, or background pipeline.
It runs in the caller's current checkout and never creates a hidden worktree.

The ` + "`ship`" + ` skill owns authorization and orchestration. No-mistakes owns the
deterministic execution boundary after that authorization:

- exact authored file scope and sensitive-path protection;
- provider commit message, author, and Gerrit ` + "`Change-Id`" + ` policy;
- the repository's explicitly configured changed-scope lint command;
- attached branch and authoritative remote-target checks;
- regular, non-force exact-SHA GitHub or iCode push behavior.

Every command emits versioned TOON with ` + "`schema_version: 1`" + `. Unknown or missing
schema fields, network/auth errors, absent refs, non-fast-forward updates, and ambiguous
provider results are blockers rather than warnings.

## Commands

### Read-only plan

` + "```sh" + `
no-mistakes check --file path/to/one --message "feat(scope): description"
` + "```" + `

` + "`check`" + ` resolves the provider, branch, HEAD, target ref, exact files, commit
policy, and lint plan without staging or pushing. A repository-provided lint command is
printed but never run by ` + "`check`" + `; ` + "`commit`" + ` runs it only with explicit
` + "`--allow-repo-lint`" + ` authorization.

### Exact commit

` + "```sh" + `
no-mistakes commit \
  --file path/to/one \
  --file path/to/two \
  --message "feat(scope): description" \
  --allow-repo-lint
` + "```" + `

Repeat ` + "`--file`" + ` for every task-owned file. Never substitute ` + "`git add -A`" + `.
Commit rechecks branch/remote prerequisites, validates the exact staged set, runs the
authorized lint with ` + "`NO_MISTAKES_BASE_SHA`" + `, ` + "`NO_MISTAKES_HEAD_SHA`" + `,
` + "`NO_MISTAKES_LINT_SCOPE=changed`" + `, and the NUL manifest at
` + "`NO_MISTAKES_CHANGED_FILES_FILE`" + `, then commits with rollback on failure.
If lint rewrites authored or tracked content, the guard blocks instead of silently committing
the rewrite.

GitHub subjects use conventional ` + "`type(scope): description`" + ` and author
` + "`fancivez <fancive@gmail.com>`" + `. GitHub commits keep normal hooks active but
disable Gerrit's standard ` + "`Change-Id`" + ` insertion for that command. iCode subjects use
` + "`{icafe-id} [Story|Bug|Task] {中文描述}`" + ` and require an executable Gerrit
commit-msg hook. Never invent an iCafe id and never add AI attribution.

### Safe push

` + "```sh" + `
no-mistakes push
` + "```" + `

For an automated caller, bind the commit produced by the guard:

` + "```sh" + `
no-mistakes push --expected-head <commit-sha>
` + "```" + `

Push always re-resolves the exact current HEAD and live remote. It never force-pushes.

- fancive GitHub repositories without a conventional ` + "`origin=fork, upstream=parent`" + `
  layout: regular fast-forward push to the resolved default branch;
- GitHub fork checkouts and other GitHub repositories: regular push to the attached feature
  branch, then return to
  ` + "`ship`" + `/GitHub tooling for PR, CI, and merge;
- iCode: require the remote ` + "`refs/heads/<SCM_FLOW branch>`" + ` to exist, push the
  immutable commit to ` + "`refs/for/<branch>`" + ` and observe machine checks/iPipe. The
  candidate config's ` + "`icode.auto_submit: true`" + ` only makes its canonical reviewer
  policy eligible for authorization; it grants no write authority by itself. Try self +2,
  reviewer fallback, and submit only when the caller also supplies ` + "`--expected-head`" + `,
  ` + "`--allow-icode-submit`" + `, and the exact ` + "`--icode-policy-hash`" + ` reported by
  ` + "`check`" + `. Otherwise return pending before those writes. Only ` + "`MERGED`" + ` is
  success; ` + "`icode-cli`" + ` has no revision-CAS write flag.

A GitHub feature branch may have no tracking ref or track ` + "`origin/<same-branch>`" + `.
If it tracks a differently named ref, the guard blocks instead of silently creating another
remote branch or guessing the intended PR target.

No-mistakes never creates an SCM_FLOW branch. When the target is missing, return custody to
the main context and invoke ` + "`$ipipe-pull-branch`" + ` explicitly under its confirmation
gate. A self-merged non-main iCode result emits an ` + "`opera-deploy`" + ` imeShahe handoff;
deployment still requires that skill's explicit confirmation.

### Legacy cleanup

` + "```sh" + `
no-mistakes legacy-cleanup --plan
no-mistakes legacy-cleanup --confirm <plan-hash>
` + "```" + `

Plan is read-only. Confirm re-inventories the old daemon, gates, worktrees, database and
owned remotes, requires the exact hash, refuses active/uncertain state, and preserves
unrelated files, hooks, remotes and user worktrees. Never bypass a cleanup blocker.

## Removed commands

` + "`init`" + `, ` + "`eject`" + `, ` + "`daemon`" + `, ` + "`attach`" + `,
` + "`rerun`" + `, ` + "`status`" + `, ` + "`sync`" + `, ` + "`runs`" + `,
` + "`stats`" + `, ` + "`eval`" + `, and the entire ` + "`axi`" + ` pipeline were removed.
Do not recreate them through shell wrappers or competing delivery drivers.
`
