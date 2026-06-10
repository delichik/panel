<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import ServerSelector from '@/components/ServerSelector.vue';
import { serversApi } from '@/api/servers';
import { t } from '@/i18n';
import type { FirewallProtocol, ServerDto, UfwRuleDto, UfwStateDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const state = ref<UfwStateDto | null>(null);
const loadingServers = ref(false);
const loadingState = ref(false);
const savingRule = ref(false);
const installing = ref(false);
const deletingRule = ref<number | null>(null);
const error = ref('');

const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const snackbarTaskId = ref('');
const confirmDialog = ref(false);
const ruleToDelete = ref<UfwRuleDto | null>(null);

const ruleForm = reactive({
  port: 22,
  protocol: 'tcp' as FirewallProtocol,
  from: '',
});

const selectedServer = computed(() => servers.value.find((server) => server.id === serverId.value) ?? null);
const selectedUfwSupported = computed(() => selectedServer.value?.traits?.['sys.ufw_supported'] !== 'false');
const canUseSudo = computed(() => selectedServer.value?.sudo?.passwordless === true);
const canManageRules = computed(() => Boolean(selectedServer.value?.reachable && canUseSudo.value && state.value?.supported && state.value?.installed));
const canAddRule = computed(() => canManageRules.value && Number(ruleForm.port) > 0 && Number(ruleForm.port) <= 65535);

const protocolOptions = computed(() => [
  { title: t('firewallPage.tcp'), value: 'tcp' },
  { title: t('firewallPage.udp'), value: 'udp' },
  { title: t('firewallPage.anyProtocol'), value: 'any' },
]);

function showMessage(text: string, color = 'success', taskId = '') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbarTaskId.value = taskId;
  snackbar.value = true;
}

