import { describe, expect, it } from 'vitest';
import { agentTone, connectionHost, connectionSignature, credentialReferences, hasBlockingPairIssues, isLegacyResourceTab, mergeServerCandidates, normalizeServerDetailTab, normalizeServerResourceSubtab, parsePairs, proxyErrorKey, proxyListHintKey, proxyModeLabelKey, proxySavePayload, proxyShortLabelKey, proxyTunnelDisplayName, reportDisconnectCount1h, reportStabilityGrade, reportStabilityStatusKey, reportStabilityTone, reportStreamStatus, reportStreamStatusKey, serverDetailTabs, serverProxyMode, serverSummaryKind, serverSummaryStatusKey, serverSummaryTone, shouldShowListReportStability, tailscaleObservedStatus, tailscalePreferences, tailscaleStatusKey, tailscaleTone, tailscaleTrait, validateProbeInput, validateProxyInput, validateServerInput } from './model';
import type { ServerDto, ServerProxyMode, ServerSaveInput } from '@/types/servers';

const server: ServerDto = {
  id: 'srv-1',
  name: 'edge',
  host: '10.0.0.1',
  port: 22,
  credentialId: 'cred-1',
  reachable: true,
  tailscaleEnabled: false,
  tailscalePreferAgent: false,
  tailscalePreferInterconnect: false,
  proxyMode: 'direct',
  tunnelServerId: '',
  tunnelServerName: '',
  socks5Address: '',
  socks5Username: '',
  proxyInvalid: false,
  proxyError: '',
  sudo: { passwordless: true },
  privilege: { privileged: true },
  traits: { 'agent.enabled': 'true', 'agent.status': 'compatible', 'sys.ufw_supported': 'true', 'sys.ufw_installed': 'false' },
};

const proxy: { proxyMode: ServerProxyMode; tunnelServerId: string; socks5Address: string; socks5Username: string; socks5Password: string } = {
  proxyMode: 'direct',
  tunnelServerId: '',
  socks5Address: '',
  socks5Username: '',
  socks5Password: '',
};

