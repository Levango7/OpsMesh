<template>
  <div class="k8s-deploy-page">
    <div class="page-head">
      <h2 class="page-title">{{ $t('k8sDeploy.title') }}</h2>
      <p class="page-desc">{{ $t('k8sDeploy.desc') }}</p>
    </div>

    <div v-if="store.error" class="poll-err"><Icon name="warning" :size="14" /> {{ store.error }}</div>

    <!-- 部署表单 -->
    <div class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8sDeploy.spec_title') }}</h3>
      </div>
      <form @submit.prevent="onDeploy">
        <div class="row">
          <div class="field">
            <label>{{ $t('k8sDeploy.cluster_name') }}</label>
            <input v-model.trim="form.clusterName" required :placeholder="$t('k8sDeploy.cluster_name_placeholder')" />
          </div>
          <div class="field">
            <label>{{ $t('k8sDeploy.k8s_version') }}</label>
            <select v-model="form.k8sVersion">
              <option value="1.29.4">v1.29.4</option>
              <option value="1.28.9">v1.28.9</option>
              <option value="1.27.13">v1.27.13</option>
              <option value="1.26.15">v1.26.15</option>
            </select>
          </div>
        </div>
        <div class="row">
          <div class="field">
            <label>{{ $t('k8sDeploy.container_runtime') }}</label>
            <select v-model="form.containerRuntime">
              <option value="containerd">containerd</option>
              <option value="cri-o">CRI-O</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('k8sDeploy.cni') }}</label>
            <select v-model="form.cni">
              <option value="calico">Calico</option>
              <option value="cilium">Cilium</option>
              <option value="flannel">Flannel</option>
            </select>
          </div>
          <div class="field">
            <label>{{ $t('k8sDeploy.csi') }}</label>
            <select v-model="form.csi">
              <option value="local-path">Local Path</option>
              <option value="nfs">NFS</option>
              <option value="ceph">Ceph</option>
            </select>
          </div>
        </div>
        <div class="row">
          <div class="field">
            <label>{{ $t('k8sDeploy.registry') }}</label>
            <input v-model.trim="form.registry" :placeholder="$t('k8sDeploy.registry_placeholder')" />
          </div>
          <div class="field">
            <label>{{ $t('k8sDeploy.ha_enabled') }}</label>
            <input type="checkbox" v-model="form.haEnabled" />
          </div>
        </div>

        <!-- 控制平面节点 -->
        <div class="card" style="background: var(--color-surface-2, #f7f9fc);">
          <div class="flowbar">
            <h3 class="card__title">{{ $t('k8sDeploy.cp_nodes_title') }}</h3>
            <Button variant="primary" size="sm" @click="addCPNode">{{ $t('k8sDeploy.add_node') }}</Button>
          </div>
          <div v-for="(node, idx) in form.controlPlaneNodes" :key="idx" class="node-row">
            <div class="field"><label>Host</label><input v-model="node.host" required placeholder="192.168.1.10" /></div>
            <div class="field"><label>User</label><input v-model="node.user" placeholder="root" /></div>
            <div class="field"><label>SSH Key</label><textarea v-model="node.sshKey" rows="3" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" /></div>
            <Button variant="danger" size="sm" @click="removeCPNode(idx)">{{ $t('common.delete') }}</Button>
          </div>
        </div>

        <!-- Worker 节点 -->
        <div class="card" style="background: var(--color-surface-2, #f7f9fc);">
          <div class="flowbar">
            <h3 class="card__title">{{ $t('k8sDeploy.worker_nodes_title') }}</h3>
            <Button variant="primary" size="sm" @click="addWorkerNode">{{ $t('k8sDeploy.add_node') }}</Button>
          </div>
          <div v-for="(node, idx) in form.workerNodes" :key="idx" class="node-row">
            <div class="field"><label>Host</label><input v-model="node.host" required placeholder="192.168.1.20" /></div>
            <div class="field"><label>User</label><input v-model="node.user" placeholder="root" /></div>
            <div class="field"><label>SSH Key</label><textarea v-model="node.sshKey" rows="3" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" /></div>
            <Button variant="danger" size="sm" @click="removeWorkerNode(idx)">{{ $t('common.delete') }}</Button>
          </div>
        </div>

        <!-- HA 配置 -->
        <div v-if="form.haEnabled" class="card">
          <div class="flowbar">
            <h3 class="card__title">{{ $t('k8sDeploy.ha_title') }}</h3>
          </div>
          <div class="row">
            <div class="field">
              <label>{{ $t('k8sDeploy.vip') }}</label>
              <input v-model.trim="form.loadBalancer.vip" required placeholder="192.168.1.100" />
            </div>
            <div class="field">
              <label>{{ $t('k8sDeploy.interface') }}</label>
              <input v-model.trim="form.loadBalancer.interface" placeholder="eth0" />
            </div>
          </div>
        </div>

        <div class="btnbar">
          <Button native-type="submit" variant="primary" :disabled="deploying">{{ $t('k8sDeploy.deploy_btn') }}</Button>
          <Button variant="ghost" size="sm" @click="resetForm">{{ $t('k8sDeploy.reset') }}</Button>
        </div>
        <p v-if="msg" :class="['msg', msgOk ? 'ok' : 'err']">{{ msg }}</p>
      </form>
    </div>

    <!-- 部署状态 -->
    <div v-if="deployStatus" class="card">
      <div class="flowbar">
        <h3 class="card__title">{{ $t('k8sDeploy.status_title') }}</h3>
      </div>
      <div class="status-row">
        <span class="status-label">{{ $t('k8sDeploy.phase') }}:</span>
        <span class="status-value">{{ deployStatus.phase }}</span>
      </div>
      <div class="status-row">
        <span class="status-label">{{ $t('k8sDeploy.progress') }}:</span>
        <span class="status-value">{{ deployStatus.progress }}%</span>
      </div>
      <div class="status-row">
        <span class="status-label">{{ $t('k8sDeploy.current_step') }}:</span>
        <span class="status-value">{{ deployStatus.currentStep }}</span>
      </div>
      <div v-if="deployStatus.error" class="poll-err">{{ deployStatus.error }}</div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { useK8sStore } from '@/stores/k8s'
