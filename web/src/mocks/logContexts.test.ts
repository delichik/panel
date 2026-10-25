import { describe, expect, it } from 'vitest';
import { mockEventsRoute } from './events';

async function read(path: string) {
  return (await mockEventsRoute(new URL(`http://panel.test/api/v1/events${path}`), 'GET')!.json()).data;
}

describe('events mock contract', () => {
  it('lists and loads lines by event id', async () => {
    const page = await read('?limit=20');
    expect(page.items.length).toBeGreaterThan(0);
    expect(page.items[0].id).toBeTruthy();
    const lines = (await mockEventsRoute(new URL('http://panel.test/api/v1/events/stream-deploy/logs'), 'GET')!.json()).data;
    expect(lines.items.some((line: { msg?: string }) => line.msg?.includes('Deploying'))).toBe(true);
  });

  it('rejects non-GET', () => {
    expect(mockEventsRoute(new URL('http://panel.test/api/v1/events'), 'DELETE')!.status).toBe(405);
  });
});
