package postgres

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"reflect"
	"strings"
	"testing"
	"time"
)

func interactionNativeActor(t *testing.T, f *savedPageNativeFixture) *savedPageNativeFixture {
	t.Helper()
	other := *f
	other.actor = identity.Actor{ID: f.id(t), AccountType: "person"}
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, other.actor.ID)
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	other.digest = digest
	other.session = f.id(t)
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, other.session, other.actor.ID, digest[:])
	return &other
}
func interactionNativeWrite(t *testing.T, f *savedPageNativeFixture, in content.MomentInteractionInput) (content.MomentInteractionReceipt, content.MomentInteractionInput) {
	t.Helper()
	p, e := f.store.PreviewMomentInteraction(f.ctx, f.digest, f.actor, in)
	if e != nil {
		t.Fatal("native interaction preview", e)
	}
	if e = p.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("native preview original proof", e)
	}
	in.SourceVersion, in.Approval = p.SourceVersion, p.Approval
	r, e := f.store.CommitMomentInteraction(f.ctx, f.digest, f.actor, in)
	if e != nil || !content.ValidMomentInteractionReceipt(r, in.OperationID) {
		t.Fatal("native interaction commit", e)
	}
	return r, in
}
func TestMomentInteractionNativeOriginalFactsNotificationsAndInboxSnapshot(t *testing.T) {
	f := savedPageNative(t)
	other := interactionNativeActor(t, f)
	draft, _ := mc144Write(t, f, mc144Input(t, f))
	input := mc144Input(t, f).Input
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: input})
	mid := draft.MomentID
	like := content.MomentInteractionInput{OperationID: f.id(t), Action: "LIKE", MomentID: mid, ExpectedMomentRevision: 2}
	liked, approved := interactionNativeWrite(t, other, like)
	if replay, e := f.store.CommitMomentInteraction(f.ctx, other.digest, other.actor, approved); e != nil || replay.AppliedVersion != liked.AppliedVersion {
		t.Fatal("original like replay", e)
	}
	comment := content.MomentInteractionInput{OperationID: f.id(t), Action: "COMMENT", MomentID: mid, ExpectedMomentRevision: 2, ExpectedLikeVersion: 1, Body: "PRIVATE_NOTICE_MUST_NOT_CACHE_COMMENT_BODY"}
	commented, original := interactionNativeWrite(t, other, comment)
	if !identity.HumanControlUUID(commented.CommentID) {
		t.Fatal("stable comment ID missing")
	}
	var wrongKind int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM birdtie_native_notification_source('moment_like',(SELECT event_id FROM moment_interaction_operations WHERE actor_account_id=$1 AND operation_id=$2),$3)`, other.actor.ID, comment.OperationID, f.actor.ID).Scan(&wrongKind); e != nil || wrongKind != 0 {
		t.Fatal("comment event masqueraded as a like notification", e)
	}
	if replay, e := f.store.CommitMomentInteraction(f.ctx, other.digest, other.actor, original); e != nil || replay.CommentID != commented.CommentID {
		t.Fatal("comment response loss duplicated original ID", e)
	}
	page, e := f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}, mid, "", commented.CommentID)
	if e != nil || page.LikeCount != 1 || page.CommentCount != 1 || len(page.Comments) != 1 || page.Comments[0].CanDelete {
		t.Fatal("current comments/counts/other owner", e)
	}
	if e = page.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("actual response proof", e)
	}
	notices, e := f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, inbox.PageRequest{View: inbox.NotificationsView})
	if e != nil || len(notices.Items) != 2 || notices.UnreadCount != 2 {
		t.Fatal("real notification writes", e, len(notices.Items))
	}
	wire, _ := json.Marshal(notices)
	if strings.Contains(string(wire), comment.Body) {
		t.Fatal("comment body copied to Inbox")
	}
	for _, item := range notices.Items {
		target, e := f.store.GetInboxMomentTarget(f.ctx, f.digest, f.actor, item.ID)
		if e != nil || target["momentId"] != mid {
			t.Fatal("real same-ID notification target", e)
		}
	}
	batch := content.InboxManagementInput{OperationID: f.id(t), Action: "MARK_ALL_READ", Category: "updates"}
	preview, e := f.store.PreviewInboxManagement(f.ctx, f.digest, f.actor, batch)
	if e != nil || preview.Count != 2 {
		t.Fatal("explicit original Inbox snapshot", e)
	}
	if e = preview.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("Inbox proof", e)
	}
	batch = preview.Input
	batch.Approval = preview.Approval
	newer := comment
	newer.OperationID = f.id(t)
	newer.Body = "之后新增的评论通知"
	interactionNativeWrite(t, other, newer)
	marked, e := f.store.CommitInboxManagement(f.ctx, f.digest, f.actor, batch)
	if e != nil || marked.AffectedCount != 2 || marked.UnreadCount != nil || marked.Cutoff == nil || marked.SourceVersion != batch.SourceVersion {
		t.Fatal("new concurrent notification was incorrectly included", e, marked)
	}
	readMarked, e := f.store.ReadInboxManagementOperation(f.ctx, f.digest, f.actor, batch.OperationID, false)
	if e != nil || !reflect.DeepEqual(marked, readMarked) {
		t.Fatal("original Inbox operation GET changed its immutable receipt", e, marked, readMarked)
	}
	repeatedMarked, e := f.store.CommitInboxManagement(f.ctx, f.digest, f.actor, batch)
	if e != nil || !reflect.DeepEqual(marked, repeatedMarked) {
		t.Fatal("same original Inbox operation retry changed its immutable receipt", e, marked, repeatedMarked)
	}
	changed := batch
	cutoff := batch.Cutoff.Add(time.Microsecond)
	changed.Cutoff = &cutoff
	if _, e = f.store.CommitInboxManagement(f.ctx, f.digest, f.actor, changed); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("same operation accepted a new cutoff", e)
	}
	unread, e := f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, inbox.PageRequest{View: inbox.NotificationsView, Category: "updates", UnreadOnly: true})
	if e != nil || len(unread.Items) != 1 || unread.UnreadCount != 1 {
		t.Fatal("independent current Inbox page lost the concurrent notification", e)
	}
	delete := content.MomentInteractionInput{OperationID: f.id(t), Action: "DELETE_COMMENT", MomentID: mid, ExpectedMomentRevision: 2, ExpectedLikeVersion: 1, CommentID: commented.CommentID}
	interactionNativeWrite(t, other, delete)
	if _, e = f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{}, mid, "", commented.CommentID); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("deleted comment anchor released old body", e)
	}
	if fact, e := f.store.ReadMomentInteractionOperation(f.ctx, other.digest, other.actor, comment.OperationID, false); e != nil || fact.CommentID != commented.CommentID {
		t.Fatal("later deletion erased original comment fact", e)
	}
	f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, f.actor.ID, other.actor.ID)
	f.exec(t, `DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.actor.ID, other.actor.ID)
	retired, e := f.store.ListHumanInboxPage(f.ctx, f.digest, f.actor, inbox.PageRequest{View: inbox.NotificationsView})
	if e != nil || len(retired.Items) != 0 {
		t.Fatal("Block ABA resurrected old sensitive interaction notifications", e, len(retired.Items))
	}
	stopped := content.MomentInteractionInput{OperationID: f.id(t), Action: "COMMENT", MomentID: mid, ExpectedMomentRevision: 2, ExpectedLikeVersion: 1, Body: "停止后不应公开"}
	p, e := f.store.PreviewMomentInteraction(f.ctx, other.digest, other.actor, stopped)
	if e != nil {
		t.Fatal(e)
	}
	if r, e := f.store.ReadMomentInteractionOperation(f.ctx, other.digest, other.actor, stopped.OperationID, true); e != nil || r.Status != "STOPPED" {
		t.Fatal("original stop", e)
	}
	stopped.SourceVersion, stopped.Approval = p.SourceVersion, p.Approval
	if r, e := f.store.CommitMomentInteraction(f.ctx, other.digest, other.actor, stopped); e != nil || r.Status != "STOPPED" {
		t.Fatal("late original comment submitted after stop", e)
	}
}

