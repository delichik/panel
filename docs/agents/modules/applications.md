# 应用模块

## List API Contract

- `GET /api/v1/applications` returns `ListPage<ApplicationSummary>` and accepts only `page`, `pageSize`, and `q`.
- The summary query reads list columns only, limits runtime aggregation to current-page IDs, and never parses application YAML/JSON or contacts nodes and registries.
- The summary includes `instanceCount` aggregated from the current-page `application_instances` rows in the same batch query, so the left list does not issue per-application runtime calls.
- Complete application and runtime data is loaded by separate ID-based endpoints.

## 适用场景

修改应用创建、编辑、appspec、模板变量解析、应用文件、编辑会话、修订、部署、停止、重启、日志、运行时状态、镜像更新或应用反向代理字段时，先读本文档。

## 后端入口

- 应用服务与 handler：`internal/modules/applications/`；HTTP 路由在 `routes.go` 注册。
- 应用文件 CRUD、文件读取和运行时 managed-file 挂载：`internal/modules/applications/files.go`
- 编辑会话与临时文件生命周期：`internal/modules/applications/edit_session.go`
- 应用规格模型、校验和渲染：`internal/modules/applications/spec/`
- 运行时规格类型：`internal/modules/applications/runtime/`
- Agent 协议与运行时客户端接口：`internal/agent/contract/`、`internal/agent/client/`
- 模板渲染接口：`internal/platform/templating/`
- 任务记录：`internal/modules/tasks/`
- 服务器能力：`internal/modules/servers/`
- 跨模块连接：`internal/bootstrap/panel/app.go`

## 前端入口

- 应用 + 设施应用页面族（v4 阶段 5）：`web/src/views/applications/index.vue`；`/applications/apps` 与 `/applications/facility-apps` 是菜单中的独立入口，不使用顶层互切 tabs。
- 页面派生逻辑和草稿校验：`web/src/views/applications/model.ts`
- API：`web/src/api/applications.ts`
- 设施应用 API：`web/src/api/facilityApps.ts`
- 类型：`web/src/types/applications.ts`、`web/src/types/facilityApps.ts`
- Mock：`web/src/mocks/applications.ts`，由 `web/src/mocks/browser.ts` 挂载同名正式路径。
- 操作记录入口：应用详情跳转 `/application-operations?applicationId=<id>`，由应用模块协调记录接口（按 intent_id 聚合 AppDB jobs）展示用户操作和系统协调记录。

## API 范围

- 应用列表/详情/删除：`GET /api/v1/applications` 返回 `ApplicationSummary[]` 轻量列表，`GET/DELETE /api/v1/applications/{id}` 提供完整详情与删除；创建和更新统一通过 durable edit-session 提交。
- 应用文件：`GET /api/v1/applications/{id}/files` 列表，`GET /api/v1/applications/{id}/files/{name}/content` 认证流式下载；`name` 是应用内唯一外部身份，物理 file id 只用于存储和运行时分配。
- 编辑会话校验：`POST /api/v1/application-edit-sessions/{id}/validate`，`POST /api/v1/application-edit-sessions/{id}/preview`
- 运行操作：`POST /api/v1/applications/{id}/deploy`，`POST /api/v1/applications/{id}/stop`，`POST /api/v1/applications/{id}/restart`
- 镜像：`POST /api/v1/applications/{id}/image/update`；手动检查入口 `POST /api/v1/applications/{id}/image/check` 已移除，检查由 `application_image_check` 任务驱动
- 运行时和日志：`GET /api/v1/applications/{id}/runtime`，`GET /api/v1/applications/{id}/logs`
- 持久化数据：`GET /api/v1/applications/{id}/persistent-data` 下载，`POST /api/v1/applications/{id}/persistent-data` 上传 zip 覆盖并重启
- 前端 v4 应用页已将持久化数据下载接为 blob 下载，将恢复接为 multipart 上传。应用 edit-session 文本写入走 JSON `PUT /files/{name}`，普通二进制走 multipart `PUT /uploads/{name}`，文件夹归档走 multipart `POST /archives`；三者都可通过 `/files/{name}/content` 下载草稿正文。旧 file key 仅作为服务端兼容输入，不属于新的外部身份。

## 校验错误

- 应用保存、计划、部署、迁移、刷新、镜像更新和重部署流程遇到 appspec/YAML 校验失败时，不能只返回泛化的 `application_invalid` 文案。API 错误响应保持 `code=application_invalid`，`error.message` 显示第一条 `<field>: <message>`，并在 `error.details.issues` 返回完整 `{ field, message }` 列表，字段结构与编辑会话 `/validate` 响应一致；`message` 和每条 issue 的 `message` 必须按当前语言翻译，`field` 保持稳定路径用于定位。

- 编辑会话 `/validate` 与 `/preview` 响应的 `diagnostics` 必须是数组（可为空数组），后端不得把 Go 的 nil 切片序列化成 JSON `null`（设施校验无问题时返回空切片）；前端赋值时也会把 `null` 归一化为空数组，避免摘要面板读取 `diagnostics.length` 崩溃。
## 数据与行为约定

