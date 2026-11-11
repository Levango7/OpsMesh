// Package controlplane ops_sql.go —— SQL 面通用收口助手（批次 12）。
//
// 批次 12 的注入面大量出现在「把用户字段拼进要下发的 shell 脚本」的场景里，且相当一部分
// 命令参数本身是 SQL 客户端（mysql / psql / mongo）或 SQL 文本。SQL 面没有「自由文本合法/
// 非法」的通用校验器（不像自由 JSON 可以 json.Valid），只能按**角色**分别收口：
//
//   - **主机名**：只会落进 `-h` / `--host` 的选项值 ⇒ 白名单（字母数字开头，仅含
//     字母数字与 . _ : -；`:` 放行 IPv6；不能以 `-` 开头，避免被 CLI 解析成选项）。
//   - **库名 / 表名**：落进 `mysqldump ... <db>` 的位置参数，或进 SQL 的标识符位置
//     ⇒ 白名单（字母数字或 _ 开头，可含 . _ -，支持 db.table 形态）。
//   - **SQL 字符串字面量**：无法白名单时，按 SQL 语义把单引号双写（`'` → `”`）。
//   - **SQL 语句整体**：不能拆开校验时的最后一道防线——**整体** shellquote.Quote
//     （调用点直接 Quote，本文件不提供二次包装）。
//
// 其余角色复用既有助手：资源名/用户名用 mqName；路径用 mqPath；自由 JSON 用
// opsQuoteJSONText / opsJSONPayload；回显消息用 opsEcho。
//
// 能力降级点（如实登记）：
//   - 主机名收窄为 `^[A-Za-z0-9][A-Za-z0-9.:_-]*$`：含空格 / `/` / shell 元字符的主机
//     会被 400 拒收。
//   - 库名/表名收窄为 `^[A-Za-z0-9_][A-Za-z0-9_.-]*$`：含引号 / 空白 / `*` 的名字会被拒收。
package controlplane

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

var (
	// opsHostRe 是主机名白名单：字母数字开头，仅含字母数字与 . _ : -。
	opsHostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.:_-]*$`)

	// opsDBNameRe 是库名/表名白名单：字母数字或 _ 开头，可含 . _ -。
	opsDBNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)
)

// opsHost 校验必填的主机名（`-h` / `--host` 的选项值）。
func opsHost(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsHostRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsHostRe.String())
	}
	return s, nil
}

// opsDBName 校验必填的库名/表名（位置参数 / SQL 标识符位置）。
func opsDBName(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsDBNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsDBNameRe.String())
	}
	return s, nil
}

// opsSQLString 按 SQL 语义转义**字符串字面量**里的单引号（`'` → `”`）。
// 用于把用户值插进 `SQL '...'` 这类字面量位置；不用于标识符位置（那类用 opsDBName）。
func opsSQLString(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// opsRejectHeredocDelim 拒绝「含一行恰好等于 delim 的内容」（heredoc 会提前闭合，
// 其后的内容被 shell 当命令执行）。用于把用户代码/模板写进 `<< 'SCRIPT_EOF'` 场景。
func opsRejectHeredocDelim(content, delim string) error {
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSuffix(line, "\r") == delim {
			return fmt.Errorf("content contains a line exactly equal to %q (heredoc would close early)", delim)
		}
	}
	return nil
}

// opsNameRe 是「模板/脚本名」白名单：字母数字开头，仅含字母数字与 _ . -，长度 ≤128。
var opsNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// opsName 校验必填的模板名。
func opsName(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsNameRe.String())
	}
	return s, nil
}

