package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Levango7/OpsMesh/services/config-svc/internal/models"
)

// Store is the interface for config-svc persistence.
type Store interface {
	// Config operations
	GetConfig(tenantID, key string) (*models.ConfigEntry, bool)
	SetConfig(item *models.ConfigEntry) *models.ConfigEntry
	DeleteConfig(tenantID, key string) bool
	ListConfigs(tenantID string) []*models.ConfigEntry
	GetConfigHistory(tenantID, key string) []*models.ConfigEntry
	RollbackConfig(tenantID, key string, version int) (*models.ConfigEntry, bool)

	// Secret operations
	CreateSecret(item *models.SecretEntry) *models.SecretEntry
	GetSecret(tenantID, key string) (*models.SecretEntry, bool)
	UpdateSecret(item *models.SecretEntry) *models.SecretEntry
	DeleteSecret(tenantID, key string) bool
	ListSecrets(tenantID string) []*models.SecretMeta
	RotateSecret(tenantID, key, newValue string) *models.SecretMeta

	// Channel operations
	CreateChannel(item *models.ChannelEntry) *models.ChannelEntry
	GetChannel(id string) *models.ChannelEntry
	UpdateChannel(item *models.ChannelEntry) bool
	DeleteChannel(id, tenantID string) bool
	ListChannels(tenantID string) []*models.ChannelEntry

	// Template operations
	CreateTemplate(item *models.TemplateEntry) *models.TemplateEntry
	GetTemplate(id string) *models.TemplateEntry
	UpdateTemplate(item *models.TemplateEntry) bool
	DeleteTemplate(id, tenantID string) bool
	ListTemplates(tenantID string) []*models.TemplateEntry
}

// MemoryStore is an in-memory implementation of Store.
type MemoryStore struct {
	mu            sync.RWMutex
	configs       map[string]*models.ConfigEntry   // key: tenantID/key
	configHistory map[string][]*models.ConfigEntry // key: tenantID/key
	secrets       map[string]*models.SecretEntry   // key: tenantID/key
	channels      map[string]*models.ChannelEntry  // key: id
	templates     map[string]*models.TemplateEntry // key: id
	encryptionKey []byte
	maxHistory    int
}

// NewMemoryStore creates a new MemoryStore.
func NewMemoryStore(encryptionKey string, maxHistory int) *MemoryStore {
	if maxHistory <= 0 {
		maxHistory = 50
	}
	return &MemoryStore{
		configs:       make(map[string]*models.ConfigEntry),
		configHistory: make(map[string][]*models.ConfigEntry),
		secrets:       make(map[string]*models.SecretEntry),
		channels:      make(map[string]*models.ChannelEntry),
		templates:     make(map[string]*models.TemplateEntry),
		encryptionKey: deriveKey(encryptionKey),
		maxHistory:    maxHistory,
	}
}

func deriveKey(passphrase string) []byte {
	h := sha256.Sum256([]byte(passphrase))
	return h[:]
}

// secretCipherPrefix 标记「本包加密原语产出的密文」并带格式版本号（便于日后换算法时区分）。
//
// 为什么用前缀而不是「试解密失败就当作明文」：后者把两类完全不同的情况混为一谈——
// 升级前的历史明文存量（应当放行并提示轮换）与密钥不匹配/数据损坏（应当硬失败）。
// 靠猜的判据在真实事故里的表现是「密文被原样当成明文返回给调用方」。
const secretCipherPrefix = "enc:v1:"

// encryptSecret 加密机密：AES-256-GCM，随机 nonce 前置，base64，带版本前缀。
//
// 两后端**共用**这一对原语（TD-61 的教训：同一职责各写一份必然漂移）——
// config-svc 的 MySQL 后端曾把值原样落库（构造时接了 encryptionKey 却从不使用），
// 而内存后端是加密的：同一份配置、两个后端、两种安全语义。
func encryptSecret(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return secretCipherPrefix + base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}

// decryptSecret 解出机密明文。
//
// 返回 (plaintext, legacy, err)：legacy=true 表示读到的是**升级前的未加密存量**（原样返回，
// 调用方应记录告警并提示轮换）；err 非 nil 表示「带版本前缀但解不开」——密钥不匹配或数据
// 损坏，调用方必须硬失败，不得把密文当明文用。
func decryptSecret(key []byte, stored string) (string, bool, error) {
	raw, ok := strings.CutPrefix(stored, secretCipherPrefix)
	if !ok {
		return stored, true, nil
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", false, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", false, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false, err
	}
	if len(data) < gcm.NonceSize() {
		return "", false, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", false, err
	}
	return string(out), false, nil
}

