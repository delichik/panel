<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useDisplay } from 'vuetify';
import { useAuthStore } from '@/stores/auth';
import { tasksApi } from '@/api/tasks';
import { systemApi } from '@/api/system';
import type { SystemVersionDto, TaskDto } from '@/types/api';
import { useI18n } from '@/i18n';
import {
  PANEL_THEME_PRESETS,
  PANEL_THEME_PRESET_NAMES,
  type PanelThemeMode,
  type PanelThemeName,
  type PanelThemePreset,
  usePanelThemePreferences,
} from '@/theme';

interface NavItem {
  to?: string;
  icon?: string;
  title: string;
  value: string;
  disabled?: boolean;
}

interface NavGroup {
  key: string;
  icon?: string;
  title?: string;
  items: NavItem[];
}

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();
const display = useDisplay();
const { t, translateTaskStage, translateTaskStatus, translateTaskType } = useI18n();
const {
  preferences: themePreferences,
  setMode: setThemeMode,
  setSharedPreset,
  setPreset,
  resetPresets,
} = usePanelThemePreferences();

const isCompactLayout = computed(() => display.mdAndDown.value);
const pageTitle = computed(() => t(String(route.meta.titleKey || 'app.name')));
const drawerOpen = ref(true);
const themeModeOptions = computed(() => [
  { value: 'system' as const, label: t('layout.theme.system'), icon: 'mdi-theme-light-dark' },
  { value: 'light' as const, label: t('layout.theme.light'), icon: 'mdi-weather-sunny' },
  { value: 'dark' as const, label: t('layout.theme.dark'), icon: 'mdi-weather-night' },
]);
const themeButtonIcon = computed(() => themeModeOptions.value.find((item) => item.value === themePreferences.mode)?.icon ?? 'mdi-palette-outline');
const themePresetOptions = computed(() => PANEL_THEME_PRESET_NAMES.map((value) => ({
  value,
  label: t(`layout.theme.presets.${value}`),
})));
const navGroups = computed<NavGroup[]>(() => [
  {
    key: 'overview',
    items: [{ to: '/overview', icon: 'mdi-monitor', title: t('layout.nav.overview'), value: 'overview' }],
  },
  {
    key: 'servers',
    icon: 'mdi-server',
    title: t('layout.nav.servers'),
    items: [
      { to: '/servers', title: t('layout.nav.node'), value: 'node' },
      { to: '/credentials', title: t('layout.nav.credentials'), value: 'credentials' },
      { to: '/servers/firewall', title: t('layout.nav.firewall'), value: 'server-firewall' },
      { to: '/servers/packages', title: t('layout.nav.systemPackages'), value: 'system-packages' },
    ],
  },
  {
    key: 'containerization',
    icon: 'mdi-docker',
    title: t('layout.nav.containerization'),
    items: [
      { to: '/containerization/applications', title: t('layout.nav.applications'), value: 'applications' },
      { to: '/containerization/facility-apps', title: t('layout.nav.facilityApps'), value: 'facility-apps' },
      { to: '/containerization/containers', title: t('layout.nav.containers'), value: 'containers' },
      { to: '/containerization/images', title: t('layout.nav.images'), value: 'images' },
      { to: '/containerization/networks', title: t('layout.nav.networks'), value: 'networks' },
      { to: '/containerization/volumes', title: t('layout.nav.volumes'), value: 'volumes' },
    ],
  },
  {
    key: 'dns',
    icon: 'mdi-dns-outline',
    title: t('layout.nav.dns'),
    items: [
      { to: '/dns/domains', icon: 'mdi-web', title: t('layout.nav.domains'), value: 'dns-domains' },
    ],
  },
  {
    key: 'certificates',
    icon: 'mdi-certificate-outline',
    title: t('layout.nav.certificates'),
    items: [
      { to: '/certificates/domains', title: t('layout.nav.domainCertificates'), value: 'certificates-domains' },
      { to: '/certificates/self-signed', title: t('layout.nav.selfSignedCertificates'), value: 'certificates-self-signed' },
      { to: '/certificates/keys', title: t('layout.nav.keys'), value: 'certificates-keys' },
    ],
  },
  {
    key: 'tasks',
    items: [{ to: '/tasks', icon: 'mdi-clipboard-list', title: t('layout.nav.taskCenter'), value: 'tasks' }],
  },
  {
    key: 'settings',
    icon: 'mdi-cog',
    title: t('layout.nav.settings'),
    items: [
      { to: '/settings/general', title: t('layout.nav.settingsGeneral'), value: 'settings-general' },
      { to: '/settings/security', title: t('layout.nav.settingsSecurity'), value: 'settings-security' },
      { to: '/settings/certificates', title: t('layout.nav.settingsCertificates'), value: 'settings-certificates' },
      { to: '/settings/system-certificates', title: t('layout.nav.settingsSystemCertificates'), value: 'settings-system-certificates' },
      { to: '/settings/system', title: t('layout.nav.settingsSystem'), value: 'settings-system' },
    ],
  },
]);

