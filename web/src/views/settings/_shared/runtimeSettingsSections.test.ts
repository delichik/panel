import { describe, expect, it } from 'vitest';
import type { RuntimeSettingsDto, RuntimeSettingsUpdate } from '@/types/api';
import { buildRuntimeSettingsSectionUpdate } from './runtimeSettingsSections';

const settings: RuntimeSettingsDto = {
  listenAddress: ':8080',
  appDatabase: 'app.db',
  metricsDatabase: 'metrics.db',
  dataRoot: 'data',
  metricsRetentionDays: 7,
  metricsCollectionIntervalSeconds: 60,
  containerReportIntervalSeconds: 5,
  cleanupSchedule: 'daily',
  tokenExpiration: '1d',
  language: 'en',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: 30,
  branding: {
    loginTitle: 'Saved title',
    loginSubtitle: 'Saved subtitle',
  },
  certificates: {
    email: 'saved@example.com',
    dnsPropagationDelaySeconds: 30,
  },
  jwtSecretConfigured: true,
};

const form: RuntimeSettingsUpdate = {
  metricsRetentionDays: 90,
  metricsCollectionIntervalSeconds: 120,
  containerReportIntervalSeconds: 3,
  cleanupSchedule: 'weekly',
  tokenExpiration: '5d',
  language: 'zh-CN',
  logLevel: 'debug',
  remoteCommandTimeoutSeconds: 120,
  branding: {
    loginTitle: 'Draft title',
    loginSubtitle: 'Draft subtitle',
  },
  certificates: {
    email: 'draft@example.com',
    dnsPropagationDelaySeconds: 90,
  },
};

describe('buildRuntimeSettingsSectionUpdate', () => {
  it('only applies branding draft values when saving branding', () => {
    expect(buildRuntimeSettingsSectionUpdate(settings, form, 'branding')).toEqual({
      metricsRetentionDays: 7,
      metricsCollectionIntervalSeconds: 60,
      containerReportIntervalSeconds: 5,
      cleanupSchedule: 'daily',
      tokenExpiration: '1d',
      language: 'en',
      logLevel: 'info',
      remoteCommandTimeoutSeconds: 30,
      branding: form.branding,
      certificates: settings.certificates,
    });
  });

  it('only applies runtime draft values when saving runtime', () => {
    expect(buildRuntimeSettingsSectionUpdate(settings, form, 'runtime')).toEqual({
      ...form,
      branding: settings.branding,
      certificates: settings.certificates,
    });
  });

  it('only applies certificate draft values when saving certificates', () => {
    expect(buildRuntimeSettingsSectionUpdate(settings, form, 'certificates')).toEqual({
      metricsRetentionDays: 7,
      metricsCollectionIntervalSeconds: 60,
      containerReportIntervalSeconds: 5,
      cleanupSchedule: 'daily',
      tokenExpiration: '1d',
      language: 'en',
      logLevel: 'info',
      remoteCommandTimeoutSeconds: 30,
      branding: settings.branding,
      certificates: form.certificates,
    });
  });
});
