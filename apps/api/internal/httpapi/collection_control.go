package httpapi

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) collectionControlGateway(w http.ResponseWriter, r *http.Request) (*http.Request, identity.Actor, [32]byte, content.HumanCollectionControlStore, bool) {
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok {
		return r, a, d, nil, false
	}
	port, ok := s.content.(content.HumanCollectionControlStore)
	if !ok || port == nil || (reflect.ValueOf(port).Kind() == reflect.Pointer && reflect.ValueOf(port).IsNil()) {
		respondError(w, 503, "collection_control_unavailable")
		return r, a, d, nil, false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "invalid_collection_control")
		return r, a, d, nil, false
	}
	return r, a, d, port, true
}

func collectionControlBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, content.MaxCollectionControlRequestBytes))
	if e != nil || len(raw) == 0 {
		respondError(w, 400, "invalid_collection_control")
		return nil, false
	}
	return raw, true
}

func collectionControlFailure(w http.ResponseWriter, e error) {
	// Preserve existing ordinary collection status semantics without exposing
	// private SQL, DSNs, title/body, authority tokens or a guessed zero effect.
	if !errors.Is(e, identity.ErrUnauthorized) && !errors.Is(e, identity.ErrControlChanged) && !errors.Is(e, identity.ErrControlInvalid) && !errors.Is(e, identity.ErrControlUnavailable) && !errors.Is(e, content.ErrNotFound) && !errors.Is(e, content.ErrConflict) && !errors.Is(e, content.ErrInvalid) && !errors.Is(e, content.ErrUnavailable) {
		e = content.ErrUnavailable
	}
	momentFailure(w, e)
}

func (s *server) previewCollectionControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, p, ok := s.collectionControlGateway(w, r)
	if !ok {
		return
	}
	raw, ok := collectionControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeCollectionControlInput(raw)
	if e != nil {
		respondError(w, 400, "invalid_collection_control")
		return
	}
	v, e := p.PreviewHumanCollectionControl(r.Context(), d, a, in)
	if e != nil {
		collectionControlFailure(w, e)
		return
	}
	if content.ValidateCollectionControlPreview(v, in, time.Now().UTC()) != nil || v.ResponseProof == nil {
		respondError(w, 503, "collection_control_unavailable")
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}

func (s *server) applyCollectionControl(w http.ResponseWriter, r *http.Request) {
	r, a, d, p, ok := s.collectionControlGateway(w, r)
	if !ok || !humanControlPath(w, r, "operationID") {
		return
	}
	raw, ok := collectionControlBody(w, r)
	if !ok {
		return
	}
	in, e := content.DecodeCollectionControlSubmission(raw)
	if e != nil || in.OperationID != r.PathValue("operationID") {
		respondError(w, 400, "invalid_collection_control")
		return
	}
	v, e := p.ApplyHumanCollectionControl(r.Context(), d, a, in)
	if e != nil {
		collectionControlFailure(w, e)
		return
	}
	if !content.ValidCollectionControlReceipt(v, in.OperationID) || v.ResponseProof == nil {
		respondError(w, 503, "collection_control_unavailable")
		return
	}
	if v.Status == "SUCCEEDED" && (v.Action != in.Action || v.Kind != in.Kind || v.CollectionID != in.CollectionID || v.ExpectedRevision == nil || *v.ExpectedRevision != in.ExpectedRevision || v.PayloadDigest != content.CollectionControlDigest(in.CollectionControlInput)) {
		respondError(w, 503, "collection_control_unavailable")
		return
	}
	if e = p.FinalizeHumanCollectionControlReceipt(r.Context(), d, a, v); e != nil {
		collectionControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}

func (s *server) collectionControlHistory(w http.ResponseWriter, r *http.Request, stop bool) {
	r, a, d, p, ok := s.collectionControlGateway(w, r)
	if !ok || !humanControlPath(w, r, "operationID") || !humanControlNoBody(w, r) {
		return
	}
	op := r.PathValue("operationID")
	var v content.CollectionControlReceipt
	var e error
	if stop {
		v, e = p.StopHumanCollectionControl(r.Context(), d, a, op)
	} else {
		v, e = p.ReadHumanCollectionControl(r.Context(), d, a, op)
	}
	if e != nil {
		collectionControlFailure(w, e)
		return
	}
	if !content.ValidCollectionControlReceipt(v, op) || v.ResponseProof == nil {
		respondError(w, 503, "collection_control_unavailable")
		return
	}
	if e = p.FinalizeHumanCollectionControlReceipt(r.Context(), d, a, v); e != nil {
		collectionControlFailure(w, e)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}

func (s *server) registerCollectionControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/me/personal-collection-controls/preview", s.previewCollectionControl)
	mux.HandleFunc("PUT /v1/me/personal-collection-controls/operations/{operationID}", s.applyCollectionControl)
	mux.HandleFunc("GET /v1/me/personal-collection-controls/operations/{operationID}", func(w http.ResponseWriter, r *http.Request) { s.collectionControlHistory(w, r, false) })
	mux.HandleFunc("POST /v1/me/personal-collection-controls/operations/{operationID}/stop", func(w http.ResponseWriter, r *http.Request) { s.collectionControlHistory(w, r, true) })
}
