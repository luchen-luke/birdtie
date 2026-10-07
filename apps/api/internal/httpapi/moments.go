package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) momentActor(w http.ResponseWriter, r *http.Request) (identity.Actor, [32]byte, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if s.access == nil {
		respondError(w, 503, "moment_unavailable")
		return identity.Actor{}, [32]byte{}, false
	}
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return identity.Actor{}, [32]byte{}, false
	}
	if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, 403, "person_account_required")
		return identity.Actor{}, [32]byte{}, false
	}
	return actor, digest, true
}
func (s *server) humanMomentGateway(w http.ResponseWriter) (content.HumanMomentStore, bool) {
	store, ok := s.content.(content.HumanMomentStore)
	if !ok || store == nil {
		respondError(w, 503, "moment_unavailable")
		return nil, false
	}
	return store, true
}
func validMomentInput(input *content.MomentInput) bool {
	normalized, err := content.NormalizeMomentInput(*input)
	if err != nil {
		return false
	}
	if normalized.OccurredAt != nil && normalized.OccurredAt.After(time.Now().Add(24*time.Hour)) {
		return false
	}
	*input = normalized
	return true
}
func momentFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		authFailed(w, err)
	case errors.Is(err, content.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(err, content.ErrConflict):
		respondError(w, 409, "draft_conflict")
	case errors.Is(err, content.ErrInvalid):
		respondError(w, 400, "invalid_moment_draft")
	case errors.Is(err, content.ErrUnavailable):
		respondError(w, 503, "moment_unavailable")
	default:
		serverError(w, err)
	}
}
func momentNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "invalid_moment_query")
		return false
	}
	return true
}
func (s *server) createMomentDraft(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	store, ok := s.humanMomentGateway(w)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	var input content.MomentInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validMomentInput(&input) {
		respondError(w, 400, "invalid_moment_draft")
		return
	}
	m, err := store.CreateHumanMomentDraft(r.Context(), digest, actor, input)
	if err != nil {
		momentFailure(w, err)
	} else {
		respond(w, 201, map[string]any{"data": m})
	}
}
func (s *server) listOwnMoments(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	store, ok := s.humanMomentGateway(w)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	rows, err := store.ListHumanMoments(r.Context(), digest, actor)
	if err != nil {
		momentFailure(w, err)
	} else {
		respond(w, 200, map[string]any{"data": rows})
	}
}
func (s *server) getOwnMoment(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	store, ok := s.humanMomentGateway(w)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	id := r.PathValue("momentID")
	if !uuidPath.MatchString(id) {
		respondError(w, 400, "invalid_moment_id")
		return
	}
	m, err := store.GetHumanMoment(r.Context(), digest, actor, id)
	if err != nil {
		momentFailure(w, err)
	} else {
		respond(w, 200, map[string]any{"data": m})
	}
}
func (s *server) updateMomentDraft(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	store, ok := s.humanMomentGateway(w)
	if !ok || !momentNoQuery(w, r) {
		return
	}
	id := r.PathValue("momentID")
	if !uuidPath.MatchString(id) {
		respondError(w, 400, "invalid_moment_id")
		return
	}
	var request struct {
		content.MomentInput
		Revision int64 `json:"revision"`
	}
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	if request.Revision < 1 || !validMomentInput(&request.MomentInput) {
		respondError(w, 400, "invalid_moment_draft")
		return
	}
	m, err := store.UpdateHumanMomentDraft(r.Context(), digest, actor, id, request.Revision, request.MomentInput)
	if err != nil {
		momentFailure(w, err)
	} else {
		respond(w, 200, map[string]any{"data": m})
	}
}
func (s *server) withdrawMoment(w http.ResponseWriter, r *http.Request) {
	actor, digest, ok := s.momentActor(w, r)
	if !ok {
		return
	}
	store, ok := s.humanMomentGateway(w)
	if !ok {
		return
	}
	id := r.PathValue("momentID")
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	revision, parseErr := strconv.ParseInt(query.Get("revision"), 10, 64)
	if !uuidPath.MatchString(id) || queryErr != nil || parseErr != nil || revision < 1 || len(query) != 1 || len(query["revision"]) != 1 {
		respondError(w, 400, "invalid_moment_revision")
		return
	}
	if err := store.WithdrawHumanMoment(r.Context(), digest, actor, id, revision); err != nil {
		momentFailure(w, err)
	} else {
		w.WriteHeader(204)
	}
}
