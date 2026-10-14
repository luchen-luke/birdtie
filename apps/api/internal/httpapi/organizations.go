package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/entitycover"
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
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if !s.captureEntityCoverHTTP(w, r, actor, digest) {
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
	has := false
	for _, i := range items {
		if i.Cover != nil || i.CoverSourceProof != "" {
			has = true
		}
	}
	if has {
		port, ok := s.access.(entitycover.Store)
		if !ok {
			entityCoverFailure(w, entitycover.ErrUnavailable)
			return
		}
		a := entitycover.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}
		w.Header().Set("Cache-Control", "no-store")
		s.respondEntityCover(w, r, a, items, func() error {
			for _, i := range items {
				if i.Cover != nil || i.CoverSourceProof != "" {
					if e := port.ValidateEntityCoverProjection(r.Context(), a, entitycover.Ref{Type: "organization", ID: i.ID}, i.Cover, i.CoverSourceProof); e != nil {
						return e
					}
				}
			}
			return nil
		})
		return
	}
	s.respondCoverWithoutMedia(w, r, entitycover.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}, 200, items)
}

func (s *server) createOrganization(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if !s.captureEntityCoverHTTP(w, r, actor, digest) {
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
	s.respondOrganizationCoverResult(w, r, actor, digest, 201, created)
}

func validOrganizationProfile(input *organization.ProfileInput) bool {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	if len([]rune(input.Name)) < 2 || len([]rune(input.Name)) > 160 ||
		len([]rune(input.Description)) > 3000 || len(input.OfficialLinks) > 8 {
		return false
	}
	if input.OfficialLinks == nil {
		input.OfficialLinks = []string{}
	}
	for i, link := range input.OfficialLinks {
		link = strings.TrimSpace(link)
		parsed, err := url.Parse(link)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" ||
			parsed.User != nil || len(link) > 2048 {
			return false
		}
		input.OfficialLinks[i] = link
	}
	return true
}

func (s *server) getPublicOrganization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		respondError(w, 400, "invalid_query")
		return
	}
	if !businessConsoleNoBody(w, r) {
		return
	}
	actor, digest, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	if !s.captureEntityCoverHTTP(w, r, actor, digest) {
		return
	}
	id := r.PathValue("organizationID")
	if !uuidPath.MatchString(id) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return
	}
	if s.organizations == nil {
		respondError(w, http.StatusServiceUnavailable, "organization_service_unavailable")
		return
	}
	profile, err := s.organizations.GetPublicProfile(r.Context(), id, actor.ID)
	if errors.Is(err, organization.ErrNotFound) {
		respondError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	encoded, err := json.Marshal(map[string]any{"data": profile})
	if err != nil {
		serverError(w, err)
		return
	}
	final, _, err := s.actor(r, false)
	if authFailed(w, err) {
		return
	}
	if final != actor || r.Context().Err() != nil {
		respondError(w, 503, "organization_profile_changed")
		return
	}
	current, err := s.organizations.GetPublicProfile(r.Context(), id, actor.ID)
	if errors.Is(err, organization.ErrNotFound) {
		respondError(w, 404, "not_found")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	if current.Cover != nil {
		copy := *current.Cover
		current.Cover = &copy
	}
	current.UpcomingActivities = append(current.UpcomingActivities[:0:0], current.UpcomingActivities...)
	if profile.CoverSourceProof != current.CoverSourceProof || !entitycover.PreserveLease(profile.Cover, current.Cover, time.Now().UTC()) || len(profile.UpcomingActivities) != len(current.UpcomingActivities) {
		entityCoverFailure(w, entitycover.ErrChanged)
		return
	}
	for i := range profile.UpcomingActivities {
		old := profile.UpcomingActivities[i]
		fresh := &current.UpcomingActivities[i]
		if fresh.Cover != nil {
			copy := *fresh.Cover
			fresh.Cover = &copy
		}
		if old.CoverSourceProof != fresh.CoverSourceProof || !entitycover.PreserveLease(old.Cover, fresh.Cover, time.Now().UTC()) {
			entityCoverFailure(w, entitycover.ErrChanged)
			return
		}
	}
	latest, err := json.Marshal(map[string]any{"data": current})
	if err != nil || !bytes.Equal(encoded, latest) {
		respondError(w, 503, "organization_profile_changed")
		return
	}
	access := entitycover.Access{AccountID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}
	if err = s.validateEntityCoverHTTP(r, access); err != nil {
		entityCoverFailure(w, err)
		return
	}
	port, ok := s.access.(entitycover.Store)
	if profile.CoverSourceProof != "" || profile.Cover != nil {
		if !ok {
			entityCoverFailure(w, entitycover.ErrUnavailable)
			return
		}
		if err = port.ValidateEntityCoverProjection(r.Context(), access, entitycover.Ref{Type: "organization", ID: id}, profile.Cover, profile.CoverSourceProof); err != nil {
			entityCoverFailure(w, err)
			return
		}
	}
	for _, act := range profile.UpcomingActivities {
		if act.CoverSourceProof != "" || act.Cover != nil {
			if !ok {
				entityCoverFailure(w, entitycover.ErrUnavailable)
				return
			}
			if err = port.ValidateEntityCoverProjection(r.Context(), access, entitycover.Ref{Type: "activity", ID: act.ID}, act.Cover, act.CoverSourceProof); err != nil {
				entityCoverFailure(w, err)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(append(encoded, '\n'))
}

func (s *server) updateOrganizationProfile(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if !s.captureEntityCoverHTTP(w, r, actor, digest) {
		return
	}
	if actor.AccountType != "person" || s.organizations == nil {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	organizationID := r.PathValue("organizationID")
	if !uuidPath.MatchString(organizationID) {
		respondError(w, http.StatusBadRequest, "invalid_organization_id")
		return
	}
	var input organization.ProfileInput
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if !validOrganizationProfile(&input) {
		respondError(w, http.StatusBadRequest, "invalid_organization_profile")
		return
	}
	updated, err := s.organizations.UpdateProfile(r.Context(), actor.ID, organizationID, input)
	if errors.Is(err, organization.ErrForbidden) {
		respondError(w, http.StatusForbidden, "organization_admin_required")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	s.respondOrganizationCoverResult(w, r, actor, digest, 200, updated)
}
