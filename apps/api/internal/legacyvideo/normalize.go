package legacyvideo

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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type diagnosticError struct {
	stage string
	cause error
}

func (e *diagnosticError) Error() string { return "legacy_private_video_codec_" + e.stage }
func (e *diagnosticError) Unwrap() error { return e.cause }

type processError struct {
	code string
	exit int
}

func (e *processError) Error() string { return "private_video_process_failed" }
func FailureStage(e error) string {
	parts := []string{}
	for e != nil {
		switch v := e.(type) {
		case *diagnosticError:
			parts = append(parts, v.stage)
		case *processError:
			parts = append(parts, v.code+"_EXIT_"+strconv.Itoa(v.exit))
		}
		e = errors.Unwrap(e)
	}
	if len(parts) == 0 {
		return "INPUT"
	}
	return strings.Join(parts, "_")
}

type Normalizer struct{ FFmpegPath, FFprobePath, PrlimitPath, TempRoot string }
type Derivative struct {
	AudioPackets                            int64
	AudioPacketSHA256                       string
	Content, Poster                         []byte
	Width, Height                           int
	DurationMS, Frames                      int64
	AudioCodec, ContentSHA256, PosterSHA256 string
}

func (d *Derivative) Erase() { clear(d.Content); clear(d.Poster); d.Content = nil; d.Poster = nil }
func (n *Normalizer) Validate() error {
	if n == nil {
		return ErrUnavailable
	}
	for _, p := range []string{n.FFmpegPath, n.FFprobePath} {
		v, e := os.Stat(p)
		if !filepath.IsAbs(p) || e != nil || !v.Mode().IsRegular() {
			return ErrUnavailable
		}
	}
	if n.TempRoot != "" && !filepath.IsAbs(n.TempRoot) {
		return ErrUnavailable
	}
	return validatePlatform(n)
}

type boundedOutput struct {
	bytes.Buffer
	max int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(p)
}
func (n *Normalizer) run(ctx context.Context, program string, args ...string) ([]byte, error) {
	cmd, e := platformCommand(ctx, n, program, args)
	if e != nil {
		return nil, e
	}
	limit := 65536
	for _, arg := range args {
		if arg == "-show_packets" {
			limit = 1024 * 1024
		}
	}
	out, stderr := &boundedOutput{max: limit}, &boundedOutput{max: 65536}
	cmd.Stdout, cmd.Stderr = out, stderr
	if cmd.Start() != nil {
		return nil, ErrInvalid
	}
	stop, e := platformStarted(cmd)
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, e
	}
	var stopped sync.Once
	release := func() { stopped.Do(stop) }
	defer release()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var waitErr error
