package logx

import (
	"context"
	"testing"
)

func TestTraceRoundTrip(t *testing.T) {
	ctx := WithTrace(context.Background(), "trace-123")
	if got := Trace(ctx); got != "trace-123" {
		t.Fatalf("Trace = %q, want trace-123", got)
	}
	if got := Trace(context.Background()); got != "" {
		t.Fatalf("Trace(empty) = %q, want empty", got)
	}
}
