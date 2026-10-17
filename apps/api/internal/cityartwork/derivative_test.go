package cityartwork

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, 600, 300))
	for y := 75; y < 225; y++ {
		for x := 150; x < 450; x++ {
			im.SetNRGBA(x, y, color.NRGBA{R: 220, G: 20, B: 40, A: 128})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestCityArtworkDerivativePreservesFrameAlphaAndIdentity(t *testing.T) {
	raw := testPNG(t)
	version := strings.Repeat("a", 64)
	v, e := Build(context.Background(), "synthetic-city", version, digest(raw), raw)
	if e != nil || !v.Valid() || v.Descriptor.Width != 324 || v.Descriptor.Height != 162 || bytes.Equal(raw, v.Bytes) {
		t.Fatal(v.Descriptor, e)
	}
	im, _, e := image.Decode(bytes.NewReader(v.Bytes))
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, a := im.At(0, 0).RGBA(); a != 0 {
		t.Fatal("transparent corner lost")
	}
	c := color.NRGBAModel.Convert(im.At(162, 81)).(color.NRGBA)
	if c.R < 215 || c.R > 225 || c.A < 127 || c.A > 129 {
		t.Fatal("premultiplied transparency changed", c)
	}
	again, e := Build(context.Background(), "synthetic-city", version, digest(raw), raw)
	if e != nil || !bytes.Equal(again.Bytes, v.Bytes) || again.Descriptor != v.Descriptor {
		t.Fatal("non-deterministic derivative")
	}
}

func TestCityArtworkRejectsWrongBytesUnsafeSelectorsAndCancellation(t *testing.T) {
	raw := testPNG(t)
	sha := digest(raw)
	v := strings.Repeat("b", 64)
	for _, in := range []struct {
		city, version, hash string
		body                []byte
	}{
		{"../private", v, sha, raw}, {"city", v, strings.Repeat("0", 64), raw}, {"city", "latest", sha, raw}, {"city", v, digest([]byte("html")), []byte("html")},
	} {
		if _, e := Build(context.Background(), in.city, in.version, in.hash, in.body); e == nil {
			t.Fatal("accepted invalid source", in.city)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Build(ctx, "city", v, sha, raw); e == nil {
		t.Fatal("accepted cancelled decode")
	}
}

// Optional actual source-byte audit; callers explicitly select the protected
// directory. It does not copy originals into fixtures, products, or reports.
func TestCityArtworkReviewedLocalSources(t *testing.T) {
	dir := os.Getenv("BIRDTIE_ARTWORK_SOURCE_DIR")
	if dir == "" {
		t.Skip("protected local original sources not selected")
	}
	for _, item := range []struct {
		name, city, hash string
		count            int
	}{
		{"青岛", "civu-geo-7a536573-67c6-42f0-9ede-c423fb5dba00", "d2988c88b86453e1ead0c637ffeb85abdcfd89c9ce3894d12351a609b4429988", 3351927},
		{"济南", "civu-geo-bbffd2af-68b4-49ee-bb01-5e5a7673aa9b", "69b9d3d653f3b7f64fd1423c54e18d73cb90f1186a824bb1cfe1319a835a4218", 2749346},
	} {
		raw, e := os.ReadFile(filepath.Join(dir, item.hash+".blob"))
		if e != nil {
			t.Fatal(item.name, e)
		}
		if len(raw) != item.count || digest(raw) != item.hash {
			t.Fatal(item.name, "original mismatch")
		}
		cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || format != "png" {
			t.Fatal(item.name, e)
		}
		v, e := Build(context.Background(), item.city, item.hash, item.hash, raw)
		if e != nil || !v.Valid() {
			t.Fatal(item.name, e)
		}
		t.Logf("%s originalBytes=%d originalSHA256=%s originalSize=%dx%d derivativeSize=%dx%d derivativeSHA256=%s", item.name, len(raw), item.hash, cfg.Width, cfg.Height, v.Descriptor.Width, v.Descriptor.Height, v.Descriptor.SHA256)
	}
}
