package httpapi

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"net/http"
	"net/http/httptest"
	"testing"
)

type privateVideoHTTPSpy struct {
	humanMomentLegacySpy
	calls int
	part  string
}

func (s *privateVideoHTTPSpy) ReadHumanPrivateVideos(_ context.Context, _ [32]byte, _ identity.Actor, _ string, _ string, p string, f media.PrivateVideoEncode) ([]byte, error) {
	s.calls++
	s.part = p
	return f([]media.PrivateVideoReceipt{{ID: imageOperationID}}, []byte("synthetic-playback"))
}
func (s *privateVideoHTTPSpy) DeleteHumanPrivateVideo(context.Context, [32]byte, identity.Actor, string, string, int64) (media.PrivateVideoReceipt, error) {
	s.calls++
	return media.PrivateVideoReceipt{}, nil
}

// The registered route shape is exercised here without a separate server or
// Session fixture. Native integration covers real owner/read/delete gates.
func TestPrivateMomentVideoRoutesRegistered(t *testing.T) {
	s := &server{}
	mux := http.NewServeMux()
	s.registerPrivateMomentVideoRoutes(mux)
	for _, path := range []string{"/v1/me/moments/" + imageMomentID + "/videos", "/v1/me/moments/" + imageMomentID + "/videos/" + imageOperationID + "/content", "/v1/me/moments/" + imageMomentID + "/videos/" + imageOperationID + "/poster"} {
		req := httptest.NewRequest("GET", path, nil)
		_, pattern := mux.Handler(req)
		if pattern == "" {
			t.Fatal("native private video route absent")
		}
	}
}
