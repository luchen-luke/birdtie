package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestIntentConversionHTTPClosedQueryBeforeAuthorization(t *testing.T) {
	s := &server{}
	for _, path := range []string{"/v1/me/social-intents/00000000-0000-0000-0000-000000000000/activity-conversion?", "/v1/me/social-intents/00000000-0000-0000-0000-000000000000/activity-conversion?confirmed=true"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		s.getOwnIntentActivityConversion(w, r)
		if w.Code != 400 {
			t.Fatal("unknown query accepted", w.Code, w.Body.String())
		}
	}
}
