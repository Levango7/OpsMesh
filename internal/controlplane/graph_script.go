// Package controlplane graph_script.go —— 图数据库面的收口助手与白名单（批次 10）。
//
// # 为什么 Graph 面要单独一个文件
//
// Graph 面的用户字段不是「拼进 shell 的一段文本」，而是**嵌进图数据库自己的查询语言**
// （Cypher / nGQL）里——这是**双层**结构：
//
//	cypher-shell -u neo4j -p password "CREATE USER %s SET PASSWORD '%s' ..." 2>/dev/null
//	             ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ shell 层
//	                                  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^ Cypher 层
//
// 只做 shell 层收口（把整条语句 Quote 起来）**挡不住图库层注入**：用户名 `x` SET
// PASSWORD 'p' WITH ROLE admin AS x` 之类的载荷在 Cypher 层照样能拼出第二条语句。
// 反过来只做图库层收口也挡不住 shell 层（字段里的 `"` 会闭掉 shell 的双引号段）。
//
// 故本文件按**字段在语句里的角色**分三档收口：
//
//   - **标识符**（user / index / name）→ 走 graphDatabaseRe 白名单（graph_ops.go 既有）
//     —— 它们在 Cypher 里是**裸标识符**，没有转义空间，只能限字符集。
//   - **schema 片段**（props）→ 走 graphPropDefsRe 白名单（类型名/逗号/方括号/空格）。
//   - **密钥**（password）→ 走 graphSecretRe 白名单。
//
// 全部通过后，再由 ops_script.go / shellquote.Quote 把**整条语句**做 shell 层收口。
//
// # 为什么密码用白名单而不用「Cypher 层转义 + shell 层 Quote」
//
// Cypher 与 nGQL 的字符串字面量转义规则**不完全一致**（`\'` 在两种方言里的支持情况
// 不同）。若某方言不认 `\'`，转义后的 `\'` 会被当作「反斜杠 + 字符串结束」⇒ 图库层
// 注入重新成立。故密码走**与方言无关**的字符集白名单：白名单里没有 `'`、反斜杠、控制
// 字符与任何 shell/Cypher 元字符 ⇒ 两层都不可能被突破。
//
// 代价（本批的**能力降级点**，如实登记在 CHANGELOG）：含引号/反斜杠/空白的口令会被
// 拒收（400），而不是被改写后写入。
package controlplane

import (
	"fmt"
	"regexp"

	"github.com/Levango7/OpsMesh/pkg/shellquote"
)

// graphPropDefsRe 校验图库的**属性定义片段**（如 `name string, age int`）。
//
// 允许：字母数字、下划线、空格、逗号、点、方括号（覆盖 `string[]` 这类列表类型）。
// 不允许：括号、分号、引号、美元号、反斜杠等一切能改变语句结构的字符。
var graphPropDefsRe = regexp.MustCompile(`^[A-Za-z0-9_ ,.\[\]]{0,256}$`)

// graphSecretRe 校验图库的**密钥/口令**。
//
// 与 graphPropDefsRe 同样的思路：只放行「密码里真的会用到、且在两层语法里都无特殊
// 含义」的字符。白名单里没有 `'`、`"`、“ ` “、`\`、`$`、`;`、`&`、`|`、`<`、`>`、
// `(`、`)`、`{`、`}`、空格与控制字符。
var graphSecretRe = regexp.MustCompile(`^[A-Za-z0-9._@#%+=:,/-]{0,128}$`)

// graphPathRe 校验图库的**备份/恢复路径**。
//
// 路径会同时进两处：shell 命令行（`neo4j-admin database dump --to-path=…`）与 nGQL 的
// 字符串字面量（`RESTORE FROM '…'`）⇒ 既要挡 shell 元字符，也要挡能闭合 nGQL 字符串
// 的单引号。故白名单里没有引号、反斜杠、美元号、分号等。
var graphPathRe = regexp.MustCompile(`^[A-Za-z0-9._/ -]{0,256}$`)

// graphCommandType 校验图库类型，并把它当作**命令名**使用时的唯一入口。
//
// 为什么要单独校验：graph_ext_ops.go 的 handleGraphUpgrade 把 req.Type 直接拼成
// `%s --version`——它要**当二进制执行** ⇒ 这是**命令注入**（比 shell 注入更严重：
// 载荷可以是一个任意二进制/脚本路径），Quote 挡不住（Quote 之后 `'x; id'` 仍是一个
// 合法参数，但作为命令名毫无意义）。
//
// 白名单直接复用 graph_ops.go 已有的 allowedGraphTypes（neo4j / nebulagraph）——
// 与同包 graph_ops.go 的既有行为对齐，不另造一份枚举。
func graphCommandType(t string) (string, error) {
	if !allowedGraphTypes[t] {
		return "", fmt.Errorf("unsupported graph type: %q (allowed: neo4j, nebulagraph)", t)
	}
	return t, nil
}

// graphIdent 校验图库里的**标识符**（用户名 / 索引名 / 标签名）。
//
// 标识符在 Cypher / nGQL 里是裸词，没有转义空间 ⇒ 只能限字符集。复用 graph_ops.go
// 的 graphDatabaseRe（`^[a-zA-Z0-9_]{1,64}$`），与同包既有行为一致。
func graphIdent(s string) (string, error) {
	if !graphDatabaseRe.MatchString(s) {
		return "", fmt.Errorf("invalid graph identifier: must match %s", graphDatabaseRe.String())
	}
	return s, nil
}

// graphPropDefs 校验属性定义片段（schema 片段）。
func graphPropDefs(s string) (string, error) {
	if !graphPropDefsRe.MatchString(s) {
		return "", fmt.Errorf("invalid property definitions: only letters, digits, underscore, space, comma, dot and [] are allowed")
	}
	return s, nil
}

// graphSecret 校验密钥/口令。
func graphSecret(s string) (string, error) {
	if !graphSecretRe.MatchString(s) {
		// 注意：错误消息里不能直接写 "% + = : ,"——这些是 fmt 的动词前缀，
		// fmt.Errorf 会把它们当成格式动词（govet printf 会判红）。故只描述
		// 「字符集受限」，具体字符集看 graphSecretRe 的定义。
		return "", fmt.Errorf("invalid secret: character set not allowed")
	}
	return s, nil
}

// graphPath 校验备份/恢复路径。
func graphPath(s string) (string, error) {
	if !graphPathRe.MatchString(s) {
		return "", fmt.Errorf("invalid path: only letters, digits and _ . / - and space are allowed")
	}
	return s, nil
}

// cypherShell 构造 cypher-shell 调用：整条语句作为**一个**参数传给 cypher-shell。
//
// 为什么语句要整体 Quote：语句里含图库层的单引号（如 `SET PASSWORD 'p'`），而 shell
// 双引号段内的 `'` 是**字面量**、但 `"`/`$`/反引号会被 shell 解释。整体 Quote 后
// shell 剥掉外层单引号，cypher-shell 收到的语句与旧形态**逐字相同** ⇒ 图库层语义
// 不变，shell 层却被彻底封住。
func cypherShell(stmt string) string {
	return "cypher-shell -u neo4j -p password " + shellquote.Quote(stmt)
}

// nebulaConsole 构造 nebula-console 调用：整条语句作为一个参数传给 nebula-console。
func nebulaConsole(stmt string) string {
	return "nebula-console -e " + shellquote.Quote(stmt)
}
