// Package controlplane mq_script.go —— MQ 面（RabbitMQ / Pulsar / RocketMQ）的收口助手与白名单（批次 11）。
//
// # 为什么 MQ 面单独一个文件
//
// 批次 11 的 60 处注入面分布在 6 个文件，但用户字段的角色只有五类，**收口手段不同**：
//
//   - **资源名**（user / queue / policy / topic / tenant / namespace / group / broker /
//     subject …）——会被 CLI 当成**选项值**，其中 broker 还会落进**路径段**
//     （`broker-%s.conf`）⇒ 白名单。
//   - **节点名与节点列表**——列表要交给 shell 的 `for node in %s` 做词拆分 ⇒ **不能整体
//     Quote**（Quote 会把整个列表压成一个词，循环只跑一次）⇒ 逐项白名单后以空格连接。
//   - **版本号**——会落进下载 URL 的**路径段**（`/pulsar-%s/`）⇒ 白名单挡住 `/`、`?`、`#`。
//   - **普通值**（password / vhost / pattern / message / URI …）——Quote 即可。
//   - **自由 JSON**（policy def / connector config）——json.Valid 后整体 Quote
//     （复用批次 10 的 opsQuoteJSONText）。
//
// # 为什么 heredoc 里的证书路径必须白名单而不是 Quote
//
// rabbitmq security 把 CACert / CertPath / KeyPath 写进 `<< 'EOF'` 的**配置文件内容**。
// 若在这里 Quote，引号会成为配置文件的字面内容（RabbitMQ 读到带引号的路径 ⇒ 启动失败）；
// 若不处理，路径里的换行可以追加任意配置行、一行 `EOF` 还能提前闭合 heredoc ⇒ 其后
// 内容被 shell 当命令执行。故这类字段走**绝对路径白名单**：拒绝空白、引号、反斜杠、
// `$`、反引号、分号、换行与 `..`（复用 pkg/shellquote.ValidatePath）。
//
// # 能力降级点（如实登记在 PR / CHANGELOG）
//
//   - 资源名收窄为 `^[A-Za-z0-9][A-Za-z0-9._:@-]*$`：含空格、`/`、`*` 等字符的名字
//     会被 400 拒收。
//   - 版本号收窄为 `^[A-Za-z0-9][A-Za-z0-9._+~:-]*$`。
//   - RocketMQ topic 额外放行 `%` 与 `|`（DLQ topic 形如 `%DLQ%group`），但仍必须整体
//     Quote 后才能进脚本——`|` 在 shell 里是管道。
//   - 备份 / 证书路径必须是**绝对**路径（旧形态允许相对路径）。
//   - `nodes` 的逗号分隔列表现在会**逐项**进入 for 循环（旧形态把 `a,b` 当成一个词）。
package controlplane

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

