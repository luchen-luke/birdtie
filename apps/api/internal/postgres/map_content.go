package postgres

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.HumanMapContentStore = (*Store)(nil)

type mapContentPlace struct {
	pin                     content.MapContentPin
	stamp                   string
	catalog, own, published bool
	until                   *time.Time
	publicUntil             *time.Time
}
type mapContentMoment struct {
	v            content.MapContentMoment
	owner, stamp string
	places       []string
}
type mapContentReadFilter struct{ PlaceIDs, MomentIDs []string }
type mapContentSnapshot struct {
	filter    *mapContentReadFilter
	at, until time.Time
	source    string
	places    map[string]*mapContentPlace
	moments   map[string]*mapContentMoment
	order     []string
	// Request-local memoization only. Every new read/proof still rebuilds the
	// authority snapshot; this never grants an asset beyond its current lease.
	references map[string][]mapContentAsset
	media      map[string][]mapContentAsset
}

// Legacy PUBLIC is an independent, explicitly preserved read grant. It does
// not turn a private native draft into a publication. Initial suspended import
// accounts are distinguished from a later suspension by their original xmin.
const mapContentMomentsSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at),candidates AS(
 SELECT m.*,a.status actor_status,a.xmin::text actor_stamp,
 CASE WHEN m.status='draft' AND m.visibility='private' AND m.author_account_id=$1 THEN 'SELF_PRIVATE'
 WHEN m.status='published' AND m.visibility='public' AND a.status='active' AND m.author_confirmed_at IS NOT NULL AND m.published_at<=cl.at AND m.author_confirmed_at<=m.published_at AND birdtie_moment_public_binding(m.id,$1,cl.at) IS NOT NULL THEN 'BIRDTIE_PUBLIC_MOMENT'
 WHEN m.status='draft' AND m.visibility='private' AND m.revision=1 AND original.run_id IS NOT NULL AND n.source_visibility='public' AND n.source_moderation_status='ok' AND NOT n.deleted
 AND m.title=n.title AND m.body=n.body AND u.source_status='active'
 AND(a.status='active' OR(a.status='suspended' AND a.xmin=original.xmin)) THEN 'CIVU_PUBLIC_MOMENT' END visibility_source,
 jsonb_build_array(m.xmin::text,a.xmin::text,n.xmin::text,x.xmin::text,u.xmin::text,r.xmin::text,original.xmin::text,
 (SELECT jsonb_agg(jsonb_build_array(owner_account_id,target_account_id,version,xmin::text)ORDER BY owner_account_id,target_account_id) FROM person_block_control_epochs WHERE(owner_account_id=$1 AND target_account_id=a.id)OR(owner_account_id=a.id AND target_account_id=$1)),
 (SELECT jsonb_build_array(version,xmin::text) FROM moment_location_epochs WHERE moment_id=m.id),
 (SELECT jsonb_agg(jsonb_build_array(l.id,l.xmin::text)ORDER BY l.id) FROM moment_location_links l WHERE l.moment_id=m.id AND l.status='ACTIVE'))::text stamp
 FROM moments m JOIN accounts a ON a.id=m.author_account_id AND a.account_type='person' CROSS JOIN cl
 LEFT JOIN legacy_native_adaptation_entities x ON x.kind='moment' AND x.target_id=m.id::text AND x.owner_account_id=m.author_account_id
 LEFT JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key AND n.source_sha256=x.source_sha256 AND n.owner_account_id=x.owner_account_id
 LEFT JOIN legacy_entity_mappings u ON u.run_id=n.run_id AND u.entity_kind='user' AND u.target_id=n.owner_account_id AND u.owner_account_id=n.owner_account_id
 LEFT JOIN legacy_map_visibility_runs r ON r.run_id=n.run_id
 LEFT JOIN legacy_import_runs original ON original.run_id=r.run_id AND original.source_file_sha256=r.source_file_sha256 AND original.manifest_file_sha256=r.manifest_file_sha256 AND original.snapshot_sha256=r.snapshot_sha256
 WHERE m.status<>'withdrawn' AND a.status IN('active','suspended')
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$1 AND b.blocked_account_id=a.id)OR(b.blocker_account_id=a.id AND b.blocked_account_id=$1))
 ) SELECT id::text,author_account_id::text,revision,status,visibility,title,body,created_at,visibility_source,stamp
 FROM candidates WHERE visibility_source IS NOT NULL AND(visibility_source<>'CIVU_PUBLIC_MOMENT' OR stamp IS NOT NULL) ORDER BY created_at DESC,id LIMIT 10001`

func mapContentCurrentMomentPlacesSQL() string {
	return `WITH original_links AS(
 SELECT l.id FROM moment_location_links l
 JOIN legacy_native_adaptation_entities mapping ON mapping.kind='moment' AND mapping.target_id=l.moment_id::text
 JOIN legacy_note_archives archive ON archive.run_id=mapping.run_id AND archive.source_note_id::text=mapping.source_key AND archive.owner_account_id=mapping.owner_account_id AND archive.source_sha256=mapping.source_sha256 AND NOT archive.deleted
 JOIN legacy_map_place_facts pf ON pf.run_id=mapping.run_id AND pf.place_id=l.place_id
 WHERE EXISTS(SELECT 1 FROM jsonb_array_elements(archive.source_relationships->'places') r WHERE r->>'sourcePlaceId'=pf.source_place_id::text)
 AND(EXISTS(SELECT 1 FROM legacy_native_context_entities proof WHERE proof.run_id=mapping.run_id AND proof.kind='note_location' AND proof.target_id=l.id AND proof.owner_account_id=mapping.owner_account_id AND proof.source_parent_key=mapping.source_key AND proof.source_parent_sha256=mapping.source_sha256)
 OR(l.id=md5('moment-locations-v1:'||l.moment_id::text)::uuid AND EXISTS(SELECT 1 FROM moment_location_compatibility_backfill b WHERE b.moment_id=l.moment_id AND b.place_id=l.place_id AND b.revision=1)))
 ) SELECT DISTINCT q.mid::text,q.pid::text,q.stamp,q.original_relation,q.private_context,q.native_public_relation FROM(
 SELECT l.moment_id mid,l.place_id pid,jsonb_build_array(l.xmin::text)::text stamp,original.id IS NOT NULL original_relation,false private_context,l.precision='place' native_public_relation FROM moment_location_links l LEFT JOIN original_links original ON original.id=l.id WHERE l.status='ACTIVE' AND l.kind='PLACE' AND(l.precision='place' OR original.id IS NOT NULL)
 UNION ALL SELECT c.moment_id,p.place_id,jsonb_build_array(c.xmin::text,p.xmin::text,i.xmin::text,v.xmin::text,asset.xmin::text)::text,
 EXISTS(SELECT 1 FROM legacy_native_context_entities ce JOIN legacy_note_archives archive ON archive.run_id=ce.run_id AND archive.source_note_id::text=ce.source_parent_key AND archive.source_sha256=ce.source_parent_sha256 AND archive.owner_account_id=ce.owner_account_id AND NOT archive.deleted
 JOIN legacy_native_adaptation_entities mm ON mm.kind='moment' AND mm.run_id=archive.run_id AND mm.source_key=archive.source_note_id::text AND mm.source_sha256=archive.source_sha256 AND mm.owner_account_id=archive.owner_account_id AND mm.target_id=c.moment_id::text
 JOIN legacy_native_context_entities cp ON cp.kind='media_place' AND cp.run_id=ce.run_id AND cp.target_id=p.id AND cp.owner_account_id=ce.owner_account_id AND cp.source_parent_key=ce.source_parent_key AND cp.source_parent_sha256=ce.source_parent_sha256
 WHERE ce.kind='media_context' AND ce.target_id=c.id AND ce.owner_account_id=c.owner_account_id) original_relation,true,false
 FROM private_moment_media_contexts c JOIN private_moment_media_context_places p ON p.context_id=c.id
 JOIN media_assets asset ON asset.id=coalesce(c.image_id,c.video_id) AND asset.owner_account_id=c.owner_account_id AND asset.state='ready_private'
 LEFT JOIN private_moment_images i ON i.id=c.image_id AND i.owner_account_id=c.owner_account_id AND i.moment_id=c.moment_id AND i.status='ready_private' AND i.retain_until>clock_timestamp() AND i.derivative IS NOT NULL
 LEFT JOIN private_moment_videos v ON v.id=c.video_id AND v.owner_account_id=c.owner_account_id AND v.moment_id=c.moment_id AND v.status='ready_private' AND v.retain_until>clock_timestamp() AND v.content IS NOT NULL
 WHERE c.status='ACTIVE' AND(i.id IS NOT NULL OR v.id IS NOT NULL)
 )q WHERE q.mid=ANY($1::uuid[]) ORDER BY 1,2,3,4,5,6`
}

func mapContentPlaceSQL() string {
	return `SELECT p.id::text,p.name,p.category_code,p.city_id,
 CASE WHEN p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN p.latitude ELSE CASE WHEN original.run_id IS NOT NULL AND f.native_binding=birdtie_legacy_map_place_binding(p) THEN f.latitude END END,
 CASE WHEN p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL THEN p.longitude ELSE CASE WHEN original.run_id IS NOT NULL AND f.native_binding=birdtie_legacy_map_place_binding(p) THEN f.longitude END END,
 coalesce(original.run_id IS NOT NULL AND f.public_catalog AND f.native_binding=birdtie_legacy_map_place_binding(p) AND(p.city_id IS NULL OR(c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()))),false),
 coalesce(p.publication_status='published' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()),false),
 least(p.expires_at,` + currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()") + `),c.expires_at,
 jsonb_build_array(p.xmin::text,f.xmin::text,r.xmin::text,original.xmin::text,c.xmin::text,c.publication_status,c.expires_at>clock_timestamp(),` + retainedPlaceReadBindingSQL("p") + `)::text
 FROM places p LEFT JOIN legacy_map_place_facts f ON f.place_id=p.id
 LEFT JOIN legacy_map_visibility_runs r ON r.run_id=f.run_id
 LEFT JOIN legacy_import_runs original ON original.run_id=r.run_id AND original.source_file_sha256=r.source_file_sha256 AND original.manifest_file_sha256=r.manifest_file_sha256 AND original.snapshot_sha256=r.snapshot_sha256
 LEFT JOIN cities c ON c.id=p.city_id
 WHERE p.publication_status IN('draft','published') AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND ` + currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()") + `
 ORDER BY p.id LIMIT 10001`
}

func ownMapPlaceRelationsSQL() string {
	return `SELECT place_id::text,jsonb_build_array(xmin::text,'saved')::text,NULL::timestamptz FROM saved_items WHERE owner_account_id=$1 AND place_id IS NOT NULL
 UNION ALL SELECT e.place_id::text,jsonb_build_array(t.xmin::text,e.xmin::text,'theme',` + mapContentBorrowedStampSQL("theme_map", "t.id") + `)::text,` + collectionCopyBorrowedDeadlineSQL("theme_map", "t.id", "e.place_id") + ` FROM theme_maps t JOIN theme_map_places e ON e.theme_map_id=t.id WHERE t.owner_account_id=$1 AND t.visibility='private' AND t.status='draft' AND ` + collectionCopyBorrowedSQL("theme_map", "t.id", "e.place_id", "$1") + `
 UNION ALL SELECT e.place_id::text,jsonb_build_array(j.xmin::text,e.xmin::text,'journey',` + mapContentBorrowedStampSQL("journey", "j.id") + `)::text,` + collectionCopyBorrowedDeadlineSQL("journey", "j.id", "e.place_id") + ` FROM journeys j JOIN journey_itinerary_entries e ON e.journey_id=j.id WHERE j.owner_account_id=$1 AND j.visibility='private' AND j.status='draft' AND e.place_id IS NOT NULL AND ` + collectionCopyBorrowedSQL("journey", "j.id", "e.place_id", "$1") + `
 UNION ALL SELECT r.target_id::text,jsonb_build_array(r.xmin::text,l.xmin::text,m.xmin::text,i.xmin::text,asset.xmin::text,bm.xmin::text,'own-image')::text,i.retain_until FROM legacy_reference_media_relations r JOIN private_reference_image_links l ON l.id=r.id AND l.status='ACTIVE' JOIN private_reference_image_materials m ON m.id=l.material_id AND m.owner_account_id=r.owner_account_id AND m.status='READY_PRIVATE' LEFT JOIN private_moment_images i ON i.id=m.backing_image_id AND i.owner_account_id=r.owner_account_id LEFT JOIN media_assets asset ON asset.id=i.id AND asset.owner_account_id=i.owner_account_id LEFT JOIN moments bm ON bm.id=i.moment_id AND bm.author_account_id=i.owner_account_id WHERE r.owner_account_id=$1 AND r.target_type='PLACE' AND NOT r.tombstone AND r.review_status<>'removed' AND((m.backing_image_id IS NULL AND m.content IS NOT NULL)OR(i.status='ready_private' AND i.retain_until>clock_timestamp() AND i.derivative IS NOT NULL AND asset.state='ready_private' AND bm.status='draft' AND bm.visibility='private'))
 ORDER BY 1,2 LIMIT 10001`
}
func mapContentBorrowedStampSQL(kind, dest string) string {
	return `(SELECT jsonb_agg(jsonb_build_array(original.xmin::text,original.origin_authority,` + collectionCopyAuthoritySQL(kind, "original.source_id", "$1") + `,grant_row.xmin::text,grant_row.grant_version,grant_row.allow_private_place_copy)ORDER BY original.destination_id)FROM personal_collection_copy_origins original LEFT JOIN personal_collection_copy_grants grant_row ON grant_row.kind=original.source_kind AND grant_row.target_id=original.source_id WHERE original.destination_kind='` + kind + `' AND original.destination_id=` + dest + `)`
}

// Independent attachment changes must retire a page/cover proof even when the
// Moment revision and its location links did not change. These are small row
// facts only: no private original, derivative or video bytea is loaded here.
const mapContentMediaFactsSQL = `SELECT coalesce(jsonb_agg(token ORDER BY kind,id),'[]')::text FROM(
 SELECT 'PRIVATE_IMAGE' kind,i.id::text id,jsonb_build_array('moment',i.moment_id,i.id,i.xmin::text,i.status,i.revision,i.derivative_sha256,i.retain_until>clock_timestamp(),a.xmin::text,a.state) token FROM private_moment_images i LEFT JOIN media_assets a ON a.id=i.id AND a.owner_account_id=i.owner_account_id WHERE i.moment_id=ANY($1::uuid[])
 UNION ALL SELECT 'PRIVATE_VIDEO',v.id::text,jsonb_build_array('moment',v.moment_id,v.id,v.xmin::text,v.status,v.revision,v.content_sha256,v.poster_sha256,v.retain_until>clock_timestamp(),a.xmin::text,a.state) FROM private_moment_videos v LEFT JOIN media_assets a ON a.id=v.id AND a.owner_account_id=v.owner_account_id WHERE v.moment_id=ANY($1::uuid[])
 UNION ALL SELECT 'PUBLIC_SET',s.moment_id::text,jsonb_build_array('moment',s.moment_id,s.moment_id,s.xmin::text,s.version,s.status,s.approved_moment_revision,s.source_binding) FROM public_moment_media_sets s WHERE s.moment_id=ANY($1::uuid[])
 UNION ALL SELECT 'PUBLIC_ATTACHMENT',concat_ws(':',a.moment_id,a.set_version,a.position),jsonb_build_array('moment',a.moment_id,a.xmin::text,a.media_id,a.media_version,a.rights,a.retention_until>clock_timestamp(),a.display IS NOT NULL,a.thumbnail IS NOT NULL) FROM public_moment_media_attachments a JOIN public_moment_media_sets s ON s.moment_id=a.moment_id AND s.version=a.set_version WHERE a.moment_id=ANY($1::uuid[])
 UNION ALL SELECT 'PUBLIC_VIDEO',concat_ws(':',p.moment_id,p.set_version,p.position),jsonb_build_array('moment',p.moment_id,p.xmin::text,p.sha256,p.contract,p.content IS NOT NULL) FROM public_moment_media_video_payloads p JOIN public_moment_media_sets s ON s.moment_id=p.moment_id AND s.version=p.set_version WHERE p.moment_id=ANY($1::uuid[])
 UNION ALL SELECT 'REFERENCE',r.id::text,jsonb_build_array('place',r.target_id,r.xmin::text,l.xmin::text,m.xmin::text,i.xmin::text,asset.xmin::text,contributor.xmin::text,l.status,m.status,i.status,i.retain_until>clock_timestamp(),asset.state,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(e.owner_account_id,e.target_account_id,e.version,e.xmin::text)ORDER BY e.owner_account_id,e.target_account_id),'[]')FROM person_block_control_epochs e WHERE(e.owner_account_id=$2 AND e.target_account_id=r.owner_account_id)OR(e.owner_account_id=r.owner_account_id AND e.target_account_id=$2)),
 EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$2 AND b.blocked_account_id=r.owner_account_id)OR(b.blocker_account_id=r.owner_account_id AND b.blocked_account_id=$2)))
 FROM legacy_reference_media_relations r JOIN private_reference_image_links l ON l.id=r.id JOIN private_reference_image_materials m ON m.id=l.material_id
 JOIN accounts contributor ON contributor.id=r.owner_account_id LEFT JOIN private_moment_images i ON i.id=m.backing_image_id LEFT JOIN media_assets asset ON asset.id=i.id
 WHERE r.target_type='PLACE' AND r.target_id=ANY($3::uuid[]) AND(r.owner_account_id=$2 OR r.review_status='approved')
 )facts`

func readMapContentSnapshot(ctx context.Context, f humanWriteFrame, filters ...*mapContentReadFilter) (mapContentSnapshot, error) {
	v := mapContentSnapshot{places: map[string]*mapContentPlace{}, moments: map[string]*mapContentMoment{}, order: []string{}}
	if len(filters) > 0 {
		v.filter = filters[0]
	}
	if f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&v.at) != nil {
		return v, content.ErrUnavailable
	}
	v.at = v.at.UTC()
	v.until = v.at.Add(30 * time.Second)
	if e := ownedContextLeaseTx(ctx, f, &v.until); e != nil {
		return v, e
	}
	var principal string
	if f.tx.QueryRow(ctx, `SELECT concat_ws(':',id,status,xmin::text) FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principal) != nil {
		return v, identity.ErrControlChanged
	}
	facts := []any{content.MapContentSchema, historyReader(f), principal}
	placeSQL := mapContentPlaceSQL()
	var placeArgs []any
	if v.filter != nil {
		placeSQL = strings.Replace(placeSQL, "WHERE p.publication_status IN", "WHERE p.id=ANY($1::uuid[]) AND p.publication_status IN", 1)
		placeSQL = strings.Replace(placeSQL, " LIMIT 10001", "", 1)
		placeArgs = []any{v.filter.PlaceIDs}
	}
	rows, e := f.tx.Query(ctx, placeSQL, placeArgs...)
	if e != nil {
		return v, content.ErrUnavailable
	}
	count := 0
	for rows.Next() {
		p := &mapContentPlace{pin: content.MapContentPin{VisibilitySources: []string{}, Moments: []content.MapContentMomentSelector{}}}
		var lat, lng *float64
		if e = rows.Scan(&p.pin.PlaceID, &p.pin.Name, &p.pin.CategoryCode, &p.pin.CityID, &lat, &lng, &p.catalog, &p.published, &p.until, &p.publicUntil, &p.stamp); e != nil {
			break
		}
		count++
		if lat != nil && lng != nil {
			p.pin.Anchor = content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: *lat, Longitude: *lng}
		}
		if p.catalog {
			p.pin.VisibilitySources = append(p.pin.VisibilitySources, "CIVU_PUBLIC_CATALOG")
		}
		v.places[p.pin.PlaceID] = p
		facts = append(facts, []any{p.pin.PlaceID, p.stamp, p.catalog})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || v.filter == nil && count > 10000 {
		return v, content.ErrUnavailable
	}
	momentSQL := mapContentMomentsSQL
	momentArgs := []any{f.a.ID}
	if v.filter != nil {
		momentSQL = strings.Replace(momentSQL, "WHERE m.status<>", "WHERE m.id=ANY($2::uuid[]) AND m.status<>", 1)
		momentSQL = strings.Replace(momentSQL, " LIMIT 10001", "", 1)
		momentArgs = append(momentArgs, v.filter.MomentIDs)
	}
	rows, e = f.tx.Query(ctx, momentSQL, momentArgs...)
	if e != nil {
		return v, content.ErrUnavailable
	}
	for rows.Next() {
		m := &mapContentMoment{v: content.MapContentMoment{SchemaVersion: content.MapContentMomentSchema, ViewerAccountID: f.a.ID, Locations: []content.MapContentLocation{}, Media: []content.MapContentMedia{}, ObservedAt: v.at, ValidUntil: v.until}, places: []string{}}
		if e = rows.Scan(&m.v.ID, &m.owner, &m.v.Revision, &m.v.Status, &m.v.Visibility, &m.v.Title, &m.v.Body, &m.v.CreatedAt, &m.v.VisibilitySource, &m.stamp); e != nil {
			break
		}
		m.v.CreatedAt = m.v.CreatedAt.UTC()
		m.v.CanEdit = m.owner == f.a.ID
		v.moments[m.v.ID] = m
		v.order = append(v.order, m.v.ID)
		facts = append(facts, []any{m.v.ID, m.stamp, m.v.VisibilitySource})
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || v.filter == nil && len(v.order) > 10000 {
		return v, content.ErrUnavailable
	}
	if len(v.order) > 0 {
		rows, e = f.tx.Query(ctx, mapContentCurrentMomentPlacesSQL(), v.order)
		if e != nil {
			return v, content.ErrUnavailable
		}
		for rows.Next() {
			var mid, pid, stamp string
			var originalRelation, privateContext, nativePublicRelation bool
			if e = rows.Scan(&mid, &pid, &stamp, &originalRelation, &privateContext, &nativePublicRelation); e != nil {
				break
			}
			m := v.moments[mid]
			p := v.places[pid]
			if m == nil || p == nil {
				continue
			}
			if m.v.VisibilitySource == "CIVU_PUBLIC_MOMENT" && !originalRelation {
				continue
			}
			if m.v.VisibilitySource == "BIRDTIE_PUBLIC_MOMENT" && m.owner != f.a.ID && (privateContext || !nativePublicRelation) {
				continue
			}
			if m.owner != f.a.ID && ((m.v.VisibilitySource == "CIVU_PUBLIC_MOMENT" && !p.catalog) || (m.v.VisibilitySource == "BIRDTIE_PUBLIC_MOMENT" && !p.published)) {
				continue
			}
			if !containsMapString(m.places, pid) {
				m.places = append(m.places, pid)
			}
			facts = append(facts, []any{mid, pid, stamp})
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return v, content.ErrUnavailable
		}
	}
	ownSQL := ownMapPlaceRelationsSQL()
	ownArgs := []any{f.a.ID}
	if v.filter != nil {
		ownSQL = "SELECT ''::text,''::text,NULL::timestamptz WHERE false"
		ownArgs = nil
	}
	rows, e = f.tx.Query(ctx, ownSQL, ownArgs...)
	if e != nil {
		return v, content.ErrUnavailable
	}
	count = 0
	for rows.Next() {
		var pid, stamp string
		var until *time.Time
		if e = rows.Scan(&pid, &stamp, &until); e != nil {
			break
		}
		count++
		if p := v.places[pid]; p != nil {
			p.own = true
			shrinkMapLease(&v.until, until)
			facts = append(facts, []any{pid, stamp})
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil || v.filter == nil && count > 10000 {
		return v, content.ErrUnavailable
	}
	var mediaFacts string
	pids := []string{}
	for id := range v.places {
		pids = append(pids, id)
	}
	sort.Strings(pids)
	if e = f.tx.QueryRow(ctx, mapContentMediaFactsSQL, v.order, f.a.ID, pids).Scan(&mediaFacts); e != nil {
		return v, content.ErrUnavailable
	}
	facts = append(facts, mediaFacts)
	for _, mid := range v.order {
		m := v.moments[mid]
		for _, pid := range m.places {
			p := v.places[pid]
			if p == nil {
				continue
			}
			p.pin.MomentCount++
			if len(p.pin.Moments) < 20 {
				p.pin.Moments = append(p.pin.Moments, content.MapContentMomentSelector{ID: mid, Revision: m.v.Revision, VisibilitySource: m.v.VisibilitySource, DetailPath: "/v1/me/map-content/moments/" + mid})
			}
			if !containsMapString(p.pin.VisibilitySources, m.v.VisibilitySource) {
				p.pin.VisibilitySources = append(p.pin.VisibilitySources, m.v.VisibilitySource)
			}
			if m.owner == f.a.ID {
				p.own = true
			}
			var anchor *content.MapContentAnchor
			if p.pin.Anchor.Valid() {
				a := p.pin.Anchor
				anchor = &a
			}
			m.v.Locations = append(m.v.Locations, content.MapContentLocation{PlaceID: pid, Name: p.pin.Name, Anchor: anchor})
		}
	}
	for _, p := range v.places {
		if p.own && !containsMapString(p.pin.VisibilitySources, "SELF_PRIVATE") {
			p.pin.VisibilitySources = append(p.pin.VisibilitySources, "SELF_PRIVATE")
		}
		sort.Strings(p.pin.VisibilitySources)
		p.pin.MomentsTruncated = p.pin.MomentCount > len(p.pin.Moments)
	}
	v.source = content.MomentControlHash(facts)
	return v, nil
}
func containsMapString(v []string, s string) bool {
	for _, x := range v {
		if x == s {
			return true
		}
	}
	return false
}
func shrinkMapLease(v *time.Time, t *time.Time) {
	if t != nil && t.Before(*v) {
		*v = t.UTC()
	}
}

type mapContentResponseProof struct {
	s                        *Store
	d                        [32]byte
	a                        identity.Actor
	kind, id, cursor, source string
	q                        content.MapContentQuery
	until                    time.Time
}

func (p *mapContentResponseProof) RevalidateHumanControl(ctx context.Context) error {
	if !p.until.After(time.Now().UTC()) {
		return identity.ErrControlChanged
	}
	var source string
	var until time.Time
	switch p.kind {
	case "map":
		v, e := p.s.ReadHumanMapContent(ctx, p.d, p.a, p.q)
		if e != nil {
			return e
		}
		source, until = v.SourceVersion, v.ValidUntil
	case "place":
		v, e := p.s.ReadHumanMapContentPlace(ctx, p.d, p.a, p.id, p.cursor)
		if e != nil {
			return e
		}
		source, until = v.SourceVersion, v.ValidUntil
	case "moment":
		v, e := p.s.ReadHumanMapContentMoment(ctx, p.d, p.a, p.id)
		if e != nil {
			return e
		}
		source, until = v.SourceVersion, v.ValidUntil
	default:
		return identity.ErrControlChanged
	}
	if source != p.source || until.Before(p.until) {
		return identity.ErrControlChanged
	}
	return nil
}
func (s *Store) beginMapContent(ctx context.Context, d [32]byte, a identity.Actor) (context.Context, humanWriteFrame, error) {
	ctx, original, e := s.humanWriteBound(ctx, d, a)
	f := humanWriteFrame{d: d, a: a, session: original}
	if e != nil {
		return ctx, f, e
	}
	f.tx, e = s.pool.Begin(ctx)
	if e != nil {
		return ctx, f, content.ErrUnavailable
	}
	// JIT dominates this bounded retained-field projection. Keep execution
	// interpreted only for this transaction; every current authority predicate
	// and final source/deadline proof still runs unchanged. Viewport cardinality
	// changes by orders of magnitude while panning: a cached generic range plan
	// otherwise repeats a tiny-viewport nested loop for a 10k-content viewport.
	// Bind the plan to this request's bounds without altering pool/global state.
	if _, e = f.tx.Exec(ctx, `SET LOCAL jit=off; SET LOCAL plan_cache_mode=force_custom_plan`); e != nil {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, content.ErrUnavailable
	}
	return ctx, f, nil
}
func (s *Store) finishMapContent(ctx context.Context, f humanWriteFrame, snapshot mapContentSnapshot, until time.Time) error {
	return s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		fresh, e := readMapContentSnapshot(ctx, f, snapshot.filter)
		if e != nil {
			return e
		}
		if fresh.source != snapshot.source || fresh.until.Before(until) {
			return identity.ErrControlChanged
		}
		return nil
	}))
}

