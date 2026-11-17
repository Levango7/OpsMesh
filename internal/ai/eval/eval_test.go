package eval

import (
	"context"
	"errors"
	"strings"
	"testing"

	gateway "github.com/Levango7/OpsMesh/internal/ai/gateway"
)

// respWith 构造一个含指定工具调用与内容的 mock 响应。
func respWith(tools []string, content string) *gateway.CompletionResponse {
	r := &gateway.CompletionResponse{}
	ch := struct {
		Index        int                `json:"index"`
		Message      gateway.Message    `json:"message"`
		ToolCalls    []gateway.ToolCall `json:"tool_calls,omitempty"`
		FinishReason string             `json:"finish_reason"`
	}{}
	ch.Message = gateway.Message{Role: gateway.RoleAssistant, Content: content}
	for _, name := range tools {
		tc := gateway.ToolCall{ID: "call-" + name, Type: "function"}
		tc.Function.Name = name
		ch.ToolCalls = append(ch.ToolCalls, tc)
	}
	r.Choices = append(r.Choices, ch)
	return r
}

func TestLoadCases(t *testing.T) {
	set, err := LoadCases("eval-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) < 30 {
		t.Fatalf("expected >=30 cases, got %d", len(set.Cases))
	}
	if set.Version == "" || set.Version == "unversioned" {
		t.Errorf("version not set: %q", set.Version)
	}
	// 每条样本字段完整。
	seen := map[string]bool{}
	for _, c := range set.Cases {
		if c.ID == "" || c.Input == "" {
			t.Errorf("incomplete case: %+v", c)
		}
		if seen[c.ID] {
			t.Errorf("duplicate case ID: %s", c.ID)
		}
		seen[c.ID] = true
		if len(c.ExpectTools) == 0 && len(c.ExpectPoints) == 0 {
			t.Errorf("case %s has no expectations", c.ID)
		}
	}
}

// perfectComplete 是"完美模型"：按 case 的 input 注入对应工具与要点。
// 测试里用 case 映射，而不是真的 LLM。
func completeFromMap(m map[string]*gateway.CompletionResponse, fallback *gateway.CompletionResponse) CompleteFn {
	return func(ctx context.Context, req gateway.CompletionRequest) (*gateway.CompletionResponse, error) {
		if len(req.Messages) == 0 {
			return fallback, nil
		}
		input := req.Messages[len(req.Messages)-1].Content
		if r, ok := m[input]; ok {
			return r, nil
		}
		return fallback, nil
	}
}

func TestEvaluatorPerfectScore(t *testing.T) {
	set, _ := LoadCases("eval-cases.json")
	m := map[string]*gateway.CompletionResponse{}
	for _, c := range set.Cases {
		m[c.Input] = respWith(c.ExpectTools, strings.Join(c.ExpectPoints, " "))
	}
	ev, err := New(completeFromMap(m, respWith(nil, "")), Options{})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ev.Run(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != len(set.Cases) {
		t.Errorf("Total = %d want %d", rep.Total, len(set.Cases))
	}
	if rep.Accuracy < 0.999 {
		t.Errorf("perfect model accuracy = %.2f want 1.0", rep.Accuracy)
	}
	if rep.AvgToolF1 < 0.999 || rep.AvgPointCov < 0.999 {
		t.Errorf("perfect model F1=%.2f cov=%.2f", rep.AvgToolF1, rep.AvgPointCov)
	}
	if len(rep.ByCategory) < 5 {
		t.Errorf("expected >=5 categories, got %d", len(rep.ByCategory))
	}
}

func TestEvaluatorMutationWrongToolsLowersScore(t *testing.T) {
	set, _ := LoadCases("eval-cases.json")
	m := map[string]*gateway.CompletionResponse{}
	// 变异：所有 case 都返回错误的工具，且内容缺要点。
	for _, c := range set.Cases {
		m[c.Input] = respWith([]string{"web_search"}, "无法回答")
	}
	ev, _ := New(completeFromMap(m, respWith([]string{"web_search"}, "无法回答")), Options{})
	rep, err := ev.Run(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Accuracy > 0.3 {
		t.Errorf("mutated (wrong tools) accuracy = %.2f want <=0.30", rep.Accuracy)
	}
}

func TestEvaluatorEmptySetRejected(t *testing.T) {
	ev, _ := New(func(context.Context, gateway.CompletionRequest) (*gateway.CompletionResponse, error) {
		return respWith(nil, ""), nil
	}, Options{})
	if _, err := ev.Run(context.Background(), CaseSet{}); err == nil {
		t.Error("empty case set must be rejected")
	}
}

func TestEvaluatorNilCompleteRejected(t *testing.T) {
	if _, err := New(nil, Options{}); err == nil {
		t.Error("nil CompleteFn must be rejected")
	}
}

func TestEvaluatorCompleteErrorPropagates(t *testing.T) {
	ev, _ := New(func(context.Context, gateway.CompletionRequest) (*gateway.CompletionResponse, error) {
		return nil, errors.New("boom")
	}, Options{})
	set := CaseSet{Version: "t", Cases: []Case{{ID: "x", Input: "q", ExpectPoints: []string{"a"}}}}
	if _, err := ev.Run(context.Background(), set); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("complete error not propagated: %v", err)
	}
}

func TestEvaluatorReasonOnFailure(t *testing.T) {
	ev, _ := New(func(context.Context, gateway.CompletionRequest) (*gateway.CompletionResponse, error) {
		return respWith(nil, "无关内容"), nil
	}, Options{})
	set := CaseSet{Version: "t", Cases: []Case{{
		ID: "fail-1", Input: "q", ExpectTools: []string{"query_metrics"}, ExpectPoints: []string{"CPU"},
	}}}
	rep, err := ev.Run(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 0 || len(rep.Results) != 1 {
		t.Fatalf("expected 0 passed, got %+v", rep)
	}
	if rep.Results[0].Reason == "" {
		t.Error("failure reason should be set")
	}
}
