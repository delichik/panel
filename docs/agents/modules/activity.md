# 活动事实（已废弃：由统一日志 + log_contexts 替代）

> [!WARNING]
> 本文描述的 AppDB `activity_events` / LogDB 投影账本已废弃。产品与过程诊断统一走：
>
> - JSONL 文件正文：`<dataRoot>/logs/panel-YYYYMMDD.jsonl`（`internal/platform/logging`）
> - 单表关联：AppDB `log_contexts`（`internal/platform/logcontexts`）
> - 产品 API：`/api/v1/log-contexts*`（`internal/modules/logcontexts`）
>
> 验收以 `ENG-LOG-001..008`、`LOG-CTX-001..002`、`UI-ACT-001..004` 为准。下文仅作历史对照，不得指导新实现。

## 原产品职责（UI 分流仍有效）

| 入口 | 回答 | 现数据来源 |
| --- | --- | --- |
| `/operations` | 用户干了什么 | `log_contexts?origin=user` |
| `/tasks` | 系统自动干了什么 | `log_contexts?origin=system` |
| `/logs` | 诊断行 / 流 | `log_contexts` 列表 + `/{streamId}/lines` |

## 历史实现入口（勿再用）

- 原事实：`internal/platform/activitylog/`（Append / control triggers 已停写）
- 原查询：`internal/modules/activity/`（产品路由已停注册）
- 现装配：`logging.Configure` + `logging.SetContextStore`；HTTP 审计改写文件+ctx
