package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"mime"
	"net/http"
	"reflect"
)

func (s *server) collectionAppendGateway(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, content.HumanCollectionAppendStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return a, d, nil, false
	}
	g, ok := s.content.(content.HumanCollectionAppendStore)
	if !ok || g == nil || reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil() {
		respondError(w, 503, "collection_append_unavailable")
		return a, d, nil, false
	}
	return a, d, g, true
}
func (s *server) collectionAppendResponse(w http.ResponseWriter, r *http.Request, a identity.Actor, d [32]byte, g content.HumanCollectionAppendStore, expectedOp string, v content.CollectionAppendReceipt, e error) {
	if e != nil {
		momentFailure(w, e)
		return
	}
	kind, id := r.PathValue("kind"), r.PathValue("collectionID")
	if !v.Valid(kind, id, expectedOp) {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": v})
	if e != nil {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	current, e := g.GetHumanCollectionAppendOperation(r.Context(), d, a, kind, id, v.OperationID)
	if e != nil {
		momentFailure(w, e)
		return
	}
	if current != v {
		momentFailure(w, content.ErrConflict)
		return
	}
	gate, ok := s.access.(interface {
		ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
	})
	if !ok || nilConnectionRequestReader(gate) {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	if e = gate.ValidateHumanSocialResponse(r.Context(), d, a); e != nil {
		momentFailure(w, e)
		return
	}
	if !privateImageResponseCurrent(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) appendPersonalCollectionPlaces(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionAppendGateway(w, r)
	if !ok || !momentNoQuery(w, r) || !collectionPath(w, r) {
		return
	}
	typeName, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typeName != "application/json" {
		respondError(w, 415, "json_required")
		return
	}
	if r.Body == nil {
		momentFailure(w, content.ErrInvalid)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil {
		momentFailure(w, content.ErrInvalid)
		return
	}
	in, e := content.DecodeCollectionAppend(raw)
	if e != nil {
		momentFailure(w, e)
		return
	}
	v, e := g.AppendHumanCollectionPlaces(r.Context(), d, a, r.PathValue("kind"), r.PathValue("collectionID"), in)
	s.collectionAppendResponse(w, r, a, d, g, in.OperationID, v, e)
}
func (s *server) getPersonalCollectionAppendOperation(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionAppendGateway(w, r)
	if !ok || !momentNoQuery(w, r) || !collectionPath(w, r) {
		return
	}
	if !content.ValidCollectionID(r.PathValue("operationID")) || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		momentFailure(w, content.ErrInvalid)
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			momentFailure(w, content.ErrInvalid)
			return
		}
	}
	v, e := g.GetHumanCollectionAppendOperation(r.Context(), d, a, r.PathValue("kind"), r.PathValue("collectionID"), r.PathValue("operationID"))
	s.collectionAppendResponse(w, r, a, d, g, r.PathValue("operationID"), v, e)
}
func (s *server) stopPersonalCollectionAppendOperation(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionAppendGateway(w, r)
	if !ok || !momentNoQuery(w, r) || !collectionPath(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !content.ValidCollectionID(op) {
		momentFailure(w, content.ErrInvalid)
		return
	}
	typeName, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || typeName != "application/json" {
		respondError(w, 415, "json_required")
		return
	}
	if r.Body == nil {
		momentFailure(w, content.ErrInvalid)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 1025))
	if e != nil {
		momentFailure(w, content.ErrInvalid)
		return
	}
	in, e := content.DecodeCollectionAppendStop(raw)
	if e != nil {
		momentFailure(w, e)
		return
	}
	v, e := g.StopHumanCollectionAppendOperation(r.Context(), d, a, r.PathValue("kind"), r.PathValue("collectionID"), op, in)
	s.collectionAppendResponse(w, r, a, d, g, op, v, e)
}
