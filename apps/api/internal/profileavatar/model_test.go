package profileavatar

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestPersonAvatarPrepareBoundedReencoding(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 256, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 256; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 220, G: 60, B: 20, A: 128})
		}
	}
	var raw bytes.Buffer
	if e := png.Encode(&raw, source); e != nil {
		t.Fatal(e)
	}
	got, e := Prepare(raw.Bytes(), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	c, format, e := image.DecodeConfig(bytes.NewReader(got.Bytes))
	if e != nil || format != "jpeg" || c.Width != Size || c.Height != Size || len(got.Bytes) > MaxThumbnailBytes || got.InputSHA256 == got.SHA256 {
		t.Fatalf("thumbnail not canonical: %+v %s %v", c, format, e)
	}
	if _, e = jpeg.Decode(bytes.NewReader(got.Bytes)); e != nil {
		t.Fatal(e)
	}
	for _, in := range []struct {
		raw  []byte
		kind string
	}{{nil, "image/png"}, {raw.Bytes(), "image/jpeg"}, {[]byte("private arbitrary bytes"), "image/png"}, {make([]byte, MaxInputBytes+1), "image/png"}} {
		if _, e = Prepare(in.raw, in.kind); e != ErrInvalid {
			t.Fatalf("accepted invalid input: %v", e)
		}
	}
}
func TestPersonAvatarOversizeDimensionsRejectedBeforeDecode(t *testing.T) {
	var raw bytes.Buffer
	png.Encode(&raw, image.NewGray(image.Rect(0, 0, 4097, 1)))
	if _, e := Prepare(raw.Bytes(), "image/png"); e != ErrInvalid {
		t.Fatal("oversize accepted", e)
	}
}
func TestPersonAvatarMetadataCanonicalPath(t *testing.T) {
	id := "01234567-89ab-4cde-8fab-0123456789ab"
	m := Metadata{id, 4, Path(id, 4), Size, Size, 25, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if !m.Valid() {
		t.Fatal("canonical metadata invalid")
	}
	m.ThumbnailPath = "https://external.invalid/private-photo"
	if m.Valid() {
		t.Fatal("external URL accepted")
	}
	m.ThumbnailPath = Path(id, 3)
	if m.Valid() {
		t.Fatal("stale path accepted")
	}
}
