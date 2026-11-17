// Package eval 提供 AI Agent 的离线评估集与评估器。
//
// 设计目标（issue #104）：让 AI 能力可量化、可迭代——
//   - 评估集是可版本化的 JSON 样本（testdata/eval-cases.json）；
//   - 评估器只依赖可注入的 CompleteFn，不调在线模型，CI 可跑；
//   - 指标覆盖「工具选择」（该查指标时是否真调 query_metrics）
//     与「答案要点」（结论是否含关键事实）。
//
// 这不是在线推理链路的一部分，而是回归门禁：改了 prompt / 工具描述 / 模型路由后
// 跑一遍评估集，看分数是否下降。
package eval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// Case 一条评估样本。
//
// ExpectTools 列出「理想情况下应调用的工具名」（对齐 agent 注册的工具名：
// query_device / query_metrics / execute_command / search_knowledge /
// web_search / create_ticket）。留空表示不考核工具选择。
// ExpectPoints 是答案中应出现的要点关键词（大小写不敏感的子串匹配）。
type Case struct {
	ID           string   `json:"id"`
	Category     string   `json:"category"` // alert / rootcause / remediation / capacity / nlquery
	Input        string   `json:"input"`    // 用户问题或告警文本
	ExpectTools  []string `json:"expect_tools"`
	ExpectPoints []string `json:"expect_points"`
}

// CaseSet 评估集（带元信息，便于版本追踪）。
type CaseSet struct {
	Version string `json:"version"`
	Cases   []Case `json:"cases"`
}

// CaseResult 单条样本的评估结果。
type CaseResult struct {
	CaseID        string
	ToolHit       bool    // 期望工具是否全部被命中
	ToolF1        float64 // 工具选择的 F1（精确率/召回率调和）
	PointCoverage float64 // 要点覆盖率
	Pass          bool    // 是否通过（F1 与覆盖率均达阈值）
	Reason        string  // 未通过原因（便于定位）
}

// Report 汇总报告。
type Report struct {
	Version     string
	Total       int
	Passed      int
	Accuracy    float64 // Passed / Total
	AvgToolF1   float64
	AvgPointCov float64
	ByCategory  map[string]CategoryStat
	Results     []CaseResult
}

// CategoryStat 分类维度统计。
type CategoryStat struct {
	Total    int
	Passed   int
	Accuracy float64
}

// CompleteFn 与 gateway.LLMProvider.Complete 同签名的函数类型，便于注入 mock。
type CompleteFn func(ctx context.Context, req gateway.CompletionRequest) (*gateway.CompletionResponse, error)

// Options 评估器配置。
type Options struct {
	// ToolF1Threshold 工具选择 F1 通过阈值（默认 1.0，即期望工具必须全部命中且无多余）。
	ToolF1Threshold float64
	// PointCovThreshold 要点覆盖率通过阈值（默认 1.0）。
	PointCovThreshold float64
}

// Evaluator 离线评估器。
type Evaluator struct {
	complete CompleteFn
	opts     Options
}

// New 构造评估器。complete 为 nil 时返回错误（避免空跑假绿）。
func New(complete CompleteFn, opts Options) (*Evaluator, error) {
	if complete == nil {
		return nil, fmt.Errorf("eval: CompleteFn is required")
	}
	if opts.ToolF1Threshold <= 0 {
		opts.ToolF1Threshold = 1.0
	}
	if opts.PointCovThreshold <= 0 {
		opts.PointCovThreshold = 1.0
	}
	return &Evaluator{complete: complete, opts: opts}, nil
}

