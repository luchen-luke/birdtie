package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const notificationHTTPPath = "/v1/me/notification-policy"
const notificationHTTPCanary = "NOTIFICATION_PRIVATE_STORE_ERROR_CANARY"

// Transport spies only. Exact native Agent/current session/source ACL and
// durable CAS are the real PostgreSQL suite's responsibility, not these stubs.
type notificationPolicyHTTPStore struct {
	policy agentnotification.Policy
	err    error
	calls  int
	access agentprofile.PrivateAccess
	input  agentnotification.PutInput
}

func (s *notificationPolicyHTTPStore) GetOwnNotificationPolicy(_ context.Context, a agentprofile.PrivateAccess) (agentnotification.Policy, error) {
	s.calls++
	s.access = a
	return s.policy, s.err
}
func (s *notificationPolicyHTTPStore) PutOwnNotificationPolicy(_ context.Context, a agentprofile.PrivateAccess, in agentnotification.PutInput) (agentnotification.Policy, error) {
	s.calls++
	s.access = a
	s.input = in
	return s.policy, s.err
}

type notificationPolicyHTTPCatalog struct {
	foundation.PublicCatalog
	*notificationPolicyHTTPStore
}

func notificationPolicyHTTPFixture(t *testing.T) (*server, *privateProfileHTTPAccess, *notificationPolicyHTTPStore, string, string) {
	t.Helper()
	token, digest, err := identity.NewToken()
	if err != nil {
		t.Fatal("synthetic token fixture")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(time.Hour)
	p := agentnotification.Policy{SchemaVersion: agentnotification.SchemaVersion, AgentID: privateProfileHTTPAgent, Version: 1, Enabled: true, DefaultRoute: agentnotification.Normal, Rules: []agentnotification.Rule{{Category: agentnotification.CategoryMessage, Route: agentnotification.Immediate}}, UpdatedAt: &now, ExpiresAt: &expires}
	if err := agentnotification.ValidatePolicy(p); err != nil {
		t.Fatal("policy fixture")
	}
	store := &notificationPolicyHTTPStore{policy: p}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: digest}
	body, err := json.Marshal(agentnotification.PutInput{ExpectedVersion: 0, Enabled: true, DefaultRoute: p.DefaultRoute, Rules: p.Rules, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	return &server{access: access, notificationPolicies: store}, access, store, token, string(body)
}

func notificationPolicyHTTPServe(s *server, method string, rw *httptest.ResponseRecorder, r *http.Request) {
	if method == http.MethodGet {
		s.getOwnNotificationPolicy(rw, r)
	} else {
		s.putOwnNotificationPolicy(rw, r)
	}
}
func notificationPolicyHTTPError(t *testing.T, rw *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rw.Code != status {
		t.Fatalf("status=%d want=%d", rw.Code, status)
	}
	if rw.Header().Get("Cache-Control") != "no-store" || strings.Contains(rw.Body.String(), notificationHTTPCanary) {
		t.Fatal("unsafe error response")
	}
	var decoded struct{ Error struct{ Code, Detail string } }
	if json.Unmarshal(rw.Body.Bytes(), &decoded) != nil || decoded.Error.Code == "" || decoded.Error.Detail == "" {
		t.Fatal("error must include fixed code and Chinese recovery detail")
	}
}

func TestNotificationPolicyHTTPAuthentication(t *testing.T) {
	cases := []struct {
		name   string
		change func(*server, *privateProfileHTTPAccess, *http.Request)
		status int
	}{
		{"anonymous", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"duplicate_token", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header.Add("Authorization", r.Header.Get("Authorization"))
		}, 401},
		{"bad_token", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header.Set("Authorization", "Bearer arbitrary")
		}, 401},
		{"expired_revoked_session", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.err = identity.ErrUnauthorized }, 401},
		{"organization", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "organization" }, 403},
		{"business", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "business" }, 403},
		{"community", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.AccountType = "community" }, 403},
		{"bad_actor_id", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) { a.actor.ID = "unknown" }, 403},
		{"org_workspace_presence", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
		}, 403},
		{"query_owner", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) {
			r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign
		}, 400},
		{"query_arbitrary", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) { r.URL.RawQuery = "page=1" }, 400},
		{"empty_query_presence", func(_ *server, _ *privateProfileHTTPAccess, r *http.Request) { r.URL.ForceQuery = true }, 400},
		{"backend_auth_failure", func(_ *server, a *privateProfileHTTPAccess, _ *http.Request) {
			a.err = errors.New(notificationHTTPCanary)
		}, 503},
		{"nil_auth", func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.access = nil }, 503},
		{"nil_policy_store", func(s *server, _ *privateProfileHTTPAccess, _ *http.Request) { s.notificationPolicies = nil }, 503},
	}
	for _, method := range []string{"GET", "PUT"} {
		for _, c := range cases {
			t.Run(method+"/"+c.name, func(t *testing.T) {
				s, a, store, token, body := notificationPolicyHTTPFixture(t)
				if method == "GET" {
					body = ""
				}
				r := privateProfileHTTPRequest(method, notificationHTTPPath, body, token, "application/json")
				c.change(s, a, r)
				rw := httptest.NewRecorder()
				notificationPolicyHTTPServe(s, method, rw, r)
				notificationPolicyHTTPError(t, rw, c.status)
				if store.calls != 0 {
					t.Fatal("denied request reached preference store")
				}
				if c.status == 401 && rw.Header().Get("WWW-Authenticate") != "Bearer" {
					t.Fatal("missing auth challenge")
				}
			})
		}
	}
}

