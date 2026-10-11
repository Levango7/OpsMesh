// Command opsmesh 是 OpsMesh 网段运维中枢的二进制内核。
// 同一份代码、同一份二进制，通过 --mode 切换控制面(controlplane)与网段 agent。
// 仅依赖 Go 标准库 + 少量稳定外部依赖（grpc/mysql/redis），无任何 protobuf 代码生成。
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Levango7/OpsMesh/internal/agent"
	"github.com/Levango7/OpsMesh/internal/config"
	"github.com/Levango7/OpsMesh/internal/controlplane"
	"github.com/Levango7/OpsMesh/internal/controlplane/backup"
	"github.com/Levango7/OpsMesh/internal/controlplane/cutover"
	"github.com/Levango7/OpsMesh/internal/logx"
	"github.com/Levango7/OpsMesh/internal/version"
)

// applyLogLevel 应用 --log-level / OPSMESH_LOG_LEVEL（P1-6 可支撑性）。
// 非法值 fail-fast 并返回非零退出码：配置类错误静默退回默认，会让现场排障
// 退化成「我明明设了 debug，为什么没有 debug 日志」。三个入口（主流程/backup/
// restore）共用，保证子命令日志级别语义一致。
func applyLogLevel(cfg *config.Config) int {
	lv, err := logx.ParseLevel(cfg.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[config] %v\n", err)
		logx.Error(context.Background(), "日志级别配置非法，启动中止", err, "logLevel", cfg.LogLevel)
		return 1
	}
	logx.SetLevel(lv)
	return 0
}

func main() {
	os.Exit(runMain())
}

// runMain 是 main 的可测试核心：返回进程退出码而非直接 os.Exit，
// 便于单测在主测试进程内安全驱动 --version / --health 等短路分支并断言返回码。
// controlplane/agent 分支会阻塞（启动服务器/agent 主循环），仅由真实二进制入口调用。
func runMain() int {
	// --version 必须在 config.Load 之前短路，否则 flag.Parse 会因未知 flag 退出。
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" {
			fmt.Print(versionString())
			return 0
		}
	}

	// --health 子命令：探活 localhost:{httpPort}/healthz，供 docker-compose / K8s 健康检查使用。
	// 必须在 config.Load 之前短路：distroless 镜像无 curl/sh，且 flag.Parse 会因未知 flag 退出。
	// 退出码：HTTP 200 → 0（健康），否则 → 1（不健康/不可达）。
	for _, a := range os.Args[1:] {
		if a == "--health" || a == "-health" {
			return runHealth()
		}
	}

	// backup/restore/migrate 子命令：导出/导入控制面数据（备份/灾备恢复），
	// 或只执行启动期 schema 迁移（部署前显式步骤，TD-83）。
	// 必须在 config.Load 之前短路：特有 flag（--output/--format 等）未注册到全局 flag 表，
	// flag.Parse 会因未知 flag 退出。子命令内部用独立 FlagSet 解析特有参数，
	// 再过滤掉特有 flag 后调用 config.Load 解析 store 相关参数（--store/--mysql-dsn 等）。
	for _, a := range os.Args[1:] {
		switch a {
		case "backup":
			return runBackup()
		case "restore":
			return runRestore()
		case "migrate":
			return runMigrate()
		case "cutover-migrate":
			return runCutoverMigrate()
		}
	}

	cfg := config.Load()

	// 日志级别先于其余校验生效，使后续启动日志（含校验失败详情）都按配置级别输出。
	if code := applyLogLevel(cfg); code != 0 {
		return code
	}

	// 启动期配置校验：明显非法配置立即失败，而非运行期诡异出错（健壮性）。
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[config] 校验失败: %v\n", err)
		return 1
	}

	switch cfg.Mode {
	case "controlplane":
		// 控制面模式：HTTP(B/S) 8080 + gRPC 9090（真实注册通道） + metrics 9091。
		srv, err := controlplane.NewServer(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[controlplane] 初始化失败: %v\n", err)
			return 1
		}
		if err := srv.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "[controlplane] 启动失败: %v\n", err)
			return 1
		}
	case "agent":
		// agent 模式：向控制面注册并托管本段网络内的自动化任务。
		ag := agent.New(cfg)
		if err := ag.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "[agent] 启动失败: %v\n", err)
			return 1
		}
	default:
		fmt.Fprintf(os.Stderr, "未知 --mode=%q（应为 controlplane 或 agent）\n", cfg.Mode)
		return 1
	}
	return 0
}

