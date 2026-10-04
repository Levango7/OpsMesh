package main

// main_test.go 钉住 aio-svc 的"数据来源必须标注"这条对外口径（#61 造假面之三）。
//
// 判定面刻意选在 HTTP 响应这一层：client 内部知道自己是模拟的没用，
// 调用方（前端 / runbook / 别的微服务）只能看见 JSON。上一轮同类缺陷就是
// "函数正确但没人调用/没人看它的返回值"。

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Levango7/OpsMesh/services/aio-svc/internal/prometheus"
)

func TestWriteSamples_SimulatedWhenPrometheusMissing(t *testing.T) {
	c := prometheus.NewClient("", 0)
	w := httptest.NewRecorder()
	writeSamples(w, c, []prometheus.MetricSample{{Value: 0.42, Time: time.Now()}})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if resp["simulated"] != true {
		t.Fatalf("未配置 PROMETHEUS_URL 时必须标 simulated=true，实际 %v", resp)
	}
	if resp["source"] != prometheus.SourceSimulated {
		t.Fatalf("source = %v, want %q", resp["source"], prometheus.SourceSimulated)
	}
	// note 必须存在且说清"不是观测值"：只给一个布尔值，读的人照样会当成真数据。
	note, _ := resp["note"].(string)
	if note == "" {
		t.Fatalf("模拟来源必须带解释性 note，实际 body=%s", w.Body.String())
	}
	// samples 仍然在（形状不变），避免把标注做成破坏性变更。
	if arr, ok := resp["samples"].([]interface{}); !ok || len(arr) != 1 {
		t.Fatalf("samples 字段丢失或形状变了：%v", resp["samples"])
	}
}

func TestWriteSamples_RealWhenPrometheusHealthy(t *testing.T) {
	// 反向排除"恒标 simulated"：健康检查通过时必须是 prometheus/false，
	// 否则标注就成了不可信的噪音。
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/-/healthy" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Prometheus Server is Healthy.\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer prom.Close()

	c := prometheus.NewClient(prom.URL, time.Second)
	if !c.Available() {
		t.Fatal("前置条件不成立：假 Prometheus 应被判为可达")
	}
	w := httptest.NewRecorder()
	writeSamples(w, c, []prometheus.MetricSample{{Value: 0.1, Time: time.Now()}})

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["simulated"] != false {
		t.Fatalf("Prometheus 可达时不该标 simulated，实际 %v", resp)
	}
	if resp["source"] != prometheus.SourcePrometheus {
		t.Fatalf("source = %v, want %q", resp["source"], prometheus.SourcePrometheus)
	}
	if n, ok := resp["note"].(string); ok && n != "" {
		t.Fatalf("真实来源不该带模拟 note：%q", n)
	}
}

// TestNoUnlabeledSamplesResponse 是结构门禁：只要有人再写出裸的
// `{"samples": samples}`（就是本次修掉的形态），本测试立刻判红。
//
// 做法是解析 main.go 的 AST 而不是 grep 字符串——grep 会被注释里的例子误导，
// AST 只认真正的 map 字面量。
func TestNoUnlabeledSamplesResponse(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("读 main.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	var naked int
	ast.Inspect(file, func(n ast.Node) bool {
		ml, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// 只认 map[string]interface{}{...} 字面量。
		mt, ok := ml.Type.(*ast.MapType)
		if !ok || identName(mt.Key) != "string" || identName(mt.Value) != "interface{}" {
			return true
		}
		for _, e := range ml.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Value == strconv.Quote("samples") {
				naked++
			}
		}
		return true
	})
	if naked != 0 {
		t.Fatalf("main.go 里还有 %d 处裸 {\"samples\": ...} 响应（未带 source/simulated 标注）", naked)
	}

	// 四个取数路由必须都走 writeSamples。
	if got := countCall(file, "writeSamples"); got < 4 {
		t.Fatalf("writeSamples 调用点 = %d，期望至少 4（cpu/memory/disk/gpu 四条路由）", got)
	}
}

func identName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.Ident:
		return v.Name
	}
	return ""
}

// countCall 统计包内函数名被调用的次数。
func countCall(file *ast.File, fn string) int {
	n := 0
	ast.Inspect(file, func(node ast.Node) bool {
		ce, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := ce.Fun.(*ast.Ident); ok && id.Name == fn {
			n++
		}
		return true
	})
	return n
}
