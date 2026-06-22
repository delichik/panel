import type { RuntimeSettingsDto, RuntimeSettingsUpdate } from '@/types/api';

export type RuntimeSettingsSection = 'branding' | 'runtime' | 'certificates';

export function buildRuntimeSettingsSectionUpdate(
  settings: RuntimeSettingsDto,
  form: RuntimeSettingsUpdate,
  section: RuntimeSettingsSection,
): RuntimeSettingsUpdate {
  const update: RuntimeSettingsUpdate = {
    metricsRetentionDays: settings.metricsRetentionDays,
    metricsCollectionIntervalSeconds: settings.metricsCollectionIntervalSeconds,
    cleanupSchedule: settings.cleanupSchedule,
    tokenExpiration: settings.tokenExpiration,
    language: settings.language,
    logLevel: settings.logLevel,
    remoteCommandTimeoutSeconds: settings.remoteCommandTimeoutSeconds,
    branding: { ...settings.branding },
    certificates: { ...settings.certificates },
  };

  if (section === 'branding') {
    update.branding = { ...form.branding };
  } else if (section === 'runtime') {
    update.metricsRetentionDays = form.metricsRetentionDays;
    update.metricsCollectionIntervalSeconds = form.metricsCollectionIntervalSeconds;
    update.cleanupSchedule = form.cleanupSchedule;
    update.tokenExpiration = form.tokenExpiration;
    update.language = form.language;
    update.logLevel = form.logLevel;
    update.remoteCommandTimeoutSeconds = form.remoteCommandTimeoutSeconds;
  } else {
    update.certificates = { ...form.certificates };
  }

  return update;
}
