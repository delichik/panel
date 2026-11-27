import { defineAsyncComponent, defineComponent, h, type AsyncComponentLoader, type Component } from 'vue';
import RoutePageFallback from '@/components/shell/RoutePageFallback.vue';

/**
 * Return a sync route component whose render tree loads the real page chunk.
 * Vue Router therefore finishes navigation immediately (title/nav/content swap
 * at once). The chunk download happens inside the wrapper with a page-structure
 * skeleton. Prefetch/hover warm-ups call the same loader factory so `import()`
 * reuses the module promise.
 *
 * We intentionally avoid passing `defineAsyncComponent(...)` directly as a
 * route `component` — vue-router warns against that and expects `() => import()`.
 * Wrapping in a sync SFC-like component keeps navigation non-blocking without
 * the warning.
 */
export function lazyPage(loader: AsyncComponentLoader): Component {
  const AsyncPage = defineAsyncComponent({
    loader: async () => {
      const mod = await loader();
      if (mod && typeof mod === 'object' && 'default' in mod) {
        return (mod as { default: Component }).default;
      }
      return mod as Component;
    },
    loadingComponent: RoutePageFallback,
    delay: 0,
    suspensible: false,
  });

  return defineComponent({
    name: 'LazyPage',
    setup() {
      return () => h(AsyncPage);
    },
  });
}

/** Warm a page module without waiting; safe to call repeatedly. */
export function prefetchPage(loader: AsyncComponentLoader): void {
  void loader();
}
