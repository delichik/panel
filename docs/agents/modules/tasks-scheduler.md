# 任务与调度

## 适用场景

修改后台任务、任务状态、步骤、日志、重试、手动运行、周期采集、软件包调度或证书续签调度时，先读本文档。

## 关键入口

- 任务模型和服务：`internal/tasks/`
- 周期调度：`internal/scheduler/scheduler.go`
- 路由装配：`internal/app/app.go`
- 前端任务中心：`web/src/features/tasks/pages/TaskCenterPage.vue`
- 前端任务操作：`web/src/features/tasks/taskOperations.ts`
- 任务日志组件：`web/src/components/tasks/TaskLogPanel.vue`
- API：`web/src/api/tasks.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- `GET /api/v1/tasks`
- `GET /api/v1/tasks/{id}`
- `GET /api/v1/tasks/{id}/logs`
- `GET /api/v1/tasks/{id}/steps`
- `POST /api/v1/tasks/{id}/retry`
- `POST /api/v1/tasks/{id}/run-now`

## 数据与行为约定

- 任务主表是 `tasks`，步骤表是 `task_steps`，日志表是 `task_logs`。
- 任务状态、触发来源、资源类型和操作 ID 是前后端筛选与追踪的稳定字段，改名需要迁移和前端同步。
- 长耗时后台操作应写入任务日志，并尽量拆出步骤，方便任务中心展示进度。
- `scheduler` 负责周期性指标采集、软件包刷新和证书续签，并可作为 `run-now` 执行入口。
- 远程命令原始输出可能包含第三方文本，翻译前要先评估是否应保留原样。

## 跨模块依赖

- 服务器测试、UFW、软件包维护依赖本模块记录任务。
- Nomad 引导、加入、移除节点和反向代理同步依赖本模块记录任务。
- 应用部署、停止、重启、镜像更新依赖本模块记录任务。
- 证书签发和续签依赖本模块记录任务。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端任务或调度改动时，运行 `task test:backend`，重点关注 `internal/tasks` 和 `internal/scheduler` 测试。
- 前端任务中心或 API 类型改动只按需要运行 `task test:web`。

## 文档更新触发

新增任务类型、状态、步骤结构、日志语义、调度项、手动运行行为或任务筛选字段时，必须更新本文档。