function taskRoute(taskId = snackbarTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function statusColor() {
  if (!state.value) return 'secondary';
  if (!state.value.supported) return 'secondary';
  if (!state.value.installed) return 'warning';
  if (state.value.active) return 'success';
  return 'primary';
}

function statusLabel() {
  if (!state.value) return t('common.unknown');
  if (!state.value.supported) return t('firewallPage.unsupported');
  if (!state.value.installed) return t('firewallPage.notInstalled');
  if (state.value.active) return t('firewallPage.active');
  return t('firewallPage.inactive');
}

async function loadServers() {
  loadingServers.value = true;
  try {
    const rows = await serversApi.listServers();
    servers.value = rows;
    if (!serverId.value && rows.length) serverId.value = rows[0].id;
    if (serverId.value && !rows.some((server) => server.id === serverId.value)) {
      serverId.value = rows[0]?.id ?? '';
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('firewallPage.loadFailed');
  } finally {
    loadingServers.value = false;
  }
}

async function loadState() {
  if (!serverId.value) {
    state.value = null;
    return;
  }
  loadingState.value = true;
  error.value = '';
  try {
    state.value = await serversApi.ufwState(serverId.value);
  } catch (err) {
    state.value = selectedServer.value
      ? { serverId: selectedServer.value.id, supported: selectedUfwSupported.value, installed: false, active: false, status: 'unknown', defaultPolicy: '', rules: [] }
      : null;
    error.value = err instanceof Error ? err.message : t('firewallPage.loadFailed');
  } finally {
    loadingState.value = false;
  }
}

async function installUFW() {
  if (!serverId.value) return;
  installing.value = true;
  try {
    const result = await serversApi.installUFW(serverId.value);
    showMessage(t('firewallPage.installStarted'), 'success', result.taskId);
    await loadServers();
    await loadState();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.installFailed'), 'error');
  } finally {
    installing.value = false;
  }
}

async function addRule() {
  if (!serverId.value || !canAddRule.value) return;
  savingRule.value = true;
  try {
    state.value = await serversApi.allowUFW(serverId.value, {
      port: Number(ruleForm.port),
      protocol: ruleForm.protocol,
      from: ruleForm.from.trim() || undefined,
    });
    showMessage(t('firewallPage.ruleAdded'));
    ruleForm.from = '';
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.ruleAddFailed'), 'error');
  } finally {
    savingRule.value = false;
  }
}

function askDeleteRule(rule: UfwRuleDto) {
  ruleToDelete.value = rule;
  confirmDialog.value = true;
}

async function deleteRule() {
  if (!serverId.value || !ruleToDelete.value) return;
  const number = ruleToDelete.value.number;
  deletingRule.value = number;
  try {
    state.value = await serversApi.deleteUFWRule(serverId.value, number);
    showMessage(t('firewallPage.ruleDeleted'));
    confirmDialog.value = false;
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.ruleDeleteFailed'), 'error');
  } finally {
    deletingRule.value = null;
  }
}

watch(serverId, () => {
  void loadState();
});

onMounted(async () => {
  await loadServers();
});
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="warning" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="firewall-workspace">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <v-card v-if="selectedServer" variant="outlined" class="firewall-panel" :loading="loadingState">
        <div class="panel-header">
          <div class="min-width-0">
            <div class="d-flex align-center ga-2 mb-1">
              <v-icon color="primary">mdi-shield-lock-outline</v-icon>
              <div class="text-h6 font-weight-bold text-truncate">{{ selectedServer.name }}</div>
            </div>
            <div class="text-body-2 text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
          </div>
          <div class="panel-actions">
            <v-chip :color="statusColor()" variant="tonal" label>{{ statusLabel() }}</v-chip>
            <v-btn icon="mdi-refresh" variant="outlined" size="small" :aria-label="t('common.refresh')" @click="loadState" />
          </div>
        </div>

        <div class="status-grid">
          <div>
            <span>{{ t('firewallPage.status') }}</span>
            <strong>{{ statusLabel() }}</strong>
          </div>
          <div>
            <span>{{ t('firewallPage.defaultPolicy') }}</span>
            <strong>{{ state?.defaultPolicy || t('common.notAvailable') }}</strong>
          </div>
          <div>
            <span>{{ t('firewallPage.passwordlessSudo') }}</span>
            <v-chip :color="canUseSudo ? 'success' : 'warning'" size="small" variant="tonal" label>{{ canUseSudo ? t('common.yes') : t('common.no') }}</v-chip>
          </div>
          <div>
            <span>{{ t('firewallPage.rules') }}</span>
            <strong>{{ state?.rules.length ?? 0 }}</strong>
          </div>
        </div>

        <v-alert v-if="state && !state.installed" type="info" variant="tonal" density="compact" class="my-4">
          <div class="install-row">
            <span>{{ t('firewallPage.ufwMissing') }}</span>
            <v-btn
              color="primary"
              variant="flat"
              size="small"
              prepend-icon="mdi-shield-plus"
              class="text-none"
              :loading="installing"
              :disabled="!selectedServer.reachable || !canUseSudo || !selectedUfwSupported"
              @click="installUFW"
            >
              {{ t('firewallPage.installUfw') }}
            </v-btn>
          </div>
        </v-alert>

        <div class="rule-form">
          <v-text-field v-model.number="ruleForm.port" type="number" :label="t('firewallPage.port')" density="compact" variant="outlined" hide-details />
          <v-select v-model="ruleForm.protocol" :items="protocolOptions" :label="t('firewallPage.protocol')" density="compact" variant="outlined" hide-details />
          <v-text-field v-model="ruleForm.from" :label="t('firewallPage.from')" :placeholder="t('firewallPage.anywhere')" density="compact" variant="outlined" hide-details />
          <v-btn color="primary" variant="flat" prepend-icon="mdi-plus" class="text-none" :loading="savingRule" :disabled="!canAddRule" @click="addRule">
            {{ t('firewallPage.addRule') }}
          </v-btn>
        </div>

        <v-table class="rule-table">
          <thead>
            <tr>
              <th>{{ t('firewallPage.number') }}</th>
              <th>{{ t('firewallPage.to') }}</th>
              <th>{{ t('firewallPage.action') }}</th>
              <th>{{ t('firewallPage.from') }}</th>
              <th class="text-right">{{ t('common.actions') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!state?.rules.length">
              <td colspan="5" class="text-center py-6 text-medium-emphasis">{{ t('firewallPage.noRules') }}</td>
            </tr>
            <tr v-for="rule in state?.rules ?? []" :key="rule.number">
              <td class="font-weight-bold">#{{ rule.number }}</td>
              <td>{{ rule.to }}</td>
              <td><v-chip size="small" color="primary" variant="tonal" label>{{ rule.action }}</v-chip></td>
              <td>{{ rule.from }}</td>
              <td class="text-right">
                <v-btn icon="mdi-delete" size="small" color="error" variant="text" :loading="deletingRule === rule.number" :aria-label="t('common.delete')" @click="askDeleteRule(rule)" />
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>

      <v-card v-else variant="outlined" class="empty-panel">
        <v-icon size="32" color="medium-emphasis">mdi-shield-search</v-icon>
        <div class="text-body-2 text-medium-emphasis">{{ t('firewallPage.selectServer') }}</div>
      </v-card>
    </div>

    <v-dialog v-model="confirmDialog" width="420">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('firewallPage.deleteRuleTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="confirmDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ t('firewallPage.deleteRuleConfirm', { number: ruleToDelete?.number ?? '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="confirmDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" class="text-none" :loading="deletingRule !== null" @click="deleteRule">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template #actions>
        <v-btn v-if="snackbarTaskId" color="white" variant="text" :to="taskRoute()">{{ t('taskCenter.task') }}</v-btn>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.firewall-workspace { display: grid; grid-template-columns: minmax(320px, 0.34fr) minmax(0, 0.66fr); gap: 18px; align-items: start; }
.firewall-panel { min-width: 0; padding: 16px; overflow: hidden; }
.panel-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 16px; }
.panel-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.status-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.status-grid > div { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; padding: 10px 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.status-grid span { color: var(--lp-text-muted); font-size: 0.78rem; }
.status-grid strong { min-width: 0; overflow-wrap: anywhere; text-align: right; font-size: 0.86rem; }
.install-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.rule-form { display: grid; grid-template-columns: minmax(110px, 0.16fr) minmax(130px, 0.18fr) minmax(180px, 1fr) auto; gap: 10px; align-items: center; margin: 16px 0; }
.rule-table { border: 1px solid var(--lp-border); border-radius: 8px; overflow: hidden; }
.empty-panel { min-height: 340px; display: grid; place-items: center; align-content: center; gap: 10px; padding: 32px; text-align: center; }
.min-width-0 { min-width: 0; }
@media (max-width: 1180px) {
  .firewall-workspace { grid-template-columns: 1fr; }
  .status-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 760px) {
  .panel-header, .install-row { flex-direction: column; align-items: stretch; }
  .status-grid, .rule-form { grid-template-columns: 1fr; }
}
</style>
