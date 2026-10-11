// migrate.go — TD-60 方案 C 的「搬号 job」：把本批用户从控制面迁到 auth-svc，
// 验证通过后写名册（名册 = 迁移工单的机器可读形态）。
//
// 决策单（docs/td60-cutover-decisions.md）问题 2 的推荐项：**一次性 job 执行、名单由系统维护**，
// 理由是「搬完才写名册」的原子序——否则会出现「名册写了但号没搬 ⇒ 用户被锁死」。
// 本 job 的原子序是**按用户**：每个用户只有在对侧建号成功、且用一次性口令**真登一次成功**之后，
// 才会写进名册；失败的用户不写名册（留在本地登录面，行为不变）⇒ 永不产生"名册有、对侧无"的锁死态。
//
// 口令交接（对侧 API 的现实约束，见协调板移交）：
//
//	auth-svc 的注册/管理建号/更新用户**都不接受 mustChangePassword**（proto 与 HTTP 均无此字段）
//	⇒「首登强制改密」暂无对侧入口。本 job 采用**一次性随机口令交接**：生成强随机口令建号，
//	用它登录验证通过后，把 口令清单 交给运维（stderr 或 --credentials-out 文件，0600），
//	由运维经安全渠道交给用户本人。对侧补上 mustChangePassword 入口后，本 job 可直接加
//	--force-change 透传（结构已留好：createUser 的 body 是 map）。
//
// 幂等：对侧已存在且 active 的用户跳过建号、只补名册（重复跑收敛到同一结果）；
// 对侧存在但非 active 的用户**不自动激活**（那是人工决策），报失败并留在本地。
package cutover

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LocalUser 本地侧迁移所需的用户事实（由调用方从 store 适配进来，本包不依赖 store）。
type LocalUser struct {
	Email   string
	RoleIDs []string
}

// LocalLookup 按用户名查本地用户（存在性即迁移前置之一：名册只描述真实存在的账号）。
type LocalLookup func(username string) (LocalUser, bool)

// MigrateOptions job 入参。
type MigrateOptions struct {
	Users         []string // 本批要迁的用户名（显式清单 = 批次；决策单 Q1：文件名册 + 流水线变更）
	AuthSvcURL    string   // 对侧 HTTP 网关地址（与代理共用 AUTH_SVC_URL）
	AdminUser     string   // 对侧管理员账号（须持 user:write；默认 admin）
	AdminPassword string   // 对侧管理员口令
	RosterFile    string   // 名册文件路径（job 的唯一产物之一；控制面 30s 热加载）
	DryRun        bool     // 只做只读核验（本地存在性 + 对侧现状 + 名册现状），不建号不写名册
}

// MigratedUser 迁移成功的用户（含一次性口令，供运维交接）。
type MigratedUser struct {
	Username        string
	OneTimePassword string // 仅新建用户有；对侧已存在的（Skipped）为空
	FarSideID       string
	DroppedRoles    []string // 本地有、对侧没有的角色 ID（权限仍按本地判，仅 /me 展示受影响）
}

// MigrationFailure 迁移失败的用户与原因（不写名册 ⇒ 该用户留在本地登录面）。
type MigrationFailure struct {
	Username string
	Reason   string
}

// MigrateResult job 结果。
type MigrateResult struct {
	Created []MigratedUser     // 本批新建（对侧建号 + 登录验证通过）
	Adopted []MigratedUser     // 对侧已 active（幂等收编：不建号，只补名册）
	Skipped []string           // 已在名册中（无需任何动作）
	Failed  []MigrationFailure // 失败：不写名册
	Added   int                // 本次实际写进名册的条数
}