function updateThemeMode(value: PanelThemeMode | null) {
  if (value) setThemeMode(value);
}

function presetPreviewStyle(preset: PanelThemePreset, mode: PanelThemeName) {
  const colors = PANEL_THEME_PRESETS[preset][mode];
  return {
    '--preset-primary': colors.primary,
    '--preset-on-primary': colors.onPrimary,
    '--preset-surface-variant': colors.surfaceVariant,
  };
}

watch(isCompactLayout, (compact) => {
  drawerOpen.value = !compact;
}, { immediate: true });

watch(() => route.fullPath, () => {
  if (isCompactLayout.value) drawerOpen.value = false;
});

const activeTasks = ref<TaskDto[]>([]);
const versionInfo = ref<SystemVersionDto | null>(null);
const taskIndex = ref(0);
let taskTimer: number | undefined;
let rotateTimer: number | undefined;
let versionTimer: number | undefined;

const currentTask = computed(() => activeTasks.value[taskIndex.value % Math.max(activeTasks.value.length, 1)]);
const isDevChannel = computed(() => versionInfo.value?.channel === 'dev');

async function loadActiveTasks() {
  try {
    const [running, queued] = await Promise.all([
      tasksApi.list({ status: 'running', pageSize: 10 }),
      tasksApi.list({ status: 'queued', pageSize: 10 }),
    ]);
    activeTasks.value = [...running.items, ...queued.items];
    if (taskIndex.value >= activeTasks.value.length) taskIndex.value = 0;
  } catch {
    activeTasks.value = [];
  }
}

async function loadVersionInfo() {
  try {
    versionInfo.value = await systemApi.version();
  } catch {
    versionInfo.value = null;
  }
}

async function logout() {
  await auth.logout();
  await router.push('/login');
}

onMounted(() => {
  void loadActiveTasks();
  void loadVersionInfo();
  taskTimer = window.setInterval(loadActiveTasks, 8000);
  versionTimer = window.setInterval(loadVersionInfo, 30 * 60 * 1000);
  rotateTimer = window.setInterval(() => {
    if (activeTasks.value.length > 1) taskIndex.value = (taskIndex.value + 1) % activeTasks.value.length;
  }, 3500);
});

onBeforeUnmount(() => {
  if (taskTimer) window.clearInterval(taskTimer);
  if (rotateTimer) window.clearInterval(rotateTimer);
  if (versionTimer) window.clearInterval(versionTimer);
});
</script>