var (
	// mqNameRe 是 MQ 资源名的统一字符集：字母数字开头，仅含字母数字与 `._:@-`。
	//
	// 为什么必须字母数字开头：这些名字会出现在 `-p %s` / `-t %s` 一类**选项值**位置；
	// 以 `-` 开头的值会被 CLI 解析成另一个选项（Quote 只保证 shell 层安全）。
	// 为什么允许 `@` / `:`：RabbitMQ 节点名（`rabbit@host`）与部分用户名的合法字符；
	// 两者都不是 shell 元字符，落进路径段也安全。
	mqNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]*$`)

	// mqVersionRe 是版本号白名单：字母数字开头，仅含字母数字与 `._+~:-`。
	//
	// 版本号会落进下载 URL 的**路径段**（`https://archive.apache.org/dist/pulsar/pulsar-%s/`）：
	// 放行 `/` 会改写路径层级、放行 `?` / `#` 会改写 URL 语义、放行 `..` 还能穿越。
	// Debian 的 epoch（`1:2.3`）与 rc 版本（`3.13.0-rc.1`）在本字符集内。
	mqVersionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~:-]*$`)

	// mqNodeRe 是节点名白名单：字母数字开头，仅含字母数字与 `._@-`。
	//
	// RabbitMQ 节点是 `rabbit@host` 形态；host 允许 `.` / `-`。节点名会交给
	// `ssh %s` 与 `rabbit@%s` 两个位置，且**列表项不做 Quote**（见 mqNodeList），
	// 故字符集里不能有任何 shell 元字符。
	mqNodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)

	// mqRocketTopicRe 是 RocketMQ topic 白名单：在 mqNameRe 的基础上放行 `%` 与 `|`。
	//
	// RocketMQ 的 DLQ topic 形如 `%DLQ%group`，故 `%` 必须放行；`|` 是 RocketMQ
	// 官方允许的 topic 字符。注意 `|` 在 shell 里是管道 ⇒ 本白名单只解决「名字语义」，
	// 调用点仍必须用 shellquote.Quote 再进脚本（AST 守卫会盯住裸用）。
	mqRocketTopicRe = regexp.MustCompile(`^[A-Za-z0-9%|][A-Za-z0-9%|._-]*$`)
)

// mqName 校验必填的资源名（user / queue / policy / topic / tenant / namespace …）。
func mqName(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !mqNameRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, mqNameRe.String())
	}
	return s, nil
}

// mqNameAllowEmpty 校验可选的资源名：空串原样放行（调用方随后套默认值），
// 非空时口径与 mqName 完全一致。
func mqNameAllowEmpty(field, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return mqName(field, s)
}

// mqVersion 校验可选的版本号：空串放行（调用方随后套默认版本）。
func mqVersion(field, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !mqVersionRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, mqVersionRe.String())
	}
	return s, nil
}

// mqNode 校验单个节点名（必填）。
func mqNode(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !mqNodeRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, mqNodeRe.String())
	}
	return s, nil
}

// mqSplitList 把 s 切分成元素列表（逗号与空白都算分隔符）。
//
// 语义：先按逗号切成段，再对每段按空白做 Fields 切分；
// 某一段（比如 `h1,,h2` 里的中间段）切不出任何词 ⇒ 返回错误（空元素拒收 400）。
func mqSplitList(field, s string) ([]string, error) {
	tokens := strings.Split(s, ",")
	out := []string{}
	for _, tok := range tokens {
		words := strings.Fields(tok)
		if len(words) == 0 {
			return nil, fmt.Errorf("invalid %s: empty element", field)
		}
		out = append(out, words...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s is required", field)
	}
	return out, nil
}

// mqNodeList 校验节点列表，返回**以空格分隔**、可安全交给 `for node in %s` 的形式。
//
// # 为什么不能整体 Quote
//
// `for node in 'a b'` 只循环**一次**（node 的值是 `a b`）；而 `for node in a b` 循环两次。
// 旧脚本的意图就是后者。整体 Quote 会把列表压成一个词 ⇒ 功能静默失效。
//
// # 为什么逐项白名单就够了
//
// 输出里只有「白名单字符 + 单个空格」：没有 `$`、反引号、分号、引号、通配符、重定向
// ⇒ shell 的词拆分只会得到我们校验过的那些节点名，产不出新语法。
//
// 逗号与空白都视为分隔符：注释里写的是「逗号分隔」，但旧脚本同时接受空格分隔；
// 两种都收，避免能力回退。相邻分隔符（如 `h1,,h2`）中的空段直接拒收（400）。
func mqNodeList(field, s string) (string, error) {
	parts, err := mqSplitList(field, s)
	if err != nil {
		return "", err
	}
	for _, p := range parts {
		if _, err := mqNode(field, p); err != nil {
			return "", err
		}
	}
	return strings.Join(parts, " "), nil
}

// mqCSVList 校验逗号分隔的**单参数**列表（如 Pulsar `--allowed-clusters c1,c2`）。
//
// 与 mqNodeList 的区别：这里的列表整体是 CLI 的**一个参数**，逗号必须保留；故逐项
// 白名单后重新用逗号连接，调用点再整体 Quote。
func mqCSVList(field, s string) (string, error) {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return "", fmt.Errorf("invalid %s: empty element", field)
		}
		if _, err := mqName(field, p); err != nil {
			return "", err
		}
		out = append(out, p)
	}
	return strings.Join(out, ","), nil
}

// mqRocketTopic 校验 RocketMQ topic（放行 `%` 与 `|`，见 mqRocketTopicRe）。
//
// 返回的是**校验后的原值**：调用点必须再 shellquote.Quote 才能进脚本（`|` 是管道）。
func mqRocketTopic(field, s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if !mqRocketTopicRe.MatchString(s) {
		return "", fmt.Errorf("invalid %s %q: must match %s", field, s, mqRocketTopicRe.String())
	}
	return s, nil
}

// mqRocketTopicAllowEmpty 校验可选的 RocketMQ topic：空串原样放行（调用方随后套默认值
// 或走"不设置"分支），非空时口径与 mqRocketTopic 完全一致。
func mqRocketTopicAllowEmpty(field, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return mqRocketTopic(field, s)
}

// mqPath 校验**绝对路径**（备份文件 / TLS 证书）。
//
// 两处用途的威胁模型不同，但白名单口径可以共用：
//   - 备份路径进 `rabbitmqctl export_definitions %s` / `ls -lh %s`：要挡 shell 元字符
//     与选项注入（绝对路径以 `/` 开头，天然不会以 `-` 开头）；
//   - 证书路径进 heredoc 的**配置文件内容**：要挡换行（追加配置行）与能单独成行的
//     `EOF`（提前闭合 heredoc）。ValidatePath 拒绝换行与全部 shell 元字符，绝对路径
//     又不可能恰好等于 `EOF` ⇒ 两条都覆盖。
//
// 返回原值；备份路径在命令里还要 shellquote.Quote（本次改动里两者都做了）。
func mqPath(field, s string) (string, error) {
	if err := shellquote.ValidatePath(s); err != nil {
		return "", fmt.Errorf("invalid %s: %w", field, err)
	}
	return s, nil
}
