import { describe, expect, it } from 'vitest';
import {
  isValidNetworkProbeIntervalMinutes,
  isValidNetworkProbeSetting,
  networkProbeIntervalMinutes,
  networkProbeIntervalMinutesFromSeconds,
  networkProbeIntervalSecondsFromMinutes,
  networkProbeSettingError,
  networkProbeSettingFields,
  networkProbeSettingRules,
  networkProbeSettingsError,
  normalizeNetworkProbeSettings,
  parseNetworkProbeIntervalMinutes,
  parseNetworkProbeSetting,
  probeAggregateOutcomeStatusKey,
  probeAggregateOutcomeTone,
  probeHistorySeries,
  probeTargetOutcome,
} from './networkProbe';

describe('network probe settings contract', () => {
  it('publishes the agreed defaults and ranges', () => {
    expect(networkProbeSettingFields).toEqual(['intervalSeconds', 'pingCount', 'pingTimeoutSeconds']);
    // 间隔按后台秒口径 60..3600（即 1..60 分钟），默认 60 秒。
    expect(networkProbeSettingRules.intervalSeconds).toMatchObject({ min: 60, max: 3600, defaultValue: 60, errorCode: 'network_probe_interval_invalid' });
    expect(networkProbeSettingRules.pingCount).toMatchObject({ min: 1, max: 10, defaultValue: 3, errorCode: 'network_probe_ping_count_invalid' });
    expect(networkProbeSettingRules.pingTimeoutSeconds).toMatchObject({ min: 1, max: 10, defaultValue: 2, errorCode: 'network_probe_ping_timeout_invalid' });
    // 界面分钟口径 1..60，默认 1 分钟。
    expect(networkProbeIntervalMinutes).toEqual({ min: 1, max: 60, step: 1, defaultValue: 1 });
  });

  it('accepts only integers inside the inclusive range', () => {
    expect(isValidNetworkProbeSetting('intervalSeconds', 60)).toBe(true);
    expect(isValidNetworkProbeSetting('intervalSeconds', 3600)).toBe(true);
    expect(isValidNetworkProbeSetting('intervalSeconds', 59)).toBe(false);
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
    expect(networkProbeSettingError('intervalSeconds', 59)).toBe('network_probe_interval_invalid');
    expect(networkProbeSettingError('pingCount', 0)).toBe('network_probe_ping_count_invalid');
    expect(networkProbeSettingError('pingTimeoutSeconds', 11)).toBe('network_probe_ping_timeout_invalid');
  });

  it('normalizes legacy or invalid input to the documented defaults', () => {
    expect(normalizeNetworkProbeSettings({})).toEqual({ intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 });
    expect(normalizeNetworkProbeSettings({ intervalSeconds: 120, pingCount: 5, pingTimeoutSeconds: 4 })).toEqual({ intervalSeconds: 120, pingCount: 5, pingTimeoutSeconds: 4 });
    // 旧版 10..59 秒不再合法：回退默认 60 秒。
    expect(normalizeNetworkProbeSettings({ intervalSeconds: 30, pingCount: 5, pingTimeoutSeconds: 4 })).toEqual({ intervalSeconds: 60, pingCount: 5, pingTimeoutSeconds: 4 });
    expect(normalizeNetworkProbeSettings({ intervalSeconds: 1_000_000, pingCount: '3', pingTimeoutSeconds: 0 })).toEqual({ intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 });
  });

  it('reports the first invalid field for save payload validation', () => {
    expect(networkProbeSettingsError({})).toBeNull();
    expect(networkProbeSettingsError({ intervalSeconds: 60, pingCount: 3, pingTimeoutSeconds: 2 })).toBeNull();
    expect(networkProbeSettingsError({ intervalSeconds: 59, pingCount: 20 })).toEqual({ field: 'intervalSeconds', code: 'network_probe_interval_invalid' });
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

describe('network probe interval minute conversion', () => {
  it('converts minutes to the backend second contract', () => {
    expect(networkProbeIntervalSecondsFromMinutes(1)).toBe(60);
    expect(networkProbeIntervalSecondsFromMinutes(2)).toBe(120);
    expect(networkProbeIntervalSecondsFromMinutes(60)).toBe(3600);
  });

  it('converts stored seconds back to whole minutes with a one-minute floor', () => {
    expect(networkProbeIntervalMinutesFromSeconds(60)).toBe(1);
    expect(networkProbeIntervalMinutesFromSeconds(90)).toBe(2);
    expect(networkProbeIntervalMinutesFromSeconds(119)).toBe(2);
    expect(networkProbeIntervalMinutesFromSeconds(120)).toBe(2);
    expect(networkProbeIntervalMinutesFromSeconds(3600)).toBe(60);
    // 旧数据 < 60 秒或越界时按最小 1 分钟展示，避免出现 0。
    expect(networkProbeIntervalMinutesFromSeconds(59)).toBe(1);
    expect(networkProbeIntervalMinutesFromSeconds(10)).toBe(1);
    expect(networkProbeIntervalMinutesFromSeconds(undefined)).toBe(1);
    expect(networkProbeIntervalMinutesFromSeconds(Number.NaN)).toBe(1);
  });

  it('validates and parses the minute form input', () => {
    expect(isValidNetworkProbeIntervalMinutes(1)).toBe(true);
    expect(isValidNetworkProbeIntervalMinutes(60)).toBe(true);
    expect(isValidNetworkProbeIntervalMinutes(0)).toBe(false);
    expect(isValidNetworkProbeIntervalMinutes(61)).toBe(false);
    expect(isValidNetworkProbeIntervalMinutes(1.5)).toBe(false);
    expect(isValidNetworkProbeIntervalMinutes('1')).toBe(false);
    expect(isValidNetworkProbeIntervalMinutes(Number.NaN)).toBe(false);

    expect(parseNetworkProbeIntervalMinutes('1')).toBe(1);
    expect(parseNetworkProbeIntervalMinutes(' 60 ')).toBe(60);
    expect(parseNetworkProbeIntervalMinutes('')).toBeNull();
    expect(parseNetworkProbeIntervalMinutes('0')).toBeNull();
    expect(parseNetworkProbeIntervalMinutes('61')).toBeNull();
    expect(parseNetworkProbeIntervalMinutes('1.5')).toBeNull();
    expect(parseNetworkProbeIntervalMinutes('abc')).toBeNull();
  });
});

describe('network probe target aggregation helpers', () => {
  it('derives aggregated target outcomes from source count, freshness, and packet loss', () => {
    expect(probeTargetOutcome({ sourceCount: 2, stale: false, packetLossPercent: 0 })).toBe('ok');
    expect(probeTargetOutcome({ sourceCount: 2, stale: false, packetLossPercent: 1.7 })).toBe('degraded');
    expect(probeTargetOutcome({ sourceCount: 2, stale: true, packetLossPercent: 0 })).toBe('stale');
    expect(probeTargetOutcome({ sourceCount: 0, stale: true, packetLossPercent: null })).toBe('noData');

    expect(probeAggregateOutcomeTone('ok')).toBe('success');
    expect(probeAggregateOutcomeTone('degraded')).toBe('warning');
    expect(probeAggregateOutcomeTone('stale')).toBe('warning');
    expect(probeAggregateOutcomeTone('noData')).toBe('neutral');
    expect(probeAggregateOutcomeStatusKey('ok')).toBe('serversPage.probe.status.ok');
    expect(probeAggregateOutcomeStatusKey('noData')).toBe('serversPage.probe.status.noData');
  });

  it('maps aggregated 24h history into chart series; zero latency breaks the line', () => {
    const historyPoints = [
      { time: '2026-11-17T10:00:00.000Z', latencyMs: 42, packetLossPercent: 0 },
      { time: '2026-11-17T10:30:00.000Z', latencyMs: 0, packetLossPercent: 100 },
      { time: '', latencyMs: 30, packetLossPercent: 0 },
    ];
    const latency = probeHistorySeries(historyPoints, 'latency', 'latency', 'Latency');
    const loss = probeHistorySeries(historyPoints, 'packetLoss', 'loss', 'Packet loss');
    expect(latency.points).toEqual([
      { time: '2026-11-17T10:00:00.000Z', value: 42 },
      { time: '2026-11-17T10:30:00.000Z', value: null },
    ]);
    expect(loss.points).toEqual([
      { time: '2026-11-17T10:00:00.000Z', value: 0 },
      { time: '2026-11-17T10:30:00.000Z', value: 100 },
    ]);
    expect(probeHistorySeries(undefined, 'latency', 'latency', 'Latency').points).toEqual([]);
  });
});