<template>
  <v-layout class="fill-height">
    <v-navigation-drawer
      v-model="drawerOpen"
      width="280"
      :permanent="!isCompactLayout"
      :temporary="isCompactLayout"
      floating
      class="app-drawer"
    >
      <div class="brand">
        <div class="brand-mark">LP</div>
        <div>
          <div class="brand-title">{{ t('app.name') }}</div>
          <div class="brand-subtitle">{{ t('app.subtitle') }}</div>
        </div>
      </div>

      <v-list nav class="app-drawer-nav py-4 px-3">
        <template v-for="group in navGroups" :key="group.key">
          <v-list-group v-if="group.items.length > 1" :value="group.key">
            <template #activator="{ props }">
              <v-list-item v-bind="props" :prepend-icon="group.icon" :title="group.title" />
            </template>
            <v-list-item
              v-for="item in group.items"
              :key="item.value"
              :to="item.to"
              :prepend-icon="item.icon"
              :title="item.title"
              :value="item.value"
              :disabled="item.disabled"
              class="pl-8"
            />
          </v-list-group>
          <v-list-item v-else :to="group.items[0].to" :prepend-icon="group.items[0].icon" :title="group.items[0].title" :value="group.items[0].value" />
        </template>
      </v-list>
    </v-navigation-drawer>

    <v-main class="app-main">
      <div class="main-content">
        <header class="app-header panel">
          <div class="app-header-title min-width-0">
            <v-btn
              v-if="isCompactLayout"
              icon
              size="small"
              variant="text"
              class="utility-btn nav-toggle"
              :aria-label="t('layout.nav.openNavigation')"
              @click="drawerOpen = true"
            >
              <v-icon>mdi-menu</v-icon>
            </v-btn>
            <h1 class="app-title text-truncate">{{ pageTitle }}</h1>
            <v-chip
              v-if="isDevChannel"
              color="info"
              variant="tonal"
              size="x-small"
              class="dev-channel-chip"
              :title="t('layout.developmentChannelDetail', { version: versionInfo?.version })"
            >
              {{ t('layout.developmentChannel') }}
            </v-chip>
          </div>

          <Transition name="task-slide" mode="out-in">
            <div v-if="currentTask" :key="currentTask.id" class="task-ticker">
              <v-icon size="16" color="primary">mdi-progress-clock</v-icon>
              <span class="task-line">
                {{ translateTaskType(currentTask.type) }}
                <span class="task-stage">{{ translateTaskStatus(currentTask.status) }} - {{ currentTask.stage ? translateTaskStage(currentTask.stage) : t('layout.taskTicker.queuedStage') }}</span>
              </span>
            </div>
          </Transition>

          <div class="app-header-actions">
            <v-chip
              v-if="versionInfo?.updateAvailable"
              color="warning"
              variant="tonal"
              size="small"
              prepend-icon="mdi-update"
              :title="t('layout.updateAvailableDetail', { current: versionInfo.version, latest: versionInfo.latestVersion })"
            >
              {{ t('layout.updateAvailable', { version: versionInfo.latestVersion }) }}
            </v-chip>

            <div class="header-utility-strip">
              <v-menu location="bottom end" :close-on-content-click="false">
                <template #activator="{ props }">
                  <v-btn
                    v-bind="props"
                    icon
                    size="small"
                    variant="text"
                    class="utility-btn"
                    :aria-label="t('layout.theme.open')"
                  >
                    <v-icon>{{ themeButtonIcon }}</v-icon>
                  </v-btn>
                </template>
                <v-card class="theme-menu-card" elevation="8">
                  <v-card-title class="theme-menu-title">
                    <v-icon size="18">mdi-palette-outline</v-icon>
                    {{ t('layout.theme.title') }}
                  </v-card-title>
                  <v-card-text class="theme-menu-content">
                    <div class="theme-field-label">{{ t('layout.theme.mode') }}</div>
                    <v-btn-toggle
                      :model-value="themePreferences.mode"
                      mandatory
                      divided
                      density="compact"
                      variant="outlined"
                      class="theme-mode-toggle"
                      @update:model-value="updateThemeMode"
                    >
                      <v-btn
                        v-for="option in themeModeOptions"
                        :key="option.value"
                        :value="option.value"
                        :prepend-icon="option.icon"
                        size="small"
                        class="text-none"
                      >
                        {{ option.label }}
                      </v-btn>
                    </v-btn-toggle>
                    <div v-if="themePreferences.mode === 'system'" class="theme-mode-hint">
                      {{ t('layout.theme.systemHint') }}
                    </div>

                    <v-divider />

                    <v-switch
                      :model-value="themePreferences.sharedPreset"
                      :label="t('layout.theme.sharedPreset')"
                      color="primary"
                      density="compact"
                      hide-details
                      @update:model-value="setSharedPreset(Boolean($event))"
                    />

                    <div v-if="themePreferences.sharedPreset" class="theme-preset-section">
                      <div class="theme-field-label">{{ t('layout.theme.preset') }}</div>
                      <div class="theme-preset-grid">
                        <button
                          v-for="option in themePresetOptions"
                          :key="option.value"
                          type="button"
                          class="theme-preset-option"
                          :class="{ 'theme-preset-option--active': themePreferences.preset === option.value }"
                          :aria-pressed="themePreferences.preset === option.value"
                          @click="setPreset('shared', option.value)"
                        >
                          <span class="theme-preset-preview theme-preset-preview--split">
                            <span class="theme-preset-half" :style="presetPreviewStyle(option.value, 'light')">
                              <span class="theme-preset-primary-dot"><v-icon size="11">mdi-check</v-icon></span>
                            </span>
                            <span class="theme-preset-half" :style="presetPreviewStyle(option.value, 'dark')">
                              <span class="theme-preset-primary-dot"><v-icon size="11">mdi-check</v-icon></span>
                            </span>
                          </span>
                          <span>{{ option.label }}</span>
                          <v-icon v-if="themePreferences.preset === option.value" size="15" color="primary">mdi-check-circle</v-icon>
                        </button>
                      </div>
                    </div>
                    <div v-else class="theme-preset-sections">
                      <div
                        v-for="mode in (['light', 'dark'] as const)"
                        :key="mode"
                        class="theme-preset-section"
                      >
                        <div class="theme-field-label">
                          {{ mode === 'light' ? t('layout.theme.lightPreset') : t('layout.theme.darkPreset') }}
                        </div>
                        <div class="theme-preset-grid">
                          <button
                            v-for="option in themePresetOptions"
                            :key="option.value"
                            type="button"
                            class="theme-preset-option"
                            :class="{ 'theme-preset-option--active': themePreferences[`${mode}Preset`] === option.value }"
                            :aria-pressed="themePreferences[`${mode}Preset`] === option.value"
                            @click="setPreset(mode, option.value)"
                          >
                            <span class="theme-preset-preview" :style="presetPreviewStyle(option.value, mode)">
                              <span class="theme-preset-primary-dot"><v-icon size="11">mdi-check</v-icon></span>
                            </span>
                            <span>{{ option.label }}</span>
                            <v-icon v-if="themePreferences[`${mode}Preset`] === option.value" size="15" color="primary">mdi-check-circle</v-icon>
                          </button>
                        </div>
                      </div>
                    </div>

                    <v-btn
                      variant="text"
                      size="small"
                      prepend-icon="mdi-restore"
                      class="text-none align-self-start"
                      @click="resetPresets"
                    >
                      {{ t('layout.theme.resetPreset') }}
                    </v-btn>
                  </v-card-text>
                </v-card>
              </v-menu>

              <v-menu location="bottom end">
                <template #activator="{ props }">
                  <v-btn
                    v-bind="props"
                    icon
                    size="small"
                    variant="text"
                    class="utility-btn"
                    :aria-label="t('layout.userMenu.open')"
                  >
                    <v-icon>mdi-account-circle-outline</v-icon>
                  </v-btn>
                </template>
                <v-list density="compact" min-width="220">
                  <v-list-item prepend-icon="mdi-account-circle-outline">
                    <v-list-item-title class="user-menu-name">{{ auth.username || t('layout.userMenu.unknownUser') }}</v-list-item-title>
                  </v-list-item>
                  <v-divider class="my-1" />
                  <v-list-item prepend-icon="mdi-logout" :title="t('layout.logout')" @click="logout" />
                </v-list>
              </v-menu>
            </div>
          </div>
        </header>
        <RouterView />
      </div>
    </v-main>
  </v-layout>
