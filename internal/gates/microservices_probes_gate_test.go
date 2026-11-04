// microservices_probes_gate_test.go 是「12 个微服务健康探针口径」的回归门禁。
//
// 背景（TD-77，docs/COORDINATION.md 2026-10-05 收口通告）：k8s 滚动更新依赖
// liveness/readiness 探针存活，而各微服务的健康端点历史上路径分叉
// （/api/v1/health、/healthz、/health 并存）。TD-77 已把 12 个微服务统一到
// 规范路径 /health（存活）+ /ready（就绪），旧别名 /api/v1/health 按通告口径
// 保留不摘（外部探针可能仍打旧路径）。**控制面 internal/controlplane 不在
// 收敛范围内**——它用 /healthz+/readyz，且 federation.go 的 peer 在线判定
// 按字面打 <peer>/healthz（通告明示的原因）。
//
// 为什么还需要门禁（2026-11-05 立）：源码侧已达标，但「达标」没有任何东西
// 守着——任何人新增一个微服务、或重构 RegisterRoutes 时漏掉探针，编译、vet、
// lint 和单测全都看不见。本用例把 TD-77 的既有事实变成构建期红。
//
// 判据（四档，缺一档就退化成摆设）：
//  1. 扫描面自证：必须扫到 12 个微服务的 handler 源码，且路由正则确实能匹配
//     已知注册（否则「0 违规」只代表扫描器坏了）。
//  2. 每个微服务必须注册 /health 与 /ready（路由在哪一层注册不限——
//     RegisterRoutes 或 main.go 均可，这是 TD-77 落地时的真实分布）。
//  3. values.yaml 里每个服务的 probe.path 必须是 /health（部署面与源码路径
//     对齐——否则会在运行期才以 404 形式暴露）。
//  4. 每个微服务 main 必须同时具备「信号等待」（signal.Notify）与「带超时
//     关停」（Shutdown(）。缺任一即红：只等信号不关停会把 k8s 滚动更新
//     挂死；只写 Shutdown 而无人通知退出则是死代码，Pod 被杀时不会执行。

package gates

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// microserviceProbesWant 是 TD-77 规范路径（存活 + 就绪）。
var microserviceProbesWant = []string{"/health", "/ready"}

// routeRe 匹配 mux.HandleFunc("…") / mux.Handle("…") 与 net/http RouteXxx 后续形态，
// 覆盖当前 12 个微服务的既有写法（RegisterRoutes 内与 main.go 内两种）。
var microserviceRouteRe = regexp.MustCompile(`Handle(?:Func)?\(\s*"([^"]+)"`)

// microserviceShutdownMarkers 是优雅关停**成对**判定标记：两个都必须命中
// （信号等待 + 带超时关停）。只命中其一的形态都是残缺的。
// 2026-11-05 实测 12 个 main 全部两标记齐备。
var microserviceShutdownMarkers = []string{"Shutdown(", "signal.Notify"}

// findMicroserviceGoFiles 收集某个微服务目录下的相关 Go 源（排除测试与 vendor）：
// main.go 一层 + internal/handler 一层，即 TD-77 路由注册的真实分布面。
func findMicroserviceGoFiles(t *testing.T, svcDir string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(svcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "testdata" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"cmd"+string(filepath.Separator)) ||
			strings.Contains(path, "internal"+string(filepath.Separator)+"handler") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", svcDir, err)
	}
	return files
}

// registeredRoutes 汇总一个微服务在 main+handler 两层注册的全部路由路径。
func registeredRoutes(t *testing.T, files []string) map[string]bool {
	t.Helper()
	routes := map[string]bool{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range microserviceRouteRe.FindAllStringSubmatch(string(src), -1) {
			routes[m[1]] = true
		}
	}
	return routes
}

