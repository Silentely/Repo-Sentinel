package notify

import (
	"context"
	"strings"
	"testing"
)

func TestTraceIDContext(t *testing.T) {
	// 1. 生成并读取
	ctx, tid := EnsureDeliveryTraceID(context.Background())
	if !strings.HasPrefix(tid, "trace-") {
		t.Fatalf("expected trace- prefix, got: %s", tid)
	}
	if got := TraceIDFromContext(ctx); got != tid {
		t.Fatalf("TraceIDFromContext mismatch: %s != %s", got, tid)
	}

	// 2. 复用已有
	_, reused := EnsureDeliveryTraceID(ctx)
	if reused != tid {
		t.Fatalf("已有 TraceID 应被复用，got: %s, expected: %s", reused, tid)
	}
}
