package momentmedia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/entitycover"
)

const VideoPurpose = "PUBLIC_MOMENT_VIDEO_STAGE_V2"
const VideoValidation = "FFMPEG_TRANSCODE_REPROBE_FULL_DECODE_V2"
const MaxVideoBytes = 20 * 1024 * 1024
const MaxVideoDurationMS = 30000
const VideoProcessMemory = 256 * 1024 * 1024

var ErrVideoBusy = errors.New("video resource admission busy")

type VideoContract struct {
	MIME        string `json:"mimeType"`
	DurationMS  int64  `json:"durationMs"`
	VideoCodec  string `json:"codecVideo"`
	AudioCodec  string `json:"codecAudio"`
	PixelFormat string `json:"pixelFormat"`
	SHA256      string `json:"videoSha256"`
	Bytes       int    `json:"videoBytes"`
	Path        string `json:"videoPath"`
}

func (v VideoContract) Valid() bool {
	return v.MIME == "video/mp4" && v.DurationMS > 0 && v.DurationMS <= MaxVideoDurationMS && v.VideoCodec == "h264" && (v.AudioCodec == "aac" || v.AudioCodec == "none") && v.PixelFormat == "yuv420p" && Hash(v.SHA256) && v.Bytes > 0 && v.Bytes <= MaxVideoBytes
}

type VideoStageInput struct {
	OperationID string `json:"operationId"`
	LocalScope  string `json:"localScope"`
	Purpose     string `json:"purpose"`
	ContentType string `json:"contentType"`
	Content     []byte `json:"-"`
}

func VideoStageDigest(in VideoStageInput) string {
	return Digest([]any{"moment-video-stage-v2", in.OperationID, in.LocalScope, in.Purpose, in.ContentType, DigestBytes(in.Content)})
}
func VideoStagePath(id string, version int64, variant string) string {
	return "/v1/me/moment-media-materials/" + id + "/" + strconv.FormatInt(version, 10) + "/" + variant
}

type VideoStore interface {
	PrepareMomentMediaVideo(context.Context, Access, VideoStageInput) (StageReceipt, error)
	ReadMomentVideoStageContent(context.Context, Access, string, int64, string, func(Stage, []byte) ([]byte, error)) ([]byte, error)
	MomentMediaVideoAvailable() bool
}

var videoWork = make(chan struct{}, 1)

type videoPermitKey struct{}
type videoPermit struct {
	active bool
	mu     sync.Mutex
}

// Admission occurs before a request body or retained clip is read.
func TryVideoWork(ctx context.Context) (context.Context, func(), error) {
	if p, ok := ctx.Value(videoPermitKey{}).(*videoPermit); ok {
		p.mu.Lock()
		active := p.active
		p.mu.Unlock()
		if active {
			return ctx, func() {}, nil
		}
	}
	select {
	case videoWork <- struct{}{}:
		p := &videoPermit{active: true}
		var once sync.Once
		return context.WithValue(ctx, videoPermitKey{}, p), func() { once.Do(func() { p.mu.Lock(); p.active = false; p.mu.Unlock(); <-videoWork }) }, nil
	default:
		return ctx, func() {}, ErrVideoBusy
	}
}

type FFmpegVideoNormalizer struct{ FFmpegPath, FFprobePath, PrlimitPath, TempRoot string }

func (v *FFmpegVideoNormalizer) Validate() error {
	if v == nil {
		return ErrUnavailable
	}
	for _, name := range []string{v.FFmpegPath, v.FFprobePath} {
		if !filepath.IsAbs(name) {
			return ErrUnavailable
		}
		info, e := os.Stat(name)
		if e != nil || !info.Mode().IsRegular() {
			return ErrUnavailable
		}
	}
	if v.TempRoot != "" && !filepath.IsAbs(v.TempRoot) {
		return ErrUnavailable
	}
	return validateVideoPlatform(v)
}

