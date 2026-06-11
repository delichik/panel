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
- API：`web/src/api/nomad.ts`
- 类型：`web/src/types/api.ts`
- 设置页 Nomad 分类：`web/src/features/settings/pages/SettingsPage.vue`

## API 范围

- 清单：`GET /api/v1/nomad/status`、`/nodes`
- 控制平面：`GET /api/v1/nomad/control-plane`
- 加入流程：`GET /api/v1/nomad/join-candidates`，`POST /api/v1/nomad/join`
- 引导、重部署、重建、切换与移除：`POST /api/v1/nomad/bootstrap-server`，`/redeploy-node`，`/rebuild-cluster`，`/switch-server`，`/remove-node`，这些接口返回 `taskId`
- 反向代理：`PUT /api/v1/nomad/reverse-proxy`，返回更新后的 `server` 和 `taskId`

## 行为约定

- Nomad 地址、namespace、region、datacenter 等运行时设置来自 `internal/settings/`。
- SSH 管理地址与 Nomad server advertise 地址是两个独立网络平面。引导、重建或切换 server 时，前端必须从目标服务器探测到的物理网卡地址中显式选择 `advertiseAddress`；后端将其保存为服务器 trait `nomad.server_advertise_address`，用于 server 的 `advertise.http/rpc/serf`、Panel 的 Nomad HTTP 地址和 client 的 `server_join.retry_join`。禁止从 SSH `host` 自动推导。
- 已存在 Nomad 任务但当前 server 没有 `nomad.server_advertise_address` 时，控制平面返回 `migration_required`，前端进入类似首次初始化的重建引导，不继续展示可误操作的旧控制平面。
- 本地或回环 Nomad 地址会使用项目托管的 TLS 资产；相关判断在 `internal/app/app.go`。
- 引导/加入流程通过 SSH 在目标服务器执行远程命令，需要考虑支持系统、sudo、幂等性和失败恢复。
- Panel 管理的 Nomad agent 必须在本机 UFW 放行 `4646/tcp`（HTTP API）、`4647/tcp`（RPC）以及 `4648/tcp`、`4648/udp`（Serf gossip）；引导、加入、重部署和 server 切换后的 client 同步都必须幂等修复这些规则。云厂商安全组或外部防火墙不由 Panel 管理，仍需允许节点间对应流量。
- 引导/加入后的本地健康检查使用带单次硬超时的 `nomad agent-info` 检查本地 agent API，不使用可能等待集群响应的 `nomad status`；整体检查必须在任务阶段超时内结束并输出 systemd/journal 诊断。
- client 加入或重部署不能只以本地 agent API 可用作为成功条件；还必须通过 Panel 使用的 Nomad API 确认匹配 `panel_server_id` 的节点已经注册且状态为 `ready`，超时则任务失败并记录最后的节点状态或 API 错误。
- 生成的 server/client 配置必须显式写入运行时 `region` 和 `datacenter`；client 通过 `server_join.retry_join` 持续重试控制平面 RPC 地址，避免 server 短暂不可达时只留下本地存活但未注册的 agent。
- 同一 `panel_server_id` 存在旧 `down` 节点和新 `ready` 节点时，控制平面投影必须优先展示 `ready` 节点，避免重部署后被旧节点记录覆盖为离线。
- Nomad 运行时准备可以安装 Docker/CNI，但不得无条件重启 Docker；Docker 已运行时只做健康检查，未运行时才启动，避免 Panel 自身部署在目标节点 Docker 中时被中断。
- server 引导、server 重部署和集群重建会临时切换 Panel 的 Nomad API 地址；只有 Panel 验证 TCP 4646/API 可达后才保留地址，失败必须回滚到旧地址。
- server 切换先使用用户选择的 advertise 地址重写并重启目标 server，再验证新 API 地址；验证成功后同步所有 Panel 托管 client 的完整配置，把 `server_join.retry_join` 更新为新 server RPC 地址，补齐 Nomad UFW 规则，逐台重启并确认重新注册。
- 节点重部署会删除 Panel 托管的旧 Nomad 配置和 TLS 文件，并根据当前运行时设置重新生成完整 server/client 配置；client 重部署使用 Panel 当前选择的 server RPC 地址。
- 集群重建必须先引导并验证新的单 server 集群，再重置并重新加入其他 Panel 托管节点，最后无条件重新注册数据库中所有 `enabled` 应用并同步反向代理。应用定义、文件、变量和启用状态保存在 Panel 数据库，不能因 Nomad 集群重建丢失。
- 长耗时流程必须写入任务、步骤和日志。
- 直接由 goroutine 执行的 Nomad 节点操作创建任务后必须先落 `running` 再返回 `taskId`，避免 Panel 进程中断后任务永久停在 `queued`。
- 前端 Nomad 节点页提交加入、重部署、重建、切换或移除后必须保留 `taskId`，并给出跳转任务中心的入口。
- 首个 server 引导从设置页跳回节点页时必须保留 `taskId`，节点页应展示任务中心入口。
- 移除 Nomad 节点属于高风险操作，前端必须先显示确认对话框。
- 不恢复 raw Nomad jobs/deployments/evaluations/services 导航、页面或公开 API；应用运行态只通过应用模块读取单个 job 的 deployment、evaluation 和 allocation 信息。
- Nomad 控制平面投影依赖最新 `nomad_*` 任务，任务查询需要保持最新优先，避免旧任务分页遮挡新近的引导、加入、重建、移除和 server 切换操作。
- 反向代理同步会读取应用模块和证书模块的数据；保存接口会创建 `nomad_reverse_proxy_sync` 任务，记录远程 UFW 放行和 Nomad job reconcile 的结果，前端必须保留任务中心入口。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `internal/nomad` 测试。
- 前端 Nomad 页面或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增 Nomad API、远程安装命令、运行时设置、TLS 行为、控制平面字段、反向代理配置或跨模块依赖时，必须更新本文档。
