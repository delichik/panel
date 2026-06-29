<script setup lang="ts">
import { ref } from 'vue';
import { backupsApi } from '@/api/backups';
import { useI18n } from '@/i18n';
import type { RestorePreflightDto } from '@/types/api';

const { t, formatDateTime } = useI18n();

const encryptBackup = ref(true);
const restorePassword = ref('');
const restoreFile = ref<File | null>(null);
const restorePlan = ref<RestorePreflightDto | null>(null);
const exportDialog = ref(false);
const restoreDialog = ref(false);
const loading = ref(false);
const error = ref('');
const snackbar = ref(false);
const snackbarText = ref('');

const canStartExport = true;

function showMessage(text: string) {
  snackbarText.value = text;
  snackbar.value = true;
}

async function startExport() {
  loading.value = true;
  error.value = '';
  try {
    const result = await backupsApi.startExport({
      encrypt: encryptBackup.value,
      password: '',
    });
    exportDialog.value = false;
    showMessage(t(result.restartSupported ? 'backupRestore.exportRestarting' : 'backupRestore.exportPending'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.exportFailed');
  } finally {
    loading.value = false;
  }
}

async function preflightRestore() {
  if (!restoreFile.value) return;
  loading.value = true;
  error.value = '';
  try {
    restorePlan.value = await backupsApi.preflightRestore(restoreFile.value, restorePassword.value);
  } catch (err) {
    restorePlan.value = null;
    error.value = err instanceof Error ? err.message : t('backupRestore.preflightFailed');
  } finally {
    loading.value = false;
  }
}

async function confirmRestore() {
  if (!restoreFile.value || !restorePlan.value) return;
  loading.value = true;
  error.value = '';
  try {
    const result = await backupsApi.confirmRestore(restoreFile.value, restorePassword.value);
    restoreDialog.value = false;
    showMessage(t(result.restartSupported ? 'backupRestore.restoreRestarting' : 'backupRestore.restorePending'));
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('backupRestore.restoreConfirmFailed');
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="backup-workspace">
      <v-card variant="outlined" class="backup-card">
        <v-card-title class="backup-title">
          <v-icon>mdi-archive-arrow-down-outline</v-icon>
          {{ t('backupRestore.exportTitle') }}
        </v-card-title>
        <v-card-text class="backup-card-body">
          <p>{{ t('backupRestore.exportDescription') }}</p>
          <v-switch
            v-model="encryptBackup"
            color="primary"
            :label="t('backupRestore.encryptBackup')"
            hide-details
          />
          <v-alert v-if="encryptBackup" type="info" variant="tonal">
            {{ t('backupRestore.exportPasswordLater') }}
          </v-alert>
          <v-alert v-else type="warning" variant="tonal">
            {{ t('backupRestore.unencryptedWarning') }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-btn
            color="primary"
            prepend-icon="mdi-play"
            :disabled="!canStartExport"
            @click="exportDialog = true"
          >
            {{ t('backupRestore.startExport') }}
          </v-btn>
        </v-card-actions>
      </v-card>

      <v-card variant="outlined" class="backup-card">
        <v-card-title class="backup-title">
          <v-icon>mdi-archive-arrow-up-outline</v-icon>
          {{ t('backupRestore.restoreTitle') }}
        </v-card-title>
        <v-card-text class="backup-card-body">
          <p>{{ t('backupRestore.restoreDescription') }}</p>
          <v-file-input
            v-model="restoreFile"
            :label="t('backupRestore.backupFile')"
            accept=".panel-backup,application/octet-stream"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
            @update:model-value="restorePlan = null"
          />
          <v-text-field
            v-model="restorePassword"
            type="password"
            :label="t('backupRestore.restorePassword')"
            variant="outlined"
            density="comfortable"
            hide-details="auto"
          />
          <v-btn
            variant="outlined"
            prepend-icon="mdi-shield-search"
            :disabled="!restoreFile"
            :loading="loading"
            @click="preflightRestore"
          >
            {{ t('backupRestore.preflight') }}
          </v-btn>

          <v-alert v-if="restorePlan" type="info" variant="tonal">
            {{ t('backupRestore.preflightSummary', {
              version: restorePlan.manifest.panelVersion,
              created: formatDateTime(restorePlan.manifest.createdAt),
            }) }}
          </v-alert>
        </v-card-text>
        <v-card-actions>
          <v-btn
            color="error"
            prepend-icon="mdi-alert"
            :disabled="!restorePlan"
            @click="restoreDialog = true"
          >
            {{ t('backupRestore.confirmRestore') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </div>

    <v-dialog v-model="exportDialog" max-width="520">
      <v-card>
        <v-card-title>{{ t('backupRestore.exportConfirmTitle') }}</v-card-title>
        <v-card-text>{{ t(encryptBackup ? 'backupRestore.exportConfirmEncrypted' : 'backupRestore.exportConfirmUnencrypted') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="exportDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" :loading="loading" @click="startExport">{{ t('common.confirm') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="restoreDialog" max-width="560">
      <v-card>
        <v-card-title>{{ t('backupRestore.restoreConfirmTitle') }}</v-card-title>
        <v-card-text>{{ t('backupRestore.restoreConfirmMessage') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="restoreDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" :loading="loading" @click="confirmRestore">{{ t('backupRestore.confirmOverwrite') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" color="success" timeout="5000">
      {{ snackbarText }}
      <template #actions>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.backup-workspace {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
  min-height: 0;
}

.backup-card {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border-color: var(--lp-border) !important;
  background: var(--lp-surface) !important;
}

.backup-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 1rem;
  font-weight: 760;
}

.backup-card-body {
  display: grid;
  gap: 14px;
}

.backup-card-body p {
  margin: 0;
  color: var(--lp-text-muted);
}

@media (max-width: 960px) {
  .backup-workspace {
    grid-template-columns: 1fr;
  }
}
</style>
