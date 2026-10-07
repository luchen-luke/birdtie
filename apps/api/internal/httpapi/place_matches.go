package httpapi

import (
	"encoding/json"
	"errors"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
	"net/http"

	"github.com/birdtie/birdtie/apps/api/internal/placematch"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
)

func (s *server) listOwnPlaceMatches(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || len(r.Header.Values("X-Birdtie-Organization-Workspace")) != 0 {
		placeProfileError(w, pp.ErrInvalid)
		return
	}
	actor, digest, err := s.actor(r, true)
	if authFailed(w, err) {
		return
	}
	if actor.AccountType != "person" {
		respondError(w, 403, "person_account_required")
		return
	}
	if s.placeMatches == nil {
		respondError(w, 503, "place_matching_unavailable")
		return
	}
	id := r.PathValue("intentID")
	if !uuidPath.MatchString(id) {
		respondError(w, 400, "invalid_intent_id")
		return
	}
	current, ok := s.placeMatches.(placematch.CurrentStore)
	if !ok || current == nil {
		respondError(w, 503, "当前地点匹配暂不可用")
		return
	}
	in, err := current.LoadCurrentPlaceMatchInputs(r.Context(), pp.Access{ActorID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}, id)
	if errors.Is(err, socialintent.ErrNotFound) {
		respondError(w, 404, "not_found")
		return
	}
	if err != nil {
		placeProfileError(w, err)
		return
	}
	if in.PersonID != actor.ID || in.Intent.ID != id || in.Intent.CreatorID != actor.ID || in.ObservedAt.IsZero() {
		placeProfileError(w, pp.ErrUnavailable)
		return
	}
	items := placematch.Generate(in.ObservedAt, in)
	targets := []so.Target{}
	for _, m := range items {
		for _, supply := range in.Supply {
			p := supply.Place
			if p.ID == m.Place.ID {
				targets = append(targets, so.Target{Type: "PLACE", ID: p.ID, Title: p.Name})
			}
		}
	}
	targets = sponsorTargets(targets)
	d, a, e := s.commercialDisclosure(r, targets)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": items, "source": "RULE_BASED", "ruleVersion": placematch.RuleVersion, "commercialTrustVersion": d.CommercialTrustVersion, "sponsoredStatus": d.SponsoredStatus, "sponsoredOpportunities": d.SponsoredOpportunities})
	if e != nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	if e = s.validateCommercial(r, a, d, targets); e != nil {
		sponsorFailure(w, e)
		return
	}
	sponsorEncoded(w, r, raw)
}
