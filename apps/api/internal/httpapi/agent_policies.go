package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func policyAPIError(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func policyAPIFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		policyAPIError(w, 401, "unauthorized", "登录已失效，请重新登录")
	case errors.Is(err, agentpolicysettings.ErrInvalid):
		policyAPIError(w, 400, "invalid_agent_policy", "策略内容或有效期无效，请检查后重试")
	case errors.Is(err, agentpolicysettings.ErrForbidden):
		policyAPIError(w, 403, "agent_policy_forbidden", "请使用当前本人账号管理个人策略")
	case errors.Is(err, agentpolicysettings.ErrNotFound):
		policyAPIError(w, 404, "agent_policy_identity_unavailable", "当前个人 Agent 身份不可用")
	case errors.Is(err, agentpolicysettings.ErrConflict):
		policyAPIError(w, 409, "agent_policy_version_conflict", "该类策略已更新，请重新读取后保存")
	default:
		policyAPIError(w, 503, "agent_policy_unavailable", "策略设置暂不可用，请稍后重试")
	}
}
func (s *server) ownAgentPolicyAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		policyAPIFailure(w, agentpolicysettings.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	if s.access == nil || s.policySettings == nil {
		policyAPIFailure(w, agentpolicysettings.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, err := s.actor(r, true)
	if err != nil {
		policyAPIFailure(w, err)
		return agentprofile.PrivateAccess{}, false
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || principal.Type != actorref.Person {
		policyAPIFailure(w, agentpolicysettings.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		policyAPIFailure(w, agentpolicysettings.ErrInvalid)
		return agentprofile.PrivateAccess{}, false
	}
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		policyAPIFailure(w, agentpolicysettings.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	return access, true
}
func policyAPIResponse(w http.ResponseWriter, access agentprofile.PrivateAccess, b agentpolicysettings.Bundle) bool {
	if agentpolicysettings.ValidateBundle(b) != nil || b.OwnerID != access.WorkspacePrincipal.ID || b.OwnerType != actorref.Person {
		policyAPIFailure(w, agentpolicysettings.ErrUnavailable)
		return false
	}
	respond(w, 200, map[string]any{"data": b})
	return true
}
func (s *server) getOwnAgentPolicies(w http.ResponseWriter, r *http.Request) {
	access, ok := s.ownAgentPolicyAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			policyAPIFailure(w, agentpolicysettings.ErrInvalid)
			return
		}
	}
	b, err := s.policySettings.GetOwnPolicies(r.Context(), access)
	if err != nil {
		policyAPIFailure(w, err)
		return
	}
	policyAPIResponse(w, access, b)
}
func (s *server) putOwnAgentPolicy(w http.ResponseWriter, r *http.Request, family agentpolicysettings.Family) {
	access, ok := s.ownAgentPolicyAccess(w, r)
	if !ok {
		return
	}
	headers := r.Header.Values("Content-Type")
	if len(headers) != 1 || r.Body == nil {
		policyAPIFailure(w, agentpolicysettings.ErrInvalid)
		return
	}
	media, params, err := mime.ParseMediaType(headers[0])
	if err != nil || !strings.EqualFold(media, "application/json") {
		policyAPIFailure(w, agentpolicysettings.ErrInvalid)
		return
	}
	for name, value := range params {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			policyAPIFailure(w, agentpolicysettings.ErrInvalid)
			return
		}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, agentpolicysettings.MaxBodyBytes))
	if err != nil {
		policyAPIFailure(w, agentpolicysettings.ErrInvalid)
		return
	}
	input, err := agentpolicysettings.DecodePutInput(family, body)
	if err != nil {
		policyAPIFailure(w, err)
		return
	}
	b, err := s.policySettings.PutOwnPolicy(r.Context(), access, family, input)
	if err != nil {
		policyAPIFailure(w, err)
		return
	}
	var saved agentpolicysettings.Record
	switch family {
	case agentpolicysettings.Attention:
		saved = b.Attention
	case agentpolicysettings.Social:
		saved = b.Social
	case agentpolicysettings.Autonomy:
		saved = b.Autonomy
	default:
		policyAPIFailure(w, agentpolicysettings.ErrUnavailable)
		return
	}
	if saved.NativeRevision != input.ExpectedVersion+1 || !saved.Configured || saved.Status != "ACTIVE" || saved.ValidFrom == nil || saved.ExpiresAt == nil || !saved.ExpiresAt.Equal(input.ExpiresAt) {
		policyAPIFailure(w, agentpolicysettings.ErrUnavailable)
		return
	}
	normalized, err := agentpolicysettings.NormalizeSettings(family, input.Settings, *saved.ValidFrom, *saved.ExpiresAt)
	if err != nil || string(normalized) != string(saved.Settings) {
		policyAPIFailure(w, agentpolicysettings.ErrUnavailable)
		return
	}
	policyAPIResponse(w, access, b)
}
