# 项目覆盖基线与变更检查矩阵

本文记录本规范在 2026-09-07 对当前仓库可观察入口的覆盖基线。它的用途是发现新增能力是否漏写验收合同，不是要求永久保持数量不变。

## 1. 覆盖守卫

- `COV-001`：新增、删除或改名任一 API、前端路由、后台任务类型、数据库表、运行时设置或构建产物时，必须在同一改动中更新本文件的数量/清单和对应领域验收项。
- `COV-002`：仅更新数量不算完成；新增入口必须有至少一个描述成功、失败与边界行为的稳定验收编号。
- `COV-003`：一个入口可由多个验收项共同覆盖；评审时必须能从入口定位到领域文档，不能依赖作者口头说明。
- `COV-004`：已存在但暂未在 UI 暴露的 API 或开发模式能力仍需服务端验收项，并在文档中标明可达条件。
- `COV-005`：历史兼容入口在移除前必须有迁移/拒绝策略；清单移除不代表可直接删除旧数据或远端资源。

## 2. 前端路由基线

- 应用详情的容器日志增加实例选择、单实例自动加载、手动刷新及请求取消；既有 logs API 必传 instanceId，未新增路由/API/数据库字段。对应 `APP-RUN-002`、`UI-APP-007`。

- 可观测性 `/activity` 与旧 `/operations`、`/tasks` 收敛为 `/events`（AppDB 进程行：state/module/level）+ `/logs`（JSONL 行：level+eventId，无 state）；旧路由兼容重定向。产品读走 `/api/v1/events*` 与 `/api/v1/logs`（`eventId` 可选；legacy `/api/v1/log-contexts*` 仍可用）。对应 `UI-EVT-004..007`、`UI-LOG-001..003` 与 `ENG-EVT-001`（`UI-ACT-*`、`ENG-LOG-008`、`LOG-CTX-*` 已废弃）；前端证据含 `IntentRecordsPage.test.ts`、`logs/index.test.ts`、`events`/`logs` API/mock、`activityRedirect`/`activityReference`。RunEngine Phase A 不改这条诊断读面，也不新增执行页路由（`ENG-RUN-002`，`UI-EVT-004` 不变）。

`web/src/router/index.ts` 当前含 30 条路由 `path:` 声明（含父路由、重定向与 catch-all）。逐操作标准见 `ui-pages.md`，全局守卫及慢网路由进行中反馈见 `ui-shell-and-conventions.md`。

| 页面族 | 路径/入口 |
| --- | --- |
| 公开入口 | `/login`、`/maintenance/backup` |
| 外壳与兜底 | `/`、`/overview`、shell 内 catch-all |
| 服务器 / 凭据 | `/servers`、`/credentials`（资源与安全并入服务器详情 tabs，无独立路由） |
| 应用 | `/applications/apps`、`create`、`:applicationId/edit`、`facility-apps`、`:facilityKind`、`:facilityKind/config` |
| DNS 与证书 | `/dns/domains`、`/certificates/domains`、`self-signed`、`keys` |
| 可观测性 | `/events`、`/logs`（`/operations`/`/tasks`/`/activity` 重定向）、`/debug` |
| 设置 | `/settings/general`、`security`、`certificates`、`agent`、`tailscale`、`system-certificates`、`system`、`backups`，以及 `/settings` 重定向 |

- `COV-UI-001`：同一路由内新增或调整字段选择、校验、空态等用户可见行为时，路由数量可以不变，但必须同步更新 `ui-pages.md` 的稳定验收项和对应前端测试。

- 本轮前端外壳/布局/动效优化：主导航抽出 `NavList.vue` 供桌面侧栏与移动抽屉复用（`aria-current` 深层归属、激活条、连续折叠动效、跳转链接），`MasterDetailLayout` 承担高度契约并支持 `detail-key` 详情进场，详情区可选横幅回归内部滚动区，Dialog 锁定背景滚动，Toast 增加图标/堆叠上限/悬停暂停，`useOverlayBehavior` 增加最上层 Escape 兜底；不新增路由、API、数据库表或后台任务，对应 `UI-SHELL-015/039/041/048/052/053/054/055/056/057`。
- 同轮窄屏适配：顶栏在 `<1024px` 粘性置顶，7 个主从工作台的主列表/服务器选择器在窄屏限高 `60dvh` 并保持内部滚动，使详情在一屏内可及；抽屉关闭按钮与导航行放大到 44px；自有 motion 类的 hover 反馈收进 `@media (hover: hover)`，交互控件加 `touch-action: manipulation`。不新增路由或组件，对应 `UI-SHELL-013`。
- 同轮窄屏主从单视图曾为 `MasterDetailLayout` 过渡能力；当前实体与操作/任务统一覆盖式详情，日志为 ListPage+Dialog，不再保留 activity 双栏切换。对应 `UI-SHELL-019/058`。
- 验证证据（2026-09-26/27，WSL Debian / Node 22.23.3 / task 3.53.1）：`task test:web` = 48 个文件 259 项全通过；`task build:web`（`vue-tsc --noEmit && vite build`）退出码 0；构建产物抽查确认新类均已生成，且入场动画只保留 `backwards`（无 `both`）。测试期间修掉 `MasterDetailLayout` 模板前置注释导致的多根问题（会静默丢弃页面传入的 class）与 `views/applications` 自建 `.workspace-panel` 入场动画。

