package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Registered HTTP transport units, not native SQL or production authentication.
type requestListSessionUnitStore struct {
	connection.Store
	calls int
	owner string
	after func()
	err   error
	items []connection.Request
}

func (s *requestListSessionUnitStore) ListRequests(_ context.Context, owner string) ([]connection.Request, error) {
	if s == nil {
		panic("typed-nil Request store must not be invoked")
	}
	s.calls++
	s.owner = owner
	if s.after != nil {
		s.after()
	}
	return s.items, s.err
}

type requestListSessionUnitAccess struct {
	*privateProfileHTTPAccess
	initial, final int
	finalDigest    [32]byte
	finalActor     identity.Actor
	finalErr       error
	onFinal        func()
	afterFinal     func()
}

func (a *requestListSessionUnitAccess) AuthenticateHumanSocial(ctx context.Context, d [32]byte) (identity.Actor, error) {
	if a == nil {
		panic("typed-nil session store must not be invoked")
	}
	a.initial++
	return a.privateProfileHTTPAccess.Authenticate(ctx, d)
}
func (a *requestListSessionUnitAccess) ValidateHumanSocialResponse(ctx context.Context, d [32]byte, actor identity.Actor) error {
	if a == nil {
		panic("typed-nil session response must not be invoked")
	}
	a.final++
	a.finalDigest = d
	a.finalActor = actor
	if a.onFinal != nil {
		a.onFinal()
	}
	if a.finalErr != nil {
		return a.finalErr
	}
	err := a.privateProfileHTTPAccess.ValidateHumanSocialResponse(ctx, d, actor)
	if a.afterFinal != nil {
		a.afterFinal()
	}
	return err
}
func requestListSessionUnitFixture(t *testing.T) (http.Handler, *server, *requestListSessionUnitStore, *requestListSessionUnitAccess, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	a := &requestListSessionUnitAccess{privateProfileHTTPAccess: &privateProfileHTTPAccess{
		actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: d}}
	sp := &requestListSessionUnitStore{items: []connection.Request{{ID: privateProfileHTTPAgent,
		Direction: "incoming", OtherAccountID: privateProfileHTTPForeign, OtherName: "合成申请人",
		Note: "SESSION_FENCE_SYNTHETIC_CANARY", Scope: "friend", State: "pending", PolicyDisposition: "SCREEN", ScreeningStatus: "PENDING_REVIEW",
		CreatedAt: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2050, 10, 6, 12, 0, 0, 0, time.UTC)}}}
	var actual *server
	h := New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, sp, nil, false, nil, nil, nil, func(s *server) { actual = s })
	return h, actual, sp, a, token
}
func requestListSessionUnitRead(h http.Handler, token string, change func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/v1/me/connection-requests", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if change != nil {
		change(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestConnectionRequestsSessionUnitLateListMustNotPublish(t *testing.T) {
	for _, mode := range []string{"revoked", "actor-changed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			h, _, sp, a, token := requestListSessionUnitFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			status := 401
			sp.after = func() {
				switch mode {
				case "revoked":
					a.err = identity.ErrUnauthorized
				case "actor-changed":
					a.actor.ID = privateProfileHTTPForeign
				case "cancelled":
					cancel()
				}
			}
			if mode == "cancelled" {
				status = 503
			}
			w := requestListSessionUnitRead(h, token, func(r *http.Request) { *r = *r.WithContext(ctx) })
			if w.Code != status || strings.Contains(w.Body.String(), "SESSION_FENCE_SYNTHETIC_CANARY") || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatalf("late %s published original private Request list: status=%d body=%s", mode, w.Code, w.Body.String())
			}
			if sp.calls != 1 || sp.owner != privateProfileHTTPOwner {
				t.Fatal("original caller/Request store changed")
			}
		})
	}
}
func TestConnectionRequestsSessionUnitCurrentCapturedOriginalList(t *testing.T) {
	for _, mode := range []string{"screen", "empty", "header-changed"} {
		t.Run(mode, func(t *testing.T) {
			h, _, sp, a, token := requestListSessionUnitFixture(t)
			if mode == "empty" {
				sp.items = []connection.Request{}
			}
			var original *http.Request
			if mode == "header-changed" {
				sp.after = func() { original.Header.Set("Authorization", "Bearer ignored-after-capture") }
			}
			w := requestListSessionUnitRead(h, token, func(r *http.Request) { original = r })
			if w.Code != 200 || sp.calls != 1 || a.initial != 1 || a.final != 1 || a.finalDigest != a.digest || a.finalActor != a.actor || sp.owner != a.actor.ID {
				t.Fatalf("original bound current read failed %s: status=%d read=%d initial=%d final=%d", mode, w.Code, sp.calls, a.initial, a.final)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private list cached")
			}
			if mode == "empty" {
				if !strings.Contains(w.Body.String(), `"data":[]`) {
					t.Fatal("true empty list changed")
				}
			} else {
				for _, term := range []string{privateProfileHTTPAgent, "SCREEN", "PENDING_REVIEW", "2050-10-06"} {
					if !strings.Contains(w.Body.String(), term) {
						t.Fatal("original Request DTO lost", term)
					}
				}
			}
		})
	}
}
func TestConnectionRequestsSessionUnitDeniedOrUnavailableDoesNotRead(t *testing.T) {
	for _, mode := range []string{"anonymous", "bad-token", "initial-expired", "initial-error", "invalid-actor", "organization-actor", "workspace", "query", "force-query", "body", "initial-cancel", "nil-access", "typednil-access", "missing-current-port", "nil-store", "typednil-store"} {
		t.Run(mode, func(t *testing.T) {
			h, s, sp, a, token := requestListSessionUnitFixture(t)
			status := 403
			var change func(*http.Request)
			switch mode {
			case "anonymous":
				token = ""
				status = 401
			case "bad-token":
				token = "bad"
				status = 401
			case "initial-expired":
				a.err = identity.ErrUnauthorized
				status = 401
			case "initial-error":
				a.err = errors.New("UNIT_RAW_ERROR_CANARY")
				status = 503
			case "invalid-actor":
				a.actor.ID = "not-id"
			case "organization-actor":
				a.actor.AccountType = "organization"
			case "workspace":
				change = func(r *http.Request) { r.Header["X-Birdtie-Organization-Workspace"] = []string{""} }
			case "query":
				status = 400
				change = func(r *http.Request) { r.URL.RawQuery = "ownerId=foreign" }
			case "force-query":
				status = 400
				change = func(r *http.Request) { r.URL.ForceQuery = true }
			case "body":
				status = 400
				change = func(r *http.Request) {
					r.Body = http.NoBody
					r.Body = &requestListUnitBody{Reader: strings.NewReader(`{"confirmed":true}`)}
				}
			case "initial-cancel":
				status = 503
				change = func(r *http.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					cancel()
					*r = *r.WithContext(ctx)
				}
			case "nil-access":
				status = 503
				s.access = nil
			case "typednil-access":
				status = 503
				s.access = (*requestListSessionUnitAccess)(nil)
			case "missing-current-port":
				status = 503
				s.access = &requestListMissingCurrentAccess{AccessStore: a}
			case "nil-store":
				status = 503
				s.connections = nil
			case "typednil-store":
				status = 503
				s.connections = (*requestListSessionUnitStore)(nil)
			}
			w := requestListSessionUnitRead(h, token, change)
			if w.Code != status || sp.calls != 0 || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatalf("%s status=%d read=%d", mode, w.Code, sp.calls)
			}
		})
	}
}

