package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConnectionDecisionOperationRegisteredLookup(t *testing.T) {
	h, _, _, _, token := requestListSessionUnitFixture(t)
	r := httptest.NewRequest("GET", "/v1/me/connection-requests/"+privateProfileHTTPAgent+"/decision-operations/"+privateProfileHTTPForeign, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusNotFound {
		t.Fatal("actual registered human API has no decision operation lookup", w.Code)
	}
}

type connectionOperationHTTPUnit struct {
	*messagePolicyHTTPSpy
	receipt       connection.DecisionOperationReceipt
	failure       error
	writes, reads int
	final         int
	after         func()
}

func (p *connectionOperationHTTPUnit) DecideRequestOperation(_ context.Context, a ea.Access, id, action, op string) (connection.DecisionOperationReceipt, error) {
	p.writes++
	p.write = a
	p.action = action
	if p.after != nil {
		p.after()
	}
	return p.receipt, p.failure
}
func (p *connectionOperationHTTPUnit) ReadRequestDecisionOperation(_ context.Context, a ea.Access, id, op string) (connection.DecisionOperationReceipt, error) {
	p.reads++
	p.write = a
	if p.after != nil {
		p.after()
	}
	return p.receipt, p.failure
}
func (p *connectionOperationHTTPUnit) ValidateRequestDecisionOperationResponse(_ context.Context, a ea.Access, receipt connection.DecisionOperationReceipt) error {
	p.final++
	if a != p.write || receipt != p.receipt {
		return connection.ErrOperationChanged
	}
	return p.failure
}
func TestConnectionDecisionOperationRegisteredCausalWire(t *testing.T) {
	const owner = "11111111-1111-4111-8111-111111111111"
	const request = "33333333-3333-4333-8333-333333333333"
	const op = "44444444-4444-4444-8444-444444444444"
	for _, name := range []string{"commit", "lookup", "no_effect", "no_key_legacy", "key_changed", "missing_unknown", "bad_receipt", "late_session", "org", "unknown_field", "null_key"} {
		t.Run(name, func(t *testing.T) {
			h, s, _, a, token := requestListSessionUnitFixture(t)
			a.actor.ID = owner
			d, _ := connection.DecisionDigest(owner, request, "accept")
			p := &connectionOperationHTTPUnit{messagePolicyHTTPSpy: &messagePolicyHTTPSpy{}, receipt: connection.DecisionOperationReceipt{SchemaVersion: connection.DecisionOperationSchema, OwnerID: owner, RequestID: request, OperationID: op, RequestDigest: d, Action: "accept", Scope: "friend", Status: "COMMITTED", State: "accepted", RecordedAt: time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)}}
			s.connections = p
			method, path, body, want := "POST", "/v1/me/connection-requests/"+request+"/decision", `{"action":"accept","operationId":"`+op+`"}`, 200
			switch name {
			case "lookup", "missing_unknown":
				method = "GET"
				path = "/v1/me/connection-requests/" + request + "/decision-operations/" + op
				body = ""
				if name == "missing_unknown" {
					p.failure = connection.ErrNotFound
					want = 404
				}
			case "no_effect":
				p.receipt.Status = "NO_EFFECT"
				p.receipt.State = ""
				p.receipt.Reason = "EXPIRED"
			case "no_key_legacy":
				body = `{"action":"accept"}`
			case "key_changed":
				p.failure = connection.ErrOperationChanged
				want = 409
			case "bad_receipt":
				p.receipt.OwnerID = op
				want = 503
			case "late_session":
				p.after = func() { a.finalErr = identity.ErrUnauthorized }
				want = 401
			case "org":
				want = 403
			case "unknown_field":
				body = `{"action":"accept","operationId":"` + op + `","confirmed":true}`
				want = 400
			case "null_key":
				body = `{"action":"accept","operationId":null}`
				want = 400
			}
			r := httptest.NewRequest(method, path, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer "+token)
			if method == "POST" {
				r.Header.Set("Content-Type", "application/json")
			}
			if name == "org" {
				r.Header.Set("X-Birdtie-Organization-Workspace", op)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s status=%d want=%d wire=%s", name, w.Code, want, w.Body)
			}
			if name == "no_key_legacy" {
				if p.calls != 1 || p.writes != 0 {
					t.Fatal("legacy bypass original writer")
				}
			} else if want == 200 {
				if p.final != 1 {
					t.Fatal("keyed response omitted original current receipt validation")
				}
				if p.write.Actor.ID != owner || p.write.SessionDigest != a.digest {
					t.Fatal("caller capture changed")
				}
				var value struct {
					Data connection.DecisionOperationReceipt `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &value) != nil || connection.ValidateDecisionReceipt(value.Data, owner, request, op, "accept") != nil {
					t.Fatal("actual registered output contract")
				}
			}
			if want != 200 && strings.Contains(w.Body.String(), "requestDigest") {
				t.Fatal("failed authority disclosed receipt")
			}
			if dir := os.Getenv("BIRDTIE_CONNECTION_OPERATION_WIRE_DIR"); dir != "" {
				if e := os.MkdirAll(dir, 0700); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(filepath.Join(dir, name+".json"), w.Body.Bytes(), 0600); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestConnectionDecisionOperationUnavailablePortStopsBeforeDispatch(t *testing.T) {
	for _, name := range []string{"typed_nil_session", "typed_nil_store", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			h, s, _, _, token := requestListSessionUnitFixture(t)
			p := &connectionOperationHTTPUnit{messagePolicyHTTPSpy: &messagePolicyHTTPSpy{}}
			s.connections = p
			r := httptest.NewRequest("GET", "/v1/me/connection-requests/"+privateProfileHTTPAgent+"/decision-operations/"+privateProfileHTTPForeign, nil)
			r.Header.Set("Authorization", "Bearer "+token)
			if name == "typed_nil_session" {
				var a *requestListSessionUnitAccess
				s.access = a
			}
			if name == "typed_nil_store" {
				var empty *connectionOperationHTTPUnit
				s.connections = empty
			}
			if name == "cancelled" {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 503 || p.reads != 0 || strings.Contains(w.Body.String(), "operationId") {
				t.Fatal("unavailable is not empty/no-effect", w.Code, w.Body)
			}
		})
	}
}
