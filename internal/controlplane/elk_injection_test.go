package controlplane

// elk_injection_test.go — 批次 9（ELK/Elasticsearch 运维面「req.* 未转义直插」收口）的注入守卫。
//
// 覆盖范围：internal/controlplane/elk_ops.go（27 处脚本行）+ elk_ext_ops.go（26 处脚本行）。
//
// 为什么是真缺陷面：这些 handler 把用户可控字段裸拼进要下发到 agent 的 shell 脚本，agent 以
// `sh -c` 执行 ⇒ `$(id)` / 反引号 / 分号被**真实执行**。鉴权面无缺口（均经 dispatchKafkaTask
// → subsystemAuthorize 的 task:write 闸）⇒ 已授权调用者可直接打到 agent 的 RCE。
//
// 本批的三个易错点（守卫专门盯这三条）：
//
//	① **URL 不能把 Quote 结果塞回双引号段**：旧形态 URL 在 `"http://…/%s"` 里，双引号内单引号
//	  是**字面量**——Quote 产出的 `'x'` 会变成 URL 里的 %27 ⇒ 请求必然 404。故正确做法是
//	  Go 层拼完整 URL 再**整体** Quote。本文件用两条断言钉住：①a 脚本里不得再出现
//	  `"http://localhost:9200/` 形态；①b 真 shell 求值后 URL 与原 URL **逐字相同**。
//	② **自由 JSON 载荷必须先校验再整体 Quote**：旧形态 `-d '%s'` 在单引号段内，payload 里出现
//	  单引号即**破段** ⇒ 可执行注入；且 payload 非法 JSON 时旧形态是把垃圾发给 ES 由 ES 报错，
//	  收口后改为控制面 400（能力变化，已在 CHANGELOG 登记）。
//	③ **回显段也要防**：`echo "…%s…"` 的双引号段内 `$` 与反引号同样会展开。
//
// 载荷形态说明（沿用批次 7 的教训）：
//   - 自由 JSON 字段（body / roles）的载荷必须是**合法 JSON**（否则会被新的 400 校验拦下，
//     测不到注入面）——故用 `{"a":"…"}` 包裹。
//   - 语义判据用**语法合法**的载荷：URL 类用 `$(touch marker)`（旧形态在双引号段内会真实执行
//     命令替换）；logstash config 类用配对单引号的 `x'; touch marker; echo '`（旧形态
//     `echo '%s'` 遇配对引号即破段执行）。带未闭合单引号的载荷会让整段脚本语法错误 ⇒ fail-closed，
//     不构成可执行注入的证明。

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// b9Marker 是本批语义判据的副作用标记文件名（放在临时目录里）。
const b9MarkerName = "pwned"

// b9PayloadAny 用于 URL / 标识符类字段（载荷原样进入 URL 路径段或回显文本）。
var b9PayloadAny = []string{
	`$(id)`,
	"`id`",
	`x; touch /tmp/opsmesh-b9-pwned`,
	`x'; touch /tmp/opsmesh-b9-pwned; echo '`,
	`"; touch /tmp/opsmesh-b9-pwned; echo "`,
}

// b9PayloadJSON 用于自由 JSON 字段：载荷必须是合法 JSON，否则会被 400 校验拦下。
var b9PayloadJSON = []string{
	`{"a":"$(id)"}`,
	`{"a":"x'; touch /tmp/opsmesh-b9-pwned; echo '"}`,
	`{"a":"` + "`id`" + `"}`,
	`{"a":"\"; touch /tmp/opsmesh-b9-pwned; echo \""}`,
}

// b9PayloadText 用于非 JSON 的自由文本字段（logstash pipeline 的 config）。
var b9PayloadText = []string{
	`x'; touch /tmp/opsmesh-b9-pwned; echo '`,
	`$(touch /tmp/opsmesh-b9-pwned)`,
}

// b9Case 覆盖一个 handler × 注入字段组合。
type b9Case struct {
	name    string
	handler http.HandlerFunc
	body    map[string]interface{}
	field   string
	kind    string // "any" | "json" | "text"
}

