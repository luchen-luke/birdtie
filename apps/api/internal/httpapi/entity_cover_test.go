package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type coverHTTPSpy struct {
	identity.AccessStore
	finalErr, sourceErr error
	projectionErr       error
	writes              int
	malformed           bool
	current             ec.State
	bytes               []byte
	receipt             ec.Receipt
	frameGeneration     int64
	frameCaptureCount   int
	frameErr            error
	originalProof       string
	receivedProof       string
	stateReads          int
	onOriginal          func()
}

func (p *coverHTTPSpy) CaptureEntityCoverSession(_ context.Context, a ec.Access) (ec.SessionFrame, error) {
	p.frameCaptureCount++
	if a.AccountID == "" {
		return ec.SessionFrame{}, nil
	}
	g := p.frameGeneration
	if g == 0 {
		g = 1
	}
	return ec.SessionFrame{ID: avatarTestOwner, Generation: g}, nil
}
func (p *coverHTTPSpy) ValidateEntityCoverSession(_ context.Context, a ec.Access, f ec.SessionFrame) error {
	if p.frameErr != nil {
		return p.frameErr
	}
	g := p.frameGeneration
	if g == 0 {
		g = 1
	}
	if a.AccountID != "" && (f.ID != avatarTestOwner || f.Generation != g) {
		return ec.ErrChanged
	}
	return nil
}

