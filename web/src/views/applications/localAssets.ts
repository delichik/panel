import type { AssetFileItem, AssetFileKind } from '@/components/patterns/assetFileManager';

/** Committed (or create-empty) baseline entry shown before local pending overlays. */
export interface LocalAssetBaseline {
  key: string;
  name: string;
  filename?: string;
  kind: AssetFileKind;
  size: number;
  sha256?: string;
  editable: boolean;
}

export type LocalAssetMutation =
  | {
    type: 'upsert';
    key: string;
    name: string;
    filename?: string;
    kind: AssetFileKind;
    file?: File;
    textContent?: string;
    size: number;
  }
  | { type: 'delete'; key: string };

export interface LocalAssetState {
  baseline: LocalAssetBaseline[];
  mutations: LocalAssetMutation[];
}

export function emptyLocalAssetState(): LocalAssetState {
  return { baseline: [], mutations: [] };
}

export function resetLocalAssetState(state: LocalAssetState, baseline: LocalAssetBaseline[]) {
  state.baseline = baseline.map((item) => ({ ...item }));
  state.mutations = [];
}

/** Fold baseline ⊕ mutations into the list AssetFileManager / pickers should show. */
export function visibleLocalAssets(state: LocalAssetState): AssetFileItem[] {
  const map = new Map<string, AssetFileItem>();
  for (const item of state.baseline) {
    map.set(item.key, {
      key: item.key,
      name: item.name,
      filename: item.filename,
      kind: item.kind,
      size: item.size,
      sha256: item.sha256,
      editable: item.editable,
    });
  }
  for (const mutation of state.mutations) {
    if (mutation.type === 'delete') {
      map.delete(mutation.key);
      continue;
    }
    if (mutation.key !== mutation.name) {
      map.delete(mutation.key);
    }
    map.set(mutation.name, {
      key: mutation.name,
      name: mutation.name,
      filename: mutation.filename ?? mutation.name,
      kind: mutation.kind,
      size: mutation.size,
      editable: mutation.kind === 'text',
    });
  }
  return [...map.values()].sort((a, b) => a.name.localeCompare(b.name));
}

export function localAssetNames(state: LocalAssetState): string[] {
  return visibleLocalAssets(state).map((item) => item.name);
}

export function findPendingText(state: LocalAssetState, key: string): string | undefined {
  for (let index = state.mutations.length - 1; index >= 0; index -= 1) {
    const mutation = state.mutations[index];
    if (mutation.type === 'delete' && (mutation.key === key)) return undefined;
    if (mutation.type === 'upsert' && (mutation.key === key || mutation.name === key) && mutation.textContent !== undefined) {
      return mutation.textContent;
    }
  }
  return undefined;
}

export function findPendingFile(state: LocalAssetState, key: string): File | undefined {
  for (let index = state.mutations.length - 1; index >= 0; index -= 1) {
    const mutation = state.mutations[index];
    if (mutation.type === 'delete' && mutation.key === key) return undefined;
    if (mutation.type === 'upsert' && (mutation.key === key || mutation.name === key) && mutation.file) {
      return mutation.file;
    }
  }
  return undefined;
}

export function upsertLocalAsset(state: LocalAssetState, mutation: Extract<LocalAssetMutation, { type: 'upsert' }>) {
  state.mutations.push(mutation);
}

export function deleteLocalAsset(state: LocalAssetState, key: string) {
  state.mutations.push({ type: 'delete', key });
}

/**
 * Collapse mutations into a flush plan: deletes first, then upserts.
 * Rename (key → name) is a single upsert that keeps `key` as the server identity;
 * it must not also emit a delete for the old key.
 */
export function planLocalAssetFlush(state: LocalAssetState): {
  deletes: string[];
  upserts: Array<Extract<LocalAssetMutation, { type: 'upsert' }>>;
} {
  type Slot =
    | { kind: 'delete' }
    | { kind: 'upsert'; mutation: Extract<LocalAssetMutation, { type: 'upsert' }> };
  /** Tracked by the identity the server currently knows (`mutation.key` / baseline key). */
  const byIdentity = new Map<string, Slot>();

  for (const mutation of state.mutations) {
    if (mutation.type === 'delete') {
      byIdentity.set(mutation.key, { kind: 'delete' });
      continue;
    }
    if (mutation.key !== mutation.name) {
      // Rename: clear any pending op under the destination name identity only.
      byIdentity.delete(mutation.name);
    }
    byIdentity.set(mutation.key, { kind: 'upsert', mutation });
  }

  const baselineKeys = new Set(state.baseline.map((item) => item.key));
  const deletes: string[] = [];
  const upserts: Array<Extract<LocalAssetMutation, { type: 'upsert' }>> = [];
  const renamedAway = new Set<string>();

  for (const slot of byIdentity.values()) {
    if (slot.kind === 'upsert' && slot.mutation.key !== slot.mutation.name) {
      renamedAway.add(slot.mutation.key);
    }
  }

  for (const [key, slot] of byIdentity) {
    if (slot.kind === 'delete') {
      if (baselineKeys.has(key) && !renamedAway.has(key)) deletes.push(key);
      continue;
    }
    upserts.push(slot.mutation);
  }

  return { deletes, upserts };
}

export function localAssetsAreDirty(state: LocalAssetState) {
  return state.mutations.length > 0;
}
