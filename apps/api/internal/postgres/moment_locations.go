package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"github.com/jackc/pgx/v5"
	"math"
	"sort"
	"time"
)

type momentLocationsSnapshot struct {
	Requested               *[]content.MomentLocationInput
	Effective               []content.MomentLocationInput
	Resolved                []content.MomentLocationResolved
	SourceVersion, external string
	ValidUntil              *time.Time
}
type momentLocationRow struct {
	input         content.MomentLocationInput
	position      int
	revision      int64
	status, stamp string
}

func readMomentLocationRows(ctx context.Context, tx pgx.Tx, id string) ([]momentLocationRow, error) {
	rows, e := tx.Query(ctx, `SELECT id,kind,coalesce(city_id,''),coalesce(place_id::text,''),coalesce(reference_id::text,''),precision,position,revision,status,xmin::text FROM moment_location_links WHERE moment_id=$1 AND status='ACTIVE' ORDER BY id LIMIT 51`, id)
	if e != nil {
		return nil, identity.ErrControlUnavailable
	}
	defer rows.Close()
	out := []momentLocationRow{}
	for rows.Next() {
		var v momentLocationRow
		if e = rows.Scan(&v.input.ID, &v.input.Kind, &v.input.CityID, &v.input.PlaceID, &v.input.ReferenceID, &v.input.Precision, &v.position, &v.revision, &v.status, &v.stamp); e != nil {
			return nil, identity.ErrControlUnavailable
		}
		out = append(out, v)
	}
	if rows.Err() != nil || len(out) > content.MomentLocationMaxLinks {
		return nil, identity.ErrControlUnavailable
	}
	return out, nil
}
func momentLocationEpoch(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var epoch string
	e := tx.QueryRow(ctx, `SELECT coalesce((SELECT jsonb_build_array(version,xmin::text)::text FROM moment_location_epochs WHERE moment_id=$1),'none')`, id).Scan(&epoch)
	if e != nil {
		return "", identity.ErrControlUnavailable
	}
	return epoch, nil
}
func momentLocationLegacyFields(v []content.MomentLocationInput) (city, place, precision string) {
	precision = "none"
	if len(v) != 1 {
		return
	}
	x := v[0]
	if x.Kind == "CITY" && x.Precision == "city" {
		city, precision = x.CityID, "city"
	}
	if x.Kind == "PLACE" && x.Precision == "place" {
		place, precision = x.PlaceID, "place"
	}
	return
}
func legacyMomentLocationID(seed string, v content.MomentLocationInput) string {
	b := sha256.Sum256([]byte("moment-locations-v1:" + seed + ":" + v.Kind + ":" + v.CityID + ":" + v.PlaceID))
	b[6] = (b[6] & 15) | 80
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func legacyMomentLocations(seed string, in content.MomentInput, old []content.MomentLocationInput) []content.MomentLocationInput {
	out := []content.MomentLocationInput{}
	v := content.MomentLocationInput{Precision: in.LocationPrecision}
	if in.PlaceID != "" {
		v.Kind, v.PlaceID = "PLACE", in.PlaceID
	} else if in.CityID != "" {
		v.Kind, v.CityID = "CITY", in.CityID
	} else {
		return out
	}
	for _, x := range old {
		if x.Kind == v.Kind && x.CityID == v.CityID && x.PlaceID == v.PlaceID {
			v.ID = x.ID
		}
	}
	if v.ID == "" {
		v.ID = legacyMomentLocationID(seed, v)
	}
	return append(out, v)
}

// All items (including a thread) lock City, then Place, then permission IDs in
// canonical order. Parent City is a source, never another author selection.
func lockMomentLocationTargets(ctx context.Context, f humanWriteFrame, all []content.MomentLocationInput) error {
	cities, places := map[string]bool{}, map[string]string{}
	references := []string{}
	for _, v := range all {
		if v.Kind == "REFERENCE" {
			references = append(references, v.ReferenceID)
		} else if v.Kind == "CITY" {
			cities[v.CityID] = true
		} else {
			var city string
			e := f.tx.QueryRow(ctx, `SELECT coalesce(city_id,'') FROM places WHERE id=$1`, v.PlaceID).Scan(&city)
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e != nil {
				return identity.ErrControlUnavailable
			}
			if city != "" {
				cities[city] = true
			}
			places[v.PlaceID] = city
		}
	}
	sort.Strings(references)
	for _, id := range references {
		var actual string
		e := f.tx.QueryRow(ctx, `SELECT id FROM location_references WHERE id=$1 FOR SHARE`, id).Scan(&actual)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return identity.ErrControlUnavailable
		}
		if e == nil {
			e = f.tx.QueryRow(ctx, `SELECT reference_id FROM location_reference_observations WHERE reference_id=$1 FOR SHARE`, id).Scan(&actual)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return identity.ErrControlUnavailable
			}
		}
	}
	ids := func(m map[string]bool) []string {
		out := []string{}
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, id := range ids(cities) {
		var actual string
		e := f.tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 FOR SHARE`, id).Scan(&actual)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return identity.ErrControlUnavailable
		}
	}
	pids := []string{}
	for id := range places {
		pids = append(pids, id)
	}
	sort.Strings(pids)
	for _, id := range pids {
		var city string
		e := f.tx.QueryRow(ctx, `SELECT coalesce(city_id,'') FROM places WHERE id=$1 FOR SHARE`, id).Scan(&city)
		if errors.Is(e, pgx.ErrNoRows) {
			continue
		}
		if e != nil {
			return identity.ErrControlUnavailable
		}
		if city != places[id] {
			return identity.ErrControlChanged
		}
	}
	if len(pids) > 0 {
		rows, e := f.tx.Query(ctx, `SELECT r.id FROM place_source_permissions r WHERE r.id IN(SELECT permission_id FROM place_sources WHERE place_id=ANY($1::uuid[]) AND permission_id IS NOT NULL UNION SELECT permission_id FROM place_field_evidence WHERE place_id=ANY($1::uuid[])) ORDER BY r.id FOR SHARE`, pids)
		if e != nil {
			return identity.ErrControlUnavailable
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				break
			}
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return identity.ErrControlUnavailable
		}
	}
	return nil
}
func resolveMomentLocation(ctx context.Context, f humanWriteFrame, v content.MomentLocationInput, position int) (content.MomentLocationResolved, error) {
	r := content.MomentLocationResolved{ID: v.ID, Position: position, Kind: v.Kind, Precision: v.Precision, CityID: v.CityID, PlaceID: v.PlaceID, ReferenceID: v.ReferenceID}
	if e := f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&r.ObservedAt); e != nil {
		return r, identity.ErrControlUnavailable
	}
	r.ObservedAt = r.ObservedAt.UTC()
	r.ValidUntil = r.ObservedAt.Add(time.Minute)
	var stamp string
	var deadline *time.Time
	var e error
	if v.Kind == "REFERENCE" {
		var lat, lon float64
		e = f.tx.QueryRow(ctx, `SELECT name,region,country_code,source_url,attribution,latitude,longitude,binding,deadline FROM birdtie_location_reference_source($1,$2)`, v.ReferenceID, r.ObservedAt).Scan(&r.ReferenceName, &r.Region, &r.CountryCode, &r.SourceURL, &r.Attribution, &lat, &lon, &stamp, &deadline)
		if e == nil && v.Precision == "place" {
			r.Anchor = &content.MomentLocationAnchor{TargetKind: "REFERENCE", ReferenceID: v.ReferenceID, Label: r.ReferenceName, CoordinateSystem: "wgs84", Precision: "point", Latitude: lat, Longitude: lon}
		}
	} else if v.Kind == "CITY" {
		e = f.tx.QueryRow(ctx, `SELECT name,region,country_code::text,xmin::text,expires_at FROM cities WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>$2)`, v.CityID, r.ObservedAt).Scan(&r.CityName, &r.Region, &r.CountryCode, &stamp, &deadline)
	} else {
		var system, precision string
		var lat, lon *float64
		e = f.tx.QueryRow(ctx, `SELECT city_id,city_name,place_name,region,country_code,coalesce(coordinate_system,''),coalesce(location_precision,''),latitude,longitude,binding,deadline FROM birdtie_moment_location_place_source($1,$2)`, v.PlaceID, r.ObservedAt).Scan(&r.CityID, &r.CityName, &r.PlaceName, &r.Region, &r.CountryCode, &system, &precision, &lat, &lon, &stamp, &deadline)
		if e == nil && v.Precision == "place" && system == "wgs84" && precision == "point" && lat != nil && lon != nil && !math.IsNaN(*lat) && !math.IsNaN(*lon) && *lat >= -90 && *lat <= 90 && *lon >= -180 && *lon <= 180 {
			r.Anchor = &content.MomentLocationAnchor{PlaceID: v.PlaceID, Label: r.PlaceName, CoordinateSystem: "wgs84", Precision: "point", Latitude: *lat, Longitude: *lon}
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		r.CityID = v.CityID
		r.SourceVersion = content.MomentControlHash([]any{"location-unavailable", v.Kind, v.CityID, v.PlaceID, v.ReferenceID})
		return r, nil
	}
	if e != nil {
		return r, identity.ErrControlUnavailable
	}
	r.Available = true
	r.SourceVersion = content.MomentControlHash([]any{v.Kind, v.CityID, v.PlaceID, v.ReferenceID, stamp})
	if deadline != nil && deadline.Before(r.ValidUntil) {
		r.ValidUntil = deadline.UTC()
	}
	return r, nil
}
func captureMomentLocationsInTx(ctx context.Context, f humanWriteFrame, current *content.Moment, in content.MomentInput) (momentLocationsSnapshot, error) {
	out := momentLocationsSnapshot{Requested: in.Locations, Effective: []content.MomentLocationInput{}, Resolved: []content.MomentLocationResolved{}}
	existing := []momentLocationRow{}
	var e error
	if current != nil && current.ID != "" {
		existing, e = readMomentLocationRows(ctx, f.tx, current.ID)
		if e != nil {
			return out, e
		}
	}
	epoch := "none"
	if current != nil && current.ID != "" {
		epoch, e = momentLocationEpoch(ctx, f.tx, current.ID)
		if e != nil {
			return out, e
		}
		if in.Locations != nil {
			for _, requested := range *in.Locations {
				var removed bool
				e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moment_location_links WHERE id=$1 AND moment_id=$2 AND status='REMOVED')`, requested.ID, current.ID).Scan(&removed)
				if e != nil {
					return out, identity.ErrControlUnavailable
				}
				if removed {
					return out, identity.ErrControlChanged
				}
			}
		}
	}
	active := []momentLocationRow{}
	old := map[string]momentLocationRow{}
	proof := []any{epoch}
	for _, v := range existing {
		old[v.input.ID] = v
		proof = append(proof, []any{v.input, v.position, v.revision, v.status, v.stamp})
		if v.status == "ACTIVE" {
			active = append(active, v)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].position < active[j].position })
	for _, v := range active {
		out.Effective = append(out.Effective, v.input)
	}
	if in.Locations != nil {
		out.Effective = append([]content.MomentLocationInput{}, (*in.Locations)...)
	} else if current == nil || current.ID == "" {
		out.Effective = legacyMomentLocations(f.op, in, nil)
	} else if in.CityID != current.CityID || in.PlaceID != current.PlaceID || in.LocationPrecision != current.LocationPrecision {
		if len(active) > 1 {
			return out, content.ErrMomentLocationsClientRequired
		}
		out.Effective = legacyMomentLocations(f.op, in, out.Effective)
	}
	if e = lockMomentLocationTargets(ctx, f, out.Effective); e != nil {
		return out, e
	}
	external := []any{}
	for i, v := range out.Effective {
		previous, existed := old[v.ID]
		if existed && (previous.status != "ACTIVE" || previous.input.Kind != v.Kind || previous.input.CityID != v.CityID || previous.input.PlaceID != v.PlaceID || previous.input.ReferenceID != v.ReferenceID) {
			return out, identity.ErrControlChanged
		}
		r, e := resolveMomentLocation(ctx, f, v, i)
		if e != nil {
			return out, e
		}
		if !r.Available && (!existed || previous.input.Precision != v.Precision) {
			return out, identity.ErrControlChanged
		}
		if current != nil {
			if e = enrichMomentLocationPrivateReference(ctx, f, current.ID, &r); e != nil {
				return out, e
			}
		}
		out.Resolved = append(out.Resolved, r)
		privateProof := ""
		if r.LegacyReference != nil {
			privateProof, _, e = legacyMomentLocationReferenceAuthorityTx(ctx, f, r.LegacyReference.Reference)
			if e != nil {
				return out, e
			}
			momentControlAddDeadline(&out.ValidUntil, &r.LegacyReference.ValidUntil)
		}
		external = append(external, []any{v, r.SourceVersion, r.Available, privateProof})
		if r.Available {
			momentControlAddDeadline(&out.ValidUntil, &r.ValidUntil)
		}
	}
	out.external = content.MomentControlHash(external)
	out.SourceVersion = content.MomentControlHash([]any{"moment-locations-v1", proof, out.Effective, out.external})
	return out, nil
}
func revalidateMomentLocationsInTx(ctx context.Context, f humanWriteFrame, old momentLocationsSnapshot, until time.Time) error {
	external := []any{}
	for i, v := range old.Effective {
		r, e := resolveMomentLocation(ctx, f, v, i)
		if e != nil {
			return e
		}
		privateProof := ""
		if old.Resolved[i].LegacyReference != nil {
			ref := old.Resolved[i].LegacyReference
			proof, deadline, e := legacyMomentLocationReferenceAuthorityTx(ctx, f, ref.Reference)
			if e != nil || deadline.Before(until) {
				return identity.ErrControlChanged
			}
			privateProof = proof
		}
		external = append(external, []any{v, r.SourceVersion, r.Available, privateProof})
		if r.Available && r.ValidUntil.Before(until) {
			return identity.ErrControlChanged
		}
	}
	if content.MomentControlHash(external) != old.external {
		return identity.ErrControlChanged
	}
	return nil
}

