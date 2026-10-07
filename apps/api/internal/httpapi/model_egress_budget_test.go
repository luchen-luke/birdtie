package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

const modelEgressHTTPBase = "/v1/me/model-egress"

// Boundary spy only; native identity, approvals and accounting are verified in
// the separately registered PostgreSQL suite, never supplied by this spy.
type modelEgressHTTPSpy struct {
	foundation.PublicCatalog
	err    error
	calls  int
	input  modelegressbudget.PreviewInput
	access agentevent.Access
}

func (s *modelEgressHTTPSpy) PreviewOwnModelEgress(_ context.Context, a agentevent.Access, in modelegressbudget.PreviewInput) (modelegressbudget.Preview, error) {
	s.calls++
	s.input = in
	s.access = a
	return modelegressbudget.Preview{}, s.err
}
func (s *modelEgressHTTPSpy) ApproveOwnModelEgress(_ context.Context, a agentevent.Access, _, _ string) error {
	s.calls++
	s.access = a
	return s.err
}
func (s *modelEgressHTTPSpy) RevokeOwnModelEgress(_ context.Context, a agentevent.Access, _ string) error {
	s.calls++
	s.access = a
	return s.err
}
func (s *modelEgressHTTPSpy) ReadOwnModelBudget(_ context.Context, a agentevent.Access, _, _ string) ([]modelegressbudget.BudgetView, error) {
	s.calls++
	s.access = a
	return []modelegressbudget.BudgetView{{Scope: "tenant", Currency: "GBP", Limits: modelegressbudget.Limits{Requests: 2}}}, s.err
}

func TestModelEgressHTTPRegisteredClosedWire(t *testing.T) {
	_, access, _, token := privateProfileHTTPFixture(t)
	spy := &modelEgressHTTPSpy{err: modelegressbudget.ErrUnavailable}
	h := privateProfileHTTPNew(spy, access)
	body := `{"rootTraceId":"` + privateProfileHTTPOwner + `","taskId":"` + privateProfileHTTPAgent + `","priceVersion":"local.v1","maxOutputTokens":64,"deadlineAt":"` + time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano) + `"}`
	for name, raw := range map[string]string{
		"duplicate":       strings.Replace(body, `"taskId":`, `"taskId":"`+privateProfileHTTPForeign+`","taskId":`, 1),
		"case":            strings.Replace(body, `"taskId"`, `"TaskId"`, 1),
		"null":            strings.Replace(body, `"priceVersion":"local.v1"`, `"priceVersion":null`, 1),
		"clientSource":    strings.TrimSuffix(body, "}") + `,"messages":[]}`,
		"clientOwner":     strings.TrimSuffix(body, "}") + `,"ownerId":"` + privateProfileHTTPOwner + `"}`,
		"clientApproval":  strings.TrimSuffix(body, "}") + `,"confirmed":true}`,
		"floatingTokens":  strings.Replace(body, `"maxOutputTokens":64`, `"maxOutputTokens":1.5`, 1),
		"oversizedTokens": strings.Replace(body, `"maxOutputTokens":64`, `"maxOutputTokens":4097`, 1),
		"badTime":         strings.Replace(body, `"deadlineAt":"`, `"deadlineAt":"NOT_A_TIME`, 1),
		"missing":         "{}", "trailing": body + "{}", "oversized": strings.Repeat("x", 8193),
	} {
		t.Run(name, func(t *testing.T) {
			before := spy.calls
			r := privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews", raw, token, "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 400 || spy.calls != before {
				t.Fatalf("invalid control reached native port: status=%d", w.Code)
			}
		})
	}
	for _, tail := range []string{"?ownerId=other", "?"} {
		t.Run("query"+tail, func(t *testing.T) {
			before := spy.calls
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews"+tail, body, token, "application/json"))
			if w.Code != 400 || spy.calls != before {
				t.Fatal("query controls accepted", w.Code)
			}
		})
	}
	for _, workspace := range []string{"", privateProfileHTTPOwner} {
		before := spy.calls
		r := privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews", body, token, "application/json")
		r.Header["X-Birdtie-Organization-Workspace"] = []string{workspace}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || spy.calls != before {
			t.Fatal("workspace accepted")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews", body, token, "application/json"))
	if w.Code != 503 || spy.calls != 1 || spy.input.TaskID != privateProfileHTTPAgent || spy.access.SessionDigest != access.digest {
		t.Fatal("registered preview not wired to native identity", w.Code, spy.calls)
	}
	for _, kind := range []string{"organization", "business"} {
		access.actor.AccountType = kind
		before := spy.calls
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews", body, token, "application/json"))
		if w.Code != 403 || spy.calls != before {
			t.Fatal("non-person model review accepted")
		}
	}
	access.actor.AccountType = "person"
	access.err = identity.ErrUnauthorized
	before := spy.calls
	w = httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews", body, token, "application/json"))
	if w.Code != 401 || spy.calls != before {
		t.Fatal("invalid identity accepted")
	}
}
func TestModelEgressHTTPRegisteredApprovalRevokeBudget(t *testing.T) {
	_, access, _, token := privateProfileHTTPFixture(t)
	spy := &modelEgressHTTPSpy{}
	h := privateProfileHTTPNew(spy, access)
	approve := `{"previewId":"` + privateProfileHTTPOwner + `","requestDigest":"` + strings.Repeat("a", 64) + `"}`
	for name, body := range map[string]string{"duplicate": strings.Replace(approve, `"previewId":`, `"previewId":"bad","previewId":`, 1), "case": strings.Replace(approve, "requestDigest", "RequestDigest", 1), "null": strings.Replace(approve, `"`+strings.Repeat("a", 64)+`"`, "null", 1), "blank": "{}", "fakeRevision": strings.TrimSuffix(approve, "}") + `,"expectedRevision":1}`, "bodyAuthority": strings.TrimSuffix(approve, "}") + `,"request":{}}`} {
		t.Run(name, func(t *testing.T) {
			before := spy.calls
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/approvals", body, token, "application/json"))
			if w.Code != 400 || spy.calls != before {
				t.Fatal("approval wire widened", w.Code)
			}
		})
	}
	for _, route := range []struct{ method, path, body string }{{"POST", modelEgressHTTPBase + "/approvals", approve}, {"DELETE", modelEgressHTTPBase + "/previews/" + privateProfileHTTPOwner, ""}, {"GET", modelEgressHTTPBase + "/roots/" + privateProfileHTTPOwner + "/tasks/" + privateProfileHTTPAgent + "/budget", ""}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest(route.method, route.path, route.body, token, "application/json"))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "UNAVAILABLE") {
			t.Fatal("human route lost closed model boundary", w.Code)
		}
		if route.method != "POST" {
			before := spy.calls
			w = httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest(route.method, route.path, `{}`, token, "application/json"))
			if w.Code != 400 || spy.calls != before {
				t.Fatal("read/revoke accepted client body")
			}
		}
	}
	for _, path := range []string{"/reserve", "/begin", "/settle", "/prices", "/quotas", "/runtime"} {
		before := spy.calls
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+path, `{}`, token, "application/json"))
		if w.Code != 404 || spy.calls != before {
			t.Fatal("dispatch or maintenance route exposed")
		}
	}
}
func TestModelEgressHumanDisplayNeverExportsAuthority(t *testing.T) {
	r := modelgateway.Request{Messages: []modelgateway.Message{{Role: "user", Content: "本人当前查询"}}, Budget: modelgateway.Budget{MaxOutputTokens: 64}}
	p := modelegressbudget.Price{Version: "local.v1", Destination: modelcapability.Key{Provider: "local", Model: "fixture", Version: "v1", WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: modelegressbudget.Retention, Currency: "GBP", InputMicrosPerToken: 2, OutputMicrosPerToken: 3, InputTokenCeiling: 100, OutputTokenCeiling: 128, Evidence: modelegressbudget.LocalPrice}
	preview := modelegressbudget.Preview{ID: privateProfileHTTPOwner, Request: r, Price: p, RequestDigest: modelegressbudget.Digest(r, p), SourceToken: "PRIVATE_NATIVE_SOURCE_TOKEN", AuthorityToken: "PRIVATE_NATIVE_AUTHORITY_TOKEN"}
	d, e := humanEgressDisplay(preview)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"本人当前查询", "LOCAL_SYNTHETIC", "requestDigest", "UNAVAILABLE"} {
		if !strings.Contains(string(raw), v) {
			t.Fatal("human review missing", v)
		}
	}
	for _, v := range []string{"PRIVATE_NATIVE", "sourceToken", "authorityToken"} {
		if strings.Contains(string(raw), v) {
			t.Fatal("human view leaked native authority")
		}
	}
	d.Request.Messages[0].Content = "mutated"
	if preview.Request.Messages[0].Content != "本人当前查询" {
		t.Fatal("display aliased source")
	}
	preview.RequestDigest = strings.Repeat("b", 64)
	if _, e = humanEgressDisplay(preview); e == nil {
		t.Fatal("digest mismatch displayed")
	}
}

