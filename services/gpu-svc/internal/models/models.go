package models

import "time"

// GPUVendor represents the GPU hardware vendor.
type GPUVendor string

const (
	GPUVendorNVIDIA  GPUVendor = "NVIDIA"
	GPUVendorAMD     GPUVendor = "AMD"
	GVendorHuaweiAscend GPUVendor = "华为昇腾"
)

// GPUInfo describes a single GPU device.
type GPUInfo struct {
	Index            int       `json:"index"`
	Model            string    `json:"model"`
	VRAMMB           int       `json:"vram_mb"`
	ComputeCapability string   `json:"compute_capability"`
	DriverVersion    string    `json:"driver_version"`
	ECCEnabled       bool      `json:"ecc_enabled"`
	Vendor           GPUVendor `json:"vendor"`
}

// NodeStatus represents the operational status of a GPU node.
type NodeStatus string

const (
	NodeStatusOnline  NodeStatus = "online"
	NodeStatusOffline NodeStatus = "offline"
	NodeStatusDraining NodeStatus = "draining"
)

// GPUNode represents a physical or virtual machine with GPU devices.
type GPUNode struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Address       string     `json:"address"`
	GPUs          []GPUInfo  `json:"gpus"`
	Status        NodeStatus `json:"status"`
	Labels        map[string]string `json:"labels"`
	TotalVRAMMB   int        `json:"total_vram_mb"`
	UsedVRAMMB    int        `json:"used_vram_mb"`
	GPUErrors     int        `json:"gpu_errors"`
	LastHeartbeat time.Time  `json:"last_heartbeat"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// NodeHealth represents the health status of a GPU node.
type NodeHealth struct {
	NodeID               string            `json:"node_id"`
	Status               NodeStatus        `json:"status"`
	ECCErrors            int               `json:"ecc_errors"`
	ThermalThrottling    bool              `json:"thermal_throttling"`
	GPUUtilization       map[int]float64   `json:"gpu_utilization"`
	TemperatureCelsius   map[int]float64   `json:"temperature_celsius"`
	PowerDrawWatts       map[int]float64   `json:"power_draw_watts"`
	MemoryUsageMB        map[int]int       `json:"memory_usage_mb"`
	LastChecked          time.Time         `json:"last_checked"`
	Issues               []string          `json:"issues"`
}

// WorkloadStatus represents the lifecycle status of an AI workload.
type WorkloadStatus string

const (
	WorkloadStatusPending    WorkloadStatus = "pending"
	WorkloadStatusScheduling WorkloadStatus = "scheduling"
	WorkloadStatusRunning    WorkloadStatus = "running"
	WorkloadStatusCompleted  WorkloadStatus = "completed"
	WorkloadStatusFailed     WorkloadStatus = "failed"
	WorkloadStatusCancelled  WorkloadStatus = "cancelled"
)

// WorkloadType represents the type of AI workload.
type WorkloadType string

const (
	WorkloadTypeTraining    WorkloadType = "training"
	WorkloadTypeInference   WorkloadType = "inference"
	WorkloadTypeFineTuning  WorkloadType = "fine_tuning"
	WorkloadTypeDataAnalysis WorkloadType = "data_analysis"
)

// GPURequest describes GPU resource requirements for a workload.
type GPURequest struct {
	Count        int    `json:"count"`
	MinVRAMMB    int    `json:"min_vram_mb"`
	GPUModel     string `json:"gpu_model,omitempty"`
	MultiGPU     bool   `json:"multi_gpu"`
	Parallelism  string `json:"parallelism,omitempty"` // "tensor", "model", "pipeline"
}

// Workload represents an AI workload submission.
type Workload struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	TenantID    string            `json:"tenant_id"`
	Type        WorkloadType      `json:"type"`
	Status      WorkloadStatus    `json:"status"`
	GPURequest  GPURequest        `json:"gpu_request"`
	NodeIDs     []string          `json:"node_ids"`
	Priority    int               `json:"priority"`
	Image       string            `json:"image,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Replicas    int               `json:"replicas"`
	ModelName   string            `json:"model_name,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	ErrorMsg    string            `json:"error_msg,omitempty"`
}

// SchedulingPolicy represents a workload scheduling policy.
type SchedulingPolicy struct {
	Name           string            `json:"name"`
	Type           string            `json:"type"` // "bin_packing", "spreading", "affinity", "anti-affinity", "priority"
	Enabled        bool              `json:"enabled"`
	Labels         map[string]string `json:"labels,omitempty"`
	PriorityWeight int               `json:"priority_weight"`
	MaxGPUsPerNode int               `json:"max_gpus_per_node"`
}

// ScheduleResult represents the outcome of a scheduling attempt.
type ScheduleResult struct {
	WorkloadID string   `json:"workload_id"`
	Assigned   bool     `json:"assigned"`
	NodeIDs    []string `json:"node_ids,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// GPUModel represents a model available for serving.
