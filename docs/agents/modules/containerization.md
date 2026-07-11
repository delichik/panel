# 容器化资源管理

## 适用场景

修改 Docker 容器、镜像、网络、卷资源页，设施应用、Application 托管 Label，每服务器容器操作队列，镜像更新检查或容器协调监控时，先读本文档。

## 关键入口

- Panel 服务与 API：`internal/modules/containers/`；HTTP 路由在 `routes.go` 注册。
- 设施应用服务与 API：`internal/modules/facilityapps/`；当前内置反向代理设施应用。
- Agent Docker Engine API：`internal/agent/docker/`；Agent gRPC service：`internal/agent/rpc/`
- Application 运行时：`internal/modules/applications/service.go`
- 周期任务：`internal/modules/containers/tasks.go`，由 `internal/modules/tasks/` 内部 worker 驱动
- 前端资源页面：`web/src/views/resources/`
- 前端设施应用页面：`web/src/views/applications/facility-apps/`
- 前端 API：`web/src/api/containerization.ts`、`web/src/api/facilityApps.ts`

## 页面与 API

菜单上容器相关能力拆分为“资源”和“应用”两个一级分组：资源包含容器、镜像、网络和卷；应用包含普通应用和设施应用。容器、镜像、网络、卷使用左侧服务器选择器和右侧内部滚动列表。

资源页右侧内容应统一为表格工作面：主从外层由 `ResourcePage.vue` 复用 `AppMasterDetailWorkspace.vue`，当前服务器上下文头由 `ResourcePage.vue` 提供，slot 内先渲染固定高度标题/操作区，再用内部滚动的表格正文承载 `v-table`。容器、镜像、网络和卷必须保持同一结构，不要把裸 `v-table` 直接放进资源页 slot，也不要把多个主操作和危险批量操作全部挤在标题右侧；保留主要动作，次要或危险批量动作收进更多菜单，并用标准 `app-dialog-*` 对话框确认。标题、批量菜单和行操作使用 `AppActionButton` / `AppActionGroup`，行尾操作不得退回纯图标删除。

- 设施应用页面当前管理内置“反向代理”设施应用。该设施应用保存 nginx 镜像、部署服务器和静态站点配置；指定的部署服务器即视为开启反向代理能力。
- 容器支持查询、查看日志、启动、停止、重启、删除；表格行操作沿用统一的 `app-table-actions` 或 `AppActionGroup context="table"` 操作组，日志入口与其他行操作使用带图标的小型文字按钮。
- 镜像支持查询、拉取、删除、删除未使用镜像、刷新更新状态、升级选中 Application 和全部升级；批量危险操作必须通过确认对话框触发。
- 网络只读。
- 卷支持查询、单个删除和批量删除未使用卷，必须展示使用状态；批量删除执行时需重新查询使用状态，只删除执行瞬间仍未使用的卷。
- 容器、镜像、卷页面发起的资源写操作同步执行并返回；手动镜像刷新和 Application 镜像升级仍创建任务。
- 同步资源写操作成功后，容器列表等待 Agent report stream 的近实时/周期快照更新缓存；镜像和卷仍立即创建对应刷新任务：镜像使用 `image_refresh`，卷使用 `volume_refresh`。

Panel API 挂在 `/api/v1/servers/{serverId}/containers|images|networks|volumes`；容器日志使用 `GET /api/v1/servers/{serverId}/containers/{containerId}/logs`，tail 行数最大为 10000；批量 Application 镜像更新使用 `/api/v1/images/upgrade-selected|upgrade-all`。

设施应用 API 挂在 `/api/v1/facility-apps/reverse-proxy`，支持读取、保存和手动同步。保存反向代理设施应用会更新本地配置和隐藏应用 desired state，然后通过 task 框架立即触发 `application_reconcile` 周期任务协调 `facility-reverse-proxy`；实际重建当前部署服务器上的 `panel-facility-reverse-proxy` nginx 容器、停止从部署服务器列表移除的节点，均由协调任务异步执行。

## 设施应用

