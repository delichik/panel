<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import YAML from 'yaml';
import AppPagination from '@/components/AppPagination.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import ServerSelector from '@/components/ServerSelector.vue';
import { serversApi } from '@/api/servers';
import { useI18n } from '@/i18n';
import { usePagination } from '@/composables/usePagination';
import type { Fail2BanConfigDto, Fail2BanJailDto, Fail2BanStateDto, FirewallProtocol, ServerDto, UfwRuleDto, UfwStateDto } from '@/types/api';

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const state = ref<UfwStateDto | null>(null);
const fail2banState = ref<Fail2BanStateDto | null>(null);
const loadingServers = ref(false);
const loadingState = ref(false);
const savingRule = ref(false);
const enabling = ref(false);
const savingFail2Ban = ref(false);
const installingFail2Ban = ref(false);
const deletingRule = ref<number | null>(null);
const error = ref('');

const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const snackbarTaskId = ref('');
const confirmDialog = ref(false);
const ruleToDelete = ref<UfwRuleDto | null>(null);
const enableDialog = ref(false);
const fail2banTab = ref<'ui' | 'raw'>('ui');
const fail2banYaml = ref('');
const fail2banForm = ref<Fail2BanConfigDto>({ jails: [] });
const fail2banRawError = ref('');
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
const canManageFail2Ban = computed(() => Boolean(selectedServer.value?.reachable && hasPrivilege.value));
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

function fail2banStatusColor() {
  if (!fail2banState.value?.installed) return 'warning';
  if (fail2banState.value.active) return 'success';
  return 'primary';
}

function fail2banStatusLabel() {
  if (!fail2banState.value) return t('common.unknown');
  if (!fail2banState.value.installed) return t('firewallPage.fail2banNotInstalled');
  if (fail2banState.value.active) return t('firewallPage.fail2banActive');
  return t('firewallPage.fail2banInactive');
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
  fail2banState.value = null;
  fail2banYaml.value = '';
  fail2banForm.value = { jails: [] };
  fail2banRawError.value = '';
  if (!requestedServerId) {
    state.value = null;
    loadingState.value = false;
    return;
  }
  loadingState.value = true;
  error.value = '';
  try {
    const [ufwResult, fail2banResult] = await Promise.all([
      serversApi.ufwState(requestedServerId),
      serversApi.fail2BanState(requestedServerId),
    ]);
    if (requestId !== stateRequestId || serverId.value !== requestedServerId) return;
    state.value = ufwResult;
    applyFail2BanState(fail2banResult);
  } catch (err) {
    if (requestId !== stateRequestId || serverId.value !== requestedServerId) return;
    error.value = err instanceof Error ? err.message : t('firewallPage.loadFailed');
  } finally {
    if (requestId === stateRequestId && serverId.value === requestedServerId) {
      loadingState.value = false;
    }
  }
}

