# Nomad 模块

## 适用场景

修改 Nomad API 客户端、节点清单、控制平面、server 引导、client 加入、节点移除、TLS 资产、Nomad 运行时设置或反向代理同步时，先读本文档。

## 后端入口

- Nomad API client：`internal/nomad/client.go`
- 类型：`internal/nomad/types.go`
- 运行配置：`internal/nomad/config.go`
- 控制平面聚合：`internal/nomad/control_plane.go`
- 节点加入和引导：`internal/nomad/join_service.go`
- TLS 资产：`internal/nomad/tls_assets.go`
- 节点特征：`internal/nomad/traits.go`
- Handler：`internal/nomad/handler.go`
- 路由装配和跨模块连接：`internal/app/app.go`

## 前端入口

- Nomad 设置/加入页面：`web/src/features/nomad/pages/NomadSetupPage.vue`
- Nomad 节点页面：`web/src/features/nomad/pages/NomadNodesPage.vue`
- 占位或重定向页面：`web/src/features/nomad/pages/NomadJobsPage.vue`
- API：`web/src/api/nomad.ts`
- 类型：`web/src/types/api.ts`
- 设置页 Nomad 分类：`web/src/features/settings/pages/SettingsPage.vue`

## API 范围

- 清单：`GET /api/v1/nomad/status`、`/nodes`、`/jobs`、`/deployments`、`/evaluations`、`/services`
- 控制平面：`GET /api/v1/nomad/control-plane`
- 加入流程：`GET /api/v1/nomad/join-candidates`，`POST /api/v1/nomad/join`
- 引导、重部署、重建、切换与移除：`POST /api/v1/nomad/bootstrap-server`，`/redeploy-node`，`/rebuild-cluster`，`/switch-server`，`/remove-node`
- 反向代理：`PUT /api/v1/nomad/reverse-proxy`

## 行为约定

- Nomad 地址、namespace、region、datacenter 等运行时设置来自 `internal/settings/`。
- 本地或回环 Nomad 地址会使用项目托管的 TLS 资产；相关判断在 `internal/app/app.go`。
- 引导/加入流程通过 SSH 在目标服务器执行远程命令，需要考虑支持系统、sudo、幂等性和失败恢复。
- server 引导、server 重部署和集群重建会临时切换 Panel 的 Nomad API 地址；只有 Panel 验证 TCP 4646/API 可达后才保留地址，失败必须回滚到旧地址。
- 集群重建必须先引导并验证新的单 server 集群，再重置其他 Panel 托管节点，避免新 server 端口未开放时提前停止旧节点。
- 长耗时流程必须写入任务、步骤和日志。
- Nomad 控制平面投影依赖最新 `nomad_*` 任务，任务查询需要保持最新优先，避免旧任务分页遮挡新近的引导、加入、重建、移除和 server 切换操作。
- 反向代理同步会读取应用模块和证书模块的数据，避免在 Nomad 模块持久化已翻译展示文案。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `internal/nomad` 测试。
- 前端 Nomad 页面或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 Nomad API、远程安装命令、运行时设置、TLS 行为、控制平面字段、反向代理配置或跨模块依赖时，必须更新本文档。
