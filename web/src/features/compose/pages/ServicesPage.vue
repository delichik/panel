<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { composeApi } from '@/api/compose';
import { serversApi } from '@/api/servers';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';
import type {
  ComposeServiceDto,
  ServerDto,
  ServiceTemplateDto,
  TaskDto,
} from '@/types/api';

const services = ref<ComposeServiceDto[]>([]);
const templates = ref<ServiceTemplateDto[]>([]);
const servers = ref<ServerDto[]>([]);
const loading = ref(false);
const taskId = ref('');
const taskTitle = ref('Compose Service Task');
const actionLoading = ref('');
const error = ref('');

// Expand states for template panels
const expandedPanels = ref<string[]>([]);

// Snackbar notification state
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

function safeArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

async function load() {
  loading.value = true;
  try {
    const [serviceRows, templateRows, serverRows] = await Promise.all([
      composeApi.listServices(),
      composeApi.listTemplates(),
      serversApi.listServers(),
    ]);
    services.value = safeArray(serviceRows);
    templates.value = safeArray(templateRows).map((template) => ({
      ...template,
      dependencies: safeArray(template.dependencies),
      variables: safeArray(template.variables)
    }));
    servers.value = safeArray(serverRows);
    error.value = '';

    // Auto-expand all active templates on first load
    if (expandedPanels.value.length === 0) {
      expandedPanels.value = templates.value.filter(t => t.active).map(t => t.id);
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load services';
  } finally {
    loading.value = false;
  }
}

// 评估服务器特征是否匹配模板选择器 (前端零延迟本地评估)
function evaluateServerMatch(template: ServiceTemplateDto, srv: ServerDto): boolean {
  if (!template.active) return false;
  const selector = template.traitSelector?.trim() ?? '';
  if (!selector) return false; // 空规则默认不匹配任何节点
  if (!srv.reachable || !srv.os?.supported) return false;

  const rules = selector.split('&&').map(part => {
    part = part.trim();
    const operators = ['==', '!=', '>=', '<=', '>', '<'];
    for (const op of operators) {
      const idx = part.indexOf(op);
      if (idx !== -1) {
        const key = part.slice(0, idx).trim();
        let val = part.slice(idx + op.length).trim();
        if ((val.startsWith('"') && val.endsWith('"')) || (val.startsWith("'") && val.endsWith("'"))) {
          val = val.slice(1, -1);
        }
        return { key, operator: op, value: val };
      }
    }
    return { key: '', operator: '==', value: '' };
  }).filter(r => r.key);

  if (rules.length === 0) return false;

  return rules.every(rule => {
    const actualVal = srv.traits?.[rule.key] ?? '';
    const compareVal = rule.value;

    if (rule.operator === '==') return actualVal === compareVal;
    if (rule.operator === '!=') return actualVal !== compareVal;

    const actNum = parseFloat(actualVal);
    const compNum = parseFloat(compareVal);

    if (!isNaN(actNum) && !isNaN(compNum)) {
      if (rule.operator === '>=') return actNum >= compNum;
      if (rule.operator === '<=') return actNum <= compNum;
      if (rule.operator === '>') return actNum > compNum;
      if (rule.operator === '<') return actNum < compNum;
    }

    if (rule.operator === '>=') return actualVal >= compareVal;
    if (rule.operator === '<=') return actualVal <= compareVal;
    if (rule.operator === '>') return actualVal > compareVal;
    if (rule.operator === '<') return actualVal < compareVal;

    return false;
  });
}

// 寻找特定模板在特定服务器上的 DeployedService 运行记录
function getDeployedService(templateId: string, serverId: string): ComposeServiceDto | undefined {
  return services.value.find(s => s.templateId === templateId && s.serverId === serverId);
}

// 计算服务器在特定模板下的集群拓扑节点状态和对应颜色
function getNodeTopologyState(template: ServiceTemplateDto, srv: ServerDto) {
  const isMatch = evaluateServerMatch(template, srv);
  const svc = getDeployedService(template.id, srv.id);

  if (isMatch) {
    if (!svc) {
      return { text: 'Targeted: Pending Reconcile', color: 'warning', icon: 'mdi-progress-clock', status: 'pending' };
    }
    const status = svc.runtimeStatus || svc.status;
    if (status === 'deploying') {
      return { text: 'Targeted: Deploying', color: 'info', icon: 'mdi-loading mdi-spin', status: 'deploying' };
    }
    if (status === 'ready' || status === 'running') {
      return { text: 'Targeted: Running', color: 'success', icon: 'mdi-check-circle', status: 'running' };
    }
    if (status === 'failed' || status === 'error') {
      return { text: 'Targeted: Deploy Failed', color: 'error', icon: 'mdi-alert-circle', status: 'failed' };
    }
    return { text: `Targeted: ${status}`, color: 'info', icon: 'mdi-server', status };
  } else {
    if (!svc) {
      return { text: 'Unmatched: Evicted', color: 'grey-darken-1', icon: 'mdi-minus-circle-outline', status: 'evicted' };
    }
    const status = svc.runtimeStatus || svc.status;
    if (status === 'removing') {
      return { text: 'Unmatched: Evicting', color: 'orange-darken-2', icon: 'mdi-loading mdi-spin', status: 'evicting' };
    }
    if (status === 'eviction_failed') {
      return { text: 'Unmatched: Evict Failed', color: 'error', icon: 'mdi-alert', status: 'eviction_failed' };
    }
    return { text: `Unmatched: Residue (${status})`, color: 'orange', icon: 'mdi-alert-rhombus', status: 'residue' };
  }
}

async function runAction(serviceId: string, serviceName: string, action: 'deploy' | 'sync' | 'restart' | 'stop' | 'remove') {
  actionLoading.value = `${action}:${serviceId}`;
  try {
    const result =
      action === 'deploy'
        ? await composeApi.deployService(serviceId)
        : action === 'sync'
          ? await composeApi.syncService(serviceId)
          : action === 'restart'
            ? await composeApi.restartService(serviceId)
            : action === 'stop'
              ? await composeApi.stopService(serviceId)
              : await composeApi.removeService(serviceId);
    taskId.value = result.taskId;
    taskTitle.value = `${action[0].toUpperCase()}${action.slice(1)} ${serviceName}`;
    showMessage(`${action[0].toUpperCase()}${action.slice(1)} started`);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : `Unable to ${action} service`, 'error');
  } finally {
    actionLoading.value = '';
  }
}

