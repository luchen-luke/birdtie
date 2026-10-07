package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestParticipationDisclosureHTTPRegisteredUnavailable(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, path := range []string{"/v1/me/activity-participation-disclosures", "/v1/me/activity-participation-disclosures/options", "/v1/me/activity-participation-disclosures/preview", "/v1/me/activity-participation-disclosures/approve"} {
		method := "GET"
		if path[len(path)-7:] == "preview" || path[len(path)-7:] == "approve" {
			method = "POST"
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
