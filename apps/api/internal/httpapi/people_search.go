package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

type publicPeopleSearcher interface {
	SearchPublicPersonIDs(context.Context, identity.ProfileAccess, string) ([]string, error)
}

// This directory returns only visible names and public identifiers. It is a
// human read, never an Agent task, relation grant or a private profile export.
func (s *server) searchPeople(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "personal_identity_required")
		return
	}
	if nilHumanSelfReviewPort(s.access) {
		respondError(w, http.StatusServiceUnavailable, "people_search_unavailable")
		return
	}
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, http.StatusForbidden, "personal_identity_required")
		return
	}
	values := r.URL.Query()["q"]
	if len(values) != 1 || len(r.URL.Query()) != 1 {
		respondError(w, http.StatusBadRequest, "invalid_people_query")
		return
	}
	query := strings.TrimSpace(values[0])
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) < 2 || len(query) > 80 || strings.IndexFunc(query, unicode.IsControl) >= 0 {
		respondError(w, http.StatusBadRequest, "invalid_people_query")
		return
	}
	search, ok := s.access.(publicPeopleSearcher)
	current, checked := s.access.(identity.CurrentProfileStore)
	if !ok || !checked || nilHumanSelfReviewPort(search) || nilHumanSelfReviewPort(current) {
		respondError(w, http.StatusServiceUnavailable, "people_search_unavailable")
		return
	}
	access := identity.ProfileAccess{Actor: actor, SessionDigest: digest}
	ids, err := search.SearchPublicPersonIDs(r.Context(), access, query)
	if err != nil {
		publicProfileFailure(w, err)
		return
	}
	if len(ids) > 20 {
		publicProfileFailure(w, identity.ErrProfileUnavailable)
		return
	}
	profiles := make([]identity.Profile, 0, len(ids))
	result := make([]map[string]string, 0, len(ids))
	seen := make(map[string]bool)
	for _, id := range ids {
		if !uuidPath.MatchString(id) || seen[id] {
			publicProfileFailure(w, identity.ErrProfileUnavailable)
			return
		}
		seen[id] = true
		profile, e := current.ReadProfileCurrent(r.Context(), access, id)
		if errors.Is(e, identity.ErrNotFound) {
			continue
		}
		if e != nil {
			publicProfileFailure(w, e)
			return
		}
		if profile.AccountID != id || profile.Visibility != "public" {
			continue
		}
		profiles = append(profiles, profile)
		result = append(result, map[string]string{"accountId": id, "displayName": profile.DisplayName})
	}
	// Revalidate each original proof after the whole response is assembled.
	for _, profile := range profiles {
		if err = current.ValidateProfileCurrent(r.Context(), access, profile.AccountID, profile); err != nil {
			publicProfileFailure(w, err)
			return
		}
	}
	latest, e := s.access.Authenticate(r.Context(), digest)
	if e != nil {
		authFailed(w, e)
		return
	}
	if latest.ID != actor.ID || latest.AccountType != actor.AccountType {
		authFailed(w, identity.ErrUnauthorized)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": result})
}
