import { createApp } from 'vue';
import 'vuetify/styles';
import { createVuetify } from 'vuetify';
import * as components from 'vuetify/components';
import * as directives from 'vuetify/directives';
import '@mdi/font/css/materialdesignicons.css';
import './styles/main.css';
import App from './app/App.vue';
import { router } from './router';
import { pinia } from './stores';

const vuetify = createVuetify({
  components,
  directives,
  theme: {
    defaultTheme: 'light',
    themes: {
      light: {
        dark: false,
        colors: {
          background: '#f8fafc',
          surface: '#ffffff',
          'surface-variant': '#f1f5f9',
          primary: '#4f46e5', // Vibrant Indigo
          secondary: '#64748b',
          success: '#10b981',
          warning: '#f59e0b',
          error: '#ef4444',
          info: '#3b82f6',
        },
      },
      dark: {
        dark: true,
        colors: {
          background: '#090d16', // Modern Slate Black
          surface: '#111827',    // Sleek Navy Slate
          'surface-variant': '#1f2937',
          primary: '#6366f1',    // Indigo Glow
          secondary: '#94a3b8',
          success: '#10b981',
          warning: '#f59e0b',
          error: '#ef4444',
          info: '#3b82f6',
        },
      },
    },
  },
});

createApp(App).use(pinia).use(router).use(vuetify).mount('#app');
