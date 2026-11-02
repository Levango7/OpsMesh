// AI Agent API — 对接 /api/v1/ai/agent/*
import { getJSON, postJSON, deleteJSON } from './request'

export const executeAgent = (body) => postJSON('/ai/agent/execute', body)
export const classifyIntent = (body) => postJSON('/ai/agent/classify', body)
export const decomposeTask = (body) => postJSON('/ai/agent/decompose', body)
export const getAgentMemory = (sessionId) => getJSON('/ai/agent/memory', { sessionId })
// 后端 HandleClearMemory 是 DELETE + ?sessionId=（agent/handler.go:270,340）；此前后端 POST
// 只有 GET/DELETE 两条注册 ⇒ 清内存恒 405（用户点「清空记忆」什么也不会发生）。
// deleteJSON 不接受 params 对象，故 sessionId 走 query 拼进 URL。
export const clearAgentMemory = (sessionId) => deleteJSON('/ai/agent/memory?sessionId=' + encodeURIComponent(sessionId))
export const getAgentTools = () => getJSON('/ai/agent/tools')

export const agentApi = {
  executeAgent, classifyIntent, decomposeTask, getAgentMemory, clearAgentMemory, getAgentTools
}
