# 多语言编写指南

本文档约束 Panel 项目内多语言功能的实现方式。目标不是“把字符串翻出来”，而是保证前后端语言行为一致、可维护、可继续扩展。

## 当前约定

- 默认语言：`en`
- 第二语言：`zh-CN`
- 前端翻译入口：`web/src/i18n/index.ts`
- 前端运行时语言同步：`web/src/stores/settings.ts`
- 后端翻译入口：`internal/i18n/i18n.go`
- 后端运行时语言设置：`internal/settings/service.go`
- 语言设置来源：`/api/v1/settings/runtime`

## 适用范围

- 前端页面文案
- 前端共享组件文案
- 路由标题、说明、副标题
- 后端 API 错误响应
- 设置页中的语言读写逻辑
- 持久化配置里与展示文案相关的结构设计

## 前端规则

### 1. 禁止直接硬编码用户可见文案

以下内容不能直接写在页面或组件中：

- 按钮文本
- 表单标签
- 空状态文案
- Snackbar / Alert 文案
- 表格列名
- 卡片标题
- 路由标题与副标题

统一放入 `web/src/i18n/index.ts`，并通过 `t()` 读取。

### 2. 页面与组件统一使用 `useI18n()`

Vue 页面或组件中统一使用：

```ts
import { useI18n } from '@/i18n';

const { t } = useI18n();
```

需要格式化日期、时间、任务状态等时，优先使用 i18n 提供的辅助函数：

- `formatDateTime`
- `formatTime`
- `translateTaskStatus`
- `translateCleanupSchedule`

### 3. 路由元信息使用 key，不直接写文案

错误示例：

```ts
meta: { title: 'Settings' }
```

正确示例：

```ts
meta: { titleKey: 'routes.settings.title' }
```

同理适用于：

- `titleKey`

### 4. 持久化配置不要保存展示文案

涉及本地存储、数据库、保存会话、卡片配置等场景时：

- 只保存稳定值，例如 `kind`、`status`、`mode`、`range`
- 不要保存当前语言下的标题、副标题、按钮名
- 展示文案必须在渲染阶段通过 key 或值映射动态生成

例如仪表盘卡片应保存 `kind: 'cpu'`，而不是保存 `title: 'CPU'` 或 `title: '处理器'`。

### 5. 翻译 key 命名保持稳定

建议按功能分组：

- `common.*`
- `layout.*`
- `routes.*`
- `overviewPage.*`
- `serversPage.*`
- `applicationsPage.*`
- `applicationEditor.*`
- `taskCenter.*`

不要把 key 命名成某一种语言的完整句子，也不要频繁重命名已存在 key。

### 6. 优先复用公共词条

通用动作或字段优先放入 `common.*`，例如：

- `common.save`
- `common.cancel`
- `common.delete`
- `common.actions`
- `common.type`
- `common.status`
- `common.name`

## 后端规则

### 1. 对外错误响应必须通过错误码翻译

后端继续返回稳定的：

- `code`
- fallback `message`

最终响应文案由统一翻译入口处理，不要在各个 handler / service 中分散拼接中文或英文分支。

### 2. 语言切换集中在设置与 i18n 模块

不要在业务代码里散落：

```go
if locale == "zh-CN" {
    ...
}
```

语言切换应集中在：

- `internal/settings/service.go`
- `internal/i18n/i18n.go`
- `internal/httpx/httpx.go`

### 3. 新增影响语言体验的运行时配置时，必须评估是否进入 settings

如果新配置会影响用户语言体验，不要只放在前端本地状态中，优先评估是否需要纳入：

- `RuntimeSettings`
- `RuntimeUpdate`
- `/api/v1/settings/runtime`

## 修改流程

处理新增或迁移文案时，按下面顺序执行：

1. 确认文案属于哪个功能域
2. 在 `web/src/i18n/index.ts` 或 `internal/i18n/i18n.go` 增加词条
3. 将调用方改为使用 key / 翻译函数
4. 如果涉及持久化结构，确认没有把展示文案写入存储
5. 如果涉及系统级语言行为，确认 settings 接口与默认值是否需要同步
6. 更新 `docs/i18n-translation-status.md`
7. 执行相关 `task build:*` / `task test:*`

## 暂不建议做的事

- 当前阶段不要引入重量级 i18n 框架，除非现有方案明确无法支撑
- 不要为了翻译把业务逻辑拆得过细
- 不要把内部标识字符串误当成用户文案翻译，例如 API 参数值、数据库键名、内部枚举常量

## 提交前自检

- 是否还有新增的硬编码用户文案
- 是否复用了已有 `common.*` 词条
- 是否同时补齐了英文和简体中文
- 是否更新了未翻译清单
- 是否运行了相关 `task build:*` / `task test:*`
