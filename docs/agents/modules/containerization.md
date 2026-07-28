# 容器化资源管理

## List And Snapshot Contracts

- Container list reads deserialize `container_observations.summary_json`, not full `container_json`. Reports persist both forms and migration backfills old observations.
- Container, image, network, and volume GET responses use `items`, `observedAt`, `stale`, `refreshing`, optional `refreshTaskId`, and optional `lastRefreshError`.
- Image, network, and volume refreshes are async task POSTs. Resource GET handlers never contact the Agent, and the frontend reloads snapshots after successful task completion.

## 适用场景

修改 Docker 容器、镜像、网络、卷资源页，设施应用、Application 托管 Label，每服务器容器操作队列，镜像更新检查或容器协调监控时，先读本文档。

## 关键入口

- Panel 服务与 API：`internal/modules/containers/`；HTTP 路由在 `routes.go` 注册。
- 设施应用服务与 API：`internal/modules/facilityapps/`；当前内置反向代理设施应用。
- Agent Docker Engine API：`internal/agent/docker/`；Agent gRPC service：`internal/agent/rpc/`
- Application 运行时：`internal/modules/applications/service.go`
- 周期任务：`internal/modules/containers/tasks.go`，由 `internal/modules/tasks/` 内部 worker 驱动
- 前端资源页面（v4 阶段 4A）：`web/src/views/resources/index.vue`，纯函数在 `web/src/views/resources/model.ts`，API 在 `web/src/api/containers.ts` 与 `web/src/api/packages.ts`。
- 前端设施应用页面：`web/src/views/applications/index.vue` 的 `/applications/facility-apps` 目录、`/:facilityKind` 详情和 `/:facilityKind/config` 配置三层 renderer。
- 前端 API：`web/src/api/containerization.ts`、`web/src/api/facilityApps.ts`

## 页面与 API

菜单上容器相关能力拆分为“资源”和“应用”两个一级分组：资源包含软件包、容器、镜像、网络、卷、防火墙，以及 dev-only Fail2Ban；应用包含普通应用和设施应用两个独立入口。容器、镜像、网络、卷使用左侧服务器选择器和右侧内部滚动列表。

资源页为服务器上下文资源维护台，外层左侧选择服务器、右侧为内部滚动工作区。软件包、容器、镜像、网络和卷是独立路由页面，不使用页内 tabs；可以复用同一个资源上下文，但必须保持不同的信息架构和操作闭环，不得退回同质通用列表。所有控件使用 Panel 自有 primitives，禁止引入 Naive UI / Vuetify。

- 设施应用页面是 `/applications/facility-apps` 独立入口，必须先呈现设施目录；内置“反向代理”只是当前唯一设施项。`facilityKind=reverse-proxy` 的只读详情展示路由与部署状态；独立配置页保存 Nginx 镜像、全局网关节点、Panel 入口、域名策略、Path 规则和静态资产草稿。指定的全局网关节点即视为开启反向代理能力。
- 容器支持查询、查看日志、启动、停止、重启、删除；日志入口与其他行操作使用带图标的小型操作，托管 Application 容器的直接停止、重启和删除入口必须禁用并提示改走 Application 生命周期。
- 镜像支持查询、拉取、删除、删除未使用镜像、刷新更新状态、升级选中 Application 和全部升级；批量危险操作必须通过确认对话框触发。
- 网络只读。
- 卷支持查询、单个删除和批量删除未使用卷，必须展示使用状态；批量删除执行时需重新查询使用状态，只删除执行瞬间仍未使用的卷。
- 容器、镜像、卷页面发起的资源写操作同步执行并返回；手动镜像刷新和 Application 镜像升级仍创建任务。
- 同步资源写操作成功后，容器列表等待 Agent report stream 的近实时/周期快照更新缓存；镜像和卷仍立即创建对应刷新任务：镜像使用 `image_refresh`，卷使用 `volume_refresh`。

Panel API 挂在 `/api/v1/servers/{serverId}/containers|images|networks|volumes`；容器日志使用 `GET /api/v1/servers/{serverId}/containers/{containerId}/logs`，tail 行数最大为 10000；批量 Application 镜像更新使用 `/api/v1/images/upgrade-selected|upgrade-all`。