func TestNotificationPolicyHTTPStrictInput(t *testing.T) {
	_, _, _, _, valid := notificationPolicyHTTPFixture(t)
	cases := []struct {
		name, body, media string
		status            int
	}{
		{"empty", "", "application/json", 400}, {"null", "null", "application/json", 400}, {"array", "[]", "application/json", 400},
		{"trailing", valid + "{}", "application/json", 400}, {"oversize", strings.Repeat(" ", agentnotification.MaxBodyBytes+1), "application/json", 413},
		{"wrong_media", valid, "text/plain", 415}, {"missing_media", valid, "", 415}, {"bad_media", valid, "application/json;broken", 415},
	}
	for _, extra := range []string{`"ownerId":"` + privateProfileHTTPForeign + `"`, `"agentId":"` + privateProfileHTTPForeign + `"`, `"confirmed":true`, `"consent":true`, `"currentSource":{"verified":true}`, `"priority":100`, `"privateBody":"` + notificationHTTPCanary + `"`} {
		cases = append(cases, struct {
			name, body, media string
			status            int
		}{"unknown_" + strings.Split(extra, ":")[0], strings.Replace(valid, "{", "{"+extra+",", 1), "application/json", 400})
	}
	for _, pair := range [][2]string{{`"enabled":true`, `"enabled":true,"enabled":false`}, {`"enabled":true`, `"Enabled":true`}, {`"enabled":true`, `"enabled":null`}, {`"enabled":true`, `"enabled":"true"`}, {`"category":"MESSAGE"`, `"category":"MESSAGE","category":"SOCIAL"`}, {`"route":"IMMEDIATE"`, `"route":"IMMEDIATE","body":"` + notificationHTTPCanary + `"`}, {`"category":"MESSAGE"`, `"category":"SECRET"`}, {`"defaultRoute":"NORMAL"`, `"defaultRoute":"PUSH"`}} {
		cases = append(cases, struct {
			name, body, media string
			status            int
		}{"wire_" + pair[1], strings.Replace(valid, pair[0], pair[1], 1), "application/json", 400})
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, store, token, _ := notificationPolicyHTTPFixture(t)
			rw := httptest.NewRecorder()
			notificationPolicyHTTPServe(s, "PUT", rw, privateProfileHTTPRequest("PUT", notificationHTTPPath, c.body, token, c.media))
			notificationPolicyHTTPError(t, rw, c.status)
			if store.calls != 0 {
				t.Fatal("malformed wire reached store")
			}
		})
	}
	for _, body := range []string{" ", "{}", notificationHTTPCanary} {
		t.Run("GET_body_"+body, func(t *testing.T) {
			s, _, store, token, _ := notificationPolicyHTTPFixture(t)
			rw := httptest.NewRecorder()
			notificationPolicyHTTPServe(s, "GET", rw, privateProfileHTTPRequest("GET", notificationHTTPPath, body, token, "application/json"))
			notificationPolicyHTTPError(t, rw, 400)
			if store.calls != 0 {
				t.Fatal("GET body reached store")
			}
		})
	}
}

