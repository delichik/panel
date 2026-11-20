import { describe, expect, it } from 'vitest';
import {
  deleteLocalAsset,
  emptyLocalAssetState,
  findPendingText,
  localAssetsAreDirty,
  planLocalAssetFlush,
  resetLocalAssetState,
  upsertLocalAsset,
  visibleLocalAssets,
} from './localAssets';

describe('localAssets', () => {
  it('overlays upserts and deletes on the committed baseline', () => {
    const state = emptyLocalAssetState();
    resetLocalAssetState(state, [
      { key: 'a', name: 'a', kind: 'text', size: 1, editable: true },
      { key: 'b', name: 'b', filename: 'b.bin', kind: 'binary', size: 2, editable: false },
    ]);
    upsertLocalAsset(state, {
      type: 'upsert', key: 'a', name: 'a', kind: 'text', textContent: 'hello', size: 5,
    });
    deleteLocalAsset(state, 'b');
    upsertLocalAsset(state, {
      type: 'upsert', key: 'c', name: 'c', filename: 'c.txt', kind: 'text', textContent: 'new', size: 3,
    });
    expect(visibleLocalAssets(state).map((item) => item.name)).toEqual(['a', 'c']);
    expect(findPendingText(state, 'a')).toBe('hello');
    expect(localAssetsAreDirty(state)).toBe(true);
  });

  it('plans deletes then upserts and treats rename as a single upsert', () => {
    const state = emptyLocalAssetState();
    resetLocalAssetState(state, [
      { key: 'old', name: 'old', kind: 'text', size: 1, editable: true },
      { key: 'gone', name: 'gone', kind: 'binary', size: 2, editable: false },
    ]);
    upsertLocalAsset(state, {
      type: 'upsert', key: 'old', name: 'renamed', kind: 'text', textContent: 'x', size: 1,
    });
    deleteLocalAsset(state, 'gone');
    upsertLocalAsset(state, {
      type: 'upsert', key: 'fresh', name: 'fresh', kind: 'binary', file: new File(['z'], 'z.bin'), size: 1,
    });
    const plan = planLocalAssetFlush(state);
    expect(plan.deletes).toEqual(['gone']);
    expect(plan.upserts.map((item) => ({ key: item.key, name: item.name }))).toEqual([
      { key: 'old', name: 'renamed' },
      { key: 'fresh', name: 'fresh' },
    ]);
  });
});
