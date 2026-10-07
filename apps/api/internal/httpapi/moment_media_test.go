package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

const imageMomentID = "11111111-1111-4111-8111-111111111111"
const imageOperationID = "22222222-2222-4222-8222-222222222222"

type imageHTTPSpy struct {
	humanMomentLegacySpy
	calls        int
	actor        identity.Actor
	digest       [32]byte
	failure      error
	finalFailure bool
	after        func()
}

func (s *imageHTTPSpy) capture(d [32]byte, a identity.Actor) { s.calls++; s.actor = a; s.digest = d }
func (s *imageHTTPSpy) PreviewHumanPrivateImage(_ context.Context, d [32]byte, a identity.Actor, m string, p media.PrivateImageInput) (media.PrivateImageReceipt, error) {
	s.capture(d, a)
	if s.after != nil {
		s.after()
	}
	return imageHTTPReceipt(), s.failure
}
func (s *imageHTTPSpy) SaveHumanPrivateImage(_ context.Context, d [32]byte, a identity.Actor, m, id, mime string, b []byte) (media.PrivateImageReceipt, error) {
	s.capture(d, a)
	if s.after != nil {
		s.after()
	}
	return imageHTTPReceipt(), s.failure
}
func (s *imageHTTPSpy) ReadHumanPrivateImages(_ context.Context, d [32]byte, a identity.Actor, m, id string, b bool, encode media.PrivateImageEncode) ([]byte, error) {
	s.capture(d, a)
	if s.failure != nil {
		return nil, s.failure
	}
	raw, e := encode([]media.PrivateImageReceipt{imageHTTPReceipt()}, []byte("synthetic derivative"))
	if s.after != nil {
		s.after()
	}
	if s.finalFailure {
		return nil, identity.ErrUnauthorized
	}
	return raw, e
}
func (s *imageHTTPSpy) ReadHumanPrivateImageOperation(ctx context.Context, d [32]byte, a identity.Actor, m, id string, encode media.PrivateImageEncode) ([]byte, error) {
	return s.ReadHumanPrivateImages(ctx, d, a, m, id, false, encode)
}
func (s *imageHTTPSpy) DeleteHumanPrivateImage(_ context.Context, d [32]byte, a identity.Actor, m, id string, rev int64) error {
	s.capture(d, a)
	if s.after != nil {
		s.after()
	}
	return s.failure
}

