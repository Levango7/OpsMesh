// memory_config_secret_test.go 验证 MemoryStore 配置/密钥/服务发现读路径的
// 深拷贝语义与并发安全（C-3）。
//
// 覆盖：
//   - 副本修改不影响内部态：GetConfig/GetSecret/RegisterService 等读方法返回深拷贝，
//     外部修改返回值（含 Metadata map）不应污染内部数据。
//   - 并发读写无 race：goroutine 循环写 + goroutine 循环读，go test -race 检测。
package store

import (
	"sync"
	"testing"
	"time"
)

// TestMemoryConfigCloneOnRead 验证 Config 读路径返回深拷贝副本，外部修改不影响内部状态。
func TestMemoryConfigCloneOnRead(t *testing.T) {
	m := NewMemoryStore()
	tenant := "t1"

	// 写入初始配置。
	m.SetConfig(&ConfigItem{
		Key: "app/db/pool", Value: "size=10", Format: "properties",
		TenantID: tenant, UpdatedBy: "alice",
	})

	// GetConfig 返回副本，修改后内部应不受影响。
	got1, ok := m.GetConfig(tenant, "app/db/pool")
	if !ok {
		t.Fatalf("GetConfig 应找到配置")
	}
	got1.Value = "MUTATED"
	got1.Version = 999
	got2, ok := m.GetConfig(tenant, "app/db/pool")
	if !ok {
		t.Fatalf("再次 GetConfig 应找到配置")
	}
	if got2.Value == "MUTATED" {
		t.Fatalf("GetConfig 返回内部指针，外部修改污染了内部: Value=%q", got2.Value)
	}
	if got2.Version == 999 {
		t.Fatalf("GetConfig 返回内部指针，外部修改污染了内部: Version=%d", got2.Version)
	}

	// ListConfigs 返回副本。
	list1 := m.ListConfigs(tenant)
	if len(list1) != 1 {
		t.Fatalf("ListConfigs 应返回 1 项, 得到 %d", len(list1))
	}
	list1[0].Value = "LIST_MUTATED"
	list2 := m.ListConfigs(tenant)
	if list2[0].Value == "LIST_MUTATED" {
		t.Fatalf("ListConfigs 返回内部指针，外部修改污染了内部")
	}

	// PublishConfig 返回副本。
	pub1, ok := m.PublishConfig(tenant, "app/db/pool")
	if !ok {
		t.Fatalf("PublishConfig 应找到配置")
	}
	pub1.Value = "PUB_MUTATED"
	pub2, ok := m.PublishConfig(tenant, "app/db/pool")
	if !ok {
		t.Fatalf("再次 PublishConfig 应找到配置")
	}
	if pub2.Value == "PUB_MUTATED" {
		t.Fatalf("PublishConfig 返回内部指针，外部修改污染了内部")
	}

	// ConfigHistory 返回副本：再 Set 一次产生历史。
	m.SetConfig(&ConfigItem{
		Key: "app/db/pool", Value: "size=20", Format: "properties",
		TenantID: tenant, UpdatedBy: "bob",
	})
	hist1 := m.ConfigHistory(tenant, "app/db/pool")
	if len(hist1) == 0 {
		t.Fatalf("ConfigHistory 应有历史版本")
	}
	hist1[0].Value = "HIST_MUTATED"
	hist2 := m.ConfigHistory(tenant, "app/db/pool")
	if hist2[0].Value == "HIST_MUTATED" {
		t.Fatalf("ConfigHistory 返回内部指针，外部修改污染了内部")
	}
}

