import { describe, expect, it } from 'vitest';
import type { RuntimeSettings } from '@/types/settings';
import { runtimeLanguageUpdate } from './settings';

const current: RuntimeSettings = {
  listenAddress: '127.0.0.1:8080',
  appDatabase: 'app.db',
  metricsDatabase: 'metrics.db',
  dataRoot: 'data',
  metricsRetentionDays: 14,
  metricsCollectionIntervalSeconds: 60,
  containerReportIntervalSeconds: 30,
  cleanupSchedule: 'daily',
  tokenExpiration: '1d',
  language: 'en',
  logLevel: 'info',
  remoteCommandTimeoutSeconds: 45,
  reconcileTraceEnabled: true,
  branding: { loginTitle: 'Seamark', loginSubtitle: 'Panel' },
  certificates: { email: 'admin@example.com', dnsPropagationDelaySeconds: 30 },
  panel: { domain: 'panel.example.com', tlsCertificateId: 'tls-1' },
  jwtSecretConfigured: true,
};

describe('runtimeLanguageUpdate', () => {
  it('preserves required scalar settings and changes only the language', () => {
    expect(runtimeLanguageUpdate(current, 'zh-CN')).toEqual({
      metricsRetentionDays: 14,
      metricsCollectionIntervalSeconds: 60,
      containerReportIntervalSeconds: 30,
      cleanupSchedule: 'daily',
      tokenExpiration: '1d',
      language: 'zh-CN',
      logLevel: 'info',
      remoteCommandTimeoutSeconds: 45,
    });
  });
});
