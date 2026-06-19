# 容器化管理设计

## 目标

在现有 Panel 中新增一级菜单“容器化”，统一承载 Application、Docker 容器、镜像、网络和卷管理。

Application 是由 Panel 声明期望状态并自动协调的托管对象。普通 Docker 容器只作为目标服务器上的运行时资源展示和操作，不自动纳入 Application 管理。

本功能继续使用目标服务器上的 `panel-agent` 访问 Docker Engine API。Panel 不通过 SSH 调用 Docker CLI，也不直接连接远程 Docker Host。

## 菜单与页面

“容器化”一级菜单包含以下二级入口：

1. 应用
2. 容器
3. 镜像
4. 网络
5. 卷

现有 Application 页面迁入“容器化 / 应用”。旧 `/applications` 地址保留重定向，避免已有书签失效。

容器、镜像、网络和卷页面统一采用左侧服务器选择器、右侧资源列表的主从布局。中大屏页面占满全局页头以下的剩余视口，资源列表在内部滚动；窄屏恢复页面级滚动。

## Agent Docker 资源接口

扩展 `panel-agent` 的 Docker Engine API 能力，新增稳定的资源 DTO 和操作接口：

- 容器：列表、启动、停止、重启、删除。
- 镜像：列表、拉取、删除。
- 网络：只读列表。
- 卷：列表、删除未使用卷。

首期不支持创建或编辑普通容器，不支持网络创建、编辑或删除，不支持卷创建或编辑。

Agent 健康检查能力列表增加对应 Docker 资源 capability。Panel 只在 Agent 健康、版本兼容且 Docker 可用时访问资源，不回退 SSH。

Agent 返回稳定错误码；Docker 原始诊断可作为错误详情保留。Panel API 错误继续通过统一错误和 i18n 入口处理。

## 托管容器标识

Panel 部署 Application 容器时写入以下 Label：

- `panel.application.managed=true`
- `panel.application.id=<application-id>`
- `panel.application.instance.id=<instance-id>`
- `panel.application.generation=<generation>`
- `panel.application.spec.hash=<spec-hash>`

Label 只保存稳定标识和协调所需版本信息，不保存变量、密钥或完整运行规格。

容器列表、风险提醒和协调监控只识别上述新 Label。现有旧 Label 不兼容，不在启动时迁移，也不自动重新部署旧容器。旧容器只有在 Application 后续部署或镜像更新后才自然获得新 Label。

## 容器页面

容器页面展示所选服务器上的全部 Docker 容器，不隔离 Application 托管容器。

列表至少展示：

- 名称和容器 ID
- 镜像
- 状态
- 创建时间
- 端口摘要
- Application 托管标志

支持操作：

- 启动
- 停止
- 重启
- 删除

操作 Application 托管容器前必须展示风险确认，说明 Panel 的容器监控会检测期望状态偏差并自动恢复。容器操作 API 只提交 Docker 操作，不直接触发 Application 恢复。

## 每服务器容器操作队列

每台服务器拥有一条独立的容器变更队列。不同服务器可并行，同一服务器上的容器生命周期变更必须串行执行。

以下操作进入同一服务器队列：

- 普通容器启动、停止、重启和删除
- Application 容器创建、启动、停止、重启和删除
- Application 部署
- Application 镜像更新后的重建
- 协调监控提交的恢复操作

所有操作必须幂等：

- 执行前重新读取 Docker 实际状态。
- 已达到目标状态时直接成功。
- 相同服务器、相同资源和相同目标状态的活跃请求复用现有任务。
- 删除不存在的目标视为已达到删除状态。
- 启动已运行容器、停止已停止容器视为成功。
- 队列任务失败后释放执行槽，不阻塞后续任务。

镜像拉取本身不要求进入容器生命周期串行区；如果拉取属于 Application 更新流程，后续容器重建仍必须进入服务器队列。

## Application 容器协调

新增容器监控循环，默认每 5 秒检查一次 Application 托管容器的实际状态。

监控以 Docker Label 识别托管容器，以 Application 和实例数据库记录取得期望状态与部署规格。容器操作接口不得直接调用恢复逻辑；无论偏差来自 Panel 页面、Docker CLI 或其他外部工具，都由同一监控流程发现。