func (s *MemoryStore) encrypt(plaintext string) (string, error) {
	return encryptSecret(s.encryptionKey, plaintext)
}

func configKey(tenantID, key string) string {
	return tenantID + "/" + key
}

// ===================== Config Operations =====================

func (s *MemoryStore) GetConfig(tenantID, key string) (*models.ConfigEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.configs[configKey(tenantID, key)]
	if !ok {
		return nil, false
	}
	cp := *entry
	return &cp, true
}

func (s *MemoryStore) SetConfig(item *models.ConfigEntry) *models.ConfigEntry {
	if item == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	ck := configKey(item.TenantID, item.Key)

	existing, exists := s.configs[ck]
	if exists {
		item.Version = existing.Version + 1
		item.CreatedAt = existing.CreatedAt
		history := s.configHistory[ck]
		history = append(history, existing)
		if len(history) > s.maxHistory {
			history = history[len(history)-s.maxHistory:]
		}
		s.configHistory[ck] = history
	} else {
		item.Version = 1
		item.CreatedAt = now
	}
	item.UpdatedAt = now

	cp := *item
	s.configs[ck] = &cp
	return item
}

func (s *MemoryStore) DeleteConfig(tenantID, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := configKey(tenantID, key)
	if _, ok := s.configs[ck]; !ok {
		return false
	}
	delete(s.configs, ck)
	delete(s.configHistory, ck)
	return true
}

func (s *MemoryStore) ListConfigs(tenantID string) []*models.ConfigEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.ConfigEntry, 0, len(s.configs))
	for _, v := range s.configs {
		if tenantID != "" && v.TenantID != tenantID {
			continue
		}
		cp := *v
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}

func (s *MemoryStore) GetConfigHistory(tenantID, key string) []*models.ConfigEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	history := s.configHistory[configKey(tenantID, key)]
	out := make([]*models.ConfigEntry, 0, len(history))
	for _, h := range history {
		cp := *h
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Version < out[j].Version
	})
	return out
}

func (s *MemoryStore) RollbackConfig(tenantID, key string, version int) (*models.ConfigEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ck := configKey(tenantID, key)
	history := s.configHistory[ck]
	var target *models.ConfigEntry
	for _, h := range history {
		if h.Version == version {
			target = h
			break
		}
	}
	if target == nil {
		return nil, false
	}

	now := time.Now()
	newEntry := &models.ConfigEntry{
		ID:          target.ID,
		TenantID:    target.TenantID,
		Key:         target.Key,
		Value:       target.Value,
		Format:      target.Format,
		Description: target.Description,
		CreatedAt:   target.CreatedAt,
		UpdatedAt:   now,
	}

	existing, exists := s.configs[ck]
	if exists {
		newEntry.Version = existing.Version + 1
		history = append(history, existing)
		if len(history) > s.maxHistory {
			history = history[len(history)-s.maxHistory:]
		}
		s.configHistory[ck] = history
	} else {
		newEntry.Version = 1
	}

	cp := *newEntry
	s.configs[ck] = &cp
	return newEntry, true
}

// ===================== Secret Operations =====================

func (s *MemoryStore) CreateSecret(item *models.SecretEntry) *models.SecretEntry {
	if item == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	ck := configKey(item.TenantID, item.Key)
	item.Version = 1
	item.CreatedAt = now
	item.UpdatedAt = now

	encrypted, err := s.encrypt(item.Value)
	if err != nil {
		// 不静默降级存明文：加密失败宁可写入失败（密钥/随机源异常是可观测的故障）。
		log.Printf("[store] CreateSecret 加密失败: %v", err)
		return nil
	}

	cp := *item
	cp.Value = encrypted
	s.secrets[ck] = &cp
	return item
}

func (s *MemoryStore) GetSecret(tenantID, key string) (*models.SecretEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.secrets[configKey(tenantID, key)]
	if !ok {
		return nil, false
	}

	decrypted, legacy, err := decryptSecret(s.encryptionKey, entry.Value)
	if err != nil {
		log.Printf("[store] GetSecret 解密失败（密钥不匹配或数据损坏）: tenant=%s key=%s: %v", tenantID, key, err)
		return nil, false
	}
	if legacy {
		log.Printf("[store] GetSecret 读到未加密的历史存量: tenant=%s key=%s——建议轮换（RotateSecret）以加密落库", tenantID, key)
	}

	cp := *entry
	cp.Value = decrypted
	return &cp, true
}