describe('server model', () => {
  it('keeps primary detail tabs compact and aliases legacy docker tabs to resources', () => {
    expect(serverDetailTabs).toEqual(['overview', 'metrics', 'agent', 'tailscale', 'resources', 'packages', 'firewall']);
    expect(normalizeServerDetailTab('resources')).toBe('resources');
    expect(normalizeServerDetailTab('containers')).toBe('resources');
    expect(normalizeServerDetailTab('images')).toBe('resources');
    expect(normalizeServerDetailTab('networks')).toBe('resources');
    expect(normalizeServerDetailTab('volumes')).toBe('resources');
    expect(normalizeServerDetailTab('unknown')).toBe('overview');
    expect(isLegacyResourceTab('containers')).toBe(true);
    expect(normalizeServerResourceSubtab(undefined, 'images')).toBe('images');
    expect(normalizeServerResourceSubtab('volumes')).toBe('volumes');
    expect(normalizeServerResourceSubtab(undefined)).toBe('containers');
  });

  it('maps agent capabilities from real server fields', () => {
    expect(agentTone(server)).toBe('success');
  });

  it('maps report stream stability without inventing excellent for missing data', () => {
    expect(reportStreamStatus(server)).toBe('');
    expect(reportStabilityGrade(server)).toBe('');
    expect(reportStabilityStatusKey(server)).toBe('serversPage.reportStability.unknown');
    expect(reportStreamStatusKey(server)).toBe('serversPage.reportStream.unknown');
    expect(shouldShowListReportStability(server)).toBe(false);

    const linked = {
      ...server,
      traits: {
        ...server.traits,
        'agent.report.status': 'connected',
        'agent.report.stability': 'good',
        'agent.report.disconnect_count_1h': '2',
      },
    };
    expect(reportStreamStatus(linked)).toBe('connected');
    expect(reportStabilityGrade(linked)).toBe('good');
    expect(reportDisconnectCount1h(linked)).toBe(2);
    expect(reportStabilityTone(linked)).toBe('info');
    expect(shouldShowListReportStability(linked)).toBe(true);

    const excellent = { ...linked, traits: { ...linked.traits, 'agent.report.stability': 'excellent', 'agent.report.disconnect_count_1h': '0' } };
    expect(shouldShowListReportStability(excellent)).toBe(false);
    expect(reportStabilityTone({ ...linked, traits: { ...linked.traits, 'agent.report.stability': 'critical' } })).toBe('danger');
  });

  it('summarizes one user-facing server status with agent problems outranking mere reachability', () => {
    expect(serverSummaryKind(server)).toBe('ready');
    expect(serverSummaryStatusKey(server)).toBe('serversPage.summary.ready');
    expect(serverSummaryTone(server)).toBe('success');

    expect(serverSummaryKind({ ...server, reachable: false })).toBe('unreachable');
    expect(serverSummaryTone({ ...server, reachable: false })).toBe('danger');

    expect(serverSummaryKind({ ...server, hostKeyMismatch: true })).toBe('hostKeyMismatch');

    const blocked = { ...server, traits: { ...server.traits, 'agent.status': 'undeployable' } };
    expect(serverSummaryKind(blocked)).toBe('agentBlocked');
    expect(serverSummaryTone(blocked)).toBe('danger');
    // Reachable alone must not read as healthy when Agent cannot run.
    expect(serverSummaryKind(blocked)).not.toBe('ready');

    expect(serverSummaryKind({ ...server, traits: { ...server.traits, 'agent.status': 'unavailable' } })).toBe('agentUnavailable');
    expect(serverSummaryKind({ ...server, traits: { ...server.traits, 'agent.status': 'incompatible' } })).toBe('agentMismatch');
    expect(serverSummaryKind({ ...server, traits: { 'agent.enabled': 'false' } })).toBe('agentNotInstalled');
    expect(serverSummaryTone({ ...server, traits: { 'agent.enabled': 'false' } })).toBe('warning');
  });

  it('computes credential references from server inventory', () => {
    expect(credentialReferences('cred-1', [server])).toEqual([{ id: 'srv-1', name: 'edge', host: '10.0.0.1' }]);
  });

  it('validates server forms before API calls', () => {
    expect(validateServerInput({ ...proxy, name: '', ipv4: '', ipv6: '', port: 70000, credentialId: '', sshUsername: '', dockerHost: '', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false, traits: {}, variables: {}, notes: '' })).toMatchObject({
      name: 'serversPage.validationName',
      ipv4: 'serversPage.validationAddressRequired',
      port: 'serversPage.validationPort',
      credentialId: 'serversPage.validationCredential',
      dockerHost: 'serversPage.validationDocker',
    });
  });

  it('validates ipv4 and ipv6 literals and derives the connection host', () => {
    const base = { ...proxy, name: 'edge', sshUsername: '', traits: {}, variables: {}, notes: '', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false };
    expect(validateServerInput({ ...base, ipv4: '999.0.0.1', ipv6: '', port: 22, credentialId: 'cred-1', dockerHost: 'unix:///var/run/docker.sock' }).ipv4).toBe('serversPage.validationIpv4');
    expect(validateServerInput({ ...base, ipv4: '', ipv6: '2001:db8::1', port: 22, credentialId: 'cred-1', dockerHost: 'unix:///var/run/docker.sock' })).toEqual({});
    expect(connectionHost({ ipv4: '203.0.113.5', ipv6: '2001:db8::5' })).toBe('203.0.113.5');
    expect(connectionHost({ ipv4: '', ipv6: '2001:db8::5' })).toBe('2001:db8::5');
  });

  it('parses key=value lines and reports blocking issues instead of silently dropping them', () => {
    const result = parsePairs('  A=1 \n\nB = 2\nbroken\n=3\nB=4');
    expect(result.pairs).toEqual({ A: '1', B: '4' });
    expect(result.issues).toEqual([
      { line: 4, kind: 'missing_separator' },
      { line: 5, kind: 'empty_key' },
      { line: 6, kind: 'duplicate_key', key: 'B' },
    ]);
    expect(hasBlockingPairIssues(result.issues)).toBe(true);
    expect(hasBlockingPairIssues([{ line: 1, kind: 'duplicate_key', key: 'B' }])).toBe(false);
    expect(hasBlockingPairIssues([])).toBe(false);
  });

  it('keeps probe gating limited to connection fields while creation stays gated on the full form', () => {
    const base = { ipv4: '203.0.113.10', ipv6: '', port: 22, sshUsername: '', credentialId: 'cred-1' };
    expect(validateProbeInput(base)).toEqual({});
    expect(validateProbeInput({ ...base, ipv4: '', ipv6: '2001:db8::1' })).toEqual({});
    expect(validateProbeInput({ ...base, ipv4: '999.1.1.1' })).toEqual({ ipv4: 'serversPage.validationIpv4' });
    expect(validateProbeInput({ ...base, ipv4: '', ipv6: '' })).toEqual({ ipv4: 'serversPage.validationAddressRequired' });
    expect(validateProbeInput({ ...base, credentialId: '' })).toEqual({ credentialId: 'serversPage.validationCredential' });
    expect(validateProbeInput({ ...base, port: 0 })).toEqual({ port: 'serversPage.validationPort' });
    // 名称与 Docker Host 错误不影响探测
    const full = { ...proxy, ...base, name: '', dockerHost: '', variables: {}, notes: '', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false };
    expect(Object.keys(validateServerInput(full))).toEqual(['name', 'dockerHost']);
  });

  it('marks probe results stale only when a probed connection field changes', () => {
    const base = { ipv4: '203.0.113.10', ipv6: '', port: 22, sshUsername: 'root', credentialId: 'cred-1' };
    const signature = connectionSignature(base);
    expect(connectionSignature({ ...base, ipv6: '  ' })).toBe(signature);
    expect(connectionSignature({ ...base, port: 2222 })).not.toBe(signature);
    expect(connectionSignature({ ...base, credentialId: 'cred-2' })).not.toBe(signature);
    expect(connectionSignature({ ...base, sshUsername: 'ops' })).not.toBe(signature);
    // 代理配置变化同样视为连接方式变化
    expect(connectionSignature({ ...base, proxyMode: 'ssh_tunnel', tunnelServerId: 'srv-2' })).not.toBe(signature);
    expect(connectionSignature({ ...base, proxyMode: 'socks5', socks5Address: '127.0.0.1:1080' })).not.toBe(signature);
    expect(connectionSignature({ ...base, socks5Password: 'secret' })).not.toBe(signature);
  });

  it('validates the proxy draft locally: tunnel required/self, socks5 host:port, password needs username', () => {
    expect(validateProxyInput(proxy)).toEqual({});
    expect(validateProxyInput({ ...proxy, proxyMode: 'ssh_tunnel' })).toEqual({ tunnelServerId: 'serversPage.proxy.validationTunnelRequired' });
    expect(validateProxyInput({ ...proxy, proxyMode: 'ssh_tunnel', tunnelServerId: 'srv-1' }, { selfId: 'srv-1' })).toEqual({ tunnelServerId: 'serversPage.proxy.validationTunnelSelf' });
    expect(validateProxyInput({ ...proxy, proxyMode: 'ssh_tunnel', tunnelServerId: 'srv-2' }, { selfId: 'srv-1' })).toEqual({});
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5' })).toEqual({ socks5Address: 'serversPage.proxy.validationSocks5AddressRequired' });
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5', socks5Address: '127.0.0.1' })).toEqual({ socks5Address: 'serversPage.proxy.validationSocks5AddressInvalid' });
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5', socks5Address: ':1080' })).toEqual({ socks5Address: 'serversPage.proxy.validationSocks5AddressInvalid' });
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5', socks5Address: '127.0.0.1:1080' })).toEqual({});
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5', socks5Address: '[::1]:1080' })).toEqual({});
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5', socks5Address: '127.0.0.1:1080', socks5Password: 'secret' })).toEqual({ socks5Username: 'serversPage.proxy.validationSocks5UsernameRequired' });
    expect(validateProxyInput({ ...proxy, proxyMode: 'socks5', socks5Address: '127.0.0.1:1080', socks5Username: 'ops', socks5Password: 'secret' })).toEqual({});
  });

  it('gates saving and probing on the proxy draft while keeping proxy fields out of other modes', () => {
    const draft: Pick<ServerSaveInput, 'proxyMode' | 'tunnelServerId' | 'socks5Address' | 'socks5Username' | 'socks5Password'> = {
      ...proxy,
      proxyMode: 'ssh_tunnel',
      tunnelServerId: 'srv-1',
    };
    expect(validateServerInput({
      ...draft, name: 'edge', ipv4: '10.0.0.1', ipv6: '', port: 22, credentialId: 'cred-1', sshUsername: '',
      dockerHost: 'unix:///var/run/docker.sock', tailscaleEnabled: false, tailscalePreferAgent: false, tailscalePreferInterconnect: false,
      variables: {}, notes: '',
    }, { selfId: 'srv-1' }).tunnelServerId).toBe('serversPage.proxy.validationTunnelSelf');
    const probeBase = { ipv4: '203.0.113.10', ipv6: '', port: 22, sshUsername: '', credentialId: 'cred-1' };
    expect(validateProbeInput({ ...probeBase, ...draft, serverId: 'srv-1' }).tunnelServerId).toBe('serversPage.proxy.validationTunnelSelf');
    expect(validateProbeInput({ ...probeBase, proxyMode: 'socks5', socks5Address: 'bad' }).socks5Address).toBe('serversPage.proxy.validationSocks5AddressInvalid');
    expect(proxySavePayload({ ...draft, tunnelServerId: ' srv-2 ' })).toEqual({ proxyMode: 'ssh_tunnel', tunnelServerId: 'srv-2', socks5Address: '', socks5Username: '', socks5Password: '' });
    expect(proxySavePayload({ ...proxy, proxyMode: 'socks5', socks5Address: ' 127.0.0.1:1080 ', socks5Username: ' ops ', socks5Password: 'secret', tunnelServerId: 'srv-2' })).toEqual({ proxyMode: 'socks5', tunnelServerId: '', socks5Address: '127.0.0.1:1080', socks5Username: 'ops', socks5Password: 'secret' });
    expect(proxySavePayload({ ...proxy, proxyMode: 'direct', socks5Address: '127.0.0.1:1080', tunnelServerId: 'srv-2' })).toEqual({ proxyMode: 'direct', tunnelServerId: '', socks5Address: '', socks5Username: '', socks5Password: '' });
  });

  it('merges page and full server lists for tunnel candidates and excludes the edited server', () => {
    const pageServer = { ...server, id: 'srv-1', name: 'page-edge' };
    const fetchedOnly = { ...server, id: 'srv-60', name: 'beyond-page' };
    const fetchedPageDuplicate = { ...server, id: 'srv-1', name: 'stale-name' };
    expect(mergeServerCandidates([pageServer], [fetchedPageDuplicate, fetchedOnly])).toEqual([pageServer, fetchedOnly]);
    expect(mergeServerCandidates([pageServer], [fetchedOnly], 'srv-1')).toEqual([fetchedOnly]);
    expect(mergeServerCandidates([], [], 'srv-1')).toEqual([]);
  });

  it('derives proxy labels, tunnel name fallbacks, and stable error keys', () => {
    expect(serverProxyMode(server)).toBe('direct');
    expect(proxyModeLabelKey('direct')).toBe('serversPage.proxy.modeDirect');
    expect(proxyModeLabelKey('ssh_tunnel')).toBe('serversPage.proxy.modeSshTunnel');
    expect(proxyModeLabelKey('socks5')).toBe('serversPage.proxy.modeSocks5');
    expect(proxyModeLabelKey('unknown')).toBe('serversPage.proxy.modeDirect');
    expect(proxyShortLabelKey(server)).toBe('');
    expect(proxyShortLabelKey({ proxyMode: 'ssh_tunnel' })).toBe('serversPage.proxy.short.ssh_tunnel');
    expect(proxyShortLabelKey({ proxyMode: 'socks5' })).toBe('serversPage.proxy.short.socks5');
    expect(proxyTunnelDisplayName({ tunnelServerId: 'srv-2', tunnelServerName: 'bastion' })).toBe('bastion');
    expect(proxyTunnelDisplayName({ tunnelServerId: 'srv-2', tunnelServerName: '' })).toBe('srv-2');
    expect(proxyTunnelDisplayName({ tunnelServerId: '', tunnelServerName: '' })).toBe('');
    expect(proxyErrorKey({ proxyError: '' })).toBe('serversPage.proxy.error.unknown');
    expect(proxyErrorKey({ proxyError: 'tunnel_missing' })).toBe('serversPage.proxy.error.tunnel_missing');
    expect(proxyErrorKey({ proxyError: 'cycle' })).toBe('serversPage.proxy.error.cycle');
    expect(proxyErrorKey({ proxyError: 'chain_too_deep' })).toBe('serversPage.proxy.error.chain_too_deep');
    expect(proxyErrorKey({ proxyError: 'socks5_address_missing' })).toBe('serversPage.proxy.error.socks5_address_missing');
    expect(proxyErrorKey({ proxyError: 'socks5_address_invalid' })).toBe('serversPage.proxy.error.socks5_address_invalid');
    expect(proxyListHintKey({ proxyInvalid: false, proxyError: '' })).toBe('serversPage.proxy.listHint');
    expect(proxyListHintKey({ proxyInvalid: true, proxyError: 'tunnel_missing' })).toBe('serversPage.proxy.error.tunnel_missing');
  });

  it('forces the tailscale preferences off when the node does not join the tailnet', () => {
    expect(tailscalePreferences({ tailscaleEnabled: false, tailscalePreferAgent: true, tailscalePreferInterconnect: true })).toEqual({ tailscalePreferAgent: false, tailscalePreferInterconnect: false });
    expect(tailscalePreferences({ tailscaleEnabled: true, tailscalePreferAgent: true, tailscalePreferInterconnect: false })).toEqual({ tailscalePreferAgent: true, tailscalePreferInterconnect: false });
  });

  it('reads the observed tailscale state from traits only and never invents one', () => {
    expect(tailscaleObservedStatus(server)).toBe('');
    expect(tailscaleStatusKey(server)).toBe('serversPage.tailscale.noStatus');
    expect(tailscaleTone(server)).toBe('neutral');

    const running = { ...server, tailscaleEnabled: true, traits: { ...server.traits, 'tailscale.status': 'running', 'tailscale.ipv4': '100.64.0.11', 'tailscale.lastError': '', 'tailscale.updatedAt': '2026-08-01T07:52:00.000Z' } };
    expect(tailscaleObservedStatus(running)).toBe('running');
    expect(tailscaleStatusKey(running)).toBe('serversPage.tailscale.status.running');
    expect(tailscaleTone(running)).toBe('success');
    expect(tailscaleTrait(running, 'ipv4')).toBe('100.64.0.11');
    expect(tailscaleTrait(running, 'lastError')).toBe('');

    const unknown = { ...server, tailscaleEnabled: true, traits: { ...server.traits, 'tailscale.status': 'reconciling' } };
    expect(tailscaleObservedStatus(unknown)).toBe('');
    expect(tailscaleTone(unknown)).toBe('neutral');
    expect(tailscaleTone({ ...server, tailscaleEnabled: true, traits: { 'tailscale.status': 'error' } })).toBe('danger');
    expect(tailscaleTone({ ...server, tailscaleEnabled: true, traits: { 'tailscale.status': 'installing' } })).toBe('warning');
    expect(tailscaleTone({ ...server, tailscaleEnabled: true, traits: { 'tailscale.status': 'unsupported' } })).toBe('info');
  });
});
