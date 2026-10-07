package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationevidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

func organizationEvidenceHTTPFixture(t *testing.T) (*privateProfileHTTPDBFixture, string, string, string, string) {
	t.Helper()
	f, org := orgMemoryHTTPFixture(t)
	base := "/v1/me/organizations/" + org + "/agent-memories/"
	mid, source := orgMemoryHTTPID(t, f), orgMemoryHTTPID(t, f)
	for _, id := range []string{mid, source} {
		in := orgMemoryHTTPInput()
		in.Key = id
		f.request(t, f.handler, "PUT", base+id, orgMemoryHTTPBody(t, in), f.tokens[0], 200, nil)
	}
	return f, org, mid, source, base + mid
}
func organizationEvidenceHTTPBody(source string) string {
	return `{"expectedMemoryVersion":1,"sourceType":"ORGANIZATION_ADMIN_INPUT","sourceId":"` + source + `"}`
}
func organizationEvidenceHTTPData(t *testing.T, w *httptest.ResponseRecorder) agentmemory.Evidence {
	t.Helper()
	var value struct {
		Data agentmemory.Evidence `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &value); e != nil {
		t.Fatal(e)
	}
	return value.Data
}
func TestOrganizationEvidenceRegisteredHTTPNativeCRUDAndRoles(t *testing.T) {
	f, org, mid, source, base := organizationEvidenceHTTPFixture(t)
	id := orgMemoryHTTPID(t, f)
	path := base + "/evidence/" + id
	body := organizationEvidenceHTTPBody(source)
	first := organizationEvidenceHTTPData(t, f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil))
	if first.OwnerID != f.accountIDs[2] || first.AgentID != f.agentIDs[2] || first.MemoryID != mid || first.Source.ID != source || first.Source.Owner.Type != "ORGANIZATION" {
		t.Fatal("wrong native response binding")
	}
	f.request(t, f.handler, "PUT", path, body, f.tokens[1], 200, nil)
	w := f.request(t, f.handler, "GET", base+"/provenance", "", f.tokens[0], 200, nil)
	var p struct {
		Data agentorganizationevidence.Provenance `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil || len(p.Data.Evidence) != 1 || p.Data.OrganizationID != org || p.Data.Declaration != agentorganizationevidence.Declaration || strings.Contains(w.Body.String(), orgMemoryHTTPInput().Summary) {
		t.Fatal("source body/fake verified fact leak", e)
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		target := path
		requestBody := body
		if method == "GET" {
			target = base + "/provenance"
			requestBody = ""
		}
		if method == "DELETE" {
			requestBody = `{"expectedVersion":1}`
		}
		for _, token := range []string{"", "unknown", f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
			f.request(t, f.handler, method, target, requestBody, token, 401, nil)
		}
		for _, index := range []int{2, 3} {
			f.request(t, f.handler, method, target, requestBody, f.tokens[index], 403, nil)
		}
	}
	for _, role := range []string{"member", "moderator"} {
		f.exec(`UPDATE organization_memberships SET role=$3 WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1], role)
		f.request(t, f.handler, "GET", base+"/provenance", "", f.tokens[1], 403, nil)
		f.request(t, f.handler, "PUT", path, body, f.tokens[1], 403, nil)
		f.request(t, f.handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[1], 403, nil)
	}
	f.exec(`UPDATE organization_memberships SET role='admin',status='removed' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
	f.request(t, f.handler, "GET", base+"/provenance", "", f.tokens[1], 403, nil)
	f.request(t, f.handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "DELETE", path, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
	f.request(t, f.handler, "PUT", path, body, f.tokens[0], 409, nil)
}
func TestOrganizationEvidenceRegisteredHTTPNativeStrictWire(t *testing.T) {
	f, _, _, source, base := organizationEvidenceHTTPFixture(t)
	path := base + "/evidence/" + orgMemoryHTTPID(t, f)
	body := organizationEvidenceHTTPBody(source)
	cases := []struct {
		name, method, path, body, media string
		status                          int
	}{
		{"query", "GET", base + "/provenance?ownerId=" + f.accountIDs[2], "", "application/json", 400}, {"empty_query", "GET", base + "/provenance?", "", "application/json", 400}, {"get_body", "GET", base + "/provenance", `{}`, "application/json", 400},
		{"authority", "PUT", path, strings.Replace(body, "{", `{"ownerId":"`+f.accountIDs[2]+`",`, 1), "application/json", 400}, {"version_claim", "PUT", path, strings.Replace(body, "{", `{"sourceVersion":99,`, 1), "application/json", 400}, {"null", "PUT", path, strings.Replace(body, `"expectedMemoryVersion":1`, `"expectedMemoryVersion":null`, 1), "application/json", 400}, {"duplicate", "PUT", path, strings.Replace(body, "{", `{"sourceId":"`+f.accountIDs[1]+`",`, 1), "application/json", 400},
		{"source_person", "PUT", path, strings.Replace(body, "ORGANIZATION_ADMIN_INPUT", "MOMENT", 1), "application/json", 400}, {"announcement", "PUT", path, strings.Replace(body, "ORGANIZATION_ADMIN_INPUT", "ORGANIZATION_ANNOUNCEMENT", 1), "application/json", 403}, {"wrong_media", "PUT", path, body, "text/plain", 415}, {"large", "PUT", path, strings.Repeat("x", agentmemory.MaxBodyBytes+1), "application/json", 400}, {"delete_confirm", "DELETE", path, `{"expectedVersion":1,"confirmed":true}`, "application/json", 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := privateProfileHTTPRequest(c.method, c.path, c.body, f.tokens[0], c.media)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, r)
			if w.Code != c.status || w.Header().Get("Cache-Control") != "no-store" || !safeRequestID.MatchString(w.Header().Get("X-Request-ID")) {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	workspace := f.accountIDs[2]
	f.request(t, f.handler, "PUT", path, body, f.tokens[0], 400, &workspace)
}

type organizationEvidenceHTTPAfter struct {
	*postgres.Store
	afterPut, afterRead func()
}

func (s *organizationEvidenceHTTPAfter) PutOrganizationMemoryEvidence(ctx context.Context, a agentorganizationmemory.Access, m, id string, in agentmemory.EvidenceReferenceInput) (agentmemory.Evidence, error) {
	r, e := s.Store.PutOrganizationMemoryEvidence(ctx, a, m, id, in)
	if e == nil && s.afterPut != nil {
		h := s.afterPut
		s.afterPut = nil
		h()
	}
	return r, e
}
func (s *organizationEvidenceHTTPAfter) ReadOrganizationMemoryProvenance(ctx context.Context, a agentorganizationmemory.Access, m string) (agentorganizationevidence.Provenance, error) {
	r, e := s.Store.ReadOrganizationMemoryProvenance(ctx, a, m)
	if e == nil && s.afterRead != nil {
		h := s.afterRead
		s.afterRead = nil
		h()
	}
	return r, e
}
func TestOrganizationEvidenceRegisteredHTTPNativeLateAuthorityAndSource(t *testing.T) {
	for _, operation := range []string{"put", "read"} {
		for _, change := range []string{"revoke_role", "revoke_session", "change_source", "delete_source", "block"} {
			t.Run(operation+"_"+change, func(t *testing.T) {
				f, org, _, source, base := organizationEvidenceHTTPFixture(t)
				id := orgMemoryHTTPID(t, f)
				path := base + "/evidence/" + id
				body := organizationEvidenceHTTPBody(source)
				if operation == "read" {
					f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil)
					path = base + "/provenance"
					body = ""
				}
				hook := func() {
					switch change {
					case "revoke_role":
						f.exec(`UPDATE organization_memberships SET role='member' WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[0])
					case "revoke_session":
						f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
					case "change_source":
						in := orgMemoryHTTPInput()
						in.Key = source
						in.ExpectedVersion = 1
						in.Summary = "合成迟到源版本"
						f.request(t, f.handler, "PUT", "/v1/me/organizations/"+org+"/agent-memories/"+source, orgMemoryHTTPBody(t, in), f.tokens[0], 200, nil)
					case "delete_source":
						f.request(t, f.handler, "DELETE", "/v1/me/organizations/"+org+"/agent-memories/"+source, `{"expectedVersion":1}`, f.tokens[0], 200, nil)
					case "block":
						f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.accountIDs[0], f.accountIDs[2])
					}
				}
				store := &organizationEvidenceHTTPAfter{Store: f.store}
				method := "PUT"
				want := 409
				if operation == "read" {
					method = "GET"
					store.afterRead = hook
					want = 200
				} else {
					store.afterPut = hook
				}
				if change == "revoke_role" || change == "revoke_session" {
					want = 403
				}
				if change == "block" { // Admin-only explicit source is private; public blocks do not change its existing admin ACL.
					want = 200
				}
				w := f.request(t, privateProfileHTTPNew(store, f.store), method, path, body, f.tokens[0], want, nil)
				if (change == "change_source" || change == "delete_source" || want == 403) && strings.Contains(w.Body.String(), source) {
					t.Fatal("late response disclosed stale source")
				}
			})
		}
	}
}
