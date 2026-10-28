# 工程质量、构建与发布验收规范

本文约束本地验证入口、生成代码、二进制、容器镜像和 GitHub Actions 发布。它不规定日常 Git 操作。

## 1. 受支持的验证入口

- `ENG-TEST-001`：后端验证通过 `task test:backend`，并在测试前生成 Agent contract hash；前端验证通过 `task test:web`；全栈契约或构建链路改动使用 `task test`/`task build`。
- `ENG-TEST-002`：只改文档不运行测试或编译；只改前端不运行后端测试；只改后端不运行前端测试。最终说明必须列出已运行检查或未运行原因。
- `ENG-TEST-003`：测试、生成和调试的仓库内中间文件只放 `tmp/`；Vitest cacheDir 指向仓库 `tmp/vitest`，不得把测试缓存写入依赖目录；不得把生成缓存散落到源码目录，规范要求的受控生成文件除外。
- `ENG-TEST-004`：API 路由清单、数据库模型接管、架构依赖、Agent contract 与前端主题/i18n 等守卫测试不得无说明地删除或放宽。
- `ENG-TEST-005`：修复回归必须增加最小稳定测试，覆盖触发条件和错误结果；不得只断言内部函数被调用。
- `ENG-TEST-006`：外部网络、SSH、Docker 或时钟相关测试必须使用可控替身或明确的集成 harness，单元测试不得依赖开发者机器状态。

## 1.1 Panel 进程日志

- `ENG-LOG-001`：Panel 进程（`cmd/panel` 与 `internal/**` 中由其加载的路径）的全部诊断打印必须经 `internal/platform/logging` 统一工具包；禁止 `log.Print*`、裸 `zap.L()` 或已移除的无 context `logging.L()`。
- `ENG-LOG-002`：每条进程日志必须带可追溯 context：必填 `streamId` 与 `module`（分类），以及等级；JSON 字段至少含 `ts`、`level`、`module`、`streamId`、`msg`。缺 stream/module 的调用不得写入业务 sink（discard + 内部计数）。
- `ENG-LOG-003`：日志流由入口创建并向下传递：HTTP 中间件每请求一流；worker/模块周期 loop 每 tick 一流；`Manager.Run`/启动关闭各自建流；子调用只 `From(ctx)` 或 `With` 改 module/关联字段，不得在叶子发明无来源 stream。
- `ENG-LOG-004`：输出目的地由代码内 module×level 路由表决定（`file`/`stdout`/`stderr`/`discard`），不是运行时 UI 配置；默认路由为 JSONL 文件 sink + stdout 镜像。全局 `log.level` 仍只控制最低等级门槛。产品/过程事实与进程诊断统一经本工具包写入 JSONL，并按 `eventId` upsert AppDB `events`（进程行索引）；不得再向 `activity_events` 追加新产品事实。控制面 `activity_*` 触发器安装为 DROP 空操作；`activitylog.CheckAdmission` 不再因 activity 容量阻断变更。
- `ENG-LOG-005`：验证点覆盖 logging 单测（中间件 stream、无效 context discard、字段存在、JSONL 写入）、以及后端编译下无残留 `logging.L()`/`log.Printf` 于 Panel 路径。
- `ENG-LOG-006`：启动装配在打开 AppDB 后必须调用 `logging.Configure(dataRoot)`（创建 `<dataRoot>/logs`）并 `logging.SetContextStore` 注入 AppDB 实现；文件 sink 按 UTC 日写入 `panel-YYYYMMDD.jsonl`（一行一个 JSON 对象）。
- `ENG-LOG-007`：有效 context 的每次日志写入必须 upsert AppDB `events`（主键 `id` 即产品 `eventId`，与 JSONL 中 `eventId` 一致），并可通过 events 模块按 `eventId` 回读近期 JSONL；`logging.From(ctx)` API 保持不变。
- `ENG-LOG-008`：**已废弃**（产品 HTTP 读接口改由 `ENG-EVT-001` 约束；legacy `/api/v1/log-contexts*` 仍作短期别名）。
- `ENG-EVT-001`：产品 HTTP 读接口为认证后的 `/api/v1/events`（列表，查询 `origin`/`operationId`/`executionId`/`module`/`from`/`to`/`hadError`/`limit`/`cursor`）、`/api/v1/events/{eventId}`、`/api/v1/events/{eventId}/logs`（或等价 `/lines`）、`/api/v1/logs`（查询可选 `eventId` 与 `level`/`limit`/`after`；缺 `eventId` 为全局扫描）与可选 `/api/v1/events/export`（NDJSON）。进程行 DTO 使用 camelCase：`eventId`/`module`/`level`/`state`/`origin`/`operationId`/`executionId`/`requestId`/`title`/`summary`/`result`/`hadError`/`startedAt`/`updatedAt`/`finishedAt`；JSONL 行页为 `{ items, nextCursor, hasMore }`，行对象至少含 `ts`/`level`/`module`/`eventId`/`msg`（**不含 state**）。实现位于 `internal/modules/events`，底层复用 AppDB `events` 与 `<dataRoot>/logs`。legacy `/api/v1/log-contexts*` 映射到同一 handler（`streamId` 作 eventId）。原 `/api/v1/activity/*` 产品读路由不再注册。验证：handler 单测（temp DB + temp log dir）与 `routes_manifest_test` 路由清单哈希（须在同一改动中更新 `wantCount`/`wantHash`）。
- `LOG-CTX-001`：**已废弃**，由 `ENG-EVT-001` 替代（AppDB 进程索引表为 `events`，非 `log_contexts`）。
- `LOG-CTX-002`：**已废弃**，由 `ENG-EVT-001` 与 `TASK-LOG-001` 替代：event executor 输出 `AppendLog` 写入统一 JSONL（带 `eventId`）；`GET /api/v1/executions/{id}/logs` 读 JSONL 行，不得再查 `activity_events`。

