<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { useTheme } from 'vuetify';
import {
  getThemeSemanticColors,
  markThemeChanging,
  persistThemePreferences,
  resolveThemeName,
  syncThemeAttribute,
  systemPrefersDark,
  usePanelThemePreferences,
  watchSystemTheme,
} from '@/theme';

const theme = useTheme();
const { preferences } = usePanelThemePreferences();
const prefersDark = ref(systemPrefersDark());
const resolvedTheme = computed(() => resolveThemeName(preferences.mode, prefersDark.value));

watch(
  resolvedTheme,
  (name) => {
    if (theme.global.name.value !== name) markThemeChanging();
    theme.global.name.value = name;
    syncThemeAttribute(name);
  },
  { immediate: true },
);

watch(
  preferences,
  (value) => {
    const themes = theme.themes.value;
    const lightColors = getThemeSemanticColors(value, 'light');
    const darkColors = getThemeSemanticColors(value, 'dark');
    if (themes.light) {
      themes.light.colors.primary = lightColors.primary;
      themes.light.colors['on-primary'] = lightColors.onPrimary;
      themes.light.colors['surface-variant'] = lightColors.surfaceVariant;
    }
    if (themes.dark) {
      themes.dark.colors.primary = darkColors.primary;
      themes.dark.colors['on-primary'] = darkColors.onPrimary;
      themes.dark.colors['surface-variant'] = darkColors.surfaceVariant;
    }
    persistThemePreferences(value);
  },
  { deep: true, immediate: true },
);

const stopSystemThemeWatch = watchSystemTheme((isDark) => {
  prefersDark.value = isDark;
});

onBeforeUnmount(stopSystemThemeWatch);
</script>

<template>
  <v-app>
    <RouterView />
  </v-app>
</template>
