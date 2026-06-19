# GitHub Actions 发布流程模块

## 范围

- `.github/workflows/docker-release.yml` 负责发布容器镜像。
- 根目录 `version.json` 指定自动发布版本的 `major` 和 `minor`，两个字段都必须是非负整数。
- 正式发布由 `main` 分支 push 触发，开发发布由 `dev` 分支 push 触发；两者都不使用 Git tag 或 GitHub Release 事件触发。
- workflow 自动生成并推送形如 `v<major>.<minor>.<patch>` 的 tag，最后在镜像打包、multi-arch manifest 发布和 inspect 都成功后，为同一个 tag 创建 GitHub Release。
- dev 发布生成形如 `v<major>.<minor>.<patch>.<yyyyMMddHHmmss>` 的构建版本，但不创建 Git tag 或 GitHub Release，只覆盖 `dev` 镜像标签。

## 关键流程

- `generate-version` 读取并校验 `version.json`，拉取远端全部 tag，并查找当前主版本和次版本下的最大正式修订号。
- main 发布将最大正式修订号加一；首次发布该主次版本时修订号为 `0`。
- dev 发布复用当前最大正式修订号并追加 UTC 时间戳；尚无正式 tag 时以修订号 `0` 为基线。
- workflow 按分支使用独立 concurrency group。main 发布串行执行；新的 dev push 会取消同分支尚未完成的旧构建。
- 新版本 tag 会在构建前指向触发 workflow 的 `main` commit 并推送到仓库，以占用版本号；同一主次版本的修订号只会递增，不会复用历史缺口。
- `build-amd64` 和 `build-arm64` 分别构建对应架构镜像，并按 digest 推送到 GHCR。
- 两个架构构建都会把自动生成的版本、发布通道（`release` 或 `dev`）、`${{ github.repository }}` 和 commit SHA 作为 Docker build args 传入，再通过 Go `ldflags` 注入 `internal/platform/buildinfo`。
- Docker 镜像同时包含主服务 `/app/panel` 和独立 agent bundle `/app/panel-agents/`；当前 bundle 包含 `linux-amd64/panel-agent` 与 `linux-arm64/panel-agent`。每个架构的镜像都必须携带完整 agent bundle，且 agent 二进制仍注入与 Panel 相同的版本信息用于展示和排查；Panel 自动部署 agent 时会按目标服务器架构读取对应文件并上传到目标机。Docker 后端构建在编译 Panel 和两种架构 Agent 前先生成被忽略的 `internal/agent/contract/contract_hash_generated.go`，确保三个二进制引用同一个 HTTP contract hash；Agent 是否需要重部署由健康检查返回的能力列表和该 hash 决定，不由 Panel/Agent 版本号相等性决定。
- `publish-manifest` 汇总两个架构的 digest，发布以下镜像标签：
  - main：自动生成的版本号、`latest` 和 commit sha。
  - dev：只发布并覆盖 `dev`，不发布版本号、commit sha 或 `latest`。
- main 的 `publish-manifest` 末尾使用 `softprops/action-gh-release` 为自动生成的版本 tag 创建 GitHub Release，并启用自动 release notes；dev 跳过该步骤。

## 修改注意事项

- 调整发布系列时只修改 `version.json` 的 `major` 和 `minor`；不要在文件中维护 `patch`。
- 不要把发布触发器改为 `release` 事件；推送到 `main` 或 `dev` 后会发布对应通道。
- workflow 需要 `permissions.contents: write` 来推送版本 tag 和创建 GitHub Release，推送 GHCR 镜像需要 `permissions.packages: write`。
- 不要移除 main 的串行 concurrency；正式版本生成依赖它避免并发运行选择相同修订号。
- dev 通道不得创建 tag、Release，或覆盖 `latest`、正式版本号和 commit sha 标签。
- 如果新增 release 附件，必须确保附件生成和上传步骤在创建 GitHub Release 之前完成，且失败时不要创建 release。
- 发布构建必须保持 `PANEL_VERSION`、`PANEL_CHANNEL`、`PANEL_REPOSITORY`、`PANEL_COMMIT` 注入一致；`PANEL_CHANNEL` 在 main 为 `release`、dev 为 `dev`，未注入或无效值按开发通道处理。
- 修改发布流程后同步更新本文档和模块索引。

## 检查和测试范围

- 只改 GitHub Actions YAML 或本文档时，没有对应的 `task build:*` / `task test:*` 本地检查项；应重点复核 YAML 结构、权限和 job 依赖顺序。
- 如果发布流程引入应用构建命令，再按受影响范围执行对应的 `task build:*` 或 `task test:*`。
