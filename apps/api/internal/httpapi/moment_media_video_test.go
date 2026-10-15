package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
)

type videoBoundaryStore struct {
	humanMomentLegacySpy
	calls int
}

func (s *videoBoundaryStore) MomentMediaVideoAvailable() bool { return true }
func (s *videoBoundaryStore) PrepareMomentMediaVideo(context.Context, mm.Access, mm.VideoStageInput) (mm.StageReceipt, error) {
	s.calls++
	return mm.StageReceipt{}, mm.ErrUnavailable
}
func (s *videoBoundaryStore) ReadMomentVideoStageContent(context.Context, mm.Access, string, int64, string, func(mm.Stage, []byte) ([]byte, error)) ([]byte, error) {
	return nil, mm.ErrUnavailable
}

type videoCountingReader struct{ calls int }

func (r *videoCountingReader) Read(p []byte) (int, error) {
	r.calls++
	return strings.NewReader("{}").Read(p)
}

func TestPublicMomentVideoHTTPPurposeAndReaderAdmission(t *testing.T) {
	token, _, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	spy := &videoBoundaryStore{}
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	s := &server{access: access, content: spy}
	path := "/v1/me/moment-media-video-materials/operations/" + imageOperationID
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", path, strings.NewReader(body))
		req.SetPathValue("operationID", imageOperationID)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.prepareMomentMediaVideo(w, req)
		return w
	}
	raw := make([]byte, 24)
	copy(raw[4:8], "ftyp")
	in := map[string]any{"operationId": imageOperationID, "localScope": imageMomentID, "purpose": "HUMAN_PRIVATE_VIDEO_MATERIAL", "contentType": "video/mp4", "contentBase64": base64.StdEncoding.EncodeToString(raw)}
	body, _ := json.Marshal(in)
	if w := call(string(body)); w.Code != 400 || spy.calls != 0 {
		t.Fatal("private purpose crossed public material boundary", w.Code)
	}
	in["purpose"] = mm.VideoPurpose
	body, _ = json.Marshal(in)
	if w := call(string(body)); w.Code != 503 || spy.calls != 1 {
		t.Fatal("fixed public video purpose did not reach owned port", w.Code)
	}
	_, release, e := mm.TryVideoWork(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	reader := &videoCountingReader{}
	req := httptest.NewRequest("PUT", path, reader)
	req.SetPathValue("operationID", imageOperationID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.prepareMomentMediaVideo(w, req)
	if w.Code != http.StatusServiceUnavailable || reader.calls != 0 || spy.calls != 1 {
		t.Fatal("admission occurred after raw reader or decoder", w.Code, reader.calls)
	}
}

type videoStageContentStore struct {
	videoBoundaryStore
	t        *testing.T
	reads    int
	bodies   map[string][]byte
	failRead bool
}

func (s *videoStageContentStore) ReadMomentVideoStageContent(ctx context.Context, a mm.Access, id string, version int64, variant string, encode func(mm.Stage, []byte) ([]byte, error)) ([]byte, error) {
	s.reads++
	frame, valid, _ := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	if !valid || frame.Generation != 1 || a.AccountID != imageMomentID || a.AccountType != "person" || id != imageOperationID || version != 1 {
		s.t.Fatal("registered stage read lost its original owner Session or selector")
	}
	if s.failRead {
		return nil, mm.ErrChanged
	}
	stage := mm.Stage{
		DisplayBytes: len(s.bodies["display"]), DisplaySHA: mm.DigestBytes(s.bodies["display"]),
		ThumbnailBytes: len(s.bodies["thumbnail"]), ThumbnailSHA: mm.DigestBytes(s.bodies["thumbnail"]),
		Video: &mm.VideoContract{Bytes: len(s.bodies["video"]), SHA256: mm.DigestBytes(s.bodies["video"])},
	}
	return encode(stage, s.bodies[variant])
}

// This tests the registered route and byte envelope, not native ownership or a
// codec. Those checks remain in the existing native reader and codec tests.
func TestPublicMomentVideoHTTPRegisteredStageContentSelectors(t *testing.T) {
	token, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	spy := &videoStageContentStore{t: t, bodies: map[string][]byte{
		"display":   []byte("synthetic canonical display"),
		"thumbnail": []byte("synthetic canonical thumbnail"),
		"video":     []byte("synthetic canonical video"),
	}}
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	s := &server{access: access, content: spy}
	mux := http.NewServeMux()
	s.registerMomentMediaVideoRoutes(mux)
	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	base := "/v1/me/moment-media-materials/" + imageOperationID + "/"
	for _, variant := range []string{"thumbnail", "display", "video"} {
		w := call(base + "1/" + variant)
		mime := "image/jpeg"
		if variant == "video" {
			mime = "video/mp4"
		}
		if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), spy.bodies[variant]) || w.Header().Get("Content-Type") != mime || w.Header().Get("Content-Length") != strconv.Itoa(len(spy.bodies[variant])) || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("registered %s stage route rejected its numeric version or returned a wrong byte envelope: %d", variant, w.Code)
		}
	}
	if spy.reads != 3 {
		t.Fatal("valid variants did not reach the owned reader")
	}
	for _, suffix := range []string{"01/thumbnail", "0/thumbnail", "-1/thumbnail", "2/thumbnail", "1/poster", "1/thumbnail?"} {
		if w := call(base + suffix); w.Code != http.StatusBadRequest || spy.reads != 3 {
			t.Fatal("invalid stage selector reached the owned reader", suffix, w.Code)
		}
	}
	if w := call("/v1/me/moment-media-materials/not-a-media-id/1/thumbnail"); w.Code != http.StatusBadRequest || spy.reads != 3 {
		t.Fatal("invalid media identity reached the owned reader", w.Code)
	}
	spy.failRead = true
	if w := call(base + "1/thumbnail"); w.Code != http.StatusConflict || strings.Contains(w.Body.String(), string(spy.bodies["thumbnail"])) {
		t.Fatal("changed native source leaked stage bytes", w.Code)
	}
}
