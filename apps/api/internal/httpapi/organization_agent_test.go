package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

type organizationAgentTransportFixture struct {
	foundation.PublicCatalog
	organization.FAQStore
	answer organization.AgentAnswer
	calls  int
	after  func()
}

// Offline transport companion only; actual native locking/expiry is tested
// separately against the registered PG store and real database sessions.
type organizationAgentTransportSession struct{ *privateProfileHTTPAccess }

func (f *organizationAgentTransportSession) AuthenticateOrganizationAgent(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return f.Authenticate(ctx, digest)
}

func (f *organizationAgentTransportSession) ValidateOrganizationAgentSession(ctx context.Context, digest [32]byte, initial identity.Actor) error {
	current, e := f.Authenticate(ctx, digest)
	if e != nil {
		return e
	}
	if current.ID != initial.ID || current.AccountType != initial.AccountType {
		return identity.ErrUnauthorized
	}
	return nil
}

func (f *organizationAgentTransportFixture) AnswerOrganization(_ context.Context, org, viewer, query string) (organization.AgentAnswer, error) {
	f.calls++
	if f.after != nil {
		f.after()
	}
	return f.answer, nil
}

func TestOrganizationCapabilityRegisteredHTTPStrictTransport(t *testing.T) {
	const org = "73000000-0000-4000-8000-000000000001"
	for _, c := range []struct {
		name, body, suffix string
		status             int
	}{
		{"valid", `{"query":"如何报名"}`, "", 200}, {"duplicate", `{"query":"如何报名","query":"官网"}`, "", 400}, {"case_alias", `{"Query":"如何报名"}`, "", 400}, {"grant", `{"query":"如何报名","grant":true}`, "", 400}, {"query_selector", `{"query":"如何报名"}`, "?ownerId=approved", 400}, {"force_query", `{"query":"如何报名"}`, "?", 400},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &organizationAgentTransportFixture{answer: organization.AgentAnswer{OrganizationID: org, Status: "unknown", Answer: "合成未找到公开答案", Sources: []organization.AnswerSource{}, Mode: "verified_rules"}}
			h := privateProfileHTTPNew(f, &privateProfileHTTPAccess{})
			r := httptest.NewRequest("POST", "/v1/organizations/"+org+"/agent/ask"+c.suffix, strings.NewReader(c.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("transport code=%d %s", w.Code, w.Body.String())
			}
			if c.status != 200 && f.calls != 0 {
				t.Fatal("invalid wire reached source adapter")
			}
		})
	}
	t.Run("malformed_source_output_closed", func(t *testing.T) {
		f := &organizationAgentTransportFixture{answer: organization.AgentAnswer{OrganizationID: org, Status: "known", Answer: "合成错误回执", Mode: "verified_rules"}}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/v1/organizations/"+org+"/agent/ask", strings.NewReader(`{"query":"如何报名"}`))
		r.Header.Set("Content-Type", "application/json")
		privateProfileHTTPNew(f, &privateProfileHTTPAccess{}).ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), f.answer.Answer) {
			t.Fatal("malformed source answer released")
		}
	})
	t.Run("late_auth_transport_failure", func(t *testing.T) {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		access := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: digest}
		f := &organizationAgentTransportFixture{answer: organization.AgentAnswer{OrganizationID: org, Status: "unknown", Answer: "合成不应在身份失效后交付", Sources: []organization.AnswerSource{}, Mode: "verified_rules"}, after: func() { access.err = identity.ErrUnauthorized }}
		r := httptest.NewRequest("POST", "/v1/organizations/"+org+"/agent/ask", strings.NewReader(`{"query":"如何报名"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		privateProfileHTTPNew(f, &organizationAgentTransportSession{access}).ServeHTTP(w, r)
		if w.Code != 401 || access.calls != 2 || strings.Contains(w.Body.String(), f.answer.Answer) {
			t.Fatal("transport reused early session")
		}
	})
	t.Log("TransportOnly: spies only test wire/output/late authentication; native source proof is separate")
}
