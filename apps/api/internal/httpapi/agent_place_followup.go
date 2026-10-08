package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type publicPlaceFollowupKey struct{}

func (s *server) continuePublicPlaceFollowup(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, task agentworkspace.Task, query string) (agentworkspace.Task, *http.Request, bool) {
	active := task
	active.Status = agentworkspace.TaskActive
	if actor.AccountType != "person" || actor.ID != task.PrincipalID || task.ActingUserID != actor.ID || task.ContextType != "CITY" || task.PrincipalType != "person" || r.Header.Get("X-Birdtie-Organization-Workspace") != "" || digest == ([32]byte{}) {
		respondError(w, http.StatusForbidden, "place_followup_forbidden")
		return task, r, false
	}
	// A count/rules reply cannot answer opening hours. Refuse before changing
	// the original Task when the actual model/source boundary is unavailable.
	if s.liveAnswers == nil || !s.liveAnswers.Eligible(r.Context(), digest, active) {
		respond(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "place_answer_unavailable", "message": "地点信息回答暂未接通，本轮尚未回答开放时间。"}})
		return task, r, false
	}
	port, ok := s.agent.(agentworkspace.PublicPlaceFollowupPort)
	if !ok || port == nil {
		respondError(w, http.StatusServiceUnavailable, "place_followup_unavailable")
		return task, r, false
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		serverError(w, e)
		return task, r, false
	}
	next, e := port.ContinueOwnPublicPlace(r.Context(), arp.Access{Actor: actor, SessionDigest: digest, TaskID: task.ID, ExpectedTask: raw}, query)
	if e != nil {
		switch {
		case errors.Is(e, agentworkspace.ErrPlaceFollowupAmbiguous):
			respond(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "place_followup_ambiguous", "message": "上轮有多个或未确定的地点，请先说明地点名称。"}})
		case errors.Is(e, agentworkspace.ErrPlaceFollowupNotFound):
			respond(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "place_followup_not_found", "message": "当前城市没有匹配且仍公开的地点，本轮尚未回答。"}})
		case errors.Is(e, agentworkspace.ErrPlaceFollowupChanged), errors.Is(e, arp.ErrChanged), errors.Is(e, agenttool.ErrChanged):
			respond(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "place_followup_changed", "message": "原地点或查询来源已变化，请明确当前地点后再问。"}})
		case errors.Is(e, arp.ErrDenied), errors.Is(e, agenttool.ErrDenied):
			respondError(w, http.StatusForbidden, "place_followup_forbidden")
		default:
			serverError(w, e)
		}
		return task, r, false
	}
	if next.ID != task.ID || next.PrincipalType != task.PrincipalType || next.PrincipalID != task.PrincipalID || next.ActingUserID != task.ActingUserID || next.CityID != task.CityID || next.ContextID != task.ContextID || next.ContextType != task.ContextType || next.Status != agentworkspace.TaskActive || next.Intent != agentworkspace.FindPlace || next.Filters["currentQuery"] != query || len(next.Conversation) != len(task.Conversation)+1 || next.Conversation[len(next.Conversation)-1].Role != "user" || next.Conversation[len(next.Conversation)-1].Text != query {
		respondError(w, http.StatusConflict, "place_followup_changed")
		return task, r, false
	}
	return next, r.WithContext(context.WithValue(r.Context(), publicPlaceFollowupKey{}, true)), true
}

func currentPublicPlaceFollowup(r *http.Request) bool {
	value, _ := r.Context().Value(publicPlaceFollowupKey{}).(bool)
	return value
}
