import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AUTO_SAVE_DELAY_MS, AUTO_SAVE_SAVED_RESET_MS, useDebouncedAutoSave } from './useDebouncedAutoSave';

interface Payload {
  value: string;
}

interface Result {
  saved: string;
}

function payload(value: string): Payload {
  return { value };
}

async function advance(ms: number) {
  await vi.advanceTimersByTimeAsync(ms);
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('useDebouncedAutoSave debounce', () => {
  it('merges rapid changes and only sends the latest payload after 500ms of quiet', async () => {
    const save = vi.fn(async (input: Payload): Promise<Result> => ({ saved: input.value }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save });

    controller.schedule(() => payload('a'));
    expect(controller.status.value).toBe('pending');
    await advance(300);
    controller.schedule(() => payload('b'));
    await advance(AUTO_SAVE_DELAY_MS - 1);
    expect(save).not.toHaveBeenCalled();
    await advance(1);

    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ value: 'b' });
    expect(controller.status.value).toBe('saved');
  });

  it('returns to idle shortly after the saved indicator', async () => {
    const save = vi.fn(async () => ({ saved: 'ok' }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save });
    controller.schedule(() => payload('a'));
    await advance(AUTO_SAVE_DELAY_MS);
    expect(controller.status.value).toBe('saved');

    await advance(AUTO_SAVE_SAVED_RESET_MS);
    expect(controller.status.value).toBe('idle');
  });

  it('cancels the pending save and restarts the full 500ms on every keystroke', async () => {
    const save = vi.fn(async (input: Payload): Promise<Result> => ({ saved: input.value }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save });

    controller.schedule(() => payload('a'));
    await advance(499);
    // 原来第 500ms 会触发，但第 499ms 又输入了一次：必须取消并重新计时。
    controller.schedule(() => payload('ab'));
    await advance(AUTO_SAVE_DELAY_MS - 1);
    expect(save).not.toHaveBeenCalled();
    expect(controller.status.value).toBe('pending');

    controller.schedule(() => payload('abc'));
    await advance(AUTO_SAVE_DELAY_MS - 1);
    expect(save).not.toHaveBeenCalled();

    await advance(1);
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ value: 'abc' });
  });
});

describe('useDebouncedAutoSave request serialization', () => {
  it('restarts the 500ms debounce for changes made while a request is in flight', async () => {
    let resolveFirst: ((result: Result) => void) | undefined;
    const save = vi.fn((input: Payload) => {
      if (save.mock.calls.length === 1) {
        return new Promise<Result>((resolve) => {
          resolveFirst = resolve;
        });
      }
      return Promise.resolve<Result>({ saved: input.value });
    });
    const onSaved = vi.fn();
    const controller = useDebouncedAutoSave<Payload, Result>({ save, onSaved });

    controller.schedule(() => payload('first'));
    await advance(AUTO_SAVE_DELAY_MS);
    expect(save).toHaveBeenCalledTimes(1);
    expect(controller.status.value).toBe('saving');

    controller.schedule(() => payload('second'));
    expect(controller.status.value).toBe('saving');

    resolveFirst?.({ saved: 'first' });
    await advance(0);

    // 请求在飞期间有新输入：响应回来后不得立即补发，而是重新等 500ms 静默。
    expect(save).toHaveBeenCalledTimes(1);
    expect(controller.status.value).toBe('pending');
    expect(onSaved).toHaveBeenNthCalledWith(1, { saved: 'first' }, { value: 'first' }, { queued: true });

    // 继续输入：再次取消并顺延，不允许在输入过程中补发。
    await advance(400);
    controller.schedule(() => payload('third'));
    await advance(AUTO_SAVE_DELAY_MS - 1);
    expect(save).toHaveBeenCalledTimes(1);

    await advance(1);
    expect(save).toHaveBeenCalledTimes(2);
    expect(save.mock.calls[1]![0]).toEqual({ value: 'third' });
    expect(controller.status.value).toBe('saved');
    expect(onSaved).toHaveBeenNthCalledWith(2, { saved: 'third' }, { value: 'third' }, { queued: false });
  });
});