func TestNotificationPolicyHTTPValidOwnAccessAndCAS(t *testing.T) {
	for _, method := range []string{"GET", "PUT"} {
		t.Run(method, func(t *testing.T) {
			s, a, store, token, body := notificationPolicyHTTPFixture(t)
			if method == "GET" {
				body = ""
			}
			rw := httptest.NewRecorder()
			notificationPolicyHTTPServe(s, method, rw, privateProfileHTTPRequest(method, notificationHTTPPath, body, token, "application/json; charset=utf-8"))
			if rw.Code != 200 || rw.Header().Get("Cache-Control") != "no-store" || store.calls != 1 {
				t.Fatal("valid own handler")
			}
			if store.access.SessionDigest != a.digest || store.access.WorkspacePrincipal != (actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}) {
				t.Fatal("identity binding not from real auth adapter")
			}
			if method == "PUT" && (store.input.ExpectedVersion != 0 || !store.input.Enabled || len(store.input.Rules) != 1) {
				t.Fatal("input lost")
			}
			var data struct{ Data agentnotification.Policy }
			if json.Unmarshal(rw.Body.Bytes(), &data) != nil || agentnotification.ValidatePolicy(data.Data) != nil {
				t.Fatal("invalid response")
			}
		})
	}
	s, _, store, token, _ := notificationPolicyHTTPFixture(t)
	p, err := agentnotification.DefaultPolicy(privateProfileHTTPAgent)
	if err != nil {
		t.Fatal(err)
	}
	store.policy = p
	rw := httptest.NewRecorder()
	s.getOwnNotificationPolicy(rw, privateProfileHTTPRequest("GET", notificationHTTPPath, "", token, ""))
	if rw.Code != 200 {
		t.Fatal("unconfigured own review")
	}
	for _, route := range []agentnotification.Route{agentnotification.Immediate, agentnotification.Normal, agentnotification.Digest, agentnotification.Silent, agentnotification.Block} {
		t.Run(string(route), func(t *testing.T) {
			s, _, store, token, body := notificationPolicyHTTPFixture(t)
			store.policy.DefaultRoute = route
			body = strings.Replace(body, `"defaultRoute":"NORMAL"`, `"defaultRoute":"`+string(route)+`"`, 1)
			rw := httptest.NewRecorder()
			s.putOwnNotificationPolicy(rw, privateProfileHTTPRequest("PUT", notificationHTTPPath, body, token, "application/json"))
			if rw.Code != 200 {
				t.Fatal("five route transport")
			}
		})
	}
	t.Run("disabled_is_normal_not_silent", func(t *testing.T) {
		s, _, store, token, body := notificationPolicyHTTPFixture(t)
		store.policy.Enabled = false
		body = strings.Replace(body, `"enabled":true`, `"enabled":false`, 1)
		rw := httptest.NewRecorder()
		s.putOwnNotificationPolicy(rw, privateProfileHTTPRequest("PUT", notificationHTTPPath, body, token, "application/json"))
		if rw.Code != 200 || store.input.Enabled {
			t.Fatal("disabled human setting")
		}
	})
}

