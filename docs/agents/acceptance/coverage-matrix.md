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

- 日志 `/activity` 移除定时刷新、summary/tail 探测、条数/新数据/快照编号展示；仅首次和用户操作读取，保留手动刷新、详情与游标分页。对应 `UI-ACT-002/003`，无路由、API、数据库或后台任务变更。

`web/src/router/index.ts` 当前含 47 个 `path` 声明（包含父路由、重定向、开发条件路由和 catch-all）。逐操作标准见 `ui-pages.md`，全局守卫及慢网路由进行中反馈见 `ui-shell-and-conventions.md`。

| 页面族 | 路径/入口 |
| --- | --- |
| 公开入口 | `/login`、`/maintenance/backup` |
| 外壳与兜底 | `/`、`/overview`、shell 内 catch-all |
| 服务器 | `/servers`、`/credentials` |
| 资源与安全 | `/resources/packages`、`containers`、`images`、`networks`、`volumes`、`firewall`、开发模式 `fail2ban`，以及旧 `/security*` 重定向 |
| 应用 | `/applications/apps`、`create`、`:applicationId/edit`、`facility-apps`、`:facilityKind`、`:facilityKind/config` |
| DNS 与证书 | `/dns/domains`、`/certificates/domains`、`self-signed`、`keys` |
| 可观测性 | `/activity`（事件/按操作）、`/system-events`、`/tasks`、`/debug` |
| 设置 | `/settings/general`、`security`、`certificates`、`agent`、`tailscale`、`system-certificates`、`system`、`backups`，以及 `/settings` 重定向 |

- `COV-UI-001`：同一路由内新增或调整字段选择、校验、空态等用户可见行为时，路由数量可以不变，但必须同步更新 `ui-pages.md` 的稳定验收项和对应前端测试。

- 本轮前端外壳/布局/动效优化：主导航抽出 `NavList.vue` 供桌面侧栏与移动抽屉复用（`aria-current` 深层归属、激活条、连续折叠动效、跳转链接），`MasterDetailLayout` 承担高度契约并支持 `detail-key` 详情进场，详情区可选横幅回归内部滚动区，Dialog 锁定背景滚动，Toast 增加图标/堆叠上限/悬停暂停，`useOverlayBehavior` 增加最上层 Escape 兜底；不新增路由、API、数据库表或后台任务，对应 `UI-SHELL-015/039/041/048/052/053/054/055/056/057`。
- 同轮窄屏适配：顶栏在 `<1024px` 粘性置顶，7 个主从工作台的主列表/服务器选择器在窄屏限高 `60dvh` 并保持内部滚动，使详情在一屏内可及；抽屉关闭按钮与导航行放大到 44px；自有 motion 类的 hover 反馈收进 `@media (hover: hover)`，交互控件加 `touch-action: manipulation`。不新增路由或组件，对应 `UI-SHELL-013`。
- 同轮窄屏主从单视图：`MasterDetailLayout` 新增 `back-label`/`has-detail`/`back` 单视图能力（`<xl` 一次只显示列表或详情，详情带返回列表操作），7 个主从页面接入并用 `useCompactViewport` 关闭窄屏自动选中首条；activity 保留自带的列表⇄详情切换。新增 `composables/useCompactViewport.ts` 与 `useCompactViewport.test.ts`，对应 `UI-SHELL-019/058`；不新增路由、API 或数据表。
- 验证证据（2026-09-26/27，WSL Debian / Node 22.23.3 / task 3.53.1）：`task test:web` = 48 个文件 259 项全通过；`task build:web`（`vue-tsc --noEmit && vite build`）退出码 0；构建产物抽查确认新类均已生成，且入场动画只保留 `backwards`（无 `both`）。测试期间修掉 `MasterDetailLayout` 模板前置注释导致的多根问题（会静默丢弃页面传入的 class）与 `views/applications` 自建 `.workspace-panel` 入场动画。

