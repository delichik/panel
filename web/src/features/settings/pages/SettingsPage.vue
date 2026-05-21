<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
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
    showMessage('Runtime settings saved successfully');
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
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Settings</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Runtime settings are stored in the application database and apply without restart.</p>
      </div>
      <div class="d-flex" style="gap: 12px;">
        <v-btn
          prepend-icon="mdi-refresh"
          :loading="loading"
          variant="outlined"
          @click="loadSettings"
          class="text-none font-weight-bold"
        >
          Refresh
        </v-btn>
        <v-btn
          color="primary"
          prepend-icon="mdi-content-save"
          :loading="saving"
          :disabled="!settings"
          @click="saveSettings"
          class="text-none font-weight-bold"
        >
          Save
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card :loading="loading" variant="outlined" class="pa-6">
      <template v-if="settings">
        <v-form class="runtime-form mb-6">
          <div class="d-flex flex-column" style="gap: 16px;">
            <v-text-field
              v-model.number="form.metricsRetentionDays"
              type="number"
              min="1"
              max="3650"
              label="Metrics Retention"
              suffix="days"
              variant="outlined"
              density="comfortable"
              hide-details="auto"
            />

            <v-text-field
              v-model.number="form.metricsCollectionIntervalSeconds"
              type="number"
              min="10"
              max="86400"
              label="Collection Interval"
              suffix="seconds"
              variant="outlined"
              density="comfortable"
              hide-details="auto"
            />

            <div>
              <div class="text-subtitle-2 mb-2 text-grey-darken-3">Cleanup Schedule</div>
              <v-btn-toggle v-model="form.cleanupSchedule" mandatory color="primary" density="compact">
                <v-btn value="hourly" class="text-none">Hourly</v-btn>
                <v-btn value="daily" class="text-none">Daily</v-btn>
                <v-btn value="weekly" class="text-none">Weekly</v-btn>
              </v-btn-toggle>
            </div>
          </div>
        </v-form>

        <v-divider class="my-6" />

        <div class="text-subtitle-1 font-weight-bold mb-3">System Properties</div>
        <v-card variant="flat" class="border" style="background: transparent;">
          <v-table density="compact" style="background: transparent;" class="text-left">
            <tbody>
              <tr>
                <td class="font-weight-bold text-caption text-grey-darken-1 py-3 text-uppercase" style="width: 200px;">Listen address</td>
                <td>{{ settings.listenAddress }}</td>
              </tr>
              <tr>
                <td class="font-weight-bold text-caption text-grey-darken-1 py-3 text-uppercase">Application database</td>
                <td class="font-mono text-caption">{{ settings.appDatabase }}</td>
              </tr>
              <tr>
                <td class="font-weight-bold text-caption text-grey-darken-1 py-3 text-uppercase">Metrics database</td>
                <td class="font-mono text-caption">{{ settings.metricsDatabase }}</td>
              </tr>
              <tr>
                <td class="font-weight-bold text-caption text-grey-darken-1 py-3 text-uppercase">Data root</td>
                <td class="font-mono text-caption">{{ settings.dataRoot }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>
      </template>
      <div v-else class="text-center py-10 text-grey-darken-1">
        <v-icon size="40" class="mb-2" color="grey-lighten-1">mdi-cog-off-outline</v-icon>
        <div>Runtime settings unavailable</div>
      </div>
    </v-card>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">Close</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.runtime-form {
  max-width: 520px;
}
.font-mono {
  font-family: monospace !important;
}
</style>
