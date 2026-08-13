<template>
  <div>
    <h2>{{ $t('secrets.title') }}</h2>
    <p class="muted">{{ $t('secrets.subtitle') }}</p>

    <div v-if="loadError" class="poll-err">
      <Icon name="warning" :size="14" /> {{ loadError }}
    </div>

    <div class="row">
      <!-- 左：提供者状态 + Vault 配置 -->
      <div class="col">
        <!-- 提供者状态卡片 -->
        <div class="card">
          <h3>{{ $t('secrets.provider_status') }}</h3>
          <div v-if="loading" class="muted">{{ $t('secrets.loading') }}</div>
          <div v-else>
            <div class="status-row">
              <span class="field-hint">{{ $t('secrets.provider_type') }}</span>
              <span class="badge info">{{ status.provider || '—' }}</span>
              <StatusBadge
                :status="status.enabled ? 'success' : 'warn'"
                :text="status.enabled ? $t('secrets.enabled') : $t('secrets.disabled')"
              />
            </div>
            <div class="status-row">
              <span class="field-hint">{{ $t('secrets.vault_addr') }}</span>
              <code>{{ status.addr || '—' }}</code>
            </div>
            <div class="status-row">
              <span class="field-hint">{{ $t('secrets.vault_mount') }}</span>
              <code>{{ status.mount || '—' }}</code>
            </div>
            <div class="status-row">
              <span class="field-hint">{{ $t('secrets.secret_file') }}</span>
              <code>{{ status.file || '—' }}</code>
            </div>
          </div>
        </div>

        <!-- Vault 配置表单 + 测试连接 -->
        <div class="card">
          <h3>{{ $t('secrets.vault_config') }}</h3>
          <form @submit.prevent="onTest">
            <div class="field">
              <label>{{ $t('secrets.vault_addr') }}</label>
              <input
                v-model="form.addr"
                placeholder="https://vault:8200"
              />
            </div>
            <div class="field">
              <label>{{ $t('secrets.vault_token') }}</label>
              <input
                v-model="form.token"
                type="password"
                :placeholder="$t('secrets.vault_token_hint')"
              />
            </div>
            <div class="field">
              <label>{{ $t('secrets.vault_mount') }}</label>
              <input v-model="form.mount" placeholder="secret" />
            </div>
            <div class="btnbar">
              <button type="submit" class="primary" :disabled="testing">
                {{ testing ? $t('secrets.testing') : $t('secrets.test_connection') }}
              </button>
            </div>
            <p v-if="testMsg" :class="['msg', testOk ? 'ok' : 'err']">{{ testMsg }}</p>
          </form>
        </div>
      </div>

      <!-- 右：密钥列表 + 帮助说明 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <h3 style="margin: 0;">{{ $t('secrets.keys_title') }}</h3>
            <button @click="fetchKeys()">↻ {{ $t('common.refresh') }}</button>
          </div>
          <DataTable
            :columns="keyCols"
            :rows="keys"
            row-key="key"
            :empty-text="$t('secrets.no_keys')"
          >
            <template #cell-key="{ value }"><code>{{ value }}</code></template>
            <template #cell-provider="{ value }">
              <span class="badge info">{{ value || '—' }}</span>
            </template>
          </DataTable>
        </div>

        <!-- 帮助说明 -->
        <div class="card">
          <h3>{{ $t('secrets.help_title') }}</h3>
          <p class="muted">{{ $t('secrets.help_ref_format') }}</p>
          <pre class="code-block">{{ $t('secrets.help_ref_example') }}</pre>
          <h4>{{ $t('secrets.help_providers') }}</h4>
          <ul class="help-list">
            <li><code>env</code> — {{ $t('secrets.help_env') }}</li>
            <li><code>file</code> — {{ $t('secrets.help_file') }}</li>
            <li><code>vault</code> — {{ $t('secrets.help_vault') }}</li>
            <li><code>chain:env,file</code> — {{ $t('secrets.help_chain') }}</li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// 密钥管理页面（task 267）：
//   - 提供者状态卡片：显示当前 provider 类型、连接状态、Vault 地址、Mount 路径、密钥文件路径
//   - Vault 配置表单：地址 / Token（密码框）/ Mount，"测试连接"按钮
//   - 密钥列表：表格显示 key 名称与来源 provider（不显示值，安全考虑）
//   - 帮助说明：解释密钥引用格式 ${key} 和支持的 provider 类型
//
// 安全约束：本页面不展示任何密钥值，仅展示 key 名称与来源 provider。
import { onMounted, reactive, ref } from 'vue'
import { getSecretProviderStatus, testSecretProvider, listSecretKeys } from '@/api/secrets'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'

