<template>
  <!-- 特殊页：运维控制台为域导航 + 动作面板布局，本次仅换骨架与标题，域导航/动作区/表单/结果面板原位保留。
       标题沿用页内双语选择器 L()（catalog 不依赖 i18n 域文件），testid 生成 infra-console-title（spec 断言）。 -->
  <PageShell
    :title="L('基础设施运维台', 'Infrastructure Console')"
    :desc="L('P4/P5/P6 运维能力统一入口：消息队列/数据库/OLAP/时序/向量/图/负载均衡/对象存储/Harbor/监控/Kernel/安全/虚拟化/Ansible',
             'Unified console for middleware / DB / OLAP / TSDB / vector / graph / LB / storage / Harbor / monitoring / kernel / security / virt / Ansible')"
    testid="infra-console"
  >

    <!-- 设备选择（全局） -->
    <div class="toolbar">
      <div class="field">
        <label>{{ L('目标设备', 'Target Device') }}</label>
        <select v-model="selectedDevice" data-testid="device-select">
          <option value="">{{ L('选择设备', 'Select device') }}</option>
          <option v-for="d in devices" :key="d.deviceID" :value="d.deviceID">
            {{ d.hostname || d.deviceID }} ({{ d.ip || d.deviceID }})
          </option>
        </select>
      </div>
      <Button variant="ghost" size="md" @click="refreshDevices" :disabled="loadingDevices">
        <Icon name="refresh" :size="14" /> {{ $t('common.refresh') }}
      </Button>
    </div>

    <div v-if="errorMsg" class="poll-err" data-testid="error-msg"><Icon name="warning" :size="14" /> {{ errorMsg }}</div>

    <div class="console-wrap">
      <!-- 左侧 domain 导航 -->
      <aside class="domain-nav" data-testid="domain-nav">
        <input
          v-model="domainSearch"
          :placeholder="L('搜索域…', 'Search domains…')"
          class="domain-search"
          type="search"
          data-testid="domain-search"
        />
        <Button
          variant="primary" size="md" v-for="d in filteredDomains"
          :key="d.id"
          :class="['domain-btn', { active: currentDomain?.id === d.id }]"
          @click="selectDomain(d)"
          :data-testid="'domain-' + d.id"
        >
          <Icon :name="d.icon || 'deploy'" :size="13" />
          <span>{{ L(d.name.zh, d.name.en) }}</span>
        </Button>
      </aside>

      <!-- 右侧动作区 -->
      <section class="action-panel">
        <div v-if="!selectedDevice" class="muted" data-testid="please-select">{{ L('请先选择目标设备', 'Please select a target device first') }}</div>
        <template v-else-if="currentDomain">
          <h3 style="margin-top: 0">
            <Icon :name="currentDomain.icon || 'deploy'" :size="15" />
            {{ L(currentDomain.name.zh, currentDomain.name.en) }}
          </h3>

          <!-- 动作 tab -->
          <div class="tabbar" data-testid="action-tabs">
            <Button
              variant="primary" size="md" v-for="a in currentDomain.actions"
              :key="a.id"
              :class="['tab', { active: currentAction?.id === a.id }]"
              @click="selectAction(a)"
              :data-testid="'action-' + a.id"
            >
              {{ L(a.name.zh, a.name.en) }}
            </Button>
          </div>

          <!-- 动态表单 -->
          <form v-if="currentAction" class="op-form" @submit.prevent="submit" data-testid="action-form">
            <div v-for="f in visibleFields" :key="f.key" class="field">
              <label>{{ L(f.label.zh, f.label.en) }} <span v-if="f.required">*</span></label>

              <!-- 设备下拉 -->
              <select v-if="f.type === 'device'" v-model="form[f.key]">
                <option value="">{{ L('选择设备', 'Select device') }}</option>
                <option v-for="d in devices" :key="d.deviceID" :value="d.deviceID">
                  {{ d.hostname || d.deviceID }}
                </option>
              </select>

              <!-- 普通下拉 -->
              <select v-else-if="f.type === 'select'" v-model="form[f.key]">
                <option value="">{{ L('请选择', 'Select') }}</option>
                <option v-for="o in normalizedOptions(f)" :key="o.value" :value="o.value">{{ o.label }}</option>
              </select>

              <!-- 复选框 -->
              <label v-else-if="f.type === 'bool'" class="chk">
                <input type="checkbox" v-model="form[f.key]" /> {{ L('启用', 'Enable') }}
              </label>

              <!-- 多行文本 -->
              <textarea
                v-else-if="f.type === 'textarea'"
                v-model="form[f.key]"
                :placeholder="f.placeholder || ''"
                rows="4"
              ></textarea>

              <!-- 数字 -->
              <input
                v-else-if="f.type === 'number'"
                type="number"
                v-model="form[f.key]"
                :placeholder="f.placeholder || ''"
              />

              <!-- 单行文本 -->
              <input v-else v-model.trim="form[f.key]" :placeholder="f.placeholder || ''" />
            </div>

            <div class="btnbar">
              <Button variant="primary" size="md" native-type="submit" :disabled="submitting" data-testid="submit-btn">
                {{ submitting ? $t('common.loading') : L('执行', 'Execute') }}
              </Button>
              <Button variant="ghost" size="md" native-type="button" @click="resetResult">{{ L('清空结果', 'Clear result') }}</Button>
            </div>
          </form>

          <!-- 结果面板 -->
          <div v-if="result" class="result-box" data-testid="result-box">
            <!-- 查询类：直接展示 JSON -->
            <template v-if="result.kind === 'query'">
              <div class="flowbar">
                <h4 style="margin: 0">{{ L('查询结果', 'Query Result') }}</h4>
                <Button variant="ghost" size="sm" @click="copyResult"><Icon name="cmdb" :size="12" /> {{ L('复制', 'Copy') }}</Button>
              </div>
              <pre class="result-pre">{{ prettyJSON(result.data) }}</pre>
            </template>

            <!-- 任务类：显示任务卡片 + 结果轮询 -->
            <template v-else>
              <div class="flowbar">
                <h4 style="margin: 0">{{ L('任务已下发', 'Task Dispatched') }}</h4>
                <span v-if="result.data?.j?.taskID" class="tag ok" data-testid="task-id">ID: {{ result.data.j.taskID }}</span>
                <span v-if="result.data?.s" class="tag">HTTP: {{ result.data.s }}</span>
                <Button variant="ghost" size="sm" @click="pollTaskResult" :disabled="polling">
                  <Icon name="refresh" :size="12" /> {{ L('查询结果', 'Fetch Result') }}
                </Button>
              </div>
              <div v-if="taskOutput" class="task-output">
                <pre class="result-pre">{{ taskOutput }}</pre>
              </div>
              <p v-else class="muted">
                {{ L('任务已在目标设备异步执行，点击"查询结果"拉取输出（需任务完成）。', 'Task is running asynchronously on the agent. Click "Fetch Result" to pull output once done.') }}
              </p>
            </template>
          </div>
        </template>
      </section>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useDeviceStore } from '@/stores/device'
