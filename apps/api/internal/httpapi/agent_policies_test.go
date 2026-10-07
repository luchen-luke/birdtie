package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

const policyAPIPath = "/v1/me/agent-policies"

type policyAPISpy struct {
	bundle        agentpolicysettings.Bundle
	err           error
	reads, writes int
	access        agentprofile.PrivateAccess
	family        agentpolicysettings.Family
	input         agentpolicysettings.PutInput
}

func (s *policyAPISpy) GetOwnPolicies(_ context.Context, a agentprofile.PrivateAccess) (agentpolicysettings.Bundle, error) {
	s.reads++
	s.access = a
	return s.bundle, s.err
}
func (s *policyAPISpy) PutOwnPolicy(_ context.Context, a agentprofile.PrivateAccess, f agentpolicysettings.Family, in agentpolicysettings.PutInput) (agentpolicysettings.Bundle, error) {
	s.writes++
	s.access = a
	s.family = f
	s.input = in
	return s.bundle, s.err
}

type policyAPICatalog struct {
	foundation.PublicCatalog
	*policyAPISpy
}

func policyAPIFixture(t *testing.T) (*server, *privateProfileHTTPAccess, *policyAPISpy, string) {
	s, a, _, token := privateProfileHTTPFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	spy := &policyAPISpy{bundle: agentpolicysettings.Bundle{SchemaVersion: agentpolicysettings.SchemaVersion, OwnerType: actorref.Person, OwnerID: privateProfileHTTPOwner, AgentID: privateProfileHTTPAgent, ObservedAt: now, Attention: agentpolicysettings.DefaultRecord(agentpolicysettings.Attention), Social: agentpolicysettings.DefaultRecord(agentpolicysettings.Social), Autonomy: agentpolicysettings.DefaultRecord(agentpolicysettings.Autonomy)}}
	s.policySettings = spy
	return s, a, spy, token
}
func policyAPIInput(f agentpolicysettings.Family, version int64, expires time.Time) string {
	raw := `{"level":"LEVEL_2_PREPARE"}`
	if f == agentpolicysettings.Attention {
		raw = `{"defaultRoute":"NORMAL","rules":[{"eventType":"UserQuery","route":"IMMEDIATE"}]}`
	} else if f == agentpolicysettings.Social {
		raw = `{"rules":[{"category":"UNKNOWN_PERSON","preference":"REVIEW_REQUIRED"}]}`
	}
	return fmt.Sprintf(`{"expectedVersion":%d,"settings":%s,"expiresAt":%q}`, version, raw, expires.UTC().Format(time.RFC3339Nano))
}
func policyAPISetSpy(t *testing.T, s *policyAPISpy, f agentpolicysettings.Family, body string) {
	in, err := agentpolicysettings.DecodePutInput(f, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	n, err := agentpolicysettings.NormalizeInput(f, in, s.bundle.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	from := s.bundle.ObservedAt
	r := agentpolicysettings.Record{Family: f, Configured: true, NativeRevision: n.ExpectedVersion + 1, Status: "ACTIVE", Settings: n.Settings, ValidFrom: &from, UpdatedAt: &from, ExpiresAt: &n.ExpiresAt}
	switch f {
	case agentpolicysettings.Attention:
		s.bundle.Attention = r
	case agentpolicysettings.Social:
		s.bundle.Social = r
	case agentpolicysettings.Autonomy:
		s.bundle.Autonomy = r
	}
}
func policyAPIInvoke(s *server, method string, r *http.Request) *httptest.ResponseRecorder {
	rw := httptest.NewRecorder()
	if method == http.MethodGet {
		s.getOwnAgentPolicies(rw, r)
	} else {
		f := agentpolicysettings.Autonomy
		if strings.HasSuffix(r.URL.Path, "attention") {
			f = agentpolicysettings.Attention
		} else if strings.HasSuffix(r.URL.Path, "social") {
			f = agentpolicysettings.Social
		}
		s.putOwnAgentPolicy(rw, r, f)
	}
	return rw
}
func TestPolicyAPIsTransport(t *testing.T) {
	t.Run("OwnGetBinding", func(t *testing.T) {
		s, a, spy, token := policyAPIFixture(t)
		rw := policyAPIInvoke(s, "GET", privateProfileHTTPRequest("GET", policyAPIPath, "", token, ""))
		if rw.Code != 200 || spy.reads != 1 || spy.writes != 0 || spy.access.SessionDigest != a.digest || spy.access.WorkspacePrincipal != (actorref.PrincipalRef{Type: actorref.Person, ID: a.actor.ID}) || rw.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("not narrowly session bound")
		}
	})
	for _, f := range agentpolicysettings.Families() {
		t.Run(string(f), func(t *testing.T) {
			s, a, spy, token := policyAPIFixture(t)
			body := policyAPIInput(f, 7, spy.bundle.ObservedAt.Add(time.Hour))
			policyAPISetSpy(t, spy, f, body)
			rw := policyAPIInvoke(s, "PUT", privateProfileHTTPRequest("PUT", policyAPIPath+"/"+strings.ToLower(string(f)), body, token, "application/json; charset=utf-8"))
			if rw.Code != 200 || spy.writes != 1 || spy.reads != 0 || spy.family != f || spy.input.ExpectedVersion != 7 || spy.access.SessionDigest != a.digest {
				t.Fatal("valid family not bound", rw.Code)
			}
		})
	}
	t.Run("RegisteredRoutes", func(t *testing.T) {
		_, a, spy, token := policyAPIFixture(t)
		h := privateProfileHTTPNew(policyAPICatalog{policyAPISpy: spy}, a)
		rw := httptest.NewRecorder()
		h.ServeHTTP(rw, privateProfileHTTPRequest("GET", policyAPIPath, "", token, ""))
		if rw.Code != 200 {
			t.Fatal("route missing")
		}
	})
}
func TestPolicyAPIsDenyRequest(t *testing.T) {
	cases := []struct {
		name, method, path, body, ct string
		want                         int
		mutate                       func(*server, *privateProfileHTTPAccess, *http.Request)
	}{
		{"Anonymous", "GET", policyAPIPath, "", "", 401, func(s *server, a *privateProfileHTTPAccess, r *http.Request) { r.Header.Del("Authorization") }},
		{"Org", "GET", policyAPIPath, "", "", 403, func(s *server, a *privateProfileHTTPAccess, r *http.Request) { a.actor.AccountType = "organization" }},
		{"Biz", "GET", policyAPIPath, "", "", 403, func(s *server, a *privateProfileHTTPAccess, r *http.Request) { a.actor.AccountType = "business" }},
		{"Workspace", "GET", policyAPIPath, "", "", 403, func(s *server, a *privateProfileHTTPAccess, r *http.Request) {
			r.Header.Set("X-Birdtie-Organization-Workspace", "")
		}},
		{"NoPort", "GET", policyAPIPath, "", "", 503, func(s *server, a *privateProfileHTTPAccess, r *http.Request) { s.policySettings = nil }},
		{"NoIdentity", "GET", policyAPIPath, "", "", 503, func(s *server, a *privateProfileHTTPAccess, r *http.Request) { s.access = nil }},
		{"OwnerQuery", "GET", policyAPIPath + "?ownerId=" + privateProfileHTTPForeign, "", "", 400, nil},
		{"EmptyQuery", "GET", policyAPIPath + "?", "", "", 400, nil},
		{"GetBody", "GET", policyAPIPath, "{}", "application/json", 400, nil},
		{"MissingBody", "PUT", policyAPIPath + "/autonomy", "", "application/json", 400, nil},
		{"WrongCT", "PUT", policyAPIPath + "/autonomy", "{}", "text/plain", 400, nil},
		{"WrongCharset", "PUT", policyAPIPath + "/autonomy", "{}", "application/json;charset=gbk", 400, nil},
		{"UnknownCTParam", "PUT", policyAPIPath + "/autonomy", "{}", "application/json;version=1", 400, nil},
		{"DuplicateCT", "PUT", policyAPIPath + "/autonomy", "{}", "application/json", 400, func(s *server, a *privateProfileHTTPAccess, r *http.Request) {
			r.Header.Add("Content-Type", "application/json")
		}},
		{"UnknownBody", "PUT", policyAPIPath + "/autonomy", `{"confirmed":true}`, "application/json", 400, nil},
		{"Oversize", "PUT", policyAPIPath + "/autonomy", strings.Repeat("x", agentpolicysettings.MaxBodyBytes+1), "application/json", 400, nil},
		{"InvalidUTF8", "PUT", policyAPIPath + "/autonomy", string([]byte{0xff}), "application/json", 400, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, a, spy, token := policyAPIFixture(t)
			r := privateProfileHTTPRequest(c.method, c.path, c.body, token, c.ct)
			if c.mutate != nil {
				c.mutate(s, a, r)
			}
			rw := policyAPIInvoke(s, c.method, r)
			if rw.Code != c.want || spy.reads+spy.writes != 0 || rw.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("invalid request reached native settings", rw.Code)
			}
			if strings.Contains(rw.Body.String(), "confirmed") || !strings.Contains(rw.Body.String(), "message") {
				t.Fatal("unsafe error")
			}
		})
	}
}
func TestPolicyAPIsDenyResponse(t *testing.T) {
	for name, m := range map[string]func(*agentpolicysettings.Bundle){"WrongOwner": func(b *agentpolicysettings.Bundle) { b.OwnerID = privateProfileHTTPForeign }, "WrongAgent": func(b *agentpolicysettings.Bundle) { b.AgentID = "provider" }, "WrongSchema": func(b *agentpolicysettings.Bundle) { b.SchemaVersion = "old" }, "FalseVersion": func(b *agentpolicysettings.Bundle) { b.Social.NativeRevision = 1 }, "FalseStatus": func(b *agentpolicysettings.Bundle) { b.Autonomy.Status = "ACTIVE" }, "PrivateText": func(b *agentpolicysettings.Bundle) {
		b.Attention.Settings = json.RawMessage(`{"agentNotes":"PRIVATE_HTTP_NO_DISCLOSURE_CANARY"}`)
	}} {
		t.Run(name, func(t *testing.T) {
			s, _, spy, token := policyAPIFixture(t)
			m(&spy.bundle)
			rw := policyAPIInvoke(s, "GET", privateProfileHTTPRequest("GET", policyAPIPath, "", token, ""))
			if rw.Code != 503 || strings.Contains(rw.Body.String(), privateProfileHTTPMarker) || strings.Contains(rw.Body.String(), "ownerId") {
				t.Fatal("misbound response escaped")
			}
		})
	}
	for name, e := range map[string]error{"Forbidden": agentpolicysettings.ErrForbidden, "Invalid": agentpolicysettings.ErrInvalid, "NotFound": agentpolicysettings.ErrNotFound, "Conflict": agentpolicysettings.ErrConflict, "RawDatabase": errors.New("PRIVATE_HTTP_NO_DISCLOSURE_CANARY SQL password")} {
		t.Run(name, func(t *testing.T) {
			s, _, spy, token := policyAPIFixture(t)
			spy.err = e
			rw := policyAPIInvoke(s, "GET", privateProfileHTTPRequest("GET", policyAPIPath, "", token, ""))
			want := 503
			switch name {
			case "Forbidden":
				want = 403
			case "Invalid":
				want = 400
			case "NotFound":
				want = 404
			case "Conflict":
				want = 409
			}
			if rw.Code != want || strings.Contains(rw.Body.String(), "SQL") || strings.Contains(rw.Body.String(), privateProfileHTTPMarker) {
				t.Fatal("error escaped", rw.Code)
			}
		})
	}
	for name, m := range map[string]func(*agentpolicysettings.Record){"LostWrite": func(r *agentpolicysettings.Record) { r.NativeRevision = 7 }, "OtherSettings": func(r *agentpolicysettings.Record) { r.Settings = json.RawMessage(`{"level":"LEVEL_0_OBSERVE"}`) }, "ChangedExpiry": func(r *agentpolicysettings.Record) { e := r.ExpiresAt.Add(time.Minute); r.ExpiresAt = &e }} {
		t.Run(name, func(t *testing.T) {
			s, _, spy, token := policyAPIFixture(t)
			body := policyAPIInput(agentpolicysettings.Autonomy, 7, spy.bundle.ObservedAt.Add(time.Hour))
			policyAPISetSpy(t, spy, agentpolicysettings.Autonomy, body)
			m(&spy.bundle.Autonomy)
			rw := policyAPIInvoke(s, "PUT", privateProfileHTTPRequest("PUT", policyAPIPath+"/autonomy", body, token, "application/json"))
			if rw.Code != 503 || strings.Contains(rw.Body.String(), "settings") {
				t.Fatal("unconfirmed write escaped")
			}
		})
	}
}