- 内容区去分栏与服务器详情打磨：反向代理设施 `routes`/`nodes` Tabs、应用详情 `data` tab 与页头 ActivityLink、StorageShare/凭据/证书详情改为纵向叠放；服务器一级 tab 合并 Docker 为 `resources`+`subtab`（旧 tab 名兼容）、列表瘦列与工具栏收敛。不新增路由/API/表；对应 `UI-FAC-003`、`UI-APP-002/003`、`UI-SRV-016/017/018/019`、`UI-CRED-001`。
- 应用部署反馈：既有应用页增加部署规划失败、monitor 无进度时的系统核实中、有进度时的真实阶段、原因与活动入口，不新增路由；对应 `UI-APP-001/002/004`、`APP-PLAN-001`、`APP-RUN-004`。
- 应用状态改为节点三态计数（2026-10-29）：应用主徽标仅启停；列表/详情展示 `healthyInstanceCount`/`abnormalInstanceCount`/`deployingInstanceCount`；部署阶段与「系统核实中」挂在 Runtime 节点 `operation`；去掉应用级 `needs_attention` 主状态与单一「当前部署」块；`planningError`/`reconcileStopped` 为独立告警。不新增路由/表；摘要字段加法兼容。对应 `APP-READ-001`、`APP-RUN-001/004`、`APP-UI-002`、`UI-APP-001/002`。
- Debug 既有清理操作增加阶段/时间、刷新恢复、部分失败与系统确认中展示；保留危险勾选确认，不新增路由；对应 `UI-DBG-007/008`。
- 部署 remote-fence monitor：传输/lease 超时进入 `uncertainty`+`execution_monitoring`，投影 Agent step 报告；无进度才「系统核实中」，有进度为普通收敛；证据窗口耗尽终态 `execution_evidence_unreachable`；人工 resolve 仅为破局 API。对应 `ORCH-LEASE-003..005`、`ORCH-RETRY-001`、`ORCH-CTRL-009`、`TASK-WORK-003`、`APP-RUN-004`。
- 派活窗口（默认 15s）后 worker 释放、monitor 独占结算，对应 `ORCH-CTRL-010`。
- 服务器创建/编辑重构：基础/高级分区、无凭据引导与弹窗内快捷创建、探测与保存解耦并标记过期、变量严格校验、创建后初始化任务跟踪与失败保留态，不新增路由；对应 `UI-SRV-003/004/005/011/012/013`，后端初始采集失败改为保留服务器并标记失败，对应 `SRV-SAVE-005/006`。
- Tailscale 收敛：新增 `/settings/tailscale` 设置分区（只写认证密钥、ACL 标签、容器 tailscale 状态与「应用 / 重连」）与服务器侧的加入/偏好开关、状态徽标和手动重试，路由总数由 46 增至 47；对应 `UI-SET-011`、`UI-SRV-014/015`、`TS-SET-*`、`TS-NODE-*`。
- Tailscale 关闭与 DNS/路由偏好（2026-10-14）：节点/容器 `tailscale up` 固定 `--accept-dns=false --accept-routes=false`；关闭意图按保存前启用态排队 `TailscaleDisable`（`tailscale logout` 离开 tailnet + `systemctl disable --now`；容器侧关闭期望态同样先 logout 再停进程），不得因同事务已清观测而跳过远端注销，不得仅 `down`/停进程而让设备仍留在 tailnet；对应 `TS-NODE-004`、`TS-AGT-003/004`、`TS-INIT-002`。
- Tailscale 节点注册名（2026-10-29）：托管节点加入/收敛时 `--hostname` 必须为 Panel `servers.id` 合法化形式（`_`→`-`），不得回落 OS 主机名；已登录节点经 `tailscale set --hostname=…` 收敛；Panel 容器仍固定 `seamark-panel`。对应 `TS-AGT-003`。
- 表格化 + 覆盖式详情 + 资源/安全并入服务器详情（2026-10-13）：实体工作台（servers/credentials/dns/certificates/applications）由主从卡片列表改为全宽表格 + 行操作区，点击「详情」后覆盖详情并在 URL 保留 `server/credential/domain/selected/application` 与 `page`；新增 `TableDetailLayout.vue` 与 `Table` 的 `active-key`/`#empty`/`#head-<key>` 能力，`Tabs` 增加 `keep-panels`；服务器详情新增 10 个一级 tab（概况/监控/Agent/Tailscale/容器/镜像/网络/存储卷/软件包/防火墙），原 `views/resources`、`views/security` 页面、`ServerContextSelector`、`useCompactViewport` 与 `/resources/*`、`/security/*` 共 11 条路由声明删除（路由声明 41 → 30），资源导航分组移除；`ConfirmDialog` 增加 `confirmDisabled`。不新增 API method/path、数据库表、后台任务或后端行为（API 清单与哈希不变），对应 `UI-SHELL-019/054/055/058/059/060/061/062`、`UI-SRV-001..019`、`UI-CRED-*`、`UI-DNS-*`、`UI-CERT-*`、`UI-APP-001..011`、`UI-SET-006`；`UI-RES-*`、`UI-SEC-*` 全部标注已废弃并指向替代项。

