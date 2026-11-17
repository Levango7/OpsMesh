package grpc

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type capturedLog struct {
	msg  string
	args []any
}

func TestAuditInterceptorOKUsesDebug(t *testing.T) {
	var debugs, warns []capturedLog
	spy := func(dst *[]capturedLog) logFunc {
		return func(ctx context.Context, msg string, args ...any) {
			*dst = append(*dst, capturedLog{msg: msg, args: args})
		}
	}
	interceptor := auditInterceptor(spy(&debugs), spy(&warns), false)
	info := &grpc.UnaryServerInfo{FullMethod: "/opsmesh.v1.Registration/Heartbeat"}
	_, err := interceptor(context.Background(), nil, info,
		func(ctx context.Context, req interface{}) (interface{}, error) { return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(debugs) != 1 || len(warns) != 0 {
		t.Fatalf("want 1 debug / 0 warn, got %d / %d", len(debugs), len(warns))
	}
	assertHasField(t, debugs[0].args, "grpc_method", "/opsmesh.v1.Registration/Heartbeat")
	assertHasField(t, debugs[0].args, "grpc_code", codes.OK.String())
	assertHasField(t, debugs[0].args, "duration_ms")
}

func TestAuditInterceptorErrorUsesWarnWithCode(t *testing.T) {
	var debugs, warns []capturedLog
	spy := func(dst *[]capturedLog) logFunc {
		return func(ctx context.Context, msg string, args ...any) {
			*dst = append(*dst, capturedLog{msg: msg, args: args})
		}
	}
	interceptor := auditInterceptor(spy(&debugs), spy(&warns), false)
	info := &grpc.UnaryServerInfo{FullMethod: "/opsmesh.v1.Registration/Register"}
	boom := status.Error(codes.Unauthenticated, "bad signature")
	_, err := interceptor(context.Background(), nil, info,
		func(ctx context.Context, req interface{}) (interface{}, error) { return nil, boom })
	if err == nil {
		t.Fatal("expected error propagated")
	}
	if len(warns) != 1 || len(debugs) != 0 {
		t.Fatalf("want 1 warn / 0 debug, got %d / %d", len(warns), len(debugs))
	}
	assertHasField(t, warns[0].args, "grpc_code", codes.Unauthenticated.String())
	assertHasField(t, warns[0].args, "grpc_error")
}

func TestAuditInterceptorPlainErrorMapsUnknown(t *testing.T) {
	var warns []capturedLog
	interceptor := auditInterceptor(
		func(context.Context, string, ...any) {},
		func(ctx context.Context, msg string, args ...any) { warns = append(warns, capturedLog{msg, args}) },
		false,
	)
	info := &grpc.UnaryServerInfo{FullMethod: "/x"}
	_, _ = interceptor(context.Background(), nil, info,
		func(ctx context.Context, req interface{}) (interface{}, error) { return nil, errors.New("plain") })
	if len(warns) != 1 {
		t.Fatalf("want 1 warn, got %d", len(warns))
	}
	assertHasField(t, warns[0].args, "grpc_code", codes.Unknown.String())
}

func assertHasField(t *testing.T, args []any, key string, want ...string) {
	t.Helper()
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == key {
			if len(want) > 0 && args[i+1] != want[0] {
				t.Errorf("field %s = %v, want %v", key, args[i+1], want[0])
			}
			return
		}
	}
	t.Errorf("missing field %s in %v", key, args)
}
