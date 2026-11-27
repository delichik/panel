# 活动事实（已废弃：由 events + JSONL 替代）

> [!WARNING]
> 本文描述的 AppDB `activity_events` / LogDB 投影账本已废弃。产品与过程诊断统一走：
>
> - JSONL 文件正文：`<dataRoot>/logs/panel-YYYYMMDD.jsonl`（`internal/platform/logging`）
> - 进程行索引：AppDB `events`（`internal/modules/events`）
> - 产品 API：`/api/v1/events*`、`/api/v1/logs?eventId=`（legacy `/api/v1/log-contexts*`）
>
> 验收以 `ENG-LOG-001..007`、`ENG-EVT-001`、`UI-EVT-004..007`、`UI-LOG-001..003` 为准（`UI-ACT-*`、`LOG-CTX-*`、`ENG-LOG-008` 已废弃）。下文仅作历史对照，不得指导新实现。

## 原产品职责（UI 分流仍有效）

| 入口 | 回答 | 现数据来源 |
| --- | --- | --- |
| `/events`（旧 `/operations`、`/tasks` 重定向） | 进程状态与摘要 | AppDB `events` |
| `/logs` | 诊断 JSONL 行 | `GET /api/v1/logs?eventId=` 或 `/events/{eventId}/logs` |

## 历史实现入口（勿再用）

- 原事实：`internal/platform/activitylog/`（Append / control triggers 已停写）
- 原查询：`internal/modules/activity/`（产品路由已停注册）
- 现装配：`logging.Configure` + `logging.SetContextStore(events.Store)`；HTTP 审计改写文件+ctx