waiting:
	for {
		select {
		case waitErr = <-done:
			break waiting
		case <-ticker.C:
			// A completed Wait wins over a concurrent supervision tick. Actual
			// over-limit samples below still abort; their result is not discarded.
			select {
			case waitErr = <-done:
				break waiting
			default:
			}
			if platformMemoryExceeded(cmd.Process.Pid) {
				_ = cmd.Process.Kill()
				release()
				<-done
				return nil, &processError{"MEMORY_BOUND", -1}
			}
			if program == n.FFmpegPath && len(args) > 0 && filepath.IsAbs(args[len(args)-1]) {
				if st, e := os.Stat(args[len(args)-1]); e == nil && st.Size() > MaxContentBytes+65536 {
					_ = cmd.Process.Kill()
					release()
					<-done
					return nil, &processError{"OUTPUT_BOUND", -1}
				}
			}
		}
	}
	if e := waitErr; e != nil {
		code := "PROCESS_FAILED"
		msg := strings.ToLower(stderr.String())
		for _, entry := range []struct{ needle, code string }{{"non monotonically", "TIMESTAMP"}, {"invalid argument", "INVALID_ARGUMENT"}, {"cannot allocate memory", "MEMORY"}, {"malloc", "MEMORY"}, {"not divisible", "DIMENSION"}, {"no space left", "STORAGE"}, {"file too large", "OUTPUT_BOUND"}, {"permission denied", "PERMISSION"}, {"invalid data", "DECODE"}, {"error while opening encoder", "ENCODER"}, {"error submitting", "SUBMIT_FRAME"}, {"incompatible", "INCOMPATIBLE"}, {"av_interleaved_write_frame", "MUX_WRITE"}} {
			if strings.Contains(msg, entry.needle) {
				code = entry.code
				break
			}
		}
		for _, needle := range []string{"tag ", "codec not currently", "timestamps are unset", "non-monotonous", "unsupported", "sar", "frame rate", "pixel format", "bitstream", "sample rate", "invalid pts", "invalid dts", "height", "width", "padding", "muxer", "demuxer", "avc1", "hvc1", "display", "layout", "changed", "reinitializing", "filter", "autorotate", "rotation", "transcoding", "unknown", "flushing"} {
			if strings.Contains(msg, needle) {
				code += "_" + strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(needle), " ", "_"))
			}
		}
		// Diagnostic vocabulary is fixed: paths, names, hashes, numeric values and
		// any unrecognised source text are erased before the aggregate leaves RAM.
		vocabulary := map[string]bool{}
		for _, w := range strings.Fields("error failed initializing opening setting processing invalid argument not supported support unsupported could write header codec parameters timebase time_base numerator denominator time base too large small maximum minimum rate frame sample audio video output input for is are the a an and or of to from with in on while value option format muxer filter encoder libx264 h264 aac positive negative integer number overflow requires divisible by width height rational pts dts inconsistent cannot use same as zero denominator parameter parsing unable dimensions") {
			vocabulary[w] = true
		}
		diag := []string{}
		for _, line := range strings.Split(msg, "\n") {
			if !strings.Contains(line, "error") && !strings.Contains(line, "invalid") && !strings.Contains(line, "support") && !strings.Contains(line, "timebase") {
				continue
			}
			for _, w := range strings.Fields(line) {
				w = strings.Trim(w, "[]():,.;?!\"'")
				if vocabulary[w] {
					diag = append(diag, w)
				} else {
					diag = append(diag, "X")
				}
				if len(diag) >= 120 {
					break
				}
			}
			if len(diag) >= 120 {
				break
			}
		}
		code += "_WORDS_" + strings.Join(diag, "-")
		exit := -1
		var ee *exec.ExitError
		if errors.As(e, &ee) {
			exit = ee.ExitCode()
		}
		return nil, &processError{code, exit}
	}
	return append([]byte(nil), out.Bytes()...), nil
}

var _ *exec.Cmd
var inputs = []string{"-v", "error", "-max_alloc", "33554432", "-protocol_whitelist", "file,pipe", "-enable_drefs", "0", "-use_absolute_path", "0", "-max_streams", "2"}

type descriptor struct {
	TimeBase               string
	Rotation               int
	Width, Height          int
	DurationMS             int64
	VideoCodec, AudioCodec string
	FPS                    float64
}

