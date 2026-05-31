<script setup lang="ts">
import { watch } from 'vue';
import { useTheme } from 'vuetify';
import { isPanelThemeName, persistTheme, syncThemeAttribute } from '@/theme';

const theme = useTheme();

watch(
  () => theme.global.name.value,
  (name) => {
    if (!isPanelThemeName(name)) return;
    syncThemeAttribute(name);
    persistTheme(name);
  },
  { immediate: true },
);
</script>

<template>
  <v-app>
    <RouterView />
  </v-app>
</template>