// Run 跑完整评估集，返回汇总报告。空集返回错误。
func (e *Evaluator) Run(ctx context.Context, set CaseSet) (*Report, error) {
	if len(set.Cases) == 0 {
		return nil, fmt.Errorf("eval: empty case set")
	}
	rep := &Report{
		Version:    set.Version,
		Total:      len(set.Cases),
		ByCategory: make(map[string]CategoryStat),
		Results:    make([]CaseResult, 0, len(set.Cases)),
	}
	var sumF1, sumCov float64
	for _, c := range set.Cases {
		if strings.TrimSpace(c.ID) == "" {
			return nil, fmt.Errorf("eval: case with empty ID")
		}
		res, err := e.evalCase(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("eval case %s: %w", c.ID, err)
		}
		rep.Results = append(rep.Results, res)
		sumF1 += res.ToolF1
		sumCov += res.PointCoverage
		if res.Pass {
			rep.Passed++
		}
		cat := c.Category
		if cat == "" {
			cat = "uncategorized"
		}
		cs := rep.ByCategory[cat]
		cs.Total++
		if res.Pass {
			cs.Passed++
		}
		rep.ByCategory[cat] = cs
	}
	rep.Accuracy = float64(rep.Passed) / float64(rep.Total)
	rep.AvgToolF1 = sumF1 / float64(rep.Total)
	rep.AvgPointCov = sumCov / float64(rep.Total)
	for cat, cs := range rep.ByCategory {
		cs.Accuracy = float64(cs.Passed) / float64(cs.Total)
		rep.ByCategory[cat] = cs
	}
	// 稳定排序：按 ID，便于 diff。
	sort.Slice(rep.Results, func(i, j int) bool { return rep.Results[i].CaseID < rep.Results[j].CaseID })
	return rep, nil
}

func (e *Evaluator) evalCase(ctx context.Context, c Case) (CaseResult, error) {
	req := gateway.CompletionRequest{
		Messages: []gateway.Message{
			{Role: gateway.RoleSystem, Content: "你是运维诊断 Agent。根据输入判断应调用哪些工具，并给出结论。"},
			{Role: gateway.RoleUser, Content: c.Input},
		},
	}
	resp, err := e.complete(ctx, req)
	if err != nil {
		return CaseResult{CaseID: c.ID}, err
	}
	// 提取命中工具名。
	gotTools := make(map[string]bool)
	content := ""
	if resp != nil {
		for _, ch := range resp.Choices {
			content += " " + ch.Message.Content
			for _, tc := range ch.ToolCalls {
				gotTools[tc.Function.Name] = true
			}
		}
	}

	// 工具选择 F1。
	var f1 float64
	if len(c.ExpectTools) > 0 {
		hit := 0
		for _, want := range c.ExpectTools {
			if gotTools[want] {
				hit++
			}
		}
		precision := 0.0
		if len(gotTools) > 0 {
			precision = float64(hit) / float64(len(gotTools))
		}
		recall := float64(hit) / float64(len(c.ExpectTools))
		if precision+recall > 0 {
			f1 = 2 * precision * recall / (precision + recall)
		}
	} else {
		f1 = 1.0 // 不考核工具
	}

	// 要点覆盖率。
	var cov float64
	if len(c.ExpectPoints) > 0 {
		low := strings.ToLower(content)
		hit := 0
		for _, p := range c.ExpectPoints {
			if p != "" && strings.Contains(low, strings.ToLower(p)) {
				hit++
			}
		}
		cov = float64(hit) / float64(len(c.ExpectPoints))
	} else {
		cov = 1.0
	}

	pass := f1 >= e.opts.ToolF1Threshold && cov >= e.opts.PointCovThreshold
	res := CaseResult{
		CaseID: c.ID, ToolHit: f1 >= 1.0, ToolF1: f1,
		PointCoverage: cov, Pass: pass,
	}
	if !pass {
		var reasons []string
		if f1 < e.opts.ToolF1Threshold {
			reasons = append(reasons, fmt.Sprintf("tool F1 %.2f < %.2f", f1, e.opts.ToolF1Threshold))
		}
		if cov < e.opts.PointCovThreshold {
			reasons = append(reasons, fmt.Sprintf("point coverage %.2f < %.2f", cov, e.opts.PointCovThreshold))
		}
		res.Reason = strings.Join(reasons, "; ")
	}
	return res, nil
}