// b9Cases 列出本批的全部注入面。
func b9Cases(s *Server) []b9Case {
	return []b9Case{
		// ---- elk_ops.go: 部署 / 升级 ----
		{"deploy/version", s.handleElasticsearchDeploy, map[string]interface{}{"mode": "single", "version": "8.13.0"}, "version", "any"},
		{"deploy/memLimit", s.handleElasticsearchDeploy, map[string]interface{}{"mode": "single", "version": "8.13.0", "memLimit": "512m"}, "memLimit", "any"},
		{"upgrade/targetVersion", s.handleElasticsearchUpgrade, map[string]interface{}{"action": "upgrade"}, "targetVersion", "any"},

		// ---- elk_ops.go: index ----
		{"index/create-index", s.handleElasticsearchIndex, map[string]interface{}{"action": "create", "index": "i1"}, "index", "any"},
		{"index/create-body", s.handleElasticsearchIndex, map[string]interface{}{"action": "create", "index": "i1"}, "body", "json"},
		{"index/delete-index", s.handleElasticsearchIndex, map[string]interface{}{"action": "delete", "index": "i1"}, "index", "any"},
		{"index/mapping-index", s.handleElasticsearchIndex, map[string]interface{}{"action": "mapping", "index": "i1"}, "index", "any"},
		{"index/settings-index", s.handleElasticsearchIndex, map[string]interface{}{"action": "settings", "index": "i1"}, "index", "any"},

		// ---- elk_ops.go: ILM ----
		{"ilm/get-policy", s.handleElasticsearchILM, map[string]interface{}{"action": "get", "policy": "p1"}, "policy", "any"},
		{"ilm/create-policy", s.handleElasticsearchILM, map[string]interface{}{"action": "create", "policy": "p1", "body": `{}`}, "policy", "any"},
		{"ilm/create-body", s.handleElasticsearchILM, map[string]interface{}{"action": "create", "policy": "p1"}, "body", "json"},
		{"ilm/delete-policy", s.handleElasticsearchILM, map[string]interface{}{"action": "delete", "policy": "p1"}, "policy", "any"},

		// ---- elk_ops.go: 快照 ----
		{"snapshot/create-repo", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "create-repo", "repo": "r1", "location": "/backups"}, "repo", "any"},
		{"snapshot/create-location", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "create-repo", "repo": "r1", "location": "/backups"}, "location", "any"},
		{"snapshot/create-snapshot", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "create-snapshot", "repo": "r1", "snapshot": "s1"}, "snapshot", "any"},
		{"snapshot/create-indices", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "create-snapshot", "repo": "r1", "snapshot": "s1"}, "indices", "any"},
		{"snapshot/restore-repo", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "restore", "repo": "r1", "snapshot": "s1"}, "repo", "any"},
		{"snapshot/list-repo", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "list", "repo": "r1"}, "repo", "any"},
		{"snapshot/delete-snapshot", s.handleElasticsearchSnapshot, map[string]interface{}{"action": "delete", "repo": "r1", "snapshot": "s1"}, "snapshot", "any"},

		// ---- elk_ops.go: reindex / security ----
		{"reindex/source", s.handleElasticsearchReindex, map[string]interface{}{"source": "a", "dest": "b"}, "source", "any"},
		{"reindex/dest", s.handleElasticsearchReindex, map[string]interface{}{"source": "a", "dest": "b"}, "dest", "any"},
		{"security/create-user", s.handleElasticsearchSecurity, map[string]interface{}{"action": "create-user", "user": "u1", "password": "p1"}, "user", "any"},
		{"security/create-password", s.handleElasticsearchSecurity, map[string]interface{}{"action": "create-user", "user": "u1", "password": "p1"}, "password", "any"},
		{"security/create-roles", s.handleElasticsearchSecurity, map[string]interface{}{"action": "create-user", "user": "u1", "password": "p1"}, "roles", "json"},
		{"security/delete-user", s.handleElasticsearchSecurity, map[string]interface{}{"action": "delete-user", "user": "u1"}, "user", "any"},
		{"security/create-role", s.handleElasticsearchSecurity, map[string]interface{}{"action": "create-role", "role": "r1"}, "role", "any"},

		// ---- elk_ops.go: logstash / kibana / filebeat / opensearch ----
		{"logstash/deploy-version", s.handleLogstash, map[string]interface{}{"action": "deploy", "version": "8.13.0"}, "version", "any"},
		{"logstash/pipeline-name", s.handleLogstash, map[string]interface{}{"action": "pipeline", "name": "p1"}, "name", "any"},
		{"logstash/pipeline-config", s.handleLogstash, map[string]interface{}{"action": "pipeline", "name": "p1"}, "config", "text"},
		{"kibana/deploy-version", s.handleKibana, map[string]interface{}{"action": "deploy", "version": "8.13.0"}, "version", "any"},
		{"kibana/deploy-esUrl", s.handleKibana, map[string]interface{}{"action": "deploy", "version": "8.13.0", "esUrl": "http://localhost:9200"}, "esUrl", "any"},
		{"filebeat/deploy-version", s.handleFilebeat, map[string]interface{}{"action": "deploy", "version": "8.13.0"}, "version", "any"},
		{"opensearch/deploy-version", s.handleOpenSearch, map[string]interface{}{"action": "deploy", "version": "2.13.0"}, "version", "any"},
		{"opensearch/index-index", s.handleOpenSearch, map[string]interface{}{"action": "index", "index": "i1"}, "index", "any"},

		// ---- elk_ext_ops.go: pipeline / template ----
		{"ext-pipeline/get-name", s.handleElasticsearchPipeline, map[string]interface{}{"action": "get", "name": "n1"}, "name", "any"},
		{"ext-pipeline/create-name", s.handleElasticsearchPipeline, map[string]interface{}{"action": "create", "name": "n1", "body": `{}`}, "name", "any"},
		{"ext-pipeline/create-body", s.handleElasticsearchPipeline, map[string]interface{}{"action": "create", "name": "n1"}, "body", "json"},
		{"ext-pipeline/delete-name", s.handleElasticsearchPipeline, map[string]interface{}{"action": "delete", "name": "n1"}, "name", "any"},
		{"ext-pipeline/simulate-body", s.handleElasticsearchPipeline, map[string]interface{}{"action": "simulate"}, "body", "json"},
		{"ext-template/get-name", s.handleElasticsearchTemplate, map[string]interface{}{"action": "get", "name": "n1"}, "name", "any"},
		{"ext-template/create-name", s.handleElasticsearchTemplate, map[string]interface{}{"action": "create", "name": "n1", "body": `{}`}, "name", "any"},
		{"ext-template/create-body", s.handleElasticsearchTemplate, map[string]interface{}{"action": "create", "name": "n1"}, "body", "json"},
		{"ext-template/delete-name", s.handleElasticsearchTemplate, map[string]interface{}{"action": "delete", "name": "n1"}, "name", "any"},

		// ---- elk_ext_ops.go: alias / datastream ----
		{"ext-alias/add-index", s.handleElasticsearchAlias, map[string]interface{}{"action": "add", "alias": "a1", "index": "i1"}, "index", "any"},
		{"ext-alias/add-alias", s.handleElasticsearchAlias, map[string]interface{}{"action": "add", "alias": "a1", "index": "i1"}, "alias", "any"},
		{"ext-alias/remove-index", s.handleElasticsearchAlias, map[string]interface{}{"action": "remove", "alias": "a1", "index": "i1"}, "index", "any"},
		{"ext-alias/remove-alias", s.handleElasticsearchAlias, map[string]interface{}{"action": "remove", "alias": "a1", "index": "i1"}, "alias", "any"},
		{"ext-datastream/create-name", s.handleElasticsearchDataStream, map[string]interface{}{"action": "create", "name": "n1"}, "name", "any"},
		{"ext-datastream/delete-name", s.handleElasticsearchDataStream, map[string]interface{}{"action": "delete", "name": "n1"}, "name", "any"},
		{"ext-datastream/rollover-name", s.handleElasticsearchDataStream, map[string]interface{}{"action": "rollover", "name": "n1"}, "name", "any"},

		// ---- elk_ext_ops.go: maintenance / cluster-settings ----
		{"ext-maintenance/forcemerge-index", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "force-merge", "index": "i1"}, "index", "any"},
		{"ext-maintenance/clearcache-index", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "clear-cache", "index": "i1"}, "index", "any"},
		{"ext-maintenance/flush-index", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "flush", "index": "i1"}, "index", "any"},
		{"ext-maintenance/refresh-index", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "refresh", "index": "i1"}, "index", "any"},
		{"ext-maintenance/shrink-index", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "shrink", "index": "i1"}, "index", "any"},
		{"ext-maintenance/split-index", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "split", "index": "i1"}, "index", "any"},
		{"ext-cluster-settings/update-body", s.handleElasticsearchClusterSettings, map[string]interface{}{"action": "update"}, "body", "json"},

		// ---- elk_ext_ops.go: CCR / watcher ----
		{"ext-ccr/follow-index", s.handleElasticsearchCCR, map[string]interface{}{"action": "create-follower", "index": "i1", "leader": "l1"}, "index", "any"},
		{"ext-ccr/follow-remote", s.handleElasticsearchCCR, map[string]interface{}{"action": "create-follower", "index": "i1", "leader": "l1"}, "remote", "any"},
		{"ext-ccr/follow-leader", s.handleElasticsearchCCR, map[string]interface{}{"action": "create-follower", "index": "i1", "leader": "l1"}, "leader", "any"},
		{"ext-ccr/pause-index", s.handleElasticsearchCCR, map[string]interface{}{"action": "pause", "index": "i1"}, "index", "any"},
		{"ext-ccr/resume-index", s.handleElasticsearchCCR, map[string]interface{}{"action": "resume", "index": "i1"}, "index", "any"},
		{"ext-watcher/get-watchid", s.handleElasticsearchWatcher, map[string]interface{}{"action": "get", "watchId": "w1"}, "watchId", "any"},
		{"ext-watcher/create-watchid", s.handleElasticsearchWatcher, map[string]interface{}{"action": "create", "watchId": "w1", "body": `{}`}, "watchId", "any"},
		{"ext-watcher/create-body", s.handleElasticsearchWatcher, map[string]interface{}{"action": "create", "watchId": "w1"}, "body", "json"},
		{"ext-watcher/delete-watchid", s.handleElasticsearchWatcher, map[string]interface{}{"action": "delete", "watchId": "w1"}, "watchId", "any"},
		{"ext-watcher/activate-watchid", s.handleElasticsearchWatcher, map[string]interface{}{"action": "activate", "watchId": "w1"}, "watchId", "any"},
		{"ext-watcher/deactivate-watchid", s.handleElasticsearchWatcher, map[string]interface{}{"action": "deactivate", "watchId": "w1"}, "watchId", "any"},
	}
}

