package httpapi

import (
	"errors"
	"net/http"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
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
	ref := ea.Ref{Type: input.Kind, ID: input.TargetID}
	if input.Kind == "group" {
		ref.Type = "community"
	}
	condition, ok := entityActionCondition(w, r, ref, ea.Save)
	if !ok {
		return
	}
	var id string
	if condition != nil {
		current, digest, e := s.humanSocialActor(r, true)
		if e != nil {
			entityActionFailure(w, e)
			return
		}
		port, found := s.saved.(saved.BoundStore)
		if !found {
			entityActionFailure(w, ea.ErrUnavailable)
			return
		}
		id, err = port.SaveBound(r.Context(), ea.Access{Actor: current, SessionDigest: digest}, input.Kind, input.TargetID, *condition)
	} else {
		id, err = s.saved.Save(r.Context(), actor.ID, input.Kind, input.TargetID)
	}
	if errors.Is(err, ea.ErrChanged) || errors.Is(err, ea.ErrInvalid) || errors.Is(err, ea.ErrNotFound) || errors.Is(err, ea.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		entityActionFailure(w, err)
		return
	}
	if errors.Is(err, saved.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		if input.Kind == "activity" {
			s.recordConversion(r, input.TargetID, "save", conversionSource(r, "direct"), "save:"+id)
		}
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
	condition, ok := entityActionCondition(w, r, ea.Ref{Type: "place", ID: id}, ea.Save)
	if !ok {
		return
	}
	if condition != nil {
		current, digest, e := s.humanSocialActor(r, true)
		if e != nil {
			entityActionFailure(w, e)
			return
		}
		port, found := s.saved.(saved.BoundStore)
		if !found {
			entityActionFailure(w, ea.ErrUnavailable)
			return
		}
		err = port.RemoveSavedBound(r.Context(), ea.Access{Actor: current, SessionDigest: digest}, id, *condition)
	} else {
		err = s.saved.RemoveSaved(r.Context(), actor.ID, id)
	}
	if errors.Is(err, ea.ErrChanged) || errors.Is(err, ea.ErrInvalid) || errors.Is(err, ea.ErrNotFound) || errors.Is(err, ea.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		entityActionFailure(w, err)
		return
	}
	if errors.Is(err, saved.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
