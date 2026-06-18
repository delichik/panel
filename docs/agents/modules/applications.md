# 应用模块

## 适用场景

修改应用创建、编辑、appspec、变量解析、应用文件、保存会话、修订、部署、停止、重启、日志、运行时状态、镜像更新或应用反向代理字段时，先读本文档。

## 后端入口

- 应用服务与 handler：`internal/modules/applications/`；HTTP 路由在 `routes.go` 注册。
- 应用文件 CRUD、文件读取和运行时 managed-file 挂载：`internal/modules/applications/files.go`
- 保存会话与临时文件生命周期：`internal/modules/applications/save_session.go`
- 应用规格模型、校验和渲染：`internal/modules/applications/spec/`
- 运行时规格类型：`internal/modules/applications/runtime/`
- Agent 协议与运行时客户端接口：`internal/agent/contract/`、`internal/agent/client/`
- 模板渲染接口：`internal/platform/templating/`
- 任务记录：`internal/modules/tasks/`
- 服务器能力：`internal/modules/servers/`
- 跨模块连接：`internal/bootstrap/panel/app.go`

## 前端入口

- 应用列表：`web/src/views/runtime/applications/index.vue`
- 编辑器：`web/src/views/runtime/applications/ApplicationEditor.vue`
- 详情：`web/src/views/runtime/applications/ApplicationDetail.vue`
- 运行时面板：`web/src/views/runtime/applications/ApplicationRuntimePanel.vue`
- 日志弹窗：`web/src/components/RuntimeLogsDialog.vue`
- API：`web/src/api/applications.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- 应用 CRUD：`GET/POST /api/v1/applications`，`GET/PUT/DELETE /api/v1/applications/{id}`
- 应用文件：`GET/POST /api/v1/applications/{id}/files`，`DELETE /api/v1/applications/{id}/files/{fileId}`
- 保存会话：`POST /api/v1/application-save-sessions`，`POST /api/v1/application-save-sessions/{id}/files`，`POST /api/v1/application-save-sessions/{id}/files/delete`，`POST /api/v1/application-save-sessions/{id}/commit`
- 校验和计划：`POST /api/v1/applications/{id}/validate`，`POST /api/v1/applications/{id}/plan`
- 运行操作：`POST /api/v1/applications/{id}/deploy`，`POST /api/v1/applications/{id}/migrate`，`POST /api/v1/applications/{id}/stop`，`POST /api/v1/applications/{id}/restart`
- 镜像：`POST /api/v1/applications/{id}/image/check`，`POST /api/v1/applications/{id}/image/update`
- 运行时和日志：`GET /api/v1/applications/{id}/runtime`，`GET /api/v1/applications/{id}/logs`
- 打包：`GET /api/v1/applications/{id}/package`
- 持久化数据：`GET /api/v1/applications/{id}/persistent-data` 下载，`POST /api/v1/applications/{id}/persistent-data` 上传 zip 覆盖并重启
- 模板目录：`GET /api/v1/application-template-catalog`

## 数据与行为约定

- 主要表包括 `applications`、`application_files`、`application_revisions`、`application_instances`。
- appspec 以 YAML 输入，经 `internal/modules/applications/spec/` 校验并渲染为 `appruntime.Spec`；部署时由 Panel 选择目标服务器并编排运行时步骤，再通过目标机 `panel-agent` 的原子接口写入托管文件、拉取镜像、删除旧容器、创建容器、启动容器和刷新状态。
- `application_instances` 是 Panel 的运行时事实表，按 `application_id + server_id` 记录实例、容器名、容器 ID、期望状态、最近状态、渲染后的 runtime spec 和部署 generation。
- 默认部署模式为 `all`，会在所有 agent 健康且兼容的服务器上各创建一个实例；`selected` 只部署到选中的服务器。含 `persistent` 挂载的应用必须且只能部署到一个服务器；已有运行时实例后，可通过实例所在服务器的 agent 将 `/opt/panel/apps/<applicationId>/persistent` 打包下载，或上传 zip 由 agent 校验路径后全量覆盖该目录并触发应用重启。
- 删除服务器会通过服务器模块修剪应用 `deployment_server_ids_json` 中的对应 ID，并依赖数据库外键级联删除该服务器上的 `application_instances` 和协调状态；如果 `selected` 应用因此没有部署目标，后续部署/计划应保持校验失败，直到用户重新选择目标服务器。
- 不含 `persistent`、host/bind 挂载和 Docker volume 挂载，且当前只有一个来源运行实例的应用可执行无损迁移。迁移要求来源实例正在运行、目标服务器 agent 兼容且没有该应用实例；Panel 将部署目标切换为目标服务器并部署新实例，成功后只移除来源 `application_instances` 记录，不停止或删除来源容器。
- 应用变量、部署模式、反向代理配置等持久化字段必须保存稳定结构，不保存已翻译展示文案。
- 文件内容通过 API 以 base64 承载；应用文件 CRUD、读取和部署时挂载转换集中在 `files.go`，保存会话用于批量上传、删除和提交。
- 保存会话的临时目录由应用装配层设置到 `<dataRoot>/tmp/application-save-sessions`，不得依赖进程工作目录下的相对 `tmp`。
- 保存会话的创建、上传、删除、提交、过期清理和临时文件转换集中在 `save_session.go`；应用 CRUD、部署和运行时流程不得复制会话锁或临时目录清理逻辑。
- 启用应用、部署、镜像更新等流程需要先校验和计划，再确认目标服务器 agent runtime 可用，然后写入应用修订和实例记录；部署编排必须留在 Panel 侧，agent 不保留胖 deploy handler，只提供写文件、创建容器、Docker 镜像和容器动作等原子接口。多目标部署中单台服务器部署失败不得提前中断后续服务器，必须记录该实例失败并继续尝试剩余目标，最后汇总失败目标返回应用运行时错误；Agent/Docker runtime 返回的部署、停止、重启和日志错误必须包装为用户可见的应用运行时错误，保留原始诊断，不能退化成统一内部错误。
- 应用部署的 `pull image` 步骤允许最长 15 分钟，以适配较慢的镜像仓库或大镜像下载；其它 agent/runtime 操作仍使用常规短超时。
- 应用 deploy/stop/restart/logs/runtime status 等依赖 agent 的操作只在目标服务器存在 `agent.url` 且 `agent.status=compatible` 时执行；agent 未部署、异常、不兼容或无法部署时不得创建新的运行时操作任务、不得修改应用启用状态，也不得回退 SSH。运行时状态刷新遇到 agent 未就绪时只返回数据库中的已知状态，不发起远端调用。
- 应用运行时部署、停止、重启、状态刷新和日志读取遇到 agent mTLS server 证书过期或尚未生效时，必须交给服务器模块标记 Agent 状态并按受限自动重装策略处理；当前应用操作仍按原始 agent 错误失败，避免在证书未修复前继续误操作。
- Application 容器使用 `panel.application.*` Label 标识；旧下划线 Label 不兼容且不自动迁移。
- Application 部署、停止、重启和镜像更新后的容器重建与普通容器操作共享目标服务器的单队列。
- scheduler 容器监控只协调已经观察到新托管 Label 的实例；发现缺失、停止或 generation/spec hash 偏差时创建 `application_reconcile`。
- `application_deploy` 任务表示 Panel 已完成一次部署请求和实例记录更新，不等于容器长期健康；实际容器健康必须通过运行时面板刷新展示。
- 应用列表接口会刷新已记录实例的运行时状态并聚合为 `runtimeStatus`，列表只展示应用名称、启用状态、运行状态、镜像更新状态和更新时间；jobId、namespace、generation、lastEval、specHash、persistentPath 等诊断字段放在详情。
- 应用列表在桌面端是满高主从工作区，左侧表格体必须独立滚动并吸收剩余高度，分页固定在列表卡片底部。
- 应用停止会更新应用为 disabled，并对当前实例调用 agent runtime stop；`purge` 参数会传给 agent 清理容器。
- 应用日志按 `instanceId` 和可选 `containerName` 读取。日志必须从 runtime 实例提供入口并在弹窗中展示，不再使用 allocation/task 语义；tail 行数最大为 10000。
- 模板目录提供 `server.id`、`server.name`、`server.ssh_host`、`server.ssh_port`、`server.ssh_username` 等节点变量；值来自实际部署目标服务器。
- 应用文件模板在后端部署渲染阶段可读 `PANEL_SERVER_*` 变量，因此同一应用在不同服务器会得到不同的服务器值。
- `panel_file` 挂载使用 `key_asset:<asset-id>:<kind>` 稳定引用 Panel 托管密钥或证书文件；旧 `certificate:<resource-id>:<kind>` 来源仍可被后端读取以服务已有应用规格，但新目录和页面只生成 `key_asset:`。
- 私钥内容不通过目录 API 返回，只在部署渲染时由后端解密并作为只读 managed file 下发给 agent。
- 密钥资产服务扫描应用 spec 和反向代理域名，返回精确的应用 ID、名称及 `panel_file` / `reverse_proxy` 引用，用于删除保护和导入覆盖确认。
- 证书续签、密钥资产重新签发、SSH 密钥重新生成和批量导入会调用 `RedeployEnabledApplications`，确保每台服务器重新按自身变量渲染。

## Application Editor Command Fields

- `ApplicationEditor.vue` 的可视化编辑同时维护 appspec `command` 和 `args` 有序数组。每一行是一个 argv 项，编辑器不得按空格拆分用户输入。
- `command` 只表示可执行文件或 entrypoint；所有 flag 和参数值必须写入 `args`。
- 后端 appspec 校验拒绝超过一个非空 `command` 项，空 command/args 项会正规化为未设置，避免不填写 command 时阻塞保存。
- 应用编辑器包含可视化和 YAML 两个标签页。可视化页是单页分区表单：标准短字段使用双列网格，端口映射和挂载行保持全宽重复行，便于阅读密集网络和存储设置。
- 应用编辑器弹窗在桌面端可使用较宽布局承载密集表单；端口、挂载和反向代理等复杂重复行在中等宽度下必须提前折叠为单列，避免多个字段、说明文本和操作按钮被挤在一行。
- `mounts` / `volumes` 属于 appspec YAML，必须支持 YAML 编辑；可视化页也要继续提供挂载编辑入口并与 YAML 往返同步。应用文件模板是应用级文件内容，不属于 appspec YAML，不能混入 YAML 编辑。
- YAML 标签页只编辑 appspec YAML；应用名称、启用状态、部署目标、反向代理规则、变量和应用文件是应用级保存字段，必须作为两个标签页共享的表单区展示，不能只出现在可视化页。
- 前端 appspec YAML 解析和输出使用标准 YAML 库，不能再在组件内手写轻量 parser。`args` 中以冒号开头或包含冒号的值（例如 `:9443`、`--listen=:9443`）必须按字符串往返。
- 镜像更新检查是 scheduler/containerization 的自动流程，应用详情只展示最近自动检查结果和手动“更新”动作，不再提供手动检查入口。
- 新部署 Application 容器名使用 `panel-<application-name>`；停止、重启、状态和日志操作必须使用 `application_instances.container_name`，以兼容旧版本按实例 ID 生成的容器名。agent 必须声明 `runtime-write-files`、`runtime-create-container` 和 `runtime-container-name` capability 才能被视为兼容；旧 `runtime-deploy` 胖接口不再使用。

## 验证

- 后端应用或 appspec 改动运行 `task test:backend`，重点关注 `internal/modules/applications`、`internal/modules/applications/spec` 和 agent runtime 相关测试。
- 前端应用页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 appspec 字段、应用持久化字段、API、应用文件行为、部署流程、镜像更新逻辑、运行时展示字段或 agent runtime 契约时，必须更新本文档。
