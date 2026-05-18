<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { Refresh, Upload } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import ServerSelector from '@/components/ServerSelector.vue';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import { packagesApi } from '@/api/packages';
import { serversApi } from '@/api/servers';
import type { PackageUpdateDto, PackageUpdatesDto, ServerDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const updates = ref<PackageUpdatesDto | null>(null);
const selectedPackages = ref<PackageUpdateDto[]>([]);
const taskId = ref('');
const loadingServers = ref(false);
const loadingUpdates = ref(false);
const error = ref('');

const currentServer = computed(() => servers.value.find((server) => server.id === serverId.value));
const operationBlocked = computed(() => {
  const server = currentServer.value;
  return !server || server.os?.supported === false || server.sudo?.passwordless === false;
});

async function loadServers() {
  loadingServers.value = true;
  try {
    servers.value = await serversApi.listServers();
    if (!serverId.value && servers.value.length) serverId.value = servers.value[0].id;
  } finally {
    loadingServers.value = false;
  }
}

async function loadUpdates() {
  if (!serverId.value) {
    updates.value = null;
    return;
  }
  loadingUpdates.value = true;
  try {
    updates.value = await packagesApi.listUpdates(serverId.value);
    selectedPackages.value = [];
    error.value = '';
  } catch (err) {
    updates.value = null;
    error.value = err instanceof Error ? err.message : 'Unable to load package updates';
  } finally {
    loadingUpdates.value = false;
  }
}

async function refreshUpdates() {
  if (!serverId.value) return;
  const result = await packagesApi.refresh(serverId.value);
  taskId.value = result.taskId;
  ElMessage.success('Package refresh started');
}

async function upgradeSelected() {
  if (!serverId.value || selectedPackages.value.length === 0) return;
  const names = selectedPackages.value.map((item) => item.name);
  const result = await packagesApi.upgradeSelected(serverId.value, names);
  taskId.value = result.taskId;
  ElMessage.success('Selected package upgrade started');
}

async function upgradeAll() {
  if (!serverId.value) return;
  const result = await packagesApi.upgradeAll(serverId.value);
  taskId.value = result.taskId;
  ElMessage.success('Full package upgrade started');
}

async function handleTaskFinished() {
  await Promise.all([loadServers(), loadUpdates()]);
  ElMessage.success('Package task finished');
}

watch(serverId, loadUpdates);
onMounted(async () => {
  await loadServers();
  await loadUpdates();
});
</script>

<template>
  <div>
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Refresh package caches and execute Debian upgrades as tracked tasks.</p>
      </div>
      <div class="toolbar">
        <el-button :icon="Refresh" :disabled="!serverId" :loading="loadingUpdates" @click="loadUpdates">
          Reload
        </el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />
    <el-alert
      v-if="operationBlocked && currentServer"
      class="page-alert"
      type="warning"
      title="This server is not eligible for package operations until distro support and passwordless sudo are confirmed."
      show-icon
    />

    <div class="package-grid">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <section class="panel package-panel" v-loading="loadingUpdates">
        <div class="panel-header">
          <div>
            <strong>{{ currentServer?.name || 'Select server' }}</strong>
            <div class="muted">Last refreshed: {{ updates?.lastRefreshedAt || 'never' }}</div>
          </div>
          <div class="toolbar">
            <el-button :icon="Refresh" :disabled="!serverId || operationBlocked" @click="refreshUpdates">
              Refresh updates
            </el-button>
            <el-button
              type="primary"
              :icon="Upload"
              :disabled="operationBlocked || selectedPackages.length === 0"
              @click="upgradeSelected"
            >
              Upgrade selected
            </el-button>
            <el-button type="danger" :disabled="operationBlocked || !updates?.updates.length" @click="upgradeAll">
              Upgrade all
            </el-button>
          </div>
        </div>
        <el-table
          :data="updates?.updates ?? []"
          empty-text="No upgradeable packages"
          @selection-change="selectedPackages = $event"
        >
          <el-table-column type="selection" width="48" />
          <el-table-column prop="name" label="Package" min-width="180" />
          <el-table-column prop="installedVersion" label="Installed" min-width="180" />
          <el-table-column prop="candidateVersion" label="Candidate" min-width="180" />
          <el-table-column prop="source" label="Source" min-width="160" />
        </el-table>
      </section>
    </div>

    <section v-if="taskId" class="panel task-section">
      <div class="panel-header"><strong>Running Task</strong></div>
      <div class="panel-body">
        <TaskLogPanel :task-id="taskId" :server-name="currentServer?.name" @finished="handleTaskFinished" />
      </div>
    </section>
  </div>
</template>

<style scoped>
.page-alert,
.package-grid,
.task-section {
  margin-top: 20px;
}

.package-grid {
  display: grid;
  grid-template-columns: 340px minmax(0, 1fr);
  gap: 20px;
}
</style>
