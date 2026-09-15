import { describe, expect, it } from 'vitest';
import { mockActivityRoute } from './activity';
async function read(path: string) { return (await mockActivityRoute(new URL(`http://panel.test/api/v1/activity/${path}`), 'GET')!.json()).data; }
describe('unified activity mock contract', () => {
  it('reads more than 200 events through snapshot cursors without duplicates', async () => {
    const ids: string[] = [];
    let cursor = '';
    do {
      const result = await read(`events?operationId=op-demo-deploy&limit=100${cursor ? `&cursor=${cursor}&snapshotSeq=246` : ''}`);
      ids.push(...result.items.map((item: { eventId: string }) => item.eventId));
      cursor = result.nextCursor;
    } while (cursor);
    expect(ids).toHaveLength(245);
    expect(new Set(ids).size).toBe(245);
  });
  it('preserves failure at its old snapshot while showing later recovery', async () => {
    const old = await read('operations/op-demo-deploy?snapshotSeq=240');
    const current = await read('operations/op-demo-deploy');
    expect(old.operation.result).toBe('partial');
    expect(current.operation.result).toBe('succeeded');
    expect(current.operation.hadError).toBe(true);
  });
  it('includes independent system events and surrounding error context', async () => {
    const standalone = await read('events/evt-demo-246');
    expect(standalone.operationId).toBeUndefined();
    const context = await read('events/evt-demo-239/context?before=2&after=2');
    expect(context.items.map((event: { seq: number }) => event.seq)).toEqual([237, 238, 239, 240, 241]);
  });
  it('does not offer history mutation routes', () => {
    expect(mockActivityRoute(new URL('http://panel.test/api/v1/activity/events/evt-demo-1'), 'DELETE')!.status).toBe(405);
  });
});
