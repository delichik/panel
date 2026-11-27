import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

/**
 * P2 设施设置页契约：每域名 80 端口策略覆盖（设施域名记录 + 应用反代规则）
 * 与镜像滚动更新节点间隔随 settings PUT 提交。
 */
describe('facility settings P2 controls', () => {
  it('offers the per-domain port 80 override in both transport editors', () => {
    expect(source.match(/t\('applicationsPage\.transport\.httpPortMode'\)/g)).toHaveLength(2);
    expect(source).toContain('domainHttpPortModeOptions');
    expect(source).toContain('facilityRecordHttpPortModeModel');
    expect(source).toContain('proxyHttpPortModeModel');
    expect(source).toContain("t('applicationsPage.transport.httpPortModeInherit')");
    expect(source).toContain("t('applicationsPage.transport.httpPortModeServe')");
    expect(source).toContain("t('applicationsPage.transport.httpPortModeRedirect')");
    expect(source).toContain("t('applicationsPage.transport.httpPortModeRedirectNeedsHttps')");
  });

  it('submits the per-domain override through both save pipelines', () => {
    expect(source).toContain('httpPortMode: proxyHttpPortModeModel.value');
    const model = readFileSync(resolve(__dirname, 'model.ts'), 'utf8');
    expect(model).toContain('httpPortMode: domainHttpPortMode(rule)');
    expect(model).toContain('httpPortMode: domainHttpPortMode(domain)');
    expect(model).toContain('httpPortMode: domainHttpPortMode(cloned)');
    expect(model).toContain("errors.httpPortMode = 'applicationsPage.validationHttpPortMode'");
  });

  it('submits the image update node interval with the settings partition', () => {
    expect(source).toContain("t('applicationsPage.facilitySettings.imageUpdateIntervalSeconds')");
    expect(source).toContain("t('applicationsPage.facilitySettings.imageUpdateIntervalSecondsHint')");
    expect(source).toContain('imageUpdateIntervalSeconds: facilitySettingsForm.imageUpdateIntervalSeconds');
    expect(source).toContain('isValidImageUpdateIntervalSeconds');
    expect(source).toContain('normalizeImageUpdateIntervalSeconds');
    expect(source).toContain('IMAGE_UPDATE_INTERVAL_MAX_SECONDS');
    expect(source).toContain('facilitySettingsErrors.imageUpdateIntervalSeconds');
  });

  it('offers per-domain HSTS and request body limit overrides in both transport editors', () => {
    expect(source.match(/t\('applicationsPage\.transport\.hsts'\)/g)).toHaveLength(2);
    expect(source.match(/t\('applicationsPage\.transport\.clientMaxBodySize'\)/g)).toHaveLength(2);
    expect(source).toContain('domainHstsOptions');
    expect(source).toContain('facilityRecordHstsModel');
    expect(source).toContain('proxyHstsModeModel');
    expect(source).toContain('facilityRecordClientMaxBodySizeModel');
    expect(source).toContain('proxyClientMaxBodySizeModel');
    expect(source).toContain("t('applicationsPage.transport.hstsInherit')");
    expect(source).toContain("t('applicationsPage.transport.hstsEnabled')");
    expect(source).toContain("t('applicationsPage.transport.hstsDisabled')");
    expect(source).toContain("t('applicationsPage.transport.clientMaxBodySizeHint')");
    expect(source).toContain("t('applicationsPage.transport.clientMaxBodySizePlaceholder')");
    expect(source).toContain("t('applicationsPage.transport.clientMaxBodySizeInvalid')");
    expect(source).toContain('facilityRecordHstsHint');
    expect(source).toContain('proxyHstsHint');
  });

  it('submits per-domain HSTS and body-limit overrides through both save pipelines', () => {
    expect(source).toContain('hstsEnabledFromMode(proxyHstsModeModel.value)');
    expect(source).toContain('clientMaxBodySize: domainClientMaxBodySize(proxyDraft)');
    const model = readFileSync(resolve(__dirname, 'model.ts'), 'utf8');
    expect(model).toContain('hstsEnabledFromMode(domainHstsMode(domain))');
    expect(model).toContain('clientMaxBodySize: domainClientMaxBodySize(domain)');
    expect(model).toContain('clientMaxBodySize: domainClientMaxBodySize(rule)');
    expect(model).toContain('isValidDomainClientMaxBodySize');
    expect(model).toContain('delete next.hstsEnabled');
  });
});
