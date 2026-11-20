import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

/**
 * P1 设施设置页契约：镜像回滚/恢复自动更新、pinned 与失败退避展示、
 * 三项 P1 安全参数随 settings PUT 提交。
 */
describe('facility settings P1 controls', () => {
  it('wires rollback and resume without treating them as automatic saves', () => {
    expect(source).toContain('facilityImagePinned');
    expect(source).toContain('facilityImageFailures');
    expect(source).toContain('facilityImageAutoUpdatePaused');
    expect(source).toContain('rollbackImage');
    expect(source).toContain('resumeImage');
    expect(source).toContain('facilityImageRollbackOpen');
    expect(source).toContain("t('applicationsPage.facilityImage.rollback')");
    expect(source).toContain("t('applicationsPage.facilityImage.resume')");
    expect(source).toContain("t('applicationsPage.facilityImage.failureBackoff'");
  });

  it('submits the P1 safety parameters with the settings partition', () => {
    expect(source).toContain('serverTokens: facilitySettingsForm.serverTokens');
    expect(source).toContain('clientMaxBodySize: facilitySettingsForm.clientMaxBodySize.trim()');
    expect(source).toContain('tlsMinVersion: facilitySettingsForm.tlsMinVersion');
    expect(source).toContain('isValidNginxClientMaxBodySize');
    expect(source).toContain('facilitySettingsErrors.clientMaxBodySize');
  });
});