- 应用部署反馈：既有应用页增加部署规划失败、结果待核实、原因与活动入口，不新增路由；对应 `UI-APP-001/002/004`、`APP-PLAN-001`、`APP-RUN-004`。
- Debug 既有清理操作增加阶段/时间、刷新恢复、部分失败与结果待核实展示；保留危险勾选确认，不新增路由；对应 `UI-DBG-007/008`。
- 服务器创建/编辑重构：基础/高级分区、无凭据引导与弹窗内快捷创建、探测与保存解耦并标记过期、变量严格校验、创建后初始化任务跟踪与失败保留态，不新增路由；对应 `UI-SRV-003/004/005/011/012/013`，后端初始采集失败改为保留服务器并标记失败，对应 `SRV-SAVE-005/006`。
- Tailscale 收敛：新增 `/settings/tailscale` 设置分区（只写认证密钥、ACL 标签、容器 tailscale 状态与「应用 / 重连」）与服务器侧的加入/偏好开关、状态徽标和手动重试，路由总数由 46 增至 47；对应 `UI-SET-011`、`UI-SRV-014/015`、`TS-SET-*`、`TS-NODE-*`。

## 3. HTTP API 基线

当前逐项清单记录 171 个 `/api` method/path 组合，路由清单 SHA-256 为 `3756d56d2a98836a4e7596d6032004f5948c25a78f82d2623257864a86055511`。上一轮新增两条 Tailscale 收敛入口，并修正此前清单计数（`servers` 少计 5 条 NAT 端口子资源，来源表与逐项清单不一致）；此前删除了两条手动防火墙入口（`POST /servers/{id}/ufw/install`、`POST /servers/{id}/ufw/enable`，见 `AGT-FW-001..006` 与已废弃的 `UFW-API-005`），随后整体移除了 NAT 服务器能力及其 4 条端口子资源路由（`SRV-NAT-*` 已废弃）。来源计数如下：

| 注册来源 | 数量 | 验收领域 |
| --- | ---: | --- |
| bootstrap auth | 5 | 身份、设置与系统 |
| activity | 9 | 协调、任务与运行事件 |
| applications | 26 | 应用与设施应用；协调、任务与运行事件 |
| backups | 3 | 备份、恢复与诊断 |
| certificates/certs | 11 | DNS、证书与密钥资产 |
| certificates/dns | 9 | DNS、证书与密钥资产 |
| containers | 17 | 容器与资源 |
| facilityapps | 20 | 应用与设施应用 |
| keyassets | 14 | DNS、证书与密钥资产 |
| diagnostics | 7 | 备份、恢复与诊断 |
| metrics | 1 | 身份、设置与系统；服务器、安全与软件包 |
| overview | 4 | 身份、设置与系统 |
| packages | 4 | 服务器、安全与软件包 |
| credentials | 5 | 服务器、安全与软件包 |
| servers | 24 | 服务器、安全与软件包 |
| settings | 6 | 身份、设置与系统 |
| systeminfo | 1 | 身份、设置与系统 |
| tasks | 7 | 协调、任务与运行事件 |

- `COV-API-001`：路由清单测试失败时必须先确定是哪一个 method/path 改变，再更新消费者、Mock、验收项和期望哈希；不得只替换哈希让测试通过。

- 应用详情、列表摘要、runtime 增加可选 `planningError`（code/message/field/fileName/retryable/operationId/occurredAt/configVersion），列表 runtimeStatus 与 runtime.status 支持 `needs_attention`；对应 `APP-PLAN-001`、`APP-RUN-004`，API method/path 数量不变。
- 概览卡片数据 `GET /api/v1/overview/cards/{cardId}/data` 响应增加 `bucketSeconds`，指标卡只物化对应 kind 的序列并按 range/服务器数量服务端降采样，`since` 改为按桶向下对齐重算；前端改为数据落地时一次性派生视图并按桶后缀替换。不新增 method/path、表或后台任务；对应 `OBS-OVW-005/006/007/008`。
- 既有 GET/POST `/api/v1/debug/clear-runtime-data` 响应补齐 `runId/stage/failedStage/startedAt/finishedAt`，明确 `cleared` 与终态、并发请求语义；清理期间业务写入返回 `runtime_data_maintenance`，不增加接口；对应 `DIAG-CLR-001/005/006/007`。

