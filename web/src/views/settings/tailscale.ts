import type { RuntimeSettings, RuntimeTailscaleContainerState } from '@/types/settings';

/** ACL tag 的本地校验规则，与后端接受的 `tag:<name>` 形态一致。 */
export const tailscaleTagPattern = /^tag:[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

export interface TailscaleTagParseResult {
  /** 去重后的合法 tag 列表，直接作为 PUT 的完整替换列表。 */
  tags: string[];
  /** 本地校验失败的原文，用于字段旁的就地提示。 */
  invalid: string[];
}

/** 逗号或空白分隔都接受；非法项保留原文以便提示，不做静默丢弃。 */
export function parseTailscaleTags(raw: string): TailscaleTagParseResult {
  const tags: string[] = [];
  const invalid: string[] = [];
  raw.split(/[\s,]+/).forEach((entry) => {
    const value = entry.trim();
    if (!value) return;
    if (!tailscaleTagPattern.test(value)) {
      invalid.push(value);
      return;
    }
    if (!tags.includes(value)) tags.push(value);
  });
  return { tags, invalid };
}

export function formatTailscaleTags(tags: string[]): string {
  return tags.join(', ');
}

/**
 * 表单初值。认证密钥是只写字段：响应只带 authKeyConfigured，密钥本身永不返回，
 * 因此这里固定以空值开始，任何情况下都不把服务端返回的内容带进输入框。
 */
export function tailscaleFormState(settings: RuntimeSettings): { tailscaleAuthKey: string; tailscaleTags: string } {
  return { tailscaleAuthKey: '', tailscaleTags: formatTailscaleTags(settings.tailscale.tags) };
}

/** 容器状态语义色：不可用为中性，出错为危险，未登录为警告，运行且已登录为成功。 */
export function tailscaleContainerTone(container: RuntimeTailscaleContainerState | null | undefined): 'neutral' | 'success' | 'warning' | 'danger' {
  if (!container || !container.available) return 'neutral';
  if (container.lastError) return 'danger';
  if (container.running && container.loggedIn) return 'success';
  if (container.running) return 'warning';
  return 'neutral';
}
