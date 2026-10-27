// OS 域动作目录（Windows/Unix/macOS/鸿蒙/信创/硬件/内核/备份/证书 等）
// 由 infra-ops.js 合并导出；条目形状见 ./shared.js 注释。
//
// 契约来源（internal/controlplane/）：os_ext_ops.go、git_unix_ops.go、os_tuning_ops.go、domestic_ops.go。
// 全部 30 个端点仅接受 POST，且一律走任务下发通道（202 + taskID），故 taskType 一律 'task'；
// list/get/detect 等查询型动作同样经任务返回，在任务卡片中拉取结果。
import { T, deviceField } from './shared'

export const OS_DOMAINS = [
  // --------------------------------------------------------------------------
  {
    id: 'os-windows',
    name: T('Windows 运维', 'Windows Ops'),
    icon: 'settings',
    actions: [
      {
        id: 'windows-service', name: T('Windows 服务管理', 'Windows Service'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-service',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'start', 'stop', 'restart', 'query', 'create', 'delete'] },
          { key: 'service', label: T('服务名', 'Service'), type: 'text',
            placeholder: 'start/stop/restart/query/create/delete 需填' }
        ]
      },
      {
        id: 'windows-firewall', name: T('Windows 防火墙', 'Windows Firewall'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-firewall',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'status', 'add', 'delete', 'enable', 'disable'] },
          { key: 'protocol', label: T('协议', 'Protocol'), type: 'select', options: ['tcp', 'udp'] },
          { key: 'port', label: T('端口', 'Port'), type: 'text', placeholder: 'add 需填，如 8080' },
          { key: 'name', label: T('规则名', 'Rule Name'), type: 'text', placeholder: 'delete 需填（add 可留空自动命名）' },
          { key: 'direction', label: T('方向', 'Direction'), type: 'select', options: ['in', 'out'] }
        ]
      },
      {
        id: 'windows-package', name: T('Windows 软件包', 'Windows Package'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-package',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'search', 'install', 'upgrade', 'remove'] },
          { key: 'package', label: T('包名', 'Package'), type: 'text', placeholder: '如 Google.Chrome / 7zip' },
          { key: 'manager', label: T('包管理器', 'Manager'), type: 'select', options: ['auto', 'winget', 'choco', 'scoop'] }
        ]
      },
      {
        id: 'windows-user', name: T('Windows 本地用户', 'Windows Local User'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-user',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'delete', 'add-to-group'] },
          { key: 'user', label: T('用户名', 'User'), type: 'text', placeholder: 'create/delete/add-to-group 需填' },
          { key: 'password', label: T('密码', 'Password'), type: 'text', placeholder: 'create 需填' },
          { key: 'group', label: T('用户组', 'Group'), type: 'text', placeholder: 'add-to-group 需填，如 Administrators' }
        ]
      },
      {
        id: 'windows-eventlog', name: T('Windows 事件日志', 'Windows Event Log'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-eventlog',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['query', 'clear', 'export'] },
          { key: 'logName', label: T('日志名称', 'Log Name'), type: 'text', placeholder: 'System' },
          { key: 'level', label: T('级别(query)', 'Level'), type: 'text', placeholder: 'Error,Warning' },
          { key: 'count', label: T('条数(query)', 'Count'), type: 'number', placeholder: '50' }
        ]
      },
      {
        id: 'windows-registry', name: T('Windows 注册表', 'Windows Registry'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-registry',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['query', 'set', 'delete'] },
          { key: 'path', label: T('注册表路径', 'Registry Path'), type: 'text', required: true,
            placeholder: 'HKLM\\SOFTWARE\\...' },
          { key: 'name', label: T('值名', 'Value Name'), type: 'text', placeholder: 'set/delete 需填' },
          { key: 'value', label: T('值数据(set)', 'Value'), type: 'text' },
          { key: 'type', label: T('值类型', 'Value Type'), type: 'select', options: ['REG_SZ', 'REG_DWORD', 'REG_BINARY'] }
        ]
      },
      {
        id: 'windows-perf', name: T('Windows 性能采集', 'Windows Performance'), method: 'POST', taskType: 'task',
        path: () => '/os/windows-perf',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['cpu', 'memory', 'disk', 'network', 'processes'] }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'os-unix',
    name: T('Unix/macOS/鸿蒙', 'Unix/macOS/HarmonyOS'),
    icon: 'runbook',
    actions: [
      {
        id: 'unix', name: T('Unix 运维 (Solaris/AIX)', 'Unix Ops (Solaris/AIX)'), method: 'POST', taskType: 'task',
        path: () => '/os/unix',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'pkg-list', 'pkg-install', 'service-start', 'service-stop'] },
          { key: 'package', label: T('安装包', 'Package'), type: 'text', placeholder: 'pkg-install 需填' },
          { key: 'service', label: T('服务名', 'Service'), type: 'text', placeholder: 'service-start/stop 需填' }
        ]
      },
      {
        id: 'macos-service', name: T('macOS 服务 (launchd)', 'macOS Service (launchd)'), method: 'POST', taskType: 'task',
        path: () => '/os/macos-service',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'status', 'start', 'stop', 'load', 'unload'] },
          { key: 'service', label: T('服务标签', 'Service Label'), type: 'text', placeholder: 'start/stop/status 需填' },
          { key: 'plist', label: T('Plist 路径', 'Plist Path'), type: 'text', placeholder: 'load/unload 需填，如 /Library/LaunchDaemons/x.plist' }
        ]
      },
      {
        id: 'macos-ext', name: T('macOS 扩展 (brew/磁盘)', 'macOS Ext (brew/disk)'), method: 'POST', taskType: 'task',
        path: () => '/os/macos-ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['brew-list', 'brew-install', 'brew-upgrade', 'brew-services', 'disk-info', 'system-info'] },
          { key: 'package', label: T('包名(brew)', 'Package'), type: 'text', placeholder: 'brew-install 需填（brew-upgrade 可空=全量）' }
        ]
      },
      {
        id: 'harmony-ext', name: T('鸿蒙 HarmonyOS 扩展', 'HarmonyOS Ext'), method: 'POST', taskType: 'task',
        path: () => '/os/harmony-ext',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'list-apps', 'install-app', 'uninstall-app', 'transfer-file'] },
          { key: 'appPath', label: T('应用包路径', 'App Path'), type: 'text', placeholder: 'install-app 需填（.hap）' },
          { key: 'appName', label: T('应用包名', 'App Name'), type: 'text', placeholder: 'uninstall-app 需填' },
          { key: 'localPath', label: T('本机文件', 'Local Path'), type: 'text', placeholder: 'transfer-file 需填' },
          { key: 'remotePath', label: T('设备目标路径', 'Remote Path'), type: 'text', placeholder: 'transfer-file 需填' }
        ]
      },
      {
        id: 'process', name: T('进程管理', 'Process'), method: 'POST', taskType: 'task',
        path: () => '/os/process',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'top', 'cgroups', 'info', 'kill', 'nice'] },
          { key: 'filter', label: T('过滤(list)', 'Filter'), type: 'text', placeholder: '如 nginx' },
          { key: 'pid', label: T('PID', 'PID'), type: 'number', placeholder: 'info/kill/nice 需填' },
          { key: 'signal', label: T('信号(kill)', 'Signal'), type: 'text', placeholder: 'TERM' },
          { key: 'nice', label: T('Nice 值', 'Nice'), type: 'number', placeholder: '-5 .. 19' }
        ]
      },
      {
        id: 'log', name: T('日志管理 (journald)', 'Logs (journald)'), method: 'POST', taskType: 'task',
        path: () => '/os/log',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['journal-query', 'journal-vacuum', 'logrotate', 'rsyslog-config', 'status'] },
          { key: 'unit', label: T('Unit(query)', 'Unit'), type: 'text', placeholder: 'sshd.service（空=全量）' },
          { key: 'since', label: T('起始时间(query)', 'Since'), type: 'text', placeholder: '1h ago' },
          { key: 'lines', label: T('行数(query)', 'Lines'), type: 'number', placeholder: '50' }
        ]
      },
      {
        id: 'git', name: T('Git 版本管理', 'Git'), method: 'POST', taskType: 'task',
        path: () => '/os/git',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['status', 'log', 'diff', 'remote', 'branch', 'clone', 'pull', 'push', 'commit', 'checkout', 'tag'] },
          { key: 'repoUrl', label: T('仓库地址', 'Repo URL'), type: 'text', placeholder: 'clone 必填，如 https://github.com/x/y.git' },
          { key: 'path', label: T('仓库目录', 'Repo Path'), type: 'text', placeholder: '/tmp/repo' },
          { key: 'branch', label: T('分支', 'Branch'), type: 'text', placeholder: 'checkout/push 用，如 main' },
          { key: 'message', label: T('提交信息', 'Commit Message'), type: 'text', placeholder: 'commit 必填' },
          { key: 'tag', label: T('标签', 'Tag'), type: 'text', placeholder: 'tag 必填' },
          { key: 'remote', label: T('远端名', 'Remote'), type: 'text', placeholder: 'origin' }
        ]
      },
      {
        id: 'agent-distribute', name: T('Agent 分发', 'Agent Distribute'), method: 'POST', taskType: 'task',
        path: () => '/os/agent-distribute',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['detect', 'download', 'install'] },
          { key: 'arch', label: T('架构', 'Arch'), type: 'select', options: ['amd64', 'arm64'] },
          { key: 'os', label: T('目标系统', 'Target OS'), type: 'select', options: ['linux', 'windows', 'darwin'] },
          { key: 'version', label: T('版本', 'Version'), type: 'text', placeholder: 'latest' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'os-hardware',
    name: T('硬件与磁盘', 'Hardware & Disks'),
    icon: 'datacenter',
    actions: [
      {
        id: 'inventory', name: T('硬件盘点 (DMI/SMBIOS)', 'Hardware Inventory'), method: 'POST', taskType: 'task',
        path: () => '/os/hardware-inventory',
        fields: [deviceField()]
      },
      {
        id: 'inventory-full', name: T('全量硬件采集 (CPU/内存/磁盘/GPU)', 'Full Inventory'), method: 'POST', taskType: 'task',
        path: () => '/os/hardware/inventory-full',
        fields: [deviceField()]
      },
      {
        id: 'sensors', name: T('硬件传感器 (IPMI/SMART)', 'Sensors (IPMI/SMART)'), method: 'POST', taskType: 'task',
        path: () => '/os/hardware/sensors',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['ipmi', 'smart', 'nvme'] }
        ]
      },
      {
        id: 'disk-health', name: T('磁盘健康 (SMART/badblocks)', 'Disk Health'), method: 'POST', taskType: 'task',
        path: () => '/os/disk-health',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['info', 'smart', 'badblocks'] },
          { key: 'disk', label: T('磁盘设备', 'Disk'), type: 'text', placeholder: 'smart/badblocks 需填，如 /dev/sda' }
        ]
      },
      {
        id: 'raid', name: T('RAID 阵列', 'RAID'), method: 'POST', taskType: 'task',
        path: () => '/os/raid',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'create', 'remove'] },
          { key: 'disks', label: T('磁盘列表(逗号分隔)', 'Disks (comma-separated)'), type: 'text',
            placeholder: 'create 需填，如 0:1,0:2' },
          { key: 'level', label: T('RAID 级别', 'RAID Level'), type: 'select', options: ['0', '1', '5', '6', '10'] },
          { key: 'name', label: T('阵列名', 'Array Name'), type: 'text', placeholder: 'create/remove 需填' }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'os-domestic',
    name: T('信创与国产化', 'Domestic Platform'),
    icon: 'gpu',
    actions: [
      {
        id: 'detect', name: T('信创 OS 检测 (UOS/麒麟/欧拉)', 'Domestic OS Detect'), method: 'POST', taskType: 'task',
        path: () => '/os/domestic/detect',
        fields: [deviceField()]
      },
      {
        id: 'cpu', name: T('国产 CPU 检测 (鲲鹏/飞腾/海光/龙芯)', 'Domestic CPU'), method: 'POST', taskType: 'task',
        path: () => '/os/domestic/cpu',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['detect', 'info'] }
        ]
      },
      {
        id: 'gpu', name: T('国产 GPU/加速器 (昇腾/寒武纪/摩尔)', 'Domestic GPU/NPU'), method: 'POST', taskType: 'task',
        path: () => '/os/domestic/gpu',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['detect', 'ascend', 'cambricon', 'moore'] }
        ]
      }
    ]
  },

  // --------------------------------------------------------------------------
  {
    id: 'os-tuning',
    name: T('内核调优与备份', 'Kernel, Tuning & Backup'),
    icon: 'tune',
    actions: [
      {
        id: 'kernel-tuning', name: T('内核参数调优 (sysctl)', 'Kernel Tuning (sysctl)'), method: 'POST', taskType: 'task',
        path: () => '/os/kernel/tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['get', 'set', 'apply-preset'] },
          { key: 'preset', label: T('调优预设', 'Preset'), type: 'select',
            options: ['database', 'web', 'high-concurrency', 'low-latency'] },
          { key: 'param', label: T('参数名(set)', 'Param'), type: 'text', placeholder: 'net.core.somaxconn' },
          { key: 'value', label: T('参数值(set)', 'Value'), type: 'text', placeholder: '65535' }
        ]
      },
      {
        id: 'kernel-module', name: T('内核模块', 'Kernel Module'), method: 'POST', taskType: 'task',
        path: () => '/os/kernel/module',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'load', 'unload', 'info'] },
          { key: 'module', label: T('模块名', 'Module'), type: 'text', placeholder: 'load/unload/info 需填，如 nf_conntrack' }
        ]
      },
      {
        id: 'app-tuning', name: T('应用调优 (JVM/MySQL/Nginx/Redis/ES)', 'App Tuning'), method: 'POST', taskType: 'task',
        path: () => '/os/app-tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['analyze', 'recommend'] },
          { key: 'app', label: T('应用', 'App'), type: 'select', required: true,
            options: ['jvm', 'mysql', 'nginx', 'redis', 'es'] }
        ]
      },
      {
        id: 'storage-tuning', name: T('存储 I/O 调优', 'Storage Tuning'), method: 'POST', taskType: 'task',
        path: () => '/os/storage-tuning',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['get', 'set', 'analyze'] },
          { key: 'device', label: T('块设备(set)', 'Block Device'), type: 'text', placeholder: 'set 需填，如 /dev/sda（不带 /dev/）' },
          { key: 'sched', label: T('调度器(set)', 'Scheduler'), type: 'select',
            options: ['none', 'mq-deadline', 'kyber', 'bfq', 'deadline'] }
        ]
      },
      {
        id: 'resource', name: T('资源管理 (cgroups/ulimit)', 'Resource Limits'), method: 'POST', taskType: 'task',
        path: () => '/os/resource',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'cgroups', 'ulimit', 'quota'] },
          { key: 'user', label: T('用户(quota)', 'User'), type: 'text', placeholder: 'quota 需填' },
          { key: 'limit', label: T('限制值(ulimit)', 'Limit'), type: 'text', placeholder: 'ulimit 需填，如 65535' }
        ]
      },
      {
        id: 'cert', name: T('证书管理 (SSL/TLS)', 'Certificates'), method: 'POST', taskType: 'task',
        path: () => '/os/cert',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['list', 'check', 'generate', 'renew'] },
          { key: 'certPath', label: T('证书路径', 'Cert Path'), type: 'text',
            placeholder: 'check/renew 需填，如 /etc/ssl/certs/server.crt' },
          { key: 'days', label: T('有效天数(generate)', 'Days'), type: 'number', placeholder: '365' }
        ]
      },
      {
        id: 'system-backup', name: T('系统备份/恢复', 'System Backup'), method: 'POST', taskType: 'task',
        path: () => '/os/system-backup',
        fields: [
          deviceField(),
          { key: 'action', label: T('操作', 'Action'), type: 'select', required: true,
            options: ['backup', 'restore', 'list'] },
          { key: 'path', label: T('备份目录', 'Backup Path'), type: 'text', placeholder: '/var/backups/system' }
        ]
      }
    ]
  }
]