设施应用目录 API 为 `GET /api/v1/facility-apps`，返回固定小目录 `FacilityAppSummary[]`，只包含目录身份、健康状态、更新时间、最近 operation 状态和错误。目录查询不得构建反向代理详情，也不得加载网关、路由、静态资产、应用路由或证书数据；这些统计只由反向代理设施详情 API `/api/v1/facility-apps/reverse-proxy` 按需读取。详情接口继续支持读取完整配置、兼容保存和手动同步。旧 `/save-sessions` 会话继续兼容已有前端：30 分钟过期并使用创建时的 `updatedAt` 检测并发覆盖。新事务编辑器使用持久 `/edit-sessions` API：创建、recoverable 查询、详情、draft patch、按稳定 `assetKey` PUT/DELETE 资产、validate、preview、commit 和 discard；会话由稳定单管理员主体持有，idle TTL 为 24 小时、absolute TTL 为 7 天，draft/资产操作使用 revision，资产操作和 commit 使用幂等键。`facility_app_configs.version` 是配置并发版本，commit 在替换资产 metadata 的同一 AppDB 事务中执行 CAS。

持久设施会话中的既有资产只保存 `source_asset_id` 和 metadata，不复制正文；新增或替换资产写入唯一 blob 目录。路由草稿在编辑期间以 `assetKey` 引用资产，commit manifest 决定最终 asset ID 并在写数据库前统一改写。删除仍被 route 引用的资产、移除仍被 origin/AnyAccess primary/Panel Entry 使用的 gateway、或把 Panel Entry 绑定到非 setup Panel host 都是阻断诊断，服务端不得静默修剪。

所有会修改正式设施配置或正式静态资产的旧、新入口共享同一单资源提交锁：直接 `PUT /reverse-proxy`、旧 save-session commit、直接静态资产上传/删除和新 edit-session commit 不得交错文件 rename 或数据库写。旧 save-session 在锁内使用创建时的 config version 做事务 CAS。新会话冲突后可通过 draft PATCH 携带当前 `baseResourceVersion` 执行 rebase；rebase 保留 session assets、更新 base version、清除 conflict 后允许重新 preview/commit。

设施资产的数据库 metadata 和 dataRoot 目录通过可恢复 commit manifest 协调：先记录目标、backup、blob 和 base version，再移动旧/新目录，然后在单一事务中替换资产、CAS 保存配置并递增 version。启动或 commit lease 过期时会读取 manifest；DB 未提交则回滚目录并恢复会话，DB 已提交或可由 version+配置精确确认则只完成收尾并标记 committed，不重复写配置。配置提交与 `application_reconcile` 请求分离；协调器不可用时 commit 仍返回 committed，并携带 `facility_apply_request_failed` warning。

commit 前必须重新散列每个新 blob 的 content 目录和每个 `source_asset_id` 的正式 content 目录，并与 session 创建/上传时记录的 `content_sha256` 比较；缺失或漂移会阻断 commit。恢复只在 config version 精确等于 `base+1` 且配置及全部资产 ID/sha256 匹配 manifest 时认定 DB 已提交。恢复会实际重新请求 traits 同步和 application reconcile；失败时 `applyRequested=false` 并返回 warning，不能把“已恢复配置”谎报成“已请求应用”。commit、recovery 和 cleanup 使用同一资源锁，长文件阶段续期 commit lease；活跃 lease 即使超过普通 TTL 也保护 workspace。

上传 handler 使用 64 MiB request 上限并清理 multipart 临时文件。bundle 解包限制最多 10000 个文件、32 层路径、256 MiB 解包总量和 100 倍压缩比。当前版本明确暂缓独立 heartbeat API、每管理员/全局草稿配额、`confirmedDiagnosticCodes` warning 确认协议、commit/recovery 专用结构化日志和 recoverable 会话摘要投影；客户端可通过现有 draft/asset mutation、validate 和 preview 延长 idle TTL，GET 只执行 TTL/lease 状态检查，recoverable 暂时返回完整会话 DTO，阻断错误仍不能绕过。

