package legacyimagerecovery

import (
	"bytes"
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"image"
	"image/jpeg"
	"testing"
)

func TestRecoveryFullJPEGAndNoVideoOrHashSubstitution(t *testing.T) {
	var b bytes.Buffer
	if e := jpeg.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 9, 7)), nil); e != nil {
		t.Fatal(e)
	}
	raw := append(b.Bytes(), []byte("SYNTHETIC_SOURCE_TRAILER")...)
	r := Record{Record: legacymedia.Record{InputBytes: int64(len(raw)), InputSHA256: legacymedia.Hash(raw)}, Format: "image/jpeg", RecoveryReason: JPEGUnknown}
	n := &Normalizer{}
	d, e := n.Normalize(context.Background(), r, raw)
	defer d.Erase()
	if e != nil || d.Width != 9 || d.Height != 7 || bytes.Contains(d.Bytes, []byte("SYNTHETIC_SOURCE_TRAILER")) {
		t.Fatal("full frame or metadata stripping lost", e)
	}
	r.InputSHA256 = legacymedia.Hash([]byte("wrong"))
	if _, e = n.Normalize(context.Background(), r, raw); e == nil {
		t.Fatal("hash substitution accepted")
	}
	r.InputSHA256 = legacymedia.Hash(raw)
	r.Format, r.RecoveryReason = "image/heic", HEICMisclassified
	if _, e = n.Normalize(context.Background(), r, raw); e == nil {
		t.Fatal("JPEG or unavailable decoder accepted as HEIC")
	}
	r.Format = "video/mp4"
	if _, e = n.Normalize(context.Background(), r, raw); e == nil {
		t.Fatal("video entered image recovery")
	}
}
