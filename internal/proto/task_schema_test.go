package proto

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TD-63: Task schema 双份定义字段数一致性浅校验。
//
// task.proto: services/task-svc/api/proto/v1/task.proto  (task-svc 独立 gRPC API)
// model.go:   internal/proto/model.go                    (controlplane JSON codec 主契约)
//
// 本测试是低成本守门，仅校验字段数一致，不校验字段名/类型/顺序。
// 已知局限：两边可能字段数相同但字段集不同（如一侧加 X 删 Y），浅校验无法发现。
// 根治方案见 docs/tech-debt.md TD-63（buf generate 单一来源，前置 TD-60 架构决策）。

// taskProtoRelPath 是 task.proto 相对于本测试包目录(internal/proto/)的路径。
const taskProtoRelPath = "../../services/task-svc/api/proto/v1/task.proto"

// expectedTaskFields 是 Task 定义的期望字段数（手工维护）。
// 当任一侧 Task 增删字段时，须同步更新此值 + 两份定义。
const expectedTaskFields = 24

// TestTaskSchemaFieldCountConsistency 校验 model.go Task struct 与 task.proto Task message
// 的字段数一致，且与硬编码期望值一致。双重守门：
//   - 断言 1: model.go 字段数 == 硬编码期望值（防止两边同改但忘更新 expectedTaskFields）
//   - 断言 2: model.go 字段数 == task.proto 实际字段数（防止只改一边）
func TestTaskSchemaFieldCountConsistency(t *testing.T) {
	// 统计 model.go Task struct 字段数（反射）
	modelFields := reflect.TypeOf(Task{}).NumField()

	// 统计 task.proto Task message 实际字段数（文件解析）
	protoFields, err := countProtoTaskFields(taskProtoRelPath)
	if err != nil {
		t.Fatalf("读取/解析 task.proto 失败 (%s): %v", taskProtoRelPath, err)
	}

	t.Logf("Task schema 字段数: model.go=%d, task.proto=%d, expected=%d",
		modelFields, protoFields, expectedTaskFields)

	// 断言 1: model.go 字段数 == 硬编码期望值
	if modelFields != expectedTaskFields {
		t.Errorf("model.go Task struct 字段数(%d) != 硬编码期望值(%d)。\n"+
			"⚠️ 演进须同步：若你修改了 Task 字段定义，请同步更新\n"+
			"expectedTaskFields 常量，并同步两份定义：\n"+
			"  - internal/proto/model.go Task struct\n"+
			"  - services/task-svc/api/proto/v1/task.proto Task message",
			modelFields, expectedTaskFields)
	}

	// 断言 2: model.go 字段数 == task.proto 实际字段数
	if modelFields != protoFields {
		t.Errorf("Task schema 字段数不一致: model.go Task struct has %d fields, "+
			"task.proto Task message has %d fields.\n"+
			"⚠️ 演进须同步两份定义：\n"+
			"  - services/task-svc/api/proto/v1/task.proto Task message\n"+
			"  - internal/proto/model.go Task struct",
			modelFields, protoFields)
	}
}

// countProtoTaskFields 解析 task.proto 文件，统计 Task message 的字段数。
//
// 解析策略：
//  1. 定位 "message Task {" 起始位置
//  2. 用花括号深度计数找到 message 体结束位置
//  3. 在 message 体内统计字段声明行（匹配 `= <number>;` 且非注释行）
func countProtoTaskFields(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(data)

	// 定位 "message Task {" 的位置
	const taskMsgHeader = "message Task {"
	taskMsgStart := strings.Index(content, taskMsgHeader)
	if taskMsgStart == -1 {
		return 0, fmt.Errorf("未找到 'message Task {'")
	}

	// 从 message Task { 之后开始，用花括号深度计数找到 message 体结束
	bodyStart := taskMsgStart + len(taskMsgHeader)
	depth := 1
	bodyEnd := -1
	for i := bodyStart; i < len(content) && bodyEnd == -1; i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				bodyEnd = i
			}
		}
	}
	if bodyEnd == -1 {
		return 0, fmt.Errorf("Task message 未闭合（花括号不匹配）")
	}

	body := content[bodyStart:bodyEnd]

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