import { currentLang } from '@/i18n'
import { toast } from '@/utils/toast'
import Icon from '@/components/Icon.vue'
import { INFRA_DOMAINS, submitInfraAction, getInfraTaskResult } from '@/api/infra-ops'
import Button from "@/components/ui/Button.vue"

// 本地双语选择器（catalog 内嵌 {zh,en}，不依赖 i18n 域文件）。
const L = (zh, en) => (currentLang.value === 'en' ? en : zh)

const deviceStore = useDeviceStore()
// device store 的设备列表 getter 是 flat（segments 扁平化）。
const devices = computed(() => deviceStore.flat || [])

const selectedDevice = ref('')
const loadingDevices = ref(false)
const errorMsg = ref('')
const submitting = ref(false)
const polling = ref(false)

const currentDomain = ref(INFRA_DOMAINS[0])
const currentAction = ref(INFRA_DOMAINS[0].actions[0])
const form = reactive({})
const result = ref(null)
const taskOutput = ref('')
const domainSearch = ref('')

const filteredDomains = computed(() => {
  const q = domainSearch.value.trim().toLowerCase()
  if (!q) return INFRA_DOMAINS
  return INFRA_DOMAINS.filter((d) => {
    const name = (d.name?.zh || d.name?.en || d.id || '').toLowerCase()
    return name.includes(q) || d.id.includes(q)
  })
})

const prettyJSON = (v) => JSON.stringify(v, null, 2)

// select 选项归一：string[] → [{value,label}]。
function normalizedOptions(f) {
  return (f.options || []).map((o) =>
    typeof o === 'string' ? { value: o, label: o } : o
  )
}

// 可见字段：device 类型字段默认取全局选中设备（仍可改）。
const visibleFields = computed(() => currentAction.value?.fields || [])

function resetForm(keepDevice = true) {
  const kept = keepDevice ? { deviceID: selectedDevice.value } : {}
  for (const k of Object.keys(form)) delete form[k]
  Object.assign(form, kept)
  for (const f of visibleFields.value) {
    if (form[f.key] === undefined) {
      form[f.key] = f.type === 'bool' ? false : ''
    }
  }
  result.value = null
  taskOutput.value = ''
}

function selectDomain(d) {
  currentDomain.value = d
  currentAction.value = d.actions[0]
  resetForm()
}

function selectAction(a) {
  currentAction.value = a
  resetForm()
}

function resetResult() {
  result.value = null
  taskOutput.value = ''
}

async function refreshDevices() {
  loadingDevices.value = true
  try {
    await deviceStore.fetchDevices()
  } catch (e) {
    errorMsg.value = e?.message || String(e)
  } finally {
    loadingDevices.value = false
  }
}

