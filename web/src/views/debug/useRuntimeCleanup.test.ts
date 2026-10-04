// @vitest-environment jsdom
import { defineComponent } from 'vue';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { debugApi, type ClearRuntimeDataStatus } from '@/api/debug';
import { useRuntimeCleanup } from './useRuntimeCleanup';

const idle: ClearRuntimeDataStatus = { cleared: false, running: false, status: 'idle' };
const active: ClearRuntimeDataStatus = { cleared: false, running: true, status: 'running', runId: 'clear-1', stage: 'clearing_logs', startedAt: '2026-09-14T00:00:00Z' };
const complete: ClearRuntimeDataStatus = { ...active, cleared: true, running: false, status: 'succeeded', stage: 'completed' };
let wrapper: VueWrapper | undefined;
let cleanup: ReturnType<typeof useRuntimeCleanup>;
let terminal: ReturnType<typeof vi.fn>;
let requestError: ReturnType<typeof vi.fn>;

function mountCleanup() {
  terminal = vi.fn();
  requestError = vi.fn();
  wrapper = mount(defineComponent({
    setup() { cleanup = useRuntimeCleanup({ onTerminal: terminal, onRequestError: requestError }); return () => null; },
  }));
}

beforeEach(() => {
  vi.useFakeTimers();
  sessionStorage.clear();
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
});
afterEach(() => {
  wrapper?.unmount();
  wrapper = undefined;
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('Debug runtime cleanup (UI-DBG-007, DIAG-CLR-005)', () => {
  it('restores progress on entry and refreshes once the same run completes', async () => {
    const query = vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValueOnce(active).mockResolvedValue(complete);
    mountCleanup();
    expect(cleanup.canStart.value).toBe(false);
    await flushPromises();
    expect(cleanup.status.value?.stage).toBe('clearing_logs');
    expect(cleanup.canStart.value).toBe(false);
    await vi.advanceTimersByTimeAsync(1500);
    expect(terminal).toHaveBeenCalledTimes(1);
    expect(terminal).toHaveBeenCalledWith(complete);
    expect(cleanup.canStart.value).toBe(true);
    await vi.advanceTimersByTimeAsync(15000);
    expect(query).toHaveBeenCalledTimes(2);
  });

  it('preserves running state on query errors and retries only status reads', async () => {
    vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValueOnce(active).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(complete);
    const post = vi.spyOn(debugApi, 'clearRuntimeData');
    mountCleanup();
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1500);
    expect(cleanup.queryFailed.value).toBe(true);
    expect(cleanup.running.value).toBe(true);
    expect(cleanup.canStart.value).toBe(false);
    expect(terminal).not.toHaveBeenCalled();
    await cleanup.refresh();
    expect(cleanup.queryFailed.value).toBe(false);
    expect(terminal).toHaveBeenCalledTimes(1);
    expect(terminal).toHaveBeenCalledWith(complete);
    expect(post).not.toHaveBeenCalled();
  });

  it('does not resubmit a cleanup after losing the acceptance response', async () => {
    vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValueOnce(idle).mockResolvedValue(active);
    const post = vi.spyOn(debugApi, 'clearRuntimeData').mockRejectedValue(new Error('connection lost'));
    mountCleanup();
    await flushPromises();
    await cleanup.start();
    expect(requestError).toHaveBeenCalledOnce();
    expect(cleanup.running.value).toBe(true);
    await vi.advanceTimersByTimeAsync(10000);
    await cleanup.start();
    expect(post).toHaveBeenCalledOnce();
    expect(terminal).not.toHaveBeenCalled();
  });

  it('does not treat a lost process status as successful completion', async () => {
    sessionStorage.setItem('panel.debug.pendingCleanup', active.runId!);
    vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValue(idle);
    mountCleanup();
    await flushPromises();
    expect(cleanup.resultMissing.value).toBe(true);
    expect(terminal).not.toHaveBeenCalled();
    // A new cleanup still requires the page's explicit confirmation.
    expect(cleanup.canStart.value).toBe(true);
  });

  it('does not attribute another cleanup result to the previously observed run', async () => {
    sessionStorage.setItem('panel.debug.pendingCleanup', 'older-run');
    vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValue(complete);
    mountCleanup();
    await flushPromises();
    expect(terminal).not.toHaveBeenCalled();
    expect(cleanup.status.value?.runId).toBe('clear-1');
  });

  it('keeps partial failure and worker recovery failure distinct', async () => {
    const failed: ClearRuntimeDataStatus = { ...complete, status: 'failed', failedStage: 'resuming_workers', errorCode: 'clear_runtime_data_resume_failed' };
    vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValueOnce(active).mockResolvedValue(failed);
    mountCleanup();
    await flushPromises();
    await vi.advanceTimersByTimeAsync(1500);
    expect(terminal).toHaveBeenCalledTimes(1);
    expect(terminal).toHaveBeenCalledWith(failed);
    expect(cleanup.status.value?.cleared).toBe(true);
    expect(cleanup.status.value?.status).toBe('failed');
    expect(cleanup.canStart.value).toBe(true);
  });

  it('pauses polling in hidden tabs and cancels timers and requests on unmount', async () => {
    const query = vi.spyOn(debugApi, 'clearRuntimeDataStatus').mockResolvedValue(active);
    mountCleanup();
    await flushPromises();
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    document.dispatchEvent(new Event('visibilitychange'));
    await vi.advanceTimersByTimeAsync(15000);
    expect(query).toHaveBeenCalledOnce();
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    query.mockImplementationOnce(signal => new Promise((_, reject) => signal!.addEventListener('abort', () => reject(new Error('aborted')))));
    document.dispatchEvent(new Event('visibilitychange'));
    await flushPromises();
    const signal = query.mock.calls[1][0];
    wrapper!.unmount();
    wrapper = undefined;
    expect(signal?.aborted).toBe(true);
    await vi.advanceTimersByTimeAsync(30000);
    expect(query).toHaveBeenCalledTimes(2);
    expect(terminal).not.toHaveBeenCalled();
  });
});
