<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import AppPagination from '@/components/AppPagination.vue';
import AppActionButton from '@/components/AppActionButton.vue';
import AppActionGroup from '@/components/AppActionGroup.vue';
import AppDetailPanel from '@/components/AppDetailPanel.vue';
import AppMasterDetailWorkspace from '@/components/AppMasterDetailWorkspace.vue';
import ServerSelector from '@/components/ServerSelector.vue';
import { serversApi } from '@/api/servers';
import { useI18n } from '@/i18n';
import { usePagination } from '@/composables/usePagination';
import type { FirewallProtocol, ServerDto, UfwRuleDto, UfwStateDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const state = ref<UfwStateDto | null>(null);
const loadingServers = ref(false);
const loadingState = ref(false);
const savingRule = ref(false);
const enabling = ref(false);
const deletingRule = ref<number | null>(null);
const error = ref('');

const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const snackbarTaskId = ref('');
const confirmDialog = ref(false);
const ruleToDelete = ref<UfwRuleDto | null>(null);
const enableDialog = ref(false);
let stateRequestId = 0;
const { t } = useI18n();

const ruleForm = reactive({
  port: 22,
  protocol: 'tcp' as FirewallProtocol,
  from: '',
});

const selectedServer = computed(() => servers.value.find((server) => server.id === serverId.value) ?? null);
const selectedUfwSupported = computed(() => selectedServer.value?.traits?.['sys.ufw_supported'] !== 'false');
const hasPrivilege = computed(() => selectedServer.value?.privilege?.privileged === true || selectedServer.value?.sudo?.passwordless === true);
const canManageRules = computed(() => Boolean(selectedServer.value?.reachable && hasPrivilege.value && state.value?.supported && state.value?.installed));
const canAddRule = computed(() => canManageRules.value && Number(ruleForm.port) > 0 && Number(ruleForm.port) <= 65535);
const rules = computed(() => state.value?.rules ?? []);
const {
  page,
  pageSize,
  total,
  pageItems: pagedRules,
} = usePagination(rules);

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
  const requestedServerId = serverId.value;
  const requestId = ++stateRequestId;
  state.value = null;
  if (!requestedServerId) {
    loadingState.value = false;
    return;
  }
  loadingState.value = true;
  error.value = '';
  try {
    const result = await serversApi.ufwState(requestedServerId);
    if (requestId !== stateRequestId || serverId.value !== requestedServerId) return;
    state.value = result;
  } catch (err) {
    if (requestId !== stateRequestId || serverId.value !== requestedServerId) return;
    error.value = err instanceof Error ? err.message : t('firewallPage.loadFailed');
  } finally {
    if (requestId === stateRequestId && serverId.value === requestedServerId) {
      loadingState.value = false;
    }
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

async function enableUFW() {
  if (!serverId.value) return;
  enabling.value = true;
  try {
    const result = await serversApi.enableUFW(serverId.value);
    enableDialog.value = false;
    showMessage(t('firewallPage.enableStarted'), 'success', result.taskId);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.enableFailed'), 'error');
  } finally {
    enabling.value = false;
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

    <AppMasterDetailWorkspace>
      <template #aside>
        <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />
      </template>

      <AppDetailPanel
        class="security-detail"
        :empty="!selectedServer"
        :empty-text="t('firewallPage.selectServer')"
        :loading="!!selectedServer && loadingState && !state"
        loading-min-height="280px"
      >
        <template v-if="selectedServer" #header>
          <div class="min-width-0">
            <div class="d-flex align-center ga-2 mb-1">
              <v-icon color="primary">mdi-shield-lock-outline</v-icon>
              <div class="text-h6 font-weight-bold text-truncate">{{ selectedServer.name }}</div>
            </div>
            <div class="text-body-2 text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
          </div>
          <AppActionGroup context="detail" class="app-detail-actions">
            <v-chip :color="statusColor()" variant="tonal" label>{{ statusLabel() }}</v-chip>
            <AppActionButton icon="mdi-refresh" :label="t('common.refresh')" @click="loadState" />
          </AppActionGroup>
        </template>

        <template v-if="selectedServer" #body>
          <section class="security-section">
            <div class="section-heading">
              <div>
                <div class="text-subtitle-1 font-weight-bold">{{ t('firewallPage.ufwSection') }}</div>
                <div class="text-caption text-medium-emphasis">{{ t('firewallPage.ufwSectionSubtitle') }}</div>
              </div>
            </div>

            <div class="info-grid">
              <div>
                <span>{{ t('firewallPage.status') }}</span>
                <strong>{{ statusLabel() }}</strong>
              </div>
              <div>
                <span>{{ t('firewallPage.defaultPolicy') }}</span>
                <strong>{{ state?.defaultPolicy || t('common.notAvailable') }}</strong>
              </div>
              <div>
                <span>{{ t('firewallPage.privilegedAccess') }}</span>
                <v-chip :color="hasPrivilege ? 'success' : 'warning'" size="small" variant="tonal" label>{{ hasPrivilege ? t('common.yes') : t('common.no') }}</v-chip>
              </div>
              <div>
                <span>{{ t('firewallPage.rules') }}</span>
                <strong>{{ state?.rules.length ?? 0 }}</strong>
              </div>
            </div>

            <v-alert v-if="state && !state.active" :type="state.installed ? 'warning' : 'info'" variant="tonal" density="compact" class="my-4">
              <div class="install-row">
                <span>{{ state.installed ? t('firewallPage.ufwInactiveHint') : t('firewallPage.ufwMissingEnableHint') }}</span>
                <AppActionButton
                  kind="warning-primary"
                  icon="mdi-shield-check"
                  :label="t('firewallPage.enableUfw')"
                  :disabled="!selectedServer.reachable || !hasPrivilege || !selectedUfwSupported"
                  @click="enableDialog = true"
                />
              </div>
            </v-alert>

            <div class="rule-form">
              <v-text-field v-model.number="ruleForm.port" type="number" :label="t('firewallPage.port')" density="compact" variant="outlined" hide-details />
              <v-select v-model="ruleForm.protocol" :items="protocolOptions" :label="t('firewallPage.protocol')" density="compact" variant="outlined" hide-details />
              <v-text-field v-model="ruleForm.from" :label="t('firewallPage.from')" :placeholder="t('firewallPage.anywhere')" density="compact" variant="outlined" hide-details />
              <AppActionButton kind="primary" icon="mdi-plus" :label="t('firewallPage.addRule')" :loading="savingRule" :disabled="!canAddRule" @click="addRule" />
            </div>

            <div class="rule-table-wrap">
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
                  <tr v-for="rule in pagedRules" :key="rule.number">
                    <td class="font-weight-bold">#{{ rule.number }}</td>
                    <td>{{ rule.to }}</td>
                    <td><v-chip size="small" color="primary" variant="tonal" label>{{ rule.action }}</v-chip></td>
                    <td>{{ rule.from }}</td>
                    <td class="text-right">
                      <AppActionGroup context="table">
                        <AppActionButton kind="danger" icon="mdi-delete" :label="t('common.delete')" :loading="deletingRule === rule.number" @click="askDeleteRule(rule)" />
                      </AppActionGroup>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </div>
            <AppPagination v-model:page="page" v-model:page-size="pageSize" :total="total" />
          </section>
        </template>
      </AppDetailPanel>
    </AppMasterDetailWorkspace>

    <v-dialog v-model="confirmDialog" width="420">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('firewallPage.deleteRuleTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="confirmDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ t('firewallPage.deleteRuleConfirm', { number: ruleToDelete?.number ?? '' }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="confirmDialog = false" />
            <AppActionButton kind="danger-primary" :label="t('common.delete')" :loading="deletingRule !== null" @click="deleteRule" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="enableDialog" width="460">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('firewallPage.enableTitle') }}</span>
          <AppActionButton kind="tool" icon="mdi-close" :label="t('common.close')" @click="enableDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ state?.installed ? t('firewallPage.enableConfirm', { port: selectedServer?.port ?? 22 }) : t('firewallPage.installEnableConfirm', { port: selectedServer?.port ?? 22 }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <AppActionGroup context="dialog">
            <AppActionButton kind="plain" :label="t('common.cancel')" @click="enableDialog = false" />
            <AppActionButton kind="warning-primary" :label="t('firewallPage.enableUfw')" :loading="enabling" @click="enableUFW" />
          </AppActionGroup>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template #actions>
        <AppActionGroup context="snackbar">
          <AppActionButton v-if="snackbarTaskId" kind="snackbar" :label="t('taskCenter.task')" :to="taskRoute()" />
          <AppActionButton kind="snackbar" :label="t('common.close')" @click="snackbar = false" />
        </AppActionGroup>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.security-section { display: flex; flex-direction: column; min-width: 0; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.install-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.rule-form { display: grid; grid-template-columns: minmax(110px, 0.16fr) minmax(130px, 0.18fr) minmax(180px, 1fr) auto; gap: 10px; align-items: center; margin: 16px 0; }
.rule-table { border: 1px solid var(--lp-border); border-radius: 8px; overflow: hidden; }
.rule-table-wrap { min-height: 220px; overflow: auto; }
.min-width-0 { min-width: 0; }
@media (max-width: 760px) {
  .install-row, .section-heading { flex-direction: column; align-items: stretch; }
  .rule-form { grid-template-columns: 1fr; }
  .rule-table-wrap { flex: none; overflow-x: auto; overflow-y: visible; }
}
</style>
