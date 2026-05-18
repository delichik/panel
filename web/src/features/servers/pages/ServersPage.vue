<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { Connection, Delete, Edit, Plus, Refresh } from '@element-plus/icons-vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { serversApi, type CredentialInput, type ServerInput } from '@/api/servers';
import type { CredentialDto, ServerDto } from '@/types/api';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';

const servers = ref<ServerDto[]>([]);
const credentials = ref<CredentialDto[]>([]);
const loading = ref(false);
const error = ref('');
const serverDialog = ref(false);
const credentialDialog = ref(false);
const editing = ref<ServerDto | null>(null);
const editingCredential = ref<CredentialDto | null>(null);
const activeTaskId = ref('');
const activeTaskServerName = ref('');
const activeTab = ref('servers');

const serverForm = reactive<ServerInput>({
  name: '',
  host: '',
  port: 22,
  sshUsername: '',
  credentialId: null,
  labels: [],
  notes: '',
});

const credentialForm = reactive<CredentialInput>({
  name: '',
  type: 'password',
  username: '',
  password: '',
  privateKey: '',
  passphrase: '',
});

const credentialOptions = computed(() =>
  credentials.value.map((credential) => ({
    label: `${credential.name} (${credential.username}, ${credential.type})`,
    value: credential.id,
  })),
);

function credentialById(id?: string | null) {
  return credentials.value.find((credential) => credential.id === id);
}

function resetServerForm(server?: ServerDto) {
  editing.value = server ?? null;
  Object.assign(serverForm, {
    name: server?.name ?? '',
    host: server?.host ?? '',
    port: server?.port ?? 22,
    sshUsername: server?.sshUsername ?? '',
    credentialId: server?.credentialId ?? null,
    labels: server?.labels ?? [],
    notes: server?.notes ?? '',
  });
  serverDialog.value = true;
}

function resetCredentialForm() {
  editingCredential.value = null;
  Object.assign(credentialForm, {
    name: '',
    type: 'password',
    username: '',
    password: '',
    privateKey: '',
    passphrase: '',
  });
  credentialDialog.value = true;
}

function editCredential(credential: CredentialDto) {
  editingCredential.value = credential;
  Object.assign(credentialForm, {
    name: credential.name,
    type: credential.type,
    username: credential.username,
    password: '',
    privateKey: '',
    passphrase: '',
  });
  credentialDialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    const [serverRows, credentialRows] = await Promise.all([
      serversApi.listServers(),
      serversApi.listCredentials(),
    ]);
    servers.value = serverRows;
    credentials.value = credentialRows;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load servers';
  } finally {
    loading.value = false;
  }
}

async function saveServer() {
  if (editing.value) {
    await serversApi.updateServer(editing.value.id, serverForm);
  } else {
    await serversApi.createServer(serverForm);
  }
  serverDialog.value = false;
  await load();
}

async function saveCredential() {
  const input = { ...credentialForm };
  if (input.type === 'password') {
    delete input.privateKey;
    delete input.passphrase;
  } else {
    delete input.password;
  }
  const credential = editingCredential.value
    ? await serversApi.updateCredential(editingCredential.value.id, input)
    : await serversApi.createCredential(input);
  serverForm.credentialId = credential.id;
  credentialDialog.value = false;
  await load();
}

async function deleteServer(server: ServerDto) {
  await ElMessageBox.confirm(`Delete server ${server.name}?`, 'Confirm delete', { type: 'warning' });
  await serversApi.deleteServer(server.id);
  await load();
}

async function deleteCredential(credential: CredentialDto) {
  await ElMessageBox.confirm(`Delete credential ${credential.name}?`, 'Confirm delete', { type: 'warning' });
  await serversApi.deleteCredential(credential.id);
  await load();
}

async function testServer(server: ServerDto) {
  const result = await serversApi.testConnection(server.id);
  activeTaskId.value = result.taskId;
  activeTaskServerName.value = server.name;
  ElMessage.success('Connectivity test started');
}

async function handleTaskFinished() {
  await load();
  ElMessage.success('Connectivity test finished');
}

onMounted(load);
</script>

