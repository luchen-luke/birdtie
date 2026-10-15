package momentmedia

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPublicMomentVideoRealCodec(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG"), os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set explicit absolute codec paths to run the real FFmpeg check")
	}
	root := t.TempDir()
	input := filepath.Join(root, "fixture.mp4")
	command := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=30:duration=1", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-map_metadata", "-1", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", "-movflags", "+faststart", input)
	if output, e := command.CombinedOutput(); e != nil {
		t.Fatalf("real synthetic fixture: %v %s", e, output)
	}
	raw, e := os.ReadFile(input)
	if e != nil {
		t.Fatal(e)
	}
	codec := &FFmpegVideoNormalizer{FFmpegPath: ffmpeg, FFprobePath: ffprobe, PrlimitPath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_PRLIMIT"), TempRoot: root}
	in := VideoStageInput{OperationID: "11111111-1111-4111-8111-111111111111", LocalScope: "22222222-2222-4222-8222-222222222222", Purpose: VideoPurpose, ContentType: "video/mp4", Content: raw}
	d, e := codec.Normalize(context.Background(), in)
	if e != nil {
		t.Fatal("actual normalized/reprobed/full-decoded derivative", e)
	}
	if !d.Contract.Valid() || d.Contract.Bytes != len(d.Video) || d.Contract.SHA256 != DigestBytes(d.Video) || d.Width != 320 || d.Height != 180 || d.Contract.AudioCodec != "aac" || d.Contract.DurationMS < 950 || d.Contract.DurationMS > 1100 {
		t.Fatal("actual output contract", d.Contract)
	}
	if _, e = jpeg.Decode(bytes.NewReader(d.Display)); e != nil {
		t.Fatal("actual JPEG poster", e)
	}
	if _, e = jpeg.Decode(bytes.NewReader(d.Thumbnail)); e != nil {
		t.Fatal("actual JPEG thumbnail", e)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 1 || entries[0].Name() != "fixture.mp4" {
		t.Fatal("private work directory not cleaned", entries, e)
	}
	_, release, e := TryVideoWork(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = codec.Normalize(context.Background(), in); e != ErrVideoBusy {
		release()
		t.Fatal("single process admission", e)
	}
	release()
	in.Purpose = "HUMAN_PRIVATE_VIDEO_MATERIAL"
	if _, e = codec.Normalize(context.Background(), in); e == nil {
		t.Fatal("private-purpose input accepted")
	}
}

// The exact self-generated QA clip exposed Linux x264 allocation failures at
// 512 MiB of virtual address space despite resident memory below 83 MiB.
// It is supplied through stdin, never a legacy owner file or database read.
func TestPublicMomentVideoSynthetic640x360(t *testing.T) {
	if os.Getenv("BIRDTIE_PUBLIC_VIDEO_SYNTHETIC_STDIN") != "1" {
		t.Skip("supply the fixed self-generated QA video on stdin")
	}
	ffmpeg, ffprobe := os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG"), os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Fatal("explicit codec paths required for the selected synthetic regression")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 285816))
	if err != nil || len(raw) != 285815 || DigestBytes(raw) != "63b3c39807ccd6547ecdf6749eab1eff36660d3d9a460d691785a5e0dffa05fb" {
		t.Fatal("fixed synthetic fixture hash and size required")
	}
	defer clear(raw)
	root := t.TempDir()
	codec := &FFmpegVideoNormalizer{FFmpegPath: ffmpeg, FFprobePath: ffprobe, PrlimitPath: os.Getenv("BIRDTIE_PUBLIC_VIDEO_PRLIMIT"), TempRoot: root}
	input := VideoStageInput{OperationID: "15500000-0000-4000-8000-000000000001", LocalScope: "15500000-0000-4000-8000-000000000002", Purpose: VideoPurpose, ContentType: "video/mp4", Content: raw}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	d, err := codec.Normalize(ctx, input)
	if err != nil {
		t.Fatal("actual Go normalizer rejected the fixed supported synthetic clip", err)
	}
	defer clear(d.Video)
	defer clear(d.Display)
	defer clear(d.Thumbnail)
	if !d.Contract.Valid() || d.Contract.Bytes != len(d.Video) || d.Contract.SHA256 != DigestBytes(d.Video) || d.Width != 640 || d.Height != 360 || d.Contract.AudioCodec != "aac" || d.Contract.DurationMS < 3900 || d.Contract.DurationMS > 4100 {
		t.Fatal("actual supported canonical contract changed")
	}
	for _, pixels := range [][]byte{d.Display, d.Thumbnail} {
		if _, err := jpeg.Decode(bytes.NewReader(pixels)); err != nil {
			t.Fatal("actual Go JPEG poster or thumbnail decode failed", err)
		}
	}
	now := time.Now().UTC()
	contract := d.Contract
	contract.Path = VideoStagePath(input.OperationID, 1, "video")
	stage := Stage{SchemaVersion: "public-moment-media-stage-v2", Kind: "VIDEO", Purpose: VideoPurpose, Video: &contract, MediaID: input.OperationID, Version: 1, LocalScope: input.LocalScope, DisplaySHA: d.DisplaySHA, DisplayBytes: len(d.Display), Width: d.Width, Height: d.Height, ThumbnailSHA: d.ThumbnailSHA, ThumbnailBytes: len(d.Thumbnail), ValidationKind: VideoValidation, ValidationStatus: "VALIDATED", AntivirusScan: "NOT_RUN", PrivacyRisk: "UNKNOWN", PreparedAt: now, RetainUntil: now.Add(StageTTL)}
	if !StageValid(stage) {
		t.Fatal("actual derived video cannot form a valid staged material")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("normalizer retained private temporary inputs or outputs", err)
	}
}
