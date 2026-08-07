// tests/auth.test.js — auth store 测试
// 覆盖：token 管理（内存持有 + 旧 localStorage 清理）、登录/注册/登出、
//       handleAuthError（事件派发 + alert + 去重）、401 刷新重试链路。
// 说明：涉及 handleAuthError 的 describe 用 vi.resetModules() + 动态 import，
//       每个用例拿到全新模块实例，重置模块级 authErrorShown 去重状态。
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  getToken, setToken, isLoggedIn,
  apiAuthLogin, apiAuthRegister, apiAuthMe, apiLogout,
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
// token 管理
// ============================================================
describe('auth store — token 管理', () => {
  beforeEach(() => {
    setToken('');
    vi.stubGlobal('fetch', vi.fn());
  });
  afterEach(() => vi.unstubAllGlobals());

  it('setToken/getToken 正确存取，isLoggedIn 反映登录态', () => {
    setToken('abc123');
    expect(getToken()).toBe('abc123');
    expect(isLoggedIn()).toBe(true);
  });

  it('setToken 空串清除 token', () => {
    setToken('abc123');
    setToken('');
    expect(getToken()).toBe('');
    expect(isLoggedIn()).toBe(false);
  });

  it('setToken(null/undefined) 安全归一为空串', () => {
    setToken(null);
    expect(getToken()).toBe('');
    setToken(undefined);
    expect(getToken()).toBe('');
    expect(isLoggedIn()).toBe(false);
  });

  it('setToken 清理旧版本遗留的 localStorage token（XSS 迁移）', () => {
    // Node 25 全局 localStorage 可能是只读 stub，构造内存 Storage 确保可写
    const store = new Map();
    vi.stubGlobal('localStorage', {
      getItem: (k) => (store.has(k) ? store.get(k) : null),
      setItem: (k, v) => store.set(k, String(v)),
      removeItem: (k) => store.delete(k),
      clear: () => store.clear(),
      key: (i) => Array.from(store.keys())[i] ?? null,
      get length() { return store.size; },
    });
    localStorage.setItem('opsmesh-token', 'legacy');
    setToken('new');
    expect(localStorage.getItem('opsmesh-token')).toBeNull();
  });
});

// ============================================================
// 登录 / 注册
// ============================================================
describe('auth store — 登录/注册', () => {
  beforeEach(() => setToken(''));
  afterEach(() => vi.unstubAllGlobals());

  it('apiAuthLogin 成功(200+token)后存 token', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(200, { token: 'T1', user: { id: 1 } })));
    const r = await apiAuthLogin('alice', 'pw');
    expect(r.s).toBe(200);
    expect(r.j.token).toBe('T1');
    expect(getToken()).toBe('T1');
    expect(isLoggedIn()).toBe(true);
  });

  it('apiAuthLogin 失败(401)不存 token', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(401, { error: 'bad credentials' })));
    const r = await apiAuthLogin('alice', 'wrong');
    expect(r.s).toBe(401);
    expect(getToken()).toBe('');
    expect(isLoggedIn()).toBe(false);
  });

  it('apiAuthRegister 成功(201+token)后存 token', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(201, { token: 'T2', user: { id: 2 } })));
    const r = await apiAuthRegister('bob', 'pw', 'bob@x.com');
    expect(r.s).toBe(201);
    expect(getToken()).toBe('T2');
  });

  it('apiAuthRegister 待审批(201 无 token)不存 token', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(201, { message: 'pending approval' })));
    const r = await apiAuthRegister('bob', 'pw');
    expect(r.s).toBe(201);
    expect(r.j.message).toBe('pending approval');
    expect(getToken()).toBe('');
  });
});

// ============================================================
// 登出
// ============================================================
describe('auth store — 登出', () => {
  beforeEach(() => setToken('T1'));
  afterEach(() => vi.unstubAllGlobals());

  it('apiLogout 清内存 token 并 POST 后端 logout', async () => {
    const fetchSpy = vi.fn().mockResolvedValue(mockRes(200, {}));
    vi.stubGlobal('fetch', fetchSpy);
    await apiLogout();
    expect(getToken()).toBe('');
    expect(fetchSpy).toHaveBeenCalledWith('/api/v1/auth/logout', { method: 'POST' });
  });

  it('apiLogout 后端失败时静默 catch，token 仍被清除', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('network')));
    await expect(apiLogout()).resolves.toBeUndefined();
    expect(getToken()).toBe('');
  });
});