## 设施应用

- 设施应用配置保存在 `facility_app_configs`，不写入普通 `applications` 表；当前持久字段为 `version`、`deployment_server_ids_json`、`panel_entry_json`、`domains_json`、错误和更新时间，旧 `domain_policies_json/static_sites_json` 已由迁移转换删除。保存反向代理设施应用时会派生维护服务器 traits 中的 `agent.reverse_proxy.enabled`，该值只反映设施全局网关范围，不作为独立节点开关。
- 反向代理设施应用使用普通 agent runtime 原子能力：拉取 nginx 镜像、写托管 nginx 配置、容器内执行结构化命令、reload 或重建容器；不得新增 agent 侧胖反向代理接口。
- Panel 托管的 Nginx 配置目录只读挂载到独立的 `/etc/panel-nginx`，容器以 `nginx -c /etc/panel-nginx/nginx.conf` 显式启动；不得挂载或覆盖镜像原生 `/etc/nginx`，以保留 `mime.types` 等镜像资产。不能把主配置作为单文件 bind mount，否则宿主机原子 rename 后运行容器仍可能引用旧 inode。证书 managed directory 独立只读挂载到 `/etc/panel-certs`。设施为每次差异返回 reload 或 recreate：纯路由、upstream、Header 和现有挂载内证书变化可使用显式指定 `/etc/panel-nginx/nginx.conf` 的 validate/reload，网络、端口、镜像、命令或 mount 结构变化必须 recreate。validate 失败回滚 managed files 并保留旧 worker；reload 失败回退 recreate。
- 默认情况下 nginx 容器使用 host network，监听节点本机端口并把应用反向代理规则转发到 `127.0.0.1:<targetPort>`。当任一应用反向代理规则选择 `targetType=container` 时，nginx 容器改用受管 `panel-apps` bridge 网络并绑定宿主机 80/443；本地目标改为通过 `host.docker.internal:<targetPort>` 访问节点本地端口，容器目标通过 Application 容器名访问目标端口。
- Panel 入口同样遵循入口容器网络模式：host network 使用 `127.0.0.1:8080`，`panel-apps` bridge 使用 `host.docker.internal:8080`。不得在 bridge 模式继续把 Panel upstream 指向入口容器自身的回环地址。
- 静态站点配置保存域名、路径和宿主机根目录；部署时作为只读 bind mount 挂入 nginx 容器。
- 应用里的 `reverseProxy` 规则只会被下发到反向代理设施应用覆盖的服务器；未指定为设施应用部署目标的服务器忽略这些规则。
- 每个设施域名必须显式选择至少一台入口节点，且入口节点必须属于设施全局网关节点；新保存请求不得把空选择解释为全部节点。读取旧配置时，旧 `deploymentServers` 为空仍按当时全部全局网关节点展开，多个旧 Path 使用不同节点集合时取并集，避免迁移缩小已有访问范围。
- 域名开启上游模式后，入口节点成为上游并保留原始静态、重定向和手工代理 Path；其余全局网关节点只生成域名级 `/` 转发。策略支持轮询、主备和客户端 IP 哈希，固定 `max_fails=3 fail_timeout=30s`。匹配到域名证书时节点间使用 HTTPS、SNI 和证书校验，否则使用 HTTP；上游全部不可用时由 Nginx 返回 502，不回退本地处理。
- 上游模式域名由设施独占，不允许普通应用或 Panel 入口共享同一规范化域名。非上游模式仍按实际服务器上的精确 `domain + path` 检测设施、应用和 Panel 入口冲突。
- 设施手工代理、普通应用代理、Panel 入口和跨节点转发的每个 Nginx location 都必须显式写入 `proxy_cache off;`。Panel 不管理客户端缓存 Header；用户如通过通用响应 Header 设置 `Cache-Control` 等字段，其语义由用户负责。
- 设施应用部署和普通 Docker/Application 写操作共享每服务器容器操作队列。

## 托管 Label

Application 新部署容器只写入：

