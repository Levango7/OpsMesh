package authctx

import (
	"net/http"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestFromHTTPHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Tenant-ID", "t1")
	h.Set("X-User-Id", "u1")
	h.Set("X-User-Roles", "admin, ops ,viewer")

	c := FromHTTPHeader(h)
	if c.TenantID != "t1" {
		t.Fatalf("TenantID = %q, want t1", c.TenantID)
	}
	if c.UserID != "u1" {
		t.Fatalf("UserID = %q, want u1", c.UserID)
	}
	want := []string{"admin", "ops", "viewer"}
	if len(c.Roles) != len(want) {
		t.Fatalf("Roles = %v, want %v", c.Roles, want)
	}
	for i := range want {
		if c.Roles[i] != want[i] {
			t.Fatalf("Roles[%d] = %q, want %q", i, c.Roles[i], want[i])
		}
	}
}

func TestFromHTTPHeader_EmptyRoles(t *testing.T) {
	c := FromHTTPHeader(http.Header{})
	if c.Roles != nil {
		t.Fatalf("Roles = %v, want nil", c.Roles)
	}
	if c.TenantID != "" || c.UserID != "" {
		t.Fatalf("expected empty context, got %+v", c)
	}
}

func TestFromGRPCMetadata(t *testing.T) {
	md := metadata.Pairs("x-tenant-id", "t2", "x-user-id", "u2", "x-user-roles", "admin,ops")
	c := FromGRPCMetadata(md)
	if c.TenantID != "t2" || c.UserID != "u2" {
		t.Fatalf("got %+v", c)
	}
	if len(c.Roles) != 2 || c.Roles[0] != "admin" || c.Roles[1] != "ops" {
		t.Fatalf("Roles = %v", c.Roles)
	}
}

func TestBelongsTo(t *testing.T) {
	// 开发模式：空租户放行全部（MVP 单租户合理降级，不是越权）
	dev := Context{}
	if !dev.BelongsTo("any") {
		t.Fatal("empty tenantID should allow all")
	}
	scoped := Context{TenantID: "t1"}
	if !scoped.BelongsTo("t1") {
		t.Fatal("same tenant should belong")
	}
	if scoped.BelongsTo("t2") {
		t.Fatal("different tenant must not belong")
	}
}

func TestHasRole(t *testing.T) {
	c := Context{Roles: []string{"admin", "ops"}}
	if !c.HasRole("admin") {
		t.Fatal("should have admin")
	}
	if c.HasRole("root") {
		t.Fatal("should not have root")
	}
}
