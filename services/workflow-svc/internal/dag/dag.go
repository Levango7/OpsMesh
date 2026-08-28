package dag

import (
	"fmt"

	"github.com/Levango7/OpsMesh/services/workflow-svc/internal/models"
)

// Graph represents a DAG of workflow nodes.
type Graph struct {
	Nodes map[string]*models.Node
	Edges map[string][]string // adjacency list: from -> []to
}

// NewGraph constructs a Graph from workflow nodes and edges.
func NewGraph(nodes []models.Node, edges []models.Edge) *Graph {
	g := &Graph{
		Nodes: make(map[string]*models.Node),
		Edges: make(map[string][]string),
	}
	for i := range nodes {
		g.Nodes[nodes[i].ID] = &nodes[i]
		g.Edges[nodes[i].ID] = []string{}
	}
	for _, e := range edges {
		g.Edges[e.From] = append(g.Edges[e.From], e.To)
	}
	return g
}

// HasCycle detects whether the DAG contains a cycle using DFS.
func (g *Graph) HasCycle() bool {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	for id := range g.Nodes {
		color[id] = white
	}

	var dfs func(node string) bool
	dfs = func(node string) bool {
		color[node] = gray
		for _, neighbor := range g.Edges[node] {
			if color[neighbor] == gray {
				return true
			}
			if color[neighbor] == white && dfs(neighbor) {
				return true
			}
		}
		color[node] = black
		return false
	}

	for id := range g.Nodes {
		if color[id] == white {
			if dfs(id) {
				return true
			}
		}
	}
	return false
}

// TopologicalSort returns nodes in topological order.
// Returns an error if the graph has a cycle.
func (g *Graph) TopologicalSort() ([]string, error) {
	if g.HasCycle() {
		return nil, fmt.Errorf("graph contains a cycle")
	}

	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = 0
	}
	for _, neighbors := range g.Edges {
		for _, n := range neighbors {
			inDegree[n]++
		}
	}

	queue := make([]string, 0)
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	result := make([]string, 0, len(g.Nodes))
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, neighbor := range g.Edges[node] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if len(result) != len(g.Nodes) {
		return nil, fmt.Errorf("graph contains a cycle")
	}
	return result, nil
}

// GetRoots returns nodes with no incoming edges.
func (g *Graph) GetRoots() []string {
	hasIncoming := make(map[string]bool)
	for _, neighbors := range g.Edges {
		for _, n := range neighbors {
			hasIncoming[n] = true
		}
	}
	roots := make([]string, 0)
	for id := range g.Nodes {
		if !hasIncoming[id] {
			roots = append(roots, id)
		}
	}
	return roots
}

// GetLeaves returns nodes with no outgoing edges.
func (g *Graph) GetLeaves() []string {
	leaves := make([]string, 0)
	for id := range g.Nodes {
		if len(g.Edges[id]) == 0 {
			leaves = append(leaves, id)
		}
	}
	return leaves
}

// GetParents returns the parent nodes (incoming edges) of a given node.
func (g *Graph) GetParents(nodeID string) []string {
	parents := make([]string, 0)
	for from, neighbors := range g.Edges {
		for _, to := range neighbors {
			if to == nodeID {
				parents = append(parents, from)
			}
		}
	}
	return parents
}

// GetChildren returns the child nodes (outgoing edges) of a given node.
func (g *Graph) GetChildren(nodeID string) []string {
	return g.Edges[nodeID]
}

// Validate checks the graph for structural correctness.
func (g *Graph) Validate() error {
	if len(g.Nodes) == 0 {
		return fmt.Errorf("graph has no nodes")
	}
	if g.HasCycle() {
		return fmt.Errorf("graph contains a cycle")
	}
	return nil
}
