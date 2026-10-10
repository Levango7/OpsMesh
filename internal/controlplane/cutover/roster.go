// Package cutover 实现 TD-60 方案 C（按用户群分批切换）的服务端裁决策略：
// 「已迁移用户名名册」+ 增量开关，供控制面 auth handler 决定每个请求归本地还是 auth-svc。
//
// 设计依据：`docs/td60-cutover-signal-design.md`（并行线，2026-10-10）——信号不是问题、**键是什么**才是：
// 两侧都按用户名定位用户，租户在 auth-svc 侧无落地面，故名册的键是**用户名**（存量）+ 全局开关（增量）。
// 本包只做**裁决**（纯逻辑，可单测），转发与接线在 controlplane 的 auth_cutover.go。
//
// 名册的定位（照搬设计 §5）：**不是路由表，是「迁移工单的机器可读形态」**——每一条背后
// 对应一次已完成的账号迁移（对侧建号 + 置随机口令 + 首登强制改密）。因此：
//   - 保留账号（两侧都 seed 的 `admin`）**永不进名册**：两侧同名并存时，一次口令提交会被
//     两个不同账号校验，构成跨库凭证混淆（设计 §3 的 R1 风险）——本包在装载期直接拒绝；
//   - 名册只描述「已确认存在于对侧」的用户：装载期对本侧存在性做提示性告警（见 controlplane
//     的启动自检），但不阻断（迁移完成后本侧账号是否清理属运维口径）。
package cutover

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
)

// ReservedUsernames 不得写入名册的用户名：两侧 seed 的同名账号。
// `admin` 是控制面（memory.go / sql_rbac.go seed）与 auth-svc 共同的种子用户名，
// 两份口令可不同 ⇒ 写入名册即制造「同一口令提交被两个账号校验」的跨库混淆面。
var ReservedUsernames = []string{"admin"}

// Roster 已迁移用户名名册（并发安全：读走原子快照，重载整体替换）。
type Roster struct {
	snap   atomic.Value // map[string]struct{}
	source string       // 人类可读来源，用于日志/自检
	file   string       // 文件来源（非空则可重载）
	inline string       // 内联来源（重载无效——env 不热更，重载返回当前值）
}

// LoadRoster 装载名册：file 优先；file 与 inline 同时给出视为**非法配置**（歧义即 fail-fast，
// 与 OPSMESH_SERVICE_PROXY「取值非法时整体拒绝」的既有哲学一致）。两者都空 ⇒ 空名册（合法）。
func LoadRoster(inline, file string) (*Roster, error) {
	if strings.TrimSpace(inline) != "" && strings.TrimSpace(file) != "" {
		return nil, fmt.Errorf("名册来源歧义：AUTH_CUTOVER_ROSTER 与 AUTH_CUTOVER_ROSTER_FILE 不能同时设置")
	}
	r := &Roster{inline: inline, file: strings.TrimSpace(file)}
	if r.file != "" {
		r.source = "file:" + r.file
	} else if strings.TrimSpace(inline) != "" {
		r.source = "env:AUTH_CUTOVER_ROSTER"
	} else {
		r.source = "empty"
	}
	if _, err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// snapshot 取当前名册快照（未装载/类型异常时为空）。
func (r *Roster) snapshot() map[string]struct{} {
	v := r.snap.Load()
	if v == nil {
		return nil
	}
	set, ok := v.(map[string]struct{})
	if !ok {
		return nil
	}
	return set
}

// Match 用户名是否已迁移（在名册内）。
func (r *Roster) Match(username string) bool {
	if r == nil {
		return false
	}
	set := r.snapshot()
	if len(set) == 0 {
		return false
	}
	_, ok := set[strings.TrimSpace(username)]
	return ok
}

// Size 名册条目数（供日志/指标判断切流进度）。
func (r *Roster) Size() int {
	if r == nil {
		return 0
	}
	return len(r.snapshot())
}

// Source 名册来源（日志用）。
func (r *Roster) Source() string {
	if r == nil {
		return "empty"
	}
	return r.source
}

// Entries 返回名册快照（升序，供启动自检逐条核对）。
func (r *Roster) Entries() []string {
	set := r.snapshot()
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Reload 重读来源并原子替换。失败时**保留上一份可用名册**（宁可继续用旧名册，也不静默清空——
// 清空会让已迁移用户回落到本地而登录失败，是用户可见事故）；返回条目数与错误。
// 内联来源（env）不支持热更：返回当前值，无副作用。
func (r *Roster) Reload() (int, error) {
	if r.file == "" {
		if r.snap.Load() == nil {
			r.snap.Store(parseRoster(r.inline))
		}
		return r.Size(), nil
	}
	b, err := os.ReadFile(r.file)
	if err != nil {
		if r.snap.Load() != nil {
			return r.Size(), fmt.Errorf("重读名册文件失败（保留上一份名册，%d 条）: %w", r.Size(), err)
		}
		return 0, fmt.Errorf("读取名册文件失败: %w", err)
	}
	set, err := parseRosterStrict(string(b))
	if err != nil {
		if r.snap.Load() != nil {
			return r.Size(), fmt.Errorf("名册文件非法（保留上一份名册，%d 条）: %w", r.Size(), err)
		}
		return 0, err
	}
	r.snap.Store(set)
	return len(set), nil
}

// parseRoster 宽松解析内联清单（逗号/换行/空白分隔，`#` 起注释）。
func parseRoster(inline string) map[string]struct{} {
	set, err := parseRosterStrict(inline)
	if err != nil || set == nil {
		return map[string]struct{}{}
	}
	return set
}

// parseRosterStrict 解析名册并做校验：格式合法、非保留账号、无重复冲突。
// 非法即返回错误——名册是裁决输入，宁可不启动也不带着错的裁决表跑。
// 注释按**行**剥离（`#` 到行尾）后再按逗号/空白分词：先分词会让 `# 迁移工单 WO-1` 的
// 后半段被当成条目（首版即此错，被用例当场抓住）。
func parseRosterStrict(src string) (map[string]struct{}, error) {
	set := map[string]struct{}{}
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		for _, raw := range strings.FieldsFunc(line, func(c rune) bool {
			return c == ',' || c == '\r' || c == '\t' || c == ' '
		}) {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if !validUsername(name) {
				return nil, fmt.Errorf("名册条目 %q 不是合法用户名（允许字母/数字/下划线/连字符/点，3-64 位，首字符字母或数字）", name)
			}
			for _, reserved := range ReservedUsernames {
				if strings.EqualFold(name, reserved) {
					return nil, fmt.Errorf("名册条目 %q 是保留账号（两侧 seed 同名，写入即构成跨库凭证混淆面）——它永不进名册", name)
				}
			}
			set[name] = struct{}{}
		}
	}
	return set, nil
}

// validUsername 与控制面注册口径一致的保守校验（与 store 侧注册校验同形：
// 只在此做「明显不可能是用户名」的拦截，真实存在性由启动自检提示）。
func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' || c == '-' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !ok {
			return false
		}
		if i == 0 && (c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
