# GitHub Actions 发布流程模块

## 范围

- `.github/workflows/docker-release.yml` 负责发布容器镜像。
- 根目录 `version.json` 指定自动发布版本的 `major` 和 `minor`，两个字段都必须是非负整数。
- 发布流程由 `main` 分支 push 触发，不使用 Git tag 或 GitHub Release 事件触发。
- workflow 自动生成并推送形如 `v<major>.<minor>.<patch>` 的 tag，最后在镜像打包、multi-arch manifest 发布和 inspect 都成功后，为同一个 tag 创建 GitHub Release。

## 关键流程

- `generate-version` 读取并校验 `version.json`，拉取远端全部 tag，查找当前主版本和次版本下的最大修订号并加一；首次发布该主次版本时修订号为 `0`。
- workflow 使用固定的 `docker-release-main` concurrency group 串行处理 `main` push，避免多个运行同时生成相同版本。
- 新版本 tag 会在构建前指向触发 workflow 的 `main` commit 并推送到仓库，以占用版本号；同一主次版本的修订号只会递增，不会复用历史缺口。
- `build-amd64` 和 `build-arm64` 分别构建对应架构镜像，并按 digest 推送到 GHCR。
- 两个架构构建都会把自动生成的版本、`${{ github.repository }}` 和 commit SHA 作为 Docker build args 传入，再通过 Go `ldflags` 注入 `internal/buildinfo`。
- `publish-manifest` 汇总两个架构的 digest，发布以下镜像标签：
  - 自动生成的版本号。
  - `latest`。
  - commit sha。
- `publish-manifest` 末尾使用 `softprops/action-gh-release` 为自动生成的版本 tag 创建 GitHub Release，并启用自动 release notes。

## 修改注意事项

- 调整发布系列时只修改 `version.json` 的 `major` 和 `minor`；不要在文件中维护 `patch`。
- 不要把发布触发器改为 `release` 事件；合并或推送到 `main` 后会自动发布。
- workflow 需要 `permissions.contents: write` 来推送版本 tag 和创建 GitHub Release，推送 GHCR 镜像需要 `permissions.packages: write`。
- 不要移除全局串行 concurrency；版本生成依赖它避免并发运行选择相同修订号。
- 如果新增 release 附件，必须确保附件生成和上传步骤在创建 GitHub Release 之前完成，且失败时不要创建 release。
- 发布构建必须保持 `PANEL_VERSION`、`PANEL_REPOSITORY`、`PANEL_COMMIT` 注入一致，否则系统信息和更新检查会退化为开发版本行为。
- 修改发布流程后同步更新本文档和模块索引。

## 检查和测试范围

- 只改 GitHub Actions YAML 或本文档时，没有对应的 `task build:*` / `task test:*` 本地检查项；应重点复核 YAML 结构、权限和 job 依赖顺序。
- 如果发布流程引入应用构建命令，再按受影响范围执行对应的 `task build:*` 或 `task test:*`。
