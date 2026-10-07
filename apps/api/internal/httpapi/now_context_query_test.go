package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNowOnlineHTTPUnavailableHasNoLegacyFallback(t *testing.T) {
	s, _, _, token := privateProfileHTTPFixture(t)
	for _, x := range []struct {
		path, workspace string
		code            int
	}{
		{"/v1/me/now/online-contexts", "", 503}, {"/v1/me/now/online-contexts?owner=x", "", 400}, {"/v1/me/now/online-contexts?", "", 400}, {"/v1/me/now/online-contexts", "org", 403},
	} {
		t.Run(x.path+x.workspace, func(t *testing.T) {
			r := httptest.NewRequest("GET", x.path, strings.NewReader(""))
			r.Header.Set("Authorization", "Bearer "+token)
			if x.workspace != "" {
				r.Header.Set("X-Birdtie-Organization-Workspace", x.workspace)
			}
			w := httptest.NewRecorder()
			s.listOwnNowOnlineContexts(w, r)
			if w.Code != x.code {
				t.Fatalf("status%d want%d", w.Code, x.code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cache")
			}
		})
	}
}
