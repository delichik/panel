import { ref, type Ref } from 'vue';

/** 设置页表单控件的防抖保存延迟：连续输入只有停止 500ms 后才提交。 */
export const AUTO_SAVE_DELAY_MS = 500;
/** `saved` 状态展示时长，随后静默回到 `idle`，避免常驻成功提示。 */
export const AUTO_SAVE_SAVED_RESET_MS = 2000;

/** 自动保存状态机。`pending` 等待防抖计时器，`saving` 请求在飞。 */
export type AutoSaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'invalid' | 'error';

/** AutoSaveStatus 的本地化文案（由页面传入，组件不绑定 i18n key）。 */
export interface AutoSaveStatusLabels {
  /** pending / saving 共用文案。 */
  saving: string;
  saved: string;
  failed: string;
  invalid: string;
  retry: string;
}

/**
 * 在真正发请求前取样。返回 `null` 表示本地校验未通过：不发送请求，
 * 状态转为 `invalid`，由页面在字段旁展示具体错误。
 */
export type AutoSaveCollector<P> = () => P | null;

export interface DebouncedAutoSaveOptions<P, R> {
  /** 实际提交函数；只提交当前分区子资源。 */
  save: (payload: P) => Promise<R>;
  /** 保存成功回调；`meta.queued` 表示请求期间又有新修改在等待补发。 */
  onSaved?: (result: R, payload: P, meta: { queued: boolean }) => void;
  /** 保存失败回调；保留用户输入，由页面决定就地状态/回填策略（自动保存失败不 toast、不弹窗）。 */
  onFailed?: (error: unknown) => void;
  /** 防抖延迟，默认 500ms。 */
  delayMs?: number;
  /** `saved` 状态保持时长，默认 2s。 */
  savedResetMs?: number;
}

export interface DebouncedAutoSave<P, R> {
  status: Ref<AutoSaveState>;
  /** 最近一次失败的错误对象；重试成功后清空。 */
  error: Ref<unknown>;
  /** 记录一次用户修改并重置防抖计时器。 */
  schedule: (collect: AutoSaveCollector<P>) => void;
  /** 跳过防抖立即保存（失败重试、卸载 flush）。 */
  flush: () => Promise<void>;
  /** 取消待发修改并丢弃在飞响应（刷新/危险动作覆盖）。 */
  cancel: () => void;
  /** 卸载时清理计时器并把待保存修改 fire-and-forget 发一次。 */
  dispose: () => void;
}

/**
 * 同分区串行自动保存控制器。
 *
 * - 同一实例同一时刻只允许一个请求在飞；请求期间的新修改先挂起，响应回来后
 *   重新从 0 开始 500ms 防抖，继续输入会继续取消并顺延，绝不边输入边补发。
 * - `collect` 在计时器触发时重新取样，保证提交的是最新输入。
 * - `cancel`/`dispose` 会递增内部代号，迟到响应不会再改状态或触发回调。
 */
