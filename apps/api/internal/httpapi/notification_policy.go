package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

// These handlers manage explicit ordinary notification preferences for the
// current human Person. They do not enable enrichment, models, Memory or A2A.
func (s *server) notificationPolicyAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		notificationPolicyError(w, http.StatusForbidden, "personal_notification_policy_required", "请切换到本人账号管理通知偏好。")
		return agentprofile.PrivateAccess{}, false
	}
	if s.access == nil || s.notificationPolicies == nil {
		notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, err := s.humanSocialActor(r, true)
	if errors.Is(err, identity.ErrUnauthorized) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		notificationPolicyError(w, http.StatusUnauthorized, "unauthorized", "请登录后管理通知偏好。")
		return agentprofile.PrivateAccess{}, false
	}
	if err != nil {
		notificationPolicyFailure(w, err)
		return agentprofile.PrivateAccess{}, false
	}
	principal, err := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if err != nil || principal.Type != actorref.Person {
		notificationPolicyError(w, http.StatusForbidden, "personal_notification_policy_required", "请使用本人账号管理通知偏好。")
		return agentprofile.PrivateAccess{}, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		notificationPolicyError(w, http.StatusBadRequest, "notification_policy_query_not_supported", "通知偏好不接受账号或来源查询参数。")
		return agentprofile.PrivateAccess{}, false
	}
	access := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if agentprofile.ValidatePrivateAccess(access) != nil {
		notificationPolicyFailure(w, agentnotification.ErrForbidden)
		return agentprofile.PrivateAccess{}, false
	}
	return access, true
}

func notificationPolicyError(w http.ResponseWriter, status int, code, detail string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "detail": detail}})
}

func notificationPolicyFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentnotification.ErrInvalid):
		notificationPolicyError(w, http.StatusBadRequest, "invalid_notification_policy", "通知偏好格式或有效期不正确，请检查后重试。")
	case errors.Is(err, agentnotification.ErrForbidden):
		notificationPolicyError(w, http.StatusForbidden, "notification_policy_forbidden", "当前无权管理此通知偏好，请重新登录本人账号。")
	case errors.Is(err, agentnotification.ErrNotFound):
		notificationPolicyError(w, http.StatusNotFound, "notification_policy_not_found", "当前个人身份不可用，请重新登录后重试。")
	case errors.Is(err, agentnotification.ErrConflict):
		notificationPolicyError(w, http.StatusConflict, "notification_policy_version_conflict", "通知偏好已更新，请重新读取后保存。")
	default:
		log.Printf("request_id=%s notification_policy_error category=unavailable", w.Header().Get("X-Request-ID"))
		notificationPolicyError(w, http.StatusServiceUnavailable, "notification_policy_unavailable", "通知偏好暂不可用，请稍后重试。")
	}
}

func (s *server) notificationPolicyResponse(w http.ResponseWriter, r *http.Request, access agentprofile.PrivateAccess, policy agentnotification.Policy) {
	// Shape validation only. The real Store resolves session, exact active
	// PersonalAgent and metadata binding; an Agent UUID is not permission.
	if agentnotification.ValidatePolicy(policy) != nil {
		notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		return
	}
	raw, err := json.Marshal(map[string]any{"data": policy})
	if err != nil {
		notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		return
	}
	current, ok := s.access.(socialnow.HumanSessionStore)
	if !ok {
		notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		return
	}
	actor := identity.Actor{ID: access.WorkspacePrincipal.ID, AccountType: "person"}
	if err = current.ValidateHumanSocialResponse(r.Context(), access.SessionDigest, actor); err != nil {
		if errors.Is(err, identity.ErrUnauthorized) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			notificationPolicyError(w, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录。")
		} else {
			notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		}
		return
	}
	if r.Context().Err() != nil {
		notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}

func (s *server) getOwnNotificationPolicy(w http.ResponseWriter, r *http.Request) {
	access, ok := s.notificationPolicyAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(body) != 0 {
			notificationPolicyError(w, http.StatusBadRequest, "notification_policy_get_body_not_supported", "读取通知偏好时不接受请求正文。")
			return
		}
	}
	policy, err := s.notificationPolicies.GetOwnNotificationPolicy(r.Context(), access)
	if err != nil {
		notificationPolicyFailure(w, err)
		return
	}
	s.notificationPolicyResponse(w, r, access, policy)
}

func (s *server) putOwnNotificationPolicy(w http.ResponseWriter, r *http.Request) {
	access, ok := s.notificationPolicyAccess(w, r)
	if !ok {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(media, "application/json") {
		notificationPolicyError(w, http.StatusUnsupportedMediaType, "json_required", "请使用正确的通知偏好请求格式。")
		return
	}
	if r.Body == nil {
		notificationPolicyFailure(w, agentnotification.ErrInvalid)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, agentnotification.MaxBodyBytes+1))
	if err != nil {
		notificationPolicyFailure(w, agentnotification.ErrInvalid)
		return
	}
	if len(body) > agentnotification.MaxBodyBytes {
		notificationPolicyError(w, http.StatusRequestEntityTooLarge, "notification_policy_body_too_large", "通知偏好内容过大，请减少规则后重试。")
		return
	}
	input, err := agentnotification.DecodePutInput(body)
	if err != nil {
		notificationPolicyFailure(w, err)
		return
	}
	policy, err := s.notificationPolicies.PutOwnNotificationPolicy(r.Context(), access, input)
	if err != nil {
		notificationPolicyFailure(w, err)
		return
	}
	if input.ExpectedVersion == agentnotification.MaxVersion || policy.Version != input.ExpectedVersion+1 {
		notificationPolicyFailure(w, agentnotification.ErrUnavailable)
		return
	}
	s.notificationPolicyResponse(w, r, access, policy)
}
