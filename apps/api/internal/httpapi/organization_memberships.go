package httpapi

import (
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
)

func (s *server) memberActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, _, err := s.actor(r, true)
	if authFailed(w, err) {
		return "", false
	}
	if actor.AccountType != "person" || s.memberships == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return "", false
	}
	return actor.ID, true
}

func memberError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, organization.ErrForbidden):
		respondError(w, http.StatusForbidden, "organization_membership_forbidden")
	case errors.Is(err, organization.ErrConflict):
		respondError(w, http.StatusConflict, "organization_membership_conflict")
	case errors.Is(err, organization.ErrInvalidTarget):
		respondError(w, http.StatusBadRequest, "invalid_member_account")
	default:
		serverError(w, err)
	}
	return true
}

func organizationPath(w http.ResponseWriter, r *http.Request) string {
	id := r.PathValue("organizationID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return ""
	}
	return id
}

func memberPath(w http.ResponseWriter, r *http.Request) string {
	id := r.PathValue("membershipID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_membership_id")
		return ""
	}
	return id
}

func (s *server) listOrganizationMembers(w http.ResponseWriter, r *http.Request) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return
	}
	orgID := organizationPath(w, r)
	if orgID == "" {
		return
	}
	members, err := s.memberships.ListMembers(r.Context(), actorID, orgID)
	if memberError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": members})
}

func (s *server) listOrganizationInvitations(w http.ResponseWriter, r *http.Request) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return
	}
	items, err := s.memberships.ListInvitations(r.Context(), actorID)
	if memberError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": items})
}

func (s *server) inviteOrganizationMember(w http.ResponseWriter, r *http.Request) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return
	}
	orgID := organizationPath(w, r)
	if orgID == "" {
		return
	}
	var input struct {
		UserAccountID string `json:"userAccountId"`
		Role          string `json:"role"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !uuidPath.MatchString(input.UserAccountID) ||
		(input.Role != "member" && input.Role != "moderator" && input.Role != "admin") {
		respondError(w, http.StatusBadRequest, "invalid_member_invitation")
		return
	}
	m, err := s.memberships.InviteMember(r.Context(), actorID, orgID, input.UserAccountID, input.Role)
	if memberError(w, err) {
		return
	}
	respond(w, http.StatusCreated, map[string]any{"data": m})
}

func (s *server) acceptOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return
	}
	membershipID := memberPath(w, r)
	if membershipID == "" {
		return
	}
	m, err := s.memberships.AcceptInvitation(r.Context(), actorID, membershipID)
	if memberError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": m})
}

func (s *server) changeOrganizationMemberRole(w http.ResponseWriter, r *http.Request) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return
	}
	orgID := organizationPath(w, r)
	if orgID == "" {
		return
	}
	membershipID := memberPath(w, r)
	if membershipID == "" {
		return
	}
	var input struct {
		Role string `json:"role"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.Role != "member" && input.Role != "moderator" && input.Role != "admin" && input.Role != "owner" {
		respondError(w, http.StatusBadRequest, "invalid_member_role")
		return
	}
	m, err := s.memberships.ChangeMemberRole(r.Context(), actorID, orgID, membershipID, input.Role)
	if memberError(w, err) {
		return
	}
	respond(w, http.StatusOK, map[string]any{"data": m})
}

func (s *server) revokeOrganizationMember(w http.ResponseWriter, r *http.Request) {
	actorID, ok := s.memberActor(w, r)
	if !ok {
		return
	}
	orgID := organizationPath(w, r)
	if orgID == "" {
		return
	}
	membershipID := memberPath(w, r)
	if membershipID == "" {
		return
	}
	if memberError(w, s.memberships.RevokeMember(r.Context(), actorID, orgID, membershipID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