func (p *coverHTTPSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: avatarTestOwner, AccountType: "person"}, nil
}
func (p *coverHTTPSpy) ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error {
	return p.finalErr
}
func (p *coverHTTPSpy) ReadEntityCover(context.Context, ec.Access, ec.Ref) (ec.State, error) {
	p.stateReads++
	return p.current, p.sourceErr
}
func (p *coverHTTPSpy) ReadEntityCoverOriginalContent(context.Context, ec.Access, ec.Ref, int64) (ec.OriginalContent, error) {
	out := ec.OriginalContent{Metadata: *p.current.Cover, Bytes: p.bytes, SourceProof: p.current.SourceProof}
	if p.originalProof != "" {
		out.SourceProof = p.originalProof
	}
	if p.onOriginal != nil {
		p.onOriginal()
	}
	return out, p.sourceErr
}
func (p *coverHTTPSpy) PreviewEntityCover(_ context.Context, _ ec.Access, r ec.Ref, in ec.Input) (ec.Approval, error) {
	d, sum, rights, e := ec.Normalize(in, time.Now().UTC())
	out := ec.Approval{Ref: r, OperationID: in.OperationID, ExpectedVersion: in.ExpectedVersion, Action: in.Action, InputSHA256: sum, RightsSHA256: rights, ExpiresAt: time.Now().Add(30 * time.Second), Token: "ec1.unit.bound.preview"}
	if in.Action == "SAVE" {
		out.PreviewDerivative = &ec.PreviewDerivative{ContentType: "image/jpeg", Width: 320, Height: 180, ByteCount: len(d.Bytes), SHA256: sum, ContentBase64: base64.StdEncoding.EncodeToString(d.Bytes)}
	}
	if p.malformed && out.PreviewDerivative != nil {
		out.PreviewDerivative.SHA256 = strings.Repeat("f", 64)
	}
	return out, e
}
func (p *coverHTTPSpy) ChangeEntityCover(_ context.Context, _ ec.Access, r ec.Ref, in ec.Input) (ec.Receipt, error) {
	p.writes++
	p.receipt = ec.Receipt{Ref: r, OperationID: in.OperationID, Action: in.Action, AppliedVersion: in.ExpectedVersion + 1, Status: "SUCCEEDED"}
	out := p.receipt
	if p.malformed {
		out.ID = avatarTestOwner
	}
	return out, nil
}
func (p *coverHTTPSpy) ReadEntityCoverOperation(_ context.Context, _ ec.Access, _ ec.Ref, _ string) (ec.Receipt, error) {
	return p.receipt, p.sourceErr
}
func (p *coverHTTPSpy) StopEntityCoverOperation(_ context.Context, _ ec.Access, r ec.Ref, op string, _ int64) (ec.Receipt, error) {
	if p.receipt.Status != "SUCCEEDED" {
		p.receipt = ec.Receipt{Ref: r, OperationID: op, Action: "STOP", Status: "STOPPED"}
	}
	return p.receipt, nil
}
func (p *coverHTTPSpy) ReadEntityCoverContent(context.Context, ec.Access, ec.Ref, int64) (ec.Metadata, []byte, error) {
	return *p.current.Cover, p.bytes, p.sourceErr
}
func (p *coverHTTPSpy) ValidateEntityCoverProjection(_ context.Context, _ ec.Access, _ ec.Ref, _ *ec.Metadata, proof string) error {
	p.receivedProof = proof
	if p.projectionErr != nil {
		return p.projectionErr
	}
	return p.sourceErr
}
func (p *coverHTTPSpy) ValidateEntityCoverPreview(context.Context, ec.Access, ec.Ref, ec.Input, ec.Approval) error {
	return p.sourceErr
}
func coverHTTPHandler(p *coverHTTPSpy) http.Handler {
	mux := http.NewServeMux()
	(&server{access: p}).registerEntityCoverRoutes(mux)
	return mux
}
func coverHTTPBody(t *testing.T, commit, save bool) string {
	t.Helper()
	m := map[string]any{"operationId": avatarTestOp, "expectedVersion": 0, "action": "DELETE"}
	if save {
		var b bytes.Buffer
		if png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 8, 8))) != nil {
			t.Fatal("fixture")
		}
		m["action"] = "SAVE"
		m["contentType"] = "image/png"
		m["contentBase64"] = base64.StdEncoding.EncodeToString(b.Bytes())
		m["rights"] = ec.Rights{Basis: "OWNED", RetentionUntil: time.Now().UTC().Add(time.Hour), CanRetain: true, CanPublish: true}
	}
	if commit {
		m["approval"] = "ec1.unit.bound.preview"
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
func TestEntityCoverHTTPClosedRightsPreviewAndActor(t *testing.T) {
	valid := coverHTTPBody(t, true, true)
	cases := []string{strings.TrimSuffix(valid, "}") + `,"unknown":true}`, strings.TrimSuffix(valid, "}") + `,"expectedVersion":0}`, strings.Replace(valid, `"canPublish":true`, `"canPublish":false`, 1), strings.Replace(valid, `"expectedVersion":0`, `"expectedVersion":null`, 1), strings.Replace(valid, `"OWNED"`, `"UNKNOWN"`, 1)}
	for i, b := range cases {
		p := &coverHTTPSpy{}
		w := httptest.NewRecorder()
		coverHTTPHandler(p).ServeHTTP(w, avatarRequest("PUT", "/v1/activities/"+communityAvatarID+"/cover", b))
		if w.Code != 400 || p.writes != 0 {
			t.Fatal("invalid request dispatched", i, w.Code)
		}
	}
	for _, bad := range []bool{false, true} {
		p := &coverHTTPSpy{malformed: bad}
		w := httptest.NewRecorder()
		coverHTTPHandler(p).ServeHTTP(w, avatarRequest("POST", "/v1/activities/"+communityAvatarID+"/cover/preview", coverHTTPBody(t, false, true)))
		want := 200
		if bad {
			want = 503
		}
		if w.Code != want || (!bad && !strings.Contains(w.Body.String(), "previewDerivative")) {
			t.Fatal("actual normalized preview", w.Code, w.Body.String())
		}
	}
	p := &coverHTTPSpy{}
	w := httptest.NewRecorder()
	req := avatarRequest("PUT", "/v1/organizations/"+communityAvatarID+"/cover", valid)
	req.Header.Set("X-Birdtie-Organization-Workspace", communityAvatarID)
	coverHTTPHandler(p).ServeHTTP(w, req)
	if w.Code != 403 || p.writes != 0 {
		t.Fatal("borrowed workspace writer")
	}
}
func TestEntityCoverHTTPOriginalReceiptFinalSessionAndStop(t *testing.T) {
	for _, failure := range []error{nil, identity.ErrUnauthorized} {
		p := &coverHTTPSpy{finalErr: failure}
		w := httptest.NewRecorder()
		coverHTTPHandler(p).ServeHTTP(w, avatarRequest("PUT", "/v1/activities/"+communityAvatarID+"/cover", coverHTTPBody(t, true, false)))
		want := 200
		if failure != nil {
			want = 401
		}
		if w.Code != want || p.writes != 1 {
			t.Fatal("postcommit unknown fence", w.Code)
		}
	}
	p := &coverHTTPSpy{}
	w := httptest.NewRecorder()
	coverHTTPHandler(p).ServeHTTP(w, avatarRequest("POST", "/v1/activities/"+communityAvatarID+"/cover/operations/"+avatarTestOp+"/stop", `{"expectedVersion":0}`))
	if w.Code != 200 || p.receipt.Status != "STOPPED" || p.writes != 0 {
		t.Fatal("causal stop", w.Code)
	}
	p.sourceErr = ec.ErrNotFound
	w = httptest.NewRecorder()
	coverHTTPHandler(p).ServeHTTP(w, avatarRequest("GET", "/v1/activities/"+communityAvatarID+"/cover/operations/"+avatarTestOp, ""))
	if w.Code != 404 {
		t.Fatal("unknown original op")
	}
	if !errors.Is(p.sourceErr, ec.ErrNotFound) {
		t.Fatal("test source")
	}
}

func TestEntityCoverHTTPWideContentAnonymousAndFinalRetirement(t *testing.T) {
	var pngRaw bytes.Buffer
	if png.Encode(&pngRaw, image.NewRGBA(image.Rect(0, 0, 320, 180))) != nil {
		t.Fatal("PNG")
	}
	d, e := ec.Prepare(pngRaw.Bytes(), "image/png")
	if e != nil {
		t.Fatal(e)
	}
	ref := ec.Ref{Type: "organization", ID: communityAvatarID}
	metadata := ec.Metadata{Ref: ref, Version: 1, ThumbnailPath: ec.Path(ref, 1), Width: 320, Height: 180, ByteCount: len(d.Bytes), SHA256: d.SHA256, ExpiresAt: time.Now().UTC().Add(45 * time.Second)}
	for _, c := range []struct {
		name            string
		auth            bool
		source, session error
		want            int
	}{{"anonymous", false, nil, nil, 200}, {"person", true, nil, nil, 200}, {"lateSource", true, ec.ErrChanged, nil, 409}, {"lateSession", true, nil, identity.ErrUnauthorized, 401}} {
		t.Run(c.name, func(t *testing.T) {
			p := &coverHTTPSpy{current: ec.State{Version: 1, Cover: &metadata, SourceProof: "PRIVATE_PROOF"}, bytes: d.Bytes, projectionErr: c.source, finalErr: c.session}
			req := httptest.NewRequest("GET", "/v1/organizations/"+ref.ID+"/cover/1/content", nil)
			if c.auth {
				req = avatarRequest("GET", "/v1/organizations/"+ref.ID+"/cover/1/content", "")
			}
			w := httptest.NewRecorder()
			coverHTTPHandler(p).ServeHTTP(w, req)
			if w.Code != c.want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("read branch", w.Code, w.Body.String())
			}
			if c.want == 200 {
				if w.Header().Get("Content-Type") != "image/jpeg" || !bytes.Equal(w.Body.Bytes(), d.Bytes) {
					t.Fatal("actual JPEG")
				}
			} else if w.Header().Get("Content-Type") == "image/jpeg" || bytes.Equal(w.Body.Bytes(), d.Bytes) {
				t.Fatal("retired media bytes escaped")
			}
		})
	}
}
