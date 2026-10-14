package agentvisual

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"image"
	"image/jpeg"
)

// Only the codec is reused. Human-attachment permissions and stored image IDs
// are never accepted here. The returned full-frame derivative strips metadata.
func DerivePixels(ctx context.Context, in ImageInput) (Pixel, []byte, error) {
	if len(in.ContentBase64) == 0 || len(in.ContentBase64) > 4*((MaxInputBytes+2)/3) {
		return Pixel{}, nil, ErrInvalid
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(in.ContentBase64)
	if e != nil || len(raw) > MaxInputBytes || len(raw) == 0 || base64.StdEncoding.EncodeToString(raw) != in.ContentBase64 || Digest(raw) != in.InputSHA256 {
		return Pixel{}, nil, ErrInvalid
	}
	defer clear(raw)
	if in.MIME == "image/jpeg" && !singleJPEG(raw) {
		return Pixel{}, nil, ErrInvalid
	}
	derivatives, e := (&media.LocalChatImageDerivativeBuilder{}).Build(ctx, raw, in.MIME)
	if e != nil {
		return Pixel{}, nil, ErrInvalid
	}
	defer clear(derivatives.Full)
	defer clear(derivatives.Thumbnail)
	src, _, e := image.Decode(bytes.NewReader(derivatives.Full))
	if e != nil || ctx.Err() != nil {
		return Pixel{}, nil, ErrInvalid
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 || w > 4096 || h > 4096 || int64(w)*int64(h) > 16*1024*1024 {
		return Pixel{}, nil, ErrInvalid
	}
	// Preserve the whole frame. A bounded derivative avoids sending original
	// metadata or making a center crop silently erase the subject of analysis.
	if w > 2048 || h > 2048 {
		ratio := float64(2048) / float64(w)
		if h > w {
			ratio = float64(2048) / float64(h)
		}
		nw, nh := int(float64(w)*ratio), int(float64(h)*ratio)
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
		out := image.NewRGBA(image.Rect(0, 0, nw, nh))
		for y := 0; y < nh; y++ {
			if ctx.Err() != nil {
				return Pixel{}, nil, ErrUnavailable
			}
			for x := 0; x < nw; x++ {
				out.Set(x, y, src.At(b.Min.X+x*w/nw, b.Min.Y+y*h/nh))
			}
		}
		src = out
		w, h = nw, nh
	}
	var encoded bytes.Buffer
	if e = jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 85}); e != nil || encoded.Len() > MaxInputBytes || ctx.Err() != nil {
		return Pixel{}, nil, ErrInvalid
	}
	pixels := encoded.Bytes()
	return Pixel{Revision: 1, SHA256: Digest(pixels), MIME: "image/jpeg", Width: w, Height: h, ByteSize: len(pixels)}, pixels, nil
}

// A JPEG decoder alone may accept a concatenated second frame or trailing
// payload. Walk every marker and entropy scan and require exactly one frame.
func singleJPEG(b []byte) bool {
	if len(b) < 4 || b[0] != 255 || b[1] != 216 {
		return false
	}
	i := 2
	frames := 0
	for i < len(b) {
		if b[i] != 255 {
			return false
		}
		for i < len(b) && b[i] == 255 {
			i++
		}
		if i >= len(b) {
			return false
		}
		marker := b[i]
		i++
		if marker == 217 {
			return frames == 1 && i == len(b)
		}
		if marker == 216 || marker == 0 {
			return false
		}
		if marker == 1 || marker >= 208 && marker <= 215 {
			continue
		}
		if i+2 > len(b) {
			return false
		}
		n := int(b[i])<<8 | int(b[i+1])
		if n < 2 || i+n > len(b) {
			return false
		}
		if marker >= 192 && marker <= 207 && marker != 196 && marker != 200 && marker != 204 {
			frames++
			if frames > 1 {
				return false
			}
		}
		i += n
		if marker != 218 {
			continue
		}
		for i < len(b) {
			if b[i] != 255 {
				i++
				continue
			}
			j := i + 1
			for j < len(b) && b[j] == 255 {
				j++
			}
			if j >= len(b) {
				return false
			}
			m := b[j]
			if m == 0 || m >= 208 && m <= 215 {
				i = j + 1
				continue
			}
			break
		}
	}
	return false
}
