package postgres

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestPreferenceInvalidationMigration102StaticContract(t *testing.T) {
	// UNIT_STATIC: these assertions inspect actual migration bytes, never execute SQL.
	up, e := os.ReadFile("../../migrations/102_agent_preference_context_invalidation.sql")
	if e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile("../../migrations/102_agent_preference_context_invalidation.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	old, e := os.ReadFile("../../migrations/101_agent_memory_context_invalidation.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{"AFTER INSERT OR UPDATE OR DELETE ON agent_private_profiles", "ap.profile_version>OLD.written_profile_version", "SELECT pp.xmin::text", "source_id=agent_id", "interval '5 seconds'", "d.attempt=0 AND d.fence=0", "p.xmin::text=(txid_current()%4294967296)::text", "i.fence=p.fence AND i.attempt=p.attempt", "schema_version=NEW.schema_version)>=6", "OLD.delivery_state NOT IN('PENDING','LEASED')"} {
		if !strings.Contains(string(up), part) {
			t.Fatal("missing actual native guard/hook", part)
		}
	}
	if !strings.Contains(string(down), "102 down refuses retained Preference events/progress/receipts") || strings.Contains(string(up), "NEW.fields") || strings.Contains(string(up), "INSERT INTO consent_grants") {
		t.Fatal("must retain history and sole consent authority")
	}
	// New branches do not erase the original 101 Memory and 082/091 Moment bodies.
	memoryStart := bytes.Index(old, []byte("    IF NEW.schema_version='agent-memory-outbox-v1'"))
	momentStart := bytes.Index(old[memoryStart:], []byte("    IF TG_OP='INSERT' THEN\r\n")) + memoryStart
	oldInbox := bytes.Index(old, []byte("CREATE OR REPLACE FUNCTION birdtie_guard_agent_consumer_inbox()"))
	if memoryStart < 0 || momentStart < memoryStart || !bytes.Contains(up, old[memoryStart:oldInbox]) {
		t.Fatal("original Memory/Moment outbox body changed")
	}
	oldInboxBody := bytes.Index(old[oldInbox:], []byte("    IF NEW.handler_version='memory-invalidation-v1'")) + oldInbox
	oldEnd := bytes.LastIndex(old, []byte("COMMIT;"))
	if !bytes.Contains(up, old[oldInboxBody:oldEnd]) || !bytes.Contains(down, old[bytes.Index(old, []byte("CREATE OR REPLACE FUNCTION birdtie_guard_agent_outbox()")):oldEnd]) {
		t.Fatal("old inbox or complete 101 down restore lost")
	}
}
