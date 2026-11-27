<script setup lang="ts">
import { Check } from '@/icons';
import { computed } from 'vue';
import StatusBadge from '../ui/StatusBadge.vue';

type ServerOption = {
  id: string;
  name: string;
  description?: string;
  status?: string;
  capabilities?: string[];
};

const props = withDefaults(defineProps<{
  modelValue: string[];
  servers: ServerOption[];
  disabled?: boolean;
  disabledIds?: string[];
  disabledReasons?: Record<string, string>;
  label?: string;
  /**
   * list（默认）：每台服务器占一行的全宽列表，适合配置流中需要展示能力标签的场景。
   * compact：紧凑网格瓦片（checkbox + 名称 + 状态/地址），适合设施配置页等节点较多的场景。
   */
  variant?: 'list' | 'compact';
}>(), {
  disabledIds: () => [],
  disabledReasons: () => ({}),
  variant: 'list',
});

const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>();

const selected = computed(() => new Set(props.modelValue));
const blocked = computed(() => new Set(props.disabledIds));

const gridClass = computed(() => (props.variant === 'compact'
  ? 'grid grid-cols-1 gap-2 motion-stagger sm:grid-cols-2 xl:grid-cols-3'
  : 'grid gap-2 motion-stagger'));

function toggle(id: string) {
  if (props.disabled || blocked.value.has(id)) return;
  const next = new Set(props.modelValue);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  emit('update:modelValue', Array.from(next));
}
</script>

<template>
  <!-- WinUI 可选中行：按钮 + aria-pressed 切换语义（外部 views 测试依赖）+ 视觉 checkbox。
       选中态只加 accent-selected 填充，保留 aria-pressed，不再自造 accent 边框或文字变色。
       图标：16px 控件内尺寸（渲染 vendored 的 Fluent 24px regular 母版；24px 母版缩放策略本轮保留），
       大图标场景用 20px（size-5）。 -->
  <section :class="gridClass" :aria-label="label">
    <template v-if="variant === 'compact'">
      <button
        v-for="server in servers"
        :key="server.id"
        type="button"
        :disabled="disabled || blocked.has(server.id)"
        :aria-pressed="selected.has(server.id)"
        class="motion-list-item flex min-w-0 w-full items-center gap-2 surface-control px-2 py-2 text-left transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:cursor-not-allowed disabled:opacity-50 aria-pressed:[--panel-hover:var(--accent-selected-bg)] aria-pressed:bg-accent-selected"
        @click="toggle(server.id)"
      >
        <span
          class="grid size-5 shrink-0 place-items-center rounded-control border transition-colors"
          :class="selected.has(server.id) ? 'border-primary bg-primary text-primary-foreground' : 'border-input bg-control text-transparent'"
          aria-hidden="true"
        >
          <Check v-if="selected.has(server.id)" class="size-4" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="flex min-w-0 items-center justify-between gap-2">
            <span class="truncate text-body-strong text-foreground">{{ server.name }}</span>
            <StatusBadge v-if="server.status" :status="server.status" domain="server" class="shrink-0" />
          </span>
          <span v-if="server.description" class="mt-0.5 block truncate text-caption text-muted-foreground">{{ server.description }}</span>
          <span v-if="server.capabilities?.length || disabledReasons[server.id]" class="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
            <span v-for="capability in server.capabilities" :key="capability" class="rounded-pill border border-stroke bg-layer px-1.5 py-0.5 text-caption text-muted-foreground">
              {{ capability }}
            </span>
            <span v-if="disabledReasons[server.id]" class="truncate text-caption text-danger">{{ disabledReasons[server.id] }}</span>
          </span>
        </span>
      </button>
    </template>
    <template v-else>
      <button
        v-for="server in servers"
        :key="server.id"
        type="button"
        :disabled="disabled || blocked.has(server.id)"
        :aria-pressed="selected.has(server.id)"
        class="motion-list-item grid min-h-20 w-full gap-2 rounded-overlay border border-stroke bg-control p-3 text-left transition-colors hover:bg-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset disabled:cursor-not-allowed disabled:opacity-50 aria-pressed:[--panel-hover:var(--accent-selected-bg)] aria-pressed:bg-accent-selected"
        @click="toggle(server.id)"
      >
        <span class="flex min-w-0 items-start gap-3">
          <span
            class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-control border transition-colors"
            :class="selected.has(server.id) ? 'border-primary bg-primary text-primary-foreground' : 'border-input bg-control text-transparent'"
            aria-hidden="true"
          >
            <Check v-if="selected.has(server.id)" class="size-4" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="flex min-w-0 items-start justify-between gap-2">
              <span class="truncate text-body-strong text-foreground">{{ server.name }}</span>
              <StatusBadge v-if="server.status" :status="server.status" domain="server" />
            </span>
            <span v-if="server.description" class="mt-1 block line-clamp-2 text-caption text-muted-foreground">{{ server.description }}</span>
            <span v-if="server.capabilities?.length || disabledReasons[server.id]" class="mt-2 flex flex-wrap gap-1.5">
              <span v-for="capability in server.capabilities" :key="capability" class="rounded-pill border border-stroke bg-layer px-2 py-0.5 text-caption text-muted-foreground">
                {{ capability }}
              </span>
              <span v-if="disabledReasons[server.id]" class="text-caption text-danger">{{ disabledReasons[server.id] }}</span>
            </span>
          </span>
        </span>
      </button>
    </template>
  </section>
</template>
