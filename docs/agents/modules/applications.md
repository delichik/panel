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

- 应用列表：`web/src/views/applications/apps/index.vue`
- 编辑器：`web/src/views/applications/apps/ApplicationEditor.vue`
- 详情：`web/src/views/applications/apps/ApplicationDetail.vue`
- 运行时面板：`web/src/views/applications/apps/ApplicationRuntimePanel.vue`
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

## 校验错误

- 应用保存、计划、部署、迁移、刷新、镜像更新和重部署流程遇到 appspec/YAML 校验失败时，不能只返回泛化的 `application_invalid` 文案。API 错误响应保持 `code=application_invalid`，`error.message` 显示第一条 `<field>: <message>`，并在 `error.details.issues` 返回完整 `{ field, message }` 列表，字段结构与 `/validate` 接口一致；`message` 和每条 issue 的 `message` 必须按当前语言翻译，`field` 保持稳定路径用于定位。

## 数据与行为约定

- 主要表包括 `applications`、`application_files`、`application_revisions`、`application_lifecycle_operations`、`application_lifecycle_targets`、`application_instances`。
- appspec 以 YAML 输入，经 `internal/modules/applications/spec/` 校验并渲染为 `appruntime.Spec`；部署时由 Panel 选择目标服务器并编排运行时步骤，再通过目标机 `panel-agent` 的原子接口写入托管文件、拉取镜像、删除旧容器、创建容器、启动容器和刷新状态。
- appspec 的 `resources.cpu` 和 `resources.memoryMb` 只有设置为正数时才表示运行时限制；字段缺省或显式为 `0` 都表示不限制，不得在规范化、渲染或部署流程中自动补默认 CPU/内存限制。
- appspec 支持 `capAdd` 字符串数组，对应 Docker `--cap-add` / `HostConfig.CapAdd`；规范化时去空、去重并转为大写。`capAdd` 只在用户显式配置时下发，可与 `privileged: true` 并存，当前不提供 `capDrop`。
- `application_lifecycle_operations` 记录一次应用生命周期意图，当前用于部署类流程，保存应用、任务、generation、spec hash、操作类型、整体状态和错误；`application_lifecycle_targets` 按服务器记录本次操作的目标状态、阶段、实例 ID、容器信息和错误。选中 3 台服务器时必须先落 3 条 target，即使某台在 agent 校验、模板渲染或容器创建前失败，也必须在运行时视图中显示为 failed/pending，不得只展示已经创建过 `application_instances` 的服务器。
- `application_instances` 是 Panel 的当前运行时事实表，按 `application_id + server_id` 记录实例、容器名、容器 ID、期望状态、最近状态、渲染后的 runtime spec 和部署 generation；它不再承担“本次部署目标清单”的职责。
- 默认部署模式为 `all`，会在所有 agent 健康且兼容的服务器上各创建一个实例；`selected` 只部署到选中的服务器。含 `persistent` 挂载的应用必须且只能部署到一个服务器；已有运行时实例后，可通过实例所在服务器的 agent 将 `/opt/panel/apps/<applicationId>/persistent` 打包下载，或上传 zip 由 agent 校验路径后全量覆盖该目录并触发应用重启。尚未部署的 `persistent` 应用允许先向选定服务器导入 zip，agent 会创建并覆盖 Panel 托管的 persistent 目录，导入完成后不触发重启，用于从 compose 等外部运行方式迁移数据后再首次启动。
- 删除服务器会通过服务器模块修剪应用 `deployment_server_ids_json` 中的对应 ID，并依赖数据库外键级联删除该服务器上的 `application_instances` 和协调状态；如果 `selected` 应用因此没有部署目标，后续部署/计划应保持校验失败，直到用户重新选择目标服务器。
- 不含 `persistent`、host/bind 挂载和 Docker volume 挂载，且当前只有一个来源运行实例的应用可执行无损迁移。迁移要求来源实例正在运行、目标服务器 agent 兼容且没有该应用实例；Panel 将部署目标切换为目标服务器并部署新实例，成功后删除来源服务器上的容器和该实例运行目录，并移除来源 `application_instances` 记录。
- `persistent` 挂载支持在单条 mount 上配置 `uid`、`gid` 和目录 `mode`（如 `"0755"`），部署写入托管文件阶段会确保对应宿主机持久化子目录存在并应用权限，解决非 root 容器进程写入持久化目录的问题。权限字段只适用于 `persistent`，不得用于 host/global bind 或 Docker volume，避免 Panel 修改用户自管宿主机目录或 Docker 管理卷。
- 应用变量、部署模式、反向代理配置等持久化字段必须保存稳定结构，不保存已翻译展示文案。
- 应用持久化目录不保存为数据库字段；是否启用 persistent 由 appspec 的 `mounts.type=persistent` 派生，详情响应里的 `persistentPath` 只读生成自 `applicationPersistentDir(app.ID)`，保存 DTO 不包含 `persistentPath`。
- 应用 `reverseProxy` 字段只描述应用希望暴露的域名、路径、目标端口和目标类型，不代表所有部署节点都会启用反向代理。`targetType` 支持 `local` 与 `container`：旧数据或空值按 `local` 处理，代理到目标节点本地端口；`container` 代理到同节点 Application 容器名和目标端口。实际生效范围由容器化中的“设施应用 / 反向代理”部署服务器决定：只有设施应用覆盖的服务器才会接收该应用在对应服务器上的反向代理配置。
- 文件内容通过 API 以 base64 承载；应用文件 CRUD、读取和部署时挂载转换集中在 `files.go`，保存会话用于批量上传、删除和提交。
- `file`、`panel_file`、内部文件和模板渲染后的 managed file 统一以只读 `managed_file` 挂载到容器，YAML 中即使写入 `readOnly: false` 也不得让容器修改这些由 Panel 管理的文件。
- 保存会话的临时目录由应用装配层设置到 `<dataRoot>/tmp/application-save-sessions`，不得依赖进程工作目录下的相对 `tmp`。
- 保存会话的创建、上传、删除、提交、过期清理和临时文件转换集中在 `save_session.go`；应用 CRUD、部署和运行时流程不得复制会话锁或临时目录清理逻辑。
- 启用应用、部署、镜像更新等流程需要先校验和计划，再确认目标服务器 agent runtime 可用，然后写入应用修订和实例记录；保存启用应用只写入期望状态并通过 task 框架立即触发 `application_reconcile` 指定该应用，协调 collector 负责收集本次需要部署的应用与服务器，并产出目标级 `application_deploy` 工作项，实际部署由 `application_deploy` handler 后台执行。手动部署 HTTP 入口只创建并启动应用部署任务后返回，实际部署由任务后台执行。部署编排必须留在 Panel 侧，agent 不保留胖 deploy handler，只提供写文件、创建容器、Docker 镜像和容器动作等原子接口。多目标部署中单台服务器部署失败不得提前中断后续服务器，必须记录该实例失败并继续尝试剩余目标，最后汇总失败目标返回应用运行时错误；容器 start 后 inspect 结果必须为 running，否则视为该目标部署失败并保留容器退出/运行时诊断，不能把启动后立刻退出的容器标记为部署成功；Agent/Docker runtime 返回的部署、停止、重启和日志错误必须包装为用户可见的应用运行时错误，保留原始诊断，不能退化成统一内部错误。
- 多目标应用部署必须按任务系统“操作 + 任务”语义建模：一次部署请求是一个操作，每个目标服务器是该操作下的一个 `application_deploy` 任务，任务参数或资源字段必须能定位到对应服务器和应用。创建启用应用、更新启用应用、手动部署、迁移、变量刷新、持久化数据恢复后的重部署和系统触发的启用应用重部署，都必须通过同一目标级部署任务入口创建可见执行对象。`application_lifecycle_operations` / `application_lifecycle_targets` 继续保存运行时领域状态和前端运行实例视图，但不得作为任务中心中目标级执行对象的替代品。
- 应用部署的 `pull image` 步骤允许最长 15 分钟，以适配较慢的镜像仓库或大镜像下载；未显式写 tag 的镜像引用必须按 Docker CLI 语义拉取 `latest`，不得触发 Docker Engine API 拉取仓库全部标签；其它 agent/runtime 操作仍使用常规短超时。
- 应用部署流程必须先创建 lifecycle operation 和全部目标 target，再逐台执行 `validate_agent`、`render`、`write_files`、`pull_image`、`remove_*_container`、`create_container`、`start_container`、`inspect` 等阶段；每阶段失败都更新对应 target，成功实例继续保留，部分失败时 operation 状态为 `partially_deployed`。
- 应用 deploy/stop/restart/logs/runtime status 等依赖 agent 的远端调用只在目标服务器存在 `agent.url` 且 `agent.status=compatible` 时执行；agent 未部署、异常、不兼容或无法部署时不得发起 agent runtime 调用，也不得回退 SSH。部署类 lifecycle operation 仍必须为选中目标记录 failed target，避免配置目标在运行时视图中消失；运行时状态刷新遇到 agent 未就绪时只返回数据库中的已知状态，不发起远端调用。
- 应用运行时部署、停止、重启、状态刷新和日志读取遇到 agent mTLS server 证书过期或尚未生效时，必须交给服务器模块标记 Agent 状态并按受限自动重装策略处理；当前应用操作仍按原始 agent 错误失败，避免在证书未修复前继续误操作。部署 target 失败原因必须写入 lifecycle target，供运行时视图展示。
- Application 容器使用 `panel.application.*` Label 标识；设施应用的反向代理 nginx 容器也复用 runtime 原子能力创建，但其配置来源和生命周期归 `internal/modules/facilityapps` 管理。
- Application 容器创建时不得向 Docker Engine 下发 `RestartPolicy`；appspec 默认 `restart.policy=no`，应用编辑器不再主动输出 `restart` 块，容器长期重启、停止和重部署只由 Panel 的任务、协调和生命周期流程管理。
- Application 部署、停止、重启和镜像更新后的容器重建与普通容器操作共享目标服务器的单队列。
- containers 模块注册的周期协调任务只处理已经观察到新托管 Label 的实例；发现缺失、停止或 generation/spec hash 偏差时，由 `application_reconcile` collector 创建对应 app/server 的 `application_deploy` 输入，部署 handler 不再重新决定目标。显式协调 payload 支持按 `applicationIds`、`serverIds` 过滤；`force=true` 时跳过漂移判断并直接为过滤后的目标产出部署输入。设施应用可以通过应用模块的 facility runtime provider 提供每台服务器的 runtime spec；目标拆分、lifecycle、队列和 agent 原子部署步骤仍复用 `application_deploy`。collector 收集为空时不创建任务记录。同一应用连续协调失败后必须按应用级指数退避设置下一次运行时间，退避状态保存在 `application_reconcile_states`，自动协调只使用这一套连续失败计数器，不叠加任务自身 retry 计数；连续 5 轮观测到该应用全部托管实例正常后，才清空协调失败计数。
- `application_deploy` 任务表示 Panel 已完成一次部署请求和实例记录更新，不等于容器长期健康；实际容器健康必须通过运行时面板刷新展示。
- 应用列表接口会刷新已记录实例的运行时状态，并合并最近 lifecycle operation targets 后聚合为 `runtimeStatus`；左侧 `AppSelectorPanel` 只展示应用名称、运行状态和更新时间，jobId、namespace、generation、lastEval、specHash、persistentPath 等诊断字段放在右侧详情。运行中的应用存在镜像更新时，选择器状态 Chip 使用 warning 色并显示“运行中 · 有更新”，其他运行状态保持原有展示。
- 应用页面在桌面端是满高主从工作区，左侧选择器内部滚动并将分页固定在底部；编辑、部署、停止、重启和删除操作位于右侧详情标题区，不放在选择行中。
- 应用页面不展示应用总数、已启用和需要关注摘要卡，页面级提示后直接进入主从工作区。
- 应用右侧详情使用单张满高 outlined 卡片：运行状态和启用状态位于标题下方，操作按钮单独位于头部右侧；可滚动正文按基本信息、镜像更新和运行实例分区。下载包、持久化数据、迁移和删除收进更多菜单，不再把运行时面板渲染为独立并列卡片。
- 应用停止会更新应用为 disabled，并对当前实例调用 agent runtime stop；停止必须删除容器以释放端口和容器名，但保留应用托管文件与 persistent 数据。删除应用、从 `selected` 部署目标中移除服务器、迁移来源实例时才使用清理模式删除对应运行数据；删除应用会清理整个应用运行目录，包含 persistent 数据。
- 应用保存、停止、删除、部署、镜像更新等需要刷新设施反向代理时，只触发 `application_reconcile` 周期任务并指定隐藏应用 `facility-reverse-proxy`，不得在当前请求内同步执行远端 Docker 操作；协调任务中的反向代理 runtime 错误仍必须包装为 `application_runtime_operation_failed` 并保留原始 Agent/Docker 诊断。设施反向代理重建前必须清理旧 `panel-facility-reverse-proxy` 容器，避免同名容器导致后续创建冲突。
- 应用日志按 `instanceId` 和可选 `containerName` 读取。日志必须从 runtime 实例提供入口并在弹窗中展示，不再使用 allocation/task 语义；tail 行数最大为 10000。运行时实例响应同时返回 `serverId`、`serverName` 和 lifecycle `stage`，前端优先展示服务器名称，并保留 ID 作为辅助信息；没有容器的 pending/failed target 不提供日志入口。
- 模板目录提供 `app.id`、`app.name`、`app.namespace`、`app.generation` 等应用变量，可用于 appspec YAML 和应用文件模板。
- 模板目录提供 `server.id`、`server.name`、`server.host`、`server.ssh_host`、`server.ssh_port`、`server.ssh_username`、`server.variables.<key>` 等节点变量；appspec YAML 中的节点差异仍通过 `${node.meta.panel_*}` 和容器内 `PANEL_SERVER_*` 环境变量表达，应用文件模板在部署到每台目标服务器前会用实际目标服务器上下文重新渲染，因此同一应用在不同服务器会得到不同文件内容。
- `panel_file` 挂载通过应用侧内部文件 registry 分发，来源模块必须在装配阶段显式注册自己的 source scheme；当前注册 `key_asset:<asset-id>:<kind>`（用户域自签 CA/TLS 与 SSH 密钥资产）和 `certificate:<cert-id>:certificate|private_key`（已签发的域名 HTTPS 证书）。
- 内部文件读取接口使用 `OpenInternalFile` 流式打开，registry 只负责按 source scheme 分发；当前 agent managed-file 契约仍在应用部署组装阶段读取为内存内容后下发。
- 系统内置 Agent CA、Panel Agent 客户端证书和 Agent 服务端证书不注册为应用内部文件来源，即使底层作为系统托管资产保存，也不能作为应用文件挂载。
- 应用内置变量同样通过应用侧变量 registry 注册，来源模块按根 key 注册变量集合；当前证书模块注册 `certs`，模板仍通过 `.certs.<variable>` 读取。
- 私钥内容不通过目录 API 返回，只在部署渲染时由后端解密并作为只读 managed file 下发给 agent。
- 密钥资产服务扫描应用 spec 和反向代理域名，返回精确的应用 ID、名称及 `panel_file` / `reverse_proxy` 引用，用于删除保护和导入覆盖确认。
- 证书续签、密钥资产重新签发、SSH 密钥重新生成和批量导入会调用 `RedeployEnabledApplications`，确保每台服务器重新按自身变量渲染。

