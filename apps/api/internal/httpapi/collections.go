package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) collectionGateway(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, content.HumanCollectionStore, bool) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return a, d, nil, false
	}
	g, ok := s.content.(content.HumanCollectionStore)
	if !ok || g == nil || (reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil()) {
		respondError(w, 503, "collection_unavailable")
		return a, d, nil, false
	}
	if !s.captureCollectionHTTP(w, r, d, a, false) {
		return a, d, nil, false
	}
	return a, d, g, true
}
func collectionResponse(w http.ResponseWriter, r *http.Request, code int, data any, err error) {
	if !privateImageResponseCurrent(w, r) {
		return
	}
	if err != nil {
		momentFailure(w, err)
		return
	}
	if code == 204 {
		w.WriteHeader(code)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": data})
	if e != nil {
		momentFailure(w, content.ErrUnavailable)
		return
	}
	if e = validateCollectionHTTP(r); e != nil {
		momentFailure(w, e)
		return
	}
	if e = validateCollectionReadProof(r.Context(), data); e != nil {
		momentFailure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) createPersonalCollection(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionGateway(w, r)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	var p content.CollectionCreate
	if !decodeStrictJSON(w, r, &p) {
		return
	}
	if !content.ValidCollectionID(p.OperationID) {
		respondError(w, 400, "invalid_collection_operation")
		return
	}
	var e error
	p.CollectionInput, e = content.NormalizeCollectionInput(p.CollectionInput)
	if e != nil {
		momentFailure(w, e)
		return
	}
	v, e := g.CreateHumanCollection(r.Context(), d, a, p)
	collectionResponse(w, r, 201, v, e)
}
func (s *server) listPersonalCollections(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionGateway(w, r)
	if !ok {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || len(q["kind"]) != 1 || !content.ValidCollectionKind(q.Get("kind")) || len(q) > 2 || (len(q) == 2 && (len(q["cursor"]) != 1 || !content.ValidCollectionID(q.Get("cursor")))) {
		respondError(w, 400, "invalid_collection_query")
		return
	}
	v, e := g.ListHumanCollections(r.Context(), d, a, q.Get("kind"), q.Get("cursor"))
	collectionResponse(w, r, 200, v, e)
}
func collectionPath(w http.ResponseWriter, r *http.Request) bool {
	if !content.ValidCollectionKind(r.PathValue("kind")) || !content.ValidCollectionID(r.PathValue("collectionID")) {
		respondError(w, 400, "invalid_collection_id")
		return false
	}
	return true
}
func (s *server) getPersonalCollection(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionGateway(w, r)
	if !ok || !momentNoQuery(w, r) || !collectionPath(w, r) {
		return
	}
	v, e := g.GetHumanCollection(r.Context(), d, a, r.PathValue("kind"), r.PathValue("collectionID"))
	collectionResponse(w, r, 200, v, e)
}
func (s *server) getPersonalCollectionOperation(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionGateway(w, r)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	if !content.ValidCollectionID(r.PathValue("operationID")) {
		respondError(w, 400, "invalid_collection_operation")
		return
	}
	v, e := g.GetHumanCollectionOperation(r.Context(), d, a, r.PathValue("operationID"))
	collectionResponse(w, r, 200, v, e)
}
func (s *server) updatePersonalCollection(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}
func (s *server) withdrawPersonalCollection(w http.ResponseWriter, r *http.Request) {
	s.legacyHumanControlWrite(w, r)
}

// Registered by the shared server constructor; worker owns this route contract.
func (s *server) registerPersonalCollectionRoutes(mux *http.ServeMux) {
	s.registerCollectionControlRoutes(mux)
	s.registerCollectionPublicationRoutes(mux)
	mux.HandleFunc("POST /v1/me/personal-collections/{kind}/{collectionID}/append-places", s.appendPersonalCollectionPlaces)
	mux.HandleFunc("GET /v1/me/personal-collections/{kind}/{collectionID}/append-operations/{operationID}", s.getPersonalCollectionAppendOperation)
	mux.HandleFunc("POST /v1/me/personal-collections/{kind}/{collectionID}/append-operations/{operationID}/stop", s.stopPersonalCollectionAppendOperation)
	mux.HandleFunc("POST /v1/me/personal-collections", s.createPersonalCollection)
	mux.HandleFunc("GET /v1/me/personal-collections", s.listPersonalCollections)
	mux.HandleFunc("GET /v1/me/personal-collections/operations/{operationID}", s.getPersonalCollectionOperation)
	mux.HandleFunc("GET /v1/me/personal-collections/{kind}/{collectionID}", s.getPersonalCollection)
	mux.HandleFunc("PUT /v1/me/personal-collections/{kind}/{collectionID}", s.updatePersonalCollection)
	mux.HandleFunc("DELETE /v1/me/personal-collections/{kind}/{collectionID}", s.withdrawPersonalCollection)
}
