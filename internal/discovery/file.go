package discovery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/internal/logx"
)

// fileDiscoveryPollInterval 是 FileDiscovery 轮询 backing file mtime 的间隔。
const fileDiscoveryPollInterval = 2 * time.Second

// fileService 是 FileDiscovery backing file 中的单个实例记录。
//
// 字段与 Service 的映射：name→Name，address→Addr，port→Port，
// healthy→Healthy，meta→Metadata；Service.ID 不落盘，读取时按 address:port 派生。
type fileService struct {
	Name    string            `json:"name"`
	Address string            `json:"address"`
	Port    int               `json:"port"`
	Healthy bool              `json:"healthy"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// fileDocument 是 FileDiscovery backing file 的顶层 JSON 文档。
type fileDocument struct {
	Services []fileService `json:"services"`
}

// FileDiscovery 基于 JSON 文件的服务发现后端（单机/无外部注册中心的 opt-in 选择）。
//
// 语义：
//   - Register：按 (Name, Addr, Port) 幂等 upsert；文件缺失或损坏时重建。
//   - Deregister：移除匹配实例；不存在返回 ErrNotRegistered。
//   - List：只返回健康实例；文件缺失/损坏/无实例时返回空切片 + nil（并记录 WARN）。
//   - Watch：每 2s 轮询文件 mtime，内容变化时向所有订阅者推送最新健康快照；
//     ctx 取消时关闭 channel；同一服务的多个订阅者共享一个轮询器。
//   - MarkHealthy：更新健康状态并落盘；实例不存在返回 ErrNotRegistered。
//
// 持久化：写操作使用同目录临时文件 + rename 原子替换，权限 0600；读-改-写全程持锁，
// 避免并发注册/注销互相覆盖。
type FileDiscovery struct {
	path string

	mu sync.Mutex // 串行化文件读-改-写

	watchMu     sync.Mutex
	watchers    map[string]map[int]chan []Service
	nextWatcher int
	fingerprint map[string]string
	pollCancel  context.CancelFunc
}

// NewFileDiscovery 构造 FileDiscovery。path 为空时返回错误（配置层已 fail-fast）。
func NewFileDiscovery(path string) (*FileDiscovery, error) {
	if path == "" {
		return nil, fmt.Errorf("服务发现文件路径为空")
	}
	return &FileDiscovery{
		path:        path,
		watchers:    make(map[string]map[int]chan []Service),
		fingerprint: make(map[string]string),
	}, nil
}

var _ ServiceDiscovery = (*FileDiscovery)(nil)

// instanceID 派生实例 ID（address:port）。
func instanceID(address string, port int) string {
	return fmt.Sprintf("%s:%d", address, port)
}

// cloneMeta 深拷贝元数据 map。
func cloneMeta(meta map[string]string) map[string]string {
	if meta == nil {
		return nil
	}
	out := make(map[string]string, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

// Register 注册或更新一个服务实例（按 Name+Addr+Port 幂等）。
// 文件缺失或损坏时按空文档处理并重建（记录 WARN）。
func (d *FileDiscovery) Register(ctx context.Context, svc Service) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	doc, err := d.readLocked()
	if err != nil {
		logx.Warn(ctx, "服务发现文件缺失或损坏，注册时重建", "path", d.path, "error", err.Error())
		doc = fileDocument{}
	}
	replaced := false
	for i := range doc.Services {
		cur := doc.Services[i]
		if cur.Name == svc.Name && cur.Address == svc.Addr && cur.Port == svc.Port {
			doc.Services[i].Healthy = svc.Healthy
			doc.Services[i].Meta = cloneMeta(svc.Metadata)
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Services = append(doc.Services, fileService{
			Name:    svc.Name,
			Address: svc.Addr,
			Port:    svc.Port,
			Healthy: svc.Healthy,
			Meta:    cloneMeta(svc.Metadata),
		})
	}
	return d.writeLocked(doc)
}

// Deregister 移除匹配实例；不存在返回 ErrNotRegistered。
func (d *FileDiscovery) Deregister(ctx context.Context, serviceName, address string, port int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	doc, err := d.readLocked()
	if err != nil {
		return ErrNotRegistered
	}
	idx := -1
	for i := range doc.Services {
		cur := doc.Services[i]
		if cur.Name == serviceName && cur.Address == address && cur.Port == port {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotRegistered
	}
	doc.Services = append(doc.Services[:idx], doc.Services[idx+1:]...)
	return d.writeLocked(doc)
}

// List 返回指定服务名的健康实例；文件缺失/损坏/无实例时返回空切片 + nil。
func (d *FileDiscovery) List(ctx context.Context, serviceName string) ([]Service, error) {
	return d.snapshot(ctx, serviceName, true), nil
}

// Watch 订阅指定服务名的健康实例快照。
//
// 首次订阅时推送当前快照；之后每 fileDiscoveryPollInterval 轮询文件，
// 内容指纹变化时推送全量快照。ctx 取消时关闭 channel 并移除订阅者。
func (d *FileDiscovery) Watch(ctx context.Context, serviceName string) (<-chan []Service, error) {
	ch := make(chan []Service, 16)

	d.watchMu.Lock()
	if d.watchers[serviceName] == nil {
		d.watchers[serviceName] = make(map[int]chan []Service)
	}
	id := d.nextWatcher
	d.nextWatcher++
	d.watchers[serviceName][id] = ch
	snap := d.snapshot(ctx, serviceName, true)
	d.fingerprint[serviceName] = snapshotFingerprint(snap)
	if d.pollCancel == nil {
		pollCtx, cancel := context.WithCancel(context.Background())
		d.pollCancel = cancel
		go d.pollLoop(pollCtx)
	}
	d.watchMu.Unlock()

	select {
	case ch <- snap:
	case <-ctx.Done():
	}

	go func() {
		<-ctx.Done()
		d.removeWatcher(serviceName, id)
	}()
	if ctx.Err() != nil {
		d.removeWatcher(serviceName, id)
	}
	return ch, nil
}

// MarkHealthy 更新指定实例的健康状态并落盘；不存在返回 ErrNotRegistered。
func (d *FileDiscovery) MarkHealthy(ctx context.Context, serviceName, address string, port int, healthy bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	doc, err := d.readLocked()
	if err != nil {
		return ErrNotRegistered
	}
	for i := range doc.Services {
		cur := doc.Services[i]
		if cur.Name == serviceName && cur.Address == address && cur.Port == port {
			doc.Services[i].Healthy = healthy
			return d.writeLocked(doc)
		}
	}
	return ErrNotRegistered
}

// readLocked 读取并解析 backing file。调用方必须持有 d.mu。
func (d *FileDiscovery) readLocked() (fileDocument, error) {
	var doc fileDocument
	raw, err := os.ReadFile(d.path)
	if err != nil {
		return doc, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return doc, fmt.Errorf("服务发现文件为空")
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fileDocument{}, err
	}
	return doc, nil
}

// writeLocked 原子写入 backing file（同目录临时文件 + rename，0600）。调用方必须持有 d.mu。
func (d *FileDiscovery) writeLocked(doc fileDocument) error {
	if doc.Services == nil {
		doc.Services = []fileService{}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化服务发现文件失败: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(d.path)
	tmp, err := os.CreateTemp(dir, ".opsmesh-discovery-*.tmp")
	if err != nil {
		return fmt.Errorf("创建服务发现临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		remove()
		return fmt.Errorf("设置服务发现文件权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		remove()
		return fmt.Errorf("写入服务发现临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return fmt.Errorf("同步服务发现临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("关闭服务发现临时文件失败: %w", err)
	}
	if err := os.Rename(tmpName, d.path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("替换服务发现文件失败: %w", err)
	}
	return nil
}

// snapshot 生成指定服务名的健康实例快照（按 ID/Addr/Port 稳定排序）。
// warnOnErr 控制缺失/损坏文件是否输出 WARN（List/Watch 输出，轮询内部不输出以避免刷屏）。
func (d *FileDiscovery) snapshot(ctx context.Context, serviceName string, warnOnErr bool) []Service {
	d.mu.Lock()
	defer d.mu.Unlock()

	doc, err := d.readLocked()
	if err != nil {
		if warnOnErr {
			logx.Warn(ctx, "服务发现文件不可读，按空列表处理", "path", d.path, "error", err.Error())
		}
		return []Service{}
	}
	out := make([]Service, 0, len(doc.Services))
	for _, inst := range doc.Services {
		if inst.Name != serviceName || !inst.Healthy {
			continue
		}
		out = append(out, Service{
			ID:       instanceID(inst.Address, inst.Port),
			Name:     inst.Name,
			Addr:     inst.Address,
			Port:     inst.Port,
			Metadata: cloneMeta(inst.Meta),
			Healthy:  true,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Addr != out[j].Addr {
			return out[i].Addr < out[j].Addr
		}
		return out[i].Port < out[j].Port
	})
	return out
}

// snapshotFingerprint 计算快照内容指纹，用于判断文件内容是否真的变化。
func snapshotFingerprint(snap []Service) string {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Sprint(snap)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// pollLoop 周期性检查 backing file；内容变化时向订阅者推送快照。
func (d *FileDiscovery) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(fileDiscoveryPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.pollOnce(ctx)
		}
	}
}

// pollOnce 检查所有已订阅服务的内容指纹，仅对发生变化的服务推送。
// 持有 watchMu 期间完成发送，避免与 removeWatcher 的 close 竞争。
func (d *FileDiscovery) pollOnce(ctx context.Context) {
	d.watchMu.Lock()
	defer d.watchMu.Unlock()
	for name, watchers := range d.watchers {
		if len(watchers) == 0 {
			continue
		}
		snap := d.snapshot(ctx, name, false)
		fp := snapshotFingerprint(snap)
		if d.fingerprint[name] == fp {
			continue
		}
		d.fingerprint[name] = fp
		for _, ch := range watchers {
			select {
			case ch <- snap:
			default:
			}
		}
	}
}

// removeWatcher 移除订阅者并关闭 channel；最后一个订阅者移除后停止轮询器。
func (d *FileDiscovery) removeWatcher(serviceName string, id int) {
	d.watchMu.Lock()
	defer d.watchMu.Unlock()

	watchers, ok := d.watchers[serviceName]
	if !ok {
		return
	}
	ch, ok := watchers[id]
	if !ok {
		return
	}
	delete(watchers, id)
	if len(watchers) == 0 {
		delete(d.watchers, serviceName)
		delete(d.fingerprint, serviceName)
	}
	close(ch)
	if len(d.watchers) == 0 && d.pollCancel != nil {
		d.pollCancel()
		d.pollCancel = nil
	}
}
