package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// The ordinary Now POST is a human declaration entry. External tool text,
// Org workspaces and anonymous queries cannot manufacture this private write.
func (s *server) executeNowPreference(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, task agentworkspace.Task, principalType, workspace, role string, permissions []string) {
	w.Header().Set("Cache-Control", "no-store")
	if actor.ID == "" || actor.AccountType != "person" || principalType != "person" || task.PrincipalID != actor.ID || task.ActingUserID != actor.ID || r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		respondError(w, http.StatusForbidden, "personal_preference_owner_required")
		return
	}
	port, ok := s.agent.(agentworkspace.NowPreferencePort)
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "now_preference_unavailable")
		return
	}
	current, e := s.agent.UpdateTask(r.Context(), task)
	if e != nil {
		serverError(w, e)
		return
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(current))
	if e != nil {
		serverError(w, e)
		return
	}
	a := arp.Access{Actor: actor, SessionDigest: digest, TaskID: current.ID, ExpectedTask: raw}
	current, e = port.ExecuteOwnNowPreference(r.Context(), a)
	if e != nil {
		switch {
		case errors.Is(e, agentmemory.ErrInvalid), errors.Is(e, agentworkspace.ErrNowPreference):
			respondError(w, http.StatusBadRequest, "invalid_now_preference")
		case errors.Is(e, agentmemory.ErrForbidden), errors.Is(e, arp.ErrDenied):
			respondError(w, http.StatusForbidden, "personal_preference_owner_required")
		case errors.Is(e, agentmemory.ErrConflict), errors.Is(e, arp.ErrChanged):
			respondError(w, http.StatusConflict, "now_preference_changed")
		default:
			respondError(w, http.StatusServiceUnavailable, "now_preference_unavailable")
		}
		return
	}
	result := emptyAgentResults(current.CityID, current.Filters["currentQuery"])
	result = workspaceEnvelope(result, current, true, principalType, workspace, role, permissions, w.Header().Get("X-Request-ID"))
	result.Message = current.Conversation[len(current.Conversation)-1].Text
	result.Note = "仅处理本人明确声明的口味；不外传，不自动执行社交行动。"
	s.respondCurrentAgentTask(w, r, result, digest, actor)
}
