package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestCommunityInterestHTTPRegisteredUnavailableHasNoFallback(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, path := range []string{"/v1/me/community-interests", "/v1/me/community-interests/options"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
