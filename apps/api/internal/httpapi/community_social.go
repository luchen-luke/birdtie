package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/community"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func validSocialInput(in *community.SocialInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	in.Summary = strings.TrimSpace(in.Summary)
	in.AvatarURL = strings.TrimSpace(in.AvatarURL)
	in.CityID = strings.TrimSpace(in.CityID)
	in.Visibility = strings.ToLower(strings.TrimSpace(in.Visibility))
	in.JoinPolicy = strings.ToLower(strings.TrimSpace(in.JoinPolicy))
	if in.Visibility == "" {
		in.Visibility = "public"
	}
	if in.JoinPolicy == "" {
		in.JoinPolicy = "request"
	}
	if len([]rune(in.Name)) < 2 || len([]rune(in.Name)) > 160 || len([]rune(in.Summary)) > 3000 || len(in.CityID) > 80 {
		return false
	}
	if in.Visibility != "public" && in.Visibility != "private" && in.Visibility != "hidden" {
		return false
	}
	if in.JoinPolicy != "open" && in.JoinPolicy != "request" && in.JoinPolicy != "invite_only" {
		return false
	}
	if in.AvatarURL != "" {
		u, e := url.Parse(in.AvatarURL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(in.AvatarURL) > 1000 {
			return false
		}
	}
	return true
}

func socialMessage(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func socialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		socialMessage(w, 401, "unauthorized", "登录已失效，请重新登录")
	case errors.Is(err, community.ErrForbidden):
		socialMessage(w, 403, "community_forbidden", "你当前没有此项社区权限")
	case errors.Is(err, community.ErrNotFound):
		socialMessage(w, 404, "community_not_found", "社区或成员当前不可见，请返回刷新")
	case errors.Is(err, community.ErrConflict):
		socialMessage(w, 409, "community_conflict", "状态已变化，请刷新后重新检查")
	case errors.Is(err, community.ErrApprovalRequired):
		socialMessage(w, 428, "community_confirmation_required", "请先检查当前操作预览并确认")
	case errors.Is(err, community.ErrApprovalStale):
		socialMessage(w, 409, "community_confirmation_stale", "确认已过期或目标已变化，请重新检查")
	case errors.Is(err, community.ErrValidation):
		socialMessage(w, 422, "community_validation_error", "请检查社区资料与操作格式")
	default:
		socialMessage(w, 503, "community_social_unavailable", "社区服务暂不可用，请稍后刷新")
	}
}

type socialHumanTransport struct {
	actor  identity.Actor
	digest [32]byte
	store  community.HumanSocialStore
}

