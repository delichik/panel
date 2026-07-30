import { describe, expect, it, vi } from 'vitest';
import { reloadFacilityTextWorkflow, saveFacilityTextWorkflow } from './facilityTextWorkflow';

describe('facility text parent workflow', () => {
  it('closes only after an asynchronous save succeeds', async () => {
    const state = { open: true, error: '', conflict: false };
    let complete!: () => void;
    const pending = saveFacilityTextWorkflow(state, () => new Promise<void>((resolve) => { complete = resolve; }), String);
    expect(state.open).toBe(true);
    complete();
    await pending;
    expect(state).toEqual({ open: false, error: '', conflict: false });
  });

  it('keeps the dialog state and content owner open when save fails', async () => {
    const state = { open: true, error: '', conflict: false };
    const content = { value: 'unsaved text' };
    const error = Object.assign(new Error('Revision changed'), { code: 'edit_session_revision_conflict' });
    await saveFacilityTextWorkflow(state, async () => { throw error; }, (value) => (value as Error).message);
    expect(state).toEqual({ open: true, error: 'Revision changed', conflict: true });
    expect(content.value).toBe('unsaved text');
  });

  it('discards the session before loading and reopening on conflict recovery', async () => {
    const state = { open: true, error: 'Revision changed', conflict: true };
    const order: string[] = [];
    const discard = vi.fn(async (id: string) => { order.push(`discard:${id}`); });
    const reopen = vi.fn(async () => { order.push('load-reopen'); });
    await reloadFacilityTextWorkflow(state, 'session-1', discard, reopen, String);
    expect(order).toEqual(['discard:session-1', 'load-reopen']);
    expect(state).toEqual({ open: false, error: '', conflict: false });
  });
});
