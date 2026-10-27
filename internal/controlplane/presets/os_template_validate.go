// Package presets: os_template_validate.go 实现 OS 模板参数验证与脚本渲染。
//
// 从 os_optimize.go 拆分而来，包含 BuildOSExecuteCommand/RenderOSScript 等纯函数，
// 以及 shell 元字符防护常量 shellUnsafeChars（middleware deploy 与 OS execute 共用）。
package presets

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// ScriptCommandMarker 是多行脚本体任务在任务 Command 字段里的**单行**调用形态。
//
// 为什么需要它：agent 端对内联命令的换行是硬拦截（checkShellMetachars，
// 防 "ls\nrm -rf /" 拼接注入），而预置模板天然是多行 bash。多行内容只能走
// 脚本体通道（proto.Task.ScriptBody，agent 落临时文件后以 `sh <file>` 执行），
// Command 字段则退化成一行「声明式」调用——它仍会过 agent 端的元字符拦截与
// 命令白名单（白名单里没有 sh 的节点不允许执行脚本类任务）。
//
// {script} 是脚本文件路径占位符（internal/proto.ScriptPathPlaceholder），
// 由 agent 在执行前替换为临时脚本文件的真实路径。
const ScriptCommandMarker = "sh " + proto.ScriptPathPlaceholder

// BuildOSExecuteCommand 将模板脚本与 params 拼接为「脚本体 + 单行命令」两段。
//
// 改造背景：原实现把 `set -- 'p1' 'p2'` 与脚本体用**换行**拼成一条多行命令，
// 而 agent 端对内联命令的换行是硬拦截 ⇒ 「OS 优化」等模板 100% 失败
// （报错还是误导性的 "shell command contains dangerous metacharacters (newline)"）。
//
// 现在的语义：
//   - 脚本体（scriptBody）：整段脚本；位置参数以 `set -- 'p1' 'p2'` 注入脚本首行
//     （shebang 行保持在最前），脚本内 $1/$2 的含义与改造前完全一致——
//     sh 执行脚本时 $1 就是第一个位置参数。
//   - 单行命令（command）：脚本体多行时为 ScriptCommandMarker（`sh {script}`），
//     单行时为脚本体原文（走内联命令通道，行为与改造前一致）。
//
// 脚本体单行时**不**拆出 scriptBody：内联通道的元字符/白名单校验对它完全适用，
// 没有理由绕道临时文件。
func BuildOSExecuteCommand(commands string, params []string) (scriptBody, command string) {
	return SplitScriptCommand(injectPositionalParams(commands, params))
}

// injectPositionalParams 把位置参数以 `set -- 'p1' 'p2'` 注入脚本首行。
//
// 单引号转义：' -> '\”（与改造前一致，值里的单引号不会提前闭合引号）。
// shebang 行（#!/bin/bash）保持在脚本首行——脚本体落盘后仍是一个合法的脚本文件。
func injectPositionalParams(commands string, params []string) string {
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
	// shebang 行前置：把参数行插在 shebang **之后**，脚本首行仍是解释器声明。
	prefix, body := "", commands
	if strings.HasPrefix(commands, "#!") {
		if idx := strings.Index(commands, "\n"); idx >= 0 {
			prefix, body = commands[:idx+1], commands[idx+1:]
		}
	}
	return prefix + b.String() + body
}

// SplitScriptCommand 把可能多行的 shell 内容拆成（脚本体, 单行命令）。
//
//   - 单行内容：scriptBody=""、command=原文（走内联命令通道，行为不变）。
//   - 多行内容：scriptBody=原文、command=ScriptCommandMarker（`sh {script}`）——
//     agent 端把脚本体落临时文件后以 `sh <file>` 执行。
//
// 为什么多行必须走脚本体通道：内联通道对换行/回车是硬拦截，多行内容留在那里
// 必然被拒；而脚本体是**整段脚本**（heredoc、函数定义、for 循环），
// 不存在「内联命令被换行拼接」的攻击面。
func SplitScriptCommand(script string) (scriptBody, command string) {
	if !strings.ContainsAny(script, "\n\r") {
		return "", script
	}
	return script, ScriptCommandMarker
}

// RenderOSScript 将脚本中的 {name}/{size}/... 占位符替换为 params 实际值。
// 占位符语法：{key}，未提供 key 时保留原占位符（便于排查）。
func RenderOSScript(script string, params map[string]string) string {
	out := script
	for k, v := range params {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}

// ValidateOSParams 校验 OS 模板参数的类型与语义。
// - int 类型：必须为整数；若参数名为 port 则校验端口范围 1-65535。
// - string 类型：非空校验。
func ValidateOSParams(params []OSParam, values map[string]string) error {
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
				if err := ValidatePort(n); err != nil {
					return err
				}
			}
		case "string":
			if err := ValidateNonEmpty(p.Name, val); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidatePort 校验端口范围 1-65535。
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}
	return nil
}

// ValidateNonEmpty 校验字符串非空（trim 后）。
func ValidateNonEmpty(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	return nil
}

// ValidatePath 校验路径以 / 开头。
func ValidatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path must start with /, got %s", path)
	}
	return nil
}

// shellUnsafeChars 是禁止出现在模板参数值中的 shell 元字符（命令注入防护）。
// 模板参数经 renderMiddlewareScript/RenderOSScript 原样替换进 shell 脚本，由 agent 以 sh -c 执行；
// 值中若含以下字符即可截断/拼接命令造成目标机 RCE，故一律拒绝（含空格，防参数歧义）。
const shellUnsafeChars = " ;&|$`\n\r\t<>(){}\"'\\*?[]!#~"

// ValidateShellSafeValues 校验全部模板参数值不含 shell 元字符（命令注入防护）。
// 对 values 中每个键值做检查，任一值含元字符即返回错误。
// 调用点：middleware deploy/uninstall 与 OS 模板 execute 在渲染脚本前统一调用。
func ValidateShellSafeValues(values map[string]string) error {
	for name, val := range values {
		if strings.ContainsAny(val, shellUnsafeChars) {
			return fmt.Errorf("param %s contains shell metacharacters and is rejected for safety", name)
		}
	}
	return nil
}

// ValidateShellSafeParamList 校验位置参数列表（旧模式模板的 params 是数组形态）中的
// 每个值不含 shell 元字符——与 ValidateShellSafeValues 同规则，只是入参形态不同。
//
// 调用点：OS 模板 execute 的旧模式分支。值会被 `set -- 'p1' 'p2'` 注入脚本体，
// 不校验就等于把「模板参数」变成任意命令注入面（虽然单引号转义已挡住拼接，
// 但元字符值本身就没有合法用途——放行只会掩盖误用与注入尝试）。
func ValidateShellSafeParamList(params []string) error {
	for i, val := range params {
		if strings.ContainsAny(val, shellUnsafeChars) {
			return fmt.Errorf("param #%d contains shell metacharacters and is rejected for safety", i)
		}
	}
	return nil
}

// NormalizeRisk 将 risk 归一为合法值（low/medium/high），空或非法值归一为 low。
func NormalizeRisk(risk string) string {
	switch risk {
	case "low", "medium", "high":
		return risk
	default:
		return "low"
	}
}