import { t } from '@/i18n'
import { deployK8sCluster, getK8sClusterDeployStatus } from '@/api/k8s'
import Icon from '@/components/Icon.vue'
import { Button } from '@/components/ui'

const store = useK8sStore()

const form = reactive({
  clusterName: '',
  k8sVersion: '1.29.4',
  containerRuntime: 'containerd',
  cni: 'calico',
  csi: 'local-path',
  registry: 'registry.k8s.io',
  haEnabled: false,
  controlPlaneNodes: [{ host: '', user: 'root', sshKey: '' }],
  workerNodes: [{ host: '', user: 'root', sshKey: '' }],
  loadBalancer: { vip: '', interface: 'eth0' }
})

const deploying = ref(false)
const msg = ref('')
const msgOk = ref(false)
const deployStatus = ref(null)
const pollTimer = ref(null)

function addCPNode() {
  form.controlPlaneNodes.push({ host: '', user: 'root', sshKey: '' })
}

function removeCPNode(idx) {
  form.controlPlaneNodes.splice(idx, 1)
}

function addWorkerNode() {
  form.workerNodes.push({ host: '', user: 'root', sshKey: '' })
}

function removeWorkerNode(idx) {
  form.workerNodes.splice(idx, 1)
}

function resetForm() {
  form.clusterName = ''
  form.k8sVersion = '1.29.4'
  form.containerRuntime = 'containerd'
  form.cni = 'calico'
  form.csi = 'local-path'
  form.registry = 'registry.k8s.io'
  form.haEnabled = false
  form.controlPlaneNodes = [{ host: '', user: 'root', sshKey: '' }]
  form.workerNodes = [{ host: '', user: 'root', sshKey: '' }]
  form.loadBalancer = { vip: '', interface: 'eth0' }
  msg.value = ''
  deployStatus.value = null
}

