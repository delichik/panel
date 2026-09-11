# 工程质量、构建与发布验收规范

本文约束本地验证入口、生成代码、二进制、容器镜像和 GitHub Actions 发布。它不规定日常 Git 操作。

## 1. 受支持的验证入口

- `ENG-TEST-001`：后端验证通过 `task test:backend`，并在测试前生成 Agent contract hash；前端验证通过 `task test:web`；全栈契约或构建链路改动使用 `task test`/`task build`。
- `ENG-TEST-002`：只改文档不运行测试或编译；只改前端不运行后端测试；只改后端不运行前端测试。最终说明必须列出已运行检查或未运行原因。
- `ENG-TEST-003`：测试、生成和调试的仓库内中间文件只放 `tmp/`；不得把生成缓存散落到源码目录，规范要求的受控生成文件除外。
- `ENG-TEST-004`：API 路由清单、数据库模型接管、架构依赖、Agent contract 与前端主题/i18n 等守卫测试不得无说明地删除或放宽。
- `ENG-TEST-005`：修复回归必须增加最小稳定测试，覆盖触发条件和错误结果；不得只断言内部函数被调用。
- `ENG-TEST-006`：外部网络、SSH、Docker 或时钟相关测试必须使用可控替身或明确的集成 harness，单元测试不得依赖开发者机器状态。

## 2. 生成代码与 Agent 合同

- `ENG-GEN-001`：Panel、panel-init 和所有架构的 panel-agent 构建前必须生成并校验同一 gRPC contract hash。
- `ENG-GEN-002`：protobuf 源是 RPC 字段的事实来源；生成文件和根目录兼容副本必须与源一致，手工修改生成文件不得作为合同变更。
- `ENG-GEN-003`：新增 RPC 字段应保持旧 Agent 的可解析性；破坏性协议变化必须有明确版本拒绝和用户可诊断错误。
- `ENG-GEN-004`：Agent 的部署兼容检查以 Panel 规定的版本策略为准；Panel 不得向已知不兼容 Agent 发送会导致未知破坏的写操作。

## 3. 本地与容器构建

- `ENG-BUILD-001`：前端生产构建使用锁文件可复现安装；Go 构建使用 `go.mod/go.sum`，不得依赖未声明的本机模块。
- `ENG-BUILD-002`：生产镜像包含 `/app/panel`、`/app/panel-init`、`/app/web/dist` 和完整 `/app/panel-agents/linux-amd64`、`linux-arm64` bundle。
- `ENG-BUILD-003`：目标镜像的 Panel/panel-init 必须匹配目标 CPU 架构；Agent bundle 同时包含 amd64 与 arm64，并验证 ELF machine，禁止靠文件名假定架构。
- `ENG-BUILD-004`：Panel、panel-init、Agent 注入相同 version、channel、repository、commit 元数据；前端和后端来自同一源码触发提交。
- `ENG-BUILD-005`：运行容器使用非 root `panel` 用户，以 `/app/panel-init` 为入口，声明 `/app/data` volume；数据目录必须可由该用户读写。
- `ENG-BUILD-006`：容器服务监听 `0.0.0.0:8443`，只暴露 8443；健康检查访问 `https://127.0.0.1:8443/`，允许内置自签名证书但不得退回 8080。
- `ENG-BUILD-007`：从源码构建的默认 `runtime` target 与 CI 使用预构建 artifact 的 `runtime-from-artifacts` target 产物布局和运行行为一致。

## 4. 版本生成

- `REL-VER-001`：`version.json` 只保存非负 `major`、`minor`；正式 patch 从远端已有正式 tag 自动计算，首次为 0，之后单调递增且不复用缺口。
- `REL-VER-002`：main 版本格式为 `v<major>.<minor>.<patch>`；dev 版本以当前正式 patch（无正式版时为 0）加 UTC 时间戳，仅作为构建元数据，不创建 tag。
- `REL-VER-003`：main 发布在构建前把新 tag 指向触发提交并推送，以占用版本；同一发布系列必须串行，防止生成重复版本。
- `REL-VER-004`：dev 新推送可以取消旧 dev 构建，但不得影响 main 发布；main 与 dev 使用独立 concurrency group。

## 5. 发布流水线

- `REL-CI-001`：发布仅由 `main` 或 `dev` push 触发；不得改为依赖人工 GitHub Release 或 release event 才构建镜像。
- `REL-CI-002`：Web、Go module cache、Agent bundle、amd64/arm64 Panel 与 panel-init 可并行构建；任一必需 artifact 缺失必须使发布失败。
- `REL-CI-003`：每个目标架构分别打包并按 digest 推送，只有全部架构成功后才创建多架构 manifest。
- `REL-CI-004`：main 发布标签必须包括版本、`latest` 和 commit sha；dev 只能更新 `dev`，不得创建正式版本、sha、`latest` 标签、Git tag 或 GitHub Release。
- `REL-CI-005`：manifest 创建后必须 inspect 对应通道标签；inspect 失败不得继续创建正式 Release或执行会掩盖失败的清理。
- `REL-CI-006`：main 在镜像及 manifest 验证成功后，为同一版本 tag 创建 GitHub Release并生成 release notes；工作流需要的 contents/packages 写权限不得扩大到无关权限。
- `REL-CI-007`：dev 清理只删除超过保护窗口且无 `dev`、`latest`、正式 semver 标签的旧 package version；必须支持组织和个人 owner API，列表或删除失败使清理步骤失败。
- `REL-CI-008`：清理不得删除当前 manifest 引用的架构 digest或任何受保护正式产物；调整清理规则时必须有可验证的保护集合。

## 6. 发布验收证据

- `REL-EVD-001`：正式发布可从 tag、GitHub Release、GHCR 版本标签、latest、sha 标签追踪到同一 commit。
- `REL-EVD-002`：`linux/amd64` 与 `linux/arm64` 拉取的镜像各自运行正确架构 Panel，同时都能为两种目标架构提供 Agent bundle。
- `REL-EVD-003`：容器启动后 HTTPS 健康检查通过，静态前端可加载，系统版本接口返回注入版本与通道。
- `REL-EVD-004`：dev 发布只改变 `dev` 指向，已有正式 tag、Release、latest 和正式镜像内容保持不变。