// versionString 返回 --version 子命令的标准输出（单行）。
// 提取为独立函数便于单测精确断言输出格式，避免依赖 stdout 捕获。
func versionString() string {
	return fmt.Sprintf("opsmesh %s (commit=%s date=%s)\n", version.Version, version.Commit, version.Date)
}

// runHealth 实现 --health 子命令：对 localhost:{httpPort}/healthz 发 GET，
// 200 → 返回 0（健康），否则 → 返回 1（不健康/不可达）。
//
// 设计要点：
//   - 用独立 FlagSet 解析 --http-port，避免污染 config.Load 的全局 flag 表；
//   - 从 os.Args[1:] 过滤掉 --health/-health 自身再 parse，避免假设 --health 必为首个参数
//     （如 `opsmesh --http-port=8080 --health` 时 os.Args[2:] 含 --health，FlagSet 不认识会 exit 2）；
//   - FlagSet 用 ContinueOnError，parse 失败返回 2（参数错误），不污染主进程 flag 表；
//   - 3s 超时与 docker-compose healthcheck.timeout 对齐，避免阻塞容器编排；
//   - 仅依赖 Go 标准库，distroless 镜像无需 curl/wget/sh 即可探活；
//   - 错误诊断输出到 stderr，stdout 保持空，便于编排系统解析退出码。
//   - 协议自适应：控制面 Web/REST 可能是明文（--http-tls=off 或未配证书）或 HTTPS
//     （--http-tls=auto/on 且证书齐备），探针两种都探，先 HTTPS 后 HTTP。
func runHealth() int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	httpPort := fs.Int("http-port", 8080, "控制面 HTTP 端口（探活目标）")
	// 过滤掉 --health/-health 自身，剩余参数交给 FlagSet 解析。
	// 这样无论 --health 出现在命令行哪个位置都能正确解析其他 flag。
	var args []string
	for _, a := range os.Args[1:] {
		if a == "--health" || a == "-health" {
			continue
		}
		args = append(args, a)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 探针只判断本机监听端口能否返回 200，不认证对端身份（等价 curl -k），
	// 故跳过证书链校验：自签证书/证书 SAN 不含 localhost 都属正常情况。
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- 本机存活探针，不认证对端身份（等价 curl -k），详见上注
		},
	}
	var lastErr error
	for _, scheme := range []string{"https", "http"} {
		url := fmt.Sprintf("%s://localhost:%d/healthz", scheme, *httpPort)
		resp, err := client.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("连接 %s 失败: %w", url, err)
			continue
		}
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusOK {
			return 0
		}
		lastErr = fmt.Errorf("%s 返回非 200: HTTP %d", url, status)
	}
	fmt.Fprintf(os.Stderr, "[health] %v\n", lastErr)
	return 1
}

// backupFlagSpecs 定义 backup 子命令特有 flag 及其是否带值（bool flag 不带值）。
// 用于从 os.Args 中过滤掉这些 flag，使剩余参数可安全交给 config.Load 的全局 flag.Parse。
var backupFlagSpecs = map[string]bool{ // flag 名 -> 是否带值（false=布尔 flag）
	"output":            true,
	"format":            true,
	"include-config":    false,
	"include-audits":    false,
	"task-window-days":  true,
	"alert-window-days": true,
	"audit-window-days": true,
}

// restoreFlagSpecs 定义 restore 子命令特有 flag。
var restoreFlagSpecs = map[string]bool{
	"input":     true,
	"dry-run":   false,
	"overwrite": false,
}

// migrateFlagSpecs 定义 migrate 子命令特有 flag。
// migrate 不注册任何特有 flag（复用 --store/--mysql-dsn 等全局参数），
// specs 为空表：filterSubcmdArgs 只移除子命令名，其余参数全部透传给
// config.Load 的全局 flag 表解析。
var migrateFlagSpecs = map[string]bool{}

// cutoverMigrateFlagSpecs 定义 cutover-migrate 子命令特有 flag（TD-60 方案 C 搬号 job）。
// 其余参数（--store/--mysql-dsn 等）复用 config.Load 全局表：job 必须与控制面读**同一份 store**。
var cutoverMigrateFlagSpecs = map[string]bool{
	"users":                   true,
	"roster-file":             true,
	"auth-svc-url":            true,
	"auth-svc-admin-user":     true,
	"auth-svc-admin-password": true,
	"dry-run":                 false,
	"credentials-out":         true,
}

