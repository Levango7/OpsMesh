// Command eval 离线运行 AI Agent 评估集（issue #104）。
//
// 用途：改了 prompt / 工具描述 / 模型路由后，跑一遍看准确率是否下降。
//
// 模式：
//   - --selftest（默认）：用评估集的「期望答案」构造 mock 响应，验证评估管线本身
//     能跑通、报告正确、样本完整。它是管线自检，不衡量模型能力；
//   - 真实评估：通过环境变量 AI_EVAL_ENDPOINT 指向模型网关（扩展点，当前未接线，
//     明确报错而非静默降级为 mock，避免"假绿"）。
//
// 真实模型能力的量化由 internal/ai/eval 库 + testdata/eval-cases.json 评估集 +
// 单测（完美模型 100% / 错配降分）承载；接入在线 provider 后本 CLI 即可跑真实分。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Levango7/OpsMesh/internal/ai/eval"
	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

func main() {
	selftest := flag.Bool("selftest", true, "用评估集期望答案做管线自检（默认，零依赖）")
	casesFile := flag.String("cases", "eval-cases.json", "评估集文件名（嵌入 testdata）")
	threshold := flag.Float64("tool-f1", 1.0, "工具选择 F1 通过阈值")
	pointTh := flag.Float64("point-cov", 1.0, "要点覆盖率通过阈值")
	jsonOut := flag.Bool("json", false, "输出 JSON 报告")
	flag.Parse()

	set, err := eval.LoadCases(*casesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load cases:", err)
		os.Exit(2)
	}

	var complete eval.CompleteFn
	if *selftest {
		complete = selftestComplete(set)
	} else {
		fmt.Fprintln(os.Stderr, "eval: 真实 provider 尚未接线；请默认使用 --selftest。"+
			"接入点见 internal/ai/gateway.LLMProvider（AI_EVAL_ENDPOINT TODO）")
		os.Exit(2)
	}

	ev, err := eval.New(complete, eval.Options{ToolF1Threshold: *threshold, PointCovThreshold: *pointTh})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	rep, err := ev.Run(context.Background(), set)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printReport(rep)
	}
	if rep.Accuracy < 0.99 {
		// 评估未全过 → 非零退出，便于 CI 门禁（真实评估模式接上后同样生效）。
		os.Exit(1)
	}
}

func printReport(rep *eval.Report) {
	fmt.Printf("评估集 %s：%d/%d 通过（准确率 %.1f%%）\n", rep.Version, rep.Passed, rep.Total, rep.Accuracy*100)
	fmt.Printf("  平均工具 F1：%.3f  平均要点覆盖：%.3f\n", rep.AvgToolF1, rep.AvgPointCov)
	cats := make([]string, 0, len(rep.ByCategory))
	for c := range rep.ByCategory {
		cats = append(cats, c)
	}
	sortStrings(cats)
	for _, c := range cats {
		s := rep.ByCategory[c]
		fmt.Printf("  [%s] %d/%d (%.0f%%)\n", c, s.Passed, s.Total, s.Accuracy*100)
	}
	for _, r := range rep.Results {
		if !r.Pass {
			fmt.Printf("  FAIL %s: %s\n", r.CaseID, r.Reason)
		}
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// selftestComplete 用评估集期望答案构造响应：输入文本 → (期望工具, 期望要点)。
// 它只验证评估管线（加载→推理接口→提取→打分→报告）正确，不代表模型能力。
func selftestComplete(set eval.CaseSet) eval.CompleteFn {
	byInput := make(map[string]*gateway.CompletionResponse, len(set.Cases))
	for i := range set.Cases {
		c := &set.Cases[i]
		byInput[c.Input] = buildResponse(c.ExpectTools, strings.Join(c.ExpectPoints, " "))
	}
	return func(ctx context.Context, req gateway.CompletionRequest) (*gateway.CompletionResponse, error) {
		if len(req.Messages) == 0 {
			return buildResponse(nil, ""), nil
		}
		input := req.Messages[len(req.Messages)-1].Content
		if r, ok := byInput[input]; ok {
			return r, nil
		}
		return buildResponse(nil, ""), nil
	}
}

func buildResponse(tools []string, content string) *gateway.CompletionResponse {
	r := &gateway.CompletionResponse{}
	ch := struct {
		Index        int                `json:"index"`
		Message      gateway.Message    `json:"message"`
		ToolCalls    []gateway.ToolCall `json:"tool_calls,omitempty"`
		FinishReason string             `json:"finish_reason"`
	}{}
	ch.Message = gateway.Message{Role: gateway.RoleAssistant, Content: content}
	for _, name := range tools {
		tc := gateway.ToolCall{ID: "selftest-" + name, Type: "function"}
		tc.Function.Name = name
		ch.ToolCalls = append(ch.ToolCalls, tc)
	}
	r.Choices = append(r.Choices, ch)
	return r
}
