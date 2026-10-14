package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

type profileCurrentHTTPAccess struct {
	*privateProfileHTTPAccess
	profile           identity.Profile
	access            identity.ProfileAccess
	readErr, finalErr error
	reads, finals     int
}

type profileCurrentNativeAfterRead struct {
	*postgres.Store
	after func()
}

func (s *profileCurrentNativeAfterRead) ReadProfileCurrent(ctx context.Context, a identity.ProfileAccess, id string) (identity.Profile, error) {
	p, e := s.Store.ReadProfileCurrent(ctx, a, id)
	if e == nil && s.after != nil {
		s.after()
	}
	return p, e
}

// Registered GET uses native Authenticate and native private proof. The hook
// changes actual committed sources after materialization, before encoding/final
// validation; it supplies no substitute authority or fabricated permission.
func TestCurrentProfileHTTPNativeAfterReadWithdrawal(t *testing.T) {
	for _, mode := range []string{"profile_private", "profile_ABA", "reverse_block", "session_revoke", "session_ABA"} {
		t.Run(mode, func(t *testing.T) {
			f := privateProfileHTTPDBNew(t)
			f.exec(`UPDATE user_profiles SET visibility='public',bio='HTTP_NATIVE_PROFILE_CURRENT_BODY_CANARY' WHERE account_id=$1`, f.accountIDs[0])
			token := f.tokens[1]
			digest, e := identity.ParseBearer("Bearer " + token)
			if e != nil {
				t.Fatal(e)
			}
			access := &profileCurrentNativeAfterRead{Store: f.store}
			access.after = func() {
				switch mode {
				case "profile_private":
					f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.accountIDs[0])
				case "profile_ABA":
					f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.accountIDs[0])
					f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.accountIDs[0])
				case "reverse_block":
					if e = f.store.BlockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]); e != nil {
						t.Fatal(e)
					}
				case "session_revoke":
					if e = f.store.RevokeSession(f.ctx, digest); e != nil {
						t.Fatal(e)
					}
				case "session_ABA":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:])
					f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, digest[:])
				}
			}
			h := privateProfileHTTPNew(f.store, access)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", token, ""))
			want := 404
			if mode == "profile_ABA" || mode == "session_ABA" {
				want = 409
			}
			if mode == "session_revoke" {
				want = 401
			}
			if w.Code != want || strings.Contains(w.Body.String(), "HTTP_NATIVE_PROFILE_CURRENT_BODY_CANARY") || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("actual final source withdrawal status=%d expected=%d body=%s", w.Code, want, w.Body.String())
			}
		})
	}
}

func (s *profileCurrentHTTPAccess) ReadProfile(context.Context, string, string) (identity.Profile, error) {
	panic("native Profile must use current typed port")
}
func (s *profileCurrentHTTPAccess) ReadProfileCurrent(_ context.Context, a identity.ProfileAccess, _ string) (identity.Profile, error) {
	s.reads++
	s.access = a
	return s.profile, s.readErr
}
func (s *profileCurrentHTTPAccess) ValidateProfileCurrent(ctx context.Context, a identity.ProfileAccess, id string, p identity.Profile) error {
	s.finals++
	if ctx.Err() != nil || a.Actor.ID != s.access.Actor.ID || a.SessionDigest != s.access.SessionDigest || id != p.AccountID || p.ResponseProof != s.profile.ResponseProof {
		return identity.ErrProfileUnavailable
	}
	return s.finalErr
}

type profileLegacyHTTPAccess struct {
	*privateProfileHTTPAccess
	profile identity.Profile
}

