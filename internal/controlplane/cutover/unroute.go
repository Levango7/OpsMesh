// unroute.go — `opsmesh cutover-unroute`：**回退**一个/一批用户（TD-60 方案 C）。
//
// 与 cutover-migrate 严格对称：迁移是"对侧建号 → 写名册"，回退是"摘名册 → 对侧吊销会话"。
//
// 为什么回退必须顺手吊销对侧会话（演练报告 F2，docs/td60-batch0-drill-2026-10-12.md）：
// 名册只决定**路由**。用户被摘除后，他在对侧已建立的会话仍被对侧认（refresh 返回 200），
// 而 /me 之类已按名册回本侧（401）⇒ 用户视角"刷新说还活着、一干正事就报错"的半死态。
// 只摘名册（例如手改文件）能回退**登录**，但消不掉这份半死态——所以回退的完整形态是两步。
//
// 为什么不做成"控制面在名册收缩时自动调对侧吊销"（更自动的方案，曾被考虑）：
// 那需要控制面**常驻持有对侧管理员口令**（对侧吊销端点在管理面上）。为一个低频运维动作
// 给一个常驻服务增加"对侧管理员"这一档长期凭据，是不划算的权限扩张；短命 job 持有凭据
// 是仓里既有形态（cutover-migrate 同此）。代价写明：**手改名册不会吊销会话**，
// 回退请走本命令（runbook §3 已同步）。
//
// 顺序：**先摘名册、后吊销**。理由：摘名册是本地原子写、不会失败，且它就是"回退生效"本身
// （控制面 ≤30s 热加载后该用户回本侧）；吊销是对侧清理，失败也不该阻止回退——失败如实记账，
// 用户下次操作会自愈（前端 401→refresh→重试再 401→跳登录）。反过来（先吊销后摘名册）会出现
// "会话已死但仍被路由到对侧"的窗口，用户连本侧都登不回来，更糟。
package cutover

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UnrouteOptions 回退参数。
type UnrouteOptions struct {
	Users         []string
	RosterFile    string
	AuthSvcURL    string
	AdminUser     string
	AdminPassword string
	DryRun        bool
}

// UnrouteFailure 回退过程中对侧吊销失败的用户（名册已摘除，仅会话未清）。
type UnrouteFailure struct {
	Username string
	Reason   string
}

// UnrouteResult job 结果。
type UnrouteResult struct {
	Removed      []string         // 实际从名册摘除的
	NotInRoster  []string         // 本来就不在名册里（幂等：无需动作）
	MissingLocal []string         // 本侧已无该账号（**回退后两侧都登不进**，必须人工处置——告警不阻断）
	Revoked      []string         // 对侧会话已吊销
	RevokeFailed []UnrouteFailure // 名册已摘但会话未吊销（可重跑本命令补吊销）
	DryRun       bool
}

