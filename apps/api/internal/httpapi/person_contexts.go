package httpapi

import (
	"errors"
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
	actor, ok := s.contextActor(w, r)
	if !ok {
		return
	}
	items, err := s.contextDeclarations.ListOwnContextDeclarations(r.Context(), actor)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) declareOwnContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.contextActor(w, r)
	if !ok {
		return
	}
	var input contextgraph.DeclarationInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if _, err := contextgraph.NormalizeDeclaration(input); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_context_declaration")
		return
	}
	item, err := s.contextDeclarations.DeclareContext(r.Context(), actor, input)
	if errors.Is(err, contextgraph.ErrUnavailable) {
		respondError(w, http.StatusNotFound, "context_unavailable")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": item})
	}
}

func (s *server) removeOwnContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.contextActor(w, r)
	if !ok {
		return
	}
	id, relation := r.PathValue("contextID"), r.PathValue("relation")
	if !uuidPath.MatchString(id) || relation == "" {
		respondError(w, http.StatusBadRequest, "invalid_context_declaration")
		return
	}
	err := s.contextDeclarations.RemoveContextDeclaration(r.Context(), actor, id, relation)
	if errors.Is(err, contextgraph.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
