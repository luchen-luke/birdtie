package postgres

import (
	"strings"
	"testing"
)

func TestParticipationCleanupIsBoundedOldReferencesOnly(t *testing.T) {
	for _, sql := range []string{participationEvidenceCleanupSQL, participationCandidateCleanupSQL} {
		for _, needle := range []string{"owner_id=$1", "agent_id=$2", "ACTIVITY_PARTICIPATION", "inv.id<=$6", "SKIP LOCKED", "LIMIT 128", "9223372036854775807"} {
			if !strings.Contains(sql, needle) {
				t.Fatal("cleanup omitted native bound", needle)
			}
		}
		if strings.Contains(sql, "UPDATE agent_memories") || strings.Contains(sql, "DELETE FROM") {
			t.Fatal("independent Memory/history deleted")
		}
	}
	if !strings.Contains(participationEvidenceCleanupSQL, "inv.created_at>=e.observed_at") || !strings.Contains(participationCandidateCleanupSQL, "inv.created_at>=c.created_at") {
		t.Fatal("094 before/after source ABA lost")
	}
	if !strings.Contains(participationCleanupMoreSQL, "IS DISTINCT FROM") || strings.Contains(participationCleanupMoreSQL, "SKIP LOCKED") || strings.Contains(participationCleanupMoreSQL, "9223372036854775807") {
		t.Fatal("more falsely hid locked/exhausted stale refs")
	}
	if strings.Contains(participationCleanupMoreSQL, "TASK_CONTEXT_READ") {
		t.Fatal("independent explicit-memory grants revoked")
	}
}