- 设施应用配置保存在 `facility_app_configs`，不写入普通 `applications` 表。保存反向代理设施应用时会派生维护服务器 traits 中的 `agent.reverse_proxy.enabled`，该值只反映设施应用部署范围，不作为独立节点开关。
- 反向代理设施应用使用普通 agent runtime 原子能力：拉取 nginx 镜像、写托管 nginx 配置、删除旧容器、创建容器并启动；不得新增 agent 侧胖反向代理接口。
- 默认情况下 nginx 容器使用 host network，监听节点本机端口并把应用反向代理规则转发到 `127.0.0.1:<targetPort>`。当任一应用反向代理规则选择 `targetType=container` 时，nginx 容器改用受管 `panel-apps` bridge 网络并绑定宿主机 80/443；本地目标改为通过 `host.docker.internal:<targetPort>` 访问节点本地端口，容器目标通过 Application 容器名访问目标端口。
- 静态站点配置保存域名、路径和宿主机根目录；部署时作为只读 bind mount 挂入 nginx 容器。
- 应用里的 `reverseProxy` 规则只会被下发到反向代理设施应用覆盖的服务器；未指定为设施应用部署目标的服务器忽略这些规则。
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
- 设施应用保存和手动同步不直接执行远端 Docker 操作；它们通过 `TriggerPeriodicNow(application_reconcile)` 立即触发指定应用协调，目标应用为隐藏身份 `facility-reverse-proxy`。协调 collector 会请求应用 planner 为设施部署节点创建或复用 apply target，并为从部署服务器列表移除的节点创建 stop/purge target；`application_target_apply|stop|purge` 任务只能由 deployment dispatcher 在 claim target 后创建为执行日志锚点。执行时仍共享同一服务器队列，并写入 `application_lifecycle_operations` / `application_lifecycle_targets` 作为设施应用部署记录。0 个入口节点是合法设施配置，表示不部署新的反向代理，只停止/清理已有实例，不得复用普通应用“至少选择一个部署服务器”的校验错误。设施模块只能提供配置校验和 runtime spec provider，不得在缺少协调器时 fallback 到直接 agent/Docker 部署。
- 刷新任务按任务类型、服务器和资源复用活跃任务；Agent 操作按目标状态幂等。
- 容器、镜像、网络、卷查询和队列操作遇到 agent mTLS server 证书过期或尚未生效时，必须交给服务器模块标记 Agent 状态并按受限自动重装策略处理；当前容器化任务或请求仍按原始 agent 错误失败。
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

- `facility_static_assets` 保存设施应用上传的静态资产元数据，文件内容存放在 `<dataRoot>/facility-apps/static-assets/<assetId>/content`。上传时必须指定 `uploaded_file` 或 `uploaded_bundle`：普通文件永远不解压；只有指定为文件夹包时才解压 zip、tar、tar.gz 或 tgz。
- 入口网关路径规则保存在现有 `staticSites` 列表中，旧数据未写 `ruleType` 时按 `static` 处理。`ruleType` 支持 `static`、`redirect`、`proxy_pass`。
- `static` 规则的 `sourceType` 支持 `host_path`、`uploaded_file`、`uploaded_bundle`。`host_path` 继续把目标节点本地目录只读 bind mount 到 nginx；上传来源通过 agent managed files 下发到节点并只读挂载。
- `redirect` 规则写入 nginx `return`，支持 301、302、307、308；`proxy_pass` 规则写入手工 upstream URL，并通过 `proxySourceMode` 控制是否透传源请求信息。
- 前端按域名设置入口节点，并同步到该域名下全部路径规则的 `deploymentServers`。为空表示跟随反向代理设施应用的全部部署服务器；非空时该域名只在这些节点生效，并且必须是设施应用部署服务器的子集。
- 反向代理部署时自动读取证书服务的 `ReverseProxyCertificates` 聚合结果。域名证书优先；没有匹配域名证书时使用匹配的用户域自签 TLS 证书；都没有时只生成 80 端口，不生成 443。
- `GET /api/v1/facility-apps/reverse-proxy` 返回 `routeSummaries`，供前端按域名汇总 HTTPS 状态：`domain_certificate`、`self_signed_certificate` 或 `disabled`。nginx 生成逻辑必须与该摘要使用同一套证书匹配规则；UI 不应把 HTTPS 状态展示成路径属性。
- 静态资产 API 挂在 `/api/v1/facility-apps/reverse-proxy/static-assets`，支持列表、multipart 上传和删除。被静态站点引用中的资产不能删除。

## Entrance Gateway UI And Static Content

