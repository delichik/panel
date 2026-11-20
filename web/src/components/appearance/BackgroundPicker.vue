<script setup lang="ts">
import { ImageIcon, RotateCcw, Trash2 } from '@/icons';
import { computed, ref } from 'vue';
import Button from '@/components/ui/Button.vue';
import FileUploadButton from '@/components/ui/FileUploadButton.vue';
import LoadingOverlay from '@/components/ui/LoadingOverlay.vue';
import InfoBar from '@/components/patterns/InfoBar.vue';
import { validateBackgroundFile } from '@/design/appearance';
import { useI18n } from '@/i18n';
import { focusWithoutScroll } from '@/utils/focus';
import type { AppearanceBackground, PresetBackground, PresetBackgroundId } from '@/types/appearance';

const props = withDefaults(defineProps<{
  background: AppearanceBackground | null;
  previewUrl: string | null;
  presets: readonly PresetBackground[];
  /** 已上传图片的模糊缩略图（Mica 底图），用于「上传图片」选项的瓦片。 */
  thumbUrl?: string | null;
  uploading?: boolean;
  loadingPreview?: boolean;
  disabled?: boolean;
}>(), {
  thumbUrl: null,
  uploading: false,
  loadingPreview: false,
  disabled: false,
});

const emit = defineEmits<{
  'select-none': [];
  'select-preset': [value: PresetBackgroundId];
  'select-image': [];
  upload: [file: File];
  remove: [];
  reset: [];
}>();

const { t } = useI18n();

/** 与服务器管线一致：只接受 JPEG/PNG 且 ≤8MB，先在前端就地校验。 */
const backgroundAccept = 'image/jpeg,image/png';
const hasImage = computed(() => Boolean(props.background?.hasImage));
const localError = ref('');

type BackgroundOption = {
  key: 'none' | 'image' | PresetBackgroundId;
  label: string;
  gradient?: string | null;
  image?: boolean;
};

const options = computed<BackgroundOption[]>(() => {
  const list: BackgroundOption[] = [
    { key: 'none', label: t('appearance.background.none') },
    ...props.presets.map((preset) => ({ key: preset.id, label: t(`appearance.preset.${preset.id}`), gradient: preset.gradient })),
  ];
  // 已上传图片在切换为纯色/预设后仍保留在服务器，这里提供重新启用同一资产的入口。
  if (hasImage.value) list.push({ key: 'image', label: t('appearance.background.image'), image: true });
  return list;
});

const selectedIndex = computed(() => {
  const background = props.background;
  if (!background) return 0;
  if (background.kind === 'preset') {
    const index = props.presets.findIndex((preset) => preset.id === background.presetId);
    return index >= 0 ? index + 1 : 0;
  }
  if (background.kind === 'image' && hasImage.value) {
    const index = options.value.findIndex((option) => option.key === 'image');
    return index >= 0 ? index : 0;
  }
  return 0;
});
const selectedOption = computed(() => options.value[selectedIndex.value] ?? options.value[0]);
const showsImagePreview = computed(() => props.background?.kind === 'image' && Boolean(props.previewUrl));

function focusOption(index: number) {
  const radios = typeof document === 'undefined'
    ? []
    : Array.from(document.querySelectorAll<HTMLButtonElement>('[data-appearance-background-option]'));
  focusWithoutScroll(radios[index]);
}

function choose(index: number) {
  if (props.disabled) return;
  const option = options.value[index];
  if (!option) return;
  if (option.key === 'none') emit('select-none');
  else if (option.key === 'image') emit('select-image');
  else emit('select-preset', option.key);
}

function onOptionKeydown(event: KeyboardEvent, index: number) {
  if (props.disabled) return;
  const total = options.value.length;
  let next = -1;
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (index + 1) % total;
  else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (index - 1 + total) % total;
  else if (event.key === 'Home') next = 0;
  else if (event.key === 'End') next = total - 1;
  if (next < 0) return;
  event.preventDefault();
  choose(next);
  focusOption(next);
}