func probe(raw []byte, canonical bool) (descriptor, error) {
	var p struct {
		Streams []struct {
			Type     string            `json:"codec_type"`
			Codec    string            `json:"codec_name"`
			Format   string            `json:"pix_fmt"`
			Width    int               `json:"width"`
			Height   int               `json:"height"`
			Rate     string            `json:"avg_frame_rate"`
			TimeBase string            `json:"time_base"`
			Tags     map[string]string `json:"tags"`
			Side     []map[string]any  `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	var d descriptor
	if len(raw) > 65536 || json.Unmarshal(raw, &p) != nil || len(p.Streams) < 1 || len(p.Streams) > 2 {
		return d, ErrInvalid
	}
	seconds, e := strconv.ParseFloat(p.Format.Duration, 64)
	if e != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > float64(MaxDurationMS)/1000 {
		return d, ErrInvalid
	}
	d.DurationMS = int64(math.Round(seconds * 1000))
	d.AudioCodec = "none"
	vc, ac := 0, 0
	for _, s := range p.Streams {
		switch s.Type {
		case "video":
			vc++
			d.Width, d.Height, d.VideoCodec = s.Width, s.Height, s.Codec
			if s.Width < 2 || s.Height < 2 || s.Width > 1920 || s.Height > 1920 || s.Width*s.Height > 2073600 {
				return d, ErrInvalid
			}
			parts := strings.Split(s.Rate, "/")
			if len(parts) != 2 {
				return d, ErrInvalid
			}
			a, e1 := strconv.ParseFloat(parts[0], 64)
			b, e2 := strconv.ParseFloat(parts[1], 64)
			if e1 != nil || e2 != nil || b <= 0 || math.IsNaN(a) || math.IsInf(a, 0) || math.IsNaN(b) || math.IsInf(b, 0) || a/b <= 0 || a/b > 60 {
				return d, ErrInvalid
			}
			d.FPS = a / b
			timeParts := strings.Split(s.TimeBase, "/")
			if len(timeParts) != 2 {
				return d, ErrInvalid
			}
			num, ne := strconv.ParseInt(timeParts[0], 10, 32)
			den, de := strconv.ParseInt(timeParts[1], 10, 32)
			if ne != nil || de != nil || num < 1 || num > 1000000 || den < 1 || den > 1000000000 {
				return d, ErrInvalid
			}
			d.TimeBase = strconv.FormatInt(num, 10) + ":" + strconv.FormatInt(den, 10)
			if canonical && (s.Codec != "h264" || (s.Format != "yuv420p" && s.Format != "yuvj420p") || s.Width%2 != 0 || s.Height%2 != 0) {
				return d, &diagnosticError{"CODEC_PIXEL_FORMAT", ErrInvalid}
			}
			for _, side := range s.Side {
				if side["side_data_type"] != "Display Matrix" {
					return d, &diagnosticError{"SIDE_DATA", ErrInvalid}
				}
				rotation, ok := side["rotation"].(float64)
				if !ok || math.IsNaN(rotation) || math.IsInf(rotation, 0) || rotation != math.Trunc(rotation) || int(rotation)%90 != 0 {
					return d, ErrInvalid
				}
				if canonical && rotation != 0 {
					return d, &diagnosticError{"SIDE_ROTATION", ErrInvalid}
				}
				d.Rotation = int(rotation)
			}
		case "audio":
			ac++
			d.AudioCodec = s.Codec
			if s.Codec != "aac" {
				return d, ErrInvalid
			}
		default:
			return d, ErrInvalid
		}
		if canonical {
			for k := range s.Tags {
				if k != "language" && k != "handler_name" && k != "vendor_id" && k != "encoder" {
					return d, ErrInvalid
				}
			}
		}
	}
	if vc != 1 || ac > 1 {
		return d, ErrInvalid
	}
	if canonical {
		for k := range p.Format.Tags {
			if k != "major_brand" && k != "minor_version" && k != "compatible_brands" && k != "encoder" {
				return d, ErrInvalid
			}
		}
	}
	return d, nil
}
func progress(raw []byte) (int64, int64, error) {
	var frames, us int64
	ended := false
	for _, line := range strings.Split(string(raw), "\n") {
		kv := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "frame", "out_time_us":
			v, e := strconv.ParseInt(kv[1], 10, 64)
			if e != nil || v < 0 {
				return 0, 0, ErrInvalid
			}
			if kv[0] == "frame" {
				if v > 7200 {
					return 0, 0, ErrInvalid
				}
				if v > frames {
					frames = v
				}
			} else {
				if v > int64(MaxDurationMS+250)*1000 {
					return 0, 0, ErrInvalid
				}
				if v > us {
					us = v
				}
			}
		case "progress":
			if kv[1] == "end" {
				ended = true
			}
		}
	}
	if !ended || frames < 1 || us < 1 {
		return 0, 0, ErrInvalid
	}
	return frames, us / 1000, nil
}
func (n *Normalizer) fullDecode(ctx context.Context, path string) (int64, int64, error) {
	a := append(append([]string{}, inputs...), "-nostdin", "-xerror", "-err_detect", "explode", "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-i", path, "-map", "0:v:0", "-map", "0:a?", "-threads", "1", "-fps_mode", "passthrough", "-progress", "pipe:1", "-nostats", "-f", "null", "-")
	raw, e := n.run(ctx, n.FFmpegPath, a...)
	if e != nil {
		return 0, 0, e
	}
	return progress(raw)
}

// AAC packets are copied, never resampled. Compare every packet payload in
// order as well as the complete decode, rather than inferring audio from length.
func (n *Normalizer) audioProof(ctx context.Context, path string) (int64, string, error) {
	args := append(append([]string{}, inputs...), "-select_streams", "a:0", "-show_packets", "-show_entries", "packet=data_hash", "-show_data_hash", "sha256", "-of", "json", path)
	raw, e := n.run(ctx, n.FFprobePath, args...)
	if e != nil {
		return 0, "", e
	}
	defer clear(raw)
	var document struct {
		Packets []struct {
			Hash string `json:"data_hash"`
		} `json:"packets"`
	}
	if json.Unmarshal(raw, &document) != nil {
		return 0, "", ErrInvalid
	}
	hashes := make([]string, 0, len(document.Packets))
	for _, packet := range document.Packets {
		if !strings.HasPrefix(packet.Hash, "SHA256:") || !ValidSHA(strings.TrimPrefix(packet.Hash, "SHA256:")) {
			return 0, "", ErrInvalid
		}
		hashes = append(hashes, packet.Hash)
	}
	if len(hashes) < 1 || len(hashes) > 10000 {
		return 0, "", ErrInvalid
	}
	return int64(len(hashes)), Hash([]byte(strings.Join(hashes, "\n"))), nil
}
func readBounded(path string, max int) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if e != nil || len(b) < 1 || len(b) > max {
		clear(b)
		return nil, ErrInvalid
	}
	return b, nil
}
func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// The authenticated original is kept separately and never served. This playback
// derivative retains the complete timeline, frame cadence, visual field and AAC
// audio; metadata is stripped and display rotation is baked by the decoder.
func (n *Normalizer) Normalize(ctx context.Context, raw []byte) (out Derivative, err error) {
	stage := "INPUT"
	defer func() {
		if err != nil {
			err = &diagnosticError{stage, err}
		}
	}()
	if n.Validate() != nil {
		return out, ErrUnavailable
	}
	if len(raw) < 24 || len(raw) > MaxInputBytes || string(raw[4:8]) != "ftyp" {
		return out, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	dir, e := os.MkdirTemp(n.TempRoot, "birdtie-private-video-")
	if e != nil {
		return out, ErrUnavailable
	}
	base := n.TempRoot
	if base == "" {
		base = os.TempDir()
	}
	rel, e := filepath.Rel(base, dir)
	if e != nil || rel == "." || filepath.Dir(rel) != "." || filepath.IsAbs(rel) {
		return out, ErrUnavailable
	}
	defer os.RemoveAll(dir)
	if os.Chmod(dir, 0700) != nil {
		return out, ErrUnavailable
	}
	original, canonical, poster := filepath.Join(dir, "source.mp4"), filepath.Join(dir, "playback.mp4"), filepath.Join(dir, "poster.jpg")
	if os.WriteFile(original, raw, 0600) != nil {
		return out, ErrUnavailable
	}
	for _, p := range []string{canonical, poster} {
		if os.WriteFile(p, nil, 0600) != nil {
			return out, ErrUnavailable
		}
	}
	defer func() {
		if err != nil {
			out.Erase()
		}
	}()
	a := append(append([]string{}, inputs...), "-show_streams", "-show_format", "-of", "json", original)
	stage = "SOURCE_PROBE"
	metadata, e := n.run(ctx, n.FFprobePath, a...)
	if e != nil {
		return out, e
	}
	stage = "SOURCE_DESCRIPTOR"
	source, e := probe(metadata, false)
	if e != nil {
		return out, e
	}
	stage = "SOURCE_FULL_DECODE"
	frames, duration, e := n.fullDecode(ctx, original)
	if e != nil {
		return out, e
	}
	if abs(duration-source.DurationMS) > 250 {
		return out, &diagnosticError{"DURATION", ErrInvalid}
	}
	var audioPackets int64
	var audioHash string
	if source.AudioCodec != "none" {
		stage = "SOURCE_AUDIO_PACKETS"
		audioPackets, audioHash, e = n.audioProof(ctx, original)
		if e != nil {
			return out, e
		}
	}
	// No trimming, cropping, scaling, fixed-rate conversion or file-size stop.
	a = append(append([]string{}, inputs...), "-nostdin", "-y", "-xerror", "-err_detect", "explode", "-threads", "1", "-filter_threads", "1", "-i", original, "-map", "0:v:0", "-map", "0:a?", "-map_metadata", "-1", "-map_chapters", "-1", "-fps_mode", "passthrough", "-enc_time_base:v", source.TimeBase, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-maxrate", "3500k", "-bufsize", "7000k", "-pix_fmt", "yuv420p", "-threads", "1", "-c:a", "copy", "-metadata:s:v:0", "rotate=0", "-movflags", "+faststart", "-fflags", "+bitexact", "-flags:v", "+bitexact", "-f", "mp4", canonical)
	stage = "CANONICAL_ENCODE"
	if _, e = n.run(ctx, n.FFmpegPath, a...); e != nil {
		return out, e
	}
	stage = "CANONICAL_PROBE"
	metadata, e = n.run(ctx, n.FFprobePath, append(append([]string{}, inputs...), "-show_streams", "-show_format", "-of", "json", canonical)...)
	if e != nil {
		return out, e
	}
	stage = "CANONICAL_DESCRIPTOR"
	checked, e := probe(metadata, true)
	if e != nil {
		return out, e
	}
	expectedW, expectedH := source.Width, source.Height
	if source.Rotation%180 != 0 {
		expectedW, expectedH = source.Height, source.Width
	}
	if checked.Width != expectedW || checked.Height != expectedH {
		return out, &diagnosticError{"PIXEL_FIELD", ErrInvalid}
	}
	if checked.AudioCodec != source.AudioCodec {
		return out, &diagnosticError{"AUDIO", ErrInvalid}
	}
	if abs(checked.DurationMS-source.DurationMS) > 250 {
		return out, &diagnosticError{"DURATION", ErrInvalid}
	}
	if math.Abs(checked.FPS-source.FPS) > .01 {
		return out, &diagnosticError{"FPS", ErrInvalid}
	}
	stage = "CANONICAL_FULL_DECODE"
	newFrames, newDuration, e := n.fullDecode(ctx, canonical)
	if e != nil {
		return out, e
	}
	if newFrames != frames {
		return out, &diagnosticError{"FRAME_COUNT", ErrInvalid}
	}
	if abs(newDuration-duration) > 250 {
		return out, &diagnosticError{"DURATION", ErrInvalid}
	}
	if source.AudioCodec != "none" {
		stage = "CANONICAL_AUDIO_PACKETS"
		count, hash, err := n.audioProof(ctx, canonical)
		if err != nil {
			return out, err
		}
		if count != audioPackets {
			return out, &diagnosticError{"COUNT", ErrInvalid}
		}
		if hash != audioHash {
			return out, &diagnosticError{"PAYLOAD", ErrInvalid}
		}
	}
	stage = "POSTER"
	a = append(append([]string{}, inputs...), "-nostdin", "-y", "-threads", "1", "-i", canonical, "-map", "0:v:0", "-frames:v", "1", "-q:v", "2", "-threads", "1", poster)
	if _, e = n.run(ctx, n.FFmpegPath, a...); e != nil {
		return out, e
	}
	pixels, e := readBounded(poster, MaxPosterBytes)
	if e != nil {
		return out, e
	}
	defer clear(pixels)
	im, format, e := image.Decode(bytes.NewReader(pixels))
	if e != nil || format != "jpeg" || im.Bounds().Dx() != checked.Width || im.Bounds().Dy() != checked.Height {
		return out, ErrInvalid
	}
	var encoded bytes.Buffer
	if jpeg.Encode(&encoded, im, &jpeg.Options{Quality: 85}) != nil || encoded.Len() > MaxPosterBytes {
		return out, ErrInvalid
	}
	content, e := readBounded(canonical, MaxContentBytes)
	if e != nil {
		return out, e
	}
	out = Derivative{AudioPackets: audioPackets, AudioPacketSHA256: audioHash, Content: content, Poster: encoded.Bytes(), Width: checked.Width, Height: checked.Height, DurationMS: checked.DurationMS, Frames: newFrames, AudioCodec: checked.AudioCodec, ContentSHA256: Hash(content), PosterSHA256: Hash(encoded.Bytes())}
	return out, nil
}
