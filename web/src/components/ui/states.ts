/**
 * WinUI 3 共享控件状态串。
 *
 * 这里集中定义“焦点 / 禁用 / 层级”三类语义 class，避免各控件各写一套：
 * - focusRing / focusRingInset：2px 实色焦点框（`--panel-ring` = 前景色，无 offset，立即出现）。
 * - fieldFocus：字段用 1px 描边 + 2px 底部 accent 线（WinUI TextBox），不再叠加 ring。
 * - disabledField / disabledText / disabledControl：禁用态统一用色板（stroke/muted/text-muted），
 *   不再用单一 opacity。字段类禁用同时屏蔽 hover 描边。
 * - zIndex：浮层层级语义（page overlay / tooltip / flyout / dialog / toast）。
 *   main.css 只提供 `z-dialog`/`z-skip`；这里先集中到单一来源，后续应提升为 main.css utility。
 */

export const focusRing = 'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring';

export const focusRingInset = 'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset';

export const fieldFocus = 'focus:border-border-strong focus:outline-none focus:focus-accent-underline';

export const fieldDisabled = 'disabled:cursor-not-allowed disabled:border-stroke disabled:bg-muted disabled:text-muted-foreground disabled:hover:border-stroke disabled:hover:bg-control';

export const disabledText = 'disabled:pointer-events-none disabled:text-muted-foreground';

export const disabledControl = 'disabled:pointer-events-none disabled:border-stroke disabled:bg-muted disabled:text-muted-foreground';

/** 条件式禁用色板（用于同时承载 loading 的按钮：loading 时保持变体配色，只有真 disabled 才走色板）。 */
export const disabledControlColors = 'border-stroke bg-muted text-muted-foreground';

export const zIndex = {
  pageOverlay: 'z-20',
  tooltip: 'z-40',
  flyout: 'z-50',
  dialog: 'z-50',
  toast: 'z-dialog',
} as const;