- `COV-API-002`：维护导出/恢复的独立最小应用路由不计入上述主 Panel 171 条，但必须由备份恢复文档覆盖其认证、状态、密码、下载、重试、退出和清除 pending 操作。

- `COV-API-003`：171 个 method/path 的逐项映射见 [主 Panel API 路由逐项清单](api-route-inventory.md)；路由清单测试与该表必须同步变化。

- ~~新增 NAT 服务器端口子资源 `GET/POST /servers/{id}/nat-ports`、`PUT/DELETE /servers/{id}/nat-ports/{mappingID}`，以及 `servers.kind`、`servers.agent_public_port`、`nat_port_mappings`~~：**该能力已整体移除**（`SRV-NAT-001..005` 全部标为已废弃）。破坏性 ORM 同步会 drop `nat_port_mappings` 表与 `servers.kind`、`servers.agent_public_port` 两列（表/列均由模型清单驱动，无需手写 DDL）。

- 新增两条 Tailscale 收敛入口：`POST /api/v1/settings/tailscale/apply`（重新下发容器期望态并请求 panel-init 收敛，202 加当前容器实际态，未经 panel-init 监管时 `tailscale_container_unavailable`）与 `POST /api/v1/servers/{id}/tailscale/apply`（创建或复用 `server_tailscale_apply` 任务，202 加 taskId）。`GET/PUT /api/v1/settings/runtime` 增加 `tailscale` 分组（只写认证密钥、ACL 标签、容器实际态）；`servers` 由 ORM 增列迁移补齐 `tailscale_enabled`、`tailscale_prefer_agent`、`tailscale_prefer_interconnect` 三列，默认 0；对应 `TS-SET-001..005`、`TS-TASK-001`。

- 防火墙（UFW）改为 Agent 部署的自动前提：新增 `internal/modules/servers/agent_firewall.go`（部署前安装、放行 SSH/Agent/反向代理端口并启用；发行版不支持时以 `agent_firewall_unsupported` 拒绝），`runDeployAgent` 在完整安装与仅重启两条分支上都先执行该步骤；**删除** `POST /servers/{id}/ufw/install` 与 `POST /servers/{id}/ufw/enable` 两个路由、对应 handler、`InstallUFW`/`runInstallUFW`/`EnableUFW`/`runEnableUFW`、`server_ufw_install`/`server_ufw_enable` 任务类型，以及 `agent_bundle.go` 中硬编码 9786 的 UFW 行；前端删除安装/接管入口并新增新建服务器时的非阻断接管警示。主 Panel `method/path` 计数 177 → 175（清单与哈希同步）；无数据库结构变更、无新增表、无新增后台任务；对应 `AGT-FW-001..006`、已废弃的 `UFW-API-005`、`UFW-API-006/007`、`UI-SRV-010/015`、`UI-SEC-002/004`、`SRV-EVD-007`。（后续轮次又整体移除了 NAT 服务器能力，本条目中曾提到的 NAT 豁免随之删除。）

- 新增 GET `/facility-apps/reverse-proxy/diagnostics?serverId=`，配置 DTO 增加逐节点 deployments；对应 `FAC-RP-012/013`、`UI-FAC-012`。失败运行日志复用 errorDetail，按 ID/时间范围限量读取并降级，不新增 Agent RPC 字段、表或后台任务；对应 `ORCH-CTRL-008`。

- Agent 节点证书 `key_assets` 资产名称纳入稳定 serverID，服务器删除时尽力清理 `agent-server-<id>` 资产；修复重名/重建服务器部署 Agent 时违反名称唯一约束的 2067 错误。无新增 API、表或后台任务；对应 `AGT-CERT-004`、`SRV-DEL-005`。

