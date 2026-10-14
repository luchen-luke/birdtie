package entitycover

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
)

const Width = 320
const Height = 180
const MaxInputBytes = 5 * 1024 * 1024
const MaxThumbnailBytes = 256 * 1024

type Derivative struct {
	Bytes  []byte
	SHA256 string
}

// Prepare decodes only bounded, static PNG/JPEG and retains one 320x180 JPEG.
// Original full-size pixels, EXIF, text chunks and animation are never stored.
func Prepare(raw []byte, contentType string) (Derivative, error) {
	if len(raw) == 0 || len(raw) > MaxInputBytes || (contentType != "image/jpeg" && contentType != "image/png") {
		return Derivative{}, ErrInvalid
	}
	conf, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || conf.Width < 1 || conf.Height < 1 || conf.Width > 4096 || conf.Height > 4096 || int64(conf.Width)*int64(conf.Height) > 16*1024*1024 || contentType != "image/"+format {
		return Derivative{}, ErrInvalid
	}
	if format == "png" {
		for at := 8; at+12 <= len(raw); {
			n := int(binary.BigEndian.Uint32(raw[at : at+4]))
			if n > len(raw)-at-12 {
				return Derivative{}, ErrInvalid
			}
			kind := string(raw[at+4 : at+8])
			if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
				return Derivative{}, ErrInvalid
			}
			at += n + 12
		}
	}
	src, actual, e := image.Decode(bytes.NewReader(raw))
	if e != nil || actual != format {
		return Derivative{}, ErrInvalid
	}
	b := src.Bounds()
	cropW, cropH := b.Dx(), b.Dy()
	if cropW*Height > cropH*Width {
		cropW = cropH * Width / Height
	} else {
		cropH = cropW * Height / Width
	}
	if cropW < 1 {
		cropW = 1
	}
	if cropH < 1 {
		cropH = 1
	}
	x0, y0 := b.Min.X+(b.Dx()-cropW)/2, b.Min.Y+(b.Dy()-cropH)/2
	dst := image.NewRGBA(image.Rect(0, 0, Width, Height))
	for y := 0; y < Height; y++ {
		for x := 0; x < Width; x++ {
			r, g, blue, a := src.At(x0+(2*x+1)*cropW/(2*Width), y0+(2*y+1)*cropH/(2*Height)).RGBA()
			dst.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - a) >> 8), uint8((g + 65535 - a) >> 8), uint8((blue + 65535 - a) >> 8), 255})
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}) != nil || out.Len() > MaxThumbnailBytes {
		return Derivative{}, ErrInvalid
	}
	h := sha256.Sum256(out.Bytes())
	return Derivative{Bytes: out.Bytes(), SHA256: hex.EncodeToString(h[:])}, nil
}