// ============================================================
// apiAuthMe（会话恢复探测）
// ============================================================
describe('auth store — apiAuthMe', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('200 返回 {s, j} 形态，不抛错', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(200, { user: { id: 1, name: 'alice' } })));
    const r = await apiAuthMe();
    expect(r.s).toBe(200);
    expect(r.j.user.name).toBe('alice');
  });

  it('401 返回 {s:401, j}，不抛错（由上层 main.js 决策跳转）', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockRes(401, { error: 'unauthorized' })));
    const r = await apiAuthMe();
    expect(r.s).toBe(401);
  });
});

// ============================================================
// handleAuthError — 401 提示
// 用动态 import 隔离 authErrorShown 模块级去重状态
// ============================================================
describe('auth store — handleAuthError (401 提示)', () => {
  let api, alertSpy, handler;
  beforeEach(async () => {
    vi.resetModules();
    vi.useFakeTimers();
    api = await import('../assets/api.js');
    alertSpy = vi.fn();
    vi.stubGlobal('alert', alertSpy);
    handler = vi.fn();
    document.addEventListener('opsmesh:auth-error', handler);
  });
  afterEach(() => {
    document.removeEventListener('opsmesh:auth-error', handler);
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('派发 opsmesh:auth-error 事件并 alert 兜底', () => {
    api.handleAuthError();
    expect(handler).toHaveBeenCalledTimes(1);
    expect(alertSpy).toHaveBeenCalledTimes(1);
  });

  it('5 秒内去重，不重复弹窗；5 秒后可再次提示', () => {
    api.handleAuthError();
    api.handleAuthError();
    api.handleAuthError();
    expect(alertSpy).toHaveBeenCalledTimes(1);
    expect(handler).toHaveBeenCalledTimes(1);
    // 推进 5 秒后重置，可再次提示
    vi.advanceTimersByTime(5000);
    api.handleAuthError();
    expect(alertSpy).toHaveBeenCalledTimes(2);
    expect(handler).toHaveBeenCalledTimes(2);
  });
});

// ============================================================
// 401 刷新重试链路
// 场景：已登录 token 过期 → 请求 401 → handleAuthError 提示 →
//       用户重新登录刷新 token → 同一接口重试成功
// 用动态 import 隔离 authErrorShown 状态
// ============================================================
describe('auth store — 401 刷新重试链路', () => {
  let api, alertSpy;
  beforeEach(async () => {
    vi.resetModules();
    vi.useFakeTimers();
    api = await import('../assets/api.js');
    api.setToken('expired-token');
    alertSpy = vi.fn();
    vi.stubGlobal('alert', alertSpy);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('401 → handleAuthError + 抛 Error(status=401)；重新登录后重试成功', async () => {
    // 第一次请求：token 过期，返回 401
    const fetch401 = vi.fn().mockResolvedValue(mockRes(401, { error: 'token expired' }));
    vi.stubGlobal('fetch', fetch401);
    await expect(api.getAgents()).rejects.toThrow('HTTP 401');
    // handleAuthError 已触发（alert 兜底 + 事件）
    expect(alertSpy).toHaveBeenCalled();

    // 模拟用户重新登录，刷新 token
    api.setToken('fresh-token');
    // 第二次请求：新 token 有效，返回 200 + 数据
    const fetch200 = vi.fn().mockResolvedValue(mockRes(200, [{ id: 'a1' }, { id: 'a2' }]));
    vi.stubGlobal('fetch', fetch200);
    const data = await api.getAgents();
    expect(data).toEqual([{ id: 'a1' }, { id: 'a2' }]);
    // 重试请求携带刷新后的新 token
    const callOpts = fetch200.mock.calls[0][1];
    expect(callOpts.headers['Authorization']).toBe('Bearer fresh-token');
  });
});
