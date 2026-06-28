import { onMounted, ref, watch } from 'vue';
import { serversApi } from '@/api/servers';
import type { ServerDto } from '@/types/api';

export function useDockerServers(load: () => Promise<void>) {
  const servers = ref<ServerDto[]>([]);
  const serverId = ref('');
  const loadingServers = ref(false);

  watch(serverId, () => void load());
  onMounted(async () => {
    loadingServers.value = true;
    try {
      servers.value = await serversApi.listServers();
      if (servers.value.length) serverId.value = servers.value[0].id;
    } finally {
      loadingServers.value = false;
    }
  });

  return { servers, serverId, loadingServers };
}
