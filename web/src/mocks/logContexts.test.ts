import { describe, expect, it } from 'vitest';
import { mockLogContextsRoute } from './logContexts';

async function read(path: string) {
  return (await mockLogContextsRoute(new URL(`http://panel.test/api/v1/log-contexts${path}`), 'GET')!.json()).data;
}

describe('log-contexts mock contract', () => {
  it('lists user intents and process streams separately', async () => {
    const user = await read('?origin=user');
    expect(user.items.every((item: { origin: string }) => item.origin === 'user')).toBe(true);
    const process = await read('?origin=process');
    expect(process.items.every((item: { origin: string }) => item.origin === 'process')).toBe(true);
  });

  it('loads warning and error lines for a stream', async () => {
    const lines = await read('/stream-deploy/lines?level=warning,error');
    expect(lines.items.map((item: { level: string }) => item.level)).toEqual(['error', 'warning']);
  });

  it('does not offer mutation routes', () => {
    expect(mockLogContextsRoute(new URL('http://panel.test/api/v1/log-contexts'), 'DELETE')!.status).toBe(405);
  });
});
