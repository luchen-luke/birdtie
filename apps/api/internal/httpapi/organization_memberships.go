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
	a, p, id, ok := s.orgControlAccess(w, r)
	if !ok || !orgControlNoBody(w, r) {
		return
	}
	out, e := p.ReadMembersControl(r.Context(), a, id)
	if e != nil {
		orgControlFailure(w, e)
		return
	}
	orgControlRespond(w, r, p, a, id, out.Proof, out.ExpiresAt, out.Members)
}

func (s *server) listOrganizationInvitations(w http.ResponseWriter, r *http.Request) {
	s.listHumanOrganizationInvitations(w, r)
}

func (s *server) inviteOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if organizationPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}

func (s *server) acceptOrganizationInvitation(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if memberPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}

func (s *server) changeOrganizationMemberRole(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if organizationPath(w, r) == "" || memberPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}

func (s *server) revokeOrganizationMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.memberActor(w, r); !ok {
		return
	}
	if organizationPath(w, r) == "" || memberPath(w, r) == "" {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondError(w, 409, "organization_control_required")
}