## 3. HTTP API 基线

主 Panel `/api/v1` method/path 组合数与路由清单 SHA-256 以 `internal/bootstrap/panel/routes_manifest_test.go` 中 `TestAPIRouteManifest` 的 `wantCount` / `wantHash` 为准（events/logs 重命名后须重跑测试并更新期望值；本文不硬编码哈希）。本轮新增产品读路由 `/api/v1/events*`（含 export、`{eventId}/logs|lines`）与 `/api/v1/logs`，保留 legacy `/api/v1/log-contexts*` 别名，对应 `ENG-EVT-001`。来源计数如下（注册模块名，非产品表名）：

| 注册来源 | 数量 | 验收领域 |
| --- | ---: | --- |
| bootstrap auth | 5 | 身份、设置与系统 |
| events | 10 | 工程质量与发布（`ENG-EVT-001`；含 6 条主路由 + 4 条 legacy `log-contexts` 别名，路由解析去重后计入清单） |
| applications | 26 | 应用与设施应用；协调、任务与运行事件 |
| backups | 3 | 备份、恢复与诊断 |
| certificates/certs | 11 | DNS、证书与密钥资产 |
| certificates/dns | 9 | DNS、证书与密钥资产 |
| containers | 17 | 容器与资源 |
| facilityapps | 21 | 应用与设施应用 |
| keyassets | 14 | DNS、证书与密钥资产 |
| diagnostics | 7 | 备份、恢复与诊断 |
| metrics | 1 | 身份、设置与系统；服务器、安全与软件包 |
| overview | 4 | 身份、设置与系统 |
| packages | 4 | 服务器、安全与软件包 |
| credentials | 5 | 服务器、安全与软件包 |
| servers | 24 | 服务器、安全与软件包 |
| settings | 6 | 身份、设置与系统 |
| systeminfo | 1 | 身份、设置与系统 |
| tasks（HTTP：`/executions`；AppDB：`event_runs`/`event_run_steps`） | 7 | 协调、任务与运行事件 |

- RunEngine 不新增 method/path。`/api/v1/executions*` 是 Run API；Run 存储是 `event_runs`/`event_run_steps`（部署行 `type='application_deploy'`）。`/api/v1/events*` 与 `/api/v1/logs` 仍是诊断读面。对应 `ENG-RUN-001..005`、`ENG-RET-001`、`TASK-DISPATCH-001..003`（`GAP-030` 已关）。

- `COV-API-001`：路由清单测试失败时必须先确定是哪一个 method/path 改变，再更新消费者、Mock、验收项和期望哈希；不得只替换哈希让测试通过。

