package momentphoto

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

const photoTestMoment = "11111111-1111-4111-8111-111111111111"
const photoTestID = "22222222-2222-4222-8222-222222222222"
const photoTestOp = "33333333-3333-4333-8333-333333333333"

func photoModelImage(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 12, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			im.Set(x, y, color.RGBA{uint8(20 * x), uint8(20 * y), 100, 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestPublicMomentPhotoModelMetadataLeaseAndClosedSelector(t *testing.T) {
	now := time.Now().UTC()
	m := Metadata{MomentID: photoTestMoment, MomentRevision: 2, PhotoID: photoTestID, Version: 1, ThumbnailPath: Path(photoTestMoment, 2, photoTestID, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(30 * time.Second)}
	if !m.Valid(now) {
		t.Fatal("valid independent public selector rejected")
	}
	raw, _ := json.Marshal(m)
	var keys map[string]any
	_ = json.Unmarshal(raw, &keys)
	if len(keys) != 11 {
		t.Fatal("metadata is not the closed11field wire")
	}
	for _, mode := range []string{"revision", "moment", "photo", "version", "path", "expiry", "shape"} {
		t.Run(mode, func(t *testing.T) {
			x := m
			switch mode {
			case "revision":
				x.MomentRevision = 1
			case "moment":
				x.MomentID = photoTestOp
			case "photo":
				x.PhotoID = photoTestOp
			case "version":
				x.Version = 2
			case "path":
				x.ThumbnailPath = "https://foreign.invalid/photo.jpg"
			case "expiry":
				x.ExpiresAt = now
			case "shape":
				x.Width = 128
			}
			if x.Valid(now) {
				t.Fatal("unbound metadata accepted")
			}
		})
	}
	x := m
	x.ExpiresAt = now.Add(40 * time.Second)
	if !PreserveLease(&m, &x, now) || x.ExpiresAt != m.ExpiresAt {
		t.Fatal("lease renewed")
	}
	x = m
	x.ExpiresAt = now.Add(20 * time.Second)
	if PreserveLease(&m, &x, now) {
		t.Fatal("shortened real deadline ignored")
	}
	if (ec.Ref{Type: "moment", ID: photoTestMoment}).Valid() {
		t.Fatal("public photo expanded unrelated cover capability")
	}
}
func TestPublicMomentPhotoModelConcreteBytesAndClosedInput(t *testing.T) {
	now := time.Now().UTC()
	in := Input{OperationID: photoTestOp, PhotoID: photoTestID, ExpectedMomentRevision: 2, Action: "SAVE", ContentType: "image/png", Content: photoModelImage(t), Rights: &Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: now.Add(time.Hour)}}
	d, sum, rights, e := Normalize(in, false, now)
	if e != nil || len(d.Bytes) == 0 || !Hex(sum) || !Hex(rights) {
		t.Fatal("actual derivative unavailable", e)
	}
	image, format, e := image.Decode(bytes.NewReader(d.Bytes))
	if e != nil || format != "jpeg" || image.Bounds().Dx() != 320 || image.Bounds().Dy() != 180 {
		t.Fatal("preview is not actual normalizedJPEG", e)
	}
	rawTuple, _ := json.Marshal([]any{"public-moment-photo-v1", photoTestMoment, photoTestOp, "SAVE", int64(2), photoTestID, int64(0), sum, rights})
	expected := sha256.Sum256(rawTuple)
	if Digest(photoTestMoment, in, sum, rights) != hex.EncodeToString(expected[:]) {
		t.Fatal("digest differs from documented ASCII tuple")
	}
	body := map[string]any{"operationId": in.OperationID, "action": in.Action, "expectedMomentRevision": 2, "photoId": in.PhotoID, "expectedPhotoVersion": 0, "contentType": in.ContentType, "contentBase64": base64.StdEncoding.EncodeToString(in.Content), "rights": in.Rights}
	raw, _ := json.Marshal(body)
	if _, e = Decode(raw, false); e != nil {
		t.Fatal("closed input rejected", e)
	}
	for _, bad := range []string{strings.TrimSuffix(string(raw), "}") + `,"ownerId":"other"}`, strings.TrimSuffix(string(raw), "}") + `,"photoId":"` + photoTestID + `"}`, strings.Replace(string(raw), `"SAVE"`, `"PUBLISH"`, 1), strings.Replace(string(raw), `"expectedMomentRevision":2`, `"expectedMomentRevision":1`, 1), strings.Replace(string(raw), `"canPublish":true`, `"canPublish":false`, 1)} {
		if _, e = Decode([]byte(bad), false); e == nil {
			t.Fatal("unchecked authority/rights/duplicate key accepted")
		}
	}
	in.Action = "DELETE"
	in.ExpectedPhotoVersion = MaxVersion
	in.Content = nil
	in.ContentType = ""
	in.Rights = nil
	if _, _, _, e = Normalize(in, false, now); e != nil || NextVersion(MaxVersion) != MaxVersion {
		t.Fatal("maximum terminal cleanup lost", e)
	}
	r := Receipt{OperationID: photoTestOp, Status: "STOPPED"}
	encoded, _ := json.Marshal(r)
	if !r.Valid(photoTestMoment, photoTestOp) || strings.Contains(string(encoded), "momentId") || strings.Contains(string(encoded), "photoId") {
		t.Fatal("STOPPED discloses current source")
	}
}
