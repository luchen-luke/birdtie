package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

func notificationScheduleFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "notification_schedule_unavailable", "通知计划暂不可用，请稍后重试。"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "登录已失效，请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(e, ns.ErrInvalid):
		status, code, message = 400, "invalid_notification_schedule", "请检查明确时区、汇总时间、静默时段和滚动24小时触达额度。"
	case errors.Is(e, ns.ErrDenied):
		status, code, message = 403, "notification_schedule_forbidden", "请使用当前本人账号管理通知计划。"
	case errors.Is(e, ns.ErrChanged):
		status, code, message = 409, "notification_schedule_changed", "通知计划或来源已更新，请重新读取后保存。"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "detail": message}})
}
func (s *server) notificationScheduleAccess(w http.ResponseWriter, r *http.Request) (agentprofile.PrivateAccess, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		notificationScheduleFailure(w, ns.ErrDenied)
		return agentprofile.PrivateAccess{}, false
	}
	if s.access == nil || s.notificationSchedules == nil {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return agentprofile.PrivateAccess{}, false
	}
	actor, digest, e := s.humanSocialActor(r, true)
	if e != nil {
		notificationScheduleFailure(w, e)
		return agentprofile.PrivateAccess{}, false
	}
	principal, e := actorref.ParsePrincipal(actor.AccountType, actor.ID)
	if e != nil || principal.Type != actorref.Person {
		notificationScheduleFailure(w, ns.ErrDenied)
		return agentprofile.PrivateAccess{}, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		notificationScheduleFailure(w, ns.ErrInvalid)
		return agentprofile.PrivateAccess{}, false
	}
	a := agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		notificationScheduleFailure(w, ns.ErrDenied)
		return a, false
	}
	return a, true
}
func (s *server) notificationScheduleResponse(w http.ResponseWriter, r *http.Request, a agentprofile.PrivateAccess, p ns.Policy) {
	if ns.ValidatePolicy(p) != nil {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": p})
	if e != nil {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	current, ok := s.access.(socialnow.HumanSessionStore)
	if !ok {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	if e = current.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, identity.Actor{ID: a.WorkspacePrincipal.ID, AccountType: "person"}); e != nil {
		notificationScheduleFailure(w, e)
		return
	}
	// The native read/write transaction has already ended. Validate the same
	// Personal Agent again after encoding; a committed PUT is not undone here.
	agents, ok := s.access.(contextInventoryAgentResolver)
	if !ok || taskContextInventoryNil(agents) {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	agent, e := agents.ResolveOwnContextAgent(r.Context(), a)
	if e != nil {
		if errors.Is(e, acb.ErrDenied) {
			e = ns.ErrDenied
		}
		notificationScheduleFailure(w, e)
		return
	}
	if agent.AgentID != p.AgentID || agent.Principal != a.WorkspacePrincipal || agent.Role != agentruntime.PersonalAgent {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	if r.Context().Err() != nil {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) getOwnNotificationSchedule(w http.ResponseWriter, r *http.Request) {
	a, ok := s.notificationScheduleAccess(w, r)
	if !ok {
		return
	}
	if r.Body != nil {
		body, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(body) != 0 {
			notificationScheduleFailure(w, ns.ErrInvalid)
			return
		}
	}
	p, e := s.notificationSchedules.GetOwnNotificationSchedule(r.Context(), a)
	if e != nil {
		notificationScheduleFailure(w, e)
		return
	}
	s.notificationScheduleResponse(w, r, a, p)
}
func (s *server) putOwnNotificationSchedule(w http.ResponseWriter, r *http.Request) {
	a, ok := s.notificationScheduleAccess(w, r)
	if !ok {
		return
	}
	headers := r.Header.Values("Content-Type")
	if len(headers) != 1 || r.Body == nil {
		notificationScheduleFailure(w, ns.ErrInvalid)
		return
	}
	media, params, e := mime.ParseMediaType(headers[0])
	if e != nil || !strings.EqualFold(media, "application/json") {
		notificationScheduleFailure(w, ns.ErrInvalid)
		return
	}
	for k, v := range params {
		if k != "charset" || !strings.EqualFold(v, "utf-8") {
			notificationScheduleFailure(w, ns.ErrInvalid)
			return
		}
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, ns.MaxBodyBytes))
	if e != nil {
		notificationScheduleFailure(w, ns.ErrInvalid)
		return
	}
	in, e := ns.DecodePut(body)
	if e != nil {
		notificationScheduleFailure(w, e)
		return
	}
	p, e := s.notificationSchedules.PutOwnNotificationSchedule(r.Context(), a, in)
	if e != nil {
		notificationScheduleFailure(w, e)
		return
	}
	if p.Version != in.ExpectedVersion+1 || p.ValidFrom == nil || p.ExpiresAt == nil || !p.ExpiresAt.Equal(in.ExpiresAt) || p.Status != "ACTIVE" && p.Status != "DISABLED" {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	normalized, e := ns.NormalizePut(in, *p.ValidFrom)
	if e != nil || !reflect.DeepEqual(normalized.Settings, p.Settings) {
		notificationScheduleFailure(w, ns.ErrUnavailable)
		return
	}
	s.notificationScheduleResponse(w, r, a, p)
}