// b9Payloads 按 kind 取载荷集。
func b9Payloads(kind string) []string {
	switch kind {
	case "json":
		return b9PayloadJSON
	case "text":
		return b9PayloadText
	default:
		return b9PayloadAny
	}
}

// b9AssertNoInjection 断言脚本里载荷只以字面量形态存在（判据①③）。
//
// 用批次 7 的 unquotedText（纯函数，取脚本中「单引号之外」的文本）：注入载荷若出现在那里，
// 就是可执行结构。
func b9AssertNoInjection(t *testing.T, name, script string, payload string) {
	t.Helper()
	out := unquotedText(script)
	for _, bad := range []string{payload, "$(", "`", "touch /tmp/opsmesh-b9-pwned"} {
		if bad == "" {
			continue
		}
		if strings.Contains(out, bad) {
			t.Errorf("%s：引号外文本含可执行结构 %q：\n脚本=%s\n引号外=%s", name, bad, script, out)
		}
	}
	// 旧形态：用户字段直接落在**双引号段**内（双引号内 $ / 反引号仍会展开 ⇒ 不是防护）。
	//
	// 这里必须在 **unquotedText** 上查，不能在原始脚本上查——原始脚本里到处是 JSON 的
	// 双引号（如 `-d '{"actions":[{"add":{"alias":"<载荷>"}}]}'`），载荷嵌在 JSON 字符串里
	// 时两侧恰好也是 `"`，在原始脚本上查会**误判**（JSON 双引号 ≠ shell 双引号：
	// 整个 JSON 在 shell 单引号段内，本来就不可执行）。
	// unquotedText 已剥掉 shell 单引号段 ⇒ 只有真正的 shell 双引号直插才会命中。
	if strings.Contains(out, `"`+payload+`"`) {
		t.Errorf("%s：用户字段仍是双引号直插形态（双引号内 $ / 反引号会展开 ⇒ 可执行注入）：\n%s", name, script)
	}
}