发现以下偏差时，监控提交去重的 `application_reconcile` 任务：

- 期望运行的托管容器不存在。
- 期望运行的托管容器已停止或失败。
- 容器 Label 中的 generation 或 spec hash 与期望值不一致。

协调器只提交恢复意图，实际 Docker 操作进入目标服务器的单队列。已有同一 Application 实例的活跃恢复或部署任务时不重复创建。

恢复失败时保留 Application 实例异常状态和任务日志，并按受控重试策略处理，避免每 5 秒无限创建失败任务。

## 镜像页面与更新检查

镜像页面展示所选服务器上的全部 Docker 镜像，包括普通容器和 Application 使用的镜像。

列表至少展示：

- 仓库和标签
- 镜像 ID
- 本地摘要
- 远端最新摘要
- 大小
- 创建时间
- 使用状态和来源
- 最近检查时间
- 更新状态或检查错误

无标签或按 digest 固定的镜像标记为不可检查。可解析标签的镜像参与更新检查，包括普通容器使用的镜像。

镜像更新检查采用与系统软件包检查相同的调度模型：

- scheduler 定期创建镜像刷新任务。
- 同一轮跨服务器检查共享 operation ID。
- 检查结果持久化，页面读取缓存结果并展示刷新状态。
- 页面提供手动刷新入口，用于显式创建或复用刷新任务。
- 更新检查任务进入任务中心，并避免同服务器重复刷新。

普通容器使用的镜像可以显示“有更新”，但界面不提供升级操作。

只有关联已启用 Application 的镜像可勾选升级。页面支持：

- 升级选中项
- 全部升级

升级一个镜像时，找出所有使用该镜像且已启用的 Application，为每个 Application 执行现有镜像更新和重新部署流程。批量入口创建一个用户操作，子任务或步骤记录各 Application 的结果；单个失败不得隐藏其他项结果。

镜像页还支持拉取新镜像和删除镜像。被任何容器引用的镜像禁止删除，并返回冲突错误；未被引用的镜像经二次确认后删除。

## 网络页面

网络页面首期只读，展示基础字段：

- 服务器
- 名称
- 网络 ID
- 驱动
- 作用域
- 创建时间

首期不展示关联容器、IPAM 详情、子网或网关，也不提供任何网络操作。

## 卷页面

卷页面展示：

- 服务器
- 名称
- 驱动
- 挂载点
- 创建时间
- 使用状态
- 关联容器数量

卷必须明确标记“使用中”或“未使用”。

- 使用中的卷禁止删除。
- 未使用的卷允许经二次确认后删除。
- 删除前 Agent 再次检查引用状态，避免列表状态过期造成误删。

首期不展示复杂挂载详情，不支持卷创建或编辑。

## Panel 后端模块

新增独立 Docker 资源管理模块，负责：

- 校验服务器和 Agent 可用性。
- 调用 Agent Docker 资源接口。
- 标记容器和镜像的 Application 关联关系。
- 创建资源操作任务。
- 管理每服务器容器操作队列和幂等键。
- 持久化镜像更新检查结果。
- 为 scheduler 提供镜像刷新执行器。

Application 模块继续拥有 Application 规格、修订、实例、镜像更新和部署逻辑。Docker 资源模块不得复制 Application 期望配置。

Application 部署改为向每服务器队列提交容器变更，而不是直接并发调用 Agent。协调监控也复用同一入口。

## API 设计

Panel API 按服务器组织资源：

- `GET /api/v1/servers/{id}/containers`
- `POST /api/v1/servers/{id}/containers/{containerId}/start`
- `POST /api/v1/servers/{id}/containers/{containerId}/stop`
- `POST /api/v1/servers/{id}/containers/{containerId}/restart`
- `DELETE /api/v1/servers/{id}/containers/{containerId}`
- `GET /api/v1/servers/{id}/images`
- `POST /api/v1/servers/{id}/images/pull`
- `POST /api/v1/servers/{id}/images/refresh`
- `DELETE /api/v1/servers/{id}/images/{imageId}`
- `POST /api/v1/images/upgrade-selected`
- `POST /api/v1/images/upgrade-all`
- `GET /api/v1/servers/{id}/networks`
- `GET /api/v1/servers/{id}/volumes`
- `DELETE /api/v1/servers/{id}/volumes/{volumeName}`

