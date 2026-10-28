package cloudprovider

import (
	"strconv"
	"sync"
	"testing"

	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/aliyun"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/aws"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/azure"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/huawei"
	"github.com/Levango7/OpsMesh/internal/platform/cloudprovider/tencent"
)

// fakeProvider 只覆写 ProviderType，其余能力复用 NotImplementedProvider，
// 以便在工厂测试里区分"按类型取回的确实是那个适配器"。
type fakeProvider struct {
	NotImplementedProvider
	typ ProviderType
}

func (p *fakeProvider) ProviderType() ProviderType { return p.typ }

// builtinTypes 是 RegisterBuiltIns 必须覆盖的厂商集合。
var builtinTypes = []ProviderType{
	ProviderTypeAliyun,
	ProviderTypeAWS,
	ProviderTypeAzure,
	ProviderTypeTencent,
	ProviderTypeHuawei,
}

// 编译期断言：5 个内建适配器都满足 CloudProvider 接口。
var (
	_ CloudProvider = (*aliyun.AliyunProvider)(nil)
	_ CloudProvider = (*aws.AWSProvider)(nil)
	_ CloudProvider = (*azure.AzureProvider)(nil)
	_ CloudProvider = (*tencent.TencentProvider)(nil)
	_ CloudProvider = (*huawei.HuaweiProvider)(nil)
)

// TestNewAdapterFactory_RegistersAllBuiltIns 钉住两件事：
// ① 5 个内建厂商一个不少（漏注册 ⇒ 控制面选到该厂商时只会拿到 nil）；
// ② 工厂键与适配器自报的 ProviderType 一致（两边写歪会让 Get 取到别的适配器）。
func TestNewAdapterFactory_RegistersAllBuiltIns(t *testing.T) {
	f := NewAdapterFactory()

	for _, want := range builtinTypes {
		p, ok := f.Get(want)
		if !ok || p == nil {
			t.Errorf("内建厂商 %q 未注册", want)
			continue
		}
		if got := p.ProviderType(); got != want {
			t.Errorf("工厂键 %q 对应的适配器自报 %q——键与类型不一致", want, got)
		}
	}

	got := f.SupportedTypes()
	if len(got) != len(builtinTypes) {
		t.Fatalf("SupportedTypes() 数量 = %d，期望 %d（实际：%v）", len(got), len(builtinTypes), got)
	}
	seen := make(map[ProviderType]bool, len(got))
	for _, tp := range got {
		if seen[tp] {
			t.Errorf("SupportedTypes() 出现重复项：%q", tp)
		}
		seen[tp] = true
	}
	for _, want := range builtinTypes {
		if !seen[want] {
			t.Errorf("SupportedTypes() 缺少内建厂商 %q（实际：%v）", want, got)
		}
	}
}

// TestAdapterFactory_RegisterOverwrites 钉住"同类型重复注册覆盖"的语义：
// 测试替身或后续真实实现接管某个厂商时，取回的是最后一次注册的实例。
func TestAdapterFactory_RegisterOverwrites(t *testing.T) {
	f := NewAdapterFactory()
	fake := &fakeProvider{typ: ProviderTypeAWS}
	f.Register(fake)

	p, ok := f.Get(ProviderTypeAWS)
	if !ok || p != fake {
		t.Fatalf("Get(%q) 未返回后注册的实例", ProviderTypeAWS)
	}
	if n := len(f.SupportedTypes()); n != len(builtinTypes) {
		t.Errorf("覆盖注册后 SupportedTypes() 数量 = %d，期望仍为 %d", n, len(builtinTypes))
	}
}

func TestAdapterFactory_GetUnknown(t *testing.T) {
	f := NewAdapterFactory()
	p, ok := f.Get(ProviderType("no-such-cloud"))
	if ok || p != nil {
		t.Fatalf("未注册类型应返回 (nil, false)，得到 (%v, %v)", p, ok)
	}
}

// TestAdapterFactory_ConcurrentUse 让 Register/Get/SupportedTypes 并发跑一遍，
// 配合 CI 的 -race 腿验证读写锁没漏（工厂是全局单例被多协程共用的形态）。
func TestAdapterFactory_ConcurrentUse(t *testing.T) {
	f := NewAdapterFactory()
	const workers = 32

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			typ := ProviderType("concurrent-" + strconv.Itoa(i))
			f.Register(&fakeProvider{typ: typ})
			if p, ok := f.Get(typ); !ok || p.ProviderType() != typ {
				t.Errorf("并发注册后 Get(%q) 未取回同一类型", typ)
			}
			_ = f.SupportedTypes()
		}(i)
	}
	wg.Wait()

	// 收尾仍可取到内建厂商（并发注册不应破坏已有条目）。
	if _, ok := f.Get(ProviderTypeAliyun); !ok {
		t.Error("并发注册后内建厂商 aliyun 丢失")
	}
}
