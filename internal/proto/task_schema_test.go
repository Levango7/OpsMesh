package proto

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TD-63: Task schema 多份定义的机器对账（根治后形态）。
//
// canonical: proto/opsmesh/task/v1/task.proto           (受 CI buf lint/breaking 守护)
// 服务副本:  services/task-svc/api/proto/v1/task.proto  (stub 生成与运行消费路径，与 canonical 逐字节锁定)
// model.go:  internal/proto/model.go                    (controlplane JSON codec 主契约，字段对账)
//
// 用例：字段数一致性、字段名双向对账、canonical↔副本字节相等（文件末尾）。
// 已知边界：类型/顺序层面的分叉不在判据内；model.go 保持手写（JSON 通道语义面不同），以对账锁死。

// taskProtoCanonicalRelPath 是 canonical Task 契约相对本测试包目录(internal/proto/)的路径。
const taskProtoCanonicalRelPath = "../../proto/opsmesh/task/v1/task.proto"

// taskProtoServiceCopyRelPath 是 task-svc 服务侧副本（stub 生成与运行消费路径），
// 与 canonical 必须逐字节一致（行尾归一后比较），由 TestTaskProtoCanonicalMatchesServiceCopy 锁定。
const taskProtoServiceCopyRelPath = "../../services/task-svc/api/proto/v1/task.proto"

// expectedTaskFields 是 Task 定义的期望字段数（手工维护）。
// 当任一侧 Task 增删字段时，须同步更新此值 + 两份定义。
const expectedTaskFields = 25

// TestTaskSchemaFieldCountConsistency 校验 model.go Task struct 与 task.proto Task message
// 的字段数一致，且与硬编码期望值一致。双重守门：
//   - 断言 1: model.go 字段数 == 硬编码期望值（防止两边同改但忘更新 expectedTaskFields）
//   - 断言 2: model.go 字段数 == task.proto 实际字段数（防止只改一边）
func TestTaskSchemaFieldCountConsistency(t *testing.T) {
	// 统计 model.go Task struct 字段数（反射）
	modelFields := reflect.TypeOf(Task{}).NumField()

	// 统计 task.proto Task message 实际字段数（文件解析）
	protoFields, err := countProtoTaskFields(taskProtoCanonicalRelPath)
	if err != nil {
		t.Fatalf("读取/解析 task.proto 失败 (%s): %v", taskProtoCanonicalRelPath, err)
	}

	t.Logf("Task schema 字段数: model.go=%d, canonical=%d, expected=%d",
		modelFields, protoFields, expectedTaskFields)

	// 断言 1: model.go 字段数 == 硬编码期望值
	if modelFields != expectedTaskFields {
		t.Errorf("model.go Task struct 字段数(%d) != 硬编码期望值(%d)。\n"+
			"⚠️ 演进须同步：若你修改了 Task 字段定义，请同步更新\n"+
			"expectedTaskFields 常量，并同步两份定义：\n"+
			"  - internal/proto/model.go Task struct\n"+
			"  - proto/opsmesh/task/v1/task.proto Task message（canonical）",
			modelFields, expectedTaskFields)
	}

	// 断言 2: model.go 字段数 == task.proto 实际字段数
	if modelFields != protoFields {
		t.Errorf("Task schema 字段数不一致: model.go Task struct has %d fields, "+
			"task.proto Task message has %d fields.\n"+
			"⚠️ 演进须同步两份定义：\n"+
			"  - proto/opsmesh/task/v1/task.proto Task message（canonical）\n"+
			"  - internal/proto/model.go Task struct",
			modelFields, protoFields)
	}
}

