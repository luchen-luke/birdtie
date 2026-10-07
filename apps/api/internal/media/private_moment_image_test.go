package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func privateImageUnitInput() (PrivateImageInput, []byte) {
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	sum := sha256.Sum256(b.Bytes())
	return PrivateImageInput{OperationID: "11111111-1111-4111-8111-111111111111", MomentRevision: 1, MIME: "image/png", ByteSize: int64(b.Len()), SHA256: hex.EncodeToString(sum[:]), PixelRisk: "UNKNOWN", Purpose: PrivateImagePurpose}, b.Bytes()
}
func TestPrivateMomentImageReviewedBytesAndCodec(t *testing.T) {
	p, b := privateImageUnitInput()
	out, h, e := PreparePrivateImage(context.Background(), p, b)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(out)
	if h != hex.EncodeToString(sum[:]) {
		t.Fatal("not actual derivative hash")
	}
	im, e := png.Decode(bytes.NewReader(out))
	if e != nil || im.Bounds().Dx() != 2 {
		t.Fatal("real derivative codec")
	}
	for _, risk := range []string{"UNKNOWN", "USER_MASKED"} {
		p.PixelRisk = risk
		if _, _, e = PreparePrivateImage(context.Background(), p, b); e != nil {
			t.Fatal(e)
		}
	}
	p.PixelRisk = "SAFE"
	if _, _, e = PreparePrivateImage(context.Background(), p, b); !errors.Is(e, content.ErrInvalid) {
		t.Fatal("clean verdict was invented")
	}
}
func TestPrivateMomentImageRejectsUnreviewedAndAuthority(t *testing.T) {
	p, b := privateImageUnitInput()
	cases := []struct {
		name   string
		change func(*PrivateImageInput)
	}{
		{"purpose", func(p *PrivateImageInput) { p.Purpose = "MODEL_CONTEXT_EGRESS" }}, {"emptyOp", func(p *PrivateImageInput) { p.OperationID = "" }},
		{"badHash", func(p *PrivateImageInput) { p.SHA256 = "yes" }}, {"differentBytes", func(p *PrivateImageInput) { p.SHA256 = strings.Repeat("a", 64) }},
		{"differentLength", func(p *PrivateImageInput) { p.ByteSize++ }}, {"missingVersion", func(p *PrivateImageInput) { p.MomentRevision = 0 }},
		{"mimeMismatch", func(p *PrivateImageInput) { p.MIME = "image/jpeg" }}, {"unsupported", func(p *PrivateImageInput) { p.MIME = "image/svg+xml" }},
		{"large", func(p *PrivateImageInput) { p.ByteSize = MaxImageInputBytes + 1 }}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := p
			c.change(&v)
			out, h, e := PreparePrivateImage(context.Background(), v, b)
			if e == nil || len(out) > 0 || h != "" {
				t.Fatal("unreviewed image accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e := PreparePrivateImage(ctx, p, b); e == nil {
		t.Fatal("cancel accepted")
	}
}