// 提供者状态
const status = reactive({ provider: '', enabled: false, addr: '', mount: '', file: '' })
const loading = ref(false)
const loadError = ref('')

// Vault 测试表单
const form = reactive({ addr: '', token: '', mount: 'secret' })
const testing = ref(false)
const testMsg = ref('')
const testOk = ref(false)

// 密钥列表
const keys = ref([])
const keyCols = [
  { key: 'key', title: 'Key', slot: 'cell-key' },
  { key: 'provider', title: 'Provider', slot: 'cell-provider' }
]

// 拉取提供者状态
async function fetchStatus() {
  loading.value = true
  loadError.value = ''
  try {
    const data = await getSecretProviderStatus()
    Object.assign(status, {
      provider: data?.provider || '',
      enabled: !!data?.enabled,
      addr: data?.addr || '',
      mount: data?.mount || '',
      file: data?.file || ''
    })
    // 用当前 addr/mount 预填表单（token 不回填，安全考虑）
    if (!form.addr && status.addr) form.addr = status.addr
    if (!form.mount || form.mount === 'secret') form.mount = status.mount || 'secret'
  } catch (e) {
    loadError.value = t('secrets.fetch_status_failed') + (e.j?.error || e.s || e.message || '')
  } finally {
    loading.value = false
  }
}

// 拉取密钥 key 列表
async function fetchKeys() {
  try {
    const data = await listSecretKeys()
    keys.value = Array.isArray(data) ? data : []
  } catch (e) {
    keys.value = []
  }
}

// 测试 Vault 连接
async function onTest() {
  if (!form.addr) {
    testMsg.value = t('secrets.need_addr')
    testOk.value = false
    return
  }
  testing.value = true
  testMsg.value = t('secrets.testing')
  testOk.value = true
  try {
    const r = await testSecretProvider({
      addr: form.addr,
      token: form.token,
      mount: form.mount
    })
    if (r.s < 400 && r.j) {
      if (r.j.ok) {
        testMsg.value = t('secrets.test_success', { ms: r.j.latencyMs ?? 0 })
        testOk.value = true
      } else {
        testMsg.value = t('secrets.test_fail') + (r.j.error || '')
        testOk.value = false
      }
    } else {
      testMsg.value = t('secrets.test_fail_http', { code: r.s || '?', msg: r.j ? JSON.stringify(r.j) : '' })
      testOk.value = false
    }
  } catch (e) {
    testMsg.value = t('secrets.test_fail_error', { msg: e.j?.error || e.message || e })
    testOk.value = false
  } finally {
    testing.value = false
  }
}

onMounted(() => {
  fetchStatus()
  fetchKeys()
})
</script>

<style scoped>
.row { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
@media (max-width: 960px) { .row { grid-template-columns: 1fr; } }
.col { display: flex; flex-direction: column; gap: 14px; }

.status-row {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 0; font-size: 13px;
  border-bottom: 1px dashed var(--border);
}
.status-row:last-child { border-bottom: none; }
.field-hint { font-size: 11.5px; color: var(--text-3); min-width: 84px; }

.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.field label { margin: 0; }

.flowbar { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.btnbar { display: flex; gap: 8px; margin-top: 4px; }

.badge {
  display: inline-flex; align-items: center; height: 20px;
  padding: 0 9px; border-radius: 999px;
  font-size: 11.5px; font-weight: 600;
}
.badge.info { background: var(--info-bg); color: var(--info); }

.code-block {
  background: var(--surface-3); color: var(--text); padding: 10px;
  border-radius: var(--radius-sm); overflow: auto;
  font-size: 12px; line-height: 1.5; white-space: pre-wrap; word-break: break-all;
  font-family: var(--font-mono);
}

.help-list { margin: 6px 0 0; padding-left: 18px; font-size: 12.5px; line-height: 1.7; }
.help-list code { font-size: 12px; }

.poll-err {
  padding: 8px 12px; margin: 6px 0 12px; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}

.msg { margin-top: 8px; font-size: 12.5px; }
.msg.ok { color: var(--teal); }
.msg.err { color: var(--fail); }
</style>