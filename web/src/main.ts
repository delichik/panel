import { createApp } from 'vue';
import ElementPlus from 'element-plus';
import 'element-plus/dist/index.css';
import './styles/main.css';
import App from './app/App.vue';
import { router } from './router';
import { pinia } from './stores';

createApp(App).use(pinia).use(router).use(ElementPlus).mount('#app');
