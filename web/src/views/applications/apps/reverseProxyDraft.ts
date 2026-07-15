import type { ApplicationReverseProxyRuleDto } from '@/types/api';

export function cloneReverseProxyRule(rule: ApplicationReverseProxyRuleDto): ApplicationReverseProxyRuleDto {
  return {
    domain: rule.domain,
    targetType: rule.targetType === 'container' ? 'container' : 'local',
    targetPort: rule.targetPort,
    originServerIds: [...(rule.originServerIds ?? [])],
    anyAccess: {
      enabled: Boolean(rule.anyAccess?.enabled),
      strategy: rule.anyAccess?.strategy || 'round_robin',
      primaryOriginServerId: rule.anyAccess?.primaryOriginServerId || '',
    },
    paths: rule.paths?.map((path) => ({
      path: path.path,
      webSocket: path.webSocket,
      options: {
        gzipMode: path.options?.gzipMode || 'inherit',
        clientMaxBodySizeMb: path.options?.clientMaxBodySizeMb || 0,
        connectTimeoutSeconds: path.options?.connectTimeoutSeconds || 0,
        readTimeoutSeconds: path.options?.readTimeoutSeconds || 0,
        sendTimeoutSeconds: path.options?.sendTimeoutSeconds || 0,
        bufferingMode: path.options?.bufferingMode || 'inherit',
        webSocketMode: path.options?.webSocketMode || (path.webSocket ? 'on' : 'off'),
        requestHeaders: (path.options?.requestHeaders ?? []).map((header) => ({ ...header })),
        responseHeaders: (path.options?.responseHeaders ?? []).map((header) => ({ ...header })),
      },
    })) ?? [{ path: '/', webSocket: false, options: { gzipMode: 'inherit', bufferingMode: 'inherit', webSocketMode: 'off', requestHeaders: [], responseHeaders: [] } }],
  };
}
