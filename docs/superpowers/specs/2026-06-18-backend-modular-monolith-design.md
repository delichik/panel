# Panel 后端模块化单体重构设计

## 1. 目标

将 Panel 主进程与 Panel Agent 的后端代码重组为边界清晰的模块化单体，解决以下问题：

- 顶层业务包扁平排列，包之间可以直接依赖具体实现。
- 单个 `Service` 同时承担 HTTP、业务流程、SQL、任务、远程操作和集成职责。
- `internal/app/app.go` 集中创建大量具体服务，并通过 setter 形成双向或环状连接。
- `internal/applications/service.go`、`internal/server/service.go` 等文件过大，难以独立理解、测试和修改。
- Panel 与 Agent 的协议、客户端、服务端实现和运行能力混放在同一包中。

本次采用集中重构：一次性建立新目录和依赖边界，并迁移现有实现，不长期保留新旧两套架构。

## 2. 不可破坏的兼容性

本次重构只改变内部代码组织，不改变外部行为。以下内容必须保持兼容：

- 所有 HTTP 方法、路径、查询参数、请求体和响应体。
- JSON 字段名、可空性、默认值、分页及错误响应格式。
- 用户可见业务逻辑、权限、校验顺序和任务触发行为。
- SQLite 数据库文件、表、字段、索引、外键、迁移顺序和持久化格式。
- 任务类型、状态、参数、metadata、并发键、重试和周期调度语义。
- Panel Agent 的 HTTP 路径、请求响应、mTLS、capability 和错误语义。
- 配置文件字段、环境变量、数据目录和构建产物位置。
- Panel 与 Agent 两个二进制的启动和关闭行为。

重构期间不顺便修改产品行为，不引入新的 API，不调整数据库结构，也不更换框架。

## 3. 架构选择

采用“按业务模块划分的模块化单体，模块内轻量分层”。

不采用全局 `handlers/services/repositories/models` 技术分层，因为它会让一个业务能力散落到整个仓库。也不采用完整 DDD 或微服务拆分，避免为当前规模引入额外部署、事务和消息一致性成本。

核心依赖方向：

```text
transport/http -> app -> domain
store/sqlite ----------------> domain
integration -----------------> ports
bootstrap -> modules + platform + agent
```

规则：

1. `domain` 只包含模型和纯业务规则，不依赖 SQL、HTTP、SSH、Agent 或其他业务模块。
2. `app` 实现用例和流程，通过 `ports` 使用数据库、任务、远端运行时和其他模块能力。
3. `store/sqlite` 实现持久化端口，不承载跨资源业务流程。
4. `transport/http` 只解析协议、调用用例、映射响应与错误。
5. `integration` 实现模块对外部模块或远端系统的适配。
6. `bootstrap` 只创建对象、连接端口、注册路由和管理生命周期，不编写业务判断。
7. 跨模块依赖只能指向对方公开的契约或端口，禁止依赖对方的 store、transport 或内部实现。

## 4. 目标目录

```text
cmd/
  panel/
  panel-agent/

internal/
  bootstrap/
    panel/
    agent/

  platform/
    buildinfo/
    config/
    database/
    errors/
    http/
    i18n/
    identity/
    linux/
    logging/
    secrets/
    ssh/
    templating/

  modules/
    identity/
    settings/
    servers/
    tasks/
    scheduling/
    applications/
    containers/
    certificates/
    keyassets/
    observability/
    packages/
    systeminfo/

  agent/
    contract/
    client/
    server/
    docker/
    system/
    security/
```

`platform` 只接收不带 Panel 业务语义、可被多个模块复用的技术能力。模块专用帮助函数留在模块内，禁止把 `platform` 变成新的通用杂物包。

## 5. 模块内部结构

复杂模块使用完整结构：

```text
applications/
  module.go
  domain/
  app/
  ports/
  store/sqlite/
  transport/http/
  integration/
```

较小模块可以省略没有实际内容的目录，但依赖方向不变。禁止为了形式创建只有转发代码的空层。

`module.go` 是模块的组合入口，负责：

