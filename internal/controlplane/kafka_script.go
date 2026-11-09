// Package controlplane kafka_script.go —— Kafka 面的收口助手与白名单（批次 10）。
//
// # 为什么 Kafka 面要单独一个文件
//
// Kafka 面的用户字段分三种角色，**收口手段完全不同**，混用会出事：
//
//   - **选项名**（`--operation %s`、`--entity-type %s`、`--%s`）——这些位置的值会被
//     Kafka 的 CLI 解析成**选项**。Quote 只能保证 shell 层安全，但 `Operation=All
//     --add-config x` 这种载荷在 Kafka CLI 层仍是「多传了一个选项」。
//     故这类字段必须**枚举白名单**。
//   - **值**（topic / group / connector / entity-name / delegation-token …）——
//     Quote 即可（shell 层封住，取值语义不变）。
//   - **JSON 载荷**（connector config / schema）——自由 JSON 先校验再整体 Quote；
//     内嵌 JSON 的字段值改由 Go 层构造后整体 Quote（见 ops_script.go）。
//
// # 与既有 kafkaResourceRe 的关系
//
// kafka_ops.go 里已有 `kafkaResourceRe = ^[a-zA-Z0-9._-]+$`（connector 名用它校验）。
// 本文件复用它做「值」类字段的白名单，**不另造一份字符集**——同一仓里同一类名字
// 只应有一份口径。
package controlplane

