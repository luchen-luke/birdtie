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

// Explicit map selection is an independent selector, not a forged prior reply
// membership. This bounded path never dispatches a model or provider fallback.
func (s *server) executeSelectedPlaceFollowup(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, task agentworkspace.Task, query string, selected agentworkspace.SelectedPlaceSelector, workspace, role string, permissions []string) {
	if actor.AccountType != "person" || actor.ID == "" || actor.ID != task.PrincipalID || task.ActingUserID != actor.ID || task.PrincipalType != "person" || task.ContextType != "CITY" || digest == ([32]byte{}) || r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		respondError(w, http.StatusForbidden, "place_followup_forbidden")
		return
	}
	if _, _, ok := agentworkspace.ParseSelectedPlaceFollowup(query); !ok || !selected.Valid() {
		respondError(w, http.StatusBadRequest, "invalid_selected_place_context")
		return
	}
	port, ok := s.agent.(agentworkspace.SelectedPlaceFollowupPort)
	local, localOK := s.agent.(agentworkspace.LocalPlaceReplyPort)
	if !ok || port == nil || !localOK || local == nil {
		respondError(w, http.StatusServiceUnavailable, "place_followup_unavailable")
		return
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	if e != nil {
		serverError(w, e)
		return
	}
	a := arp.Access{Actor: actor, SessionDigest: digest, TaskID: task.ID, ExpectedTask: raw}
	next, e := port.ContinueOwnSelectedPlace(r.Context(), a, query, selected)
	if e != nil {
		switch {
		case errors.Is(e, arp.ErrDenied), errors.Is(e, agenttool.ErrDenied):
			respondError(w, http.StatusForbidden, "place_followup_forbidden")
		case errors.Is(e, agentworkspace.ErrPlaceFollowupNotFound):
			respondError(w, http.StatusNotFound, "place_followup_not_found")
		case errors.Is(e, agentworkspace.ErrPlaceFollowupChanged), errors.Is(e, agentworkspace.ErrPlaceFollowupAmbiguous), errors.Is(e, arp.ErrChanged), errors.Is(e, agenttool.ErrChanged):
			respondError(w, http.StatusConflict, "place_followup_changed")
		default:
			serverError(w, e)
		}
		return
	}
	newTask := task.Status == agentworkspace.TaskActive && task.Intent == "PENDING" && len(task.Conversation) == 1 && task.Conversation[0].Text == query
	count := len(task.Conversation) + 1
	if newTask {
		count--
	}
	if next.ID != task.ID || next.PrincipalType != task.PrincipalType || next.PrincipalID != task.PrincipalID || next.ActingUserID != task.ActingUserID || next.ContextType != task.ContextType || next.ContextID != task.ContextID || next.CityID != task.CityID || next.Status != agentworkspace.TaskActive || next.Intent != agentworkspace.FindPlace || next.Filters["currentQuery"] != query || next.Filters[agentworkspace.PlaceFollowupIDFilter] != selected.PlaceID || next.Filters[agentworkspace.SelectedPlaceContextFilter] != "native-map-v1" || len(next.Conversation) != count || next.Conversation[count-1].Role != "user" || next.Conversation[count-1].Text != query {
		respondError(w, http.StatusConflict, "place_followup_changed")
		return
	}
	raw, e = json.Marshal(agentworkspace.SanitizeTaskForResponse(next))
	if e != nil {
		serverError(w, e)
		return
	}
	a.ExpectedTask = raw
	reply, handled, e := local.TryOwnLocalPlaceReply(r.Context(), a)
	if e != nil {
		agentTaskWriteFailure(w, e, false)
		return
	}
	if !handled || reply == nil {
		agentTaskWriteFailure(w, errLocalPlaceAnswerUnavailable, false)
		return
	}
	completed := reply.Task()
	if completed.ID != next.ID || completed.PrincipalType != next.PrincipalType || completed.PrincipalID != next.PrincipalID || completed.ActingUserID != next.ActingUserID || completed.CityID != next.CityID || completed.ContextID != next.ContextID || completed.ContextType != next.ContextType || completed.Status != agentworkspace.TaskCompleted || len(completed.Conversation) != len(next.Conversation)+1 || completed.Conversation[len(completed.Conversation)-1].Role != "assistant" {
		respondError(w, http.StatusConflict, "place_followup_changed")
		return
	}
	result := emptyAgentResults(completed.CityID, query)
	result.Message = completed.Conversation[len(completed.Conversation)-1].Text
	result = workspaceEnvelope(result, completed, true, "person", workspace, role, permissions, w.Header().Get("X-Request-ID"))
	r = r.WithContext(context.WithValue(r.Context(), nowHumanReplyKey{}, true))
	r = r.WithContext(context.WithValue(r.Context(), nowLocalReplyKey{}, reply))
	s.respondCurrentAgentTask(w, r, result, digest, actor)
}

func isBoundedLocalPlaceFactQuestion(query string) bool {
	if agentworkspace.IsLocalPlaceHoursFollowupQuery(query) || agentworkspace.IsLocalPlaceAddressFollowupQuery(query) {
		return true
	}
	question, ok := agentworkspace.ParseLocalPlaceQuestion(query)
	return ok && !question.Recommendation && (question.Field == agentworkspace.PlaceAddress || question.Field == agentworkspace.PlaceOpeningHours)
}

func (s *server) continuePublicPlaceFollowup(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, task agentworkspace.Task, query string) (agentworkspace.Task, *http.Request, bool) {
	active := task
	active.Status = agentworkspace.TaskActive
	if actor.AccountType != "person" || actor.ID != task.PrincipalID || task.ActingUserID != actor.ID || task.ContextType != "CITY" || task.PrincipalType != "person" || r.Header.Get("X-Birdtie-Organization-Workspace") != "" || digest == ([32]byte{}) {
		respondError(w, http.StatusForbidden, "place_followup_forbidden")
		return task, r, false
	}
	port, ok := s.agent.(agentworkspace.PublicPlaceFollowupPort)
	localPort, localOK := s.agent.(agentworkspace.LocalPlaceReplyPort)
	localReady := isBoundedLocalPlaceFactQuestion(query) && localOK && localPort != nil && ok && port != nil
	// Independently permitted native address/hours facts do not require a model
	// account. The original native resolver still owns every entity/session gate;
	// a declined local answer cannot later become a count/rules response.
	if !localReady && (s.liveAnswers == nil || !s.liveAnswers.Eligible(r.Context(), digest, active)) {
		respond(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]string{"code": "place_answer_unavailable", "message": "地点信息回答暂未接通，本轮尚未回答你的问题。"}})
		return task, r, false
	}
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