func TestPrivateMomentImageHTTPCancelledBeforeResponse(t *testing.T) {
	root := "/v1/me/moments/" + imageMomentID
	p := media.PrivateImageInput{OperationID: imageOperationID, MomentRevision: 1, MIME: "image/png", ByteSize: 1, SHA256: strings.Repeat("a", 64), PixelRisk: "UNKNOWN", Purpose: media.PrivateImagePurpose}
	body, _ := json.Marshal(p)
	for _, r := range []struct{ method, path, body string }{{"POST", root + "/private-image-previews", string(body)}, {"PUT", root + "/private-image-previews/" + imageMomentID + "/content", "x"}, {"GET", root + "/private-images/" + imageMomentID + "/content", ""}, {"GET", root + "/private-images", ""}, {"DELETE", root + "/private-images/" + imageMomentID + "?revision=2", ""}} {
		t.Run(r.method+r.path, func(t *testing.T) {
			token, _, e := identity.NewToken()
			if e != nil {
				t.Fatal(e)
			}
			access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			spy := &imageHTTPSpy{after: cancel}
			h := New(nil, access, nil, spy, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			req := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body)).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			if r.method == "PUT" {
				req.Header.Set("Content-Type", "image/png")
				req.Header.Set("X-Birdtie-Private-Image-Confirmation", imageMomentID)
			}
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, req)
			if rw.Code >= 200 && rw.Code < 300 || rw.Body.Len() != 0 {
				t.Fatal("cancelled request returned success/private receipt", rw.Code, rw.Body.Len())
			}
		})
	}
}
func TestPrivateMomentImageHTTPTypedNilStore(t *testing.T) {
	var store *imageHTTPSpy
	rw, _ := imageHTTPCall(t, store, "GET", "/v1/me/moments/"+imageMomentID+"/private-images", "", "")
	if rw.Code != 503 {
		t.Fatal("typed nil gateway should be unavailable", rw.Code)
	}
}
func imageHTTPReceipt() media.PrivateImageReceipt {
	return media.PrivateImageReceipt{ID: imageMomentID, OperationID: imageOperationID, MomentID: imageMomentID, OwnerID: imageMomentID, MomentRevision: 1, MIME: "image/png", ByteSize: 1, InputSHA256: strings.Repeat("a", 64), SHA256: strings.Repeat("b", 64), PixelRisk: "UNKNOWN", Purpose: media.PrivateImagePurpose, Status: "ready_private", Revision: 2, PreviewExpiresAt: time.Now().Add(time.Minute), RetainUntil: time.Now().Add(time.Hour)}
}
func imageHTTPCall(t *testing.T, store content.MomentStore, method, path, body, workspace string) (*httptest.ResponseRecorder, *humanMomentAccessSpy) {
	t.Helper()
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	h := New(nil, access, nil, store, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if workspace != "" {
		req.Header.Set("X-Birdtie-Organization-Workspace", workspace)
	}
	if strings.HasSuffix(path, "/content") {
		req.Header.Set("Content-Type", "image/png")
		req.Header.Set("X-Birdtie-Private-Image-Confirmation", imageMomentID)
	}
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	return rw, access
}
func TestPrivateMomentImageRegisteredGateway(t *testing.T) {
	root := "/v1/me/moments/" + imageMomentID
	p := media.PrivateImageInput{OperationID: imageOperationID, MomentRevision: 1, MIME: "image/png", ByteSize: 1, SHA256: strings.Repeat("a", 64), PixelRisk: "UNKNOWN", Purpose: media.PrivateImagePurpose}
	body, _ := json.Marshal(p)
	for _, r := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", root + "/private-image-previews", string(body), 201},
		{"PUT", root + "/private-image-previews/" + imageMomentID + "/content", "x", 200},
		{"GET", root + "/private-images", "", 200},
		{"GET", root + "/private-image-previews/" + imageMomentID, "", 200},
		{"GET", root + "/private-image-operations/" + imageOperationID, "", 200},
		{"GET", root + "/private-images/" + imageMomentID + "/content", "", 200},
		{"DELETE", root + "/private-images/" + imageMomentID + "?revision=2", "", 204},
	} {
		t.Run(r.method+r.path, func(t *testing.T) {
			legacy := &humanMomentLegacySpy{}
			rw, _ := imageHTTPCall(t, legacy, r.method, r.path, r.body, "")
			if rw.Code != 503 || legacy.calls != 0 {
				t.Fatalf("must register fail-closed native route, got %d", rw.Code)
			}
			spy := &imageHTTPSpy{}
			rw, access := imageHTTPCall(t, spy, r.method, r.path, r.body, "")
			if rw.Code != r.status || spy.calls != 1 || spy.digest != access.seen || spy.actor != access.actor || spy.humanMomentLegacySpy.calls != 0 {
				t.Fatalf("native trusted route %d %s", rw.Code, rw.Body)
			}
			if rw.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response cache")
			}
		})
	}
}
func TestPrivateMomentImageHTTPAuthorityAndFinalRead(t *testing.T) {
	path := "/v1/me/moments/" + imageMomentID + "/private-image-previews"
	p := map[string]any{"operationId": imageOperationID, "momentRevision": 1, "mimeType": "image/png", "byteSize": 1, "sha256": strings.Repeat("a", 64), "pixelRisk": "UNKNOWN", "purpose": media.PrivateImagePurpose}
	for _, field := range []string{"ownerId", "modelConsent", "public", "verified", "sourceBinding", "sessionId"} {
		t.Run(field, func(t *testing.T) {
			p[field] = true
			raw, _ := json.Marshal(p)
			spy := &imageHTTPSpy{}
			rw, _ := imageHTTPCall(t, spy, "POST", path, string(raw), "")
			delete(p, field)
			if rw.Code != 400 || spy.calls != 0 {
				t.Fatal("client authority accepted", rw.Code)
			}
		})
	}
	spy := &imageHTTPSpy{}
	raw, _ := json.Marshal(p)
	rw, _ := imageHTTPCall(t, spy, "POST", path, string(raw), "org")
	if rw.Code != 403 || spy.calls != 0 {
		t.Fatal("organization crossed human boundary")
	}
	spy = &imageHTTPSpy{finalFailure: true}
	rw, _ = imageHTTPCall(t, spy, "GET", "/v1/me/moments/"+imageMomentID+"/private-images/"+imageMomentID+"/content", "", "")
	if rw.Code != 401 || strings.Contains(rw.Body.String(), "synthetic derivative") {
		t.Fatal("encoded bytes escaped after final authorization failure")
	}
	for _, e := range []error{content.ErrUnavailable, content.ErrConflict, content.ErrNotFound, identity.ErrUnauthorized} {
		spy = &imageHTTPSpy{failure: e}
		rw, _ = imageHTTPCall(t, spy, "GET", "/v1/me/moments/"+imageMomentID+"/private-images", "", "")
		if rw.Code == 200 {
			t.Fatal("read failure masqueraded as empty success")
		}
	}
}
