package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	lpr "github.com/birdtie/birdtie/apps/api/internal/legacyplacereference"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
)

var _ lpr.HumanStore = (*Store)(nil)

// Each read joins the immutable original owner and source frame to an exact
// current native relationship. Knowing a migrated Place UUID is never a grant.
func legacyPlaceReferenceSQL() string {
	return `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),
 mapped_places AS(SELECT run_id,source_key,source_sha256,target_id,xmin::text stamp FROM legacy_native_adaptation_entities WHERE kind='place' UNION ALL SELECT run_id,source_key,source_sha256,target_id::text,xmin::text FROM legacy_native_context_entities WHERE kind='unplaced_place'),
 mapped_records AS(SELECT run_id,kind,source_key,source_sha256,target_id,owner_account_id,xmin::text stamp FROM legacy_native_adaptation_entities WHERE kind IN('saved_place','theme_map','moment') UNION ALL SELECT run_id,kind,source_key,source_sha256,target_id::text,owner_account_id,xmin::text FROM legacy_native_context_entities WHERE kind='saved_place'),
 archived_records AS(SELECT run_id,kind,source_key,owner_account_id,source_sha256,deleted,payload,source_system,source_owner_id,xmin::text stamp FROM legacy_personal_archives UNION ALL SELECT n.run_id,'moment',n.source_note_id::text,n.owner_account_id,n.source_sha256,n.deleted,n.source_relationships,n.source_system,u.source_id,n.xmin::text FROM legacy_note_archives n JOIN legacy_entity_mappings u ON u.run_id=n.run_id AND u.entity_kind='user' AND u.target_id=n.owner_account_id AND u.owner_account_id=n.owner_account_id)
 SELECT p.name,p.category_code,p.summary,coalesce(p.address_label,'') address_label,coalesce(p.city_id,'') city_id,coalesce(c.name,'') city_name,
 CASE WHEN p.coordinate_system='wgs84' AND p.location_precision='point' THEN p.latitude END latitude,
 CASE WHEN p.coordinate_system='wgs84' AND p.location_precision='point' THEN p.longitude END longitude,
 cl.at observed_at,least(cl.at+interval '30 seconds',p.expires_at,
 CASE WHEN $2='moment' THEN(SELECT min(least(i.retain_until,v.retain_until)) FROM private_moment_media_contexts ctx JOIN private_moment_media_context_places cp ON cp.context_id=ctx.id AND cp.place_id=p.id JOIN media_assets asset ON asset.id=coalesce(ctx.image_id,ctx.video_id) AND asset.owner_account_id=$1 AND asset.state='ready_private' LEFT JOIN private_moment_images i ON i.id=ctx.image_id AND i.owner_account_id=$1 AND i.status='ready_private' AND i.derivative IS NOT NULL AND i.retain_until>cl.at LEFT JOIN private_moment_videos v ON v.id=ctx.video_id AND v.owner_account_id=$1 AND v.status='ready_private' AND v.content IS NOT NULL AND v.retain_until>cl.at WHERE ctx.moment_id=$3::uuid AND ctx.owner_account_id=$1 AND ctx.status='ACTIVE' AND(i.id IS NOT NULL OR v.id IS NOT NULL)) END,` + currentRetainedPlaceDeadlineSQL("p", "cl.at") + `,(SELECT min(least(expires_at,idle_expires_at)) FROM sessions WHERE account_id=$1 AND revoked_at IS NULL AND expires_at>cl.at AND idle_expires_at>cl.at)) valid_until,
 jsonb_build_array(p.xmin::text,c.xmin::text,own.xmin::text,pm.stamp,rm.stamp,nr.xmin::text,ar.stamp,archive_run.xmin::text,u.xmin::text,
 ` + retainedPlaceReadBindingSQL("p") + `,
 CASE WHEN $2='saved_place' THEN (SELECT jsonb_build_array(legacy_saved.xmin::text,legacy_saved.owner_account_id,legacy_saved.place_id) FROM saved_items legacy_saved WHERE legacy_saved.id=$3::uuid)
 WHEN $2='moment' THEN(SELECT jsonb_build_array(m.xmin::text,m.revision,m.status,(SELECT version FROM moment_location_epochs WHERE moment_id=m.id),(SELECT coalesce(jsonb_agg(jsonb_build_array(l.id,l.xmin::text)ORDER BY l.id),'[]') FROM moment_location_links l WHERE l.moment_id=m.id AND l.place_id=p.id AND l.status='ACTIVE'),(SELECT coalesce(jsonb_agg(jsonb_build_array(ctx.id,ctx.xmin::text,cp.id,cp.xmin::text,
 (SELECT jsonb_build_array(i.xmin::text,asset.xmin::text) FROM private_moment_images i JOIN media_assets asset ON asset.id=i.id WHERE i.id=ctx.image_id),
 (SELECT jsonb_build_array(v.xmin::text,asset.xmin::text) FROM private_moment_videos v JOIN media_assets asset ON asset.id=v.id WHERE v.id=ctx.video_id))ORDER BY ctx.id,cp.position),'[]') FROM private_moment_media_contexts ctx JOIN private_moment_media_context_places cp ON cp.context_id=ctx.id WHERE ctx.moment_id=m.id AND ctx.owner_account_id=$1 AND ctx.status='ACTIVE' AND cp.place_id=p.id)) FROM moments m WHERE m.id=$3::uuid)
 ELSE (SELECT jsonb_build_array(t.xmin::text,e.xmin::text,t.revision,t.status,t.visibility,e.place_id) FROM theme_maps t JOIN theme_map_places e ON e.theme_map_id=t.id AND e.place_id=p.id WHERE t.id=$3::uuid) END)::text source
 FROM places p JOIN mapped_places pm ON pm.target_id=p.id::text
 JOIN legacy_native_adaptation_runs nr ON nr.run_id=pm.run_id
 JOIN mapped_records rm ON rm.kind=$2 AND rm.target_id=$3::text AND rm.owner_account_id=$1 AND rm.run_id=pm.run_id
 JOIN archived_records ar ON ar.kind=rm.kind AND ar.source_key=rm.source_key AND ar.owner_account_id=$1 AND ar.run_id=rm.run_id AND ar.source_sha256=rm.source_sha256 AND NOT ar.deleted
 JOIN legacy_import_runs archive_run ON archive_run.run_id=ar.run_id
 JOIN legacy_entity_mappings u ON u.entity_kind='user' AND u.source_system=ar.source_system AND u.source_id=ar.source_owner_id AND u.target_id=$1 AND u.owner_account_id=$1 AND u.run_id=ar.run_id
 JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active'
 LEFT JOIN cities c ON c.id=p.city_id CROSS JOIN cl
 WHERE p.id=$4::uuid AND p.publication_status='draft' AND(p.expires_at IS NULL OR p.expires_at>cl.at)
 AND ` + currentRetainedPlaceVisibilitySQL("p", "cl.at") + `
 AND((($2='saved_place' AND ar.payload->>'sourcePlaceId'=pm.source_key AND EXISTS(SELECT 1 FROM saved_items legacy_saved WHERE legacy_saved.id=$3::uuid AND legacy_saved.owner_account_id=$1 AND legacy_saved.place_id=p.id))
 OR($2='theme_map' AND EXISTS(SELECT 1 FROM jsonb_array_elements(ar.payload->'entries') original WHERE original->>'sourcePlaceId'=pm.source_key)
 AND EXISTS(SELECT 1 FROM theme_maps t JOIN theme_map_places e ON e.theme_map_id=t.id AND e.place_id=p.id AND e.city_id=p.city_id WHERE t.id=$3::uuid AND t.owner_account_id=$1 AND t.visibility='private' AND t.status='draft')))
 OR($2='moment' AND ar.kind='moment' AND EXISTS(SELECT 1 FROM moments m WHERE m.id=$3::uuid AND m.author_account_id=$1 AND m.status<>'withdrawn' AND(
 (EXISTS(SELECT 1 FROM moment_location_links l WHERE l.moment_id=m.id AND l.place_id=p.id AND l.status='ACTIVE') AND EXISTS(SELECT 1 FROM jsonb_array_elements(ar.payload->'places') original WHERE original->>'sourcePlaceId'=pm.source_key))
 OR EXISTS(SELECT 1 FROM private_moment_media_contexts ctx JOIN private_moment_media_context_places cp ON cp.context_id=ctx.id AND cp.place_id=p.id JOIN legacy_native_context_entities ce ON ce.kind='media_context' AND ce.target_id=ctx.id AND ce.owner_account_id=$1 AND ce.run_id=ar.run_id AND ce.source_parent_key=ar.source_key AND ce.source_parent_sha256=ar.source_sha256 WHERE ctx.moment_id=m.id AND ctx.status='ACTIVE' AND ctx.owner_account_id=$1 AND(
 (ctx.asset_kind='IMAGE' AND EXISTS(SELECT 1 FROM private_moment_images i WHERE i.id=ctx.image_id AND i.owner_account_id=$1 AND i.moment_id=m.id AND i.status='ready_private' AND i.derivative IS NOT NULL AND i.retain_until>cl.at AND EXISTS(SELECT 1 FROM media_assets asset WHERE asset.id=i.id AND asset.owner_account_id=$1 AND asset.state='ready_private')))OR(ctx.asset_kind='VIDEO' AND EXISTS(SELECT 1 FROM private_moment_videos v WHERE v.id=ctx.video_id AND v.owner_account_id=$1 AND v.moment_id=m.id AND v.status='ready_private' AND v.content IS NOT NULL AND v.retain_until>cl.at AND EXISTS(SELECT 1 FROM media_assets asset WHERE asset.id=v.id AND asset.owner_account_id=$1 AND asset.state='ready_private')))))))))
 FOR SHARE OF p,own`
}