- 应用详情、列表摘要、runtime 增加可选 `planningError`（code/message/field/fileName/retryable/operationId/occurredAt/configVersion），列表 runtimeStatus 与 runtime.status 支持 `needs_attention`；对应 `APP-PLAN-001`、`APP-RUN-004`，API method/path 数量不变。
- 概览卡片数据 `GET /api/v1/overview/cards/{cardId}/data` 响应增加 `bucketSeconds`，指标卡只物化对应 kind 的序列并按 range/服务器数量服务端降采样，`since` 改为按桶向下对齐重算；前端改为数据落地时一次性派生视图并按桶后缀替换。不新增 method/path、表或后台任务；对应 `OBS-OVW-005/006/007/008`。
- 既有 GET/POST `/api/v1/debug/clear-runtime-data` 响应补齐 `runId/stage/failedStage/startedAt/finishedAt`，明确 `cleared` 与终态、并发请求语义；清理期间业务写入返回 `runtime_data_maintenance`，不增加接口；对应 `DIAG-CLR-001/005/006/007`。

- `COV-API-002`：维护导出/恢复的独立最小应用路由不计入上述主 Panel 167 条，但必须由备份恢复文档覆盖其认证、状态、密码、下载、重试、退出和清除 pending 操作。

- `COV-API-003`：method/path 逐项映射见 [主 Panel API 路由逐项清单](api-route-inventory.md)；条目数须与 `routes_manifest_test` 一致，表格与测试必须同步变化。

- ~~新增 NAT 服务器端口子资源 `GET/POST /servers/{id}/nat-ports`、`PUT/DELETE /servers/{id}/nat-ports/{mappingID}`，以及 `servers.kind`、`servers.agent_public_port`、`nat_port_mappings`~~：**该能力已整体移除**（`SRV-NAT-001..005` 全部标为已废弃）。破坏性 ORM 同步会 drop `nat_port_mappings` 表与 `servers.kind`、`servers.agent_public_port` 两列（表/列均由模型清单驱动，无需手写 DDL）。

- 新增两条 Tailscale 收敛入口：`POST /api/v1/settings/tailscale/apply`（重新下发容器期望态并请求 panel-init 收敛，202 加当前容器实际态，未经 panel-init 监管时 `tailscale_container_unavailable`）与 `POST /api/v1/servers/{id}/tailscale/apply`（创建或复用 `server_tailscale_apply` 任务，202 加 taskId）。`GET/PUT /api/v1/settings/runtime` 增加 `tailscale` 分组（只写认证密钥、ACL 标签、容器实际态）；`servers` 由 ORM 增列迁移补齐 `tailscale_enabled`、`tailscale_prefer_agent`、`tailscale_prefer_interconnect` 三列，默认 0；对应 `TS-SET-001..005`、`TS-TASK-001`。

- 防火墙（UFW）改为 Agent 部署的自动前提：新增 `internal/modules/servers/agent_firewall.go`（部署前安装、放行 SSH/Agent/反向代理端口并启用；发行版不支持时以 `agent_firewall_unsupported` 拒绝），`runDeployAgent` 在完整安装与仅重启两条分支上都先执行该步骤；**删除** `POST /servers/{id}/ufw/install` 与 `POST /servers/{id}/ufw/enable` 两个路由、对应 handler、`InstallUFW`/`runInstallUFW`/`EnableUFW`/`runEnableUFW`、`server_ufw_install`/`server_ufw_enable` 任务类型，以及 `agent_bundle.go` 中硬编码 9786 的 UFW 行；前端删除安装/接管入口并新增新建服务器时的非阻断接管警示。主 Panel `method/path` 计数 177 → 175（清单与哈希同步）；无数据库结构变更、无新增表、无新增后台任务；对应 `AGT-FW-001..006`、已废弃的 `UFW-API-005`、`UFW-API-006/007`、`UI-SRV-010/015`、`UI-SEC-002/004`、`SRV-EVD-007`。（后续轮次又整体移除了 NAT 服务器能力，本条目中曾提到的 NAT 豁免随之删除。）

- Docker 成为 Agent 部署的第二个环境前提：新增 `internal/modules/servers/agent_docker.go`（默认本机 socket 下探测缺失则安装 `docker.io`、启用服务并在固定窗口内等待就绪；自定义 Docker host 完全跳过；失败以 `agent_docker_failed` 结束），`runDeployAgent` 在防火墙步骤之后、签发与投递之前对完整安装与仅重启两条分支执行；`remoteops` 新增 `DockerStatusScript`/`ParseDockerStatus`，后端新增 `agent_docker_failed` 中文翻译。无新增路由、数据库结构、表或后台任务；主 Panel `method/path` 计数不变。对应 `AGT-DKR-001..004`。