type requestListUnitBody struct{ *strings.Reader }

func (*requestListUnitBody) Close() error { return nil }

type requestListMissingCurrentAccess struct{ identity.AccessStore }

func TestConnectionRequestsSessionUnitFinalAndOriginalErrors(t *testing.T) {
	for _, mode := range []string{"late-typednil-access", "late-typednil-store", "marshal-invalid-date", "final-unavailable", "final-cancel", "final-cancel-after-validation", "domain-forbidden", "domain-notfound", "domain-conflict", "domain-rate", "domain-raw"} {
		t.Run(mode, func(t *testing.T) {
			h, s, sp, a, token := requestListSessionUnitFixture(t)
			status := 503
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "late-typednil-access":
				sp.after = func() { s.access = (*requestListSessionUnitAccess)(nil) }
			case "late-typednil-store":
				sp.after = func() { s.connections = (*requestListSessionUnitStore)(nil) }
			case "marshal-invalid-date":
				sp.items[0].ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "final-unavailable":
				a.finalErr = mp.ErrUnavailable
			case "final-cancel":
				a.onFinal = cancel
			case "final-cancel-after-validation":
				a.afterFinal = cancel
			case "domain-forbidden":
				sp.err = connection.ErrForbidden
				status = 403
			case "domain-notfound":
				sp.err = connection.ErrNotFound
				status = 404
			case "domain-conflict":
				sp.err = connection.ErrConflict
				status = 409
			case "domain-rate":
				sp.err = connection.ErrRateLimit
				status = 429
			case "domain-raw":
				sp.err = errors.New("UNIT_RAW_ERROR_CANARY")
				status = 500
			}
			w := requestListSessionUnitRead(h, token, func(r *http.Request) { *r = *r.WithContext(ctx) })
			if w.Code != status || sp.calls != 1 || strings.Contains(w.Body.String(), `"data"`) || strings.Contains(w.Body.String(), "CANARY") {
				t.Fatalf("%s: status=%d body=%s", mode, w.Code, w.Body.String())
			}
			if mode == "marshal-invalid-date" && a.final != 0 {
				t.Fatal("response validated before encoding was known safe")
			}
		})
	}
}
