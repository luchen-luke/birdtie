package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sponsorWirePerson = "22222222-2222-4222-8222-222222222222"

type sponsorWireAuth struct {
	identity.AccessStore
	actor identity.Actor
	err   error
}

func (a sponsorWireAuth) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return a.actor, a.err
}

type sponsorWireStore struct {
	foundation.PublicCatalog
	calls  int
	fail   error
	out    so.Disclosure
	mutate func()
}

func (s *sponsorWireStore) SubmitSponsoredOpportunity(context.Context, so.Access, string, so.SubmitInput) (so.Declaration, bool, error) {
	s.calls++
	return so.Declaration{}, false, so.ErrForbidden
}
func (s *sponsorWireStore) ListOwnSponsoredOpportunities(context.Context, so.Access, string) (so.Management, error) {
	s.calls++
	return so.Management{}, so.ErrForbidden
}
func (s *sponsorWireStore) ListSponsoredOpportunityReview(context.Context, so.Access, string) ([]so.Declaration, error) {
	s.calls++
	return nil, so.ErrForbidden
}
func (s *sponsorWireStore) ReviewSponsoredOpportunity(context.Context, so.Access, string, so.ReviewInput) (so.Declaration, error) {
	s.calls++
	return so.Declaration{}, so.ErrForbidden
}
func (s *sponsorWireStore) RevokeSponsoredOpportunity(context.Context, so.Access, string, so.ReviewInput) (so.Declaration, error) {
	s.calls++
	return so.Declaration{}, so.ErrForbidden
}
func (s *sponsorWireStore) ReadSponsoredOpportunities(context.Context, so.Access, []so.Target) (so.Disclosure, error) {
	s.calls++
	if s.mutate != nil {
		s.mutate()
	}
	return s.out, s.fail
}
func TestSponsoredHTTPStrictTransportAndPermissionErrors(t *testing.T) {
	for _, name := range []string{"duplicate", "case alias", "null", "unknown", "actor org", "workspace", "missing identity", "too large"} {
		t.Run(name, func(t *testing.T) {
			store := &sponsorWireStore{}
			auth := sponsorWireAuth{actor: identity.Actor{ID: sponsorWirePerson, AccountType: "person"}}
			s := &server{catalog: store, access: auth}
			r := httptest.NewRequest("POST", "/v1/businesses/"+sponsorWirePerson+"/sponsored-opportunities", strings.NewReader(`{}`))
			r.SetPathValue("businessID", sponsorWirePerson)
			r.Header.Set("Content-Type", "application/json")
			// Canonical bearer syntax only; this transport spy creates no native
			// session and does not prove an authenticated business permission.
			token, _, err := identity.NewToken()
			if err != nil {
				t.Fatal(err)
			}
			r.Header.Set("Authorization", "Bearer "+token)
			want := 400
			body := `{"expectedRevision":1,"snapshot":"` + strings.Repeat("a", 64) + `","decision":"approve","note":"合成审核","confirmed":true}`
			switch name {
			case "duplicate":
				body = strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":false`, 1)
			case "case alias":
				body = strings.Replace(body, `"confirmed"`, `"Confirmed"`, 1)
			case "null":
				body = strings.Replace(body, `"confirmed":true`, `"confirmed":null`, 1)
			case "unknown":
				body = strings.TrimSuffix(body, "}") + `,"permission":true}`
			case "actor org":
				auth.actor.AccountType = "organization"
				s.access = auth
				want = 403
			case "workspace":
				r.Header.Set("X-Birdtie-Organization-Workspace", sponsorWirePerson)
			case "missing identity":
				r.Header.Del("Authorization")
				want = 401
			case "too large":
				body = strings.Repeat("x", so.MaxBodyBytes+1)
			}
			r.Body = http.NoBody
			if name == "actor org" || name == "workspace" || name == "missing identity" {
				r.Body = ioBody(body)
			} else {
				r.Body = ioBody(body)
			}
			r.SetPathValue("declarationID", sponsorWirePerson)
			w := httptest.NewRecorder()
			s.reviewSponsoredOpportunity(w, r)
			if w.Code != want || store.calls != 0 {
				t.Fatal(name, w.Code, w.Body.String(), store.calls)
			}
			if strings.Contains(w.Body.String(), "rightsNote") {
				t.Fatal("private error leak")
			}
		})
	}
	for _, e := range []error{so.ErrInvalid, so.ErrForbidden, so.ErrConflict, so.ErrUnavailable, so.ErrNotFound, identity.ErrUnauthorized, errors.New("PRIVATE_PROVIDER_SECRET")} {
		w := httptest.NewRecorder()
		sponsorFailure(w, e)
		if strings.Contains(w.Body.String(), "PRIVATE_PROVIDER_SECRET") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Body.String())
		}
	}
}

type sponsorTestBody struct{ *strings.Reader }

func (sponsorTestBody) Close() error  { return nil }
func ioBody(s string) sponsorTestBody { return sponsorTestBody{strings.NewReader(s)} }
func TestSponsoredHTTPRevalidationRejectsNewOrWithdrawnLaneWithoutPanic(t *testing.T) {
	at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	target := so.Target{Type: "ACTIVITY", ID: sponsorWirePerson, Title: "合成活动"}
	p := so.Public{ID: sponsorWirePerson, Revision: 2, Kind: "SPONSORED", Label: "赞助", Sponsor: so.Sponsor{Type: "BUSINESS", ID: sponsorWirePerson, Name: "合成商家"}, Target: target, Source: so.Source{URL: "https://qa.example", ObservedAt: at, ReviewedAt: at.Add(time.Minute), ExpiresAt: at.Add(time.Hour)}, CheckedAt: at.Add(2 * time.Minute)}
	for _, name := range []string{"new", "withdrawn", "wrong label", "different revision", "fresh clock"} {
		t.Run(name, func(t *testing.T) {
			old := so.Empty(true)
			old.SponsoredOpportunities = []so.Public{p}
			current := so.Empty(true)
			current.SponsoredOpportunities = []so.Public{p}
			switch name {
			case "new":
				old.SponsoredOpportunities = []so.Public{}
			case "withdrawn":
				current.SponsoredOpportunities = []so.Public{}
			case "wrong label":
				current.SponsoredOpportunities[0].Label = "推荐"
			case "different revision":
				current.SponsoredOpportunities[0].Revision = 3
			case "fresh clock":
				current.SponsoredOpportunities[0].CheckedAt = at.Add(3 * time.Minute)
			}
			store := &sponsorWireStore{out: current}
			s := &server{catalog: store}
			e := s.validateCommercial(httptest.NewRequest("GET", "/", nil), so.Access{}, old, []so.Target{target})
			if name == "fresh clock" && e != nil || name != "fresh clock" && e == nil {
				t.Fatal(name, e)
			}
			raw, _ := json.Marshal(current)
			if strings.Contains(string(raw), "reviewedBy") {
				t.Fatal("private wire")
			}
		})
	}
}
