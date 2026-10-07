package httpapi

import (
	"errors"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnrichmentPurposeHTTPClosedBodyAndErrors(t *testing.T) {
	for _, raw := range []string{`{}`, `{"expectedRevision":null}`, `{"expectedRevision":1,"confirmed":true}`, `{"expectedRevision":1,"expectedRevision":1}`, `{"expectedRevision":1} {}`, strings.Repeat("a", 8193)} {
		r := httptest.NewRequest("DELETE", "/", strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		if _, ok := enrichmentPurposeBody(w, r, "expectedRevision"); ok || w.Code != 400 {
			t.Fatal(raw, w.Code)
		}
	}
	for _, item := range []struct {
		e      error
		status int
	}{{aep.ErrInvalid, 400}, {aep.ErrDenied, 403}, {aep.ErrExpired, 409}, {aep.ErrConflict, 409}, {aep.ErrUnavailable, 503}, {errors.New("PRIVATE_DATABASE_CANARY"), 503}} {
		w := httptest.NewRecorder()
		enrichmentPurposeHTTPError(w, item.e)
		if w.Code != item.status || strings.Contains(w.Body.String(), "PRIVATE_DATABASE_CANARY") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"expectedRevision":1}`))
	r.Header.Set("Content-Type", "text/plain")
	if _, ok := enrichmentPurposeBody(w, r, "expectedRevision"); ok || w.Code != 415 {
		t.Fatal(w.Code)
	}
}
func TestEnrichmentPurposeHTTPSelectionExact(t *testing.T) {
	a := aep.Selection{TaskID: "44000000-0000-4000-8000-000000000001", MomentID: "44000000-0000-4000-8000-000000000002", MomentRevision: 1, Fields: []string{"title", "body"}, DeadlineAt: time.Now().UTC().Truncate(time.Microsecond)}
	b := a
	b.Fields = []string{"body", "title"}
	if !sameEnrichmentSelection(a, b) {
		t.Fatal("canonical field order")
	}
	b.MomentRevision++
	if sameEnrichmentSelection(a, b) {
		t.Fatal("different source revision")
	}
}
