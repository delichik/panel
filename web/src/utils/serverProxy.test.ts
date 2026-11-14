import { describe, expect, it } from 'vitest';
import { deriveServerProxy, isSocks5Address, normalizeServerProxyMode, proxyChainSaveError } from './serverProxy';

describe('server proxy helpers', () => {
  it('normalizes unknown proxy modes to direct', () => {
    expect(normalizeServerProxyMode('direct')).toBe('direct');
    expect(normalizeServerProxyMode('ssh_tunnel')).toBe('ssh_tunnel');
    expect(normalizeServerProxyMode('socks5')).toBe('socks5');
    expect(normalizeServerProxyMode('')).toBe('direct');
    expect(normalizeServerProxyMode(undefined)).toBe('direct');
    expect(normalizeServerProxyMode('socks')).toBe('direct');
  });

  it('accepts only host:port SOCKS5 addresses', () => {
    expect(isSocks5Address('127.0.0.1:1080')).toBe(true);
    expect(isSocks5Address('proxy.internal:1080')).toBe(true);
    expect(isSocks5Address('[::1]:1080')).toBe(true);
    expect(isSocks5Address(' 127.0.0.1:1080 ')).toBe(true);
    expect(isSocks5Address('127.0.0.1')).toBe(false);
    expect(isSocks5Address(':1080')).toBe(false);
    expect(isSocks5Address('127.0.0.1:')).toBe(false);
    expect(isSocks5Address('127.0.0.1:0')).toBe(false);
    expect(isSocks5Address('127.0.0.1:65536')).toBe(false);
    expect(isSocks5Address('http://127.0.0.1:1080')).toBe(false);
    expect(isSocks5Address('user@host:1080')).toBe(false);
    expect(isSocks5Address('::1:1080')).toBe(false);
    expect(isSocks5Address('')).toBe(false);
  });

  it('derives nothing for direct servers and validates standalone socks5 addresses', () => {
    expect(deriveServerProxy({ id: 'a', proxyMode: 'direct', tunnelServerId: '', socks5Address: '' }, [])).toEqual({ tunnelServerName: '', proxyInvalid: false, proxyError: '' });
    expect(deriveServerProxy({ id: 'a', proxyMode: 'socks5', socks5Address: '' }, [])).toEqual({ tunnelServerName: '', proxyInvalid: true, proxyError: 'socks5_address_missing' });
    expect(deriveServerProxy({ id: 'a', proxyMode: 'socks5', socks5Address: 'bad' }, [])).toEqual({ tunnelServerName: '', proxyInvalid: true, proxyError: 'socks5_address_invalid' });
    expect(deriveServerProxy({ id: 'a', proxyMode: 'socks5', socks5Address: '127.0.0.1:1080' }, [])).toEqual({ tunnelServerName: '', proxyInvalid: false, proxyError: '' });
  });

  it('resolves tunnel names and reports missing tunnels (deleted tunnel server)', () => {
    const nodes = [
      { id: 'tunnel', name: 'bastion', proxyMode: 'direct' as const, tunnelServerId: '', socks5Address: '' },
      { id: 'target', name: 'target', proxyMode: 'ssh_tunnel' as const, tunnelServerId: 'tunnel', socks5Address: '' },
      { id: 'dangling', name: 'dangling', proxyMode: 'ssh_tunnel' as const, tunnelServerId: 'gone', socks5Address: '' },
    ];
    expect(deriveServerProxy(nodes[1]!, nodes)).toEqual({ tunnelServerName: 'bastion', proxyInvalid: false, proxyError: '' });
    expect(deriveServerProxy(nodes[2]!, nodes)).toEqual({ tunnelServerName: '', proxyInvalid: true, proxyError: 'tunnel_missing' });
  });

  it('detects cycles and chains deeper than eight hops', () => {
    const a = { id: 'a', name: 'a', proxyMode: 'ssh_tunnel' as const, tunnelServerId: 'b', socks5Address: '' };
    const b = { id: 'b', name: 'b', proxyMode: 'ssh_tunnel' as const, tunnelServerId: 'a', socks5Address: '' };
    expect(deriveServerProxy(a, [a, b]).proxyError).toBe('cycle');

    const leaf = { id: 'leaf', name: 'leaf', proxyMode: 'direct' as const, tunnelServerId: '', socks5Address: '' };
    // 8 层隧道仍合法；再加一层即超限。
    const eight = Array.from({ length: 8 }, (_, index) => ({
      id: `d${index}`,
      name: `d${index}`,
      proxyMode: 'ssh_tunnel' as const,
      tunnelServerId: index === 7 ? 'leaf' : `d${index + 1}`,
      socks5Address: '',
    }));
    expect(deriveServerProxy(eight[0]!, [...eight, leaf])).toEqual({ tunnelServerName: 'd1', proxyInvalid: false, proxyError: '' });
    const nine = [{ ...eight[0]!, id: 'e0', tunnelServerId: 'e1' }, leaf];
    for (let index = 1; index < 9; index += 1) {
      nine.splice(index, 0, {
        id: `e${index}`,
        name: `e${index}`,
        proxyMode: 'ssh_tunnel' as const,
        tunnelServerId: index === 8 ? 'leaf' : `e${index + 1}`,
        socks5Address: '',
      });
    }
    expect(deriveServerProxy(nine[0]!, nine).proxyError).toBe('chain_too_deep');
  });

  it('validates the candidate chain before saving, including self-reference and socks5 auth', () => {
    const nodes = [
      { id: 'a', name: 'a', proxyMode: 'direct' as const, tunnelServerId: '', socks5Address: '' },
      { id: 'b', name: 'b', proxyMode: 'ssh_tunnel' as const, tunnelServerId: 'a', socks5Address: '' },
    ];
    expect(proxyChainSaveError({ id: 'a', proxyMode: 'ssh_tunnel', tunnelServerId: 'b', socks5Address: '' }, nodes)).toBe('cycle');
    expect(proxyChainSaveError({ id: 'c', proxyMode: 'ssh_tunnel', tunnelServerId: 'gone', socks5Address: '' }, nodes)).toBe('tunnel_missing');
    expect(proxyChainSaveError({ id: 'c', proxyMode: 'socks5', tunnelServerId: '', socks5Address: '' }, nodes)).toBe('socks5_address_missing');
    expect(proxyChainSaveError({ id: 'c', proxyMode: 'socks5', tunnelServerId: '', socks5Address: 'bad' }, nodes)).toBe('socks5_address_invalid');
    expect(proxyChainSaveError({ id: 'c', proxyMode: 'socks5', tunnelServerId: '', socks5Address: '127.0.0.1:1080' }, nodes)).toBe('');
    expect(proxyChainSaveError({ id: 'c', proxyMode: 'direct', tunnelServerId: '', socks5Address: '' }, nodes)).toBe('');
  });
});
