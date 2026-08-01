<template>
  <div>
    <h2>配置项 CMDB</h2>
    <p class="muted">按类型组织配置项，支持实例 CRUD 与关系图查看。</p>

    <div class="row">
      <!-- 左：实例列表 -->
      <div class="col">
        <div class="card">
          <div class="flowbar">
            <div class="field">
              <label>类型</label>
              <select v-model="store.currentType" @change="store.fetchInstances()">
                <option value="">（先选一个类型）</option>
                <option v-for="t in store.types" :key="t.name" :value="t.name">
                  {{ t.displayName }} ({{ t.name }})
                </option>
              </select>
            </div>
            <button @click="store.fetchInstances()">↻ 刷新</button>
          </div>
          <DataTable
            :columns="ciCols"
            :rows="store.instances"
            row-key="id"
            :clickable="true"
            empty-text="请先选择一个类型"
            @row-click="onOpenCI"
          >
            <template #cell-id="{ value }"><code>{{ value }}</code></template>
          </DataTable>
        </div>
      </div>

      <!-- 右：新建 + 详情 -->
      <div class="col">
        <div class="card">
          <h3>新建配置项</h3>
          <form @submit.prevent="onCreate">
            <div class="field">
              <label>类型</label>
              <select v-model="form.ciType" required>
                <option value="">（先选一个类型）</option>
                <option v-for="t in store.types" :key="t.name" :value="t.name">{{ t.name }}</option>
              </select>
            </div>
            <div class="field">
              <label>名称</label>
              <input v-model="form.name" required />
            </div>
            <div class="field">
              <label>属性（JSON）</label>
              <textarea v-model="form.attrsRaw" rows="3" placeholder='{"env":"prod"}' style="width:100%" />
            </div>
            <button type="submit" class="primary">创建</button>
            <p v-if="store.msg" :class="['msg', store.error ? 'err' : 'ok']">{{ store.msg }}</p>
          </form>
        </div>

        <div class="card" v-if="store.graph">
          <h3>关系图</h3>
          <div v-if="store.graph.error" class="msg err">{{ store.graph.error }}</div>
          <div v-else>
            <h4>{{ center.name }} <small class="muted">({{ center.ciType }} / {{ center.id }})</small></h4>
            <p>状态: {{ center.status }} ｜ 来源: {{ center.source }} ｜ 版本: {{ center.version }}</p>
            <p v-if="hasAttrs">
              属性:
              <template v-for="(v, k) in center.attrs" :key="k">
                <code>{{ k }}</code>={{ v }}&nbsp;
              </template>
            </p>
            <h4>关系拓扑（{{ (store.graph.relations || []).length }}）</h4>
            <div v-if="!(store.graph.relations || []).length" class="muted">无关系</div>
            <div v-for="(r, i) in (store.graph.relations || [])" :key="i" class="rel">
              <b>{{ r.relationType }}</b> → {{ r.targetName }}
              <small class="muted">({{ r.targetType }})</small>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive } from 'vue'
import { useCmdbStore } from '@/stores/cmdb'
import DataTable from '@/components/DataTable.vue'

const store = useCmdbStore()
const form = reactive({ ciType: '', name: '', attrsRaw: '' })

const ciCols = [
  { key: 'id', title: 'ID', slot: 'cell-id' },
  { key: 'name', title: '名称' },
  { key: 'status', title: '状态' },
  { key: 'source', title: '来源' },
  { key: 'version', title: '版本' }
]
const center = computed(() => (store.graph && store.graph.centerCI) || {})
const hasAttrs = computed(() => center.value.attrs && Object.keys(center.value.attrs).length)

function onOpenCI(row) { store.openGraph(row.id) }
async function onCreate() {
  let attrs = {}
  if (form.attrsRaw.trim()) {
    try { attrs = JSON.parse(form.attrsRaw) }
    catch (e) { store.msg = '属性 JSON 解析失败: ' + e; store.error = 'json'; return }
  }
  try {
    const r = await store.create({ ciType: form.ciType, name: form.name, attrs })
    store.msg = `[${r.s}] ${JSON.stringify(r.j)}`
    store.error = r.s >= 400 ? 'create' : ''
    if (r.s < 400) { form.name = ''; form.attrsRaw = '' }
  } catch (e) {
    store.msg = 'error: ' + (e.j?.error || e.message)
    store.error = 'create'
  }
}

onMounted(() => { store.fetchTypes() })
</script>

<style scoped>
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
.field label { margin: 0; }
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; margin-bottom: 10px; }
.rel {
  font-size: 13px; padding: 7px 12px;
  border-left: 3px solid var(--teal); margin: 5px 0;
  background: var(--teal-soft); border-radius: 0 8px 8px 0;
}
.rel b { color: var(--teal); }
</style>