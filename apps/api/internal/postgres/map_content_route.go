package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.HumanMapContentThreadRouteStore = (*Store)(nil)

// Route lookup accepts only exact leaves from their original signed pages.
// Pagination, membership and private/public visibility remain map-reader work.
func routePagePin(page content.MapContentPage, endpoint content.MapContentRouteEndpoint) (content.MapContentPin, error) {
	for _, pin := range page.Pins {
		if pin.MarkerID == endpoint.MarkerID && pin.SourceVersion == endpoint.SourceVersion && pin.Kind == "MOMENT_ANCHOR" && pin.MomentID != "" {
			return pin, nil
		}
	}
	return content.MapContentPin{}, content.ErrNotFound
}

func (s *Store) ReadHumanMapContentThreadRoutePlan(ctx context.Context, d [32]byte, a identity.Actor, q content.MapContentThreadRouteQuery) (content.MapContentThreadRoutePlan, error) {
	return s.readHumanMapContentThreadRoutePlan(ctx, d, a, q, true)
}

func (s *Store) readHumanMapContentThreadRoutePlan(ctx context.Context, d [32]byte, a identity.Actor, q content.MapContentThreadRouteQuery, proof bool) (content.MapContentThreadRoutePlan, error) {
	var result content.MapContentThreadRoutePlan
	if !q.Valid() {
		return result, content.ErrInvalid
	}
	fq, _ := q.From.Query()
	tq, _ := q.To.Query()
	fromPage, err := s.ReadHumanMapContent(ctx, d, a, fq)
	if err != nil {
		return result, err
	}
	toPage := fromPage
	if tq != fq {
		toPage, err = s.ReadHumanMapContent(ctx, d, a, tq)
		if err != nil {
			return result, err
		}
	}
	from, err := routePagePin(fromPage, q.From)
	if err != nil {
		return result, err
	}
	to, err := routePagePin(toPage, q.To)
	if err != nil {
		return result, err
	}
	if from.ThreadID != q.ThreadID || to.ThreadID != q.ThreadID || from.MomentID == to.MomentID {
		return result, content.ErrNotFound
	}
	at := time.Now().UTC()
	until := fromPage.ValidUntil
	if toPage.ValidUntil.Before(until) {
		until = toPage.ValidUntil
	}
	result.Route = content.MapContentThreadRoute{SchemaVersion: content.MapContentThreadRouteSchema, ViewerAccountID: a.ID, ThreadID: q.ThreadID, FromMarkerID: q.From.MarkerID, ToMarkerID: q.To.MarkerID, FromSourceVersion: q.From.SourceVersion, ToSourceVersion: q.To.SourceVersion, Status: "NOT_AVAILABLE", CoordinateSystem: "wgs84", Segments: [][]content.ThreadRoutePoint{}, ObservedAt: at, ValidUntil: until}
	ctx, f, err := s.beginMapContent(ctx, d, a)
	if err != nil {
		return result, err
	}
	defer f.tx.Rollback(context.Background())
	var mode, legStamp string
	err = f.tx.QueryRow(ctx, `SELECT l.mode,jsonb_build_array(l.id,l.xmin::text,t.xmin::text,e.xmin::text,e.version,fx.xmin::text,tx.xmin::text,fd.xmin::text,td.xmin::text)::text
 FROM moment_thread_route_legs l JOIN moment_threads t ON t.id=l.thread_id
 JOIN moment_thread_members fx ON fx.thread_id=t.id AND fx.moment_id=l.from_moment_id
 JOIN moment_thread_members tx ON tx.thread_id=t.id AND tx.moment_id=l.to_moment_id
 LEFT JOIN moment_thread_edit_states e ON e.thread_id=t.id
 LEFT JOIN moment_thread_display_members fd ON fd.thread_id=t.id AND fd.moment_id=fx.moment_id
 LEFT JOIN moment_thread_display_members td ON td.thread_id=t.id AND td.moment_id=tx.moment_id
 WHERE t.id=$1 AND l.from_moment_id=$2 AND l.to_moment_id=$3
 AND l.from_position=$4 AND l.to_position=$5 AND l.to_position=l.from_position+1
 AND CASE WHEN e.thread_id IS NULL THEN fx.position ELSE fd.display_position END=$4
 AND CASE WHEN e.thread_id IS NULL THEN tx.position ELSE td.display_position END=$5
 AND(l.from_place_id IS NULL OR l.from_place_id=$6::uuid) AND(l.to_place_id IS NULL OR l.to_place_id=$7::uuid)
 AND `+mapContentThreadIdentitySQL("t")+` AND `+mapContentThreadAccessSQL("t", "e", "$8")+`
 AND(NOT EXISTS(SELECT 1 FROM legacy_native_context_entities imported WHERE imported.kind='thread' AND imported.target_id=t.id) OR EXISTS(
 SELECT 1 FROM legacy_native_context_entities original
 JOIN legacy_native_context_entities origin_leg ON origin_leg.kind='thread_leg' AND origin_leg.target_id=l.id AND origin_leg.run_id=original.run_id AND origin_leg.owner_account_id=original.owner_account_id AND origin_leg.source_parent_key=original.source_key AND origin_leg.source_parent_sha256=original.source_sha256
 JOIN legacy_native_context_runs run ON run.run_id=original.run_id
 JOIN legacy_import_runs sealed ON sealed.run_id=run.run_id AND sealed.source_file_sha256=run.source_file_sha256 AND sealed.manifest_file_sha256=run.manifest_file_sha256
 WHERE original.kind='thread' AND original.target_id=t.id AND original.owner_account_id=t.author_account_id))`, q.ThreadID, from.MomentID, to.MomentID, from.ThreadPosition, to.ThreadPosition, nullableRoutePlace(from.PlaceID), nullableRoutePlace(to.PlaceID), a.ID).Scan(&mode, &legStamp)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, content.ErrUnavailable
	}
	hasLeg := err == nil && to.ThreadPosition == from.ThreadPosition+1
	linked := false
	for _, id := range from.NextMarkerIDs {
		if id == to.MarkerID {
			linked = true
		}
	}
	points := []content.ThreadRoutePoint{{Latitude: from.Anchor.Latitude, Longitude: from.Anchor.Longitude}, {Latitude: to.Anchor.Latitude, Longitude: to.Anchor.Longitude}}
	cityStamps := []string{"", ""}
	countriesCurrent := true
	for i, pin := range []content.MapContentPin{from, to} {
		if pin.CityID == nil {
			countriesCurrent = false
			continue
		}
		var deadline *time.Time
		err = f.tx.QueryRow(ctx, `SELECT name,trim(country_code::text),time_zone,xmin::text,expires_at FROM cities WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp())`, *pin.CityID).Scan(&points[i].City, &points[i].CountryCode, &points[i].TimeZone, &cityStamps[i], &deadline)
		if errors.Is(err, pgx.ErrNoRows) {
			countriesCurrent = false
			continue
		}
		if err != nil {
			return result, content.ErrUnavailable
		}
		if len(points[i].CountryCode) != 2 || points[i].CountryCode != strings.ToUpper(points[i].CountryCode) {
			countriesCurrent = false
		}
		if deadline != nil && deadline.Before(result.Route.ValidUntil) {
			result.Route.ValidUntil = *deadline
		}
	}
	if hasLeg && linked && !(from.PlaceID != "" && from.PlaceID == to.PlaceID) {
		if from.Anchor.Precision != "point" || to.Anchor.Precision != "point" || !countriesCurrent {
			result.Route.Status = "BLOCKED"
		} else {
			switch mode {
			case "walking", "cycling", "driving", "transit", "train", "flight", "recommended":
				result.CanPlan = true
				result.Request = content.ThreadRouteRequest{Mode: mode, From: points[0], To: points[1], DepartureAt: at}
				if !content.ThreadRouteMatchesExpectedProvider(result.Request, q.ExpectedProvider) {
					result.CanPlan = false
					result.Route.Status = "BLOCKED"
					result.Route.Mode = mode
				}
			}
		}
	}
	result.Route.SourceVersion = content.MomentControlHash([]any{content.MapContentThreadRouteSchema, historyReader(f), q.ThreadID, q.ExpectedProvider, q.From, q.To, from.SourceVersion, to.SourceVersion, legStamp, mode, cityStamps, points, result.CanPlan, result.Route.Status})
	if err = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, result.Route.ValidUntil, func() error { return nil })); err != nil {
		return result, err
	}
	if !result.Route.Valid(time.Now().UTC()) {
		return result, content.ErrUnavailable
	}
	if proof {
		result.Route.ResponseProof = &mapContentThreadRouteProof{s: s, d: d, a: a, q: q, source: result.Route.SourceVersion, until: result.Route.ValidUntil}
	}
	return result, nil
}

func nullableRoutePlace(id string) any {
	if id == "" {
		return nil
	}
	return id
}

type mapContentThreadRouteProof struct {
	s      *Store
	d      [32]byte
	a      identity.Actor
	q      content.MapContentThreadRouteQuery
	source string
	until  time.Time
}

func (p *mapContentThreadRouteProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	fresh, err := p.s.readHumanMapContentThreadRoutePlan(ctx, p.d, p.a, p.q, false)
	if err != nil {
		return err
	}
	if fresh.Route.SourceVersion != p.source || fresh.Route.ValidUntil.Before(p.until) || !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	return nil
}
