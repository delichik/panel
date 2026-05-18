<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { Refresh, Select } from '@element-plus/icons-vue';
import { ElMessage } from 'element-plus';
import { settingsApi } from '@/api/settings';
import type { RuntimeSettingsDto } from '@/types/api';

const settings = ref<RuntimeSettingsDto | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const form = reactive({
  metricsRetentionDays: 7,
  metricsCollectionIntervalSeconds: 60,
  cleanupSchedule: 'daily',
});

function syncForm(next: RuntimeSettingsDto) {
  form.metricsRetentionDays = next.metricsRetentionDays;
  form.metricsCollectionIntervalSeconds = next.metricsCollectionIntervalSeconds;
  form.cleanupSchedule = next.cleanupSchedule;
}

async function loadSettings() {
  loading.value = true;
  try {
    settings.value = await settingsApi.runtime();
    syncForm(settings.value);
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load runtime settings';
  } finally {
    loading.value = false;
  }
}

async function saveSettings() {
  saving.value = true;
  try {
    settings.value = await settingsApi.updateRuntime({ ...form });
    syncForm(settings.value);
    error.value = '';
    ElMessage.success('Runtime settings saved');
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to save runtime settings';
  } finally {
    saving.value = false;
  }
}

onMounted(loadSettings);
</script>

<template>
  <div>
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Runtime settings are stored in the application database and apply without restart.</p>
      </div>
      <div class="toolbar">
        <el-button :icon="Refresh" :loading="loading" @click="loadSettings">Refresh</el-button>
        <el-button type="primary" :icon="Select" :loading="saving" :disabled="!settings" @click="saveSettings">
          Save
        </el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />

    <section class="panel settings-panel" v-loading="loading">
      <template v-if="settings">
        <el-form class="runtime-form" label-position="top">
          <el-form-item label="Metrics retention">
            <el-input-number v-model="form.metricsRetentionDays" :min="1" :max="3650" />
            <span class="unit-label">days</span>
          </el-form-item>
          <el-form-item label="Collection interval">
            <el-input-number v-model="form.metricsCollectionIntervalSeconds" :min="10" :max="86400" />
            <span class="unit-label">seconds</span>
          </el-form-item>
          <el-form-item label="Cleanup schedule">
            <el-radio-group v-model="form.cleanupSchedule">
              <el-radio-button label="hourly">Hourly</el-radio-button>
              <el-radio-button label="daily">Daily</el-radio-button>
              <el-radio-button label="weekly">Weekly</el-radio-button>
            </el-radio-group>
          </el-form-item>
        </el-form>

        <el-divider />

        <el-descriptions :column="1" border>
          <el-descriptions-item label="Listen address">{{ settings.listenAddress }}</el-descriptions-item>
          <el-descriptions-item label="Application database">{{ settings.appDatabase }}</el-descriptions-item>
          <el-descriptions-item label="Metrics database">{{ settings.metricsDatabase }}</el-descriptions-item>
          <el-descriptions-item label="Data root">{{ settings.dataRoot }}</el-descriptions-item>
        </el-descriptions>
      </template>
      <el-empty v-else description="Runtime settings unavailable" />
    </section>
  </div>
</template>

<style scoped>
.page-alert,
.settings-panel {
  margin-top: 20px;
}

.settings-panel {
  padding: 20px;
}

.runtime-form {
  max-width: 520px;
}

.unit-label {
  margin-left: 10px;
  color: #667085;
}
</style>