// filterSubcmdArgs 从 args 中移除子命令名及其特有 flag，返回剩余参数（供 config.Load 解析）。
//
// 处理两种 flag 形式：
//   - --flag=value：整 token 移除；
//   - --flag value：当前 token 与下一 token 一起移除（带值 flag）；
//   - --flag：仅当前 token 移除（布尔 flag）。
//
// 子命令名（如 "backup"/"restore"）作为首 token 出现时也被移除。
func filterSubcmdArgs(args []string, subcmd string, specs map[string]bool) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == subcmd {
			continue
		}
		// 解析 --name 或 --name=value
		if strings.HasPrefix(a, "--") || strings.HasPrefix(a, "-") {
			name := strings.TrimLeft(a, "-")
			hasEq := false
			if idx := strings.Index(name, "="); idx >= 0 {
				name = name[:idx]
				hasEq = true
			}
			if takesValue, ok := specs[name]; ok {
				if takesValue && !hasEq {
					// --flag value：跳过当前 token 与下一 token（值）
					i++
				}
				// 否则仅跳过当前 token（布尔 flag 或 --flag=value 形式）
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// pickSubcmdArgs 从 args 中**只挑出**子命令特有 flag 的 token（含其值），供子命令自己的 FlagSet 解析。
//
// 为什么需要它（既有缺陷，2026-10-11 实测）：此前三个子命令都把「过滤后的剩余参数」交给
// config.Load，却把**未过滤的全量参数**交给自己的 FlagSet —— 于是全局 flag（--store/--mysql-dsn）
// 会在子命令 FlagSet 上报 "flag provided but not defined"。backup 的文档注释明明承诺
// `[--store memory|mysql]`，实际 `opsmesh backup --output x --store mysql` 直接退出 2
// ⇒ MySQL 部署下备份/恢复按文档用法根本跑不了（memory 默认值下碰巧没人发现）。
// 修法：子命令 FlagSet 只解析 specs 里自己的 flag（本函数），其余 token 照旧由
// filterSubcmdArgs 交给 config.Load —— 未知 flag 在全局 flag.Parse 上仍有报错兜底，不会静默吞。
func pickSubcmdArgs(args []string, specs map[string]bool) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		hasEq := false
		if idx := strings.Index(name, "="); idx >= 0 {
			name = name[:idx]
			hasEq = true
		}
		takesValue, ok := specs[name]
		if !ok {
			continue
		}
		out = append(out, a)
		if takesValue && !hasEq && i+1 < len(args) {
			i++
			out = append(out, args[i])
		}
	}
	return out
}