<template>
  <div>
    <div class="panel-header panel">
      <div>
        <p class="page-subtitle">Register SSH targets, attach credentials, and validate distro readiness.</p>
      </div>
      <div class="toolbar">
        <el-button :icon="Refresh" :loading="loading" @click="load">Refresh</el-button>
        <el-button v-if="activeTab === 'credentials'" type="primary" :icon="Plus" @click="resetCredentialForm()">
          Credential
        </el-button>
        <el-button v-else type="primary" :icon="Plus" @click="resetServerForm()">Server</el-button>
      </div>
    </div>

    <el-alert v-if="error" class="page-alert" type="error" :title="error" show-icon />

    <section class="panel table-panel">
      <el-tabs v-model="activeTab" class="entity-tabs">
        <el-tab-pane label="Servers" name="servers">
          <el-table v-loading="loading" :data="servers" empty-text="No servers registered">
            <el-table-column prop="name" label="Name" min-width="160" />
            <el-table-column prop="host" label="Host" min-width="160" />
            <el-table-column label="SSH User" width="170">
              <template #default="{ row }">
                {{ row.sshUsername || credentialById(row.credentialId)?.username || 'credential' }}
                <div v-if="row.sshUsername" class="muted">override</div>
                <div v-else class="muted">from credential</div>
              </template>
            </el-table-column>
            <el-table-column label="Distro" min-width="190">
              <template #default="{ row }">
                <el-tag :type="row.os?.supported ? 'success' : 'warning'">
                  {{ row.os?.prettyName || 'unknown' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="Sudo" width="140">
              <template #default="{ row }">
                <span :class="['status-dot', row.sudo?.passwordless ? 'ok' : 'warn']"></span>
                {{ row.sudo?.passwordless ? 'passwordless' : 'unchecked' }}
              </template>
            </el-table-column>
            <el-table-column label="Actions" width="260" fixed="right">
              <template #default="{ row }">
                <el-button size="small" :icon="Connection" @click="testServer(row)">Test</el-button>
                <el-button size="small" :icon="Edit" @click="resetServerForm(row)">Edit</el-button>
                <el-button size="small" type="danger" :icon="Delete" @click="deleteServer(row)">Delete</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="Credentials" name="credentials">
          <el-table v-loading="loading" :data="credentials" empty-text="No credentials registered">
            <el-table-column prop="name" label="Name" min-width="180" />
            <el-table-column prop="username" label="Username" min-width="150" />
            <el-table-column prop="type" label="Type" width="140" />
            <el-table-column label="Actions" width="180" fixed="right">
              <template #default="{ row }">
                <el-button size="small" :icon="Edit" @click="editCredential(row)">Edit</el-button>
                <el-button size="small" type="danger" :icon="Delete" @click="deleteCredential(row)">Delete</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </section>

    <section v-if="activeTaskId" class="panel task-section">
      <div class="panel-header">
        <strong>Connectivity Test</strong>
      </div>
      <div class="panel-body">
        <TaskLogPanel
          :task-id="activeTaskId"
          :server-name="activeTaskServerName"
          compact
          @finished="handleTaskFinished"
        />
      </div>
    </section>

    <el-dialog v-model="serverDialog" :title="editing ? 'Edit server' : 'Add server'" width="560px">
      <el-form label-position="top">
        <el-form-item label="Name"><el-input v-model="serverForm.name" /></el-form-item>
        <el-form-item label="Host"><el-input v-model="serverForm.host" /></el-form-item>
        <el-form-item label="Port"><el-input-number v-model="serverForm.port" :min="1" :max="65535" /></el-form-item>
        <el-form-item label="Credential">
          <el-select v-model="serverForm.credentialId" class="wide" placeholder="Select credential">
            <el-option
              v-for="item in credentialOptions"
              :key="item.value"
              :label="item.label"
              :value="item.value"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="SSH username override">
          <el-input
            v-model="serverForm.sshUsername"
            placeholder="Optional. Leave empty to use the credential username."
          />
        </el-form-item>
        <el-form-item label="Labels"><el-select v-model="serverForm.labels" multiple allow-create filterable /></el-form-item>
        <el-form-item label="Notes"><el-input v-model="serverForm.notes" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="serverDialog = false">Cancel</el-button>
        <el-button type="primary" @click="saveServer">Save</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="credentialDialog" :title="editingCredential ? 'Edit credential' : 'Create credential'" width="620px">
      <el-form label-position="top">
        <el-form-item label="Name"><el-input v-model="credentialForm.name" /></el-form-item>
        <el-form-item label="Type">
          <el-radio-group v-model="credentialForm.type">
            <el-radio-button label="password">Password</el-radio-button>
            <el-radio-button label="private_key">Private key</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="Username"><el-input v-model="credentialForm.username" /></el-form-item>
        <el-form-item v-if="credentialForm.type === 'password'" label="Password">
          <el-input
            v-model="credentialForm.password"
            type="password"
            show-password
            :placeholder="editingCredential ? 'Leave empty to keep existing password' : ''"
          />
        </el-form-item>
        <template v-else>
          <el-form-item label="Private key">
            <el-input
              v-model="credentialForm.privateKey"
              type="textarea"
              :rows="8"
              :placeholder="editingCredential ? 'Leave empty to keep existing private key' : ''"
            />
          </el-form-item>
          <el-form-item label="Passphrase">
            <el-input v-model="credentialForm.passphrase" type="password" show-password />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="credentialDialog = false">Cancel</el-button>
        <el-button type="primary" @click="saveCredential">{{ editingCredential ? 'Save' : 'Create' }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page-alert,
.table-panel,
.task-section {
  margin-top: 20px;
}

.wide {
  width: 100%;
}

.entity-tabs {
  padding: 0 20px 20px;
}
</style>
