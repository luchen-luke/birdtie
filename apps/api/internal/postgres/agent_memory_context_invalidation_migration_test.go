package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestMemoryInvalidationMigrationStaticActualHookAndOldGuards(t *testing.T) {
	read := func(path string) string {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		return strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	up := read("../../migrations/101_agent_memory_context_invalidation.sql")
	down := read("../../migrations/101_agent_memory_context_invalidation.down.sql")
	original := read("../../migrations/082_agent_candidate_atomic_consumer.sql")
	for _, name := range []string{"birdtie_guard_agent_outbox", "birdtie_guard_agent_consumer_inbox"} {
		start := strings.Index(original, "CREATE OR REPLACE FUNCTION "+name+"()")
		end := start + strings.Index(original[start:], "END $$;") + len("END $$;")
		old := original[start:end]
		before := old[:strings.Index(old, "BEGIN\n")+len("BEGIN\n")]
		after := old[len(before):]
		if !strings.Contains(down, old) || !strings.Contains(up, after) {
			t.Fatal("UNIT_STATIC: exact normalized original082 function body lost", name)
		}
	}
	for _, s := range []string{"AFTER INSERT OR UPDATE ON agent_memories", "NEW.source_type='EXPLICIT' OR NEW.status='DELETED'", "NEW.version=OLD.version", "NEW.causation_id IS NOT NULL", "only sameTx completed root", "NEW.received_at=i.updated_at", "p.xmin::text=(txid_current()%4294967296)::text", "i.xmin::text=(txid_current()%4294967296)::text", "76034", ")>=6", "memory_outbox_logical_mutation", "memory_outbox_root_budget"} {
		if !strings.Contains(up, s) {
			t.Fatal("UNIT_STATIC missing native producer/fence/root predicate", s)
		}
	}
	for _, s := range []string{"LOCK TABLE agent_memories,agent_domain_outbox,agent_consumer_inbox", "IF EXISTS", "101 down refuses retained Memory", "DROP TRIGGER memory_outbox_capture"} {
		if !strings.Contains(down, s) {
			t.Fatal("UNIT_STATIC unsafe down", s)
		}
	}
	if !strings.Contains(memoryGrantMoreSQL, "g.revoked_at IS NULL") || strings.Contains(memoryGrantMoreSQL, "g.revision<") || !strings.Contains(memoryPreviewCleanupSQL, "NOT EXISTS") || !strings.Contains(memoryGrantCleanupSQL, "g.revision=x.revision") {
		t.Fatal("stale grant exhausted revision must remain unfinished; bound/consumed history preserved")
	}
}
