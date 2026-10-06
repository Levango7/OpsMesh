// plugin_hook_gate_test.go 钉住「扩展点必须有真实宿主接线」（TD-62 决策点 ④）。
//
// # 为什么需要这条门禁
//
// internal/plugin 的 Manager 完整且并发安全，但控制面里**没有任何一处触发它**——
// FireHook/RegisterHook 在 internal/controlplane 下零调用点，唯一调用者是示例
// plugins/hello 自己 fire 自己。"可插拔扩展"因此只是框架，不是能力。
//
// 这个状态能长期存在，是因为**没有任何机制检查它**：Hook 只是裸字符串，
// 谁都能凭空写一个 plugin.Hook("xxx")，而不触发它不会让任何测试变红。
//
// 本门禁把扩展点变成契约：plugin.AllHooks() 里的每一个 Hook，都必须
//  1. 在控制面源码里有 FireHook 调用点（真实触发，不是自证）
//  2. 有对应的行为测试（证明触发确实生效）
//
// 只加常量不加触发点 ⇒ 判红。只加触发点不加常量 ⇒ 常量清单漂移，同样判红。
//
// # 变异检验
//
// 本门禁自身经变异验证：从 AllHooks() 删掉一个 Hook（应判红"清单与触发点不一致"）、
// 在 AllHooks() 加一个未接线的 Hook（应判红"缺触发点"）。
package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Levango7/OpsMesh/internal/plugin"
	"github.com/Levango7/OpsMesh/internal/store"

	grpcserver "github.com/Levango7/OpsMesh/internal/controlplane/grpc"
)

// TestEveryFrozenHookHasAFireSiteInControlPlane 断言每个冻结扩展点都有宿主触发点。
//
// 扫描面刻意限定 internal/controlplane（含子包），不含 internal/plugin 自身——
// 否则 plugins/hello 那种"自己 fire 自己"的自证写法也能让门禁变绿，
// 恰恰是我们要排除的形态。
func TestEveryFrozenHookHasAFireSiteInControlPlane(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("无法定位仓库根：%v", err)
	}
	scanDir := filepath.Join(root, "internal", "controlplane")

	// 双向对账，缺一不可：
	//   A. 清单里有、源码里没有 ⇒ 声明了却没接线（TD-62 的原始形态）
	//   B. 源码里有、清单里没有 ⇒ 冻结清单漂移（有人接了线却没登记，
	//      或有人从 AllHooks 删掉了仍在使用的常量）
	//
	// 只做 A 不做 B 会让门禁在「删掉一个已接线扩展点」时保持绿色——
	// 本门禁第一版正是这个缺陷，经变异检验发现（2026-10-05）。
	srcRefs := map[string]bool{} // 常量名 -> 在控制面被引用
	files := 0
	_ = filepath.Walk(scanDir, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		files++
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for _, m := range hookConstRef.FindAllStringSubmatch(string(data), -1) {
			srcRefs[m[1]] = true
		}
		return nil
	})
	if files == 0 {
		t.Fatalf("扫描面为空（0 个 go 文件）——门禁已失效，检查 scanDir=%s", scanDir)
	}

	// 引用面必须先用 hooks.go 里**真实声明的 Hook 常量**过滤一遍。
	// 起因（2026-10-07 实测）：TD-62 交付时新文件里写了 `plugin.HookHandler`（那是 handler 的**函数类型**），
	// 正则 `plugin\.(Hook[A-Za-z]+)` 把它当成扩展点常量捕获，门禁据此报"清单漂移"——
	// 一条**假阳性**。假阳性在这类门禁上比漏报更贵：它会让人去"修"一个本来对的东西
	// （本仓已有先例：把已修好的表格行按错误计数改回去）。
	// 过滤的判据是确定性的：只有 `^\s*(HookXxx) Hook = ` 这种声明才算扩展点常量。
	declared := declaredHookConsts(t, root)
	for name := range srcRefs {
		if !declared[name] {
			delete(srcRefs, name)
		}
	}

	frozen := map[string]bool{} // 常量名 -> 在 AllHooks 里
	for _, h := range plugin.AllHooks() {
		name := constNameOf(h)
		if name == "" {
			// 不这样处理的话，未收录的 Hook 会往 frozen 里塞一个空串键，
			// 报错文案就变成"扩展点常量  在 AllHooks() 里"（名字是空的，读的人无从下手）。
			t.Errorf("plugin.AllHooks() 里的 %q 没有被 constNameOf 收录——请同步本函数（两侧 key 必须同形态）", h)
			continue
		}
		frozen[name] = true
	}

	// A. 清单里有 ⇒ 必须有触发点。
	for name := range frozen {
		if !srcRefs[name] {
			t.Errorf("扩展点常量 %s 在 AllHooks() 里，但 internal/controlplane 下**没有触发点**——"+
				"TD-62 的原始缺陷就是这个：框架齐备但零接线。请补 FireHook 调用点", name)
		}
	}
	// B. 有触发点 ⇒ 必须在冻结清单里（防删除/漂移）。
	for name := range srcRefs {
		if !frozen[name] {
			t.Errorf("internal/controlplane 触发了扩展点常量 %s，但它不在 plugin.AllHooks() 里——"+
				"冻结清单已漂移。若确为新增扩展点，请加入 AllHooks() 并补测试", name)
		}
	}
	// C. hooks.go 里声明了却没进 AllHooks() ⇒ 判红。
	// 加了这第三向后，门禁的判定面从"引用 ↔ 清单"升级成"声明 ↔ 清单 ↔ 触发点"三向：
	// 一个常量只要声明出来，就必须同时出现在冻结清单里，无论有没有人引用它。
	for name := range declared {
		if !frozen[name] {
			t.Errorf("internal/plugin/hooks.go 声明了扩展点常量 %s，但它不在 AllHooks() 里——"+
				"声明而未冻结的扩展点不会有任何触发点保证，属 TD-62 原来那类缺口的变体", name)
		}
	}
	// 只在真的没失败时记成功日志：原先无条件打「双向一致」，而 t.Errorf 已经发生，
	// 读者翻到最后一行看到的是自相矛盾的两句话（这条日志本身就是给人看的证据，不能说谎）。
	if !t.Failed() {
		t.Logf("宿主接线门禁：hooks.go 声明 %d 项，AllHooks=%d 项，控制面引用 %d 项，三向一致（扫描 %d 个文件）",
			len(declared), len(frozen), len(srcRefs), files)
	}
}