- 主要业务表包括 `applications`、`application_files`、`application_instances`、`application_revisions` 和 `jobs`，新的应用控制面统一保存在 `Store.AppDB()`。`application_revisions` 与 `jobs` 是部署事实的一部分；旧 `application_lifecycle_operations`、`application_lifecycle_targets`、`application_target_stages` 三张 CoordDB 表已通过迁移步骤 DROP TABLE IF EXISTS 删除，协调库 `Store.CoordDB()` 不再注册任何模型（`CoordinationModels` 返回空），生产 planner 只写 AppDB `application_revisions` 与 `jobs`。
- 启动迁移会把旧 LogDB `application_revisions` 的不可变快照幂等复制到 AppDB；新 revision 写入只落 AppDB。旧 LogDB 表仍可供兼容诊断读取，不能再作为 worker 的 revision 来源。
- 应用层采用轻量控制平面模型：`applications` 保存 desired state，`kind=application` 表示普通用户应用，`kind=facility_application` 表示设施应用投影出的隐藏受控应用；`deletion_requested=1` 表示删除期望已提交，普通列表隐藏该应用并由协调器清理运行时资源。业务 HTTP 入口不得直接部署、停止或清理远端容器，只能校验、保存 desired state 并触发 `application_reconcile`。
- appspec 以 YAML 输入，经 `internal/modules/applications/spec/` 校验并渲染为 `appruntime.Spec`；部署时由 Panel 选择目标服务器并编排运行时步骤，再通过目标机 `panel-agent` 的原子接口写入托管文件、拉取镜像、删除旧容器、创建容器、启动容器和刷新状态。
- appspec 的 `resources.cpu` 和 `resources.memoryMb` 只有设置为正数时才表示运行时限制；字段缺省或显式为 `0` 都表示不限制，不得在规范化、渲染或部署流程中自动补默认 CPU/内存限制。
- appspec 支持 `capAdd` 字符串数组，对应 Docker `--cap-add` / `HostConfig.CapAdd`；规范化时去空、去重并转为大写。`capAdd` 只在用户显式配置时下发，可与 `privileged: true` 并存，当前不提供 `capDrop`。
- `application_instances` 是 Panel 的当前运行时事实表，按 `application_id + server_id` 记录 desired 与 observed 两组字段：desired 包括状态、generation、spec hash、revision 和安全的 runtime spec 快照；observed 包括状态、容器身份、generation、spec hash、镜像 digest、时间、sequence、source 和最后一次协调错误。业务模块只能读取 observed；所有 Agent report、周期巡检和 `RuntimeReconcile` 回写统一通过 `internal/orchestrator.ObservationWriter`。
- `jobs` 是每个 application/server 冲突域当前唯一的 durable 协调工作。`pending/running/failed_retryable` 通过 AppDB partial unique index 保证同键最多一条；Job 保存 action、revision、lease token、execution ID、attempt/backoff、步骤和结构化错误。任务表只做用户操作/触发记录，不是部署事实来源。
- 应用部署控制面的入口是 `Service.PlanApplicationDeployment` → `orchestrator.Planner` → `orchestrator.Controller`。planner 只保存 desired、不可变 revision 和 Job，不直接执行 Agent/Docker RPC；controller 负责 scanner、claim、lease heartbeat、RuntimeReconcile、ObservationWriter 写回、fencing 和重试。提交后只发送 wake，wake 丢失由 DB due scan 修复。
- 停止、禁用和删除分别规划 `stop` / `purge` Job；删除使用 finalizer：先设置 `deletion_requested=true`，所有现有 instance 变为 `desired_state=purged`，每个 purge Job 成功并写入 observed `missing` 后删除 instance，最后在无 instance 时物理删除应用；物理删除前会先清理该应用的终态 Job（succeeded/failed/cancelled），因为 `jobs` 对 `applications` 使用 RESTRICT 外键（设计上禁止级联删除 Job），不清理会导致 finalizer 无法删应用。不得通过应用删除级联绕过 active Job。
- `ApplicationInstance.status`、`runtime_spec_json`、`last_deployed_generation` 等旧列仅作为兼容投影；新代码读取 desired/observed 字段，旧投影由 ObservationWriter 在需要时同步，不能反向覆盖控制面事实。
- 默认部署模式为 `all`，会在所有 agent 健康且兼容的服务器上各创建一个实例；`selected` 只部署到选中的服务器。含 `persistent` 挂载的应用必须且只能部署到一个服务器；已有运行时实例后，可通过实例所在服务器的 agent 将 `/opt/panel/apps/<applicationId>/persistent` 打包下载，或上传 zip 由 agent 校验路径后全量覆盖该目录并触发应用重启。尚未部署的 `persistent` 应用允许先向选定服务器导入 zip，agent 会创建并覆盖 Panel 托管的 persistent 目录，导入完成后不触发重启，用于从 compose 等外部运行方式迁移数据后再首次启动。
- 删除服务器会通过服务器模块修剪应用 `deployment_server_ids_json` 中的对应 ID；该修剪属于用户配置变化，必须同时递增应用 `version` 和配置 `updated_at`。数据库外键会级联删除该服务器上的 `application_instances` 和协调状态；如果 `selected` 应用因此没有部署目标，后续部署/计划应保持校验失败，直到用户重新选择目标服务器。
- 不含 `persistent`、host/bind 挂载和 Docker volume 挂载，且当前只有一个来源运行实例的应用可执行无损迁移。迁移要求来源实例正在运行、目标服务器 agent 兼容且没有该应用实例；Panel 将部署目标切换为目标服务器并部署新实例，成功后删除来源服务器上的容器和该实例运行目录，并移除来源 `application_instances` 记录。
- `persistent` 挂载支持在单条 mount 上配置 `uid`、`gid` 和目录 `mode`（如 `"0755"`），部署写入托管文件阶段会确保对应宿主机持久化子目录存在并应用权限，解决非 root 容器进程写入持久化目录的问题。`file` 和 `panel_file` 挂载也支持 `uid`/`gid`，用于 Panel 写入节点 runtime 文件后的 chown；`file` 挂载额外支持 `mode`，前端可视化编辑只暴露“可执行”开关并写为 `0755`。Agent 写入普通 managed file 后必须显式 chmod 到解析后的 mode，确保重新部署能覆盖既有文件权限；普通 managed file 的父目录固定为 `0755`，保证以非 root 用户运行的容器（如 Nginx worker）可以进入挂载目录读取文件，文件本身的 mode 仍由 managed file 控制。`panel_file` 不允许用户配置 mode，其权限来自内部文件来源。`readOnly` 只表示 Docker bind mount 的 `:ro`，不等同于节点文件权限。权限字段不得用于 host/global bind 或 Docker volume，避免 Panel 修改用户自管宿主机目录或 Docker 管理卷。
- 应用不保存应用级自定义变量；部署模式、反向代理配置等持久化字段必须保存稳定结构，不保存已翻译展示文案。容器环境变量属于 AppSpec YAML 的 `env` 字段。
- 应用持久化目录不保存为数据库字段；是否启用 persistent 由 appspec 的 `mounts.type=persistent` 派生，详情响应里的 `persistentPath` 只读生成自 `applicationPersistentDir(app.ID)`，保存 DTO 不包含 `persistentPath`。
- `mounts.type=storage_share` 是共享存储设施挂载：来源为 `storage-share:<存储服务器ID>`（兼容旧值 `storage-share` = 配置的第一台存储服务器，每台存储服务器各自指定根目录），目标为容器内绝对路径；校验不接受 `uid`/`gid`/`mode`。渲染阶段先透传为运行时 `storage_share` 挂载，`runtimeSpecForServer` 通过注入的 `StorageShareResolver`（由 `internal/modules/facilityapps` 实现）解析为 `nfs` 挂载并登记分区记录；只有挂载列表包含 `storage_share` 的应用才会受设施配置影响，设施未配置时这类应用部署校验失败。存储服务器的导出配置、分区打包下载与目录删除由 Agent 执行（`StorageConfigureExport` / `StorageArchiveDirectory` / `StorageDeleteDirectory`）。应用停止/删除只释放引用，不删除 NFS 侧数据。
- 应用 AppSpec 不再提供或保存 `networkMode`；所有应用容器固定加入受管 `panel-apps` bridge 网络。应用 `reverseProxy` 字段只描述域名、Path、目标端口和结构化 Path 选项，不提供目标类型选择。实际生效范围由容器化中的“设施应用 / 反向代理”部署服务器决定：只有设施应用覆盖且应用实际部署到的服务器才会接收对应反向代理配置，并始终代理到同节点应用容器名和目标端口。保存路径与启动迁移都会清理旧 `networkMode` 和路由 `targetType` 数据。
- 应用代理由入口网关使用 `$panel_proxy_upstream` 变量在请求时解析同节点应用容器名，不要求目标容器在 Nginx 启动或重载时已经存在；目标容器恢复后入口网关自动重新连通，不需要人工再次同步。
- 应用反向代理上游暂时不可达时，入口网关展示 Seamark 风格统一提示页：标题左侧带红色断开图标，底部为品牌图标和名称；页面按浏览器语言显示对应文案，说明服务暂时无法连接并请稍后重试，不暴露 Nginx 或错误码等技术信息。
- 入口代理保存后会自动联动 DNS：当前配置的全部域名（含 Panel 访问入口域名和已删除域名）进入异步 `dns_proxy_records_sync` 任务检查生效状态；已生效且未变化的记录只比对不写入，只有与期望记录不一致时才创建/更新/删除。域名 A/AAAA 记录指向其生效服务器列表（anyAccess 关闭时用 `originServerIds`，开启时用全部网关节点），服务器未配置 IPv4/IPv6 时跳过该域名并标记提示。每域名同步状态（pending/synced/failed/skipped）持久化在 `facility_app_configs.dns_sync_json` 并通过 `ReverseProxyConfig.dnsSync` 返回。
- `ReverseProxyPath.options` 与设施 Path 共用 `HTTPRouteOptions`：`gzipMode`、`clientMaxBodySizeMb`、连接/读取/发送超时、`bufferingMode`、`webSocketMode`、请求 Header 和响应 Header。旧 `webSocket=true/false` 必须映射到结构化模式并保留兼容；规范化、模板渲染、`ApplicationReverseProxyConfigs` 转换和 spec hash 不得丢失 options。Header 名称按 HTTP token 校验并大小写不敏感判重，值禁止 CR/LF/NUL 和 Nginx 变量注入。所有应用代理 location 必须明确生成 `proxy_cache off;`；Panel 不主动生成、覆盖或隐藏 `Cache-Control`、`Expires`、ETag 等客户端缓存 Header。
- 应用文件列表不返回正文；模板编辑可用 `GET .../files/{name}` 的 JSON/base64 读取，用户下载必须使用 session 或 committed `/content` 接口，不把 base64 当下载协议。JSON PUT 只允许 `template`，普通 `binary` 必须经 64 MiB multipart 入口上传，MIME 由服务端按名称/内容推断。每个应用文件在应用范围内使用唯一 `name`，它是不透明的应用内身份，不代表反向代理 URL Path 或宿主机路径；旧版本的 path/file key 只在兼容层解析并原样迁移。`archive` 保存原始文件夹压缩包为一个稳定的应用内 name 条目，不在管理端展开；替换必须复用原 name。
- `file`、`panel_file`、内部文件和模板渲染后的 managed file 统一以只读 `managed_file` 挂载到容器。archive 在应用 Panel、设施 bundle Panel 和 Agent 实际解包侧都限制最多 10000 个总条目（文件与目录均计数）、32 层目录、256 MiB 解包总量和 100 倍压缩比，并继续拒绝路径逃逸和不支持格式；Agent 保存原始压缩包副本并校验 sha256，再覆盖解包到挂载目录。普通文件 drift 检查 sha256、mode 和显式 uid/gid，archive drift 检查节点保留原包 sha256 和解包目录 tree hash；drift 检查先用 stat 指纹（size+mtime）快速跳过未变化的文件，指纹缓存在 Agent 本地 `state/managed-files.fingerprint.json`，只有元数据变化时才重新计算 sha256 / tree hash。Agent 解包采用单遍流式写盘（边写边算 sha256），不再把解压内容整包读入内存。
- 保存会话服务层的临时目录仍由应用装配层设置到 `<dataRoot>/tmp/application-save-sessions`；该基础目录也用于推导 durable edit-session 工作目录，不得依赖进程工作目录下的相对 `tmp`。
- 编辑会话的临时文件生命周期集中在 `edit_session.go`（归档解包与文件名归一辅助位于 `archive_helpers.go`）；旧的 save-session 内存会话实现与 HTTP 接口已移除，应用 CRUD、部署和运行时流程不得复制会话锁或临时目录清理逻辑。
- 启用、停用、部署同步、删除和镜像更新等流程先校验并保存 desired state，再通过 task 框架触发 `application_reconcile`；collector 只收集报告/周期输入并调用 planner，不能创建 `application_target_*` 任务或直接执行远端变更。编辑器“保存并应用”仍只提交编辑会话，部署完成通过 Job/runtime 视图观察。
- planner 按完整目标集合规划 apply 与 removed-target purge；每台服务器写入一个 Instance desired 和一个可合并 Job。`force` 只能绕过满足态过滤和应用级退避，不能绕过 application/server active Job 唯一性；running Job 不会被第二个 Job 并发覆盖，执行结束会重新比较最新 desired。
- Controller 在 AppDB 外调用 Agent `RuntimeReconcile`，按 lease token 保护 observation、Job succeed/fail 和 desired-change requeue；RPC deadline 小于 lease TTL，同时由 heartbeat 延长合法长任务。单个服务器失败只影响该 Job，其它服务器继续由 scanner/worker 收敛。
- Agent 只负责 RuntimeReconcile 的幂等 ensure：校验 managed identity，写托管文件、确保镜像、检查/复用或替换托管容器、启动并确认 running；非托管同名资源必须返回 terminal conflict，不能删除。stop/purge 对缺失或已达到目标状态按成功处理。
- 应用运行时错误写入 Job 的 `error_code/error_class/error_message/error_detail` 和 `last_steps_json`，可重试错误按 30 秒起、上限 1 小时并带 jitter 退避；每个 Job 有最大尝试次数（`orchestrator.ControllerConfig.MaxAttempts`，默认 10 次、含首次），达到上限后以 `error_code=max_attempts_exceeded` 进入终态失败，不再无限重试；任务表不能反过来覆盖 Job 状态。
- 应用同步的 `pull image` 步骤允许最长 15 分钟，以适配较慢的镜像仓库或大镜像下载；未显式写 tag 的镜像引用必须按 Docker CLI 语义拉取 `latest`，不得触发 Docker Engine API 拉取仓库全部标签；其它 agent/runtime 操作仍使用常规短超时。
- RuntimeReconcile 的 apply 步骤为 `validate_spec`、`write_files`、`ensure_image`、`inspect/reuse_or_replace`、`create`、`start`、`verify_running`；Agent 不暴露胖部署 handler。应用 runtime 调用只在 `agent.url` 存在且 `agent.status=compatible` 时执行，不回退 SSH；Agent 未就绪的 Job 保持可重试并等待服务器修复。
- 应用运行时状态读取优先使用 AppDB observed 快照；详情页需要主动刷新时才调用 Agent status，刷新结果仍必须经 ObservationWriter，不得由 handler 直接写 Instance。证书/Docker/网络错误保留原始结构化诊断并按 Job retry policy 收敛。
- Application 容器使用 `panel.application.*` Label 标识；设施应用的反向代理 nginx 容器也复用 runtime 原子能力创建，但其配置来源和生命周期归 `internal/modules/facilityapps` 管理。
- 设施应用（入口网关）的运行时 spec 使用应用级 SpecHash：`RuntimeSpecForServer` 返回前把 `spec.SpecHash` 覆盖为 `app.SpecHash`（非空时）。每台服务器的渲染内容只体现在 managed file 层，Job 的 desired spec 保存该服务器实际渲染快照，避免期望 hash、部署校验与容器漂移检测互相不一致。
- 设施应用的 `generation`/`spec_hash` 只允许设施模块写入（`ensureReverseProxyApplication`，hash 为 `facilityConfigHash`）：`refreshApplicationSnapshot`/`prepareDeploy` 等应用侧路径对 `kind=facility_application` 必须原样返回、不得用 `applicationHash` 覆盖 SpecHash 或据此递增 generation。设施的 per-server runtime spec 作为该 Job 的 desired spec 快照保存，不能再写 lifecycle target。
- Application 容器创建时不得向 Docker Engine 下发 `RestartPolicy`；appspec 默认 `restart.policy=no`，应用编辑器不再主动输出 `restart` 块，容器长期重启、停止和重部署只由 Panel 的任务、协调和生命周期流程管理。
- Application 部署、停止、重启和镜像更新后的容器重建与普通容器操作共享目标服务器的单队列。
- 同步校验阶段发现 spec hash 与期望不一致时，Job 按 `failed_retryable` 进入指数退避重试，而不是终态失败；controller 到期恢复后重新执行 RuntimeReconcile，协调器扫描仍然作为兜底恢复路径。失败汇总文案使用服务器当前名称并隐藏原始 hash，hash 只保留在 Job/Instance 诊断字段。
- 自动协调连续失败达到 10 次后，应用置为 `reconcile_stopped` 特殊状态并停止自动重试：调度器扫描与 agent 上报的漂移协调不再创建新部署；用户显式同步、部署、重启等人工操作会清除该状态并重新开始计数。失败计数由 orchestrator Controller 的 `OnFailed` 回调在每次 Job 失败（含可重试与终态）时累计到 `application_reconcile_states.reconcile_failures`，并在未达到阈值期间以指数退避控制下次扫描。前端在应用列表与详情将该状态展示为“需人工处理”。标记/清除 `reconcile_stopped` 是派生状态变更，不得写 `applications.updated_at`，避免协调动作污染配置更新时间。
- containers 模块注册的周期协调任务只处理已经观察到新托管 Label 的实例；发现缺失、停止、generation/spec hash 偏差或 managed file manifest 漂移时，由 `application_reconcile` collector 请求应用 planner 创建或复用对应 app/server 的 Job，collector 本身不再产出 `application_target_apply|stop|purge` 输入。collector 必须在内部规划请求中标记这些节点已经由 Agent 观测到运行时漂移；planner 对这组明确节点不得再使用可能滞后的 running 缓存做满足态过滤，否则 Docker 服务重启或节点侧停止容器后会漏掉自动恢复。显式协调 payload 支持按 `applicationIds`、`serverIds` 过滤；默认同步只为未满足 desired state 的目标规划 Job，已经 running 且 generation/spec hash 与 managed files 都匹配的成功节点不得因为其他节点失败而重复部署；配置保存、停用、删除、设施应用保存和系统级重部署等 desired state 变更可以使用 `force=true` 绕过退避和满足态过滤，但不能绕过 `uq_jobs_active_app_server`。设施应用可以通过应用模块的 facility runtime provider 提供每台服务器的 runtime spec；目标拆分和 Agent RuntimeReconcile 仍复用应用控制面。collector 收集为空或只完成规划时不创建任务记录。同一应用连续协调失败后必须按应用级指数退避设置下一次运行时间，退避状态保存在 `application_reconcile_states`，自动协调和非强制显式协调必须尊重 `reconcile_next_run_at`。
- 设施 runtime provider 可额外实现逐次更新规划。默认和未知结果均为 recreate；只有设施为当前新旧 spec 明确返回 reload strategy，且应用层确认镜像、命令、环境、网络、端口、挂载、权限、资源和 restart 等容器结构完全一致时，才调用 Agent `RuntimeReload`。validate 失败保留旧容器并使 target 失败；reload 或 reload 后状态确认失败时在同一服务器操作队列内回退现有 recreate 流程。
- Docker labels 在创建后不可修改。Agent 在 recreate 或 reload 成功后写入实例 `applied-state.json`，容器报告仅在 container ID/name 匹配时用动态 generation/spec hash 覆盖静态 labels，避免成功 reload 后被协调器误判为旧版本。
- 应用部署收敛记录不等于容器长期健康：协调记录页按 intent_id 聚合 AppDB Job（一个 intent 一条记录、一个 Job 一个目标）展示一次收敛；实际容器健康必须通过运行时面板刷新展示。
- 应用列表接口使用 `ApplicationSummary[]`，只包含首屏必要字段：`id`、`name`、`enabled`、`instanceCount`、`jobId`、`namespace`、`runtimeStatus`、`imageUpdateAvailable`、`lastError`、`updatedAt`。列表必须走专用摘要查询，只读取摘要列，并用固定数量的本地批量查询合并 AppDB Instance observed 状态、实例数量（`instanceCount`）和当前 Job 诊断；不得调用完整应用 scanner、解析 appspec/YAML/配置 JSON，也不得逐应用、逐实例或逐节点查询。`specYaml`、`reverseProxy`、`deploymentServers`、`persistentPath`、`imageUpdateTargets`、`specHash`、`generation`、`lastEvalId`、`lastDeploymentId` 等详情/诊断字段只从 `GET /api/v1/applications/{id}` 获取。实时运行时刷新留给详情页 `GET /api/v1/applications/{id}/runtime`。
- 普通应用页首屏只加载应用列表必要数据，不加载设施接口，也不预拉多个应用的 runtime；当前选中应用的 runtime 在列表可用后异步按需加载。设施目录、详情和配置页进入对应模式时再加载设施数据；直达设施 URL 必须先加载设施目录后再判断是否支持该 `facilityKind`。
- 左侧列表行的镜像与实例数量来自摘要，列表加载或刷新期间行内显示骨架动画，不使用 `jobId` 或 0 占位。摘要缺少 `imageReference` 的历史应用，前端按行异步读取详情补齐镜像，补齐范围只限当前页缺少镜像引用的行，不得扩大到全部应用，也不得改为预拉多个应用的 runtime。
- 应用页面在桌面端是满高主从布局，左侧选择器和右侧详情正文内部滚动。创建/编辑应用使用隐藏独立页和连续纵向配置流，宽屏为主体 + sticky 摘要，中屏摘要下移，窄屏恢复页面级滚动并保持提交栏可达；不得恢复左 rail、分页卡片或依赖横向滚动。部署、停止和删除操作位于详情标题或操作区，不放在选择行中。
- 应用、设施应用、创建/编辑应用中的操作入口必须复用 `AppActionButton` 和 `AppActionGroup`：详情级编辑、同步、停用、重启等位于详情标题区；挂载、反向代理、文件和路由摘要行的编辑/删除位于行尾并使用带文字的小按钮；完整编辑表单进入标准 dialog，避免在页面正文下方展开一组容易误解的操作区。
- 应用页面不展示应用总数、已启用和需要关注摘要卡，页面级提示后直接进入主从工作区。
- 应用右侧详情使用单张满高 outlined 卡片：运行状态和启用状态位于标题下方，操作按钮单独位于头部右侧；可滚动正文按基本信息、镜像更新和运行实例分区，运行时实例（节点实例）加载期间显示骨架动画。详情正文分区不得再次做成同等级 outlined/阴影/渐变卡片，只使用标题、分隔线和轻量信息单元表达层级。下载包、持久化数据、迁移和删除收进更多菜单，不再把运行时面板渲染为独立并列卡片。
- 应用停止会更新应用为 disabled 并触发协调，由协调器为现有实例创建 `action=stop` 目标任务；停止必须删除容器以释放端口和容器名，但保留应用托管文件与 persistent 数据。删除应用会设置 `deletion_requested=true` 并触发 `action=purge` 目标任务，由协调清理整个应用运行目录，包含 persistent 数据。业务保存、停止和删除请求不得同步调用 agent runtime stop。
- 应用保存、停止、删除、部署、镜像更新等需要刷新设施反向代理时，只触发 `application_reconcile` 周期任务并指定隐藏应用 `facility-reverse-proxy`，不得在当前请求内同步执行远端 Docker 操作；协调任务中的反向代理 runtime 错误仍必须包装为 `application_runtime_operation_failed` 并保留原始 Agent/Docker 诊断。设施反向代理重建前必须清理旧 `panel-facility-reverse-proxy` 容器，避免同名容器导致后续创建冲突。任意应用部署收敛成功后都会触发入口代理重新协调（让代理立刻拿到新路由），但入口代理**自身**的收敛成功必须跳过该再触发：代理自己的 apply 写入的就是最新期望配置，再触发只会形成“同步完成→立即再同步”的自循环。
- 设施反向代理应用 `facility-reverse-proxy` 的 spec hash 纳入全部已启用应用的反向代理路由（模板渲染后的路由结构），因此只修改应用的 `reverseProxy` 字段也会提升代理应用 generation 并触发代理重新部署；路由不变时协调会复用现有部署目标，不产生无效重部署。
- 应用反向代理规则与设施域名路由统一持久化在 `reverse_proxy_routes`（AppDB）：`domain` 主键全局唯一，`app_id` 标识所属应用，设施代理自身的路由使用 `facility-reverse-proxy`；`origin_server_ids`/`any_access_json`/`target_port`/`paths_json` 为统一列，应用侧保存规范化规则、读时渲染模板。旧 `applications.reverse_proxy_json` 与 `facility_app_configs.domains_json` 已由启动预迁移回填后删除。
- 设施应用的 `reverse_proxy_routes` 行是设施域名路由（由设施模块管理，`target_port` 恒为 0，不使用应用目标端口语义），应用模块的 `Get` 与 `ListForReconcile` 不得把它们加载为应用反向代理规则（`ApplicationReverseProxyConfigs` 走 `List` 本就排除设施应用）：否则设施应用的部署规划（`prepareDeploy → refreshApplicationSnapshot → normalizeReverseProxyRules`）会把 `target_port=0` 当成非法端口拒绝，报 `application_reverse_proxy_target_port_invalid`。应用反向代理规则的 `target_port` 在保存时校验必须在 1-65535，设施路由永远不经过该校验。
- 应用日志按 `instanceId` 读取，容器名由后端从实例记录解析（DTO 不再接受 `containerName`）。日志必须从 runtime 实例提供入口并在弹窗中展示，不再使用 allocation/task 语义；tail 行数最大为 10000。运行时实例响应同时返回 `serverId`、`serverName` 和运行阶段 `stage`，前端优先展示服务器名称，并保留 ID 作为辅助信息；没有容器的 pending/failed Job 不提供日志入口。
- 应用运行时实例状态支持 `missing`，表示期望存在的托管容器在目标 Docker 中已找不到。Agent runtime status 遇到 Docker not found 必须返回 `missing`，不得映射为普通 `stopped`；前端以“缺失”展示，用于区分外部删除容器和正常停止。
- 模板目录提供 `app.id`、`app.name`、`app.namespace`、`app.generation` 等内置模板变量，可用于 appspec YAML 和应用文件模板；应用不再提供应用级自定义变量。
- 已删除的应用级自定义变量不再参与模板渲染；旧模板若引用这些变量，渲染会以 missing variable 校验错误失败，需改用 `server.variables.<key>` 或内置模板变量。
- 模板目录提供 `server.id`、`server.name`、`server.host`、`server.ssh_host`、`server.ssh_port`、`server.ssh_username`、`server.variables.<key>` 等节点变量；appspec YAML 中的节点差异仍通过 `${node.meta.panel_*}` 和容器内 `PANEL_SERVER_*` 环境变量表达，应用文件模板在部署到每台目标服务器前会用实际目标服务器上下文重新渲染，因此同一应用在不同服务器会得到不同文件内容。
- `panel_file` 挂载通过应用侧内部文件 registry 分发，来源模块必须在装配阶段显式注册自己的 source scheme；当前注册 `key_asset:<asset-id>:<kind>`（用户域自签 CA/TLS 与 SSH 密钥资产）和 `certificate:<cert-id>:certificate|private_key`（已签发的域名 HTTPS 证书）。
- 内部文件读取接口使用 `OpenInternalFile` 流式打开，registry 只负责按 source scheme 分发；当前 agent managed-file 契约仍在应用部署组装阶段读取为内存内容后下发。
- 系统内置 Agent CA、Panel Agent 客户端证书和 Agent 服务端证书不注册为应用内部文件来源，即使底层作为系统托管资产保存，也不能作为应用文件挂载。
- 应用内置变量同样通过应用侧变量 registry 注册，来源模块按根 key 注册变量集合；当前证书模块注册 `certs`，模板仍通过 `.certs.<variable>` 读取。
- 私钥内容不通过目录 API 返回，只在部署渲染时由后端解密并作为只读 managed file 下发给 agent。
- 密钥资产服务扫描应用 spec 和反向代理域名，返回精确的应用 ID、名称及 `panel_file` / `reverse_proxy` 引用，用于删除保护和导入覆盖确认。
- 证书续签、密钥资产重新签发、SSH 密钥重新生成和批量导入会调用应用重部署/刷新，并独立触发设施反向代理协调；普通应用刷新失败不能阻断入口网关证书更新尝试。

