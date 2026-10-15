package legacyhistory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"testing"
)

func TestHistoricalProjectionKeepsThreadOwnerRolesAndOriginalNotificationState(t *testing.T) {
	const a = "16500000-0000-4000-8000-000000000001"
	const b = "16500000-0000-4000-8000-000000000002"
	const tid = "16500000-0000-4000-8000-000000000003"
	stamp := "2026-10-14T00:00:00Z"
	thread := row{"id": tid, "owner_id": b, "journey_id": "16500000-0000-4000-8000-000000000004", "summary": "历史合成记录", "status": "active", "context_revision": json.Number("2"), "created_at": stamp, "updated_at": stamp}
	message := row{"id": "16500000-0000-4000-8000-000000000005", "thread_id": tid, "role": "assistant", "content": "仅合成历史回复", "request_id": "original-request", "created_at": stamp}
	notice := row{"id": "16500000-0000-4000-8000-000000000006", "recipient_id": a, "type": "follow", "event_type": "follow", "category": "follow", "aggregate_type": "user", "aggregate_id": b, "payload": row{"targetTitle": "不作当前投影"}, "created_at": stamp, "payload_schema_version": json.Number("1"), "inbox_version": json.Number("3")}
	p := Plan{Original: legacymigration.Plan{Owners: []legacymigration.OwnerPlan{{SourceID: a}, {SourceID: b}}}}
	project(&p, map[string][]row{"ai_journey_threads": {thread}, "ai_journey_messages": {message}, "notifications": {notice}})
	if p.Threads[0].OwnerSourceID != b || p.Messages[0].Role != "ASSISTANT" || p.Messages[0].RequestID != "original-request" || p.Notifications[0].ReadAt != nil || p.Counts.HistoricalUnread != 1 || p.Counts.Messages != 1 {
		t.Fatal("private owner, role, or historical unread changed")
	}
	if _, e := Build("", nil, nil, nil, nil, nil, nil); e == nil {
		t.Fatal("unsealed source accepted")
	}
	defer func() {
		if recover() == nil {
			t.Error("orphan history message accepted")
		}
	}()
	message["thread_id"] = a
	project(&p, map[string][]row{"ai_journey_messages": {message}})
}
