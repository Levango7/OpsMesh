<template>
  <PageShell :title="$t('k8s.resources')" :desc="$t('k8s.resourcesHint')" testid="k8s-resources" :error="store.error">
    <!-- 子页签：集群管理 / 资源管理（保留原位，位于骨架头部之下） -->
    <K8sSubNav />

    <div class="card">
      <div class="flowbar">
        <div class="field field--fixed">
          <label>{{ $t('k8s.selectCluster') }}</label>
          <select v-model="store.currentClusterID" @change="onClusterChange" data-testid="k8s-cluster-select">
            <option value="">— {{ $t('k8s.selectCluster') }} —</option>
            <option v-for="c in store.clusters" :key="c.id" :value="c.id">
              {{ c.name }}{{ c.server ? ' (' + c.server + ')' : '' }}
            </option>
          </select>
        </div>
        <div class="field field--fixed">
          <label>{{ $t('k8s.namespace') }}</label>
          <input
            v-model="nsInput"
            :placeholder="$t('k8s.namespaceHint')"
            data-testid="k8s-namespace-input"
            @keyup.enter="onNsChange"
          />
        </div>
        <Button variant="ghost" size="sm" @click="onNsChange">
          <Icon name="refresh" :size="13" /> {{ $t('common.refresh') }}
        </Button>
      </div>

      <!-- 资源类型 tab -->
      <div class="res-tabs">
        <Button
          v-for="rt in resourceTypes"
          :key="rt"
          :variant="store.resourceType === rt ? 'primary' : 'ghost'"
          size="sm"
          class="res-tab"
          :class="{ active: store.resourceType === rt }"
          :data-testid="'k8s-tab-' + rt"
          @click="store.setResourceType(rt)"
        >{{ $t('k8s.' + rt) }}</Button>
      </div>

      <div v-if="store.resourcesLoading && !store.resources.length" class="muted">{{ $t('common.loading') }}</div>
      <div v-else-if="!store.currentClusterID" class="muted">{{ $t('k8s.noClusterSelected') }}</div>
      <DataTable
        v-else
        :columns="resourceCols"
        :rows="store.resources"
        :row-key="resourceRowKey"
        :empty-text="$t('k8s.noResources')"
      >
        <template #cell-name="{ value }"><code>{{ value }}</code></template>
        <template #cell-status="{ value }">
          <StatusBadge :status="podStatus(value)" :text="value || '-'" />
        </template>
        <template #cell-image="{ value }"><code>{{ value || '-' }}</code></template>
        <template #cell-images="{ value }">
          <span v-if="value && value.length">{{ value.join(', ') }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-clusterIP="{ value }"><code>{{ value || '-' }}</code></template>
        <template #cell-externalIP="{ value }"><code>{{ value || '-' }}</code></template>
        <template #cell-ports="{ value }"><code>{{ value || '-' }}</code></template>
        <template #cell-dataKeys="{ value }">
          <span v-if="value && value.length">{{ value.join(', ') }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-roles="{ value }">
          <span v-if="value && value.length">{{ value.join(', ') }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-hosts="{ value }">
          <span v-if="value && value.length">{{ value.join(', ') }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-list="{ value }">
          <span v-if="listText(value)">{{ listText(value) }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-endpoints="{ value }">
          <span v-if="endpointText(value)">{{ endpointText(value) }}</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-bool="{ value }">
          <span v-if="value === true">✓</span>
          <span v-else class="muted">—</span>
        </template>
        <template #cell-ready="{ value }">
          <span v-if="value === true">✓</span>
          <span v-else-if="value === false">✗</span>
          <span v-else>{{ value ?? '—' }}</span>
        </template>
        <template #cell-actions="{ row }">
          <div class="row-actions" @click.stop>
            <template v-if="store.resourceType === 'pods'">
              <Button variant="ghost" size="sm" @click="onViewLogs(row)" data-testid="k8s-view-logs-btn">
                {{ $t('k8s.viewLogs') }}
              </Button>
              <Button variant="danger" size="sm" @click="onDeletePod(row)" data-testid="k8s-delete-pod-btn">
                {{ $t('k8s.delete') }}
              </Button>
            </template>
            <template v-else-if="store.resourceType === 'deployments'">
              <Button variant="ghost" size="sm" @click="onScale(row)" data-testid="k8s-scale-btn">
                {{ $t('k8s.scale') }}
              </Button>
              <Button variant="ghost" size="sm" @click="onRestart(row)" data-testid="k8s-restart-btn">
                {{ $t('k8s.restart') }}
              </Button>
            </template>
            <template v-else-if="store.resourceType === 'statefulsets'">
              <Button variant="ghost" size="sm" @click="onScaleStatefulSet(row)">
                {{ $t('k8s.scale') }}
              </Button>
              <Button variant="ghost" size="sm" @click="onRestartStatefulSet(row)">
                {{ $t('k8s.restart') }}
              </Button>
            </template>
            <template v-else-if="store.resourceType === 'daemonsets'">
              <Button variant="ghost" size="sm" @click="onRestartDaemonSet(row)">
                {{ $t('k8s.restart') }}
              </Button>
            </template>
            <template v-else-if="store.resourceType === 'jobs' || store.resourceType === 'cronjobs'">
              <Button variant="danger" size="sm" @click="onDeleteWorkload(row)">
                {{ $t('k8s.delete') }}
              </Button>
            </template>
            <span v-else class="muted">—</span>
          </div>
        </template>
      </DataTable>
    </div>

    <!-- Pod 日志对话框 -->
    <div v-if="logsOpen" class="modal-mask modal-mask--inset" data-testid="k8s-logs-modal" @click.self="logsOpen = false">
      <div class="modal modal--fluid modal--wide">
        <header class="modal-head modal-head--bordered">
          <h3>{{ $t('k8s.viewLogs') }} · <code>{{ logsTarget?.name }}</code></h3>
          <Button variant="ghost" size="sm" @click="logsOpen = false">✕</Button>
        </header>
        <div class="modal-body">
          <div class="flowbar">
            <div class="field field--fixed">
              <label>{{ $t('k8s.tailLines') }}</label>
              <input v-model.number="logsForm.tailLines" type="number" min="1" max="10000" style="width: 100px" />
            </div>
            <div class="field field--fixed">
              <label>{{ $t('k8s.container') }}</label>
              <input v-model="logsForm.container" :placeholder="$t('k8s.containerHint')" />
            </div>
            <Button variant="primary" size="sm" @click="fetchLogs">{{ $t('k8s.refreshLogs') }}</Button>
          </div>
          <pre class="code-block logs-block">{{ logsContent || '—' }}</pre>
        </div>
      </div>
    </div>

    <!-- 扩缩容对话框 -->
    <div v-if="scaleOpen" class="modal-mask modal-mask--inset" data-testid="k8s-scale-modal" @click.self="scaleOpen = false">
      <div class="modal modal--fluid modal--narrow">
        <header class="modal-head modal-head--bordered">
          <h3>{{ $t('k8s.scale') }} · <code>{{ scaleTarget?.name }}</code></h3>
          <Button variant="ghost" size="sm" @click="scaleOpen = false">✕</Button>
        </header>
        <div class="modal-body">
          <p class="muted">{{ $t('k8s.scaleHint') }}</p>
          <p>{{ $t('k8s.scaleCurrent', { replicas: scaleTarget?.replicas || 0, available: scaleTarget?.availableReplicas || 0 }) }}</p>
          <div class="field field--fixed">
            <label>{{ $t('k8s.targetReplicas') }}</label>
            <input v-model.number="scaleReplicas" type="number" min="0" data-testid="k8s-scale-replicas" />
          </div>
          <div class="btnbar">
            <Button variant="primary" size="sm" @click="confirmScale" :disabled="scaling" data-testid="k8s-scale-confirm">
              {{ $t('k8s.confirm') }}
            </Button>
            <Button variant="ghost" size="sm" @click="scaleOpen = false">{{ $t('k8s.cancel') }}</Button>
          </div>
          <p v-if="scaleMsg" :class="['msg', scaleOk ? 'ok' : 'err']">{{ scaleMsg }}</p>
        </div>
      </div>
    </div>

    <ConfirmModal
      v-model="deleteConfirm.show"
      data-testid="k8s-delete-confirm-modal"
      :title="deleteConfirm.kind === 'restart' ? $t('k8s.restart') : $t('k8s.delete')"
      :message="deleteConfirm.kind === 'restart' ? $t('k8s.restartConfirm') : $t('k8s.deletePodConfirm')"
      @confirm="onDeleteConfirm"
    />
  </PageShell>
</template>

<script setup>
import PageShell from '@/components/PageShell.vue'

import { computed, onMounted, reactive, ref } from 'vue'
import { useK8sStore } from '@/stores/k8s'
import { t } from '@/i18n'
import DataTable from '@/components/DataTable.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import Icon from '@/components/Icon.vue'
import ConfirmModal from '@/components/ConfirmModal.vue'
import K8sSubNav from '@/components/K8sSubNav.vue'
import { Button } from '@/components/ui'
import { toast } from '@/utils/toast'

const store = useK8sStore()

const resourceTypes = ['pods', 'deployments', 'statefulsets', 'daemonsets', 'replicasets', 'jobs', 'cronjobs', 'services', 'ingresses', 'configmaps', 'secrets', 'pvcs', 'pvs', 'hpas', 'roles', 'rolebindings', 'clusterroles', 'clusterrolebindings', 'crds', 'nodes', 'networkpolicies', 'ingressclasses', 'storageclasses', 'pdbs', 'serviceaccounts', 'endpointslices', 'resourcequotas', 'limitranges', 'priorityclasses', 'runtimeclasses', 'leases', 'events', 'mutatingwebhooks', 'validatingwebhooks', 'csrs']

const resourceCols = computed(() => {
  switch (store.resourceType) {
    case 'pods':
      return [
        { key: 'name', title: 'Pod', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'podIP', title: t('k8s.col.podIP') },
        { key: 'nodeIP', title: t('k8s.col.nodeIP') },
        { key: 'nodeName', title: t('k8s.col.nodeName') },
        { key: 'images', title: t('k8s.col.images'), slot: 'cell-images' },
        { key: 'restarts', title: t('k8s.col.restarts') },
        { key: 'affinity', title: t('k8s.col.affinity') },
        { key: 'age', title: t('k8s.col.age') },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '160px' }
      ]
    case 'deployments':
      return [
        { key: 'name', title: 'Deployment', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'replicas', title: t('k8s.col.replicas') },
        { key: 'availableReplicas', title: t('k8s.col.available') },
        { key: 'image', title: t('k8s.col.image'), slot: 'cell-image' },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '140px' }
      ]
    case 'statefulsets':
      return [
        { key: 'name', title: 'StatefulSet', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'replicas', title: t('k8s.col.replicas') },
        { key: 'ready', title: t('k8s.col.ready') },
        { key: 'image', title: t('k8s.col.image'), slot: 'cell-image' },
        { key: 'serviceName', title: t('k8s.col.serviceName') },
        { key: 'age', title: t('k8s.col.age') },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '140px' }
      ]
    case 'daemonsets':
      return [
        { key: 'name', title: 'DaemonSet', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'desired', title: t('k8s.col.desired') },
        { key: 'ready', title: t('k8s.col.ready') },
        { key: 'available', title: t('k8s.col.available') },
        { key: 'image', title: t('k8s.col.image'), slot: 'cell-image' },
        { key: 'age', title: t('k8s.col.age') },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '100px' }
      ]
    case 'replicasets':
      return [
        { key: 'name', title: 'ReplicaSet', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'replicas', title: t('k8s.col.replicas') },
        { key: 'ready', title: t('k8s.col.ready') },
        { key: 'image', title: t('k8s.col.image'), slot: 'cell-image' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'jobs':
      return [
        { key: 'name', title: 'Job', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'succeeded', title: t('k8s.col.succeeded') },
        { key: 'failed', title: t('k8s.col.failed') },
        { key: 'active', title: t('k8s.col.active') },
        { key: 'age', title: t('k8s.col.age') },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '100px' }
      ]
    case 'cronjobs':
      return [
        { key: 'name', title: 'CronJob', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'schedule', title: t('k8s.col.schedule') },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'age', title: t('k8s.col.age') },
        { key: 'actions', title: t('k8s.col.action'), slot: 'cell-actions', width: '100px' }
      ]
    case 'services':
      return [
        { key: 'name', title: 'Service', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'type', title: t('k8s.col.type') },
        { key: 'clusterIP', title: t('k8s.col.clusterIP'), slot: 'cell-clusterIP' },
        { key: 'externalIP', title: t('k8s.col.externalIP'), slot: 'cell-externalIP' },
        { key: 'ports', title: t('k8s.col.ports'), slot: 'cell-ports' }
      ]
    case 'ingresses':
      return [
        { key: 'name', title: 'Ingress', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'class', title: t('k8s.col.ingressClass') },
        { key: 'hosts', title: t('k8s.col.hosts'), slot: 'cell-hosts' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'configmaps':
      return [
        { key: 'name', title: 'ConfigMap', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'dataKeys', title: t('k8s.col.dataKeys'), slot: 'cell-dataKeys' }
      ]
    case 'secrets':
      return [
        { key: 'name', title: 'Secret', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'type', title: t('k8s.col.type') },
        { key: 'dataKeys', title: t('k8s.col.dataKeys'), slot: 'cell-dataKeys' }
      ]
    case 'pvcs':
      return [
        { key: 'name', title: 'PVC', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'capacity', title: t('k8s.col.capacity') },
        { key: 'storageClass', title: t('k8s.col.storageClass') },
        { key: 'volumeName', title: t('k8s.col.volumeName') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'pvs':
      return [
        { key: 'name', title: 'PV', slot: 'cell-name' },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'capacity', title: t('k8s.col.capacity') },
        { key: 'storageClass', title: t('k8s.col.storageClass') },
        { key: 'claimRef', title: t('k8s.col.claimRef') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'hpas':
      return [
        { key: 'name', title: 'HPA', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'target', title: t('k8s.col.target') },
        { key: 'minReplicas', title: t('k8s.col.minReplicas') },
        { key: 'maxReplicas', title: t('k8s.col.maxReplicas') },
        { key: 'current', title: t('k8s.col.current') },
        { key: 'desired', title: t('k8s.col.desired') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'roles':
      return [
        { key: 'name', title: 'Role', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'rules', title: t('k8s.col.rules') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'rolebindings':
      return [
        { key: 'name', title: 'RoleBinding', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'roleRef', title: t('k8s.col.roleRef') },
        { key: 'subjects', title: t('k8s.col.subjects') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'clusterroles':
      return [
        { key: 'name', title: 'ClusterRole', slot: 'cell-name' },
        { key: 'rules', title: t('k8s.col.rules') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'clusterrolebindings':
      return [
        { key: 'name', title: 'ClusterRoleBinding', slot: 'cell-name' },
        { key: 'roleRef', title: t('k8s.col.roleRef') },
        { key: 'subjects', title: t('k8s.col.subjects') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'crds':
      return [
        { key: 'name', title: 'CRD', slot: 'cell-name' },
        { key: 'kind', title: t('k8s.col.kind') }
      ]
    case 'nodes':
      return [
        { key: 'name', title: 'Node', slot: 'cell-name' },
        { key: 'status', title: t('k8s.col.status'), slot: 'cell-status' },
        { key: 'roles', title: t('k8s.col.roles'), slot: 'cell-roles' },
        { key: 'version', title: t('k8s.col.version') },
        { key: 'internalIP', title: t('k8s.col.internalIP') },
        { key: 'externalIP', title: t('k8s.col.externalIPNode') },
        { key: 'cpu', title: t('k8s.col.cpu') },
        { key: 'memory', title: t('k8s.col.memory') }
      ]
    case 'networkpolicies':
      return [
        { key: 'name', title: 'NetworkPolicy', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'podSelector', title: t('k8s.col.podSelector') },
        { key: 'policyTypes', title: t('k8s.col.policyTypes'), slot: 'cell-list' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'ingressclasses':
      return [
        { key: 'name', title: 'IngressClass', slot: 'cell-name' },
        { key: 'controller', title: t('k8s.col.controller') },
        { key: 'isDefault', title: t('k8s.col.isDefault'), slot: 'cell-bool' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'storageclasses':
      return [
        { key: 'name', title: 'StorageClass', slot: 'cell-name' },
        { key: 'provisioner', title: t('k8s.col.provisioner') },
        { key: 'reclaimPolicy', title: t('k8s.col.reclaimPolicy') },
        { key: 'volumeBindingMode', title: t('k8s.col.volumeBindingMode') },
        { key: 'isDefault', title: t('k8s.col.isDefault'), slot: 'cell-bool' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'pdbs':
      return [
        { key: 'name', title: 'PodDisruptionBudget', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'minAvailable', title: t('k8s.col.minAvailable') },
        { key: 'maxUnavailable', title: t('k8s.col.maxUnavailable') },
        { key: 'currentHealthy', title: t('k8s.col.currentHealthy') },
        { key: 'desiredHealthy', title: t('k8s.col.desiredHealthy') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'serviceaccounts':
      return [
        { key: 'name', title: 'ServiceAccount', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'secrets', title: t('k8s.col.secrets'), slot: 'cell-list' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'endpointslices':
      return [
        { key: 'name', title: 'EndpointSlice', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'service', title: t('k8s.col.service') },
        { key: 'addressType', title: t('k8s.col.addressType') },
        { key: 'endpoints', title: t('k8s.col.endpoints'), slot: 'cell-endpoints' },
        { key: 'ready', title: t('k8s.col.ready'), slot: 'cell-ready' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'resourcequotas':
      return [
        { key: 'name', title: 'ResourceQuota', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'hard', title: t('k8s.col.hard'), slot: 'cell-list' },
        { key: 'used', title: t('k8s.col.used'), slot: 'cell-list' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'limitranges':
      return [
        { key: 'name', title: 'LimitRange', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'limits', title: t('k8s.col.limits'), slot: 'cell-list' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'priorityclasses':
      return [
        { key: 'name', title: 'PriorityClass', slot: 'cell-name' },
        { key: 'value', title: t('k8s.col.value') },
        { key: 'globalDefault', title: t('k8s.col.globalDefault'), slot: 'cell-bool' },
        { key: 'preemptionPolicy', title: t('k8s.col.preemptionPolicy') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'runtimeclasses':
      return [
        { key: 'name', title: 'RuntimeClass', slot: 'cell-name' },
        { key: 'handler', title: t('k8s.col.handler') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'leases':
      return [
        { key: 'name', title: 'Lease', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'holder', title: t('k8s.col.holder') },
        { key: 'renewTime', title: t('k8s.col.renewTime') },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'events':
      return [
        { key: 'name', title: 'Event', slot: 'cell-name' },
        { key: 'namespace', title: t('k8s.col.namespace') },
        { key: 'type', title: t('k8s.col.type') },
        { key: 'reason', title: t('k8s.col.reason') },
        { key: 'object', title: t('k8s.col.object') },
        { key: 'count', title: t('k8s.col.count') },
        { key: 'lastSeen', title: t('k8s.col.lastSeen') }
      ]
    case 'mutatingwebhooks':
      return [
        { key: 'name', title: 'MutatingWebhookConfiguration', slot: 'cell-name' },
        { key: 'webhooks', title: t('k8s.col.webhooks'), slot: 'cell-list' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'validatingwebhooks':
      return [
        { key: 'name', title: 'ValidatingWebhookConfiguration', slot: 'cell-name' },
        { key: 'webhooks', title: t('k8s.col.webhooks'), slot: 'cell-list' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    case 'csrs':
      return [
        { key: 'name', title: 'CSR', slot: 'cell-name' },
        { key: 'signer', title: t('k8s.col.signer') },
        { key: 'condition', title: t('k8s.col.condition'), slot: 'cell-status' },
        { key: 'age', title: t('k8s.col.age') }
      ]
    default:
      return []
  }
})

const resourceRowKey = computed(() => 'name')

const nsInput = ref('')

function podStatus(s) {
  if (!s) return 'info'
  const v = String(s).toLowerCase()
  if (v === 'running' || v === 'ready' || v === 'online' || v === 'succeeded' || v === 'active' || v === 'approved') return 'success'
  if (v === 'failed' || v === 'error' || v === 'crashloopbackoff' || v === 'offline' || v === 'denied') return 'failed'
  if (v === 'pending' || v === 'unknown' || v === 'suspended') return 'info'
  return 'warn'
}

// 数组/对象字段渲染：数组逐项 join，对象渲染为 k=v 列表
function listText(value) {
  if (value == null || value === '') return ''
  if (Array.isArray(value)) {
    return value
      .map((v) => (v && typeof v === 'object' ? Object.entries(v).map(([k, val]) => `${k}=${val}`).join(' ') : String(v)))
      .join(', ')
  }
  if (typeof value === 'object') {
    return Object.entries(value).map(([k, val]) => `${k}=${val}`).join(', ')
  }
  return String(value)
}

// EndpointSlice endpoints：条目可能是字符串，也可能是 {address, ready, ...} 对象
function endpointText(value) {
  if (!Array.isArray(value)) return listText(value)
  return value
    .map((e) => (e && typeof e === 'object' ? e.address || '' : String(e)))
    .filter(Boolean)
    .join(', ')
}

async function onClusterChange() {
  nsInput.value = ''
  await store.selectCluster(store.currentClusterID)
}

function onNsChange() {
  store.setNamespace(nsInput.value.trim())
}

// ---- Pod 日志 ----
const logsOpen = ref(false)
const logsTarget = ref(null)
const logsForm = ref({ tailLines: 100, container: '' })
const logsContent = ref('')

async function onViewLogs(row) {
  logsTarget.value = row
  logsOpen.value = true
  logsForm.value = { tailLines: 100, container: '' }
  logsContent.value = t('k8s.logsLoading')
  await fetchLogs()
}

async function fetchLogs() {
  if (!logsTarget.value || !store.currentClusterID) return
  try {
    const data = await store.fetchPodLogs(
      store.currentClusterID,
      logsTarget.value.namespace,
      logsTarget.value.name,
      logsForm.value.tailLines,
      logsForm.value.container
    )
    logsContent.value = (data && data.logs) || data || '—'
  } catch (e) {
    logsContent.value = t('k8s.logsFetchFail', { msg: (e.j?.error || e.message || e) })
  }
}

// ---- 删除 Pod ----
const deleteConfirm = reactive({ show: false, kind: '', row: null })

function onDeletePod(row) {
  deleteConfirm.kind = 'pod'
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function onDeleteConfirm() {
  const { kind, row } = deleteConfirm
  if (!kind) return
  if (kind === 'pod') await doDeletePod(row)
  else if (kind === 'restart') await doRestart()
  else if (kind === 'deleteWorkload') await doDeleteWorkload(row)
}

async function doDeletePod(row) {
  try {
    const r = await store.removePod(store.currentClusterID, row.namespace, row.name)
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      store.fetchResources()
    } else {
      toast.error(t('k8s.deleteFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.deleteFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- 扩缩容 Deployment ----
const scaleOpen = ref(false)
const scaleTarget = ref(null)
const scaleReplicas = ref(0)
const scaling = ref(false)
const scaleMsg = ref('')
const scaleOk = ref(false)

function onScale(row) {
  scaleTarget.value = row
  scaleReplicas.value = row.replicas || 0
  scaleOpen.value = true
  scaleMsg.value = ''
  scalingStatefulSet.value = false
}

async function confirmScale() {
  if (!scaleTarget.value) return
  scaling.value = true
  try {
    let r
    if (scalingStatefulSet.value) {
      r = await store.scaleStatefulSet(store.currentClusterID, scaleTarget.value.namespace, scaleTarget.value.name, scaleReplicas.value)
    } else {
      r = await store.scaleDeployment(store.currentClusterID, scaleTarget.value.namespace, scaleTarget.value.name, scaleReplicas.value)
    }
    if (r.s >= 200 && r.s < 300) {
      scaleMsg.value = t('k8s.scaleSuccess'); scaleOk.value = true
      await store.fetchResources()
      setTimeout(() => { scaleOpen.value = false; scalingStatefulSet.value = false }, 1200)
    } else {
      scaleMsg.value = t('k8s.scaleFailHttp', { code: r.s }); scaleOk.value = false
    }
  } catch (e) {
    scaleMsg.value = t('k8s.scaleFailError', { msg: (e.j?.error || e.message || e) }); scaleOk.value = false
  } finally {
    scaling.value = false
  }
}

// ---- 重启 Deployment ----
const restartTarget = ref(null)

function onRestart(row) {
  restartTarget.value = row
  deleteConfirm.kind = 'restart'
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function doRestart() {
  const row = restartTarget.value
  if (!row) return
  try {
    const r = await store.restartDeployment(store.currentClusterID, row.namespace, row.name)
    if (r.s >= 200 && r.s < 300) {
      toast.success(t('k8s.restartSuccess'))
      store.fetchResources()
    } else {
      toast.error(t('k8s.restartFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.restartFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- StatefulSet 扩缩容/重启 ----
const scalingStatefulSet = ref(false)

function onScaleStatefulSet(row) {
  scaleTarget.value = row
  scaleReplicas.value = row.replicas || 0
  scaleOpen.value = true
  scaleMsg.value = ''
  scalingStatefulSet.value = true
}

async function onRestartStatefulSet(row) {
  try {
    const r = await store.restartStatefulSet(store.currentClusterID, row.namespace, row.name)
    if (r.s >= 200 && r.s < 300) {
      toast.success(t('k8s.restartSuccess'))
      store.fetchResources()
    } else {
      toast.error(t('k8s.restartFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.restartFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- DaemonSet 重启 ----
async function onRestartDaemonSet(row) {
  try {
    const r = await store.restartDaemonSet(store.currentClusterID, row.namespace, row.name)
    if (r.s >= 200 && r.s < 300) {
      toast.success(t('k8s.restartSuccess'))
      store.fetchResources()
    } else {
      toast.error(t('k8s.restartFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.restartFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

// ---- 删除 Job/CronJob ----
async function onDeleteWorkload(row) {
  deleteConfirm.kind = 'deleteWorkload'
  deleteConfirm.row = row
  deleteConfirm.show = true
}

async function doDeleteWorkload(row) {
  try {
    let r
    if (store.resourceType === 'jobs') {
      r = await store.deleteJob(store.currentClusterID, row.namespace, row.name)
    } else {
      r = await store.deleteCronJob(store.currentClusterID, row.namespace, row.name)
    }
    if (r.s === 204 || (r.s >= 200 && r.s < 300)) {
      store.fetchResources()
    } else {
      toast.error(t('k8s.deleteFailHttp', { code: r.s }))
    }
  } catch (e) {
    toast.error(t('k8s.deleteFailError', { msg: (e.j?.error || e.message || e) }))
  }
}

onMounted(() => {
  store.fetchClusters()
  if (store.currentClusterID) store.selectCluster(store.currentClusterID)
})
</script>

<style scoped>
.field input, .field select {
  padding: 7px 9px; border: 1px solid var(--color-hairline, #d3dbe8);
  border-radius: var(--radius-sm, 6px); font-size: 13px;
  background: var(--color-surface-1, #ffffff); color: var(--color-ink, #1f2937);
}

.res-tabs { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
.res-tab { font-size: 12px; }

.code-block { background: var(--color-surface-2, #f7f9fc); padding: 12px; border-radius: var(--radius-sm, 6px); overflow: auto; font-size: 12px; font-family: var(--font-family-mono); white-space: pre-wrap; word-break: break-all; max-height: 480px; }
.logs-block { min-height: 200px; }

/* 对话框已全部改用 styles/pages.css 全局家族 A（2026-10-30 收编）：
   .modal-mask--inset（原 16px 内距，本页 z-index 保持全局 50）/
   .modal--fluid + .modal--wide/--narrow（原 100%+520px，lg/sm 抬上限到 760/420）/
   .modal-head--bordered（原带下边框页头）。模板已挂对应修饰类。 */

/* 暗色模式：.card 的暗色规则在 pages.css；.field 输入控件的暗色规则随对话框家族一并上移 */
[data-theme="dark"] .card {
  background: var(--color-surface-2, #1e2430); border-color: var(--color-hairline, #23252a); color: var(--color-ink, #f7f8f8);
}
</style>
