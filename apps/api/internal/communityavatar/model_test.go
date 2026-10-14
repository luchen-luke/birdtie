package communityavatar

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestCommunityAvatarNormalizedApprovalDigest(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 140, 160))
	img.SetNRGBA(40, 50, color.NRGBA{R: 200, A: 255})
	var raw bytes.Buffer
	if e := png.Encode(&raw, img); e != nil {
		t.Fatal(e)
	}
	in := Input{OperationID: "11111111-1111-4111-8111-111111111111", ContentType: "image/png", Content: raw.Bytes()}
	_, thumb, action, sum, e := Normalize(in)
	if e != nil || action != "SAVE" || sum != thumb.SHA256 || sum == thumb.InputSHA256 {
		t.Fatal("approval must bind reencoded thumbnail", e)
	}
	id := "22222222-2222-4222-8222-222222222222"
	d := Digest(id, in, action, sum)
	for _, change := range []Input{{OperationID: "33333333-3333-4333-8333-333333333333"}, {OperationID: in.OperationID, ExpectedVersion: 1}} {
		if Digest(id, change, action, sum) == d {
			t.Fatal("selector/version not bound")
		}
	}
	if Digest(in.OperationID, in, action, sum) == d || Digest(id, in, "DELETE", "") == d {
		t.Fatal("target/action not bound")
	}
	for _, bad := range []Input{{OperationID: in.OperationID, ExpectedVersion: MaxVersion, Delete: true}, {OperationID: in.OperationID, Delete: true, Content: raw.Bytes()}, {OperationID: in.OperationID, ContentType: "image/jpeg", Content: raw.Bytes()}, {Delete: true}} {
		if _, _, _, _, e = Normalize(bad); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid accepted", e)
		}
	}
	clear := Input{OperationID: in.OperationID, ExpectedVersion: 3, Delete: true}
	if _, _, a, s, e := Normalize(clear); e != nil || a != "DELETE" || s != "" {
		t.Fatal("clear needs no pixels", e)
	}
}

func TestCommunityAvatarCanonicalCommunityBinding(t *testing.T) {
	id := "22222222-2222-4222-8222-222222222222"
	m := Metadata{CommunityID: id, Version: 2, ThumbnailPath: Path(id, 2), Width: 128, Height: 128, ByteCount: 123, SHA256: strings.Repeat("a", 64)}
	if !m.Valid() {
		t.Fatal("canonical community thumbnail rejected")
	}
	for _, path := range []string{"https://fixture.invalid/private-photo", "/v1/accounts/" + id + "/avatar/2/content", Path(id, 1)} {
		bad := m
		bad.ThumbnailPath = path
		if bad.Valid() {
			t.Fatal("foreign/stale media accepted", path)
		}
	}
	bad := m
	bad.Version = MaxVersion + 1
	bad.ThumbnailPath = Path(id, bad.Version)
	if bad.Valid() {
		t.Fatal("unsafe web integer")
	}
}