function onFileChange(value: File | File[]) {
  localError.value = '';
  const file = Array.isArray(value) ? value[0] : value;
  if (!file) return;
  const invalid = validateBackgroundFile(file);
  if (invalid) {
    localError.value = t(`appearance.errors.${invalid}`);
    return;
  }
  emit('upload', file);
}
</script>

<template>
  <div class="grid gap-4">
    <div class="grid gap-4 lg:grid-cols-detail-wide">
      <!-- 预览：上传图片用 store 提供的 blob URL；预设用与服务端/主题层一致的渐变；纯色由 Mica 底色承担。 -->
      <div class="relative aspect-video w-full overflow-hidden rounded-overlay border border-stroke bg-control">
        <img
          v-if="showsImagePreview"
          :src="previewUrl!"
          :alt="t('appearance.background.preview')"
          class="size-full object-cover"
        />
        <div
          v-else-if="selectedOption?.gradient"
          class="size-full"
          role="img"
          :aria-label="t('appearance.background.preview')"
          :style="{ backgroundImage: selectedOption.gradient }"
        />
        <div v-else-if="selectedOption?.image && thumbUrl" class="size-full bg-cover bg-center" role="img" :aria-label="t('appearance.background.preview')" :style="{ backgroundImage: `url(${thumbUrl})` }" />
        <div v-else class="grid size-full place-items-center p-4 text-center text-sm text-muted-foreground">
          {{ t('appearance.background.none') }}
        </div>
        <LoadingOverlay v-if="uploading" :label="t('appearance.background.uploading')" />
        <LoadingOverlay v-else-if="loadingPreview" :label="t('appearance.background.preview')" />
      </div>

      <div class="grid content-start gap-3">
        <div role="radiogroup" :aria-label="t('appearance.background.label')" class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-2 xl:grid-cols-3">
          <button
            v-for="(option, index) in options"
            :key="option.key"
            type="button"
            role="radio"
            data-appearance-background-option
            :aria-checked="selectedIndex === index"
            :aria-label="option.label"
            :tabindex="selectedIndex === index ? 0 : -1"
            :disabled="disabled"
            class="motion-control relative aspect-video overflow-hidden surface-control-line text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-45"
            :class="[
              selectedIndex === index ? 'ring-2 ring-ring' : 'hover:border-foreground',
              option.gradient ? undefined : 'bg-muted text-foreground',
            ]"
            :style="option.gradient ? { backgroundImage: option.gradient } : (option.image && thumbUrl ? { backgroundImage: `url(${thumbUrl})`, backgroundSize: 'cover', backgroundPosition: 'center' } : undefined)"
            @click="choose(index)"
            @keydown="onOptionKeydown($event, index)"
          >
            <ImageIcon v-if="option.image" class="absolute left-2 top-2 size-4" aria-hidden="true" />
            <span class="absolute inset-x-0 bottom-0 truncate bg-card-solid px-2 py-1 text-xs font-medium text-foreground">{{ option.label }}</span>
          </button>
        </div>

        <div class="flex flex-wrap gap-2">
          <FileUploadButton
            :accept="backgroundAccept"
            variant="secondary"
            size="sm"
            :loading="uploading"
            :disabled="disabled || uploading"
            :label="hasImage ? t('appearance.background.replace') : t('appearance.background.upload')"
            @change="onFileChange"
          />
          <Button v-if="hasImage" variant="danger" size="sm" :disabled="disabled || uploading" @click="emit('remove')">
            <Trash2 />{{ t('appearance.background.remove') }}
          </Button>
          <Button variant="ghost" size="sm" :disabled="disabled || uploading" @click="emit('reset')">
            <RotateCcw />{{ t('appearance.reset') }}
          </Button>
        </div>
        <InfoBar v-if="localError" tone="danger">{{ localError }}</InfoBar>
      </div>
    </div>
  </div>
</template>