type GPUModel struct {
	Name         string    `json:"name"`
	SizeBytes    int64     `json:"size_bytes"`
	ParameterCount string  `json:"parameter_count"`
	Quantized    bool      `json:"quantized"`
	Serving      bool      `json:"serving"`
	Port         int       `json:"port,omitempty"`
	NodeID       string    `json:"node_id,omitempty"`
	Replicas     int       `json:"replicas"`
	LastPulled   time.Time `json:"last_pulled"`
}

// GPUQuota represents a tenant's GPU resource quota.
type GPUQuota struct {
	TenantID    string    `json:"tenant_id"`
	MaxGPUs     int       `json:"max_gpus"`
	MaxVRAMMB   int       `json:"max_vram_mb"`
	MaxWorkloads int      `json:"max_workloads"`
	Priority    int       `json:"priority"`
	UsedGPUs    int       `json:"used_gpus"`
	UsedVRAMMB  int       `json:"used_vram_mb"`
	UsedWorkloads int     `json:"used_workloads"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// QuotaUsage represents the current quota usage for a tenant.
type QuotaUsage struct {
	TenantID      string    `json:"tenant_id"`
	UsedGPUs      int       `json:"used_gpus"`
	UsedVRAMMB    int       `json:"used_vram_mb"`
	UsedWorkloads int       `json:"used_workloads"`
	MaxGPUs       int       `json:"max_gpus"`
	MaxVRAMMB     int       `json:"max_vram_mb"`
	MaxWorkloads  int       `json:"max_workloads"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// GPUMetrics represents GPU metrics for a specific node.
type GPUMetrics struct {
	NodeID            string            `json:"node_id"`
	Timestamp         time.Time         `json:"timestamp"`
	GPUs              []GPUMetricsPerGPU `json:"gpus"`
	AvgUtilization    float64           `json:"avg_utilization"`
	TotalMemoryUsedMB int               `json:"total_memory_used_mb"`
	TotalMemoryTotalMB int              `json:"total_memory_total_mb"`
	AvgTemperatureC   float64           `json:"avg_temperature_c"`
	TotalPowerDrawW   float64           `json:"total_power_draw_w"`
}

// GPUMetricsPerGPU represents metrics for a single GPU.
type GPUMetricsPerGPU struct {
	Index            int     `json:"index"`
	UtilizationPct   float64 `json:"utilization_pct"`
	MemoryUsedMB     int     `json:"memory_used_mb"`
	MemoryTotalMB    int     `json:"memory_total_mb"`
	TemperatureC     float64 `json:"temperature_c"`
	PowerDrawW       float64 `json:"power_draw_w"`
	FanSpeedPct      float64 `json:"fan_speed_pct"`
	ClockSpeedMHz    int     `json:"clock_speed_mhz"`
	ECCErrorCount    int     `json:"ecc_error_count"`
	ThermalThrottle  bool    `json:"thermal_throttle"`
}

// ResourceSummary represents total GPU resources across all nodes.
type ResourceSummary struct {
	TotalGPUs        int            `json:"total_gpus"`
	AvailableGPUs    int            `json:"available_gpus"`
	AllocatedGPUs    int            `json:"allocated_gpus"`
	TotalVRAMMB      int            `json:"total_vram_mb"`
	AvailableVRAMMB  int            `json:"available_vram_mb"`
	AllocatedVRAMMB  int            `json:"allocated_vram_mb"`
	OnlineNodes      int            `json:"online_nodes"`
	OfflineNodes     int            `json:"offline_nodes"`
	NodeResources    []NodeResource `json:"node_resources"`
}

// NodeResource represents GPU resources for a single node.
type NodeResource struct {
	NodeID         string       `json:"node_id"`
	Status         NodeStatus   `json:"status"`
	TotalGPUs      int          `json:"total_gpus"`
	AvailableGPUs  int          `json:"available_gpus"`
	AllocatedGPUs  int          `json:"allocated_gpus"`
	TotalVRAMMB    int          `json:"total_vram_mb"`
	AvailableVRAMMB int         `json:"available_vram_mb"`
	AllocatedVRAMMB int         `json:"allocated_vram_mb"`
}