func (s *Store) mapContentMomentTx(ctx context.Context, f humanWriteFrame, v *mapContentSnapshot, id string) (content.MapContentMoment, error) {
	m := v.moments[id]
	if m == nil {
		return content.MapContentMoment{}, content.ErrNotFound
	}
	r := m.v
	r.ObservedAt, r.ValidUntil = v.at, v.until
	assets, e := s.mapContentMomentMediaTx(ctx, f, v, m)
	if e != nil {
		return r, e
	}
	r.Media = []content.MapContentMedia{}
	for _, x := range assets {
		r.Media = append(r.Media, x.media)
		shrinkMapLease(&r.ValidUntil, x.until)
	}
	for _, pid := range m.places {
		if p := v.places[pid]; p != nil {
			shrinkMapLease(&r.ValidUntil, p.until)
			if m.owner != f.a.ID {
				shrinkMapLease(&r.ValidUntil, p.publicUntil)
			}
		}
	}
	r.SourceVersion = content.MomentControlHash([]any{v.source, m.stamp, r.Media, r.Locations})
	if !r.Valid(time.Now().UTC()) {
		return r, content.ErrUnavailable
	}
	return r, nil
}
func (s *Store) ReadHumanMapContentMoment(ctx context.Context, d [32]byte, a identity.Actor, id string) (content.MapContentMoment, error) {
	if !identity.HumanControlUUID(id) {
		return content.MapContentMoment{}, content.ErrInvalid
	}
	ctx, f, e := s.beginMapContent(ctx, d, a)
	if e != nil {
		return content.MapContentMoment{}, e
	}
	defer f.tx.Rollback(context.Background())
	filter, e := mapContentTargetFilter(ctx, f, "moments", id)
	if e != nil {
		return content.MapContentMoment{}, e
	}
	v, e := readMapContentSnapshot(ctx, f, filter)
	if e != nil {
		return content.MapContentMoment{}, e
	}
	r, e := s.mapContentMomentTx(ctx, f, &v, id)
	if e != nil {
		return r, e
	}
	if e = s.finishMapContent(ctx, f, v, r.ValidUntil); e != nil {
		return content.MapContentMoment{}, e
	}
	r.ResponseProof = &mapContentResponseProof{s: s, d: d, a: a, kind: "moment", id: id, source: r.SourceVersion, until: r.ValidUntil}
	return r, nil
}

