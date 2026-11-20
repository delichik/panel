<script setup lang="ts">
import { RefreshCcw } from '@/icons';
import ProgressRing from '@/components/ui/ProgressRing.vue';
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import AccentPalette from '@/components/appearance/AccentPalette.vue';
import BackgroundPicker from '@/components/appearance/BackgroundPicker.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import Button from '@/components/ui/Button.vue';
import Select from '@/components/ui/Select.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Switch from '@/components/ui/Switch.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import { useAppearance } from '@/design/appearance';
import { useI18n } from '@/i18n';
import type { AccentId, PresetBackgroundId, ThemeMode } from '@/types/appearance';

const { t } = useI18n();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

/**
 * 外观状态全部来自共享 store（与 AppShell 的外观 Flyout 消费同一实例）。
 * store 负责乐观更新/回滚/缓存与 DOM 应用；本分区只编排界面与反馈。
 */
const {
  appearance,
  ready,
  accentPresets,
  presetBackgrounds,
  previewUrl,
  load,
  setThemeMode,
  setMica,
  setAccent,
  setBackground,
  enableImageBackground,
  uploadBackground,
  removeBackground,
  reset,
  loadPreview,
  clearPreview,
} = useAppearance();

/** 单个进行中的操作；乐观更新期间禁用其余控件，避免并发覆盖。 */
const pending = ref<'' | 'save' | 'upload' | 'remove' | 'reset'>('');
const previewPending = ref(false);
const busy = computed(() => pending.value !== '');
const loadFailed = computed(() => ready.value && !appearance.value);

const themeOptions = computed(() => (['system', 'light', 'dark'] as const).map((value) => ({
  label: t(`appearance.theme.${value}`),
  value,
})));

let disposed = false;

async function syncPreview() {
  if (disposed) return;
  const background = appearance.value?.background;
  if (!background || background.kind !== 'image' || !background.hasImage) {
    clearPreview();
    return;
  }
  previewPending.value = true;
  try {
    const url = await loadPreview();
    // 组件在预览请求期间卸载时立即释放 blob URL，避免泄漏。
    if (disposed || !url) clearPreview();
  } catch {
    // 预览拉取失败不影响主题/Mica（缩略图仍生效），保持占位而不是打断设置页。
    clearPreview();
  } finally {
    previewPending.value = false;
  }
}

async function ensureLoaded() {
  if (!ready.value) await load();
  await syncPreview();
}

async function reload() {
  await load();
  await syncPreview();
}

/**
 * 统一失败反馈：store 回滚并抛异常，由这里给出全局阻塞错误 Dialog（带日志入口）。
 * 成功时界面因乐观更新已即时变化，只有上传/移除/恢复默认这类有明确结果的动作补 success toast。
 */
async function runAction(
  kind: 'save' | 'upload' | 'remove' | 'reset',
  failureKey: string,
  action: () => Promise<void>,
  successKey?: string,
) {
  if (pending.value) return;
  pending.value = kind;
  try {
    await action();
    if (successKey) notifySuccess(t(successKey));
  } catch (error) {
    notifyError(t(failureKey), error);
  } finally {
    pending.value = '';
  }
}

function changeTheme(value: string) {
  const mode = value as ThemeMode;
  if (!appearance.value || appearance.value.themeMode === mode) return;
  void runAction('save', 'appearance.errors.save', () => setThemeMode(mode));
}

function toggleMica(value: boolean) {
  if (!appearance.value || appearance.value.mica === value) return;
  void runAction('save', 'appearance.errors.save', () => setMica(value));
}

function changeAccent(value: AccentId) {
  if (!appearance.value || appearance.value.accent === value) return;
  void runAction('save', 'appearance.errors.save', () => setAccent(value));
}

function selectSolid() {
  if (!appearance.value) return;
  void runAction('save', 'appearance.errors.save', async () => {
    await setBackground('none', null);
    clearPreview();
  });
}

function selectPreset(value: PresetBackgroundId) {
  if (!appearance.value) return;
  void runAction('save', 'appearance.errors.save', async () => {
    await setBackground('preset', value);
    clearPreview();
  });
}

