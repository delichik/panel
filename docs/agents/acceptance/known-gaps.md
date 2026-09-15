# 已知实现与验收差距

本文件记录编制验收规范时确认的现状缺口。它不是放宽标准的清单：对应代码被触及时，应修复缺口并增加证据，或经明确产品决策修改验收合同。关闭条目时保留编号、记录关闭依据，不复用编号。

| 编号 | 范围 | 当前差距与风险 | 关闭条件 |
| --- | --- | --- | --- |
| GAP-001 | 前端自动化 | 页面级 Playwright E2E、axe、多断点视觉/滚动回归未系统恢复；URL 恢复、脏状态离开、两阶段任务、上传下载、维护 token 隔离主要依赖人工检查 | 为关键页面族补浏览器级证据并关联 UI 验收编号 |
| GAP-002 | URL 状态 | servers、applications、dns 等页面的页码/选择没有完整从 URL 初始化并双向同步 | 满足 `UI-SHELL-035` 及各页面 `*-001`，覆盖前进/后退/刷新测试 |
| GAP-003 | 应用编辑返回 | 编辑器 footer 取消使用 `router.back()`，进入历史不同会回到不确定页面 | 改为合同指定的明确返回目标并覆盖直接访问与站内进入 |
| GAP-004 | 页面错误态 | 应用详情/runtime/files 加载失败主要用 toast，缺少作用域错误空态和重试 | 实现并测试 `UI-APP-002/003` 的错误恢复 |
| GAP-005 | 证书页面入口 | domains/self-signed 模式可能显示“生成 SSH”入口，跨越 keys 工作台边界 | 入口只在 keys 工作台出现并覆盖路由条件测试 |
| GAP-006 | 危险确认 | 多处仍用普通 Dialog 手写确认，影响范围、复选确认和 loading 锁不统一 | 全部高风险动作满足 `UI-SHELL-040` 与领域确认条目 |
| GAP-007 | 前端/后端证书类型 | `web/src/types/certificates.ts` 仍声明后端明确不输出的 `certificatePath/privateKeyPath` | 删除旧字段或形成安全且实际存在的新 API 合同，并更新测试 |
| GAP-008 | 密钥资产详情类型 | 前端要求 references/referenceCount，后端 detail DTO 未输出 | 前后端选择同一合同并覆盖被引用资产删除门禁测试 |
| GAP-009 | 系统版本类型 | 后端可能返回 `checkError`，前端 `VersionInfo` 未声明 | typed client 声明并在页面安全展示，或服务端合同明确移除 |
| GAP-010 | 备份旧文档引用 | 旧指引引用已不存在的 `web/src/api/backups.ts` 与 `web/src/types/api.ts` | 旧指引由本规范替代；后续引用只指向实际 maintenance/settings API 与类型 |
| GAP-011 | Agent report 频率说明 | 旧指引写每秒检查，当前实现为每 5 秒 | 本规范以当前实现/运行时设置为准；增加频率行为测试或统一实现 |
| GAP-012 | UFW Agent 准入 | 旧 compatible-Agent 准入说明与 `InstallUFW` 仍强制本地 privilege 的实现不同 | 明确唯一产品策略并同步 service、UI capability 与测试 |
| GAP-013 | 账户更新原子性 | 账户、JWT secret、nonce 通过多次写入更新，缺少跨写入事务，失败可能部分成功 | 使用单事务或等价原子机制满足账户合同失败不变式 |
| GAP-014 | 未来概览 API | 旧材料出现 `/overview/dashboard` 与 `baseVersion`，当前路由/服务未实现 | 保持不在当前合同；若实现，先新增 API/并发合同和前端消费测试 |
| GAP-015 | 应用迁移入口 | service 有迁移行为门禁，但主 applications 路由无公开迁移 HTTP 入口 | 保持不可达，或先定义权限、请求、任务、回滚与 UI 合同后新增入口 |
| GAP-016 | 设施编辑增强 | heartbeat、全局草稿配额、warning 确认、专用结构化恢复日志暂缓 | 不得假设存在；分别完成持久化/并发/API/UI 验收后启用 |
| GAP-017 | 旧 lifecycle 类型 | 应用 runtime/前端类型仍带 lifecycle 命名，但旧 CoordDB lifecycle 表和 dispatcher 已删除 | 逐步移除兼容命名；任何期间均不得恢复第二事实源 |
| GAP-018 | 容器完整详情 | 无通用 inspect API，report 摘要不含 command/created/mounts，但前端资源类型仍残留完整字段 | 清理旧字段，或新增独立受限 inspect 合同与按需 API，不扩大列表上报 |
| GAP-019 | 容器测试命名 | `TestContainerActionRejectsManagedApplicationContainer` 名称与断言相反，易误导维护者 | 重命名以表达“允许直接动作、由协调修复”，保留行为断言 |
| GAP-020 | Docker 操作物理队列 | 普通容器、orchestrator、设施应用由不同所有者组合，缺少证明所有写路径共享同一 per-server 物理队列的集成测试 | 增加跨模块并发场景并证明同服务器串行、跨服务器并行 |
| GAP-021 | 资源页面回归 | 首次空快照、请求竞态、桌面/窄屏滚动缺少完整 E2E | 补 `RES-UI` 对应浏览器测试与三档桌面/一档窄屏布局证据 |
| GAP-022 | Docker network 写操作 | network 当前只读，尚无系统网络保护和实时使用门禁合同 | 继续只读；若新增 mutation，先定义 `panel-apps` 保护、确认、队列和使用中拒绝 |
| GAP-023 | 多实例任务调度 | 通用任务队首缓存只适用于单 Panel 进程，共享 SQLite 多实例下没有 claim/fencing 一致性 | 保持单实例部署边界，或先实现数据库级 claim/lease/fencing |
| GAP-024 | Runtime event 丢弃观测 | 满缓冲丢弃计数/告警与批量失败重试没有稳定对外合同 | 事件仍非业务事实；新增指标/告警/重试合同和测试后关闭 |
| GAP-025 | 任务中心刷新 | 兼容任务页没有后台自动轮询，响应竞态、窄屏、无障碍证据不足 | 明确手动刷新语义或实现可见自动刷新，并补页面级验证 |
| GAP-026 | 无效详情保留字段 | `runtimeEventDetailRetentionDays` 与 `runtime_event_details` 属兼容遗留且当前无有效业务意义 | 在可迁移版本中删除，或先定义真实写入/读取/清理合同，禁止半复活 |
| GAP-027 | 复杂表单本地验证 | DNS、applications、certificates、settings 仍有大量输入依赖服务端拒绝 | 为高频/安全关键输入增加本地阻止，同时完整保留服务端结构化错误 |
| GAP-028 | 启动灾难兜底语言 | i18n 初始化失败前的启动兜底只有英文 | 保持仅灾难兜底例外；不得复制到正常页面，有可行初始化方案时补多语言 |

## 缺口处理规则

- `GAP-RULE-001`：任何变更若扩大某缺口的影响面、删除现有保护或让临时兼容成为新依赖，验收失败。
- `GAP-RULE-002`：关闭缺口必须引用实现改动、自动测试和对应验收编号；“现象未复现”不算关闭。
- `GAP-RULE-003`：若产品决定不再需要某验收目标，应先修改领域合同并写明迁移/兼容影响，再关闭缺口；不得先改代码后追认。
- `GAP-RULE-004`：缺口优先级由数据丢失/秘密泄漏/远端误操作 > 状态错误/不可恢复 > 可访问性/体验 > 命名与文档一致性排序。

