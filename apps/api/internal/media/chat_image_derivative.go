package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"image"
	"image/png"
	"io"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
)

// Pure codec only: no upload ownership, model processing, publication or store.
type LocalChatImageDerivativeBuilder struct{}
type ChatImageDerivatives struct {
	Metadata  connection.ImageMetadata
	Full      []byte
	Thumbnail []byte
}

func chatStaticPNG(raw []byte) error {
	if len(raw) < 8 || !bytes.Equal(raw[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return ErrImageInvalid
	}
	idat := false
	for p, chunks := 8, 0; ; chunks++ {
		if chunks >= 4096 || len(raw)-p < 12 {
			return ErrImageInvalid
		}
		n := uint64(binary.BigEndian.Uint32(raw[p : p+4]))
		if n > uint64(len(raw)-p-12) {
			return ErrImageInvalid
		}
		end := p + 12 + int(n)
		kind := string(raw[p+4 : p+8])
		if chunks == 0 && (kind != "IHDR" || n != 13) {
			return ErrImageInvalid
		}
		if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
			return ErrImageType
		}
		if crc32.ChecksumIEEE(raw[p+4:end-4]) != binary.BigEndian.Uint32(raw[end-4:end]) {
			return ErrImageInvalid
		}
		if kind == "IDAT" {
			idat = true
		}
		if kind == "IEND" {
			if n != 0 || end != len(raw) || !idat {
				return ErrImageInvalid
			}
			return nil
		}
		p = end
	}
}

func ValidateChatImageDerivative(ctx context.Context, raw []byte, mime string, w, h int) error {
	if ctx == nil || len(raw) < 1 || len(raw) > MaxImageOutputBytes {
		return ErrImageLimit
	}
	if mime == "image/png" {
		if e := chatStaticPNG(raw); e != nil {
			return e
		}
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil {
		return ErrImageInvalid
	}
	if !((format == "png" && mime == "image/png") || (format == "jpeg" && mime == "image/jpeg")) {
		return ErrImageType
	}
	if cfg.Width != w || cfg.Height != h || w < 1 || h < 1 || w > MaxImageDimension || h > MaxImageDimension || int64(w)*int64(h) > MaxImagePixels {
		return ErrImageLimit
	}
	decoded, actual, e := image.Decode(&imageContextReader{ctx, bytes.NewReader(raw)})
	if e != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrImageInvalid
	}
	if actual != format || decoded.Bounds().Dx() != w || decoded.Bounds().Dy() != h {
		return ErrImageInvalid
	}
	return ctx.Err()
}

func (*LocalChatImageDerivativeBuilder) Build(ctx context.Context, raw []byte, mime string) (ChatImageDerivatives, error) {
	var out ChatImageDerivatives
	if ctx == nil || len(raw) < 1 || len(raw) > MaxImageInputBytes {
		return out, ErrImageLimit
	}
	if mime == "image/png" {
		if e := chatStaticPNG(raw); e != nil {
			return out, e
		}
	}
	full, actual, e := (&LocalImageDerivativeBuilder{}).BuildPublic(ctx, bytes.NewReader(raw), mime)
	if e != nil {
		return out, e
	}
	defer full.Close()
	out.Full, e = io.ReadAll(io.LimitReader(&imageContextReader{ctx, full}, MaxImageOutputBytes+1))
	if e != nil {
		return ChatImageDerivatives{}, e
	}
	if len(out.Full) < 1 || len(out.Full) > MaxImageOutputBytes {
		return ChatImageDerivatives{}, ErrImageLimit
	}
	pixels, format, e := image.Decode(&imageContextReader{ctx, bytes.NewReader(out.Full)})
	if e != nil {
		return ChatImageDerivatives{}, e
	}
	if !((format == "png" && actual == "image/png") || (format == "jpeg" && actual == "image/jpeg")) {
		return ChatImageDerivatives{}, ErrImageType
	}
	w, h := pixels.Bounds().Dx(), pixels.Bounds().Dy()
	tw, th := connection.ImageThumbnailDimensions(w, h)
	thumb := image.NewNRGBA(image.Rect(0, 0, tw, th))
	for y := 0; y < th; y++ {
		if e = ctx.Err(); e != nil {
			return ChatImageDerivatives{}, e
		}
		for x := 0; x < tw; x++ {
			xp, yp := min(w-1, (2*x+1)*w/(2*tw)), min(h-1, (2*y+1)*h/(2*th))
			thumb.Set(x, y, pixels.At(pixels.Bounds().Min.X+xp, pixels.Bounds().Min.Y+yp))
		}
	}
	encoded := &chatThumbnailWriter{ctx: ctx}
	if e = png.Encode(encoded, thumb); e != nil {
		return ChatImageDerivatives{}, e
	}
	out.Thumbnail = encoded.buf.Bytes()
	meta := func(content []byte, mime string, w, h int) connection.ImageDerivativeMetadata {
		sum := sha256.Sum256(content)
		return connection.ImageDerivativeMetadata{MIME: mime, ByteSize: int64(len(content)), SHA256: hex.EncodeToString(sum[:]), Width: w, Height: h}
	}
	out.Metadata = connection.ImageMetadata{Schema: connection.ConversationImageSchema, ImageDerivativeMetadata: meta(out.Full, actual, w, h), Thumbnail: meta(out.Thumbnail, "image/png", tw, th)}
	if !connection.ValidImageMetadata(out.Metadata) {
		return ChatImageDerivatives{}, ErrImageInvalid
	}
	if e = ctx.Err(); e != nil {
		return ChatImageDerivatives{}, e
	}
	return out, nil
}

type chatThumbnailWriter struct {
	ctx context.Context
	buf bytes.Buffer
}

func (w *chatThumbnailWriter) Write(p []byte) (int, error) {
	if e := w.ctx.Err(); e != nil {
		return 0, e
	}
	if len(p) > connection.MaxChatImageThumbnailBytes-w.buf.Len() {
		return 0, ErrImageLimit
	}
	return w.buf.Write(p)
}
