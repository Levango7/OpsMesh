<template>
  <div>
    <h2>{{ $t('deploys.title') }}</h2>
    <p class="muted">{{ $t('deploys.subtitle') }}</p>

    <div class="row">
      <!-- 左：登记 -->
      <div class="col">
        <div class="card">
          <h3>{{ $t('deploys.register_title') }}</h3>
          <form @submit.prevent="onCreate">
            <div class="field">
              <label>{{ $t('deploys.name_label') }}</label>
              <input v-model="form.name" required />
            </div>
            <div class="row">
              <div class="field">
                <label>{{ $t('deploys.type_label') }}</label>
                <select v-model="form.type">
                  <option value="script">script</option>
                  <option value="git">git</option>
                </select>
              </div>
              <div class="field">
                <label>{{ $t('deploys.target_ids_label') }}</label>
                <input v-model="form.target_ids" placeholder="dev-10.0.0.1, dev-10.0.0.2" />
              </div>
            </div>
            <div class="field">
              <label>{{ $t('deploys.repo_url_label') }}</label>
              <input v-model="form.repo_url" placeholder="https://git.example.com/ops/nginx-deploy.git" />
            </div>
            <div class="row">
              <div class="field">
                <label>{{ $t('deploys.path_label') }}</label>
                <input v-model="form.path" />
              </div>
              <div class="field">
                <label>{{ $t('deploys.content_label') }}</label>
                <input v-model="form.content" />
              </div>
            </div>
            <div class="btnbar">
              <button type="submit" class="primary">{{ $t('deploys.register_btn') }}</button>
              <button type="button" @click="loadDemo">{{ $t('deploys.demo_btn') }}</button>
            </div>
            <p v-if="store.msg" :class="['msg', store.error ? 'err' : 'ok']">{{ store.msg }}</p>
          </form>
        </div>
      </div>

      <!-- 右：列表 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <div class="field">
              <label>{{ $t('deploys.status_label') }}</label>
              <select v-model="store.statusFilter" @change="store.fetchList()">
                <option value="">{{ $t('common.all') }}</option>
                <option value="created">created</option>
                <option value="running">running</option>
                <option value="success">success</option>
                <option value="failed">failed</option>
                <option value="rolledback">rolledback</option>
              </select>
            </div>
            <button @click="store.fetchList()">↻ {{ $t('common.refresh') }}</button>
          </div>

          <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

          <DataTable :columns="columns" :rows="store.list" row-key="id" :loading="store.loading" :empty-text="$t('deploys.empty')">
            <template #cell-id="{ value }"><code>{{ value }}</code></template>
            <template #cell-target_ids="{ value }"><code>{{ (value || '').replace(/,/g, ', ') }}</code></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="value" :text="value" />
            </template>
            <template #cell-actions="{ row }">
              <button class="xs" @click.stop="onExec(row.id)">▶ {{ $t('deploys.execute') }}</button>
              <button class="xs outline" @click.stop="onRollback(row.id)">↩ {{ $t('deploys.rollback') }}</button>
              <button class="xs outline" @click.stop="onOpen(row.id)">{{ $t('deploys.detail_btn') }}</button>
            </template>
          </DataTable>
        </div>
      </div>
    </div>

    <DetailDrawer :open="!!store.current" :title="drawerTitle" @close="store.current = null">
      <div v-if="store.current">
        <p>{{ $t('deploys.type_label') }}: {{ store.current.type }} ｜ {{ $t('deploys.status_label') }}: <StatusBadge :status="store.current.status" :text="store.current.status" /></p>
        <p>{{ $t('deploys.target_ids_label') }}: <code>{{ (store.current.target_ids || '').replace(/,/g, ', ') }}</code></p>
        <p v-if="store.current.repo_url">{{ $t('deploys.repo_url_label') }}: <code>{{ store.current.repo_url }}</code></p>
        <p v-if="store.current.path">{{ $t('deploys.path_label') }}: <code>{{ store.current.path }}</code></p>
        <p v-if="store.current.content">{{ $t('deploys.content_label') }}: <code>{{ store.current.content }}</code></p>
        <p class="muted">
          {{ $t('deploys.created_by_label') }}: {{ store.current.created_by }} ｜ {{ $t('deploys.created_label') }}: {{ fmtTime(store.current.created_at) }} ｜ {{ $t('deploys.updated_label') }}: {{ fmtTime(store.current.updated_at) }}
        </p>
        <p v-if="store.current.task_ids">{{ $t('deploys.task_ids_label') }}: <code>{{ (store.current.task_ids || '').replace(/,/g, ', ') }}</code></p>
      </div>
    </DetailDrawer>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive } from 'vue'
import { useDeployStore } from '@/stores/deploy'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'
import { fmtTime } from '@/composables/useFormatTime'

const store = useDeployStore()
const form = reactive({
  name: '', type: 'script', repo_url: '', content: '', path: '', target_ids: ''
})

const columns = [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'name', title: t('deploys.name_label') },
  { key: 'type', title: t('deploys.type_label') },
  { key: 'target_ids', title: t('deploys.target_ids_label'), slot: 'cell-target_ids' },
  { key: 'status', title: t('deploys.status_label'), slot: 'cell-status' },
  { key: 'actions', title: t('deploys.actions_label'), slot: 'cell-actions', width: '140px' }
]
const drawerTitle = computed(() =>
  store.current ? t('deploys.drawer_title', { id: store.current.id, name: store.current.name }) : ''
)


function loadDemo() {
  form.name = 'deploy-nginx'
  form.type = 'script'
  form.repo_url = 'https://git.example.com/ops/nginx-deploy.git'
  form.content = ''
  form.path = ''
  form.target_ids = 'dev-10.0.0.1, dev-10.0.0.2'
  store.msg = t('deploys.demo_loaded_msg'); store.error = ''
}
async function onCreate() {
  try {
    const r = await store.create({ ...form })
    store.msg = `[${r.s}] ${JSON.stringify(r.j)}`; store.error = r.s >= 400 ? 'create' : ''
    if (r.s < 400) { form.name = ''; form.target_ids = ''; form.repo_url = ''; form.content = ''; form.path = '' }
  } catch (e) { store.msg = 'error: ' + (e.j?.error || e.message); store.error = 'create' }
}
async function onExec(id) {
  if (!confirm(t('deploys.confirm_execute'))) return
  try { const r = await store.execute(id); store.msg = `[${r.s}] ${r.j.error || t('deploys.executed_msg', { id })}`; store.error = r.s >= 400 ? 'exec' : '' }
  catch (e) { store.msg = 'error: ' + (e.j?.error || e.message); store.error = 'exec' }
}
async function onRollback(id) {
  if (!confirm(t('deploys.confirm_rollback'))) return
  try { const r = await store.rollback(id); store.msg = `[${r.s}] ${r.j.error || t('deploys.rolled_back_msg', { id })}`; store.error = r.s >= 400 ? 'rb' : '' }
  catch (e) { store.msg = 'error: ' + (e.j?.error || e.message); store.error = 'rb' }
}
async function onOpen(id) { await store.open(id) }

onMounted(() => { store.fetchList() })
</script>

<style scoped>
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
.field label { margin: 0; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0; border-radius: var(--radius-sm);
  background: var(--fail-soft); border: 1px solid var(--fail-bg);
  color: var(--fail); font-size: 12.5px; font-weight: 500;
}
</style>