export function useDebouncedAutoSave<P, R>(
  options: DebouncedAutoSaveOptions<P, R>,
): DebouncedAutoSave<P, R> {
  const delayMs = options.delayMs ?? AUTO_SAVE_DELAY_MS;
  const savedResetMs = options.savedResetMs ?? AUTO_SAVE_SAVED_RESET_MS;

  const status = ref<AutoSaveState>('idle');
  const error = ref<unknown>(null);

  let collector: AutoSaveCollector<P> | null = null;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let resetTimer: ReturnType<typeof setTimeout> | undefined;
  let inFlight = false;
  let queued = false;
  let disposed = false;
  /** 代际编号：cancel/dispose 后旧请求的回调一律丢弃。 */
  let generation = 0;

  function clearTimer() {
    if (timer !== undefined) {
      clearTimeout(timer);
      timer = undefined;
    }
  }

  function clearResetTimer() {
    if (resetTimer !== undefined) {
      clearTimeout(resetTimer);
      resetTimer = undefined;
    }
  }

  function collect(): P | null {
    if (!collector) return null;
    try {
      return collector();
    } catch {
      // 校验函数本身抛错时按本地校验失败处理，绝不把半成品发出去。
      return null;
    }
  }

  function showSaved() {
    status.value = 'saved';
    clearResetTimer();
    resetTimer = setTimeout(() => {
      resetTimer = undefined;
      if (!disposed && status.value === 'saved') status.value = 'idle';
    }, savedResetMs);
  }

  /**
   * 启动（或重启）500ms 防抖计时器。任何新输入都必须取消旧计时器并重新计时：
   * 用户仍在连续输入时绝不触发保存，只有停止输入满 500ms 才提交。
   */
  function armTimer() {
    clearTimer();
    clearResetTimer();
    error.value = null;
    status.value = 'pending';
    timer = setTimeout(() => {
      timer = undefined;
      void run();
    }, delayMs);
  }

  async function run() {
    if (disposed) return;
    if (inFlight) {
      queued = true;
      return;
    }
    if (!collector) return;
    const payload = collect();
    if (payload === null) {
      status.value = 'invalid';
      return;
    }

    const runGeneration = generation;
    inFlight = true;
    status.value = 'saving';
    error.value = null;

    let succeeded = false;
    let result: R | undefined;
    let failure: unknown;
    try {
      result = await options.save(payload);
      succeeded = true;
    } catch (err) {
      failure = err;
    } finally {
      inFlight = false;
    }

    if (disposed) return;
    if (runGeneration !== generation) {
      // 刷新/危险动作取消了本次保存：丢弃迟到响应；若期间又产生新修改，
      // 重新开始 500ms 防抖，不立即补发。
      const hadQueued = queued;
      queued = false;
      if (hadQueued) armTimer();
      return;
    }

    const hasQueued = queued;
    queued = false;

    if (succeeded) {
      if (hasQueued) {
        // 请求在飞期间用户仍在输入：本次响应不回填，重新等待 500ms 静默后
        // 才补发最新值；继续输入会再次取消并顺延。
        options.onSaved?.(result as R, payload, { queued: true });
        armTimer();
      } else {
        collector = null;
        options.onSaved?.(result as R, payload, { queued: false });
        showSaved();
      }
    } else {
      error.value = failure;
      status.value = 'error';
      options.onFailed?.(failure);
      // 失败时若期间已有新输入，同样从最后一次输入重新计时，避免边输入边重试。
      if (hasQueued) armTimer();
    }
  }

  function schedule(next: AutoSaveCollector<P>) {
    if (disposed) return;
    collector = next;
    clearTimer();
    clearResetTimer();
    if (inFlight) {
      // 请求在飞：记下“有更新”，等响应回来后重新从 0 开始 500ms 防抖。
      queued = true;
      return;
    }
    if (collect() === null) {
      status.value = 'invalid';
      return;
    }
    armTimer();
  }

  async function flush() {
    if (disposed) return;
    clearTimer();
    clearResetTimer();
    if (inFlight) {
      queued = true;
      return;
    }
    await run();
  }

  function cancel() {
    clearTimer();
    clearResetTimer();
    collector = null;
    queued = false;
    generation += 1;
    error.value = null;
    status.value = 'idle';
  }

  function dispose() {
    if (disposed) return;
    clearTimer();
    clearResetTimer();
    // 卸载时仍有未提交修改（防抖等待中，或请求在飞期间又改了）：立即发一次。
    const shouldFlush = collector !== null && (queued || !inFlight);
    const remaining = shouldFlush ? collect() : null;
    if (shouldFlush && remaining !== null) {
      void options.save(remaining).catch(() => {
        // 页面已卸载，失败无人消费；服务端状态以刷新后的读取为准。
      });
    }
    disposed = true;
    generation += 1;
    collector = null;
    queued = false;
    inFlight = false;
  }

  return { status, error, schedule, flush, cancel, dispose };
}
