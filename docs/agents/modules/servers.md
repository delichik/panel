# 服务器、凭据、指标与软件包

## 适用场景

修改 SSH 凭据、服务器登记、连通性测试、系统探测、sudo 检查、UFW 安装、概览指标采集、APT 软件包刷新或升级时，先读本文档。

## 后端入口

- SSH 凭据：`internal/credential/`
- 服务器：`internal/server/`
- SSH 执行器：`internal/sshx/`
- Linux 发行版适配：`internal/linux/`
- 通用远程运维操作：`internal/remoteops/`
- 指标采集：`internal/metrics/`
- 概览聚合：`internal/overview/`
- 软件包维护：`internal/packages/`
- 调度触发：`internal/scheduler/`
- 任务记录：`internal/tasks/`
- 路由装配：`internal/app/app.go`

## 前端入口

- 服务器与凭据页面：`web/src/features/servers/pages/ServersPage.vue`
- 服务器选择器：`web/src/components/ServerSelector.vue`
- 软件包页面：`web/src/features/packages/pages/PackageUpdatesPage.vue`
- 防火墙页面：`web/src/features/firewall/pages/FirewallPage.vue`
- 概览页面：`web/src/features/overview/pages/OverviewPage.vue`
- API：`web/src/api/servers.ts`、`web/src/api/packages.ts`、`web/src/api/overview.ts`
- 类型：`web/src/types/api.ts`

## API 范围

- 凭据：`GET/POST /api/v1/credentials`，`PUT/DELETE /api/v1/credentials/{id}`
- 服务器：`GET/POST /api/v1/servers`，`POST /api/v1/servers/probe`，`PUT/DELETE /api/v1/servers/{id}`；新增服务器响应可携带 `initialTaskId` 指向首连信息采集任务。
- 服务器操作：`POST /api/v1/servers/{id}/test`，`POST /api/v1/servers/{id}/restart`，`POST /api/v1/servers/{id}/ufw/install`
- UFW 防火墙：`GET /api/v1/servers/{id}/ufw`，`POST /api/v1/servers/{id}/ufw/enable`，`POST /api/v1/servers/{id}/ufw/rules`，`DELETE /api/v1/servers/{id}/ufw/rules/{number}`
- 指标：`GET /api/v1/servers/{id}/metrics`
- 软件包：`GET /api/v1/servers/{id}/packages/updates`，`POST /api/v1/servers/{id}/packages/refresh`，`POST /api/v1/servers/{id}/packages/upgrade-selected`，`POST /api/v1/servers/{id}/packages/upgrade-all`
- 概览：`GET /api/v1/overview`

## 数据与行为约定

- `servers` 和 `credentials` 在应用数据库，指标快照在指标数据库。
- 系统探测通过 SSH 执行远程命令，并交给 `internal/linux/` 解析支持的 Debian/Ubuntu 版本。
- 系统探测写入 `sys.*` traits，当前包括 CPU 核数、内存、磁盘、主机名、架构、CPU 型号、物理/直通网卡地址摘要、发行版和 UFW 支持/安装/启用状态。网卡采集要求 `/sys/class/net/{name}/device` 存在，并过滤 Docker、veth、bridge、CNI、隧道和 overlay 等常见虚拟接口。
- 安装软件、日志化 sudo 命令、sudo 写文件和 UFW allow/delete/status 这类基础远程操作应优先复用 `internal/remoteops/`，避免在业务模块里散落长脚本。
- 前端登记或测试服务器前必须选择已有 SSH 凭据；没有凭据时应引导先创建凭据，不能提交空 `credentialId`。
- 维护操作通常要求 root 或免密 sudo；相关检查结果写回服务器记录。
- 软件包维护基于 APT，只对支持的系统执行；刷新和升级都依赖远程 sudo，前端会在发行版或免密 sudo 未确认时阻断手动维护操作。
- `POST /api/v1/servers/{id}/packages/refresh` 会创建或复用 `package_refresh` 任务并返回 `taskId`；调度器按一轮所有服务器刷新时，同一轮创建的多个 `package_refresh` 任务必须共享一个 `operationId`；刷新失败必须落到任务错误和日志里，不能只写后台日志。
- 周期性指标采集会创建 `metrics_collect` 任务记录；同一轮多台服务器采集共享一个 `operationId`。任务中心默认“常用类型”会隐藏该高频类型，切到“所有类型”或精确选择 `metrics_collect` 时可查看。
- `POST /api/v1/servers/{id}/ufw/install` 返回 `taskId`；前端启动后必须保留任务中心入口，避免用户无法追踪远程安装进度。UFW 安装任务由内存 goroutine 执行，创建后必须先标记为 `running` 再返回，遗留旧 `queued` 由任务清理兜底标记失败。
- `POST /api/v1/servers/{id}/restart` 要求服务器可达且已确认免密 sudo，返回 `server_restart` 任务的 `taskId`；前端必须二次确认并保留任务中心入口。远程命令先后台延迟再调用 `systemctl reboot` 或 `shutdown -r now`，避免 SSH 主动断开被误判为重启失败。
- UFW 管理页面只支持 UFW：状态查询、添加 allow 规则和按编号删除规则通过远程 sudo 同步执行。启用操作返回 `server_ufw_enable` 任务；未安装时先安装，随后放行服务器当前 SSH 端口并执行 `ufw --force enable`，页面需要二次确认并保留任务中心入口；禁用 UFW 暂不由页面提供。
- 新增服务器时只创建一个可见的 `server_info_collect` 首连信息采集任务，并在创建响应返回 `initialTaskId` 供前端展示任务入口；该任务首连失败时必须标记失败并删除刚创建的服务器记录，让用户回到创建表单重新调整 SSH 信息。后续编辑、手动测试和陈旧刷新复用内部 `server_connectivity_test` 连通性任务，默认不在任务中心展示；一次服务器列表触发的多台陈旧服务器刷新应共享一个 `operationId`。
- 长耗时操作应记录为任务，日志和步骤交给 `internal/tasks/`。
- 概览指标卡片在窄尺寸下会自动隐藏重叠的时间轴标签；单项指标拉取失败时不在卡片内展示错误文案，图表浮窗挂载到页面层以避免被卡片边界裁剪。
- 服务器详情按网卡分组展示接口名及 IPv4/IPv6 地址，不把所有接口地址拼在同一个属性值中；连接测试结果使用紧凑的分项网卡标签。
- 服务器、软件包和防火墙页面的左侧服务器选择器使用统一的紧凑平面列表风格，桌面宽度为 `clamp(300px, 26vw, 340px)`；软件包和防火墙切换服务器时清空上一台服务器的异步详情并显示加载状态，且忽略迟到响应。

## 验证

- 先按模块索引的“检查和测试范围”判断是否需要验证。
- 需要验证后端改动时，运行 `task test:backend`，重点关注 `server`、`credential`、`linux`、`metrics`、`packages` 相关测试。
- 前端页面或 API 类型改动只按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

新增支持系统、远程命令、服务器字段、凭据字段、指标字段、软件包行为或相关 API 时，必须更新本文档。