// TestB9InjectionQuoted 判据①③：载荷只以单引号字面量形态出现。
func TestB9InjectionQuoted(t *testing.T) {
	s := newShellQuoteServer()
	for _, c := range b9Cases(s) {
		for pi, payload := range b9Payloads(c.kind) {
			body := map[string]interface{}{"deviceID": "b9-" + c.name + "-" + string(rune('a'+pi))}
			for k, v := range c.body {
				body[k] = v
			}
			body[c.field] = payload
			dev := body["deviceID"].(string)
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
			}
			sc := sqDispatchScript(t, s, dev)
			b9AssertNoInjection(t, c.name+"/"+payload, sc, payload)
		}
	}
}

// TestB9FreeFormJSONRejected 判据②：自由 JSON 字段非法 ⇒ 400 拒收；合法 ⇒ 受理。
func TestB9FreeFormJSONRejected(t *testing.T) {
	s := newShellQuoteServer()
	bad := []string{
		`{"a":`,
		`not json`,
		`{'a':'b'}`,
		`{"a":"b",}`,
	}
	cases := []b9Case{
		{"index-body", s.handleElasticsearchIndex, map[string]interface{}{"action": "create", "index": "i1"}, "body", "json"},
		{"ilm-body", s.handleElasticsearchILM, map[string]interface{}{"action": "create", "policy": "p1"}, "body", "json"},
		{"pipeline-body", s.handleElasticsearchPipeline, map[string]interface{}{"action": "create", "name": "n1"}, "body", "json"},
		{"template-body", s.handleElasticsearchTemplate, map[string]interface{}{"action": "create", "name": "n1"}, "body", "json"},
		{"cluster-body", s.handleElasticsearchClusterSettings, map[string]interface{}{"action": "update"}, "body", "json"},
		{"watcher-body", s.handleElasticsearchWatcher, map[string]interface{}{"action": "create", "watchId": "w1"}, "body", "json"},
	}
	for _, c := range cases {
		for i, payload := range bad {
			body := map[string]interface{}{"deviceID": "b9-free-" + c.name + "-" + string(rune('a'+i))}
			for k, v := range c.body {
				body[k] = v
			}
			body[c.field] = payload
			rec := sqPost(t, s, c.handler, body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s：非法 JSON 应 400 拒收，实得 %d：%s", c.name, rec.Code, rec.Body.String())
			}
		}
		// 合法 JSON 仍受理（能力不误伤）
		body := map[string]interface{}{"deviceID": "b9-free-ok-" + c.name}
		for k, v := range c.body {
			body[k] = v
		}
		body[c.field] = `{"properties":{"a":{"type":"string"}}}`
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Errorf("%s：合法 JSON 应受理，实得 %d：%s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// b9Shell 返回本机可用的 POSIX shell（nil=无）。
func b9Shell() string {
	if p, err := exec.LookPath("sh"); err == nil {
		return p
	}
	for _, cand := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
	} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return ""
}

// TestB9SemanticNoSideEffect 判据③：把脚本交给**真 sh** 执行，注入载荷不得产生副作用。
//
// URL 类载荷用 `$(touch marker)`：旧形态 URL 在双引号段内，而**双引号内命令替换照样展开**
// ⇒ 旧码下真的执行。logstash config 类用配对单引号的载荷（旧形态 `echo '%s'` 破段执行）。
func TestB9SemanticNoSideEffect(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, b9MarkerName)
	if strings.ContainsAny(marker, " \t") {
		t.Skip("临时目录路径含空白，跳过语义判据")
	}
	posixMarker := filepath.ToSlash(marker)

	s := newShellQuoteServer()
	plan := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]interface{}
		field   string
		payload string
	}{
		{"index-mapping", s.handleElasticsearchIndex, map[string]interface{}{"action": "mapping", "index": "i1"}, "index", "$(touch " + posixMarker + ")"},
		{"index-delete", s.handleElasticsearchIndex, map[string]interface{}{"action": "delete", "index": "i1"}, "index", "$(touch " + posixMarker + ")"},
		{"ilm-get", s.handleElasticsearchILM, map[string]interface{}{"action": "get", "policy": "p1"}, "policy", "$(touch " + posixMarker + ")"},
		{"datastream-rollover", s.handleElasticsearchDataStream, map[string]interface{}{"action": "rollover", "name": "n1"}, "name", "$(touch " + posixMarker + ")"},
		{"maintenance-refresh", s.handleElasticsearchMaintenance, map[string]interface{}{"action": "refresh", "index": "i1"}, "index", "$(touch " + posixMarker + ")"},
		{"watcher-get", s.handleElasticsearchWatcher, map[string]interface{}{"action": "get", "watchId": "w1"}, "watchId", "$(touch " + posixMarker + ")"},
		{"logstash-pipeline", s.handleLogstash, map[string]interface{}{"action": "pipeline", "name": "p1"}, "config", "x'; touch " + posixMarker + "; echo '"},
	}
	for _, c := range plan {
		body := map[string]interface{}{"deviceID": "b9-sem-" + c.name}
		for k, v := range c.body {
			body[k] = v
		}
		body[c.field] = c.payload
		rec := sqPost(t, s, c.handler, body)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s：应受理：%d %s", c.name, rec.Code, rec.Body.String())
		}
		sc := sqDispatchScript(t, s, body["deviceID"].(string))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		exec.CommandContext(ctx, sh, "-c", sc).Run() // 退出码无关紧要（ES 多半不存在）
		cancel()
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s：注入载荷被执行（标记文件已落地）：\n%s", c.name, sc)
		}
	}
}

