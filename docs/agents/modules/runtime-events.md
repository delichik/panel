# 运行事件入口已统一

系统日志、任务历史、协调记录统一由 [activity.md](activity.md) 定义。

生产查询使用 `/api/v1/activity`，界面使用 `/activity`。原始事实位于 AppDB，只追加、不修改、不删除；LogDB 是可重建的查询索引。

旧 `runtimeevents` 包的简化数据结构仅供未完全移除的内部生产者适配和旧模块测试使用；生产装配不再使用其查询路由、BufferedWriter 或 CleanupWorker。新增代码不得接入这些旧接口，不得写入 runtime_events/runtime_event_details 作为另一份审计来源。
