<template>
  <div class="auth-page">
    <!-- 装饰背景 -->
    <div class="auth-bg">
      <div class="bg-blob blob-1"></div>
      <div class="bg-blob blob-2"></div>
    </div>

    <!-- 注册卡片 -->
    <div class="auth-card">
      <!-- 品牌 -->
      <div class="brand">
        <div class="logo">
          <Icon name="brand" :size="22" />
        </div>
        <h1>{{ $t('app.title') }}</h1>
        <p class="sub">{{ $t('register.subtitle') }}</p>
      </div>

      <!-- 表单 -->
      <form class="auth-form" @submit.prevent="onSubmit">
        <div class="field">
          <label>{{ $t('register.username') }}</label>
          <input
            v-model.trim="username"
            type="text"
            autocomplete="username"
            :placeholder="$t('register.username')"
            :disabled="loading"
          />
        </div>
        <div class="field">
          <label>{{ $t('register.password') }}</label>
          <input
            v-model.trim="password"
            type="password"
            autocomplete="new-password"
            :placeholder="$t('register.password')"
            :disabled="loading"
          />
        </div>
        <div class="field">
          <label>{{ $t('register.email') }}</label>
          <input
            v-model.trim="email"
            type="email"
            autocomplete="email"
            :placeholder="$t('register.email')"
            :disabled="loading"
          />
        </div>

        <div v-if="error" class="err-msg">
          <Icon name="warning" :size="14" />
          <span>{{ error }}</span>
        </div>
        <div v-if="success" class="ok-msg">
          <Icon name="success" :size="14" />
          <span>{{ success }}</span>
        </div>

        <button type="submit" class="primary submit-btn" :disabled="loading">
          <Icon v-if="loading" name="refresh" :size="16" class="spin" />
          <Icon v-else name="register" :size="16" />
          <span>{{ loading ? $t('register.loading') : $t('register.submit') }}</span>
        </button>
      </form>

      <!-- 切换到登录 -->
      <div class="auth-switch">
        <router-link to="/login">{{ $t('register.to_login') }}</router-link>
      </div>

      <!-- 顶栏小工具：主题 + 语言 -->
      <div class="auth-tools">
        <button class="tool-btn" @click="themeStore.toggle()" :title="themeStore.isDark ? $t('topbar.theme_light') : $t('topbar.theme_dark')">
          <Icon :name="themeStore.isDark ? 'theme-light' : 'theme-dark'" :size="16" />
        </button>
        <button class="tool-btn" @click="toggleLang" :title="$t('topbar.lang')">
          <Icon name="lang" :size="16" />
          <span>{{ currentLang === 'zh' ? '中' : 'EN' }}</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
// 注册页 — 全屏居中卡片，调用后端 /auth/register
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { currentLang, setLang, t } from '@/i18n'
import Icon from '@/components/Icon.vue'

const router = useRouter()
const authStore = useAuthStore()
const themeStore = useThemeStore()

const username = ref('')
const password = ref('')
const email = ref('')
const error = ref('')
const success = ref('')
const loading = ref(false)

async function onSubmit() {
  error.value = ''
  success.value = ''
  if (!username.value) { error.value = t('register.need_username'); return }
  if (!password.value) { error.value = t('register.need_password'); return }
  loading.value = true
  try {
    await authStore.register(username.value, password.value, email.value)
    success.value = t('register.success')
    // 短暂展示后跳转
    setTimeout(() => router.push('/devices'), 600)
  } catch (e) {
    error.value = e.j?.error || t('register.username_taken')
  } finally {
    loading.value = false
  }
}

function toggleLang() {
  setLang(currentLang.value === 'zh' ? 'en' : 'zh')
}
</script>

<style scoped>
.auth-page {
  position: fixed; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: var(--bg);
  overflow: hidden;
}

.auth-bg { position: absolute; inset: 0; z-index: 0; pointer-events: none; }
.bg-blob {
  position: absolute; border-radius: 50%;
  filter: blur(80px); opacity: .35;
}
.blob-1 {
  width: 420px; height: 420px;
  background: var(--sky);
  top: -120px; right: -120px;
}
.blob-2 {
  width: 380px; height: 380px;
  background: var(--teal);
  bottom: -100px; left: -100px;
}

.auth-card {
  position: relative; z-index: 1;
  width: 380px; max-width: 92vw;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 32px 28px 24px;
}

.brand { text-align: center; margin-bottom: 24px; }
.brand .logo {
  width: 52px; height: 52px; border-radius: 14px;
  background: linear-gradient(135deg, var(--sky), var(--indigo));
  display: inline-flex; align-items: center; justify-content: center;
  color: #fff;
  box-shadow: 0 6px 20px rgba(99,102,241,.4);
  margin-bottom: 12px;
}
.brand h1 { font-size: 18px; color: var(--text); }
.brand .sub { font-size: 12.5px; color: var(--text-3); margin: 4px 0 0; }

.auth-form { display: flex; flex-direction: column; gap: 14px; }
.field { display: flex; flex-direction: column; gap: 6px; }
.field label { font-size: 12.5px; color: var(--text-2); margin: 0; }
.field input { width: 100%; }

.err-msg, .ok-msg {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px;
  padding: 7px 10px; border-radius: var(--radius-sm);
}
.err-msg { color: var(--fail); background: var(--fail-soft); border: 1px solid var(--fail-bg); }
.ok-msg { color: var(--ok); background: var(--ok-soft); border: 1px solid var(--ok-bg); }

.submit-btn {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  width: 100%; height: 40px; font-size: 14px; font-weight: 600;
  margin-top: 4px;
}
.submit-btn .spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

.auth-switch {
  text-align: center; margin-top: 18px;
  font-size: 12.5px; color: var(--text-3);
}
.auth-switch a { color: var(--accent); }

.auth-tools {
  display: flex; justify-content: center; gap: 8px;
  margin-top: 16px; padding-top: 16px;
  border-top: 1px dashed var(--border);
}
.tool-btn {
  display: inline-flex; align-items: center; gap: 5px;
  height: 30px; padding: 0 10px;
  background: var(--surface-3); border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  color: var(--text-2); font-size: 12px;
  cursor: pointer;
}
.tool-btn:hover { background: var(--bg-soft); color: var(--text); }
</style>