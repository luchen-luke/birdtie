package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
)

func connectionError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, connection.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, connection.ErrConflict):
		respondError(w, http.StatusConflict, "connection_conflict")
	case errors.Is(err, connection.ErrRateLimit):
		respondError(w, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(err, connection.ErrForbidden):
		respondError(w, http.StatusForbidden, "connection_forbidden")
	default:
		serverError(w, err)
	}
	return true
}

func (s *server) createConnectionRequest(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	var input struct {
		RecipientAccountID string `json:"recipientAccountId"`
		CityID             string `json:"cityId"`
		Note               string `json:"note"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.CityID = strings.TrimSpace(input.CityID)
	input.Note = strings.TrimSpace(input.Note)
	if !uuidPath.MatchString(input.RecipientAccountID) ||
		input.CityID == "" || len(input.CityID) > 80 ||
		len(input.Note) < 1 || len(input.Note) > 280 {
		respondError(w, http.StatusBadRequest, "invalid_connection_request")
		return
	}
	request, err := s.connections.CreateRequest(r.Context(), actor.ID,
		input.RecipientAccountID, input.CityID, input.Note)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusCreated, map[string]any{"data": request})
}

func (s *server) listConnectionRequests(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	requests, err := s.connections.ListRequests(r.Context(), actor.ID)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": requests})
}

func (s *server) decideConnectionRequest(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("requestID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_request_id")
		return
	}
	var input struct {
		Action string `json:"action"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.Action != "accept" && input.Action != "decline" && input.Action != "withdraw" {
		respondError(w, http.StatusBadRequest, "invalid_decision")
		return
	}
	request, err := s.connections.DecideRequest(r.Context(), actor.ID, id, input.Action)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": request})
}

func (s *server) listConversations(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	items, err := s.connections.ListConversations(r.Context(), actor.ID)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) listMessages(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("conversationID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	items, err := s.connections.ListMessages(r.Context(), actor.ID, id)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("conversationID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	var input struct {
		Body string `json:"body"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if len(input.Body) < 1 || len(input.Body) > 2000 {
		respondError(w, http.StatusBadRequest, "invalid_message")
		return
	}
	item, err := s.connections.SendMessage(r.Context(), actor.ID, id, input.Body)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusCreated, map[string]any{"data": item})
}
