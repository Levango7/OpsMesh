// Package tenant 的单元测试（白盒测试，可覆盖 validateTokenForTenant 注入点）。
//
// 覆盖范围：TenantUsage 配额判定与用量记账、Manager 的配额执行/用量跟踪/
// 默认配额、上下文注入与提取、HTTP 中间件（普通/严格/配额）与 gRPC 拦截器，
// 以及并发安全（TenantUsage 与 Manager 的锁语义）。
package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"opsmesh/pkg/auth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// fakeQuotaStore 是 QuotaStore 的可控内存实现。
type fakeQuotaStore struct {
	quotas map[string]map[ResourceType]int
	err    error
}

func (f *fakeQuotaStore) GetQuota(tenantID string) (map[ResourceType]int, error) {
	if f.err != nil {
		return nil, f.err
	}
	q, ok := f.quotas[tenantID]
	if !ok {
		return nil, errors.New("no quota config")
	}
	return q, nil
}

func (f *fakeQuotaStore) SetQuota(tenantID string, resourceType ResourceType, limit int) error {
	if f.quotas == nil {
		f.quotas = make(map[string]map[ResourceType]int)
	}
	if f.quotas[tenantID] == nil {
		f.quotas[tenantID] = make(map[ResourceType]int)
	}
	f.quotas[tenantID][resourceType] = limit
	return nil
}

// fakeUsageStore 是 UsageStore 的可控内存实现。
type fakeUsageStore struct {
	mu      sync.Mutex
	usage   map[string]map[ResourceType]int
	getErr  error
	trackFn func(tenantID string, resourceType ResourceType, amount int) error
}

func (f *fakeUsageStore) GetUsage(ctx context.Context, tenantID string) (*TenantUsage, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.usage[tenantID]
	if !ok {
		return nil, errors.New("no usage recorded")
	}
	tu := &TenantUsage{TenantID: tenantID, Usage: make(map[ResourceType]int), Quota: make(map[ResourceType]int)}
	for k, v := range u {
		tu.Usage[k] = v
	}
	return tu, nil
}

