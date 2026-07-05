<script setup lang="ts">
import { ref } from 'vue';
import { backupsApi } from '@/api/backups';
import { useI18n } from '@/i18n';
import type { RestorePreflightDto } from '@/types/api';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';

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

    <v-card :loading="loading" variant="outlined" class="settings-surface">
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

          <AppActionGroup context="section" align="start" mobile-stack class="form-actions">
            <AppActionButton kind="primary" icon="mdi-play" :label="t('backupRestore.startExport')" :disabled="!canStartExport" @click="exportDialog = true" />
          </AppActionGroup>
        </section>

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

          <AppActionGroup context="section" align="start" mobile-stack class="form-actions">
            <AppActionButton icon="mdi-shield-search" :label="t('backupRestore.preflight')" :disabled="!restoreFile" :loading="loading" @click="preflightRestore" />
          </AppActionGroup>

          <v-alert v-if="restorePlan" type="info" variant="tonal">
            {{ t('backupRestore.preflightSummary', {
              version: restorePlan.manifest.panelVersion,
              created: formatDateTime(restorePlan.manifest.createdAt),
            }) }}
          </v-alert>

          <AppActionGroup context="section" align="start" mobile-stack class="form-actions">
            <AppActionButton kind="danger" icon="mdi-alert" :label="t('backupRestore.confirmRestore')" :disabled="!restorePlan" @click="restoreDialog = true" />
          </AppActionGroup>
        </section>
      </v-form>
    </v-card>

    <v-dialog v-model="exportDialog" width="520">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('backupRestore.exportConfirmTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="exportDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">{{ t(encryptBackup ? 'backupRestore.exportConfirmEncrypted' : 'backupRestore.exportConfirmUnencrypted') }}</v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="exportDialog = false" />
            <AppActionButton kind="primary" :label="t('common.confirm')" :loading="loading" @click="startExport" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="restoreDialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('backupRestore.restoreConfirmTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.cancel')" @click="restoreDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">{{ t('backupRestore.restoreConfirmMessage') }}</v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="restoreDialog = false" />
            <AppActionButton kind="danger-primary" :label="t('backupRestore.confirmOverwrite')" :loading="loading" @click="confirmRestore" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" color="success" timeout="5000">
      {{ snackbarText }}
      <template #actions>
        <AppActionGroup context="snackbar">
          <AppActionButton kind="snackbar" :label="t('common.close')" @click="snackbar = false" />
        </AppActionGroup>
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
  overflow: hidden;
  border-color: var(--lp-border) !important;
  background-color: var(--lp-surface) !important;
}

.settings-header {
  display: flex;
  align-items: center;
  flex: 0 0 auto;
  justify-content: space-between;
  gap: 16px;
  margin: 0;
  padding: 18px 20px;
  border-bottom: 1px solid color-mix(in srgb, var(--lp-border), transparent 12%);
  background:
    linear-gradient(90deg, color-mix(in srgb, rgb(var(--v-theme-primary)), transparent 97%), transparent 62%),
    color-mix(in srgb, var(--lp-surface-container), transparent 42%);
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
  flex: 1 1 auto;
  max-width: 560px;
  width: 100%;
  min-height: 0;
  gap: 16px;
  padding: 20px;
  align-content: start;
  overflow: auto;
}

.settings-section {
  display: grid;
  gap: 14px;
  min-width: 0;
  padding: 16px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 10%);
  border-radius: var(--lp-radius-sm);
  background: color-mix(in srgb, var(--lp-surface-container), transparent 46%);
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

@media (max-width: 720px) {
  .settings-surface {
    flex: none;
    overflow: visible;
  }

  .settings-form {
    min-height: auto;
    overflow: visible;
  }

  .settings-header {
    align-items: flex-start;
    flex-direction: column;
  }

  .settings-form {
    width: 100%;
  }
}
</style>
