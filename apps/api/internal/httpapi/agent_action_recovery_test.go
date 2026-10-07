package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestSandboxRecoveryHumanRegisteredRouteExists(t *testing.T) {
	h := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/v1/me/agent-sandbox-approvals/38000000-0000-4000-8000-000000000003/reconcile", nil))
	if w.Code != 503 {
		t.Fatalf("registered recovery must fail closed without its gateway: status=%d body=%s", w.Code, w.Body.String())
	}
}

const recoveryHTTPOwner = "38000000-0000-4000-8000-000000000001"
const recoveryHTTPApproval = "38000000-0000-4000-8000-000000000003"
const recoveryHTTPPath = "/v1/me/agent-sandbox-approvals/" + recoveryHTTPApproval + "/reconcile"

type sandboxRecoveryHTTPUnit struct {
	value  aa.HumanRecoveryReceipt
	err    error
	calls  int
	access agentprofile.PrivateAccess
	id     string
}

func (p *sandboxRecoveryHTTPUnit) RecoverOwn(_ context.Context, a agentprofile.PrivateAccess, id string) (aa.HumanRecoveryReceipt, error) {
	p.calls++
	p.access = a
	p.id = id
	return p.value, p.err
}

type sandboxRecoveryHTTPAccess struct {
	identity.AccessStore
	digest    [32]byte
	actor     identity.Actor
	err, late error
	cancel    context.CancelFunc
	final     int
}

func (a *sandboxRecoveryHTTPAccess) Authenticate(_ context.Context, d [32]byte) (identity.Actor, error) {
	if d != a.digest {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return a.actor, a.err
}
func (a *sandboxRecoveryHTTPAccess) AuthenticateHumanSocial(c context.Context, d [32]byte) (identity.Actor, error) {
	return a.Authenticate(c, d)
}
func (a *sandboxRecoveryHTTPAccess) ValidateHumanSocialResponse(_ context.Context, d [32]byte, p identity.Actor) error {
	a.final++
	if d != a.digest || p != a.actor {
		return identity.ErrUnauthorized
	}
	if a.cancel != nil {
		a.cancel()
	}
	return a.late
}
func recoveryHTTPFixture(t *testing.T) (*sandboxRecoveryHTTPUnit, *sandboxRecoveryHTTPAccess, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: recoveryHTTPOwner}
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	effect := "38000000-0000-4000-8000-000000000004"
	applied := at.Add(time.Second)
	p := &sandboxRecoveryHTTPUnit{value: aa.HumanRecoveryReceipt{SchemaVersion: aa.HumanRecoverySchema, Owner: owner, ApprovalID: recoveryHTTPApproval, DispatchID: "38000000-0000-4000-8000-000000000002", Status: aa.Succeeded, CommittedAt: at, EffectID: &effect, AppliedAt: &applied}}
	a := &sandboxRecoveryHTTPAccess{digest: d, actor: identity.Actor{ID: owner.ID, AccountType: "person"}}
	return p, a, token
}
func recoveryHTTPHandler(p aa.HumanRecoveryGateway, a identity.AccessStore) http.Handler {
	return New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, WithSandboxRecovery(p))
}
func recoveryHTTPCall(h http.Handler, token string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", recoveryHTTPPath, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestSandboxRecoveryHumanRegisteredReceiptOnlyAndStableAddress(t *testing.T) {
	p, a, token := recoveryHTTPFixture(t)
	h := recoveryHTTPHandler(p, a)
	var first string
	for i := 0; i < 2; i++ {
		w := recoveryHTTPCall(h, token, nil)
		if w.Code != 200 || a.final != i+1 || p.calls != i+1 || p.id != recoveryHTTPApproval || p.access.SessionDigest != a.digest || p.access.WorkspacePrincipal.ID != recoveryHTTPOwner || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		if i == 0 {
			first = w.Body.String()
			t.Logf("SYNTHETIC_REGISTERED_RECOVERY_WIRE %s", w.Body.Bytes())
		} else if w.Body.String() != first {
			t.Fatal("same original receipt changed")
		}
	}
	var body map[string]json.RawMessage
	if json.Unmarshal([]byte(first), &body) != nil || len(body) != 1 || body["data"] == nil {
		t.Fatal("not original data wrapper")
	}
	for _, forbidden := range []string{"effectKey", "effect_key", "binding", "payload", "query", "session", "authority", "value"} {
		if strings.Contains(first, forbidden) {
			t.Fatal("private state exported", forbidden)
		}
	}
	for _, path := range []string{"/v1/me/agent-sandbox-approvals/preview", "/v1/me/agent-sandbox-approvals/" + recoveryHTTPApproval + "/confirm", "/v1/me/agent-sandbox-approvals/" + recoveryHTTPApproval + "/commit", "/v1/me/agent-sandbox-approvals/" + recoveryHTTPApproval + "/execute"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 404 {
			t.Fatal("new write capability route registered", path, w.Code)
		}
	}
}
func TestSandboxRecoveryHumanRegisteredDenialUnknownAndLateResponse(t *testing.T) {
	for _, name := range []string{"anonymous", "expired", "Org", "body", "query", "unknown", "private error", "foreign owner", "wrong approval", "poisoned status", "late session", "cancel", "nil gateway", "typed nil"} {
		t.Run(name, func(t *testing.T) {
			p, a, token := recoveryHTTPFixture(t)
			var port aa.HumanRecoveryGateway = p
			want := 503
			var edit func(*http.Request)
			switch name {
			case "anonymous":
				token = ""
				want = 401
			case "expired":
				a.err = identity.ErrUnauthorized
				want = 401
			case "Org":
				want = 403
				edit = func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", "") }
			case "body":
				want = 400
				edit = func(r *http.Request) {
					r.Body = httptest.NewRequest("POST", "/", strings.NewReader(`{"confirmed":true}`)).Body
				}
			case "query":
				want = 400
				edit = func(r *http.Request) { r.URL.RawQuery = "ownerId=" + recoveryHTTPOwner }
			case "unknown":
				p.err = aa.ErrUnknown
			case "private error":
				p.err = errors.New("PRIVATE_RECOVERY_CANARY")
			case "foreign owner":
				p.value.Owner.ID = p.value.DispatchID
			case "wrong approval":
				p.value.ApprovalID = p.value.DispatchID
			case "poisoned status":
				p.value.Status = "NO_EFFECT_BY_404"
			case "late session":
				a.late = identity.ErrUnauthorized
				want = 401
			case "cancel":
				edit = func(r *http.Request) {
					c, cancel := context.WithCancel(r.Context())
					a.cancel = cancel
					*r = *r.WithContext(c)
				}
			case "nil gateway":
				port = nil
			case "typed nil":
				var n *sandboxRecoveryHTTPUnit
				port = n
			}
			w := recoveryHTTPCall(recoveryHTTPHandler(port, a), token, edit)
			if w.Code != want || p.calls > 1 || strings.Contains(w.Body.String(), "PRIVATE_RECOVERY_CANARY") || strings.Contains(w.Body.String(), "dispatchId") {
				t.Fatal(name, w.Code, w.Body.String(), p.calls)
			}
			if name == "unknown" && !strings.Contains(w.Body.String(), "sandbox_recovery_unknown") {
				t.Fatal("unknown became failure/no-effect", w.Body.String())
			}
		})
	}
}