- 接收已经构造好的平台能力和跨模块端口。
- 创建 repository、用例和 handler。
- 返回该模块公开能力及路由注册器。
- 不执行数据库迁移或启动无关模块。

模块公开类型应尽量小。外部模块只获得它实际需要的接口，而不是整个服务对象。

## 6. 现有包迁移映射

| 现有包 | 目标位置 |
| --- | --- |
| `internal/app` | `internal/bootstrap/panel` |
| `internal/config` | `internal/platform/config` |
| `internal/storage` | `internal/platform/database` |
| `internal/logging` | `internal/platform/logging` |
| `internal/httpx` | `internal/platform/http` |
| `internal/panelerr` | `internal/platform/errors` |
| `internal/i18n` | `internal/platform/i18n` |
| `internal/id` | `internal/platform/identity` |
| `internal/secretstore` | `internal/platform/secrets` |
| `internal/sshx` | `internal/platform/ssh` |
| `internal/linux`、`internal/remoteops` | `internal/platform/linux`，按解析与远程操作拆分 |
| `internal/templatex` | `internal/platform/templating` |
| `internal/buildinfo` | `internal/platform/buildinfo` |
| `internal/auth` | `internal/modules/identity` |
| `internal/settings` | `internal/modules/settings` |
| `internal/server`、`internal/credential` | `internal/modules/servers` |
| `internal/tasks` | `internal/modules/tasks` |
| `internal/scheduler` | `internal/modules/scheduling` |
| `internal/applications`、`internal/appspec`、`internal/appruntime` | `internal/modules/applications` |
| `internal/containerization`、`internal/orchestrator` | `internal/modules/containers` |
| `internal/certs`、`internal/dns`、`internal/proxycert` | `internal/modules/certificates` |
| `internal/keyassets` | `internal/modules/keyassets` |
| `internal/metrics`、`internal/overview`、`internal/diagnostics` | `internal/modules/observability` |
| `internal/packages` | `internal/modules/packages` |
| `internal/systeminfo` | `internal/modules/systeminfo` |
| `internal/agent` | `internal/agent/{contract,client,server,docker,system,security}` |

包合并只表示共同归入一个业务模块，不表示所有代码继续放在同一个 Go package。子包仍按职责隔离。

## 7. Applications 模块

现有 `applications.Service` 拆成多个用例对象，共享领域模型和 repository：

```text
modules/applications/
  domain/
    application.go
    file.go
    runtime.go
    revision.go
    validation.go
  app/
    catalog.go
    files.go
    save_session.go
    validation.go
    rendering.go
    deployment.go
    lifecycle.go
    runtime.go
    persistent_data.go
    image_updates.go
    reverse_proxy.go
  ports/
    repositories.go
    runtime.go
    servers.go
    tasks.go
    certificates.go
    container_queue.go
  store/sqlite/
    applications.go
    files.go
    revisions.go
    instances.go
  transport/http/
    routes.go
    applications.go
    files.go
    runtime.go
  integration/
    agent_runtime.go
    task_recorder.go
    certificate_files.go
```

重点约束：

- 应用保存与部署是不同用例，不能继续由同一个万能对象承担。
- 渲染和校验保持纯粹，数据库和远端操作通过输入或端口提供。
- 应用实例持久化集中在 repository，不在部署流程中散落 SQL。
- 保存会话负责临时文件生命周期，但提交时调用应用保存用例。
- 反向代理协调通过明确端口调用，不使用 setter 回连。
- Agent 错误处理通过 `AgentErrorReporter` 端口进入服务器模块。

## 8. Servers 模块

现有 `server.Service` 按资源管理和运维流程拆分：

```text
modules/servers/
  domain/
    server.go
    credential.go
    agent_status.go
    firewall.go
  app/
    registry.go
    credentials.go
    connectivity.go
    discovery.go
    firewall.go
    maintenance.go
    agent_management.go
    agent_certificates.go
  ports/
    repositories.go
    task_manager.go
    ssh.go
    agent.go
    cleanup.go
  store/sqlite/
    servers.go
    credentials.go
  transport/http/
    routes.go
    servers.go
    credentials.go
    firewall.go
    agent.go
```

