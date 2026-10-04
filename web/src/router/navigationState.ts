import { computed, reactive } from 'vue';
import type { Router } from 'vue-router';

const state = reactive({
  pending: false,
  targetPath: '',
  titleKey: '',
});
let nextAttemptId = 0;

export const routeNavigation = {
  pending: computed(() => state.pending),
  targetPath: computed(() => state.targetPath),
  titleKey: computed(() => state.titleKey),
};

export function beginRouteNavigation(targetPath: string, titleKey?: unknown) {
  const attemptId = ++nextAttemptId;
  state.pending = true;
  state.targetPath = targetPath;
  state.titleKey = typeof titleKey === 'string' ? titleKey : '';
  return attemptId;
}

export function finishRouteNavigation(attemptId?: number) {
  if (attemptId !== undefined && attemptId !== nextAttemptId) return;
  state.pending = false;
  state.targetPath = '';
  state.titleKey = '';
}

/**
 * Starts feedback before async route components and guards resolve, then clears
 * only the matching attempt. The token prevents an older cancelled navigation
 * from clearing feedback for a newer navigation already in flight.
 */
export function installRouteNavigationFeedback(router: Router) {
  const attempts = new WeakMap<object, number>();
  router.beforeEach((to) => {
    attempts.set(to, beginRouteNavigation(to.fullPath, to.meta.titleKey));
    return true;
  });
  router.afterEach((to) => finishRouteNavigation(attempts.get(to)));
  router.onError((_error, to) => finishRouteNavigation(attempts.get(to)));
}