func legacyPlaceReferenceSQLFor(owner, kind, record, place string) string {
	return strings.NewReplacer("$1", owner, "$2", kind, "$3", record, "$4", place).Replace(legacyPlaceReferenceSQL())
}
func legacyPlaceReferenceSummarySQL(alias, kind, record, place, at string) string {
	return `CASE WHEN ` + alias + `.name IS NOT NULL THEN jsonb_build_object('kind',` + kind + `,'recordId',` + record + `,'placeId',` + place + `,'observedAt',` + at + `,'validUntil',least(` + alias + `.valid_until,` + at + `+interval '30 seconds'),'sourceVersion',encode(sha256(convert_to(` + alias + `.source,'UTF8')),'hex')) END`
}

func legacyPlaceReferenceTx(ctx context.Context, tx pgx.Tx, owner string, ref lpr.Reference, at time.Time) (lpr.Detail, error) {
	v := lpr.Detail{SchemaVersion: lpr.SchemaVersion, Reference: lpr.Summary{Reference: ref}, LocationPrecision: "none"}
	if !ref.Valid() {
		return v, lpr.ErrInvalid
	}
	var raw string
	e := tx.QueryRow(ctx, legacyPlaceReferenceSQL(), owner, ref.Kind, ref.RecordID, ref.PlaceID).Scan(&v.Name, &v.CategoryCode, &v.Summary, &v.AddressLabel, &v.CityID, &v.CityName, &v.Latitude, &v.Longitude, &v.Reference.ObservedAt, &v.Reference.ValidUntil, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, lpr.ErrNotFound
	}
	if e != nil {
		return v, lpr.ErrUnavailable
	}
	v.Reference.ObservedAt = v.Reference.ObservedAt.UTC()
	v.Reference.ValidUntil = v.Reference.ValidUntil.UTC()
	if !at.IsZero() {
		v.Reference.ObservedAt = at.UTC()
		if bound := at.Add(30 * time.Second); bound.Before(v.Reference.ValidUntil) {
			v.Reference.ValidUntil = bound
		}
	}
	v.Reference.SourceVersion = identity.HumanControlTextHash(raw)
	if v.Latitude != nil && v.Longitude != nil {
		v.CoordinateSystem, v.LocationPrecision = "wgs84", "point"
	}
	if !v.Valid(ref, time.Now().UTC()) {
		return v, lpr.ErrChanged
	}
	return v, nil
}