- Agent 投递增加目标机 HTTP 下载路径：新增公开产物路由 `GET /agent/{version}/{platform}/panel-agent.gz`（`/api` 之外，含 `/agent/` 前缀 404 兜底）、运行时设置 `agent.downloadBaseUrl`、`agent.downloadVerifyTls`、`agent.transferTimeoutSeconds`（`runtime_settings` 既有表，无结构变更）、设置页 `settings/agent` 分区，以及构建产物 `panel-agent.gz` + `panel-agent.sha256`。主 Panel `method/path` 计数不因该能力变化，公开产物路由单独由 `API-COV-006` 与公开路由清单断言覆盖；对应 `AGT-DL-001..011`、`AGT-DEP-002/003/005`、`AGT-RPT-001`、`SRV-EVD-005/006`、`ENG-BUILD-002`。

## 4. 持久化基线

当前 ORM 模型有 43 个数据库内表声明；`application_revisions` 在 app 与 log 库分别存在，含义不同。coordination 库当前 0 个业务模型。

| 数据库 | 在管表 |
| --- | --- |
| app | `credentials`、`servers`、`package_updates`、`package_refreshes`、`fail2ban_configs`、`image_updates`、`image_refreshes`、`applications`、`application_persistent_locations`、`application_reconcile_states`、`container_observations`、`docker_resource_snapshots`、`application_revisions`、`jobs`、`dns_domains`、`dns_record_snapshots`、`application_edit_sessions`、`application_edit_session_files`、`application_edit_session_operations`、`application_files`、`application_instances`、`facility_app_configs`、`facility_static_assets`、`reverse_proxy_routes`、`facility_edit_sessions`、`facility_edit_session_assets`、`facility_edit_session_operations`、`storage_share_configs`、`storage_share_partitions`、`certificates`、`self_signed_certificates`、`key_assets`、`overview_card_configurations`、`runtime_settings`、`auth_state`、`auth_accounts` |
| log | `tasks`、`task_steps`、`task_logs`、`application_revisions`、`runtime_events`、`runtime_event_details`、`key_asset_exports` |
| coordination | 无业务表 |
| metrics | `metrics_snapshots` |

- `COV-DATA-001`：新增表必须列入正确数据库；同名跨库表必须分别说明用途、备份和迁移，不得按表名误连。
- `COV-DATA-002`：删除表必须同时处理模型、迁移、索引、外键、服务查询、备份恢复和旧版本升级；仅从 `AllModels` 移除不构成安全删除。
- `COV-DATA-003`：AppDB 的 `activity_events/activity_evidence_chunks` 使用专有只追加 schema，不计入 ORM 模型数量；LogDB 的 Activity projection/checkpoint/FTS 是可重建查询索引。原始事实与投影不得交换归属或互相替代。

- `applications.planning_error_json` 保存当前规划诊断，默认空值；随应用进入备份/恢复，旧数据库由 ORM 增列且不修改原有配置。失败与恢复事件继续使用 AppDB 活动日志，不新增表；对应 `APP-PLAN-001`、`ORCH-PLAN-006`。
- Debug 清理同步清除 `applications.planning_error_json`，保留资源版本和期望/观测事实。清理状态为当前进程状态，会话标记只保存未确认清理的 runId；本轮无数据库 schema 变更，对应 `DIAG-CLR-002/007`。

- Tailscale 不新增数据库表：`servers` 由 ORM 自动增列 `tailscale_enabled`、`tailscale_prefer_agent`、`tailscale_prefer_interconnect`（默认 0，旧库升级后即为未启用、不偏好），节点观测态保存在既有 `servers.traits` 的 `tailscale.*` 键；全局设置使用既有 `runtime_settings` 的新键 `tailscale.authKey`、`tailscale.tags`（换行编码，密钥不参与任何读取响应）；对应 `TS-NODE-001..005`、`TS-SET-001..003`。

- 内部例行任务的日志降噪不新增表：既有 `tasks` 表由 ORM 自动增列 `quiet`（`NOT NULL DEFAULT 0`，旧库升级后存量任务与未知类型保持 info），创建任务时由注册定义写入，Activity 控制触发器按 `NEW.quiet` 决定 `debug`/`info`；对应 `TASK-REG-003`。

