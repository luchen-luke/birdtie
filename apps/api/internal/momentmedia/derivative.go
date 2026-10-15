package momentmedia

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"image"
	"image/jpeg"
	"io"
)

type Derivative struct {
	Display, Thumbnail       []byte
	Width, Height            int
	DisplaySHA, ThumbnailSHA string
}

// Bound aggregate pixel work on the small API host, including concurrent users.
var imageWork = make(chan struct{}, 1)

type Normalizer func(context.Context, io.Reader, string) (io.ReadCloser, string, error)

func Prepare(ctx context.Context, in StageInput, normalize Normalizer) (Derivative, error) {
	var out Derivative
	if normalize == nil || !ID(in.OperationID) || !ID(in.LocalScope) || len(in.Content) < 1 || len(in.Content) > MaxInputBytes || (in.ContentType != "image/jpeg" && in.ContentType != "image/png") {
		return out, ErrInvalid
	}
	select {
	case imageWork <- struct{}{}:
		defer func() { <-imageWork }()
	case <-ctx.Done():
		return out, ctx.Err()
	}
	reader, mime, e := normalize(ctx, bytes.NewReader(in.Content), in.ContentType)
	if e != nil {
		return out, ErrInvalid
	}
	defer reader.Close()
	normalized, e := io.ReadAll(io.LimitReader(reader, 16*1024*1024+1))
	if e != nil || len(normalized) > 16*1024*1024 {
		return out, ErrInvalid
	}
	src, format, e := image.Decode(bytes.NewReader(normalized))
	if e != nil || (mime != "image/jpeg" && mime != "image/png") || (format != "jpeg" && format != "png") {
		return out, ErrInvalid
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > 1920 || h > 1920 {
		if w >= h {
			h = max(1, h*1920/w)
			w = 1920
		} else {
			w = max(1, w*1920/h)
			h = 1920
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	var display bytes.Buffer
	if jpeg.Encode(&display, dst, &jpeg.Options{Quality: 85}) != nil || display.Len() < 1 || display.Len() > MaxDisplayBytes {
		return out, ErrInvalid
	}
	thumb, e := ec.Prepare(display.Bytes(), "image/jpeg")
	if e != nil {
		return out, ErrInvalid
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	out = Derivative{Display: display.Bytes(), Thumbnail: thumb.Bytes, Width: w, Height: h, DisplaySHA: byteHash(display.Bytes()), ThumbnailSHA: thumb.SHA256}
	return out, nil
}
func byteHash(b []byte) string { // the digest is over actual retained bytes
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