// runCutoverMigrate 实现 cutover-migrate 子命令：TD-60 方案 C 的搬号 job
// （把本批用户迁到 auth-svc，验证通过后写名册；决策单 docs/td60-cutover-decisions.md Q2-A）。
//
// 用法：
//
//	opsmesh cutover-migrate --users alice,bob
//	                       [--roster-file <path>] [--auth-svc-url <url>]
//	                       [--auth-svc-admin-user admin] [--auth-svc-admin-password <pwd>]
//	                       [--dry-run] [--credentials-out <path>]
//	                       [--store memory|mysql] [--mysql-dsn ...] ...
//
// env 兜底：AUTH_SVC_URL / AUTH_CUTOVER_ROSTER_FILE / AUTH_SVC_MIGRATE_ADMIN_USER / AUTH_SVC_MIGRATE_ADMIN_PASSWORD。
// 退出码：0=本批全部成功，1=有失败（名册已写成功者），2=参数/整体不可运行。
func runCutoverMigrate() int {
	fs := flag.NewFlagSet("cutover-migrate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	users := fs.String("users", "", "本批要迁的用户名（逗号分隔；清单即批次，不做全量）")
	rosterFile := fs.String("roster-file", "", "名册文件路径（默认 env AUTH_CUTOVER_ROSTER_FILE）")
	authSvcURL := fs.String("auth-svc-url", "", "auth-svc HTTP 网关地址（默认 env AUTH_SVC_URL）")
	adminUser := fs.String("auth-svc-admin-user", "", "对侧管理员账号（默认 admin）")
	adminPassword := fs.String("auth-svc-admin-password", "", "对侧管理员口令（默认 env AUTH_SVC_MIGRATE_ADMIN_PASSWORD）")
	dryRun := fs.Bool("dry-run", false, "只做只读核验（本地存在性 + 对侧现状 + 名册现状），不建号、不写名册")
	credentialsOut := fs.String("credentials-out", "", "一次性口令写入该文件（0600）而不是打印到终端")

	if err := fs.Parse(pickSubcmdArgs(os.Args[1:], cutoverMigrateFlagSpecs)); err != nil {
		return 2
	}
	if strings.TrimSpace(*users) == "" {
		fmt.Fprintln(os.Stderr, "[cutover-migrate] --users 必填（清单即批次；不做全量迁移）")
		return 2
	}

	// 过滤特有 flag 后复用 config.Load 解析 store 参数（与 backup 同法）。
	cfgArgs := filterSubcmdArgs(os.Args[1:], "cutover-migrate", cutoverMigrateFlagSpecs)
	oldArgs := os.Args
	os.Args = append([]string{"opsmesh"}, cfgArgs...)
	cfg := config.Load()
	os.Args = oldArgs
	if code := applyLogLevel(cfg); code != 0 {
		return code
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[cutover-migrate] 配置校验失败: %v\n", err)
		return 1
	}
	st, err := backup.NewStoreForCLI(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[cutover-migrate] Store 初始化失败: %v\n", err)
		return 1
	}

	envOr := func(flagVal, envKey string) string {
		if strings.TrimSpace(flagVal) != "" {
			return flagVal
		}
		return os.Getenv(envKey)
	}
	opts := cutover.MigrateOptions{
		Users:         strings.Split(*users, ","),
		RosterFile:    envOr(*rosterFile, "AUTH_CUTOVER_ROSTER_FILE"),
		AuthSvcURL:    envOr(*authSvcURL, "AUTH_SVC_URL"),
		AdminUser:     envOr(*adminUser, "AUTH_SVC_MIGRATE_ADMIN_USER"),
		AdminPassword: envOr(*adminPassword, "AUTH_SVC_MIGRATE_ADMIN_PASSWORD"),
		DryRun:        *dryRun,
	}
	// 本地事实适配：store -> LocalLookup（job 只需要 email + 角色 ID）。
	lookup := func(username string) (cutover.LocalUser, bool) {
		u := st.GetUserByUsername(username)
		if u == nil {
			return cutover.LocalUser{}, false
		}
		return cutover.LocalUser{Email: u.Email, RoleIDs: u.RoleIDs}, true
	}

	res, err := cutover.RunMigration(opts, lookup)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[cutover-migrate] %v\n", err)
		return 1
	}

	// 报告（stderr；一次性口令单独一段并带警告——它是凭据，进终端历史/日志都要当心）。
	fmt.Fprintf(os.Stderr, "[cutover-migrate] 本批结果：新建 %d · 收编 %d · 名册已有 %d · 失败 %d · 名册新增 %d\n",
		len(res.Created), len(res.Adopted), len(res.Skipped), len(res.Failed), res.Added)
	for _, u := range res.Adopted {
		fmt.Fprintf(os.Stderr, "  [收编] %s（对侧已 active，仅补名册）\n", u.Username)
	}
	for _, u := range res.Skipped {
		fmt.Fprintf(os.Stderr, "  [已有] %s（名册中已存在，未动）\n", u)
	}
	for _, f := range res.Failed {
		fmt.Fprintf(os.Stderr, "  [失败] %s：%s（未写名册，留在本地登录面）\n", f.Username, f.Reason)
	}
	creds := res.Created
	if *dryRun {
		creds = nil
	}
	if len(creds) > 0 {
		var buf strings.Builder
		buf.WriteString("# 一次性口令清单（cutover-migrate）——凭据，勿入库、勿进工单系统；经安全渠道交给用户本人。\n")
		buf.WriteString("# 注意：对侧暂无「首登强制改密」API 入口（已登记缺口），请提示用户收到后自行改密。\n")
		for _, u := range creds {
			fmt.Fprintf(&buf, "%s\t%s\n", u.Username, u.OneTimePassword)
		}
		if strings.TrimSpace(*credentialsOut) != "" {
			if err := os.WriteFile(*credentialsOut, []byte(buf.String()), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "[cutover-migrate] 一次性口令写文件失败: %v（改回打印到终端）\n", err)
				fmt.Fprintln(os.Stderr, "\n"+strings.TrimRight(buf.String(), "\n"))
			} else {
				fmt.Fprintf(os.Stderr, "[cutover-migrate] 一次性口令已写入 %s（权限 0600），请经安全渠道交给用户\n", *credentialsOut)
			}
		} else {
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, strings.TrimRight(buf.String(), "\n"))
		}
	}
	if len(res.Failed) > 0 {
		fmt.Fprintln(os.Stderr, "[cutover-migrate] 有失败用户：修复后仅重跑失败者即可（幂等，成功者不会重复建号）")
		return 1
	}
	return 0
}

