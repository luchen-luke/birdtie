package legacyimagerecovery

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

// Only the guarded offline importer can invoke this decoder. No URL, network,
// ordinary upload type or public-media permission is added.
type Normalizer struct{ HEIFConvertPath, PrlimitPath, TempRoot string }
type Derivative struct {
	Bytes          []byte
	Width, Height  int
	ValidationKind string
}

func (d *Derivative) Erase() { clear(d.Bytes); d.Bytes = nil }

type codecError struct{ stage string }

func (e *codecError) Error() string { return "legacy_image_recovery_codec_" + e.stage }
func FailureStage(e error) string {
	var v *codecError
	if errors.As(e, &v) {
		return v.stage
	}
	return "INPUT"
}
func (n *Normalizer) Validate() error {
	if n == nil || !filepath.IsAbs(n.HEIFConvertPath) || n.TempRoot != "" && !filepath.IsAbs(n.TempRoot) {
		return ErrUnavailable
	}
	st, e := os.Stat(n.HEIFConvertPath)
	if e != nil || !st.Mode().IsRegular() {
		return ErrUnavailable
	}
	return validatePlatform(n)
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(p)
}

type jpegOutput struct{ bytes.Buffer }

func (b *jpegOutput) Write(p []byte) (int, error) {
	if len(p) > media.MaxImageOutputBytes-b.Len() {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(p)
}
func (n *Normalizer) run(ctx context.Context, input, output string) error {
	cmd, e := platformCommand(ctx, n, n.HEIFConvertPath, []string{input, output})
	if e != nil {
		return e
	}
	stdout, stderr := &boundedOutput{}, &boundedOutput{}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if cmd.Start() != nil {
		return &codecError{"PROCESS_START"}
	}
	stop, e := platformStarted(cmd)
	if e != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return e
	}
	var once sync.Once
	release := func() { once.Do(stop) }
	defer release()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case e = <-done:
			if e != nil || stderr.Len() != 0 {
				return &codecError{"FULL_HEIC_DECODE"}
			}
			return nil
		case <-ticker.C:
			select {
			case e = <-done:
				if e != nil || stderr.Len() != 0 {
					return &codecError{"FULL_HEIC_DECODE"}
				}
				return nil
			default:
			}
			exceeded := platformMemoryExceeded(cmd.Process.Pid)
			if files, se := os.ReadDir(filepath.Dir(output)); se != nil || len(files) > 2 {
				exceeded = true
			}
			if st, se := os.Stat(output); se == nil && st.Size() > 64*1024*1024 {
				exceeded = true
			}
			if exceeded {
				_ = cmd.Process.Kill()
				release()
				<-done
				return &codecError{"RESOURCE_BOUND"}
			}
		}
	}
}
func (n *Normalizer) Normalize(ctx context.Context, r Record, raw []byte) (out Derivative, err error) {
	if ctx == nil || ctx.Err() != nil || len(raw) < 3 || len(raw) > MaxInputBytes || int64(len(raw)) != r.InputBytes || legacymedia.Hash(raw) != r.InputSHA256 {
		return out, ErrInvalid
	}
	defer func() {
		if err != nil {
			out.Erase()
		}
	}()
	if r.Format == "image/jpeg" && r.RecoveryReason == JPEGUnknown {
		result, mime, e := (&media.LocalImageDerivativeBuilder{}).BuildLegacyPrivateJPEG(ctx, bytes.NewReader(raw))
		if e != nil {
			return out, &codecError{"JPEG_FULL_DECODE_REENCODE"}
		}
		defer result.Close()
		out.Bytes, e = io.ReadAll(io.LimitReader(result, media.MaxImageOutputBytes+1))
		if e != nil || mime != "image/jpeg" || len(out.Bytes) == 0 || len(out.Bytes) > media.MaxImageOutputBytes {
			return out, ErrInvalid
		}
		cfg, _, e := image.DecodeConfig(bytes.NewReader(out.Bytes))
		if e != nil {
			return out, ErrInvalid
		}
		out.Width, out.Height = cfg.Width, cfg.Height
		out.ValidationKind = "STATIC_IMAGE_DECODE_REENCODE_V1"
		return out, nil
	}
	if r.Format != "image/heic" || r.RecoveryReason != HEICMisclassified || len(raw) < 24 || string(raw[4:8]) != "ftyp" || string(raw[8:12]) != "heic" || n.Validate() != nil {
		return out, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	dir, e := os.MkdirTemp(n.TempRoot, "birdtie-private-heic-")
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
	input, output := filepath.Join(dir, "source.heic"), filepath.Join(dir, "decoded.png")
	if os.WriteFile(input, raw, 0600) != nil || os.WriteFile(output, nil, 0600) != nil {
		return out, ErrUnavailable
	}
	if e = n.run(ctx, input, output); e != nil {
		return out, e
	}
	files, e := os.ReadDir(dir)
	if e != nil || len(files) != 2 {
		return out, &codecError{"SINGLE_STILL_REQUIRED"}
	}
	st, e := os.Lstat(output)
	if e != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 64*1024*1024 {
		return out, &codecError{"PNG_OUTPUT_BOUND"}
	}
	encoded, e := os.ReadFile(output)
	if e != nil {
		return out, ErrUnavailable
	}
	defer clear(encoded)
	cfg, format, e := image.DecodeConfig(bytes.NewReader(encoded))
	if e != nil || format != "png" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 48*1024*1024 {
		return out, &codecError{"DIMENSION_BOUND"}
	}
	pixels, format, e := image.Decode(bytes.NewReader(encoded))
	if e != nil || format != "png" || pixels.Bounds().Dx() != cfg.Width || pixels.Bounds().Dy() != cfg.Height {
		return out, &codecError{"FULL_PNG_DECODE"}
	}
	// libheif has already applied the HEIC primary image's transforms. Encode
	// pixels only, ignoring EXIF/XMP, so orientation cannot be applied twice.
	var buf jpegOutput
	if e = jpeg.Encode(&buf, pixels, &jpeg.Options{Quality: 85}); e != nil || buf.Len() < 1 || buf.Len() > media.MaxImageOutputBytes || ctx.Err() != nil {
		return out, &codecError{"JPEG_OUTPUT_BOUND"}
	}
	out.Bytes = append([]byte(nil), buf.Bytes()...)
	out.Width, out.Height = cfg.Width, cfg.Height
	out.ValidationKind = "HEIC_FULL_DECODE_PNG_JPEG_REENCODE_V1"
	return out, nil
}
