package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentplacememory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const placeMemoryHTTPID = "27000000-0000-4000-8000-000000000001"

type placeMemoryHTTPSpy struct {
	foundation.PublicCatalog
	p                    agentplacememory.Projection
	record               agentmemory.Record
	calls, revalidations int
	err, finalErr        error
}

func (s *placeMemoryHTTPSpy) ReadOwnPlaceMemory(context.Context, agentprofile.PrivateAccess, string) (agentplacememory.Projection, error) {
	s.calls++
	return s.p, s.err
}
func (s *placeMemoryHTTPSpy) RevalidateOwnPlaceMemory(context.Context, agentprofile.PrivateAccess, agentplacememory.Projection) error {
	s.revalidations++
	return s.finalErr
}
func (s *placeMemoryHTTPSpy) PutOwnPlaceDeclaration(context.Context, agentprofile.PrivateAccess, string, agentplacememory.PutDeclarationInput) (agentmemory.Record, error) {
	s.calls++
	return s.record, s.err
}
func (s *placeMemoryHTTPSpy) PutOwnPlaceDeclarationBound(ctx context.Context, a agentprofile.PrivateAccess, agent, id string, in agentplacememory.PutDeclarationInput) (agentmemory.Record, error) {
	if agent != s.p.AgentID {
		return agentmemory.Record{}, agentplacememory.ErrForbidden
	}
	return s.PutOwnPlaceDeclaration(ctx, a, id, in)
}
func (s *placeMemoryHTTPSpy) DeleteOwnPlaceDeclaration(context.Context, agentprofile.PrivateAccess, string, int64) (agentmemory.Record, error) {
	s.calls++
	return s.record, s.err
}
func placeMemoryHTTPUnit(t *testing.T) (*server, *placeMemoryHTTPSpy, *privateProfileHTTPAccess, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Add(-time.Millisecond).Truncate(time.Microsecond)
	p := agentplacememory.Projection{SchemaVersion: agentplacememory.Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}, AgentID: privateProfileHTTPAgent, PlaceID: placeMemoryHTTPID, CityID: "local-city", AuthorityDigest: strings.Repeat("a", 64), TargetDigest: strings.Repeat("b", 64), Signals: []agentplacememory.Signal{}, ObservedAt: now, ExpiresAt: now.Add(time.Minute), VerifiedVisit: "UNAVAILABLE", Attendance: "UNAVAILABLE", ProcessingStatus: "UNAVAILABLE"}
	p.SnapshotID = agentplacememory.SnapshotID(p)
	spy := &placeMemoryHTTPSpy{p: p}
	a := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: d}
	return &server{catalog: spy, access: a}, spy, a, token
}
func placeMemoryHTTPServe(s *server, method string, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.SetPathValue("placeID", placeMemoryHTTPID)
	r.SetPathValue("memoryID", memoryHTTPID)
	switch method {
	case "GET":
		s.getOwnPlaceMemory(w, r)
	case "PUT":
		s.putOwnPlaceDeclaration(w, r)
	case "DELETE":
		s.deleteOwnPlaceDeclaration(w, r)
	}
	return w
}
func TestPlaceMemoryHTTPStrictAuthentication(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		for _, name := range []string{"anon", "duplicateAuth", "expired", "orgActor", "businessActor", "emptyWorkspace", "query", "forceQuery", "missingPort"} {
			t.Run(method+"/"+name, func(t *testing.T) {
				s, spy, a, token := placeMemoryHTTPUnit(t)
				r := privateProfileHTTPRequest(method, "/", "{}", token, "application/json")
				want := 400
				switch name {
				case "anon":
					r.Header.Del("Authorization")
					want = 401
				case "duplicateAuth":
					r.Header.Add("Authorization", r.Header.Get("Authorization"))
					want = 401
				case "expired":
					a.err = identity.ErrUnauthorized
					want = 401
				case "orgActor":
					a.actor.AccountType = "organization"
					want = 403
				case "businessActor":
					a.actor.AccountType = "business"
					want = 403
				case "emptyWorkspace":
					r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
					want = 403
				case "query":
					r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign
				case "forceQuery":
					r.URL.ForceQuery = true
				case "missingPort":
					s.catalog = nil
					want = 503
				}
				w := placeMemoryHTTPServe(s, method, r)
				memoryHTTPError(t, w, want)
				if spy.calls != 0 || spy.revalidations != 0 {
					t.Fatal("invalid transport reached native source")
				}
			})
		}
	}
}
func TestPlaceMemoryHTTPReadClosedProjectionAndFinalChecks(t *testing.T) {
	for _, name := range []string{"valid", "body", "wrongOwner", "wrongPlace", "sourceChanged", "expiredLease", "finalSession"} {
		t.Run(name, func(t *testing.T) {
			s, spy, a, token := placeMemoryHTTPUnit(t)
			r := privateProfileHTTPRequest("GET", "/", "", token, "application/json")
			want := 200
			switch name {
			case "body":
				r.Body = http.NoBody
				r.Body = ioBodyForPlace("{}")
				want = 400
			case "wrongOwner":
				spy.p.Owner.ID = privateProfileHTTPForeign
				spy.p.SnapshotID = agentplacememory.SnapshotID(spy.p)
				want = 503
			case "wrongPlace":
				spy.p.PlaceID = memoryHTTPID
				spy.p.SnapshotID = agentplacememory.SnapshotID(spy.p)
				want = 503
			case "sourceChanged":
				spy.finalErr = agentplacememory.ErrConflict
				want = 409
			case "expiredLease":
				spy.finalErr = agentplacememory.ErrExpired
				want = 409
			case "finalSession":
				s.access = &placeHTTPFinalAuth{privateProfileHTTPAccess: a}
				want = 401
			}
			w := placeMemoryHTTPServe(s, "GET", r)
			if w.Code != want {
				t.Fatalf("status %d want %d", w.Code, want)
			}
			for _, private := range []string{"authorityDigest", "targetDigest", "snapshotId", "processingStatus", "structuredValue", "latitude", "longitude", "body", "summary"} {
				if strings.Contains(w.Body.String(), `"`+private+`"`) {
					t.Fatal("closed human payload leaked", private)
				}
			}
			if want == 200 {
				var v struct{ Data humanPlaceMemory }
				if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Data.PlaceID != placeMemoryHTTPID || v.Data.Signals == nil || spy.revalidations != 1 {
					t.Fatal("current read/revalidation missing")
				}
			}
		})
	}
}

