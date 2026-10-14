package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParticipationSourceVersionNamespacesPreserveNativeEpoch(t *testing.T) {
	id := "27000000-0000-4000-8000-000000000001"
	at := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	raw := []byte(`{"status":"going","updated_at":"2026-10-13T00:00:00+00:00"}`)
	token, a, e := participationSourceTokens(id, at, raw, "11")
	token2, b, e2 := participationSourceTokens(id, at, raw, "12")
	if e != nil || e2 != nil || token != token2 || a == b || token == a {
		t.Fatal("057 snapshot,063 epoch or metadata hash conflated")
	}
	if _, _, e = participationSourceTokens(id, time.Time{}, nil, "11"); e == nil {
		t.Fatal("invented source accepted")
	}
}
func TestParticipation133ClosedCompositionKeepsTask132(t *testing.T) {
	p := func(name string) string {
		raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if e != nil {
			t.Fatal(e)
		}
		return string(raw)
	}
	up, down := p("133_agent_participation_invalidation.sql"), p("133_agent_participation_invalidation.down.sql")
	for _, need := range []string{"participation_outbox_capture AFTER INSERT OR UPDATE", "birdtie_capture_participation_outbox", "source_revision=0", "NEW.causation_id IS NOT NULL", "ACTIVITY_PARTICIPATION_UPDATED", "agent-task-request-outbox-v1", "only sameTx completed root may create fixed depth1 invalidation child"} {
		if !strings.Contains(up, need) {
			t.Fatal("closed133 baseline lost", need)
		}
	}
	if strings.Contains(up, "event_type IN('TASK_REQUEST_CHANGED','TASK_CONTEXT_INVALIDATION') AND source_status IN('ACTIVE','COMPLETED','FAILED','going'") {
		t.Fatal("task status contract widened")
	}
	for _, forbidden := range []string{"DROP TRIGGER memory_participation_invalidated", "UPDATE agent_memories", "UPDATE activity_participations SET", "INSERT INTO agent_memory_candidates"} {
		if strings.Contains(up, forbidden) {
			t.Fatal("metadata hook widened domain", forbidden)
		}
	}
	for _, need := range []string{"participation maintenance history prevents downgrade", "agent-task-request-outbox-v1", "CREATE OR REPLACE FUNCTION birdtie_guard_agent_outbox()", "DROP TRIGGER participation_outbox_capture"} {
		if !strings.Contains(down, need) {
			t.Fatal("used down/132 restore lost", need)
		}
	}
	guard := regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION birdtie_guard_agent_consumer_inbox\(\).*?END \$\$;`)
	currentInbox := guard.FindString(up)
	for _, need := range []string{"IF NEW.handler_version='participation-invalidation-v1'", "parent.schema_version IS DISTINCT FROM 'agent-participation-outbox-v1'", "NEW.fence<>parent.fence", "NEW.attempt<>parent.attempt", "OLD.control_state NOT IN('LEASED','PENDING')", "NEW.fence<>OLD.fence+1", "NEW.attempt<>OLD.attempt+1", "parent.lease_until<=clock_timestamp()", "parent.expires_at<=clock_timestamp()", "participation checkpoint requires retained fenced claim"} {
		if !strings.Contains(currentInbox, need) {
			t.Fatal("participation inbox bounded progress guard absent", need)
		}
	}
	if baseline := guard.FindString(p("132_agent_task_request_context_invalidation.sql")); baseline == "" || guard.FindString(down) != baseline {
		t.Fatal("133 down did not restore exact frozen132 inbox guard")
	}
}