// The parent's intended revision/location write is fenced by the caller's exact
// effect stamp. Retained source rights and original owner provenance are a
// separate external fence, so editing that parent cannot invalidate itself.
func legacyMomentLocationReferenceAuthorityTx(ctx context.Context, f humanWriteFrame, ref lpr.Reference) (string, time.Time, error) {
	var raw string
	var deadline time.Time
	e := f.tx.QueryRow(ctx, `SELECT (r.source::jsonb-(jsonb_array_length(r.source::jsonb)-1))::text,r.valid_until FROM (`+legacyPlaceReferenceSQL()+`) r`, f.a.ID, ref.Kind, ref.RecordID, ref.PlaceID).Scan(&raw, &deadline)
	if e != nil {
		return "", deadline, identity.ErrControlChanged
	}
	return identity.HumanControlTextHash(raw), deadline, nil
}
func applyMomentLocationsInTx(ctx context.Context, f humanWriteFrame, id string, revision int64, captured momentLocationsSnapshot) error {
	var valid bool
	if e := f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moments WHERE id=$1 AND author_account_id=$2 AND revision=$3 AND status<>'withdrawn')`, id, f.a.ID, revision).Scan(&valid); e != nil || !valid {
		return identity.ErrControlChanged
	}

	ids := []string{}
	for _, v := range captured.Effective {
		ids = append(ids, v.ID)
	}
	if _, e := f.tx.Exec(ctx, `UPDATE moment_location_links SET status='REMOVED',revision=revision+1,updated_at=clock_timestamp() WHERE moment_id=$1 AND status='ACTIVE' AND NOT(id=ANY($2::uuid[]))`, id, ids); e != nil {
		return identity.ErrControlUnavailable
	}
	for position, v := range captured.Effective {
		tag, e := f.tx.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,city_id,place_id,precision,position,reference_id)VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,'')::uuid,$6,$7,NULLIF($8,'')::uuid)ON CONFLICT(id)DO UPDATE SET precision=EXCLUDED.precision,position=EXCLUDED.position,revision=moment_location_links.revision+1,updated_at=clock_timestamp() WHERE moment_location_links.moment_id=EXCLUDED.moment_id AND moment_location_links.status='ACTIVE' AND moment_location_links.kind=EXCLUDED.kind AND moment_location_links.city_id IS NOT DISTINCT FROM EXCLUDED.city_id AND moment_location_links.place_id IS NOT DISTINCT FROM EXCLUDED.place_id AND moment_location_links.reference_id IS NOT DISTINCT FROM EXCLUDED.reference_id AND(moment_location_links.precision<>EXCLUDED.precision OR moment_location_links.position<>EXCLUDED.position)`, v.ID, id, v.Kind, v.CityID, v.PlaceID, v.Precision, position, v.ReferenceID)
		if e != nil {
			return identity.ErrControlChanged
		}
		if tag.RowsAffected() == 0 {
			var valid bool
			if e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moment_location_links WHERE id=$1 AND moment_id=$2 AND status='ACTIVE' AND kind=$3 AND coalesce(city_id,'')=$4 AND coalesce(place_id::text,'')=$5 AND precision=$6 AND position=$7 AND coalesce(reference_id::text,'')=$8)`, v.ID, id, v.Kind, v.CityID, v.PlaceID, v.Precision, position, v.ReferenceID).Scan(&valid); e != nil || !valid {
				return identity.ErrControlChanged
			}
		}
	}
	return nil
}

var _ content.MomentLocationsStore = (*Store)(nil)

func locationReceiptSeal(a content.PublicMomentAccess, r content.MomentLocationsReceipt) (string, error) {
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return "", content.ErrUnavailable
	}
	raw, e := json.Marshal([]any{a.Actor, hex.EncodeToString(a.SessionDigest[:]), r.View, r.Own, r.Proof, r.Frame.ID, r.Frame.Generation})
	if e != nil {
		return "", content.ErrUnavailable
	}
	h := hmac.New(sha256.New, humanWriteSigning.key[:])
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func readMomentLocationsView(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, id string, own bool) (content.MomentLocationsView, string, error) {
	out := content.MomentLocationsView{SchemaVersion: content.MomentLocationsSchemaVersion, MomentID: id}
	var owner, status, stamp string
	var binding *string
	var bodyDeadline *time.Time
	e := tx.QueryRow(ctx, `SELECT m.author_account_id,m.status,m.revision,m.xmin::text,birdtie_moment_public_binding(m.id,NULLIF($2,'')::uuid,clock_timestamp()),clock_timestamp(),`+momentPublishableMediaDeadlineSQL("m")+` FROM moments m WHERE m.id=$1`, id, a.Actor.ID).Scan(&owner, &status, &out.MomentRevision, &stamp, &binding, &out.ObservedAt, &bodyDeadline)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, "", content.ErrNotFound
	}
	if e != nil {
		return out, "", content.ErrUnavailable
	}
	if own && (a.Actor.ID == "" || owner != a.Actor.ID || status == "withdrawn") || !own && binding == nil {
		return out, "", content.ErrNotFound
	}
	out.ObservedAt = out.ObservedAt.UTC()
	out.ValidUntil = out.ObservedAt.Add(time.Minute)
	if !own && bodyDeadline != nil && bodyDeadline.Before(out.ValidUntil) {
		out.ValidUntil = bodyDeadline.UTC()
	}
	rows, e := readMomentLocationRows(ctx, tx, id)
	if e != nil {
		return out, "", e
	}
	epoch, e := momentLocationEpoch(ctx, tx, id)
	if e != nil {
		return out, "", e
	}
	proof := []any{stamp, binding, epoch}
	sort.Slice(rows, func(i, j int) bool { return rows[i].position < rows[j].position })
	owned := []content.MomentLocationResolved{}
	public := []content.PublicMomentLocation{}
	f := humanWriteFrame{tx: tx, a: a.Actor}
	for _, row := range rows {
		proof = append(proof, []any{row.input, row.position, row.revision, row.status, row.stamp})
		if row.status != "ACTIVE" {
			continue
		}
		r, e := resolveMomentLocation(ctx, f, row.input, row.position)
		if e != nil {
			return out, "", e
		}
		if own {
			if e = enrichMomentLocationPrivateReference(ctx, f, id, &r); e != nil {
				return out, "", e
			}
		}
		privateProof := ""
		if r.LegacyReference != nil {
			privateProof = r.LegacyReference.SourceVersion
			if r.LegacyReference.ValidUntil.Before(out.ValidUntil) {
				out.ValidUntil = r.LegacyReference.ValidUntil
			}
		}
		proof = append(proof, []any{r.SourceVersion, r.Available, privateProof})
		if own {
			owned = append(owned, r)
		}
		if r.Available && (own || r.Precision != "none") {
			if r.ValidUntil.Before(out.ValidUntil) {
				out.ValidUntil = r.ValidUntil
			}
		}
		if !r.Available || r.Precision == "none" {
			continue
		}
		p := content.PublicMomentLocation{ReferenceID: r.ReferenceID, ReferenceName: r.ReferenceName, Attribution: r.Attribution, SourceURL: r.SourceURL, ID: r.ID, DisplayOrder: len(public), Kind: r.Kind, Precision: r.Precision, CityID: r.CityID, CityName: r.CityName, PlaceID: r.PlaceID, PlaceName: r.PlaceName, Region: r.Region, CountryCode: r.CountryCode, SourceVersion: r.SourceVersion, Anchor: r.Anchor}
		if p.Precision == "city" {
			p.Kind = "CITY"
			p.PlaceID, p.PlaceName = "", ""
			p.Anchor = nil
		}
		public = append(public, p)
	}
	if a.Actor.ID != "" {
		var deadline time.Time
		var session string
		e = tx.QueryRow(ctx, `SELECT least(s.expires_at,s.idle_expires_at),jsonb_build_array(s.id,s.authorization_generation,a.xmin::text)::text FROM sessions s JOIN accounts a ON a.id=s.account_id WHERE s.token_sha256=$1 AND s.account_id=$2 AND s.revoked_at IS NULL AND a.status='active' AND least(s.expires_at,s.idle_expires_at)>clock_timestamp()`, a.SessionDigest[:], a.Actor.ID).Scan(&deadline, &session)
		if e != nil {
			return out, "", identity.ErrUnauthorized
		}
		if deadline.Before(out.ValidUntil) {
			out.ValidUntil = deadline.UTC()
		}
		proof = append(proof, session)
	}
	if own {
		out.Items = owned
	} else {
		out.Items = public
	}
	out.SourceVersion = content.MomentControlHash(proof)
	return out, out.SourceVersion, nil
}
func (s *Store) CaptureMomentLocations(ctx context.Context, a content.PublicMomentAccess, id string, own bool) (content.MomentLocationsReceipt, error) {
	r := content.MomentLocationsReceipt{Own: own}
	if own && a.Actor.ID == "" {
		return r, identity.ErrUnauthorized
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, momentPhotoPublicAccess(a), id)
	if e != nil {
		return r, e
	}
	defer tx.Rollback(context.Background())
	r.Frame = frame
	r.View, r.Proof, e = readMomentLocationsView(ctx, tx, a, id, own)
	if e != nil {
		return r, e
	}
	if e = s.finishPublicMomentPhoto(ctx, tx, momentPhotoPublicAccess(a), frame, func() error {
		next, proof, e := readMomentLocationsView(ctx, tx, a, id, own)
		if e != nil {
			return e
		}
		if proof != r.Proof || next.ValidUntil.Before(r.View.ValidUntil) || !r.View.ValidUntil.After(next.ObservedAt) {
			return identity.ErrControlChanged
		}
		return nil
	}); e != nil {
		return r, e
	}
	r.Seal, e = locationReceiptSeal(a, r)
	return r, e
}
func (s *Store) FinalizeMomentLocations(ctx context.Context, a content.PublicMomentAccess, old content.MomentLocationsReceipt) (content.MomentLocationsView, error) {
	seal, e := locationReceiptSeal(a, old)
	if e != nil || !hmac.Equal([]byte(seal), []byte(old.Seal)) || !old.View.ValidUntil.After(time.Now().UTC()) {
		return content.MomentLocationsView{}, identity.ErrControlChanged
	}
	if a.Actor.ID != "" {
		if first, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, a.Actor); present && (!valid || first != old.Frame) {
			return content.MomentLocationsView{}, identity.ErrControlChanged
		}
		ctx = identity.WithHumanControlFrame(ctx, a.SessionDigest, a.Actor, old.Frame)
	} else if old.Frame != (identity.HumanControlFrame{}) {
		return content.MomentLocationsView{}, identity.ErrControlChanged
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, momentPhotoPublicAccess(a), old.View.MomentID)
	if e != nil {
		return content.MomentLocationsView{}, e
	}
	defer tx.Rollback(context.Background())
	if frame != old.Frame {
		return content.MomentLocationsView{}, identity.ErrControlChanged
	}
	e = s.finishPublicMomentPhoto(ctx, tx, momentPhotoPublicAccess(a), frame, func() error {
		next, proof, e := readMomentLocationsView(ctx, tx, a, old.View.MomentID, old.Own)
		if e != nil {
			return e
		}
		if proof != old.Proof || next.ValidUntil.Before(old.View.ValidUntil) || !old.View.ValidUntil.After(next.ObservedAt) {
			return identity.ErrControlChanged
		}
		return nil
	})
	return old.View, e
}

func enrichMomentLocationPrivateReference(ctx context.Context, f humanWriteFrame, mid string, r *content.MomentLocationResolved) error {
	if r.Available || r.Kind != "PLACE" || f.a.ID == "" {
		return nil
	}
	v, e := legacyPlaceReferenceTx(ctx, f.tx, f.a.ID, lpr.Reference{Kind: "moment", RecordID: mid, PlaceID: r.PlaceID}, r.ObservedAt)
	if errors.Is(e, lpr.ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	r.LegacyReference = &v.Reference
	if v.Reference.ValidUntil.Before(r.ValidUntil) {
		r.ValidUntil = v.Reference.ValidUntil
	}
	return nil
}
