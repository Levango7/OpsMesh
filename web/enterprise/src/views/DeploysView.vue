<template>
  <div>
    <h2>部署中心</h2>
    <p class="muted">登记部署计划，触发执行或回滚。部署会派发为任务到目标设备。</p>

    <div class="row">
      <!-- 左：登记 -->
      <div class="col">
        <div class="card">
          <h3>登记部署</h3>
          <form @submit.prevent="onCreate">
            <div class="field">
              <label>名称</label>
              <input v-model="form.name" required />
            </div>
            <div class="row">
              <div class="field">
                <label>类型</label>
                <select v-model="form.type">
                  <option value="script">script</option>
                  <option value="git">git</option>
                </select>
              </div>
              <div class="field">
                <label>目标设备（逗号分隔）</label>
                <input v-model="form.target_ids" placeholder="dev-10.0.0.1, dev-10.0.0.2" />
              </div>
            </div>
            <div class="field">
              <label>仓库 URL</label>
              <input v-model="form.repo_url" placeholder="https://git.example.com/ops/nginx-deploy.git" />
            </div>
            <div class="row">
              <div class="field">
                <label>路径</label>
                <input v-model="form.path" />
              </div>
              <div class="field">
                <label>内容（script 用）</label>
                <input v-model="form.content" />
              </div>
            </div>
            <div class="btnbar">
              <button type="submit" class="primary">登记部署</button>
              <button type="button" @click="loadDemo">📋 示例</button>
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
              <label>状态</label>
              <select v-model="store.statusFilter" @change="store.fetchList()">
                <option value="">全部</option>
                <option value="created">created</option>
                <option value="running">running</option>
                <option value="success">success</option>
                <option value="failed">failed</option>
                <option value="rolledback">rolledback</option>
              </select>
            </div>
            <button @click="store.fetchList()">↻ 刷新</button>
          </div>

          <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

          <DataTable :columns="columns" :rows="store.list" row-key="id" empty-text="暂无部署任务">
            <template #cell-id="{ value }"><code>{{ value }}</code></template>
            <template #cell-target_ids="{ value }"><code>{{ (value || '').replace(/,/g, ', ') }}</code></template>
            <template #cell-status="{ value }">
              <StatusBadge :status="value" :text="value" />
            </template>
            <template #cell-actions="{ row }">
              <button class="xs" @click.stop="onExec(row.id)">▶</button>
              <button class="xs outline" @click.stop="onRollback(row.id)">↩</button>
              <button class="xs outline" @click.stop="onOpen(row.id)">详情</button>
            </template>
          </DataTable>
        </div>
      </div>
    </div>

    <DetailDrawer :open="!!store.current" :title="drawerTitle" @close="store.current = null">
      <div v-if="store.current">
        <p>类型: {{ store.current.type }} ｜ 状态: <StatusBadge :status="store.current.status" :text="store.current.status" /></p>
        <p>目标设备: <code>{{ (store.current.target_ids || '').replace(/,/g, ', ') }}</code></p>
        <p v-if="store.current.repo_url">仓库: <code>{{ store.current.repo_url }}</code></p>
        <p v-if="store.current.path">路径: <code>{{ store.current.path }}</code></p>
        <p v-if="store.current.content">内容: <code>{{ store.current.content }}</code></p>
        <p class="muted">
          创建人: {{ store.current.created_by }} ｜ 创建: {{ fmtTime(store.current.created_at) }} ｜ 更新: {{ fmtTime(store.current.updated_at) }}
        </p>
        <p v-if="store.current.task_ids">派发任务: <code>{{ (store.current.task_ids || '').replace(/,/g, ', ') }}</code></p>
      </div>
    </DetailDrawer>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive } from 'vue'
import { useDeployStore } from '@/stores/deploy'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import Icon from '@/components/Icon.vue'

const store = useDeployStore()
const form = reactive({
  name: '', type: 'script', repo_url: '', content: '', path: '', target_ids: ''
})

const columns = [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'name', title: '名称' },
  { key: 'type', title: '类型' },
  { key: 'target_ids', title: '目标设备', slot: 'cell-target_ids' },
  { key: 'status', title: '状态', slot: 'cell-status' },
  { key: 'actions', title: '操作', slot: 'cell-actions', width: '140px' }
]
const drawerTitle = computed(() =>
  store.current ? `部署 #${store.current.id} · ${store.current.name}` : ''
)

function fmtTime(s) {
  if (!s) return ''
  const d = new Date(s)
  return isNaN(d.getTime()) ? s : d.toLocaleString('zh-CN', { hour12: false })
}
function loadDemo() {
  form.name = 'deploy-nginx'
  form.type = 'script'
  form.repo_url = 'https://git.example.com/ops/nginx-deploy.git'
  form.content = ''
  form.path = ''
  form.target_ids = 'dev-10.0.0.1, dev-10.0.0.2'
  store.msg = '已载入示例，可改后点「登记部署」'; store.error = ''
}
async function onCreate() {
  try {
    const r = await store.create({ ...form })
    store.msg = `[${r.s}] ${JSON.stringify(r.j)}`; store.error = r.s >= 400 ? 'create' : ''
    if (r.s < 400) { form.name = ''; form.target_ids = ''; form.repo_url = ''; form.content = ''; form.path = '' }
  } catch (e) { store.msg = 'error: ' + (e.j?.error || e.message); store.error = 'create' }
}
async function onExec(id) {
  try { const r = await store.execute(id); store.msg = `[${r.s}] ${r.j.error || '已触发执行 #' + id}`; store.error = r.s >= 400 ? 'exec' : '' }
  catch (e) { store.msg = 'error: ' + (e.j?.error || e.message); store.error = 'exec' }
}
async function onRollback(id) {
  try { const r = await store.rollback(id); store.msg = `[${r.s}] ${r.j.error || '已回滚 #' + id}`; store.error = r.s >= 400 ? 'rb' : '' }
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