// declaredHookConsts 读出 internal/plugin/hooks.go 里真正声明的 Hook 常量名。
//
// 判据是声明形态而不是名字形状：`^\s*(HookXxx) Hook = "..."`。
// 这样 `plugin.HookHandler`（函数类型）、`plugin.Hook`（类型本身）都不会被误当扩展点常量。
func declaredHookConsts(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "internal", "plugin", "hooks.go"))
	if err != nil {
		t.Fatalf("读 internal/plugin/hooks.go 失败：%v（本门禁的过滤面就是它，读不到就等于门禁失明）", err)
	}
	out := map[string]bool{}
	for _, m := range hookConstDecl.FindAllStringSubmatch(string(data), -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatalf("hooks.go 里没解析到任何 Hook 常量声明——正则失配或文件被清空，本节在空转")
	}
	return out
}

// hookConstRef 提取控制面源码里对插件扩展点常量的引用（含间接传给 firePluginHook 的写法）。
var hookConstRef = regexp.MustCompile(`plugin\.(Hook[A-Za-z]+)`)

// hookConstDecl 匹配 hooks.go 里的常量声明行。
//
// 两种写法都要认：`const (...)` 块内的缩进行，以及独立一行 `const HookXxx Hook = "…"`。
// 第一版只认缩进形态，于是变异检验里"声明一个独立 const 而不进 AllHooks"这条**照样判绿**——
// 漏掉的那一类声明会同时废掉两件事：它不会被 C 方向抓到，而引用它的代码会被过滤面误删。
var hookConstDecl = regexp.MustCompile(`(?m)^\s*(?:const\s+)?(Hook[A-Za-z]+)\s+Hook\s*=`)

// constNameOf 返回该 Hook 对应的常量**裸名**（不含 "plugin." 前缀），
// 与 hookConstRef 正则捕获到的形态保持一致。
//
// ⚠️ 两侧 key 必须同一形态：第一版这里返回 "plugin.HookXxx"（带前缀）
// 而正则只捕获 "HookXxx"，于是**永远匹配不上，门禁恒红**——
// 表现为"变异检验两个方向都判红"的假阳性，还原后仍红才暴露出来。
// 改任一侧时都必须同步另一侧。
//
// 未收录的 Hook 返回空串（会触发门禁报错，防静默放行）。
func constNameOf(h plugin.Hook) string {
	switch h {
	case plugin.HookConfigPreSet:
		return "HookConfigPreSet"
	case plugin.HookConfigPostSet:
		return "HookConfigPostSet"
	case plugin.HookTaskPreClaim:
		return "HookTaskPreClaim"
	}
	return ""
}

