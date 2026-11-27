<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId } from 'vue';
import { useRouter } from 'vue-router';
import { Check, ChevronRight, Image as ImageIcon, Moon, Sun } from '@/icons';
import Switch from '@/components/ui/Switch.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useI18n } from '@/i18n';
import { ACCENT_PRESETS, THEME_MODES, useAppearance } from '@/design/appearance';
import { focusWithoutScroll } from '@/utils/focus';
import type { AccentId, ThemeMode } from '@/types/appearance';

/**
 * WinUI 3 title bar "Appearance" flyout.
 *
 * Replaces the retired theme + scheme dropdowns with one flyout that edits the
 * server-persisted appearance store: theme mode (automatic/light/dark), Mica,
 * the 9 Windows accent presets, and a shortcut to the settings background
 * manager. The flyout is a non-modal popup: it is anchored to its trigger,
 * closes on Escape / outside click / route change, keeps Tab inside while open
 * and returns focus to the trigger. All persistence goes through
 * `useAppearance()`; failures roll back and are reported through the global
 * blocking-error dialog (the store never toasts).
 */
const { t } = useI18n();
const router = useRouter();
const notifyError = useErrorDialog();
const { appearance, resolvedTheme, setThemeMode, setMica, setAccent } = useAppearance();

const open = ref(false);
const saving = ref(false);
const flyoutId = useId();
const trigger = ref<HTMLButtonElement | null>(null);
const root = ref<HTMLElement | null>(null);
const panel = ref<HTMLElement | null>(null);
const panelStyle = ref<Record<string, string>>({});
let restoreFocusTo: HTMLElement | null = null;

const themeMode = computed<ThemeMode>(() => appearance.value?.themeMode ?? 'system');
const mica = computed(() => appearance.value?.mica ?? true);
const accent = computed<AccentId>(() => appearance.value?.accent ?? 'blue');
const triggerIcon = computed(() => (resolvedTheme.value === 'dark' ? Moon : Sun));
const themeOptions = computed(() => THEME_MODES.map((mode) => ({ mode, label: t(`appearance.theme.${mode}`) })));
const ready = computed(() => appearance.value !== null);

const FOCUSABLE_SELECTOR = [
  'button:not([disabled])',
  'a[href]',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

const FLYOUT_WIDTH = 320;
const VIEWPORT_PADDING = 8;

function updatePosition() {
  if (!open.value || !trigger.value) return;
  const rect = trigger.value.getBoundingClientRect();
  const width = Math.min(FLYOUT_WIDTH, window.innerWidth - VIEWPORT_PADDING * 2);
  const left = Math.max(VIEWPORT_PADDING, Math.min(rect.right - width, window.innerWidth - width - VIEWPORT_PADDING));
  const top = Math.max(VIEWPORT_PADDING, rect.bottom + 6);
  panelStyle.value = {
    position: 'fixed',
    top: `${top}px`,
    left: `${left}px`,
    width: `${width}px`,
  };
}

function onDocumentPointerDown(event: MouseEvent) {
  if (!open.value) return;
  const target = event.target as Node;
  if (root.value?.contains(target) || panel.value?.contains(target)) return;
  closeFlyout(false);
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !open.value) return;
  event.preventDefault();
  closeFlyout(true);
}

function onWindowReflow() {
  if (open.value) updatePosition();
}

async function openFlyout() {
  if (open.value) return;
  restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null;
  open.value = true;
  await nextTick();
  updatePosition();
  // Position must be applied to the DOM before focus moves: focusing while the
  // teleported panel is still in normal flow can scroll the document and leave
  // the shell shifted up.
  await nextTick();
  // Initial focus lands on the current theme option, like the WinUI flyout that
  // opens with the selected radio focused.
  focusWithoutScroll(panel.value?.querySelector<HTMLElement>('[role="radiogroup"] [aria-checked="true"]'));
}

function closeFlyout(restoreFocus = true) {
  if (!open.value) return;
  open.value = false;
  const target = restoreFocusTo?.isConnected ? restoreFocusTo : trigger.value;
  restoreFocusTo = null;
  if (restoreFocus) void nextTick(() => focusWithoutScroll(target));
}

