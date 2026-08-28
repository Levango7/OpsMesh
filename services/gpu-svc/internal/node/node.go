package node

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/gpu"
	"github.com/Levango7/OpsMesh/services/gpu-svc/internal/models"
)

// ErrNodeNotFound is returned when a node does not exist.
var ErrNodeNotFound = errors.New("gpu node not found")

// ErrNodeAlreadyExists is returned when registering a duplicate node.
var ErrNodeAlreadyExists = errors.New("gpu node already exists")

// ErrNodeInvalid is returned when node data is invalid.
var ErrNodeInvalid = errors.New("gpu node invalid")

// Store is the interface for node persistence.
type Store interface {
	CreateNode(*models.GPUNode) error
	GetNode(id string) *models.GPUNode
	ListNodes() []*models.GPUNode
	UpdateNode(*models.GPUNode) error
	DeleteNode(id string) error
}

// Manager handles GPU node registration and lifecycle.
type Manager struct {
	mu    sync.RWMutex
	nodes map[string]*models.GPUNode
	now   func() time.Time
}

// NewManager creates a new Manager.
func NewManager(now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{
		nodes: make(map[string]*models.GPUNode),
		now:   now,
	}
}

// RealGPUs detects real GPUs on the machine using nvidia-smi.
// Falls back to simulated data if nvidia-smi is not available.
func RealGPUs() []models.GPUInfo {
	gpus, detector, err := gpu.AutoDetect()
	if err != nil {
		return simulatedGPUs()
	}

	result := make([]models.GPUInfo, 0, len(gpus))
	for i, g := range gpus {
		result = append(result, models.GPUInfo{
			Index:             i,
			Model:             g.Name,
			VRAMMB:            g.MemoryTotalMB,
			ComputeCapability: g.ComputeCapability,
			DriverVersion:     g.DriverVersion,
			Vendor:            models.GPUVendorNVIDIA,
		})
	}

	_ = detector // used for detection, result already obtained
	return result
}

// simulatedGPUs returns simulated GPU data for non-GPU machines.
func simulatedGPUs() []models.GPUInfo {
	return []models.GPUInfo{
		{
			Index:             0,
			Model:             "NVIDIA A100-SXM4-80GB (simulated)",
			VRAMMB:            81920,
			ComputeCapability: "8.0",
			DriverVersion:     "535.104.05",
			Vendor:            models.GPUVendorNVIDIA,
		},
		{
			Index:             1,
			Model:             "NVIDIA A100-SXM4-80GB (simulated)",
			VRAMMB:            81920,
			ComputeCapability: "8.0",
			DriverVersion:     "535.104.05",
			Vendor:            models.GPUVendorNVIDIA,
		},
	}
}

// Register adds a new GPU node.
func (m *Manager) Register(node *models.GPUNode) error {
	if node == nil || node.Name == "" {
		return ErrNodeInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.nodes[node.ID]; exists {
		return ErrNodeAlreadyExists
	}

	now := m.now()
	if node.ID == "" {
		node.ID = uuid.New().String()
	}
	node.Status = models.NodeStatusOnline
	node.CreatedAt = now
	node.UpdatedAt = now
	node.LastHeartbeat = now
	node.TotalVRAMMB = computeTotalVRAM(node.GPUs)

	cp := *node
	m.nodes[node.ID] = &cp
	return nil
}

// Get retrieves a node by ID.
func (m *Manager) Get(id string) (*models.GPUNode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node, exists := m.nodes[id]
	if !exists {
		return nil, ErrNodeNotFound
	}
	cp := *node
	return &cp, nil
}

// List returns all registered nodes.
func (m *Manager) List() []*models.GPUNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.GPUNode, 0, len(m.nodes))
	for _, n := range m.nodes {
		cp := *n
		out = append(out, &cp)
	}
	return out
}

// Update updates an existing node.
func (m *Manager) Update(node *models.GPUNode) error {
	if node == nil || node.ID == "" {
		return ErrNodeInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	old, exists := m.nodes[node.ID]
	if !exists {
		return ErrNodeNotFound
	}

	node.CreatedAt = old.CreatedAt
	node.UpdatedAt = m.now()
	node.TotalVRAMMB = computeTotalVRAM(node.GPUs)

	cp := *node
	m.nodes[node.ID] = &cp
	return nil
}

// Unregister removes a node.
func (m *Manager) Unregister(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.nodes[id]; !exists {
		return ErrNodeNotFound
	}
	delete(m.nodes, id)
	return nil
}

// UpdateHeartbeat updates the node's heartbeat timestamp.
func (m *Manager) UpdateHeartbeat(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node, exists := m.nodes[id]
	if !exists {
		return ErrNodeNotFound
	}
	node.LastHeartbeat = m.now()
	return nil
}

// GetHealth returns the health status of a node.
func (m *Manager) GetHealth(id string) (*models.NodeHealth, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	node, exists := m.nodes[id]
	if !exists {
		return nil, ErrNodeNotFound
	}

	health := &models.NodeHealth{
		NodeID:  id,
		Status:  node.Status,
		ECCErrors: node.GPUErrors,
		Issues:  make([]string, 0),
	}

	if node.Status == models.NodeStatusOffline {
		health.Issues = append(health.Issues, "node is offline")
	}

	sinceHeartbeat := m.now().Sub(node.LastHeartbeat)
	if sinceHeartbeat > 2*time.Minute {
		health.Issues = append(health.Issues, "heartbeat timeout")
	}

	if node.GPUErrors > 0 {
		health.Issues = append(health.Issues, "GPU errors detected")
	}

	return health, nil
}

// SetNodeStatus updates the status of a node.
func (m *Manager) SetNodeStatus(id string, status models.NodeStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node, exists := m.nodes[id]
	if !exists {
		return ErrNodeNotFound
	}
	node.Status = status
	node.UpdatedAt = m.now()
	return nil
}

// GetOnlineNodes returns all online nodes.
func (m *Manager) GetOnlineNodes() []*models.GPUNode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*models.GPUNode, 0)
	for _, n := range m.nodes {
		if n.Status == models.NodeStatusOnline {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out
}

func computeTotalVRAM(gpus []models.GPUInfo) int {
	total := 0
	for _, g := range gpus {
		total += g.VRAMMB
	}
	return total
}
