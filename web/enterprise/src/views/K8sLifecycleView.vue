<template>
  <PageShell
    :title="$t('k8sLifecycle.title')"
    :desc="$t('k8sLifecycle.desc')"
    testid="k8s-lifecycle"
    :error="loadErr"
  >
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8sLifecycle.upgrade_title') }}</h3>
      </div>
      <div class="row">
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.cluster_id') }}</label>
          <input v-model.trim="upgradeForm.clusterId" placeholder="cluster-1" />
        </div>
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.target_version') }}</label>
          <select v-model="upgradeForm.targetVersion">
            <option value="1.30.0">v1.30.0</option>
            <option value="1.29.4">v1.29.4</option>
            <option value="1.28.9">v1.28.9</option>
          </select>
        </div>
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.cp_node') }}</label>
          <input v-model.trim="upgradeForm.cpNode" placeholder="192.168.1.10" />
        </div>
      </div>
      <div class="btnbar">
        <Button variant="primary" size="sm" @click="planUpgrade" :disabled="planning">{{ $t('k8sLifecycle.plan_btn') }}</Button>
        <Button variant="primary" size="sm" disabled :title="$t('k8sLifecycle.upgrade_unimplemented')">{{ $t('k8sLifecycle.upgrade_btn') }}</Button>
      </div>
      <div class="hint">{{ $t('k8sLifecycle.upgrade_unimplemented') }}</div>
      <div v-if="upgradePlan" class="result-box">
        <pre class="code-block">{{ upgradePlan.planOutput }}</pre>
      </div>
    </div>

    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8sLifecycle.etcd_title') }}</h3>
      </div>
      <div class="row">
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.cp_node') }}</label>
          <input v-model.trim="etcdForm.cpNode" placeholder="192.168.1.10" />
        </div>
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.backup_dir') }}</label>
          <input v-model.trim="etcdForm.backupDir" placeholder="/var/backups/etcd" />
        </div>
      </div>
      <div class="btnbar">
        <Button variant="primary" size="sm" @click="doBackup" :disabled="backing">{{ $t('k8sLifecycle.backup_btn') }}</Button>
        <Button variant="ghost" size="sm" @click="listBackups">{{ $t('k8sLifecycle.list_backups') }}</Button>
      </div>
      <div v-if="backupResult" class="result-box">
        <div class="result-row"><span>{{ $t('k8sLifecycle.backup_path') }}:</span> {{ backupResult.backupPath }}</div>
        <div class="result-row"><span>{{ $t('k8sLifecycle.backup_size') }}:</span> {{ backupResult.size }} bytes</div>
      </div>
      <div v-if="backupList.length" class="backup-list">
        <div v-for="b in backupList" :key="b" class="backup-item">{{ b }}</div>
      </div>
    </div>

    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8sLifecycle.cert_title') }}</h3>
      </div>
      <div class="row">
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.cluster_id') }}</label>
          <input v-model.trim="certForm.clusterId" placeholder="cluster-1" />
        </div>
      </div>
      <div class="btnbar">
        <Button variant="primary" size="sm" disabled :title="$t('k8sLifecycle.cert_unimplemented')">{{ $t('k8sLifecycle.cert_rotate_btn') }}</Button>
      </div>
      <div class="hint">{{ $t('k8sLifecycle.cert_unimplemented') }}</div>
    </div>

    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8sLifecycle.node_scale_title') }}</h3>
      </div>
      <div class="row">
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.action') }}</label>
          <select v-model="scaleForm.action">
            <option value="add">Add</option>
            <option value="remove">Remove</option>
          </select>
        </div>
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.nodes') }}</label>
          <input v-model.trim="scaleForm.nodes" placeholder="192.168.1.20, 192.168.1.21" />
        </div>
        <div class="field field--fixed">
          <label>{{ $t('k8sLifecycle.cp_node') }}</label>
          <input v-model.trim="scaleForm.cpNode" placeholder="192.168.1.10" />
        </div>
      </div>
      <div class="btnbar">
        <Button variant="primary" size="sm" @click="doNodeScale" :disabled="scaling">{{ $t('k8sLifecycle.scale_btn') }}</Button>
      </div>
      <div v-if="scaleResult" class="result-box">
        <pre class="code-block">{{ JSON.stringify(scaleResult, null, 2) }}</pre>
      </div>
    </div>
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { ref } from 'vue'
import { t } from '@/i18n'
import {
  planK8sUpgrade,
  scaleK8sNodes,
  backupK8sEtcd,
  listK8sEtcdBackups
} from '@/api/k8s'
import { Button } from '@/components/ui'

// 六个操作此前静默吞错——统一骨架批次补上可见错误横幅
const loadErr = ref('')

const planning = ref(false)
const backing = ref(false)
const scaling = ref(false)

const upgradeForm = ref({ clusterId: '', targetVersion: '1.30.0', cpNode: '', user: 'root', sshKey: '' })
const etcdForm = ref({ cpNode: '', backupDir: '/var/backups/etcd' })
const certForm = ref({ clusterId: '' })
const scaleForm = ref({ action: 'add', nodes: '', cpNode: '', joinCmd: '' })

const upgradePlan = ref(null)
const backupResult = ref(null)
const backupList = ref([])
const scaleResult = ref(null)

async function planUpgrade() {
  planning.value = true
  try {
    const r = await planK8sUpgrade({
      clusterId: upgradeForm.value.clusterId,
      targetVersion: upgradeForm.value.targetVersion,
      host: upgradeForm.value.cpNode,
      user: upgradeForm.value.user,
      sshKey: upgradeForm.value.sshKey
    })
    upgradePlan.value = r.j
  } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
  finally { planning.value = false }
}

async function doBackup() {
  backing.value = true
  try {
    const r = await backupK8sEtcd({
      clusterId: 'cluster-1',
      cpNode: etcdForm.value.cpNode,
      backupDir: etcdForm.value.backupDir
    })
    backupResult.value = r.j
  } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
  finally { backing.value = false }
}

async function listBackups() {
  try {
    const j = await listK8sEtcdBackups({
      cpNode: etcdForm.value.cpNode,
      backupDir: etcdForm.value.backupDir
    })
    backupList.value = j.backups || []
  } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
}

async function doNodeScale() {
  scaling.value = true
  try {
    const nodes = scaleForm.value.nodes.split(',').map(s => s.trim()).filter(Boolean)
    const r = await scaleK8sNodes({
      clusterId: 'cluster-1',
      action: scaleForm.value.action,
      nodes,
      cpNode: scaleForm.value.cpNode,
      joinCmd: scaleForm.value.joinCmd
    })
    scaleResult.value = r.j
  } catch (e) { loadErr.value = e.j?.error || t('common.load_failed') }
  finally { scaling.value = false }
}
</script>

<style scoped>
.field input, .field select { padding: 7px 9px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); font-size: 13px; background: var(--color-surface-1, #ffffff); }
.result-box { margin-top: 12px; padding: 12px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); }
.code-block { font-size: 12px; font-family: var(--font-family-mono); white-space: pre-wrap; word-break: break-all; max-height: 300px; overflow: auto; }
.status-box { margin-top: 12px; padding: 12px; background: var(--color-surface-2, #f7f9fc); border-radius: var(--radius-sm, 6px); }
.backup-list { margin-top: 8px; }
.backup-item { font-size: 12px; padding: 4px 8px; background: var(--color-surface-2, #f7f9fc); border-radius: 4px; margin-bottom: 4px; }
</style>