func (s *profileLegacyHTTPAccess) ReadProfile(context.Context, string, string) (identity.Profile, error) {
	return s.profile, nil
}
func profileCurrentHTTPFixture(t *testing.T) (http.Handler, *profileCurrentHTTPAccess, string) {
	t.Helper()
	_, a, _, token := privateProfileHTTPFixture(t)
	p := identity.Profile{AccountID: privateProfileHTTPForeign, DisplayName: "可见资料", Bio: "仅已获准的人类资料", Visibility: "public", ResponseProof: &struct{}{}}
	s := &profileCurrentHTTPAccess{privateProfileHTTPAccess: a, profile: p}
	return privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, s), s, token
}
func TestCurrentProfileHTTPFiveAudiences(t *testing.T) {
	for _, scope := range []string{"public", "followers", "connections", "close", "private"} {
		t.Run(scope, func(t *testing.T) {
			h, s, token := profileCurrentHTTPFixture(t)
			s.profile.Visibility = scope
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", "/v1/accounts/"+s.profile.AccountID+"/profile", "", token, ""))
			if w.Code != 200 || s.reads != 1 || s.finals != 1 || s.access.Actor.ID != privateProfileHTTPOwner || s.access.SessionDigest != s.digest {
				t.Fatalf("typed current read/final guard status=%d reads=%d finals=%d", w.Code, s.reads, s.finals)
			}
			var body map[string]json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body) != 1 {
				t.Fatal("original wrapper changed")
			}
			var p map[string]json.RawMessage
			if json.Unmarshal(body["data"], &p) != nil || len(p) != 4 || p["ResponseProof"] != nil || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private proof escaped or original four-field DTO changed")
			}
		})
	}
}
func TestCurrentProfileHTTPFinalWithdrawalStopsPayload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"source changed", identity.ErrConflict, 409}, {"withdrawn", identity.ErrNotFound, 404},
		{"session revoked", identity.ErrUnauthorized, 401}, {"native unavailable", identity.ErrProfileUnavailable, 503},
		{"unexpected native error", errors.New("PRIVATE_NATIVE_ERROR_CANARY"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, s, token := profileCurrentHTTPFixture(t)
			s.finalErr = tc.err
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", "/v1/accounts/"+s.profile.AccountID+"/profile", "", token, ""))
			if w.Code != tc.status || s.finals != 1 || strings.Contains(w.Body.String(), s.profile.Bio) || strings.Contains(w.Body.String(), "PRIVATE_NATIVE_ERROR_CANARY") {
				t.Fatalf("no stale body on failed final guard: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestCurrentProfileHTTPAnonymousIsBoundToAnonymous(t *testing.T) {
	h, s, _ := profileCurrentHTTPFixture(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("GET", "/v1/accounts/"+s.profile.AccountID+"/profile", "", "", ""))
	if w.Code != 200 || s.access.Actor.ID != "" || s.access.SessionDigest != ([32]byte{}) || s.calls != 0 || s.finals != 1 {
		t.Fatal("anonymous read reused authenticated identity")
	}
}
func TestCurrentProfileHTTPLegacyCannotAssertNewAudience(t *testing.T) {
	for _, scope := range []string{"public", "private", "followers", "connections", "close", "unknown"} {
		t.Run(scope, func(t *testing.T) {
			_, a, _, token := privateProfileHTTPFixture(t)
			s := &profileLegacyHTTPAccess{privateProfileHTTPAccess: a, profile: identity.Profile{AccountID: privateProfileHTTPForeign, Visibility: scope}}
			h := privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, s)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", "/v1/accounts/"+s.profile.AccountID+"/profile", "", token, ""))
			want := 503
			if scope == "public" || scope == "private" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("legacy new scope status=%d", w.Code)
			}
		})
	}
}
func TestCurrentProfileHTTPRejectsMismatchAndCancellation(t *testing.T) {
	for _, mode := range []string{"wrong target", "unknown audience", "cancelled", "invalid bearer"} {
		t.Run(mode, func(t *testing.T) {
			h, s, token := profileCurrentHTTPFixture(t)
			path := "/v1/accounts/" + s.profile.AccountID + "/profile"
			if mode == "wrong target" {
				s.profile.AccountID = privateProfileHTTPOwner
			}
			if mode == "unknown audience" {
				s.profile.Visibility = "PUBLIC"
			}
			r := privateProfileHTTPRequest("GET", path, "", token, "")
			if mode == "cancelled" {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			if mode == "invalid bearer" {
				r.Header.Set("Authorization", "Bearer invalid")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			want := 503
			if mode == "invalid bearer" {
				want = 401
			}
			if w.Code != want || s.finals != 0 || strings.Contains(w.Body.String(), s.profile.Bio) {
				t.Fatalf("invalid request leaked payload status=%d", w.Code)
			}
		})
	}
}
