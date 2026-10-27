// 运维套件动作目录（CI/CD、容器运行时、IaC/容量、HA/自动化、DB 扩展、平台套件、资产扩展、安全扫描、OAuth/SSO）
// 由 infra-ops.js 合并导出；条目形状见 ./shared.js 注释。
//
// 契约来源（internal/controlplane/，2026-10-27 逐条核对注册行与 handler 内请求 struct）：
//   cicd_ops.go:263-267        CI/CD（Jenkins/GitLab/GitHub Actions/Tekton）
//   container_ops.go:167-169   容器与镜像
//   iac_ops.go:65-66 / capacity_ops.go:62-63    Terraform/Pulumi、容量规划
//   ha_ops.go:53-54            HA（Pacemaker/Keepalived/HAProxy）
//   automation_ops.go:59-60    自动化编排（Rundeck/StackStorm）
//   db_backup_ops.go:172-173 / db_replication_ops.go:95-96   备份/恢复、复制
//   domestic_ops.go:374-375    GaussDB（注册在同一文件的国产化路由组）
//   identity_ops.go:51-52 / oncall_ops.go:52-53 / servicemesh_ops.go:94-95
//   servicediscovery_ops.go:97-98 / statuspage_ops.go:52-53 / synthetic_ops.go:114-115
//   network_ext_ops.go:66-67 / script_compile_ops.go:486-490
//   distfs_ops.go:480-481 / storage_fastdfs.go:125-126 / harbor_ops.go:454
//   graph_ext_ops.go:478 / security_ext_ops.go:370-376 / kafka_advanced_ops.go:297-302
//
// 形态：全部仅接受 POST，且一律走任务下发（dispatchKafkaTask / createSystemTask / dispatchFastDFSTask，
// 返回 taskID → GET /tasks/{id}/result 取输出），故 method 统一 'POST'、taskType 统一 'task'。
// 注：harbor/artifact 用 createSystemTask 直返 task 对象（HTTP 200，非 202），仍是任务形态。
// 字段只收录 handler 真正读取的 json tag（声明但从不读取的字段已剔除：如 ha 的 cluster、
// identity 的 provider、oncall 的 team/user、statuspage 的 component/status、db/backup 的 table 等）。
import { T, deviceField } from './shared'