## Application Editor Command Fields

- `ApplicationEditor.vue` 的可视化编辑只维护 appspec `command` 有序数组。每一行是一个 argv 项，编辑器不得按空格拆分用户输入。
- `command` 表示完整容器命令数组，包含可执行文件、flag 和参数值；运行时写入 Docker `Cmd`，不得翻译成 `Entrypoint`。
- 后端 appspec 校验允许多个非空 `command` 项，空 command 项会正规化为未设置，避免不填写 command 时阻塞保存。
- 应用编辑器使用隐藏独立 `EditorPage`：正文按基本信息、运行设置、网络与访问、环境与存储、部署目标、应用文件顺序连续展开，右侧 sticky 摘要展示保存前检查、变更数量、编辑状态和检查结果；顶部只保留表单编辑，不提供分区切换。创建页强调名称和镜像的起步配置；编辑页强调当前修改和保存结果。
- 应用编辑器的可视化草稿必须是结构化数据，不得把容器环境变量、部署服务器、端口、挂载或反向代理规则压成 JSON/多行文本作为主要交互。复杂重复项使用“摘要列表 + 新增/编辑对话框 + 删除确认”，对话框内使用独立克隆草稿，取消不得污染主草稿。
- 应用反向代理规则对话框必须通过明确 DTO 克隆函数创建独立草稿，不能对 Vue reactive Proxy 直接调用 `structuredClone`；只有点击保存才替换 `form.reverseProxy` 中对应规则，新建或编辑后取消不得留下空规则、空 Path 或高级选项修改。每个 Path 的高级字段复用 `RoutePathAdvancedFields.vue`。
- 应用反向代理规则的源站集合完全由后端计算：`源站 = 应用部署目标 ∩ 设施全局网关节点`（`selected` 模式取部署服务器，`all` 模式取全部全局网关节点）。前端不提供也不提交源站：保存请求中的 `originServerIds` 与 `anyAccess.primaryOriginServerId` 一律被忽略，当不存在处理；后端保存时重新计算并校验。对话框只读展示自动源站，无可用的源站时提示选择属于网关节点的部署目标。`primary_backup` 策略下前端只排列源站优先级 `anyAccess.originPriority`（首位即主源站，其余按序为备），主源站由后端据此推导。源站集合在保存后变化（网关或部署目标变更）时，后端对存储的优先级做**前向投影**而不是报错：仍存在的服务器保留用户排列的相对顺序，被移除的服务器丢弃，新增服务器按序追加——避免陈旧优先级卡死后续保存与自动协调。`AnyAccess` 开启后所有全局网关节点都部署域名，非源站节点通过入口网关转发到源站；`anyAccess.relayServerIds` 为空表示所有非源站全局网关节点，非空表示只在这些指定节点生成转发。转发节点必须属于全局网关节点且不能是源站节点。策略只允许 `round_robin`、`primary_backup`、`ip_hash`。
- 应用反向代理域名在设施路由、其他应用代理规则和 Panel 入口之间全局唯一；同一规则下可配置多个 Path，不允许通过多个所有者共享域名。
- 反向代理域名在保存与渲染阶段按 DNS hostname 规则校验（支持单个 `*.` 通配前缀），Path 排除 `# ? \` 等 nginx 特殊字符，校验失败返回结构化 `application_reverse_proxy_domain_invalid` / `application_reverse_proxy_path_invalid` 错误。
- Panel 访问入口启用时，入口服务器必须是已登记的 Panel 宿主节点；尚未登记时，首次保存会将该服务器登记为宿主节点，已登记后入口服务器不允许再选择其他服务器。
- 应用详情的“反向代理路由”分区只读展示源站、AnyAccess、流量策略、主源站和每个 Path 的高级设置。
- 应用编辑器可视化页必须往返保存 appspec `capAdd` 列表；输入项保存时按 Docker capability 稳定值大写化，不保存翻译文案。
- 应用编辑器的可视化挂载区在页面正文只展示挂载摘要列表：类型、来源、容器路径、只读状态以及编辑/删除动作。新增或编辑挂载必须打开对话框承载完整字段，包含 Docker 只读挂载开关，以及按类型可用的节点文件权限字段：`file` / `panel_file` / `persistent` 支持 `uid`、`gid`，`file` 支持“可执行”开关，`persistent` 支持任意 `mode`，`panel_file` 不显示 mode。
- 普通应用创建和编辑都使用隐藏独立页。文件区只提供一个上传入口，弹窗内选择文本文件、普通文件或上传文件夹压缩包；用户不填写 kind 或 MIME。文本类型进入代码编辑器，普通文件和归档类型显示文件选择控件；已存在的文本文件编辑仍复用同一弹窗，binary/archive 只有替换、下载、删除，不得出现文本编辑入口。替换保留原 `name`，操作中的 pending 和错误显示在对应文件行或上传弹窗内。
- 设施编辑会话删除静态资产时，服务端必须先检查当前 draft route 对 `assetName` 的引用；仍被引用时应在 revision、资产记录和 blob 均未变化前拒绝，前端把错误显示在对应资产行。
- 普通应用编辑页使用 `/api/v1/application-edit-sessions` durable 会话：进入时查询可恢复编辑，首次修改后懒创建会话；修改和文件操作串行携带 revision。保存主流程为本地校验 → 服务端检查 → 预览变更 → 保存并应用，提交期间禁用离开和重复提交；成功只表示配置已保存并请求应用，部署完成仍通过协调记录页（`/application-operations`）和运行时区观察。
- v3 编辑页有路由离开和浏览器关闭保护；离开默认保留可恢复草稿，取消按钮会显式 discard 当前会话后返回列表。
- `mounts` / `volumes` 属于 appspec YAML；应用编辑器只提供表单编辑，挂载通过结构化挂载区编辑，表单生成 YAML 时保留未覆盖的规格字段。应用文件模板是应用级文件内容，不属于 appspec YAML，不能混入应用规格。
- 应用文件名校验拒绝 `/`、`\`、`..` 和控制字符，长度上限 255，避免 zip-slip 与“能保存无法下载”的死数据。
- 应用名称、启用状态、部署目标、反向代理规则和应用文件是应用级保存字段，必须留在结构化面板与保存输入中。容器环境变量属于 appspec YAML 的 `env`，由表单生成。
- 前端 appspec YAML 解析和输出使用标准 YAML 库，不能再在组件内手写轻量 parser。`command` 中以冒号开头或包含冒号的值（例如 `:9443`、`--listen=:9443`）必须按字符串往返。
- 应用同步由 AppDB planner 创建/合并 per-server Job；HTTP 同步入口只保存 desired state 并触发协调。`orchestrator.Controller` 条件 claim Job 后调用 `RuntimeReconcile`，没有 `application_target_*` 任务锚点、dispatcher 或 lifecycle aggregation worker。Agent 返回已达到目标状态的 stop/purge 响应时按幂等成功处理，Job 直接收敛为 succeeded。
- `application_refresh` 和 `application_image_update` executor 只更新 desired state 并触发 `application_reconcile`，由 planner/controller 创建或复用 Job 后经 Agent `RuntimeReconcile` 执行；executor 不在任务内部直接执行 runtime apply，也不创建 `application_target_*` 任务。HTTP 保存、同步、停用、删除和设施应用保存同样只写 desired state 并触发协调。
- 镜像更新检查是可重放应用任务，`application_image_check` 由应用模块注册 executor、`run-now` 和 `retry` 能力；应用详情只展示最近自动检查结果和手动“更新”动作，不再提供手动检查入口。
- 应用详情的镜像更新状态必须聚合已部署实例所在服务器的 `image_updates` 结果；只要任一实例服务器对应镜像有更新，应用 DTO 的 `imageUpdateAvailable` 即为 true，并通过 `imageUpdateTargets` 返回节点级本地摘要、最新摘要、检查时间和错误。应用镜像更新成功后需要把对应节点镜像检查缓存标记为已更新，避免旧缓存让详情继续显示可更新。
- 应用停止是可重放应用任务，`application_stop` 由应用模块注册 executor、`run-now` 和 `retry` 能力；HTTP 停止入口会把 `purge` 写入任务参数，executor 解析参数后复用 runtime stop 流程并完成传入任务。
- 应用重启是可重放应用任务，`application_restart` 由应用模块注册 executor、`run-now` 和 `retry` 能力；executor 只强制触发应用部署 planner 创建或复用 apply Job，并在规划完成后完成传入任务，不得直接调用 Agent runtime restart。
- 应用刷新是可重放应用任务，`application_refresh` 由应用模块注册 executor、`run-now` 和 `retry` 能力；批量刷新和任务 executor 共用单应用刷新准备逻辑，只对启用且渲染 hash 变化的应用重新部署，任务 executor 在无变化时也会完成传入任务。
- 应用镜像更新是可重放应用任务，`application_image_update` 由应用模块注册 executor、`run-now` 和 `retry` 能力；HTTP 更新入口和任务 executor 共用镜像解析、digest 状态写入、revision 记录和 runtime redeploy 准备逻辑。
- 应用同步、停止、重启、刷新和镜像更新任务共享应用级 lifecycle 并发 key，同一应用同一时间不得并行运行多个生命周期操作；不同应用仍可并行。
- 新部署 Application 容器名使用 `panel-<application-name>`；停止、重启、状态和日志操作必须使用 `application_instances.container_name`。agent 必须与 Panel 构建版本完全一致才能被视为兼容；部署流程使用当前 agent 原子接口。

## 验证

- 后端应用或 appspec 改动运行 `task test:backend`，重点关注 `internal/modules/applications`、`internal/modules/applications/spec`、agent runtime 相关测试，以及 `internal/integration/appdeploy` 剧本驱动集成测试。
- `internal/integration/appdeploy` 用 mock agent 按剧本报告/响应，驱动真实 Store + applications.Service + orchestrator.Controller 验证部署全链路：快乐路径收敛、停止、删除 finalizer、漂移修复、可重试/不可重试失败、租约过期恢复、并发 claim 互斥、stale token fencing、执行中 desired 变化不被旧 apply 覆盖、非托管冲突终态失败。
- 前端应用页面、API 或类型改动按影响范围运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 appspec 字段、应用持久化字段、API、应用文件行为、部署流程、镜像更新逻辑、运行时展示字段或 agent runtime 契约时，必须更新本文档。

## Application Folder Archives

- Legacy save-session HTTP endpoints are removed; the current durable editor uses `/application-edit-sessions/{id}/archives`. Ordinary binary files use the durable multipart `/uploads/{name}` endpoint and must never enter an unpacking path; JSON/base64 durable writes are template-only.
- Folder archive mode is a separate user action saved as `application_files.kind=archive`. Template files always use the text editor; binary and archive rows expose replace/download/delete only.
- Replacing a folder archive reuses its stable application-local `name` so mounts and draft identity remain intact.
- Folder archives support zip, tar, tar.gz, and tgz. The backend stores the original archive as one `application_files` row keyed by the application-local opaque `name`; managed runtime paths are allocated separately from the stable file ID. It must not expand archive entries into multiple management-side application files.
- During deployment, an appspec `mounts.type=file` that references an `archive` application file becomes a managed archive. The agent keeps the original archive under the instance `archives` area for sha256 verification, then extracts it into the instance `files/<source>` directory and bind-mounts that extracted directory to the container. If the retained archive sha256 differs, the agent rewrites it from Panel content before extraction; even when the archive sha256 matches, extraction overwrites the target directory to remove node-side drift. This feature does not introduce a new appspec mount type.

## Application Create Editor UX

- The application create/edit workspace uses a top step rail and intent panels instead of the old left section list. The structured panels are Identity, Runtime source, Networking, Storage, Deployment, and Files/Assets.
- AppSpec is edited through the structured form only; there is no YAML source editor. The form regenerates the appspec YAML on save and preserves spec keys it does not cover (`uncoveredSpec`), so advanced fields such as `capAdd`, `sysctls`, healthcheck and resource limits are kept.
- Structured editing must remain complete. Image, command, env, ports, mounts, reverse proxy, deployment targets, and session files are all edited in the form.
- Command is edited as a list of arguments, not a textarea. New dialog rows (command, port, mount, proxy rule, proxy path, facility path) start empty; the UI validates required values instead of pre-filling misleading defaults.
- The create editor starts with a blank draft: application name, image, ports, env, mounts, and reverse proxy rules are not pre-filled with sample values; required fields are validated before preview and commit.
- The application reverse proxy dialog exposes AnyAccess (kept untranslated) with load-balancing strategy, plus the shared HTTP route options (gzip, request body limit, timeouts, buffering, WebSocket, request/response headers) so application routes carry the same entrance-proxy options as facility paths. Origin servers always follow the application's deployment targets intersected with gateway nodes and are resolved automatically (the client never sends `originServerIds`); the proxy dialog does not expose them. For the primary_backup strategy the dialog lets the user arrange the auto-resolved origin servers into a priority order (first entry is the primary origin, the rest are backups); the client sends only this ordering and never sends originServerIds or primaryOriginServerId.
- Application name, enabled state, deployment targets, reverse proxy rules, and application files are application-level fields outside AppSpec YAML. Container environment variables belong to AppSpec YAML. They remain part of the same durable edit session and commit flow even though the spec YAML is generated from the form.
- The editor pending-change diff normalizes both the saved application and the current draft through the same pipeline (`draftFromApplication` + `saveInputFromDraft`), so YAML round-trip formatting and defaulted route options do not surface as a fake changed entry when the editor is opened without edits.
- 服务器选择器（部署目标、网关节点、源服务器、Panel 入口）展示全部服务器，不再只显示已被应用或设施引用的服务器；部署目标选择器对 agent 未兼容或不可达的服务器显示禁用原因，避免用户误选后到部署阶段才失败。
- 编辑器确认放弃未保存修改并离开后，路由切换必须同步清理脏状态和弹窗状态，避免同一组件实例被复用后再次导航仍弹出放弃确认。
- 应用与设施编辑器有未保存修改时的离开/取消保护统一使用 `web/src/components/ui/ConfirmDialog.vue`，不再使用浏览器原生 `window.confirm`；路由离开保护先取消导航并弹出确认框，确认后再按原目标路径继续导航，取消则保持当前编辑状态。
- The editor must preserve local validation, patch draft, validate, preview, commit, and dirty guard behavior. File mutations still use application edit-session file/archive endpoints.

## Durable Application Edit Sessions

- The durable editor API also exposes multipart `PUT /uploads/{name}` for ordinary binary files and `GET /files/{name}/content` for authenticated downloads. Committed content uses `GET /applications/{id}/files/{name}/content`. JSON `PUT /files/{name}` is template-only. Every mutation keeps revision, client operation ID, idempotency key, and stable-name upsert semantics; commit still requires the current revision, base resource version, preview token, and idempotency key. Physical file ids remain internal to storage and runtime allocation.
- 编辑会话新建文件在 commit 落库前分配全局唯一物理 id（`id.New("afile")`），保证两个不同应用可以各自新增同名文件；session `file_key`/blob 路径仍使用应用内 name 语义，不因物理 id 改变而受影响。
- Template session files use the large CodeMirror editor with line numbers, internal scrolling, automatic path/MIME language inference, and a per-dialog language override for plain text, YAML, JSON, Shell, Nginx, INI/Properties, and Dockerfile. Existing file kinds are immutable in the dialog, load failures disable saving, and empty or whitespace-only template content remains valid. Binary files do not enter the text editor.
- Application edit sessions are owned by the stable single-administrator principal, not by the editable administrator username. Renaming the account must not hide or orphan in-progress edit sessions.
- `baseResourceVersion` is a snapshot of the application configuration version and its configuration `updated_at` value when the session starts. Session reads must never replace the base timestamp with the application's current timestamp.
- Updating an application with configuration identical to the persisted user-owned fields and file set is a no-op for `version`; commit performs the base-version comparison as a CAS inside the same AppDB transaction that updates the application row and replaces application files. Runtime revisions are immutable AppDB control-plane snapshots written with the desired generation; a revision write failure must fail/roll back the control-plane apply request rather than silently leaving a Job without its revision. Legacy LogDB revision rows are migration input/compatibility history only. A losing concurrent edit receives `resource_version_conflict`, and none of its AppDB application/file changes persist.
- Application `version` and configuration `updated_at` change only for user-owned configuration mutations, including application-file create, content replacement, and deletion. Saving an identical application definition or identical file is a no-op. Image inspection, resolved-variable refresh, renderer snapshots, and other derived/runtime refreshes persist through the derived update path and must not create edit conflicts or overwrite concurrent user fields.
- Stop/Delete/镜像更新入口只更新必要列（`enabled`/`deletion_requested` 生命周期标志，或镜像相关派生字段），不使用旧快照整行覆盖并发编辑；Stop/Delete 递增 `version` 使进行中的编辑会话在提交时自然冲突。
- Every file upload/replacement writes a new immutable session blob before the database transaction. Revision, idempotency, path, or database conflicts delete the new blob and preserve the previously referenced blob; after the row transaction commits, the replaced blob is removed.
- Cleanup and expiry logic must never expire or remove the workspace of a `committing` session. An expired commit lease is resolved by commit recovery before ordinary cleanup can act on the session.
- Validation refreshes the session idle TTL. Periodic cleanup acts as the recovery worker for expired commit leases even when no client performs a session GET, removes expired/terminal workspaces, and cleans abandoned `.partial` files, unreferenced blobs, and workspace directories that no longer have a database session row. Orphan candidates require at least one hour of staleness; a workspace without a database row is removed only when both the directory and every contained file are stale, because directory timestamps alone cannot distinguish an abandoned workspace from `BeginEditSession` or an upload that has not committed its database row yet. A live commit lease always protects its workspace.
- Configuration persistence and apply/reconcile dispatch are separate outcomes. If application rows/files were committed but lifecycle or reverse-proxy dispatch fails, the session is finalized as `committed` with an `application_apply_request_failed` warning. Commit recovery verifies the exact persisted draft and file set before finalizing. If persistence is observable through the reserved create ID or a newer application version but later changes prevent exact verification, recovery moves the session to `conflict` with `commit_outcome_ambiguous`; it must never reset such a session to `active` or create/apply it twice.
- Facility reverse-proxy editing uses its own `facility_edit_sessions`, asset, operation, manifest, and config-version contract documented in `containerization.md`; it must not reuse application edit-session rows or expose the hidden `facility-reverse-proxy` application as the editable resource. Both commit adapters separate durable configuration success from the later application reconcile request.
- 前端设施应用入口使用前后端内置的设施适配器，不调用通用设施 list API；当前 `reverse-proxy` 详情与配置直接使用其专属 API。新增设施类型时应新增自己的类型、配置和 API adapter，而不是把所有设施抽象成共享 summary。
- 前端设施 API adapter 按设施提供 `getConfig`、`reconcile`、`beginEdit` 等专属域语义，直接映射到 `/api/v1/facility-apps/reverse-proxy`；不提供 `listFacilities` 目录方法。新增设施时新增自己的 adapter 和配置契约。设施静态资产同样通过设施配置/编辑会话返回自己的 `assets` 集合，客户端按设施内唯一 `name` 操作，不新增通用资产目录 API。
- 前端入口代理设施配置页必须是独立编辑页：正文按网关服务器、域名和路由、Panel 访问入口、静态文件顺序连续展开，右侧 sticky 摘要展示新增、修改、删除数量与检查结果；不提供左侧分区导航，域名、Path、静态文件和 Panel 入口不得合并成 JSON 大文本框。
- 设施域名和 Path 编辑使用“列表 + 对话框”模式：域名对话框编辑域名和源站节点，Path 对话框按 `static` / `redirect` / `proxy_pass` 展示不同字段；复杂项取消时不得污染主草稿。域名源服务器选择器只展示当前草稿已选择的网关节点，避免选出后端不允许的非网关源站；取消网关节点时前端同步清理受影响域名的源服务器、AnyAccess 主源站和转发节点并给出提示。保存仍走 `/api/v1/facility-apps/reverse-proxy/edit-sessions/*` 的 patch、validate、preview、commit 路径，不新增 mock-only endpoint。

## 设施反向代理隐藏应用（facility-reverse-proxy）

- 隐藏 identity `facility-reverse-proxy` 仅用于把入口代理作为受控隐藏应用管理：它的配置保存在 `facility_app_configs` / `reverse_proxy_routes`，部署由 planner/controller 写 AppDB `application_instances` 与 `jobs` 完成；旧 CoordDB lifecycle 层已全部下线，不再有迁移/兼容读取。
- `applications.Service.List` 和普通应用页继续过滤 `facility_application` 类型；协调记录列表/详情在 SQL 聚合时额外使用 NOT IN 子查询，排除该类型与保留 identity `facility-reverse-proxy`。设施页面读取自己的配置和 Job/observed 投影，不得把隐藏应用暴露为可编辑普通应用。

## 旧 Lifecycle/Dispatcher 层（已全部下线）

- 以下旧实现已全部删除，不再存在：CoordDB 的 `application_lifecycle_operations` / `application_lifecycle_targets` / `application_target_stages` 三张表（迁移步骤 DROP TABLE IF EXISTS，协调库不再注册任何模型）；旧 deployment dispatcher（claim / executor / recovery / repair / startup、verify 与 aggregate worker）；旧 lifecycle executor；`application_target_batch|apply|stop|purge` 任务类型；Task Center 的 DeploymentProjection（TaskDeploymentOperationProjection / TaskDeploymentTargetProjection、task.deployment 字段）以及 `afterLifecycleTargetVerified` 等验证/聚合钩子。
- 生产部署权威是 AppDB `jobs` + `orchestrator.Controller` + Agent `RuntimeReconcile`：planner 只写 desired / immutable revision / Job，controller 负责 claim、lease、RuntimeReconcile、ObservationWriter 写回与重试；运行时状态由 `application_instances` observed 字段 + 活跃 Job（pending/running/failed_retryable）派生；协调记录由 AppDB jobs 按 intent_id 聚合。

## Coordination Records（协调记录）

- 协调记录页（原“操作记录”，路由 `/application-operations`）展示应用协调记录，只读；记录由应用模块直接聚合 AppDB `jobs`：按 `intent_id` 分组，一个 intent（一次触发）对应一条协调记录，一个 application/server Job 对应一个目标。不再读取 CoordDB lifecycle 表。
- 记录列表/详情由应用模块提供（`GET /api/v1/application-operations`、`GET /api/v1/application-operations/{id}`），读取时直接聚合 AppDB jobs，**不建投影表**；`application_operation_records` 已随旧 lifecycle 层删除（不建、不用）。
- 旧 `application_lifecycle_operations`、`application_lifecycle_targets`、`application_target_stages` 位于独立协调库 `Store.CoordDB()`（默认 `data/db/coordination.db`），已通过迁移步骤 DROP TABLE IF EXISTS 删除；协调库不再注册任何模型（`CoordinationModels` 返回空）。
- 目标信息来自 Job 及其 Instance observed：Job 保存 action、状态、attempt/backoff、`last_steps_json` 与结构化错误；`application_instances` observed 字段提供“期望 vs 实际”观测快照（observed_state / observed_generation / observed_spec_hash / observed_image / observed_at 等），读不到实例则留空，前端显示“未知”。详情接口的目标携带由 Job 步骤派生的 `stages[]`。
- jobs 可空时间列（next_run_at / lease_expires_at / started_at / finished_at）统一以 NULL 表示“未设置”：orchestrator 不再写入空串；ORM 读取时把空串按未设置处理，启动期通用归一化（`orm.NormalizeBlankTimeColumns`，覆盖全部库/全部模型的可空时间列）把存量空串清成 NULL，协调记录列表/详情不会因 `orm: cannot parse time ""` 返回 500。
- 运行时状态由 `application_instances` observed 字段 + 活跃 Job（pending/running/failed_retryable）派生；没有 lifecycle 目标/步骤表需要清理，原 `applications.StageCleanupWorker` 已删除。
- 事件（`runtimeevents`）仅作系统事件页诊断，协调记录不依赖事件、不返回事件。
