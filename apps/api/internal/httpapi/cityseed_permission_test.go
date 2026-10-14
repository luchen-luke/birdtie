package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type placePermissionHTTPSpy struct {
	cityseed.Store
	calls   int
	session cityseed.HumanSession
}

func (s *placePermissionHTTPSpy) Submit(ctx context.Context, actor, city string, in cityseed.SubmitInput) (cityseed.Candidate, error) {
	s.calls++
	s.session, _ = cityseed.CurrentHumanSession(ctx)
	return cityseed.Candidate{ID: cityActivityHTTPCandidate}, nil
}
func (s *placePermissionHTTPSpy) Review(ctx context.Context, actor, id string, in cityseed.ReviewInput) (cityseed.Candidate, error) {
	s.calls++
	s.session, _ = cityseed.CurrentHumanSession(ctx)
	return cityseed.Candidate{ID: id}, nil
}
func placePermissionHTTPFixture(t *testing.T) (*server, *placePermissionHTTPSpy, string) {
	t.Helper()
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: cityActivityHTTPActor, AccountType: "person"}, digest: digest}
	spy := &placePermissionHTTPSpy{}
	return &server{access: access, seed: spy}, spy, token
}
func placePermissionHTTPInput() cityseed.SubmitInput {
	now := time.Now().UTC().Add(-time.Minute)
	until := now.Add(24 * time.Hour)
	return cityseed.SubmitInput{Name: "Synthetic unit place", CategoryCode: "restaurant", Summary: "Synthetic transport fixture only", LocationPrecision: "none", SourceLabel: "Synthetic source", SourceURL: "https://example.invalid/place", RightsNote: "Synthetic directly authorized unit source", ExpiresAt: until, Rights: &cityseed.SourcePermissionInput{State: cityseed.PermissionConfirmed, Acquisition: "FIRST_PARTY", OriginProvider: "BIRDTIE_FIRST_PARTY", LicenseURL: "https://example.invalid/license", LicenseVersion: "synthetic-v1", Attribution: "Synthetic unit fixture", AcquiredAt: now, RetentionUntil: until}, Fields: []cityseed.PlaceFieldInput{{Key: "name", Value: json.RawMessage(`"Synthetic unit place"`), ObservedAt: now, ValidFrom: now, ValidTo: until, RefreshDue: until, ConflictState: "NONE"}}}
}
func TestPlaceSourcePermissionHTTPRejectsBeforeStore(t *testing.T) {
	for _, state := range []string{"MISSING", cityseed.PermissionPending, cityseed.PermissionProhibited, "CONFIRMED_RELABELED_WSA"} {
		s, spy, token := placePermissionHTTPFixture(t)
		in := placePermissionHTTPInput()
		switch state {
		case "MISSING":
			in.Rights = nil
		case "CONFIRMED_RELABELED_WSA":
			in.ProviderCode = "tencent_wsa"
		default:
			in.Rights.State = state
		}
		raw, _ := json.Marshal(in)
		r := privateProfileHTTPRequest(http.MethodPost, "/", string(raw), token, "application/json")
		r.SetPathValue("cityID", "aberdeen-gb")
		w := httptest.NewRecorder()
		s.submitPlaceCandidate(w, r)
		if w.Code != 422 || spy.calls != 0 {
			t.Fatalf("unconfirmed material reached store %s status=%d calls=%d", state, w.Code, spy.calls)
		}
	}
}
func TestPlaceSourcePermissionHTTPPassesNativeSessionAndReviewAcknowledgement(t *testing.T) {
	s, spy, token := placePermissionHTTPFixture(t)
	raw, _ := json.Marshal(placePermissionHTTPInput())
	r := privateProfileHTTPRequest(http.MethodPost, "/", string(raw), token, "application/json")
	r.SetPathValue("cityID", "aberdeen-gb")
	w := httptest.NewRecorder()
	s.submitPlaceCandidate(w, r)
	if w.Code != 201 || spy.calls != 1 || spy.session.ActorID != cityActivityHTTPActor || spy.session.Digest == ([32]byte{}) {
		t.Fatalf("current session not bound status=%d calls=%d", w.Code, spy.calls)
	}
	for _, body := range []string{`{"decision":"publish","note":"Independent human review","expectedPermissionVersion":1}`, `{"decision":"publish","note":"Independent human review","rightsConfirmed":true,"fieldsConfirmed":false,"expectedPermissionVersion":1}`} {
		r = privateProfileHTTPRequest(http.MethodPost, "/", body, token, "application/json")
		r.SetPathValue("candidateID", cityActivityHTTPCandidate)
		w = httptest.NewRecorder()
		s.reviewPlaceCandidate(w, r)
		if w.Code != 422 || spy.calls != 1 {
			t.Fatal("missing explicit review acknowledgement reached store")
		}
	}
	r = privateProfileHTTPRequest(http.MethodPost, "/", `{"decision":"revoke_permission","note":"Independent source permission revoked","permissionState":"PROHIBITED","expectedPermissionVersion":1}`, token, "application/json")
	r.SetPathValue("candidateID", cityActivityHTTPCandidate)
	w = httptest.NewRecorder()
	s.reviewPlaceCandidate(w, r)
	if w.Code != 200 || spy.calls != 2 {
		t.Fatal("bounded revocation transport failed", w.Code)
	}
}
