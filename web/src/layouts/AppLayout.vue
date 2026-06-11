<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useDisplay, useTheme } from 'vuetify';
import { useAuthStore } from '@/stores/auth';
import { tasksApi } from '@/api/tasks';
import { systemApi } from '@/api/system';
import type { SystemVersionDto, TaskDto } from '@/types/api';
import { useI18n } from '@/i18n';
import { markThemeChanging } from '@/theme';

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
const theme = useTheme();
const display = useDisplay();
const { t, translateTaskStage, translateTaskStatus, translateTaskType } = useI18n();

const isDark = computed(() => theme.global.current.value.dark);
const isCompactLayout = computed(() => display.mdAndDown.value);
const pageTitle = computed(() => t(String(route.meta.titleKey || 'app.name')));
const drawerOpen = ref(true);
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
    key: 'dns',
    icon: 'mdi-dns-outline',
    title: t('layout.nav.dns'),
    items: [
      { to: '/dns/domains', title: t('layout.nav.domains'), value: 'dns-domains' },
      { to: '/dns/certificates', title: t('layout.nav.certificates'), value: 'dns-certificates' },
    ],
  },
  {
    key: 'runtime',
    icon: 'mdi-cloud-braces',
    title: t('layout.nav.runtime'),
    items: [
      { to: '/applications', title: t('layout.nav.applications'), value: 'applications' },
      { to: '/nomad/nodes', title: t('layout.nav.nomadNodes'), value: 'nomad-nodes' },
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
      { to: '/settings/nomad', title: t('layout.nav.settingsNomad'), value: 'settings-nomad' },
      { to: '/settings/certificates', title: t('layout.nav.settingsCertificates'), value: 'settings-certificates' },
      { to: '/settings/system', title: t('layout.nav.settingsSystem'), value: 'settings-system' },
    ],
  },
]);

function toggleTheme() {
  const nextTheme = theme.global.current.value.dark ? 'light' : 'dark';
  markThemeChanging();
  theme.global.name.value = nextTheme;
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

    <v-main class="fill-height overflow-y-auto">
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
          </div>

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
            <Transition name="task-slide" mode="out-in">
              <div v-if="currentTask" :key="currentTask.id" class="task-ticker">
                <v-icon size="16" color="primary">mdi-progress-clock</v-icon>
                <span class="task-line">
                  {{ translateTaskType(currentTask.type) }}
                  <span class="task-stage">{{ translateTaskStatus(currentTask.status) }} - {{ currentTask.stage ? translateTaskStage(currentTask.stage) : t('layout.taskTicker.queuedStage') }}</span>
                </span>
              </div>
            </Transition>

            <div class="header-utility-strip">
              <v-btn icon size="small" variant="text" class="utility-btn" :aria-label="isDark ? t('layout.theme.toLight') : t('layout.theme.toDark')" @click="toggleTheme">
                <v-icon>{{ isDark ? 'mdi-weather-sunny' : 'mdi-weather-night' }}</v-icon>
              </v-btn>
              <div class="user-pill">
                <v-icon size="16">mdi-account-circle-outline</v-icon>
                <span v-if="auth.username" class="user-name">{{ auth.username }}</span>
              </div>
              <v-btn variant="outlined" size="small" prepend-icon="mdi-logout" class="text-none logout-btn" @click="logout">
                {{ t('layout.logout') }}
              </v-btn>
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

.user-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  min-height: 36px;
  padding: 0 4px;
  color: var(--lp-text-muted);
}

.logout-btn {
  min-width: 88px;
  box-shadow: none !important;
}

:deep(.v-main) {
  height: 100dvh;
  overflow-y: auto;
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
  align-items: center;
  gap: 8px;
  min-width: 180px;
  max-width: min(380px, 28vw);
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

.user-name {
  color: var(--lp-text);
  font-size: 0.86rem;
  font-weight: 650;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.main-content {
  width: min(100%, 1640px);
  margin: 0 auto;
  padding: clamp(16px, 2vw, 28px);
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
    align-items: flex-start;
    flex-direction: column;
    gap: 10px;
  }

  .app-header-actions {
    justify-content: flex-start;
    width: 100%;
    flex-wrap: wrap;
  }

  .task-ticker {
    max-width: 100%;
  }

  .header-utility-strip {
    justify-content: space-between;
    max-width: 100%;
  }

  .main-content {
    padding: 16px;
  }
}

@media (max-width: 640px) {
  .app-title {
    font-size: 1.18rem;
  }

  .header-utility-strip {
    width: 100%;
    flex-wrap: wrap;
  }

  .user-pill {
    flex: 1 1 140px;
    justify-content: center;
  }

  .logout-btn {
    flex: 1 1 100%;
  }
}
</style>