function toggleFlyout() {
  if (open.value) closeFlyout();
  else void openFlyout();
}

function onPanelKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault();
    closeFlyout(true);
    return;
  }
  if (event.key !== 'Tab' || !panel.value) return;
  // The flyout is a light-dismiss popup but Tab cycles inside it while open, so
  // keyboard users never fall through to the page behind the trigger.
  const items = Array.from(panel.value.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
  if (!items.length) return;
  const first = items[0]!;
  const last = items[items.length - 1]!;
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    focusWithoutScroll(last);
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    focusWithoutScroll(first);
  }
}

function moveRadio<T>(event: KeyboardEvent, items: readonly T[], current: T, select: (item: T) => void) {
  const forward = event.key === 'ArrowRight' || event.key === 'ArrowDown';
  const backward = event.key === 'ArrowLeft' || event.key === 'ArrowUp';
  if (!forward && !backward && event.key !== 'Home' && event.key !== 'End') return;
  event.preventDefault();
  const index = Math.max(0, items.indexOf(current));
  let next = index;
  if (forward) next = (index + 1) % items.length;
  if (backward) next = (index - 1 + items.length) % items.length;
  if (event.key === 'Home') next = 0;
  if (event.key === 'End') next = items.length - 1;
  const group = event.currentTarget as HTMLElement | null;
  select(items[next]!);
  // Roving tabindex: after the store's optimistic commit re-renders the group,
  // move DOM focus to the newly checked option.
  void nextTick(() => focusWithoutScroll(group?.querySelector<HTMLElement>('[aria-checked="true"]')));
}

async function persist(action: () => Promise<void>) {
  if (!ready.value || saving.value) return;
  saving.value = true;
  try {
    await action();
  } catch (error) {
    notifyError(t('appearance.errors.save'), error);
  } finally {
    saving.value = false;
  }
}

function selectTheme(mode: ThemeMode) {
  if (mode === themeMode.value) return;
  void persist(() => setThemeMode(mode));
}

function selectMica(value: boolean) {
  if (value === mica.value) return;
  void persist(() => setMica(value));
}

function selectAccent(id: AccentId) {
  if (id === accent.value) return;
  void persist(() => setAccent(id));
}

function manageBackground() {
  closeFlyout(false);
  void router.push('/settings/appearance');
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown);
  document.addEventListener('keydown', onDocumentKeydown);
  window.addEventListener('resize', onWindowReflow);
  window.addEventListener('scroll', onWindowReflow, true);
});

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown);
  document.removeEventListener('keydown', onDocumentKeydown);
  window.removeEventListener('resize', onWindowReflow);
  window.removeEventListener('scroll', onWindowReflow, true);
});
</script>

