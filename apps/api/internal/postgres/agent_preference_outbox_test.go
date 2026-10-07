package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestPreferenceInvalidationNativeSourcePredicate(t *testing.T) {
	x, _ := preferenceInvalidationUnitFixture(false)
	source, current, e := resolvePreferenceOutboxTx(context.Background(), x, x.record.Event)
	if e != nil || !current || source.revision != 2 || source.row != "450" {
		t.Fatal(source, current, e)
	}
	x.rowRevision++
	if _, current, e = resolvePreferenceOutboxTx(context.Background(), x, x.record.Event); e != nil || current {
		t.Fatal("updated source must not inherit old permission", current, e)
	}
	// UNIT_STATIC: SQL selector text, not a PostgreSQL execution assertion.
	for _, part := range []string{"pp.written_profile_version", "pp.xmin::text", "FOR SHARE OF pp", "pp.owner_id=$2"} {
		if !strings.Contains(preferenceOutboxConfiguredSQL, part) {
			t.Fatal(part)
		}
	}
	for _, part := range []string{"ap.profile_version", "ap.xmin::text", "NOT EXISTS(SELECT 1 FROM agent_private_profiles", "FOR UPDATE OF ap"} {
		if !strings.Contains(preferenceOutboxClearedSQL, part) {
			t.Fatal(part)
		}
	}
	if strings.Contains(preferenceOutboxConfiguredSQL, "pp.fields") || strings.Contains(preferenceGrantMoreSQL, "g.revision<") || !strings.Contains(preferencePreviewCleanupSQL, "NOT EXISTS(SELECT 1 FROM agent_context_purpose_bindings") {
		t.Fatal("private bodies/history/anomaly must remain protected")
	}
	if !strings.Contains(outboxControlDueSQL, "preference-invalidation-v1") || !strings.Contains(outboxControlActiveSQL, "preference-invalidation-v1") || !strings.Contains(outboxControlDispatchHeadsSQL, "agent-preference-outbox-v1") {
		t.Fatal("new handler must use original global quota and terminal history")
	}
}
