package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Transport unit spies only. They do not prove SQL, session persistence or delivery.
type messagePolicyHTTPSpy struct {
	connection.Store
	record                              mp.Record
	decision                            mp.Decision
	err                                 error
	calls                               int
	own                                 agentprofile.PrivateAccess
	write                               ea.Access
	peer, version, kind, entity, action string
	after                               func()
}
type messagePolicyInviteUnitSpy struct {
	newpeople.Store
	access                           ea.Access
	source, candidate, note, version string
	calls                            int
	after                            func()
}

func (s *messagePolicyInviteUnitSpy) InviteNewPeopleCurrent(_ context.Context, a ea.Access, source, candidate, note, version string) (connection.Request, error) {
	s.access = a
	s.source = source
	s.candidate = candidate
	s.note = note
	s.version = version
	s.calls++
	if s.after != nil {
		s.after()
	}
	return connection.Request{ID: privateProfileHTTPAgent, State: "pending", PolicyDisposition: "SCREEN", ScreeningStatus: "PENDING_REVIEW"}, nil
}

type messagePolicyInviteUnitCatalog struct {
	foundation.PublicCatalog
	*messagePolicyInviteUnitSpy
}

func TestMessagePolicyUnitNewPeopleKeepsOriginalDomainAndCurrentCaller(t *testing.T) {
	for _, tc := range []string{"current", "not-confirmed", "bad-source", "owner-override", "workspace", "late-revoked", "late-owner"} {
		t.Run(tc, func(t *testing.T) {
			_, _, a, token := messagePolicyUnitHTTP(t)
			sp := &messagePolicyInviteUnitSpy{}
			h := New(messagePolicyInviteUnitCatalog{messagePolicyInviteUnitSpy: sp}, a, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
			body := `{"sourceIntentId":"` + privateProfileHTTPAgent + `","candidateIntentId":"` + privateProfileHTTPForeign + `","note":"  明确申请  ","confirmed":true}`
			status, calls := 201, 1
			var change func(*http.Request)
			switch tc {
			case "not-confirmed":
				body = strings.Replace(body, "true", "false", 1)
				status, calls = 400, 0
			case "bad-source":
				body = strings.Replace(body, privateProfileHTTPAgent, "not-id", 1)
				status, calls = 400, 0
			case "owner-override":
				body = strings.Replace(body, `"confirmed":true`, `"confirmed":true,"ownerId":"foreign"`, 1)
				status, calls = 400, 0
			case "workspace":
				change = func(r *http.Request) { r.Header["X-Birdtie-Organization-Workspace"] = []string{""} }
				status, calls = 403, 0
			case "late-revoked":
				sp.after = func() { a.err = identity.ErrUnauthorized }
				status = 401
			case "late-owner":
				sp.after = func() { a.actor.ID = privateProfileHTTPForeign }
				status = 401
			}
			w := messagePolicyUnitRequest(h, "POST", "/v1/me/new-people/invitations", body, token, change)
			if w.Code != status || sp.calls != calls {
				t.Fatalf("original current invitation status=%d calls=%d", w.Code, sp.calls)
			}
			if calls == 1 && (sp.access.Actor.ID != privateProfileHTTPOwner || sp.access.SessionDigest != a.digest || sp.source != privateProfileHTTPAgent || sp.candidate != privateProfileHTTPForeign || sp.note != "明确申请") {
				t.Fatal("current caller/source/candidate must stay captured in original domain")
			}
			if tc == "current" && !strings.Contains(w.Body.String(), "PENDING_REVIEW") {
				t.Fatal("pending screen is not accepted")
			}
		})
	}
}

func (s *messagePolicyHTTPSpy) called() {
	s.calls++
	if s.after != nil {
		s.after()
	}
}
func (s *messagePolicyHTTPSpy) GetOwnMessagePolicy(_ context.Context, a agentprofile.PrivateAccess) (mp.Record, error) {
	s.own = a
	s.called()
	return s.record, s.err
}
func (s *messagePolicyHTTPSpy) PutOwnMessagePolicy(_ context.Context, a agentprofile.PrivateAccess, in mp.PutInput) (mp.Record, error) {
	s.own = a
	s.called()
	r := s.record
	r.Configured = true
	r.NativeRevision = in.ExpectedVersion + 1
	r.Status = "ACTIVE"
	r.IncomingRequests = in.IncomingRequests
	r.ValidFrom = &r.ObservedAt
	r.ExpiresAt = &in.ExpiresAt
	return r, s.err
}
func (s *messagePolicyHTTPSpy) ReadMessagePolicyDecision(_ context.Context, a agentprofile.PrivateAccess, p string) (mp.Decision, error) {
	s.own = a
	s.peer = p
	s.called()
	return s.decision, s.err
}
func (s *messagePolicyHTTPSpy) CreateRequestCurrent(_ context.Context, a ea.Access, p, city, note, v string, b *ea.BoundCondition) (connection.Request, error) {
	s.write = a
	s.peer = p
	s.version = v
	s.called()
	return connection.Request{ID: privateProfileHTTPAgent, State: "pending", Scope: "conversation"}, s.err
}
func (s *messagePolicyHTTPSpy) CreateFriendRequestCurrent(_ context.Context, a ea.Access, p, note, v string, b *ea.BoundCondition) (connection.Request, error) {
	s.write = a
	s.peer = p
	s.version = v
	s.called()
	return connection.Request{ID: privateProfileHTTPAgent, State: "pending", Scope: "friend"}, s.err
}
func (s *messagePolicyHTTPSpy) DecideRequestCurrent(_ context.Context, a ea.Access, id, action string) (connection.Request, error) {
	s.write = a
	s.action = action
	s.called()
	return connection.Request{ID: id, State: "accepted"}, s.err
}
func (s *messagePolicyHTTPSpy) StartFriendConversationCurrent(_ context.Context, a ea.Access, id string, b *ea.BoundCondition) (connection.Conversation, error) {
	s.write = a
	s.called()
	return connection.Conversation{ID: id}, s.err
}
func (s *messagePolicyHTTPSpy) SendMessageCurrent(_ context.Context, a ea.Access, id, body, kind, entity string) (connection.Message, error) {
	s.write = a
	s.kind = kind
	s.entity = entity
	s.called()
	return connection.Message{ID: privateProfileHTTPAgent, ConversationID: id}, s.err
}

type messagePolicyUnitCatalog struct {
	foundation.PublicCatalog
	*messagePolicyHTTPSpy
}

func messagePolicyUnitHTTP(t *testing.T) (http.Handler, *messagePolicyHTTPSpy, *privateProfileHTTPAccess, string) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	p := &messagePolicyHTTPSpy{record: mp.Record{Schema: mp.Schema, OwnerID: privateProfileHTTPOwner, AgentID: privateProfileHTTPAgent, Status: "UNCONFIGURED", IncomingRequests: mp.Request, ObservedAt: now}, decision: mp.Decision{Schema: mp.Schema, Disposition: mp.Screen, SourceVersion: strings.Repeat("a", 64), ObservedAt: now, ValidUntil: now.Add(30 * time.Second), Authority: mp.Authority}}
	a := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: d}
	h := New(messagePolicyUnitCatalog{messagePolicyHTTPSpy: p}, a, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil)
	return h, p, a, token
}
func messagePolicyUnitRequest(h http.Handler, method, path, body, token string, change func(*http.Request)) *httptest.ResponseRecorder {
	r := privateProfileHTTPRequest(method, path, body, token, "application/json")
	if change != nil {
		change(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestMessagePolicyUnitRegisteredReadPutFourStateAndChineseErrors(t *testing.T) {
	h, p, a, token := messagePolicyUnitHTTP(t)
	w := messagePolicyUnitRequest(h, "GET", messagePolicyPath, "", token, nil)
	if w.Code != 200 || p.calls != 1 || p.own.SessionDigest != a.digest || p.own.WorkspacePrincipal.ID != a.actor.ID {
		t.Fatal("registered private read must capture current owner/digest")
	}
	for _, mode := range []string{"REQUEST", "SCREEN", "BLOCK"} {
		w = messagePolicyUnitRequest(h, "PUT", messagePolicyPath, `{"expectedVersion":0,"incomingRequests":"`+mode+`","expiresAt":"2026-10-07T12:00:00Z"}`, token, nil)
		if w.Code != 200 {
			t.Fatalf("put %s: %d %s", mode, w.Code, w.Body.String())
		}
	}
	for _, mode := range []mp.Disposition{mp.Allow, mp.Request, mp.Screen, mp.Block} {
		p.decision.Disposition = mode
		p.decision.SourceVersion = strings.Repeat("a", 64)
		if mode == mp.Block {
			p.decision.SourceVersion = ""
		}
		w = messagePolicyUnitRequest(h, "GET", messagePolicyPath+"/decisions/"+privateProfileHTTPForeign, "", token, nil)
		if w.Code != 200 || p.peer != privateProfileHTTPForeign || strings.Contains(w.Body.String(), "ownerId") {
			t.Fatal("decision wire must omit recipient preferences and private identities")
		}
	}
	for _, tc := range []struct {
		e      error
		status int
	}{{mp.ErrInvalid, 400}, {mp.ErrDenied, 403}, {mp.ErrChanged, 409}, {mp.ErrUnavailable, 503}, {identity.ErrUnauthorized, 401}, {errors.New("MESSAGE_SECRET_CANARY"), 503}} {
		p.err = tc.e
		w = messagePolicyUnitRequest(h, "GET", messagePolicyPath, "", token, nil)
		var data struct{ Error struct{ Code, Detail string } }
		json.Unmarshal(w.Body.Bytes(), &data)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" || data.Error.Detail == "" || strings.Contains(w.Body.String(), "MESSAGE_SECRET_CANARY") {
			t.Fatal("bounded Chinese error mapping")
		}
	}
}
func TestMessagePolicyUnitAuthenticationAndOverrideRejection(t *testing.T) {
	for _, endpoint := range []struct{ method, path, body string }{{"GET", messagePolicyPath, ""}, {"PUT", messagePolicyPath, `{"expectedVersion":0,"incomingRequests":"SCREEN","expiresAt":"2026-10-07T12:00:00Z"}`}, {"GET", messagePolicyPath + "/decisions/" + privateProfileHTTPForeign, ""}, {"POST", "/v1/me/connection-requests", `{"recipientAccountId":"` + privateProfileHTTPForeign + `","scope":"friend","note":"人工申请"}`}} {
		for _, tc := range []struct {
			name   string
			status int
			change func(*http.Request, *privateProfileHTTPAccess)
		}{
			{"anonymous", 401, func(r *http.Request, _ *privateProfileHTTPAccess) { r.Header.Del("Authorization") }},
			{"duplicate", 401, func(r *http.Request, _ *privateProfileHTTPAccess) {
				r.Header.Add("Authorization", r.Header.Get("Authorization"))
			}},
			{"expired", 401, func(_ *http.Request, a *privateProfileHTTPAccess) { a.err = identity.ErrUnauthorized }},
			{"organization", 403, func(_ *http.Request, a *privateProfileHTTPAccess) { a.actor.AccountType = "organization" }},
			{"business", 403, func(_ *http.Request, a *privateProfileHTTPAccess) { a.actor.AccountType = "business" }},
			{"workspace", 403, func(r *http.Request, _ *privateProfileHTTPAccess) {
				r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
			}},
			{"query", 400, func(r *http.Request, _ *privateProfileHTTPAccess) {
				r.URL.RawQuery = "ownerId=" + privateProfileHTTPForeign
			}},
			{"emptyquery", 400, func(r *http.Request, _ *privateProfileHTTPAccess) { r.URL.ForceQuery = true }},
		} {
			t.Run(endpoint.method+endpoint.path+"/"+tc.name, func(t *testing.T) {
				h, p, a, token := messagePolicyUnitHTTP(t)
				w := messagePolicyUnitRequest(h, endpoint.method, endpoint.path, endpoint.body, token, func(r *http.Request) { tc.change(r, a) })
				if w.Code != tc.status || p.calls != 0 {
					t.Fatalf("denied path reached port: %d calls=%d", w.Code, p.calls)
				}
			})
		}
	}
}
func TestMessagePolicyUnitOriginalWritersUseCapturedCurrentPort(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		status     int
	}{{"/v1/me/connection-requests", `{"recipientAccountId":"` + privateProfileHTTPForeign + `","scope":"friend","note":"人工申请"}`, 201}, {"/v1/me/connection-requests", `{"recipientAccountId":"` + privateProfileHTTPForeign + `","cityId":"aberdeen","note":"人工申请"}`, 201}, {"/v1/me/connection-requests/" + privateProfileHTTPAgent + "/decision", `{"action":"accept"}`, 200}, {"/v1/me/ties/" + privateProfileHTTPAgent + "/conversation", "", 200}, {"/v1/me/conversations/" + privateProfileHTTPAgent + "/messages", `{"body":"人类明确发送"}`, 201}, {"/v1/me/conversations/" + privateProfileHTTPAgent + "/messages", `{"entity":{"type":"place","id":"` + privateProfileHTTPForeign + `"}}`, 201}} {
		t.Run(tc.path+tc.body, func(t *testing.T) {
			h, p, a, token := messagePolicyUnitHTTP(t)
			original := a.actor
			p.after = func() { a.actor.ID = privateProfileHTTPForeign } // The invocation must never reread this as its writer.
			w := messagePolicyUnitRequest(h, "POST", tc.path, tc.body, token, nil)
			if w.Code != 401 || p.calls != 1 || p.write.Actor != original || p.write.SessionDigest != a.digest || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatalf("late response must be denied; captured original writer: %d calls=%d", w.Code, p.calls)
			}
			h, p, a, token = messagePolicyUnitHTTP(t)
			w = messagePolicyUnitRequest(h, "POST", tc.path, tc.body, token, nil)
			if w.Code != tc.status || p.calls != 1 || p.write.Actor != a.actor {
				t.Fatal("valid current writer contract")
			}
		})
	}
	h, p, _, token := messagePolicyUnitHTTP(t)
	version := strings.Repeat("b", 64)
	w := messagePolicyUnitRequest(h, "POST", "/v1/me/connection-requests", `{"recipientAccountId":"`+privateProfileHTTPForeign+`","scope":"friend","note":"人工申请"}`, token, func(r *http.Request) { r.Header.Set("X-Birdtie-Message-Policy-Version", version) })
	if w.Code != 201 || p.version != version {
		t.Fatal("policy version must be only a captured condition")
	}
	for _, v := range []string{"", "confirmed", "owner", strings.Repeat("A", 64)} {
		before := p.calls
		w = messagePolicyUnitRequest(h, "POST", "/v1/me/connection-requests", `{"recipientAccountId":"`+privateProfileHTTPForeign+`","scope":"friend","note":"人工申请"}`, token, func(r *http.Request) { r.Header["X-Birdtie-Message-Policy-Version"] = []string{v} })
		if w.Code != 400 || p.calls != before {
			t.Fatal("invalid source condition reached writer")
		}
	}
}
func TestMessagePolicyUnitLateReadAndFalseScreeningDoNotReturnSuccess(t *testing.T) {
	for _, tc := range []string{"revoked", "ownerABA", "automatic", "screening", "wrongrecordowner", "badrecordid"} {
		t.Run(tc, func(t *testing.T) {
			h, p, a, token := messagePolicyUnitHTTP(t)
			path := messagePolicyPath + "/decisions/" + privateProfileHTTPForeign
			status := 503
			switch tc {
			case "revoked":
				p.after = func() { a.err = identity.ErrUnauthorized }
				status = 401
			case "ownerABA":
				p.after = func() { a.actor.ID = privateProfileHTTPForeign }
				status = 401
			case "automatic":
				p.decision.AutomaticAcceptance = true
			case "screening":
				p.decision.ScreeningAvailable = true
			case "wrongrecordowner":
				p.record.OwnerID = privateProfileHTTPForeign
				path = messagePolicyPath
			case "badrecordid":
				p.record.AgentID = "not-an-id"
				path = messagePolicyPath
			}
			w := messagePolicyUnitRequest(h, "GET", path, "", token, nil)
			if w.Code != status || strings.Contains(w.Body.String(), `"data"`) {
				t.Fatal("invalid/late output was published")
			}
		})
	}
}
