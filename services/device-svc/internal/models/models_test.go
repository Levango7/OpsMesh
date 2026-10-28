package models

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func marshalToObject(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal %T: %v (json=%s)", v, err, raw)
	}
	return out
}

func assertKeySet(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("键集合 = %v, want %v", got, want)
	}
}

func TestDeviceJSONContract(t *testing.T) {
	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	device := Device{
		ID:            "dev-1",
		TenantID:      "t1",
		Name:          "web-01",
		IP:            "10.0.0.1",
		MAC:           "aa:bb:cc:dd:ee:ff",
		OS:            "ubuntu",
		Arch:          "amd64",
		Status:        "online",
		AgentID:       "agent-1",
		Tags:          []string{"prod"},
		Labels:        map[string]string{"env": "prod"},
		Group:         "web",
		LastHeartbeat: now,
		CreatedAt:     now,
		UpdatedAt:     now,
		Retired:       false,
	}

	obj := marshalToObject(t, device)
	// 设备域的 JSON 历史上是 camelCase，保持即可，勿与其它服务的 snake_case 混用。
	assertKeySet(t, obj, "id", "tenantID", "name", "ip", "mac", "os", "arch", "status",
		"agentID", "tags", "labels", "group", "lastHeartbeat", "createdAt", "updatedAt", "retired")

	if obj["tenantID"] != "t1" || obj["agentID"] != "agent-1" || obj["retired"] != false {
		t.Fatalf("字段值失真: %v", obj)
	}
	// 时间字段是 RFC3339 字符串（Go time.Time 默认序列化），往返必须等价。
	raw, err := json.Marshal(device)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Device
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw2, err := json.Marshal(back)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if string(raw) != string(raw2) {
		t.Fatalf("往返不等价:\n%s\n%s", raw, raw2)
	}
}

func TestAgentAndCIRelationJSONContract(t *testing.T) {
	agent := Agent{ID: "a1", TenantID: "t1", DeviceID: "d1", Hostname: "web-01",
		Version: "1.2.3", Status: "online", Load: 7, OS: "linux", Arch: "arm64",
		Addr: "10.0.0.1:9090", GRPCPort: 9090, MetricsPort: 9100}
	assertKeySet(t, marshalToObject(t, agent), "id", "tenantID", "deviceID", "hostname",
		"version", "status", "load", "os", "arch", "addr", "grpcPort", "metricsPort",
		"lastHeartbeat", "createdAt", "updatedAt")

	ci := CI{ID: "ci-1", TenantID: "t1", CiType: "server", Name: "web", Status: "active",
		Attributes: map[string]string{"cpu": "8"}, Source: "agent", AgentID: "a1",
		DeviceID: "d1", Version: 3}
	assertKeySet(t, marshalToObject(t, ci), "id", "tenantID", "ciType", "name", "status",
		"attributes", "source", "agentID", "deviceID", "version", "createdAt", "updatedAt")

	relation := CIRelation{ID: 42, SourceCIID: "ci-1", TargetCIID: "ci-2",
		RelationType: "runs_on", TenantID: "t1", Attributes: map[string]string{"weight": "1"}}
	obj := marshalToObject(t, relation)
	assertKeySet(t, obj, "id", "sourceCIID", "targetCIID", "relationType", "tenantID",
		"attributes", "createdAt")
	if obj["id"] != float64(42) {
		t.Fatalf("id = %v, want 42", obj["id"])
	}
}

func TestDiscoveryJobAndDeviceStatusJSONContract(t *testing.T) {
	job := DiscoveryJob{ID: "job-1", TenantID: "t1", CIDR: "10.0.0.0/24", Status: "running",
		TotalHosts: 254, ScannedHosts: 10, FoundDevices: 3}
	assertKeySet(t, marshalToObject(t, job), "id", "tenantID", "cidr", "status",
		"totalHosts", "scannedHosts", "foundDevices", "error", "startedAt", "completedAt")

	status := DeviceStatus{DeviceID: "dev-1", Status: "online", Reachable: true,
		UptimeSeconds: 3600}
	obj := marshalToObject(t, status)
	assertKeySet(t, obj, "deviceID", "status", "reachable", "uptimeSeconds", "lastHeartbeat")
	if obj["reachable"] != true || obj["uptimeSeconds"] != float64(3600) {
		t.Fatalf("字段值失真: %v", obj)
	}
}