// opsProfileRe 是构建 profile 白名单：字母数字开头，仅含字母数字与 . _ , : @ -（逗号分隔多个 profile），无空格。
var opsProfileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._,:@-]{0,255}$`)

// opsProfile 校验必填的 profile。
func opsProfile(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsProfileRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsProfileRe.String())
	}
	return s, nil
}

// opsSysctlParamRe 是 sysctl 参数名白名单：字母数字/_ . -（仅含这几类字符，不含空格与 shell 元字符）。
var opsSysctlParamRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// opsSysctlParam 校验必填的 sysctl 参数名（如 vm.swappiness）。
func opsSysctlParam(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsSysctlParamRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsSysctlParamRe.String())
	}
	return s, nil
}

// opsSysctlValueRe 是 sysctl 参数值白名单：字母数字/_ . - , : = %（不含空格与 shell 元字符）。
var opsSysctlValueRe = regexp.MustCompile(`^[A-Za-z0-9_.,:%=-]{0,128}$`)

// opsSysctlValue 校验可选的 sysctl 参数值（空串放行）。
func opsSysctlValue(field, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !opsSysctlValueRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsSysctlValueRe.String())
	}
	return s, nil
}

// opsModuleRe 是内核模块名白名单：小写字母数字/_ -（Linux 模块名惯例）。
var opsModuleRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// opsModule 校验必填的内核模块名。
func opsModule(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsModuleRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsModuleRe.String())
	}
	return s, nil
}

// opsDeviceNameRe 是块设备名白名单：字母数字/_ -（如 sda / nvme0n1 / mmcblk0）。
var opsDeviceNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

// opsDeviceName 校验必填的块设备名。
func opsDeviceName(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsDeviceNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsDeviceNameRe.String())
	}
	return s, nil
}

// opsSchedRe 是 I/O 调度器名白名单：小写字母数字/-（如 mq-deadline / kyber / bfq / noop）。
var opsSchedRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// opsSched 校验必填的调度器名。
func opsSched(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsSchedRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsSchedRe.String())
	}
	return s, nil
}

// opsDigitsRe 是纯数字白名单（如 ulimit -n 的文件描述符上限）。
var opsDigitsRe = regexp.MustCompile(`^[0-9]{1,10}$`)

// opsDigits 校验必填的纯数字字段。
func opsDigits(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsDigitsRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must be digits", field, s)
	}
	return s, nil
}

// opsDomainRe 是域名白名单：字母数字开头，仅含字母数字与 . -（不含空格/斜杠/ shell 元字符）。
var opsDomainRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`)

// opsDomain 校验必填的域名。
func opsDomain(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsDomainRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsDomainRe.String())
	}
	return s, nil
}

// opsHTTPURL 校验必填的扫描目标 URL：只放行 http:// 与 https://，消灭 file:// 等协议面。
func opsHTTPURL(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if len(s) > 2048 {
		return "", fmt.Errorf("%s too long (%d > 2048)", field, len(s))
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s, nil
	}
	return "", fmt.Errorf("invalid %s %q: must start with http:// or https://", field, s)
}

// opsOracleNameRe 是 Oracle 对象名白名单：字母开头，仅含字母数字与 _ $ #（DIRECTORY/db unique name/网络连接串名以外的标识符）。
var opsOracleNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_$#]{0,127}$`)

// opsOracleName 校验必填的 Oracle 对象名。
func opsOracleName(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsOracleNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsOracleNameRe.String())
	}
	return s, nil
}

// opsFileNameRe 是备份文件名白名单：字母数字开头，仅含字母数字与 . _ -（不含 $ / . / shell 元字符/斜杠）。
var opsFileNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// opsFileName 校验必填的文件名。
func opsFileName(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsFileNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsFileNameRe.String())
	}
	return s, nil
}

// opsTableListRe 是 Oracle 表名列表白名单：字母数字/_ $ # . , * @ -（支持 SCOTT.EMP,*DEPT,ORDERS@remote 形态），无空格。
var opsTableListRe = regexp.MustCompile(`^[A-Za-z0-9_$#.,*@-]{1,512}$`)

// opsTableList 校验必填的表名列表。
func opsTableList(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsTableListRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsTableListRe.String())
	}
	return s, nil
}

// opsPatchIDRe 是 Oracle 补丁号白名单：5–15 位纯数字。
var opsPatchIDRe = regexp.MustCompile(`^[0-9]{5,15}$`)

// opsPatchID 校验必填的补丁号。
func opsPatchID(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsPatchIDRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must be 5-15 digits", field, s)
	}
	return s, nil
}

// opsSQLIDRe 是 Oracle SQL_ID 白名单：字母数字（Oracle SQL_ID 为 13 位 base64；放宽到 1–20 位）。
var opsSQLIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,20}$`)

// opsSQLID 校验必填的 SQL_ID。
func opsSQLID(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !opsSQLIDRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, opsSQLIDRe.String())
	}
	return s, nil
}

// opsArgList 校验「一整串自由参数」并**原样返回**（空格拆分的多参数，禁止一切 shell 元字符）。
// 与直接调用 shellquote.ValidateArgList 的区别：本函数返回值可安全进脚本
// （白名单内字节没有任何 shell 语法含义），且语义仍是「原样透传」。
func opsArgList(field, s string) (string, error) {
	if err := shellquote.ValidateArgList(s, 1024); err != nil {
		return "", fmt.Errorf("invalid %s: %w", field, err)
	}
	return s, nil
}

// opsHeredocContent 校验 heredoc 内容（写文件/模板场景）：
// 若任何一行恰好等于 delim ⇒ 拒收（heredoc 会提前闭合，其后的内容被 shell 当命令执行）；
// 否则原样返回内容（无 markdown/引号加花，语义不变）。
func opsHeredocContent(field, content, delim string) (string, error) {
	if err := opsRejectHeredocDelim(content, delim); err != nil {
		return "", fmt.Errorf("invalid %s: %w", field, err)
	}
	return content, nil
}
