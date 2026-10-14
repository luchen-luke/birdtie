package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
)

type momentPhotoHTTPSpy struct {
	hcSpy
	content.MomentStore
	committed  mp.Receipt
	loseAfter  bool
	badPreview bool
}

func (p *momentPhotoHTTPSpy) ReadMomentPhotoState(context.Context, mp.Access, string) (mp.State, error) {
	if p.lost {
		return mp.State{}, mp.ErrForbidden
	}
	now := time.Now().UTC()
	return mp.State{MomentID: hcTestID, MomentRevision: 2, PhotoVersion: 0, CanSave: true, ObservedAt: now, ExpiresAt: now.Add(30 * time.Second), SourceVersion: strings.Repeat("a", 64), ResponseProof: p.proof()}, nil
}
func (p *momentPhotoHTTPSpy) PreviewMomentPhoto(_ context.Context, _ mp.Access, id string, in mp.Input) (mp.Preview, error) {
	d, sum, rights, e := mp.Normalize(in, false, time.Now().UTC())
	if e != nil {
		return mp.Preview{}, e
	}
	v := mp.Preview{HumanControlPreview: p.base(in.OperationID, in.Action, mp.Digest(id, in, sum, rights)), MomentID: id, PhotoID: in.PhotoID, ExpectedMomentRevision: in.ExpectedMomentRevision, ExpectedPhotoVersion: in.ExpectedPhotoVersion, InputSHA256: sum, RightsSHA256: rights, SourceVersion: strings.Repeat("a", 64), ResponseProof: p.proof()}
	if in.Action == "SAVE" {
		v.PreviewDerivative = &mp.PreviewDerivative{ContentType: "image/jpeg", Width: 320, Height: 180, ByteCount: len(d.Bytes), SHA256: sum, ContentBase64: base64.StdEncoding.EncodeToString(d.Bytes)}
		if p.badPreview {
			v.PreviewDerivative.ContentBase64 = "SGVsbG8="
		}
	}
	return v, nil
}
func (p *momentPhotoHTTPSpy) CommitMomentPhoto(_ context.Context, _ mp.Access, id string, in mp.Input) (mp.Receipt, error) {
	p.writes++
	_, sum, rights, _ := mp.Normalize(in, true, time.Now().UTC())
	at := time.Now().UTC()
	r, v := in.ExpectedMomentRevision, in.ExpectedPhotoVersion
	p.committed = mp.Receipt{OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, MomentID: id, PhotoID: in.PhotoID, ExpectedMomentRevision: &r, ExpectedPhotoVersion: &v, AppliedPhotoVersion: mp.NextVersion(v), PayloadDigest: mp.Digest(id, in, sum, rights), CommittedAt: &at}
	if p.loseAfter {
		p.final = identity.ErrUnauthorized
	}
	return p.committed, nil
}
func (p *momentPhotoHTTPSpy) ReadMomentPhotoOperation(context.Context, mp.Access, string, string) (mp.Receipt, error) {
	p.reads++
	if p.missing {
		return mp.Receipt{}, mp.ErrNotFound
	}
	return p.committed, nil
}
func (p *momentPhotoHTTPSpy) StopMomentPhotoOperation(_ context.Context, _ mp.Access, _ string, op string) (mp.Receipt, error) {
	p.stops++
	return mp.Receipt{OperationID: op, Status: "STOPPED"}, nil
}
func (p *momentPhotoHTTPSpy) ReadMomentPhotoContent(context.Context, mp.Access, mp.Selector, mp.Encode) ([]byte, error) {
	return nil, mp.ErrNotFound
}
func momentPhotoHTTPImage(t *testing.T) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			im.Set(x, y, color.RGBA{uint8(x * 30), 90, uint8(y * 30), 255})
		}
	}
	var b bytes.Buffer
	if e := png.Encode(&b, im); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func momentPhotoHTTPInput(t *testing.T, commit bool) string {
	t.Helper()
	m := map[string]any{"operationId": hcTestOp, "action": "SAVE", "expectedMomentRevision": 2, "photoId": "33333333-3333-4333-8333-333333333333", "expectedPhotoVersion": 0, "contentType": "image/png", "contentBase64": base64.StdEncoding.EncodeToString(momentPhotoHTTPImage(t)), "rights": mp.Rights{Basis: "OWNED", CanRetain: true, CanPublish: true, RetentionUntil: time.Now().UTC().Add(time.Hour)}}
	if commit {
		m["sourceVersion"] = strings.Repeat("a", 64)
		m["approval"] = "EXACT_INPUT"
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
func TestPublicMomentPhotoHTTPActualPreviewAndUnknownOriginalReceipt(t *testing.T) {
	p := &momentPhotoHTTPSpy{}
	h := New(nil, p, nil, p, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	base := "/v1/me/moments/" + hcTestID + "/public-photo"
	w := momentControlHTTPCall(h, "GET", base, "")
	if w.Code != 200 || p.writes != 0 {
		t.Fatal("owner state bypass", w.Code)
	}
	w = momentControlHTTPCall(h, "POST", base+"/previews", momentPhotoHTTPInput(t, false))
	if w.Code != 200 || p.writes != 0 || !strings.Contains(w.Body.String(), "previewDerivative") {
		t.Fatal("actual public preview missing", w.Code)
	}
	p.badPreview = true
	w = momentControlHTTPCall(h, "POST", base+"/previews", momentPhotoHTTPInput(t, false))
	if w.Code != 503 || p.writes != 0 {
		t.Fatal("metadata-only fake preview trusted")
	}
	p.badPreview = false
	p.loseAfter = true
	path := base + "/operations/" + hcTestOp
	w = momentControlHTTPCall(h, "PUT", path, momentPhotoHTTPInput(t, true))
	if w.Code != 401 || p.writes != 1 || strings.Contains(w.Body.String(), "SUCCEEDED") {
		t.Fatal("commit final fence failed UNKNOWN", w.Code)
	}
	p.final = nil
	p.lost = true
	w = momentControlHTTPCall(h, "GET", path, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "SUCCEEDED") || strings.Contains(w.Body.String(), "contentBase64") || strings.Contains(w.Body.String(), "retentionUntil") {
		t.Fatal("current authority replaced original tiny fact", w.Code)
	}
	p.missing = true
	w = momentControlHTTPCall(h, "GET", path, "")
	if w.Code != 404 || p.writes != 1 {
		t.Fatal("404 guesses no effect")
	}
	w = momentControlHTTPCall(h, "POST", path+"/stop", "")
	if w.Code != 200 || p.stops != 1 || strings.Contains(w.Body.String(), "photoId") {
		t.Fatal("bodyless stop exposes current material", w.Code)
	}
}
func TestPublicMomentPhotoHTTPClosedAuthorityAndNoPrivateAttachmentImport(t *testing.T) {
	p := &momentPhotoHTTPSpy{}
	h := New(nil, p, nil, p, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	base := "/v1/me/moments/" + hcTestID + "/public-photo"
	good := momentPhotoHTTPInput(t, false)
	for _, bad := range []string{strings.TrimSuffix(good, "}") + `,"privateImageId":"` + hcTestID + `"}`, strings.TrimSuffix(good, "}") + `,"ownerId":"` + hcTestID + `"}`, strings.TrimSuffix(good, "}") + `,"sourceVersion":"client"}`, strings.TrimSuffix(good, "}") + `,"photoId":"` + hcTestID + `"}`} {
		w := momentControlHTTPCall(h, "POST", base+"/previews", bad)
		if w.Code != 400 || p.writes != 0 {
			t.Fatal("caller authority reached photo writer", w.Code)
		}
	}
	for _, header := range []string{"X-Birdtie-Organization-Workspace", "X-Birdtie-Business-Workspace"} {
		req := hcRequest("POST", base+"/previews", good)
		req.Header.Set(header, hcTestID)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Fatal("organization borrowed for person photo", w.Code)
		}
	}
	w := momentControlHTTPCall(h, "POST", base+"/operations/"+hcTestOp+"/stop", `{}`)
	if w.Code != 400 || p.stops != 0 {
		t.Fatal("stop accepted source body")
	}
	w = momentControlHTTPCall(h, "POST", base+"/previews?owner="+hcTestID, good)
	if w.Code != 400 {
		t.Fatal("query authority accepted")
	}
}
