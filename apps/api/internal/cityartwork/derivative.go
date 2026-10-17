package cityartwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
)

// Build checks the archived original digest, strips embedded metadata, and
// retains transparency/aspect ratio. The result is always a bounded PNG.
func Build(ctx context.Context, city, version, inputSHA string, raw []byte) (Content, error) {
	if ctx == nil || ctx.Err() != nil || !ValidCity(city) || !ValidVersion(version) ||
		!ValidVersion(inputSHA) || len(raw) < 1 || len(raw) > 16*1024*1024 {
		return Content{}, ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != inputSHA {
		return Content{}, ErrUnavailable
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "png" && format != "jpeg") || cfg.Width < 1 || cfg.Height < 1 ||
		cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 50*1024*1024 {
		return Content{}, ErrUnavailable
	}
	src, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil || ctx.Err() != nil {
		return Content{}, ErrUnavailable
	}
	w, h := cfg.Width, cfg.Height
	if w > Edge || h > Edge {
		if w >= h {
			h = max(1, h*Edge/w)
			w = Edge
		} else {
			w = max(1, w*Edge/h)
			h = Edge
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	// Area sampling averages premultiplied channels before conversion back to
	// NRGBA, avoiding dark borders around transparent souvenir illustrations.
	for y := 0; y < h; y++ {
		if ctx.Err() != nil {
			return Content{}, ErrUnavailable
		}
		y0, y1 := y*cfg.Height/h, max(y*cfg.Height/h+1, (y+1)*cfg.Height/h)
		for x := 0; x < w; x++ {
			x0, x1 := x*cfg.Width/w, max(x*cfg.Width/w+1, (x+1)*cfg.Width/w)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					rr, gg, bb, aa := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
					r += uint64(rr)
					g += uint64(gg)
					bl += uint64(bb)
					a += uint64(aa)
					n++
				}
			}
			c := color.NRGBA{}
			if a > 0 {
				c.R = uint8(r * 255 / a)
				c.G = uint8(g * 255 / a)
				c.B = uint8(bl * 255 / a)
				c.A = uint8(a / n / 257)
			}
			dst.SetNRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	if png.Encode(&out, dst) != nil || out.Len() < 1 || out.Len() > MaxBytes || ctx.Err() != nil {
		return Content{}, ErrUnavailable
	}
	body := out.Bytes()
	hash := sha256.Sum256(body)
	return Content{Descriptor: Descriptor{CityID: city, Version: version, ContentPath: ContentPath(city, version), ContentType: "image/png", Width: w, Height: h, ByteCount: len(body), SHA256: hex.EncodeToString(hash[:])}, Bytes: body}, nil
}
