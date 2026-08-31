// agent_leak_test.go：goroutine 泄漏防线（CI OOM 修复的验收测试）。
//
// 背景：CI 7GB runner 上 agent 包测试 OOM（goroutine 累计 1700+）。为定位并
// 防回归，本文件提供 TestMain：全包测试跑完后 dump 存活 goroutine 按「顶层
// 用户帧」分组统计，超过阈值即失败并列出 top 泄漏点（有存量泄漏时给出清单
// 而非直接红，便于渐进修复——阈值先设宽松，随修复收紧）。
package agent

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// leakGoroutineLimit 泄漏阈值：当前基线（含 gRPC conn 内部重连 goroutine 等合法驻留）。
// TODO: 随泄漏修复逐步收紧到 100 以内。
const leakGoroutineLimit = 600

func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		n := runtime.NumGoroutine()
		if n > leakGoroutineLimit {
			fmt.Fprintf(os.Stderr, "\n[leak-check] 存活 goroutine=%d 超过阈值 %d，top 泄漏点：\n%s\n",
				n, leakGoroutineLimit, topGoroutineSites(20))
			// 阈值内先警告不失败（渐进收紧），超过 2 倍才红：
			if n > leakGoroutineLimit*2 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// topGoroutineSites 抓取全部 goroutine 栈，按「第一个非 runtime 帧所在行」分组统计。
func topGoroutineSites(top int) string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	counts := map[string]int{}
	for _, g := range strings.Split(string(buf), "\n\n") {
		lines := strings.Split(g, "\n")
		if len(lines) < 2 {
			continue
		}
		site := ""
		for _, l := range lines[1:] {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "opsmesh/") {
				site = t
				break
			}
		}
		if site == "" {
			site = strings.TrimSpace(lines[1])
		}
		counts[site]++
	}
	type kv struct {
		k string
		v int
	}
	var lst []kv
	for k, v := range counts {
		lst = append(lst, kv{k, v})
	}
	sort.Slice(lst, func(i, j int) bool { return lst[i].v > lst[j].v })
	var b strings.Builder
	for i, e := range lst {
		if i >= top {
			break
		}
		fmt.Fprintf(&b, "  %4d  %s\n", e.v, e.k)
	}
	return b.String()
}