- `panel.application.managed`
- `panel.application.id`
- `panel.application.instance.id`
- `panel.application.generation`
- `panel.application.spec.hash`

Application 托管容器只识别以上 Label。

Agent 上报托管容器快照时会在返回给 Panel 的 Label map 中补充观测字段，不写入 Docker 容器本身：`panel.application.files.manifest.v1` 保存当前节点 managed files 的 path、sha256、mode、uid、gid；`panel.application.files.manifest.error` 保存读取失败原因。协调器只把这些观测 Label 用于漂移判断，不把它们当作容器身份 Label。

Application bridge 网络容器由 Agent 创建时自动放入受管 Docker 网络 `panel-apps`；该网络不存在时由 Agent 创建。该网络用于入口网关在容器目标模式下解析并访问 Application 容器名。

Application appspec 的 `capAdd` 会由 Panel 渲染到 agent runtime spec，并在创建容器时写入 Docker `HostConfig.CapAdd`。缺省或空数组不下发任何 capability；该字段仅表示用户显式追加的 Linux capability，可与 `Privileged` 同时出现。

## 队列、同步操作与协调

- 每台服务器一条独立 Docker 资源写操作队列；同服务器串行，不同服务器并行。
- 普通容器、镜像、卷页面发起的写操作进入队列同步执行，不创建操作任务；API 在 Agent 操作完成或失败后返回。
- 镜像拉取是长耗时操作，Panel 到 agent 以及 agent 到 Docker Engine 的 pull 请求超时均为 15 分钟；未显式写 tag 的镜像引用按 Docker CLI 语义拉取 `latest`，agent 调 Docker Engine API 时必须显式传递 `tag=latest`；其它 Docker 查询、容器动作和卷动作保持常规短超时。
- Application 部署、停止、重启也共享同一服务器队列，但保留 Application 自身任务记录；Application 部署由 Panel 编排写文件、拉镜像、删旧容器、创建、启动和状态刷新等原子 agent/Docker 调用，不使用 agent 侧胖部署接口。
- 设施应用保存和手动同步不直接执行远端 Docker 操作；它们通过 `TriggerPeriodicNow(application_reconcile)` 立即触发指定应用协调，目标应用为隐藏身份 `facility-reverse-proxy`。协调 collector 会请求应用 planner 为设施部署节点创建或复用 apply target，并为从部署服务器列表移除的节点创建 stop/purge target；`application_target_apply|stop|purge` 任务只能由 deployment dispatcher 在 claim target 后创建为执行日志锚点。执行时仍共享同一服务器队列，并写入 `application_lifecycle_operations` / `application_lifecycle_targets` 作为设施应用部署记录。没有任何域名路由时，0 个全局网关节点是合法设施配置，表示不部署新的反向代理，只停止/清理已有实例；一旦存在域名路由，每个域名仍必须显式选择至少一台入口节点。设施模块只能提供配置校验和 runtime spec provider，不得在缺少协调器时 fallback 到直接 agent/Docker 部署。
- 刷新任务按任务类型、服务器和资源复用活跃任务；Agent 操作按目标状态幂等。
- 容器、镜像、网络、卷的 GET 列表只读取本地快照，不得同步调用 Agent。容器使用 `container_observations`；镜像、网络和卷使用 `docker_resource_snapshots`。镜像、网络、卷刷新分别通过 `image_refresh`、`network_refresh`、`volume_refresh` 任务访问 Agent 并原子替换快照；首次尚无快照时 GET 返回空集合。刷新任务遇到 agent mTLS server 证书过期或尚未生效时，必须交给服务器模块标记 Agent 状态并按受限自动重装策略处理。
- 镜像和卷的“删除未使用”是 Panel 侧同步批量操作，通过现有 Agent 单项删除接口逐项执行；执行瞬间仍在使用的资源会跳过，删除失败会使当前请求失败。
- containers 模块注册的周期任务每 5 秒收集容器协调输入，由 tasks 内部 worker 驱动；5 秒只是采集频率，不是失败重试间隔。`application_reconcile` 是 collector-only 周期入口，不注册固定失败的 executor，也不向任务中心暴露 run-now/retry；被动扫描只有已经观察到托管 Label 并写入 `application_reconcile_states` 的实例会持续协调；应用或设施配置变更等外部事件可以通过 `PeriodicTrigger` payload 指定 application ID 立即触发一次 `application_reconcile` collector，不依赖 Label 已被观察。
- 监控发现容器缺失、停止、generation/spec hash 偏差或 managed file manifest 与已部署 runtime spec 不一致时，`application_reconcile` collector 只负责收集需要处理的应用与服务器，应用服务必须先通过 `PlanApplicationDeployment` 用 `application_lifecycle_targets.target_key = application:<appId>:server:<serverId>` 的活跃 target 守卫过滤、复用或 supersede；collector 不得把本轮新建 target 转换为 `application_target_apply|stop|purge` 输入。collector 发起规划时必须把 Agent 已确认的漂移节点作为精确目标，并携带内部 observed runtime drift 语义，使 planner 跳过这些节点在 `application_instances` 中可能滞后的 running 缓存；普通非漂移规划仍执行满足态过滤，observed runtime drift 也不得扩大到同一应用的健康节点或替代用户/系统 `force` 语义。manifest 漂移包含期望文件缺失、sha256 不匹配、mode 不匹配，以及已显式配置 uid/gid 的属主不匹配；额外文件不触发漂移。显式触发的协调 payload 可传 `applicationIds`、`serverIds`、`force`、`stopServers` 和 `purge`：未强制时这些字段只限制观测范围并跳过已经满足 desired state 的目标；`force=true` 用于配置保存、停用、删除、设施应用保存和系统级重部署等 desired state 变更，会绕过退避和满足态过滤，但不能绕过活跃 target 唯一性。协调恢复必须通过应用服务生成 lifecycle target，并由 dispatcher 负责目标级任务与错误记录；自动协调和非强制显式协调只使用 `application_reconcile_states.reconcile_failures` 这一套连续失败计数器计算指数退避，并把等待时间写入 `reconcile_next_run_at`。任务自身不再维护另一套自动 retry 计数；未到 `reconcile_next_run_at`、collector 结果为空或只完成规划时不创建任务记录。连续 5 轮观测到该应用全部托管实例正常后，才清空失败计数和下次运行时间。不得调用手动部署入口异步再创建一组“用户部署”任务，否则会绕过退避并造成重复部署。
- Container resource actions must not directly mutate containers that cached observations identify as managed Application containers (`panel.application.managed=true`). Stop/restart/delete for those containers must go through the application lifecycle planner/dispatcher so the runtime mutation remains visible, deduplicated, and recoverable.

