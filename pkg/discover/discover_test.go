package discover

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSweep_FindsListeningHost(t *testing.T) {
	// 在 127.0.0.1 上开一个临时监听，扫描 127.0.0.1/32 的该端口，期望被发现。
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()
	port := lis.Addr().(*net.TCPAddr).Port

	ips, err := Sweep(context.Background(), "127.0.0.1/32", []int{port}, 8, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	found := false
	for _, ip := range ips {
		if ip == "127.0.0.1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Sweep 未发现 127.0.0.1，得到 %v", ips)
	}
}

func TestSweep_BadCIDR(t *testing.T) {
	if _, err := Sweep(context.Background(), "not-a-cidr", nil, 4, time.Second); err == nil {
		t.Fatal("expected error for bad CIDR")
	}
}
