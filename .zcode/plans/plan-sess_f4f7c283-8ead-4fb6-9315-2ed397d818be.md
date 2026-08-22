# P0 修复计划

## P0-1: 修复 HttpOnly Cookie 导致会话丢失

**文件**: `web/enterprise/src/stores/auth.js`

**问题分析**:  
第 77–78 行使用 `document.cookie` 检查 `opsmesh_at` 和 `opsmesh_rt` 是否存在，但后端设置 Cookie 时指定了 `HttpOnly: true`，JS 无法读取 HttpOnly Cookie，导致条件永远为 true，`fetchMe()` 永远跳过 API 调用，页面刷新后已登录用户被误判未登录。

**解决方案**:  
删除 Cookie 存在性检查，直接调用 `authApi.me()`，由后端 401 响应判断会话是否有效。

```js
async function fetchMe() {
  try {
    const me = await authApi.me()
    user.value = me
    return me
  } catch (e) {
    if (e.s === 401) user.value = null
    return null
  } finally {
    initialized.value = true
    _readyResolve()
  }
}
```

**测试验证**:  
- 更新 `auth.test.js` 中 `fetchMe` 的 mock 测试，移除 cookie 相关的模拟。
- 手动测试：登录后刷新页面，会话保持，不重定向到 /login。
- 使用 Playwright 的 `e2e-real` 真实后端场景验证。

---

## P0-2: 修复 SSE 帧分割边界残留

**文件**: `web/enterprise/src/api/sse.js`

**问题分析**:  
第 156 行在匹配到 `\r\n\r\n` 后，使用 `slice(idx + 2)` 只移除 2 字符，但实际分隔符长度为 4，残留 `\r\n` 进入下一轮 buffer，导致帧错位。

**解决方案**:  
使用 `String.match()` 获取完整匹配长度。

```js
let match
while ((match = buf.match(/\r?\n\r?\n/)) !== null) {
  const idx = match.index
  const frameEnd = buf.slice(0, idx)
  buf = buf.slice(idx + match[0].length)
  this._dispatchFrame(frameEnd)
}
```

**测试验证**:  
- 在 `sse.test.js` 中补充包含 `\r\n\r\n` 边界情况的测试用例。
- 手动验证：启动应用，打开 SSE 连接，观察事件解析正常。

---

## 风险与回滚

- **P0-1**: 低风险，删除的短路条件在生产中从未生效，改动后使功能恢复正常；若出现问题，可快速回滚到原代码。
- **P0-2**: 低风险，仅修正切片长度；若出现解析异常，可回滚到原实现（原实现虽有 bug 但大部分场景仍能工作）。

## 执行顺序

1. 先修复 `auth.js`，运行单元测试和 e2e-real 确认。
2. 再修复 `sse.js`，运行单元测试和手动验证。
3. 确保所有测试通过后提交。

---

请审核此计划，确认后我将开始执行。