// TestTaskSchemaFieldNamesStayInSync 按**字段名**双向对账（TD-63 浅校验的补强）。
//
// 为什么还需要这一条：上面的字段数校验只能发现"一边多一边少"。
// 若两侧同时各增删一个字段（一侧加 X 删 Y），字段数仍然相等，浅校验直接放行，
// 而契约其实已经分叉了——这是字段数校验自己写在文件头的已知局限。
//
// 双向而不是单向：只查"proto 有而 Go 没有"的话，把 Go 侧字段删掉也能判绿；
// 两个方向的差集都要为空。
func TestTaskSchemaFieldNamesStayInSync(t *testing.T) {
	protoNames, err := protoTaskFieldNames(taskProtoCanonicalRelPath)
	if err != nil {
		t.Fatalf("读取/解析 task.proto 失败 (%s): %v", taskProtoCanonicalRelPath, err)
	}
	modelNames := modelTaskFieldNames()

	// 扫描面自检：任何一侧塌成空集都会让差集恒空，门禁空转判绿。
	if len(protoNames) < 20 || len(modelNames) < 20 {
		t.Fatalf("扫描面疑似塌陷：task.proto=%d 个字段, model.go=%d 个字段（应各 ≥20）",
			len(protoNames), len(modelNames))
	}

	var onlyInProto, onlyInModel []string
	for n := range protoNames {
		if _, ok := modelNames[n]; !ok {
			onlyInProto = append(onlyInProto, n)
		}
	}
	for n := range modelNames {
		if _, ok := protoNames[n]; !ok {
			onlyInModel = append(onlyInModel, n)
		}
	}
	sort.Strings(onlyInProto)
	sort.Strings(onlyInModel)

	if len(onlyInProto) > 0 || len(onlyInModel) > 0 {
		t.Errorf("Task schema 字段名分叉（已按 snake_case/camelCase 归一化比对）：\n"+
			"  仅存在于 task.proto: %v\n"+
			"  仅存在于 model.go:   %v\n"+
			"⚠️ 演进须同步两份定义：\n"+
			"  - proto/opsmesh/task/v1/task.proto Task message（canonical）\n"+
			"  - internal/proto/model.go Task struct",
			onlyInProto, onlyInModel)
	}
}

// protoTaskFieldNames 解析 task.proto，返回 Task message 的字段名集合（已归一化）。
func protoTaskFieldNames(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	body, err := taskProtoMessageBody(string(data))
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{})
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		// 取 '=' 前一个 token 作为字段名，兼容 `repeated string depends_on = 20;`
		// 与 `string task_id = 1;` 两种形态，也不受类型前缀影响。
		fields := strings.Fields(trimmed)
		for i, f := range fields {
			if f == "=" && i > 0 {
				out[normalizeSchemaName(fields[i-1])] = struct{}{}
				break
			}
		}
	}
	return out, nil
}

// modelTaskFieldNames 用反射取 Task struct 的 json tag 字段名集合（已归一化）。
func modelTaskFieldNames() map[string]struct{} {
	out := make(map[string]struct{})
	typ := reflect.TypeOf(Task{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out[normalizeSchemaName(name)] = struct{}{}
	}
	return out
}

// normalizeSchemaName 归一化字段名，消除 snake_case 与 camelCase 的差异：
// 转小写 + 去掉下划线（`claimed_at` 与 `claimedAt` → `claimedat`）。
func normalizeSchemaName(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", "")
}

// TestTaskSchemaCountingHelperStillParses 自检：字段名解析与字段数解析必须看到同一份数据，
// 防止将来有人改了一处解析而另一处悄悄塌陷。
func TestTaskSchemaCountingHelperStillParses(t *testing.T) {
	body, err := taskProtoMessageBody(readTaskProto(t))
	if err != nil {
		t.Fatalf("解析 task.proto: %v", err)
	}
	names, err := protoTaskFieldNames(taskProtoCanonicalRelPath)
	if err != nil {
		t.Fatalf("解析字段名: %v", err)
	}
	count, err := countProtoTaskFields(taskProtoCanonicalRelPath)
	if err != nil {
		t.Fatalf("统计字段数: %v", err)
	}
	if len(names) != count {
		t.Errorf("同一份 Task message 解析出 %d 个字段名却有 %d 个字段声明——两处解析逻辑已分叉",
			len(names), count)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("Task message 体为空——解析面塌陷")
	}
}

// readTaskProto 读取 task.proto 全文（供自检用例使用）。
func readTaskProto(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(taskProtoCanonicalRelPath)
	if err != nil {
		t.Fatalf("读 %s: %v", taskProtoCanonicalRelPath, err)
	}
	return string(b)
}

// taskProtoMessageBody 抽出 `message Task { ... }` 的 message 体（不含首尾花括号）。
func taskProtoMessageBody(content string) (string, error) {
	const taskMsgHeader = "message Task {"
	start := strings.Index(content, taskMsgHeader)
	if start == -1 {
		return "", fmt.Errorf("未找到 'message Task {'")
	}
	bodyStart := start + len(taskMsgHeader)
	depth := 1
	for i := bodyStart; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[bodyStart:i], nil
			}
		}
	}
	return "", fmt.Errorf("Task message 未闭合（花括号不匹配）")
}

