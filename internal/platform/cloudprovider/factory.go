package cloudprovider

import (
	"context"
	"sync"
)

// AdapterFactory 按 ProviderType 注册/查找云厂商适配器。
type AdapterFactory struct {
	mu       sync.RWMutex
	adapters map[ProviderType]CloudProvider
}

// NewAdapterFactory 构造工厂并注册内建所有云厂商适配器。
func NewAdapterFactory() *AdapterFactory {
	f := &AdapterFactory{adapters: make(map[ProviderType]CloudProvider)}
	RegisterBuiltIns(f)
	return f
}

// Register 注册适配器，以其 ProviderType 为键（同一类型重复注册会覆盖）。
func (f *AdapterFactory) Register(p CloudProvider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adapters[p.ProviderType()] = p
}

// Get 返回指定类型的适配器；未注册时返回 (nil, false)。
func (f *AdapterFactory) Get(t ProviderType) (CloudProvider, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.adapters[t]
	return p, ok
}

// SupportedTypes 返回已注册厂商类型列表（顺序不固定）。
func (f *AdapterFactory) SupportedTypes() []ProviderType {
	f.mu.RLock()
	defer f.mu.RUnlock()
	types := make([]ProviderType, 0, len(f.adapters))
	for t := range f.adapters {
		types = append(types, t)
	}
	return types
}

// 编译期断言：context.Context 已作为所有接口方法首参数。
var _ = context.Background