- The reverse proxy facility app is presented in the frontend as an entrance gateway. Deployment servers are called gateway nodes in this context because selecting them means those nodes listen on 80/443 and process application routes plus static sites.
- The entrance gateway owns the Panel access entry. `panelEntry` is a system route, not a normal static site row: when enabled, the selected gateway node proxies the configured domain at `/` to the local Panel service at `http://127.0.0.1:8080`. The selected Panel host must also be one of the gateway nodes, and the Panel entry cannot share the same domain plus root path with a static route.
- Entrance gateway routes are configured as domain groups with multiple path routes under each domain. The API still persists the existing flat `staticSites` list; the UI groups rows by domain and does not require a database migration for this shape change.
- Gateway node selection is edited on the domain group header and is written to every path route under that domain. A path row should not expose its own node selector.
- Each path route can be static content, a redirect, or a manual proxy_pass. The route row displays route-specific fields only; HTTPS status belongs to the domain group header because certificates are matched by domain, not by path.
- A static route points at either a target-node server directory, one uploaded file, or an uploaded folder archive. Server directories and uploaded folder archives are treated as directory trees, so one route path can serve multiple files below that path.
- The user must not configure or see the nginx container mount target. The backend chooses internal read-only mount targets for bind mounts and managed-file assets during reconciliation.
- nginx config generation writes a tuned managed `/etc/nginx/nginx.conf` that includes `/etc/nginx/conf.d/*.conf`; each domain gets one managed `conf.d/<domain>.conf` file per gateway node. The domain file contains one HTTP server block, plus one HTTPS server block when a matching certificate exists, and includes all static locations and application proxy locations for that domain. Manual `proxy_pass` entrance gateway routes always emit HTTP/1.1 WebSocket upgrade headers; application routes continue to use their per-path WebSocket setting from the application editor.
- Facility routes and application reverse proxy routes both show HTTPS/certificate state on the domain group header. Individual path rows show only path-specific properties such as rule type, target, static source, redirect target, or upstream proxy target.
- The facility app page currently has only the entrance gateway item, but its selector must remain a scalable facility-app list and should reuse the same selector components as the normal applications page. The selector should only identify the facility app; gateway nodes, route counts, static asset counts, and other configuration metrics belong in the detail workspace, not in the selector. The entrance gateway detail workspace must preserve full horizontal room for route editing, so medium and large screens use a single-column detail flow with domain route groups as the primary editor surface, followed by supporting gateway nodes, Panel entry, static assets, and deployment records. The detail body should fill the available right-side workspace width and must not be constrained to a centered fixed max-width column. Do not add a right rail for deployment records or route summaries if it compresses the route editor. Application route summaries belong to normal application reverse proxy settings and should not appear as a competing section in the facility app editor. Deployment records should read as lightweight runtime status rows, not as a visually separate monitoring dashboard. Static asset upload is supporting material for route rules and should not appear before the route editor. Keep the facility detail as one framed work surface; route sections, domain groups, setup, assets, and deployment records are internal body sections or lightweight rows, not nested cards with their own shadow/border hierarchy.
- Facility page actions follow the shared operation model: detail-level save and manual sync live in the detail header; route/domain row edit and delete actions use visible text buttons in the row action area; full route editing uses a dialog.
- Entrance gateway route editing is the dominant workspace in the facility app page. Domain groups should be wide enough for the domain, domain-level gateway-node selector, HTTPS state, and actions. Routes should first appear as a compact list with edit/delete actions, and editing should open a dialog for static, redirect, or manual proxy fields so multiple routes do not flood the page with inputs.
- Uploaded site content is distributed through agent managed files and mounted read-only into the nginx container. Server directories still use read-only bind mounts from the target node.
- Facility reverse proxy deployment maintains a hidden managed application identity with id `facility-reverse-proxy` for application lifecycle deployment record queries. The facility configuration remains in `facility_app_configs`, the managed identity is filtered out of normal application lists, and each save/reconcile writes the latest `application_lifecycle_operations` plus per-node `application_lifecycle_targets` in `Store.LogDB()` shown on the facility app page.
 
## Agent Report Cache

- Container list reads use the latest `container_observations` snapshot saved from the agent report stream. They no longer pull `DockerContainers` during normal list or application reconciliation paths.
- The agent sends periodic full container snapshots and near-real-time change snapshots over the report stream. Panel replaces the per-server observation set atomically for each full report.
- Application reconciliation collectors read cached observations only. A server that reports a failed container, stale generation/spec hash, or managed file manifest drift can cause the application planner to create or reuse a lifecycle target for that server without redeploying other servers that are already healthy.
- `application_reconcile_states` keeps the exponential backoff state. Automatic reconciliation must honor `reconcile_next_run_at`; healthy observations clear failures only after the configured success streak.
- Agent reports and forced/manual reconciliation triggers must not create another application target while the same app/server conflict domain already has an active lifecycle target. The application planner owns that durable in-flight check before lifecycle rows and task rows are created; report collectors only provide the requested app/server scope.
- Agent report 的 `container_change` 触发在 collector 已确认应用/服务器发生停止、缺失、generation/spec hash 或 managed file drift 后，可绕过应用级 `application_reconcile_states.reconcile_next_run_at` 退避立即请求 planner；该绕过只作用于已 drift 的 app/server，不等同于 `force=true`，不得重部署同节点其它已满足 desired state 的应用。普通 scheduler 与非强制显式协调仍尊重应用级退避。
- Immediate application reconcile triggers call `PlanApplicationDeployment` directly and return no `application_target_*` task inputs. If the dispatcher cannot create a task log anchor after claiming a target, it marks that target `failed_retryable` with `task_create_failed` instead of leaving it pending.
