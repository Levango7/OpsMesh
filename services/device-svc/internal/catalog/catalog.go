package catalog

import (
	"fmt"
	"sync"
)

// CatalogNode represents a single node in the service topology.
type CatalogNode struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Status   string            `json:"status"`
	Metadata map[string]string `json:"metadata"`
	Children []string          `json:"children"`
}

// CatalogEdge represents a directed relationship between two nodes.
type CatalogEdge struct {
	From         string `json:"from"`
	To           string `json:"to"`
	RelationType string `json:"relationType"`
}

// CatalogGraph is the full topology graph for a tenant.
type CatalogGraph struct {
	Nodes    []*CatalogNode `json:"nodes"`
	Edges    []*CatalogEdge `json:"edges"`
	TenantID string         `json:"tenantID"`
}

// ImpactAnalysis holds upstream and downstream impact for a node.
type ImpactAnalysis struct {
	NodeID     string   `json:"nodeID"`
	Upstream   []string `json:"upstream"`
	Downstream []string `json:"downstream"`
}

// Catalog manages topology graphs.
type Catalog struct {
	mu      sync.RWMutex
	nodes   map[string]*CatalogNode
	edges   []*CatalogEdge
	tenants map[string]*CatalogGraph
}

// NewCatalog creates a new Catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		nodes:   make(map[string]*CatalogNode),
		tenants: make(map[string]*CatalogGraph),
	}
}

// AddNode adds a node to the catalog.
func (c *Catalog) AddNode(n *CatalogNode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes[n.ID] = n
}

// AddEdge adds an edge to the catalog.
func (c *Catalog) AddEdge(e *CatalogEdge) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.edges = append(c.edges, e)
}

// BuildTopology constructs the full topology graph for a tenant.
func (c *Catalog) BuildTopology(tenantID string) *CatalogGraph {
	c.mu.RLock()
	defer c.mu.RUnlock()

	graph := &CatalogGraph{
		Nodes:    make([]*CatalogNode, 0),
		Edges:    make([]*CatalogEdge, 0),
		TenantID: tenantID,
	}

	for _, n := range c.nodes {
		if v, ok := n.Metadata["tenantID"]; ok && v != tenantID {
			continue
		}
		graph.Nodes = append(graph.Nodes, n)
	}

	for _, e := range c.edges {
		graph.Edges = append(graph.Edges, e)
	}

	c.tenants[tenantID] = graph
	return graph
}

// GetNode returns a node by ID.
func (c *Catalog) GetNode(id string) (*CatalogNode, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	n, ok := c.nodes[id]
	if !ok {
		return nil, fmt.Errorf("node not found: %s", id)
	}
	return n, nil
}

// GetRelations returns all edges connected to a node.
func (c *Catalog) GetRelations(id string) ([]*CatalogEdge, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.nodes[id]; !ok {
		return nil, fmt.Errorf("node not found: %s", id)
	}
	rels := make([]*CatalogEdge, 0)
	for _, e := range c.edges {
		if e.From == id || e.To == id {
			rels = append(rels, e)
		}
	}
	return rels, nil
}

// GetImpactAnalysis returns upstream and downstream node IDs for a given node.
func (c *Catalog) GetImpactAnalysis(id string) (*ImpactAnalysis, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if _, ok := c.nodes[id]; !ok {
		return nil, fmt.Errorf("node not found: %s", id)
	}
	upstream := make([]string, 0)
	downstream := make([]string, 0)
	for _, e := range c.edges {
		if e.To == id {
			upstream = append(upstream, e.From)
		}
		if e.From == id {
			downstream = append(downstream, e.To)
		}
	}
	return &ImpactAnalysis{
		NodeID:     id,
		Upstream:   upstream,
		Downstream: downstream,
	}, nil
}
