// Vue 应用入口 — 挂载 Pinia + Router + i18n
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { t, initLang } from './i18n'
import { useThemeStore } from './stores/theme'
import { useAuthStore } from './stores/auth'
import './assets/tokens.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)

// i18n：注入全局 $t，并初始化 DOM lang 属性
app.config.globalProperties.$t = t
initLang()

// 主题：在挂载前初始化，避免首屏闪烁
useThemeStore().init()

// 认证：在挂载前启动会话恢复（fetchMe），路由守卫会 await auth.ready
// 等待其完成后再判断 isLoggedIn，避免冷启动已登录用户被误重定向到 /login。
useAuthStore().fetchMe()

app.mount('#app')
