package entitycover

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), 80, 255})
		}
	}
	var b bytes.Buffer
	if png.Encode(&b, img) != nil {
		t.Fatal("PNG fixture")
	}
	return b.Bytes()
}
func TestEntityCoverDerivativeActualWideJPEGAndBounds(t *testing.T) {
	raw := coverPNG(t)
	d, e := Prepare(raw, "image/png")
	if e != nil {
		t.Fatal(e)
	}
	conf, format, e := image.DecodeConfig(bytes.NewReader(d.Bytes))
	sum := sha256.Sum256(d.Bytes)
	if e != nil || format != "jpeg" || conf.Width != 320 || conf.Height != 180 || len(d.Bytes) > MaxThumbnailBytes || d.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("actual canonical derivative")
	}
	again, e := Prepare(raw, "image/png")
	if e != nil || !bytes.Equal(d.Bytes, again.Bytes) {
		t.Fatal("preview/commit differ")
	}
	for _, bad := range []struct {
		b    []byte
		mime string
	}{{raw, "image/jpeg"}, {nil, "image/png"}, {bytes.Repeat([]byte{1}, MaxInputBytes+1), "image/jpeg"}, {[]byte("not a picture"), "image/png"}} {
		if _, e = Prepare(bad.b, bad.mime); e == nil {
			t.Fatal("unbounded or mismatched input")
		}
	}
	animated := append([]byte{}, raw[:8]...)
	animated = append(animated, []byte{0, 0, 0, 0, 'a', 'c', 'T', 'L', 0, 0, 0, 0}...)
	animated = append(animated, raw[8:]...)
	if _, e = Prepare(animated, "image/png"); e == nil {
		t.Fatal("animation accepted")
	}
}
