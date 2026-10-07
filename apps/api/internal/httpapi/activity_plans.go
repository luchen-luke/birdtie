package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) listActivityPlans(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "person_account_required")
		return
	}
	if native, ok := s.activityPlans.(activityplan.HumanStore); ok {
		current, digest, e := s.humanSocialActor(r, true)
		if authFailed(w, e) {
			return
		}
		if current.AccountType != "person" || current.ID != actor.ID {
			respondError(w, 403, "person_account_required")
			return
		}
		plans, check, e := native.ListActivityPlansCurrent(r.Context(), activityplan.CurrentAccess{OwnerID: current.ID, SessionDigest: digest})
		respondOwnPlansCurrent(w, r, plans, check, e)
		return
	}
	plans, err := s.activityPlans.ListActivityPlans(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": plans})
}

func respondOwnPlansCurrent(w http.ResponseWriter, r *http.Request, data any, check activityplan.CurrentValidation, err error) {
	if err == nil && check == nil {
		err = activityplan.ErrChanged
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(map[string]any{"data": data})
	}
	if err == nil {
		err = check(r.Context())
	}
	if err == nil {
		err = r.Context().Err()
	}
	if errors.Is(err, identity.ErrUnauthorized) {
		respondError(w, 401, "unauthorized")
		return
	}
	if errors.Is(err, activityplan.ErrChanged) {
		respondError(w, 409, "plans_source_changed")
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		respondError(w, 503, "plans_unavailable")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
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