// TestMemorySecretCloneOnRead 验证 Secret 读路径返回深拷贝副本。
func TestMemorySecretCloneOnRead(t *testing.T) {
	m := NewMemoryStore()
	tenant := "t1"

	m.SetSecret(&SecretItem{Key: "app/db/pass", Value: "secret-v1", KeyType: "passphrase"}, tenant)

	// GetSecret 返回副本。
	got1, ok := m.GetSecret(tenant, "app/db/pass")
	if !ok {
		t.Fatalf("GetSecret 应找到密钥")
	}
	got1.Value = "MUTATED"
	got2, ok := m.GetSecret(tenant, "app/db/pass")
	if !ok {
		t.Fatalf("再次 GetSecret 应找到密钥")
	}
	if got2.Value == "MUTATED" {
		t.Fatalf("GetSecret 返回内部指针，外部修改污染了内部")
	}

	// ListSecrets 返回副本。
	list1 := m.ListSecrets(tenant)
	if len(list1) != 1 {
		t.Fatalf("ListSecrets 应返回 1 项")
	}
	list1[0].Version = 999
	list2 := m.ListSecrets(tenant)
	if list2[0].Version == 999 {
		t.Fatalf("ListSecrets 返回内部指针，外部修改污染了内部")
	}

	// SecretVersions 返回副本。
	vers1 := m.SecretVersions(tenant, "app/db/pass")
	if len(vers1) == 0 {
		t.Fatalf("SecretVersions 应有版本")
	}
	vers1[0].Version = 999
	vers2 := m.SecretVersions(tenant, "app/db/pass")
	if vers2[0].Version == 999 {
		t.Fatalf("SecretVersions 返回内部指针，外部修改污染了内部")
	}
}

// TestMemoryDiscoveryCloneOnRead 验证 Discovery 读路径返回深拷贝副本（含 Metadata map）。
func TestMemoryDiscoveryCloneOnRead(t *testing.T) {
	m := NewMemoryStore()

	inst := &ServiceInstance{
		ServiceID: "svc-1", ServiceName: "orders", Address: "10.0.0.1", Port: 8080,
		Metadata: map[string]string{"region": "us-east", "weight": "100"},
		Status:   "healthy", TenantID: "t1",
	}
	m.RegisterService(inst)

	// RegisterService 返回副本（含 Metadata map 深拷贝）。
	reg1 := m.RegisterService(inst)
	reg1.Address = "MUTATED"
	reg1.Metadata["region"] = "MUTATED"
	reg2 := m.RegisterService(inst)
	if reg2.Address == "MUTATED" {
		t.Fatalf("RegisterService 返回内部指针，外部修改污染了内部")
	}
	if reg2.Metadata["region"] == "MUTATED" {
		t.Fatalf("RegisterService 返回的 Metadata map 与内部共享，外部修改污染了内部")
	}

	// ServiceInstances 返回副本。
	si1 := m.ServiceInstances("t1", "orders")
	if len(si1) != 1 {
		t.Fatalf("ServiceInstances 应返回 1 项")
	}
	si1[0].Address = "SI_MUTATED"
	si1[0].Metadata["weight"] = "SI_MUTATED"
	si2 := m.ServiceInstances("t1", "orders")
	if si2[0].Address == "SI_MUTATED" {
		t.Fatalf("ServiceInstances 返回内部指针，外部修改污染了内部")
	}
	if si2[0].Metadata["weight"] == "SI_MUTATED" {
		t.Fatalf("ServiceInstances 返回的 Metadata map 与内部共享")
	}

	// AllServices 返回副本。
	all1 := m.AllServices("t1")
	if len(all1) != 1 {
		t.Fatalf("AllServices 应返回 1 项")
	}
	all1[0].ServiceName = "ALL_MUTATED"
	all2 := m.AllServices("t1")
	if all2[0].ServiceName == "ALL_MUTATED" {
		t.Fatalf("AllServices 返回内部指针，外部修改污染了内部")
	}

	// StaleServices 返回副本：注册一个实例并等待其过期。
	m.RegisterService(&ServiceInstance{
		ServiceID: "svc-stale", ServiceName: "stale-svc", Address: "10.0.0.9",
		Status: "healthy", TenantID: "t1",
	})
	time.Sleep(20 * time.Millisecond)
	stale1 := m.StaleServices("t1", 10*time.Millisecond)
	if len(stale1) == 0 {
		t.Fatalf("StaleServices 应有过期实例")
	}
	stale1[0].Address = "STALE_MUTATED"
	stale2 := m.StaleServices("t1", 10*time.Millisecond)
	if len(stale2) == 0 {
		t.Fatalf("StaleServices 应仍有过期实例")
	}
	// 在两个实例中找到被修改的那个不应出现 STALE_MUTATED。
	for _, s := range stale2 {
		if s.Address == "STALE_MUTATED" {
			t.Fatalf("StaleServices 返回内部指针，外部修改污染了内部")
		}
	}
}

