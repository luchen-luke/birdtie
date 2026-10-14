package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const hcTestID = "11111111-1111-1111-1111-111111111111"
const hcTestOp = "22222222-2222-2222-2222-222222222222"

type hcSpy struct {
	identity.AccessStore
	organization.FAQStore
	contextgraph.DeclarationStore
	safety.Store
	writes, reads, stops, proofs int
	final                        error
	lost, missing                bool
}

func (p *hcSpy) AuthenticateHumanSocial(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: hcTestID, AccountType: "person"}, nil
}
func (p *hcSpy) ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error {
	return p.final
}
func (p *hcSpy) CaptureHumanControlSession(context.Context, [32]byte, identity.Actor) (identity.HumanControlFrame, error) {
	return identity.HumanControlFrame{ID: hcTestID, Generation: 1}, nil
}
func (p *hcSpy) ValidateHumanControlSession(context.Context, [32]byte, identity.Actor, identity.HumanControlFrame) error {
	return p.final
}
func (p *hcSpy) proof() identity.HumanControlProof {
	return humanControlFunctionProof(func(context.Context) error { p.proofs++; return p.final })
}
func (p *hcSpy) base(op, action, digest string) identity.HumanControlPreview {
	now := time.Now()
	return identity.HumanControlPreview{OperationID: op, Action: action, PayloadDigest: digest, ObservedAt: now, ExpiresAt: now.Add(30 * time.Second), Approval: "EXACT_INPUT", ResponseProof: p.proof()}
}
func (p *hcSpy) receipt(family, op, action, target string, stop bool) (identity.HumanControlReceipt, error) {
	if stop {
		p.stops++
		return identity.HumanControlReceipt{Family: family, OperationID: op, Status: "STOPPED"}, nil
	}
	p.reads++
	if p.missing {
		return identity.HumanControlReceipt{}, identity.ErrNotFound
	}
	now := time.Now()
	v := identity.HumanControlReceipt{Family: family, OperationID: op, Status: "SUCCEEDED", Action: action, PayloadDigest: strings.Repeat("a", 64), AppliedVersion: 1, TargetID: target, CommittedAt: &now}
	if family == "FAQ" || family == "CONTEXT" {
		v.ResultID = target
	}
	if family == "REPORT" {
		v.AppliedVersion = 0
		v.ResultID = hcTestID
	}
	return v, nil
}
func (p *hcSpy) ReadFAQControl(context.Context, [32]byte, identity.Actor, string) (organization.FAQControlState, error) {
	if p.lost {
		return organization.FAQControlState{}, organization.ErrForbidden
	}
	return organization.FAQControlState{OrganizationID: hcTestID, Role: "owner", Items: []organization.FAQ{}, ResponseProof: p.proof()}, nil
}
func (p *hcSpy) PreviewFAQControl(_ context.Context, _ [32]byte, _ identity.Actor, id string, in organization.FAQControlInput) (organization.FAQControlPreview, error) {
	return organization.FAQControlPreview{HumanControlPreview: p.base(in.OperationID, in.Action, organization.FAQControlDigest(id, in)), OrganizationID: id, FAQID: in.FAQID, ExpectedVersion: in.ExpectedVersion, Question: in.Question, Answer: in.Answer, Published: in.Published}, nil
}
func (p *hcSpy) CommitFAQControl(context.Context, [32]byte, identity.Actor, string, organization.FAQControlInput) (identity.HumanControlReceipt, error) {
	p.writes++
	return p.receipt("FAQ", hcTestOp, "CREATE", hcTestID, false)
}
func (p *hcSpy) ReadFAQControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, _, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("FAQ", op, "CREATE", hcTestID, false)
}
func (p *hcSpy) StopFAQControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, _, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("FAQ", op, "", hcTestID, true)
}
func (p *hcSpy) ReadDeclarationControl(context.Context, [32]byte, identity.Actor) (contextgraph.DeclarationControlState, error) {
	return contextgraph.DeclarationControlState{Version: 0, Items: []contextgraph.Declaration{}, ResponseProof: p.proof()}, nil
}
func (p *hcSpy) PreviewDeclarationControl(_ context.Context, _ [32]byte, _ identity.Actor, in contextgraph.DeclarationControlInput) (contextgraph.DeclarationControlPreview, error) {
	return contextgraph.DeclarationControlPreview{HumanControlPreview: p.base(in.OperationID, in.Action, contextgraph.DeclarationControlDigest(in)), ExpectedVersion: in.ExpectedVersion, Type: in.Type, SourceKey: in.SourceKey, Relation: in.Relation}, nil
}
func (p *hcSpy) CommitDeclarationControl(context.Context, [32]byte, identity.Actor, contextgraph.DeclarationControlInput) (identity.HumanControlReceipt, error) {
	p.writes++
	return p.receipt("CONTEXT", hcTestOp, "DECLARE", hcTestID, false)
}
func (p *hcSpy) ReadDeclarationControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("CONTEXT", op, "DECLARE", hcTestID, false)
}
func (p *hcSpy) StopDeclarationControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("CONTEXT", op, "", hcTestID, true)
}
func (p *hcSpy) ReadReportControl(context.Context, [32]byte, identity.Actor) (safety.ReportControlState, error) {
	return safety.ReportControlState{RemainingQuota: 5, CanSubmit: true, ResponseProof: p.proof()}, nil
}
func (p *hcSpy) PreviewReportControl(_ context.Context, _ [32]byte, _ identity.Actor, in safety.ReportControlInput) (safety.ReportControlPreview, error) {
	return safety.ReportControlPreview{HumanControlPreview: p.base(in.OperationID, in.Action, safety.ReportControlDigest(in)), ReportInput: in.ReportInput}, nil
}
func (p *hcSpy) CommitReportControl(context.Context, [32]byte, identity.Actor, safety.ReportControlInput) (identity.HumanControlReceipt, error) {
	p.writes++
	return p.receipt("REPORT", hcTestOp, "SUBMIT", "", false)
}
func (p *hcSpy) ReadReportControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("REPORT", op, "SUBMIT", "", false)
}
func (p *hcSpy) StopReportControlOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("REPORT", op, "", "", true)
}
func (p *hcSpy) ReadGrantRevokeControl(context.Context, [32]byte, identity.Actor, string) (identity.GrantRevokeControlState, error) {
	return identity.GrantRevokeControlState{Grant: identity.Grant{ID: hcTestID, Revision: 1}, CanRevoke: true, ResponseProof: p.proof()}, nil
}
func (p *hcSpy) PreviewGrantRevokeControl(_ context.Context, _ [32]byte, _ identity.Actor, in identity.GrantRevokeControlInput) (identity.GrantRevokeControlPreview, error) {
	return identity.GrantRevokeControlPreview{HumanControlPreview: p.base(in.OperationID, in.Action, identity.GrantRevokeControlDigest(in)), GrantID: in.GrantID, ExpectedVersion: in.ExpectedVersion}, nil
}
func (p *hcSpy) CommitGrantRevokeControl(context.Context, [32]byte, identity.Actor, identity.GrantRevokeControlInput) (identity.HumanControlReceipt, error) {
	p.writes++
	return p.receipt("GRANT_REVOKE", hcTestOp, "REVOKE", hcTestID, false)
}
func (p *hcSpy) ReadGrantRevokeOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("GRANT_REVOKE", op, "REVOKE", hcTestID, false)
}
func (p *hcSpy) StopGrantRevokeOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("GRANT_REVOKE", op, "", hcTestID, true)
}
func (p *hcSpy) ReadBlockControl(context.Context, [32]byte, identity.Actor, string) (identity.BlockControlState, error) {
	return identity.BlockControlState{AccountID: hcTestID, Version: 0, CanBlock: true, ResponseProof: p.proof()}, nil
}
func (p *hcSpy) PreviewBlockControl(_ context.Context, _ [32]byte, _ identity.Actor, in identity.BlockControlInput) (identity.BlockControlPreview, error) {
	return identity.BlockControlPreview{HumanControlPreview: p.base(in.OperationID, in.Action, identity.BlockControlDigest(in)), AccountID: in.AccountID, ExpectedVersion: in.ExpectedVersion}, nil
}
func (p *hcSpy) CommitBlockControl(context.Context, [32]byte, identity.Actor, identity.BlockControlInput) (identity.HumanControlReceipt, error) {
	p.writes++
	return p.receipt("BLOCK", hcTestOp, "BLOCK", hcTestID, false)
}
func (p *hcSpy) ReadBlockOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("BLOCK", op, "BLOCK", hcTestID, false)
}
func (p *hcSpy) StopBlockOperation(_ context.Context, _ [32]byte, _ identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return p.receipt("BLOCK", op, "", hcTestID, true)
}
func hcServer(p *hcSpy) *http.ServeMux {
	s := &server{access: p, faqs: p, contextDeclarations: p, safety: p}
	m := http.NewServeMux()
	s.registerHumanWriteControlRoutes(m)
	m.HandleFunc("POST /v1/me/contexts", s.declareOwnContext)
	m.HandleFunc("POST /v1/me/reports", s.createIncidentReport)
	m.HandleFunc("POST /v1/me/blocks", s.blockAccount)
	m.HandleFunc("DELETE /v1/me/blocks/{accountID}", s.unblockAccount)
	m.HandleFunc("DELETE /v1/me/consents/{grantID}", s.revokeConsent)
	m.HandleFunc("POST /v1/organizations/{organizationID}/faqs", s.createOrganizationFAQ)
	return m
}
func hcRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	token, _, e := identity.NewToken()
	if e != nil {
		panic(e)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	return r
}
func hcInputs() []struct{ name, base, body string } {
	return []struct{ name, base, body string }{{"FAQ", "/v1/me/organizations/" + hcTestID + "/faq-control", `{"operationId":"` + hcTestOp + `","action":"CREATE","faqId":"","expectedVersion":0,"question":"活动时间","answer":"上午九点","published":false}`}, {"CONTEXT", "/v1/me/context-control", `{"operationId":"` + hcTestOp + `","action":"DECLARE","expectedVersion":0,"contextType":"CITY","sourceKey":"aberdeen-gb","relation":"current"}`}, {"REPORT", "/v1/me/report-control", `{"operationId":"` + hcTestOp + `","action":"SUBMIT","targetType":"general","targetId":"","reason":"technical","details":"这是一份足够详细的故障说明"}`}, {"GRANT_REVOKE", "/v1/me/consent-revoke-control", `{"operationId":"` + hcTestOp + `","action":"REVOKE","grantId":"` + hcTestID + `","expectedVersion":1}`}, {"BLOCK", "/v1/me/block-control", `{"operationId":"` + hcTestOp + `","action":"BLOCK","accountId":"` + hcTestID + `","expectedVersion":0}`}}
}
func TestHumanWriteControlHTTPClosedFiveFamiliesAndUnknown(t *testing.T) {
	for _, c := range hcInputs() {
		t.Run(c.name, func(t *testing.T) {
			p := &hcSpy{}
			mux := hcServer(p)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("POST", c.base+"/previews", c.body))
			if w.Code != 200 || p.proofs != 1 {
				t.Fatal("actual preview", w.Code, w.Body.String())
			}
			var out map[string]json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &out) != nil {
				t.Fatal("invalid envelope")
			}
			w = httptest.NewRecorder()
			bad := strings.TrimSuffix(c.body, "}") + `,"actorId":"` + hcTestID + `"}`
			mux.ServeHTTP(w, hcRequest("PUT", c.base+"/operations/"+hcTestOp, bad))
			if w.Code != 400 || p.writes != 0 {
				t.Fatal("extra actor reaches write", w.Code)
			}
			p.final = identity.ErrUnauthorized
			w = httptest.NewRecorder()
			commit := strings.TrimSuffix(c.body, "}") + `,"approval":"EXACT_INPUT"}`
			mux.ServeHTTP(w, hcRequest("PUT", c.base+"/operations/"+hcTestOp, commit))
			if w.Code != 401 || p.writes != 1 || strings.Contains(w.Body.String(), "SUCCEEDED") {
				t.Fatal("postcommit denied still definitive", w.Code, w.Body.String())
			}
			p.final = nil
			p.lost = true
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("GET", c.base+"/operations/"+hcTestOp, ""))
			if w.Code != 200 || !strings.Contains(w.Body.String(), "SUCCEEDED") {
				t.Fatal("own receipt hidden by current role", w.Code, w.Body.String())
			}
			p.missing = true
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("GET", c.base+"/operations/"+hcTestOp, ""))
			if w.Code != 404 || p.writes != 1 {
				t.Fatal("404 became rewrite")
			}
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("POST", c.base+"/operations/"+hcTestOp+"/stop", `{}`))
			if w.Code != 400 || p.stops != 0 {
				t.Fatal("stop accepted body")
			}
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, hcRequest("POST", c.base+"/operations/"+hcTestOp+"/stop", ""))
			if w.Code != 200 || p.stops != 1 || !strings.Contains(w.Body.String(), `"status":"STOPPED"`) {
				t.Fatal("own bodyless stop", w.Code, w.Body.String())
			}
		})
	}
}
func TestHumanWriteControlHTTPNoLegacyFallbackAndOrganizationBorrow(t *testing.T) {
	for _, c := range []struct{ method, path string }{{"POST", "/v1/me/contexts"}, {"POST", "/v1/me/reports"}, {"POST", "/v1/me/blocks"}, {"DELETE", "/v1/me/blocks/" + hcTestID}, {"DELETE", "/v1/me/consents/" + hcTestID}, {"POST", "/v1/organizations/" + hcTestID + "/faqs"}} {
		t.Run(c.method+c.path, func(t *testing.T) {
			p := &hcSpy{}
			w := httptest.NewRecorder()
			hcServer(p).ServeHTTP(w, hcRequest(c.method, c.path, `{}`))
			if w.Code != 409 || p.writes != 0 {
				t.Fatal("legacy fallback", w.Code)
			}
		})
	}
	p := &hcSpy{}
	for _, c := range hcInputs() {
		r := hcRequest("POST", c.base+"/previews", c.body)
		r.Header.Set("X-Birdtie-Organization-Workspace", hcTestID)
		w := httptest.NewRecorder()
		hcServer(p).ServeHTTP(w, r)
		if w.Code != 403 || p.writes != 0 {
			t.Fatal("organization lent person authority", c.name, w.Code)
		}
	}
}
