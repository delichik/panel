# 轻量 ORM（internal/platform/database/orm）

## 适用场景

使用或扩展 Panel 的轻量 ORM：模型注册、链式查询、CRUD、自动迁移（AutoMigrate/AutoMigrateModels）与版本化迁移步骤（RunSteps）。ORM 已接管存量数据库迁移：`Store.Migrate` 的 schema 由模型 + AutoMigrateModels 管理，一次性数据迁移包装为 Step 执行。

## 包位置与约束

- 包路径：`internal/platform/database/orm`
- 依赖：仅标准库（database/sql、reflect、context、encoding/json、fmt、sort、strings、time、sync 及少量标准库辅助包）；不引入外部依赖，不 import 任何业务模块。
- 驱动：测试使用仓库已有的 `modernc.org/sqlite`；ORM 自身不依赖具体驱动，`Executor` 接口同时被 `*sql.DB` 与 `*sql.Tx` 满足。
- SQLite-first：`Dialect` 接口保留扩展空间，目前只有默认 SQLite 实现。

## 模型约定

- 表名：实现 `TableName() string` 方法，或在任意字段上写 `orm:"table:xxx"`（该字段仅用于声明表名，不再映射为列）；两者都没有时用结构体名 snake_case（不做复数化，`Server` → `server`）。
- 列名：`orm:"column:xxx"` 或字段名 snake_case（`CredentialID` → `credential_id`、`OSRelease` → `os_release`）。
- 元数据在 `Register` 或首次使用时经 reflect 解析并缓存（并发安全）；`Register` 幂等，非法 tag 返回错误。
- 同名表被多个模型注册时，AutoMigrate 按最后一次注册的模型为准。
- 表级约束：模型可实现 `TableConstraints() []string`，返回原始表级约束子句（如 `CHECK(type IN ('password','private_key'))`），拼进 CREATE TABLE，表重建时保留。
- 额外索引：模型可实现 `ExtraIndexDDL() map[string][]string`，按表名声明 orm tag 无法表达的复合/部分/复合 UNIQUE 索引的完整 `CREATE [UNIQUE] INDEX` DDL；AutoMigrate 按名称或列形状识别这些索引避免误报/误删，实际创建由 `Store.Migrate` 以 `IF NOT EXISTS` 幂等执行。
- 未注册类型首次使用（CRUD）时懒解析并缓存；只有 `Register` 过的模型参与 AutoMigrate。

### tag 语法（分号分隔）

| tag | 含义 |
| --- | --- |
| `table:xxx` | 表名（仅可作为字段唯一 tag） |
| `column:xxx` | 列名 |
| `primary_key` | 主键（可多个组成复合主键） |
| `auto_increment` | SQLite 自增（要求整数类型 + primary_key，单一主键） |
| `not_null` / `unique` / `index` | 列约束与索引 |
| `default:<expr>` | 默认值（按原始 SQL 表达式写入，如 `0`、`'{}'`） |
| `type:<type>` / `size:<n>` | 列类型覆盖 / 长度 |
| `embedded` | 展开内嵌结构体（字段直接平铺，无前缀；列名冲突报错） |
| `json` | struct/map/slice 以 JSON TEXT 存取；map/slice 未加 tag 也会自动按 JSON 处理，普通 struct 必须显式 `json` 或 `embedded` |
| `references:t(c)` / `on_delete:x` / `on_update:x` | 外键（仅建表/重建时生效） |
| `auto_create_time` / `auto_update_time` | time.Time 字段自动填充（写入时为空才填/更新时置 now） |
| `time_format:unix` | time.Time 存 INTEGER unix 秒；缺省 TEXT RFC3339Nano(UTC) |
| `-` | 忽略字段（不映射） |

### 类型映射

- string→TEXT；int/uint→INTEGER；float→REAL；bool→INTEGER(0/1)；time.Time→TEXT(RFC3339Nano, UTC) 或 unix INTEGER；[]byte→BLOB；nil/any→TEXT；同时实现 sql.Scanner 与 driver.Valuer 的类型透传。
- 可空列用 `*T` 或 `sql.Null*`；NULL 读入 `*T` 时保持 nil，读入非指针标量时留零值。

## Builder API

- 链式：`From/Select/SelectExpr/Distinct/Where/And/Or/WhereGroup/AndIn/OrIn/AndNotIn/AndLike/OrLike/AndNull/AndNotNull/AndBetween/Join/LeftJoin/RightJoin/GroupBy/Having/OrderBy/Limit/Offset`。
- `WhereGroup(func(*Condition))` 生成括号分组；`Condition` 提供 `Where/And/Or/WhereGroup`。
- `AndIn`/`OrIn`/`AndNotIn` 接受切片或 `[]any`；空 IN 生成 `1=0`，空 NOT IN 生成 `1=1`。
- `AndLike/OrLike` 生成 `col LIKE ? ESCAPE '\'`，配合 `LikeEscaped(term)`（转义 `\ % _` 并包 `%...%`）使用。
- `OrderBy` 项按原样拼接（支持 `created_at DESC` 等表达式）；`Select/GroupBy` 项按标识符引用。
- `ToSQL()` 返回带 `?` 占位符的 SQL 与参数（调试用）。
- 参数一律 `?` 占位，禁止拼接用户输入。

