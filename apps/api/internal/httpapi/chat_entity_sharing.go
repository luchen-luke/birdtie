package httpapi

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"
)

func entityShareControlFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, connection.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(e, connection.ErrConflict):
		respondError(w, 409, "entity_share_changed")
	case errors.Is(e, connection.ErrRateLimit):
		respondError(w, 429, "message_rate_limited")
	default:
		humanControlFailure(w, e)
	}
}

// Original raw074 POST is closed. Historical GET/STOP are exact namespace
// bridges; they expose only the original own tiny fact, never a copied card.
func (s *server) shareHumanEntity(w http.ResponseWriter, r *http.Request) {
	_, _, _, ok := s.humanControlActor(w, r)
	if !ok {
		return
	}
	respondError(w, 409, "entity_share_control_required")
}
func (s *server) getHumanEntityShare(w http.ResponseWriter, r *http.Request) {
	s.historyEntityShareControl(w, r, connection.EntityShareRecipient{Type: "CONVERSATION", ID: r.PathValue("conversationID")}, false)
}
func (s *server) stopLegacyHumanEntityShare(w http.ResponseWriter, r *http.Request) {
	s.historyEntityShareControl(w, r, connection.EntityShareRecipient{Type: "CONVERSATION", ID: r.PathValue("conversationID")}, true)
}
func entityShareTarget(r *http.Request) connection.EntityShareRecipient {
	return connection.EntityShareRecipient{Type: r.PathValue("recipientType"), ID: r.PathValue("recipientID")}
}
func (s *server) entityShareControlGateway(w http.ResponseWriter, r *http.Request, target connection.EntityShareRecipient) (*http.Request, connection.EntityShareAccess, connection.EntityShareControlStore, bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	access := connection.EntityShareAccess{Actor: a, SessionDigest: d}
	if !ok {
		return r, access, nil, false
	}
	if !humanControlPath(w, r) || !connection.ValidEntityShareRecipient(target) {
		respondError(w, 400, "invalid_entity_share")
		return r, access, nil, false
	}
	port, ok := s.connections.(connection.EntityShareControlStore)
	if !ok || port == nil {
		respondError(w, 503, "entity_share_unavailable")
		return r, access, nil, false
	}
	return r, access, port, true
}
func (s *server) previewEntityShareControl(w http.ResponseWriter, r *http.Request) {
	target := entityShareTarget(r)
	r, a, port, ok := s.entityShareControlGateway(w, r, target)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := connection.DecodeEntityShareControl(raw, target, false)
	if e != nil {
		entityShareControlFailure(w, e)
		return
	}
	v, e := port.PreviewEntityShareControl(r.Context(), a, target, in)
	if e != nil {
		entityShareControlFailure(w, e)
		return
	}
	if v.OperationID != in.OperationID || v.Recipient != target || v.Entity != in.Entity || v.EntityTitle == "" || !identity.HumanControlUUID(v.RecipientAccountID) || v.RecipientAccountID == a.Actor.ID || len(v.SourceVersion) != 64 || v.PayloadDigest != connection.EntityShareControlDigest(target, in) || v.Approval == "" || !v.ObservedAt.Before(v.ExpiresAt) || v.ExpiresAt.Sub(v.ObservedAt) > 30*1000000000 {
		respondError(w, 503, "entity_share_unavailable")
		return
	}
	s.respondHumanControl(w, r, a.SessionDigest, a.Actor, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitEntityShareControl(w http.ResponseWriter, r *http.Request) {
	target := entityShareTarget(r)
	r, a, port, ok := s.entityShareControlGateway(w, r, target)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := connection.DecodeEntityShareControl(raw, target, true)
	if e != nil {
		entityShareControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_entity_share")
		return
	}
	v, e := port.CommitEntityShareControl(r.Context(), a, target, in)
	if e != nil {
		entityShareControlFailure(w, e)
		return
	}
	if !connection.ValidEntityShareControlReceipt(v, target, in.OperationID) || v.Status == "SUCCEEDED" && (v.Entity == nil || *v.Entity != in.Entity || v.PayloadDigest != connection.EntityShareControlDigest(target, in)) {
		respondError(w, 503, "entity_share_unavailable")
		return
	}
	s.respondHumanControl(w, r, a.SessionDigest, a.Actor, map[string]any{"data": v}, nil, false)
}
func (s *server) readEntityShareControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyEntityShareControl(w, r, entityShareTarget(r), false)
}
func (s *server) stopEntityShareControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyEntityShareControl(w, r, entityShareTarget(r), true)
}
func (s *server) historyEntityShareControl(w http.ResponseWriter, r *http.Request, target connection.EntityShareRecipient, stop bool) {
	r, a, port, ok := s.entityShareControlGateway(w, r, target)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	var v connection.EntityShareControlReceipt
	var e error
	if stop {
		v, e = port.StopEntityShareControlOperation(r.Context(), a, target, r.PathValue("operationID"))
	} else {
		v, e = port.ReadEntityShareControlOperation(r.Context(), a, target, r.PathValue("operationID"))
	}
	if e != nil {
		entityShareControlFailure(w, e)
		return
	}
	if !connection.ValidEntityShareControlReceipt(v, target, r.PathValue("operationID")) {
		respondError(w, 503, "entity_share_unavailable")
		return
	}
	s.respondHumanControl(w, r, a.SessionDigest, a.Actor, map[string]any{"data": v}, nil, false)
}
func (s *server) registerEntityShareControlRoutes(mux *http.ServeMux) {
	base := "/v1/me/entity-shares/{recipientType}/{recipientID}"
	mux.HandleFunc("POST "+base+"/previews", s.previewEntityShareControl)
	mux.HandleFunc("PUT "+base+"/operations/{operationID}", s.commitEntityShareControl)
	mux.HandleFunc("GET "+base+"/operations/{operationID}", s.readEntityShareControlOperation)
	mux.HandleFunc("POST "+base+"/operations/{operationID}/stop", s.stopEntityShareControlOperation)
	mux.HandleFunc("POST /v1/me/conversations/{conversationID}/entity-shares/{operationID}/stop", s.stopLegacyHumanEntityShare)
}
