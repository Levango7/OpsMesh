package controlplane

// b17_injection_test.go — 批次 17 守卫（vector Metric/ID/Payload + kafka 集群名/Topics）。
//
// # 本批修的是什么
//
// vector_ops.go：
//   - req.Metric 进 curl 的 JSON 体（milvus 建集合的 metricType、qdrant 的 distance），
//     落点在 shell 单引号段内，此前**无任何校验** ⇒ `COSINE'; id; echo '` 可闭合引号并执行；
//   - req.ID 进 milvus 的 JSON 与 pgvector 的 `VALUES ('%s')`，此前无校验；
//   - req.Payload 原样进 curl 的 `-d '...'`（nonEmptyPayload 不转义）⇒ 单引号可闭合 shell 串。
//
// kafka_advanced_ops.go：
//   - req.SourceCluster / req.TargetCluster 进 MM2 的 bootstrap.servers，此前**仅非空校验**；
//   - req.Topics（逗号分隔）进 MM2 的 source->target.topics，此前无校验。
//
// # 守卫
//
// ① 实参级 AST 守卫（字段级）：本批 6 个字段不得作为脚本拼装的实参裸引用；
// ② 白名单拒收语义判据：6 类注入载荷必须 400 且不下发脚本。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// b17Files 是本批守卫覆盖的源文件。
var b17Files = []string{
	"vector_ops.go",
	"kafka_advanced_ops.go",
}

// b17Fields 是本批收口（白名单 + 局部变量承接）的字段。
var b17Fields = []string{
	"Metric", "ID", "Payload",
	"SourceCluster", "TargetCluster", "Topics",
}

// b17FieldRe 匹配本批字段的裸引用（req.<字段>）。
var b17FieldRe = regexp.MustCompile(`\breq\.(Metric|ID|Payload|SourceCluster|TargetCluster|Topics)\b`)

// b17BuilderTargets 是被认作「脚本 builder」的接收者形态。
var b17BuilderTargets = map[string]bool{
	"b":       true,
	"buf":     true,
	"sb":      true,
	"builder": true,
	"script":  true,
}

// TestB17NoRawReqInScriptArgs 判据①：脚本拼装的实参不得裸引用本批字段。
func TestB17NoRawReqInScriptArgs(t *testing.T) {
	for _, f := range b17Files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败：%v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var args []ast.Expr
			switch b9CallName(call.Fun) {
			case "fmt.Sprintf":
				if len(call.Args) < 2 {
					return true
				}
				args = call.Args[1:]
			case "fmt.Fprintf":
				if len(call.Args) < 3 || !b17IsBuilderTarget(call.Args[0]) {
					return true
				}
				args = call.Args[2:]
			default:
				return true
			}
			for _, a := range args {
				src := b9Src(fset, a)
				if m := b17FieldRe.FindString(src); m != "" {
					t.Errorf("%s:%d 脚本实参裸引用本批字段 %s：%s",
						f, fset.Position(a.Pos()).Line, m, src)
				}
			}
			return true
		})
	}
}

// b17IsBuilderTarget 判断 Fprintf 的接收者是否是脚本 builder。
func b17IsBuilderTarget(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			if id, ok := x.X.(*ast.Ident); ok {
				return b17BuilderTargets[id.Name]
			}
		}
	case *ast.Ident:
		return b17BuilderTargets[x.Name]
	}
	return false
}

// TestB17WhitelistRejects 判据②：6 类注入载荷必须 400。
func TestB17WhitelistRejects(t *testing.T) {
	s := newShellQuoteServer()

	cases := []struct {
		name string
		post func() *httptest.ResponseRecorder
	}{
		{"vector collection metric", func() *httptest.ResponseRecorder {
			return sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
				s.handleVectorCollection(w, r, "milvus")
			}, map[string]interface{}{
				"deviceID": "b17-vec", "action": "create", "collection": "c1",
				"dimension": 8, "metric": "COSINE'; id; echo '",
			})
		}},
		{"vector write id", func() *httptest.ResponseRecorder {
			return sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
				s.handleVectorWrite(w, r, "milvus")
			}, map[string]interface{}{
				"deviceID": "b17-vec", "collection": "c1",
				"id": "x'; id; echo '", "vector": []float64{0.1, 0.2},
			})
		}},
		{"vector write payload 非 JSON", func() *httptest.ResponseRecorder {
			return sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
				s.handleVectorWrite(w, r, "qdrant")
			}, map[string]interface{}{
				"deviceID": "b17-vec", "collection": "c1",
				"id": "p1", "vector": []float64{0.1}, "payload": "{'a':1}",
			})
		}},
		{"vector write payload 含单引号", func() *httptest.ResponseRecorder {
			return sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
				s.handleVectorWrite(w, r, "qdrant")
			}, map[string]interface{}{
				"deviceID": "b17-vec", "collection": "c1",
				"id": "p1", "vector": []float64{0.1}, "payload": `{"a":"'"}`,
			})
		}},
		{"kafka sourceCluster 注入", func() *httptest.ResponseRecorder {
			return sqPost(t, s, s.handleMirrorMaker, map[string]interface{}{
				"deviceID": "b17-kafka", "action": "start",
				"sourceCluster": "1.2.3.4; id", "targetCluster": "5.6.7.8:9092",
				"topics": "t1",
			})
		}},
		{"kafka topics 注入", func() *httptest.ResponseRecorder {
			return sqPost(t, s, s.handleMirrorMaker, map[string]interface{}{
				"deviceID": "b17-kafka", "action": "start",
				"sourceCluster": "1.2.3.4:9092", "targetCluster": "5.6.7.8:9092",
				"topics": "t1; id",
			})
		}},
	}
	for _, c := range cases {
		rec := c.post()
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：注入载荷应被拒（400），得到 %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// 断言：白名单只挡注入，不挡合法值（防止收口过头）。
func TestB17WhitelistAllowsLegitValues(t *testing.T) {
	s := newShellQuoteServer()
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorCollection(w, r, "milvus")
	}, map[string]interface{}{
		"deviceID": "b17-ok", "action": "create", "collection": "c1",
		"dimension": 8, "metric": "COSINE",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法 metric（COSINE）不应被拒：%d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "invalid metric") {
		t.Fatalf("合法 metric 被误判：%s", rec.Body.String())
	}
}
