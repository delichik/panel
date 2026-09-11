import { fileURLToPath, URL } from 'node:url';
import vue from '@vitejs/plugin-vue';
import { defineConfig } from 'vite';
import tailwindcss from '@tailwindcss/vite';

const backendProxyTarget = process.env.PANEL_WEB_PROXY_TARGET ?? 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: backendProxyTarget,
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        // Stable vendor chunks: route-level code splitting keeps every page
        // family in its own async chunk, and these manual chunks stop the
        // heavy third-party libs from being merged back into the entry chunk.
        // Keep each emitted chunk below the 500 kB warning threshold.
        manualChunks: {
          'vue-vendor': ['vue', 'vue-router', 'pinia'],
          'code-editor': [
            '@codemirror/state',
            '@codemirror/view',
            '@codemirror/commands',
            '@codemirror/search',
            '@codemirror/language',
            '@codemirror/lang-json',
            '@codemirror/lang-yaml',
            '@codemirror/legacy-modes/mode/dockerfile',
            '@codemirror/legacy-modes/mode/nginx',
            '@codemirror/legacy-modes/mode/properties',
            '@codemirror/legacy-modes/mode/shell',
            '@lezer/highlight',
          ],
          echarts: ['echarts', 'vue-echarts'],
          yaml: ['yaml'],
        },
      },
    },
  },
});
