package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/saved"
)

func (s *server) listSaved(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	items, err := s.saved.ListSaved(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) saveItem(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	var input struct {
		Kind     string `json:"kind"`
		TargetID string `json:"targetId"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if (input.Kind != "place" && input.Kind != "activity" && input.Kind != "group") ||
		!uuidPath.MatchString(input.TargetID) {
		respondError(w, http.StatusBadRequest, "invalid_saved_target")
		return
	}
	id, err := s.saved.Save(r.Context(), actor.ID, input.Kind, input.TargetID)
	if errors.Is(err, saved.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": map[string]string{"id": id}})
	}
}

func (s *server) removeSaved(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("savedID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_saved_id")
		return
	}
	err = s.saved.RemoveSaved(r.Context(), actor.ID, id)
	if errors.Is(err, saved.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
