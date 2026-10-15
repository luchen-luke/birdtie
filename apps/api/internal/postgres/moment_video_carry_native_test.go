package postgres

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

// One real-codec/native transaction chain covers the device19 regression and
// its authority boundaries. All SQL fixtures live in the disposable child DB.
func TestPublicMomentVideoCarryNativeExpiredPurgedAndBoundaries(t *testing.T) {
	if os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG") == "" || os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE") == "" {
		t.Skip("explicit real codec paths required")
	}
	f := savedPageNative(t)
	codec := &mm.FFmpegVideoNormalizer{FFmpegPath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG"), FFprobePath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE"), PrlimitPath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_PRLIMIT"), TempRoot: t.TempDir()}
	if e := f.store.ConfigureMomentMediaVideo(codec); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "carry-fixture.mp4")
	cmd := exec.Command(codec.FFmpegPath, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30:duration=1", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-movflags", "+faststart", file)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("real fixture codec: %v %s", e, output)
	}
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	access := mm.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	scope, empty, restore := f.id(t), "", false
	stage, e := f.store.PrepareMomentMediaVideo(f.ctx, access, mm.VideoStageInput{OperationID: f.id(t), LocalScope: scope, Purpose: mm.VideoPurpose, ContentType: "video/mp4", Content: raw})
	if e != nil || stage.Material == nil || !mm.StageValid(*stage.Material) {
		t.Fatal("canonical stage", e)
	}
	selected := []mm.AttachmentInput{{MediaID: stage.Material.MediaID, MediaVersion: stage.Material.Version, Rights: mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}}
	none := []content.MomentLocationInput{}
	text := func(body string) content.MomentInput {
		return content.MomentInput{Body: body, TimePrecision: "unknown", LocationPrecision: "none", Locations: &none}
	}
	removed := []content.MomentThreadRemoval{}
	in := content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", BaseMomentID: &empty, LocalScope: scope, Audience: "PUBLIC", RemovedItems: &removed, Items: []content.MomentThreadItem{
		{Restore: &restore, Input: text("video source"), Media: selected},
		{Restore: &restore, Input: text("text source"), Media: []mm.AttachmentInput{}},
	}}
	originalInput := threadNativeApprove(t, f, in)
	original, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, originalInput)
	if e != nil || len(original.Members) != 2 {
		t.Fatal("fresh canonical publication", e)
	}
	videoID, textID := original.Members[0].MomentID, original.Members[1].MomentID
	other := interactionNativeActor(t, f)
	interactionNativeWrite(t, other, content.MomentInteractionInput{OperationID: f.id(t), Action: "LIKE", MomentID: videoID, ExpectedMomentRevision: 2})
	comment, _ := interactionNativeWrite(t, other, content.MomentInteractionInput{OperationID: f.id(t), Action: "COMMENT", MomentID: videoID, ExpectedMomentRevision: 2, ExpectedLikeVersion: 1, Body: "same original video identity"})
	handler := httpapi.New(nil, nil, nil, f.store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	readVideo := func() mm.Set {
		t.Helper()
		set, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, videoID)
		if e != nil || !set.Valid(videoID, time.Now().UTC()) || len(set.Attachments) != 1 || set.Attachments[0].Video == nil {
			t.Fatal("current public video", e)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", set.Attachments[0].Video.Path, nil))
		if response.Code != 200 || mm.DigestBytes(response.Body.Bytes()) != stage.Material.Video.SHA256 || response.Body.Len() != stage.Material.Video.Bytes {
			t.Fatal("current canonical public bytes changed", response.Code)
		}
		return set
	}
	firstSet := readVideo()
	capture := func() momentMediaCarry {
		t.Helper()
		ctx, frame, e := f.store.beginMomentMedia(f.ctx, access, videoID, f.id(t))
		if e != nil {
			t.Fatal(e)
		}
		defer frame.tx.Rollback(context.Background())
		carry, e := f.store.captureMomentMediaCarry(ctx, frame.tx, access, videoID)
		if e != nil || len(carry.materials) != 1 {
			t.Fatal("exact current owned carry", e)
		}
		return carry
	}
	firstCarry := capture()
	// Advance only this isolated stage's timestamps; its bounded ten-minute
	// retention constraint remains valid. The public rights are still current.
	f.exec(t, `UPDATE moment_media_materials SET prepared_at=prepared_at-interval '11 minutes',retain_until=retain_until-interval '11 minutes' WHERE id=$1`, stage.Material.MediaID)
	var fresh bool
	if e := f.pool.QueryRow(f.ctx, `SELECT retain_until>clock_timestamp() FROM moment_media_materials WHERE id=$1`, stage.Material.MediaID).Scan(&fresh); e != nil || fresh {
		t.Fatal("stage did not expire", e)
	}
	newExpired := in
	newExpired.OperationID = f.id(t)
	if _, e := f.store.PreviewMomentThread(f.ctx, f.digest, f.actor, newExpired); e == nil {
		t.Fatal("expired stage authorized a new publication")
	}
	next := func(r content.MomentThreadReceipt, body string) content.MomentThreadInput {
		t.Helper()
		items := []content.MomentThreadItem{}
		for _, id := range []string{textID, videoID} {
			m, e := f.store.GetOwnMoment(f.ctx, f.actor.ID, id)
			if e != nil {
				t.Fatal(e)
			}
			value := text(m.Body)
			if id == textID {
				value.Body = body
			}
			items = append(items, content.MomentThreadItem{MomentID: id, ExpectedRevision: m.Revision, Restore: &restore, Input: value, Media: nil})
		}
		return content.MomentThreadInput{OperationID: f.id(t), Action: "EDIT", ThreadID: r.ThreadID, BaseMomentID: &empty, LocalScope: scope, Audience: "PUBLIC", ExpectedVersion: r.AppliedVersion, RemovedItems: &removed, Items: items}
	}
	reorder := threadNativeApprove(t, f, next(original, "edited text after stage expiry"))
	current, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, reorder)
	if e != nil {
		// The schema168 reproduction must prove atomic rollback, not just error.
		m, readErr := f.store.GetOwnMoment(f.ctx, f.actor.ID, videoID)
		_, operationErr := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, reorder.OperationID, false)
		if readErr != nil || m.Revision != 2 || !errors.Is(operationErr, content.ErrNotFound) {
			t.Fatal("failed carry was not rolled back", readErr, operationErr)
		}
		t.Fatalf("expired-stage EDIT carry rejected; original revision intact and operation absent: %v", e)
	}
	if current.RootMomentID != videoID || current.Members[0].MomentID != textID || current.Members[1].MomentID != videoID {
		t.Fatal("reorder replaced original identity")
	}
	secondSet := readVideo()
	if secondSet.SetVersion != firstSet.SetVersion+2 || secondSet.MomentRevision != firstSet.MomentRevision+1 {
		t.Fatal("carry did not advance direct set/revision")
	}
	var proved bool
	if e := f.pool.QueryRow(f.ctx, `SELECT content IS NULL AND retired_xact_id IS NOT NULL FROM public_moment_media_video_payloads WHERE moment_id=$1 AND set_version=$2`, videoID, firstSet.SetVersion).Scan(&proved); e != nil || !proved {
		t.Fatal("retirement lacks full transaction proof", e)
	}
	oldURL := httptest.NewRecorder()
	handler.ServeHTTP(oldURL, httptest.NewRequest("GET", firstSet.Attachments[0].Video.Path, nil))
	if oldURL.Code == 200 {
		t.Fatal("old revision bytes still public")
	}
	interactions, e := f.store.ReadMomentInteractions(f.ctx, content.PublicMomentAccess{Actor: f.actor, SessionDigest: f.digest}, videoID, "", comment.CommentID)
	if e != nil || interactions.LikeCount != 1 || interactions.CommentCount != 1 || interactions.Comments[0].ID != comment.CommentID {
		t.Fatal("carry detached original interactions", e)
	}
	t.Log("expired-stage EDIT/reorder preserved exact bytes, Moment ID, like/comment and retired old URL")
	var purged int
	if e := f.pool.QueryRow(f.ctx, `SELECT birdtie_purge_moment_media_materials(100)`).Scan(&purged); e != nil || purged < 1 {
		t.Fatal("stage purge", e)
	}
	var remaining int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM moment_media_video_materials WHERE media_id=$1`, stage.Material.MediaID).Scan(&remaining); e != nil || remaining != 0 {
		t.Fatal("canonical upload stage survived purge", e)
	}
	approved := threadNativeApprove(t, f, next(current, "edited text after canonical stage purge"))
	current, e = f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approved)
	if e != nil {
		t.Fatal("purged-stage carry", e)
	}
	readVideo()
	replayed, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, approved)
	if e != nil || !reflect.DeepEqual(replayed, current) {
		t.Fatal("carry retry duplicated or changed fact", e)
	}
	originalAgain, e := f.store.ReadMomentThreadOperation(f.ctx, f.digest, f.actor, original.OperationID, false)
	if e != nil || !reflect.DeepEqual(originalAgain, original) {
		t.Fatal("original publication receipt changed", e)
	}
	t.Log("purged-stage EDIT carried exact public bytes; same operation replay and original receipt immutable")
	carry := capture()
	// Each direct adversarial helper runs in a transaction that is rolled back;
	// it traverses the real insertion guard after the normal retirement trigger.
	rejectCarry := func(label, target string, a mm.Access, candidate momentMediaCarry) {
		t.Helper()
		ctx, frame, e := f.store.beginMomentMedia(f.ctx, access, target, f.id(t))
		if e != nil {
			t.Fatal(e)
		}
		defer frame.tx.Rollback(context.Background())
		var revision, version int64
		if e = frame.tx.QueryRow(ctx, `UPDATE moments SET revision=revision+1 WHERE id=$1 RETURNING revision`, target).Scan(&revision); e != nil {
			t.Fatal(e)
		}
		if e = frame.tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM public_moment_media_sets WHERE moment_id=$1),0)`, target).Scan(&version); e != nil {
			t.Fatal(e)
		}
		if _, e = f.store.applyMomentMediaCarry(ctx, frame.tx, a, target, revision, version, candidate); e == nil {
			t.Fatal(label, "unexpectedly authorized carry")
		}
	}
	otherAccess := mm.Access{AccountID: other.actor.ID, AccountType: "person", SessionDigest: other.digest}
	rejectCarry("cross owner", videoID, otherAccess, carry)
	rejectCarry("cross Moment", textID, access, carry)
	tampered := carry
	tampered.materials = append([]momentMediaMaterial{}, carry.materials...)
	tampered.materials[0].video = append([]byte{}, carry.materials[0].video...)
	tampered.materials[0].video[0] ^= 1
	rejectCarry("changed canonical bytes", videoID, access, tampered)
	extended := carry
	extended.inputs = append([]mm.AttachmentInput{}, carry.inputs...)
	extended.inputs[0].Rights.RetentionUntil = extended.inputs[0].Rights.RetentionUntil.Add(time.Hour)
	rejectCarry("extended retention", videoID, access, extended)
	readVideo()
	// Retire the currently public set in a committed operation. Retired bytes
	// are not a source for a later edit, even when an old snapshot is retained.
	set := readVideo()
	removeMedia := mm.Input{OperationID: f.id(t), Action: "DELETE", ExpectedMomentRevision: set.MomentRevision, ExpectedSetVersion: set.SetVersion, Attachments: []mm.AttachmentInput{}}
	preview, e := f.store.PreviewMomentMedia(f.ctx, access, videoID, removeMedia)
	if e != nil {
		t.Fatal(e)
	}
	removeMedia.SourceVersion, removeMedia.Approval = preview.SourceVersion, preview.Approval
	if _, e = f.store.CommitMomentMedia(f.ctx, access, videoID, removeMedia); e != nil {
		t.Fatal(e)
	}
	rejectCarry("previously retired source", videoID, access, carry)
	rejectCarry("older historical source", videoID, access, firstCarry)
	if _, e := f.pool.Exec(f.ctx, `UPDATE public_moment_media_video_payloads SET content=NULL WHERE moment_id=$1 AND set_version=$2`, videoID, set.SetVersion); e == nil {
		t.Fatal("already retired no-op forged transaction provenance")
	}
	if _, e := f.pool.Exec(f.ctx, `UPDATE public_moment_media_video_payloads SET retired_xact_id=pg_current_xact_id() WHERE moment_id=$1 AND set_version=$2`, videoID, set.SetVersion); e == nil {
		t.Fatal("retirement full transaction proof can be forged")
	}
	final, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, videoID)
	if e != nil || final.Status != "DELETED" || len(final.Attachments) != 0 {
		t.Fatal("negative probes revived retired media", e)
	}
	t.Log("expired new upload, cross-owner/Moment, altered bytes, extended rights and historical-retired carry rejected; NULL no-op cannot forge xmin")
}