type legacyPlaceReferenceProof struct {
	s     *Store
	d     [32]byte
	a     identity.Actor
	frame identity.HumanControlFrame
	value lpr.Detail
}

func (p *legacyPlaceReferenceProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.value.Reference.Valid(time.Now().UTC()) {
		return lpr.ErrChanged
	}
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	_, e := p.s.readHumanLegacyPlaceReference(ctx, p.d, p.a, p.value.Reference.Reference, &p.value.Reference)
	return e
}
func (s *Store) readHumanLegacyPlaceReference(ctx context.Context, d [32]byte, a identity.Actor, ref lpr.Reference, expected *lpr.Summary) (lpr.Detail, error) {
	if !ref.Valid() {
		return lpr.Detail{}, lpr.ErrInvalid
	}
	ctx, frame, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return lpr.Detail{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return lpr.Detail{}, lpr.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, d: d, a: a, session: frame}
	v, e := legacyPlaceReferenceTx(ctx, tx, a.ID, ref, time.Time{})
	if e != nil {
		return v, e
	}
	if expected != nil && (expected.SourceVersion != v.Reference.SourceVersion || !expected.ValidUntil.After(v.Reference.ObservedAt) || v.Reference.ValidUntil.Before(expected.ValidUntil)) {
		return v, lpr.ErrChanged
	}
	if expected != nil {
		v.Reference.ObservedAt, v.Reference.ValidUntil = expected.ObservedAt, expected.ValidUntil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, v.Reference.ValidUntil, func() error {
		fresh, e := legacyPlaceReferenceTx(ctx, tx, a.ID, ref, v.Reference.ObservedAt)
		if e != nil {
			return e
		}
		if fresh.Reference.SourceVersion != v.Reference.SourceVersion || fresh.Reference.ValidUntil.Before(v.Reference.ValidUntil) {
			return lpr.ErrChanged
		}
		return nil
	})); e != nil {
		return lpr.Detail{}, e
	}
	v.ResponseProof = &legacyPlaceReferenceProof{s, d, a, frame, v}
	return v, nil
}
func (s *Store) ReadHumanLegacyPlaceReference(ctx context.Context, d [32]byte, a identity.Actor, ref lpr.Reference) (lpr.Detail, error) {
	return s.readHumanLegacyPlaceReference(ctx, d, a, ref, nil)
}