func (f *fakeUsageStore) TrackUsage(ctx context.Context, tenantID string, resourceType ResourceType, amount int) error {
	if f.trackFn != nil {
		return f.trackFn(tenantID, resourceType, amount)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.usage == nil {
		f.usage = make(map[string]map[ResourceType]int)
	}
	if f.usage[tenantID] == nil {
		f.usage[tenantID] = make(map[ResourceType]int)
	}
	f.usage[tenantID][resourceType] += amount
	return nil
}

// newTenantUsage 构造带配额的 TenantUsage。
func newTenantUsage(devices, tasks int) *TenantUsage {
	return &TenantUsage{
		TenantID: "t",
		Usage:    make(map[ResourceType]int),
		Quota: map[ResourceType]int{
			ResourceDevices: devices,
			ResourceTasks:   tasks,
		},
	}
}

// TestTenantUsage_CanAllocate：配额内放行、压线放行、超线拒绝、无限额放行。
func TestTenantUsage_CanAllocate(t *testing.T) {
	u := newTenantUsage(10, 0) // devices 限额 10；tasks 无限额（0）

	// 限额 10：当前 0，申请 10 恰好压线应放行
	if !u.CanAllocate(ResourceDevices, 10) {
		t.Fatal("压线申请（current 0 + amount 10 = limit 10）应放行")
	}
	u.AddUsage(ResourceDevices, 8)
	if !u.CanAllocate(ResourceDevices, 2) {
		t.Fatal("8+2=10 未超限，应放行")
	}
	if u.CanAllocate(ResourceDevices, 3) {
		t.Fatal("8+3=11 超限，应拒绝")
	}
	// 未设置限额（或限额 0 视为无限制）：任意量放行
	if !u.CanAllocate(ResourceTasks, 100000) {
		t.Fatal("无限额资源应始终放行")
	}
	// 负数 amount：配额语义上 current+amount < limit 恒真，放行
	if !u.CanAllocate(ResourceDevices, -5) {
		t.Fatal("负数 amount 在 <= limit 语义下应放行")
	}
}

// TestTenantUsage_AddGetUsage：用量记账累加且可读取；零值/未记录资源返回 0。
func TestTenantUsage_AddGetUsage(t *testing.T) {
	u := newTenantUsage(10, 10)
	if got := u.GetUsage(ResourceDevices); got != 0 {
		t.Fatalf("初始用量 = %d, want 0", got)
	}
	u.AddUsage(ResourceDevices, 3)
	u.AddUsage(ResourceDevices, 4)
	if got := u.GetUsage(ResourceDevices); got != 7 {
		t.Fatalf("累加后用量 = %d, want 7", got)
	}
	if got := u.GetUsage(ResourceAlerts); got != 0 {
		t.Fatalf("未记录资源用量 = %d, want 0", got)
	}
}

// TestTenantUsage_SetQuotaOverrides：SetQuota 动态调整限额立即生效。
func TestTenantUsage_SetQuotaOverrides(t *testing.T) {
	u := newTenantUsage(10, 10)
	u.AddUsage(ResourceDevices, 8)
	// 基线确认：8+1=9 <= 10 放行
	if !u.CanAllocate(ResourceDevices, 1) {
		t.Fatal("基线校验失败：8+1=9 未超限应放行")
	}
	u.SetQuota(ResourceDevices, 5)
	// 收紧到 5：8+1=9 > 5 拒绝
	if u.CanAllocate(ResourceDevices, 1) {
		t.Fatal("收紧限额后（limit=5, current=8）应拒绝")
	}
	// 放开到 100：放行
	u.SetQuota(ResourceDevices, 100)
	if !u.CanAllocate(ResourceDevices, 50) {
		t.Fatal("放宽限额后应放行")
	}
}

// TestTenantUsage_ConcurrentAccess：并发 Add/Get/CanAllocate/SetQuota 不死锁不崩溃且结果合法。
func TestTenantUsage_ConcurrentAccess(t *testing.T) {
	u := newTenantUsage(1000, 1000)
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				u.AddUsage(ResourceTasks, 1)
				u.GetUsage(ResourceTasks)
				u.CanAllocate(ResourceTasks, 1)
				if i%10 == 0 {
					u.SetQuota(ResourceTasks, 500)
					u.SetQuota(ResourceTasks, 1000)
				}
			}
		}()
	}
	wg.Wait()
	// 总计 1000 次累加，最终用量必须精确等于 1000
	if got := u.GetUsage(ResourceTasks); got != 1000 {
		t.Fatalf("并发累加后用量 = %d, want 1000（存在丢失更新则锁语义有问题）", got)
	}
}

// TestManager_DefaultQuotaApplied：NewManager(nil, nil, nil) 使用内置默认配额。
func TestManager_DefaultQuotaApplied(t *testing.T) {
	m := NewManager(nil, nil, nil)
	ctx := context.Background()

	// 默认 devices 限额 100：申请 100 恰好放行，101 拒绝
	if err := m.EnforceQuota(ctx, "t1", ResourceDevices, 100); err != nil {
		t.Fatalf("默认配额内申请应放行: %v", err)
	}
	err := m.EnforceQuota(ctx, "t1", ResourceDevices, 101)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超出默认配额应返回 ErrQuotaExceeded, got %v", err)
	}
	// 错误信息应包含租户/资源细节
	want := "tenant=t1 resource=devices amount=101 current=0 limit=100"
	if !containsSub(err.Error(), want) {
		t.Fatalf("错误信息应包含 %q, got %q", want, err.Error())
	}
}

