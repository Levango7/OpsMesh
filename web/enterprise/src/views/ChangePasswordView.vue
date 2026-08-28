<template>
  <div class="auth-page">
    <div class="auth-bg">
      <div class="bg-blob blob-1"></div>
      <div class="bg-blob blob-2"></div>
    </div>

    <div class="auth-card">
      <div class="brand">
        <div class="logo">
          <Icon name="key" :size="22" />
        </div>
        <h1>{{ $t('change_password.title') }}</h1>
        <p class="sub">{{ $t('change_password.subtitle') }}</p>
      </div>

      <form class="auth-form" data-testid="change-password-form" @submit.prevent="onSubmit">
        <div class="field">
          <label>{{ $t('change_password.old_password') }}</label>
          <input
            v-model.trim="oldPassword"
            type="password"
            autocomplete="current-password"
            :placeholder="$t('change_password.old_password')"
            :disabled="loading || success"
            data-testid="cp-old-password"
          />
        </div>

        <div class="field">
          <label>{{ $t('change_password.new_password') }}</label>
          <input
            v-model.trim="newPassword"
            type="password"
            autocomplete="new-password"
            :placeholder="$t('change_password.new_password')"
            :disabled="loading || success"
            data-testid="cp-new-password"
          />
          <PasswordStrength :password="newPassword" />
        </div>

        <div class="field">
          <label>{{ $t('change_password.confirm_password') }}</label>
          <input
            v-model.trim="confirmPassword"
            type="password"
            autocomplete="new-password"
            :placeholder="$t('change_password.confirm_password')"
            :disabled="loading || success"
            data-testid="cp-confirm-password"
          />
        </div>

        <div v-if="error" class="err-msg" data-testid="cp-error">
          <Icon name="warning" :size="14" />
          <span>{{ error }}</span>
        </div>

        <div v-if="success" class="success-msg" data-testid="cp-success">
          <Icon name="check" :size="14" />
          <span>{{ $t('change_password.success') }}</span>
        </div>

        <button type="submit" class="primary submit-btn" :disabled="loading || success" data-testid="cp-submit">
          <Icon v-if="loading" name="refresh" :size="16" class="spin" />
          <Icon v-else name="key" :size="16" />
          <span>{{ loading ? $t('change_password.submitting') : $t('change_password.submit') }}</span>
        </button>
      </form>

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
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useThemeStore } from '@/stores/theme'
import { currentLang, setLang, t } from '@/i18n'
import Icon from '@/components/Icon.vue'
import PasswordStrength from '@/components/PasswordStrength.vue'

const router = useRouter()
const authStore = useAuthStore()
const themeStore = useThemeStore()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const error = ref('')
const loading = ref(false)
const success = ref(false)

const validationError = computed(() => {
  if (!oldPassword.value) return t('change_password.need_old_password')
  if (!newPassword.value) return t('change_password.need_new_password')
  if (newPassword.value.length < 8) return t('change_password.min_length')
  if (!/[a-z]/.test(newPassword.value)) return t('change_password.need_lowercase')
  if (!/[A-Z]/.test(newPassword.value)) return t('change_password.need_uppercase')
  if (!/[0-9]/.test(newPassword.value)) return t('change_password.need_number')
  if (newPassword.value !== confirmPassword.value) return t('change_password.mismatch')
  return ''
})

async function onSubmit() {
  error.value = ''
  const err = validationError.value
  if (err) { error.value = err; return }

  loading.value = true
  try {
    await authStore.changePassword(oldPassword.value, newPassword.value)
    success.value = true
    setTimeout(() => {
      router.push('/login')
    }, 1500)
  } catch (e) {
    error.value = e.j?.error || t('change_password.failed')
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
  background: var(--indigo);
  top: -120px; left: -120px;
}
.blob-2 {
  width: 380px; height: 380px;
  background: var(--sky);
  bottom: -100px; right: -100px;
}

.auth-card {
  position: relative; z-index: 1;
  width: 420px; max-width: 92vw;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
  padding: 32px 28px 24px;
}

.brand { text-align: center; margin-bottom: 24px; }
.brand .logo {
  width: 52px; height: 52px; border-radius: 14px;
  background: linear-gradient(135deg, var(--indigo), var(--sky));
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

.err-msg {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px; color: var(--fail);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  padding: 7px 10px; border-radius: var(--radius-sm);
}

.success-msg {
  display: flex; align-items: center; gap: 6px;
  font-size: 12.5px; color: var(--success, #10b981);
  background: var(--success-soft, rgba(16,185,129,.1));
  border: 1px solid var(--success-bg, rgba(16,185,129,.25));
  padding: 7px 10px; border-radius: var(--radius-sm);
}

.submit-btn {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  width: 100%; height: 40px; font-size: 14px; font-weight: 600;
  margin-top: 4px;
}
.submit-btn .spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }

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