## Application Editor Command Fields

- `ApplicationEditor.vue` 的可视化编辑只维护 appspec `command` 有序数组。每一行是一个 argv 项，编辑器不得按空格拆分用户输入。
- `command` 表示完整容器命令数组，包含可执行文件、flag 和参数值；运行时写入 Docker `Cmd`，不得翻译成 `Entrypoint`。
- 后端 appspec 校验允许多个非空 `command` 项，空 command 项会正规化为未设置，避免不填写 command 时阻塞保存。
- 应用编辑器包含可视化和 YAML 两个标签页。可视化页是单页分区表单：标准短字段使用双列网格，端口映射和挂载行保持全宽重复行，便于阅读密集网络和存储设置。
- 应用编辑器可视化页必须往返保存 appspec `capAdd` 列表；输入项保存时按 Docker capability 稳定值大写化，不保存翻译文案。
- 应用编辑器的可视化挂载行必须展示并往返保存 `persistent` 挂载的 `uid`、`gid` 和 `mode`；`file`、`panel_file` 等 Panel managed file 挂载在可视化页显示为只读且不可取消。
- 应用编辑器弹窗在桌面端可使用较宽布局承载密集表单；端口、挂载和反向代理等复杂重复行在中等宽度下必须提前折叠为单列，避免多个字段、说明文本和操作按钮被挤在一行。
- `mounts` / `volumes` 属于 appspec YAML，必须支持 YAML 编辑；可视化页也要继续提供挂载编辑入口并与 YAML 往返同步。应用文件模板是应用级文件内容，不属于 appspec YAML，不能混入 YAML 编辑。
- YAML 标签页只编辑 appspec YAML；应用名称、启用状态、部署目标、反向代理规则、变量和应用文件是应用级保存字段，必须作为两个标签页共享的表单区展示，不能只出现在可视化页。
- 前端 appspec YAML 解析和输出使用标准 YAML 库，不能再在组件内手写轻量 parser。`command` 中以冒号开头或包含冒号的值（例如 `:9443`、`--listen=:9443`）必须按字符串往返。
- 应用部署是可重放应用任务，`application_deploy` 由应用模块注册 executor、`run-now` 和 `retry` 能力；HTTP 部署入口和任务 executor 共用当前应用快照刷新、部署校验、启用应用和 runtime deploy 准备逻辑。部署目标超过一个时，入口应通过 `tasks.Manager.CreateBatch` / `CreateBatchAndRun` 创建同一操作下的目标级部署任务，并按注册定义决定串行或并行执行；应用生命周期并发策略当前要求同一应用串行部署，因此应用部署 batch 使用 `ExecutionModeSerial`。部署开始前先创建一个覆盖全部目标的 `application_lifecycle_operations` 聚合，并把 lifecycle operation ID 写入每个目标任务参数，子任务只更新自己的 target，最后一个完成的子任务负责把聚合状态收敛为 deployed、failed 或 partially_deployed。`application_deploy` 任务参数可带 `action=stop`，用于同一部署 handler 停止/清理单个目标服务器上的应用实例，不新增设施专用 executor。
- 镜像更新检查是可重放应用任务，`application_image_check` 由应用模块注册 executor、`run-now` 和 `retry` 能力；应用详情只展示最近自动检查结果和手动“更新”动作，不再提供手动检查入口。
- 应用详情的镜像更新状态必须聚合已部署实例所在服务器的 `image_updates` 结果；只要任一实例服务器对应镜像有更新，应用 DTO 的 `imageUpdateAvailable` 即为 true，并通过 `imageUpdateTargets` 返回节点级本地摘要、最新摘要、检查时间和错误。应用镜像更新成功后需要把对应节点镜像检查缓存标记为已更新，避免旧缓存让详情继续显示可更新。
- 应用停止是可重放应用任务，`application_stop` 由应用模块注册 executor、`run-now` 和 `retry` 能力；HTTP 停止入口会把 `purge` 写入任务参数，executor 解析参数后复用 runtime stop 流程并完成传入任务。
- 应用重启是可重放应用任务，`application_restart` 由应用模块注册 executor、`run-now` 和 `retry` 能力；executor 复用现有 runtime restart 流程并完成传入任务。
- 应用刷新是可重放应用任务，`application_refresh` 由应用模块注册 executor、`run-now` 和 `retry` 能力；批量刷新和任务 executor 共用单应用刷新准备逻辑，只对启用且渲染 hash 变化的应用重新部署，任务 executor 在无变化时也会完成传入任务。
- 应用镜像更新是可重放应用任务，`application_image_update` 由应用模块注册 executor、`run-now` 和 `retry` 能力；HTTP 更新入口和任务 executor 共用镜像解析、digest 状态写入、revision 记录和 runtime redeploy 准备逻辑。
- 应用部署、停止、重启、刷新和镜像更新任务共享应用级 lifecycle 并发 key，同一应用同一时间不得并行运行多个生命周期操作；不同应用仍可并行。
- 新部署 Application 容器名使用 `panel-<application-name>`；停止、重启、状态和日志操作必须使用 `application_instances.container_name`。agent 必须与 Panel 构建版本完全一致才能被视为兼容；部署流程使用当前 agent 原子接口。

