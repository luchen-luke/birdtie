package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ap "github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestHumanParticipationOperationRegisteredRoutes(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	p := "/v1/activities/11111111-1111-4111-8111-111111111111/participation-operations"
	for _, c := range []struct{ method, path string }{{"POST", p}, {"GET", p + "/22222222-2222-4222-8222-222222222222"}, {"POST", p + "/22222222-2222-4222-8222-222222222222/close"}, {"GET", p + "/22222222-2222-4222-8222-222222222222/closure"}} {
		t.Run(c.method+c.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, strings.NewReader("")))
			if w.Code != 503 {
				t.Fatalf("registered unavailable native port status=%d want503", w.Code)
			}
		})
	}
}

// These spies test transport and exact response binding, not PostgreSQL effects.
type participationHTTPSpy struct {
	ap.Store
	writes, reads, closes, closureReads, final int
	receipt                                    ap.OperationReceipt
	err, finalErr                              error
	badDigest                                  bool
}

func (p *participationHTTPSpy) WriteHumanParticipationOperation(_ context.Context, a ea.Access, id string, in ap.OperationInput, b ea.BoundCondition) (ap.OperationReceipt, bool, error) {
	p.writes++
	r := p.receipt
	r.RequestDigest, _ = ap.HumanParticipationRequestDigest(a.Actor.ID, id, in, b)
	if p.badDigest {
		r.RequestDigest = strings.Repeat("b", 64)
	}
	return r, true, p.err
}
func (p *participationHTTPSpy) ReadHumanParticipationOperation(_ context.Context, _ ea.Access, _, _ string) (ap.OperationReceipt, error) {
	p.reads++
	return p.receipt, p.err
}
func (p *participationHTTPSpy) CloseHumanParticipationOperation(_ context.Context, _ ea.Access, _, _ string, in ap.OperationCloseInput) (ap.OperationClosure, error) {
	p.closes++
	r := ap.NewOperationClosure(p.receipt)
	r.RequestDigest = in.RequestDigest
	return r, p.err
}
func (p *participationHTTPSpy) ReadHumanParticipationClosure(_ context.Context, _ ea.Access, _, _ string) (ap.OperationClosure, error) {
	p.closureReads++
	return ap.NewOperationClosure(p.receipt), p.err
}
func (p *participationHTTPSpy) ValidateHumanParticipationOperationResponse(_ context.Context, _ ea.Access, _ ap.OperationReceipt) error {
	p.final++
	return p.finalErr
}
func (p *participationHTTPSpy) ValidateHumanParticipationClosureResponse(_ context.Context, _ ea.Access, _ ap.OperationClosure) error {
	p.final++
	return p.finalErr
}

type participationHTTPCatalog struct {
	foundation.PublicCatalog
	*participationHTTPSpy
}