## 镜像更新

- `image_updates` 保存每服务器镜像引用、本地摘要、远端摘要、状态、错误和检查时间。
- `image_refreshes` 保存最近刷新时间。
- 镜像刷新必须在数据库事务外完成所有远端 registry digest 查询；结果准备完成后再用短事务原子替换 `image_updates` 并更新 `image_refreshes`，禁止在持有 SQLite 写锁时等待网络请求。
- containers 模块注册的镜像检查周期与软件包刷新一致。
- 所有带标签且可解析的镜像都显示更新状态；普通容器镜像不提供升级操作。
- Application 镜像升级复用 `applications.Service.UpdateImage` 并重新部署。
- Application 详情不提供手动镜像检查按钮；镜像检查由 containers 周期任务自动刷新，用户只手动触发实际更新。
- Application 详情会按运行实例的服务器和镜像引用读取 `image_updates` 聚合为应用级状态；任一节点可更新即视为该应用可更新。应用镜像更新成功后，应用模块会同步对应节点的检查缓存，后续周期刷新再校准实际 Docker 镜像状态。

## 验证

- 后端改动运行 `task test:backend`，必要时运行 `task build:backend`。
- 前端改动运行 `task test:web` 和 `task build:web`。

## 设施应用反向代理静态站点与 TLS

