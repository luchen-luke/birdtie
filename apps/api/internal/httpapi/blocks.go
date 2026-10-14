package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) listBlocks(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) || !humanControlNoBody(w, r) {
		return
	}
	if s.access == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	items, e := s.access.ListBlocks(r.Context(), a.ID)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	original, _ := json.Marshal(items)
	proof := humanControlFunctionProof(func(ctx context.Context) error {
		current, e := s.access.ListBlocks(ctx, a.ID)
		if e != nil {
			return identity.ErrControlUnavailable
		}
		raw, e := json.Marshal(current)
		if e != nil || string(raw) != string(original) {
			return identity.ErrControlChanged
		}
		return nil
	})
	s.respondHumanControl(w, r, d, a, map[string]any{"data": items}, proof, true)
}

func (s *server) blockAccount(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) unblockAccount(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

func (s *server) readBlockControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "accountID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.access.(identity.BlockControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	v, e := port.ReadBlockControl(r.Context(), d, a, r.PathValue("accountID"))
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) previewBlockControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r) {
		return
	}
	port, ok := s.access.(identity.BlockControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := identity.DecodeBlockControl(raw, false)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	v, e := port.PreviewBlockControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlPreviewMatches(v.HumanControlPreview, in.OperationID, in.Action, identity.BlockControlDigest(in)) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
func (s *server) commitBlockControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	port, ok := s.access.(identity.BlockControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	in, e := identity.DecodeBlockControl(raw, true)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_human_control")
		return
	}
	v, e := port.CommitBlockControl(r.Context(), d, a, in)
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "BLOCK", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) readBlockControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyBlockControl(w, r, false)
}
func (s *server) stopBlockControlOperation(w http.ResponseWriter, r *http.Request) {
	s.historyBlockControl(w, r, true)
}
func (s *server) historyBlockControl(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	port, ok := s.access.(identity.BlockControlStore)
	if !ok || port == nil {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	var v identity.HumanControlReceipt
	var e error
	if stop {
		v, e = port.StopBlockOperation(r.Context(), d, a, r.PathValue("operationID"))
	} else {
		v, e = port.ReadBlockOperation(r.Context(), d, a, r.PathValue("operationID"))
	}
	if e != nil {
		humanControlFailure(w, e)
		return
	}
	if !humanControlReceiptMatches(v, "BLOCK", r.PathValue("operationID")) {
		respondError(w, 503, "human_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, nil, false)
}
func (s *server) registerBlockControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/block-control/{accountID}", s.readBlockControl)
	mux.HandleFunc("POST /v1/me/block-control/previews", s.previewBlockControl)
	mux.HandleFunc("PUT /v1/me/block-control/operations/{operationID}", s.commitBlockControl)
	mux.HandleFunc("GET /v1/me/block-control/operations/{operationID}", s.readBlockControlOperation)
	mux.HandleFunc("POST /v1/me/block-control/operations/{operationID}/stop", s.stopBlockControlOperation)
}
