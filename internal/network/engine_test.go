package network

import (
	"net"
	"testing"
)

func TestValidDeviceType(t *testing.T) {
	valid := []DeviceType{DeviceTypeSwitch, DeviceTypeRouter, DeviceTypeFirewall, DeviceTypeLoadBalancer}
	for _, vt := range valid {
		if !ValidDeviceType(vt) {
			t.Errorf("ValidDeviceType(%q) = false, want true", vt)
		}
	}
	if ValidDeviceType("invalid") {
		t.Error("ValidDeviceType(invalid) = true, want false")
	}
}

func TestAllDeviceTypes(t *testing.T) {
	all := AllDeviceTypes()
	if len(all) != 4 {
		t.Fatalf("AllDeviceTypes() = %d, want 4", len(all))
	}
}

func TestDiscover_InvalidCIDR(t *testing.T) {
	e := NewEngine()
	result := e.Discover(DiscoverRequest{Subnet: "not-a-cidr"})
	if result.Error == "" {
		t.Error("Discover(invalid CIDR) should return error")
	}
}

func TestDiscover_EmptySubnet(t *testing.T) {
	e := NewEngine()
	result := e.Discover(DiscoverRequest{Subnet: ""})
	if result.Error == "" {
		t.Error("Discover(empty subnet) should return error")
	}
}

func TestDiscover_InvalidMask(t *testing.T) {
	e := NewEngine()
	result := e.Discover(DiscoverRequest{Subnet: "192.168.1.1/33"})
	if result.Error == "" {
		t.Error("Discover(invalid mask) should return error")
	}
}

func TestDiscover_ValidCIDR(t *testing.T) {
	e := NewEngine()
	result := e.Discover(DiscoverRequest{Subnet: "127.0.0.0/30"})
	if result.Error != "" {
		t.Fatalf("Discover(/30) error: %v", result.Error)
	}
	if result.Scanned == 0 {
		t.Error("Scanned should be > 0")
	}
}

func TestIncIP(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	incIP(ip)
	if ip.String() != "192.168.1.2" {
		t.Fatalf("incIP(192.168.1.1) = %s, want 192.168.1.2", ip.String())
	}
}

func TestIncIP_Rollover(t *testing.T) {
	ip := net.ParseIP("192.168.1.255")
	incIP(ip)
	if ip.String() != "192.168.2.0" {
		t.Fatalf("incIP(192.168.1.255) = %s, want 192.168.2.0", ip.String())
	}
}

func TestBuildTopology(t *testing.T) {
	e := NewEngine()
	devices := []Device{
		{ID: "sw1", Name: "Switch-1", Type: DeviceTypeSwitch, IP: "10.0.0.1", Status: DeviceStatusUp},
		{ID: "rt1", Name: "Router-1", Type: DeviceTypeRouter, IP: "10.0.0.2", Status: DeviceStatusUp},
	}
	topo := e.BuildTopology("default", devices)
	if len(topo.Nodes) != 2 {
		t.Fatalf("Nodes = %d, want 2", len(topo.Nodes))
	}
	if topo.TenantID != "default" {
		t.Errorf("TenantID = %q, want default", topo.TenantID)
	}
	if len(topo.Links) != 0 {
		t.Errorf("Links = %d, want 0 (MVP)", len(topo.Links))
	}
}
