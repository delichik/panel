import { describe, expect, it } from 'vitest';
import {
  isValidNetworkProbeSetting,
  networkProbeSettingError,
  networkProbeSettingFields,
  networkProbeSettingRules,
  networkProbeSettingsError,
  normalizeNetworkProbeSettings,
  parseNetworkProbeSetting,
} from './networkProbe';

describe('network probe settings contract', () => {
  it('publishes the agreed defaults and ranges', () => {
    expect(networkProbeSettingFields).toEqual(['intervalSeconds', 'pingCount', 'pingTimeoutSeconds']);
    expect(networkProbeSettingRules.intervalSeconds).toMatchObject({ min: 10, max: 3600, defaultValue: 60, errorCode: 'network_probe_interval_invalid' });
    expect(networkProbeSettingRules.pingCount).toMatchObject({ min: 1, max: 10, defaultValue: 3, errorCode: 'network_probe_ping_count_invalid' });
    expect(networkProbeSettingRules.pingTimeoutSeconds).toMatchObject({ min: 1, max: 10, defaultValue: 2, errorCode: 'network_probe_ping_timeout_invalid' });
  });

  it('accepts only integers inside the inclusive range', () => {
    expect(isValidNetworkProbeSetting('intervalSeconds', 10)).toBe(true);
    expect(isValidNetworkProbeSetting('intervalSeconds', 3600)).toBe(true);
    expect(isValidNetworkProbeSetting('intervalSeconds', 9)).toBe(false);
    expect(isValidNetworkProbeSetting('intervalSeconds', 3601)).toBe(false);
    expect(isValidNetworkProbeSetting('intervalSeconds', 60.5)).toBe(false);
    expect(isValidNetworkProbeSetting('intervalSeconds', '60')).toBe(false);
    expect(isValidNetworkProbeSetting('intervalSeconds', Number.NaN)).toBe(false);
    expect(isValidNetworkProbeSetting('pingCount', 1)).toBe(true);
    expect(isValidNetworkProbeSetting('pingCount', 11)).toBe(false);
    expect(isValidNetworkProbeSetting('pingTimeoutSeconds', 10)).toBe(true);
    expect(isValidNetworkProbeSetting('pingTimeoutSeconds', 0)).toBe(false);
  });

  it('treats omitted legacy fields as valid but explicit invalid values as contract errors', () => {
    expect(networkProbeSettingError('intervalSeconds', undefined)).toBeNull();
    expect(networkProbeSettingError('intervalSeconds', null)).toBeNull();
    expect(networkProbeSettingError('intervalSeconds', 60)).toBeNull();
    expect(networkProbeSettingError('intervalSeconds', 5)).toBe('network_probe_interval_invalid');
    expect(networkProbeSettingError('pingCount', 0)).toBe('network_probe_ping_count_invalid');
    expect(networkProbeSettingError('pingTimeoutSeconds', 11)).toBe('network_probe_ping_timeout_invalid');
  });

  it('normalizes legacy or invalid input to the documented defaults', () => {
    expect(normalizeNetworkProbeSettings({})).toEqual({ intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 });
    expect(normalizeNetworkProbeSettings({ intervalSeconds: 30, pingCount: 5, pingTimeoutSeconds: 4 })).toEqual({ intervalSeconds: 30, pingCount: 5, pingTimeoutSeconds: 4 });
    expect(normalizeNetworkProbeSettings({ intervalSeconds: 1_000_000, pingCount: '3', pingTimeoutSeconds: 0 })).toEqual({ intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 });
  });

  it('reports the first invalid field for save payload validation', () => {
    expect(networkProbeSettingsError({})).toBeNull();
    expect(networkProbeSettingsError({ intervalSeconds: 30, pingCount: 3, pingTimeoutSeconds: 2 })).toBeNull();
    expect(networkProbeSettingsError({ intervalSeconds: 9, pingCount: 20 })).toEqual({ field: 'intervalSeconds', code: 'network_probe_interval_invalid' });
    expect(networkProbeSettingsError({ pingCount: 20, pingTimeoutSeconds: 0 })).toEqual({ field: 'pingCount', code: 'network_probe_ping_count_invalid' });
    expect(networkProbeSettingsError({ pingTimeoutSeconds: 0 })).toEqual({ field: 'pingTimeoutSeconds', code: 'network_probe_ping_timeout_invalid' });
  });

  it('parses numeric form input and rejects blanks and fractions', () => {
    expect(parseNetworkProbeSetting('60')).toBe(60);
    expect(parseNetworkProbeSetting(' 3600 ')).toBe(3600);
    expect(parseNetworkProbeSetting('')).toBeNull();
    expect(parseNetworkProbeSetting('   ')).toBeNull();
    expect(parseNetworkProbeSetting('1.5')).toBeNull();
    expect(parseNetworkProbeSetting('abc')).toBeNull();
  });
});
