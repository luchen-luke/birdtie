package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"reflect"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) registerCollectionPublicationRoutes(mux *http.ServeMux) {
	s.registerCollectionCopyRoutes(mux)
	if content.JourneysEnabled {
		mux.HandleFunc("GET /v1/me/journeys/discovery", s.listJourneyDiscovery)
	}
	mux.HandleFunc("POST /v1/me/personal-collections/{kind}/{collectionID}/publication", s.publishPersonalCollection)
	mux.HandleFunc("POST /v1/me/personal-collections/{kind}/{collectionID}/unpublish", s.unpublishPersonalCollection)
	mux.HandleFunc("GET /v1/me/personal-collections/publication-operations/{operationID}/receipt", s.getCollectionPublicationReceipt)
	mux.HandleFunc("POST /v1/me/personal-collections/publication-operations/{operationID}/close", s.closeCollectionPublicationOperation)
	mux.HandleFunc("GET /v1/public-collections/{kind}/{collectionID}", s.getPublicCollection)
}
func (s *server) collectionPublicationGateway(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, content.CollectionPublicationStore, bool) {
	a, d, ok := s.momentActor(w, r)
	if !ok {
		return a, d, nil, false
	}
	g, ok := s.content.(content.CollectionPublicationStore)
	if !ok || g == nil || (reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil()) {
		respondError(w, 503, "collection_publication_unavailable")
		return a, d, nil, false
	}
	if !s.captureCollectionHTTP(w, r, d, a, false) {
		return a, d, nil, false
	}
	return a, d, g, true
}
func (s *server) publishPersonalCollection(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionPublicationGateway(w, r)
	if !ok || !momentNoQuery(w, r) || !collectionPath(w, r) {
		return
	}
	if r.PathValue("kind") == "journey" && !content.JourneysEnabled {
		respondError(w, http.StatusServiceUnavailable, "journey_unavailable")
		return
	}
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		respondError(w, 415, "json_required")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 131073))
	if e != nil {
		momentFailure(w, content.ErrInvalid)
		return
	}
	in, e := content.DecodeJourneyOrCollectionPublication(raw)
	if e != nil {
		momentFailure(w, e)
		return
	}
	v, e := g.PublishHumanCollection(r.Context(), d, a, r.PathValue("kind"), strings.ToLower(r.PathValue("collectionID")), in)
	collectionResponse(w, r, 200, v, e)
}
func (s *server) unpublishPersonalCollection(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionPublicationGateway(w, r)
	if !ok || !momentNoQuery(w, r) || !collectionPath(w, r) {
		return
	}
	var in content.CollectionUnpublishInput
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	if !content.ValidCollectionID(in.OperationID) || in.Revision < 1 || in.Revision >= math.MaxInt64 {
		momentFailure(w, content.ErrInvalid)
		return
	}
	in.OperationID = strings.ToLower(in.OperationID)
	v, e := g.UnpublishHumanCollection(r.Context(), d, a, r.PathValue("kind"), strings.ToLower(r.PathValue("collectionID")), in)
	collectionResponse(w, r, 200, v, e)
}
func (s *server) getCollectionPublicationReceipt(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionPublicationGateway(w, r)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !content.ValidCollectionID(op) {
		momentFailure(w, content.ErrInvalid)
		return
	}
	v, e := g.GetHumanCollectionPublicationOperation(r.Context(), d, a, strings.ToLower(op))
	collectionResponse(w, r, 200, v, e)
}
func (s *server) closeCollectionPublicationOperation(w http.ResponseWriter, r *http.Request) {
	a, d, g, ok := s.collectionPublicationGateway(w, r)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	op := r.PathValue("operationID")
	if !content.ValidCollectionID(op) {
		momentFailure(w, content.ErrInvalid)
		return
	}
	mediaType, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 4097))
	if e != nil {
		momentFailure(w, content.ErrInvalid)
		return
	}
	in, e := content.DecodeCollectionPublicationClose(raw)
	if e != nil {
		momentFailure(w, e)
		return
	}
	v, e := g.CloseHumanCollectionPublicationOperation(r.Context(), d, a, strings.ToLower(op), in)
	collectionResponse(w, r, 200, v, e)
}
func publicCollectionFailure(w http.ResponseWriter, e error) {
	code, status, message := "public_collection_unavailable", 503, "公开收藏暂不可用，请重新读取"
	switch {
	case errors.Is(e, content.ErrInvalid):
		code, status, message = "invalid_public_collection", 400, "公开收藏请求无效"
	case errors.Is(e, identity.ErrUnauthorized):
		code, status, message = "unauthorized", 401, "登录已失效，请重新登录"
	case errors.Is(e, content.ErrNotFound):
		code, status, message = "not_found", 404, "收藏当前不可公开查看"
	}
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func (s *server) getPublicCollection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || !content.ValidCollectionKind(r.PathValue("kind")) || !content.ValidCollectionID(r.PathValue("collectionID")) {
		publicCollectionFailure(w, content.ErrInvalid)
		return
	}
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) != 0 {
			publicCollectionFailure(w, content.ErrInvalid)
			return
		}
	}
	var access content.PublicMomentAccess
	if len(r.Header.Values("Authorization")) > 0 {
		if s.access == nil {
			publicCollectionFailure(w, content.ErrUnavailable)
			return
		}
		a, d, e := s.actor(r, true)
		if e != nil || a.AccountType != "person" {
			publicCollectionFailure(w, identity.ErrUnauthorized)
			return
		}
		access = content.PublicMomentAccess{Actor: a, SessionDigest: d}
		if !s.captureCollectionHTTP(w, r, d, a, false) {
			return
		}
	}
	g, ok := s.content.(content.CollectionPublicationStore)
	if !ok || g == nil || (reflect.ValueOf(g).Kind() == reflect.Pointer && reflect.ValueOf(g).IsNil()) {
		publicCollectionFailure(w, content.ErrUnavailable)
		return
	}
	kind, id := r.PathValue("kind"), strings.ToLower(r.PathValue("collectionID"))
	initial, e := g.GetPublicCollection(r.Context(), access, kind, id)
	if e != nil {
		publicCollectionFailure(w, e)
		return
	}
	if content.ValidatePublicCollection(initial, kind, id) != nil {
		publicCollectionFailure(w, content.ErrUnavailable)
		return
	}
	// Copy exactly the encoded payload, including nested coordinates, before a
	// possibly waiting Authenticate. Native proof remains private and immutable.
	data, e := json.Marshal(initial)
	if e != nil {
		publicCollectionFailure(w, content.ErrUnavailable)
		return
	}
	var frozen content.PublicCollection
	if json.Unmarshal(data, &frozen) != nil {
		publicCollectionFailure(w, content.ErrUnavailable)
		return
	}
	frozen.SourceBinding, frozen.ResponseProof = initial.SourceBinding, initial.ResponseProof
	encoded := append([]byte(`{"data":`), data...)
	encoded = append(encoded, '}', '\n')
	if access.Actor.ID != "" {
		a, d, e := s.actor(r, true)
		if e != nil {
			publicCollectionFailure(w, e)
			return
		}
		if a.ID != access.Actor.ID || a.AccountType != access.Actor.AccountType || d != access.SessionDigest {
			publicCollectionFailure(w, content.ErrUnavailable)
			return
		}
	}
	// No authentication waits or re-encoding follow this current native fence.
	if e = validateCollectionHTTP(r); e != nil {
		publicCollectionFailure(w, e)
		return
	}
	if e = g.RevalidatePublicCollection(r.Context(), access, frozen); e != nil {
		publicCollectionFailure(w, e)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(encoded)
}