</template>

<style scoped>
:deep(.v-navigation-drawer) {
  background-color: transparent !important;
  border: none !important;
}

:deep(.v-navigation-drawer__content) {
  margin: 16px;
  height: calc(100dvh - 32px) !important;
  border-radius: 8px !important;
  border: 1px solid var(--lp-border) !important;
  background: var(--lp-surface) !important;
  box-shadow: var(--lp-shadow-sm) !important;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: background-color 0.25s ease, border-color 0.25s ease, box-shadow 0.25s ease;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 64px;
  margin-bottom: 14px;
  padding: 12px 16px;
}

.app-header:hover {
  border-color: var(--lp-border);
  box-shadow: var(--lp-shadow-sm);
}

.app-header-title {
  display: flex;
  flex: 1 1 auto;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.app-title {
  margin: 0;
  color: var(--lp-text);
  font-size: 1.28rem;
  font-weight: 760;
  line-height: 1.15;
  letter-spacing: 0;
  text-wrap: balance;
}

.dev-channel-chip {
  flex: 0 0 auto;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.app-header-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  min-width: 0;
}

.header-utility-strip {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  min-height: 40px;
  padding: 0;
  border: 0;
  background: transparent;
}

.utility-btn {
  color: var(--lp-text-muted);
}

:deep(.v-main) {
  min-height: 0;
  overflow: hidden;
  transition: background-color 0.25s ease;
}

:deep(.v-main__wrap) {
  display: flex;
  min-height: 0;
}

.app-main {
  min-height: 0;
}

.app-main :deep(.v-main__wrap) {
  flex: 1 1 auto;
}

.main-content {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  width: min(100%, 1640px);
  height: 100%;
  min-height: 0;
  margin: 0 auto;
  padding: clamp(16px, 2vw, 28px);
  transition: background-color 0.25s ease;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 72px;
  padding: 0 18px;
  border-bottom: 1px solid var(--lp-border);
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: linear-gradient(135deg, rgb(var(--v-theme-primary)) 0%, rgba(var(--v-theme-primary), 0.72) 100%);
  color: #ffffff;
  font-weight: 700;
  box-shadow: 0 2px 8px rgba(var(--v-theme-primary), 0.25);
}

.brand-title {
  font-weight: 700;
  letter-spacing: 0;
}

.brand-subtitle {
  color: var(--lp-text-muted);
  font-size: 11px;
  font-weight: 500;
}

.app-drawer-nav {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.task-ticker {
  display: inline-flex;
  flex: 0 1 380px;
  align-items: center;
  gap: 8px;
  min-width: 0;
  max-width: 380px;
  overflow: hidden;
  padding: 8px 10px;
  border: 1px solid var(--lp-border);
  border-radius: 8px;
  background: var(--lp-surface);
  font-size: 13px;
}

.task-line {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.82rem;
  font-weight: 600;
}

.task-stage {
  margin-left: 8px;
  color: var(--lp-text-muted);
}

.main-content > :last-child {
  min-height: 0;
}

.task-slide-enter-active,
.task-slide-leave-active {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.task-slide-enter-from,
.task-slide-leave-to {
  transform: translateY(6px);
  opacity: 0;
}

/* Navigation items premium aesthetics */
:deep(.v-list-item) {
  border-radius: 8px !important;
  margin: 2px 0 !important;
  padding: 8px 12px !important;
  font-size: 0.9rem !important;
  font-weight: 500 !important;
  color: var(--lp-text-muted) !important;
  transition: background-color 0.2s ease, color 0.2s ease !important;
}

:deep(.v-list-item:hover) {
  color: var(--lp-text) !important;
  background-color: rgba(var(--v-theme-on-surface), 0.04) !important;
}

:deep(.v-list-item--active) {
  color: rgb(var(--v-theme-primary)) !important;
  background-color: rgba(var(--v-theme-primary), 0.06) !important;
  font-weight: 600 !important;
}

:deep(.v-list-item--active::before) {
  content: '';
  position: absolute;
  left: 0;
  top: 10px;
  bottom: 10px;
  width: 3px;
  background-color: rgb(var(--v-theme-primary));
  border-radius: 99px;
}

.min-width-0 {
  min-width: 0;
}

@media (max-width: 980px) {
  :deep(.v-navigation-drawer__content) {
    margin: 12px;
    height: calc(100dvh - 24px) !important;
  }

  .app-header {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px 10px;
    min-height: 56px;
    padding: 8px 10px;
  }

  .app-header-actions {
    grid-column: 2;
    grid-row: 1;
    flex: 0 0 auto;
    gap: 6px;
  }

  .task-ticker {
    grid-column: 1 / -1;
    grid-row: 2;
    width: 100%;
    max-width: 100%;
    padding: 7px 9px;
  }

  .header-utility-strip {
    gap: 4px;
    min-height: 36px;
  }

  .main-content {
    padding: 16px;
  }
}

@media (max-width: 760px) {
  :deep(.v-main) {
    overflow-y: auto;
  }

  .main-content {
    height: auto;
    min-height: 100%;
  }
}

@media (max-width: 640px) {
  .app-title {
    font-size: 1.18rem;
  }

  .app-header-actions > .v-chip {
    max-width: 36px;
    padding-inline: 8px;
    overflow: hidden;
  }

}
</style>

<style>
.theme-menu-card {
  width: min(440px, calc(100vw - 24px));
  border: 1px solid var(--lp-border);
  border-radius: var(--lp-radius-md) !important;
}

.theme-menu-title {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 16px 10px;
  font-size: 0.95rem;
  font-weight: 700;
}

.theme-menu-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 8px 16px 14px !important;
}

.theme-field-label {
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  font-weight: 650;
}

.theme-mode-toggle {
  display: grid !important;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  width: 100%;
}

.theme-mode-toggle .v-btn {
  min-width: 0 !important;
  padding-inline: 8px !important;
}

.theme-mode-hint {
  margin-top: -6px;
  color: var(--lp-text-muted);
  font-size: 0.76rem;
  line-height: 1.4;
}

.theme-preset-sections,
.theme-preset-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.theme-preset-sections {
  gap: 14px;
}

.theme-preset-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}

.theme-preset-option {
  display: grid;
  grid-template-columns: 42px minmax(0, 1fr) 16px;
  align-items: center;
  gap: 9px;
  min-height: 42px;
  padding: 5px 8px;
  border: 1px solid var(--lp-border);
  border-radius: var(--lp-radius-sm);
  background: var(--lp-surface);
  color: var(--lp-text);
  cursor: pointer;
  font: inherit;
  font-size: 0.8rem;
  font-weight: 600;
  text-align: left;
  transition: border-color 160ms ease, background-color 160ms ease, box-shadow 160ms ease;
}

.theme-preset-option:hover {
  border-color: rgba(var(--v-theme-primary), 0.45);
  background: rgba(var(--v-theme-primary), 0.04);
}

.theme-preset-option:focus-visible {
  outline: 2px solid rgba(var(--v-theme-primary), 0.6);
  outline-offset: 2px;
}

.theme-preset-option--active {
  border-color: rgb(var(--v-theme-primary));
  background: rgba(var(--v-theme-primary), 0.07);
  box-shadow: 0 0 0 1px rgba(var(--v-theme-primary), 0.12);
}

.theme-preset-preview {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 42px;
  height: 30px;
  overflow: hidden;
  border: 1px solid rgba(var(--v-theme-on-surface), 0.12);
  border-radius: 7px;
  background: var(--preset-surface-variant);
}

.theme-preset-preview--split {
  align-items: stretch;
  justify-content: stretch;
}

.theme-preset-half {
  display: flex;
  flex: 1 1 50%;
  align-items: center;
  justify-content: center;
  background: var(--preset-surface-variant);
}

.theme-preset-primary-dot {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  border-radius: 99px;
  background: var(--preset-primary);
  color: var(--preset-on-primary);
}

.user-menu-name {
  max-width: 220px;
  overflow: hidden;
  font-weight: 650;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 420px) {
  .theme-mode-toggle {
    grid-template-columns: 1fr;
  }

  .theme-preset-grid {
    grid-template-columns: 1fr;
  }
}
</style>
