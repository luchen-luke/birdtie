package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type videoHTTPRepo struct {
	media.VideoMaterialRepository
	calls   int
	actor   identity.Actor
	digest  [32]byte
	after   func()
	receipt media.VideoMaterialReceipt
}

func (s *videoHTTPRepo) hit(d [32]byte, a identity.Actor) {
	s.calls++
	s.actor = a
	s.digest = d
	if s.after != nil {
		s.after()
	}
}
func (s *videoHTTPRepo) ReserveHumanVideo(_ context.Context, d [32]byte, a identity.Actor, in media.VideoMaterialInput) (media.VideoMaterialRecord, error) {
	s.hit(d, a)
	v := s.receipt
	v.VideoMaterialInput = in
	return media.VideoMaterialRecord{Receipt: v, QuarantineKey: "opaque-only"}, nil
}
func (s *videoHTTPRepo) ListHumanVideos(_ context.Context, d [32]byte, a identity.Actor, _ string) (media.VideoMaterialPage, error) {
	s.hit(d, a)
	return media.VideoMaterialPage{Items: []media.VideoMaterialReceipt{}}, nil
}
func (s *videoHTTPRepo) QueueHumanVideo(_ context.Context, d [32]byte, a identity.Actor, _ string, _ int64) (media.VideoMaterialReceipt, error) {
	s.hit(d, a)
	v := s.receipt
	v.Status = "queued"
	return v, nil
}
func (s *videoHTTPRepo) ReadHumanVideo(_ context.Context, d [32]byte, a identity.Actor, _ string, _ bool, f func(media.VideoMaterialRecord) ([]byte, error)) ([]byte, error) {
	s.hit(d, a)
	return f(media.VideoMaterialRecord{Receipt: s.receipt})
}

type videoHTTPStorage struct{ media.VideoPrivateStorage }

func (videoHTTPStorage) SignQuarantineWrite(_ context.Context, in media.UploadIntent) (media.UploadTarget, error) {
	return media.UploadTarget{URL: "https://storage.invalid/synthetic", ExpiresAt: in.ExpiresAt}, nil
}
func videoHTTPHandler(access identity.AccessStore, repo *videoHTTPRepo, available bool) http.Handler {
	runtime := media.VideoRuntime{}
	if available {
		runtime = media.VideoRuntime{Storage: videoHTTPStorage{}, Scanner: &media.HTTPVideoScanner{}, Inspector: &media.FFmpegVideoInspector{}, ProviderLabel: "test", Region: "synthetic", UploadOrigin: "https://storage.invalid"}
	}
	s := &server{access: access, videoMaterials: media.NewVideoMaterialService(repo, runtime)}
	mux := http.NewServeMux()
	s.registerVideoMaterialRoutes(mux)
	return mux
}
func TestVideoMaterialHTTPExplicitPersonSessionAndUnavailable(t *testing.T) {
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	repo := &videoHTTPRepo{receipt: media.VideoMaterialReceipt{ID: imageMomentID, OwnerID: imageMomentID, Status: "quarantine", Revision: 1, UploadExpiresAt: time.Now().UTC().Add(time.Minute)}}
	runtime := media.VideoRuntime{Storage: videoHTTPStorage{}, Scanner: &media.HTTPVideoScanner{}, Inspector: &media.FFmpegVideoInspector{}, ProviderLabel: "test", Region: "synthetic", UploadOrigin: "https://storage.invalid"}
	in := media.VideoMaterialInput{OperationID: imageMomentID, MIME: "video/mp4", ByteSize: 100, SHA256: strings.Repeat("a", 64), DurationMS: 1000, Purpose: media.VideoMaterialPurpose, ConfirmedPrivateSave: true, StoragePolicyID: runtime.PolicyID()}
	raw, _ := json.Marshal(in)
	request := func(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request(videoHTTPHandler(access, repo, true), "POST", "/v1/me/video-materials", string(raw))
	if w.Code != 201 || repo.actor != access.actor || repo.digest != access.seen || strings.Contains(w.Body.String(), "opaque-only") {
		t.Fatal("trusted current source or private key leak", w.Code, w.Body.String())
	}
	w = request(videoHTTPHandler(access, repo, false), "POST", "/v1/me/video-materials", string(raw))
	if w.Code != 503 || repo.calls != 1 {
		t.Fatal("disabled provider write", w.Code, repo.calls)
	}
	access.actor.AccountType = "organization"
	w = request(videoHTTPHandler(access, repo, true), "GET", "/v1/me/video-materials", "")
	if w.Code != 403 || repo.calls != 1 {
		t.Fatal("organization context crossed private scope", w.Code)
	}
}
func TestVideoMaterialHTTPClosedDTOQueryCancellationAndAsyncStatus(t *testing.T) {
	token, _, _ := identity.NewToken()
	access := &humanMomentAccessSpy{actor: identity.Actor{ID: imageMomentID, AccountType: "person"}}
	repo := &videoHTTPRepo{receipt: media.VideoMaterialReceipt{ID: imageMomentID, Status: "quarantine"}}
	h := videoHTTPHandler(access, repo, true)
	for _, path := range []string{"/v1/me/video-materials?unknown=x", "/v1/me/video-materials?cursor=broken", "/v1/me/video-materials?cursor=" + imageMomentID + "&cursor=" + imageMomentID} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 || repo.calls != 0 {
			t.Fatal("unbounded query", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/v1/me/video-materials/"+imageMomentID+"/complete", strings.NewReader(`{"revision":1}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"status":"queued"`) || strings.Contains(w.Body.String(), "ready_private") {
		t.Fatal("queue called save success", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	repo.after = cancel
	r = httptest.NewRequest("GET", "/v1/me/video-materials", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Body.Len() != 0 {
		t.Fatal("late cancelled response leaked bytes", w.Body.String())
	}
}
