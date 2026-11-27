import type { ImageRegistryScheme } from '@/types/settings';

/** 镜像仓库 endpoint 的默认值：空串表示官方仓库。 */
export const DEFAULT_IMAGE_REGISTRY_ENDPOINT = '';
/** 镜像仓库协议默认值。 */
export const DEFAULT_IMAGE_REGISTRY_SCHEME: ImageRegistryScheme = 'https';

const hostnameLabel = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const bracketedHost = /^(\[[^\]]+\])(?::(.*))?$/;

/** 协议只接受 http/https；未知值按默认 https 处理。 */
export function isImageRegistryScheme(value: unknown): value is ImageRegistryScheme {
  return value === 'https' || value === 'http';
}

export function normalizeImageRegistryScheme(value: unknown): ImageRegistryScheme {
  return isImageRegistryScheme(value) ? value : DEFAULT_IMAGE_REGISTRY_SCHEME;
}

function isIPv4(value: string): boolean {
  const parts = value.split('.');
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

function isValidHostname(value: string): boolean {
  if (value.startsWith('.') || value.endsWith('.') || value.includes('..')) return false;
  return value.split('.').every((label) => hostnameLabel.test(label));
}

function normalizeHost(rawHost: string): string | null {
  const host = rawHost.toLowerCase();
  if (!host) return null;

  // IPv6 必须写成 [addr]（可再带 :port），裸 IPv6 与端口分隔符无法区分。
  if (host.startsWith('[') || host.endsWith(']')) {
    if (!host.startsWith('[') || !host.endsWith(']')) return null;
    const inner = host.slice(1, -1);
    if (!inner.includes(':') || !/^[0-9a-f:]+$/i.test(inner)) return null;
    try {
      // URL 解析器负责 IPv6 语法校验（压缩格式、分组长度等）。
      new URL(`http://${host}/`);
      return host;
    } catch {
      return null;
    }
  }

  if (host.length > 253) return null;
  if (isIPv4(host)) return host;
  // 纯数字点分形式看起来像 IP，但不是合法 IPv4：不要把它当成主机名放行。
  if (/^\d+(?:\.\d+)*$/.test(host)) return null;
  return isValidHostname(host) ? host : null;
}

/** 端口：缺省返回 undefined，非法返回 null，合法返回十进制端口文本。 */
function normalizePort(rawPort: string | undefined): string | null | undefined {
  if (rawPort === undefined) return undefined;
  if (!/^\d+$/.test(rawPort)) return null;
  const port = Number(rawPort);
  if (!Number.isInteger(port) || port < 1 || port > 65535) return null;
  return String(port);
}

/**
 * Panel 镜像仓库 endpoint：空串表示官方仓库；非空必须是裸 `host[:port]`。
 * 拒绝 scheme/path/query/fragment/userinfo、空白、引号、反斜杠以及非法端口。
 * 合法值返回规范化文本（主机小写、端口去前导零），非法返回 null。
 */
export function normalizeImageRegistryEndpoint(raw: string): string | null {
  const value = raw.trim();
  if (!value) return DEFAULT_IMAGE_REGISTRY_ENDPOINT;
  if (/[\s"'`\\/?#@]/.test(value)) return null;

  let hostPart: string;
  let portPart: string | undefined;
  if (value.startsWith('[')) {
    const match = value.match(bracketedHost);
    if (!match) return null;
    hostPart = match[1]!;
    portPart = match[2];
  } else {
    const parts = value.split(':');
    if (parts.length > 2) return null;
    hostPart = parts[0] ?? '';
    portPart = parts[1];
  }

  const host = normalizeHost(hostPart);
  if (host === null) return null;
  const port = normalizePort(portPart);
  if (port === null) return null;
  return port === undefined ? host : `${host}:${port}`;
}
