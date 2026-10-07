package main

import "testing"

func TestMemoryInvalidationCommandUnitRegisteredHandler(t *testing.T) {
	_, ok := parseSettings([]string{"--local-development-only", "--subject", "82000000-0000-4000-8000-000000000001", "--handler", "memory-invalidation-v1"})
	if !ok {
		t.Fatal("原一次性控制 CLI 不能消费本人 Memory 失效链")
	}
}
