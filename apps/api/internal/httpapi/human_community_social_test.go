package httpapi

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type commUXTransport struct {
	community.SocialStore
	read    community.HumanRead
	command community.HumanCommand
	digest  [32]byte
	actor   identity.Actor
	calls   int
	out     community.HumanResult
	err     error
}

func (s *commUXTransport) ReadHumanCommunity(_ context.Context, d [32]byte, a identity.Actor, in community.HumanRead) (community.HumanResult, error) {
	s.calls++
	s.digest = d
	s.actor = a
	s.read = in
	return s.out, s.err
}
func (s *commUXTransport) MutateHumanCommunity(_ context.Context, d [32]byte, a identity.Actor, in community.HumanCommand) (community.HumanResult, error) {
	s.calls++
	s.digest = d
	s.actor = a
	s.command = in
	return s.out, s.err
}
func commUXSpy(t *testing.T) (http.Handler, *commUXTransport, string, *privateProfileHTTPAccess) {
	t.Helper()
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	a := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: d}
	port := &commUXTransport{}
	s := &server{access: a, socialCommunities: port}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/communities/{communityID}", s.getSocialCommunity)
	mux.HandleFunc("POST /v1/communities/{communityID}/invitations", s.inviteSocialMember)
	mux.HandleFunc("PUT /v1/communities/{communityID}/members/{memberID}/role", s.changeSocialMemberRole)
	mux.HandleFunc("POST /v1/communities/{communityID}/archive", s.archiveSocialCommunity)
	return mux, port, token, a
}
func TestCommunityUXTransportStrictManagementBody(t *testing.T) {
	for _, raw := range []string{`{"userAccountId":null}`, `{"userAccountId":[]}`, `{"USERACCOUNTID":"` + privateProfileHTTPForeign + `"}`, `{"userAccountId":"` + privateProfileHTTPForeign + `","actorId":"` + privateProfileHTTPOwner + `"}`, `{"userAccountId":"` + privateProfileHTTPForeign + `","userAccountId":"` + privateProfileHTTPAgent + `"}`, `{"userAccountId":"\ud800"}`, `{"userAccountId":"\udc00"}`, `null`, `[]`, `{} {}`, "{\"userAccountId\":\"\xff\"}", strings.Repeat(" ", 21*1024)} {
		t.Run(raw[:min(len(raw), 35)], func(t *testing.T) {
			h, p, token, _ := commUXSpy(t)
			r := privateProfileHTTPRequest("POST", "/v1/communities/"+privateProfileHTTPAgent+"/invitations", raw, token, "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 400 || p.calls != 0 {
				t.Fatal("strict exact flat strings before native call", w.Code, p.calls)
			}
		})
	}
}
func TestCommunityUXTransportCurrentDigestAndPreview(t *testing.T) {
	h, p, token, a := commUXSpy(t)
	p.out.Approval = &community.HumanApproval{Snapshot: "SYNTHETIC_TRANSPORT_ONLY", ActorID: a.actor.ID, CommunityID: privateProfileHTTPAgent, TargetID: privateProfileHTTPForeign, Operation: "invite", ExpiresAt: time.Now().Add(time.Second)}
	r := privateProfileHTTPRequest("POST", "/v1/communities/"+privateProfileHTTPAgent+"/invitations", `{"userAccountId":"`+privateProfileHTTPForeign+`"}`, token, "application/json")
	r.Header.Set("X-Birdtie-Community-Preview", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || p.calls != 1 || p.digest != a.digest || p.actor.ID != a.actor.ID || !p.command.Preview || p.command.TargetID != privateProfileHTTPForeign || p.command.Snapshot != "" {
		t.Fatal("server-derived ordinary current person transport", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private confirmation cache")
	}
}
func TestCommunityUXTransportWrongResultAndSafeError(t *testing.T) {
	for _, mode := range []string{"wrong_resource", "wrong_actor", "nil_result", "unsafe_error"} {
		t.Run(mode, func(t *testing.T) {
			h, p, token, a := commUXSpy(t)
			method, path, body := "GET", "/v1/communities/"+privateProfileHTTPAgent, ""
			switch mode {
			case "wrong_resource":
				p.out.Community = &community.SocialRecord{ID: privateProfileHTTPForeign, Description: privateProfileHTTPMarker}
			case "wrong_actor":
				method = "POST"
				path += "/invitations"
				body = `{"userAccountId":"` + privateProfileHTTPForeign + `"}`
				p.out.Approval = &community.HumanApproval{ActorID: privateProfileHTTPForeign, CommunityID: privateProfileHTTPAgent, TargetID: privateProfileHTTPForeign, Operation: "invite", Snapshot: "fixture"}
			case "unsafe_error":
				p.err = errors.New(privateProfileHTTPMarker + " SQL secret digest")
			case "nil_result":
			}
			r := privateProfileHTTPRequest(method, path, body, token, "application/json")
			if mode == "wrong_actor" {
				r.Header.Set("X-Birdtie-Community-Preview", "1")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 503 || strings.Contains(w.Body.String(), privateProfileHTTPMarker) || strings.Contains(w.Body.String(), a.actor.ID) {
				t.Fatal("unsafe or mismatched result must return empty safe error", mode, w.Code, w.Body.String())
			}
		})
	}
}
func TestCommunityUXTransportHeaderAndPersonBoundary(t *testing.T) {
	for _, mode := range []string{"wrong_preview", "duplicate_preview", "both", "duplicate_snapshot", "organization", "anonymous"} {
		t.Run(mode, func(t *testing.T) {
			h, p, token, a := commUXSpy(t)
			r := privateProfileHTTPRequest("POST", "/v1/communities/"+privateProfileHTTPAgent+"/archive", "", token, "application/json")
			want := 400
			switch mode {
			case "wrong_preview":
				r.Header.Set("X-Birdtie-Community-Preview", "true")
			case "duplicate_preview":
				r.Header.Add("X-Birdtie-Community-Preview", "1")
				r.Header.Add("X-Birdtie-Community-Preview", "1")
			case "both":
				r.Header.Set("X-Birdtie-Community-Preview", "1")
				r.Header.Set("X-Birdtie-Community-Snapshot", "fixture")
			case "duplicate_snapshot":
				r.Header.Add("X-Birdtie-Community-Snapshot", "a")
				r.Header.Add("X-Birdtie-Community-Snapshot", "b")
			case "organization":
				a.actor.AccountType = "organization"
				want = 403
			case "anonymous":
				r.Header.Del("Authorization")
				want = 401
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != want || p.calls != 0 {
				t.Fatal("transport boundary before domain", mode, w.Code, p.calls)
			}
		})
	}
}
func TestCommunityUXTransportNoActorIDFallback(t *testing.T) {
	token, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	access := &privateProfileHTTPAccess{actor: identity.Actor{ID: privateProfileHTTPOwner, AccountType: "person"}, digest: d}
	s := &server{access: access, socialCommunities: &struct{ community.SocialStore }{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/communities/{communityID}", s.getSocialCommunity)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, privateProfileHTTPRequest("GET", "/v1/communities/"+privateProfileHTTPAgent, "", token, "application/json"))
	if w.Code != 503 {
		t.Fatal("current capability missing must fail closed", w.Code)
	}
}
