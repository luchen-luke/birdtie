package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Registered transport RED first. The caller is a synthetic current human;
// this test does not claim PostgreSQL or real IdP verification.
func TestCloseConnectionsRegisteredMissingCapability(t *testing.T) {
	h, _, _, token := messagePolicyUnitHTTP(t)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			path := "/v1/me/close-connections"
			if method != "GET" {
				path += "/" + privateProfileHTTPForeign
			}
			w := messagePolicyUnitRequest(h, method, path, "", token, nil)
			if w.Code != 503 {
				t.Fatalf("registered Close route without typed current capability: want 503, got %d", w.Code)
			}
		})
	}
}

type closeHTTPSpy struct {
	connection.Store
	view          connection.CloseView
	calls, checks int
	access        ea.Access
	put           connection.ClosePut
	del           connection.CloseDelete
	peer          string
	err, finalErr error
	after         func()
}

func (p *closeHTTPSpy) called(a ea.Access) {
	p.calls++
	p.access = a
	if p.after != nil {
		p.after()
	}
}
func (p *closeHTTPSpy) ListOwnCloseConnectionsCurrent(_ context.Context, a ea.Access) (connection.CloseView, error) {
	p.called(a)
	return p.view, p.err
}
func (p *closeHTTPSpy) PutOwnCloseConnectionCurrent(_ context.Context, a ea.Access, peer string, in connection.ClosePut) (connection.CloseResult, error) {
	p.called(a)
	p.peer = peer
	p.put = in
	e := p.view.Connections[0]
	e.IsClose = true
	e.NativeRevision = 1
	return connection.CloseResult{SchemaVersion: p.view.SchemaVersion, OwnerID: p.view.OwnerID, ObservedAt: p.view.ObservedAt, ValidUntil: in.ValidUntil, Connection: e}, p.err
}
func (p *closeHTTPSpy) DeleteOwnCloseConnectionCurrent(_ context.Context, a ea.Access, peer string, in connection.CloseDelete) (connection.CloseDeleteReceipt, error) {
	p.called(a)
	p.peer = peer
	p.del = in
	return connection.CloseDeleteReceipt{PersonID: peer}, p.err
}
func (p *closeHTTPSpy) ValidateCloseConnectionsResponse(_ context.Context, a ea.Access, v connection.CloseView) error {
	p.checks++
	return p.finalErr
}
func (p *closeHTTPSpy) ValidateCloseConnectionResponse(_ context.Context, a ea.Access, peer string, v connection.CloseResult) error {
	p.checks++
	return p.finalErr
}
func (p *closeHTTPSpy) ValidateCloseDeleteResponse(_ context.Context, a ea.Access, v connection.CloseDeleteReceipt) error {
	p.checks++
	return p.finalErr
}
func closeHTTPFixture(t *testing.T) (http.Handler, *closeHTTPSpy, *privateProfileHTTPAccess, string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	p := &closeHTTPSpy{view: connection.CloseView{SchemaVersion: connection.CloseSchema, OwnerID: a.actor.ID, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), Limit: 100, Connections: []connection.CloseEntry{{PersonID: privateProfileHTTPForeign, TieID: privateProfileHTTPAgent, RequestID: "b1000000-0000-0000-0000-000000000004", DisplayName: "好友资料", SourceVersion: strings.Repeat("a", 64)}}}}
	h := New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil)
	return h, p, a, token
}
func closeHTTPPut(p *closeHTTPSpy) string {
	v := p.view.Connections[0]
	b, _ := json.Marshal(connection.ClosePut{TieID: v.TieID, RequestID: v.RequestID, SourceVersion: v.SourceVersion, ValidUntil: p.view.ValidUntil, Confirmed: true})
	return string(b)
}
func closeHTTPDelete(p *closeHTTPSpy) string {
	v := p.view.Connections[0]
	b, _ := json.Marshal(connection.CloseDelete{TieID: v.TieID, RequestID: v.RequestID, ExpectedRevision: 1, Confirmed: true})
	return string(b)
}
func TestCloseConnectionsRegisteredCurrentSpecificActions(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			h, p, a, token := closeHTTPFixture(t)
			path := "/v1/me/close-connections"
			body := ""
			want := 200
			if method == "PUT" {
				path += "/" + privateProfileHTTPForeign
				body = closeHTTPPut(p)
			}
			if method == "DELETE" {
				path += "/" + privateProfileHTTPForeign
				body = closeHTTPDelete(p)
				want = 204
			}
			w := messagePolicyUnitRequest(h, method, path, body, token, nil)
			if w.Code != want || p.calls != 1 || p.checks != 1 || p.access.Actor != a.actor || p.access.SessionDigest != a.digest || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("current original action status=%d calls=%d checks=%d body=%s", w.Code, p.calls, p.checks, w.Body.String())
			}
			if method == "GET" && !strings.Contains(w.Body.String(), `"modelAccess":false`) {
				t.Fatal("human resource selection is not model access")
			}
			if method == "DELETE" && w.Body.Len() != 0 {
				t.Fatal("delete cannot expose private proof or old source")
			}
		})
	}
}
func TestCloseConnectionsRegisteredInvalidAndLate(t *testing.T) {
	for _, tc := range []string{"query", "workspace", "anonymous", "not-person", "not-confirmed", "unknown-owner", "duplicate", "bad-calendar", "missing-delete-confirm", "late-session", "late-source", "bad-dto", "internal-secret", "encoding-then-source"} {
		t.Run(tc, func(t *testing.T) {
			h, p, a, token := closeHTTPFixture(t)
			method := "PUT"
			path := "/v1/me/close-connections/" + privateProfileHTTPForeign
			body := closeHTTPPut(p)
			want, calls := 400, 0
			var change func(*http.Request)
			switch tc {
			case "query":
				path += "?ownerId=foreign"
			case "workspace":
				want = 403
				change = func(r *http.Request) { r.Header["X-Birdtie-Organization-Workspace"] = []string{""} }
			case "anonymous":
				token = ""
				want = 401
			case "not-person":
				a.actor.AccountType = "organization"
				want = 403
			case "not-confirmed":
				body = strings.Replace(body, "true", "false", 1)
			case "unknown-owner":
				body = strings.Replace(body, `"confirmed":true`, `"confirmed":true,"ownerId":"foreign"`, 1)
			case "duplicate":
				body = strings.Replace(body, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1)
			case "bad-calendar":
				body = strings.Replace(body, "2026-10-10", "2026-02-30", 1)
			case "missing-delete-confirm":
				method = "DELETE"
				body = strings.Replace(closeHTTPDelete(p), `,"confirmed":true`, "", 1)
			case "late-session":
				p.after = func() { a.err = identity.ErrUnauthorized }
				want, calls = 401, 1
			case "late-source", "encoding-then-source":
				p.finalErr = connection.ErrCloseChanged
				want, calls = 409, 1
			case "bad-dto":
				p.view.OwnerID = privateProfileHTTPForeign
				want, calls = 503, 1
			case "internal-secret":
				p.err = errors.New("CLOSE_INTERNAL_SECRET_CANARY")
				want, calls = 503, 1
			}
			w := messagePolicyUnitRequest(h, method, path, body, token, change)
			if w.Code != want || p.calls != calls || strings.Contains(w.Body.String(), "CLOSE_INTERNAL_SECRET_CANARY") {
				t.Fatalf("guard status=%d calls=%d body=%s", w.Code, p.calls, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("privacy responses must not cache")
			}
		})
	}
}
