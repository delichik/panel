<script setup lang="ts">
import { Check, ChevronDown } from '@lucide/vue';
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useAttrs, useId, watch } from 'vue';
import { cn } from './cn';

defineOptions({ inheritAttrs: false });

type SelectOption = { label: string; value: string; disabled?: boolean };

const props = defineProps<{
  modelValue?: string;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  /** 启用下拉内搜索。父级可监听 search 事件做远程搜索，把结果合并回 options。 */
  searchable?: boolean;
  searchPlaceholder?: string;
  /** 搜索无匹配项时显示的文案（仅在输入了查询词时显示）。 */
  searchEmptyText?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
  change: [value: string];
  /** 搜索词变化；带空串表示清空搜索。父级可据此加载/清理远程候选。 */
  search: [query: string];
}>();

const attrs = useAttrs();
const root = ref<HTMLElement | null>(null);
const button = ref<HTMLButtonElement | null>(null);
const listbox = ref<HTMLElement | null>(null);
const searchInput = ref<HTMLInputElement | null>(null);
const open = ref(false);
const activeIndex = ref(-1);
const query = ref('');
const id = useId();

const selectedOption = computed(() => props.options.find((option) => option.value === props.modelValue));
const selectedLabel = computed(() => selectedOption.value?.label ?? props.placeholder ?? '');
const normalizedQuery = computed(() => query.value.trim().toLowerCase());
/**
 * 搜索只过滤下拉可见项，不影响 selectedLabel：即使当前值不在过滤结果里，
 * 触发器仍展示已选中的标签，避免用户以为选择丢失。
 */
const visibleOptions = computed(() => {
  if (!props.searchable || !normalizedQuery.value) return props.options;
  const term = normalizedQuery.value;
  return props.options.filter((option) => option.label.toLowerCase().includes(term) || option.value.toLowerCase().includes(term));
});
const enabledOptions = computed(() => visibleOptions.value.filter((option) => !option.disabled));
const isDisabled = computed(() => props.disabled);
const activeOption = computed(() => visibleOptions.value[activeIndex.value]);
const activeOptionId = computed(() => (open.value && activeOption.value ? `${id}-option-${activeIndex.value}` : undefined));
const listboxStyle = ref<Record<string, string>>({});

const rootClasses = computed(() => cn('relative w-full min-w-0', attrs.class as string));
const buttonClasses = computed(() => cn(
  'motion-field flex h-9 w-full min-w-0 items-center justify-between gap-2 rounded-xl border border-input bg-card px-3 text-left text-sm text-foreground shadow-sm transition-[background-color,border-color,box-shadow]',
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45 focus-visible:ring-offset-2 focus-visible:ring-offset-card',
  'disabled:cursor-not-allowed disabled:opacity-45',
  open.value && 'border-border-strong bg-popover panel-shadow-overlay',
));
const searchInputClasses = cn(
  'motion-field h-9 w-full rounded-xl border border-input bg-card px-3 text-sm text-foreground shadow-sm placeholder:text-muted-foreground',
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/45 focus-visible:ring-offset-2 focus-visible:ring-offset-card',
);

const hiddenSelectAttrs = computed(() => ({
  name: typeof attrs.name === 'string' ? attrs.name : undefined,
  form: typeof attrs.form === 'string' ? attrs.form : undefined,
  required: attrs.required === '' || attrs.required === true || attrs.required === 'true' ? true : undefined,
}));

/**
 * `inheritAttrs: false` keeps the visual markup clean, but ARIA attributes
 * passed by pages (aria-label / aria-invalid / aria-describedby) must still
 * reach the combobox control instead of being dropped.
 */
const comboboxAriaAttrs = computed(() => Object.fromEntries(
  Object.entries(attrs).filter(([key, value]) => key.startsWith('aria-') && value !== undefined && value !== false),
));

function firstEnabledIndex() {
  return visibleOptions.value.findIndex((option) => !option.disabled);
}

function currentOrFirstEnabledIndex() {
  const currentIndex = visibleOptions.value.findIndex((option) => option.value === props.modelValue && !option.disabled);
  return currentIndex >= 0 ? currentIndex : firstEnabledIndex();
}

function setActiveIndex(nextIndex: number) {
  if (!visibleOptions.value.length) {
    activeIndex.value = -1;
    return;
  }

  const direction = nextIndex >= activeIndex.value ? 1 : -1;
  let cursor = nextIndex;
  for (let checked = 0; checked < visibleOptions.value.length; checked += 1) {
    const normalized = (cursor + visibleOptions.value.length) % visibleOptions.value.length;
    if (!visibleOptions.value[normalized]?.disabled) {
      activeIndex.value = normalized;
      return;
    }
    cursor += direction;
  }
  activeIndex.value = -1;
}