func TestMicroservicesProbeGate(t *testing.T) {
	root := repoRoot(t)

	// ---- 扫描面自证（判据 1）：枚举微服务目录 & 每服务有 cmd/main + handler 源 ----
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatalf("read services/: %v", err)
	}
	var svcs []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-svc") {
			continue
		}
		svcs = append(svcs, e.Name())
	}
	sort.Strings(svcs)
	if len(svcs) < 12 {
		t.Fatalf("扫描面异常：只枚举到 %d 个 *-svc（%v）——门禁前提是 12 个微服务都在 services/ 下", len(svcs), svcs)
	}

	type spec struct {
		name     string
		files    []string
		routes   map[string]bool
		hasShutd bool
	}
	specs := make([]spec, 0, len(svcs))
	totalRoutes := 0
	for _, s := range svcs {
		files := findMicroserviceGoFiles(t, filepath.Join(root, "services", s))
		if len(files) == 0 {
			t.Errorf("%s：扫不到 cmd/handler 层 Go 源——新增微服务必须落在 services/<name>/cmd/<name>/main.go 或 internal/handler/", s)
			continue
		}
		routes := registeredRoutes(t, files)
		totalRoutes += len(routes)

		// 关停兑现：两标记必须**同时**命中（任一缺失都不是优雅关停）。
		markersHit := map[string]bool{}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for _, marker := range microserviceShutdownMarkers {
				if strings.Contains(string(b), marker) {
					markersHit[marker] = true
				}
			}
		}
		hasShutd := len(markersHit) == len(microserviceShutdownMarkers)

		// 判据 2：/health + /ready 必须注册（原来两层都允许）。
		for _, want := range microserviceProbesWant {
			if !routes[want] {
				t.Errorf("%s：缺 %s 注册——TD-77 规范路径（docs/COORDINATION.md 2026-10-05 通告）；如确需豁免须带原因上板", s, want)
			}
		}
		// 判据 4：优雅关停兑现。
		if !hasShutd {
			t.Errorf("%s：优雅关停不完整（需同时有 signal.Notify 与 Shutdown(）——单缺 signal.Notify 时 Pod 被杀不会执行关停；单缺 Shutdown 时滚动更新会被挂死", s)
		}
		specs = append(specs, spec{name: s, files: files, routes: routes, hasShutd: hasShutd})
	}

	// ---- 扫描面自证（判据 1 收尾）：路由总量 & 已知路由断言 ----
	if totalRoutes < 60 {
		t.Fatalf("扫描面异常：12 个微服务合计只扫到 %d 条路由——正则很可能没匹配上现有写法（TD-77 时每服务至少十余条）", totalRoutes)
	}
	var selfCheck []string
	for _, s := range specs {
		if s.routes["/health"] {
			selfCheck = append(selfCheck, s.name)
		}
	}
	if len(selfCheck) != len(svcs) {
		t.Fatalf("扫描面异常：%s 未把已知的 /health 算成已注册——正则漏匹配", strings.Join(selfCheck, ","))
	}

	// ---- 判据 3：部署面 values.yaml 的 probe.path 与源码对齐 ----
	valsRaw, err := os.ReadFile(filepath.Join(root, "deploy", "helm", "opsmesh", "values.yaml"))
	if err != nil {
		t.Fatalf("read values.yaml: %v", err)
	}
	vals := string(valsRaw)

	// probe.path 全部实例（services.<svc>.probe.path）必须是 /health。
	probePathRe := regexp.MustCompile(`(?m)^\s*path:\s*(\S+)\s*$`)
	var probePaths []string
	for _, m := range probePathRe.FindAllStringSubmatch(vals, -1) {
		probePaths = append(probePaths, m[1])
	}
	badProbe := 0
	for _, p := range probePaths {
		if p != "/health" && p != "/" { // "/" 属 controlplane dashboard 探测，不在本判据射程
			badProbe++
			t.Errorf("values.yaml：probe.path=%s 不是规范 /health——部署面与源码（TD-77）分叉会让探针在运行期 404", p)
		}
	}
	if len(probePaths) < 12 {
		t.Fatalf("values.yaml 只扫到 %d 处 probe.path——与 12 个微服务的部署面不一致，正则或结构漂移", len(probePaths))
	}
	if badProbe == 0 && len(probePaths) >= 12 {
		// 自证通过：12+ 处 probe.path 全部是 /health（或 controlplane 的 "/"）。
		t.Logf("values.yaml：%d 处 probe.path 全部合规", len(probePaths))
	}

	// ---- 摘要：达标清单（便于红灯时直接对照） ----
	var lines []string
	for _, s := range specs {
		mark := "OK"
		for _, want := range microserviceProbesWant {
			if !s.routes[want] {
				mark = "GAP:" + want
				break
			}
		}
		if !s.hasShutd {
			mark = "NO-SHUTDOWN"
		}
		lines = append(lines, fmt.Sprintf("%s=%s", s.name, mark))
	}
	t.Logf("微服务探针门禁：%d 个服务，%d 条路由，%d 处 probe.path 合规\n%s", len(specs), totalRoutes, len(probePaths)-badProbe, strings.Join(lines, " "))
}
