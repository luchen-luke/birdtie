package foundation

import (
	"testing"
	"time"
)

func TestSourceFreshness(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	before := now.Add(-48 * time.Hour)
	after := now.Add(48 * time.Hour)

	tests := []struct {
		name   string
		source Source
		want   string
	}{
		{"unverified", Source{UpdatedAt: before}, "unverified"},
		{"current", Source{UpdatedAt: before, VerifiedAt: &before, ExpiresAt: &after}, "current"},
		{"changed since verification", Source{UpdatedAt: now, VerifiedAt: &before, ExpiresAt: &after}, "review_needed"},
		{"expired", Source{UpdatedAt: now, VerifiedAt: &before, ExpiresAt: &before}, "expired"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.source.SetFreshness(now)
			if tt.source.Freshness != tt.want {
				t.Fatalf("freshness = %q, want %q", tt.source.Freshness, tt.want)
			}
		})
	}
}
