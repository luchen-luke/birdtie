package audittrace

import (
	"context"
	"strings"
	"testing"
)

func TestPrivateRequestContext(t *testing.T) {
	for _, id := range []string{"request_123", strings.Repeat("a", 64)} {
		ctx := WithRequestID(context.Background(), id)
		if RequestID(ctx) != id {
			t.Fatal("valid request correlation missing")
		}
		if RequestID(context.WithValue(context.Background(), "request_id", id)) != "" {
			t.Fatal("public key impersonated trace")
		}
	}
	for _, id := range []string{"", "short", strings.Repeat("a", 65), "request\nforged", "request你好", "request/unsafe"} {
		if RequestID(WithRequestID(context.Background(), id)) != "" {
			t.Fatal("dirty trace accepted")
		}
	}
	if RequestID(nil) != "" || RequestID(context.Background()) != "" {
		t.Fatal("background must be unknown")
	}
}

func TestTraceDoesNotCarryAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	traced := WithRequestID(ctx, "request_valid")
	cancel()
	if traced.Err() != context.Canceled || RequestID(traced) != "request_valid" {
		t.Fatal("context semantics lost")
	}
	if RequestID(WithRequestID(traced, "bad")) != "" {
		t.Fatal("invalid rebinding inherited old trace")
	}
}