<template>
  <div ref="root" class="relative">
    <button
      ref="trigger"
      type="button"
      class="flyout-trigger inline-flex h-8 items-center gap-2 rounded-control border border-transparent px-2.5 text-sm text-secondary-foreground hover:bg-hover hover:text-foreground active:bg-pressed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
      :aria-label="t('appearance.title')"
      aria-haspopup="dialog"
      :aria-controls="flyoutId"
      :aria-expanded="open ? 'true' : 'false'"
      @click="toggleFlyout"
    >
      <component :is="triggerIcon" class="size-4" aria-hidden="true" />
      <span class="max-sm:hidden">{{ t('appearance.title') }}</span>
    </button>
    <Teleport to="body">
      <Transition name="menu">
        <div
          v-if="open"
          :id="flyoutId"
          ref="panel"
          role="dialog"
          aria-modal="false"
          :aria-label="t('appearance.title')"
          :aria-busy="saving ? 'true' : undefined"
          class="flyout-panel z-50 max-h-[calc(100dvh-24px)] overflow-y-auto surface-flyout p-3 text-popover-foreground backdrop-blur-flyout"
          :style="panelStyle"
          @keydown="onPanelKeydown"
        >
          <!-- Theme mode: WinUI RadioButtons rendered as a compact segmented row. -->
          <p class="mb-1.5 text-xs font-semibold text-secondary-foreground">{{ t('appearance.theme.label') }}</p>
          <div
            role="radiogroup"
            :aria-label="t('appearance.theme.label')"
            class="flex gap-0.5 surface-control p-0.5"
            @keydown="moveRadio($event, THEME_MODES, themeMode, selectTheme)"
          >
            <button
              v-for="option in themeOptions"
              :key="option.mode"
              type="button"
              role="radio"
              :aria-checked="themeMode === option.mode ? 'true' : 'false'"
              :tabindex="themeMode === option.mode ? 0 : -1"
              class="h-7 flex-1 rounded-control px-2 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
              :class="themeMode === option.mode ? 'bg-accent-selected font-semibold text-accent-text' : 'text-secondary-foreground hover:bg-hover hover:text-foreground'"
              @click="selectTheme(option.mode)"
            >
              {{ option.label }}
            </button>
          </div>

          <!-- Mica: ToggleSwitch row with a caption explaining the effect. -->
          <div class="mt-3 flex items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="m-0 text-sm text-foreground">{{ t('appearance.mica.label') }}</p>
              <p class="m-0 mt-0.5 text-xs leading-4 text-muted-foreground">{{ t('appearance.mica.hint') }}</p>
            </div>
            <Switch
              class="mt-0.5"
              :model-value="mica"
              :label="t('appearance.mica.label')"
              @update:model-value="selectMica"
            />
          </div>

          <!-- Accent palette: 9 Windows personalization colors, radio semantics. -->
          <p class="mb-1.5 mt-3 text-xs font-semibold text-secondary-foreground">{{ t('appearance.accent.label') }}</p>
          <div
            role="radiogroup"
            :aria-label="t('appearance.accent.label')"
            class="grid grid-cols-9 gap-1.5"
            @keydown="moveRadio($event, ACCENT_PRESETS.map((preset) => preset.id), accent, selectAccent)"
          >
            <button
              v-for="preset in ACCENT_PRESETS"
              :key="preset.id"
              type="button"
              role="radio"
              :aria-label="t(`appearance.accent.${preset.id}`)"
              :aria-checked="accent === preset.id ? 'true' : 'false'"
              :title="t(`appearance.accent.${preset.id}`)"
              :tabindex="accent === preset.id ? 0 : -1"
              class="grid size-6 place-items-center justify-self-center rounded-full border border-stroke transition-shadow focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              :class="accent === preset.id ? 'ring-2 ring-accent-text ring-offset-1 ring-offset-flyout' : undefined"
              :style="{ backgroundColor: preset.seed }"
              @click="selectAccent(preset.id)"
            >
              <Check v-if="accent === preset.id" class="size-3.5 text-white" aria-hidden="true" />
            </button>
          </div>

          <!-- Background management lives on the settings page; keep one entry point. -->
          <div class="mt-3 border-t border-divider pt-2">
            <button
              type="button"
              class="flex h-8 w-full items-center justify-between gap-2 rounded-control px-2.5 text-sm text-secondary-foreground hover:bg-hover hover:text-foreground active:bg-pressed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
              @click="manageBackground"
            >
              <span class="flex min-w-0 items-center gap-2">
                <ImageIcon class="size-4 shrink-0" aria-hidden="true" />
                <span class="truncate">{{ t('appearance.background.manage') }}</span>
              </span>
              <ChevronRight class="size-4 shrink-0" aria-hidden="true" />
            </button>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
/* Fill-only feedback at WinUI timings; the flyout itself uses the shared menu transition. */
.flyout-trigger {
  transition:
    background-color var(--panel-motion-duration-fast) var(--panel-motion-ease-move),
    color var(--panel-motion-duration-fast) var(--panel-motion-ease-move);
}

.flyout-trigger:active {
  transition-duration: var(--panel-motion-duration-fade);
  transition-timing-function: var(--panel-motion-ease-linear);
}
</style>
