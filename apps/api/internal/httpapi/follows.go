package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/follow"
)

func (s *server) followActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", false
	}
	if s.follows == nil {
		respondError(w, http.StatusServiceUnavailable, "follows_unavailable")
		return "", false
	}
	return actor.ID, true
}

func (s *server) listOwnFollows(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.followActor(w, r)
	if !ok {
		return
	}
	items, err := s.follows.ListOwnFollows(r.Context(), actor)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) followTarget(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.followActor(w, r)
	if !ok {
		return
	}
	var target follow.Target
	if !decodeStrictJSON(w, r, &target) {
		return
	}
	target, err := follow.Normalize(target)
	if err != nil || !uuidPath.MatchString(target.ID) {
		respondError(w, http.StatusBadRequest, "invalid_follow_target")
		return
	}
	item, err := s.follows.Follow(r.Context(), actor, target)
	if errors.Is(err, follow.ErrUnavailable) {
		respondError(w, http.StatusNotFound, "follow_target_unavailable")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": item})
	}
}

func (s *server) unfollowTarget(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.followActor(w, r)
	if !ok {
		return
	}
	target, err := follow.Normalize(follow.Target{
		Type: r.PathValue("targetType"), ID: r.PathValue("targetID"),
	})
	if err != nil || !uuidPath.MatchString(target.ID) {
		respondError(w, http.StatusBadRequest, "invalid_follow_target")
		return
	}
	err = s.follows.Unfollow(r.Context(), actor, target)
	if errors.Is(err, follow.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}