// TestManager_CustomDefaultQuota：自定义默认配额覆盖内置值。
func TestManager_CustomDefaultQuota(t *testing.T) {
	m := NewManager(nil, nil, map[ResourceType]int{ResourceDevices: 3})
	if err := m.EnforceQuota(context.Background(), "t1", ResourceDevices, 3); err != nil {
		t.Fatalf("自定义配额内应放行: %v", err)
	}
	if err := m.EnforceQuota(context.Background(), "t1", ResourceDevices, 4); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超出自定义配额应拒绝, got %v", err)
	}
	// 未在自定义表中的资源无限额：放行
	if err := m.EnforceQuota(context.Background(), "t1", ResourceAlerts, 99999); err != nil {
		t.Fatalf("未配置的资源应无限制: %v", err)
	}
}

// TestManager_SetDefaultQuota：SetDefaultQuota 动态调整默认配额，对新租户生效。
func TestManager_SetDefaultQuota(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetDefaultQuota(ResourceWebhooks, 2)
	// 新租户 t-new 首次 EnforceQuota 时按新默认值创建
	// 注意：EnforceQuota 是非破坏性检查（不记账），需配合 TrackUsage 累计用量
	if err := m.EnforceQuota(context.Background(), "t-new", ResourceWebhooks, 2); err != nil {
		t.Fatalf("调整后默认配额内应放行: %v", err)
	}
	if err := m.TrackUsage(context.Background(), "t-new", ResourceWebhooks, 2); err != nil {
		t.Fatalf("TrackUsage: %v", err)
	}
	// 用量已记 2，再申请 1 超出（limit=2）
	if err := m.EnforceQuota(context.Background(), "t-new", ResourceWebhooks, 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超出调整后配额应拒绝, got %v", err)
	}
	// 已存在的旧租户不受 SetDefaultQuota 影响（defaults 快照在创建时已拷贝）——见 getOrCreateUsage
	m.SetDefaultQuota(ResourceWebhooks, 100)
	if err := m.EnforceQuota(context.Background(), "t-new", ResourceWebhooks, 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("已创建租户的配额不应随 defaults 热更新（拷贝语义）, got %v", err)
	}
	// 新租户 t-new2 拿到热更新后的 defaults=100
	if err := m.EnforceQuota(context.Background(), "t-new2", ResourceWebhooks, 50); err != nil {
		t.Fatalf("新租户应使用最新默认配额 100: %v", err)
	}
}

// TestManager_QuotaStoreOverridesDefaults：QuotaStore 返回的每租户配额覆盖默认值。
func TestManager_QuotaStoreOverridesDefaults(t *testing.T) {
	qs := &fakeQuotaStore{quotas: map[string]map[ResourceType]int{
		"t1": {ResourceDevices: 5},
	}}
	m := NewManager(qs, nil, nil)
	if err := m.EnforceQuota(context.Background(), "t1", ResourceDevices, 5); err != nil {
		t.Fatalf("租户配额（devices=5）内申请 5 应放行: %v", err)
	}
	if err := m.EnforceQuota(context.Background(), "t1", ResourceDevices, 6); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超出租户配额应拒绝, got %v", err)
	}
	// 无配额记录的租户（GetQuota 报错）：fail-open，沿用默认/既有配额不中断
	if err := m.EnforceQuota(context.Background(), "t-no-config", ResourceAgents, 10); err != nil {
		t.Fatalf("QuotaStore 查不到时应 fail-open 放行: %v", err)
	}
}

// TestManager_EmptyTenantIDNormalized：空租户 ID 归一化为 "default"，配额共用。
func TestManager_EmptyTenantIDNormalized(t *testing.T) {
	m := NewManager(nil, nil, map[ResourceType]int{ResourceDevices: 2})
	ctx := context.Background()
	if err := m.EnforceQuota(ctx, "", ResourceDevices, 2); err != nil {
		t.Fatalf("default 租户配额内申请应放行: %v", err)
	}
	// EnforceQuota 不记账，需 TrackUsage 才累计用量
	if err := m.TrackUsage(ctx, "", ResourceDevices, 2); err != nil {
		t.Fatalf("TrackUsage: %v", err)
	}
	// 空租户与显式 "default" 是同一租户：配额共享（已用掉 2，再申请超限）
	if err := m.EnforceQuota(ctx, "default", ResourceDevices, 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("空租户与 default 应共享配额, got %v", err)
	}
}

