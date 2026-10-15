package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// These server-owned bounds apply before allocating decoded pixels. This codec
// is not an upload owner check, a privacy scanner, or an AI/visibility grant.
const (
	MaxImageInputBytes  = 10 * 1024 * 1024
	MaxImageOutputBytes = 16 * 1024 * 1024
	MaxImageDimension   = 8192
	MaxImagePixels      = 16 * 1024 * 1024
)

var (
	ErrImageType    = errors.New("仅支持静态 JPEG 或 PNG 图片")
	ErrImageLimit   = errors.New("图片超过本地处理大小限制")
	ErrImageInvalid = errors.New("图片内容或方向元数据无效")
)

// LocalImageDerivativeBuilder implements the existing codec contract. It has
// no storage/network/provider and cannot publish or approve any image.
type LocalImageDerivativeBuilder struct{}

var _ DerivativeBuilder = (*LocalImageDerivativeBuilder)(nil)

func (*LocalImageDerivativeBuilder) BuildPublic(ctx context.Context, src io.Reader, mime string) (io.ReadCloser, string, error) {
	return buildImageDerivative(ctx, src, mime, MaxImageInputBytes, MaxImagePixels, false)
}

// BuildLegacyPrivateJPEG is reserved for the guarded historical media importer.
// It keeps the full frame and the original orientation correction; it grants no
// public purpose and does not change ordinary upload limits. The importer must
// authenticate the exact backup object before calling it, one image at a time.
func (*LocalImageDerivativeBuilder) BuildLegacyPrivateJPEG(ctx context.Context, src io.Reader) (io.ReadCloser, string, error) {
	return buildImageDerivative(ctx, src, "image/jpeg", 16*1024*1024, 48*1024*1024, true)
}

// BuildLegacyPrivateImage is an offline authenticated JPEG/PNG preservation
// codec. It keeps the full frame and does not enlarge ordinary upload limits.
func (*LocalImageDerivativeBuilder) BuildLegacyPrivateImage(ctx context.Context, src io.Reader, mime string) (io.ReadCloser, string, error) {
	return buildImageDerivative(ctx, src, mime, 16*1024*1024, 48*1024*1024, true)
}

type imageDerivativeFailure struct {
	stage string
	cause error
}

func (e *imageDerivativeFailure) Error() string { return "image_derivative_" + e.stage }
func (e *imageDerivativeFailure) Unwrap() error { return e.cause }

// ImageDerivativeFailureStage returns a closed diagnostic label, never source
// data or an underlying decoder/provider message.
func ImageDerivativeFailureStage(err error) string {
	var e *imageDerivativeFailure
	if errors.As(err, &e) {
		return e.stage
	}
	return "UNCLASSIFIED"
}

func buildImageDerivative(ctx context.Context, src io.Reader, mime string, inputLimit int64, pixelLimit int, diagnostic bool) (result io.ReadCloser, outputMIME string, failure error) {
	stage := "INPUT"
	defer func() {
		if diagnostic && failure != nil {
			failure = &imageDerivativeFailure{stage, failure}
		}
	}()
	if ctx == nil || src == nil {
		return nil, "", ErrImageInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, "", ErrImageType
	}
	data, err := io.ReadAll(io.LimitReader(&imageContextReader{ctx, src}, inputLimit+1))
	defer clear(data)
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > inputLimit {
		return nil, "", ErrImageLimit
	}
	stage = "HEADER"
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrImageInvalid
	}
	if (format == "jpeg" && mime != "image/jpeg") || (format == "png" && mime != "image/png") || (format != "jpeg" && format != "png") {
		return nil, "", ErrImageType
	}
	stage = "DIMENSION_OR_PIXEL_LIMIT"
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxImageDimension || cfg.Height > MaxImageDimension || int64(cfg.Width)*int64(cfg.Height) > int64(pixelLimit) {
		return nil, "", ErrImageLimit
	}
	stage = "ORIENTATION"
	orientation, err := imageOrientationWithPolicy(data, format, diagnostic)
	if err != nil {
		return nil, "", err
	}
	stage = "DECODE"
	decoded, decodedFormat, err := image.Decode(&imageContextReader{ctx, bytes.NewReader(data)})
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", ErrImageInvalid
	}
	if decodedFormat != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return nil, "", ErrImageInvalid
	}
	stage = "ORIENTATION_TRANSFORM"
	pixels, err := orientImage(ctx, decoded, orientation)
	if err != nil {
		return nil, "", err
	}
	stage = "ENCODE"
	out := &imageContextWriter{ctx: ctx}
	if format == "jpeg" {
		err = jpeg.Encode(out, pixels, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(out, pixels)
	}
	if err != nil {
		return nil, "", err
	}
	if err = ctx.Err(); err != nil {
		return nil, "", err
	}
	// Encoders receive pixels, never raw EXIF/XMP/filename/text chunks. Returning
	// a derivative here does not imply the remaining privacy/consent gates passed.
	return io.NopCloser(bytes.NewReader(out.buf.Bytes())), mime, nil
}

type imageContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *imageContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type imageContextWriter struct {
	ctx context.Context
	buf bytes.Buffer
}

func (w *imageContextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > MaxImageOutputBytes-w.buf.Len() {
		return 0, ErrImageLimit
	}
	return w.buf.Write(p)
}

func orientImage(ctx context.Context, src image.Image, orientation int) (image.Image, error) {
	if orientation < 1 || orientation > 8 {
		return nil, ErrImageInvalid
	}
	if orientation == 1 {
		return src, ctx.Err()
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	ow, oh := w, h
	if orientation >= 5 {
		ow, oh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, ow, oh))
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := 0; x < w; x++ {
			xo, yo := x, y
			switch orientation {
			case 2:
				xo = w - 1 - x
			case 3:
				xo, yo = w-1-x, h-1-y
			case 4:
				yo = h - 1 - y
			case 5:
				xo, yo = y, x
			case 6:
				xo, yo = h-1-y, x
			case 7:
				xo, yo = h-1-y, w-1-x
			case 8:
				xo, yo = y, w-1-x
			}
			out.Set(xo, yo, src.At(src.Bounds().Min.X+x, src.Bounds().Min.Y+y))
		}
	}
	return out, nil
}
