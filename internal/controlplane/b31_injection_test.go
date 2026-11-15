package controlplane

// b31_injection_test.go — 批次 31 守卫（vector_ops.go 形态收口）。
//
// vector_ops.go 7 个 builder：buildVectorDeployScript / buildVectorActionScript /
// buildVectorCollectionScript / buildVectorWriteScript / buildVectorSearchScript /
// buildVectorIndexScript / buildVectorMigrateScript
// 里 req.Type/req.Action/req.Collection/req.ID/req.Vector/req.Payload/req.TopK/req.Dimension/req.Metric/req.Replicas 直接作为脚本实参。
//
// 这些字段**在 handler 里都有白名单**：
//   - Type → allowedVectorTypes（milvus/qdrant/pgvector/chroma/weaviate/elasticsearch）
//   - Action → allowedVectorActions / allowedVectorCollectionActions / allowedVectorIndexActions / allowedVectorMigrateActions
//   - Collection → vectorNameRe（集合名标识符）
//   - Dimension / TopK / Replicas → handler 整数范围校验
//   - Metric → allowedVectorMetrics（cosine/euclidean/dotproduct/...）
//   - DeviceID → 仅进 createSystemTask 不进脚本
//
// 全形态面，无真洞。本批只做**局部变量承接** ⇒ 判据 E/F 归零；脚本文本逐字节不变。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestB31NoRawReqInScriptArgs 判据①。
func TestB31NoRawReqInScriptArgs(t *testing.T) {
	const f = "vector_ops.go"
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
			if len(call.Args) < 3 || !b26IsBuilderTarget(call.Args[0]) {
				return true
			}
			args = call.Args[2:]
		default:
			return true
		}
		for _, a := range args {
			src := b9Src(fset, a)
			if strings.Contains(src, "req.") {
				t.Errorf("%s:%d 脚本实参裸引用 req.*：%s", f, fset.Position(a.Pos()).Line, src)
			}
		}
		return true
	})
}

// TestB31WhitelistStillRejects 判据②：既有白名单不失效。
func TestB31WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// 未识别的 type 必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorAction(w, r, "milvus;id", "status")
	}, map[string]interface{}{
		"deviceID": "b31-vec-type",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("type 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}

	// 未识别的 action 必须被拒（400）。
	rec = sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorAction(w, r, "milvus", "start;id")
	}, map[string]interface{}{
		"deviceID": "b31-vec-act",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("action 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB31LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB31LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 action（status）不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleVectorAction(w, r, "milvus", "status")
	}, map[string]interface{}{
		"deviceID": "b31-vec-ok",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
