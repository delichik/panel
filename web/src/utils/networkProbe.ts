import type { NetworkProbeSettings } from '@/types/networkProbe';

/**
 * 网络探测节拍参数契约：前端表单、Mock 与测试共用同一组默认值/区间/错误码，
 * 避免界面提示、Mock 校验与后端契约各自漂移。
 */
export type NetworkProbeSettingField = keyof NetworkProbeSettings;

export interface NetworkProbeSettingRule {
  min: number;
  max: number;
  step: number;
  defaultValue: number;
  /** 越界/非法值对应的稳定错误码。 */
  errorCode: string;
}

export const networkProbeSettingRules: Record<NetworkProbeSettingField, NetworkProbeSettingRule> = {
  intervalSeconds: { min: 10, max: 3600, step: 1, defaultValue: 60, errorCode: 'network_probe_interval_invalid' },
  pingCount: { min: 1, max: 10, step: 1, defaultValue: 3, errorCode: 'network_probe_ping_count_invalid' },
  pingTimeoutSeconds: { min: 1, max: 10, step: 1, defaultValue: 2, errorCode: 'network_probe_ping_timeout_invalid' },
};

export const networkProbeSettingFields = Object.keys(networkProbeSettingRules) as NetworkProbeSettingField[];

/** 合法取值必须是区间内的整数；字符串、NaN、小数一律非法。 */
export function isValidNetworkProbeSetting(field: NetworkProbeSettingField, value: unknown): boolean {
  const rule = networkProbeSettingRules[field];
  return typeof value === 'number'
    && Number.isInteger(value)
    && value >= rule.min
    && value <= rule.max;
}

/**
 * 缺省（undefined/null）表示旧 payload，交由服务端补默认值，不算错误；
 * 显式给出但非法时返回稳定错误码。
 */
export function networkProbeSettingError(field: NetworkProbeSettingField, value: unknown): string | null {
  if (value === undefined || value === null || value === '') return null;
  return isValidNetworkProbeSetting(field, value) ? null : networkProbeSettingRules[field].errorCode;
}

/** 旧 payload 缺省参数按默认值补齐；非法值也回退默认值（调用方应先做校验/报错）。 */
export function normalizeNetworkProbeSettings(input: Partial<Record<NetworkProbeSettingField, unknown>>): NetworkProbeSettings {
  const settings = {} as NetworkProbeSettings;
  for (const field of networkProbeSettingFields) {
    const value = input[field];
    settings[field] = isValidNetworkProbeSetting(field, value)
      ? (value as number)
      : networkProbeSettingRules[field].defaultValue;
  }
  return settings;
}

/** 保存输入校验：返回首个非法字段及错误码；缺省字段视为合法（服务端补默认值）。 */
export function networkProbeSettingsError(
  input: Partial<Record<NetworkProbeSettingField, unknown>>,
): { field: NetworkProbeSettingField; code: string } | null {
  for (const field of networkProbeSettingFields) {
    const code = networkProbeSettingError(field, input[field]);
    if (code) return { field, code };
  }
  return null;
}

/** 表单原始输入（字符串）转数字；空串与小数返回 null，由调用方按非法处理。 */
export function parseNetworkProbeSetting(raw: string): number | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const value = Number(trimmed);
  return Number.isInteger(value) ? value : null;
}