服务器删除涉及应用目标、概览卡片、指标和任务清理。该流程由 `registry` 用例通过窄接口调用清理端口，并在现有事务边界要求下执行，不能把其他模块的 SQL 重新塞回服务器用例。

Agent 自动部署状态机归入 `agent_management`，证书签发和重置归入 `agent_certificates`。两者共享领域状态规则，但远程安装脚本和 SSH 上传属于 integration 实现。

## 9. Tasks 与 Scheduling

`tasks` 是任务领域与执行框架，`scheduling` 只负责时间驱动和周期扫描。

```text
modules/tasks/
  domain/
  app/
    commands.go
    queries.go
    manager.go
    execution_registry.go
  store/sqlite/
  transport/http/
  registry/

modules/scheduling/
  scheduler.go
  periodic_runner.go
  cleanup_runner.go
```

业务模块通过 `TaskManager` 端口创建、启动、记录步骤和结束任务。业务模块可以注册任务定义，但不能直接访问任务数据库。

周期定义仍由业务能力决定是否生成输入；调度模块只枚举注册定义并驱动执行，不硬编码业务 switch。

## 10. Agent 子系统

Panel Agent 与 Panel 主进程共享协议，但不共享具体服务实现。

```text
internal/agent/
  contract/
    requests.go
    responses.go
    capabilities.go
    routes.go
  client/
    client.go
    runtime.go
    docker.go
    system.go
  server/
    routes.go
    middleware.go
    handlers.go
  docker/
    engine.go
    containers.go
    images.go
    networks.go
    volumes.go
    runtime.go
  system/
    collector.go
    metrics.go
    firewall.go
    osrelease.go
  security/
    assets.go
    client_tls.go
    server_tls.go
```

兼容性规则：

- `contract` 中保留原 JSON tag、路由和 capability 字符串。
- client 和 server 都依赖 `contract`，但互不依赖。
- Agent server handler 只处理 HTTP 协议，Docker 和系统读取由独立端口实现。
- Panel 业务模块不直接依赖 Agent server 或 Docker 实现，只依赖 client 侧窄接口。
- `cmd/panel-agent` 只调用 `bootstrap/agent`。

## 11. HTTP 路由

移除 `app.routes` 中的大型路径 switch。每个模块提供路由注册函数：

```go
type RouteRegistrar interface {
    RegisterRoutes(*http.ServeMux, Middleware)
}
```

`bootstrap/panel` 依次注册模块路由，公共认证路由和静态前端托管仍在 bootstrap 管理。

迁移时必须使用现有方法和路径逐条建立路由兼容清单。路由注册方式可以改变，最终注册的 method-pattern 不得改变。

HTTP DTO 留在 `transport/http`，不直接复用数据库 row。必要时增加显式 mapper，确保内部模型拆分不会改变 JSON。

## 12. 数据库

- 保留三个数据库及其现有职责：应用、任务、指标。
- `platform/database` 负责连接、pragma、生命周期和迁移执行。
- 业务 SQL 和扫描函数迁入各模块 `store/sqlite`。
- 迁移文件可以按数据库或模块拆分源码文件，但执行顺序和 SQL 内容必须保持一致。
- 不新增迁移，不重建业务表，不更改历史迁移。
- 需要跨模块原子操作的现有流程使用显式 transaction coordinator，并把 `*sql.Tx` 封装为模块 store transaction，而不是向 app 层暴露任意 SQL。

## 13. 依赖装配与生命周期

`bootstrap/panel` 分为以下部分：

```text
bootstrap/panel/
  app.go
  dependencies.go
  modules.go
  routes.go
  background.go
  lifecycle.go
```

推荐装配顺序保持当前语义：

1. 打开数据库并运行迁移。
2. 初始化 Agent TLS 和密钥存储。
3. 完成旧凭据、DNS 凭据和自签名资产迁移。
4. 创建设置、认证和任务模块。
5. 创建服务器、应用、容器、证书等业务模块及适配器。
6. 注册任务定义和周期定义。
7. 恢复或失败孤立 running 任务。
8. 注册 HTTP 路由。
9. 启动更新检查、Agent 检查和 scheduler。