## 5. 常驻与周期后台行为

- 应用启动在既有 `verify_running` 阶段验证连续运行 10 秒；短时退出按失败重试，自动巡检保留强制 Job 的退避与诊断。无新增 API、表、用户配置或阶段枚举；对应 `ORCH-CTRL-007`、`ORCH-RETRY-002`，覆盖普通应用和设施应用的 apply 路径。

| 后台行为 | 主要事实/输出 | 验收领域 |
| --- | --- | --- |
| 应用 orchestrator scanner/worker | app 库 Job、Instance desired/observed、revision、事件 | 协调、任务与运行事件 |
| 通用任务 worker | log 库 Task/step/log、远端副作用 | 协调、任务与运行事件及各触发域 |
| Task 清理 | 任务保留策略 | 协调、任务与运行事件 |
| metrics 清理 | metrics 快照保留策略 | 身份、设置与系统 |
| runtime event buffered writer/清理 | 运行事件及详情 | 协调、任务与运行事件 |
| Agent report collector | Server/Agent 状态、指标、资源观察、包状态、应用漂移、节点 Tailscale 观测 | 服务器、安全与软件包；容器与资源；协调 |
| 启动 Agent 检查 | 配置服务器的兼容与在线状态 | 服务器、安全与软件包 |
| systeminfo/update checker | 版本与更新状态 | 身份、设置与系统 |
| 证书续签、包/镜像/系统信息调度 | 由运行时设置产生 Task | 相应领域与协调、任务文档 |

- `COV-BG-001`：新增 goroutine、ticker、cron 或 queue consumer 必须说明启动顺序、停止等待、重复运行、失败重试、配置热更新和进程重启恢复。
- `COV-BG-002`：后台触发的远端写操作必须与等价手动操作共享服务端安全门和可追踪记录。

- Agent report collector 对应用规划失败逐应用隔离并继续扫描，自动相同诊断去重，不新增定时任务；对应 `ORCH-PLAN-006`。
- Tailscale 新增任务类型 `server_tailscale_apply`，由通用任务 worker 执行：保存节点开关后自动排队，手动 apply 创建或复用，缺少 `agent.tailscale` 能力只失败该任务而不触发整节点重装；不新增 ticker/cron，对应 `TS-TASK-001..004`。
- 周期 Agent 检查（`server_agent_check`）保持既有周期与状态写回，仅把成功巡检的活动日志事实降为 `debug`（定义声明 `Quiet`），失败仍为 `error`：不新增 ticker、任务类型、接口或表；对应 `AGT-STATE-005`、`TASK-REG-003`。
- 容器内 tailscale 自愈复用既有启动/30 秒 Agent 检查周期刷新实际态并在需要时触发收敛，不新增 ticker/cron；失败只更新容器实际态与 `lastError`，绝不终止或阻塞 Panel，对应 `TS-INIT-007`。
- 节点 tailnet 地址出现、变化、消失或互联偏好意图变化时按需重同步互联设施：排队存储导出协调任务，并在受影响节点确实作为网关或源站参与时重新渲染入口代理设施，无设施配置时为 no-op；对应 `TS-ADDR-009`、`FAC-STO-009`、`FAC-RP-014`。
- Debug 清理使用可恢复暂停门拒绝新 writer 并有界等待在途工作，保护节点报告、活动投影、任务收集及编辑会话清理；清理完成或失败都释放暂停门并恢复原运行状态，对应 `DIAG-CLR-006/008`。

## 6. 构建与交付产物