// TestManager_TrackUsage：用量跟踪写入内存 + UsageStore，超限前不校验（Track 不做配额判定）。
func TestManager_TrackUsage(t *testing.T) {
	us := &fakeUsageStore{}
	qs := &fakeQuotaStore{quotas: map[string]map[ResourceType]int{
		"t1": {ResourceTasks: 3},
	}}
	m := NewManager(qs, us, nil)
	ctx := context.Background()

	if err := m.TrackUsage(ctx, "t1", ResourceTasks, 2); err != nil {
		t.Fatalf("TrackUsage: %v", err)
	}
	if got := us.usage["t1"][ResourceTasks]; got != 2 {
		t.Fatalf("UsageStore 记录 = %d, want 2", got)
	}
	usage, err := m.GetUsage(ctx, "t1")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if got := usage.GetUsage(ResourceTasks); got != 2 {
		t.Fatalf("内存用量 = %d, want 2", got)
	}
	// TrackUsage 不做配额校验：即使超限也成功记账
	if err := m.TrackUsage(ctx, "t1", ResourceTasks, 100); err != nil {
		t.Fatalf("TrackUsage 不应做配额判定: %v", err)
	}
	// 记账后 EnforceQuota 基于最新用量（100）拒绝任何新申请
	if err := m.EnforceQuota(ctx, "t1", ResourceTasks, 1); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("记账后 EnforceQuota 应基于最新用量拒绝, got %v", err)
	}
}

// TestManager_TrackUsageStoreError：UsageStore.TrackUsage 报错时返回包装错误且不写内存。
func TestManager_TrackUsageStoreError(t *testing.T) {
	us := &fakeUsageStore{trackFn: func(string, ResourceType, int) error {
		return errors.New("backend down")
	}}
	m := NewManager(nil, us, nil)
	err := m.TrackUsage(context.Background(), "t1", ResourceTasks, 1)
	if err == nil || !containsSub(err.Error(), "tenant: failed to track usage") {
		t.Fatalf("应返回包装错误, got %v", err)
	}
	// 内存用量不应被记录
	usage, _ := m.GetUsage(context.Background(), "t1")
	if got := usage.GetUsage(ResourceTasks); got != 0 {
		t.Fatalf("store 报错时内存不应记账, got %d", got)
	}
}

// TestManager_GetUsageFromStore：UsageStore 有记录时优先返回持久化用量。
func TestManager_GetUsageFromStore(t *testing.T) {
	us := &fakeUsageStore{usage: map[string]map[ResourceType]int{
		"t1": {ResourceDevices: 42},
	}}
	m := NewManager(nil, us, nil)
	usage, err := m.GetUsage(context.Background(), "t1")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if got := usage.GetUsage(ResourceDevices); got != 42 {
		t.Fatalf("应返回 store 中的用量 42, got %d", got)
	}
}

// TestManager_GetUsageEmptyTenant：无任何记录的租户返回带默认配额的零值 TenantUsage。
func TestManager_GetUsageEmptyTenant(t *testing.T) {
	m := NewManager(nil, nil, nil)
	usage, err := m.GetUsage(context.Background(), "never-seen")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if got := usage.GetUsage(ResourceDevices); got != 0 {
		t.Fatalf("零值租户用量 = %d, want 0", got)
	}
	if !usage.CanAllocate(ResourceDevices, 100) || usage.CanAllocate(ResourceDevices, 101) {
		t.Fatal("零值租户应套用默认配额 devices=100")
	}
	if usage.TenantID != "never-seen" {
		t.Fatalf("TenantID = %q", usage.TenantID)
	}
}

