<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useI18n } from '@/i18n';
import { useSettingsStore } from '@/stores/settings';
import type { RuntimeSettingsDto } from '@/types/api';

const settingsStore = useSettingsStore();
const { t, translateCleanupSchedule } = useI18n();
const settings = ref<RuntimeSettingsDto | null>(null);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const form = reactive({
  metricsRetentionDays: 7,
  metricsCollectionIntervalSeconds: 60,
  cleanupSchedule: 'daily',
  language: 'en',
});

// Snackbar notification state
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function syncForm(next: RuntimeSettingsDto) {
  form.metricsRetentionDays = next.metricsRetentionDays;
  form.metricsCollectionIntervalSeconds = next.metricsCollectionIntervalSeconds;
  form.cleanupSchedule = next.cleanupSchedule;
  form.language = next.language;
}

async function loadSettings() {
  loading.value = true;
  try {
    const next = await settingsStore.loadRuntime(true);
    if (next) {
      settings.value = next;
      syncForm(next);
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function saveSettings() {
  saving.value = true;
  try {
    const next = await settingsStore.updateRuntime({ ...form });
    settings.value = next;
    syncForm(next);
    error.value = '';
    showMessage(t('settingsPage.saveSuccess'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('settingsPage.saveFailed');
  } finally {
    saving.value = false;
  }
}

onMounted(loadSettings);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <v-card :loading="loading" variant="outlined" class="pa-6">
      <div class="settings-header">
        <v-btn
          color="primary"
          prepend-icon="mdi-content-save"
          :loading="saving"
          :disabled="!settings"
          @click="saveSettings"
          class="text-none font-weight-bold action-btn"
        >
          {{ t('common.save') }}
        </v-btn>
      </div>
      <template v-if="settings">
        <v-form class="runtime-form mb-6">
          <div class="d-flex flex-column" style="gap: 16px;">
            <v-text-field
              v-model.number="form.metricsRetentionDays"
              type="number"
              min="1"
              max="3650"
              :label="t('settingsPage.metricsRetention')"
              :suffix="t('settingsPage.days')"
              variant="outlined"
              density="comfortable"
              hide-details="auto"
            />

            <v-text-field
              v-model.number="form.metricsCollectionIntervalSeconds"
              type="number"
              min="10"
              max="86400"
              :label="t('settingsPage.collectionInterval')"
              :suffix="t('settingsPage.seconds')"
              variant="outlined"
              density="comfortable"
              hide-details="auto"
            />

            <div>
              <div class="text-subtitle-2 mb-2 text-medium-emphasis">{{ t('settingsPage.cleanupSchedule') }}</div>
              <v-btn-toggle v-model="form.cleanupSchedule" mandatory color="primary" density="compact">
                <v-btn value="hourly" class="text-none">{{ translateCleanupSchedule('hourly') }}</v-btn>
                <v-btn value="daily" class="text-none">{{ translateCleanupSchedule('daily') }}</v-btn>
                <v-btn value="weekly" class="text-none">{{ translateCleanupSchedule('weekly') }}</v-btn>
              </v-btn-toggle>
            </div>
            <v-select
              v-model="form.language"
              :items="[
                { title: t('languages.en'), value: 'en' },
                { title: t('languages.zh-CN'), value: 'zh-CN' },
              ]"
              :label="t('settingsPage.language')"
              variant="outlined"
              density="comfortable"
              hide-details="auto"
            />
          </div>
        </v-form>

        <v-divider class="my-6" />

        <div class="text-subtitle-1 font-weight-bold mb-3">{{ t('settingsPage.systemProperties') }}</div>
        <v-card variant="flat" class="border" style="background: transparent;">
          <v-table density="compact" style="background: transparent;" class="text-left">
            <tbody>
              <tr>
                <td class="font-weight-bold text-caption text-medium-emphasis py-3 text-uppercase" style="width: 200px;">{{ t('settingsPage.listenAddress') }}</td>
                <td>{{ settings.listenAddress }}</td>
              </tr>
              <tr>
                <td class="font-weight-bold text-caption text-medium-emphasis py-3 text-uppercase">{{ t('settingsPage.applicationDatabase') }}</td>
                <td class="font-mono text-caption">{{ settings.appDatabase }}</td>
              </tr>
              <tr>
                <td class="font-weight-bold text-caption text-medium-emphasis py-3 text-uppercase">{{ t('settingsPage.metricsDatabase') }}</td>
                <td class="font-mono text-caption">{{ settings.metricsDatabase }}</td>
              </tr>
              <tr>
                <td class="font-weight-bold text-caption text-medium-emphasis py-3 text-uppercase">{{ t('settingsPage.dataRoot') }}</td>
                <td class="font-mono text-caption">{{ settings.dataRoot }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>
      </template>
      <div v-else class="text-center py-10 text-medium-emphasis">
        <v-icon size="40" class="mb-2" color="medium-emphasis">mdi-cog-off-outline</v-icon>
        <div>{{ t('settingsPage.unavailable') }}</div>
      </div>
    </v-card>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.settings-header {
  display: flex;
  justify-content: flex-start;
  margin-bottom: 20px;
}

.runtime-form {
  max-width: 520px;
}
</style>
