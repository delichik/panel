import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const listSource = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');
const detailSource = readFileSync(resolve(__dirname, './components/ApplicationDetailPane.vue'), 'utf8');

/**
 * 重设计契约（task-46）：
 * - 列表列 ≤5：名称(+namespace/image 副行) / 状态(单徽标+一个次级) / 实例摘要合并 / 更新时间 / 操作；
 * - 详情 ≥wide 主内容 + 摘要侧栏，短属性 PropertyList 2 列，长列表全宽；
 * - 编辑器保持 1000 窄列 + 表单两列 + ≥xl 摘要右列，长代码/YAML 资产区全宽（不再塞进窄主列）。
 */
describe('applications list layout', () => {
  it('keeps at most five columns and folds image/update columns into name/status', () => {
    const block = listSource.slice(listSource.indexOf('const applicationColumns'), listSource.indexOf('const logsApplication'));
    const keys = [...block.matchAll(/key: '([^']+)'/g)].map((match) => match[1]);
    expect(keys).toEqual(['name', 'status', 'instances', 'updatedAt', 'actions']);

    // 移除独立 image / imageUpdate 列及其单元格插槽。
    expect(listSource).not.toContain('<template #image="{ row }">');
    expect(listSource).not.toContain('<template #imageUpdate="{ row }">');
    // 名称副行合并 namespace + image reference；状态只保留一枚次级标记。
    expect(listSource).toContain('row.app.namespace');
    expect(listSource).toContain('rowImageReference(row.app)');
    expect(listSource).toContain('row.app.imageUpdateAvailable');
  });
});

describe('applications detail layout', () => {
  it('splits runtime into main content + summary sidebar at wide with a 2-column PropertyList', () => {
    expect(detailSource).toContain('wide:grid-cols-[minmax(0,1fr)_320px]');
    expect(detailSource).toContain('wide:sticky wide:top-0');
    expect(detailSource).toContain(':columns="2"');
    // 主内容是实例列表，长表格/长列表不再被挤进三列摘要。
    const block = detailSource.slice(detailSource.indexOf('wide:grid-cols-[minmax(0,1fr)_320px]'), detailSource.indexOf('</template>'));
    expect(block).toContain('<ApplicationInstanceList');
    expect(block).toContain('<PropertyList');
    expect(detailSource).not.toContain(':columns="3"');
  });
});

describe('applications editor layout', () => {
  it('keeps the long asset/code area full width below the two-column form grid', () => {
    // 两个编辑器（应用 / 设施记录）都把资产区移出 xl 双列，放在网格后全宽呈现。
    expect(listSource.match(/class="mt-4 surface-card p-4"/g) ?? []).toHaveLength(2);
    const assetBlocks = [...listSource.matchAll(/<\/aside>\s*<\/div>\s*\n\s*<!--[^>]*资产[\s\S]*?<AssetFileManager/g)];
    expect(assetBlocks).toHaveLength(2);
    // 表单仍是 ≥sm 两列，摘要仍是 ≥xl 右列 + sticky（1024–1279 下移）。
    expect(listSource.match(/sm:grid-cols-2/g)?.length ?? 0).toBeGreaterThanOrEqual(4);
    expect(listSource.match(/xl:grid-cols-\[minmax\(0,1fr\)_320px\]/g) ?? []).toHaveLength(2);
    expect(listSource.match(/xl:sticky xl:top-0/g) ?? []).toHaveLength(2);
  });
});
