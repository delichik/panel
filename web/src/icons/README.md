# Fluent System Icons（本地 vendored）

来源：[`@fluentui/svg-icons@1.1.346`](https://www.npmjs.com/package/@fluentui/svg-icons)（MIT，Microsoft Fluent UI System Icons）。

- `fluent/`：从官方包挑选的 **24px regular** SVG（白名单见生成脚本 `tmp/gen-fluent-icons.mjs`），
  只保留本轮页面实际使用到的图标；不引入任何运行时外部请求/字体。
- `index.ts`：**自动生成**的 Vue 兼容层。导出名与旧 `@lucide/vue` 导入名一致，
  页面只改 import 路径（`@/icons`），标签、class、`size` 用法保持不变；渲染为
  `<svg viewBox="0 0 24 24" fill="currentColor">`，默认 `aria-hidden="true"`（装饰性图标）。
- 许可：图标原始文件遵循 MIT（见 <https://github.com/microsoft/fluentui-system-icons>）；
  本仓库仅保留图标文件与生成的包装代码，不修改图标语义。

> **例外**：操作系统发行版标识（Ubuntu/Debian/Arch 等品牌色徽标）不属于本 Fluent 图标层，位于
> `web/src/components/patterns/`（`OsMark.vue` + Simple Icons CC0 数据），不要在此目录添加品牌图标。

## 重新生成

```powershell
# 1) 下载并解压官方包（tmp 下，临时文件不入库）
Invoke-WebRequest https://registry.npmjs.org/@fluentui/svg-icons/-/svg-icons-1.1.346.tgz -OutFile tmp/fluent-icons/svg-icons.tgz
tar -xzf tmp/fluent-icons/svg-icons.tgz -C tmp/fluent-icons
# 2) 按 tmp/gen-fluent-icons.mjs 的白名单复制需要的 24px regular SVG 到 web/src/icons/fluent/
# 3) 生成 index.ts
node tmp/gen-fluent-icons.mjs
```

禁止直接编辑 `index.ts`；新增图标时同时更新白名单脚本与本文。
