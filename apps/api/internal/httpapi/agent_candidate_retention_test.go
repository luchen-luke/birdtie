package httpapi

import (
	"errors"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCandidateRetentionHTTPClosedBodyAndErrors(t *testing.T) {
	for _, raw := range []string{`{}`, `{"expectedRevision":null}`, `{"expectedRevision":1,"confirmed":true}`, `{"expectedRevision":1,"expectedRevision":1}`, `{"expectedRevision":1} {}`, strings.Repeat("a", 8193)} {
		r := httptest.NewRequest("DELETE", "/", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, ok := candidateRetentionBody(w, r, "expectedRevision"); ok || w.Code != 400 {
			t.Fatal(raw, w.Code)
		}
	}
	for _, item := range []struct {
		e      error
		status int
	}{{acr.ErrInvalid, 400}, {acr.ErrDenied, 403}, {acr.ErrExpired, 409}, {acr.ErrConflict, 409}, {acr.ErrUnavailable, 503}, {errors.New("PRIVATE_DATABASE_CANARY"), 503}} {
		w := httptest.NewRecorder()
		candidateRetentionHTTPError(w, item.e)
		if w.Code != item.status || strings.Contains(w.Body.String(), "PRIVATE_DATABASE_CANARY") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"expectedRevision":1}`))
	r.Header.Set("Content-Type", "text/plain")
	if _, ok := candidateRetentionBody(w, r, "expectedRevision"); ok || w.Code != 415 {
		t.Fatal(w.Code)
	}
}