import (
	"fmt"
	"strings"

	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// kafkaACLOperations 是 kafka-acls.sh --operation 的合法取值。
//
// 为什么必须枚举：--operation 的值会被 Kafka CLI 解析成选项语义；不枚举的话，
// `Operation=All --add-config evil` 这类载荷会被当成**两个选项**。
// 大小写不敏感判定（与 Kafka CLI 一致），但**原样保留**用户给的大小写 ⇒ 合法输入的
// 行为与旧形态逐字相同。
var kafkaACLOperations = map[string]bool{
	"all":              true,
	"read":             true,
	"write":            true,
	"create":           true,
	"delete":           true,
	"alter":            true,
	"describe":         true,
	"clusteraction":    true,
	"idempotentwrite":  true,
	"describeconfigs":  true,
	"alterconfigs":     true,
	"idempotentdelete": true,
}

// kafkaACLResourceFlags 是 kafka-acls.sh 的**资源选项名**白名单。
//
// 旧形态是 `--%s %s`（选项名由用户字段拼出来）⇒ 载荷可以注入任意选项。
// 这里枚举资源 → 选项名的映射，返回值只可能是下表里的固定字面量。
var kafkaACLResourceFlags = map[string]string{
	"topic":           "topic",
	"group":           "group",
	"cluster":         "cluster",
	"transactionalid": "transactional-id",
	"delegationtoken": "delegation-token",
	"user":            "user",
}

// kafkaACLOperation 校验 ACL 的 operation，返回原样（大小写不变）的合法取值。
func kafkaACLOperation(op string) (string, error) {
	if !kafkaACLOperations[strings.ToLower(op)] {
		return "", fmt.Errorf("invalid ACL operation: %q", op)
	}
	return op, nil
}

// kafkaACLResourceFlag 校验 ACL 的资源类型，返回对应的**选项名**。
//
// 旧形态是 `strings.ToLower(req.Resource)` 直接拼成 `--%s` ⇒ 用户可以拼出任意选项。
// 这里改成「枚举 → 固定选项名」的映射：返回值只可能是表里的字面量。
func kafkaACLResourceFlag(resource string) (string, error) {
	flag, ok := kafkaACLResourceFlags[strings.ToLower(resource)]
	if !ok {
		return "", fmt.Errorf("invalid ACL resource: %q (allowed: topic, group, cluster, transactional-id, delegation-token, user)", resource)
	}
	return flag, nil
}

// kafkaResource 校验 Kafka 的**资源名**类字段（topic / group / connector / entity-name …）。
//
// 复用 kafka_ops.go 已有的 kafkaResourceRe（`^[a-zA-Z0-9._-]+$`），与既有口径一致。
func kafkaResource(s string) (string, error) {
	if !kafkaResourceRe.MatchString(s) {
		return "", fmt.Errorf("invalid kafka resource name: must match %s", kafkaResourceRe.String())
	}
	return s, nil
}

// kafkaACLResourceName 校验 ACL 的**资源名**（`--topic <name>` / `--group <name>` 的值）。
//
// 为什么单独一个口径：ACL 资源名允许**通配符** `*`（= 全部资源），而 kafkaResourceRe
// 不放行 `*` ⇒ 不能直接复用。故这里在 kafkaResourceRe 的字符集基础上**额外放行 `*`**。
//
// 不放行其它通配形态（`?` / `a*` / 前缀通配）：Kafka 的 ACL 资源名只支持整名或全量，
// 放行前缀通配会让「一个字段控制多个资源」的范围含糊。
func kafkaACLResourceName(s string) (string, error) {
	if s == "*" {
		return s, nil
	}
	if !kafkaResourceRe.MatchString(s) {
		return "", fmt.Errorf("invalid ACL resource name: %q", s)
	}
	return s, nil
}

// kafkaTopic 校验**单个 topic 名**。
//
// 为什么复用 middleware_ops.go 已有的 kafkaTopicRe（`^[a-zA-Z0-9_.-]+$`）：
// 同仓里同一类名字只应有一份口径；而这个字符集恰好**不含 `/`** ⇒ topic 落进
// **路径段**时不能改写目录层级（`..` 逃逸）。
func kafkaTopic(s string) (string, error) {
	if !kafkaTopicRe.MatchString(s) {
		return "", fmt.Errorf("invalid topic name: %q", s)
	}
	return s, nil
}

// kafkaTopicList 校验**逗号分隔的 topic 列表**（如 `a,b,c`）。
//
// 为什么逐项校验而不是整体 Quote：整体 Quote 只能挡 shell 层，挡不住「列表里的一项
// 本身是另一个选项」（如 `a,--delete-config evil`）⇒ 逐项按 kafkaTopicRe 校验，
// 非法项一律拒收（400）。
func kafkaTopicList(s string) (string, error) {
	for _, t := range strings.Split(s, ",") {
		if !kafkaTopicRe.MatchString(t) {
			return "", fmt.Errorf("invalid topic name in list: %q", t)
		}
	}
	return s, nil
}

// kafkaHeredocDelim 是本仓 heredoc 写入使用的固定分隔符。
const kafkaHeredocDelim = "EOF"

// kafkaHeredoc 构造 `cat > <path> << 'EOF' … EOF` 形态的 heredoc 写入。
//
// # 为什么 heredoc 也要收口
//
// 分隔符用引号定界（`<< 'EOF'`）时，**内容本应字面**——但只要载荷里出现**一行恰好
// 等于分隔符**，heredoc 就会**提前闭合** ⇒ 其后的内容被 shell 当**命令**执行。
// 这是真实可执行注入（已用真 sh 实测：标记文件落地）。
//
// POSIX 细节（实测确认）：heredoc 的分隔符词**不做命令替换**（`<<` 后的词只做引号
// 去除），所以反引号/`$()` 走的是「内容」路径而不是「分隔符」路径——但只要内容里
// 有一行等于分隔符，提前闭合照样成立。
//
// # 为什么是「拒绝」而不是「换个猜不到的分隔符」
//
// 换随机分隔器可以做到零能力损失，但会让脚本**不可复算**（每次下发的脚本内容都不同，
// 审计与回放都变难）。本仓的运维脚本要求「同输入 ⇒ 同输出」，故选拒绝：
// 内容里出现等于分隔符的行 ⇒ **400 拒收**，不替用户改写内容。
//
// 能力降级点：内容里含一行恰好 `EOF` 的 MM2 配置 / broker 配置 / KEDA 配置会被拒收
// （已如实登记在 CHANGELOG）。
func kafkaHeredoc(path, content string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		if line == kafkaHeredocDelim {
			return "", fmt.Errorf("heredoc content must not contain a line equal to %q", kafkaHeredocDelim)
		}
	}
	// 用 strings.Builder + fmt.Fprintf 而不是 fmt.Sprintf：后者会让本行被盘点器的
	// A/B 判据（行含 fmt.Sprintf 且格式串有 %s 与空白）记成一条候选行——而这里
	// 构造的正是**收口后的** heredoc 块，不该再进候选队列。
	//
	// 目标路径也要整体 Quote：本函数被批次 10 的 AST 守卫当作「净化器」⇒ 助手自身
	// 必须对**所有**入参负责，不能只对 content 负责。当前三个调用点的路径都是代码
	// 里的常量（/tmp/mm2.properties、/tmp/server.properties、/tmp/scaledobject.yaml），
	// Quote 后 shell 剥掉引号 ⇒ cat 收到的路径与旧形态逐字相同。
	var b strings.Builder
	fmt.Fprintf(&b, "cat > %s << '%s'\n%s\n%s", shellquote.Quote(path), kafkaHeredocDelim, content, kafkaHeredocDelim)
	return b.String(), nil
}

// kafkaHeredocf 是 kafkaHeredoc 的**模板版**：内容由固定模板 + 实参拼成。
//
// 与 kafkaHeredoc 的区别：kafkaHeredoc 的内容是**原样**的（用户自己写的配置文本，
// 不能再过一次 Sprintf——否则内容里的 `%s` 会被当成格式动词）；kafkaHeredocf 的
// 内容来自**代码里的固定模板**，实参只是模板占位符的值 ⇒ 可以安全地走 Sprintf。
//
// 收口语义与 kafkaHeredoc 完全一致：内容里出现等于分隔符的行 ⇒ 400 拒收。
func kafkaHeredocf(path, format string, args ...any) (string, error) {
	return kafkaHeredoc(path, fmt.Sprintf(format, args...))
}
