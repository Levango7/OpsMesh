// Bot / ChatOps API
// 契约：
//   POST   /api/v1/bot/command       {command,platform}     → 200 {id,command,response,status,executedAt}
//   GET    /api/v1/bot/history       ?platform={p}&limit={n} → 200 {history: [{id,command,response,platform,status,executedAt}]}
//   GET    /api/v1/bot/platforms                              → 200 {platforms: [{id,name,enabled}]}
//   GET    /api/v1/bot/quick-commands                         → 200 {commands: [{label,command,platform}]}
import { getJSON, postJSON } from './request'

// 执行命令
export const executeCommand = (command, platform) =>
  postJSON('/bot/command', { command, platform })

// 获取命令历史
export const getCommandHistory = (platform, limit) =>
  getJSON('/bot/history', { platform, limit })

// 获取支持的平台列表
export const getBotPlatforms = () => getJSON('/bot/platforms')

// 获取快捷命令列表
export const getQuickCommands = () => getJSON('/bot/quick-commands')

// 兼容旧引用：聚合对象形式
export const botApi = {
  executeCommand,
  getCommandHistory,
  getBotPlatforms,
  getQuickCommands
}
