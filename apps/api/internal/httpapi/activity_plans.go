package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
)

func (s *server) listActivityPlans(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	plans, err := s.activityPlans.ListActivityPlans(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": plans})
}

func (s *server) planActivity(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	var input struct {
		ActivityID string `json:"activityId"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !uuidPath.MatchString(input.ActivityID) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return
	}
	id, err := s.activityPlans.PlanActivity(r.Context(), actor.ID, input.ActivityID)
	if errors.Is(err, activityplan.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": map[string]string{"id": id}})
	}
}

func (s *server) removeActivityPlan(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	id := r.PathValue("planID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_plan_id")
		return
	}
	err = s.activityPlans.RemoveActivityPlan(r.Context(), actor.ID, id)
	if errors.Is(err, activityplan.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
