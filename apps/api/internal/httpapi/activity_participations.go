package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/activityparticipation"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) participationActor(w http.ResponseWriter, r *http.Request) (identity.Actor, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return identity.Actor{}, false
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return identity.Actor{}, false
	}
	if s.participations == nil {
		respondError(w, http.StatusServiceUnavailable, "participation_unavailable")
		return identity.Actor{}, false
	}
	if !uuidPath.MatchString(r.PathValue("activityID")) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return identity.Actor{}, false
	}
	return actor, true
}

func (s *server) getOwnParticipation(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.participationActor(w, r)
	if !ok {
		return
	}
	item, err := s.participations.GetParticipation(r.Context(), actor.ID, r.PathValue("activityID"))
	if errors.Is(err, activityparticipation.ErrNotFound) {
		w.Header().Set("Cache-Control", "no-store")
		respond(w, http.StatusOK, map[string]any{"data": nil})
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		respond(w, http.StatusOK, map[string]any{"data": item})
	}
}

func (s *server) listOwnParticipations(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if s.participations == nil {
		respondError(w, http.StatusServiceUnavailable, "participation_unavailable")
		return
	}
	if native, ok := s.participations.(activityparticipation.HumanStore); ok {
		current, digest, e := s.humanSocialActor(r, true)
		if authFailed(w, e) {
			return
		}
		if current.AccountType != "person" || current.ID != actor.ID {
			respondError(w, 403, "person_account_required")
			return
		}
		items, check, e := native.ListParticipationsCurrent(r.Context(), activityplan.CurrentAccess{OwnerID: current.ID, SessionDigest: digest})
		respondOwnPlansCurrent(w, r, items, check, e)
		return
	}
	items, err := s.participations.ListParticipations(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) joinActivity(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.participationActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("activityID")
	condition, ok := entityActionCondition(w, r, ea.Ref{Type: "activity", ID: id}, ea.Join)
	if !ok {
		return
	}
	var item activityparticipation.Participation
	var changed bool
	var err error
	if condition != nil {
		current, digest, e := s.humanSocialActor(r, true)
		if e != nil {
			entityActionFailure(w, e)
			return
		}
		bound, found := s.participations.(activityparticipation.BoundStore)
		if !found {
			entityActionFailure(w, ea.ErrUnavailable)
			return
		}
		item, changed, err = bound.JoinActivityBound(r.Context(), ea.Access{Actor: current, SessionDigest: digest}, id, *condition)
	} else {
		item, changed, err = s.participations.JoinActivity(r.Context(), actor.ID, id)
	}
	switch {
	case errors.Is(err, ea.ErrChanged) || errors.Is(err, ea.ErrInvalid) || errors.Is(err, ea.ErrNotFound) || errors.Is(err, ea.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized):
		entityActionFailure(w, err)
	case errors.Is(err, activityparticipation.ErrNotFound):
		respondError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, activityparticipation.ErrUnavailable):
		respondError(w, http.StatusConflict, "activity_unavailable")
	case errors.Is(err, activityparticipation.ErrFull):
		respondError(w, http.StatusConflict, "activity_full")
	case err != nil:
		serverError(w, err)
	default:
		if changed {
			s.recordConversion(r, r.PathValue("activityID"), "rsvp", conversionSource(r, "direct"), "rsvp:"+item.ID+":"+item.UpdatedAt.UTC().Format("20060102150405.000000000"))
		}
		w.Header().Set("Cache-Control", "no-store")
		status := http.StatusOK
		if changed {
			status = http.StatusCreated
		}
		respond(w, status, map[string]any{"data": item})
	}
}

func (s *server) cancelOwnParticipation(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.participationActor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("activityID")
	condition, ok := entityActionCondition(w, r, ea.Ref{Type: "activity", ID: id}, ea.Join)
	if !ok {
		return
	}
	var item activityparticipation.Participation
	var err error
	if condition != nil {
		current, digest, e := s.humanSocialActor(r, true)
		if e != nil {
			entityActionFailure(w, e)
			return
		}
		bound, found := s.participations.(activityparticipation.BoundStore)
		if !found {
			entityActionFailure(w, ea.ErrUnavailable)
			return
		}
		item, err = bound.CancelParticipationBound(r.Context(), ea.Access{Actor: current, SessionDigest: digest}, id, *condition)
	} else {
		item, err = s.participations.CancelParticipation(r.Context(), actor.ID, id)
	}
	if errors.Is(err, ea.ErrChanged) || errors.Is(err, ea.ErrInvalid) || errors.Is(err, ea.ErrNotFound) || errors.Is(err, ea.ErrUnavailable) || errors.Is(err, identity.ErrUnauthorized) {
		entityActionFailure(w, err)
		return
	}
	if errors.Is(err, activityparticipation.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.Header().Set("Cache-Control", "no-store")
		respond(w, http.StatusOK, map[string]any{"data": item})
	}
}
