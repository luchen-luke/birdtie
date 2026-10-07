package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"io"
	"mime"
	"net/http"
	"strings"
)

func messagePolicyError(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Cache-Control", "no-store")
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "detail": detail}})
}
func messagePolicyFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		messagePolicyError(w, 401, "unauthorized", "登录已失效，请重新登录。")
	case errors.Is(e, mp.ErrInvalid):
		messagePolicyError(w, 400, "invalid_message_policy", "请检查消息请求策略的内容和有效期。")
	case errors.Is(e, mp.ErrDenied):
		messagePolicyError(w, 403, "message_policy_forbidden", "请在本人账号中管理消息请求。")
	case errors.Is(e, mp.ErrChanged):
		messagePolicyError(w, 409, "message_policy_changed", "消息请求条件已变化，请重新查看后确认。")
	default:
		messagePolicyError(w, 503, "message_policy_unavailable", "消息请求策略暂不可用，请稍后重试。")
	}
}
func (s *server) messageWriteAccess(w http.ResponseWriter, r *http.Request) (ea.Access, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		messagePolicyFailure(w, mp.ErrDenied)
		return ea.Access{}, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		messagePolicyFailure(w, mp.ErrInvalid)
		return ea.Access{}, false
	}
	if s.access == nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return ea.Access{}, false
	}
	a, d, e := s.humanSocialActor(r, true)
	if e != nil {
		messagePolicyFailure(w, e)
		return ea.Access{}, false
	}
	p, e := actorref.ParsePrincipal(a.AccountType, a.ID)
	if e != nil || p.Type != actorref.Person {
		messagePolicyFailure(w, mp.ErrDenied)
		return ea.Access{}, false
	}
	access := ea.Access{Actor: a, SessionDigest: d}
	if !access.Valid() {
		messagePolicyFailure(w, mp.ErrDenied)
		return ea.Access{}, false
	}
	return access, true
}
func (s *server) messagePolicyAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	a, ok := s.messageWriteAccess(w, r)
	if !ok {
		return agentprofile.PrivateAccess{}, false
	}
	if s.messagePolicies == nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	return agentprofile.PrivateAccess{SessionDigest: a.SessionDigest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: a.Actor.ID}}, true
}
func messagePolicyVersion(w http.ResponseWriter, r *http.Request) (string, bool) {
	v := r.Header.Values("X-Birdtie-Message-Policy-Version")
	if len(v) == 0 {
		return "", true
	}
	if len(v) != 1 || !mp.ValidSourceVersion(v[0]) {
		messagePolicyFailure(w, mp.ErrInvalid)
		return "", false
	}
	return v[0], true
}
func messagePolicyNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil {
		return true
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 1))
	if e != nil || len(b) != 0 {
		messagePolicyFailure(w, mp.ErrInvalid)
		return false
	}
	return true
}
func (s *server) messagePolicyResponse(w http.ResponseWriter, r *http.Request, a agentprofile.PrivateAccess, value any) {
	s.messageWriteResponse(w, r, eaMessageHTTPAccess(a), http.StatusOK, value)
}
func eaMessageHTTPAccess(a agentprofile.PrivateAccess) ea.Access {
	return ea.Access{Actor: identity.Actor{ID: a.WorkspacePrincipal.ID, AccountType: "person"}, SessionDigest: a.SessionDigest}
}
func (s *server) messageWriteResponse(w http.ResponseWriter, r *http.Request, a ea.Access, status int, value any) {
	raw, e := json.Marshal(map[string]any{"data": value})
	if e != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	access, ok := s.access.(socialnow.HumanSessionStore)
	if !ok {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	if e = access.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, a.Actor); e != nil {
		messagePolicyFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) getOwnMessagePolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.messagePolicyAccess(w, r)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	p, e := s.messagePolicies.GetOwnMessagePolicy(r.Context(), a)
	if e != nil {
		messagePolicyFailure(w, e)
		return
	}
	if !mp.ValidRecord(p, a.WorkspacePrincipal.ID) || !uuidPath.MatchString(p.AgentID) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.messagePolicyResponse(w, r, a, p)
}
func (s *server) putOwnMessagePolicy(w http.ResponseWriter, r *http.Request) {
	a, ok := s.messagePolicyAccess(w, r)
	if !ok {
		return
	}
	ct, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || ct != "application/json" || len(r.Header.Values("Content-Type")) != 1 || len(params) > 1 || params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8") {
		messagePolicyFailure(w, mp.ErrInvalid)
		return
	}
	for key := range params {
		if key != "charset" {
			messagePolicyFailure(w, mp.ErrInvalid)
			return
		}
	}
	if r.Body == nil {
		messagePolicyFailure(w, mp.ErrInvalid)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, mp.MaxBodyBytes+1))
	if e != nil {
		messagePolicyFailure(w, mp.ErrInvalid)
		return
	}
	in, e := mp.DecodePut(raw)
	if e != nil {
		messagePolicyFailure(w, e)
		return
	}
	p, e := s.messagePolicies.PutOwnMessagePolicy(r.Context(), a, in)
	if e != nil {
		messagePolicyFailure(w, e)
		return
	}
	if !mp.ValidRecord(p, a.WorkspacePrincipal.ID) || !uuidPath.MatchString(p.AgentID) || !p.Configured || p.NativeRevision != in.ExpectedVersion+1 || p.IncomingRequests != in.IncomingRequests || p.ExpiresAt == nil || !p.ExpiresAt.Equal(in.ExpiresAt) || p.Status != "ACTIVE" {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.messagePolicyResponse(w, r, a, p)
}
func (s *server) getMessagePolicyDecision(w http.ResponseWriter, r *http.Request) {
	a, ok := s.messagePolicyAccess(w, r)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	peer := r.PathValue("personID")
	if !uuidPath.MatchString(peer) {
		messagePolicyFailure(w, mp.ErrInvalid)
		return
	}
	p, e := s.messagePolicies.ReadMessagePolicyDecision(r.Context(), a, peer)
	if e != nil {
		messagePolicyFailure(w, e)
		return
	}
	if !mp.ValidDecision(p) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.messagePolicyResponse(w, r, a, p)
}
