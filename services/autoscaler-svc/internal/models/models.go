package models

import "time"

// ScaleRule defines a scaling rule with thresholds and limits.
type ScaleRule struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Deployment         string        `json:"deployment"`
	Namespace          string        `json:"namespace"`
	Metric             string        `json:"metric"`
	ScaleUpThreshold   float64       `json:"scaleUpThreshold"`
	ScaleDownThreshold float64       `json:"scaleDownThreshold"`
	MinReplicas        int32         `json:"minReplicas"`
	MaxReplicas        int32         `json:"maxReplicas"`
	CooldownUp         time.Duration `json:"cooldownUp"`
	CooldownDown       time.Duration `json:"cooldownDown"`
	Enabled            bool          `json:"enabled"`
	CreatedAt          time.Time     `json:"createdAt"`
	UpdatedAt          time.Time     `json:"updatedAt"`
}

// ScaleDecision records a scaling action that was taken.
type ScaleDecision struct {
	ID           string  `json:"id"`
	RuleID       string  `json:"ruleId"`
	Deployment   string  `json:"deployment"`
	Namespace    string  `json:"namespace"`
	Action       string  `json:"action"` // "scale_up", "scale_down", "no_action"
	FromReplicas int32   `json:"fromReplicas"`
	ToReplicas   int32   `json:"toReplicas"`
	Reason       string  `json:"reason"`
	MetricValue  float64 `json:"metricValue"`
	// Executor 记录这条决策由哪个执行器执行（memory-simulated / k8s-kubeconfig /
	// k8s-in-cluster）。"memory-simulated" 意味着副本数只改了进程内的账，集群没动。
	Executor  string    `json:"executor,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// EvaluateRequest triggers evaluation of all rules or a specific rule.
type EvaluateRequest struct {
	RuleID string `json:"ruleId,omitempty"`
}

// EvaluateResponse contains the decisions made during evaluation.
type EvaluateResponse struct {
	Decisions []ScaleDecision `json:"decisions"`
	Timestamp time.Time       `json:"timestamp"`
}

// MetricsData holds current metric values for a deployment.
type MetricsData struct {
	Deployment string             `json:"deployment"`
	Namespace  string             `json:"namespace"`
	Values     map[string]float64 `json:"values"`
	Timestamp  time.Time          `json:"timestamp"`
}
