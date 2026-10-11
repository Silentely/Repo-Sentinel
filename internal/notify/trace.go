package notify

import (
	"context"

	"github.com/oklog/ulid/v2"
)

type traceContextKey struct{}

// EnsureDeliveryTraceID 从上下文中获取现有 TraceID，若缺失则自动生成新的规范化 TraceID。
func EnsureDeliveryTraceID(ctx context.Context) (context.Context, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if v, ok := ctx.Value(traceContextKey{}).(string); ok && v != "" {
		return ctx, v
	}

	newID := "trace-" + ulid.Make().String()
	return context.WithValue(ctx, traceContextKey{}, newID), newID
}

// TraceIDFromContext 从 context 中读取 TraceID。
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceContextKey{}).(string); ok {
		return v
	}
	return ""
}