## 2. 生成代码与 Agent 合同

- `ENG-GEN-001`：Panel、panel-init 和所有架构的 panel-agent 构建前必须生成并校验同一 gRPC contract hash。
- `ENG-GEN-002`：protobuf 源是 RPC 字段的事实来源；生成文件和根目录兼容副本必须与源一致，手工修改生成文件不得作为合同变更。
- `ENG-GEN-003`：新增 RPC 字段应保持旧 Agent 的可解析性；破坏性协议变化必须有明确版本拒绝和用户可诊断错误。
- `ENG-GEN-004`：Agent 的部署兼容检查以 Panel 规定的版本策略为准；Panel 不得向已知不兼容 Agent 发送会导致未知破坏的写操作。
- `ENG-GEN-005`：Tailscale 相关的 `TailscaleStatus`、`TailscaleConfigure`、`TailscaleDisable` RPC 与 `AgentReport.tailscale` 可选字段必须保持旧 Agent 的可解析性，并随本次修改更新契约 hash；缺省字段不得被解释为“已关闭”，缺少能力只让具体操作失败，不得作为通用兼容门槛。详见 `TS-AGT-001`、`TS-NODE-005`。

## 3. 本地与容器构建

- `ENG-BUILD-001`：前端生产构建使用锁文件可复现安装；Go 构建使用 `go.mod/go.sum`，不得依赖未声明的本机模块。
- `ENG-BUILD-002`：生产镜像包含 `/app/panel`、`/app/panel-init`、`/app/web/dist` 和完整 `/app/panel-agents/linux-amd64`、`linux-arm64` bundle。每个平台的 bundle 必须是构建期产出的 `panel-agent.gz` 与 `panel-agent.sha256`（解压后二进制的 sha256）；`.gz` 必须用 `gzip -9 -n` 生成，构建期还必须回验「解压后哈希等于 `.sha256` 内容」。镜像内不得保留裸二进制，Panel 运行时不压缩也不计算哈希。
- `ENG-BUILD-003`：目标镜像的 Panel/panel-init 必须匹配目标 CPU 架构；Agent bundle 同时包含 amd64 与 arm64，并验证 ELF machine，禁止靠文件名假定架构。
- `ENG-BUILD-004`：Panel、panel-init、Agent 注入相同 version、channel、repository、commit 元数据；前端和后端来自同一源码触发提交。
- `ENG-BUILD-005`：运行容器以 root 的 `/app/panel-init` 为 PID 1，由它把 Panel 子进程降权到非 root 的 `panel` 用户（`-panel-user`、`PANEL_INIT_PANEL_USER` 可覆盖），容器内 `tailscaled` 由 root 的 panel-init 管理，并声明 `/app/data` volume；数据目录必须对 Panel 子进程可读写，`<dataRoot>/tailscale` 目录必须允许 Panel 写入期望态配置。运行容器需要 `--cap-add=NET_ADMIN`、`--cap-add=NET_RAW` 与 `--device=/dev/net/tun`，宿主需加载 `tun` 模块；缺少任一项时容器仍须正常启动与提供 Panel，但容器内 Tailscale 必须明确报告为不可用（见 `TS-INIT-001`），不得静默失败。运行身份不允许降级：容器被强制为非 root（compose `user:`、`docker run --user`，或设置了 `USER` 的旧镜像）时 `panel-init` 必须拒绝启动并以非零码退出，错误文本给出原因与修法；不得以非 root 身份继续运行，也不得静默把 Panel 子进程跑成 root（只有显式 `-panel-user=""` 才表示有意同身份运行）。该权限模型取代此前“容器整体以非 root panel 用户运行”的表述。
- `ENG-BUILD-006`：容器服务监听 `0.0.0.0:8443`，只暴露 8443；健康检查访问 `https://127.0.0.1:8443/`，允许内置自签名证书但不得退回 8080。
- `ENG-BUILD-007`：从源码构建的默认 `runtime` target 与 CI 使用预构建 artifact 的 `runtime-from-artifacts` target 产物布局和运行行为一致。
- `ENG-BUILD-008`：镜像必须包含 Alpine `tailscale` 包提供的 `tailscale` 与 `tailscaled` 可执行文件，并位于 panel-init 的默认探测路径（`PANEL_INIT_TAILSCALE_PATH`、`PANEL_INIT_TAILSCALED_PATH` 可覆盖，显式置空表示有意关闭容器内 Tailscale 管理）。镜像不得内置 tailnet 认证密钥、已登录的节点身份或状态文件；`panel-init` 不得把认证密钥写入镜像、命令行参数或日志。容器内 tailscaled 生命周期见 `TS-INIT-001..007`。
- `ENG-BUILD-009`：镜像构建必须用 `panel-init` 下发的完整监督参数集（`--maintenance-mode`、`--init-control-socket`、`--init-control-token`，附加 `--help` 只触发参数解析）对目标架构 Panel 二进制执行冒烟；任一参数未被接受时构建必须失败，不得产出镜像。源码构建的 `runtime` 与 CI 的 `runtime-from-artifacts` 两个 target 必须一致执行，见 `TS-INIT-005`。

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

