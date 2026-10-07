package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestMultiCandidateHTTPBodyRejectsBooleanAuthority(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if _, ok := multiCandidateBody(w, r, "analysisGrantIds", "retainUntil"); ok {
		t.Fatal("empty accepted")
	}
}