function scrollActiveIntoView() {
  nextTick(() => {
    if (activeIndex.value < 0) return;
    const item = document.getElementById(`${id}-option-${activeIndex.value}`);
    if (typeof item?.scrollIntoView === 'function') item.scrollIntoView({ block: 'nearest' });
  });
}

function openList() {
  if (isDisabled.value) return;
  // 非搜索模式下没有任何选项时保持现状（不展开空面板）；搜索模式下允许展开
  // 面板以便用户输入关键词触发远程搜索。
  if (!props.searchable && !props.options.length) return;
  open.value = true;
  query.value = '';
  activeIndex.value = currentOrFirstEnabledIndex();
  scrollActiveIntoView();
  nextTick(() => {
    updateListboxPosition();
    if (props.searchable) searchInput.value?.focus();
  });
}

function closeList({ restoreFocus = false } = {}) {
  open.value = false;
  activeIndex.value = -1;
  query.value = '';
  if (restoreFocus) button.value?.focus();
}

function toggleList() {
  if (open.value) closeList();
  else openList();
}

function choose(option: SelectOption) {
  if (option.disabled || isDisabled.value) return;
  emit('update:modelValue', option.value);
  emit('change', option.value);
  closeList({ restoreFocus: true });
}

function chooseValue(value: string) {
  const option = props.options.find((item) => item.value === value);
  if (option) choose(option);
}

function setActiveFromPointer(option: SelectOption, index: number) {
  if (!option.disabled) activeIndex.value = index;
}

function moveActive(delta: number) {
  if (!enabledOptions.value.length) return;
  if (!open.value) {
    openList();
    return;
  }
  const start = activeIndex.value >= 0 ? activeIndex.value : currentOrFirstEnabledIndex();
  setActiveIndex(start + delta);
  scrollActiveIntoView();
}

function onKeydown(event: KeyboardEvent) {
  if (isDisabled.value) return;
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    moveActive(1);
    return;
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault();
    moveActive(-1);
    return;
  }
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    if (!open.value) {
      openList();
      return;
    }
    if (activeOption.value) choose(activeOption.value);
    return;
  }
  if (event.key === 'Escape' && open.value) {
    event.preventDefault();
    closeList({ restoreFocus: true });
    return;
  }
  if (event.key === 'Home' && open.value) {
    event.preventDefault();
    setActiveIndex(0);
    scrollActiveIntoView();
    return;
  }
  if (event.key === 'End' && open.value) {
    event.preventDefault();
    setActiveIndex(visibleOptions.value.length - 1);
    scrollActiveIntoView();
    return;
  }
  if (event.key === 'Tab') closeList();
}

function onSearchInput(event: Event) {
  query.value = (event.target as HTMLInputElement).value;
  emit('search', query.value);
  if (!visibleOptions.value.length) {
    activeIndex.value = -1;
    return;
  }
  activeIndex.value = currentOrFirstEnabledIndex();
  scrollActiveIntoView();
}

function onSearchKeydown(event: KeyboardEvent) {
  if (isDisabled.value) return;
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    const start = activeIndex.value >= 0 ? activeIndex.value : -1;
    setActiveIndex(start + 1);
    scrollActiveIntoView();
    return;
  }
  if (event.key === 'ArrowUp') {
    event.preventDefault();
    const start = activeIndex.value >= 0 ? activeIndex.value : visibleOptions.value.length;
    setActiveIndex(start - 1);
    scrollActiveIntoView();
    return;
  }
  if (event.key === 'Enter') {
    event.preventDefault();
    if (activeOption.value) choose(activeOption.value);
    return;
  }
  if (event.key === 'Escape' && open.value) {
    event.preventDefault();
    closeList({ restoreFocus: true });
    return;
  }
  if (event.key === 'Tab') closeList();
}

function onDocumentPointerDown(event: PointerEvent) {
  const target = event.target as Node;
  if (root.value?.contains(target) || listbox.value?.contains(target)) return;
  closeList();
}