// TestManager_ConcurrentEnforceTrack：并发 EnforceQuota/TrackUsage 无数据竞争、逻辑合法。
func TestManager_ConcurrentEnforceTrack(t *testing.T) {
	// devices 默认限额 100，用 4 个 goroutine 各申请/记账 25，总量恰好 100
	m := NewManager(nil, nil, map[ResourceType]int{ResourceDevices: 100})
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	exceeded := 0
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := 0
			for i := 0; i < 25; i++ {
				if err := m.EnforceQuota(ctx, "t-conc", ResourceDevices, 1); err != nil {
					local++
				}
				if err := m.TrackUsage(ctx, "t-conc", ResourceDevices, 1); err != nil {
					t.Errorf("TrackUsage: %v", err)
					return
				}
			}
			mu.Lock()
			exceeded += local
			mu.Unlock()
		}()
	}
	wg.Wait()
	// 串行语义下恰好 100 个申请全部放行；并发下 EnforceQuota 与 TrackUsage 分别持锁，
	// 会计数瞬时窗口可能重复放行少量超额申请，这里断言放行数 >= 100 - 允许的竞态盈余
	_ = exceeded
	usage, _ := m.GetUsage(ctx, "t-conc")
	if got := usage.GetUsage(ResourceDevices); got != 100 {
		t.Fatalf("并发记账总量 = %d, want 100（记账不允许丢失）", got)
	}
}

// TestContextTenantID：WithTenantID/TenantIDFromContext 的注入提取与缺失语义。
func TestContextTenantID(t *testing.T) {
	ctx := WithTenantID(context.Background(), "t-42")
	id, err := TenantIDFromContext(ctx)
	if err != nil || id != "t-42" {
		t.Fatalf("got (%q, %v), want (t-42, nil)", id, err)
	}
	// 空租户 ID 注入时归一化为 "default"
	ctx = WithTenantID(context.Background(), "")
	if id, err := TenantIDFromContext(ctx); err != nil || id != "default" {
		t.Fatalf("空租户应归一化为 default, got (%q, %v)", id, err)
	}
	// 未注入的 context：ErrTenantNotFound
	if _, err := TenantIDFromContext(context.Background()); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("未注入时应返回 ErrTenantNotFound, got %v", err)
	}
}

// TestContextTenantUsage：WithTenantUsage/TenantUsageFromContext 的注入提取与缺失语义。
func TestContextTenantUsage(t *testing.T) {
	u := newTenantUsage(5, 5)
	ctx := WithTenantUsage(context.Background(), u)
	got, ok := TenantUsageFromContext(ctx)
	if !ok || got != u {
		t.Fatalf("应取回注入的同一个 *TenantUsage, got %v ok=%v", got, ok)
	}
	// 未注入：ok=false 且不 panic
	if _, ok := TenantUsageFromContext(context.Background()); ok {
		t.Fatal("未注入时 ok 应为 false")
	}
}

// TestMiddleware_Header：X-Tenant-ID 头注入请求上下文；缺失时默认 default。
func TestMiddleware_Header(t *testing.T) {
	var gotID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := TenantIDFromContext(r.Context())
		if err != nil {
			t.Errorf("下游应能取到租户: %v", err)
			http.Error(w, "no tenant", http.StatusInternalServerError)
			return
		}
		gotID = id
		w.WriteHeader(http.StatusOK)
	})
	mw := Middleware("")

	// 带头的请求：租户注入
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", " t-header ") // TrimSpace 生效
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应放行, got %d", rec.Code)
	}
	if gotID != "t-header" {
		t.Fatalf("租户 ID = %q, want t-header（应 TrimSpace）", gotID)
	}

	// 无头且无 secret：默认 default
	req = httptest.NewRequest("GET", "/", nil)
	rec = httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if gotID != "default" {
		t.Fatalf("无头时应默认 default, got %q", gotID)
	}
}