type placeHTTPFinalAuth struct{ *privateProfileHTTPAccess }

func (a *placeHTTPFinalAuth) ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error {
	return identity.ErrUnauthorized
}
func ioBodyForPlace(s string) io.ReadCloser { return io.NopCloser(strings.NewReader(s)) }
func TestPlaceMemoryHTTPStrictBodies(t *testing.T) {
	now := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	base := `{"agentId":"` + privateProfileHTTPAgent + `","expectedVersion":0,"placeId":"` + placeMemoryHTTPID + `","kind":"LIKED","visibility":"PRIVATE","validUntil":"` + now + `"}`
	for _, body := range []string{`{}`, `null`, base + `{}`, strings.Replace(base, `"kind":"LIKED"`, `"kind":"LIKED","kind":"VISITED"`, 1), strings.Replace(base, `"kind":"LIKED"`, `"Kind":"LIKED"`, 1), strings.Replace(base, `"kind":"LIKED"`, `"kind":"ATTENDED_ACTIVITY_AT"`, 1), strings.Replace(base, `"visibility":"PRIVATE"`, `"visibility":"PUBLIC"`, 1), strings.Replace(base, `"expectedVersion":0`, `"expectedVersion":null`, 1), strings.Replace(base, `"kind":"LIKED"`, `"kind":"LIKED","confirmed":true`, 1), strings.Replace(base, now, "2026-13-32T25:00:00Z", 1)} {
		s, spy, _, token := placeMemoryHTTPUnit(t)
		w := placeMemoryHTTPServe(s, "PUT", privateProfileHTTPRequest("PUT", "/", body, token, "application/json"))
		memoryHTTPError(t, w, 400)
		if spy.calls != 0 {
			t.Fatal("malformed declaration reached Store")
		}
	}
	for _, body := range []string{`{"expectedVersion":null}`, `{"ExpectedVersion":1}`, `{"expectedVersion":1,"expectedVersion":1}`, `{"expectedVersion":1,"ownerId":"x"}`, `{"expectedVersion":1} {}`} {
		s, spy, _, token := placeMemoryHTTPUnit(t)
		w := placeMemoryHTTPServe(s, "DELETE", privateProfileHTTPRequest("DELETE", "/", body, token, "application/json"))
		memoryHTTPError(t, w, 400)
		if spy.calls != 0 {
			t.Fatal("invalid withdrawal reached Store")
		}
	}
	for _, method := range []string{"PUT", "DELETE"} {
		s, spy, _, token := placeMemoryHTTPUnit(t)
		w := placeMemoryHTTPServe(s, method, privateProfileHTTPRequest(method, "/", strings.Repeat("x", 2049), token, "application/json"))
		memoryHTTPError(t, w, 413)
		if spy.calls != 0 {
			t.Fatal("oversized request reached Store")
		}
	}
}
func TestPlaceMemoryHTTPFailureClasses(t *testing.T) {
	for _, c := range []struct {
		e      error
		status int
	}{{agentplacememory.ErrInvalid, 400}, {agentplacememory.ErrForbidden, 403}, {agentplacememory.ErrNotFound, 404}, {agentplacememory.ErrConflict, 409}, {agentplacememory.ErrExpired, 409}, {errors.New("PRIVATE_HTTP_NO_DISCLOSURE_CANARY"), 503}} {
		w := httptest.NewRecorder()
		w.Header().Set("Cache-Control", "no-store")
		placeMemoryFailure(w, c.e)
		memoryHTTPError(t, w, c.status)
	}
}

