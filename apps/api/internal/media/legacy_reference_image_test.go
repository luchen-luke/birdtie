package media

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"testing"
)

func TestLegacyReferencePNGKeepsFrameAndStripsTrailingMetadata(t *testing.T) {
	var b bytes.Buffer
	if png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 3, 2))) != nil {
		t.Fatal("encode")
	}
	raw := append(b.Bytes(), []byte("SYNTHETIC_PRIVATE_METADATA")...)
	r, mime, e := (&LocalImageDerivativeBuilder{}).BuildLegacyPrivateImage(context.Background(), bytes.NewReader(raw), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	out, e := io.ReadAll(r)
	cfg, _, ce := image.DecodeConfig(bytes.NewReader(out))
	if e != nil || ce != nil || mime != "image/png" || cfg.Width != 3 || cfg.Height != 2 || bytes.Contains(out, []byte("SYNTHETIC_PRIVATE_METADATA")) {
		t.Fatal("full frame or privacy changed")
	}
}
