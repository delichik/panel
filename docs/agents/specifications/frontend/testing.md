# v3 前端测试规范

> 测试基座：Vitest + jsdom（单元）、@vue/test-utils（组件挂载）。旧 e2e/a11y/visual 测试不迁移，将随 v4 页面族按新交互重建。验证命令一律走 Taskfile，不使用 curl、browser 等工具强行检查页面。

## 命令

| 命令 | 用途 |
| --- | --- |
| `task test:web` | 前端测试聚合入口（当前等同 `task test:web:unit`；e2e/a11y 重建后重新聚合） |
| `task test:web:unit` | 单元测试（Node 环境，`npm run test:unit` → `vitest run --config vitest.unit.config.ts`） |
| `task build:web` | 构建检查（`vue-tsc --noEmit && vite build`），类型/路由/样式/依赖变更后运行 |
| `task run:web:test` | Mock 模式开发服务器（`VITE_PANEL_TEST_MODE=true`，Mock 接管 `/api/v1`，默认不启用认证验证），用于人工联调 |
| `task run:web:test AUTH=true` | Mock 模式开发服务器并启用认证验证（登录、token session、强制改密、JWT secret 校验） |

执行范围遵守 AGENTS.md：只改前端不跑后端测试；纯文档改动不跑测试/编译。

## 单元测试组织

- 测试文件 `*.test.ts` **就近放置**在被测对象旁（如 `web/src/components/shell/navModel.test.ts`、`web/src/components/templates/templates.test.ts`、`web/src/design/useThemeMode.test.ts`），不建平行的 `tests/` 目录镜像。
- 需要 DOM 的组件测试在文件首行标注 `// @vitest-environment jsdom`，用 `@vue/test-utils` 的 `mount` 断言结构区域、插槽渲染与事件（参考 `templates.test.ts` 对六个模板的断言方式：结构 class、插槽内容就位、缺省插槽不渲染、props/emit 行为）。
- 纯逻辑优先抽成纯函数模块再测（如 `navModel.ts` 的导航可见性/高亮模型），避免为测逻辑而挂载组件。
- 主题与视觉数值的测试断言 token 来源正确（经 `main.css` / `theme.ts`），而不是断言具体色值——色值以冻结规格为准，测试不应成为改数值的第二处修改点。

## 页面族重写时的测试要求

每完成一个页面族（设计文档 §10 的重设计四步之后），必须同步补齐：

1. **单元**：新增共享组件、composable、纯逻辑模型的 `*.test.ts`。
2. **e2e**：Playwright 按页面族重建，覆盖交互模型的关键链路——列表查询进 URL 与恢复、编辑脏状态离开保护、异步任务两阶段反馈。e2e 基座（Playwright）保留，v1 旧用例不迁移，按新页面重写。
3. **a11y**：自有 primitives 必须提供基础语义、focus ring 和可访问名称；关键页面在 e2e 中保留 axe 检查（`@axe-core/playwright`）。
4. **visual**：视情况恢复 Playwright 截图对比，覆盖三档桌面宽度 + 一档窄屏（1024px 断点两侧）。

e2e/a11y 的聚合任务（`task test:web:e2e`、`task test:web:a11y`）随首个页面族的 e2e 落地时在 Taskfile 恢复，并重新挂入 `task test:web`。

## Mock 体系

- `task run:web:test` 启动的 Mock 模式是新页面开发的主要联调环境；当前通过 `web/src/mocks/browser.ts` 在浏览器内拦截 `/api/v1`，默认直接提供已认证演示 session，不做登录、token、强制改密和 JWT secret 校验。需要验证认证链路时显式运行 `task run:web:test AUTH=true`。
- Mock 必须与 `web/src/api/` 的路径和统一 JSON envelope 一致；未实现的 Mock 路由返回明确的 `mock_route_not_found`。
- 种子数据覆盖正常、空、错误、长文本、禁用等代表性状态；新增页面状态时同步补充。
