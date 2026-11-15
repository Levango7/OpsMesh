package controlplane

// b30_injection_test.go — 批次 30 守卫（中间件族 *_middleware_ops.go 形态收口）。
//
// 覆盖 5 个文件 / 7 个 builder：
//   activemq_middleware_ops.go::buildActiveMQScript
//   kafka_middleware_ops.go::buildKafkaConsumerGroupScript / buildKafkaTopicScript
//   rabbitmq_middleware_ops.go::buildRabbitMQScript
//   redis_middleware_ops.go::buildRedisSentinelScript / buildRedisClusterScript
//   zookeeper_middleware_ops.go::buildZookeeperCmdScript
//
// 这些字段**在 handler 里都有白名单**：
//   - Type（activemq） / resource（rabbitmq 队列/交换机） → allowed 列表
//   - Action → allowed map（create/delete/list/monitor/failover/stat/ruok/...）
//   - Vhost/Queue/Topic → 名称正则
//   - Partitions/Replicas → handler 整数范围校验
//   - Host → 校验（回环/内网 IP 语义）
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

// TestB30NoRawReqInScriptArgs 判据①。
func TestB30NoRawReqInScriptArgs(t *testing.T) {
	files := []string{
		"activemq_middleware_ops.go",
		"kafka_middleware_ops.go",
		"rabbitmq_middleware_ops.go",
		"redis_middleware_ops.go",
		"zookeeper_middleware_ops.go",
	}
	for _, f := range files {
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
}

// TestB30WhitelistStillRejects 判据②：既有白名单不失效。
func TestB30WhitelistStillRejects(t *testing.T) {
	s := newShellQuoteServer()

	// action 注入载荷必须被拒（400）。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleZookeeperCmd(w, r)
	}, map[string]interface{}{
		"deviceID": "b30-zk", "cmd": "stat;id", "host": "127.0.0.1",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cmd 注入载荷应被拒（400），得到 %d %s", rec.Code, rec.Body.String())
	}
}

// TestB30LegitValuesNotBlocked 判据③：合法值不被误拒。
func TestB30LegitValuesNotBlocked(t *testing.T) {
	s := newShellQuoteServer()

	// 合法 ruok 不应被拒。
	rec := sqPost(t, s, func(w http.ResponseWriter, r *http.Request) {
		s.handleZookeeperCmd(w, r)
	}, map[string]interface{}{
		"deviceID": "b30-zk-ok", "cmd": "ruok", "host": "127.0.0.1",
	})
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("合法请求不应被拒：%d %s", rec.Code, rec.Body.String())
	}
}