// runBackup 实现 backup 子命令：导出控制面数据到文件。
//
// 用法：
//
//	opsmesh backup --output <file> [--format json|sql] [--include-config] [--include-audits]
//	               [--task-window-days N] [--alert-window-days N] [--audit-window-days N]
//	               [--store memory|mysql] [--mysql-dsn ...] ...
//
// 设计要点：
//   - 用独立 FlagSet 解析 backup 特有 flag，避免污染 config.Load 全局 flag 表；
//   - 过滤掉特有 flag 后调用 config.Load 解析 store 相关参数（复用全部持久化配置）；
//   - 不启动 HTTP/gRPC，仅初始化 Store 后导出；
//   - 退出码：0=成功，1=运行错误，2=参数错误。
func runBackup() int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	output := fs.String("output", "", "备份输出文件路径（必填）")
	format := fs.String("format", "json", "备份格式: json（默认） | sql（MySQL dump）")
	includeConfig := fs.Bool("include-config", false, "是否导出运行配置（含敏感字段，慎用）")
	includeAudits := fs.Bool("include-audits", false, "是否导出审计日志（最近 30 天）")
	taskWindowDays := fs.Int("task-window-days", 7, "任务导出时间窗（天，默认 7）")
	alertWindowDays := fs.Int("alert-window-days", 7, "告警导出时间窗（天，默认 7）")
	auditWindowDays := fs.Int("audit-window-days", 30, "审计导出时间窗（天，默认 30）")

	// 过滤掉 backup 子命令名及其特有 flag，剩余参数交给 FlagSet 解析。
	if err := fs.Parse(pickSubcmdArgs(os.Args[1:], backupFlagSpecs)); err != nil {
		return 2
	}
	if *output == "" {
		fmt.Fprintln(os.Stderr, "[backup] --output 必填")
		return 2
	}

	// 过滤掉 backup 特有 flag，剩余参数交给 config.Load 解析 store 配置。
	cfgArgs := filterSubcmdArgs(os.Args[1:], "backup", backupFlagSpecs)
	// 临时替换 os.Args 以复用 config.Load（它解析全局 flag 表）。
	oldArgs := os.Args
	os.Args = append([]string{"opsmesh"}, cfgArgs...)
	cfg := config.Load()
	os.Args = oldArgs

	if code := applyLogLevel(cfg); code != 0 {
		return code
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[backup] 配置校验失败: %v\n", err)
		return 1
	}

	st, err := backup.NewStoreForCLI(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup] Store 初始化失败: %v\n", err)
		return 1
	}

	opts := backup.ExportOptions{
		Format:          *format,
		IncludeConfig:   *includeConfig,
		IncludeAudits:   *includeAudits,
		TaskWindowDays:  *taskWindowDays,
		AlertWindowDays: *alertWindowDays,
		AuditWindowDays: *auditWindowDays,
	}
	data, err := backup.ExportBackupFile(context.Background(), st, cfg, opts, *output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backup] 导出失败: %v\n", err)
		return 1
	}

	fmt.Fprintf(os.Stderr, "[backup] 导出成功 → %s\n", *output)
	fmt.Fprintf(os.Stderr, "  设备: %d, agent: %d, 任务: %d, 告警: %d\n",
		data.Meta.Counts.Devices, data.Meta.Counts.Agents, data.Meta.Counts.Tasks, data.Meta.Counts.Alerts)
	fmt.Fprintf(os.Stderr, "  告警规则: %d, 用户: %d, 角色: %d, 权限: %d\n",
		data.Meta.Counts.AlertRules, data.Meta.Counts.Users, data.Meta.Counts.Roles, data.Meta.Counts.Permissions)
	if data.Meta.IncludeAudits {
		fmt.Fprintf(os.Stderr, "  审计: %d\n", data.Meta.Counts.Audits)
	}
	return 0
}