// TestB9APIURLQuoteSemantics 钉判据①b：**整体 Quote 后，curl 收到的 URL 与原 URL 逐字相同**。
//
// 这是本批最容易被做错的一步——若把 Quote 结果塞回双引号段（`"http://…/'idx'"`），
// URL 里会多出字面量单引号（%27）⇒ 请求 404，而且**文本断言看不出来**（引号是配对的）。
// 故必须交给真 shell 求值来验。
func TestB9APIURLQuoteSemantics(t *testing.T) {
	sh := b9Shell()
	if sh == "" {
		t.Skip("本机无 POSIX shell（Windows），语义判据由 CI 的 ubuntu runner 承担")
	}
	paths := []string{
		"myindex",
		"myindex/_mapping?pretty",
		"_ilm/policy/p1?pretty",
		"_snapshot/repo1/snap1?wait_for_completion=true",
		"idx with space",
		"idx'quote",
	}
	for _, p := range paths {
		out, err := exec.Command(sh, "-c", "printf '%s' "+esAPIURL(p)).CombinedOutput()
		if err != nil {
			t.Fatalf("求值 %q 失败：%v（%s）", p, err, out)
		}
		want := esBaseURL + "/" + p
		if string(out) != want {
			t.Errorf("URL 求值结果与原 URL 不等：\n got=%q\nwant=%q", string(out), want)
		}
	}
}

// TestB9NoRawReqInterpolation 判据④：两个文件里不得再有未经收口的用户字段直插脚本。
//
// 收口形态：shellquote.Quote / esAPIURL / esQuoteJSONText / esJSONPayload / esEcho。
func TestB9NoRawReqInterpolation(t *testing.T) {
	markers := []string{"shellquote.Quote(", "esAPIURL(", "esQuoteJSONText(", "esJSONPayload(", "esEcho("}
	for _, f := range []string{"elk_ops.go", "elk_ext_ops.go"} {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败：%v", f, err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			if !strings.Contains(line, "script") || !strings.Contains(line, "fmt.Sprintf") {
				continue
			}
			if !strings.Contains(line, "req.") {
				continue
			}
			covered := false
			for _, m := range markers {
				if strings.Contains(line, m) {
					covered = true
					break
				}
			}
			if covered {
				continue
			}
			t.Errorf("%s:%d 用户字段未经收口直插脚本：%s", f, i+1, strings.TrimSpace(line))
		}
	}
}