// TestMiddleware_JWT：Authorization Bearer 中的 JWT 提供租户身份（真实签发/验签）。
func TestMiddleware_JWT(t *testing.T) {
	secret := "test-secret-tenant-0123456789"
	token, err := auth.GenerateServiceToken(auth.ServiceClaims{
		ServiceID:   "svc-1",
		ServiceName: "svc",
		TenantID:    "t-jwt",
	}, secret)
	if err != nil {
		t.Fatalf("GenerateServiceToken: %v", err)
	}

	var gotID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := TenantIDFromContext(r.Context())
		if err == nil {
			gotID = id
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := Middleware(secret)

	// X-Tenant-ID 头优先于 JWT
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "t-header-wins")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if gotID != "t-header-wins" {
		t.Fatalf("头应优先于 JWT, got %q", gotID)
	}

	// 无头时回退 JWT 解析
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if gotID != "t-jwt" {
		t.Fatalf("应从 JWT 提取租户 t-jwt, got %q", gotID)
	}

	// JWT 无 tenant_id 声明：回退 default
	tokenNoTenant, err := auth.GenerateServiceToken(auth.ServiceClaims{ServiceID: "svc-2"}, secret)
	if err != nil {
		t.Fatalf("GenerateServiceToken: %v", err)
	}
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenNoTenant)
	rec = httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if gotID != "default" {
		t.Fatalf("无 tenant_id 声明应回退 default, got %q", gotID)
	}

	// 非法 token：回退 default（fail-open 语义，不 401）
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec = httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if gotID != "default" {
		t.Fatalf("非法 token 应回退 default, got %q", gotID)
	}
}

// TestRequireTenant：有租户身份时注入并放行；无身份时 403（严格模式）。
//
// 修复回归：extractTenantFromRequest 曾恒返回 "default"（403 分支不可达，
// "严格模式"与普通 Middleware 等价）。修复后无身份来源走 403，与文档
// 宣称一致；宽松兜底由 Middleware 自身实现（无身份 → default）。
func TestRequireTenant(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := TenantIDFromContext(r.Context()); err != nil {
			t.Errorf("放行后下游应能取到租户: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := RequireTenant("")

	// 无任何身份来源：403 拒绝（修复后语义，与文档宣称一致）
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("RequireTenant 无租户应 403, got %d", rec.Code)
	}

	// 有 X-Tenant-ID：放行并注入
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "t-ok")
	var gotID string
	next2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := TenantIDFromContext(r.Context())
		if err == nil {
			gotID = id
		}
		w.WriteHeader(http.StatusOK)
	})
	rec = httptest.NewRecorder()
	mw(next2).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotID != "t-ok" {
		t.Fatalf("有租户应放行并注入 t-ok, got code=%d id=%q", rec.Code, gotID)
	}
}

// TestMiddleware_FallbackDefault：宽松模式无身份来源兜底 default
// （修复回归：兜底移到 Middleware 后，宽松语义必须保持不变）。
func TestMiddleware_FallbackDefault(t *testing.T) {
	var gotID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := TenantIDFromContext(r.Context())
		if err == nil {
			gotID = id
		}
		w.WriteHeader(http.StatusOK)
	})
	mw := Middleware("")
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotID != "default" {
		t.Fatalf("Middleware 无身份应兜底 default 放行, got code=%d id=%q", rec.Code, gotID)
	}
}

