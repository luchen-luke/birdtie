package httpapi

import (
	"encoding/json"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"net/http"
	"reflect"
	"strings"
)

func messageOperationMissing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	return r.Kind() == reflect.Pointer && r.IsNil()
}
func (s *server) messageOperationAccess(w http.ResponseWriter, r *http.Request) (ea.Access, connection.HumanMessageOperationStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := s.connections.(connection.HumanMessageOperationStore)
	if !ok || messageOperationMissing(p) || messageOperationMissing(s.access) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, false
	}
	a, ok := s.messageWriteAccess(w, r)
	if !ok {
		return a, nil, false
	}
	if !uuidPath.MatchString(r.PathValue("conversationID")) || r.PathValue("conversationID") != strings.ToLower(r.PathValue("conversationID")) {
		respondError(w, 400, "invalid_conversation_id")
		return a, nil, false
	}
	return a, p, true
}
func (s *server) sendHumanMessageOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.messageOperationAccess(w, r)
	if !ok {
		return
	}
	if r.Body == nil {
		respondError(w, 400, "invalid_message_operation")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil {
		respondError(w, 400, "invalid_message_operation")
		return
	}
	in, e := connection.DecodeMessageOperation(raw)
	if e != nil {
		respondError(w, 400, "invalid_message_operation")
		return
	}
	out, e := p.SendHumanMessageOperation(r.Context(), a, r.PathValue("conversationID"), in)
	if messageOperationError(w, e) {
		return
	}
	if out.PayloadDigest != connection.HumanMessagePayloadDigest(r.PathValue("conversationID"), in.Body) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.messageOperationResponse(w, r, a, p, out, in.OperationID, 201)
}
func (s *server) readHumanMessageOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.messageOperationAccess(w, r)
	if !ok {
		return
	}
	if !uuidPath.MatchString(r.PathValue("operationID")) || r.PathValue("operationID") != strings.ToLower(r.PathValue("operationID")) {
		respondError(w, 400, "invalid_message_operation")
		return
	}
	if !messagePolicyNoBody(w, r) {
		return
	}
	out, e := p.ReadHumanMessageOperation(r.Context(), a, r.PathValue("conversationID"), r.PathValue("operationID"))
	if messageOperationError(w, e) {
		return
	}
	s.messageOperationResponse(w, r, a, p, out, r.PathValue("operationID"), 200)
}
func messageOperationError(w http.ResponseWriter, e error) bool {
	if e == nil {
		return false
	}
	for _, known := range []error{connection.ErrNotFound, connection.ErrConflict, connection.ErrForbidden, connection.ErrRateLimit, mp.ErrInvalid, mp.ErrDenied, mp.ErrChanged, mp.ErrUnavailable} {
		if errors.Is(e, known) {
			return connectionError(w, e)
		}
	}
	if errors.Is(e, identity.ErrUnauthorized) {
		return connectionError(w, e)
	}
	messagePolicyFailure(w, mp.ErrUnavailable)
	return true
}
func (s *server) messageOperationResponse(w http.ResponseWriter, r *http.Request, a ea.Access, p connection.HumanMessageOperationStore, out connection.MessageOperationReceipt, op string, status int) {
	if !connection.ValidMessageOperationReceipt(out, a.Actor.ID, r.PathValue("conversationID"), op) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw) > 4096 {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	// No later Session-only database wait may supersede this complete source gate.
	if e = p.ValidateHumanMessageOperationResponse(r.Context(), a, out); messageOperationError(w, e) {
		return
	}
	if r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
