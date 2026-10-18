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

func TestMomentReplyClosedParentVersionAndDigest(t *testing.T) {
	top := MomentInteractionInput{OperationID: momentControlTestOp, Action: "COMMENT", MomentID: momentControlTestID, ExpectedMomentRevision: 2, Body: "reply"}
	reply := top
	reply.ParentCommentID = momentControlTestOp
	reply.ExpectedParentCommentVersion = 1
	raw, _ := json.Marshal(reply)
	got, e := DecodeMomentInteraction(raw, false)
	if e != nil || got.ParentCommentID != reply.ParentCommentID {
		t.Fatal("reply decode", e)
	}
	if MomentInteractionDigest(top) == MomentInteractionDigest(reply) {
		t.Fatal("reply target omitted from digest")
	}
	changed := reply
	changed.ExpectedParentCommentVersion++
	if MomentInteractionDigest(changed) == MomentInteractionDigest(reply) {
		t.Fatal("parent revision omitted")
	}
	for _, bad := range []MomentInteractionInput{{OperationID: top.OperationID, Action: "COMMENT", MomentID: top.MomentID, ExpectedMomentRevision: 2, Body: "reply", ParentCommentID: reply.ParentCommentID}, {OperationID: top.OperationID, Action: "COMMENT", MomentID: top.MomentID, ExpectedMomentRevision: 2, Body: "reply", ExpectedParentCommentVersion: 1}, {OperationID: top.OperationID, Action: "LIKE", MomentID: top.MomentID, ExpectedMomentRevision: 2, ParentCommentID: reply.ParentCommentID, ExpectedParentCommentVersion: 1}} {
		if _, e := NormalizeMomentInteraction(bad, false); e == nil {
			t.Fatal("invalid parent pair/action accepted")
		}
	}
	duplicate := strings.TrimSuffix(string(raw), "}") + `,"parentCommentId":"` + momentControlTestID + `"}`
	if _, e := DecodeMomentInteraction([]byte(duplicate), false); e == nil {
		t.Fatal("duplicate parent accepted")
	}
}