// Only an already imported owned private collection can retain its original
// draft references. Removed IDs cannot be re-added through this exception.
// The target revision fences the person's own text/order changes separately;
// this authority fingerprint excludes the entry row that the write replaces.
func legacyCollectionRetainedAuthorityTx(ctx context.Context, tx pgx.Tx, owner, id string, input content.CollectionInput) (bool, string, *time.Time, error) {
	if input.Kind != "theme_map" || id == "" {
		return false, "", nil, nil
	}
	var imported bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_native_adaptation_entities n JOIN theme_maps t ON t.id::text=n.target_id WHERE n.kind='theme_map' AND n.owner_account_id=$1 AND n.target_id=$2 AND t.owner_account_id=$1 AND t.status='draft' AND t.visibility='private' AND t.city_id=$3)`, owner, id, input.CityID).Scan(&imported); e != nil {
		return true, "", nil, content.ErrUnavailable
	}
	if !imported {
		return false, "", nil, nil
	}
	var cityStamp string
	var deadline *time.Time
	if e := tx.QueryRow(ctx, `SELECT xmin::text,expires_at FROM cities WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, input.CityID).Scan(&cityStamp, &deadline); e != nil {
		return true, "", nil, content.ErrConflict
	}
	ids := make([]string, 0, len(input.Entries))
	byID := map[string]content.CollectionEntry{}
	for _, entry := range input.Entries {
		ids = append(ids, entry.PlaceID)
		byID[entry.PlaceID] = entry
	}
	sort.Strings(ids)
	values := []any{"owned-imported-theme-retain-v1", id, input.CityID, cityStamp}
	for _, place := range ids {
		var stamp string
		var until *time.Time
		e := tx.QueryRow(ctx, `SELECT concat_ws(':',p.xmin::text,`+retainedPlaceReadBindingSQL("p")+`),`+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+` FROM places p WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` FOR SHARE`, place, input.CityID).Scan(&stamp, &until)
		if errors.Is(e, pgx.ErrNoRows) {
			if byID[place].LocationVisibility != "hidden" {
				return true, "", nil, content.ErrConflict
			}
			query := legacyPlaceReferenceSQLFor("$1", "'theme_map'", "$2", "$3")
			e = tx.QueryRow(ctx, `SELECT (r.source::jsonb - (jsonb_array_length(r.source::jsonb)-1))::text,r.valid_until FROM (`+query+`) r`, owner, id, place).Scan(&stamp, &until)
			if e == nil {
				stamp = identity.HumanControlTextHash(stamp)
			}
		}
		if e != nil {
			return true, "", nil, content.ErrConflict
		}
		values = append(values, []any{place, stamp})
		momentControlAddDeadline(&deadline, until)
	}
	return true, content.MomentControlHash(values), deadline, nil
}
