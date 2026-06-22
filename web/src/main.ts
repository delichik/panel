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
import { syncDocumentLanguage } from './i18n';
import { getInitialTheme, getInitialThemeColors, syncThemeAttribute } from './theme';

if (import.meta.env.VITE_PANEL_TEST_MODE === 'true' && !globalThis.localStorage?.getItem('authToken')) {
  globalThis.localStorage?.setItem('authToken', 'mock-session-token');
}
const initialTheme = getInitialTheme();
const initialThemeColors = getInitialThemeColors();
syncThemeAttribute(initialTheme);
syncDocumentLanguage();

const vuetify = createVuetify({
  components,
  directives,
  theme: {
    defaultTheme: initialTheme,
    themes: {
      light: {
        dark: false,
        colors: {
          background: '#f7f8f5',
          surface: '#ffffff',
          'surface-variant': initialThemeColors.light.surfaceVariant,
          'surface-container': '#f9faf7',
          'surface-muted': '#eef1ec',
          primary: initialThemeColors.light.primary,
          'on-primary': initialThemeColors.light.onPrimary,
          secondary: '#6b7280',
          success: '#16a34a',
          warning: '#b45309',
          error: '#dc2626',
          info: '#2563eb',
          'on-background': '#181d1b',
          'on-surface': '#1f2724',
          'on-surface-variant': '#5f6f68',
        },
        variables: {
          'border-color': '#d9e2dc',
          'border-opacity': 1,
        },
      },
      dark: {
        dark: true,
        colors: {
          background: '#0d100e',
          surface: '#151a17',
          'surface-variant': initialThemeColors.dark.surfaceVariant,
          'surface-container': '#171d1a',
          'surface-muted': '#222a25',
          primary: initialThemeColors.dark.primary,
          'on-primary': initialThemeColors.dark.onPrimary,
          secondary: '#b5beb7',
          success: '#4ade80',
          warning: '#f59e0b',
          error: '#fb7185',
          info: '#60a5fa',
          'on-background': '#f3f6f1',
          'on-surface': '#e7ece6',
          'on-surface-variant': '#b3beb6',
        },
        variables: {
          'border-color': '#334139',
          'border-opacity': 1,
        },
      },
    },
  },
});

createApp(App).use(pinia).use(router).use(vuetify).mount('#app');
