# GitHub Actions 发布流程模块

## 范围

- `.github/workflows/docker-release.yml` 负责发布容器镜像。
- 发布流程由 Git tag push 触发，不使用 GitHub Release 事件触发。
- workflow 最后在镜像打包、multi-arch manifest 发布和 inspect 都成功后，为同一个 tag 创建 GitHub Release。

## 关键流程

- `verify-tag` 会确认 tag 指向的提交属于 `origin/main`。
- `build-amd64` 和 `build-arm64` 分别构建对应架构镜像，并按 digest 推送到 GHCR。
- 两个架构构建都会把 Git tag、`${{ github.repository }}` 和 commit SHA 作为 Docker build args 传入，再通过 Go `ldflags` 注入 `internal/buildinfo`。
- `publish-manifest` 汇总两个架构的 digest，发布以下镜像标签：
  - Git tag 名称。
  - `latest`。
  - commit sha。
- `publish-manifest` 末尾使用 `softprops/action-gh-release` 为 `${{ github.ref_name }}` 创建 GitHub Release，并启用自动 release notes。

## 修改注意事项

- 不要把发布触发器改为 `release` 事件；需要发布时推送 Git tag。
- 创建 GitHub Release 的最终 job 需要 `permissions.contents: write`，推送 GHCR 镜像需要 `permissions.packages: write`。
- 如果新增 release 附件，必须确保附件生成和上传步骤在创建 GitHub Release 之前完成，且失败时不要创建 release。
- 发布构建必须保持 `PANEL_VERSION`、`PANEL_REPOSITORY`、`PANEL_COMMIT` 注入一致，否则系统信息和更新检查会退化为开发版本行为。
- 修改发布流程后同步更新本文档和模块索引。

## 检查和测试范围

- 只改 GitHub Actions YAML 或本文档时，没有对应的 `task build:*` / `task test:*` 本地检查项；应重点复核 YAML 结构、权限和 job 依赖顺序。
- 如果发布流程引入应用构建命令，再按受影响范围执行对应的 `task build:*` 或 `task test:*`。
