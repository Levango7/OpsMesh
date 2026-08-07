// tests/api.test.js — request 拦截器测试
// 覆盖：Authorization 头注入、租户头兜底语义（X-Tenant-ID 由网关注入，前端透传不主动设置）、
//       返回形态归一、JSON 解析容错、authGet/authFetch/authPost 错误处理（401/500/网络异常）。
// 说明：authGet 401 会触发 handleAuthError（含模块级 authErrorShown 去重），
//       该 describe 用 vi.resetModules() + 动态 import 隔离状态。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  setToken, request, jsonBody, jsonMethod,
  authFetch, provisionDevice,
} from '../assets/api.js';

// ---------- 辅助：构造 fetch Response 形态 ----------
function mockRes(status, data) {
  return {
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(data),
  };
}

// ============================================================
// Authorization 头注入
// ============================================================
describe('request 拦截器 — Authorization 头注入', () => {
  afterEach(() => {
    setToken('');
    vi.unstubAllGlobals();
  });

  it('未登录时不注入 Authorization 头', async () => {
    setToken('');
    const spy = vi.fn().mockResolvedValue(mockRes(200, { ok: true }));
    vi.stubGlobal('fetch', spy);
    await request('/api/v1/x', {});
    const opts = spy.mock.calls[0][1];
    expect(opts.headers).toBeUndefined();
  });

  it('已登录时注入 Authorization: Bearer <token>', async () => {
    setToken('TOK');
    const spy = vi.fn().mockResolvedValue(mockRes(200, {}));
    vi.stubGlobal('fetch', spy);
    await request('/api/v1/x', {});
    const opts = spy.mock.calls[0][1];
    expect(opts.headers['Authorization']).toBe('Bearer TOK');
  });

  it('不覆盖调用方已传入的 headers（合并而非替换）', async () => {
    setToken('TOK');
    const spy = vi.fn().mockResolvedValue(mockRes(200, {}));
    vi.stubGlobal('fetch', spy);
    await request('/api/v1/x', { headers: { 'X-Custom': 'c' } });
    const opts = spy.mock.calls[0][1];
    expect(opts.headers['X-Custom']).toBe('c');
    expect(opts.headers['Authorization']).toBe('Bearer TOK');
  });
});

// ============================================================
// 租户头注入（兜底语义）
// 契约：X-Tenant-ID / X-User / X-User-Roles 由前置网关注入，前端不主动设置；
//       但若调用方显式传入（兜底场景），request 应原样透传，不被 Authorization 覆盖。
// ============================================================
describe('request 拦截器 — 租户头注入（兜底语义）', () => {
  afterEach(() => {
    setToken('');
    vi.unstubAllGlobals();
  });

  it('前端不主动设置 X-Tenant-ID（由网关处理）', async () => {
    setToken('TOK');
    const spy = vi.fn().mockResolvedValue(mockRes(200, {}));
    vi.stubGlobal('fetch', spy);
    await request('/api/v1/x', {});
    const opts = spy.mock.calls[0][1];
    expect(opts.headers).not.toHaveProperty('X-Tenant-ID');
  });

  it('调用方传入 X-Tenant-ID 时被原样透传（兜底），与 Authorization 共存', async () => {
    setToken('TOK');
    const spy = vi.fn().mockResolvedValue(mockRes(200, {}));
    vi.stubGlobal('fetch', spy);
    await request('/api/v1/x', { headers: { 'X-Tenant-ID': 'tenant-42' } });
    const opts = spy.mock.calls[0][1];
    expect(opts.headers['X-Tenant-ID']).toBe('tenant-42');
    expect(opts.headers['Authorization']).toBe('Bearer TOK');
  });

  it('jsonBody 不主动设置 X-Tenant-ID', () => {
    setToken('TOK');
    const o = jsonBody({ a: 1 });
    expect(o.headers['Content-Type']).toBe('application/json');
    expect(o.headers['Authorization']).toBe('Bearer TOK');
    expect(o.headers).not.toHaveProperty('X-Tenant-ID');
    expect(o.method).toBe('POST');
    expect(o.body).toBe(JSON.stringify({ a: 1 }));
  });
});

// ============================================================
// 返回形态归一 / JSON 解析容错
// ============================================================
describe('request 拦截器 — 返回形态归一', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('返回 {s, j} 形态', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(200, { data: 1 })));
    const r = await request('/api/v1/x', {});
    expect(r).toEqual({ s: 200, j: { data: 1 } });
  });

  it('JSON 解析失败时 j 为 null（不抛错）', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      status: 200, ok: true,
      json: () => Promise.reject(new SyntaxError('bad json')),
    }));
    const r = await request('/api/v1/x', {});
    expect(r.s).toBe(200);
    expect(r.j).toBeNull();
  });
});