func TestMomentRepliesNativeParentCurrentUnknownAndUnavailable(t *testing.T) {
	f := savedPageNative(t)
	parentActor := interactionNativeActor(t, f)
	replier := interactionNativeActor(t, f)
	draft, _ := mc144Write(t, f, mc144Input(t, f))
	input := mc144Input(t, f).Input
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "PUBLISH", MomentID: draft.MomentID, ExpectedRevision: 1, Input: input})
	mid := draft.MomentID
	parent, _ := interactionNativeWrite(t, parentActor, content.MomentInteractionInput{OperationID: f.id(t), Action: "COMMENT", MomentID: mid, ExpectedMomentRevision: 2, Body: "parent current public body"})
	replyInput := content.MomentInteractionInput{OperationID: f.id(t), Action: "COMMENT", MomentID: mid, ExpectedMomentRevision: 2, Body: "actual same moment reply", ParentCommentID: parent.CommentID, ExpectedParentCommentVersion: 1}
	replied, approved := interactionNativeWrite(t, replier, replyInput)
	replay, e := f.store.CommitMomentInteraction(f.ctx, replier.digest, replier.actor, approved)
	if e != nil || replay.CommentID != replied.CommentID {
		t.Fatal("reply duplicated original operation", e)
	}
	fact, e := f.store.ReadMomentInteractionOperation(f.ctx, replier.digest, replier.actor, replyInput.OperationID, false)
	if e != nil || fact.PayloadDigest != content.MomentInteractionDigest(replyInput) {
		t.Fatal("reply original unknown recovery", e)
	}
	page, e := f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}, mid, "", replied.CommentID)
	if e != nil || !page.CanComment || len(page.Comments) != 1 || page.Comments[0].Version != 1 || !page.Comments[0].CanReply || page.Comments[0].ReplyTo == nil || page.Comments[0].ReplyTo.Status != "AVAILABLE" || page.Comments[0].ReplyTo.Body != "parent current public body" {
		t.Fatal("canonical reply relation unavailable", e, page)
	}
	anonymous, e := f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{}, mid, "", replied.CommentID)
	if e != nil || anonymous.CanComment || anonymous.Comments[0].CanReply {
		t.Fatal("anonymous reply eligibility", e)
	}
	changed := replyInput
	changed.OperationID = f.id(t)
	changed.ExpectedParentCommentVersion = 2
	if _, e = f.store.PreviewMomentInteraction(f.ctx, replier.digest, replier.actor, changed); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("stale parent revision allowed", e)
	}

	blocked := replyInput
	blocked.OperationID = f.id(t)
	bp, err := f.store.PreviewMomentInteraction(f.ctx, replier.digest, replier.actor, blocked)
	if err != nil {
		t.Fatal(err)
	}
	blocked.SourceVersion, blocked.Approval = bp.SourceVersion, bp.Approval
	hcNativeBlock(t, replier, identity.BlockControlInput{OperationID: f.id(t), Action: "BLOCK", AccountID: parentActor.actor.ID})
	if _, err = f.store.CommitMomentInteraction(f.ctx, replier.digest, replier.actor, blocked); !errors.Is(err, content.ErrNotFound) {
		t.Fatal("blocked reply parent accepted", err)
	}
	hcNativeBlock(t, replier, identity.BlockControlInput{OperationID: f.id(t), Action: "UNBLOCK", AccountID: parentActor.actor.ID, ExpectedVersion: 1})
	if _, err = f.store.CommitMomentInteraction(f.ctx, replier.digest, replier.actor, blocked); !errors.Is(err, identity.ErrControlChanged) {
		t.Fatal("parent block ABA reused approval", err)
	}
	pending := replyInput
	pending.OperationID = f.id(t)
	preview, e := f.store.PreviewMomentInteraction(f.ctx, replier.digest, replier.actor, pending)
	if e != nil {
		t.Fatal(e)
	}
	pending.SourceVersion, pending.Approval = preview.SourceVersion, preview.Approval
	interactionNativeWrite(t, parentActor, content.MomentInteractionInput{OperationID: f.id(t), Action: "DELETE_COMMENT", MomentID: mid, ExpectedMomentRevision: 2, CommentID: parent.CommentID})
	if _, e = f.store.CommitMomentInteraction(f.ctx, replier.digest, replier.actor, pending); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("deleted parent silently downgraded to top level", e)
	}
	if e = page.ResponseProof.RevalidateHumanControl(f.ctx); e == nil {
		t.Fatal("withdrawn parent reused old response proof")
	}
	after, e := f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}, mid, "", replied.CommentID)
	if e != nil || len(after.Comments) != 1 || after.Comments[0].ReplyTo == nil || after.Comments[0].ReplyTo.Status != "UNAVAILABLE" || after.Comments[0].ReplyTo.Body != "" || after.Comments[0].ReplyTo.AuthorPublicProfile != nil {
		t.Fatal("unavailable parent leaked content", e)
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM moment_comments WHERE moment_id=$1`, mid).Scan(&count); e != nil || count != 2 {
		t.Fatal("invalid reply wrote another comment", e, count)
	}
	wrongMoment := replyInput
	wrongMoment.OperationID = f.id(t)
	wrongMoment.ParentCommentID = f.id(t)
	if _, e = f.store.PreviewMomentInteraction(f.ctx, replier.digest, replier.actor, wrongMoment); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("unknown parent accepted", e)
	}
}
