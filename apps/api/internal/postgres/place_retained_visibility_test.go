package postgres

import (
	"strings"
	"testing"
)

func TestRetainedPlaceVisibilityUsesCanonicalSourceAndBusinessClocks(t *testing.T) {
	sql := currentRetainedPlaceVisibilitySQL("public_place", "read_clock.n")
	for _, expected := range []string{"permission_id IS NOT NULL", "rights.source_url=public_place.source_ref", "rights.version=f.permission_version", "rights.state='CONFIRMED'", "rights.reviewed_by<>rights.declared_by", "f.valid_to>read_clock.n", "f.refresh_due>read_clock.n", "rights.retention_until>read_clock.n", "f.conflict_state='NONE'", "'name'", "'address'", "'coordinates'"} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("missing predicate %s", expected)
		}
	}
	if strings.Contains(sql, "@P@") || strings.Contains(sql, "@N@") {
		t.Fatal("unbound SQL template")
	}
}

func TestRetainedPlaceDeadlineBoundedToCanonicalFieldEvidence(t *testing.T) {
	sql := currentRetainedPlaceDeadlineSQL("p", "n")
	for _, expected := range []string{"least(p.expires_at", "min(least(f.valid_to,f.refresh_due,rights.retention_until))", "rights.source_url=p.source_ref", "rights.version=f.permission_version"} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("missing bound %s", expected)
		}
	}
}