func TestModelEgressHTTPMissingPortAndFailureRedaction(t *testing.T) {
	_, access, _, token := privateProfileHTTPFixture(t)
	body := `{"previewId":"` + privateProfileHTTPOwner + `","requestDigest":"` + strings.Repeat("a", 64) + `"}`
	h := privateProfileHTTPNew(struct{ foundation.PublicCatalog }{}, access)
	for _, tc := range []struct {
		token  string
		status int
	}{{token, 503}, {"", 401}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/approvals", body, tc.token, "application/json"))
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing native port invented success or borrowed identity", w.Code)
		}
	}
	spy := &modelEgressHTTPSpy{}
	h = privateProfileHTTPNew(spy, access)
	for _, tc := range []struct {
		err    error
		status int
	}{{modelegressbudget.ErrInvalid, 400}, {modelegressbudget.ErrDenied, 403}, {modelegressbudget.ErrConflict, 409}, {modelegressbudget.ErrBudget, 409}, {modelegressbudget.ErrUnavailable, 503}, {fmt.Errorf("PRIVATE_ERROR_SQL_TOKEN_CANARY"), 503}} {
		spy.err = tc.err
		w := httptest.NewRecorder()
		h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/approvals", body, token, "application/json"))
		if w.Code != tc.status || strings.Contains(w.Body.String(), "PRIVATE_ERROR") {
			t.Fatal("failure semantic or redaction changed", w.Code)
		}
	}
	before := spy.calls
	w := httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/approvals", body, token, "text/plain"))
	if w.Code != 415 || spy.calls != before {
		t.Fatal("non-JSON approval accepted")
	}
}
