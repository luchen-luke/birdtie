package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	sc "github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sharedHistoryEncodingStore struct {
	*postgres.Store
	mutate func()
}

func (s *sharedHistoryEncodingStore) SharedSocialContextCurrent(ctx context.Context, a sc.CurrentAccess) (sc.Signals, sc.CurrentValidation, error) {
	v, c, e := s.Store.SharedSocialContextCurrent(ctx, a)
	if e != nil {
		return v, c, e
	}
	return v, func(ctx context.Context) error { s.mutate(); return c(ctx) }, nil
}
func TestSharedHistoryHTTPRegisteredWireAndAfterEncodingNativeGuard(t *testing.T) {
	for _, change := range []string{"none", "optout", "hiddenPlace", "block", "revoke"} {
		t.Run(change, func(t *testing.T) {
			f := intentPlacePlansNative(t)
			f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, f.accountIDs[:2])
			for i := 0; i < 2; i++ {
				f.request("POST", "/v1/activities/"+f.activity+"/participations", nil, i, 201, nil)
				f.request("PUT", "/v1/me/social-disclosure", sc.Disclosure{SharedActivities: true}, i, 200, nil)
			}
			before := f.publicDomainDigest()
			idBefore := f.identitySnapshot()
			path := "/v1/accounts/" + f.accountIDs[0] + "/shared-context"
			w := f.request("GET", path, nil, 1, 200, nil)
			var wire struct {
				Data sc.Signals `json:"data"`
			}
			decodeIntentPlacePlans(t, w, &wire)
			if wire.Data.ViewerID != f.accountIDs[1] || wire.Data.TargetID != f.accountIDs[0] || len(wire.Data.Activities) != 1 || wire.Data.Activities[0].ID != f.activity || len(wire.Data.Places) != 1 || wire.Data.Places[0].ID != f.place || wire.Data.Attendance != "UNKNOWN" || wire.Data.Visit != "UNKNOWN" {
				t.Fatal("real wire", w.Body.String())
			}
			t.Log("ACTN003 RAW_HTTP_JSON", w.Body.String())
			for _, bad := range []string{"latitude", "longitude", "participantAccountId", "token", "sessionDigest", "sourceVersion"} {
				if strings.Contains(w.Body.String(), bad) {
					t.Fatal("unnecessary private/proof wire", bad)
				}
			}
			if f.publicDomainDigest() != before || f.identitySnapshot() != idBefore {
				t.Fatal("GET changed domain or identity beyond legitimate session idle")
			}
			f.request("GET", path+"?confirmed=true", nil, 1, 400, nil)
			f.request("GET", path+"?", nil, 1, 400, nil)
			f.request("GET", path, nil, -1, 401, nil)
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Authorization", "Bearer "+f.tokens[1])
			r.Header.Set("X-Birdtie-Organization-Workspace", f.accountIDs[2])
			out := httptest.NewRecorder()
			f.handler.ServeHTTP(out, r)
			if out.Code != 403 {
				t.Fatal("workspace", out.Code)
			}
			if change == "none" {
				return
			}
			wrapped := &sharedHistoryEncodingStore{Store: f.store, mutate: func() {
				switch change {
				case "optout":
					if _, e := f.store.SetSocialDisclosure(f.ctx, f.accountIDs[0], sc.Disclosure{}); e != nil {
						t.Fatal(e)
					}
				case "hiddenPlace":
					f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
				case "block":
					f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[0], f.accountIDs[1])
				case "revoke":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[1])
				}
			}}
			f.handler = New(wrapped, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
			want := 409
			if change == "block" {
				want = 404
			}
			if change == "revoke" {
				want = 401
			}
			w = f.request("GET", path, nil, 1, want, nil)
			if strings.Contains(w.Body.String(), f.activity) || strings.Contains(w.Body.String(), f.place) {
				t.Fatal("encoded stale payload escaped final native guard")
			}
		})
	}
}

// A legacy-only Store is explicitly unavailable; it cannot silently bypass the
// current native Session/source fence even though the old read method exists.
type sharedHistoryLegacyOnly struct{ sc.Store }

func TestSharedHistoryHTTPNativeCapabilityUnavailable(t *testing.T) {
	f := intentPlacePlansNative(t)
	s := &server{access: f.store, socialContext: sharedHistoryLegacyOnly{Store: f.store}}
	r := httptest.NewRequest("GET", "/v1/accounts/"+f.accountIDs[0]+"/shared-context", nil)
	r.SetPathValue("accountID", f.accountIDs[0])
	r.Header.Set("Authorization", "Bearer "+f.tokens[1])
	w := httptest.NewRecorder()
	s.sharedSocialContext(w, r)
	if w.Code != 503 || json.Valid(w.Body.Bytes()) == false {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSharedHistoryHTTPPersonVenueWithdrawalDoesNotInventVerifiedVenue(t *testing.T) {
	for _, kind := range []string{"venueExpiry", "candidateExpiry", "candidateWithdraw"} {
		t.Run(kind, func(t *testing.T) {
			f := intentPlacePlansNative(t)
			f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=ANY($1::uuid[])`, f.accountIDs[:2])
			for i := 0; i < 2; i++ {
				f.request("POST", "/v1/activities/"+f.activity+"/participations", nil, i, 201, nil)
				f.request("PUT", "/v1/me/social-disclosure", sc.Disclosure{SharedActivities: true}, i, 200, nil)
			}
			digest, e := identity.ParseBearer("Bearer " + f.tokens[1])
			if e != nil {
				t.Fatal(e)
			}
			a := sc.CurrentAccess{ViewerID: f.accountIDs[1], TargetID: f.accountIDs[0], SessionDigest: digest}
			_, check, e := f.store.SharedSocialContextCurrent(f.ctx, a)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "venueExpiry":
				f.exec(`UPDATE venues SET expires_at=clock_timestamp()-interval '1 millisecond' WHERE place_id=$1`, f.place)
			case "candidateExpiry":
				f.exec(`UPDATE venue_candidates SET expires_at=clock_timestamp()-interval '1 millisecond' WHERE id=$1`, f.candidate)
			case "candidateWithdraw":
				f.exec(`UPDATE venue_candidates SET status='rejected' WHERE id=$1`, f.candidate)
			}
			if e = check(f.ctx); !errors.Is(e, sc.ErrChanged) {
				t.Fatal("current Venue source missing from closure", kind, e)
			}
			w := f.request("GET", "/v1/accounts/"+f.accountIDs[0]+"/shared-context", nil, 1, 200, nil)
			var v struct{ Data sc.Signals }
			decodeIntentPlacePlans(t, w, &v)
			if len(v.Data.Places) != 1 || v.Data.Places[0].ID != f.place {
				t.Fatal("public Place reference was confused with separate Venue authorization", w.Body.String())
			}
			for _, bad := range []string{"reservation", "verified", "approved", "venueStatus", "latitude", "longitude"} {
				if strings.Contains(w.Body.String(), bad) {
					t.Fatal("Place ref invented Venue fact", bad)
				}
			}
		})
	}
}
