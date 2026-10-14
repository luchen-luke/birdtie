package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
)

type photoHTTPSpy struct {
	identity.AccessStore
	writes              int
	finalErr, sourceErr error
	malformed           bool
	state               pp.State
	pixels              []byte
	receipt             pp.Receipt
}

func (p *photoHTTPSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: avatarTestOwner, AccountType: "person"}, nil
}
func (p *photoHTTPSpy) ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error {
	return p.finalErr
}
func (p *photoHTTPSpy) ReadPlacePhoto(context.Context, pp.Access, string) (pp.State, error) {
	return p.state, p.sourceErr
}
func (p *photoHTTPSpy) ReadPlacePhotoEditorial(_ context.Context, _ pp.Access, id string) (pp.Editorial, error) {
	at := time.Now().UTC()
	return pp.Editorial{PlaceID: id, CityID: "aberdeen-gb", CanContribute: true, ObservedAt: at, ExpiresAt: at.Add(30 * time.Second), Candidates: []pp.Candidate{}}, p.sourceErr
}
func (p *photoHTTPSpy) PreviewPlacePhoto(_ context.Context, _ pp.Access, id string, in pp.Input) (pp.Approval, error) {
	d, e := pp.Normalize(in, time.Now().UTC())
	out := pp.Approval{PlaceID: id, OperationID: in.OperationID, ExpectedVersion: in.ExpectedVersion, Action: in.Action, CandidateID: in.OperationID, InputSHA256: d.SHA256, RightsSHA256: in.Rights.Digest(), ExpiresAt: time.Now().Add(30 * time.Second), Token: "pp1.unit.bound.preview", PreviewDerivative: &pp.Derivative{ContentType: "image/jpeg", Width: 320, Height: 180, ByteCount: len(d.Bytes), SHA256: d.SHA256, ContentBase64: base64.StdEncoding.EncodeToString(d.Bytes)}}
	if p.malformed {
		out.PreviewDerivative.SHA256 = strings.Repeat("f", 64)
	}
	return out, e
}
func (p *photoHTTPSpy) ChangePlacePhoto(_ context.Context, _ pp.Access, id string, in pp.Input) (pp.Receipt, error) {
	p.writes++
	p.receipt = pp.Receipt{PlaceID: id, OperationID: in.OperationID, Action: "SUBMIT", CandidateID: in.OperationID, CandidateVersion: 1, Status: "SUCCEEDED"}
	return p.receipt, nil
}
func (p *photoHTTPSpy) ReadPlacePhotoOperation(context.Context, pp.Access, string, string) (pp.Receipt, error) {
	if p.receipt.Status == "" {
		return pp.Receipt{}, pp.ErrNotFound
	}
	return p.receipt, p.sourceErr
}
func (p *photoHTTPSpy) StopPlacePhotoOperation(_ context.Context, _ pp.Access, id, op string, _ int64) (pp.Receipt, error) {
	if p.receipt.Status != "SUCCEEDED" {
		p.receipt = pp.Receipt{PlaceID: id, OperationID: op, Action: "STOP", Status: "STOPPED"}
	}
	return p.receipt, nil
}
func (p *photoHTTPSpy) ReadPlacePhotoContent(context.Context, pp.Access, string, int64) (pp.Metadata, []byte, error) {
	return *p.state.PlacePhoto, p.pixels, p.sourceErr
}
func (p *photoHTTPSpy) ValidatePlacePhotoProjection(context.Context, pp.Access, string, *pp.Metadata, string) error {
	return p.sourceErr
}
func (p *photoHTTPSpy) ValidatePlacePhotoPreview(context.Context, pp.Access, string, pp.Input, pp.Approval) error {
	return p.sourceErr
}
func (p *photoHTTPSpy) ValidatePlacePhotoEditorial(context.Context, pp.Access, string, pp.Editorial) error {
	return p.sourceErr
}
func photoHTTPHandler(p *photoHTTPSpy) http.Handler {
	mux := http.NewServeMux()
	(&server{access: p}).registerPlacePhotoRoutes(mux)
	return mux
}
func photoHTTPBody(t *testing.T, commit bool) string {
	return strings.ReplaceAll(strings.ReplaceAll(coverHTTPBody(t, commit, true), `"SAVE"`, `"SUBMIT"`), "ec1.", "pp1.")
}
func TestPlacePhotoHTTPClosedRightsAndActualReviewDerivative(t *testing.T) {
	base := "/v1/places/" + communityAvatarID + "/photos"
	valid := photoHTTPBody(t, true)
	for i, body := range []string{strings.TrimSuffix(valid, "}") + `,"expectedVersion":0}`, strings.TrimSuffix(valid, "}") + `,"candidateId":"` + avatarTestOp + `"}`, strings.Replace(valid, `"canRetain":true`, `"canRetain":false`, 1), strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":null`, 1)} {
		t.Run("closed"+string(rune('a'+i)), func(t *testing.T) {
			p := &photoHTTPSpy{}
			w := httptest.NewRecorder()
			photoHTTPHandler(p).ServeHTTP(w, avatarRequest("PUT", base, body))
			if w.Code != 400 || p.writes != 0 {
				t.Fatal("bad request wrote", w.Code)
			}
		})
	}
	for _, bad := range []bool{false, true} {
		p := &photoHTTPSpy{malformed: bad}
		w := httptest.NewRecorder()
		photoHTTPHandler(p).ServeHTTP(w, avatarRequest("POST", base+"/preview", photoHTTPBody(t, false)))
		want := 200
		if bad {
			want = 503
		}
		if w.Code != want {
			t.Fatal("actual preview hash validation", w.Code, w.Body.String())
		}
		if !bad && !strings.Contains(w.Body.String(), "previewDerivative") {
			t.Fatal("canonical actual preview absent")
		}
	}
	p := &photoHTTPSpy{}
	req := avatarRequest("POST", base+"/preview", photoHTTPBody(t, false))
	req.Header["X-Birdtie-Organization-Workspace"] = []string{""}
	w := httptest.NewRecorder()
	photoHTTPHandler(p).ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("organization borrowed editorial identity", w.Code)
	}
}
func TestPlacePhotoHTTPOriginalUnknownStopAndPostCommitSession(t *testing.T) {
	base := "/v1/places/" + communityAvatarID + "/photos"
	p := &photoHTTPSpy{finalErr: identity.ErrUnauthorized}
	w := httptest.NewRecorder()
	photoHTTPHandler(p).ServeHTTP(w, avatarRequest("PUT", base, photoHTTPBody(t, true)))
	if w.Code != 401 || p.writes != 1 {
		t.Fatal("late Session response claimed zero", w.Code, p.writes)
	}
	p.finalErr = nil
	w = httptest.NewRecorder()
	photoHTTPHandler(p).ServeHTTP(w, avatarRequest("GET", base+"/operations/"+avatarTestOp, ""))
	if w.Code != 200 || p.writes != 1 || !strings.Contains(w.Body.String(), "SUCCEEDED") {
		t.Fatal("original operation not recovered", w.Code)
	}
	unknown := &photoHTTPSpy{}
	w = httptest.NewRecorder()
	photoHTTPHandler(unknown).ServeHTTP(w, avatarRequest("GET", base+"/operations/"+avatarTestOp, ""))
	if w.Code != 404 || unknown.writes != 0 {
		t.Fatal("unknown means no new write", w.Code)
	}
	w = httptest.NewRecorder()
	photoHTTPHandler(unknown).ServeHTTP(w, avatarRequest("POST", base+"/operations/"+avatarTestOp+"/stop", `{"expectedVersion":0}`))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "STOPPED") || unknown.writes != 0 {
		t.Fatal("explicit stop lost original zero effect", w.Code)
	}
}
func TestPlacePhotoHTTPAnonymousCurrentContentAndFinalRetirement(t *testing.T) {
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(photoHTTPBody(t, false)), &body) != nil {
		t.Fatal("fixture")
	}
	var encoded string
	json.Unmarshal(body["contentBase64"], &encoded)
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	d, e := ec.Prepare(raw, "image/png")
	if e != nil {
		t.Fatal(e)
	}
	m := &pp.Metadata{EntityType: "place", EntityID: communityAvatarID, Version: 1, ThumbnailPath: pp.Path(communityAvatarID, 1), Width: 320, Height: 180, ByteCount: len(d.Bytes), SHA256: d.SHA256, ExpiresAt: time.Now().UTC().Add(30 * time.Second)}
	p := &photoHTTPSpy{state: pp.State{Version: 1, PlacePhoto: m, SourceProof: "PRIVATE_IMAGE_SOURCE"}, pixels: d.Bytes}
	path := m.ThumbnailPath
	for _, authenticated := range []bool{false, true} {
		w := httptest.NewRecorder()
		req := avatarRequest("GET", path, "")
		if !authenticated {
			req.Header.Del("Authorization")
		}
		photoHTTPHandler(p).ServeHTTP(w, req)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), d.Bytes) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("current JPEG content", w.Code)
		}
	}
	p.sourceErr = pp.ErrChanged
	w := httptest.NewRecorder()
	photoHTTPHandler(p).ServeHTTP(w, avatarRequest("GET", path, ""))
	if w.Code != 409 || bytes.Equal(w.Body.Bytes(), d.Bytes) {
		t.Fatal("late source leaked JPEG", w.Code)
	}
	p.sourceErr = nil
	p.finalErr = identity.ErrUnauthorized
	w = httptest.NewRecorder()
	photoHTTPHandler(p).ServeHTTP(w, avatarRequest("GET", path, ""))
	if w.Code != 401 {
		t.Fatal("late Session leaked JPEG", w.Code)
	}
	p.finalErr = nil
	w = httptest.NewRecorder()
	photoHTTPHandler(p).ServeHTTP(w, avatarRequest("GET", "/v1/places/"+communityAvatarID+"/photos", ""))
	if strings.Contains(w.Body.String(), "PRIVATE_IMAGE_SOURCE") {
		t.Fatal("private proof leaked")
	}
}