- 新增 GET `/facility-apps/reverse-proxy/diagnostics?serverId=`，配置 DTO 增加逐节点 deployments；对应 `FAC-RP-012/013`、`UI-FAC-012`。失败运行日志复用 errorDetail，按 ID/时间范围限量读取并降级，不新增 Agent RPC 字段、表或后台任务；对应 `ORCH-CTRL-008`。

- 破局人工核实 uncertainty 成功/失败后调用与自动收敛相同的 OnSucceeded/OnFailed 钩子（仍不伪造 observed）；自动证据超时失败同样走 OnFailed。对应 `ORCH-CTRL-009`、`ORCH-LEASE-005`。

- Agent 节点证书 `key_assets` 资产名称纳入稳定 serverID，服务器删除时尽力清理 `agent-server-<id>` 资产；修复重名/重建服务器部署 Agent 时违反名称唯一约束的 2067 错误。无新增 API、表或后台任务；对应 `AGT-CERT-004`、`SRV-DEL-005`。

- Agent 投递增加目标机 HTTP 下载路径：新增公开产物路由 `GET /agent/{version}/{platform}/panel-agent.gz`（`/api` 之外，含 `/agent/` 前缀 404 兜底）、运行时设置 `agent.downloadBaseUrl`、`agent.downloadVerifyTls`、`agent.transferTimeoutSeconds`（`runtime_settings` 既有表，无结构变更）、设置页 `settings/agent` 分区，以及构建产物 `panel-agent.gz` + `panel-agent.sha256`。主 Panel `method/path` 计数不因该能力变化，公开产物路由单独由 `API-COV-006` 与公开路由清单断言覆盖；对应 `AGT-DL-001..011`、`AGT-DEP-002/003/005`、`AGT-RPT-001`、`SRV-EVD-005/006`、`ENG-BUILD-002`。

- 卷/网络刷新、Panel TLS 重置、证书手动续签、密钥资产重签/重生成/导出/导入改为经已注册 Execute 的 `CreateAndRun` 或 `Create`+`Manager.Run`。续签、重签、重生成、导出、导入的 HTTP 成功响应为 202 + `taskId`（续签 `renewed=false`）；导出在任务完成前不可下载。自签 leaf 续期 HTTP 仍为 200：服务等待 `key_asset_tls_reissue` 完成后再返回新证书（`CRT-SELF-004`）。无新增 API、表或任务类型。对应 `NET-REF-001`、`VOL-REF-001`、`CRT-ACME-010`、`KAS-MUT-001/002`、`KAS-EXP-002`、`KAS-IMP-005/007`、`KAS-SYS-005`、`AGT-CERT-003`、`UI-CERT-004/011/013/014`。
- 修复服务器删除被 `jobs.server_id` 的 RESTRICT 外键阻塞的问题：删除事务现在先终结并删除该服务器的全部协调 Job（活跃 Job 置 `cancelled` 并释放租约，清空 `applications.job_id`），并一并清理 `application_persistent_locations`、`facility_app_configs.deployment_server_ids_json`、`reverse_proxy_routes.origin_server_ids`/`any_access_json`、`storage_share_configs.servers_json` 与 `storage_share_partitions`。因删除而失去全部部署目标的应用由系统自动停用（`enabled=0`，活动日志写 `application.disabled`）。删除提交后入口代理与存储共享按修剪后的配置重新收敛（新增 `servers.ServerReferenceReconciler` 注入点，装配层接入设施服务）。前端新增 `activity.applicationDisabled` 词条（en / zh-CN）渲染该停用事件。无新增 API、表、列或后台任务；主 Panel `method/path` 计数不变；对应 `SRV-DEL-006/007/008/009`、`SRV-EVD-002`。

## 4. 持久化基线

当前 ORM 模型有 44 个数据库内表声明；`application_revisions` 在 app 与 log 库分别存在，含义不同。coordination 库当前 0 个业务模型。

| 数据库 | 在管表 |
| --- | --- |
| app | `credentials`、`servers`、`package_updates`、`package_refreshes`、`fail2ban_configs`、`image_updates`、`image_refreshes`、`applications`、`application_persistent_locations`、`application_reconcile_states`、`container_observations`、`docker_resource_snapshots`、`application_revisions`、`dns_domains`、`dns_record_snapshots`、`application_edit_sessions`、`application_edit_session_files`、`application_edit_session_operations`、`application_files`、`application_instances`、`facility_app_configs`、`facility_static_assets`、`reverse_proxy_routes`、`facility_edit_sessions`、`facility_edit_session_assets`、`facility_edit_session_operations`、`storage_share_configs`、`storage_share_partitions`、`certificates`、`self_signed_certificates`、`key_assets`、`overview_card_configurations`、`runtime_settings`、`auth_state`、`auth_accounts`、`events`、`event_runs`、`event_run_steps` |
| log | `application_revisions`、`runtime_events`、`runtime_event_details`、`key_asset_exports` |
| coordination | 无业务表 |
| metrics | `metrics_snapshots` |

