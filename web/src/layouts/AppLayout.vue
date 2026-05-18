<script setup lang="ts">
import { computed } from 'vue';
import { Box, Cpu, Monitor, Setting, SwitchButton, Upload } from '@element-plus/icons-vue';
import { useRoute, useRouter } from 'vue-router';
import { useAuthStore } from '@/stores/auth';

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();
const routeTitle = computed(() => String(route.meta.title || 'Linux Panel'));

async function logout() {
  await auth.logout();
  await router.push('/login');
}
</script>

<template>
  <el-container class="app-shell">
    <el-aside width="248px" class="sidebar">
      <div class="brand">
        <div class="brand-mark">LP</div>
        <div>
          <div class="brand-title">Linux Panel</div>
          <div class="brand-subtitle">SSH control plane</div>
        </div>
      </div>

      <el-menu router class="nav" :default-active="$route.path">
        <el-menu-item index="/overview">
          <el-icon><Monitor /></el-icon>
          <span>Overview</span>
        </el-menu-item>
        <el-menu-item index="/servers">
          <el-icon><Cpu /></el-icon>
          <span>Servers</span>
        </el-menu-item>
        <el-menu-item index="/packages">
          <el-icon><Upload /></el-icon>
          <span>Package Updates</span>
        </el-menu-item>
        <el-menu-item index="/tasks">
          <el-icon><Box /></el-icon>
          <span>Task Center</span>
        </el-menu-item>
        <el-menu-item index="/settings">
          <el-icon><Setting /></el-icon>
          <span>Settings</span>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="topbar">
        <h1 class="topbar-title">{{ routeTitle }}</h1>
        <div class="toolbar">
          <span class="muted">{{ auth.username }}</span>
          <el-button :icon="SwitchButton" @click="logout">Logout</el-button>
        </div>
      </el-header>
      <el-main class="content">
        <RouterView />
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.app-shell {
  min-height: 100vh;
}

.sidebar {
  border-right: 1px solid #dfe4ea;
  background: #ffffff;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 72px;
  padding: 0 18px;
  border-bottom: 1px solid #edf0f3;
}

.brand-mark {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  background: #1f2937;
  color: #fff;
  font-weight: 700;
}

.brand-title {
  font-weight: 700;
}

.brand-subtitle {
  color: #667085;
  font-size: 12px;
}

.nav {
  border-right: 0;
}

.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #dfe4ea;
  background: #fff;
}

.topbar-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
}

.content {
  padding: 24px;
  background: #f4f6f8;
}
</style>
