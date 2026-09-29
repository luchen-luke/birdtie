package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

var organizationTypes = map[string]bool{
	"student_society": true, "club": true, "business": true, "university": true,
	"community": true, "venue": true, "nonprofit": true, "other": true,
}

func (s *server) workspacePrincipal(w http.ResponseWriter, r *http.Request, actor identity.Actor) (string, string, bool) {
	organizationID := r.Header.Get("X-Birdtie-Organization-Workspace")
	if organizationID == "" {
		return actor.ID, "owner", false
	}
	if actor.AccountType != "person" || s.organizations == nil || !uuidPath.MatchString(organizationID) {
		respondError(w, http.StatusForbidden, "organization_workspace_forbidden")
		return "", "", false
	}
	principalID, role, err := s.organizations.ResolveWorkspace(r.Context(), actor.ID, organizationID)
	if errors.Is(err, organization.ErrForbidden) {
		respondError(w, http.StatusForbidden, "organization_workspace_forbidden")
		return "", "", false
	}
	if err != nil {
		serverError(w, err)
		return "", "", false
	}
	return principalID, role, true
}

func (s *server) listOrganizations(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || s.organizations == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	items, err := s.organizations.ListOrganizations(r.Context(), actor.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) createOrganization(w http.ResponseWriter, r *http.Request) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || s.organizations == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		respondError(w, http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var input organization.CreateInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		respondError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if !organizationTypes[input.OrganizationType] || len([]rune(input.Name)) < 2 || len([]rune(input.Name)) > 160 {
		respondError(w, http.StatusBadRequest, "invalid_organization")
		return
	}
	created, err := s.organizations.CreateOrganization(r.Context(), actor.ID, input)
	if errors.Is(err, organization.ErrForbidden) {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	respond(w, http.StatusCreated, map[string]any{"data": created})
}
