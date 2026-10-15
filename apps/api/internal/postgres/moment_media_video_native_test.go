package postgres

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

func TestPublicMomentVideoNativeCanonicalHTTPAndRetirement(t *testing.T) {
	if os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG") == "" || os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE") == "" {
		t.Skip("set explicit absolute codec paths for the video native chain")
	}
	f := savedPageNative(t)
	codec := &mm.FFmpegVideoNormalizer{FFmpegPath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG"), FFprobePath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE"), PrlimitPath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_PRLIMIT"), TempRoot: t.TempDir()}
	if e := f.store.ConfigureMomentMediaVideo(codec); e != nil {
		t.Fatal("real codec configuration", e)
	}
	file := filepath.Join(t.TempDir(), "fixture.mp4")
	cmd := exec.Command(codec.FFmpegPath, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30:duration=1", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-movflags", "+faststart", file)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("actual local fixture: %v %s", e, output)
	}
	raw, e := os.ReadFile(file)
	if e != nil {
		t.Fatal(e)
	}
	moment, _ := publicPhotoNativeMoment(t, f, false)
	access := mm.Access{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
	stageInput := mm.VideoStageInput{OperationID: f.id(t), LocalScope: f.id(t), Purpose: mm.VideoPurpose, ContentType: "video/mp4", Content: raw}
	prepared, e := f.store.PrepareMomentMediaVideo(f.ctx, access, stageInput)
	if e != nil || prepared.Material == nil || !mm.StageValid(*prepared.Material) || prepared.Material.Kind != "VIDEO" {
		t.Fatal("actual video stage", e)
	}
	replay, e := f.store.PrepareMomentMediaVideo(f.ctx, access, stageInput)
	if e != nil || !reflect.DeepEqual(replay, prepared) {
		t.Fatal("original stage replay", e)
	}
	image, e := f.store.PrepareMomentMedia(f.ctx, access, mm.StageInput{OperationID: f.id(t), LocalScope: stageInput.LocalScope, ContentType: "image/png", Content: publicPhotoNativeBytes(t, false)})
	if e != nil {
		t.Fatal(e)
	}
	rights := mm.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}
	selected := mm.AttachmentInput{MediaID: prepared.Material.MediaID, MediaVersion: 1, Rights: rights}
	in := mm.Input{OperationID: f.id(t), Action: "REPLACE", ExpectedMomentRevision: 2, ExpectedSetVersion: 0, Attachments: []mm.AttachmentInput{selected}}
	mixed := in
	mixed.OperationID = f.id(t)
	mixed.Attachments = append([]mm.AttachmentInput{selected}, mm.AttachmentInput{MediaID: image.Material.MediaID, MediaVersion: 1, Rights: rights})
	if _, e = f.store.PreviewMomentMedia(f.ctx, access, moment, mixed); e == nil {
		t.Fatal("mixed image/video accepted")
	}
	preview, e := f.store.PreviewMomentMedia(f.ctx, access, moment, in)
	if e != nil || len(preview.Images) != 1 || preview.Images[0].Stage.Kind != "VIDEO" || preview.Images[0].ThumbnailBase64 == "" {
		t.Fatal("sealed full video contract and actual poster", e)
	}
	in.SourceVersion, in.Approval = preview.SourceVersion, preview.Approval
	original, e := f.store.CommitMomentMedia(f.ctx, access, moment, in)
	if e != nil || !original.Valid(moment, in.OperationID) {
		t.Fatal("atomic video commit", e)
	}
	again, e := f.store.CommitMomentMedia(f.ctx, access, moment, in)
	if e != nil || !reflect.DeepEqual(again, original) {
		t.Fatal("original publication retry", e)
	}
	current, e := f.store.ReadPublicMomentMedia(f.ctx, mm.Access{}, moment)
	if e != nil || !current.Valid(moment, time.Now().UTC()) || current.SchemaVersion != "public-moment-media-set-v2" || len(current.Attachments) != 1 {
		t.Fatal("typed current public set", e)
	}
	video := current.Attachments[0]
	handler := httpapi.New(nil, nil, nil, f.store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", video.Video.Path, nil))
	if response.Code != 200 || response.Header().Get("Content-Type") != "video/mp4" || response.Header().Get("Cache-Control") != "no-store" || mm.DigestBytes(response.Body.Bytes()) != video.Video.SHA256 || response.Body.Len() != video.Video.Bytes {
		t.Fatal("actual registered public canonical byte route", response.Code, response.Body.Len())
	}
	// 153's existing helper sees the complete typed stage and all its real bytes.
	ctx, frame, e := f.store.beginMomentMedia(f.ctx, access, "", f.id(t))
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := f.store.CaptureMomentMediaMaterialsInTx(ctx, frame.tx, access, []mm.AttachmentInput{selected})
	if e != nil || snapshot.Materials[0].Video == nil || len(snapshot.Thumbnails[0]) == 0 {
		t.Fatal("whole thread typed capture", e)
	}
	if _, e = f.store.AttachMomentMediaInTx(ctx, frame.tx, access, moment, 2, 1, []mm.AttachmentInput{selected}); e != nil {
		t.Fatal("canonical video in parent transaction", e)
	}
	frame.tx.Rollback(context.Background())
	var rolled int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM public_moment_media_video_payloads WHERE moment_id=$1 AND set_version=2`, moment).Scan(&rolled); e != nil || rolled != 0 {
		t.Fatal("rolled back video bytes escaped", rolled, e)
	}
	withdrawn := mm.Input{OperationID: f.id(t), Action: "DELETE", ExpectedMomentRevision: 2, ExpectedSetVersion: 1, Attachments: []mm.AttachmentInput{}}
	approval, e := f.store.PreviewMomentMedia(f.ctx, access, moment, withdrawn)
	if e != nil {
		t.Fatal(e)
	}
	withdrawn.SourceVersion, withdrawn.Approval = approval.SourceVersion, approval.Approval
	if _, e = f.store.CommitMomentMedia(f.ctx, access, moment, withdrawn); e != nil {
		t.Fatal("whole collection withdrawal", e)
	}
	var retained bool
	if e = f.pool.QueryRow(f.ctx, `SELECT content IS NOT NULL FROM public_moment_media_video_payloads WHERE moment_id=$1 AND set_version=1`, moment).Scan(&retained); e != nil || retained {
		t.Fatal("withdrawal left canonical clip bytes", e)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", video.Video.Path, nil))
	if response.Code == 200 {
		t.Fatal("withdrawn URL remained readable")
	}
	fact, e := f.store.ReadMomentMediaOperation(f.ctx, access, moment, in.OperationID, false)
	if e != nil || !reflect.DeepEqual(fact, original) {
		t.Fatal("withdrawal changed immutable original effect", e)
	}
	// Publishing a fresh current version and editing text uses the same byte
	// retirement trigger; original facts retain only IDs and approved hashes.
	in.OperationID = f.id(t)
	in.ExpectedSetVersion = 2
	in.SourceVersion = ""
	in.Approval = ""
	approval, e = f.store.PreviewMomentMedia(f.ctx, access, moment, in)
	if e != nil {
		t.Fatal(e)
	}
	in.SourceVersion, in.Approval = approval.SourceVersion, approval.Approval
	if _, e = f.store.CommitMomentMedia(f.ctx, access, moment, in); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE moments SET body='视频正文修改',revision=revision+1 WHERE id=$1`, moment)
	if e = f.pool.QueryRow(f.ctx, `SELECT content IS NOT NULL FROM public_moment_media_video_payloads WHERE moment_id=$1 AND set_version=3`, moment).Scan(&retained); e != nil || retained {
		t.Fatal("body revision left canonical clip bytes", e)
	}
	var receipt []byte
	if e = f.pool.QueryRow(f.ctx, `SELECT receipt FROM public_moment_media_operations WHERE actor_account_id=$1 AND operation_id=$2`, access.AccountID, in.OperationID).Scan(&receipt); e != nil {
		t.Fatal(e)
	}
	var decoded map[string]any
	if json.Unmarshal(receipt, &decoded) != nil || decoded["approval"] != nil || decoded["contentBase64"] != nil {
		t.Fatal("private input or approval leaked into original facts")
	}
}
