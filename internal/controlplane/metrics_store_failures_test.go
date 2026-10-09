// metrics_store_failures_test.go 守住"抓取面必须推存储吞错计数"这条接线。
//
// 背景（2026-10-02 真机实测）：opsmesh_store_write_failures_total 只在诊断包路径
// renderPrometheus() 里被 SetStoreFailures 推过，/metrics（8080 与 9091 共用）从不推
// ⇒ Prometheus 侧该序列**恒为 0**，而同一台机器上日志已记录 1844 次存储吞错。
// "被吞掉的错误只有这个指标看得见"这句话本身被吞掉了。

package controlplane

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Levango7/OpsMesh/internal/metrics"
	"github.com/Levango7/OpsMesh/internal/store/storefail"
)

// renderedStoreFailures 从指标文本里取出 opsmesh_store_write_failures_total 的数值。
func renderedStoreFailures(t *testing.T, body string) uint64 {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "opsmesh_store_write_failures_total ") {
			v, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "opsmesh_store_write_failures_total ")), 10, 64)
			if err != nil {
				t.Fatalf("指标值解析失败：%q: %v", line, err)
			}
			return v
		}
	}
	t.Fatalf("抓取面输出里没有 opsmesh_store_write_failures_total 这一行（序列消失＝引用它的规则永不触发）")
	return 0
}

func TestMetricsBodyPushesStoreFailures(t *testing.T) {
	s := newMetricsTestServer()
	// 换成全新注册表并预置一个"陈旧值"：装配路径若漏掉 SetStoreFailures，
	// 渲染出来的就是这个哨兵值——用"不等于 0"来断言是抓不住的，因为本进程里
	// 根本没有真失败可推，0 既可能是"推对了"也可能是"没推"。
	const stale = uint64(12345)
	s.metrics = metrics.New()
	s.metrics.SetStoreFailures(stale)

	before, _ := storefail.StoreFailureStats()
	body := s.metricsBody()
	after, _ := storefail.StoreFailureStats()

	got := renderedStoreFailures(t, body)
	if got == stale {
		t.Fatalf("metricsBody() 没有推存储吞错计数：渲染值仍是哨兵 %d（这正是抓取面恒为 0 的成因）", stale)
	}
	if got < before {
		t.Fatalf("吞错计数出现回退：推送前实际累计 %d，渲染值 %d", before, got)
	}
	if got > after {
		t.Fatalf("渲染值 %d 大于实际累计 %d：指标不是来自 StoreFailureStats", got, after)
	}
}

// seriesNames 取指标文本里的序列名集合（丢分数值与标签）。
//
// 只比名字不比全文：注册表含 go_/process_ 这类每次渲染都会动的序列，逐字节比较会把
// 本测试变成 flaky，而它要守的是"两条出口装配的是同一批仪表"。
func seriesNames(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := line
		if i := strings.IndexAny(name, " {"); i > 0 {
			name = name[:i]
		}
		out[name] = true
	}
	return out
}

func diffNames(a, b map[string]bool) []string {
	var out []string
	for n := range a {
		if !b[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// TestDiagnosticMetricsMatchScrapeBody 断言诊断包与抓取面是同一份装配。
// 两者曾各推各的仪表（诊断包只推 store failures，抓取面只推 app gauges），
// 而代码注释声称这里是"相同渲染路径"。
func TestDiagnosticMetricsMatchScrapeBody(t *testing.T) {
	s := newMetricsTestServer()
	s.metrics = metrics.New()
	scrape := seriesNames(s.metricsBody())
	diag := seriesNames(s.renderPrometheus(context.Background()))

	if len(scrape) < 10 {
		t.Fatalf("只解析到 %d 个序列名，本断言在空转（渲染路径异常）", len(scrape))
	}
	onlyScrape, onlyDiag := diffNames(scrape, diag), diffNames(diag, scrape)
	if len(onlyScrape) > 0 || len(onlyDiag) > 0 {
		t.Fatalf("两条出口序列集合不同：只在抓取面有 [%s]；只在诊断包有 [%s]",
			strings.Join(onlyScrape, ","), strings.Join(onlyDiag, ","))
	}
}
