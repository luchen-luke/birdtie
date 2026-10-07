package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/communitychat"
)

func (s *server) communityChatActor(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", "", false
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", "", false
	}
	if s.communityChat == nil {
		respondError(w, http.StatusServiceUnavailable, "community_chat_unavailable")
		return "", "", false
	}
	id := r.PathValue("communityID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_community_id")
		return "", "", false
	}
	return actor.ID, id, true
}
func communityChatError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, communitychat.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, communitychat.ErrConflict):
		respondError(w, http.StatusConflict, "community_message_conflict")
	case errors.Is(err, communitychat.ErrRateLimited):
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(err, communitychat.ErrForbidden):
		respondError(w, http.StatusForbidden, "community_chat_forbidden")
	default:
		serverError(w, err)
	}
	return true
}
func (s *server) getCommunityChat(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.communityChatActor(w, r)
	if !ok {
		return
	}
	state, err := s.communityChat.CommunityChatState(r.Context(), actor, id)
	if !communityChatError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": state})
	}
}
func (s *server) joinCommunityChat(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.communityChatActor(w, r)
	if !ok {
		return
	}
	var input struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !input.Confirmed {
		respondError(w, http.StatusBadRequest, "community_chat_confirmation_required")
		return
	}
	state, err := s.communityChat.JoinCommunityChat(r.Context(), actor, id)
	if !communityChatError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": state})
	}
}
func (s *server) leaveCommunityChat(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.communityChatActor(w, r)
	if !ok {
		return
	}
	if !communityChatError(w, s.communityChat.LeaveCommunityChat(r.Context(), actor, id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
func (s *server) listCommunityChatMessages(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.communityChatActor(w, r)
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	if before != "" && !uuidPath.MatchString(before) {
		respondError(w, http.StatusBadRequest, "invalid_message_cursor")
		return
	}
	page, err := s.communityChat.CommunityChatMessages(r.Context(), actor, id, before)
	if !communityChatError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": page})
	}
}
func (s *server) sendCommunityChatMessage(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.communityChatActor(w, r)
	if !ok {
		return
	}
	var input struct {
		Body     string `json:"body"`
		ClientID string `json:"clientMessageId"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if !uuidPath.MatchString(input.ClientID) || len([]rune(input.Body)) < 1 || len([]rune(input.Body)) > 2000 {
		respondError(w, http.StatusBadRequest, "invalid_community_message")
		return
	}
	message, err := s.communityChat.SendCommunityChatMessage(r.Context(), actor, id, input.ClientID, input.Body)
	if !communityChatError(w, err) {
		respond(w, http.StatusCreated, map[string]any{"data": message})
	}
}
func (s *server) removeCommunityChatMessage(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.communityChatActor(w, r)
	if !ok {
		return
	}
	message := r.PathValue("messageID")
	if !uuidPath.MatchString(message) {
		respondError(w, http.StatusBadRequest, "invalid_message_id")
		return
	}
	if !communityChatError(w, s.communityChat.RemoveCommunityChatMessage(r.Context(), actor, id, message)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
