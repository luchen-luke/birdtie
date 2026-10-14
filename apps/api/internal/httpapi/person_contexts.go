package httpapi

import (
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
)

func (s *server) contextActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", false
	}
	if s.contextDeclarations == nil {
		respondError(w, http.StatusServiceUnavailable, "contexts_unavailable")
		return "", false
	}
	return actor.ID, true
}

func (s *server) listOwnContexts(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.contextDeclarations.(contextgraph.DeclarationControlStore)
	if !ok {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadDeclarationControl(r.Context(), d, a)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v.Items, "version": v.Version}, v.ResponseProof, true)
}

func (s *server) declareOwnContext(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) removeOwnContext(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) readDeclarationControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.contextDeclarations.(contextgraph.DeclarationControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadDeclarationControl(r.Context(), d, a)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewDeclarationControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	port, ok := s.contextDeclarations.(contextgraph.DeclarationControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := contextgraph.DecodeDeclarationControl(raw, false)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	v, e := port.PreviewDeclarationControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, contextgraph.DeclarationControlDigest(in)) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitDeclarationControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	port, ok := s.contextDeclarations.(contextgraph.DeclarationControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := contextgraph.DecodeDeclarationControl(raw, true)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_human_control")
		return
	}
	v, e := port.CommitDeclarationControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "CONTEXT", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readDeclarationControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyDeclarationControl(w, r, false)
}
func (s *server) stopDeclarationControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyDeclarationControl(w, r, true)
}
func (s *server) historyDeclarationControl(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.contextDeclarations.(contextgraph.DeclarationControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	var v identity.HumanControlReceipt
	var e error
	if stop {
		v, e = port.StopDeclarationControlOperation(r.Context(), d, a, r.PathValue("operationID"))
	} else {
		v, e = port.ReadDeclarationControlOperation(r.Context(), d, a, r.PathValue("operationID"))
	}
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "CONTEXT", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) registerDeclarationControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/context-control", s.readDeclarationControl)
	mux.HandleFunc("POST /v1/me/context-control/previews", s.previewDeclarationControl)
	mux.HandleFunc("PUT /v1/me/context-control/operations/{operationID}", s.commitDeclarationControl)
	mux.HandleFunc("GET /v1/me/context-control/operations/{operationID}", s.readDeclarationControlOperation)
	mux.HandleFunc("POST /v1/me/context-control/operations/{operationID}/stop", s.stopDeclarationControlOperation)
}