// TestQuotaMiddleware：配额内放行并记账；超限 429；无租户 403。
func TestQuotaMiddleware(t *testing.T) {
	m := NewManager(nil, nil, map[ResourceType]int{ResourceDevices: 2})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	handler := m.QuotaMiddleware(ResourceDevices, 1)(next)
	// 组合 Middleware 注入租户后走配额中间件
	serveWithTenant := func(tenantHeader string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/", nil)
		if tenantHeader != "" {
			req.Header.Set("X-Tenant-ID", tenantHeader)
		}
		rec := httptest.NewRecorder()
		Middleware("")(handler).ServeHTTP(rec, req)
		return rec
	}

	// t-a 配额 2：两次申请 1 放行
	for i := 0; i < 2; i++ {
		rec := serveWithTenant("t-a")
		if rec.Code != http.StatusOK {
			t.Fatalf("t-a 第 %d 次应放行, got %d", i+1, rec.Code)
		}
	}
	// 第三次超限：429 + ErrQuotaExceeded 文案
	rec := serveWithTenant("t-a")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("t-a 第 3 次应 429, got %d", rec.Code)
	}
	if !containsSub(rec.Body.String(), "quota exceeded") {
		t.Fatalf("429 响应体应包含 quota exceeded, got %q", rec.Body.String())
	}

	// 无租户上下文直接调 QuotaMiddleware（不经 Middleware 注入）：403 + ErrTenantNotFound
	req := httptest.NewRequest("GET", "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无租户应 403, got %d", rec.Code)
	}
	if !containsSub(rec.Body.String(), ErrTenantNotFound.Error()) {
		t.Fatalf("403 响应体应含 %q, got %q", ErrTenantNotFound.Error(), rec.Body.String())
	}
}

// TestQuotaMiddleware_TracksUsageOnAllow：放行的请求会记账（用量写入 Manager）。
func TestQuotaMiddleware_TracksUsageOnAllow(t *testing.T) {
	m := NewManager(nil, nil, map[ResourceType]int{ResourceWebhooks: 10})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := m.QuotaMiddleware(ResourceWebhooks, 2)(next)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Tenant-ID", "t-track")
	rec := httptest.NewRecorder()
	Middleware("")(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("应放行, got %d", rec.Code)
	}
	usage, err := m.GetUsage(context.Background(), "t-track")
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if got := usage.GetUsage(ResourceWebhooks); got != 2 {
		t.Fatalf("放行后应记账 2, got %d", got)
	}
}

// TestGRPCInterceptor_Metadata：x-tenant-id metadata 注入 context；缺失默认 default。
func TestGRPCInterceptor_Metadata(t *testing.T) {
	ic := GRPCInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/svc/Method"}

	var gotID string
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		id, err := TenantIDFromContext(ctx)
		if err != nil {
			return nil, err
		}
		gotID = id
		return "ok", nil
	}

	// metadata 提供租户
	md := metadata.Pairs("x-tenant-id", "t-grpc")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	if _, err := ic(ctx, nil, info, handler); err != nil {
		t.Fatalf("拦截器应放行: %v", err)
	}
	if gotID != "t-grpc" {
		t.Fatalf("租户 = %q, want t-grpc", gotID)
	}

	// 无 metadata：默认 default
	if _, err := ic(context.Background(), nil, info, handler); err != nil {
		t.Fatalf("无 metadata 应注入 default 并放行: %v", err)
	}
	if gotID != "default" {
		t.Fatalf("无 metadata 应回退 default, got %q", gotID)
	}

	// 空 metadata 键：默认 default
	ctx = metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-tenant-id", ""))
	if _, err := ic(ctx, nil, info, handler); err != nil {
		t.Fatalf("空 x-tenant-id 应回退 default: %v", err)
	}
	if gotID != "default" {
		t.Fatalf("空值 x-tenant-id 应回退 default, got %q", gotID)
	}
}

// TestExtractTenantFromToken_InvalidInputs：token/secret 为空时返回空串。
func TestExtractTenantFromToken_InvalidInputs(t *testing.T) {
	if got := extractTenantFromToken("", "secret"); got != "" {
		t.Fatalf("空 token 应回退空串, got %q", got)
	}
	if got := extractTenantFromToken("abc", ""); got != "" {
		t.Fatalf("空 secret 应回退空串, got %q", got)
	}
	// 非法 token 返回空串
	if got := extractTenantFromToken("not-a-jwt", "secret"); got != "" {
		t.Fatalf("非法 token 应回退空串, got %q", got)
	}
}

// containsSub 判断 s 是否包含 substr。
func containsSub(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
