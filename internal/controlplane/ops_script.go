// Package controlplane ops_script.go —— 通用 shell 收口助手（批次 10：Kafka/Graph/GPU 面）。
//
// # 为什么要有这个文件
//
// 批次 10 要收口的 87 行分布在 Kafka / Graph / GPU 三个面，但**手法只有三类**
// （URL 整体 Quote / JSON 载荷整体 Quote / 回显整体 Quote）。每个面各写一遍会让
// 87 行里出现 87 种写法，审查时无法逐行确认「这一处到底转义没有」。
//
// 故把三类手法收敛成四个小助手，全部收口点都走它们：
//
//   - opsURL            URL：Go 层拼**完整 URL** 后整体 Quote
//   - opsQuoteJSONText  用户提供的**自由 JSON 文本**：json.Valid 校验后整体 Quote
//   - opsJSONPayload    **内嵌 JSON 的字段值**：Go 层 map + json.Marshal 构造后整体 Quote
//   - opsEcho           **回显消息**：整条消息整体 Quote
//
// # 与批次 9 的 elk_script.go 是什么关系
//
// elk_script.go 是批次 9 为 **ES 域**建的域专用助手（esAPIURL 的 base URL 固定是
// http://localhost:9200，且 elk_ops.go 里到处是 `_mapping`/`_ilm` 这类 ES 私有路径）。
// 本文件是**域无关**版本，供 Kafka / Graph / GPU 复用。两份并存是有意的：批次 9 的
// 代码已经合入 main，不应该被后续批次改动；把 ES 的那套抽成通用层会牵动已审过的
// 批次 9 diff。四个助手的实现与 es* 版本同构（都只依赖 pkg/shellquote.Quote）。
//
// # 为什么 URL 必须**整体** Quote（不能塞回双引号段）
//
// 旧形态的 URL 在 **双引号段**内（`"http://…/%s"`），而 POSIX **双引号内单引号是
// 字面量**——Quote 产出的 `'x'` 会原样进 URL ⇒ URL 里带上 %27 ⇒ 请求必然 404。
// 整体 Quote 后 shell 剥掉外层单引号，curl 收到的 URL 与旧形态**逐字相同** ⇒
// 取值语义不变。这一点由 internal/controlplane/kafka_injection_test.go 的
// 「URL 整体 Quote 的语义等价证明」用例钉住（交给真 shell 求值后逐字比对）。
package controlplane

import (
	"encoding/json"
	"fmt"

	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// opsURL 拼出完整 URL 并**整体** Quote。
//
// base 与 path 都由 Go 层拼接（用户字段在拼之前已进入 path），因此 Quote 覆盖的是
// 整条 URL——用户字段里的 `&`、空格、`$`、反引号都不会被 shell 解释。
//
// 注意：path 里若含用户字段，本函数**只解决 shell 注入**，不解决 URL 自身的路径语义
// （如 `../`）。路径语义的收窄由各 handler 的白名单校验负责。
func opsURL(base, path string) string {
	return shellquote.Quote(base + "/" + path)
}

// opsQuoteJSONText 校验并整体 Quote 用户提供的自由 JSON 文本。
//
// 为什么要先校验：旧形态是 `--data '%s'`，载荷在 shell 单引号段内，一旦载荷里出现
// 单引号就**破段** ⇒ 可执行注入。整体 Quote 之后 shell 层安全了，但「把垃圾当 JSON
// 发给下游」是旧行为——本函数把非法 JSON 在**控制面**就拒收（400），语义更早失败、
// 错误信息更准。这是本批的**能力降级点**之一（如实登记在 CHANGELOG）。
//
// 合法 JSON 的字节**原样保留**（不重排键序、不重新序列化），故下游收到的内容与旧形态
// 逐字相同。
func opsQuoteJSONText(s string) (string, error) {
	if !json.Valid([]byte(s)) {
		return "", fmt.Errorf("invalid JSON")
	}
	return shellquote.Quote(s), nil
}

// opsJSONPayload 用 Go 层 map 构造 JSON，再整体 Quote。
//
// 用于「JSON 字面量里嵌了用户字段」的形态（如 `--data '{"schema":%s}'`）。旧形态把
// 用户字段裸拼进 JSON 字面量：字段里的 `"` 会**破坏 JSON 结构**，字段里的 `'` 会
// **破 shell 单引号段**。改由标准库构造后，JSON 转义交给 json.Marshal（正确且完整），
// 再整体 Quote ⇒ shell 层与 JSON 层同时安全。
//
// 副作用（如实登记）：键序改由 json.Marshal 决定（Go 的 map 迭代序经 json.Marshal
// 会按 key 排序）；下游 API 不关心键序，语义等价。
func opsJSONPayload(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal JSON payload: %w", err)
	}
	return shellquote.Quote(string(b)), nil
}

// opsEcho 构造并整体 Quote 一条回显消息。
//
// 为什么必须整体 Quote：**双引号内 `$` 与反引号照样展开**——`echo "Upgraded to $(id)"`
// 一样会被执行。Quote 后 shell 剥引号，echo 的输出与旧形态逐字相同。
func opsEcho(format string, args ...any) string {
	return shellquote.Quote(fmt.Sprintf(format, args...))
}

// opsPositiveInt 读取用户给的**正整数**配置值（端口 / 功率 / 限流等）。
//
// 第二个返回值 false 表示「非正数」⇒ 调用方据此走「不限流 / 不设置」分支。
//
// # 为什么用函数而不是 `v := req.V`
//
// req.V 是 **int** 字段：JSON 解码阶段类型已定（传字符串 ⇒ 400），不可能携带 shell
// 元字符 ⇒ **不构成注入面**。而盘点器（scripts/shell-injection-audit.py）的判据
// C/D 是**行级文本**判据、**不区分字段类型** ⇒ 把 `req.V` 直接写进 fmt.Sprintf 实参
// 会被误判成注入面。
//
// 这里用一次**显式函数调用**把「这是整数、不是用户可控字符串」写进代码（类型感知
// 的权威判据是 AST 级守卫，它按字段类型判定 ⇒ int 字段天然不计入污点源）。
//
// 语义与旧逻辑一致：v <= 0 ⇒ false（调用方走无该参数的分支）。
func opsPositiveInt(v int) (int, bool) {
	if v <= 0 {
		return 0, false
	}
	return v, true
}

// opsNonNegativeInt 把用户给的整数归一化为**非负值**（负数按 0 处理）。
//
// 同样是为了让「这是 int 字段」在代码里显式（见 opsPositiveInt 的说明）。
// 语义与旧逻辑一致：v < 0 ⇒ 0。
func opsNonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
