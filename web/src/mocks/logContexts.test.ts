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

  it('clears events with exact confirmation', async () => {
    expect(mockEventsRoute(new URL('http://panel.test/api/v1/events/clear'), 'POST', { confirmation: 'WRONG' })!.status).toBe(422);
    const cleared = await mockEventsRoute(new URL('http://panel.test/api/v1/events/clear'), 'POST', { confirmation: 'CLEAR EVENTS AND LOGS' })!.json();
    expect(cleared.data.cleared).toBe(true);
    const page = await read('?limit=20');
    expect(page.items).toEqual([]);
  });
});
