// SSE 客户端单元测试
// 覆盖：帧解析（parseSSEFrame）、信封契约校验（validateEnvelope）、
// data 字段校验（validateEventData）、客户端事件分发与契约违规丢弃。
// 不发起真实网络：直接调用纯函数 + 构造 mock fetch 验证分发。
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { parseSSEFrame, validateEnvelope, validateEventData, EVENT_CONTRACT, SSEClient } from '@/api/sse'

describe('parseSSEFrame', () => {
  it('解析单帧 event + data', () => {
    const frames = parseSSEFrame('event: task_status\ndata: {"taskID":"t1","status":"done"}\n\n')
    expect(frames).toHaveLength(1)
    expect(frames[0].event).toBe('task_status')
    expect(JSON.parse(frames[0].data).status).toBe('done')
  })

  it('解析 hello 握手帧（空对象 data）', () => {
    const frames = parseSSEFrame('event: hello\ndata: {}\n\n')
    expect(frames).toHaveLength(1)
    expect(frames[0].event).toBe('hello')
    expect(JSON.parse(frames[0].data)).toEqual({})
  })

  it('忽略注释帧（心跳 : ping）', () => {
    const frames = parseSSEFrame(': ping\n\n')
    expect(frames).toHaveLength(0)
  })

  it('多帧连续解析', () => {
    const frames = parseSSEFrame(
      'event: hello\ndata: {}\n\n' +
      'event: device_online\ndata: {"deviceID":"d1","segment":"default","addr":"1.2.3.4"}\n\n'
    )
    expect(frames).toHaveLength(2)
    expect(frames[0].event).toBe('hello')
    expect(frames[1].event).toBe('device_online')
  })

  it('data 多行拼接（SSE 规范）', () => {
    const frames = parseSSEFrame('event: agent_logs\ndata: {"lines":["a"]}\ndata: {"lines":["b"]}\n\n')
    expect(frames).toHaveLength(1)
    // 多行 data 以 \n 拼接——注意这里是测试解析器拼接行为，非真实 JSON
    expect(frames[0].data).toContain('\n')
  })

  it('缺省 event 时默认 message', () => {
    const frames = parseSSEFrame('data: {"x":1}\n\n')
    expect(frames[0].event).toBe('message')
  })
})

describe('validateEnvelope（信封契约）', () => {
  it('合法信封通过', () => {
    expect(validateEnvelope({ type: 'task_status', data: { taskID: 't1', status: 'done' } })).toBe(true)
    expect(validateEnvelope({ type: 'hello', data: {} })).toBe(true)
  })

  it('未知事件类型拒绝', () => {
    expect(validateEnvelope({ type: 'evil_event', data: {} })).toBe(false)
  })

  it('缺 data 拒绝', () => {
    expect(validateEnvelope({ type: 'hello' })).toBe(false)
    expect(validateEnvelope({ type: 'hello', data: null })).toBe(false)
  })

  it('data 非对象拒绝', () => {
    expect(validateEnvelope({ type: 'hello', data: 'str' })).toBe(false)
  })
})

describe('validateEventData（data 关键字段）', () => {
  it('task_status 缺 status 拒绝', () => {
    expect(validateEventData('task_status', { taskID: 't1' })).toBe(false)
    expect(validateEventData('task_status', { taskID: 't1', status: 'running' })).toBe(true)
  })

  it('hello 无必填字段恒通过', () => {
    expect(validateEventData('hello', {})).toBe(true)
  })

  it('所有事件类型在 EVENT_CONTRACT 有定义（与协议文档 10 种对齐）', () => {
    expect(Object.keys(EVENT_CONTRACT).sort()).toEqual([
      'agent_logs', 'alert_new', 'approval_status', 'device_offline', 'device_online',
      'hello', 'mw_template_changed', 'os_template_changed', 'schedule_status', 'task_status'
    ])
  })
})

describe('SSEClient 事件分发', () => {
  let client
  const onEvent = vi.fn()

  beforeEach(() => {
    onEvent.mockClear()
    // 构造可读流 mock：推入两段 SSE 文本后结束。
    // 注意两种帧形态（与后端 sse.go 一致）：
    //   - 握手帧：event: hello + data: {}（裸空对象）
    //   - 事件帧：event: <type> + data: 信封 {"type":...,"data":{...}}
    const chunks = [
      new TextEncoder().encode('event: hello\ndata: {}\n\n'),
      new TextEncoder().encode('event: task_status\ndata: {"type":"task_status","tenantID":"t1","data":{"taskID":"t1","status":"done"}}\n\n')
    ]
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader() {
          let i = 0
          return {
            async read() {
              if (i < chunks.length) return { done: false, value: chunks[i++] }
              return { done: true, value: undefined }
            }
          }
        }
      }
    })
    client = new SSEClient({ url: '/x', onEvent })
  })

  it('分发合法事件', async () => {
    client.start()
    // 等待异步读取完成
    await new Promise((r) => setTimeout(r, 50))
    expect(onEvent).toHaveBeenCalledTimes(2)
    expect(onEvent.mock.calls[0][0].type).toBe('hello')
    expect(onEvent.mock.calls[1][0].type).toBe('task_status')
    expect(onEvent.mock.calls[1][0].data.status).toBe('done')
    client.stop()
  })

  it('契约违规事件被丢弃不崩', async () => {
    const chunks = [
      new TextEncoder().encode('event: task_status\ndata: {"nofield":1}\n\n'),
      new TextEncoder().encode('event: unknown_type\ndata: {}\n\n')
    ]
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader() {
          let i = 0
          return {
            async read() {
              if (i < chunks.length) return { done: false, value: chunks[i++] }
              return { done: true, value: undefined }
            }
          }
        }
      }
    })
    const c2 = new SSEClient({ url: '/x', onEvent })
    c2.start()
    await new Promise((r) => setTimeout(r, 50))
    expect(onEvent).not.toHaveBeenCalled()
    expect(c2._validateCount).toBeGreaterThan(0)
    c2.stop()
  })
})
