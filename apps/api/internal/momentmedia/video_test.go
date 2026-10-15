package momentmedia

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
