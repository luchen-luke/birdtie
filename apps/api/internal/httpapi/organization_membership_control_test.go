package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type orgControlSpy struct {
	identity.AccessStore
	org.HumanControlStore
	org.MembershipStore
	writes, reads, stops int
	final                error
	lost                 bool
}

func (p *orgControlSpy) Authenticate(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: avatarTestOwner, AccountType: "person"}, nil
}
func (p *orgControlSpy) ReadMembersControl(context.Context, org.HumanAccess, string) (org.MembersControl, error) {
	return org.MembersControl{OrganizationID: avatarTestOwner, Members: []org.Membership{}, AuthorityVersion: 2, Role: "owner", Proof: "INITIAL_SOURCE", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (p *orgControlSpy) ApplyOrganizationControl(_ context.Context, _ org.HumanAccess, id string, in org.ControlInput, _ string) (org.ControlReceipt, error) {
	p.writes++
	return org.ControlReceipt{OrganizationID: id, OperationID: in.OperationID, Action: in.Action, Status: "SUCCEEDED", AuthorityVersion: 3, MemberVersion: 1, Proof: "CURRENT"}, nil
}
func (p *orgControlSpy) ReadOrganizationControlOperation(_ context.Context, _ org.HumanAccess, id, op string) (org.ControlReceipt, error) {
	p.reads++
	return org.ControlReceipt{OrganizationID: id, OperationID: op, Action: "REVOKE", Status: "SUCCEEDED", AuthorityVersion: 4, MemberVersion: 3, Proof: "HISTORY"}, nil
}
func (p *orgControlSpy) StopOrganizationControlOperation(_ context.Context, _ org.HumanAccess, id, op string, _ int64) (org.ControlReceipt, error) {
	p.stops++
	return org.ControlReceipt{OrganizationID: id, OperationID: op, Action: "STOP", Status: "STOPPED", Proof: "HISTORY"}, nil
}
func (p *orgControlSpy) ValidateOrganizationControlResponse(context.Context, org.HumanAccess, string, string, time.Time) error {
	return p.final
}
func orgControlRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	token, _, e := identity.NewToken()
	if e != nil {
		panic(e)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	return r
}
func TestOrganizationMembershipControlStrictAndFinalFence(t *testing.T) {
	body := `{"action":"INVITE","operationId":"` + avatarTestOwner + `","expectedAuthorityVersion":2,"userAccountId":"` + avatarTestOwner + `","role":"member","expectedMemberVersion":0,"approval":"bound"}`
	for n, b := range map[string]string{"missingVersion": strings.Replace(body, `,"expectedMemberVersion":0`, "", 1), "duplicate": strings.Replace(body, `"role":"member"`, `"role":"member","role":"admin"`, 1), "extraPoint": strings.Replace(body, `"approval":"bound"`, `"latitude":0,"approval":"bound"`, 1), "null": strings.Replace(body, `"expectedMemberVersion":0`, `"expectedMemberVersion":null`, 1)} {
		t.Run(n, func(t *testing.T) {
			w := httptest.NewRecorder()
			if _, _, e := decodeOrgControl(w, orgControlRequest("PUT", "/", b), true); e != org.ErrControlInvalid {
				t.Fatal("strict input accepted", e)
			}
		})
	}
	p := &orgControlSpy{final: identity.ErrUnauthorized}
	s := &server{access: p, memberships: p}
	mux := http.NewServeMux()
	s.registerOrganizationControlRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, orgControlRequest("PUT", "/v1/me/organizations/"+avatarTestOwner+"/control", body))
	if p.writes != 1 || w.Code != 401 || strings.Contains(w.Body.String(), "SUCCEEDED") {
		t.Fatal("postcommit unauthorized must remain unknown", w.Code, w.Body.String())
	}
	t.Run("capturedSessionRetirementIsChangedAndUnknown", func(t *testing.T) {
		p.final = org.ErrControlChanged
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, orgControlRequest("PUT", "/v1/me/organizations/"+avatarTestOwner+"/control", body))
		if w.Code != 409 || strings.Contains(w.Body.String(), "SUCCEEDED") || strings.Contains(w.Body.String(), "not_found") {
			t.Fatal("original captured Session loss presented as missing resource or known effect", w.Code, w.Body.String())
		}
	})
	p.final = nil
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, orgControlRequest("GET", "/v1/me/organizations/"+avatarTestOwner+"/control/operations/"+avatarTestOwner, ""))
	if w.Code != 200 || p.reads != 1 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "HISTORY") {
		t.Fatal("original minimal receipt", w.Code, w.Body.String())
	}
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal("receipt wire")
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, orgControlRequest("POST", "/v1/me/organizations/"+avatarTestOwner+"/control/operations/"+avatarTestOwner+"/stop", `{"expectedAuthorityVersion":2}`))
	if w.Code != 200 || p.stops != 1 || !strings.Contains(w.Body.String(), "STOPPED") {
		t.Fatal("explicit original stop", w.Code)
	}
}
func TestOrganizationControlLegacyWritesCannotBypass(t *testing.T) {
	p := &orgControlSpy{}
	s := &server{access: p, memberships: p}
	for n, h := range map[string]http.HandlerFunc{"invite": s.inviteOrganizationMember, "accept": s.acceptOrganizationInvitation, "role": s.changeOrganizationMemberRole, "revoke": s.revokeOrganizationMember, "point": s.submitOrganizationMapLocation, "hide": s.hideOrganizationMapLocation, "review": s.reviewOrganizationMapLocation} {
		t.Run(n, func(t *testing.T) {
			r := orgControlRequest("POST", "/", `{}`)
			r.SetPathValue("organizationID", avatarTestOwner)
			r.SetPathValue("membershipID", avatarTestOwner)
			w := httptest.NewRecorder()
			h(w, r)
			if w.Code != 409 || !strings.Contains(w.Body.String(), "organization_control_required") || p.writes != 0 {
				t.Fatal("legacy write bypass", w.Code, w.Body.String())
			}
		})
	}
}
func TestOrganizationControlReadClosedContextAndBody(t *testing.T) {
	p := &orgControlSpy{}
	s := &server{access: p}
	mux := http.NewServeMux()
	s.registerOrganizationControlRoutes(mux)
	for name, change := range map[string]func(*http.Request){"body": func(r *http.Request) { r.Body = http.NoBody; r.Body = io.NopCloser(strings.NewReader(`{}`)) }, "query": func(r *http.Request) { r.URL.RawQuery = "q=someone" }, "organizationContext": func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", avatarTestOwner) }} {
		t.Run(name, func(t *testing.T) {
			r := orgControlRequest("GET", "/v1/me/organizations/"+avatarTestOwner+"/members/control", "")
			change(r)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 400 && w.Code != 403 {
				t.Fatal("closed private read accepted", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "place_photo") {
				t.Fatal("unrelated domain error leaked into organization read")
			}
		})
	}
}