// RunUnroute 执行回退。返回结果与错误（错误只用于"整体不可运行"：名册不可读/管理员登录失败等）。
//
// lookup 用于**预检本侧账号是否还在**（可为 nil = 跳过预检）。为什么值得一查：回退后该用户
// 只能在本侧登录，若本侧账号已被清理，摘除名册等于把他两侧的登录面都断了——这种形态必须
// 在动手前让人看见（告警不阻断：是否继续由运维判断）。
func RunUnroute(opts UnrouteOptions, now time.Time, lookup LocalLookup) (*UnrouteResult, error) {
	res := &UnrouteResult{DryRun: opts.DryRun}
	if strings.TrimSpace(opts.RosterFile) == "" {
		return nil, fmt.Errorf("名册文件未指定（--roster-file 或 env AUTH_CUTOVER_ROSTER_FILE）")
	}
	users := make([]string, 0, len(opts.Users))
	for _, u := range opts.Users {
		if u = strings.TrimSpace(u); u != "" {
			users = append(users, u)
		}
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("--users 必填（回退清单即回退范围；不做全量）")
	}

	// 读现有名册（不带着坏名册做减法——拼错的条目应当被人看见，而不是被静默摘掉）。
	old := ""
	if b, err := os.ReadFile(opts.RosterFile); err == nil {
		old = string(b)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读名册失败: %w", err)
	}
	existing, err := parseRosterStrict(old)
	if err != nil {
		return nil, fmt.Errorf("现有名册非法（不带着坏名册做回退）: %w", err)
	}
	for _, u := range users {
		if _, ok := existing[u]; ok {
			res.Removed = append(res.Removed, u)
		} else {
			res.NotInRoster = append(res.NotInRoster, u)
		}
	}
	if lookup != nil {
		for _, u := range res.Removed {
			if _, ok := lookup(u); !ok {
				res.MissingLocal = append(res.MissingLocal, u)
			}
		}
	}
	if opts.DryRun {
		// dry-run 零副作用：连对侧都不登录（不产生会话、不改任何状态）。
		return res, nil
	}
	if len(res.Removed) == 0 {
		return res, nil // 幂等：都不在名册里，无事可做
	}

	// 1) 先摘名册（本地、原子、不会失败）。失败即整体失败——名册没摘成，回退就没发生。
	if _, err := removeFromRoster(opts.RosterFile, res.Removed, now); err != nil {
		return res, fmt.Errorf("写名册失败（回退未生效，可修复后重跑）: %w", err)
	}

	// 2) 再吊销对侧会话。失败只记账，不回滚名册（理由见文件头）。
	client, err := newAuthSvcClient(opts.AuthSvcURL)
	if err != nil {
		// 对侧地址不可用：名册已摘（回退已生效），会话清理只能事后再跑。
		for _, u := range res.Removed {
			res.RevokeFailed = append(res.RevokeFailed, UnrouteFailure{Username: u, Reason: err.Error()})
		}
		return res, nil
	}
	if err := client.adminLogin(opts.AdminUser, opts.AdminPassword); err != nil {
		for _, u := range res.Removed {
			res.RevokeFailed = append(res.RevokeFailed, UnrouteFailure{Username: u, Reason: err.Error()})
		}
		return res, nil
	}
	farUsers, err := client.listUsers()
	if err != nil {
		for _, u := range res.Removed {
			res.RevokeFailed = append(res.RevokeFailed, UnrouteFailure{Username: u, Reason: err.Error()})
		}
		return res, nil
	}
	for _, u := range res.Removed {
		fu, ok := farUsers[u]
		if !ok {
			// 对侧没有这个账号：无会话可吊销（可能从未迁移成功、或对侧已清理）——如实记一条，
			// 不当失败（回退本身已生效），但要让人看见"这一条没做过对侧清理"。
			res.RevokeFailed = append(res.RevokeFailed, UnrouteFailure{Username: u, Reason: "对侧无此账号（无会话可吊销）"})
			continue
		}
		n, err := client.revokeSessions(fu.ID)
		if err != nil {
			res.RevokeFailed = append(res.RevokeFailed, UnrouteFailure{Username: u, Reason: err.Error()})
			continue
		}
		_ = n // 条数进日志更细，结果里只记"已吊销"
		res.Revoked = append(res.Revoked, u)
	}
	return res, nil
}

// removeFromRoster 从名册文件摘除 names（temp+rename 原子替换）。
// **保留全部既有注释**（名册的 provenance 历史可审计），在末尾追加一行本次来源注释；
// 返回实际摘除的条数。与 appendToRoster 同风格。
func removeFromRoster(path string, names []string, now time.Time) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	old := string(b)
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	var kept []string // 保留的行：注释行原样、条目行按是否被摘除过滤
	removed := 0
	for _, line := range strings.Split(strings.TrimRight(old, "\r\n"), "\n") {
		code := line
		if i := strings.Index(code, "#"); i >= 0 {
			code = code[:i]
		}
		fields := strings.FieldsFunc(code, func(c rune) bool {
			return c == ',' || c == '\r' || c == '\t' || c == ' '
		})
		// 注释行（或空行）原样保留
		if len(fields) == 0 {
			kept = append(kept, line)
			continue
		}
		// 条目行：整行只有一个名字时才能整体摘除；多名字行按名字逐个判断后重建
		hit := false
		for _, f := range fields {
			if drop[strings.TrimSpace(f)] {
				hit = true
			}
		}
		if !hit {
			kept = append(kept, line)
			continue
		}
		var rest []string
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if drop[f] {
				removed++
				continue
			}
			rest = append(rest, f)
		}
		if len(rest) > 0 {
			kept = append(kept, strings.Join(rest, ","))
		}
	}
	if removed == 0 {
		return 0, nil
	}
	var out strings.Builder
	out.WriteString(strings.TrimRight(strings.Join(kept, "\n"), "\r\n"))
	out.WriteString("\n\n")
	fmt.Fprintf(&out, "# cutover-unroute %s removed=%d\n", now.UTC().Format(time.RFC3339), removed)

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".roster-*.tmp")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(out.String()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return 0, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return 0, err
	}
	return removed, nil
}

// revokeSessions 调对侧管理端点吊销该用户全部会话，返回吊销的刷新令牌条数。
func (c *authSvcClient) revokeSessions(userID string) (int, error) {
	var resp struct {
		Status               string `json:"status"`
		RefreshTokensDeleted int    `json:"refreshTokensDeleted"`
		Error                string `json:"error"`
	}
	code, _, err := c.doJSON(http.MethodPost, "/api/v1/users/"+userID+"/revoke-sessions", nil, &resp)
	if err != nil {
		return 0, err
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("POST /api/v1/users/%s/revoke-sessions => %d: %s", userID, code, orDefault(resp.Error, "unexpected status"))
	}
	return resp.RefreshTokensDeleted, nil
}