export const OPS_SUITE_DOMAINS = [
  // ==========================================================================
  // 1. CI/CD 流水线（4 条 /cicd/*）
  // ==========================================================================
  {
    id: 'cicd',
    name: T('CI/CD 流水线', 'CI/CD Pipelines'),
    icon: 'branch',
    actions: [
      {
        id: 'jenkins', name: T('Jenkins', 'Jenkins'), method: 'POST', taskType: 'task',
        path: () => '/cicd/jenkins',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'job-list', 'job-build', 'job-disable', 'job-enable', 'plugin-list'] },
          { key: 'version', label: T('版本(deploy)', 'Version'), type: 'text', placeholder: 'lts-jdk17' },
          { key: 'job', label: T('任务名(job-*)', 'Job'), type: 'text', placeholder: 'job-build/job-disable/job-enable 需填' }
        ]
      },
      {
        id: 'gitlab', name: T('GitLab CI/CD', 'GitLab CI/CD'), method: 'POST', taskType: 'task',
        path: () => '/cicd/gitlab',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'projects', 'pipelines', 'jobs'] },
          { key: 'project', label: T('项目 ID/路径', 'Project'), type: 'text', placeholder: 'pipelines/jobs 需填' }
        ]
      },
      {
        id: 'github-actions', name: T('GitHub Actions', 'GitHub Actions'), method: 'POST', taskType: 'task',
        path: () => '/cicd/github-actions',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'workflows', 'runs'] },
          { key: 'repo', label: T('仓库(owner/repo)', 'Repository'), type: 'text', placeholder: 'workflows/runs 需填' }
        ]
      },
      {
        id: 'tekton', name: T('Tekton', 'Tekton'), method: 'POST', taskType: 'task',
        path: () => '/cicd/tekton',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'pipelines', 'pipelineruns', 'tasks'] }
        ]
      }
    ]
  },

  // ==========================================================================
  // 2. 容器运行时（2 条 /container*）
  // ==========================================================================
  {
    id: 'container-runtime',
    name: T('容器运行时', 'Container Runtime'),
    icon: 'deploy',
    actions: [
      {
        id: 'container', name: T('容器管理', 'Container'), method: 'POST', taskType: 'task',
        path: () => '/container',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'inspect', 'start', 'stop', 'restart', 'remove', 'logs', 'stats'] },
          { key: 'container', label: T('容器名/ID', 'Container'), type: 'text',
            placeholder: 'inspect/start/stop/restart/remove/logs 需填' }
        ]
      },
      {
        id: 'image', name: T('镜像管理', 'Image'), method: 'POST', taskType: 'task',
        path: () => '/container/image',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'pull', 'push', 'build', 'scan', 'sign', 'tag', 'remove'] },
          { key: 'image', label: T('镜像', 'Image'), type: 'text', placeholder: 'pull/push/build/scan/sign/tag/remove 需填' },
          { key: 'tag', label: T('目标标签(tag)', 'Target Tag'), type: 'text', placeholder: 'tag 需填' },
          { key: 'dockerfile', label: T('Dockerfile(build)', 'Dockerfile'), type: 'text', placeholder: 'build 需填' },
          { key: 'context', label: T('构建上下文(build)', 'Build Context'), type: 'text', placeholder: '默认 .' }
        ]
      }
    ]
  },

  // ==========================================================================
  // 3. IaC 与容量规划（2 条：/iac、/capacity）
  // ==========================================================================
  {
    id: 'iac-capacity',
    name: T('IaC 与容量规划', 'IaC & Capacity'),
    icon: 'tune',
    actions: [
      {
        id: 'iac', name: T('基础设施即代码', 'Infrastructure as Code'), method: 'POST', taskType: 'task',
        path: () => '/iac',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['init', 'plan', 'apply', 'destroy', 'status'] },
          { key: 'tool', label: T('工具', 'Tool'), type: 'select', options: ['terraform', 'pulumi'] },
          { key: 'workDir', label: T('工作目录', 'Work Dir'), type: 'text', placeholder: '默认 /tmp/terraform' },
          { key: 'args', label: T('附加参数', 'Args'), type: 'text' }
        ]
      },
      {
        id: 'capacity', name: T('容量规划', 'Capacity Planning'), method: 'POST', taskType: 'task',
        path: () => '/capacity',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['analyze', 'forecast', 'rightsize', 'report'] },
          { key: 'target', label: T('目标(analyze)', 'Target'), type: 'select',
            options: ['cpu', 'memory', 'disk', 'network', 'all'] },
          { key: 'days', label: T('预测天数(forecast)', 'Days'), type: 'number', placeholder: '30' }
        ]
      }
    ]
  },

  // ==========================================================================
  // 4. HA 与自动化编排（2 条：/ha、/automation）
  // ==========================================================================
  {
    id: 'ha-automation',
    name: T('HA 与自动化编排', 'HA & Orchestration'),
    icon: 'autoscaler',
    actions: [
      {
        id: 'ha', name: T('高可用 (Pacemaker/Keepalived)', 'HA Cluster Ops'), method: 'POST', taskType: 'task',
        path: () => '/ha',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'failover', 'test', 'backup', 'restore'] }
        ]
      },
      {
        id: 'automation', name: T('自动化编排 (Rundeck/StackStorm)', 'Automation (Rundeck/StackStorm)'), method: 'POST', taskType: 'task',
        path: () => '/automation',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy-rundeck', 'deploy-stackstorm', 'status', 'run-job', 'list-jobs'] },
          { key: 'job', label: T('作业名(run-job)', 'Job'), type: 'text', placeholder: 'run-job 需填' }
        ]
      }
    ]
  },

  // ==========================================================================
  // 5. 数据库扩展（3 条：/db/backup、/db/replication、/db/gaussdb）
  // ==========================================================================
  {
    id: 'db-ext',
    name: T('数据库扩展', 'Database Extended'),
    icon: 'cmdb',
    actions: [
      {
        id: 'db-backup', name: T('备份/恢复', 'Backup/Restore'), method: 'POST', taskType: 'task',
        path: () => '/db/backup',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['mysql', 'postgresql', 'mongodb', 'redis'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['backup', 'restore', 'list'] },
          { key: 'db', label: T('库名', 'Database'), type: 'text', placeholder: '空=全库（mysql/postgres 默认 postgres，mongo 默认 admin）' },
          { key: 'path', label: T('备份目录', 'Backup Path'), type: 'text', placeholder: '默认 /var/backups/db' },
          { key: 'host', label: T('主机', 'Host'), type: 'text', placeholder: 'localhost' },
          { key: 'port', label: T('端口', 'Port'), type: 'number', placeholder: '3306 / 5432 / 27017' },
          { key: 'user', label: T('用户', 'User'), type: 'text' },
          { key: 'password', label: T('密码', 'Password'), type: 'text' }
        ]
      },
      {
        id: 'db-replication', name: T('复制/主从', 'Replication'), method: 'POST', taskType: 'task',
        path: () => '/db/replication',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['mysql', 'postgresql', 'mongodb', 'redis'] },
          { key: 'action', label: T('操作(promote 仅 mysql/postgresql)', 'Action'), type: 'select', required: true,
            options: ['status', 'lag', 'failover', 'promote'] }
        ]
      },
      {
        id: 'db-gaussdb', name: T('GaussDB', 'GaussDB'), method: 'POST', taskType: 'task',
        path: () => '/db/gaussdb',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'deploy', 'backup', 'restore', 'user'] },
          { key: 'mode', label: T('模式(deploy)', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本(deploy)', 'Version'), type: 'text', placeholder: '5.0.0' }
        ]
      }
    ]
  },

  // ==========================================================================
  // 6. 平台套件（11 条：身份/值班/网格/服务发现/状态页/拨测/网络扩展/脚本三件套/构建工具）
  // ==========================================================================
  {
    id: 'platform-suite',
    name: T('平台套件', 'Platform Suite'),
    icon: 'ops',
    actions: [
      {
        id: 'identity', name: T('身份与访问 (LDAP/AD/SSO)', 'Identity (LDAP/AD/SSO)'), method: 'POST', taskType: 'task',
        path: () => '/identity',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['ldap-status', 'ldap-config', 'sso-status', 'sso-config'] }
        ]
      },
      {
        id: 'oncall', name: T('On-Call 值班', 'On-Call'), method: 'POST', taskType: 'task',
        path: () => '/oncall',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['schedules', 'rotations', 'escalation', 'whoisoncall'] }
        ]
      },
      {
        id: 'servicemesh', name: T('服务网格 (Istio/Linkerd/Consul)', 'Service Mesh'), method: 'POST', taskType: 'task',
        path: () => '/servicemesh',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'traffic', 'canary', 'bluegreen'] },
          { key: 'mesh', label: T('网格(deploy)', 'Mesh'), type: 'select', options: ['istio', 'linkerd', 'consul'] },
          { key: 'version', label: T('版本(deploy)', 'Version'), type: 'text', placeholder: '1.21.0' }
        ]
      },
      {
        id: 'servicediscovery', name: T('服务发现 (Consul/Eureka/Nacos)', 'Service Discovery'), method: 'POST', taskType: 'task',
        path: () => '/servicediscovery',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['consul', 'eureka', 'nacos'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'services', 'kv-get', 'kv-set', 'kv-delete', 'health'] },
          { key: 'key', label: T('键(kv-*)', 'Key'), type: 'text', placeholder: 'kv-get/kv-set/kv-delete 需填' },
          { key: 'value', label: T('值(kv-set)', 'Value'), type: 'text', placeholder: 'kv-set 需填' }
        ]
      },
      {
        id: 'statuspage', name: T('公开状态页', 'Status Page'), method: 'POST', taskType: 'task',
        path: () => '/statuspage',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'components', 'incidents', 'maintenance'] }
        ]
      },
      {
        id: 'synthetic', name: T('合成监控 (Uptime)', 'Synthetic Monitoring'), method: 'POST', taskType: 'task',
        path: () => '/synthetic',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['create', 'list', 'delete', 'run'] },
          { key: 'name', label: T('名称', 'Name'), type: 'text', placeholder: 'create/delete 需填' },
          { key: 'url', label: T('URL/地址', 'URL'), type: 'text', placeholder: 'create/run 需填' },
          { key: 'type', label: T('探针类型', 'Probe Type'), type: 'select', options: ['http', 'tcp', 'icmp'] },
          { key: 'interval', label: T('间隔秒(create)', 'Interval (s)'), type: 'number', placeholder: '60' },
          { key: 'timeout', label: T('超时秒(create)', 'Timeout (s)'), type: 'number', placeholder: '10' },
          { key: 'expected', label: T('期望状态码(create)', 'Expected Code'), type: 'number', placeholder: '200' }
        ]
      },
      {
        id: 'network-ext', name: T('网络扩展 (VPN/CDN/DNS)', 'Network Ext'), method: 'POST', taskType: 'task',
        path: () => '/network/ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['vpn-status', 'vpn-config', 'cdn-purge', 'dns-flush', 'sdwan-status'] },
          { key: 'vpn', label: T('VPN 类型(vpn-config)', 'VPN'), type: 'select',
            options: ['wireguard', 'openvpn', 'ipsec'] },
          { key: 'url', label: T('URL(cdn-purge)', 'URL'), type: 'text', placeholder: 'cdn-purge 需填' }
        ]
      },
      {
        id: 'script-exec', name: T('脚本执行 (多语言)', 'Script Exec'), method: 'POST', taskType: 'task',
        path: () => '/script/exec',
        fields: [
          deviceField(),
          { key: 'language', label: T('语言', 'Language'), type: 'select',
            options: ['shell', 'bash', 'python', 'python3', 'powershell', 'cmd', 'bat', 'ruby', 'perl', 'node', 'go', 'rust', 'java', 'groovy', 'php', 'lua'] },
          { key: 'content', label: T('脚本内容', 'Content'), type: 'textarea', required: true },
          { key: 'args', label: T('命令行参数', 'Args'), type: 'text' },
          { key: 'workDir', label: T('工作目录', 'Work Dir'), type: 'text', placeholder: '默认 /tmp' },
          { key: 'timeout', label: T('超时秒', 'Timeout (s)'), type: 'number', placeholder: '120' }
        ]
      },
      {
        id: 'script-compile', name: T('脚本编译 (go/rust/java/c)', 'Script Compile'), method: 'POST', taskType: 'task',
        path: () => '/script/compile',
        fields: [
          deviceField(),
          { key: 'language', label: T('语言', 'Language'), type: 'select',
            options: ['go', 'rust', 'java', 'c', 'cpp', 'csharp'] },
          { key: 'source', label: T('源码', 'Source'), type: 'textarea', required: true },
          { key: 'output', label: T('输出文件名', 'Output'), type: 'text', placeholder: '默认 /tmp/compiled_out' },
          { key: 'args', label: T('编译参数', 'Args'), type: 'text' },
          { key: 'workDir', label: T('工作目录', 'Work Dir'), type: 'text', placeholder: '默认 /tmp' }
        ]
      },
      {
        id: 'script-template', name: T('脚本模板库', 'Script Templates'), method: 'POST', taskType: 'task',
        path: () => '/script/template',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'get', 'create', 'delete'] },
          { key: 'name', label: T('模板名', 'Name'), type: 'text', placeholder: 'get/create/delete 需填' },
          { key: 'content', label: T('内容(create)', 'Content'), type: 'textarea', placeholder: 'create 需填' }
        ]
      },
      {
        id: 'build-tool', name: T('构建工具 (maven/npm/cargo)', 'Build Tool'), method: 'POST', taskType: 'task',
        path: () => '/build/tool',
        fields: [
          deviceField(),
          { key: 'tool', label: T('工具', 'Tool'), type: 'select', required: true,
            options: ['maven', 'gradle', 'npm', 'yarn', 'cargo', 'make', 'cmake', 'go', 'python'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select',
            options: ['build', 'test', 'clean', 'install', 'package', 'configure', 'mod'] },
          { key: 'project', label: T('项目路径', 'Project Path'), type: 'text', placeholder: '默认 /tmp/project' },
          { key: 'args', label: T('附加参数', 'Args'), type: 'text' },
          { key: 'profile', label: T('Profile/Task(maven/gradle)', 'Profile'), type: 'text' }
        ]
      }
    ]
  },

  // ==========================================================================
  // 7. 资产扩展（4 条：分布式文件系统/FastDFS/Harbor artifact/图库备份扩展）
  // ==========================================================================
  {
    id: 'asset-ext',
    name: T('资产扩展', 'Asset Extended'),
    icon: 'datacenter',
    actions: [
      {
        id: 'storage-distfs', name: T('分布式文件系统 (Ceph/GlusterFS/HDFS/SeaweedFS)', 'Distributed FS'), method: 'POST', taskType: 'task',
        path: () => '/storage/distfs',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['ceph', 'glusterfs', 'hdfs', 'seaweedfs'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'create-pool', 'delete-pool', 'list-pools', 'cephfs-create', 'rbd-create',
              'create-volume', 'delete-volume', 'list-volumes', 'peer-probe', 'mount',
              'mkdir', 'ls', 'put', 'get', 'rm', 'du', 'start-master', 'start-volume', 'upload', 'download'] },
          { key: 'mode', label: T('模式(deploy)', 'Mode'), type: 'select', options: ['single', 'cluster'] },
          { key: 'version', label: T('版本(deploy)', 'Version'), type: 'text' },
          { key: 'pool', label: T('存储池(ceph)', 'Pool'), type: 'text' },
          { key: 'pgNum', label: T('PG 数(ceph)', 'PG Num'), type: 'number' },
          { key: 'volume', label: T('卷(glusterfs/seaweedfs)', 'Volume'), type: 'text' },
          { key: 'bricks', label: T('Brick 列表(glusterfs)', 'Bricks'), type: 'text', placeholder: '逗号分隔' },
          { key: 'path', label: T('路径(HDFS)', 'Path'), type: 'text' },
          { key: 'file', label: T('本机文件', 'Local File'), type: 'text' },
          { key: 'remote', label: T('远端路径(hdfs get/weed download)', 'Remote Path'), type: 'text' },
          { key: 'host', label: T('主机', 'Host'), type: 'text' }
        ]
      },
      {
        id: 'storage-fastdfs', name: T('FastDFS', 'FastDFS'), method: 'POST', taskType: 'task',
        path: () => '/storage/fastdfs',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy-tracker', 'deploy-storage', 'status', 'upload', 'download', 'list', 'delete'] },
          { key: 'tracker', label: T('Tracker 地址(deploy-storage)', 'Tracker'), type: 'text', placeholder: '默认 localhost' },
          { key: 'file', label: T('文件路径(upload/download)', 'File'), type: 'text' },
          { key: 'remote', label: T('远端文件 ID(download/delete)', 'Remote File ID'), type: 'text' }
        ]
      },
      {
        id: 'harbor-artifact', name: T('Harbor Artifact', 'Harbor Artifact'), method: 'POST', taskType: 'task',
        path: () => '/harbor/artifact',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'delete'] },
          { key: 'project', label: T('项目名', 'Project'), type: 'text', required: true },
          { key: 'repository', label: T('仓库名', 'Repository'), type: 'text', required: true },
          { key: 'reference', label: T('Tag/Digest(delete)', 'Reference'), type: 'text', placeholder: 'delete 需填' }
        ]
      },
      {
        id: 'graph-backup-ext', name: T('图库备份 (neo4j-admin/nebula)', 'Graph Backup Ext'), method: 'POST', taskType: 'task',
        path: () => '/graph/backup-ext',
        fields: [
          deviceField(),
          { key: 'type', label: T('类型', 'Type'), type: 'select', required: true,
            options: ['neo4j', 'nebulagraph'] },
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['backup', 'restore', 'list'] },
          { key: 'path', label: T('备份目录', 'Backup Path'), type: 'text', placeholder: '默认 /var/backups/graph' }
        ]
      }
    ]
  },

  // ==========================================================================
  // 8. 安全扫描与加固（5 条 /security/*，与 CORE security 域（scan/baseline/patch）不重叠）
  // ==========================================================================
  {
    id: 'security-scan',
    name: T('安全扫描与加固', 'Security Scan & Hardening'),
    icon: 'permissions',
    actions: [
      {
        id: 'waf', name: T('WAF (ModSecurity)', 'WAF (ModSecurity)'), method: 'POST', taskType: 'task',
        path: () => '/security/waf',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'enable', 'disable', 'list-rules', 'add-rule', 'delete-rule'] },
          { key: 'rule', label: T('规则内容/关键字', 'Rule'), type: 'text', placeholder: 'add-rule 填完整规则；delete-rule 填匹配关键字' }
        ]
      },
      {
        id: 'ddos', name: T('DDoS 防护 (fail2ban/iptables)', 'DDoS Protection'), method: 'POST', taskType: 'task',
        path: () => '/security/ddos',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'enable-fail2ban', 'enable-iptables', 'enable-cloudflare', 'status-all'] },
          { key: 'threshold', label: T('连接数阈值(enable-iptables)', 'Threshold'), type: 'number', placeholder: '100' }
        ]
      },
      {
        id: 'injection', name: T('注入检测 (SQLi/XSS)', 'Injection Scan'), method: 'POST', taskType: 'task',
        path: () => '/security/injection',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['scan-sqli', 'scan-xss', 'scan-headers', 'scan-all'] },
          { key: 'target', label: T('目标 URL', 'Target URL'), type: 'text', required: true, placeholder: 'https://example.com/path' }
        ]
      },
      {
        id: 'vuln-scan', name: T('漏洞扫描 (trivy/semgrep/gosec)', 'Vulnerability Scan'), method: 'POST', taskType: 'task',
        path: () => '/security/vuln-scan',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['scan-image', 'scan-fs', 'scan-repo', 'scan-code'] },
          { key: 'scanner', label: T('扫描器(scan-code)', 'Scanner'), type: 'select',
            options: ['trivy', 'semgrep', 'gosec', 'bandit'] },
          { key: 'target', label: T('目标', 'Target'), type: 'text', required: true, placeholder: '镜像名 / 路径 / 仓库 URL' }
        ]
      },
      {
        id: 'hardening', name: T('安全加固 (CIS/STIG)', 'Hardening (CIS/STIG)'), method: 'POST', taskType: 'task',
        path: () => '/security/hardening',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['cis-baseline', 'stig-baseline', 'ssh-harden', 'audit-enable'] },
          { key: 'level', label: T('级别(cis-baseline)', 'Level'), type: 'select', options: ['level1', 'level2'] }
        ]
      }
    ]
  },

  // ==========================================================================
  // 9. OAuth / SSO 服务端（Keycloak 部署与 realm/client 管理，1 条）
  // 注：路径是 /auth/oauth，但语义不是"登录页 OAuth"——是设备上的 Keycloak 运维（deploy/status/
  // create-realm/create-client），与 /identity 的 SSO 检查同属平台套件；登录页按钮缺失是另一件事。
  // ==========================================================================
  {
    id: 'identity-sso',
    name: T('OAuth/SSO 服务端', 'OAuth/SSO Server'),
    icon: 'login',
    actions: [
      {
        id: 'oauth', name: T('Keycloak 管理', 'Keycloak'), method: 'POST', taskType: 'task',
        path: () => '/auth/oauth',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['deploy', 'status', 'create-realm', 'create-client'] },
          { key: 'version', label: T('版本(deploy)', 'Version'), type: 'text', placeholder: '23.0' },
          { key: 'realm', label: T('Realm(create-realm/create-client)', 'Realm'), type: 'text' },
          { key: 'clientId', label: T('Client ID(create-client)', 'Client ID'), type: 'text' }
        ]
      }
    ]
  }
]
