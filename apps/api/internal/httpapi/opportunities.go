package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
)

func (s *server) listOwnOpportunities(w http.ResponseWriter, r *http.Request) {
	actor, digest, err := s.humanSocialActor(r, true)
	if humanSocialAuthFailed(w, err) {
		return
	}
	if actor.AccountType != "person" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		respondError(w, http.StatusForbidden, "person_account_required")
		return
	}
	// This endpoint has no filters or caller-supplied owner/principal. Decode
	// neither unknown keys nor duplicate query values into authorization state.
	if r.URL.RawQuery != "" {
		respondError(w, http.StatusBadRequest, "owner_override_not_allowed")
		return
	}
	if s.opportunities == nil {
		respondError(w, http.StatusServiceUnavailable, "opportunity_engine_unavailable")
		return
	}
	human, ok := s.opportunities.(socialnow.HumanOpportunitiesStore)
	if !ok {
		respondError(w, http.StatusServiceUnavailable, "current_social_sources_unavailable")
		return
	}
	items, err := human.ListHumanOpportunities(r.Context(), digest, actor)
	if err != nil {
		socialNowReadError(w, err)
		return
	}
	targets := []so.Target{}
	for _, item := range items {
		targets = append(targets, so.Target{Type: "ACTIVITY", ID: item.Entity.ID, Title: item.Title})
	}
	targets = sponsorTargets(targets)
	d, a, e := s.commercialDisclosure(r, targets)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	if e = s.validateCommercial(r, a, d, targets); e != nil {
		sponsorFailure(w, e)
		return
	}
	s.respondHumanSocialNow(w, r, digest, actor, map[string]any{
		"commercialTrustVersion": d.CommercialTrustVersion, "sponsoredStatus": d.SponsoredStatus, "sponsoredOpportunities": d.SponsoredOpportunities,
		"data":   items,
		"source": "RULE_BASED", "ruleVersion": opportunity.RuleVersion,
	})
}

// Anonymous public intent reads preserve the original path. Signed reads must
// resolve the real digest with current native clock eligibility, never fallback.
func (s *server) humanSocialActor(r *http.Request, required bool) (identity.Actor, [32]byte, error) {
	var zero [32]byte
	headers := r.Header.Values("Authorization")
	if len(headers) == 0 && !required {
		return identity.Actor{}, zero, nil
	}
	if len(headers) != 1 {
		return identity.Actor{}, zero, identity.ErrUnauthorized
	}
	digest, e := identity.ParseBearer(headers[0])
	if e != nil {
		return identity.Actor{}, zero, e
	}
	access, ok := s.access.(socialnow.HumanSessionStore)
	if !ok {
		return identity.Actor{}, zero, socialnow.ErrUnavailable
	}
	actor, e := access.AuthenticateHumanSocial(r.Context(), digest)
	return actor, digest, e
}
func humanSocialAuthFailed(w http.ResponseWriter, e error) bool {
	if errors.Is(e, socialnow.ErrUnavailable) {
		socialNowReadError(w, e)
		return true
	}
	return authFailed(w, e)
}

func socialNowReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrUnauthorized) {
		respondError(w, http.StatusUnauthorized, "unauthorized")
	} else if errors.Is(err, socialnow.ErrUnavailable) {
		respondError(w, http.StatusServiceUnavailable, "current_social_sources_unavailable")
	} else {
		serverError(w, err)
	}
}

// Marshal first, then validate the actual current Session after real row waits.
// Validation does not refresh idle expiry or re-authorize the source snapshot.
func (s *server) respondHumanSocialNow(w http.ResponseWriter, r *http.Request, digest [32]byte, actor identity.Actor, payload any) {
	raw, e := json.Marshal(payload)
	if e != nil {
		serverError(w, e)
		return
	}
	access, ok := s.access.(interface {
		ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
	})
	if !ok {
		socialNowReadError(w, socialnow.ErrUnavailable)
		return
	}
	if e = access.ValidateHumanSocialResponse(r.Context(), digest, actor); e != nil {
		socialNowReadError(w, e)
		return
	}
	if r.Context().Err() != nil {
		socialNowReadError(w, socialnow.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
