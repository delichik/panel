import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * ListPage 状态槽契约（修复批次 3）：
 * 列表页的页级 loading/error/空态必须走 `ListPage` 的 `state` + `#loading/#error/#empty` 槽，
 * 而不是在默认插槽里用 v-if 手写；同时保留 Table 自带的 inset `#empty`（筛选无结果）。
 * applications/events(operations/tasks 共用)/logs 三个列表族在同一批接入。
 */
const sources = {
  applications: readFileSync(resolve(__dirname, '../applications/index.vue'), 'utf8'),
  records: readFileSync(resolve(__dirname, './IntentRecordsPage.vue'), 'utf8'),
  logs: readFileSync(resolve(__dirname, '../logs/index.vue'), 'utf8'),
} as const;

describe('list page state slots', () => {
  it.each(Object.entries(sources))('%s uses ListPage state + named state slots', (name, source) => {
    expect(source).toMatch(/<ListPage\s+:state="/);
    expect(source).toMatch(/<template #loading>/);
    expect(source).toMatch(/<template #error>/);
    expect(source).toMatch(/<template #empty>/);
    // 页级槽已接管错误/空态后，列表默认插槽不再手写错误/空态分支。
    expect(source).not.toMatch(/<EmptyState v-if="error/);
    if (name !== 'applications') {
      // applications 页面还有设施配置等内嵌表格，仅事件/日志为单表页面可直接断言。
      expect(source).not.toMatch(/<Table\s+v-else/);
    }
  });

  it('keeps the in-table inset empty state and auto rows where expected', () => {
    for (const source of Object.values(sources)) {
      expect(source).toContain('variant="inset"');
    }
    // applications 状态列改为多行行（StatusBadge + planning/attention）。
    expect(sources.applications).toContain('row-height="auto"');
    expect(sources.applications).toContain('flex flex-nowrap items-center gap-1');
    // events/logs 的多行行契约保持。
    expect(sources.records).toContain('row-height="auto"');
    expect(sources.logs).toContain('row-height="auto"');
  });
});
