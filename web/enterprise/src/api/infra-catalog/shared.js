// infra-catalog 共享 helper —— 各域目录文件与 infra-ops.js 共用。
// 从 infra-ops.js 抽出（2026-10-27 目录按域拆分时），形状保持逐字不变。

export const T = (zh, en) => ({ zh, en })

// 设备选择字段（几乎所有执行类动作共用）
export const deviceField = (required = true) => ({
  key: 'deviceID', label: T('设备', 'Device'), type: 'device', required
})

// 通用启停字段
export const lifecycleActionOptions = [
  { value: 'start', label: 'start' },
  { value: 'stop', label: 'stop' },
  { value: 'restart', label: 'restart' },
  { value: 'status', label: 'status' }
]

// 动作条目形状（供各域文件参考，不参与运行）：
//   { id, name: T(zh,en), method:'GET'|'POST'|'DELETE', taskType:'task'|'query',
//     path: (f)=>string, fields:[{key,label:T(),type:'device'|'text'|'number'|'select'|'bool'|'textarea',required,options,placeholder,array?}] }
