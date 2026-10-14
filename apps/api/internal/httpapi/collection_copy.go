package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type collectionHTTPFrame struct {
	store  content.CollectionCopySessionStore
	actor  identity.Actor
	digest [32]byte
	frame  content.CollectionSessionFrame
}
type collectionHTTPFrameKey struct{}

func (s *server) captureCollectionHTTP(w http.ResponseWriter, r *http.Request, d [32]byte, a identity.Actor, required bool) bool {
	g, ok := s.content.(content.CollectionCopySessionStore)
	if !ok {
		if required {
			momentFailure(w, content.ErrUnavailable)
			return false
		}
		return true
	}
	if prior, ok := r.Context().Value(collectionHTTPFrameKey{}).(collectionHTTPFrame); ok {
		if prior.actor != a || prior.digest != d {
			momentFailure(w, content.ErrConflict)
			return false
		}
		return true
	}
	frame, valid, present := content.OriginalCollectionSession(r.Context(), d, a)
	var e error
	if present {
		if !valid {
			momentFailure(w, content.ErrConflict)
			return false
		}
		e = g.ValidateCollectionSession(r.Context(), d, a, frame)
	} else {
		frame, e = g.CaptureCollectionSession(r.Context(), d, a)
	}
	if e != nil {
		momentFailure(w, e)
		return false
	}
	ctx := content.WithCollectionSession(r.Context(), d, a, frame)
	ctx = context.WithValue(ctx, collectionHTTPFrameKey{}, collectionHTTPFrame{g, a, d, frame})
	*r = *r.WithContext(ctx)
	return true
}
func validateCollectionHTTP(r *http.Request) error {
	if f, ok := r.Context().Value(collectionHTTPFrameKey{}).(collectionHTTPFrame); ok {
		return f.store.ValidateCollectionSession(r.Context(), f.digest, f.actor, f.frame)
	}
	return nil
}
func validateCollectionReadProof(ctx context.Context, v any) error {
	switch x := v.(type) {
	case content.PersonalCollection:
		if x.ResponseProof != nil {
			return x.ResponseProof.RevalidateCollection(ctx)
		}
	case content.CollectionPage:
		for _, row := range x.Items {
			if e := validateCollectionReadProof(ctx, row); e != nil {
				return e
			}
		}
	case content.CollectionCopyPreview:
		if x.ResponseProof == nil {
			return content.ErrUnavailable
		}
		return x.ResponseProof.RevalidateCollection(ctx)
	}
	return nil
}
func (s *server) registerCollectionCopyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/public-collections/{kind}/{collectionID}/copy/preview", s.previewCollectionCopy)
	mux.HandleFunc("PUT /v1/public-collections/{kind}/{collectionID}/copy", s.commitCollectionCopy)
	mux.HandleFunc("GET /v1/me/personal-collections/copy-operations/{operationID}/receipt", s.getCollectionCopyReceipt)
	mux.HandleFunc("POST /v1/me/personal-collections/copy-operations/{operationID}/stop", s.stopCollectionCopy)
}
func (s *server) collectionCopyGateway(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, content.CollectionCopyStore, bool) {
	a, d, ok := s.momentActor(w, r)
	if !ok || !momentNoQuery(w, r) {
		return a, d, nil, false
	}
	g, ok := s.content.(content.CollectionCopyStore)
	if !ok || g == nil {
		momentFailure(w, content.ErrUnavailable)
		return a, d, nil, false
	}
	if !s.captureCollectionHTTP(w, r, d, a, true) {
		return a, d, nil, false
	}
	return a, d, g, true
}
func collectionCopyBody(w http.ResponseWriter, r *http.Request, commit bool) (content.CollectionRef, content.CollectionCopyInput, bool) {
	ref := content.CollectionRef{Kind: r.PathValue("kind"), ID: strings.ToLower(r.PathValue("collectionID"))}
	if !content.ValidCollectionRef(ref) {
		momentFailure(w, content.ErrInvalid)
		return ref, content.CollectionCopyInput{}, false
	}
	kind, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || kind != "application/json" {
		respondError(w, 415, "json_required")
		return ref, content.CollectionCopyInput{}, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil {
		momentFailure(w, content.ErrInvalid)
		return ref, content.CollectionCopyInput{}, false
	}
	in, e := content.DecodeCollectionCopy(raw, ref, commit)
	if e != nil {
		momentFailure(w, e)
		return ref, in, false
	}
	return ref, in, true
}
func respondCollectionCopy(w http.ResponseWriter, r *http.Request, v any, e error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if e != nil {
		collectionCopyFailure(w, e)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		collectionCopyFailure(w, content.ErrUnavailable)
		return
	}
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if e = validateCollectionHTTP(r); e != nil {
		collectionCopyFailure(w, e)
		return
	}
	if e = validateCollectionReadProof(r.Context(), v); e != nil {
		collectionCopyFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func collectionCopyFailure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, content.ErrConflict):
		respondError(w, 409, "collection_copy_changed")
	case errors.Is(e, content.ErrNotFound):
		respondError(w, 404, "collection_copy_not_found")
	default:
		momentFailure(w, e)
	}
}
func (s *server) previewCollectionCopy(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionCopyGateway(w, r)
	if !ok {
		return
	}
	ref, in, ok := collectionCopyBody(w, r, false)
	if !ok {
		return
	}
	out, e := g.PreviewHumanCollectionCopy(r.Context(), d, a, ref, in)
	respondCollectionCopy(w, r, out, e)
}
func (s *server) commitCollectionCopy(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionCopyGateway(w, r)
	if !ok {
		return
	}
	ref, in, ok := collectionCopyBody(w, r, true)
	if !ok {
		return
	}
	out, e := g.CopyHumanCollection(r.Context(), d, a, ref, in)
	respondCollectionCopy(w, r, out, e)
}
func (s *server) collectionCopyHistory(w http.ResponseWriter, r *http.Request, stop bool) {
	a, d, g, ok := s.collectionCopyGateway(w, r)
	if !ok {
		return
	}
	op := strings.ToLower(r.PathValue("operationID"))
	if !content.ValidCollectionID(op) {
		momentFailure(w, content.ErrInvalid)
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) > 0 {
			momentFailure(w, content.ErrInvalid)
			return
		}
	}
	var out content.CollectionCopyReceipt
	var e error
	if stop {
		out, e = g.StopHumanCollectionCopyOperation(r.Context(), d, a, op)
	} else {
		out, e = g.GetHumanCollectionCopyOperation(r.Context(), d, a, op)
	}
	respondCollectionCopy(w, r, out, e)
}
func (s *server) getCollectionCopyReceipt(w http.ResponseWriter, r *http.Request) {
	s.collectionCopyHistory(w, r, false)
}
func (s *server) stopCollectionCopy(w http.ResponseWriter, r *http.Request) {
	s.collectionCopyHistory(w, r, true)
}
