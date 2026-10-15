package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
