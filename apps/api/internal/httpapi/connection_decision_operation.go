package httpapi

import (
	"encoding/json"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"net/http"
)

func connectionDecisionOperationError(w http.ResponseWriter, e error) bool {
	if errors.Is(e, connection.ErrOperationChanged) {
		respondError(w, 409, "connection_operation_changed")
		return true
	}
	if errors.Is(e, connection.ErrOperationUnavailable) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return true
	}
	return connectionError(w, e)
}
func (s *server) getConnectionDecisionOperation(w http.ResponseWriter, r *http.Request) {
	if nilConnectionRequestReader(s.access) || r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	a, ok := s.messageWriteAccess(w, r)
	if !ok || !messagePolicyNoBody(w, r) {
		return
	}
	p, ok := s.connections.(connection.DecisionOperationStore)
	if !ok || nilConnectionRequestReader(p) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	id, op := r.PathValue("requestID"), r.PathValue("operationID")
	if !connection.ValidDecisionID(id) || !connection.ValidDecisionID(op) {
		respondError(w, 400, "invalid_operation_id")
		return
	}
	v, e := p.ReadRequestDecisionOperation(r.Context(), a, id, op)
	if connectionDecisionOperationError(w, e) {
		return
	}
	if connection.ValidateDecisionReceipt(v, a.Actor.ID, id, op, "") != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.connectionDecisionOperationResponse(w, r, a, v)
}

// Only keyed original Request decisions use this receipt-specific boundary.
// Marshal first, then recheck the captured Session and current original read
// authority. A late refusal does not roll back an already committed decision.
func (s *server) connectionDecisionOperationResponse(w http.ResponseWriter, r *http.Request, a ea.Access, receipt connection.DecisionOperationReceipt) {
	raw, err := json.Marshal(map[string]any{"data": receipt})
	if err != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	session, ok := s.access.(socialnow.HumanSessionStore)
	if !ok || nilConnectionRequestReader(session) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	if err = session.ValidateHumanSocialResponse(r.Context(), a.SessionDigest, a.Actor); err != nil {
		messagePolicyFailure(w, err)
		return
	}
	port, ok := s.connections.(connection.DecisionOperationResponseStore)
	if !ok || nilConnectionRequestReader(port) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	if err = port.ValidateRequestDecisionOperationResponse(r.Context(), a, receipt); connectionDecisionOperationError(w, err) {
		return
	}
	if r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