type VideoDerivative struct {
	Derivative
	Video    []byte
	Contract VideoContract
}
type probeDocument struct {
	Streams []struct {
		Type     string            `json:"codec_type"`
		Codec    string            `json:"codec_name"`
		Format   string            `json:"pix_fmt"`
		Width    int               `json:"width"`
		Height   int               `json:"height"`
		Duration string            `json:"duration"`
		Frames   string            `json:"nb_frames"`
		Rate     string            `json:"avg_frame_rate"`
		Tags     map[string]string `json:"tags"`
		SideData []map[string]any  `json:"side_data_list"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		Name     string            `json:"format_name"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}
type videoInfo struct {
	width, height                       int
	duration                            int64
	videoCodec, audioCodec, pixelFormat string
	frames                              int
}

func parseVideoProbe(raw []byte, normalized bool) (videoInfo, error) {
	var doc probeDocument
	var out videoInfo
	if len(raw) > 64*1024 || json.Unmarshal(raw, &doc) != nil || len(doc.Streams) < 1 || len(doc.Streams) > 2 {
		return out, ErrInvalid
	}
	seconds, e := strconv.ParseFloat(doc.Format.Duration, 64)
	if e != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > 30 {
		return out, ErrInvalid
	}
	out.duration = int64(math.Round(seconds * 1000))
	out.audioCodec = "none"
	videoCount, audioCount := 0, 0
	for _, s := range doc.Streams {
		if s.Type == "video" {
			videoCount++
			out.width, out.height, out.videoCodec, out.pixelFormat = s.Width, s.Height, s.Codec, s.Format
			out.frames, _ = strconv.Atoi(s.Frames)
			if s.Width < 2 || s.Height < 2 || max(s.Width, s.Height) > 1920 || min(s.Width, s.Height) > 1080 || s.Width*s.Height > 1920*1080 {
				return out, ErrInvalid
			}
			if normalized && (s.Codec != "h264" || s.Format != "yuv420p" || max(s.Width, s.Height) > 1280 || min(s.Width, s.Height) > 720 || s.Width%2 != 0 || s.Height%2 != 0 || len(s.SideData) > 0) {
				return out, ErrInvalid
			}
		} else if s.Type == "audio" {
			audioCount++
			out.audioCodec = s.Codec
			if normalized && s.Codec != "aac" {
				return out, ErrInvalid
			}
		} else {
			return out, ErrInvalid
		}
		for key := range s.Tags {
			if normalized && key != "language" && key != "handler_name" && key != "vendor_id" && key != "encoder" {
				return out, ErrInvalid
			}
		}
	}
	if videoCount != 1 || audioCount > 1 {
		return out, ErrInvalid
	}
	for key := range doc.Format.Tags {
		if normalized && key != "major_brand" && key != "minor_version" && key != "compatible_brands" && key != "encoder" {
			return out, ErrInvalid
		}
	}
	return out, nil
}

type boundedOutput struct {
	bytes.Buffer
	maximum int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.maximum {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(p)
}

func (v *FFmpegVideoNormalizer) run(ctx context.Context, program string, args ...string) ([]byte, error) {
	cmd, cleanup, e := videoPlatformCommand(ctx, v, program, args)
	if e != nil {
		return nil, e
	}
	defer cleanup()
	out, stderr := &boundedOutput{maximum: 64 * 1024}, &boundedOutput{maximum: 64 * 1024}
	cmd.Stdout, cmd.Stderr = out, stderr
	if e = cmd.Start(); e != nil {
		return nil, ErrInvalid
	}
	stop, e := videoPlatformStarted(cmd)
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, e
	}
	defer stop()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	watch := time.NewTicker(30 * time.Millisecond)
	defer watch.Stop()
	for {
		select {
		case e = <-done:
			if e != nil {
				return nil, ErrInvalid
			}
			return out.Bytes(), nil
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			<-done
			return nil, ErrInvalid
		case <-watch.C:
			if videoPlatformMemoryExceeded(cmd.Process.Pid) {
				_ = cmd.Process.Kill()
				<-done
				return nil, ErrInvalid
			}
		}
	}
}

func writeVideoPrivate(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	return closeErr
}
func readVideoBounded(path string, maximum int) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(maximum)+1))
	if e != nil || len(b) < 1 || len(b) > maximum {
		return nil, ErrInvalid
	}
	return b, nil
}