所有变更接口返回任务 ID。批量升级请求使用稳定的 Application ID 或镜像关联 ID，不使用翻译后的展示名称。

具体路径编码必须安全处理 Docker ID、镜像 ID 和卷名；无法安全放入路径的资源标识使用请求体传递。

## 持久化与迁移

Docker 实时资源清单不复制到 Panel 数据库。

新增镜像更新缓存表，至少保存：

- server ID
- 稳定镜像引用
- 本地镜像 ID 和摘要
- 远端摘要
- 是否可检查
- 是否有更新
- 检查错误
- 检查时间

新增刷新状态表或等价记录，用于保存每台服务器最近刷新时间。迁移必须可重复执行，并覆盖旧数据库升级。

队列的当前执行状态可以由任务表和进程内执行注册表共同维护；不支持跨进程恢复的运行任务在进程重启后按现有 orphaned 任务规则失败，后续监控重新提交仍未满足的期望状态。

## 任务类型

新增或调整任务类型：

- `container_start`
- `container_stop`
- `container_restart`
- `container_delete`
- `image_pull`
- `image_refresh`
- `image_delete`
- `application_image_upgrade_selected`
- `application_image_upgrade_all`
- `application_reconcile`
- `volume_delete`

Application 原有 `application_deploy` 和 `application_image_update` 保留，但其容器变更经过每服务器队列执行。

任务中心按稳定类型翻译标题、阶段和状态。定时 `image_refresh` 默认归入 scheduler 任务，不进入常用任务列表。

## 前端行为

新增 API client、DTO、路由、i18n 词条和页面测试。

所有用户可见文案同时提供英文和简体中文。Docker、镜像仓库或 Agent 返回的原始诊断可以作为第三方文本保留。

页面使用已有组件和模式：

- `ServerSelector`
- `PageLoadingState`
- `AppPagination`
- 标准表格、确认对话框、Snackbar 和任务中心入口

容器操作按钮根据实际状态启用：

- 运行中：停止、重启、删除
- 已停止：启动、删除

托管容器操作始终先显示风险确认。镜像批量升级只允许选择存在可更新且可操作 Application 关联的行。普通容器镜像即使有更新也保持只读更新状态。

## 错误处理

- 服务器不存在、不可达、Agent 不兼容或 Docker 不可用时返回明确错误。
- Agent 请求失败时不回退 SSH。
- 使用中的镜像和卷返回冲突错误。
- 队列重复请求返回已有任务 ID。
- 资源状态已经满足目标时任务幂等完成。
- 镜像仓库检查失败保留上次成功结果，并展示本次错误和检查时间。
- 批量 Application 镜像升级记录每个 Application 的成功或失败结果。
- 协调恢复失败保留实例异常状态，使用退避或任务状态限制重复提交。

## 测试与验证

后端测试覆盖：

- Agent Docker 容器、镜像、网络和卷 API。
- 新 Label 注入和托管识别。
- 不识别旧 Label。
- 每服务器串行、跨服务器并行。
- 容器操作幂等与活跃任务复用。
- 容器监控发现删除、停止和规格偏差。
- 操作接口不直接触发恢复。
- 协调任务去重和失败退避。
- 镜像定时检查、缓存替换和刷新去重。
- 普通容器镜像只展示更新。
- Application 镜像选中升级和全部升级。
- 使用中镜像和卷的删除保护。
- 旧数据库增量迁移。

前端测试覆盖：

- 容器化菜单和路由。
- 服务器切换时清空旧资源并忽略迟到响应。
- 托管容器标志和风险确认。
- 容器操作按钮状态。
- 镜像更新状态、选择限制、选中升级和全部升级。
- 网络只读行为。
- 卷使用状态和删除限制。
- 中大屏内部滚动和窄屏布局降级。

实现完成后执行：

- `task test:backend`
- `task test:web`

涉及路由、类型、Agent capability 和装配编译时，再执行：

- `task build:backend`
- `task build:web`

## 文档更新

实现时新增容器化模块指引并更新模块索引，同时更新：

- 后端核心与 API 路由指引
- 前端页面与导航指引
- 服务器与 Agent 能力指引
- Application 模块指引
- 任务与调度指引
- 前端导航和相关组件设计规范
- i18n 翻译状态
