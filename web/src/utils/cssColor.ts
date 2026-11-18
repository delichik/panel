/**
 * Resolve a CSS color token (hex / rgb(a) / color-mix(...) / etc.) to the concrete
 * computed color string that canvas renderers can parse.
 *
 * CSS custom properties can hold functional colors such as `color-mix(...)`
 * (the dark-theme accent ramp does); ECharts/zrender cannot parse those, so a
 * hidden element's computed `color` is used to materialize the real value.
 * Invalid values fall back to the raw token so callers never silently inherit
 * an unrelated color.
 */
export function resolveCssColor(raw: string, doc: Document | null = typeof document === 'undefined' ? null : document): string {
  if (!raw || !doc) return raw;
  const probe = doc.createElement('span');
  probe.style.cssText = 'position:absolute;visibility:hidden;pointer-events:none;inline-size:0;block-size:0';
  doc.body.appendChild(probe);
  try {
    probe.style.color = '';
    probe.style.color = raw;
    // The CSSOM rejects invalid values by clearing style.color.
    if (!probe.style.color) return raw;
    return doc.defaultView?.getComputedStyle(probe).color || raw;
  } finally {
    probe.remove();
  }
}
