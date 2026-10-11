import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './Hub.vue'

createApp(App).use(createPinia()).mount('#app')
