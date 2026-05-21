<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useTheme } from 'vuetify';
import { useAuthStore } from '@/stores/auth';
import { tasksApi } from '@/api/tasks';
import type { TaskDto } from '@/types/api';

const router = useRouter();
const auth = useAuthStore();
const theme = useTheme();

const isDark = computed(() => theme.global.current.value.dark);

function toggleTheme() {
  const nextTheme = theme.global.current.value.dark ? 'light' : 'dark';
  theme.global.name.value = nextTheme;
  localStorage.setItem('theme', nextTheme);
}
const activeTasks = ref<TaskDto[]>([]);
const taskIndex = ref(0);
let taskTimer: number | undefined;
let rotateTimer: number | undefined;

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

async function logout() {
  await auth.logout();
  await router.push('/login');
}

onMounted(() => {
  const savedTheme = localStorage.getItem('theme');
  if (savedTheme) {
    theme.global.name.value = savedTheme;
  }
  void loadActiveTasks();
  taskTimer = window.setInterval(loadActiveTasks, 8000);
  rotateTimer = window.setInterval(() => {
    if (activeTasks.value.length > 1) taskIndex.value = (taskIndex.value + 1) % activeTasks.value.length;
  }, 3500);
});

onBeforeUnmount(() => {
  if (taskTimer) window.clearInterval(taskTimer);
  if (rotateTimer) window.clearInterval(rotateTimer);
});
</script>

<template>
  <v-layout class="fill-height">
    <v-navigation-drawer width="264" permanent floating style="background: transparent;">
      <div class="brand">
        <div class="brand-mark">LP</div>
        <div>
          <div class="brand-title">Linux Panel</div>
          <div class="brand-subtitle">SSH control plane</div>
        </div>
      </div>

      <v-list nav class="py-4 px-3">
        <v-list-item to="/overview" prepend-icon="mdi-monitor" title="Overview" value="overview" />
        <v-list-item to="/servers" prepend-icon="mdi-server" title="Servers" value="servers" />
        <v-list-item to="/packages" prepend-icon="mdi-package-variant" title="Package Updates" value="packages" />

        <v-list-group value="docker">
          <template #activator="{ props }">
            <v-list-item v-bind="props" prepend-icon="mdi-docker" title="Docker" />
          </template>
          <v-list-item to="/services" title="Services" value="services" class="pl-8" />
          <v-list-item to="/runtime-resources" title="Runtime Resources" value="runtime" class="pl-8" />
          <v-list-item to="/service-templates" title="Service Templates" value="templates" class="pl-8" />
        </v-list-group>

        <v-list-item to="/tasks" prepend-icon="mdi-clipboard-list" title="Task Center" value="tasks" />
        <v-list-item to="/settings" prepend-icon="mdi-cog" title="Settings" value="settings" />
      </v-list>
    </v-navigation-drawer>

    <v-app-bar flat height="72" class="glass-bar">
      <div class="task-ticker px-4 flex-grow-1">
        <span v-if="currentTask">
          <v-icon size="small" color="primary" class="mr-2">mdi-play-circle-outline</v-icon>
        </span>
        <span v-if="currentTask" class="task-line text-body-2">
          {{ currentTask.summary || currentTask.type }}
          <span class="text-medium-emphasis ml-2">{{ currentTask.status }} - {{ currentTask.stage || 'queued' }}</span>
        </span>
        <span v-else class="text-medium-emphasis text-body-2">
          <v-icon size="small" class="mr-2">mdi-circle-double</v-icon>
          No active tasks
        </span>
      </div>

      <template v-slot:append>
        <div class="d-flex align-center px-4" style="gap: 16px;">
          <v-btn icon size="small" variant="text" @click="toggleTheme">
            <v-icon>{{ isDark ? 'mdi-weather-sunny' : 'mdi-weather-night' }}</v-icon>
          </v-btn>
          <span class="text-subtitle-2 font-weight-bold text-medium-emphasis">{{ auth.username }}</span>
          <v-btn variant="outlined" size="small" prepend-icon="mdi-logout" class="text-none" @click="logout">
            Logout
          </v-btn>
        </div>
      </template>
    </v-app-bar>

    <v-main class="fill-height overflow-y-auto" style="height: 100vh;">
      <div class="pa-6">
        <RouterView />
      </div>
    </v-main>
  </v-layout>
</template>

<style scoped>
/* Glassmorphism sidebar drawer container floating */
:deep(.v-navigation-drawer) {
  background-color: transparent !important;
  border: none !important;
}

:deep(.v-navigation-drawer__content) {
  margin: 16px;
  height: calc(100vh - 32px) !important;
  border-radius: 14px !important;
  border: 1px solid rgba(var(--v-border-color), 0.06) !important;
  background: rgb(var(--v-theme-surface)) !important;
  box-shadow: 0 4px 20px -2px rgba(0, 0, 0, 0.03), 0 2px 8px -1px rgba(0, 0, 0, 0.02) !important;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: background-color 0.25s ease, border-color 0.25s ease, box-shadow 0.25s ease;
}

/* Translucent premium glass top bar */
.glass-bar {
  background: rgba(var(--v-theme-surface), 0.75) !important;
  backdrop-filter: blur(12px) !important;
  -webkit-backdrop-filter: blur(12px) !important;
  border-bottom: 1px solid rgba(var(--v-border-color), 0.06) !important;
  transition: background-color 0.25s ease, border-color 0.25s ease;
}

/* Offset top-bar positioning for breathing room if needed (or standard flush) */
:deep(.v-main) {
  height: 100vh;
  overflow-y: auto;
  transition: background-color 0.25s ease;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 72px;
  padding: 0 18px;
  border-bottom: 1px solid rgba(var(--v-border-color), 0.06);
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: linear-gradient(135deg, rgb(var(--v-theme-primary)) 0%, #4f46e5 100%);
  color: #ffffff;
  font-weight: 700;
  box-shadow: 0 2px 8px rgba(var(--v-theme-primary), 0.25);
}

.brand-title {
  font-weight: 700;
  letter-spacing: -0.01em;
}

.brand-subtitle {
  color: rgba(var(--v-theme-on-surface), 0.5);
  font-size: 11px;
  font-weight: 500;
}

.task-ticker {
  min-width: 0;
  overflow: hidden;
  font-size: 14px;
}

.task-line {
  display: inline-flex;
  align-items: center;
  max-width: 60vw;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Navigation items premium aesthetics */
:deep(.v-list-item) {
  border-radius: 8px !important;
  margin: 2px 0 !important;
  padding: 8px 12px !important;
  font-size: 0.9rem !important;
  font-weight: 500 !important;
  color: rgba(var(--v-theme-on-surface), 0.75) !important;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1) !important;
}

:deep(.v-list-item:hover) {
  color: rgb(var(--v-theme-on-surface)) !important;
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
</style>