- `facility_app_configs` 只持久化 `deployment_server_ids_json`、`panel_entry_json` 和 `domains_json`。`domains_json` 按域名保存 `originServerIds`、`anyAccess` 与嵌套 Path；旧 `image`、`static_sites_json`、`domain_policies_json` 在启动迁移中一次性转换并通过 SQLite 重建表删除。
- 设施入口网关镜像固定为 `nginx:1.28-alpine`，API 和前端不提供镜像设置。
- 每个设施域名至少选择一个源站节点，且源站必须属于全局网关节点。AnyAccess 关闭时只有源站节点开放域名；开启时其他全局网关作为转发节点，按轮询、主备或客户端 IP 哈希连接源站入口网关。
- 设施路由、应用路由和 Panel 入口的规范化域名全局唯一。旧库迁移发现跨所有者冲突时必须中止并列出冲突，不得静默合并。
- 手工代理、应用代理、Panel 入口和 AnyAccess 转发均明确生成 `proxy_cache off;`；Panel 不管理客户端缓存 Header。

- `facility_static_assets` 保存设施应用上传的静态资产元数据，文件内容存放在 `<dataRoot>/facility-apps/static-assets/<assetId>/content`。上传时必须指定 `uploaded_file` 或 `uploaded_bundle`：普通文件永远不解压；只有指定为文件夹包时才解压 zip、tar、tar.gz 或 tgz。
- 入口网关路径规则保存在 `domains[].paths` 中。`ruleType` 支持 `static`、`redirect`、`proxy_pass`。
- `static` 规则的 `sourceType` 支持 `host_path`、`uploaded_file`、`uploaded_bundle`。`host_path` 继续把目标节点本地目录只读 bind mount 到 nginx；上传来源通过 agent managed files 下发到节点并只读挂载。
- `redirect` 规则写入 nginx `return`，支持 301、302、307、308；`proxy_pass` 规则写入手工 upstream URL，并通过 `proxySourceMode` 控制是否透传源请求信息。
- 前端按域名设置源站节点，Path 不再单独保存节点字段。每个域名至少选择一台源站，且只能从设施全局网关节点中选择。
- 反向代理部署时自动读取证书服务的 `ReverseProxyCertificates` 聚合结果。域名证书优先；没有匹配域名证书时使用匹配的用户域自签 TLS 证书；都没有时只生成 80 端口，不生成 443。
- `GET /api/v1/facility-apps/reverse-proxy` 返回 `routeSummaries`，供前端按域名汇总 HTTPS 状态：`domain_certificate`、`self_signed_certificate` 或 `disabled`。nginx 生成逻辑必须与该摘要使用同一套证书匹配规则；UI 不应把 HTTPS 状态展示成路径属性。
- 静态资产 API 挂在 `/api/v1/facility-apps/reverse-proxy/static-assets`，支持兼容列表、multipart 上传和删除；新配置页不即时调用持久化上传/删除，而是随设施保存会话一起提交。被最终配置引用但不在会话资产集合中的资产会使提交失败。

## Entrance Gateway UI And Static Content