async function handleTaskFinished(_task: TaskDto) {
  await load();
}

onMounted(load);
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Fleet Deployments</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Declarative cluster scheduler monitor. Groups templates, evaluates Node Traits, and auto-aligns fleet state.</p>
      </div>
      <div>
        <v-btn
          prepend-icon="mdi-refresh"
          :loading="loading"
          variant="outlined"
          @click="load"
          class="text-none font-weight-bold"
        >
          Reconcile Now
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <!-- Active Task Status Panel -->
    <v-card v-if="taskId" class="mb-6 pa-4 border" variant="flat" color="grey-lighten-4">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">
        <v-icon start icon="mdi-console-line" color="primary"></v-icon>
        {{ taskTitle }}
      </v-card-title>
      <v-card-text class="px-0 pb-0">
        <TaskLogPanel :task-id="taskId" compact @finished="handleTaskFinished" />
      </v-card-text>
    </v-card>

    <v-alert v-if="templates.length === 0" type="info" variant="tonal" class="text-left">
      No service templates created yet. Go to <strong>Service Templates</strong> page to define templates and enable Fleet placement rules.
    </v-alert>

    <!-- Fleet Placement Reconcile Grid (集群微服务对账折叠面板) -->
    <v-expansion-panels v-model="expandedPanels" multiple class="mb-6">
      <v-expansion-panel
        v-for="tpl in templates"
        :key="tpl.id"
        :value="tpl.id"
        variant="outlined"
        class="mb-3 rounded-lg border overflow-hidden"
      >
        <v-expansion-panel-title class="py-4 bg-surface">
          <div class="d-flex align-center w-100 pr-4" style="gap: 16px;">
            <!-- 模板标识与名称 -->
            <div>
              <div class="text-subtitle-1 font-weight-bold text-left">{{ tpl.name }}</div>
              <div class="text-caption text-grey-darken-1 text-left">{{ tpl.description || 'No description' }}</div>
            </div>

            <v-spacer />

            <!-- Selector 描述展示 -->
            <div class="text-right d-none d-md-block">
              <span class="text-caption text-grey-darken-2 font-weight-bold">Traits Selector:</span>
              <v-chip size="small" variant="outlined" color="primary" class="ml-2 font-mono" label>
                {{ tpl.traitSelector || 'Global (All Servers)' }}
              </v-chip>
            </div>

            <!-- 全局编排激活状态 -->
            <div>
              <v-chip
                v-if="tpl.active"
                color="success"
                size="small"
                prepend-icon="mdi-sync"
                label
                class="font-weight-bold"
              >
                Fleet Active
              </v-chip>
              <v-chip
                v-else
                color="grey"
                size="small"
                prepend-icon="mdi-sync-off"
                label
              >
                Inactive
              </v-chip>
            </div>
          </div>
        </v-expansion-panel-title>

        <v-expansion-panel-text class="pa-0 border-top bg-grey-lighten-5">
          <v-table class="text-left" style="background: transparent;">
            <thead>
              <tr class="bg-grey-lighten-4">
                <th class="font-weight-bold" style="width: 240px;">Server Node</th>
                <th class="font-weight-bold">IP & Host</th>
                <th class="font-weight-bold">Topology Placement Role</th>
                <th class="font-weight-bold text-right" style="width: 320px;">Orchestration Lifecycle Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="servers.length === 0">
                <td colspan="4" class="text-center py-6 text-grey-darken-1">No managed servers registered</td>
              </tr>
              <tr v-for="srv in servers" :key="srv.id">
                <!-- 服务器名 -->
                <td class="font-weight-bold py-3">
                  <div>{{ srv.name }}</div>
                  <div class="text-caption text-grey-darken-1">
                    {{ srv.traits?.['sys.hostname'] || 'unknown hostname' }}
                  </div>
                </td>

                <!-- IP/Host -->
                <td class="font-tabular">{{ srv.host }}:{{ srv.port }}</td>

                <!-- 集群调度拓扑匹配与对账状态 -->
                <td>
                  <v-chip
                    :color="getNodeTopologyState(tpl, srv).color"
                    size="small"
                    :prepend-icon="getNodeTopologyState(tpl, srv).icon"
                    label
                    class="font-weight-bold"
                  >
                    {{ getNodeTopologyState(tpl, srv).text }}
                  </v-chip>
                </td>

                <!-- 声明式生命周期动作 (Restart/Stop/Evict/Logs) -->
                <td class="text-right">
                  <div v-if="getDeployedService(tpl.id, srv.id)" class="d-flex justify-end align-center" style="gap: 8px;">
                    <v-chip size="x-small" variant="tonal" class="mr-2 font-mono">
                      v{{ getDeployedService(tpl.id, srv.id)?.templateVersion }}
                    </v-chip>

                    <!-- 当部署失败或驱逐失败时，提供手动重试动作 -->
                    <v-btn
                      v-if="getNodeTopologyState(tpl, srv).status === 'failed'"
                      size="small"
                      color="primary"
                      variant="flat"
                      prepend-icon="mdi-refresh"
                      class="text-none font-weight-bold"
                      :loading="actionLoading === `deploy:${getDeployedService(tpl.id, srv.id)?.id}`"
                      @click="runAction(getDeployedService(tpl.id, srv.id)!.id, tpl.name, 'deploy')"
                    >
                      Deploy Now
                    </v-btn>

                    <v-btn
                      v-else-if="getNodeTopologyState(tpl, srv).status === 'eviction_failed'"
                      size="small"
                      color="error"
                      variant="flat"
                      prepend-icon="mdi-trash-can"
                      class="text-none font-weight-bold"
                      :loading="actionLoading === `remove:${getDeployedService(tpl.id, srv.id)?.id}`"
                      @click="runAction(getDeployedService(tpl.id, srv.id)!.id, tpl.name, 'remove')"
                    >
                      Force Evict
                    </v-btn>

                    <!-- 正常微服务运行中的常规操作 (Restart, Stop) -->
                    <template v-else>
                      <v-btn
                        size="small"
                        variant="outlined"
                        density="comfortable"
                        prepend-icon="mdi-restart"
                        class="text-none font-weight-bold"
                        :loading="actionLoading === `restart:${getDeployedService(tpl.id, srv.id)?.id}`"
                        @click="runAction(getDeployedService(tpl.id, srv.id)!.id, tpl.name, 'restart')"
                      >
                        Restart
                      </v-btn>
                      <v-btn
                        size="small"
                        variant="outlined"
                        color="warning"
                        density="comfortable"
                        prepend-icon="mdi-stop"
                        class="text-none font-weight-bold"
                        :loading="actionLoading === `stop:${getDeployedService(tpl.id, srv.id)?.id}`"
                        @click="runAction(getDeployedService(tpl.id, srv.id)!.id, tpl.name, 'stop')"
                      >
                        Stop
                      </v-btn>
                    </template>
                  </div>
                  <div v-else class="text-caption text-grey-darken-1 pr-4">
                    {{ tpl.active ? 'Non-targeted' : 'Reconciler disabled' }}
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-expansion-panel-text>
      </v-expansion-panel>
    </v-expansion-panels>
  </div>
</template>

<style scoped>
.font-mono {
  font-family: monospace !important;
}
.font-tabular {
  font-variant-numeric: tabular-nums;
}
</style>
