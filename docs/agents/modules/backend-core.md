# 后端核心、配置与存储

## 适用场景

修改服务启动、依赖装配、API 路由、配置加载、数据库迁移、认证、运行时设置、统一响应或错误处理时，先读本文档。

## 关键入口

- 后端入口：`cmd/panel/main.go`
- 应用装配与路由：`internal/app/app.go`
- 配置：`internal/config/config.go`
- 存储和迁移：`internal/storage/store.go`、`internal/storage/migrations.go`
- 认证：`internal/auth/`
- 运行时设置：`internal/settings/`
- 统一 HTTP 响应：`internal/httpx/`
- 统一错误：`internal/panelerr/`
- 后端错误翻译：`internal/i18n/`

## 结构约定

- `app.New` 负责打开数据库、创建 service、装配 scheduler、连接跨模块依赖并注册路由。
- API 统一挂在 `/api/v1/`。`/api/v1/auth/login` 和 `/api/v1/auth/session` 是开放入口，其余 API 经认证中间件保护。
- 根路径由后端静态托管 `web/dist`；没有构建前端时返回纯文本后端运行提示。
- 运行时设置从数据库读取，并以配置文件、环境变量和内置默认值作为基础。
- 后端对外错误响应需要走 `panelerr`、`httpx` 和 `internal/i18n`，不要在 handler 中散落用户可见错误文案。

## 数据库约定

- 应用数据在 `Store.AppDB()`，指标数据在 `Store.MetricsDB()`，不要把指标表误建到应用数据库。
- 当前处于 alpha 但已有使用者，修改表结构必须考虑旧版本迁移。
- 新字段优先使用可重复执行的增量迁移，并在 `internal/storage/store_test.go` 或相关 service 测试覆盖旧库升级路径。
- 会被展示的持久化配置只保存稳定 key、kind、value，不保存当前语言下的展示文案。

## API 变更检查

- 路由在 `internal/app/app.go` 中集中维护；新增后端 API 时同步更新前端 `web/src/api/` 与 `web/src/types/api.ts`。
- 认证、强制改密、运行时设置会影响前端路由守卫，相关变更要补读 [frontend.md](frontend.md)。
- 新增用户可见错误时补读 [../i18n-guide.md](../i18n-guide.md)。

## 验证

- 先按根目录 `AGENTS.md` 的“检查和测试范围”判断是否需要验证。
- 需要验证后端相关改动时，优先运行 `task test:backend`。
- 影响编译、路由装配、配置或迁移且需要构建检查时，运行 `task build:backend`。
- 如果同时改前端 API 调用，再按需要运行 `task test:web` 或 `task build:web`。

## 文档更新触发

修改启动装配、配置项、运行时设置、认证流程、API 路由、数据库表/字段、错误响应结构时，必须更新本文档或模块索引。