// RunMigration 执行搬号。返回结果与错误（错误只用于"整体不可运行"：对侧不可达、管理员登录失败等；
// 单用户问题记入 Failed，不阻断其他用户）。
func RunMigration(opts MigrateOptions, lookup LocalLookup) (*MigrateResult, error) {
	// 校验顺序：先验输入（不联网就能发现的错最先报），再验连通性与写面。
	if len(opts.Users) == 0 {
		return nil, fmt.Errorf("--users 为空：本批要迁的用户名清单就是批次（决策单 Q2），不做全量迁移")
	}
	// 用户名预校验：格式 + 保留账号（保留账号两侧 seed 同名，写名册即跨库凭证混淆面）。
	seen := map[string]bool{}
	for _, raw := range opts.Users {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !validUsername(name) {
			return nil, fmt.Errorf("用户名 %q 不是合法用户名（3-64 位，字母/数字/下划线/连字符/点，首字符字母或数字）", name)
		}
		for _, reserved := range ReservedUsernames {
			if strings.EqualFold(name, reserved) {
				return nil, fmt.Errorf("用户名 %q 是保留账号（两侧 seed 同名 ⇒ 跨库凭证混淆面），永不迁移", name)
			}
		}
		seen[name] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("--users 解析后为空")
	}
	if strings.TrimSpace(opts.AuthSvcURL) == "" {
		return nil, fmt.Errorf("AUTH_SVC_URL 未设置：搬号需要访问对侧 HTTP 网关（建号/审批/登录验证）")
	}
	if strings.TrimSpace(opts.RosterFile) == "" && !opts.DryRun {
		return nil, fmt.Errorf("AUTH_CUTOVER_ROSTER_FILE 未设置：job 只写文件名册（内联 env 名册不可写、不可 diff）")
	}

	res := &MigrateResult{}
	// 名册现状（只读）：已在名册的用户直接跳过。**文件尚不存在是合法状态**（首批迁移就是从零开始），
	// 只有"存在但非法"才拒绝——不带着坏名册跑迁移。
	rosterSet := map[string]struct{}{}
	if strings.TrimSpace(opts.RosterFile) != "" {
		if b, err := os.ReadFile(opts.RosterFile); err == nil {
			set, perr := parseRosterStrict(string(b))
			if perr != nil {
				return nil, fmt.Errorf("现有名册非法（不带着坏名册跑迁移）: %w", perr)
			}
			for n := range set {
				rosterSet[n] = struct{}{}
			}
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("读取名册失败: %w", err)
		}
	}

	// 对侧客户端：管理员会话（cookie jar）。
	cli, err := newAuthSvcClient(opts.AuthSvcURL)
	if err != nil {
		return nil, err
	}
	if err := cli.adminLogin(opts.AdminUser, opts.AdminPassword); err != nil {
		return nil, err
	}
	farUsers, err := cli.listUsers()
	if err != nil {
		return nil, fmt.Errorf("拉取对侧用户清单失败: %w", err)
	}
	farRoles, err := cli.listRoles()
	if err != nil {
		return nil, fmt.Errorf("拉取对侧角色清单失败: %w", err)
	}

	for name := range seen {
		if _, ok := rosterSet[name]; ok {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		local, ok := lookup(name)
		if !ok {
			res.Failed = append(res.Failed, MigrationFailure{Username: name, Reason: "本侧不存在该用户（名册只描述真实存在的账号；拼写错误？）"})
			continue
		}
		far, exists := farUsers[name]
		if exists && far.Status != "active" {
			res.Failed = append(res.Failed, MigrationFailure{Username: name, Reason: "对侧已存在但状态=" + far.Status + "（是否激活属人工决策，job 不自动激活）"})
			continue
		}
		// 本地角色映射：只透传对侧存在的角色；缺的记入 DroppedRoles（控制面权限仍按本地判，
		// 影响的只是对侧 /me 的展示面）。
		keep := make([]string, 0, len(local.RoleIDs))
		var dropped []string
		for _, rid := range local.RoleIDs {
			if rid != "" && farRoles[rid] {
				keep = append(keep, rid)
			} else if rid != "" {
				dropped = append(dropped, rid)
			}
		}
		if exists { // 对侧已 active：幂等收编，不建号、无一次性口令。
			res.Adopted = append(res.Adopted, MigratedUser{Username: name, FarSideID: far.ID, DroppedRoles: dropped})
			continue
		}
		if opts.DryRun {
			// 干跑：到此为止（上面的只读核验已足够给出计划），不建号、不验证、不写名册。
			res.Created = append(res.Created, MigratedUser{Username: name, DroppedRoles: dropped})
			continue
		}
		oneTime, err := generateOneTimePassword()
		if err != nil {
			res.Failed = append(res.Failed, MigrationFailure{Username: name, Reason: "生成一次性口令失败: " + err.Error()})
			continue
		}
		if err := cli.createUser(name, oneTime, local.Email, keep); err != nil {
			res.Failed = append(res.Failed, MigrationFailure{Username: name, Reason: "对侧建号失败: " + err.Error()})
			continue
		}
		// 登录即验证：用一次性口令真登一次对侧，通不过就不写名册（保证「名册有 ⇒ 对侧可登录」）。
		if err := cli.loginVerify(name, oneTime); err != nil {
			res.Failed = append(res.Failed, MigrationFailure{Username: name, Reason: "登录验证失败（不写名册，该用户留在本地）: " + err.Error()})
			continue
		}
		// 重新拉一次清单拿对侧 ID（建号响应不含 id，对侧 API 现状）。
		if after, err := cli.listUsers(); err == nil {
			if u, ok := after[name]; ok {
				far.ID = u.ID
			}
		}
		res.Created = append(res.Created, MigratedUser{Username: name, OneTimePassword: oneTime, FarSideID: far.ID, DroppedRoles: dropped})
	}

	if opts.DryRun {
		return res, nil
	}
	// 写名册：只写 Created + Adopted（每个都已在对侧可登录/已 active）。逐条原子 ⇒ 失败用户天然留在本地。
	names := make([]string, 0, len(res.Created)+len(res.Adopted))
	for _, u := range res.Created {
		names = append(names, u.Username)
	}
	for _, u := range res.Adopted {
		names = append(names, u.Username)
	}
	added, err := appendToRoster(opts.RosterFile, names, time.Now())
	if err != nil {
		return res, fmt.Errorf("写名册失败（用户已建好，可修复后重跑——幂等会只补名册）: %w", err)
	}
	res.Added = added
	return res, nil
}

// appendToRoster 把 names 里尚不在名册中的条目追加进文件（temp+rename 原子替换，保留既有内容与注释）。
// 每批附一行来源注释（谁、何时、迁了多少）——名册是「迁移工单的机器可读形态」，可 diff、可审计（决策单 Q1）。
func appendToRoster(path string, names []string, now time.Time) (int, error) {
	old := ""
	if b, err := os.ReadFile(path); err == nil {
		old = string(b)
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	existing, err := parseRosterStrict(old)
	if err != nil {
		return 0, fmt.Errorf("现有名册非法（不带着坏名册追加）: %w", err)
	}
	var addLines []string
	added := 0
	for _, n := range names {
		if _, ok := existing[n]; ok {
			continue
		}
		existing[n] = struct{}{}
		addLines = append(addLines, n)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	var b strings.Builder
	if old != "" {
		b.WriteString(strings.TrimRight(old, "\r\n"))
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "# cutover-migrate %s added=%d\n", now.UTC().Format(time.RFC3339), added)
	for _, n := range addLines {
		b.WriteString(n + "\n")
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".roster-*.tmp")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
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
	return added, nil
}

// ---------- 对侧 HTTP 客户端 ----------

type authSvcClient struct {
	base string
	hc   *http.Client // 管理员会话（cookie jar）
}

func newAuthSvcClient(base string) (*authSvcClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &authSvcClient{
		base: strings.TrimRight(base, "/"),
		hc:   &http.Client{Timeout: 15 * time.Second, Jar: jar},
	}, nil
}

func (c *authSvcClient) doJSON(method, path string, body any, out any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, data, fmt.Errorf("解析响应 JSON 失败: %w", err)
		}
	}
	return resp.StatusCode, data, nil
}

// adminLogin 对侧管理员登录。首登强制改密态（返回 changePasswordToken）直接报错并给出处置指引——
// 用管理员新口令跑一次人工登录完成改密，再回来跑迁移（job 不能替人发明管理员新口令）。
func (c *authSvcClient) adminLogin(user, password string) error {
	if strings.TrimSpace(user) == "" {
		user = "admin"
	}
	if strings.TrimSpace(password) == "" {
		return fmt.Errorf("对侧管理员口令未提供（--auth-svc-admin-password 或 AUTH_SVC_MIGRATE_ADMIN_PASSWORD）")
	}
	var resp struct {
		MustChangePassword  bool   `json:"mustChangePassword"`
		ChangePasswordToken string `json:"changePasswordToken"`
		Error               string `json:"error"`
	}
	code, _, err := c.doJSON(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": user, "password": password}, &resp)
	if err != nil {
		return fmt.Errorf("对侧管理员登录请求失败: %w", err)
	}
	if code == http.StatusOK && resp.MustChangePassword {
		return fmt.Errorf("对侧管理员 %q 处于首登强制改密态：请先用该账号人工登录完成改密，再跑迁移", user)
	}
	if code != http.StatusOK {
		return fmt.Errorf("对侧管理员登录失败（%d）: %s", code, orDefault(resp.Error, "unexpected status"))
	}
	return nil
}

type farUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Status   string `json:"status"`
}

func (c *authSvcClient) listUsers() (map[string]farUser, error) {
	var resp struct {
		Users []farUser `json:"users"`
		Error string    `json:"error"`
	}
	code, _, err := c.doJSON(http.MethodGet, "/api/v1/users", nil, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("GET /api/v1/users => %d: %s", code, orDefault(resp.Error, "unexpected status"))
	}
	out := make(map[string]farUser, len(resp.Users))
	for _, u := range resp.Users {
		out[u.Username] = u
	}
	return out, nil
}

func (c *authSvcClient) listRoles() (map[string]bool, error) {
	var resp struct {
		Roles []struct {
			ID string `json:"id"`
		} `json:"roles"`
		Error string `json:"error"`
	}
	code, _, err := c.doJSON(http.MethodGet, "/api/v1/roles", nil, &resp)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("GET /api/v1/roles => %d: %s", code, orDefault(resp.Error, "unexpected status"))
	}
	out := make(map[string]bool, len(resp.Roles))
	for _, r := range resp.Roles {
		out[r.ID] = true
	}
	return out, nil
}

