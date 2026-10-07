package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func connectionError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, mp.ErrInvalid) || errors.Is(err, mp.ErrDenied) || errors.Is(err, mp.ErrChanged) || errors.Is(err, mp.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized):
		messagePolicyFailure(w, err)
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
	access, ok := s.messageWriteAccess(w, r)
	if !ok {
		return
	}
	port, found := s.connections.(connection.CurrentStore)
	if !found {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	var err error
	var input struct {
		RecipientAccountID string `json:"recipientAccountId"`
		CityID             string `json:"cityId"`
		Note               string `json:"note"`
		Scope              string `json:"scope"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.CityID = strings.TrimSpace(input.CityID)
	input.Note = strings.TrimSpace(input.Note)
	if !uuidPath.MatchString(input.RecipientAccountID) ||
		len(input.Note) < 1 || len(input.Note) > 280 ||
		(input.Scope != "" && input.Scope != "conversation" && input.Scope != "friend") ||
		(input.Scope == "friend" && input.CityID != "") ||
		(input.Scope != "friend" && (input.CityID == "" || len(input.CityID) > 80)) {
		respondError(w, http.StatusBadRequest, "invalid_connection_request")
		return
	}
	version, ok := messagePolicyVersion(w, r)
	if !ok {
		return
	}
	condition, ok := entityActionCondition(w, r, ea.Ref{Type: "person", ID: input.RecipientAccountID}, ea.Connect)
	if !ok {
		return
	}
	var request connection.Request
	if input.Scope == "friend" {
		request, err = port.CreateFriendRequestCurrent(r.Context(), access, input.RecipientAccountID, input.Note, version, condition)
	} else {
		request, err = port.CreateRequestCurrent(r.Context(), access, input.RecipientAccountID, input.CityID, input.Note, version, condition)
	}

	if errors.Is(err, ea.ErrChanged) || errors.Is(err, ea.ErrInvalid) || errors.Is(err, ea.ErrNotFound) || errors.Is(err, ea.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		entityActionFailure(w, err)
		return
	}
	if connectionError(w, err) {
		return
	}
	s.messageWriteResponse(w, r, access, http.StatusCreated, request)
}

func (s *server) listPersonTies(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if r.URL.RawQuery != "" {
		respondError(w, http.StatusBadRequest, "owner_override_not_allowed")
		return
	}
	human, ok := s.connections.(socialnow.HumanTiesStore)
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "current_social_sources_unavailable")
		return
	}
	items, err := human.ListHumanTies(r.Context(), digest, actor)
	if err != nil {
		socialNowReadError(w, err)
		return
	}
	s.respondHumanSocialNow(w, r, digest, actor, map[string]any{"data": items})
}

func (s *server) removePersonTie(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("tieID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_tie_id")
		return
	}
	if connectionError(w, s.connections.RemoveTie(r.Context(), actor.ID, id)) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) startFriendConversation(w http.ResponseWriter, r *http.Request) {
	access, ok := s.messageWriteAccess(w, r)
	if !ok {
		return
	}
	port, found := s.connections.(connection.CurrentStore)
	if !found {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	id := r.PathValue("tieID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_tie_id")
		return
	}
	// The original native writer derives the actual other Person from this
	// Tie. The path ID here only validates the optional condition's shape.
	condition, ok := entityActionCondition(w, r, ea.Ref{Type: "person", ID: id}, ea.Message)
	if !ok {
		return
	}
	item, err := port.StartFriendConversationCurrent(r.Context(), access, id, condition)

	if errors.Is(err, ea.ErrChanged) || errors.Is(err, ea.ErrInvalid) || errors.Is(err, ea.ErrNotFound) || errors.Is(err, ea.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		entityActionFailure(w, err)
		return
	}
	if connectionError(w, err) {
		return
	}
	s.messageWriteResponse(w, r, access, http.StatusOK, item)
}

func (s *server) listConnectionRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	requestsPort := s.connections
	if nilConnectionRequestReader(s.access) || nilConnectionRequestReader(requestsPort) || r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	access, ok := s.messageWriteAccess(w, r)
	if !ok {
		return
	}
	if !messagePolicyNoBody(w, r) {
		messagePolicyFailure(w, mp.ErrInvalid)
		return
	}
	if nilConnectionRequestReader(s.access) || r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	requests, err := requestsPort.ListRequests(r.Context(), access.Actor.ID)
	if connectionError(w, err) {
		return
	}
	if nilConnectionRequestReader(s.access) || nilConnectionRequestReader(s.connections) || r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	// Original writer response: encode first, then recheck the same native
	// Person/Session after reads and waits. Never refresh idle or grant access.
	s.messageWriteResponse(w, r, access, http.StatusOK, requests)
}

// This guard is local to the Request list reader, not a new authorization port.
func nilConnectionRequestReader(port any) bool {
	v := reflect.ValueOf(port)
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func (s *server) decideConnectionRequest(w http.ResponseWriter, r *http.Request) {
	if nilConnectionRequestReader(s.access) || r.Context().Err()!=nil {messagePolicyFailure(w,mp.ErrUnavailable);return}
	access, ok := s.messageWriteAccess(w, r)
	if !ok {
		return
	}
	port, found := s.connections.(connection.CurrentStore)
	if !found {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	id := r.PathValue("requestID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_request_id")
		return
	}
	var input struct {
		Action string `json:"action"`
		OperationID json.RawMessage `json:"operationId,omitempty"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.Action != "accept" && input.Action != "decline" && input.Action != "withdraw" {
		respondError(w, http.StatusBadRequest, "invalid_decision")
		return
	}
	if input.OperationID != nil {
		var operationID string
		if json.Unmarshal(input.OperationID,&operationID)!=nil{respondError(w,400,"invalid_operation_id");return}
		if !connection.ValidDecisionID(operationID){respondError(w,400,"invalid_operation_id");return}
		operationPort,ok:=s.connections.(connection.DecisionOperationStore);if !ok||nilConnectionRequestReader(operationPort){messagePolicyFailure(w,mp.ErrUnavailable);return}
		receipt,e:=operationPort.DecideRequestOperation(r.Context(),access,id,input.Action,operationID)
		if connectionDecisionOperationError(w,e){return}
		if connection.ValidateDecisionReceipt(receipt,access.Actor.ID,id,operationID,input.Action)!=nil{messagePolicyFailure(w,mp.ErrUnavailable);return}
		s.connectionDecisionOperationResponse(w,r,access,receipt);return
	}
	request, err := port.DecideRequestCurrent(r.Context(), access, id, input.Action)
	if connectionError(w, err) {
		return
	}
	s.messageWriteResponse(w, r, access, http.StatusOK, request)
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

func (s *server) markConversationRead(w http.ResponseWriter, r *http.Request) {
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
		ThroughMessageID string `json:"throughMessageId"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !uuidPath.MatchString(input.ThroughMessageID) {
		respondError(w, http.StatusBadRequest, "invalid_message_id")
		return
	}
	state, err := s.connections.MarkRead(r.Context(), actor.ID, id, input.ThroughMessageID)
	if connectionError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": state})
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	access, ok := s.messageWriteAccess(w, r)
	if !ok {
		return
	}
	port, found := s.connections.(connection.CurrentStore)
	if !found {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	id := r.PathValue("conversationID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_conversation_id")
		return
	}
	var input struct {
		Body   string `json:"body"`
		Entity *struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"entity"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	input.Body = strings.TrimSpace(input.Body)
	if input.Entity != nil {
		if !uuidPath.MatchString(input.Entity.ID) || !validChatEntityType(input.Entity.Type) {
			respondError(w, http.StatusBadRequest, "invalid_message_entity")
			return
		}
		if input.Body == "" {
			input.Body = "分享了一张卡片"
		}
	}
	if len(input.Body) < 1 || len(input.Body) > 2000 {
		respondError(w, http.StatusBadRequest, "invalid_message")
		return
	}
	var kind, entity string
	if input.Entity != nil {
		kind = input.Entity.Type
		entity = input.Entity.ID
	}
	item, err := port.SendMessageCurrent(r.Context(), access, id, input.Body, kind, entity)

	if connectionError(w, err) {
		return
	}
	s.messageWriteResponse(w, r, access, http.StatusCreated, item)
}

func validChatEntityType(kind string) bool {
	switch kind {
	case "activity", "place", "person", "community", "organization", "business", "moment":
		return true
	}
	return false
}