function applyFail2BanState(result: Fail2BanStateDto) {
  fail2banState.value = result;
  fail2banYaml.value = result.configYaml || YAML.stringify(result.config ?? { jails: [] });
  fail2banForm.value = cloneConfig(result.config ?? { jails: [] });
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

function cloneConfig(config: Fail2BanConfigDto): Fail2BanConfigDto {
  return { jails: (config.jails ?? []).map((jail) => ({ ...jail, ignoreip: [...(jail.ignoreip ?? [])], options: { ...(jail.options ?? {}) } })) };
}

function newJail(): Fail2BanJailDto {
  return {
    name: 'sshd',
    enabled: true,
    filter: 'sshd',
    port: 'ssh',
    logpath: '/var/log/auth.log',
    backend: 'systemd',
    maxretry: 5,
    findtime: '10m',
    bantime: '1h',
    ignoreip: ['127.0.0.1/8'],
    options: {},
  };
}

function addJail() {
  const existing = new Set(fail2banForm.value.jails.map((jail) => jail.name));
  const jail = newJail();
  let index = 2;
  while (existing.has(jail.name)) {
    jail.name = `custom-${index}`;
    index += 1;
  }
  fail2banForm.value = { jails: [...fail2banForm.value.jails, jail] };
  syncRawFromUi();
}

function removeJail(index: number) {
  fail2banForm.value = { jails: fail2banForm.value.jails.filter((_, itemIndex) => itemIndex !== index) };
  syncRawFromUi();
}

function addOption(jail: Fail2BanJailDto) {
  const options = { ...(jail.options ?? {}) };
  let key = 'mode';
  let index = 2;
  while (Object.prototype.hasOwnProperty.call(options, key)) {
    key = `option${index}`;
    index += 1;
  }
  options[key] = '';
  jail.options = options;
  syncRawFromUi();
}

function removeOption(jail: Fail2BanJailDto, key: string) {
  const options = { ...(jail.options ?? {}) };
  delete options[key];
  jail.options = options;
  syncRawFromUi();
}

function renameOption(jail: Fail2BanJailDto, oldKey: string, nextKey: string) {
  nextKey = nextKey.trim();
  if (!nextKey || nextKey === oldKey) return;
  const options = { ...(jail.options ?? {}) };
  const value = options[oldKey] ?? '';
  delete options[oldKey];
  options[nextKey] = value;
  jail.options = options;
  syncRawFromUi();
}

function optionEntries(jail: Fail2BanJailDto) {
  return Object.entries(jail.options ?? {});
}

function parseRawConfig() {
  fail2banRawError.value = '';
  try {
    const parsed = YAML.parse(fail2banYaml.value || 'jails: []') as Fail2BanConfigDto;
    if (!parsed || !Array.isArray(parsed.jails)) throw new Error(t('firewallPage.fail2banRawInvalidShape'));
    fail2banForm.value = cloneConfig(parsed);
    return parsed;
  } catch (err) {
    fail2banRawError.value = err instanceof Error ? err.message : t('firewallPage.fail2banRawInvalid');
    return null;
  }
}

function syncRawFromUi() {
  if (fail2banTab.value !== 'ui') return;
  fail2banYaml.value = YAML.stringify(fail2banForm.value);
}

function onTabUpdate(value: unknown) {
  if (value === 'raw') {
    syncRawFromUi();
  } else if (value === 'ui') {
    parseRawConfig();
  }
}

async function saveFail2Ban() {
  if (!serverId.value || !canManageFail2Ban.value) return;
  const config = fail2banTab.value === 'raw' ? parseRawConfig() : fail2banForm.value;
  if (!config) return;
  if (fail2banTab.value === 'ui') syncRawFromUi();
  savingFail2Ban.value = true;
  try {
    const result = await serversApi.saveFail2Ban(serverId.value, { configYaml: fail2banYaml.value });
    showMessage(t('firewallPage.fail2banSaveStarted'), 'success', result.taskId);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.fail2banSaveFailed'), 'error');
  } finally {
    savingFail2Ban.value = false;
  }
}

async function installFail2Ban() {
  if (!serverId.value || !canManageFail2Ban.value) return;
  installingFail2Ban.value = true;
  try {
    const result = await serversApi.installFail2Ban(serverId.value);
    showMessage(t('firewallPage.fail2banInstallStarted'), 'success', result.taskId);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.fail2banInstallFailed'), 'error');
  } finally {
    installingFail2Ban.value = false;
  }
}

watch(serverId, () => {
  void loadState();
});

watch(fail2banTab, onTabUpdate);

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
            <v-chip :color="fail2banStatusColor()" variant="tonal" label>{{ fail2banStatusLabel() }}</v-chip>
            <v-btn icon="mdi-refresh" variant="outlined" size="small" :aria-label="t('common.refresh')" @click="loadState" />
          </div>
        </div>

        <div class="firewall-panel-body">
          <PageLoadingState v-if="loadingState && !state" min-height="280px" />

          <template v-else>
            <section class="security-section">
              <div class="section-heading">
                <div>
                  <div class="text-subtitle-1 font-weight-bold">{{ t('firewallPage.ufwSection') }}</div>
                  <div class="text-caption text-medium-emphasis">{{ t('firewallPage.ufwSectionSubtitle') }}</div>
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
                  <v-btn
                    color="warning"
                    variant="flat"
                    size="small"
                    prepend-icon="mdi-shield-check"
                    class="text-none"
                    :disabled="!selectedServer.reachable || !hasPrivilege || !selectedUfwSupported"
                    @click="enableDialog = true"
                  >
                    {{ t('firewallPage.enableUfw') }}
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
                        <v-btn icon="mdi-delete" size="small" color="error" variant="text" :loading="deletingRule === rule.number" :aria-label="t('common.delete')" @click="askDeleteRule(rule)" />
                      </td>
                    </tr>
                  </tbody>
                </v-table>
              </div>
              <AppPagination v-model:page="page" v-model:page-size="pageSize" :total="total" />
            </section>

            <v-divider class="my-5" />

            <section class="security-section fail2ban-section">
              <div class="section-heading">
                <div>
                  <div class="text-subtitle-1 font-weight-bold">{{ t('firewallPage.fail2banSection') }}</div>
                  <div class="text-caption text-medium-emphasis">{{ t('firewallPage.fail2banSectionSubtitle') }}</div>
                </div>
                <div class="panel-actions">
                  <v-btn
                    color="primary"
                    variant="outlined"
                    prepend-icon="mdi-download"
                    class="text-none"
                    :loading="installingFail2Ban"
                    :disabled="!canManageFail2Ban"
                    @click="installFail2Ban"
                  >
                    {{ fail2banState?.installed ? t('firewallPage.fail2banApply') : t('firewallPage.fail2banInstall') }}
                  </v-btn>
                  <v-btn
                    color="primary"
                    variant="flat"
                    prepend-icon="mdi-content-save"
                    class="text-none"
                    :loading="savingFail2Ban"
                    :disabled="!canManageFail2Ban"
                    @click="saveFail2Ban"
                  >
                    {{ t('common.save') }}
                  </v-btn>
                </div>
              </div>

              <div class="status-grid status-grid--three">
                <div>
                  <span>{{ t('firewallPage.status') }}</span>
                  <strong>{{ fail2banStatusLabel() }}</strong>
                </div>
                <div>
                  <span>{{ t('firewallPage.fail2banRunningJails') }}</span>
                  <strong>{{ fail2banState?.jails?.length ?? 0 }}</strong>
                </div>
                <div>
                  <span>{{ t('firewallPage.fail2banManagedJails') }}</span>
                  <strong>{{ fail2banForm.jails.length }}</strong>
                </div>
              </div>

              <v-tabs v-model="fail2banTab" density="compact" class="mt-4">
                <v-tab value="ui">{{ t('firewallPage.fail2banUiMode') }}</v-tab>
                <v-tab value="raw">{{ t('firewallPage.fail2banRawMode') }}</v-tab>
              </v-tabs>

              <v-window v-model="fail2banTab" class="fail2ban-editor">
                <v-window-item value="ui">
                  <div class="jail-list">
                    <v-card v-for="(jail, index) in fail2banForm.jails" :key="`${jail.name}-${index}`" variant="outlined" class="jail-card">
                      <div class="jail-card-header">
                        <v-switch v-model="jail.enabled" color="primary" density="compact" hide-details :label="t('firewallPage.fail2banEnabled')" @update:model-value="syncRawFromUi" />
                        <v-btn icon="mdi-delete" color="error" variant="text" size="small" :aria-label="t('common.delete')" @click="removeJail(index)" />
                      </div>
                      <div class="jail-grid">
                        <v-text-field v-model="jail.name" :label="t('firewallPage.fail2banJailName')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.filter" :label="t('firewallPage.fail2banFilter')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.port" :label="t('firewallPage.port')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.protocol" :label="t('firewallPage.protocol')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.logpath" :label="t('firewallPage.fail2banLogPath')" variant="outlined" density="compact" class="span-all" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.backend" :label="t('firewallPage.fail2banBackend')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.action" :label="t('firewallPage.fail2banAction')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model.number="jail.maxretry" type="number" :label="t('firewallPage.fail2banMaxRetry')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.findtime" :label="t('firewallPage.fail2banFindTime')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-text-field v-model="jail.bantime" :label="t('firewallPage.fail2banBanTime')" variant="outlined" density="compact" @update:model-value="syncRawFromUi" />
                        <v-combobox v-model="jail.ignoreip" :label="t('firewallPage.fail2banIgnoreIp')" multiple chips closable-chips variant="outlined" density="compact" class="span-all" @update:model-value="syncRawFromUi" />
                      </div>

                      <div class="option-list">
                        <div class="option-list-header">
                          <span>{{ t('firewallPage.fail2banOptions') }}</span>
                          <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addOption(jail)">{{ t('firewallPage.fail2banAddOption') }}</v-btn>
                        </div>
                        <div v-for="[key, value] in optionEntries(jail)" :key="key" class="option-row">
                          <v-text-field :model-value="key" :label="t('common.name')" density="compact" variant="outlined" hide-details @update:model-value="renameOption(jail, key, String($event))" />
                          <v-text-field :model-value="value" :label="t('common.value')" density="compact" variant="outlined" hide-details @update:model-value="jail.options = { ...(jail.options ?? {}), [key]: String($event) }; syncRawFromUi()" />
                          <v-btn icon="mdi-close" color="error" variant="text" size="small" :aria-label="t('common.delete')" @click="removeOption(jail, key)" />
                        </div>
                      </div>
                    </v-card>

                    <v-btn variant="outlined" prepend-icon="mdi-plus" class="text-none align-self-start" @click="addJail">
                      {{ t('firewallPage.fail2banAddJail') }}
                    </v-btn>
                  </div>
                </v-window-item>

                <v-window-item value="raw">
                  <v-alert v-if="fail2banRawError" type="error" variant="tonal" density="compact" class="mb-3">{{ fail2banRawError }}</v-alert>
                  <v-textarea
                    v-model="fail2banYaml"
                    :label="t('firewallPage.fail2banRawYaml')"
                    variant="outlined"
                    density="compact"
                    rows="18"
                    class="raw-yaml"
                    spellcheck="false"
                    @blur="parseRawConfig"
                  />
                </v-window-item>
              </v-window>
            </section>
          </template>
        </div>
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

    <v-dialog v-model="enableDialog" width="460">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('firewallPage.enableTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="enableDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body text-body-1">
          {{ state?.installed ? t('firewallPage.enableConfirm', { port: selectedServer?.port ?? 22 }) : t('firewallPage.installEnableConfirm', { port: selectedServer?.port ?? 22 }) }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="enableDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="warning" variant="flat" class="text-none" :loading="enabling" @click="enableUFW">{{ t('firewallPage.enableUfw') }}</v-btn>
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
.firewall-workspace { display: grid; grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr); gap: 18px; flex: 1 1 auto; min-height: 0; align-items: stretch; }
.firewall-panel { display: flex; flex-direction: column; min-width: 0; min-height: 0; padding: 16px; overflow: hidden; }
.panel-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 16px; }
.panel-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.firewall-panel-body { display: flex; flex: 1 1 auto; flex-direction: column; min-height: 0; overflow: auto; padding-right: 2px; }
.security-section { display: flex; flex-direction: column; min-width: 0; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.status-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
.status-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.status-grid > div { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; padding: 10px 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.status-grid span { color: var(--lp-text-muted); font-size: 0.78rem; }
.status-grid strong { min-width: 0; overflow-wrap: anywhere; text-align: right; font-size: 0.86rem; }
.install-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.rule-form { display: grid; grid-template-columns: minmax(110px, 0.16fr) minmax(130px, 0.18fr) minmax(180px, 1fr) auto; gap: 10px; align-items: center; margin: 16px 0; }
.rule-table { border: 1px solid var(--lp-border); border-radius: 8px; overflow: hidden; }
.rule-table-wrap { min-height: 220px; overflow: auto; }
.fail2ban-editor { margin-top: 12px; }
.jail-list { display: flex; flex-direction: column; gap: 12px; }
.jail-card { padding: 14px; }
.jail-card-header { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px; }
.jail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 12px; }
.span-all { grid-column: 1 / -1; }
.option-list { display: flex; flex-direction: column; gap: 8px; margin-top: 4px; }
.option-list-header { display: flex; align-items: center; justify-content: space-between; gap: 10px; color: var(--lp-text-muted); font-size: 0.78rem; font-weight: 650; }
.option-row { display: grid; grid-template-columns: minmax(120px, 0.34fr) minmax(0, 1fr) auto; gap: 8px; align-items: center; }
.raw-yaml { font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace; }
.empty-panel { min-height: 340px; display: grid; place-items: center; align-content: center; gap: 10px; padding: 32px; text-align: center; }
.min-width-0 { min-width: 0; }
@media (max-width: 1080px) {
  .firewall-workspace { grid-template-columns: 1fr; }
  .status-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 760px) {
  .panel-header, .install-row, .section-heading { flex-direction: column; align-items: stretch; }
  .status-grid, .status-grid--three, .rule-form, .jail-grid, .option-row { grid-template-columns: 1fr; }
  .firewall-workspace { flex: none; min-height: auto; }
  .firewall-panel { overflow: visible; }
  .firewall-panel-body { flex: none; overflow: visible; }
  .rule-table-wrap { flex: none; overflow-x: auto; overflow-y: visible; }
}
</style>
