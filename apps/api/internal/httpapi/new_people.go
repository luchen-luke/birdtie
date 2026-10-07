package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

func (s *server) newPeopleActor(w http.ResponseWriter, r *http.Request, candidates bool) (string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	w.Header().Set("Cache-Control", "no-store")
	if actor.AccountType != "person" || r.Header.Get("X-Birdtie-Organization-Workspace") != "" {
		respondError(w, 403, "personal_workspace_required")
		return "", false
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil || (!candidates && r.URL.RawQuery != "") {
		respondError(w, 400, "owner_override_not_allowed")
		return "", false
	}
	for key, values := range query {
		if !candidates || key != "sourceIntentId" || len(values) != 1 {
			respondError(w, 400, "owner_override_not_allowed")
			return "", false
		}
	}
	if s.newPeople == nil {
		respondError(w, 503, "new_people_unavailable")
		return "", false
	}
	return actor.ID, true
}

func newPeopleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, mp.ErrInvalid) || errors.Is(err, mp.ErrDenied) || errors.Is(err, mp.ErrChanged) || errors.Is(err, mp.ErrUnavailable):
		messagePolicyFailure(w, err)
	case errors.Is(err, identity.ErrUnauthorized):
		respondError(w, 401, "unauthorized")
	case errors.Is(err, newpeople.ErrChanged):
		respondError(w, 409, "new_people_source_changed")
	case errors.Is(err, newpeople.ErrNotFound), errors.Is(err, socialintent.ErrNotFound), errors.Is(err, connection.ErrNotFound):
		respondError(w, 404, "not_found")
	case errors.Is(err, newpeople.ErrInvalid), errors.Is(err, socialintent.ErrInvalidAudience), errors.Is(err, socialintent.ErrInvalidConstraints):
		respondError(w, 400, "invalid_new_people_intent")
	case errors.Is(err, newpeople.ErrForbidden), errors.Is(err, connection.ErrForbidden):
		respondError(w, 403, "new_people_forbidden")
	case errors.Is(err, connection.ErrConflict):
		respondError(w, 409, "request_conflict")
	case errors.Is(err, connection.ErrRateLimit):
		respondError(w, 429, "request_limit")
	default:
		serverError(w, err)
	}
	return true
}
func (s *server) ownNewPeopleConsent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.newPeopleActor(w, r, false)
	if !ok {
		return
	}
	out, err := s.newPeople.GetNewPeopleConsent(r.Context(), actor)
	if !newPeopleError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
func (s *server) setNewPeopleConsent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.newPeopleActor(w, r, false)
	if !ok {
		return
	}
	var raw struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeStrictJSON(w, r, &raw) {
		return
	}
	if raw.Enabled == nil {
		respondError(w, 400, "explicit_consent_required")
		return
	}
	out, err := s.newPeople.SetNewPeopleConsent(r.Context(), actor, *raw.Enabled)
	if !newPeopleError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
func (s *server) listOwnNewPeopleIntents(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.newPeopleActor(w, r, false)
	if !ok {
		return
	}
	out, err := s.newPeople.ListNewPeopleIntents(r.Context(), actor)
	if !newPeopleError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
func (s *server) createNewPeopleIntent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.newPeopleActor(w, r, false)
	if !ok {
		return
	}
	var raw newpeople.DraftInput
	if !decodeStrictJSON(w, r, &raw) {
		return
	}
	out, err := s.newPeople.CreateNewPeopleIntent(r.Context(), actor, raw)
	if !newPeopleError(w, err) {
		respond(w, 201, map[string]any{"data": out})
	}
}
func (s *server) newPeopleCandidates(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.newPeopleActor(w, r, true)
	if !ok {
		return
	}
	source := r.URL.Query().Get("sourceIntentId")
	if !uuidPath.MatchString(source) {
		respondError(w, 400, "invalid_source_intent_id")
		return
	}
	// Explicit human matching retains the original source/opt-in writer and
	// cannot become a FindPerson query or a machine social-purpose permission.
	if current, yes := s.newPeople.(agenttool.CurrentMatchPort); yes {
		digest, err := identity.ParseBearer(r.Header.Get("Authorization"))
		if newPeopleError(w, err) {
			return
		}
		input := agenttool.CurrentMatch{Actor: identity.Actor{ID: actor, AccountType: "person"}, SessionDigest: digest, SourceIntentID: source}
		receipt, err := agenttool.ReadCurrentMatch(r.Context(), current, input)
		if err != nil {
			resultProjectionFailure(w, currentToolReadError(err))
			return
		}
		encoded, err := json.Marshal(map[string]any{"data": receipt.Source.Response})
		if err != nil {
			respondError(w, 503, "new_people_unavailable")
			return
		}
		if err = current.RevalidateOwnCurrentMatch(r.Context(), input, receipt); err != nil {
			resultProjectionFailure(w, currentToolReadError(err))
			return
		}
		if r.Context().Err() != nil {
			respondError(w, 503, "new_people_unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write(append(encoded, '\n'))
		return
	}
	// Native receipts bind the rendered response to its current Session and
	// source versions. Legacy fixture Stores have no native-current guarantee.
	if native, yes := s.newPeople.(newpeople.HumanStore); yes {
		digest, err := identity.ParseBearer(r.Header.Get("Authorization"))
		if newPeopleError(w, err) {
			return
		}
		access := identity.Actor{ID: actor, AccountType: "person"}
		receipt, err := native.ReadHumanNewPeople(r.Context(), access, digest, source)
		if newPeopleError(w, err) {
			return
		}
		encoded, err := json.Marshal(map[string]any{"data": receipt.Response})
		if newPeopleError(w, err) {
			return
		}
		if newPeopleError(w, native.RevalidateHumanNewPeople(r.Context(), access, digest, receipt)) {
			return
		}
		if r.Context().Err() != nil {
			respondError(w, 503, "new_people_unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write(append(encoded, '\n'))
		return
	}
	out, err := s.newPeople.FindNewPeople(r.Context(), actor, source)
	if !newPeopleError(w, err) {
		respond(w, 200, map[string]any{"data": out})
	}
}
func (s *server) createNewPeopleInvitation(w http.ResponseWriter, r *http.Request) {
	access, ok := s.messageWriteAccess(w, r)
	if !ok {
		return
	}
	var raw struct {
		Source    string `json:"sourceIntentId"`
		Candidate string `json:"candidateIntentId"`
		Note      string `json:"note"`
		Confirmed bool   `json:"confirmed"`
	}
	if !decodeStrictJSON(w, r, &raw) {
		return
	}
	raw.Note = strings.TrimSpace(raw.Note)
	if !raw.Confirmed || !uuidPath.MatchString(raw.Source) || !uuidPath.MatchString(raw.Candidate) || len(raw.Note) < 1 || len(raw.Note) > 280 {
		respondError(w, 400, "explicit_invitation_required")
		return
	}
	version, ok := messagePolicyVersion(w, r)
	if !ok {
		return
	}
	port, ok := s.newPeople.(newPeopleCurrentInvitationStore)
	if !ok {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	out, err := port.InviteNewPeopleCurrent(r.Context(), access, raw.Source, raw.Candidate, raw.Note, version)
	if !newPeopleError(w, err) {
		s.messageWriteResponse(w, r, access, http.StatusCreated, out)
	}
}

type newPeopleCurrentInvitationStore interface {
	InviteNewPeopleCurrent(context.Context, ea.Access, string, string, string, string) (connection.Request, error)
}
