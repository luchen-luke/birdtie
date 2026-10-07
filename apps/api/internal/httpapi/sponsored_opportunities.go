package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
)

func sponsorFailure(w http.ResponseWriter, e error) {
	status, code, message := 503, "sponsorship_unavailable", "暂时无法核对赞助声明，请重新读取。"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "请重新登录。"
		w.Header().Set("WWW-Authenticate", "Bearer")
	case errors.Is(e, so.ErrForbidden):
		status, code, message = 403, "sponsorship_forbidden", "当前身份没有此声明的管理或独立审核权限。"
	case errors.Is(e, so.ErrInvalid):
		status, code, message = 400, "invalid_sponsorship", "请检查具体声明、来源、有效期与确认版本。"
	case errors.Is(e, so.ErrConflict):
		status, code, message = 409, "sponsorship_changed", "内容、来源或权限已变化，请重新读取并确认。"
	case errors.Is(e, so.ErrNotFound):
		status, code, message = 404, "sponsorship_not_found", "未找到当前可管理的赞助声明。"
	}
	w.Header().Set("Cache-Control", "no-store")
	respond(w, status, map[string]any{"error": code, "message": message})
}
func (s *server) sponsorAccess(w http.ResponseWriter, r *http.Request) (so.Access, so.Store, bool) {
	w.Header().Set("Cache-Control", "no-store")
	var a so.Access
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || len(r.Header.Values("X-Birdtie-Business-Workspace")) > 0 {
		sponsorFailure(w, so.ErrInvalid)
		return a, nil, false
	}
	if s.access == nil {
		sponsorFailure(w, so.ErrUnavailable)
		return a, nil, false
	}
	actor, digest, e := s.actor(r, true)
	if e != nil {
		sponsorFailure(w, e)
		return a, nil, false
	}
	a = so.Access{ActorID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}
	if so.ValidateAccess(a, false) != nil {
		sponsorFailure(w, so.ErrForbidden)
		return a, nil, false
	}
	store, ok := s.catalog.(so.Store)
	if !ok {
		sponsorFailure(w, so.ErrUnavailable)
		return a, nil, false
	}
	return a, store, true
}
func sponsorBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	mt, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || mt != "application/json" {
		sponsorFailure(w, so.ErrInvalid)
		return nil, false
	}
	if r.Body == nil {
		sponsorFailure(w, so.ErrInvalid)
		return nil, false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, so.MaxBodyBytes+1))
	if e != nil || len(raw) > so.MaxBodyBytes {
		sponsorFailure(w, so.ErrInvalid)
		return nil, false
	}
	return raw, true
}
func sponsorNoBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Body != nil {
		b, e := io.ReadAll(io.LimitReader(r.Body, 1))
		if e != nil || len(b) > 0 {
			sponsorFailure(w, so.ErrInvalid)
			return false
		}
	}
	return true
}
func (s *server) submitSponsoredOpportunity(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.sponsorAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("businessID")
	if !so.ValidID(id) {
		sponsorFailure(w, so.ErrInvalid)
		return
	}
	raw, ok := sponsorBody(w, r)
	if !ok {
		return
	}
	in, e := so.DecodeSubmit(raw)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	d, created, e := store.SubmitSponsoredOpportunity(r.Context(), a, id, in)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	if d.BusinessID != id || !so.ValidID(d.ID) {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	s.respondSponsorDeclaration(w, r, a, store, d, created, false)
}
func (s *server) listOwnSponsoredOpportunities(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.sponsorAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("businessID")
	if !so.ValidID(id) || !sponsorNoBody(w, r) {
		sponsorFailure(w, so.ErrInvalid)
		return
	}
	result, e := store.ListOwnSponsoredOpportunities(r.Context(), a, id)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	if result.Declarations == nil || result.Targets == nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": result})
	if e != nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	current, e := store.ListOwnSponsoredOpportunities(r.Context(), a, id)
	if e != nil || !reflect.DeepEqual(result, current) {
		if e == nil {
			e = so.ErrConflict
		}
		sponsorFailure(w, e)
		return
	}
	sponsorEncoded(w, r, encoded)
}
func (s *server) listSponsoredOpportunityReview(w http.ResponseWriter, r *http.Request) {
	a, store, ok := s.sponsorAccess(w, r)
	if !ok {
		return
	}
	city := r.PathValue("cityID")
	if !so.City(city) || !sponsorNoBody(w, r) {
		sponsorFailure(w, so.ErrInvalid)
		return
	}
	result, e := store.ListSponsoredOpportunityReview(r.Context(), a, city)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	if result == nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	encoded, e := json.Marshal(map[string]any{"data": result})
	if e != nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	current, e := store.ListSponsoredOpportunityReview(r.Context(), a, city)
	if e != nil || !reflect.DeepEqual(result, current) {
		if e == nil {
			e = so.ErrConflict
		}
		sponsorFailure(w, e)
		return
	}
	sponsorEncoded(w, r, encoded)
}
func (s *server) reviewSponsoredOpportunity(w http.ResponseWriter, r *http.Request) {
	s.mutateSponsoredOpportunity(w, r, false)
}
func (s *server) revokeSponsoredOpportunity(w http.ResponseWriter, r *http.Request) {
	s.mutateSponsoredOpportunity(w, r, true)
}
func (s *server) mutateSponsoredOpportunity(w http.ResponseWriter, r *http.Request, revoke bool) {
	a, store, ok := s.sponsorAccess(w, r)
	if !ok {
		return
	}
	id := r.PathValue("declarationID")
	if !so.ValidID(id) {
		sponsorFailure(w, so.ErrInvalid)
		return
	}
	raw, ok := sponsorBody(w, r)
	if !ok {
		return
	}
	in, e := so.DecodeReview(raw)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	if so.ValidateReview(in, revoke) != nil {
		sponsorFailure(w, so.ErrInvalid)
		return
	}
	var d so.Declaration
	if revoke {
		d, e = store.RevokeSponsoredOpportunity(r.Context(), a, id, in)
	} else {
		d, e = store.ReviewSponsoredOpportunity(r.Context(), a, id, in)
	}
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	if d.ID != id {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	s.respondSponsorDeclaration(w, r, a, store, d, false, !revoke)
}
func (s *server) respondSponsorDeclaration(w http.ResponseWriter, r *http.Request, a so.Access, store so.Store, d so.Declaration, created, review bool) {
	encoded, e := json.Marshal(map[string]any{"data": d, "created": created})
	if e != nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	var items []so.Declaration
	if review {
		items, e = store.ListSponsoredOpportunityReview(r.Context(), a, d.CityID)
	} else {
		var m so.Management
		m, e = store.ListOwnSponsoredOpportunities(r.Context(), a, d.BusinessID)
		items = m.Declarations
		if e != nil {
			items, e = store.ListSponsoredOpportunityReview(r.Context(), a, d.CityID)
		}
	}
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	matched := false
	for _, item := range items {
		if reflect.DeepEqual(item, d) {
			matched = true
		}
	}
	if !matched {
		sponsorFailure(w, so.ErrConflict)
		return
	}
	sponsorEncoded(w, r, encoded)
}
func sponsorEncoded(w http.ResponseWriter, r *http.Request, raw []byte) {
	if r.Context().Err() != nil {
		sponsorFailure(w, so.ErrUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}

func (s *server) commercialDisclosure(r *http.Request, targets []so.Target) (so.Disclosure, so.Access, error) {
	store, ok := s.catalog.(so.Store)
	if !ok {
		return so.Empty(false), so.Access{}, nil
	}
	actor, digest, e := s.actor(r, false)
	if e != nil {
		return so.Empty(false), so.Access{}, e
	}
	// Organization workspace discovery remains its own original domain path.
	// It never inherits an acting Person's commercial management or private data.
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 || actor.AccountType != "" && actor.AccountType != "person" {
		return so.Empty(false), so.Access{}, nil
	}
	var a so.Access
	if actor.ID != "" {
		a = so.Access{ActorID: actor.ID, AccountType: actor.AccountType, SessionDigest: digest}
	}
	d, e := store.ReadSponsoredOpportunities(r.Context(), a, targets)
	if e != nil {
		return so.Empty(false), a, e
	}
	if so.ValidateDisclosure(d, targets) != nil {
		return so.Empty(false), a, so.ErrUnavailable
	}
	return d, a, nil
}
func sponsorTargets(items []so.Target) []so.Target {
	out := []so.Target{}
	seen := map[string]bool{}
	for _, t := range items {
		key := t.Type + ":" + t.ID
		if so.ValidateTarget(t) == nil && !seen[key] {
			seen[key] = true
			out = append(out, t)
		}
	}
	return out
}
func (s *server) validateCommercial(r *http.Request, a so.Access, d so.Disclosure, targets []so.Target) error {
	if d.SponsoredStatus == "unavailable" {
		return nil
	}
	store, ok := s.catalog.(so.Store)
	if !ok {
		return so.ErrUnavailable
	}
	current, e := store.ReadSponsoredOpportunities(r.Context(), a, targets)
	if e != nil {
		return e
	}
	if so.ValidateDisclosure(current, targets) != nil {
		return so.ErrUnavailable
	}
	// CheckedAt advances on an actual fresh read. Stable data, order, state and
	// exact reviewed revision must match; no host-clock substitution is used.
	if len(current.SponsoredOpportunities) != len(d.SponsoredOpportunities) {
		return so.ErrConflict
	}
	for i := range current.SponsoredOpportunities {
		current.SponsoredOpportunities[i].CheckedAt = d.SponsoredOpportunities[i].CheckedAt
	}
	if !reflect.DeepEqual(d, current) {
		return so.ErrConflict
	}
	return nil
}
func (s *server) respondAgentWithCommercial(w http.ResponseWriter, r *http.Request, result agentworkspace.Results) {
	targets := []so.Target{}
	publicRef := func(kind, id string) bool {
		if !result.NativeProjection {
			return true
		}
		for _, ref := range result.PublicCommercialRefs {
			if ref.Type == kind && ref.ID == id {
				return true
			}
		}
		return false
	}
	for _, a := range result.Activities {
		if !publicRef("activity", a.ID) {
			continue
		}
		targets = append(targets, so.Target{Type: "ACTIVITY", ID: a.ID, Title: a.Title})
	}
	for _, p := range result.Places {
		if !publicRef("place", p.ID) {
			continue
		}
		targets = append(targets, so.Target{Type: "PLACE", ID: p.ID, Title: p.Name})
	}
	// Typed native items retain their original public domain refs. Owner-private
	// opportunities are never promotional targets or copied into public lanes.
	for _, item := range result.ProjectionItems {
		eligible := false
		for _, ref := range result.PublicCommercialRefs {
			if ref == item.Entity {
				eligible = true
				break
			}
		}
		if !eligible {
			continue
		}
		switch item.Entity.Type {
		case "activity":
			targets = append(targets, so.Target{Type: "ACTIVITY", ID: item.Entity.ID, Title: item.Title})
		case "place":
			targets = append(targets, so.Target{Type: "PLACE", ID: item.Entity.ID, Title: item.Title})
		}
	}
	targets = sponsorTargets(targets)
	d, a, e := s.commercialDisclosure(r, targets)
	if e != nil {
		sponsorFailure(w, e)
		return
	}
	result.CommercialTrustVersion = d.CommercialTrustVersion
	result.SponsoredStatus = d.SponsoredStatus
	result.SponsoredOpportunities = d.SponsoredOpportunities
	raw, e := json.Marshal(map[string]any{"data": result})
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
