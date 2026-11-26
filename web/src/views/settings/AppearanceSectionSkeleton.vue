<script setup lang="ts">
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useI18n } from '@/i18n';

const { t } = useI18n();
</script>

<template>
  <!-- Async/data fallback for AppearanceSection: mirrors the real 4 SettingsCards with fixed labels. -->
  <SettingsSection :description="t('appearance.description')" aria-busy="true">
    <SettingsCard :title="t('appearance.theme.label')">
      <template #control>
        <Skeleton class="h-8 w-full sm:w-48" />
      </template>
    </SettingsCard>

    <SettingsCard :title="t('appearance.mica.label')" :description="t('appearance.mica.hint')">
      <template #control>
        <div class="flex items-center gap-2">
          <Skeleton class="h-5 w-10 rounded-pill" />
          <Skeleton class="h-4 w-10" />
        </div>
      </template>
    </SettingsCard>

    <SettingsCard :title="t('appearance.accent.label')">
      <div class="flex flex-wrap gap-2">
        <Skeleton v-for="item in 9" :key="item" class="size-8 rounded-pill" />
      </div>
    </SettingsCard>

    <SettingsCard :title="t('appearance.background.label')" :description="t('appearance.background.hint')">
      <div class="grid gap-4 lg:grid-cols-detail-wide">
        <Skeleton class="aspect-video w-full rounded-overlay" />
        <div class="grid content-start gap-3">
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-2 xl:grid-cols-3">
            <Skeleton v-for="item in 5" :key="item" class="aspect-video w-full rounded-control" />
          </div>
          <div class="flex flex-wrap gap-2">
            <Skeleton class="h-8 w-24" />
            <Skeleton class="h-8 w-24" />
          </div>
        </div>
      </div>
    </SettingsCard>
  </SettingsSection>
</template>