function updateListboxPosition() {
  if (!open.value || !button.value) return;
  const rect = button.value.getBoundingClientRect();
  const gap = 8;
  const viewportPadding = 16;
  const availableBelow = window.innerHeight - rect.bottom - gap - viewportPadding;
  const availableAbove = rect.top - gap - viewportPadding;
  const placeAbove = availableBelow < 160 && availableAbove > availableBelow;
  // 搜索输入占用面板顶部空间，最小高度略增以保证至少能看到一条选项。
  const maxHeight = Math.max(120, Math.min(280, placeAbove ? availableAbove : availableBelow));
  listboxStyle.value = {
    position: 'fixed',
    left: `${rect.left}px`,
    top: placeAbove ? 'auto' : `${rect.bottom + gap}px`,
    bottom: placeAbove ? `${window.innerHeight - rect.top + gap}px` : 'auto',
    width: `${rect.width}px`,
    maxHeight: `${maxHeight}px`,
  };
}

watch(() => props.modelValue, () => {
  if (open.value) activeIndex.value = currentOrFirstEnabledIndex();
});

// 远程搜索结果合并回 options 后，保持高亮项在可见范围内。
watch(visibleOptions, () => {
  if (!open.value) return;
  if (activeIndex.value >= visibleOptions.value.length) activeIndex.value = currentOrFirstEnabledIndex();
});

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointerDown);
  window.addEventListener('resize', updateListboxPosition);
  window.addEventListener('scroll', updateListboxPosition, true);
});
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onDocumentPointerDown);
  window.removeEventListener('resize', updateListboxPosition);
  window.removeEventListener('scroll', updateListboxPosition, true);
});
</script>

<template>
  <div ref="root" :class="rootClasses">
    <select
      v-bind="hiddenSelectAttrs"
      class="pointer-events-none absolute size-px opacity-0"
      tabindex="-1"
      aria-hidden="true"
      :disabled="isDisabled"
      :value="modelValue"
      @change="chooseValue(($event.target as HTMLSelectElement).value)"
    >
      <option v-if="placeholder" value="" disabled>{{ placeholder }}</option>
      <option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">
        {{ option.label }}
      </option>
    </select>

    <button
      :id="attrs.id as string | undefined"
      ref="button"
      type="button"
      role="combobox"
      v-bind="comboboxAriaAttrs"
      aria-haspopup="listbox"
      :aria-controls="`${id}-listbox`"
      :aria-expanded="open"
      :aria-activedescendant="activeOptionId"
      :disabled="isDisabled"
      :class="buttonClasses"
      @click="toggleList"
      @keydown="onKeydown"
    >
      <span class="min-w-0 flex-1 truncate" :class="selectedOption ? 'text-foreground' : 'text-muted-foreground'">
        {{ selectedLabel }}
      </span>
      <ChevronDown class="size-4 shrink-0 text-muted-foreground transition-transform duration-150 ease-out" :class="open ? 'rotate-180' : undefined" aria-hidden="true" />
    </button>

    <Teleport to="body">
      <Transition name="menu">
        <div
          v-if="open"
          :id="`${id}-listbox`"
          ref="listbox"
          class="panel-shadow-overlay z-50 overflow-y-auto overflow-x-hidden rounded-xl border border-border bg-popover p-1 text-popover-foreground"
          :style="listboxStyle"
          role="listbox"
          :aria-labelledby="attrs.id as string | undefined"
        >
          <div v-if="searchable" class="sticky top-0 z-10 bg-popover px-1 pb-1 pt-1">
            <input
              ref="searchInput"
              type="search"
              role="searchbox"
              :aria-label="searchPlaceholder"
              :placeholder="searchPlaceholder"
              :value="query"
              :class="searchInputClasses"
              @input="onSearchInput"
              @keydown="onSearchKeydown"
            />
          </div>
          <div>
            <button
              v-for="(option, index) in visibleOptions"
              :id="`${id}-option-${index}`"
              :key="option.value"
              type="button"
              role="option"
              :aria-selected="modelValue === option.value"
              :disabled="option.disabled"
              class="motion-menu-item flex w-full min-w-0 items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors focus-visible:outline-none disabled:pointer-events-none disabled:opacity-40"
              :class="[
                modelValue === option.value ? 'bg-brand-bg text-brand' : 'text-foreground/72 hover:bg-accent hover:text-foreground',
                activeIndex === index && modelValue !== option.value ? 'bg-accent text-foreground' : undefined,
              ]"
              @mouseenter="setActiveFromPointer(option, index)"
              @click="choose(option)"
            >
              <span class="min-w-0 flex-1 truncate">{{ option.label }}</span>
              <Check v-if="modelValue === option.value" class="size-4 shrink-0 text-brand" aria-hidden="true" />
            </button>
            <p
              v-if="searchable && normalizedQuery && !visibleOptions.length"
              class="m-0 px-3 py-2 text-sm text-muted-foreground"
            >
              {{ searchEmptyText }}
            </p>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>