// ============================================================
// jsonMethod — 指定 method + JSON + token
// ============================================================
describe('jsonMethod — 指定 method + JSON + token', () => {
  afterEach(() => setToken(''));

  it('PUT 请求携带 Content-Type + Authorization + body', () => {
    setToken('TOK');
    const o = jsonMethod('PUT', { name: 'updated' });
    expect(o.method).toBe('PUT');
    expect(o.headers['Content-Type']).toBe('application/json');
    expect(o.headers['Authorization']).toBe('Bearer TOK');
    expect(o.body).toBe(JSON.stringify({ name: 'updated' }));
  });

  it('未登录时不注入 Authorization', () => {
    setToken('');
    const o = jsonMethod('DELETE', { id: 1 });
    expect(o.headers['Content-Type']).toBe('application/json');
    expect(o.headers).not.toHaveProperty('Authorization');
  });
});

// ============================================================
// authGet（经 getAgents）— 错误处理
// 401 会触发 handleAuthError，用动态 import 隔离 authErrorShown 状态
// ============================================================
describe('authGet（经 getAgents）— 错误处理', () => {
  let api, alertSpy;
  beforeEach(async () => {
    vi.resetModules();
    vi.useFakeTimers();
    api = await import('../assets/api.js');
    api.setToken('TOK');
    alertSpy = vi.fn();
    vi.stubGlobal('alert', alertSpy);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('2xx 返回解析后的 json', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(200, [{ id: 'a1' }])));
    const data = await api.getAgents();
    expect(data).toEqual([{ id: 'a1' }]);
  });

  it('401 → handleAuthError + 抛 Error(status=401)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(401, { error: 'no auth' })));
    await expect(api.getAgents()).rejects.toMatchObject({ message: 'HTTP 401', status: 401 });
    expect(alertSpy).toHaveBeenCalled();
  });

  it('500 → 抛 Error(status=500)，不触发 handleAuthError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(500, { error: 'server' })));
    await expect(api.getAgents()).rejects.toMatchObject({ status: 500 });
    expect(alertSpy).not.toHaveBeenCalled();
  });

  it('网络异常 → reject 原始错误', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('fetch failed')));
    await expect(api.getAgents()).rejects.toThrow('fetch failed');
  });
});

// ============================================================
// authFetch — 返回 {s, j}
// ============================================================
describe('authFetch — 返回 {s, j}', () => {
  afterEach(() => {
    setToken('');
    vi.unstubAllGlobals();
  });

  it('携带 Authorization 并返回 {s, j}', async () => {
    setToken('TOK');
    const spy = vi.fn().mockResolvedValue(mockRes(200, { me: true }));
    vi.stubGlobal('fetch', spy);
    const r = await authFetch('/api/v1/me', 'GET');
    expect(r).toEqual({ s: 200, j: { me: true } });
    const opts = spy.mock.calls[0][1];
    expect(opts.headers['Authorization']).toBe('Bearer TOK');
    expect(opts.method).toBe('GET');
  });

  it('JSON 解析失败返回 {s, j:null}', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      status: 200, ok: true,
      json: () => Promise.reject(new SyntaxError('bad')),
    }));
    const r = await authFetch('/api/v1/me', 'GET');
    expect(r.s).toBe(200);
    expect(r.j).toBeNull();
  });
});

// ============================================================
// authPost（经 provisionDevice）— 返回 {s, j}
// ============================================================
describe('authPost（经 provisionDevice）— 返回 {s, j}', () => {
  afterEach(() => {
    setToken('');
    vi.unstubAllGlobals();
  });

  it('携带 Authorization(POST) 并返回 {s, j}', async () => {
    setToken('TOK');
    const spy = vi.fn().mockResolvedValue(mockRes(200, { ok: true }));
    vi.stubGlobal('fetch', spy);
    const r = await provisionDevice('dev-1');
    expect(r).toEqual({ s: 200, j: { ok: true } });
    const url = spy.mock.calls[0][0];
    const opts = spy.mock.calls[0][1];
    expect(url).toBe('/api/v1/devices/dev-1/provision');
    expect(opts.method).toBe('POST');
    expect(opts.headers['Authorization']).toBe('Bearer TOK');
  });
});