## 终端方法与顶层函数

- 查询：`All(ctx, *[]T)`、`First(ctx, *T)`（无记录返回 `sql.ErrNoRows`）、`One(ctx, *T)`（行数 != 1 报错）、`Count`、`Exists`、`Pluck(ctx, column, *[]T)`、`ScanValue(ctx, *T)`。
- 写：`Insert`（回填 auto_increment，自动填充时间）、`InsertBatch`（按 500 行分块）、`Update`（按主键，PK 为空报错）、`UpdateColumns`（必须带 WHERE）、`Delete`（必须带 WHERE）。
- 顶层便捷函数：`orm.Insert/InsertBatch/Update/Delete`（按模型元数据推断表）、`Raw/RawExec/RawRow`（复杂 SQL 逃生口）。
- 事务：`orm.WithTx(ctx, db, fn)`；`New(tx)` 可直接在事务上执行。

## AutoMigrate / AutoMigrateModels

- `orm.AutoMigrateModels(ctx, db, models []any, opts...)`：按库隔离入口，只迁移传入的模型清单；模型元数据即时解析，不触碰全局注册表。
- `orm.AutoMigrate(ctx, db, opts...)`：委托 `AutoMigrateModels`，模型清单为全局 `Register` 过的全部模型。
- 两者都管理元数据表 `orm_meta`（table_name、schema 快照、synced_at），共同语义如下：

- 首次接管：表已存在只记录快照、不删任何内容（防模型遗漏误删）；表不存在则按模型建表并记录。
- 增量同步（幂等）：建缺失表；补缺失列（`ALTER TABLE ADD COLUMN`；NOT NULL 仅在有 default 时保留，否则按可空添加并记入 Pending）；索引同步（缺则建、快照中但模型不再声明则删）；外键仅在建表/重建时生效，存量表与模型不一致记入 Pending。
- 删列：快照中有而模型不再声明的列 → `DROP COLUMN`；被索引约束时先删关联索引，仍受 PK/UNIQUE/FK/CHECK 约束则自动“表重建”（建新表→拷数据→换名→重建索引；期间 `PRAGMA foreign_keys=OFF` 在事务外切换，完成后恢复 ON）。
- 删表：快照中有而模型不再注册的表 → 按 `PRAGMA foreign_key_list` 拓扑先删子表；存在 FK 循环或被未接管/模型表引用时记入 Pending，不删除。
- 删除安全门：只删“ORM 曾接管并记录过快照”的列/表/索引；外部（未接管）列/索引永不进入快照、永不自动删；破坏性操作受 `WithDestructive` 控制，非破坏模式只出 `DriftReport.SkippedDestructive`。
- `DriftReport` 字段：`Added/DroppedTables`、`Added/DroppedColumns`、`Added/DroppedIndexes`、`RebuiltTables`、`Pending`（无法自动处理的差异）、`SkippedDestructive`。
- 选项：`WithDestructive(bool)`（默认 false）、`WithLogger(fn)`。
- 迁移在单个专用连接上执行，保证 `PRAGMA foreign_keys` 在整次迁移内生效，结束后恢复原值。

### 迁移边界与删除语义

- 模型必须覆盖存量表的全部在管列；要退役的列先写显式 Step（备份/搬移）再移除模型字段。
- 从未接管的表/列永不自动删；接管后新增的外部列会保持“未管理”状态并进入 Pending。
- 非破坏模式（默认）只报告不删除；确认 drift 归零后可用 `WithDestructive(true)` 开启删除。
- 删除不可逆，建议在启动日志中保留每次 DriftReport。

## 版本化迁移步骤

- `orm.RunSteps(ctx, db, steps []Step)`：在指定库上按序执行给定 steps，`orm_migrations(id, applied_at)` 记录已执行，失败整体回滚；按库隔离时只传入该库的 steps。
- `orm.RegisterSteps(Step{ID, Run})`：重复 ID 报错；`orm.MigrateSteps(ctx, db)` 委托 `RunSteps`，steps 为全局注册清单。
- 存量 `migrations.go` 中的一次性数据迁移已包装为 Step（保留原守卫条件，旧库首次升级执行一次且为 no-op），由 `Store.Migrate` 按库执行；DDL 由模型 + AutoMigrate 取代。

## 与 Store.Migrate 的分工