func (s *server) socialActor(w http.ResponseWriter, r *http.Request, hasID bool) (socialHumanTransport, bool) {
	w.Header().Set("Cache-Control", "no-store")
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return socialHumanTransport{}, false
	}
	if actor.AccountType != "person" {
		socialMessage(w, 403, "person_account_required", "请使用个人账号参与社区")
		return socialHumanTransport{}, false
	}
	store, ok := s.socialCommunities.(community.HumanSocialStore)
	if !ok || store == nil {
		socialMessage(w, 503, "community_social_unavailable", "社区服务暂不可用，请稍后刷新")
		return socialHumanTransport{}, false
	}
	if hasID && !uuidPath.MatchString(r.PathValue("communityID")) {
		socialMessage(w, 400, "invalid_community_id", "社区标识无效")
		return socialHumanTransport{}, false
	}
	return socialHumanTransport{actor: actor, digest: digest, store: store}, true
}
func socialFields(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]string, bool) {
	fail := func() (map[string]string, bool) {
		socialMessage(w, 400, "invalid_community_body", "请检查社区操作内容与格式")
		return nil, false
	}
	if len(r.Header.Values("Content-Type")) != 1 {
		return fail()
	}
	media, params, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || !strings.EqualFold(media, "application/json") {
		return fail()
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			return fail()
		}
	}
	if r.Body == nil {
		return fail()
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 20*1024))
	if e != nil || !utf8.Valid(raw) {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return fail()
	}
	out := map[string]string{}
	keys := map[string]bool{}
	for _, k := range allowed {
		keys[k] = true
	}
	for d.More() {
		t, e := d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !keys[k] {
			return fail()
		}
		if _, dup := out[k]; dup {
			return fail()
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return fail()
		}
		text, e := publicProfileJSONString(v)
		if e != nil {
			return fail()
		}
		out[k] = text
	}
	if token, e = d.Token(); e != nil || token != json.Delim('}') {
		return fail()
	}
	if _, e = d.Token(); e != io.EOF {
		return fail()
	}
	return out, true
}
func socialInput(w http.ResponseWriter, r *http.Request) (community.SocialInput, bool) {
	f, ok := socialFields(w, r, "name", "description", "avatarUrl", "cityId", "visibility", "joinPolicy")
	if !ok {
		return community.SocialInput{}, false
	}
	in := community.SocialInput{Name: f["name"], Summary: f["description"], AvatarURL: f["avatarUrl"], CityID: f["cityId"], Visibility: f["visibility"], JoinPolicy: f["joinPolicy"]}
	if !validSocialInput(&in) {
		socialError(w, community.ErrValidation)
		return community.SocialInput{}, false
	}
	return in, true
}
func socialRead(w http.ResponseWriter, r *http.Request, a socialHumanTransport, in community.HumanRead) (community.HumanResult, bool) {
	out, e := a.store.ReadHumanCommunity(r.Context(), a.digest, a.actor, in)
	if e != nil {
		socialError(w, e)
		return community.HumanResult{}, false
	}
	if in.Kind == "detail" && (out.Community == nil || out.Community.ID != in.CommunityID) {
		socialError(w, community.ErrUnavailable)
		return community.HumanResult{}, false
	}
	for _, m := range out.Members {
		if m.CommunityID != in.CommunityID {
			socialError(w, community.ErrUnavailable)
			return community.HumanResult{}, false
		}
	}
	return out, true
}
func socialMutation(w http.ResponseWriter, r *http.Request, a socialHumanTransport, in community.HumanCommand) (community.HumanResult, bool) {
	if in.Operation == "join" || in.Operation == "leave" {
		condition, ok := entityActionCondition(w, r, ea.Ref{Type: "community", ID: in.CommunityID}, ea.Join)
		if !ok {
			return community.HumanResult{}, false
		}
		in.ActionCondition = condition
	}
	previews := r.Header.Values("X-Birdtie-Community-Preview")
	snaps := r.Header.Values("X-Birdtie-Community-Snapshot")
	if len(previews) > 1 || len(snaps) > 1 || (len(previews) == 1 && previews[0] != "1") || (len(snaps) == 1 && (snaps[0] == "" || len(snaps[0]) > 200)) {
		socialMessage(w, 400, "invalid_community_confirmation", "确认格式无效，请重新检查")
		return community.HumanResult{}, false
	}
	in.Preview = len(previews) == 1
	in.Snapshot = r.Header.Get("X-Birdtie-Community-Snapshot")
	if in.Preview && in.Snapshot != "" {
		socialMessage(w, 400, "invalid_community_confirmation", "请先重新检查操作预览")
		return community.HumanResult{}, false
	}
	out, e := a.store.MutateHumanCommunity(r.Context(), a.digest, a.actor, in)
	if e != nil {
		if errors.Is(e, ea.ErrChanged) || errors.Is(e, ea.ErrInvalid) || errors.Is(e, ea.ErrNotFound) || errors.Is(e, ea.ErrUnavailable) {
			entityActionFailure(w, e)
			return community.HumanResult{}, false
		}
		socialError(w, e)
		return community.HumanResult{}, false
	}
	if in.Preview {
		p := out.Approval
		if p == nil || p.ActorID != a.actor.ID || p.CommunityID != in.CommunityID || p.TargetID != in.TargetID || p.Operation != in.Operation || p.Role != in.Role || p.Snapshot == "" {
			socialError(w, community.ErrUnavailable)
			return community.HumanResult{}, false
		}
		respond(w, 200, map[string]any{"data": p})
		return community.HumanResult{}, false
	}
	if out.Member != nil && (out.Member.CommunityID != in.CommunityID || (in.Operation == "join" && out.Member.UserAccountID != a.actor.ID)) {
		socialError(w, community.ErrUnavailable)
		return community.HumanResult{}, false
	}
	if out.Community != nil && in.Operation != "create" && out.Community.ID != in.CommunityID {
		socialError(w, community.ErrUnavailable)
		return community.HumanResult{}, false
	}
	return out, true
}
func (s *server) createSocialCommunity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActor(w, r, false)
	if !ok {
		return
	}
	in, ok := socialInput(w, r)
	if !ok {
		return
	}
	out, ok := socialMutation(w, r, a, community.HumanCommand{Operation: "create", Input: in})
	if !ok {
		return
	}
	if out.Community == nil || out.Community.CreatedBy != a.actor.ID {
		socialError(w, community.ErrUnavailable)
		return
	}
	respond(w, 201, map[string]any{"data": out.Community})
}
func (s *server) discoverSocialCommunities(w http.ResponseWriter, r *http.Request) {
	s.socialList(w, r, false)
}
func (s *server) listMySocialCommunities(w http.ResponseWriter, r *http.Request) {
	s.socialList(w, r, true)
}
func (s *server) socialList(w http.ResponseWriter, r *http.Request, mine bool) {
	a, ok := s.socialActor(w, r, false)
	if !ok {
		return
	}
	city := r.URL.Query().Get("cityId")
	if mine {
		city = ""
	}
	out, ok := socialRead(w, r, a, community.HumanRead{Kind: "list", CityID: city, Mine: mine})
	if ok {
		respond(w, 200, map[string]any{"data": out.Communities})
	}
}
func (s *server) getSocialCommunity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	out, ok := socialRead(w, r, a, community.HumanRead{Kind: "detail", CommunityID: r.PathValue("communityID")})
	if ok {
		respond(w, 200, map[string]any{"data": out.Community})
	}
}
func (s *server) updateSocialCommunity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	in, ok := socialInput(w, r)
	if !ok {
		return
	}
	out, ok := socialMutation(w, r, a, community.HumanCommand{Operation: "update", CommunityID: r.PathValue("communityID"), Input: in})
	if ok {
		if out.Community == nil {
			socialError(w, community.ErrUnavailable)
			return
		}
		respond(w, 200, map[string]any{"data": out.Community})
	}
}
func (s *server) archiveSocialCommunity(w http.ResponseWriter, r *http.Request) {
	s.socialEmptyAction(w, r, "archive")
}
func (s *server) leaveSocialCommunity(w http.ResponseWriter, r *http.Request) {
	s.socialEmptyAction(w, r, "leave")
}
func (s *server) socialEmptyAction(w http.ResponseWriter, r *http.Request, op string) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	_, ok = socialMutation(w, r, a, community.HumanCommand{Operation: op, CommunityID: r.PathValue("communityID")})
	if ok {
		w.WriteHeader(204)
	}
}
func (s *server) joinSocialCommunity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	out, ok := socialMutation(w, r, a, community.HumanCommand{Operation: "join", CommunityID: r.PathValue("communityID")})
	if ok {
		if out.Member == nil {
			socialError(w, community.ErrUnavailable)
			return
		}
		respond(w, 200, map[string]any{"data": out.Member})
	}
}
func (s *server) listSocialMembers(w http.ResponseWriter, r *http.Request) {
	s.listSocialMembersByState(w, r, false)
}
func (s *server) listSocialRequests(w http.ResponseWriter, r *http.Request) {
	s.listSocialMembersByState(w, r, true)
}
func (s *server) listSocialMembersByState(w http.ResponseWriter, r *http.Request, requests bool) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	out, ok := socialRead(w, r, a, community.HumanRead{Kind: "members", CommunityID: r.PathValue("communityID"), Requests: requests})
	if ok {
		respond(w, 200, map[string]any{"data": out.Members})
	}
}
func (s *server) approveSocialRequest(w http.ResponseWriter, r *http.Request) {
	s.decideSocialRequest(w, r, true)
}
func (s *server) rejectSocialRequest(w http.ResponseWriter, r *http.Request) {
	s.decideSocialRequest(w, r, false)
}
func (s *server) decideSocialRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	op := "reject"
	if approve {
		op = "approve"
	}
	s.socialMemberAction(w, r, op, "requestID")
}
func (s *server) changeSocialMemberRole(w http.ResponseWriter, r *http.Request) {
	s.socialMemberAction(w, r, "role", "memberID")
}
func (s *server) removeSocialMember(w http.ResponseWriter, r *http.Request) {
	s.socialMemberAction(w, r, "remove", "memberID")
}
func (s *server) socialMemberAction(w http.ResponseWriter, r *http.Request, op, pathKey string) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	target := r.PathValue(pathKey)
	if !uuidPath.MatchString(target) {
		socialMessage(w, 400, "invalid_member_id", "成员标识无效")
		return
	}
	in := community.HumanCommand{Operation: op, CommunityID: r.PathValue("communityID"), TargetID: target}
	if op == "role" {
		f, ok := socialFields(w, r, "role")
		if !ok {
			return
		}
		in.Role = strings.ToLower(strings.TrimSpace(f["role"]))
	}
	out, ok := socialMutation(w, r, a, in)
	if !ok {
		return
	}
	if op == "remove" {
		w.WriteHeader(204)
		return
	}
	if out.Member == nil || out.Member.ID != target {
		socialError(w, community.ErrUnavailable)
		return
	}
	respond(w, 200, map[string]any{"data": out.Member})
}
func (s *server) inviteSocialMember(w http.ResponseWriter, r *http.Request) {
	s.socialPersonAction(w, r, "invite")
}
func (s *server) transferSocialOwner(w http.ResponseWriter, r *http.Request) {
	s.socialPersonAction(w, r, "transfer")
}
func (s *server) socialPersonAction(w http.ResponseWriter, r *http.Request, op string) {
	a, ok := s.socialActor(w, r, true)
	if !ok {
		return
	}
	f, ok := socialFields(w, r, "userAccountId")
	if !ok {
		return
	}
	target := f["userAccountId"]
	if !uuidPath.MatchString(target) {
		socialError(w, community.ErrValidation)
		return
	}
	out, ok := socialMutation(w, r, a, community.HumanCommand{Operation: op, CommunityID: r.PathValue("communityID"), TargetID: target})
	if !ok {
		return
	}
	if op == "transfer" {
		w.WriteHeader(204)
		return
	}
	if out.Member == nil || out.Member.UserAccountID != target {
		socialError(w, community.ErrUnavailable)
		return
	}
	respond(w, 201, map[string]any{"data": out.Member})
}
