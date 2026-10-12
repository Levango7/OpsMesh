package cutover

import "os"

// Router 切流路由裁决（TD-60 方案 C）。三个输入：
//   - D 的执行开关（**总闸**：为 false 时本机制完全不生效，行为与切流前逐字一致）；
//   - 名册（存量：哪些用户已经迁移到对侧微服务）；
//   - 各域的「增量开关」（新账号/新任务归哪侧持有，默认 false=控制面持有）。
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

// Enabled 总闸：对应域的代理开关为 true 时才可能路由。
// 与 svcproxy 的 Rule.IsActive 读**同一变量**（见各域 Enabled*）：D 开关关闭 = 该域机制整体不生效。
//
// 为何按域分而不是一个总闸：task 与 auth 的切流是**分批独立**进行的
// （施工图 §2.3：两侧分库、各有名册与增量开关），一个总闸会把两个
// 尚在双轨期的域捆在一起切。
func (rt *Router) Enabled() bool {
	return rt.AuthEnabled()
}

// AuthEnabled auth 域总闸：AUTH_SVC_PROXY_ENABLED=true 时才可能路由。
func (rt *Router) AuthEnabled() bool {
	return os.Getenv("AUTH_SVC_PROXY_ENABLED") == "true"
}

// TaskEnabled task 域总闸：TASK_SVC_PROXY_ENABLED=true 时才可能路由。
//
// 注意与 auth 域的**默认方向差异**：task-svc 的 HTTP 建号入口
// （`TASK_SVC_HTTP_GATEWAY_ENABLED`）默认 true，即服务侧从来就能建；
// 而 auth-svc 的 `AUTH_SVC_HTTP_ENABLED` 默认 false。
// ⇒ 「新任务归哪侧」此前**无任何开关表达**，默认状态是两侧都能建——
// `TaskOwnsNewTasks` 即为此而加（详见该函数注释）。
func (rt *Router) TaskEnabled() bool {
	return os.Getenv("TASK_SVC_PROXY_ENABLED") == "true"
}

// OwnsNewAccounts 新账号归 auth-svc 持有（AUTH_SVC_OWNS_NEW_ACCOUNTS=true）。
// 与名册正交：名册管存量、本开关管增量；两者同时打开才意味着「这一批彻底切完」。
func (rt *Router) OwnsNewAccounts() bool {
	return os.Getenv("AUTH_SVC_OWNS_NEW_ACCOUNTS") == "true"
}

// TaskOwnsNewTasks 新任务归 task-svc 持有（TASK_SVC_OWNS_NEW_TASKS=true）。
// 与 auth 域 OwnsNewAccounts 完全同构：名册管存量、本开关管增量，
// 两者同时打开才意味着「这一批彻底切完」。
//
// 为什么必须显式表达（而非沿用"两侧都能建"）：
// task 域的建任务入口在两侧都开着（控制面 `POST /api/v1/tasks` 与
// 批量下发；task-svc 的 HTTP 与 gRPC CreateTask），默认状态下
// 「新任务归哪侧」无声明。切流打开总闸后仍两侧可写 ⇒ 与 auth 域
// 曾有过的「账号建在被切走的一侧，用户登录时对侧查无此人」同形态：
// 任务建在错误一侧 ⇒ agent 拉不到、状态查不着，且**无任何报错**。
func (rt *Router) TaskOwnsNewTasks() bool {
	return os.Getenv("TASK_SVC_OWNS_NEW_TASKS") == "true"
}

// RouteLogin 登录按**请求体用户名**裁决（登录本就是「用用户名找账号」，信号天然在场）。
func (rt *Router) RouteLogin(username string) bool {
	if rt == nil || !rt.AuthEnabled() {
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
	return rt != nil && rt.AuthEnabled() && rt.OwnsNewAccounts()
}

// RouteTaskCreate 建任务**不做请求级路由**：与 RouteRegister 同理——新任务尚无归属，
// 归属由 `TASK_SVC_OWNS_NEW_TASKS` 全局表达。仅在开关为 true 且总闸开启时
// 由控制面拒绝本地建号并指引去 task-svc 侧（见 server_tasks.go 的接法）。
//
// 为什么不像 RouteLogin 那样按用户名裁：建任务时**调用方就是不信任的那一侧**——
// 请求发自需要被拒绝的那个入口本身，没有"已归属的用户名"可查。
func (rt *Router) RouteTaskCreate() bool {
	return rt != nil && rt.TaskEnabled() && rt.TaskOwnsNewTasks()
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
	if rt == nil || !rt.AuthEnabled() {
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
	if rt == nil || !rt.AuthEnabled() {
		return false
	}
	return !localOwns
}