func TestNotificationPolicyHTTPBackendErrorsAndResponseSafety(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
	}{{"invalid", agentnotification.ErrInvalid, 400}, {"forbidden", agentnotification.ErrForbidden, 403}, {"not_found", agentnotification.ErrNotFound, 404}, {"conflict", agentnotification.ErrConflict, 409}, {"unavailable", agentnotification.ErrUnavailable, 503}, {"raw_error", errors.New(notificationHTTPCanary), 503}}
	var output bytes.Buffer
	original := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(original)
	for _, method := range []string{"GET", "PUT"} {
		for _, c := range cases {
			t.Run(method+"/"+c.name, func(t *testing.T) {
				s, _, store, token, body := notificationPolicyHTTPFixture(t)
				store.err = c.err
				if method == "GET" {
					body = ""
				}
				rw := httptest.NewRecorder()
				notificationPolicyHTTPServe(s, method, rw, privateProfileHTTPRequest(method, notificationHTTPPath, body, token, "application/json"))
				notificationPolicyHTTPError(t, rw, c.status)
				if store.calls != 1 {
					t.Fatal("expected backend call")
				}
			})
		}
	}
	if strings.Contains(output.String(), notificationHTTPCanary) {
		t.Fatal("underlying error leaked into log")
	}
	corrupt := []struct {
		name   string
		change func(*agentnotification.Policy)
	}{{"bad_schema", func(p *agentnotification.Policy) { p.SchemaVersion = notificationHTTPCanary }}, {"bad_agent", func(p *agentnotification.Policy) { p.AgentID = notificationHTTPCanary }}, {"nil_rules", func(p *agentnotification.Policy) { p.Rules = nil }}, {"wrong_category", func(p *agentnotification.Policy) { p.Rules[0].Category = "SECRET" }}, {"unknown_route", func(p *agentnotification.Policy) { p.DefaultRoute = "SEND" }}, {"nil_expiry", func(p *agentnotification.Policy) { p.ExpiresAt = nil }}, {"default_v0_with_content", func(p *agentnotification.Policy) { p.Version = 0 }}}
	for _, method := range []string{"GET", "PUT"} {
		for _, c := range corrupt {
			t.Run(method+"/"+c.name, func(t *testing.T) {
				s, _, store, token, body := notificationPolicyHTTPFixture(t)
				c.change(&store.policy)
				if method == "GET" {
					body = ""
				}
				rw := httptest.NewRecorder()
				notificationPolicyHTTPServe(s, method, rw, privateProfileHTTPRequest(method, notificationHTTPPath, body, token, "application/json"))
				notificationPolicyHTTPError(t, rw, 503)
			})
		}
	}
	t.Run("writer_wrong_version", func(t *testing.T) {
		s, _, store, token, body := notificationPolicyHTTPFixture(t)
		store.policy.Version = 2
		rw := httptest.NewRecorder()
		s.putOwnNotificationPolicy(rw, privateProfileHTTPRequest("PUT", notificationHTTPPath, body, token, "application/json"))
		notificationPolicyHTTPError(t, rw, 503)
	})
	// A well-formed different Agent UUID is NOT an HTTP shape error. Ownership
	// must be denied by the actual native store join, covered by integration.
	p := notificationTestPolicyForHTTP(t)
	p.AgentID = privateProfileHTTPForeign
	if agentnotification.ValidatePolicy(p) != nil {
		t.Fatal("shape validator pretended to authenticate UUID")
	}
}

func notificationTestPolicyForHTTP(t *testing.T) agentnotification.Policy {
	t.Helper()
	_, _, store, _, _ := notificationPolicyHTTPFixture(t)
	return store.policy
}

func TestNotificationPolicyHTTPConstructorUsesOnlyNativePolicyInterface(t *testing.T) {
	_, a, store, token, _ := notificationPolicyHTTPFixture(t)
	handler := New(notificationPolicyHTTPCatalog{notificationPolicyHTTPStore: store}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, privateProfileHTTPRequest("GET", notificationHTTPPath, "", token, ""))
	if rw.Code != 200 || store.calls != 1 || !reflect.DeepEqual(store.access.WorkspacePrincipal, actorref.PrincipalRef{Type: actorref.Person, ID: privateProfileHTTPOwner}) {
		t.Fatal("registered GET route not native policy store")
	}
}