async function submit() {
  errorMsg.value = ''
  // Destructive action confirmation
  const destructive = ['delete', 'drop', 'remove', 'purge', 'uninstall', 'umount', 'delete-queue', 'delete-topic', 'delete-collection', 'delete-user', 'delete-node', 'delete-volume', 'delete-pool', 'delete-snapshot', 'delete-backup']
  const actionId = currentAction.value?.id || ''
  const actionName = currentAction.value?.name?.zh || actionId
  if (destructive.some(d => actionId.includes(d) || actionName.toLowerCase().includes(d))) {
    if (!confirm(L(`确认执行破坏性操作「${actionName}」？`, `Confirm destructive action "${actionName}"?`))) return
  }
  submitting.value = true
  try {
    // 设备字段注入：动作含 deviceID 字段时确保已选。
    const hasDeviceField = visibleFields.value.some((f) => f.key === 'deviceID')
    if (hasDeviceField && !form.deviceID) form.deviceID = selectedDevice.value
    // 数组字段（ruleIDs/packages）由 api 层 split（字段声明 array:true）。
    for (const f of visibleFields.value) {
      if (f.array && typeof form[f.key] === 'string') {
        form[f.key] = form[f.key].split(',').map((s) => s.trim()).filter(Boolean)
      }
    }
    const r = await submitInfraAction(currentDomain.value, currentAction.value, form)
    result.value = r
    if (r.kind === 'task') {
      toast.success(L('任务已下发', 'Task dispatched'))
      // Auto-poll for result after 3s
      setTimeout(() => pollTaskResult(), 3000)
    }
  } catch (e) {
    errorMsg.value = e?.response?.data?.error || e?.message || String(e)
  } finally {
    submitting.value = false
  }
}

async function pollTaskResult() {
  const taskID = result.value?.data?.j?.taskID
  if (!taskID) return
  polling.value = true
  try {
    const data = await getInfraTaskResult(taskID)
    const out = data?.output ?? data?.stdout ?? data?.result ?? data
    taskOutput.value = typeof out === 'string' ? out : prettyJSON(out)
  } catch (e) {
    errorMsg.value = e?.response?.data?.error || e?.message || String(e)
  } finally {
    polling.value = false
  }
}

async function copyResult() {
  try {
    const payload = result.value?.data?.j ?? result.value?.data
    await navigator.clipboard.writeText(taskOutput.value || prettyJSON(payload))
    toast.success(L('已复制', 'Copied'))
  } catch {
    toast.error(L('复制失败', 'Copy failed'))
  }
}

// 同步全局设备选择到表单。
watch(selectedDevice, (v) => {
  if (visibleFields.value.some((f) => f.key === 'deviceID')) form.deviceID = v
})

onMounted(async () => {
  if (!devices.value.length) await refreshDevices()
  resetForm()
})
</script>

<style scoped>
.console-wrap {
  display: flex;
  gap: 16px;
  align-items: flex-start;
  margin-top: 8px;
}
.domain-search {
  width: 100%;
  padding: 6px 8px;
  margin-bottom: 8px;
  border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: 6px;
  font-size: 12px;
  background: var(--color-surface-1, #ffffff);
  color: var(--color-ink, #1f2937);
  box-sizing: border-box;
}
.domain-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  text-align: left;
  padding: 8px 10px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  font-size: 13px;
  color: var(--text-1, #2f3542);
}
.domain-btn:hover { background: var(--bg-2, #f4f5f7); }
.domain-btn.active {
  background: var(--bg-2, #f4f5f7);
  border-color: var(--border, #dfe3e8);
  font-weight: 600;
}
.action-panel {
  flex: 1;
  min-width: 380px;
}
.tabbar {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin: 10px 0;
  border-bottom: 1px solid var(--border, #dfe3e8);
  padding-bottom: 6px;
}
.tab {
  padding: 6px 12px;
  border: 1px solid transparent;
  border-radius: 14px;
  background: transparent;
  cursor: pointer;
  font-size: 13px;
}
.domain-btn:focus-visible,
.tab:focus-visible,
button:focus-visible {
  outline: 2px solid var(--color-primary, #2563eb);
  outline-offset: 2px;
}
.tab.active {
  background: var(--color-primary, #2563eb);
  color: #fff;
}
.op-form .field { margin-bottom: 10px; }
.op-form .field label { display: block; font-size: 12px; color: var(--text-3, #7a8394); margin-bottom: 4px; }
.op-form .field input,
.op-form .field select,
.op-form .field textarea {
  width: 100%;
  max-width: 520px;
  padding: 7px 9px;
  border: 1px solid var(--border, #dfe3e8);
  border-radius: 6px;
  font-size: 13px;
  box-sizing: border-box;
}
.op-form .chk { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; }
.result-box {
  margin-top: 14px;
  padding: 12px;
  border: 1px solid var(--border, #dfe3e8);
  border-radius: 8px;
  background: var(--bg-2, #fafbfc);
}
.result-pre {
  margin: 6px 0 0;
  padding: 10px;
  background: var(--color-surface-2, #1e2430);
  color: var(--color-ink, #d7e3f4);
  border-radius: 6px;
  font-size: 12px;
  max-height: 320px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.flowbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.task-output { margin-top: 8px; }
@media (max-width: 900px) {
  .console-wrap { flex-direction: column; }
  .domain-nav {
    flex-direction: row;
    flex-wrap: wrap;
    max-width: 100%;
    position: static;
    max-height: none;
  }
}
</style>
