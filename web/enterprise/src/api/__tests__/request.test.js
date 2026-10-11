// request.js 的登录页 URL 构造：登录态失效必须带 reason（登录页据此给出原因），
// 其余情况不带（普通进入登录页不该出现"原因"提示）。
// 断言 URL 构造而不是 window.location.href：jsdom 不实现真实导航，
// 对 location.href 的断言只能证明"赋了值"，证不了"赋对了什么"。
import { describe, it, expect } from 'vitest'
import { loginURL } from '@/api/request'

describe('loginURL', () => {
  it('不带 reason 时就是普通登录页', () => {
    expect(loginURL()).toBe('/login')
  })

  it('带 reason 时拼成查询串', () => {
    expect(loginURL('session_expired')).toBe('/login?reason=session_expired')
  })

  it('reason 做 URL 编码（异常值不破坏查询串）', () => {
    expect(loginURL('a b&c=d')).toBe('/login?reason=a%20b%26c%3Dd')
  })
})