// TestMemoryConfigSecretConcurrentReadWrite 验证 Config/Secret 并发读写无 race。
// 需 go test -race 运行；若存在 race，-race 检测器会报错。
func TestMemoryConfigSecretConcurrentReadWrite(t *testing.T) {
	m := NewMemoryStore()
	tenant := "t1"

	// 预置初始数据。
	m.SetConfig(&ConfigItem{Key: "k", Value: "v0", TenantID: tenant})
	m.SetSecret(&SecretItem{Key: "s", Value: "v0", KeyType: "passphrase"}, tenant)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 写 goroutine：循环 SetConfig/SetSecret。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			m.SetConfig(&ConfigItem{Key: "k", Value: "v", TenantID: tenant})
			m.SetSecret(&SecretItem{Key: "s", Value: "v", KeyType: "passphrase"}, tenant)
		}
	}()

	// 读 goroutine：循环 GetConfig/GetSecret/ListConfigs/ListSecrets/ConfigHistory/SecretVersions。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if c, ok := m.GetConfig(tenant, "k"); ok {
				_ = c.Value // 触发读
			}
			if s, ok := m.GetSecret(tenant, "s"); ok {
				_ = s.Value
			}
			for _, c := range m.ListConfigs(tenant) {
				_ = c.Value
			}
			for _, s := range m.ListSecrets(tenant) {
				_ = s.Version
			}
			for _, h := range m.ConfigHistory(tenant, "k") {
				_ = h.Version
			}
			for _, v := range m.SecretVersions(tenant, "s") {
				_ = v.Version
			}
			if p, ok := m.PublishConfig(tenant, "k"); ok {
				_ = p.Value
			}
		}
	}()

	// 运行 200ms 让并发跑起来。
	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestMemoryDiscoveryConcurrentReadWrite 验证 Discovery 并发读写无 race。
func TestMemoryDiscoveryConcurrentReadWrite(t *testing.T) {
	m := NewMemoryStore()

	inst := &ServiceInstance{
		ServiceID: "svc-1", ServiceName: "orders", Address: "10.0.0.1", Port: 8080,
		Metadata: map[string]string{"region": "us-east"}, Status: "healthy", TenantID: "t1",
	}
	m.RegisterService(inst)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// 写 goroutine：循环 RegisterService（含 Metadata map）/HeartbeatService。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			inst2 := &ServiceInstance{
				ServiceID: "svc-1", ServiceName: "orders", Address: "10.0.0.1", Port: 8080,
				Metadata: map[string]string{"region": "us-east", "tick": "x"},
				Status:   "healthy", TenantID: "t1",
			}
			m.RegisterService(inst2)
			m.HeartbeatService("t1", "svc-1", "healthy")
		}
	}()

	// 读 goroutine：循环 ServiceInstances/AllServices/StaleServices（读 Metadata map）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, s := range m.ServiceInstances("t1", "orders") {
				_ = s.Address
				_ = s.Metadata["region"]
			}
			for _, s := range m.AllServices("t1") {
				_ = s.ServiceName
				_ = s.Metadata["region"]
			}
			for _, s := range m.StaleServices("t1", 1*time.Hour) {
				_ = s.Status
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}