function selectImage() {
  if (!appearance.value) return;
  void runAction('save', 'appearance.errors.save', async () => {
    await enableImageBackground();
    await syncPreview();
  });
}

function uploadImage(file: File) {
  void runAction('upload', 'appearance.errors.upload', async () => {
    await uploadBackground(file);
    await syncPreview();
  }, 'appearance.saved');
}

function removeImage() {
  void runAction('remove', 'appearance.errors.upload', async () => {
    await removeBackground();
    clearPreview();
  }, 'appearance.saved');
}

function resetAppearance() {
  void runAction('reset', 'appearance.errors.save', async () => {
    await reset();
    clearPreview();
  }, 'appearance.saved');
}

onMounted(() => {
  void ensureLoaded();
});

onBeforeUnmount(() => {
  disposed = true;
  clearPreview();
});

defineExpose({ reload });
</script>

<template>
  <SettingsSection :description="t('appearance.description')">
    <template #description>
      <span v-if="busy" class="inline-flex items-center gap-1.5 text-xs text-muted-foreground" aria-live="polite">
        <ProgressRing :size="14" aria-hidden="true" />
        {{ t('common.loading') }}
      </span>
    </template>

    <SettingsCard v-if="loadFailed">
      <InfoBar tone="danger">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <span>{{ t('appearance.errors.load') }}</span>
          <Button size="sm" variant="secondary" :loading="!ready" @click="reload">
            <RefreshCcw />{{ t('common.retry') }}
          </Button>
        </div>
      </InfoBar>
    </SettingsCard>

    <template v-else-if="!appearance">
      <SettingsCard><Skeleton class="h-6 w-full" /></SettingsCard>
      <SettingsCard><Skeleton class="h-6 w-full" /></SettingsCard>
      <SettingsCard><Skeleton class="h-32 w-full" /></SettingsCard>
    </template>

    <template v-else>
      <!-- 主题：自动 / 浅色 / 深色 -->
      <SettingsCard :title="t('appearance.theme.label')">
        <template #control>
          <Select
            id="appearance-theme-mode"
            class="w-full sm:w-48"
            :model-value="appearance.themeMode"
            :options="themeOptions"
            :disabled="busy"
            :aria-label="t('appearance.theme.label')"
            @update:model-value="changeTheme"
          />
        </template>
      </SettingsCard>

      <!-- Mica 开关 -->
      <SettingsCard :title="t('appearance.mica.label')" :description="t('appearance.mica.hint')">
        <template #control>
          <div class="flex items-center gap-2">
            <Switch
              id="appearance-mica"
              :model-value="appearance.mica"
              :label="t('appearance.mica.label')"
              :disabled="busy"
              @update:model-value="toggleMica"
            />
            <span class="text-sm text-muted-foreground">{{ appearance.mica ? t('appearance.mica.on') : t('appearance.mica.off') }}</span>
          </div>
        </template>
      </SettingsCard>

      <!-- 强调色 9 预设 -->
      <SettingsCard :title="t('appearance.accent.label')">
        <AccentPalette
          :model-value="appearance.accent"
          :label="t('appearance.accent.label')"
          :presets="accentPresets"
          :disabled="busy"
          @update:model-value="changeAccent"
        />
      </SettingsCard>

      <!-- 背景：纯色 / 预设 / 上传图片 / 移除 / 恢复默认 -->
      <SettingsCard :title="t('appearance.background.label')" :description="t('appearance.background.hint')">
        <BackgroundPicker
          :background="appearance.background"
          :preview-url="previewUrl"
          :presets="presetBackgrounds"
          :thumb-url="appearance.thumbDataUrl"
          :uploading="pending === 'upload'"
          :loading-preview="previewPending"
          :disabled="busy"
          @select-none="selectSolid"
          @select-preset="selectPreset"
          @select-image="selectImage"
          @upload="uploadImage"
          @remove="removeImage"
          @reset="resetAppearance"
        />
      </SettingsCard>
    </template>
  </SettingsSection>
</template>