func participationHTTP(t *testing.T) (http.Handler, *participationHTTPSpy, string, map[string]string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	n := time.Date(2026, 10, 10, 1, 2, 4, 0, time.UTC)
	effect := n.Add(-time.Second)
	p := &participationHTTPSpy{receipt: ap.OperationReceipt{SchemaVersion: ap.HumanParticipationOperationSchema, OwnerID: privateProfileHTTPOwner, ActivityID: humanHTTPConv, OperationID: humanHTTPOp, Operation: "JOIN", EffectKey: ap.HumanParticipationEffectKey(privateProfileHTTPOwner, humanHTTPOp), RequestDigest: strings.Repeat("a", 64), ToolVersion: ap.HumanParticipationToolVersion, Status: "COMMITTED", RecordedAt: n, EffectID: privateProfileHTTPAgent, State: "going", EffectAt: &effect}}
	headers := map[string]string{"X-Birdtie-Action-Version": strings.Repeat("a", 64), "X-Birdtie-Action-Until": "2026-10-10T01:02:03.123456Z", "X-Birdtie-Action-Operation": "JOIN"}
	return New(participationHTTPCatalog{participationHTTPSpy: p}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil), p, token, headers
}
func participationHTTPCall(h http.Handler, method, path, body, token string, headers map[string]string, modify func(*http.Request)) *httptest.ResponseRecorder {
	r := privateProfileHTTPRequest(method, path, body, token, "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if modify != nil {
		modify(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestHumanParticipationOperationHTTPCurrentAndOpaque(t *testing.T) {
	for _, kind := range []string{"write", "read", "close", "closure"} {
		t.Run(kind, func(t *testing.T) {
			h, p, token, headers := participationHTTP(t)
			path := "/v1/activities/" + humanHTTPConv + "/participation-operations"
			method, body, want := "GET", "", 200
			switch kind {
			case "write":
				method = "POST"
				body = `{"operationId":"` + humanHTTPOp + `","operation":"JOIN"}`
				want = 201
			case "read":
				path += "/" + humanHTTPOp
				headers = nil
			case "close":
				method = "POST"
				path += "/" + humanHTTPOp + "/close"
				headers = nil
				body = `{"operation":"JOIN","requestDigest":"` + strings.Repeat("a", 64) + `"}`
			case "closure":
				path += "/" + humanHTTPOp + "/closure"
				headers = nil
			}
			w := participationHTTPCall(h, method, path, body, token, headers, nil)
			if w.Code != want || p.final != 1 || p.writes+p.reads+p.closes+p.closureReads != 1 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Body, p)
			}
			if kind == "close" || kind == "closure" {
				var v map[string]any
				if json.Unmarshal(w.Body.Bytes(), &v) != nil {
					t.Fatal(w.Body)
				}
				for _, key := range []string{"status", "state", "effectId", "effectAt", "reason"} {
					if strings.Contains(w.Body.String(), `"`+key+`"`) {
						t.Fatal("closure disclosed actual effect", w.Body)
					}
				}
			}
		})
	}
}
func TestHumanParticipationOperationHTTPRejectsAndLateGate(t *testing.T) {
	for _, tc := range []string{"no-auth", "org-workspace", "query", "force-query", "missing-approval", "duplicate-approval", "mismatch-action", "duplicate-json", "unknown-json", "nil-op", "submicro", "wrong-digest", "not-found", "conflict", "unavailable", "late-session", "bad-owner", "bad-schema", "late-source"} {
		t.Run(tc, func(t *testing.T) {
			h, p, token, headers := participationHTTP(t)
			path := "/v1/activities/" + humanHTTPConv + "/participation-operations"
			body := `{"operationId":"` + humanHTTPOp + `","operation":"JOIN"}`
			want, calls := 400, 0
			var modify func(*http.Request)
			switch tc {
			case "no-auth":
				token = ""
				want = 401
			case "org-workspace":
				headers["X-Birdtie-Organization-Workspace"] = privateProfileHTTPForeign
			case "query":
				path += "?ownerId=" + privateProfileHTTPForeign
			case "force-query":
				path += "?"
			case "missing-approval":
				headers = nil
			case "duplicate-approval":
				modify = func(r *http.Request) { r.Header.Add("X-Birdtie-Action-Version", strings.Repeat("a", 64)) }
			case "mismatch-action":
				headers["X-Birdtie-Action-Operation"] = "CANCEL_RSVP"
			case "duplicate-json":
				body = `{"operationId":"` + humanHTTPOp + `","operation":"JOIN","operation":"JOIN"}`
			case "unknown-json":
				body = `{"operationId":"` + humanHTTPOp + `","operation":"JOIN","confirmed":true}`
			case "nil-op":
				body = strings.Replace(body, humanHTTPOp, "00000000-0000-0000-0000-000000000000", 1)
			case "submicro":
				headers["X-Birdtie-Action-Until"] = "2026-10-10T01:02:03.123456001Z"
			case "wrong-digest":
				p.badDigest = true
				want, calls = 503, 1
			case "not-found":
				p.err = ea.ErrNotFound
				want, calls = 404, 1
			case "conflict":
				p.err = ap.ErrOperationConflict
				want, calls = 409, 1
			case "unavailable":
				p.err = ea.ErrUnavailable
				want, calls = 503, 1
			case "late-session":
				p.finalErr = identity.ErrUnauthorized
				want, calls = 401, 1
			case "bad-owner":
				p.receipt.OwnerID = privateProfileHTTPForeign
				want, calls = 503, 1
			case "bad-schema":
				p.receipt.SchemaVersion = "x"
				want, calls = 503, 1
			case "late-source":
				p.finalErr = ea.ErrChanged
				want, calls = 409, 1
			}
			w := participationHTTPCall(h, "POST", path, body, token, headers, modify)
			if w.Code != want || p.writes != calls || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatal(w.Code, want, w.Body, p)
			}
		})
	}
}
func TestHumanParticipationOperationHTTPReadAndCloseNoOldApproval(t *testing.T) {
	for _, kind := range []string{"read-body", "read-approval", "closure-body", "close-approval", "close-confirmed", "close-mismatch-target"} {
		t.Run(kind, func(t *testing.T) {
			h, p, token, headers := participationHTTP(t)
			path := "/v1/activities/" + humanHTTPConv + "/participation-operations/" + humanHTTPOp
			method, body, want := "GET", "", 400
			switch kind {
			case "read-body":
				headers = nil
				body = "{}"
			case "read-approval":
			case "closure-body":
				path += "/closure"
				headers = nil
				body = "{}"
			case "close-approval":
				method = "POST"
				path += "/close"
				body = `{"operation":"JOIN","requestDigest":"` + strings.Repeat("a", 64) + `"}`
			case "close-confirmed":
				method = "POST"
				path += "/close"
				headers = nil
				body = `{"operation":"JOIN","requestDigest":"` + strings.Repeat("a", 64) + `","confirmed":true}`
			case "close-mismatch-target":
				method = "POST"
				path = strings.Replace(path, humanHTTPConv, privateProfileHTTPForeign, 1) + "/close"
				headers = nil
				body = `{"operation":"JOIN","requestDigest":"` + strings.Repeat("a", 64) + `"}`
				want = 503
			}
			w := participationHTTPCall(h, method, path, body, token, headers, nil)
			if w.Code != want || strings.Contains(w.Body.String(), `"effectId"`) {
				t.Fatal(w.Code, want, w.Body, p)
			}
		})
	}
}
