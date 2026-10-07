package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/activitychat"
)

func (s *server) activityChatActor(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", "", false
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", "", false
	}
	if s.activityChat == nil {
		respondError(w, http.StatusServiceUnavailable, "activity_chat_unavailable")
		return "", "", false
	}
	id := r.PathValue("activityID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return "", "", false
	}
	return actor.ID, id, true
}
func activityChatError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, activitychat.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, activitychat.ErrClosed):
		respondError(w, http.StatusConflict, "activity_chat_closed")
	case errors.Is(err, activitychat.ErrConflict):
		respondError(w, http.StatusConflict, "activity_message_conflict")
	case errors.Is(err, activitychat.ErrRateLimited):
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(err, activitychat.ErrForbidden):
		respondError(w, http.StatusForbidden, "activity_chat_forbidden")
	default:
		serverError(w, err)
	}
	return true
}
func (s *server) getActivityChat(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.activityChatActor(w, r)
	if !ok {
		return
	}
	state, err := s.activityChat.ActivityChatState(r.Context(), actor, id)
	if !activityChatError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": state})
	}
}
func (s *server) joinActivityChat(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.activityChatActor(w, r)
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
		respondError(w, http.StatusBadRequest, "activity_chat_confirmation_required")
		return
	}
	state, err := s.activityChat.JoinActivityChat(r.Context(), actor, id)
	if !activityChatError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": state})
	}
}
func (s *server) leaveActivityChat(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.activityChatActor(w, r)
	if !ok {
		return
	}
	if !activityChatError(w, s.activityChat.LeaveActivityChat(r.Context(), actor, id)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
func (s *server) listActivityChatMessages(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.activityChatActor(w, r)
	if !ok {
		return
	}
	before := r.URL.Query().Get("before")
	if before != "" && !uuidPath.MatchString(before) {
		respondError(w, http.StatusBadRequest, "invalid_message_cursor")
		return
	}
	page, err := s.activityChat.ActivityChatMessages(r.Context(), actor, id, before)
	if !activityChatError(w, err) {
		respond(w, http.StatusOK, map[string]any{"data": page})
	}
}
func (s *server) sendActivityChatMessage(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.activityChatActor(w, r)
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
		respondError(w, http.StatusBadRequest, "invalid_activity_message")
		return
	}
	message, err := s.activityChat.SendActivityChatMessage(r.Context(), actor, id, input.ClientID, input.Body)
	if !activityChatError(w, err) {
		respond(w, http.StatusCreated, map[string]any{"data": message})
	}
}
func (s *server) removeActivityChatMessage(w http.ResponseWriter, r *http.Request) {
	actor, id, ok := s.activityChatActor(w, r)
	if !ok {
		return
	}
	message := r.PathValue("messageID")
	if !uuidPath.MatchString(message) {
		respondError(w, http.StatusBadRequest, "invalid_message_id")
		return
	}
	if !activityChatError(w, s.activityChat.RemoveActivityChatMessage(r.Context(), actor, id, message)) {
		w.WriteHeader(http.StatusNoContent)
	}
}