- `COV-DATA-001`：新增表必须列入正确数据库；同名跨库表必须分别说明用途、备份和迁移，不得按表名误连。
- `COV-DATA-002`：删除表必须同时处理模型、迁移、索引、外键、服务查询、备份恢复和旧版本升级；仅从 `AllModels` 移除不构成安全删除。
- `COV-DATA-003`：AppDB 遗留 `activity_events/activity_evidence_chunks` 仍为专有只追加 schema（不计入 ORM 模型数量），但新产品/过程事实改走 JSONL + AppDB `events`（进程行索引）；LogDB 的 Activity projection/checkpoint/FTS 保留为遗留只读索引，启动不再投影新事实。

- `applications.planning_error_json` 保存当前规划诊断，默认空值；随应用进入备份/恢复，旧数据库由 ORM 增列且不修改原有配置。失败与恢复事件继续使用 AppDB 活动日志，不新增表；对应 `APP-PLAN-001`、`ORCH-PLAN-006`。
- Debug 清理同步清除 `applications.planning_error_json`，保留资源版本和期望/观测事实。清理状态为当前进程状态，会话标记只保存未确认清理的 runId；本轮无数据库 schema 变更，对应 `DIAG-CLR-002/007`。

- Tailscale 不新增数据库表：`servers` 由 ORM 自动增列 `tailscale_enabled`、`tailscale_prefer_agent`、`tailscale_prefer_interconnect`（默认 0，旧库升级后即为未启用、不偏好），节点观测态保存在既有 `servers.traits` 的 `tailscale.*` 键；全局设置使用既有 `runtime_settings` 的新键 `tailscale.authKey`、`tailscale.tags`（换行编码，密钥不参与任何读取响应）；对应 `TS-NODE-001..005`、`TS-SET-001..003`。

- 内部例行 event run 的日志降噪不新增表：AppDB `event_runs` 由 ORM 自动增列 `quiet`（`NOT NULL DEFAULT 0`，自旧 `tasks` 迁移后存量行保持 info），创建 run 时由注册定义写入；对应 `TASK-REG-003`。
- 部署 Run 已并入 `event_runs`（`type='application_deploy'`），ORM 不再注册 `jobs`；生产 claim/Execute 经 Manager。诊断表 `events` 不并入 Run。对应 `ENG-RUN-001`、`ENG-RET-001`（`GAP-030` 已关）。

## 5. 常驻与周期后台行为

- 应用启动在既有 `verify_running` 阶段验证连续运行 10 秒；短时退出按失败重试，自动巡检保留强制 Job 的退避与诊断。无新增 API、表、用户配置或阶段枚举；对应 `ORCH-CTRL-007`、`ORCH-RETRY-002`，覆盖普通应用和设施应用的 apply 路径。

| 后台行为 | 主要事实/输出 | 验收领域 |
| --- | --- | --- |
| 应用 orchestrator scanner/worker | app 库 Job、Instance desired/observed、revision、事件 | 协调、任务与运行事件 |
| Event executor worker（queue/stale/orphan） | `event_runs` 执行与恢复；**不含**周期 ticker | 协调、任务与运行事件及各触发域 |
| 模块自有周期 SchedulerLoop | `StartPeriodic`：agent 检查、信息采集、镜像/包刷新、证书续签、Panel TLS、应用 reconcile 等。除应用 reconcile 外，经 `TriggerPeriodicNow`/`CollectInputs` 创建 event run。应用 reconcile 的 CollectInputs 只请求 Planner，不创建 event run（`TASK-PER-003`） | 相应领域与 `TASK-PER-001` |
| Task 清理 | 任务保留策略 | 协调、任务与运行事件 |
| metrics 清理 | metrics 快照保留策略 | 身份、设置与系统 |
| runtime event buffered writer/清理 | 运行事件及详情 | 协调、任务与运行事件 |
| Agent report collector | Server/Agent 状态、指标、资源观察、包状态、应用漂移、节点 Tailscale 观测 | 服务器、安全与软件包；容器与资源；协调 |
| 启动 Agent 检查 | 配置服务器的兼容与在线状态 | 服务器、安全与软件包 |
| systeminfo/update checker | 版本与更新状态 | 身份、设置与系统 |

