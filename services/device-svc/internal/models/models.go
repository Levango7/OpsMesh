package models

import "time"

// Device represents a managed device in the domain model.
type Device struct {
	ID            string            `json:"id"`
	TenantID      string            `json:"tenantID"`
	Name          string            `json:"name"`
	IP            string            `json:"ip"`
	MAC           string            `json:"mac"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	Status        string            `json:"status"`
	AgentID       string            `json:"agentID"`
	Tags          []string          `json:"tags"`
	Labels        map[string]string `json:"labels"`
	Group         string            `json:"group"`
	LastHeartbeat time.Time         `json:"lastHeartbeat"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// Agent represents a registered agent in the domain model.
type Agent struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenantID"`
	DeviceID      string    `json:"deviceID"`
	Hostname      string    `json:"hostname"`
	Version       string    `json:"version"`
	Status        string    `json:"status"`
	Load          int       `json:"load"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	Addr          string    `json:"addr"`
	GRPCPort      int       `json:"grpcPort"`
	MetricsPort   int       `json:"metricsPort"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// CI represents a Configuration Item in the CMDB domain model.
type CI struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenantID"`
	CiType     string            `json:"ciType"`
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Attributes map[string]string `json:"attributes"`
	Source     string            `json:"source"`
	AgentID    string            `json:"agentID"`
	DeviceID   string            `json:"deviceID"`
	Version    int               `json:"version"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// CIRelation represents a relationship between two CIs.
type CIRelation struct {
	ID           int64             `json:"id"`
	SourceCIID   string            `json:"sourceCIID"`
	TargetCIID   string            `json:"targetCIID"`
	RelationType string            `json:"relationType"`
	TenantID     string            `json:"tenantID"`
	Attributes   map[string]string `json:"attributes"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// DiscoveryJob represents a network discovery job.
type DiscoveryJob struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantID"`
	CIDR         string    `json:"cidr"`
	Status       string    `json:"status"`
	TotalHosts   int       `json:"totalHosts"`
	ScannedHosts int       `json:"scannedHosts"`
	FoundDevices int       `json:"foundDevices"`
	Error        string    `json:"error"`
	StartedAt    time.Time `json:"startedAt"`
	CompletedAt  time.Time `json:"completedAt"`
}

// DeviceStatus represents the current status of a device.
type DeviceStatus struct {
	DeviceID      string    `json:"deviceID"`
	Status        string    `json:"status"`
	Reachable     bool      `json:"reachable"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
}
