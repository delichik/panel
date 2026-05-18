<script setup lang="ts">
import type { ServerDto } from '@/types/api';

defineProps<{
  modelValue: string;
  servers: ServerDto[];
  loading?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();
</script>

<template>
  <section class="panel server-selector" v-loading="loading">
    <div class="panel-header">
      <strong>Servers</strong>
      <el-tag>{{ servers.length }}</el-tag>
    </div>
    <div v-if="servers.length" class="server-cards">
      <button
        v-for="server in servers"
        :key="server.id"
        class="server-card"
        :class="{ active: server.id === modelValue }"
        @click="emit('update:modelValue', server.id)"
      >
        <div class="server-name">{{ server.name }}</div>
        <div class="muted">{{ server.host }}:{{ server.port }}</div>
        <div class="server-flags">
          <el-tag :type="server.reachable ? 'success' : 'danger'" size="small">
            {{ server.reachable ? 'reachable' : 'offline' }}
          </el-tag>
          <el-tag :type="server.os?.supported ? 'success' : 'warning'" size="small">
            {{ server.os?.prettyName || 'unknown' }}
          </el-tag>
          <el-tag :type="server.sudo?.passwordless ? 'success' : 'warning'" size="small">
            {{ server.sudo?.passwordless ? 'sudo ready' : 'sudo unchecked' }}
          </el-tag>
        </div>
      </button>
    </div>
    <el-empty v-else description="No servers registered" />
  </section>
</template>

<style scoped>
.server-selector {
  min-width: 280px;
}

.server-cards {
  display: grid;
  gap: 10px;
  padding: 14px;
}

.server-card {
  width: 100%;
  padding: 14px;
  text-align: left;
  border: 1px solid #dfe4ea;
  border-radius: 8px;
  background: #fff;
  cursor: pointer;
}

.server-card.active {
  border-color: #409eff;
  box-shadow: inset 3px 0 0 #409eff;
}

.server-name {
  font-weight: 700;
}

.server-flags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 12px;
}
</style>