- Phase A 的 orchestrator worker 与 event executor worker 仍然分开。单一 RunEngine 不是本阶段的后台行为变更；remote-fence / local-timeout 与三类保留见 `ENG-RUN-004`、`ENG-RET-001`。已注册 Execute 的通用变更仍须 `CreateAndRun`（`TASK-DISPATCH-001..003`）。
- `COV-BG-001`：新增 goroutine、ticker、cron 或 queue consumer 必须说明启动顺序、停止等待、重复运行、失败重试、配置热更新和进程重启恢复。
- `COV-BG-002`：后台触发的远端写操作必须与等价手动操作共享服务端安全门和可追踪记录。

- Agent report collector 对应用规划失败逐应用隔离并继续扫描，自动相同诊断去重，不新增定时任务；对应 `ORCH-PLAN-006`。
- Tailscale 新增任务类型 `server_tailscale_apply`，由通用任务 worker 执行：保存节点开关后自动排队，手动 apply 创建或复用，缺少 `agent.tailscale` 能力只失败该任务而不触发整节点重装；不新增 ticker/cron，对应 `TS-TASK-001..004`。
- 周期 Agent 检查（`server_agent_check`）由 servers 模块 `StartPeriodic` 自有 loop（5 分钟）驱动，经 `CollectInputs`/`TriggerPeriodicNow` 入 event executor 框架；成功巡检 JSONL 为 `debug`（`Quiet`），失败仍为 `error`：不经 event executor 中央 PeriodicRunner；对应 `AGT-STATE-005`、`TASK-REG-003`、`TASK-PER-001`。
- 应用协调仍由 containers `StartPeriodic` 每 5 秒调用 `application_reconcile` CollectInputs，以及业务侧 `TriggerApplicationReconcile`。两者都直接请求 Planner 写 `event_runs`（`type='application_deploy'`），不经 Manager 创建通用 event run。已删除任务类型 `application_reconcile_batch`（noop Execute）。无新增 API 或 ticker；存量非终态监控壳由 `cancel_application_reconcile_monitoring_shells` 取消。对应 `TASK-PER-003`、`ORCH-DRIFT-001`、`ENG-RUN-001`。
- Panel 进程日志统一经 `platform/logging`（context 带 `eventId`+`module`+等级，默认 JSONL 文件 + stdout，AppDB `events` 索引）；产品读 API 为 `/api/v1/events*` 与 `/api/v1/logs`（`eventId` 可选）；对应 `ENG-LOG-001..007` 与 `ENG-EVT-001`。
- 进程日志文件落在 `<dataRoot>/logs/panel-YYYYMMDD.jsonl`（随 dataRoot 进入全量备份）；对应 `ENG-LOG-006`、`BKP-API-005`。
- 容器内 tailscale 自愈复用既有启动/30 秒 Agent 检查周期刷新实际态并在需要时触发收敛，不新增 ticker/cron；失败只更新容器实际态与 `lastError`，绝不终止或阻塞 Panel，对应 `TS-INIT-007`。
- 节点 tailnet 地址出现、变化、消失或互联偏好意图变化时按需重同步互联设施：排队存储导出协调任务，并在受影响节点确实作为网关或源站参与时重新渲染入口代理设施，无设施配置时为 no-op；对应 `TS-ADDR-009`、`FAC-STO-009`、`FAC-RP-014`。
- Debug 清理使用可恢复暂停门拒绝新 writer 并有界等待在途工作，保护节点报告、活动投影、任务收集及编辑会话清理；清理完成或失败都释放暂停门并恢复原运行状态，对应 `DIAG-CLR-006/008`。

## 6. 构建与交付产物

| 产物 | 必须包含/支持 |
| --- | --- |
| Web bundle | 懒加载页面 chunk、指纹化静态资源、生产 API client |
| `panel` | HTTPS API、静态托管、全部主模块和后台服务 |
| `panel-init` | 容器启动/维护恢复所需初始化行为；以 root 作为 PID 1、把 panel 子进程降权到非 root 用户，并拥有容器内 tailscaled 生命周期（期望态 `config.json`、LocalAPI/状态目录、`<dataRoot>/run/panel-control.sock` Unix socket 控制面、SIGTERM/SIGINT 关停顺序） |
| `panel-agent` amd64/arm64 | 同一 RPC contract 与版本元数据、Docker/系统能力、无 server 子层的节点应用工作区及旧布局升级 |
| linux/amd64 image | amd64 Panel/init + 两架构 Agent bundle + Web + 容器内 `tailscale` 运行前提（Alpine `tailscale` 包与 `panel-init` 默认可执行文件路径） |
| linux/arm64 image | arm64 Panel/init + 两架构 Agent bundle + Web + 容器内 `tailscale` 运行前提（Alpine `tailscale` 包与 `panel-init` 默认可执行文件路径） |
| GHCR manifest | main 正式多架构标签或唯一 dev 标签集合 |
| GitHub Release | 仅 main 正式版本，在镜像验证成功后创建 |

