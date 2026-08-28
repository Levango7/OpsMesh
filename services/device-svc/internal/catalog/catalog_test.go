package catalog

import (
	"testing"
)

func TestNewCatalog(t *testing.T) {
	c := NewCatalog()
	if c == nil {
		t.Fatal("expected non-nil catalog")
	}
	if len(c.nodes) != 0 {
		t.Errorf("expected empty nodes, got %d", len(c.nodes))
	}
}

func TestAddNode(t *testing.T) {
	c := NewCatalog()
	n := &CatalogNode{ID: "n1", Name: "server-01", Type: "host", Status: "online"}
	c.AddNode(n)

	got, err := c.GetNode("n1")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if got.Name != "server-01" {
		t.Errorf("expected name server-01, got %s", got.Name)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	c := NewCatalog()
	_, err := c.GetNode("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestAddEdge(t *testing.T) {
	c := NewCatalog()
	c.AddNode(&CatalogNode{ID: "n1", Name: "host", Type: "host", Status: "online"})
	c.AddNode(&CatalogNode{ID: "n2", Name: "service", Type: "service", Status: "running"})
	c.AddEdge(&CatalogEdge{From: "n1", To: "n2", RelationType: "runs_on"})

	rels, err := c.GetRelations("n1")
	if err != nil {
		t.Fatalf("GetRelations failed: %v", err)
	}
	if len(rels) != 1 {
		t.Errorf("expected 1 relation, got %d", len(rels))
	}
	if rels[0].RelationType != "runs_on" {
		t.Errorf("expected relation type runs_on, got %s", rels[0].RelationType)
	}
}

func TestGetRelationsNotFound(t *testing.T) {
	c := NewCatalog()
	_, err := c.GetRelations("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestBuildTopology(t *testing.T) {
	c := NewCatalog()
	c.AddNode(&CatalogNode{ID: "n1", Name: "host", Type: "host", Status: "online", Metadata: map[string]string{"tenantID": "t1"}})
	c.AddNode(&CatalogNode{ID: "n2", Name: "service", Type: "service", Status: "running", Metadata: map[string]string{"tenantID": "t1"}})
	c.AddEdge(&CatalogEdge{From: "n1", To: "n2", RelationType: "runs_on"})

	graph := c.BuildTopology("t1")
	if len(graph.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(graph.Edges))
	}
	if graph.TenantID != "t1" {
		t.Errorf("expected tenantID t1, got %s", graph.TenantID)
	}
}

func TestBuildTopologyFiltersByTenant(t *testing.T) {
	c := NewCatalog()
	c.AddNode(&CatalogNode{ID: "n1", Name: "host-t1", Type: "host", Status: "online", Metadata: map[string]string{"tenantID": "t1"}})
	c.AddNode(&CatalogNode{ID: "n2", Name: "host-t2", Type: "host", Status: "online", Metadata: map[string]string{"tenantID": "t2"}})

	graph := c.BuildTopology("t1")
	if len(graph.Nodes) != 1 {
		t.Errorf("expected 1 node for tenant t1, got %d", len(graph.Nodes))
	}
	if graph.Nodes[0].ID != "n1" {
		t.Errorf("expected node n1, got %s", graph.Nodes[0].ID)
	}
}

func TestGetImpactAnalysis(t *testing.T) {
	c := NewCatalog()
	c.AddNode(&CatalogNode{ID: "n1", Name: "db", Type: "database", Status: "online"})
	c.AddNode(&CatalogNode{ID: "n2", Name: "api", Type: "service", Status: "running"})
	c.AddNode(&CatalogNode{ID: "n3", Name: "web", Type: "frontend", Status: "running"})
	c.AddEdge(&CatalogEdge{From: "n1", To: "n2", RelationType: "depends_on"})
	c.AddEdge(&CatalogEdge{From: "n2", To: "n3", RelationType: "depends_on"})

	impact, err := c.GetImpactAnalysis("n2")
	if err != nil {
		t.Fatalf("GetImpactAnalysis failed: %v", err)
	}
	if len(impact.Upstream) != 1 || impact.Upstream[0] != "n1" {
		t.Errorf("expected upstream [n1], got %v", impact.Upstream)
	}
	if len(impact.Downstream) != 1 || impact.Downstream[0] != "n3" {
		t.Errorf("expected downstream [n3], got %v", impact.Downstream)
	}
}

func TestGetImpactAnalysisNotFound(t *testing.T) {
	c := NewCatalog()
	_, err := c.GetImpactAnalysis("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent node")
	}
}

func TestGetImpactAnalysisNoRelations(t *testing.T) {
	c := NewCatalog()
	c.AddNode(&CatalogNode{ID: "n1", Name: "isolated", Type: "host", Status: "online"})

	impact, err := c.GetImpactAnalysis("n1")
	if err != nil {
		t.Fatalf("GetImpactAnalysis failed: %v", err)
	}
	if len(impact.Upstream) != 0 {
		t.Errorf("expected no upstream, got %v", impact.Upstream)
	}
	if len(impact.Downstream) != 0 {
		t.Errorf("expected no downstream, got %v", impact.Downstream)
	}
}