// TestConfigPostSetHookIsActuallyFired 端到端证明 config.postSet 真的被控制面触发。
//
// 这条是"触发点存在"之外更强的断言：**注册一个插件，走一次真实 HTTP 配置写入，
// 插件必须被调用**。只测 FireHook 存在，测不出"写在了永远走不到的分支里"。
func TestConfigPostSetHookIsActuallyFired(t *testing.T) {
	var fired int32
	mgr := plugin.NewManager()
	if err := mgr.RegisterHook(plugin.HookConfigPostSet, func(ev plugin.Event) error {
		atomic.AddInt32(&fired, 1)
		return nil
	}); err != nil {
		t.Fatalf("注册钩子失败：%v", err)
	}
	prev := PluginManager()
	SetPluginManager(mgr)
	t.Cleanup(func() { SetPluginManager(prev) })

	s := newAPIKeyTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config",
		strings.NewReader(`{"alertRetentionDays":7}`))
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleUpdatePlatformConfig(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("配置写入应成功（post 钩子无副作用），实际 %d: %s", w.Code, w.Body.String())
	}
	if atomic.LoadInt32(&fired) != 1 {
		t.Errorf("config.postSet 钩子未被触发（fired=%d）——扩展点接线形同虚设", fired)
	}
}

// TestConfigPreSetHookCanBlockTheWrite 证明 config.preSet 的阻断语义成立：
// 钩子返回 error 时，配置**不落库**且响应 4xx。
func TestConfigPreSetHookCanBlockTheWrite(t *testing.T) {
	mgr := plugin.NewManager()
	if err := mgr.RegisterHook(plugin.HookConfigPreSet, func(ev plugin.Event) error {
		return context.Canceled // 任意 error 即阻断
	}); err != nil {
		t.Fatalf("注册钩子失败：%v", err)
	}
	prev := PluginManager()
	SetPluginManager(mgr)
	t.Cleanup(func() { SetPluginManager(prev) })

	s := newAPIKeyTestServer()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/config",
		strings.NewReader(`{"alertRetentionDays":7}`))
	req.Header.Set("Authorization", loginAsAdmin(t, s))
	req.Header.Set("X-Tenant-ID", "default")
	w := httptest.NewRecorder()
	s.handleUpdatePlatformConfig(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("pre 钩子阻断时应返回 400（业务未落库），实际 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "plugin policy") {
		t.Errorf("响应体应说明是被插件策略拒绝，实际：%s", w.Body.String())
	}
}

// TestTaskPreClaimHookCanBlockClaim 证明 task.preClaim 的阻断语义成立：
// 钩子返回 error 时 ClaimTask 返回 nil（不下发任务），且与"无待领任务"同形。
func TestTaskPreClaimHookCanBlockClaim(t *testing.T) {
	svc := grpcserver.NewStoreAgentServiceWithHooks(
		store.NewMemoryStore().WithDemo(true),
		func(ctx context.Context, agentID string) error { return context.Canceled },
	)
	if got := svc.ClaimTask("agent-blocked"); got != nil {
		t.Errorf("pre 钩子阻断时 ClaimTask 应返回 nil（不下发任务），实际 %v", got)
	}
}

// TestNoPluginManagerMeansNoBehaviorChange 是回归护栏：
// 未启用插件宿主时，所有接线点必须与接线前完全一致（零行为变化）。
//
// 这条守护的是"扩展点是可选增强而非必经路径"这一设计承诺——
// 若哪天有人在 firePluginHook 里加了默认拒绝，这里会立刻变红。
func TestNoPluginManagerMeansNoBehaviorChange(t *testing.T) {
	prev := PluginManager()
	SetPluginManager(nil)
	t.Cleanup(func() { SetPluginManager(prev) })

	s := newAPIKeyTestServer()
	if err := s.firePluginHook(context.Background(), plugin.HookConfigPreSet, plugin.Event{}); err != nil {
		t.Errorf("未启用插件宿主时 firePluginHook 必须返回 nil（放行），实际 %v", err)
	}
	if err := s.firePluginHook(context.Background(), plugin.HookConfigPostSet, plugin.Event{}); err != nil {
		t.Errorf("未启用插件宿主时 firePluginHook 必须返回 nil（放行），实际 %v", err)
	}
}