func (s *Store) ReadHumanMapContentPlace(ctx context.Context, d [32]byte, a identity.Actor, id, cursor string) (content.MapContentPlacePage, error) {
	p := content.MapContentPlacePage{SchemaVersion: content.MapContentPlaceSchema, ViewerAccountID: a.ID, PlaceID: id, Items: []content.MapContentMoment{}}
	if !identity.HumanControlUUID(id) || len(cursor) > 4096 {
		return p, content.ErrInvalid
	}
	ctx, f, e := s.beginMapContent(ctx, d, a)
	if e != nil {
		return p, e
	}
	defer f.tx.Rollback(context.Background())
	filter, e := mapContentTargetFilter(ctx, f, "places", id)
	if e != nil {
		return p, e
	}
	v, e := readMapContentSnapshot(ctx, f, filter)
	if e != nil {
		return p, e
	}
	place := v.places[id]
	if place == nil || len(place.pin.VisibilitySources) == 0 {
		return p, content.ErrNotFound
	}
	p.ObservedAt, p.ValidUntil = v.at, v.until
	shrinkMapLease(&p.ValidUntil, place.until)
	if place.catalog || place.published {
		shrinkMapLease(&p.ValidUntil, place.publicUntil)
	}
	query := content.MomentControlHash([]any{content.MapContentPlaceSchema, id})
	source := content.MomentControlHash([]any{v.source, id})
	c, e := decodeJourneyHistoryCursor(cursor)
	if e != nil {
		return p, e
	}
	start := 0
	ids := []string{}
	for _, mid := range v.order {
		if containsMapString(v.moments[mid].places, id) {
			ids = append(ids, mid)
		}
	}
	if c != nil {
		if c.Owner != a.ID || c.Reader != historyReader(f) || c.Query != query || c.Source != source || !c.Until.After(v.at) {
			return p, identity.ErrControlChanged
		}
		found := false
		for i, mid := range ids {
			if mid == c.Before {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return p, identity.ErrControlChanged
		}
		shrinkMapLease(&p.ValidUntil, &c.Until)
	}
	end := min(start+20, len(ids))
	for _, mid := range ids[start:end] {
		r, e := s.mapContentMomentTx(ctx, f, &v, mid)
		if e != nil {
			return p, e
		}
		shrinkMapLease(&p.ValidUntil, &r.ValidUntil)
		p.Items = append(p.Items, r)
	}
	// A cursor or any one item's retained material can shorten the envelope.
	// No nested item may claim a lease beyond the page that delivered it.
	for i := range p.Items {
		shrinkMapLease(&p.Items[i].ValidUntil, &p.ValidUntil)
	}
	if end < len(ids) {
		next, e := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: a.ID, Reader: historyReader(f), Query: query, Source: source, Before: ids[end-1], Until: p.ValidUntil})
		if e != nil {
			return p, e
		}
		p.NextCursor = &next
	}
	p.SourceVersion = source
	if !p.Valid(time.Now().UTC()) {
		return p, content.ErrUnavailable
	}
	if e = s.finishMapContent(ctx, f, v, p.ValidUntil); e != nil {
		return content.MapContentPlacePage{}, e
	}
	p.ResponseProof = &mapContentResponseProof{s: s, d: d, a: a, kind: "place", id: id, cursor: cursor, source: p.SourceVersion, until: p.ValidUntil}
	return p, nil
}

// Keep DB errors opaque at the HTTP boundary; only the native fixture logs its
// exact synthetic SQL failure in disposable databases.
func mapContentFailure(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return content.ErrNotFound
	}
	return content.ErrUnavailable
}