- 现状：`Store.Migrate` 已切换为 ORM 驱动：对 app/log/metrics 三库分别 `AutoMigrateModels(WithDestructive(true))` → 幂等创建 `ExtraIndexDDL` 复合/部分/复合 UNIQUE 索引 → `RunSteps` 执行一次性数据迁移。migrations.go 不再维护任何 CREATE/ALTER TABLE 类 DDL。
- 特殊直连迁移：证书 scope 约束重建（`migrateCertificateScopeConstraint`）需要在事务外切换 `PRAGMA foreign_keys=OFF`，无法放入 Step 事务，由 `Store.Migrate` 直接调用；其守卫保证已应用后为 no-op。
- 旧库升级路径：存量表不在 `orm_meta` 快照中时本次只记录快照、不删除（模型与存量 schema 已验证零差异）；带 CHECK/索引的表由 `TableConstraints()`/`ExtraIndexDDL()` 覆盖；历史遗留表（如旧 tasks/certificates）由对应 Step 显式清理，不依赖自动删除。

## 存量表模型（internal/platform/database/models）

- 包位置：`internal/platform/database/models`，为 `migrations.go` 中全部存量表（42 张）提供 ORM 模型。
- 与 orm 包的关系：models 只含纯 tag 结构体，仅 import `time` / `database/sql`（`sql.Null*`），不 import orm（tag 只是字符串）；由使用方在初始化时 `orm.Register(models.AllModels()...)` 注册后参与 `AutoMigrate`；按库分组注册用 `models.AppModels()` / `LogModels()` / `MetricsModels()`。
- 三库划分（按库分文件，结构体内按模块分组）：
  - `app_models.go`：app 库 31 张（credentials、servers、panel_installation、packages/image/container 相关、applications 及编辑会话/文件、facility 相关、dns/certificates/key_assets、settings/auth 等）。
  - `log_models.go`：log 库 10 张（tasks、task_steps、task_logs、application_revisions、application_lifecycle_*、runtime_events、runtime_event_details、application_operation_records、key_asset_exports）。
  - `metrics_models.go`：metrics 库 1 张（metrics_snapshots）。
- 命名约定：结构体名 = 表名 PascalCase，实现 `TableName() string` 返回真实表名；列名 = 存量 snake_case 列名，字段名 snake_case 与存量列名不一致时用 `column:xxx` 显式指定（如 `os_id`、`load_1`、`deployment_server_ids_json`）；类型/默认值/not_null/unique/index/primary_key/references 与 migrations.go DDL 逐一对应；时间列一律 `time.Time`（TEXT RFC3339Nano，不写 `time_format`），可空列用 `*T`，布尔用 `bool`（INTEGER 0/1），JSON 列用 `orm:"json"` + map/slice/struct。
- 模型扩展接口补齐无法用 orm tag 表达的元素：`TableConstraints()` 声明原始表级约束子句（12 处 CHECK，如 `CHECK(type IN ('password','private_key'))`），建表时拼入并在重建时保留；`ExtraIndexDDL()` 按表名声明 30 个索引（17 复合索引 + 2 部分索引 + 9 复合 UNIQUE + 2 单列普通索引），由 `Store.Migrate` 以 `CREATE [UNIQUE] INDEX IF NOT EXISTS` 幂等创建。
- 接管验证：`models_test.go` 用临时目录构造全新库跑完 `Store.Migrate` 后，以 `orm.Register(全部模型) + orm.AutoMigrate(WithDestructive(false))` 做非破坏接管，断言对存量表零 drift（CHECK/复合/部分/复合 UNIQUE 已被 `TableConstraints()`/`ExtraIndexDDL()` 覆盖，Pending 为空、无增删列/索引）；并额外校验模型建出的表与存量 schema 逐列一致（类型/默认值/非空/主键），以及 `ExtraIndexDDL` 注册表按“表 + 列 + 唯一 + 部分”形状完整覆盖所有无法表达的存量索引。
- 注意：`AutoMigrateModels` 提供严格按库隔离——只处理传入清单的表，不会跨库为其他库模型建表（`Store.Migrate` 的三库迁移即走该入口）；`AutoMigrate`（全局注册表路径）仍会处理全部已注册模型。

## 验证

- 单元测试覆盖元数据解析、builder、CRUD、批量、事务、AutoMigrate/AutoMigrateModels（幂等/补列/索引/删列/删表/重建/drift/非破坏）、RunSteps，以及内部表（orm_meta/orm_migrations）永不进入 DriftReport 与删除逻辑。
- 模型扩展测试覆盖 `TableConstraints()` 建表拼入与重建保留、`ExtraIndexDDL()` 解析与存量 `sqlite_autoindex_*` 按列形状匹配、按库隔离不跨库建表。
- 快速迭代：`go test ./internal/platform/database/orm/...`；回归基线：`task test:backend`。