func (s *MemoryStore) UpdateSecret(item *models.SecretEntry) *models.SecretEntry {
	if item == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ck := configKey(item.TenantID, item.Key)
	existing, ok := s.secrets[ck]
	if !ok {
		return nil
	}

	encrypted, err := s.encrypt(item.Value)
	if err != nil {
		// 不静默降级存明文：加密失败宁可写入失败（密钥/随机源异常是可观测的故障）。
		log.Printf("[store] UpdateSecret 加密失败: %v", err)
		return nil
	}

	item.Version = existing.Version + 1
	item.CreatedAt = existing.CreatedAt
	item.UpdatedAt = time.Now()

	cp := *item
	cp.Value = encrypted
	s.secrets[ck] = &cp
	return item
}

func (s *MemoryStore) DeleteSecret(tenantID, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ck := configKey(tenantID, key)
	if _, ok := s.secrets[ck]; !ok {
		return false
	}
	delete(s.secrets, ck)
	return true
}

func (s *MemoryStore) ListSecrets(tenantID string) []*models.SecretMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.SecretMeta, 0, len(s.secrets))
	for _, v := range s.secrets {
		if tenantID != "" && v.TenantID != tenantID {
			continue
		}
		out = append(out, &models.SecretMeta{
			ID:        v.ID,
			TenantID:  v.TenantID,
			Key:       v.Key,
			KeyType:   v.KeyType,
			Version:   v.Version,
			CreatedAt: v.CreatedAt,
			UpdatedAt: v.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	return out
}

func (s *MemoryStore) RotateSecret(tenantID, key, newValue string) *models.SecretMeta {
	s.mu.Lock()
	defer s.mu.Unlock()

	ck := configKey(tenantID, key)
	existing, ok := s.secrets[ck]
	if !ok {
		return nil
	}

	encrypted, err := s.encrypt(newValue)
	if err != nil {
		log.Printf("[store] RotateSecret 加密失败: %v", err)
		return nil
	}

	existing.Value = encrypted
	existing.Version++
	existing.UpdatedAt = time.Now()

	return &models.SecretMeta{
		ID:        existing.ID,
		TenantID:  existing.TenantID,
		Key:       existing.Key,
		KeyType:   existing.KeyType,
		Version:   existing.Version,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: existing.UpdatedAt,
	}
}

// ===================== Channel Operations =====================

func (s *MemoryStore) CreateChannel(item *models.ChannelEntry) *models.ChannelEntry {
	if item == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now
	cp := *item
	s.channels[item.ID] = &cp
	return item
}

func (s *MemoryStore) GetChannel(id string) *models.ChannelEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.channels[id]
	if !ok {
		return nil
	}
	cp := *entry
	return &cp
}

func (s *MemoryStore) UpdateChannel(item *models.ChannelEntry) bool {
	if item == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[item.ID]; !ok {
		return false
	}
	item.UpdatedAt = time.Now()
	cp := *item
	s.channels[item.ID] = &cp
	return true
}

func (s *MemoryStore) DeleteChannel(id, tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.channels[id]
	if !ok {
		return false
	}
	if tenantID != "" && entry.TenantID != tenantID {
		return false
	}
	delete(s.channels, id)
	return true
}

func (s *MemoryStore) ListChannels(tenantID string) []*models.ChannelEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.ChannelEntry, 0, len(s.channels))
	for _, v := range s.channels {
		if tenantID != "" && v.TenantID != tenantID {
			continue
		}
		cp := *v
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// ===================== Template Operations =====================

func (s *MemoryStore) CreateTemplate(item *models.TemplateEntry) *models.TemplateEntry {
	if item == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now
	cp := *item
	s.templates[item.ID] = &cp
	return item
}

func (s *MemoryStore) GetTemplate(id string) *models.TemplateEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.templates[id]
	if !ok {
		return nil
	}
	cp := *entry
	return &cp
}

func (s *MemoryStore) UpdateTemplate(item *models.TemplateEntry) bool {
	if item == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.templates[item.ID]; !ok {
		return false
	}
	item.UpdatedAt = time.Now()
	cp := *item
	s.templates[item.ID] = &cp
	return true
}

func (s *MemoryStore) DeleteTemplate(id, tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.templates[id]
	if !ok {
		return false
	}
	if tenantID != "" && entry.TenantID != tenantID {
		return false
	}
	delete(s.templates, id)
	return true
}

func (s *MemoryStore) ListTemplates(tenantID string) []*models.TemplateEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.TemplateEntry, 0, len(s.templates))
	for _, v := range s.templates {
		if tenantID != "" && v.TenantID != tenantID {
			continue
		}
		cp := *v
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
