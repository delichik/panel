import { describe, expect, it } from 'vitest';
import { createServer, deleteServer, MockServerProxyError, mockServers, probeServer, updateServer } from './servers';
import type { ServerSaveInput } from '@/types/servers';

function input(overrides: Partial<ServerSaveInput> = {}): ServerSaveInput {
  return {
    name: 'proxy-lab',
    ipv4: '203.0.113.80',
    ipv6: '',
    port: 22,
    sshUsername: '',
    credentialId: 'cred-root-key',
    dockerHost: 'unix:///var/run/docker.sock',
    proxyMode: 'direct',
    tunnelServerId: '',
    socks5Address: '',
    socks5Username: '',
    socks5Password: '',
    tailscaleEnabled: false,
    tailscalePreferAgent: false,
    tailscalePreferInterconnect: false,
    traits: {},
    variables: {},
    notes: '',
    ...overrides,
  };
}

function expectProxyError(fn: () => unknown, code: string) {
  try {
    fn();
  } catch (err) {
    expect(err).toBeInstanceOf(MockServerProxyError);
    expect((err as MockServerProxyError).code).toBe(code);
    return;
  }
  throw new Error(`expected ${code} to be thrown`);
}

describe('servers mock proxy behavior', () => {
  it('seeds derived proxy state without exposing passwords', () => {
    const tunnelled = mockServers.find((item) => item.id === 'srv-api-hkg-02')!;
    expect(tunnelled.proxyMode).toBe('ssh_tunnel');
    expect(tunnelled.tunnelServerName).toBe('api-hkg-01');
    expect(tunnelled.proxyInvalid).toBe(false);

    const dangling = mockServers.find((item) => item.id === 'srv-media-syd')!;
    expect(dangling.proxyInvalid).toBe(true);
    expect(dangling.proxyError).toBe('tunnel_missing');

    const socks5 = mockServers.find((item) => item.id === 'srv-batch-iad')!;
    expect(socks5.socks5PasswordSet).toBe(true);
    expect((socks5 as unknown as Record<string, unknown>).socks5Password).toBeUndefined();
  });

  it('stores socks5 config, keeps a blank edit password, and clears it on direct', () => {
    const created = createServer(input({ name: 'socks5-new', proxyMode: 'socks5', socks5Address: '127.0.0.1:1080', socks5Username: 'ops', socks5Password: 'top-secret' }));
    expect(created.socks5Address).toBe('127.0.0.1:1080');
    expect(created.socks5Username).toBe('ops');
    expect(created.socks5PasswordSet).toBe(true);

    const kept = updateServer(created.id, input({ name: 'socks5-new', proxyMode: 'socks5', socks5Address: '127.0.0.1:1081', socks5Username: 'ops', socks5Password: '' }))!;
    expect(kept.socks5Address).toBe('127.0.0.1:1081');
    expect(kept.socks5PasswordSet).toBe(true);

    const cleared = updateServer(created.id, input({ name: 'socks5-new' }))!;
    expect(cleared.proxyMode).toBe('direct');
    expect(cleared.socks5Address).toBe('');
    expect(cleared.socks5PasswordSet).toBe(false);
  });

  it('keeps dependent servers but derives tunnel_missing after the tunnel server is deleted', () => {
    const tunnel = createServer(input({ name: 'tunnel-1', ipv4: '203.0.113.81' }));
    const dependent = createServer(input({ name: 'dependent-1', ipv4: '203.0.113.82', proxyMode: 'ssh_tunnel', tunnelServerId: tunnel.id }));
    expect(dependent.tunnelServerName).toBe('tunnel-1');
    expect(dependent.proxyInvalid).toBe(false);

    expect(deleteServer(tunnel.id)).toBe(true);
    const orphan = mockServers.find((item) => item.id === dependent.id)!;
    expect(orphan.tunnelServerId).toBe(tunnel.id);
    expect(orphan.tunnelServerName).toBe('');
    expect(orphan.proxyInvalid).toBe(true);
    expect(orphan.proxyError).toBe('tunnel_missing');
  });

  it('rejects invalid proxy saves with contract error codes', () => {
    const tunnel = createServer(input({ name: 'tunnel-2', ipv4: '203.0.113.83' }));
    expectProxyError(() => createServer(input({ name: 'bad-mode', proxyMode: 'socks' as never })), 'server_proxy_mode_invalid');
    expectProxyError(() => createServer(input({ name: 'no-tunnel', proxyMode: 'ssh_tunnel' })), 'server_proxy_tunnel_required');
    expectProxyError(() => createServer(input({ name: 'missing-tunnel', proxyMode: 'ssh_tunnel', tunnelServerId: 'srv-does-not-exist' })), 'server_proxy_tunnel_missing');
    expectProxyError(() => updateServer(tunnel.id, input({ name: 'tunnel-2', proxyMode: 'ssh_tunnel', tunnelServerId: tunnel.id })), 'server_proxy_tunnel_self');
    expectProxyError(() => createServer(input({ name: 'no-address', proxyMode: 'socks5' })), 'server_proxy_socks5_address_required');
    expectProxyError(() => createServer(input({ name: 'bad-address', proxyMode: 'socks5', socks5Address: '127.0.0.1' })), 'server_proxy_socks5_address_invalid');
    expectProxyError(() => createServer(input({ name: 'no-user', proxyMode: 'socks5', socks5Address: '127.0.0.1:1080', socks5Password: 'secret' })), 'server_proxy_socks5_username_required');

    const cycleA = createServer(input({ name: 'cycle-a', ipv4: '203.0.113.84' }));
    const cycleB = createServer(input({ name: 'cycle-b', ipv4: '203.0.113.85', proxyMode: 'ssh_tunnel', tunnelServerId: cycleA.id }));
    expectProxyError(() => updateServer(cycleA.id, input({ name: 'cycle-a', proxyMode: 'ssh_tunnel', tunnelServerId: cycleB.id })), 'server_proxy_cycle');
  });

  it('probes with the proxy draft and reuses a stored password through serverId', () => {
    const created = createServer(input({ name: 'probe-socks5', proxyMode: 'socks5', socks5Address: '127.0.0.1:1080', socks5Username: 'ops', socks5Password: 'stored-secret' }));
    expect(probeServer({
      ipv4: '203.0.113.86',
      ipv6: '',
      port: 22,
      sshUsername: '',
      credentialId: 'cred-root-key',
      proxyMode: 'socks5',
      socks5Address: '127.0.0.1:1080',
      socks5Username: 'ops',
      socks5Password: '',
      serverId: created.id,
    }).reachable).toBe(true);
    expectProxyError(() => probeServer({
      ipv4: '203.0.113.86',
      ipv6: '',
      port: 22,
      sshUsername: '',
      credentialId: 'cred-root-key',
      proxyMode: 'socks5',
      socks5Address: 'bad',
    }), 'server_proxy_socks5_address_invalid');
  });
});