| 产物 | 必须包含/支持 |
| --- | --- |
| Web bundle | 懒加载页面 chunk、指纹化静态资源、生产 API client |
| `panel` | HTTPS API、静态托管、全部主模块和后台服务 |
| `panel-init` | 容器启动/维护恢复所需初始化行为；以 root 作为 PID 1、把 panel 子进程降权到非 root 用户，并拥有容器内 tailscaled 生命周期（期望态 `config.json`、LocalAPI/状态目录、loopback 控制面、SIGTERM/SIGINT 关停顺序） |
| `panel-agent` amd64/arm64 | 同一 RPC contract 与版本元数据、Docker/系统能力、无 server 子层的节点应用工作区及旧布局升级 |
| linux/amd64 image | amd64 Panel/init + 两架构 Agent bundle + Web + 容器内 `tailscale` 运行前提（Alpine `tailscale` 包与 `panel-init` 默认可执行文件路径） |
| linux/arm64 image | arm64 Panel/init + 两架构 Agent bundle + Web + 容器内 `tailscale` 运行前提（Alpine `tailscale` 包与 `panel-init` 默认可执行文件路径） |
| GHCR manifest | main 正式多架构标签或唯一 dev 标签集合 |
| GitHub Release | 仅 main 正式版本，在镜像验证成功后创建 |

## 7. 自动化覆盖现状

仓库当前基线为 115 个 Go `*_test.go` 文件与 29 个 Web `*.test.ts` 文件（记录于 2026-09-07）。本轮前端外壳/布局优化后 `web/src` 下实有 48 个 `*.test.ts`（新增 `components/shell/NavList.test.ts`、`components/templates/MasterDetailLayout.test.ts`、`composables/useCompactViewport.test.ts`）；Tailscale 一轮复核时仓库（排除 `tmp/`）实有 156 个 Go `*_test.go` 与 `web/src` 下 50 个 `*.test.ts`。数量不是质量目标，但下降或大规模重命名时必须解释覆盖是否迁移。

- `COV-TEST-001`：关键安全门至少有自动测试：认证、路径安全、非托管资源保护、迁移原子性、lease fencing、秘密脱敏、恢复/导出密码流程。
- `COV-TEST-002`：关键 UI 状态至少覆盖：会话守卫、顶栏语言持久化及失败回滚、日志页默认范围首次加载、慢网路由进行中反馈及清理、请求竞态、加载/错误/空态、确认弹窗、状态色语义、日期范围和编辑器输入。
- `COV-TEST-003`：本文件的静态数量只作为漏项信号，不允许通过增加空测试或合并路由来追求数量不变。
- `COV-TEST-004`：应用编辑器的嵌套代理 path 必须覆盖 Vue reactive 既有值回显所需的完整克隆与父草稿隔离；不得把 reactive Proxy 直接交给浏览器深拷贝 API。

- 本轮增加 `planning_outcome_test.go`、`planning_error_migration_test.go`，扩展容器巡检与前端应用模型测试，覆盖 `APP-PLAN-001`、`APP-RUN-004`、`ORCH-PLAN-006`；Vitest 缓存固定在仓库 `tmp/vitest`，遵守 `ENG-TEST-001/003` 的测试入口和中间产物约定。
- Tailscale 本轮新增 Go 测试 `internal/platform/tailscale/tailscale_test.go`、`internal/modules/settings/tailscale_test.go`、`internal/modules/servers/tailscale_report_test.go`、`internal/modules/facilityapps/interconnect_test.go`、`interconnect_sync_test.go`、`cmd/panel-init/tailscale_test.go`，并扩展 `internal/agent/endpoint/endpoint_test.go` 与 `cmd/panel-init/main_test.go`；前端新增 `web/src/views/settings/tailscale.test.ts`、`tailscaleSection.test.ts`（`web/src` 下 `*.test.ts` 由 48 增至 50）。真实 tailscaled、真实 tailnet 与宿主 `tun` 前提属于人工观察点，单元测试只使用替身控制面与临时目录；对应 `TS-EVD-001/002`、`TS-INIT-001..007`。
- Debug 清理新增状态生命周期、暂停门排空/超时、HTTP 豁免、worker 恢复原状态与资源保留回归；前端 `useRuntimeCleanup.test.ts` 覆盖刷新恢复、查询失败、请求结果丢失、进程重启结果丢失、runId 归属与卸载/隐藏行为，Mock 路由测试覆盖确认值、202、重复请求和阶段终态；对应 `DIAG-CLR-001..008`、`UI-DBG-007/008`。
