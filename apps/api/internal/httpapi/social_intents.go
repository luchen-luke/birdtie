package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
)

func validSocialIntentDraft(input *socialintent.DraftInput) bool {
	input.Title = strings.TrimSpace(input.Title)
	input.CityID = strings.TrimSpace(input.CityID)
	input.CommunityID = strings.TrimSpace(input.CommunityID)
	if len([]rune(input.Title)) < 1 || len([]rune(input.Title)) > 160 ||
		!input.ExpiresAt.After(time.Now().Add(time.Minute)) ||
		input.ExpiresAt.After(time.Now().Add(90*24*time.Hour)) ||
		(input.ContextID != "" && !uuidPath.MatchString(input.ContextID)) {
		return false
	}
	switch input.Type {
	case "FIND_ACTIVITY", "FIND_COMPANION", "ORGANIZE", "ASK_HELP", "OTHER":
	default:
		return false
	}
	switch input.Audience {
	case "PRIVATE", "FRIENDS", "COMMUNITY", "LOCAL", "PUBLIC", "INVITE_ONLY":
	default:
		return false
	}
	switch input.Audience {
	case "LOCAL":
		if input.CityID == "" || len(input.CityID) > 80 || input.CommunityID != "" || len(input.InviteeIDs) != 0 {
			return false
		}
	case "COMMUNITY":
		if !uuidPath.MatchString(input.CommunityID) || input.CityID != "" || len(input.InviteeIDs) != 0 {
			return false
		}
	case "INVITE_ONLY":
		if input.CityID != "" || input.CommunityID != "" || len(input.InviteeIDs) == 0 || len(input.InviteeIDs) > 20 {
			return false
		}
		seen := map[string]bool{}
		for _, id := range input.InviteeIDs {
			if !uuidPath.MatchString(id) || seen[id] {
				return false
			}
			seen[id] = true
		}
	default:
		if input.CityID != "" || input.CommunityID != "" || len(input.InviteeIDs) != 0 {
			return false
		}
	}
	switch input.Modality {
	case "IN_PERSON", "ONLINE", "HYBRID":
	default:
		return false
	}
	_, normalized, err := socialintent.ParseConstraints(input.Constraints, input.Modality)
	if err != nil {
		return false
	}
	input.Constraints = normalized
	return true
}

func (s *server) createSocialIntentDraft(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if s.socialIntents == nil {
		respondError(w, http.StatusServiceUnavailable, "social_intents_unavailable")
		return
	}
	var input socialintent.DraftInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.OperationID != "" {
		s.createKeyedPrivateIntent(w, r, "", input)
		return
	}
	if !validSocialIntentDraft(&input) {
		respondError(w, http.StatusBadRequest, "invalid_social_intent")
		return
	}
	item, err := s.socialIntents.CreateSocialIntentDraft(r.Context(), actor.ID, input)
	if errors.Is(err, socialintent.ErrNotFound) {
		respondError(w, http.StatusForbidden, "person_account_required")
	} else if errors.Is(err, socialintent.ErrInvalidConstraints) || errors.Is(err, socialintent.ErrInvalidAudience) {
		respondError(w, http.StatusBadRequest, "invalid_social_intent")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": item})
	}
}

func (s *server) createSocialIntentDraftFromTask(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if s.socialIntents == nil || s.agent == nil {
		respondError(w, http.StatusServiceUnavailable, "social_intents_unavailable")
		return
	}
	taskID := r.PathValue("taskID")
	if !uuidPath.MatchString(taskID) {
		respondError(w, http.StatusBadRequest, "invalid_task_id")
		return
	}
	if !s.requireActiveWorkspaceAgent(w, r, "person", actor.ID) {
		return
	}
	var request struct {
		Confirmed bool                    `json:"confirmed"`
		Draft     socialintent.DraftInput `json:"draft"`
	}
	if !decodeStrictJSON(w, r, &request) {
		return
	}
	if request.Draft.OperationID != "" {
		if !request.Confirmed {
			respondError(w, 400, "invalid_social_intent")
			return
		}
		s.createKeyedPrivateIntent(w, r, strings.ToLower(taskID), request.Draft)
		return
	}
	// The conversion creates a private, editable declaration. Publication is a
	// separate owner-confirmed transition and never follows from a search.
	if !request.Confirmed || request.Draft.Audience != "PRIVATE" ||
		request.Draft.Type != "FIND_ACTIVITY" || !validSocialIntentDraft(&request.Draft) {
		respondError(w, http.StatusBadRequest, "invalid_social_intent")
		return
	}
	item, err := s.socialIntents.CreateSocialIntentDraftFromTask(r.Context(), actor.ID, taskID, request.Draft)
	if errors.Is(err, socialintent.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if errors.Is(err, socialintent.ErrConflict) {
		respondError(w, http.StatusConflict, "intent_already_created_from_task")
	} else if errors.Is(err, socialintent.ErrInvalidConstraints) || errors.Is(err, socialintent.ErrInvalidAudience) {
		respondError(w, http.StatusBadRequest, "invalid_social_intent")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusCreated, map[string]any{"data": item})
	}
}

