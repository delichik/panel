import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

function read(relative: string) {
  return readFileSync(resolve(__dirname, relative), 'utf8');
}

const indexSource = read('index.vue');
const metricsSource = read('MetricsFacility.vue');
const probeSource = read('NetworkProbeFacility.vue');
const instanceListSource = read('components/ApplicationInstanceList.vue');
const diagnosticsSource = read('FacilityDiagnostics.vue');

/**
 * 迁移批次 5 契约：
 * - applications 内数值输入统一走 NumberBox（含应用/设施路径高级选项、Metrics/NetworkProbe 数值字段），
 *   不再出现 `Input type="number"`；
 * - 原生 `<details>/<summary>` 统一替换为 Expander（aria-expanded/controls + 插槽内容不变）；
 * - 编辑器/设施配置页内容列与所有页面一致（ConsolePage width="content"）。
 */
describe('applications NumberBox migration', () => {
  it('removes every native number input in the applications family', () => {
    for (const source of [indexSource, metricsSource, probeSource, instanceListSource, diagnosticsSource]) {
      expect(source).not.toMatch(/type="number"/);
      expect(source).not.toMatch(/<Input[^>]*type="number"/);
    }
  });

  it('uses NumberBox for numeric fields with FormField aria wiring', () => {
    // 弹窗高级选项 8 处 + 指标 2 处 + 探测 2 个模板节点（间隔 + 参数 v-for，运行时渲染 3 个 NumberBox）。
    expect(indexSource.match(/<NumberBox/g)?.length ?? 0).toBe(8);
    expect(metricsSource.match(/<NumberBox/g)?.length ?? 0).toBe(2);
    expect(probeSource.match(/<NumberBox/g)?.length ?? 0).toBe(2);
    // FormField 插槽属性接到 NumberBox 的 id/aria-describedby/invalid。
    expect(indexSource).toContain(':aria-describedby="describedBy"');
    expect(indexSource).toContain(':invalid="invalid"');
    expect(probeSource).toContain('data-testid="network-probe-interval-minutes"');
    expect(metricsSource).toContain('data-testid="metrics-retention"');
  });

  it('replaces native details with the shared Expander', () => {
    expect(instanceListSource).not.toContain('<details');
    expect(diagnosticsSource).not.toContain('<details');
    expect(instanceListSource).not.toContain('<summary');
    expect(diagnosticsSource).not.toContain('<summary');
    expect(instanceListSource).toContain('<Expander');
    expect(diagnosticsSource.match(/<Expander/g)?.length ?? 0).toBe(2);
    // 关闭态语义/内容文案保持不变。
    expect(instanceListSource).toContain("t('facilityDiagnostics.technicalDetails')");
    expect(diagnosticsSource).toContain("t('facilityDiagnostics.evidence')");
  });
});

describe('applications content column consistency', () => {
  it('keeps editors on the shared 1200 content column', () => {
    expect(indexSource.match(/width="content"/g)?.length ?? 0).toBeGreaterThanOrEqual(2);
    expect(indexSource).not.toContain('width="narrow"');
  });
});
