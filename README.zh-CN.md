# no-mistakes

`no-mistakes` 是一个同步、无状态的交付守卫，只保留适合确定性执行的部分：

- 精确校验本次修改文件范围和敏感路径；
- 校验 GitHub / iCode 的提交信息、作者和 Gerrit `Change-Id`；
- 运行仓库显式配置的 changed-file lint；
- 校验当前分支和远端目标；
- 用普通、非 force 的方式推送精确 HEAD。

它不是 AI reviewer，也不是自动修复循环。它没有 daemon、隐藏 worktree、运行数据库、
rebase/sync 引擎、TUI、GitHub PR 创建和 CI 监控。`$ship` 等外层流程负责意图、授权、
review、测试、CI、合入和部署。

## 命令

```sh
no-mistakes check --file internal/example.go --message "feat(core): add guard"
no-mistakes commit --file internal/example.go --message "feat(core): add guard" --allow-repo-lint
no-mistakes push --expected-head <commit-sha>
```

所有命令都输出带 `schema_version: 1` 的 TOON。`check` 只读；`commit` 只暂存重复传入的
`--file`，失败时恢复 index/HEAD；`push` 在写入前重新读取当前分支、精确 HEAD 和远端状态。

iCode 的 SCM_FLOW 目标分支必须预先存在。缺失时会阻塞并提示显式运行
`$ipipe-pull-branch`，no-mistakes 自己不会建分支。iCode 仍保留检查、自助 `+2`、
reviewer fallback、submit、`MERGED` 证明，以及可选的 `opera-deploy` / `imeShahe` 交接信息。
但仓库配置本身不能授权 reviewer、`+2` 或 submit 写操作。

## 配置

```yaml
output_language: zh-CN
commands:
  lint: bash scripts/lint-changed.sh
icode:
  auto_submit: true
  reviewers: [alice, bob]
```

`check` 只展示 lint 命令；`commit` 只有在显式传入 `--allow-repo-lint` 后才运行它，并提供
NUL 分隔的 changed-file manifest。`output_language: zh-CN` 会让结构化的用户提示使用中文。

`icode.auto_submit` 默认是 `false`。设为 `true` 只表示该策略允许被调用方显式授权。
`check` 会输出 `icode_policy_hash`；调用方还必须同时绑定提交 SHA 和当前策略：

```sh
no-mistakes push --expected-head <commit-sha> \
  --allow-icode-submit --icode-policy-hash <check-输出的哈希>
```

不传这组授权时，iCode 仍可推送 CR 并查看检查，但会在 reviewer、`+2`、submit 前返回
pending。`icode-cli` 没有 revision-CAS 写参数，因此显式授权后的写操作仍会立即复核 revision，
且只有平台返回 `MERGED` 才算交付成功。

## 旧状态清理

旧版本可能在 `~/.no-mistakes` 留下 daemon、数据库、gate 或托管 worktree：

```sh
no-mistakes legacy-cleanup --plan
no-mistakes legacy-cleanup --confirm <plan-hash>
```

确认时会重新扫描并核对 hash；活动中或无法证明归属的状态会阻塞。无关文件、hook、
remote、配置和用户 worktree 都会保留。

升级前请先用旧版二进制运行 `no-mistakes daemon stop`；精简版清理器不会仅凭推断去杀进程。

开发验证命令：`make build`、`make lint`、`make test`、`make e2e`。