## 7. 自动化覆盖现状

仓库当前基线为 115 个 Go `*_test.go` 文件与 29 个 Web `*.test.ts` 文件（记录于 2026-09-07）。Tailscale 一轮复核时 `web/src` 下 50 个 `*.test.ts`；表格化 + 覆盖详情一轮后 `web/src` 下 53 个 `*.test.ts`（删除 `useCompactViewport`/旧资源/安全模型测试，新增 `Table.test.ts`、`TableDetailLayout.test.ts`、`ConsolePage.test.ts`、`views/servers/index.test.ts`、`servers/tabs/*.test.ts`），`task test:web` 实跑 53 个文件 / 293 项通过。数量不是质量目标，但下降或大规模重命名时必须解释覆盖是否迁移。

- `COV-TEST-001`：关键安全门至少有自动测试：认证、路径安全、非托管资源保护、迁移原子性、lease fencing、秘密脱敏、恢复/导出密码流程。
- `COV-TEST-002`：关键 UI 状态至少覆盖：会话守卫、顶栏语言持久化及失败回滚、日志页默认范围首次加载、慢网路由进行中反馈及清理、请求竞态、加载/错误/空态、确认弹窗、状态色语义、日期范围和编辑器输入。
- `COV-TEST-003`：本文件的静态数量只作为漏项信号，不允许通过增加空测试或合并路由来追求数量不变。
- `COV-TEST-004`：应用编辑器的嵌套代理 path 必须覆盖 Vue reactive 既有值回显所需的完整克隆与父草稿隔离；不得把 reactive Proxy 直接交给浏览器深拷贝 API。

- 本轮增加 `planning_outcome_test.go`、`planning_error_migration_test.go`，扩展容器巡检与前端应用模型测试，覆盖 `APP-PLAN-001`、`APP-RUN-004`、`ORCH-PLAN-006`；Vitest 缓存固定在仓库 `tmp/vitest`，遵守 `ENG-TEST-001/003` 的测试入口和中间产物约定。
- Tailscale 本轮新增 Go 测试 `internal/platform/tailscale/tailscale_test.go`、`internal/modules/settings/tailscale_test.go`、`internal/modules/servers/tailscale_report_test.go`、`internal/modules/facilityapps/interconnect_test.go`、`interconnect_sync_test.go`、`cmd/panel-init/tailscale_test.go`，并扩展 `internal/agent/endpoint/endpoint_test.go` 与 `cmd/panel-init/main_test.go`；前端新增 `web/src/views/settings/tailscale.test.ts`、`tailscaleSection.test.ts`（`web/src` 下 `*.test.ts` 由 48 增至 50）。回归修复新增 `internal/platform/panelsupervision` 的监督参数合同用例与镜像构建冒烟（`TS-INIT-005`、`ENG-BUILD-009`）；panel-init 控制面由 loopback TCP 端口改为 `<dataRoot>/run/panel-control.sock` Unix domain socket（重启与 tailscale 共用，`TS-INIT-005`、`BKP-API-003`）；容器与节点两侧的 tailscale 登录密钥都改经 0600 临时文件以 `--auth-key=file:` 传递（`TS-INIT-006`、`TS-AGT-003`），容器侧 `tailscale up` 增加超时。真实 tailscaled、真实 tailnet 与宿主 `tun` 前提属于人工观察点，单元测试只使用替身控制面与临时目录；对应 `TS-EVD-001/002`、`TS-INIT-001..007`。
- Debug 清理新增状态生命周期、暂停门排空/超时、HTTP 豁免、worker 恢复原状态与资源保留回归；前端 `useRuntimeCleanup.test.ts` 覆盖刷新恢复、查询失败、请求结果丢失、进程重启结果丢失、runId 归属与卸载/隐藏行为，Mock 路由测试覆盖确认值、202、重复请求和阶段终态；对应 `DIAG-CLR-001..008`、`UI-DBG-007/008`。