## 验证

- 后端应用或 appspec 改动运行 `task test:backend`，重点关注 `internal/modules/applications`、`internal/modules/applications/spec` 和 agent runtime 相关测试。
- 前端应用页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 appspec 字段、应用持久化字段、API、应用文件行为、部署流程、镜像更新逻辑、运行时展示字段或 agent runtime 契约时，必须更新本文档。

## Application Folder Archives

- Application save sessions support `POST /api/v1/application-save-sessions/{id}/files/archive` for multipart folder archive uploads. This endpoint is used only when the user explicitly chooses the folder archive mode; ordinary single-file uploads continue to use the `/files` JSON/base64 endpoint and must not be unpacked.
- Folder archives support zip, tar, tar.gz, and tgz. The backend validates archive paths so entries cannot escape the application workspace, then expands each entry as `basePath + archive relative path` into `application_files`.
- Extracted entries keep normal application-file semantics and can be mounted with appspec `mounts.type=file`. This feature does not introduce a new mount type.

## Managed Facility Application Identity

- Facility applications can reserve hidden application identities for lifecycle records. The reverse proxy facility app uses `facility-reverse-proxy`; it exists so `application_lifecycle_operations` and `application_lifecycle_targets` keep their normal foreign-key and query semantics.
- `applications.Service.List` and normal application pages must filter this identity out. Facility pages should read their own config endpoint and render the embedded lifecycle operation instead of exposing the managed identity as a user-editable application.