func (s *server) listOwnSocialIntents(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if s.socialIntents == nil {
		respondError(w, http.StatusServiceUnavailable, "social_intents_unavailable")
		return
	}
	items, err := s.socialIntents.ListOwnSocialIntents(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": items})
	}
}

func (s *server) getOwnSocialIntent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if s.socialIntents == nil {
		respondError(w, http.StatusServiceUnavailable, "social_intents_unavailable")
		return
	}
	id := r.PathValue("intentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_intent_id")
		return
	}
	item, err := s.socialIntents.GetOwnSocialIntent(r.Context(), actor.ID, id)
	if errors.Is(err, socialintent.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": item})
	}
}

func (s *server) listVisibleSocialIntents(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.humanSocialActor(r, false)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if s.socialIntents == nil {
		respondError(w, http.StatusServiceUnavailable, "social_intents_unavailable")
		return
	}
	if actor.ID != "" {
		if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
			respondError(w, http.StatusForbidden, "person_account_required")
			return
		}
		human, ok := s.socialIntents.(socialnow.HumanIntentsStore)
		if !ok {
			respondError(w, http.StatusServiceUnavailable, "current_social_sources_unavailable")
			return
		}
		items, e := human.ListHumanVisibleSocialIntents(r.Context(), digest, actor)
		if e != nil {
			socialNowReadError(w, e)
			return
		}
		s.respondHumanSocialNow(w, r, digest, actor, map[string]any{"data": items})
		return
	}
	items, err := s.socialIntents.ListVisibleSocialIntents(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": items})
	}
}

func (s *server) getVisibleSocialIntent(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.humanSocialActor(r, false)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if s.socialIntents == nil {
		respondError(w, http.StatusServiceUnavailable, "social_intents_unavailable")
		return
	}
	id := r.PathValue("intentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_intent_id")
		return
	}
	if actor.ID != "" {
		if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
			respondError(w, http.StatusForbidden, "person_account_required")
			return
		}
		human, ok := s.socialIntents.(socialnow.HumanIntentsStore)
		if !ok {
			respondError(w, http.StatusServiceUnavailable, "current_social_sources_unavailable")
			return
		}
		item, e := human.GetHumanVisibleSocialIntent(r.Context(), digest, actor, id)
		if errors.Is(e, socialintent.ErrNotFound) {
			respondError(w, http.StatusNotFound, "not_found")
			return
		}
		if e != nil {
			socialNowReadError(w, e)
			return
		}
		s.respondHumanSocialNow(w, r, digest, actor, map[string]any{"data": item})
		return
	}
	item, err := s.socialIntents.GetVisibleSocialIntent(r.Context(), actor.ID, id)
	if errors.Is(err, socialintent.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": item})
	}
}

func (s *server) activateSocialIntent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || s.socialIntents == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	id := r.PathValue("intentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_intent_id")
		return
	}
	var input struct {
		Confirmed bool `json:"confirmed"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !input.Confirmed {
		respondError(w, http.StatusBadRequest, "confirmation_required")
		return
	}
	item, err := s.socialIntents.ActivateSocialIntent(r.Context(), actor.ID, id)
	respondSocialIntentTransition(w, item, err)
}

func (s *server) cancelSocialIntent(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || s.socialIntents == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	id := r.PathValue("intentID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_intent_id")
		return
	}
	item, err := s.socialIntents.CancelSocialIntent(r.Context(), actor.ID, id)
	respondSocialIntentTransition(w, item, err)
}

func respondSocialIntentTransition(w http.ResponseWriter, item socialintent.Record, err error) {
	if errors.Is(err, socialintent.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
	} else if errors.Is(err, socialintent.ErrConflict) {
		respondError(w, http.StatusConflict, "intent_state_conflict")
	} else if err != nil {
		serverError(w, err)
	} else {
		respond(w, http.StatusOK, map[string]any{"data": item})
	}
}
