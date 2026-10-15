package postgres

import (
	"context"
	"encoding/json"
	"errors"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"reflect"
	"testing"
	"time"
)

func TestMomentMediaNativeNineAtomicReplayRetireAndCompose(t *testing.T) {
	f := savedPageNative(t)
	moment, _ := publicPhotoNativeMoment(t, f, false)
	a := mm.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	scope := f.id(t)
	inputs := []mm.AttachmentInput{}
	for i := 0; i < 9; i++ {
		stage, e := f.store.PrepareMomentMedia(f.ctx, a, mm.StageInput{OperationID: f.id(t), LocalScope: scope, ContentType: "image/png", Content: publicPhotoNativeBytes(t, i%2 == 0)})
		if e != nil || stage.Material == nil || !mm.StageValid(*stage.Material) {
			t.Fatal("real owned material preparation", e)
		}
		inputs = append(inputs, mm.AttachmentInput{MediaID: stage.Material.MediaID, MediaVersion: stage.Material.Version, Rights: mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}})
	}
	in := mm.Input{OperationID: f.id(t), Action: "REPLACE", ExpectedMomentRevision: 2, ExpectedSetVersion: 0, Attachments: inputs}
	p, e := f.store.PreviewMomentMedia(f.ctx, a, moment, in)
	if e != nil || len(p.Images) != 9 {
		t.Fatal("full nine image preview", e)
	}
	if e = p.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal(e)
	}
	in.SourceVersion, in.Approval = p.SourceVersion, p.Approval
	r, e := f.store.CommitMomentMedia(f.ctx, a, moment, in)
	if e != nil || !r.Valid(moment, in.OperationID) || len(r.Media) != 9 {
		t.Fatal("atomic full collection", e)
	}
	original := r
	replay, e := f.store.CommitMomentMedia(f.ctx, a, moment, in)
	if e != nil || !reflect.DeepEqual(original, replay) {
		t.Fatal("original effect replay changed", e)
	}
	view, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, moment)
	if e != nil || len(view.Attachments) != 9 || !view.Valid(moment, time.Now().UTC()) {
		t.Fatal("actual public collection", e)
	}
	for i, image := range view.Attachments {
		if image.MediaID != inputs[i].MediaID || image.Position != i {
			t.Fatal("author order changed")
		}
	}
	image := view.Attachments[0]
	b, e := f.store.ReadPublicMomentMediaContent(f.ctx, mm.Access{}, mm.ContentSelector{MomentID: moment, MomentRevision: 2, SetVersion: 1, MediaID: image.MediaID, MediaVersion: image.MediaVersion, Variant: "thumbnail"}, func(meta mm.Attachment, b []byte) ([]byte, error) {
		if mm.DigestBytes(b) != meta.ThumbnailSHA {
			return nil, mm.ErrChanged
		}
		return b, nil
	})
	if e != nil || len(b) == 0 {
		t.Fatal("actual normalized content", e)
	}
	// An invalid later item cannot leave a partial replacement, effect or receipt.
	bad := mm.Input{OperationID: f.id(t), Action: "REPLACE", ExpectedMomentRevision: 2, ExpectedSetVersion: 1, Attachments: append([]mm.AttachmentInput{}, inputs...)}
	bad.Attachments[8].MediaID = f.id(t)
	if _, e = f.store.PreviewMomentMedia(f.ctx, a, moment, bad); e == nil {
		t.Fatal("missing ninth material accepted")
	}
	var rows int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM public_moment_media_attachments WHERE moment_id=$1 AND display IS NOT NULL`, moment).Scan(&rows); e != nil || rows != 9 {
		t.Fatal("partial failed effect", rows, e)
	}
	stopped, e := f.store.ReadMomentMediaOperation(f.ctx, a, moment, bad.OperationID, true)
	if e != nil || stopped.Status != "STOPPED" {
		t.Fatal("stop fence", e)
	}
	bad.SourceVersion, bad.Approval = in.SourceVersion, in.Approval
	closed, e := f.store.CommitMomentMedia(f.ctx, a, moment, bad)
	if e != nil || closed.Status != "STOPPED" {
		t.Fatal("stopped operation resurrected", e)
	}
	// Composition captures stage before a future parent and attaches in one tx.
	ctx, frame, e := f.store.beginMomentMedia(f.ctx, a, "", f.id(t))
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := f.store.CaptureMomentMediaMaterialsInTx(ctx, frame.tx, a, inputs[:1])
	if e != nil || len(snapshot.Thumbnails) != 1 || snapshot.SourceVersion == "" {
		t.Fatal("thread material snapshot", e)
	}
	if e = f.store.LockMomentMediaMaterialsInTx(ctx, frame.tx, a, []string{inputs[0].MediaID}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.AttachMomentMediaInTx(ctx, frame.tx, a, moment, 2, 1, inputs[:1]); e != nil {
		t.Fatal("in-tx composition", e)
	}
	frame.tx.Rollback(context.Background())
	after, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, moment)
	if e != nil || len(after.Attachments) != 9 || after.SetVersion != 1 {
		t.Fatal("rollback left fake partial collection", e)
	}
	f.exec(t, `UPDATE moments SET body='修改后的正文',revision=revision+1 WHERE id=$1`, moment)
	if _, e = f.store.FinalizePublicMomentMedia(f.ctx, mm.Access{}, view); e == nil {
		t.Fatal("old output survived revision")
	}
	retired, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, moment)
	if e != nil || retired.Status != "DELETED" || len(retired.Attachments) != 0 {
		t.Fatal("whole old set not retired", e)
	}
	fact, e := f.store.ReadMomentMediaOperation(f.ctx, a, moment, in.OperationID, false)
	if e != nil || !reflect.DeepEqual(fact, original) {
		t.Fatal("original receipt rewritten after retirement", e)
	}
	if _, e = f.store.ReadPublicMomentMediaContent(f.ctx, mm.Access{}, mm.ContentSelector{MomentID: moment, MomentRevision: 2, SetVersion: 1, MediaID: image.MediaID, MediaVersion: 1, Variant: "display"}, func(_ mm.Attachment, b []byte) ([]byte, error) { return b, nil }); !errors.Is(e, mm.ErrNotFound) && !errors.Is(e, mm.ErrChanged) {
		t.Fatal("old media bytes stayed readable", e)
	}
	var stored []byte
	if e = f.pool.QueryRow(f.ctx, `SELECT receipt FROM public_moment_media_operations WHERE actor_account_id=$1 AND operation_id=$2`, a.AccountID, in.OperationID).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	var raw map[string]any
	if json.Unmarshal(stored, &raw) != nil || raw["approval"] != nil || raw["contentBase64"] != nil {
		t.Fatal("approval/raw bytes entered immutable facts")
	}
}
