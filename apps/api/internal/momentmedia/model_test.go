package momentmedia_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	. "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"
)

const testID = "11111111-1111-4111-8111-111111111111"

func ImageForTest(t *testing.T, index int) []byte {
	t.Helper()
	src := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			src.Set(x, y, color.RGBA{uint8(20 + index*20), uint8(y * 10), uint8(x * 12), 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, src); e != nil {
		t.Fatal(e)
	}
	raw := b.Bytes()
	text := []byte("private\x00BIRDTIE-PRIVATE-EXIF-CANARY")
	chunk := make([]byte, len(text)+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(text)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], text)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	return append(append(append([]byte{}, raw[:33]...), chunk...), raw[33:]...)
}
func TestMomentMediaRealStaticDerivativesAndScope(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 9; i++ {
		raw := ImageForTest(t, i)
		d, e := Prepare(context.Background(), StageInput{OperationID: testID, LocalScope: testID, ContentType: "image/png", Content: raw}, (&media.LocalImageDerivativeBuilder{}).BuildPublic)
		if e != nil {
			t.Fatal(e)
		}
		if d.Width != 16 || d.Height != 12 || DigestBytes(d.Display) != d.DisplaySHA || DigestBytes(d.Thumbnail) != d.ThumbnailSHA || seen[d.DisplaySHA] || bytes.Contains(d.Display, []byte("CANARY")) {
			t.Fatal("actual normalized bytes, unique order and metadata stripping required")
		}
		seen[d.DisplaySHA] = true
		cfg, e := jpeg.DecodeConfig(bytes.NewReader(d.Thumbnail))
		if e != nil || cfg.Width != 320 || cfg.Height != 180 {
			t.Fatal("actual canonical JPEG required", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Prepare(ctx, StageInput{OperationID: testID, LocalScope: testID, ContentType: "image/png", Content: ImageForTest(t, 0)}, (&media.LocalImageDerivativeBuilder{}).BuildPublic); e == nil {
		t.Fatal("cancelled decode returned material")
	}
	if _, e := Prepare(context.Background(), StageInput{OperationID: testID, LocalScope: testID, ContentType: "image/jpeg", Content: ImageForTest(t, 0)}, (&media.LocalImageDerivativeBuilder{}).BuildPublic); e == nil {
		t.Fatal("MIME mismatch accepted")
	}
	if _, e := Prepare(context.Background(), StageInput{OperationID: testID, LocalScope: testID, ContentType: "image/svg+xml", Content: []byte("<svg/>")}, (&media.LocalImageDerivativeBuilder{}).BuildPublic); e == nil {
		t.Fatal("nonstatic image accepted")
	}
}
func TestMomentMediaOrderedRightsAndClosedContract(t *testing.T) {
	rights := Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}
	in := Input{OperationID: testID, Action: "REPLACE", ExpectedMomentRevision: 2, Attachments: []AttachmentInput{{MediaID: testID, MediaVersion: 1, Rights: rights}}}
	if ValidateInput(in, false, time.Now().UTC()) != nil {
		t.Fatal("explicitly declared owned rights rejected")
	}
	digest := InputDigest(testID, in)
	in.Attachments[0].Rights.CanPublish = false
	if ValidateInput(in, false, time.Now().UTC()) == nil || InputDigest(testID, in) == digest {
		t.Fatal("unconfirmed rights or changed public consequence accepted")
	}
	in.Attachments[0].Rights = rights
	in.Attachments = append(in.Attachments, in.Attachments[0])
	if ValidateInput(in, false, time.Now().UTC()) == nil {
		t.Fatal("duplicate stable ID accepted")
	}
	var decoded Input
	if StrictJSON([]byte(`{"operationId":"`+testID+`","approved":true}`), &decoded) == nil {
		t.Fatal("unknown approval accepted")
	}
	if Hash(strings.Repeat("A", 64)) {
		t.Fatal("noncanonical checksum accepted")
	}
}