// createUser 对侧管理端建号（默认 active、无须审批——比"注册→审批"少一步，且绕开公开注册闸门）。
func (c *authSvcClient) createUser(username, password, email string, roleIDs []string) error {
	if roleIDs == nil {
		roleIDs = []string{}
	}
	var resp struct {
		Error string `json:"error"`
	}
	code, _, err := c.doJSON(http.MethodPost, "/api/v1/users",
		map[string]any{"username": username, "password": password, "email": email, "roleIDs": roleIDs}, &resp)
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("POST /api/v1/users => %d: %s", code, orDefault(resp.Error, "unexpected status"))
	}
	return nil
}

// loginVerify 用一次性口令真登一次对侧（独立 cookie jar，不污染管理员会话），随后登出清理会话。
// 这是「搬完才写名册」的验证半边：登录通不过 = 该账号对用户不可用 = 绝不能进名册。
func (c *authSvcClient) loginVerify(username, password string) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	vc := &authSvcClient{base: c.base, hc: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
	var resp struct {
		Error string `json:"error"`
	}
	code, _, err := vc.doJSON(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": username, "password": password}, &resp)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("登录 => %d: %s", code, orDefault(resp.Error, "unexpected status"))
	}
	// 清理验证会话（at 15min 自然过期，但登出更干净——不留挂着的 rt）。
	// 登出是清理性动作：失败只意味着对侧多一个短时会话，不构成迁移失败，忽略。
	if _, _, lerr := vc.doJSON(http.MethodPost, "/api/v1/auth/logout", map[string]string{}, nil); lerr != nil {
		_ = lerr // best-effort：见上行注释
	}
	return nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// generateOneTimePassword 生成 24 位强随机一次性口令：保证含大写/小写/数字/特殊字符，
// 满足对侧强口令策略（minLen=12 + 要求特殊字符，见其 config 默认）。
func generateOneTimePassword() (string, error) {
	const (
		upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		lower   = "abcdefghijkmnopqrstuvwxyz"
		digits  = "23456789"
		special = "!@#$%^&*-_=+"
		all     = upper + lower + digits + special
	)
	pick := func(set string) (byte, error) {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		if err != nil {
			return 0, err
		}
		return set[n.Int64()], nil
	}
	out := make([]byte, 0, 24)
	for _, set := range []string{upper, lower, digits, special} {
		c, err := pick(set)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < 24 {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	// Fisher-Yates 洗牌（前 4 位是保底字符，不洗会降低熵的可信度）。
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}
