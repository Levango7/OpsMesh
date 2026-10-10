package cutover

import "os"

// Router 切流路由裁决（TD-60 方案 C）。三个输入：
//   - D 的执行开关 AUTH_SVC_PROXY_ENABLED（**总闸**：为 false 时本机制完全不生效，行为与切流前逐字一致）；
//   - 名册（存量：哪些用户已经迁移到 auth-svc）；
//   - AUTH_SVC_OWNS_NEW_ACCOUNTS（增量：新注册账号归哪侧持有，默认 false=控制面持有）。
//
// 全部裁决都是**纯函数**（除读环境变量），便于单测把每条规则钉住；转发动作在 controlplane 侧。
type Router struct {
	roster *Roster
}

// NewRouter 构造裁决器（roster 可为 nil ⇒ 空名册）。
func NewRouter(roster *Roster) *Router {
	return &Router{roster: roster}
}

// Roster 暴露名册（供启动自检与日志）。
func (rt *Router) Roster() *Roster {
	if rt == nil {
		return nil
	}
	return rt.roster
}

// Enabled 总闸：AUTH_SVC_PROXY_ENABLED=true 时才可能路由。
// 与 svcproxy 的 Rule.IsActive 读同一变量：D 开关关闭 = auth 域机制整体不生效。
func (rt *Router) Enabled() bool {
	return os.Getenv("AUTH_SVC_PROXY_ENABLED") == "true"
}

// OwnsNewAccounts 新账号归 auth-svc 持有（AUTH_SVC_OWNS_NEW_ACCOUNTS=true）。
// 与名册正交：名册管存量、本开关管增量；两者同时打开才意味着「这一批彻底切完」。
func (rt *Router) OwnsNewAccounts() bool {
	return os.Getenv("AUTH_SVC_OWNS_NEW_ACCOUNTS") == "true"
}

// RouteLogin 登录按**请求体用户名**裁决（登录本就是「用用户名找账号」，信号天然在场）。
func (rt *Router) RouteLogin(username string) bool {
	if rt == nil || !rt.Enabled() {
		return false
	}
	return rt.roster.Match(username)
}

// RouteUser me/logout/改密（已登录态）按 **JWT 的 sub/username** 裁决：
// 名册命中即去 auth-svc——登出必须在签发侧吊销，me 必须由对手侧回答（否则两侧各答一半）。
func (rt *Router) RouteUser(username string) bool {
	return rt.RouteLogin(username)
}

// RouteRegister 注册**不做请求级路由**：新账号尚无归属，归属是全局决策（设计 §4）。
func (rt *Router) RouteRegister() bool {
	return rt != nil && rt.Enabled() && rt.OwnsNewAccounts()
}

// RouteRefresh 刷新令牌按**归属侧**裁决（rt 是签发侧的私有状态，只有签发侧能吊销/旋转）。
//
// 三种情形：
//   - localOwns=false：本地库无此 rt ⇒ 属对侧（或已失效的未知 token）⇒ 去 auth-svc；
//     后者在 auth-svc 侧同样是 401，用户可见结果与本地一致（代价是一次跨进程往返）。
//   - localOwns=true && 该 rt 的用户已在名册：**仍送 auth-svc** ⇒ auth-svc 不认识这份本地会话
//     ⇒ 该用户被强制重登一次（这正是切流的语义：名册生效后旧会话不再被本地续命，
//     否则用户能靠 rt 轮换永远留在本地侧，批次"切完了"就不成立）。
//   - localOwns=true && 用户不在名册：本地处理（与切流前逐字一致）。
func (rt *Router) RouteRefresh(localOwns bool, localUserInRoster bool) bool {
	if rt == nil || !rt.Enabled() {
		return false
	}
	if !localOwns {
		return true
	}
	return localUserInRoster
}

// RouteTokenOwner 归属侧裁决的通用形态（change-password 的一次性改密令牌同此形态：
// 令牌是**不透明串**，只存在于签发侧的会话存储里，消费失败即说明不属本地）。
func (rt *Router) RouteTokenOwner(localOwns bool) bool {
	if rt == nil || !rt.Enabled() {
		return false
	}
	return !localOwns
}
