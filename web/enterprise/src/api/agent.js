// AI Agent API — 对接 /api/v1/ai/agent/*
import { getJSON, postJSON } from './request'

export const executeAgent = (body) => postJSON('/ai/agent/execute', body)
export const classifyIntent = (body) => postJSON('/ai/agent/classify', body)
export const decomposeTask = (body) => postJSON('/ai/agent/decompose', body)
export const getAgentMemory = (sessionId) => getJSON('/ai/agent/memory', { sessionId })
export const clearAgentMemory = (sessionId) => postJSON('/ai/agent/memory', { sessionId })
export const getAgentTools = () => getJSON('/ai/agent/tools')

export const agentApi = {
  executeAgent, classifyIntent, decomposeTask, getAgentMemory, clearAgentMemory, getAgentTools
}