func (v *FFmpegVideoNormalizer) Normalize(ctx context.Context, in VideoStageInput) (VideoDerivative, error) {
	var out VideoDerivative
	if v.Validate() != nil || !ID(in.OperationID) || !ID(in.LocalScope) || in.Purpose != VideoPurpose || in.ContentType != "video/mp4" || len(in.Content) < 24 || len(in.Content) > MaxVideoBytes || string(in.Content[4:8]) != "ftyp" {
		return out, ErrInvalid
	}
	ctx, release, e := TryVideoWork(ctx)
	if e != nil {
		return out, e
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	dir, e := os.MkdirTemp(v.TempRoot, "birdtie-video-")
	if e != nil {
		return out, ErrUnavailable
	}
	base := v.TempRoot
	if base == "" {
		base = os.TempDir()
	}
	absoluteBase, _ := filepath.Abs(base)
	absoluteDir, _ := filepath.Abs(dir)
	relative, e := filepath.Rel(absoluteBase, absoluteDir)
	if e != nil || relative == "." || filepath.IsAbs(relative) || filepath.Dir(relative) != "." {
		return out, ErrUnavailable
	}
	defer os.RemoveAll(absoluteDir)
	if e = os.Chmod(dir, 0700); e != nil {
		return out, ErrUnavailable
	}
	original, normalized, poster := filepath.Join(dir, "original.mp4"), filepath.Join(dir, "canonical.mp4"), filepath.Join(dir, "poster.jpg")
	if writeVideoPrivate(original, in.Content) != nil {
		return out, ErrUnavailable
	}
	if writeVideoPrivate(normalized, nil) != nil || writeVideoPrivate(poster, nil) != nil {
		return out, ErrUnavailable
	}
	inputArgs := []string{"-v", "error", "-max_alloc", "33554432", "-protocol_whitelist", "file,pipe", "-enable_drefs", "0", "-use_absolute_path", "0", "-max_streams", "2"}
	probeArgs := append(append([]string{}, inputArgs...), "-show_streams", "-show_format", "-of", "json", original)
	raw, e := v.run(ctx, v.FFprobePath, probeArgs...)
	if e != nil {
		return out, e
	}
	source, e := parseVideoProbe(raw, false)
	if e != nil {
		return out, e
	}
	// Probe bounds metadata, then actually decode every input sample before
	// writing a normalized derivative. A damaged tail cannot be ignored.
	decode := append(append([]string{}, inputArgs...), "-nostdin", "-xerror", "-err_detect", "explode", "-threads", "1", "-i", original, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-")
	if _, e = v.run(ctx, v.FFmpegPath, decode...); e != nil {
		return out, e
	}
	filter := "scale=w='if(gte(iw,ih),min(1280,iw),min(720,iw))':h='if(gte(iw,ih),min(720,ih),min(1280,ih))':force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1"
	args := append(append([]string{}, inputArgs...), "-nostdin", "-y", "-xerror", "-err_detect", "explode", "-threads", "1", "-filter_threads", "1", "-i", original, "-map", "0:v:0", "-map", "0:a?", "-map_metadata", "-1", "-map_chapters", "-1", "-vf", filter, "-r", "30", "-t", strconv.FormatFloat(float64(source.duration)/1000, 'f', 3, 64), "-c:v", "libx264", "-preset", "veryfast", "-crf", "24", "-pix_fmt", "yuv420p", "-threads", "1", "-c:a", "aac", "-b:a", "96k", "-ac", "2", "-ar", "48000", "-movflags", "+faststart", "-fflags", "+bitexact", "-flags:v", "+bitexact", "-flags:a", "+bitexact", "-fs", strconv.Itoa(MaxVideoBytes+1), "-f", "mp4", normalized)
	if _, e = v.run(ctx, v.FFmpegPath, args...); e != nil {
		return out, e
	}
	if os.Chmod(normalized, 0600) != nil {
		return out, ErrUnavailable
	}
	raw, e = v.run(ctx, v.FFprobePath, append(append([]string{}, inputArgs...), "-show_streams", "-show_format", "-of", "json", normalized)...)
	if e != nil {
		return out, e
	}
	checked, e := parseVideoProbe(raw, true)
	if e != nil || abs64(checked.duration-source.duration) > 100 {
		return out, ErrInvalid
	}
	decoded := append(append([]string{}, inputArgs...), "-nostdin", "-xerror", "-err_detect", "explode", "-threads", "1", "-i", normalized, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-")
	if _, e = v.run(ctx, v.FFmpegPath, decoded...); e != nil {
		return out, e
	}
	posterArgs := append(append([]string{}, inputArgs...), "-nostdin", "-y", "-threads", "1", "-i", normalized, "-map", "0:v:0", "-frames:v", "1", "-q:v", "3", "-threads", "1", poster)
	if _, e = v.run(ctx, v.FFmpegPath, posterArgs...); e != nil {
		return out, e
	}
	if os.Chmod(poster, 0600) != nil {
		return out, ErrUnavailable
	}
	clip, e := readVideoBounded(normalized, MaxVideoBytes)
	if e != nil {
		return out, e
	}
	pixels, e := readVideoBounded(poster, MaxDisplayBytes)
	if e != nil {
		return out, e
	}
	configuration, format, e := image.DecodeConfig(bytes.NewReader(pixels))
	if e != nil || format != "jpeg" || configuration.Width != checked.width || configuration.Height != checked.height {
		return out, ErrInvalid
	}
	// Re-encode the poster in Go as well; no embedded input metadata survives.
	im, _, e := image.Decode(bytes.NewReader(pixels))
	if e != nil {
		return out, ErrInvalid
	}
	var canonicalPoster bytes.Buffer
	if jpeg.Encode(&canonicalPoster, im, &jpeg.Options{Quality: 85}) != nil || canonicalPoster.Len() > MaxDisplayBytes {
		return out, ErrInvalid
	}
	thumb, e := entitycover.Prepare(canonicalPoster.Bytes(), "image/jpeg")
	if e != nil {
		return out, e
	}
	out = VideoDerivative{Derivative: Derivative{Display: canonicalPoster.Bytes(), Thumbnail: thumb.Bytes, Width: checked.width, Height: checked.height, DisplaySHA: DigestBytes(canonicalPoster.Bytes()), ThumbnailSHA: thumb.SHA256}, Video: clip, Contract: VideoContract{MIME: "video/mp4", DurationMS: checked.duration, VideoCodec: checked.videoCodec, AudioCodec: checked.audioCodec, PixelFormat: checked.pixelFormat, SHA256: DigestBytes(clip), Bytes: len(clip)}}
	return out, nil
}
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Keep command programs absolute and fixed; neither JSON nor filenames become
// executable arguments. Platform hooks add a bounded process container.
