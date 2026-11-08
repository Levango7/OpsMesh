// Package controlplane elk_script.go 提供 Elasticsearch/ELK 运维面的 shell 脚本构造助手。
//
// # 为什么需要这一层
//
// ELK 面的脚本几乎全是 `curl <URL> -d '<JSON>'` 形态，而这两处恰好是**最危险**的两个位置：
//
//  1. URL：旧形态是 `curl "http://localhost:9200/%s"`——URL 在**双引号段**内。
//     双引号不是防护（POSIX 双引号内 `$` 与反引号同样会展开），所以 `$(id)` 会被执行。
//     但也不能把 shellquote.Quote 的结果直接塞回双引号段：Quote 产出 `'x'`，
//     而**双引号内单引号是字面量**——`'x'` 会变成 URL 里的 %27 ⇒ 请求必然 404。
//     正确做法只有一条：**在 Go 层拼出完整 URL，再对整个 URL 做 Quote**。
//     Quote 后 shell 剥掉外层单引号，curl 收到的 URL 与旧形态逐字相同 ⇒ 取值语义不变。
//     另有旧形态的 URL **根本没有引号**（如 `_ilm/policy/%s?pretty`），
//     `&`（后台执行符）与空格会被 shell 直接解释 ⇒ 同样是可执行注入。
//
//  2. JSON 载荷：旧形态是 `-d '%s'`，payload 在**单引号段**内。payload 里出现一个单引号即
//     **破段**（`'{"a":"b'}'; touch /tmp/x; echo ”`）⇒ 可执行注入。
//     正确做法：用户给的自由 JSON 先校验合法性，再**整体 Quote**（Quote 会按
//     关闭-转义-重开还原单引号，payload 字节不变）；嵌在 JSON 字面量里的字段值则改由
//     Go 层 map + json.Marshal 构造（JSON 转义交给标准库，顺带修掉旧形态的
//     `"` 破坏 JSON 的问题）。
//
// # 三个易错点（本文件的存在理由）
//
//   - 不要在双引号段内放 Quote 的结果：单引号会变成字面量。
//   - 不要用 Quote 包多词形态：会把多个词压成一个词，改掉命令语义
//     （那种形态用 shellquote.ValidateArgList 白名单，见批次 8）。
//   - 回显段 `echo "…%s…"` 里的值同样要防：双引号内 `$`/反引号会展开。
package controlplane

import (
	"encoding/json"
	"fmt"

	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// esBaseURL 是本仓约定的 ES/OpenSearch 本地端点。
const esBaseURL = "http://localhost:9200"

// esAPIURL 拼出 ES API 的完整 URL 并整体 Quote 成 POSIX 单引号字面量。
//
// path 是**相对路径**（如 "myindex/_mapping?pretty"），可含多个路径段与查询串。
// 调用方负责在 Go 层把用户字段拼进 path（例如 fmt.Sprintf("_snapshot/%s/%s", repo, snap)），
// 本函数只负责把整条 URL 安全地交给 shell。
//
// 语义说明：Quote 只影响 shell 层的字面量边界，shell 求值后 curl 收到的 URL
// 与「直接拼接」的旧形态**逐字相同**——即路径段里若含 `/`、`?` 等字符，
// 其对 URL 结构的改变与旧形态一致（本函数不新增、也不消除这类语义）。
func esAPIURL(path string) string {
	return shellquote.Quote(esBaseURL + "/" + path)
}

// esQuoteJSONText 校验用户提供的 JSON 文本合法，再整体 Quote 成单引号字面量。
//
// 为什么先校验：`-d` 的参数必须是合法 JSON 才有意义；旧形态是把垃圾原样发给 ES，
// 由 ES 自己报错。收口后改为**在控制面就 400 拒收**（本批的能力变化，已在 CHANGELOG 登记）。
//
// 为什么保留原字节：合法 JSON 的字节序列由用户给定，整体 Quote 只加 shell 字面量边界，
// 不改动 payload 内部内容（键序、空白、数字格式都保持用户原样）。
func esQuoteJSONText(s string) (string, error) {
	if !json.Valid([]byte(s)) {
		return "", fmt.Errorf("payload is not valid JSON")
	}
	return shellquote.Quote(s), nil
}

// esJSONPayload 用 Go 结构构造 JSON 载荷，再整体 Quote 成单引号字面量。
//
// 用于「值嵌在 JSON 字面量里」的场景（alias 的 index/alias、snapshot 的 location/indices、
// reindex 的 source/dest、CCR 的 remote/leader、create-user 的 password/roles）：
// 由标准库负责 JSON 转义，用户字段里的 `"`/`\`/控制字符不再破坏 JSON 结构。
//
// raw 字段（如 roles 数组文本）用 json.RawMessage 承载：校验通过后原样嵌入，
// 仅做 JSON compact（去多余空白），语义等价。
func esJSONPayload(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	return shellquote.Quote(string(b)), nil
}

// esEcho 生成一条回显消息：整体 Quote 后作为 `echo` 的唯一参数。
//
// 为什么不用 `echo "…%s…"`：双引号段内 `$` 与反引号会展开（`echo "x $(id)"` 会被执行）。
// 整体 Quote 后 shell 剥掉外层单引号，**输出内容与原写法一致**。
func esEcho(msg string) string {
	return "echo " + shellquote.Quote(msg)
}