// countProtoTaskFields 解析 task.proto 文件，统计 Task message 的字段数。
//
// 解析策略：
//  1. 定位 "message Task {" 起始位置
//  2. 用花括号深度计数找到 message 体结束位置
//  3. 在 message 体内统计字段声明行（匹配 `= <number>;` 且非注释行）
func countProtoTaskFields(path string) (int, error) {
	body, err := taskProtoMessageBody(readFileOrDie(path))
	if err != nil {
		return 0, err
	}

	// proto3 字段声明格式: `[repeated] <type> <name> = <number>;`
	// 匹配行内含 `= <数字>;` 且行首非注释。
	fieldLineRe := regexp.MustCompile(`^\s*\S.*=\s*\d+\s*;\s*$`)

	count := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		// 跳过空行和注释行
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if fieldLineRe.MatchString(line) {
			count++
		}
	}
	return count, nil
}

// readFileOrDie 读文件；失败时 panic，仅在无 *testing.T 的辅助路径使用。
// 正式用例请走 protoTaskFieldNames / taskProtoMessageBody 返回 error 的入口。
func readFileOrDie(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("读 %s: %v", path, err))
	}
	return string(b)
}

// TestTaskProtoCanonicalMatchesServiceCopy 锁定 canonical 与 task-svc 服务侧副本一致（TD-63 根治面）。
//
// 为什么按行尾归一后比较：本仓 .gitattributes 将 *.proto 固定为 LF，但 Windows 工作区
// 可能被外部工具以 CRLF 回写（实测发生过）；行尾差异不构成契约分叉，归一后比较才不假红。
func TestTaskProtoCanonicalMatchesServiceCopy(t *testing.T) {
	canonical, err := os.ReadFile(taskProtoCanonicalRelPath)
	if err != nil {
		t.Fatalf("读 canonical 失败 (%s): %v", taskProtoCanonicalRelPath, err)
	}
	serviceCopy, err := os.ReadFile(taskProtoServiceCopyRelPath)
	if err != nil {
		t.Fatalf("读服务侧副本失败 (%s): %v", taskProtoServiceCopyRelPath, err)
	}
	if len(canonical) == 0 || len(serviceCopy) == 0 {
		t.Fatalf("契约文件疑似塌陷为空：canonical=%d 字节, 副本=%d 字节", len(canonical), len(serviceCopy))
	}
	if !bytes.Equal(normalizeProtoEOL(canonical), normalizeProtoEOL(serviceCopy)) {
		t.Errorf("canonical 与服务侧副本已分叉（行尾归一后仍不一致）：\n"+
			"  canonical: %s\n"+
			"  副本:      %s\n"+
			"⚠️ TD-63：两份必须逐字节一致——改 canonical 后同步复制到副本（或反之），不要只动一份。",
			taskProtoCanonicalRelPath, taskProtoServiceCopyRelPath)
	}
}

// normalizeProtoEOL 归一 CRLF→LF，供字节相等断言使用（见用例注释）。
func normalizeProtoEOL(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}
