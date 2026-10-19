package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// WithThreadRouteProvider is a trusted startup dependency. No HTTP caller can
// select a supplier, supply coordinates or override a saved transport mode.
func WithThreadRouteProvider(p content.ThreadRouteProvider) Option {
	return func(s *server) { s.threadRoutes = p }
}

func (s *server) registerMapContentThreadRoute(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/me/map-content/thread-route", s.readMapContentThreadRoute)
}

func (s *server) readMapContentThreadRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if !humanControlPath(w, r) {
		return
	}
	if len(r.Header.Values("X-Birdtie-Organization-Workspace")) > 0 {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	r, a, d, ok := s.humanControlActor(w, r)
	if !ok {
		return
	}
	raw, ok := humanControlBody(w, r)
	if !ok {
		return
	}
	var query content.MapContentThreadRouteQuery
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&query) != nil || decoder.Decode(&struct{}{}) != io.EOF || !query.Valid() {
		momentControlFailure(w, content.ErrInvalid)
		return
	}
	port, ok := s.content.(content.HumanMapContentThreadRouteStore)
	if !ok || nilHumanSelfReviewPort(port) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	plan, err := port.ReadHumanMapContentThreadRoutePlan(r.Context(), d, a, query)
	if err != nil {
		momentControlFailure(w, err)
		return
	}
	v := plan.Route
	if !v.Valid(time.Now().UTC()) || v.ResponseProof == nil || v.ViewerAccountID != a.ID || v.ThreadID != query.ThreadID || v.FromMarkerID != query.From.MarkerID || v.ToMarkerID != query.To.MarkerID || v.FromSourceVersion != query.From.SourceVersion || v.ToSourceVersion != query.To.SourceVersion {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	// Revalidate before provider egress, and independently again immediately
	// before response bytes. A map read grant is not a transferable cache grant.
	frame, valid, _ := identity.OriginalHumanControlFrame(r.Context(), d, a)
	sessions, ok := s.access.(identity.HumanControlSessionStore)
	if !valid || !ok {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	if err = sessions.ValidateHumanControlSession(r.Context(), d, a, frame); err != nil {
		humanControlFailure(w, err)
		return
	}
	if err = v.ResponseProof.RevalidateHumanControl(r.Context()); err != nil {
		momentControlFailure(w, err)
		return
	}
	if plan.CanPlan {
		request := plan.Request
		if !request.From.Valid() || !request.To.Valid() || request.From.CountryCode == "" || request.To.CountryCode == "" {
			momentControlFailure(w, content.ErrUnavailable)
			return
		}
		geometry := content.ThreadRouteGeometry{Status: "BLOCKED"}
		if !content.ThreadRouteMatchesExpectedProvider(request, query.ExpectedProvider) {
			geometry = content.ThreadRouteGeometry{Status: "BLOCKED", Mode: request.Mode}
		} else if request.Mode == "train" || request.Mode == "flight" {
			geometry = content.ThreadRouteGeometry{Status: "AVAILABLE", Mode: request.Mode, Provider: "saved-transfer", CoordinateSystem: "wgs84", GeometryKind: "TRANSFER", Segments: [][]content.ThreadRoutePoint{{request.From, request.To}}}
		} else if s.threadRoutes != nil {
			ctx, cancel := context.WithDeadline(r.Context(), v.ValidUntil)
			geometry, err = s.threadRoutes.Plan(ctx, request)
			cancel()
			if err != nil {
				geometry = content.ThreadRouteGeometry{Status: "NOT_AVAILABLE"}
			}
		}
		switch geometry.Status {
		case "AVAILABLE":
			if geometry.Mode != request.Mode && request.Mode != "recommended" || geometry.GeometryKind == "ROAD" && geometry.Provider != query.ExpectedProvider {
				momentControlFailure(w, content.ErrUnavailable)
				return
			}
			v.Status, v.Mode, v.Provider, v.CoordinateSystem, v.GeometryKind = geometry.Status, geometry.Mode, geometry.Provider, geometry.CoordinateSystem, geometry.GeometryKind
			v.Segments, v.DistanceMeters, v.DurationSeconds = geometry.Segments, geometry.DistanceMeters, geometry.DurationSeconds
		case "BLOCKED", "NOT_AVAILABLE":
			v.Status = geometry.Status
			v.Mode, v.Provider = geometry.Mode, geometry.Provider
		default:
			momentControlFailure(w, content.ErrUnavailable)
			return
		}
	}
	v.SourceVersion = content.MomentControlHash([]any{plan.Route.SourceVersion, v.Status, v.Mode, v.Provider, v.CoordinateSystem, v.GeometryKind, v.Segments, v.DistanceMeters, v.DurationSeconds})
	if !v.Valid(time.Now().UTC()) {
		momentControlFailure(w, content.ErrUnavailable)
		return
	}
	s.respondHumanControl(w, r, d, a, map[string]any{"data": v}, v.ResponseProof, true)
}
