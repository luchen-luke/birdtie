package httpapi

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
)

const pushHTTPID = "33333333-3333-4333-8333-333333333333"
const pushHTTPOp = "44444444-4444-4444-8444-444444444444"
const pushHTTPBinding = "55555555-5555-4555-8555-555555555555"
const pushHTTPAddress = "synthetic-http-fcm-token-private-123456789"

type systemPushHTTPSpy struct {
	*peopleSearchSpy
	calls, final int
	fail         error
	lastAccess   sp.Access
	lastAddress  sp.SecretAddress
}

func (p *systemPushHTTPSpy) ReadOptions(context.Context, identity.ProfileAccess) (sp.Options, error) {
	p.calls++
	return sp.Options{Schema: "system-push-options-v1", Available: true, RegistrationKinds: []sp.Kind{sp.FCMToken}, Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) ValidateOptions(context.Context, identity.ProfileAccess, sp.Options) error {
	p.final++
	return p.fail
}
func (p *systemPushHTTPSpy) ReadInstallation(_ context.Context, a sp.Access) (sp.Installation, error) {
	p.calls++
	p.lastAccess = a
	at := time.Now().UTC()
	binding := pushHTTPBinding
	k := sp.FCMToken
	until := at.Add(time.Hour)
	return sp.Installation{Schema: "system-push-installation-v1", InstallationID: a.InstallationID, Version: 1, BindingID: &binding, Enabled: true, RegistrationKind: &k, RegisteredAt: &at, ValidUntil: &until, Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) ValidateInstallation(context.Context, sp.Access, sp.Installation) error {
	p.final++
	return p.fail
}
func (p *systemPushHTTPSpy) CheckAddress(_ context.Context, a sp.Access, in sp.AddressCheckInput) (sp.AddressMatch, error) {
	p.calls++
	p.lastAccess = a
	p.lastAddress = in.Address
	return sp.AddressMatch{Schema: "system-push-address-match-v1", InstallationID: a.InstallationID, BindingID: pushHTTPBinding, Version: in.ExpectedVersion, Matches: true, Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) ValidateAddressMatch(context.Context, sp.Access, sp.AddressCheckInput, sp.AddressMatch) error {
	p.final++
	return p.fail
}
func (p *systemPushHTTPSpy) RegisterInstallation(_ context.Context, a sp.Access, in sp.RegistrationInput) (sp.Operation, error) {
	p.calls++
	p.lastAccess = a
	p.lastAddress = in.Address
	binding := pushHTTPBinding
	return sp.Operation{Schema: "system-push-operation-v1", OperationID: in.OperationID, Operation: "REGISTER", ExpectedVersion: in.ExpectedVersion, ResultVersion: in.ExpectedVersion + 1, BindingID: &binding, State: "COMMITTED", CommittedAt: time.Now().UTC(), Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) RevokeInstallation(_ context.Context, a sp.Access, in sp.RevokeInput) (sp.Operation, error) {
	p.calls++
	p.lastAccess = a
	return sp.Operation{Schema: "system-push-operation-v1", OperationID: in.OperationID, Operation: "REVOKE", ExpectedVersion: in.ExpectedVersion, ResultVersion: in.ExpectedVersion + 1, State: "COMMITTED", CommittedAt: time.Now().UTC(), Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) ReadOperation(_ context.Context, a sp.Access, op string) (sp.Operation, error) {
	p.calls++
	p.lastAccess = a
	binding := pushHTTPBinding
	return sp.Operation{Schema: "system-push-operation-v1", OperationID: op, Operation: "REGISTER", ResultVersion: 1, BindingID: &binding, State: "COMMITTED", CommittedAt: time.Now().UTC(), Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) ValidateOperation(context.Context, sp.Access, sp.Operation) error {
	p.final++
	return p.fail
}
func (p *systemPushHTTPSpy) ResolveEvent(_ context.Context, a sp.Access, in sp.OpenInput) (sp.Open, error) {
	p.calls++
	p.lastAccess = a
	return sp.Open{Schema: "system-push-open-v1", EventID: in.EventID, BindingID: in.BindingID, BindingVersion: in.BindingVersion, Item: inbox.Item{ID: pushHTTPOp, ResourceType: "conversation_message", ResourceID: pushHTTPOp, Detail: "ORIGINAL_PRIVATE_OBJECT"}, Proof: "SERVER_PRIVATE_PROOF"}, nil
}
func (p *systemPushHTTPSpy) ValidateEvent(context.Context, sp.Access, sp.OpenInput, sp.Open) error {
	p.final++
	return p.fail
}
func TestSystemPushHTTPRegisteredClosedPersonalWireAndFinalGates(t *testing.T) {
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	proof := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("p", 32)))
	install := "/v1/me/system-push/installations/" + pushHTTPID
	registration := `{"schema":"system-push-registration-v1","operationId":"` + pushHTTPOp + `","expectedVersion":0,"registrationKind":"FCM_TOKEN","platform":"ANDROID","address":"` + pushHTTPAddress + `","permission":"AUTHORIZED"}`
	check := `{"schema":"system-push-address-check-v1","expectedVersion":1,"registrationKind":"FCM_TOKEN","address":"` + pushHTTPAddress + `"}`
	event := "/v1/me/system-push/events/" + pushHTTPOp + "?bindingId=" + pushHTTPBinding + "&bindingVersion=1"
	for _, tc := range []struct {
		name, method, path, body                     string
		want, calls                                  int
		fail                                         error
		anonymous, org, missingProof, duplicateProof bool
	}{
		{name: "options", method: "GET", path: "/v1/me/system-push/options", want: 200, calls: 1},
		{name: "installation", method: "GET", path: install, want: 200, calls: 1},
		{name: "register", method: "PUT", path: install, body: registration, want: 200, calls: 1},
		{name: "revoke", method: "DELETE", path: install, body: `{"schema":"system-push-revoke-v1","operationId":"` + pushHTTPOp + `","expectedVersion":1}`, want: 200, calls: 1},
		{name: "causal-receipt", method: "GET", path: install + "/operations/" + pushHTTPOp, want: 200, calls: 1},
		{name: "address-read-check", method: "POST", path: install + "/address-check", body: check, want: 200, calls: 1},
		{name: "original-open", method: "GET", path: event, want: 200, calls: 1},
		{name: "late-session-denied", method: "PUT", path: install, body: registration, fail: identity.ErrUnauthorized, want: 401, calls: 1},
		{name: "late-original-ACL-denied", method: "GET", path: event, fail: sp.ErrNotFound, want: 404, calls: 1},
		{name: "late-address-changed", method: "POST", path: install + "/address-check", body: check, fail: sp.ErrConflict, want: 409, calls: 1},
		{name: "anonymous", method: "GET", path: install, anonymous: true, want: 401},
		{name: "organization-even-empty-header", method: "GET", path: install, org: true, want: 403},
		{name: "missing-proof", method: "GET", path: install, missingProof: true, want: 400},
		{name: "duplicate-proof", method: "GET", path: install, duplicateProof: true, want: 400},
		{name: "owner-selector", method: "GET", path: install + "?owner=other", want: 400},
		{name: "null-version", method: "PUT", path: install, body: strings.Replace(registration, `"expectedVersion":0`, `"expectedVersion":null`, 1), want: 400},
		{name: "fraction-version", method: "PUT", path: install, body: strings.Replace(registration, `"expectedVersion":0`, `"expectedVersion":0.1`, 1), want: 400},
		{name: "FID-rejected", method: "PUT", path: install, body: strings.Replace(registration, "FCM_TOKEN", "FID", 1), want: 400},
		{name: "duplicate-key", method: "PUT", path: install, body: strings.Replace(registration, `"schema":`, `"schema":"system-push-registration-v1","schema":`, 1), want: 400},
		{name: "provider-selector", method: "PUT", path: install, body: strings.TrimSuffix(registration, "}") + `,"providerUrl":"https://other.invalid"}`, want: 400},
		{name: "trailing-object", method: "PUT", path: install, body: registration + `{}`, want: 400},
		{name: "closed-address-check", method: "POST", path: install + "/address-check", body: strings.TrimSuffix(check, "}") + `,"hash":"caller-hash"}`, want: 400},
		{name: "duplicate-binding", method: "GET", path: event + "&bindingId=" + pushHTTPBinding, want: 400},
		{name: "noncanonical-version", method: "GET", path: strings.Replace(event, "bindingVersion=1", "bindingVersion=01", 1), want: 400},
		{name: "open-owner-selector", method: "GET", path: event + "&owner=other", want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &systemPushHTTPSpy{peopleSearchSpy: &peopleSearchSpy{actor: identity.Actor{ID: "11111111-1111-4111-8111-111111111111", AccountType: "person"}, digest: digest}, fail: tc.fail}
			h := New(nil, p, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, func(s *server) { s.systemPush = p })
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if !tc.anonymous {
				r.Header.Set("Authorization", "Bearer "+token)
			}
			if tc.body != "" {
				r.Header.Set("Content-Type", "application/json")
			}
			if tc.org {
				r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
			}
			if !tc.missingProof {
				r.Header.Set("X-Birdtie-Push-Installation-Proof", proof)
			}
			if tc.duplicateProof {
				r.Header.Add("X-Birdtie-Push-Installation-Proof", proof)
			}
			if strings.Contains(tc.path, "/events/") {
				r.Header.Set("X-Birdtie-Push-Installation", pushHTTPID)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want || p.calls != tc.calls || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d calls=%d want status=%d calls=%d", w.Code, p.calls, tc.want, tc.calls)
			}
			if tc.want == 200 && p.final != 1 {
				t.Fatal("response omitted final gate")
			}
			body := w.Body.String()
			for _, secret := range []string{pushHTTPAddress, proof, "SERVER_PRIVATE_PROOF", "address_fingerprint", "proof_sha256"} {
				if strings.Contains(body, secret) {
					t.Fatal("transport secret/proof leaked")
				}
			}
			if tc.want != 200 && (strings.Contains(body, "ORIGINAL_PRIVATE_OBJECT") || strings.Contains(body, pushHTTPBinding)) {
				t.Fatal("denied payload escaped")
			}
			if tc.calls > 0 && tc.path != "/v1/me/system-push/options" && (p.lastAccess.Actor.ID != p.actor.ID || p.lastAccess.SessionDigest != digest || p.lastAccess.InstallationID != pushHTTPID) {
				t.Fatal("caller ownership selectors crossed wire")
			}
		})
	}
}

var _ http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