启动失败必须按逆序关闭已经创建的资源。应用关闭时停止所有后台服务，再关闭数据库。

禁止使用可变 setter 解决环状依赖。环状关系通过以下方式拆除：

- 在 bootstrap 创建 adapter，并把窄接口注入双方。
- 把协调流程提升到明确的应用用例。
- 对不要求同步返回的后置动作使用进程内事件分发器。

本次默认优先使用同步端口，只有证书更新后重部署应用等天然后置动作才考虑进程内事件。事件不得改变现有同步错误语义。

## 14. 错误处理

- 保留现有错误码、HTTP 状态和 i18n key。
- `platform/errors` 提供基础错误类型，不包含具体模块规则。
- 模块 app 层产生稳定业务错误；transport 统一映射为现有响应。
- integration 错误必须保留原始 cause，特别是 Agent、Docker、SSH 和 ACME 诊断。
- 重构前后针对关键错误建立快照式断言，避免错误码或状态悄然变化。

## 15. 测试策略

集中重构仍按可验证阶段进行，每个阶段结束都保持可编译：

1. 建立兼容性基线测试：路由、JSON、数据库迁移、任务定义和 Agent contract。
2. 搬迁 platform 包，只改 import，不改行为。
3. 拆分 Agent contract/client/server，并运行 Agent 相关测试。
4. 拆分 tasks 和 scheduling。
5. 拆分 servers。
6. 拆分 applications。
7. 迁移其余模块。
8. 替换 bootstrap 并删除旧包。
9. 运行完整后端测试和后端构建。

必须通过仓库规定命令验证：

```text
task test:backend
task build:backend
```

禁止使用 curl 或浏览器代替测试。测试产生的临时文件放入 `tmp`。

重点兼容测试：

- 所有现有 API method-pattern 均被注册。
- 核心 DTO JSON 序列化结果不变。
- 旧数据库可正常启动，迁移版本和 schema 不变。
- 三个数据库仍使用正确连接和表归属。
- 任务类型、能力和周期定义集合不变。
- Agent capability、路由和 contract JSON 不变。
- Panel 和 Agent 二进制都可构建。

## 16. 集中迁移执行策略

“集中重构”表示在一个重构工作流中完成并最终删除旧结构，不表示无序地同时修改所有包。实施仍按依赖由内向外推进：

1. 先添加兼容性护栏。
2. 搬迁无业务依赖的平台包。
3. 拆分 Agent 和任务基础能力。
4. 拆分服务器、应用两个高耦合模块。
5. 搬迁外围模块。
6. 重写 bootstrap。
7. 删除旧目录和兼容别名。

迁移中允许短期使用类型别名或 adapter 保持编译，但最终结构不得长期保留两套公开入口。每个临时兼容层必须在同一重构中删除。

## 17. 文档更新

实施完成后必须同步更新：

- `docs/agents/modules/README.md`
- `docs/agents/modules/backend-core.md`
- `docs/agents/modules/servers.md`
- `docs/agents/modules/tasks-scheduler.md`
- `docs/agents/modules/applications.md`
- `docs/agents/modules/containerization.md`
- `docs/agents/modules/dns-certificates.md`
- 其他被实际目录或调用关系影响的模块文档

文档应描述新的入口、依赖边界和模块职责，不记录已经删除的临时迁移层。

## 18. 完成标准

同时满足以下条件才算重构完成：

- 旧的扁平业务包和巨型 `app.routes` 已删除。
- Applications 和 Servers 不再存在万能服务对象。
- SQL、HTTP 和外部集成已从用例职责中分离。
- 跨模块调用全部通过公开端口或明确协调器。
- Panel 与 Agent contract、client、server 和运行实现已经分离。
- 无可变 setter 形成的跨模块循环装配。
- API、数据库、任务、配置和 Agent 契约兼容测试通过。
- `task test:backend` 与 `task build:backend` 通过。
- 相关模块文档和索引已更新。

