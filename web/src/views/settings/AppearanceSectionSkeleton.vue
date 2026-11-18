<script setup lang="ts">
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import { useI18n } from '@/i18n';

const { t } = useI18n();
</script>

<template>
  <!-- Async AppearanceSection fallback: keep the SettingsSection/SettingsCard shell so the tab panel never flashes empty. -->
  <SettingsSection :title="t('appearance.title')" :description="t('appearance.description')">
    <SettingsCard><Skeleton class="h-6 w-full" /></SettingsCard>
    <SettingsCard><Skeleton class="h-6 w-full" /></SettingsCard>
    <SettingsCard><Skeleton class="h-32 w-full" /></SettingsCard>
  </SettingsSection>
</template>