- The reverse proxy facility app is presented in the frontend as an entrance gateway. Deployment servers are called gateway nodes in this context because selecting them means those nodes listen on 80/443 and process application routes plus static sites.
- The entrance gateway owns the Panel access entry. `panelEntry` is a system route, not a normal static site row: it is locked to the singleton Panel host registered by `panel setup`, and that host must remain a global gateway node. Host-network gateways use `127.0.0.1:8080`; bridge-network gateways use `host.docker.internal:8080`.
- The facility catalog page lists facility apps first. The reverse-proxy facility detail page is read-only, exposes immediate sync and the link to `/applications/facility-apps/reverse-proxy/config`, and must not render editable gateway, domain, path, Panel-entry, or asset controls.
- Entrance gateway routes are edited on the facility configuration route `/applications/facility-apps/:facilityKind/config` with `facilityKind=reverse-proxy` as `domains` with nested paths. The configuration page contains only editable facility settings; application routes remain read-only on the facility detail page.
- Origin selection and AnyAccess are edited at domain level. A path row does not expose its own node selector. Origins are required and are chosen only from the current global gateway nodes.
- Each path route can be static content, a redirect, or a manual proxy_pass. The route row displays route-specific fields only; HTTPS status belongs to the domain group header because certificates are matched by domain, not by path.
- A static route points at either a target-node server directory, one uploaded file, or an uploaded folder archive. Server directories and uploaded folder archives are treated as directory trees, so one route path can serve multiple files below that path.
- The user must not configure or see the nginx container mount target. The backend chooses internal read-only mount targets for bind mounts and managed-file assets during reconciliation.
- nginx config generation writes a tuned managed `/etc/panel-nginx/nginx.conf` that retains the image-provided `/etc/nginx/mime.types` and includes `/etc/panel-nginx/conf.d/*.conf`; each domain gets one managed `conf.d/<domain>.conf` file on every node where it should be reachable. Upstream nodes contain original facility routes; relay nodes contain a domain upstream pool and one catch-all proxy location. Manual facility and application proxy paths use the shared structured Gzip, timeout, buffering, WebSocket, request-header and response-header options.
- Facility routes and application reverse proxy routes both show HTTPS/certificate state on the domain group header. Individual path rows show only path-specific properties such as rule type, target, static source, redirect target, or upstream proxy target.
- The facility app selector remains scalable and only identifies the facility app. Metrics, route summaries and deployment records belong in the read-only detail workspace. The dedicated configuration page is one full-height work surface with internal body scrolling; it keeps all changes in a local draft and performs a single Save and apply operation.
- Facility actions follow the shared operation model: immediate sync and entering configuration live in the read-only detail header; the configuration page owns its Save and apply action. Domain/path/asset deletion requires a standard confirmation dialog. Path editing uses an independent dialog draft, and saving is blocked by a persistent progress overlay while assets and configuration are committed through the save session.
- Uploaded site content is distributed through agent managed files and mounted read-only into the nginx container. Server directories still use read-only bind mounts from the target node.
- Agent managed files use full desired-set synchronization: ordinary files are written to temporary siblings and renamed, the manifest is committed last, and files managed by the previous manifest but absent from the new manifest are removed. Files that never belonged to a Panel manifest are preserved. This applies to both reload and recreate.
- Facility reverse proxy deployment maintains a hidden managed application identity with id `facility-reverse-proxy` for application lifecycle deployment record queries. The facility configuration remains in `facility_app_configs`, the managed identity is filtered out of normal application lists, and each save/reconcile writes the latest `application_lifecycle_operations` plus per-node `application_lifecycle_targets` in `Store.LogDB()` shown on the facility app page.
 
## Agent Report Cache

- Container list reads use the latest `container_observations` snapshot saved from the agent report stream. They no longer pull `DockerContainers` during normal list or application reconciliation paths.
- The agent sends periodic full container snapshots and near-real-time change snapshots over the report stream. Panel replaces the per-server observation set atomically for each full report.
- Application reconciliation collectors read cached observations only. A server that reports a failed container, stale generation/spec hash, or managed file manifest drift can cause the application planner to create or reuse a lifecycle target for that server without redeploying other servers that are already healthy.
- `application_reconcile_states` keeps the exponential backoff state. Automatic reconciliation must honor `reconcile_next_run_at`; healthy observations clear failures only after the configured success streak.
- Agent reports and forced/manual reconciliation triggers must not create another application target while the same app/server conflict domain already has an active lifecycle target. The application planner owns that durable in-flight check before lifecycle rows and task rows are created; report collectors only provide the requested app/server scope.
- Agent report 的 `container_change` 触发在 collector 已确认应用/服务器发生停止、缺失、generation/spec hash 或 managed file drift 后，可绕过应用级 `application_reconcile_states.reconcile_next_run_at` 退避立即请求 planner；该绕过只作用于已 drift 的 app/server，不等同于 `force=true`，不得重部署同节点其它已满足 desired state 的应用。普通 scheduler 与非强制显式协调仍尊重应用级退避。
- Immediate application reconcile triggers call `PlanApplicationDeployment` directly and return no `application_target_*` task inputs. If the dispatcher cannot create a task log anchor after claiming a target, it marks that target `failed_retryable` with `task_create_failed` instead of leaving it pending.