// runRestore 实现 restore 子命令：从备份文件导入数据到 Store。
//
// 用法：
//
//	opsmesh restore --input <file> [--dry-run] [--overwrite]
//	                [--store memory|mysql] [--mysql-dsn ...] ...
//
// 仅支持 JSON 格式（SQL dump 需先用 mysql 客户端导入 DB）。
// 退出码：0=成功，1=运行错误，2=参数错误。
func runRestore() int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	input := fs.String("input", "", "备份输入文件路径（必填，JSON 格式）")
	dryRun := fs.Bool("dry-run", false, "只校验不实际写入")
	overwrite := fs.Bool("overwrite", false, "覆盖已存在的数据（否则跳过）")

	if err := fs.Parse(pickSubcmdArgs(os.Args[1:], restoreFlagSpecs)); err != nil {
		return 2
	}
	if *input == "" {
		fmt.Fprintln(os.Stderr, "[restore] --input 必填")
		return 2
	}

	cfgArgs := filterSubcmdArgs(os.Args[1:], "restore", restoreFlagSpecs)
	oldArgs := os.Args
	os.Args = append([]string{"opsmesh"}, cfgArgs...)
	cfg := config.Load()
	os.Args = oldArgs

	if code := applyLogLevel(cfg); code != 0 {
		return code
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[restore] 配置校验失败: %v\n", err)
		return 1
	}

	st, err := backup.NewStoreForCLI(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[restore] Store 初始化失败: %v\n", err)
		return 1
	}

	opts := backup.ImportOptions{
		DryRun:    *dryRun,
		Overwrite: *overwrite,
	}
	_, res, err := backup.ImportBackupFile(context.Background(), st, opts, *input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[restore] 导入失败: %v\n", err)
		return 1
	}

	mode := "导入"
	if *dryRun {
		mode = "校验（dry-run）"
	}
	fmt.Fprintf(os.Stderr, "[restore] %s成功 ← %s\n", mode, *input)
	fmt.Fprintf(os.Stderr, "  设备: %d, agent: %d, 任务: %d, 告警: %d\n",
		res.Devices, res.Agents, res.Tasks, res.Alerts)
	fmt.Fprintf(os.Stderr, "  告警规则: %d, 用户: %d, 角色: %d, 跳过: %d\n",
		res.AlertRules, res.Users, res.Roles, res.Skipped)
	return 0
}

// runMigrate 实现 migrate 子命令：只执行启动期 schema 迁移然后退出。
//
// 用法：
//
//	opsmesh migrate [--store=mysql] [--mysql-dsn ...] [其余全局参数]
//
// 背景（TD-83）：020/021 是"重建表 + 禁写"型迁移，大表上耗时远超启动
// 健康预算（§44 实测 30 万行 ≈117s、100 万行 ≈376s）。此前迁移只在
// 控制面启动内联执行，吃掉 deploy.sh 的 120s 健康预算并在窗口内阻塞
// CMDB 写入。本子命令把迁移提前到放量前单独跑（deploy.sh migrate），
// 与启动内联走同一条代码路径（factory.SelectStore → NewSQLStore →
// runMigrations），不启动 HTTP/gRPC/metrics，短生命周期。
//
// 设计要点（与 backup/restore 同型）：
//   - 过滤掉子命令名后复用 config.Load 解析全部全局参数；
//   - 迁移在 Store 构造内执行，失败显式返回 1，不静默回退 memory——
//     factory.SelectStore 对非 mysql 后端返回 MemoryStore 且无错误，
//     对 migrate 那是"什么都没做"的假成功，故 store 非 mysql 或
//     DSN 缺失时显式拒绝（默认 store=memory 不会执行任何迁移）；
//   - 退出码：0=成功，1=运行错误，2=参数错误（本命令无特有 flag，
//     参数错误由 config.Load 的全局 flag.Parse 以退出码 2 报出）；
//   - 耗时输出到 stderr，stdout 保持空，便于编排系统解析退出码。
func runMigrate() int {
	cfgArgs := filterSubcmdArgs(os.Args[1:], "migrate", migrateFlagSpecs)
	oldArgs := os.Args
	os.Args = append([]string{"opsmesh"}, cfgArgs...)
	cfg := config.Load()
	os.Args = oldArgs

	if code := applyLogLevel(cfg); code != 0 {
		return code
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "[migrate] 配置校验失败: %v\n", err)
		return 1
	}
	if cfg.Store != "mysql" || cfg.MySQLDSN == "" {
		fmt.Fprintln(os.Stderr, "[migrate] 迁移仅对 mysql 后端有意义：请显式 --store=mysql --mysql-dsn=...（默认 store=memory 不会执行任何迁移）")
		return 1
	}

	start := time.Now()
	if _, err := backup.NewStoreForCLI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "[migrate] 迁移失败: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "[migrate] 迁移完成（耗时 %.1fs）\n", time.Since(start).Seconds())
	return 0
}
