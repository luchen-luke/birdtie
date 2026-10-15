package postgres

import (
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

func threadNativeInput(t *testing.T, f *savedPageNativeFixture) content.MomentThreadInput {
	t.Helper()
	input := *mc144Input(t, f).Input
	second := input
	second.Title, second.Body = "第二条", "第二条的独立正文"
	return content.MomentThreadInput{OperationID: f.id(t), Action: "CREATE_PUBLISH", LocalScope: f.id(t), Audience: "PUBLIC", Items: []content.MomentThreadItem{{Input: input, Media: []momentmedia.AttachmentInput{}}, {Input: second, Media: []momentmedia.AttachmentInput{}}}}
}

func threadNativeApprove(t *testing.T, f *savedPageNativeFixture, in content.MomentThreadInput) content.MomentThreadInput {
	t.Helper()
	p, e := f.store.PreviewMomentThread(f.ctx, f.digest, f.actor, in)
	if e != nil {
		t.Fatal("native thread preview", e)
	}
	if e = p.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("original thread preview frame", e)
	}
	in.SourceVersion, in.Approval = p.SourceVersion, p.Approval
	return in
}

func TestMomentThreadNativeAtomicRollbackOriginalRecoveryAndWholeWithdraw(t *testing.T) {
	f := savedPageNative(t)
	failed := threadNativeInput(t, f)
	var pixels bytes.Buffer
	if e := jpeg.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil); e != nil {
		t.Fatal(e)
	}
	a := momentmedia.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	stage, e := f.store.PrepareMomentMedia(f.ctx, a, momentmedia.StageInput{OperationID: f.id(t), LocalScope: failed.LocalScope, ContentType: "image/jpeg", Content: pixels.Bytes()})
	if e != nil || stage.Material == nil {
		t.Fatal("actual owner stage", e)
	}
	selected := []momentmedia.AttachmentInput{{MediaID: stage.Material.MediaID, MediaVersion: stage.Material.Version, Rights: entitycover.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}}
	failed.Items[0].Media = selected
	failed.Items[1].Input.Body = "THREAD_ROLLBACK_SECOND"
	approvedFailure := threadNativeApprove(t, f, failed)
	f.exec(t, `CREATE FUNCTION thread_test_second_child_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.body='THREAD_ROLLBACK_SECOND' THEN RAISE EXCEPTION 'isolated second-child rollback probe'; END IF; RETURN NEW; END $$`)
	f.exec(t, `CREATE TRIGGER thread_test_second_child BEFORE INSERT ON moments FOR EACH ROW EXECUTE FUNCTION thread_test_second_child_fail()`)
	if _, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approvedFailure); e == nil {
		t.Fatal("second child failure committed a partial thread")
	}
	f.exec(t, `DROP TRIGGER thread_test_second_child ON moments`)
	f.exec(t, `DROP FUNCTION thread_test_second_child_fail()`)
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM moments WHERE author_account_id=$1)+(SELECT count(*) FROM moment_threads WHERE author_account_id=$1)+(SELECT count(*) FROM moment_thread_operations WHERE actor_account_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='moment')+(SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1)+(SELECT count(*) FROM public_moment_media_sets WHERE owner_account_id=$1)+(SELECT count(*) FROM public_moment_media_attachments a JOIN moment_media_materials m ON m.id=a.media_id WHERE m.owner_account_id=$1)`, f.actor.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("atomic rollback left canonical children or operation fact", e, count)
	}
	if _, e := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, failed.OperationID, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("failed operation has a success fact", e)
	}

	in := threadNativeInput(t, f)
	in.LocalScope = failed.LocalScope
	in.Items[0].Media = selected
	approved := threadNativeApprove(t, f, in)
	r, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approved)
	if e != nil || !content.ValidMomentThreadReceipt(r, in.OperationID) || len(r.Members) != 2 || r.Members[0].MediaSetVersion != 1 {
		t.Fatal("complete atomic thread and same-tx attachment", e)
	}
	if old, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approved); e != nil || old.ThreadID != r.ThreadID || old.Members[1].MomentID != r.Members[1].MomentID {
		t.Fatal("original retry created new IDs", e)
	}
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM moments WHERE author_account_id=$1`, f.actor.ID).Scan(&count); e != nil || count != 2 {
		t.Fatal("retry duplicated children", e, count)
	}
	view, e := f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, r.Members[1].MomentID, true)
	if e != nil || len(view.Members) != 2 || view.Members[0].MomentID == nil || view.Members[1].MomentID == nil {
		t.Fatal("current ordered public selectors", e)
	}
	if e = view.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("encoded thread read proof", e)
	}
	edit := in.Items[1].Input
	edit.Body = "单条修改保留原位置"
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "UPDATE_PUBLIC", MomentID: r.Members[1].MomentID, ExpectedRevision: 2, Input: &edit})
	beforeWithdraw := content.MomentThreadInput{OperationID: f.id(t), Action: "WITHDRAW", ThreadID: r.ThreadID, Audience: "PUBLIC", ExpectedVersion: 1, Items: []content.MomentThreadItem{}}
	stale := threadNativeApprove(t, f, beforeWithdraw)
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: r.Members[0].MomentID, ExpectedRevision: 2})
	if _, e = f.store.CommitMomentThread(f.ctx, f.digest, f.actor, stale); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("whole withdrawal reused old child snapshot", e)
	}
	view, e = f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, r.Members[1].MomentID, true)
	if e != nil || view.Members[0].Position != 0 || view.Members[0].MomentID != nil || view.RootMomentID != nil || view.Members[1].Revision != 3 {
		t.Fatal("single withdrawal leaked or removed original position", e)
	}
	beforeWithdraw.OperationID = f.id(t)
	current := threadNativeApprove(t, f, beforeWithdraw)
	withdrawn, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, current)
	if e != nil || withdrawn.Members[0].AppliedRevision != 3 || withdrawn.Members[1].AppliedRevision != 4 || withdrawn.AppliedVersion != 2 {
		t.Fatal("whole withdrawal did not use actual current child states", e)
	}
	if _, e = f.store.ReadMomentThread(f.ctx, content.PublicMomentAccess{}, r.ThreadID, false); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("withdrawn thread remained public", e)
	}
	if original, e := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, in.OperationID, false); e != nil || original.Members[1].AppliedRevision != 2 || original.ThreadID != r.ThreadID {
		t.Fatal("current withdrawal erased original publication fact", e)
	}
	stopped := threadNativeInput(t, f)
	late := threadNativeApprove(t, f, stopped)
	if stop, e := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, stopped.OperationID, true); e != nil || stop.Status != "STOPPED" {
		t.Fatal("thread STOP", e)
	}
	if old, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, late); e != nil || old.Status != "STOPPED" {
		t.Fatal("late thread commit crossed STOP", e)
	}
}
