package content

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"strings"
	"testing"
	"time"
)

func TestMomentInteractionClosedContractAndInboxConcreteSnapshot(t *testing.T) {
	in := MomentInteractionInput{OperationID: momentControlTestOp, Action: "COMMENT", MomentID: momentControlTestID, ExpectedMomentRevision: 2, Body: "公开评论<仅此原操作>"}
	raw, _ := json.Marshal(in)
	if _, e := DecodeMomentInteraction(raw, false); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.TrimSuffix(string(raw), "}") + `,"recipientId":"other"}`, strings.Replace(string(raw), `"action":"COMMENT"`, `"action":"COMMENT","action":"LIKE"`, 1)} {
		if _, e := DecodeMomentInteraction([]byte(bad), false); e == nil {
			t.Fatal("unknown/duplicate writer field accepted")
		}
	}
	batch := InboxManagementInput{OperationID: momentControlTestOp, Action: "MARK_ALL_READ"}
	now := time.Now().UTC()
	batch.Cutoff = &now
	batch.SourceVersion = strings.Repeat("a", 64)
	first := InboxManagementDigest(batch)
	later := now.Add(time.Microsecond)
	batch.Cutoff = &later
	if first == InboxManagementDigest(batch) {
		t.Fatal("different concrete cutoff shares original digest")
	}
	batch.Cutoff = &now
	batch.SourceVersion = strings.Repeat("b", 64)
	if first == InboxManagementDigest(batch) {
		t.Fatal("different source snapshot shares original digest")
	}
	cursor, e := inbox.EncodePageCursor(inbox.PageCursor{Version: 1, OwnerID: momentControlTestOp, View: inbox.NotificationsView, Priority: 50, Time: now, ID: momentControlTestID, Category: "updates", UnreadOnly: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = inbox.NormalizePage(inbox.PageRequest{View: inbox.NotificationsView, Cursor: cursor, Category: "requests", UnreadOnly: true}, momentControlTestOp); e == nil {
		t.Fatal("cursor granted a different notification filter")
	}
}
