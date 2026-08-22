package agent

import "testing"

func TestGRPCTarget(t *testing.T) {
	cases := []struct {
		in   string
		port int
		want string
	}{
		{"http://127.0.0.1:8080", 9090, "127.0.0.1:9090"},
		{"cp1.example:9090", 9090, "cp1.example:9090"}, // 多控制面 host:port 形式
		{"cp2:9091", 9090, "cp2:9091"},                 // 显式端口优先
		{"http://[::1]:8080", 9090, "[::1]:9090"},
		{"hostonly", 9090, "hostonly:9090"},
		{"x", 0, "x:9090"}, // 端口 <=0 用默认 9090
	}
	for _, c := range cases {
		got, err := grpcTarget(c.in, c.port)
		if err != nil {
			t.Fatalf("grpcTarget(%q) error: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("grpcTarget(%q, %d) = %q, want %q", c.in, c.port, got, c.want)
		}
	}
}

func TestNewGRPCClient_EmptyAddrs(t *testing.T) {
	if _, err := NewGRPCClient(nil, "", "", "", 9090); err == nil {
		t.Fatal("空地址应返回错误")
	}
}