describe('useDebouncedAutoSave validation and failures', () => {
  it('does not send invalid payloads and reports the invalid state', async () => {
    const save = vi.fn(async () => ({ saved: 'ok' }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save });

    controller.schedule(() => null);
    expect(controller.status.value).toBe('invalid');
    await advance(AUTO_SAVE_DELAY_MS * 2);

    expect(save).not.toHaveBeenCalled();
    expect(controller.status.value).toBe('invalid');
  });

  it('keeps the payload after a failure and retries through flush()', async () => {
    const failure = new Error('offline');
    const onFailed = vi.fn();
    const save = vi.fn()
      .mockRejectedValueOnce(failure)
      .mockResolvedValueOnce({ saved: 'retry-ok' });
    const controller = useDebouncedAutoSave<Payload, Result>({ save, onFailed });

    controller.schedule(() => payload('retry'));
    await advance(AUTO_SAVE_DELAY_MS);

    expect(controller.status.value).toBe('error');
    expect(controller.error.value).toBe(failure);
    expect(onFailed).toHaveBeenCalledWith(failure);

    await controller.flush();
    expect(save).toHaveBeenCalledTimes(2);
    expect(save.mock.calls[1]![0]).toEqual({ value: 'retry' });
    expect(controller.status.value).toBe('saved');
    expect(controller.error.value).toBeNull();
  });

  it('cancels a pending timer without sending anything', async () => {
    const save = vi.fn(async () => ({ saved: 'ok' }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save });

    controller.schedule(() => payload('a'));
    controller.cancel();
    await advance(AUTO_SAVE_DELAY_MS * 2);

    expect(save).not.toHaveBeenCalled();
    expect(controller.status.value).toBe('idle');
  });
});

describe('useDebouncedAutoSave teardown', () => {
  it('flushes the pending change once on dispose and stops following timers', async () => {
    const save = vi.fn(async () => ({ saved: 'ok' }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save });

    controller.schedule(() => payload('pending'));
    controller.dispose();
    expect(save).toHaveBeenCalledTimes(1);
    expect(save).toHaveBeenCalledWith({ value: 'pending' });

    await advance(AUTO_SAVE_DELAY_MS * 2);
    expect(save).toHaveBeenCalledTimes(1);
  });

  it('ignores a late response after dispose', async () => {
    let resolveSave: ((result: Result) => void) | undefined;
    const onSaved = vi.fn();
    const save = vi.fn(() => new Promise<Result>((resolve) => {
      resolveSave = resolve;
    }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save, onSaved });

    controller.schedule(() => payload('late'));
    await advance(AUTO_SAVE_DELAY_MS);
    expect(controller.status.value).toBe('saving');

    controller.dispose();
    resolveSave?.({ saved: 'late' });
    await advance(0);

    expect(onSaved).not.toHaveBeenCalled();
    expect(controller.status.value).not.toBe('saved');
  });

  it('ignores a late response after cancel so a refresh cannot be overwritten', async () => {
    let resolveSave: ((result: Result) => void) | undefined;
    const onSaved = vi.fn();
    const save = vi.fn(() => new Promise<Result>((resolve) => {
      resolveSave = resolve;
    }));
    const controller = useDebouncedAutoSave<Payload, Result>({ save, onSaved });

    controller.schedule(() => payload('stale'));
    await advance(AUTO_SAVE_DELAY_MS);
    controller.cancel();
    resolveSave?.({ saved: 'stale' });
    await advance(0);

    expect(onSaved).not.toHaveBeenCalled();
    expect(controller.status.value).toBe('idle');

    // 取消后重新修改仍可正常保存。
    const saveAgain = vi.fn(async (input: Payload) => ({ saved: input.value }));
    const next = useDebouncedAutoSave<Payload, Result>({ save: saveAgain });
    next.schedule(() => payload('fresh'));
    await advance(AUTO_SAVE_DELAY_MS);
    expect(saveAgain).toHaveBeenCalledWith({ value: 'fresh' });
  });
});