async function onDeploy() {
  if (deploying.value) return
  deploying.value = true
  msg.value = ''
  try {
    const spec = {
      clusterName: form.clusterName,
      k8sVersion: form.k8sVersion,
      containerRuntime: form.containerRuntime,
      cni: form.cni,
      csi: form.csi,
      registry: form.registry,
      haEnabled: form.haEnabled,
      controlPlaneNodes: form.controlPlaneNodes.map(n => ({ host: n.host, user: n.user, sshKey: n.sshKey, port: 22 })),
      workerNodes: form.workerNodes.map(n => ({ host: n.host, user: n.user, sshKey: n.sshKey, port: 22 })),
      loadBalancer: form.haEnabled ? form.loadBalancer : null
    }
    const r = await deployK8sCluster(spec)
    msg.value = t('k8sDeploy.deploy_started', { id: r.deployID })
    msgOk.value = true
    // 开始轮询状态
    startPolling(r.deployID)
  } catch (e) {
    msg.value = t('k8sDeploy.deploy_failed') + (e.j?.error || e.message || '')
    msgOk.value = false
  } finally {
    deploying.value = false
  }
}

function startPolling(deployID) {
  if (pollTimer.value) clearInterval(pollTimer.value)
  pollTimer.value = setInterval(async () => {
    try {
      const status = await getK8sClusterDeployStatus(deployID)
      deployStatus.value = status
      if (status.phase === 'Completed' || status.phase === 'Failed') {
        clearInterval(pollTimer.value)
        pollTimer.value = null
      }
    } catch {
      // ignore
    }
  }, 3000)
}
</script>

<style scoped>
.k8s-deploy-page { max-width: 900px; margin: 0 auto; }
.page-head { margin-bottom: 20px; }
.page-title { margin: 0 0 4px 0; font-size: 20px; font-weight: 600; }
.page-desc { margin: 0; color: var(--color-ink-muted, #6b7280); font-size: 13px; }
.card {
  background: var(--color-surface-1, #ffffff);
  border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: var(--radius-md, 8px);
  padding: 16px 18px;
  margin-bottom: 16px;
}
.card__title { margin: 0 0 12px 0; font-size: 13px; font-weight: 600; color: var(--color-ink-muted, #6b7280); }
.row { display: flex; gap: 16px; flex-wrap: wrap; margin-bottom: 16px; }
.field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; min-width: 200px; }
.field label { font-size: 11px; font-weight: 500; color: var(--color-ink-muted, #6b7280); }
.field input, .field select, .field textarea {
  padding: 7px 9px; border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: var(--radius-sm, 6px); font-size: 13px;
  background: var(--color-surface-1, #ffffff); color: var(--color-ink, #1f2937);
}
.node-row { display: flex; gap: 10px; flex-wrap: wrap; margin-bottom: 12px; padding: 10px; border: 1px solid var(--color-hairline, #d3dbe8); border-radius: var(--radius-sm, 6px); }
.btnbar { display: flex; gap: 8px; margin-bottom: 12px; }
.status-row { display: flex; gap: 8px; margin-bottom: 6px; font-size: 13px; }
.status-label { color: var(--color-ink-muted, #6b7280); font-weight: 500; }
.status-value { font-weight: 600; }
.muted { color: var(--color-ink-muted, #6b7280); font-size: 12.5px; }
.poll-err {
  padding: 8px 12px; margin: 6px 0 12px; border-radius: var(--radius-sm, 6px);
  background: var(--color-error-soft, #fef2f2); border: 1px solid var(--color-error-light, #fecaca);
  color: var(--color-error, #ef4444); font-size: 12.5px; font-weight: 500;
}
.msg { margin-top: 8px; font-size: 12.5px; }
.msg.ok { color: var(--color-success, #059669); }
.msg.err { color: var(--color-error, #ef4444); }
</style>