// Transport does not own PostgreSQL's clock. These are transport-spy cases;
// the native pool-wait tests separately exercise current database expiry/ACL.
func TestPlaceMemoryHTTPUsesNativeClockAndFinalAuthority(t *testing.T) {
	for _, tc := range []struct {
		name     string
		offset   time.Duration
		finalErr error
		want     int
	}{
		{"database_ahead", 3 * time.Minute, nil, 200},
		{"database_behind", -3 * time.Minute, nil, 200},
		{"database_ahead_expired_native", 3 * time.Minute, agentplacememory.ErrExpired, 409},
		{"database_behind_revoked_native", -3 * time.Minute, agentplacememory.ErrForbidden, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, spy, _, token := placeMemoryHTTPUnit(t)
			spy.p.ObservedAt = time.Now().UTC().Add(tc.offset).Truncate(time.Microsecond)
			spy.p.ExpiresAt = spy.p.ObservedAt.Add(time.Minute)
			spy.finalErr = tc.finalErr
			w := placeMemoryHTTPServe(s, "GET", privateProfileHTTPRequest("GET", "/", "", token, "application/json"))
			if w.Code != tc.want || spy.revalidations != 1 {
				t.Fatalf("native clock final authority status=%d want=%d revalidations=%d", w.Code, tc.want, spy.revalidations)
			}
			if tc.want != 200 && strings.Contains(w.Body.String(), `"signals"`) {
				t.Fatal("native expiry/revocation disclosed signals")
			}
		})
	}
	t.Run("invalid_native_structure", func(t *testing.T) {
		s, spy, _, token := placeMemoryHTTPUnit(t)
		spy.p.ExpiresAt = spy.p.ObservedAt
		w := placeMemoryHTTPServe(s, "GET", privateProfileHTTPRequest("GET", "/", "", token, "application/json"))
		if w.Code != 503 || spy.revalidations != 0 {
			t.Fatal("invalid projection reached final native authority")
		}
	})
}
