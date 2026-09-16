// Package controlplane: os_template_validate.go 实现 OS 模板参数验证与脚本渲染。
//
// 从 os_optimize.go 拆分而来，包含 buildOSExecuteCommand/renderOSScript 等纯函数，
// 以及 shell 元字符防护常量 shellUnsafeChars（middleware deploy 与 OS execute 共用）。
package controlplane

import (
	"fmt"
	"strconv"
	"strings"
)

// buildOSExecuteCommand 将模板脚本与 params 拼接为最终 shell 命令。
// 通过 `set -- 'p1' 'p2' ...` 注入位置参数（单引号转义），脚本内 $1/$2 即可引用。
// 无 params 时直接返回原脚本。
func buildOSExecuteCommand(commands string, params []string) string {
	if len(params) == 0 {
		return commands
	}
	var b strings.Builder
	b.WriteString("set --")
	for _, p := range params {
		b.WriteString(" '")
		// 单引号转义：' -> '\''
		b.WriteString(strings.ReplaceAll(p, "'", `'\''`))
		b.WriteString("'")
	}
	b.WriteString("\n")
	b.WriteString(commands)
	return b.String()
}

// renderOSScript 将脚本中的 {name}/{size}/... 占位符替换为 params 实际值。
// 占位符语法：{key}，未提供 key 时保留原占位符（便于排查）。
func renderOSScript(script string, params map[string]string) string {
	out := script
	for k, v := range params {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// validateOSParams 校验 OS 模板参数的类型与语义。
// - int 类型：必须为整数；若参数名为 port 则校验端口范围 1-65535。
// - string 类型：非空校验。
func validateOSParams(params []OSParam, values map[string]string) error {
	for _, p := range params {
		val, ok := values[p.Name]
		if !ok || val == "" {
			continue // 必填已在调用前处理
		}
		switch p.Type {
		case "int":
			n, err := strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("param %s must be integer, got %s", p.Name, val)
			}
			if p.Name == "port" {
				if err := validatePort(n); err != nil {
					return err
				}
			}
		case "string":
			if err := validateNonEmpty(p.Name, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePort 校验端口范围 1-65535。
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}
	return nil
}

// validateNonEmpty 校验字符串非空（trim 后）。
func validateNonEmpty(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

// validatePath 校验路径以 / 开头。
func validatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /, got %s", path)
	}
	return nil
}

// shellUnsafeChars 是禁止出现在模板参数值中的 shell 元字符（命令注入防护）。
// 模板参数经 renderMiddlewareScript/renderOSScript 原样替换进 shell 脚本，由 agent 以 sh -c 执行；
// 值中若含以下字符即可截断/拼接命令造成目标机 RCE，故一律拒绝（含空格，防参数歧义）。
const shellUnsafeChars = " ;&|$`\n\r\t<>(){}\"'\\*?[]!#~"

// validateShellSafeValues 校验全部模板参数值不含 shell 元字符（命令注入防护）。
// 对 values 中每个键值做检查，任一值含元字符即返回错误。
// 调用点：middleware deploy/uninstall 与 OS 模板 execute 在渲染脚本前统一调用。
func validateShellSafeValues(values map[string]string) error {
	for name, val := range values {
		if strings.ContainsAny(val, shellUnsafeChars) {
			return fmt.Errorf("param %s contains shell metacharacters and is rejected for safety", name)
		}
	}
	return nil
}

// normalizeRisk 将 risk 归一为合法值（low/medium/high），空或非法值归一为 low。
func normalizeRisk(risk string) string {
	switch risk {
	case "low", "medium", "high":
		return risk
	default:
		return "low"
	}
}
