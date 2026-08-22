import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'

import '@unocss/reset/tailwind.css'
import 'element-plus/dist/index.css'
import 'uno.css'

// 样式顺序：令牌 → 基础 → Element Plus 微调（后加载可覆盖组件默认值）
import '@/styles/tokens.css'
import '@/styles/base.css'
import '@/styles/element.css'

import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)
app.use(ElementPlus)

// 将 auth store 的 token 注册为 http 层读取源（单向依赖，避免循环 import）
useAuthStore(pinia).bindHttpToken()

app.mount('#app')
