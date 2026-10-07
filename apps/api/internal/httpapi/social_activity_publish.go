package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *server) socialActivityActor(w http.ResponseWriter, r *http.Request, needsID bool) (identity.Actor, bool) {
	a, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return identity.Actor{}, false
	}
	if a.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return identity.Actor{}, false
	}
	if s.socialActivityPublish == nil {
		respondError(w, http.StatusServiceUnavailable, "activity_publish_unavailable")
		return identity.Actor{}, false
	}
	if needsID && !uuidPath.MatchString(r.PathValue("activityID")) {
		respondError(w, http.StatusBadRequest, "invalid_activity_id")
		return identity.Actor{}, false
	}
	return a, true
}

func socialActivityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, activitypublish.ErrForbidden):
		respondError(w, http.StatusForbidden, "organizer_permission_required")
	case errors.Is(err, activitypublish.ErrConflict):
		respondError(w, http.StatusConflict, "activity_state_conflict")
	default:
		serverError(w, err)
	}
}

func normalizeActivityOrganizer(in *activitypublish.Input, actor string, required bool) string {
	in.Organizer.Type = strings.ToUpper(strings.TrimSpace(in.Organizer.Type))
	in.Organizer.ID = strings.TrimSpace(in.Organizer.ID)
	if !required && in.Organizer.Type == "" && in.Organizer.ID == "" {
		return ""
	}
	_, err := actorref.ParseType(in.Organizer.Type)
	if err != nil {
		return "organizer_type_invalid"
	}
	ref, err := in.Organizer.ActorRef()
	if err != nil {
		return "organizer_id_invalid"
	}
	in.Organizer.Type, in.Organizer.ID = string(ref.Type), ref.ID
	if ref.Type == actorref.Person && !ref.Equal(actorref.ActorRef{Type: actorref.Person, ID: actor}) {
		return "person_organizer_mismatch"
	}
	if ref.Type == actorref.Person && in.Visibility == "organizer_members" {
		return "person_members_only_invalid"
	}
	return ""
}

func (s *server) decodeSocialActivityInput(w http.ResponseWriter, r *http.Request, actor string, required bool) (activitypublish.Input, bool) {
	in, ok := s.decodeActivityInput(w, r)
	if !ok {
		return in, false
	}
	if code := normalizeActivityOrganizer(&in, actor, required); code != "" {
		respondError(w, http.StatusUnprocessableEntity, code)
		return in, false
	}
	return in, true
}

func (s *server) createSocialActivityDraft(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, false)
	if !ok {
		return
	}
	in, ok := s.decodeSocialActivityInput(w, r, a.ID, true)
	if !ok {
		return
	}
	out, err := s.socialActivityPublish.CreateSocialDraft(r.Context(), a.ID, in)
	if err != nil {
		socialActivityError(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"data": out})
}

func (s *server) listManagedBusinessOrganizers(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, false)
	if !ok {
		return
	}
	items, err := s.socialActivityPublish.ListManagedBusinessOrganizers(r.Context(), a.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) updateSocialActivity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, true)
	if !ok {
		return
	}
	in, ok := s.decodeSocialActivityInput(w, r, a.ID, false)
	if !ok {
		return
	}
	out, err := s.socialActivityPublish.UpdateSocialActivity(r.Context(), a.ID, r.PathValue("activityID"), in)
	if err != nil {
		socialActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": out})
}

func (s *server) publishSocialActivity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, true)
	if !ok {
		return
	}
	out, err := s.socialActivityPublish.PublishSocialActivity(r.Context(), a.ID, r.PathValue("activityID"))
	if err != nil {
		socialActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": out})
}

func (s *server) cancelSocialActivity(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, true)
	if !ok {
		return
	}
	out, err := s.socialActivityPublish.CancelSocialActivity(r.Context(), a.ID, r.PathValue("activityID"))
	if err != nil {
		socialActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": out})
}

func (s *server) listSocialManagedActivities(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, false)
	if !ok {
		return
	}
	out, err := s.socialActivityPublish.ListSocialActivities(r.Context(), a.ID)
	if err != nil {
		socialActivityError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": out})
}

func (s *server) inviteSocialActivityPerson(w http.ResponseWriter, r *http.Request) {
	a, ok := s.socialActivityActor(w, r, true)
	if !ok {
		return
	}
	var in struct {
		UserAccountID string `json:"userAccountId"`
	}
	if !decodeStrictJSON(w, r, &in) {
		return
	}
	if !uuidPath.MatchString(in.UserAccountID) {
		respondError(w, http.StatusUnprocessableEntity, "user_account_id_invalid")
		return
	}
	if err := s.socialActivityPublish.InviteActivityPerson(r.Context(), a.ID, r.PathValue("activityID"), in.UserAccountID); err != nil {
		socialActivityError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
