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

    <v-card :loading="loading" variant="outlined" class="settings-surface pa-6">
      <div class="settings-header">
        <div>
          <div class="text-overline text-medium-emphasis">{{ t('layout.nav.settings') }}</div>
          <h2 class="settings-title">{{ t('routes.settingsBackups.title') }}</h2>
        </div>
      </div>

      <v-form class="settings-form">
        <section class="settings-section">
          <div class="section-title">{{ t('backupRestore.exportTitle') }}</div>

          <p class="section-description">{{ t('backupRestore.exportDescription') }}</p>

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

          <div class="form-actions">
            <v-btn
              color="primary"
              prepend-icon="mdi-play"
              :disabled="!canStartExport"
              class="text-none font-weight-bold"
              @click="exportDialog = true"
            >
              {{ t('backupRestore.startExport') }}
            </v-btn>
          </div>
        </section>

        <v-divider class="my-2" />

        <section class="settings-section">
          <div class="section-title">{{ t('backupRestore.restoreTitle') }}</div>

          <p class="section-description">{{ t('backupRestore.restoreDescription') }}</p>

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

          <div class="form-actions">
            <v-btn
              variant="outlined"
              prepend-icon="mdi-shield-search"
              :disabled="!restoreFile"
              :loading="loading"
              class="text-none font-weight-bold"
              @click="preflightRestore"
            >
              {{ t('backupRestore.preflight') }}
            </v-btn>
          </div>

          <v-alert v-if="restorePlan" type="info" variant="tonal">
            {{ t('backupRestore.preflightSummary', {
              version: restorePlan.manifest.panelVersion,
              created: formatDateTime(restorePlan.manifest.createdAt),
            }) }}
          </v-alert>

          <div class="form-actions">
            <v-btn
              color="error"
              prepend-icon="mdi-alert"
              :disabled="!restorePlan"
              class="text-none font-weight-bold"
              @click="restoreDialog = true"
            >
              {{ t('backupRestore.confirmRestore') }}
            </v-btn>
          </div>
        </section>
      </v-form>
    </v-card>

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
.settings-surface {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  min-height: 0;
  overflow: auto;
  border-color: var(--lp-border) !important;
  background-color: var(--lp-surface) !important;
}

.settings-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 24px;
}

.settings-title {
  margin: 0;
  color: var(--lp-text);
  font-size: 1.18rem;
  font-weight: 760;
  line-height: 1.2;
  letter-spacing: 0;
}

.settings-form {
  display: grid;
  max-width: 560px;
  gap: 16px;
}

.settings-section {
  display: grid;
  gap: 16px;
}

.section-title {
  color: var(--lp-text);
  font-size: 0.96rem;
  font-weight: 720;
}

.section-description {
  margin: 0;
  color: var(--lp-text-muted);
  line-height: 1.6;
}

.form-actions {
  display: flex;
  justify-content: flex-start;
}

@media (max-width: 720px) {
  .settings-surface {
    flex: none;
    overflow: visible;
  }

  .settings-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .settings-form,
  .form-actions,
  .form-actions .v-btn {
    width: 100%;
  }
}
</style>
