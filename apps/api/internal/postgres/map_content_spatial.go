package postgres

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Bounded by the geographic query, never by a first-N catalogue prefix. The
// database applies current read authority before returning any count or cover.
func mapContentSpatialSQL() string {
	bbox := func(lat, lon string) string {
		return lat + ` BETWEEN $3 AND $5 AND (($2::float8<$4::float8 AND ` + lon + ` BETWEEN $2 AND $4) OR ($2::float8>$4::float8 AND (` + lon + ` >=$2 OR ` + lon + ` <=$4)))`
	}
	places := strings.Replace(mapContentPlaceSQL(), " ORDER BY p.id LIMIT 10001", "", 1)
	nativePoint := `(p.coordinate_system='wgs84' AND p.location_precision='point' AND p.latitude IS NOT NULL AND p.longitude IS NOT NULL)`
	// Separate indexed coordinate sources before joining expensive authority
	// facts. A cross-table OR makes the planner scan the whole Place catalogue.
	spatialIDs := `SELECT p.id pid FROM places p WHERE p.coordinate_system='wgs84' AND p.location_precision='point' AND p.publication_status IN('draft','published') AND ` + bbox("p.latitude", "p.longitude") + ` UNION SELECT f.place_id FROM legacy_map_place_facts f JOIN places p ON p.id=f.place_id WHERE NOT ` + nativePoint + ` AND ` + bbox("f.latitude", "f.longitude")
	places = strings.Replace(places, "FROM places p LEFT JOIN", "FROM spatial_place_ids spatial JOIN places p ON p.id=spatial.pid LEFT JOIN", 1)
	moments := strings.Replace(mapContentMomentsSQL, "WHERE m.status<>", "WHERE m.id IN(SELECT mid FROM raw_ids) AND m.status<>", 1)
	moments = strings.Replace(moments, " ORDER BY coalesce(published_at,CASE WHEN original_run IS NULL OR created_at<adapted_at THEN created_at END) DESC NULLS LAST,id LIMIT 10001", "", 1)
	rel := strings.Replace(mapContentCurrentMomentPlacesSQL(), "q.mid=ANY($1::uuid[])", "q.mid IN(SELECT id::uuid FROM vm)", 1)
	return `WITH spatial_place_ids AS MATERIALIZED (` + spatialIDs + `), sp(id,name,category,city,lat,lon,catalog,published,deadline,public_deadline,stamp) AS MATERIALIZED (` + places + `),
 anchors AS MATERIALIZED(
 SELECT a.id,a.moment_id,a.media_id,a.latitude lat,a.longitude lon,a.precision,jsonb_build_array(a.xmin::text,run.xmin::text,m.xmin::text,ctx.xmin::text)::text stamp
 FROM legacy_map_anchor_facts a JOIN legacy_map_anchor_runs run ON run.run_id=a.run_id
 JOIN legacy_import_runs original ON original.run_id=run.run_id AND original.source_file_sha256=run.source_file_sha256 AND original.manifest_file_sha256=run.manifest_file_sha256 AND original.snapshot_sha256=run.snapshot_sha256
 JOIN moments m ON m.id=a.moment_id AND birdtie_legacy_map_moment_binding(m)=a.native_binding
 LEFT JOIN private_moment_media_contexts ctx ON ctx.id=a.context_id AND ctx.moment_id=a.moment_id AND ctx.owner_account_id=m.author_account_id
 WHERE ` + bbox("a.latitude", "a.longitude") + ` AND(a.context_id IS NULL OR(ctx.status='ACTIVE' AND ctx.original_map_enabled AND ctx.source_latitude::float8=a.latitude AND ctx.source_longitude::float8=a.longitude))
 AND(a.media_id IS NULL OR EXISTS(SELECT 1 FROM private_moment_images i JOIN media_assets ma ON ma.id=i.id AND ma.state='ready_private' WHERE i.id=a.media_id AND i.moment_id=a.moment_id AND i.owner_account_id=m.author_account_id AND i.status='ready_private' AND i.derivative IS NOT NULL AND i.retain_until>clock_timestamp()) OR EXISTS(SELECT 1 FROM private_moment_videos v JOIN media_assets ma ON ma.id=v.id AND ma.state='ready_private' WHERE v.id=a.media_id AND v.moment_id=a.moment_id AND v.owner_account_id=m.author_account_id AND v.status='ready_private' AND v.content IS NOT NULL AND v.poster IS NOT NULL AND v.retain_until>clock_timestamp()))
 ), refs AS MATERIALIZED(
 SELECT l.id,l.moment_id,o.latitude lat,o.longitude lon,o.name,o.valid_until deadline,jsonb_build_array(l.xmin::text,l.revision,r.xmin::text,o.xmin::text,o.version)::text stamp
 FROM moment_location_links l JOIN location_references r ON r.id=l.reference_id AND r.status='ACTIVE' JOIN location_reference_observations o ON o.reference_id=r.id
 WHERE l.kind='REFERENCE' AND l.status='ACTIVE' AND l.precision='place' AND o.observed_at<=clock_timestamp() AND o.valid_until>clock_timestamp() AND ` + bbox("o.latitude", "o.longitude") + `
 ),raw_ids AS MATERIALIZED(
 SELECT l.moment_id mid FROM moment_location_links l JOIN sp ON sp.id::uuid=l.place_id WHERE l.status='ACTIVE' AND l.kind='PLACE'
 UNION SELECT c.moment_id FROM private_moment_media_contexts c JOIN private_moment_media_context_places p ON p.context_id=c.id JOIN sp ON sp.id::uuid=p.place_id WHERE c.status='ACTIVE'
 UNION SELECT moment_id FROM anchors UNION SELECT moment_id FROM refs
 ),vm(id,owner,revision,status,visibility,title,body,created_at,source,stamp,published_at,time_source) AS MATERIALIZED (` + moments + `),
 relations(mid,pid,stamp,original_relation,private_context,native_public_relation,relation_id) AS MATERIALIZED (` + rel + `),
 leaves AS(
 SELECT 'MOMENT_ANCHOR' kind,relations.relation_id leaf,vm.id entity,coalesce(nullif(vm.title,''),nullif(left(vm.body,80),''),'动态') name,'moment' category,sp.city,sp.lat,sp.lon,'point' coordinate_precision,vm.id mid,vm.revision,vm.source,jsonb_build_array(sp.stamp,vm.stamp,relations.stamp)::text stamp,''::text media,least(sp.deadline,sp.public_deadline) deadline,sp.id place_id,coalesce(vm.published_at,CASE WHEN vm.time_source<>'UNKNOWN' THEN vm.created_at END) content_time,CASE WHEN vm.published_at IS NOT NULL THEN 'PUBLISHED' ELSE vm.time_source END time_source
 FROM sp JOIN relations ON relations.pid=sp.id JOIN vm ON vm.id=relations.mid
 WHERE sp.lat IS NOT NULL AND sp.lon IS NOT NULL AND(vm.source<>'CIVU_PUBLIC_MOMENT' OR relations.original_relation)
 AND(vm.source<>'BIRDTIE_PUBLIC_MOMENT' OR vm.owner=$1::text OR(NOT relations.private_context AND relations.native_public_relation))
 AND(vm.owner=$1::text OR(vm.source='CIVU_PUBLIC_MOMENT' AND sp.catalog) OR(vm.source='BIRDTIE_PUBLIC_MOMENT' AND sp.published))
 UNION ALL SELECT 'MOMENT_ANCHOR',a.id::text,vm.id,coalesce(nullif(vm.title,''),nullif(left(vm.body,80),''),'动态'),'moment',NULL::text,a.lat,a.lon,a.precision,vm.id,vm.revision,vm.source,jsonb_build_array(a.stamp,vm.stamp)::text,coalesce(a.media_id::text,''),NULL::timestamptz,''::text,coalesce(vm.published_at,CASE WHEN vm.time_source<>'UNKNOWN' THEN vm.created_at END),CASE WHEN vm.published_at IS NOT NULL THEN 'PUBLISHED' ELSE vm.time_source END FROM anchors a JOIN vm ON vm.id=a.moment_id::text
 UNION ALL SELECT 'MOMENT_ANCHOR',r.id::text,vm.id,coalesce(nullif(vm.title,''),nullif(left(vm.body,80),''),'动态'),'moment',NULL::text,r.lat,r.lon,'point',vm.id,vm.revision,vm.source,jsonb_build_array(r.stamp,vm.stamp)::text,''::text,r.deadline,''::text,coalesce(vm.published_at,CASE WHEN vm.time_source<>'UNKNOWN' THEN vm.created_at END),CASE WHEN vm.published_at IS NOT NULL THEN 'PUBLISHED' ELSE vm.time_source END FROM refs r JOIN vm ON vm.id=r.moment_id::text WHERE vm.source IN('SELF_PRIVATE','BIRDTIE_PUBLIC_MOMENT')
 ) SELECT kind,leaf,entity,name,category,city,lat,lon,coordinate_precision,mid,revision,source,stamp,media,deadline,place_id,content_time,time_source FROM leaves ORDER BY content_time DESC NULLS LAST,mid,leaf`
}

type mapSpatialLeaf struct {
	pin     content.MapContentPin
	moments map[string]content.MapContentMomentSelector
	stamps  []string
	media   string
	until   *time.Time
}

func mapClusterCategory(leaves []mapSpatialLeaf, members []int) (string, string) {
	places, anchors := false, false
	for _, i := range members {
		places = places || leaves[i].pin.Kind == "PLACE_CONTENT"
		anchors = anchors || leaves[i].pin.Kind == "MOMENT_ANCHOR"
	}
	if places && !anchors {
		return "地点", "place_cluster"
	}
	if anchors && !places {
		return "动态", "moment_cluster"
	}
	return "地图内容", "mixed_cluster"
}

func readMapSpatial(ctx context.Context, f humanWriteFrame, q content.MapContentQuery, stores ...*Store) ([]mapSpatialLeaf, string, time.Time, time.Time, error) {
	at := time.Now().UTC()
	until := at.Add(30 * time.Second)
	if e := ownedContextLeaseTx(ctx, f, &until); e != nil {
		return nil, "", at, until, e
	}
	rows, e := f.tx.Query(ctx, mapContentSpatialSQL(), f.a.ID, q.Bounds.West, q.Bounds.South, q.Bounds.East, q.Bounds.North)
	if e != nil {
		return nil, "", at, until, content.ErrUnavailable
	}
	defer rows.Close()
	grouped := map[string]*mapSpatialLeaf{}
	facts := []any{content.MapContentSchema, historyReader(f), q.Bounds, q.Zoom, "DEFAULT_CONTENT", "CONTENT_FIRST_V4"}
	for rows.Next() {
		var kind, id, entity, name, category, precision, mid, scope, stamp, media, placeID, timeSource string
		var city *string
		var lat, lon float64
		var rev int64
		var deadline, contentTime *time.Time
		if e = rows.Scan(&kind, &id, &entity, &name, &category, &city, &lat, &lon, &precision, &mid, &rev, &scope, &stamp, &media, &deadline, &placeID, &contentTime, &timeSource); e != nil {
			return nil, "", at, until, content.ErrUnavailable
		}
		anchor := content.MapContentAnchor{CoordinateSystem: "wgs84", Precision: precision, Latitude: lat, Longitude: lon}
		if !anchor.Valid() || (lat == 0 && lon == 0) {
			continue
		}
		key := "place:" + id
		if kind == "MOMENT_ANCHOR" {
			key = "anchor:" + id
		}
		x := grouped[key]
		if x == nil {
			x = &mapSpatialLeaf{pin: content.MapContentPin{MarkerID: key, Kind: kind, EntityID: entity, Name: name, CategoryCode: category, CityID: city, Anchor: anchor, AnchorCount: 1, PlaceID: placeID, ContentTime: contentTime, TimeSource: timeSource, VisibilitySources: []string{}, Moments: []content.MapContentMomentSelector{}}, moments: map[string]content.MapContentMomentSelector{}, media: media}
			if kind == "MOMENT_ANCHOR" {
				x.pin.AnchorID = id
				x.pin.MomentID = mid
				x.pin.EntryID = mid
			}
			grouped[key] = x
		}
		x.moments[mid] = content.MapContentMomentSelector{ID: mid, Revision: rev, VisibilitySource: scope, DetailPath: "/v1/me/map-content/moments/" + mid}
		if !containsMapString(x.pin.VisibilitySources, scope) {
			x.pin.VisibilitySources = append(x.pin.VisibilitySources, scope)
		}
		x.stamps = append(x.stamps, stamp)
		if deadline != nil && (x.until == nil || deadline.Before(*x.until)) {
			x.until = deadline
		}
		shrinkMapLease(&until, deadline)
	}
	if rows.Err() != nil {
		return nil, "", at, until, content.ErrUnavailable
	}
	rows.Close()
	keys := []string{}
	for k := range grouped {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []mapSpatialLeaf{}
	// Only stable relation/anchor identity merges rows. Equal coordinates or a
	// shared Moment ID do not prove separate anchors are duplicates.
	for _, k := range keys {
		x := grouped[k]
		mids := []string{}
		for m := range x.moments {
			mids = append(mids, m)
		}
		sort.Strings(mids)
		for _, m := range mids {
			if len(x.pin.Moments) < 20 {
				x.pin.Moments = append(x.pin.Moments, x.moments[m])
			}
		}
		sort.Strings(x.pin.VisibilitySources)
		x.pin.MomentCount = len(mids)
		x.pin.MomentsTruncated = len(mids) > 20
		x.pin.SourceVersion = mapSpatialLeafSourceVersion(historyReader(f), x)
		out = append(out, *x)
	}
	sort.SliceStable(out, func(i, j int) bool { return mapContentPinNewer(out[i].pin, out[j].pin) })
	momentIDs, placeIDs := []string{}, []string{}
	seenMID := map[string]bool{}
	for _, x := range out {
		if x.pin.PlaceID != "" {
			placeIDs = append(placeIDs, x.pin.PlaceID)
		}
		for mid := range x.moments {
			if !seenMID[mid] {
				seenMID[mid] = true
				momentIDs = append(momentIDs, mid)
			}
		}
	}
	sort.Strings(momentIDs)
	sort.Strings(placeIDs)
	var mediaFacts string
	if e = f.tx.QueryRow(ctx, mapContentMediaFactsSQL, momentIDs, f.a.ID, placeIDs).Scan(&mediaFacts); e != nil {
		return nil, "", at, until, content.ErrUnavailable
	}
	if e = mapSpatialMediaVersions(out, mediaFacts); e != nil {
		return nil, "", at, until, e
	}
	var store *Store
	if len(stores) > 0 {
		store = stores[0]
	} else {
		store = &Store{}
	}
	routeFacts, e := mapContentRouteFacts(ctx, f, out, store, &until)
	if e != nil {
		return nil, "", at, until, e
	}
	facts = append(facts, routeFacts)
	for _, x := range out {
		facts = append(facts, x.pin.SourceVersion)
	}
	facts = append(facts, mediaFacts)
	return out, content.MomentControlHash(facts), at, until, nil
}

// Attachment facts are batched but attributed to their own moment/place. The
// same leaf keeps its identity when the viewport or selected cover budget
// changes; a changed attachment still retires that leaf's old reader.
func mapSpatialMediaVersions(leaves []mapSpatialLeaf, raw string) error {
	var facts [][]json.RawMessage
	if json.Unmarshal([]byte(raw), &facts) != nil {
		return content.ErrUnavailable
	}
	byEntity := map[string][]string{}
	for _, fact := range facts {
		var kind, id string
		if len(fact) < 2 || json.Unmarshal(fact[0], &kind) != nil || json.Unmarshal(fact[1], &id) != nil || (kind != "moment" && kind != "place") {
			return content.ErrUnavailable
		}
		byEntity[kind+":"+id] = append(byEntity[kind+":"+id], content.MomentControlHash(fact))
	}
	for i := range leaves {
		x := &leaves[i]
		versions := append([]string{}, byEntity["place:"+x.pin.PlaceID]...)
		for mid := range x.moments {
			versions = append(versions, byEntity["moment:"+mid]...)
		}
		sort.Strings(versions)
		x.pin.SourceVersion = content.MomentControlHash([]any{x.pin.SourceVersion, versions})
	}
	return nil
}

func mapContentTargetFilter(ctx context.Context, f humanWriteFrame, kind, id string) (*mapContentReadFilter, error) {
	v := &mapContentReadFilter{PlaceIDs: []string{}, MomentIDs: []string{}}
	if kind == "moments" {
		v.MomentIDs = append(v.MomentIDs, id)
	} else {
		v.PlaceIDs = append(v.PlaceIDs, id)
	}
	rows, e := f.tx.Query(ctx, `SELECT DISTINCT moment_id::text,place_id::text FROM(SELECT moment_id,place_id FROM moment_location_links WHERE status='ACTIVE' AND kind='PLACE' UNION ALL SELECT c.moment_id,p.place_id FROM private_moment_media_contexts c JOIN private_moment_media_context_places p ON p.context_id=c.id WHERE c.status='ACTIVE') r WHERE ($1='moments' AND moment_id=$2::uuid)OR($1='places' AND place_id=$2::uuid)`, kind, id)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var mid, pid string
		if rows.Scan(&mid, &pid) != nil {
			return nil, content.ErrUnavailable
		}
		if !containsMapString(v.MomentIDs, mid) {
			v.MomentIDs = append(v.MomentIDs, mid)
		}
		if !containsMapString(v.PlaceIDs, pid) {
			v.PlaceIDs = append(v.PlaceIDs, pid)
		}
	}
	if rows.Err() != nil {
		return nil, content.ErrUnavailable
	}
	sort.Strings(v.PlaceIDs)
	sort.Strings(v.MomentIDs)
	return v, nil
}

// Each stable object owns its real point, including coincident objects.
// Transport stays bounded to 40 independently selectable points per page.
func mapSpatialGroups(leaves []mapSpatialLeaf, _ float64) [][]int {
	groups := make([][]int, len(leaves))
	for i := range leaves {
		groups[i] = []int{i}
	}
	return groups
}

func mapSpatialLeafSourceVersion(reader string, x *mapSpatialLeaf) string {
	stamps := append([]string(nil), x.stamps...)
	sort.Strings(stamps)
	return content.MomentControlHash([]any{reader, x.pin.MarkerID, stamps, x.pin.Moments, x.pin.ContentTime, x.pin.TimeSource, x.media})
}

func mapClusterID(reader string, leaves []mapSpatialLeaf, ids []int) string {
	members := []string{}
	for _, i := range ids {
		members = append(members, leaves[i].pin.MarkerID)
	}
	sort.Strings(members)
	return "cluster:" + content.MomentControlHash([]any{"coincident-cluster-v2", reader, leaves[ids[0]].pin.Anchor, members})
}

// Viewport/cursor identity still fences the complete response. A displayed
// coincident group has its own facts: unrelated regions and zoom changes must
// not revoke identical authorized glyphs or names in this group.
func mapClusterSourceVersion(reader string, leaves []mapSpatialLeaf, ids []int) string {
	facts := []string{}
	for _, i := range ids {
		p := leaves[i].pin
		facts = append(facts, content.MomentControlHash([]any{p.MarkerID, p.SourceVersion, p.Kind, p.Name, p.CategoryCode, p.Anchor, p.MomentCount, p.AnchorCount, p.Moments, p.VisibilitySources}))
	}
	sort.Strings(facts)
	return content.MomentControlHash([]any{"coincident-cluster-source-v2", reader, facts})
}
func mapClusterBounds(q content.MapContentQuery, leaves []mapSpatialLeaf, ids []int) content.MapContentBounds {
	b := content.MapContentBounds{West: 540, East: -540, South: 90, North: -90}
	for _, i := range ids {
		a := leaves[i].pin.Anchor
		lon := a.Longitude
		if q.Bounds.West > q.Bounds.East && lon < q.Bounds.West {
			lon += 360
		}
		b.West = min(b.West, lon)
		b.East = max(b.East, lon)
		b.South = min(b.South, a.Latitude)
		b.North = max(b.North, a.Latitude)
	}
	if b.West == b.East {
		b.West -= 0.000001
		b.East += 0.000001
	}
	if b.South == b.North {
		b.South = max(-90, b.South-0.000001)
		b.North = min(90, b.North+0.000001)
	}
	for b.West > 180 {
		b.West -= 360
	}
	for b.West < -180 {
		b.West += 360
	}
	for b.East > 180 {
		b.East -= 360
	}
	for b.East < -180 {
		b.East += 360
	}
	return b
}
func mapMembersPath(q content.MapContentQuery, cluster, source, cursor string) string {
	v := url.Values{"west": {strconv.FormatFloat(q.Bounds.West, 'g', -1, 64)}, "south": {strconv.FormatFloat(q.Bounds.South, 'g', -1, 64)}, "east": {strconv.FormatFloat(q.Bounds.East, 'g', -1, 64)}, "north": {strconv.FormatFloat(q.Bounds.North, 'g', -1, 64)}, "zoom": {strconv.FormatFloat(q.Zoom, 'g', -1, 64)}, "filterScope": {"DEFAULT_CONTENT"}, "clusterId": {cluster}, "sourceVersion": {source}}
	if cluster == "" {
		v.Del("clusterId")
	}
	if q.CityID != "" {
		v.Set("cityId", q.CityID)
	}
	if cursor != "" {
		v.Set("cursor", cursor)
	}
	if q.SelectedMarkerID != "" {
		v.Set("selectedMarkerId", q.SelectedMarkerID)
	}
	return "/v1/me/map-content?" + v.Encode()
}
func (s *Store) ReadHumanMapContent(ctx context.Context, d [32]byte, a identity.Actor, q content.MapContentQuery) (content.MapContentPage, error) {
	p := content.MapContentPage{SchemaVersion: content.MapContentSchema, Scope: "MAP_CONTENT", ViewerAccountID: a.ID, CityID: q.CityID, Bounds: q.Bounds, Zoom: q.Zoom, FilterScope: "DEFAULT_CONTENT", Pins: []content.MapContentPin{}}
	if !q.Valid() {
		return p, content.ErrInvalid
	}
	ctx, f, e := s.beginMapContent(ctx, d, a)
	if e != nil {
		return p, e
	}
	defer f.tx.Rollback(context.Background())
	leaves, source, at, until, e := readMapSpatial(ctx, f, q, s)
	if e != nil {
		return p, e
	}
	p.ObservedAt = at
	p.ValidUntil = until
	p.SourceVersion = source
	p.TotalAnchorCount = len(leaves)
	mids := map[string]bool{}
	for _, x := range leaves {
		for id := range x.moments {
			mids[id] = true
		}
	}
	p.TotalContentCount = len(mids)
	groups := mapSpatialGroups(leaves, q.Zoom)
	p.TotalPointCount = len(groups)
	p.ViewportPath = mapMembersPath(q, "", source, "")
	if q.SourceVersion != "" && q.SourceVersion != source {
		return p, identity.ErrControlChanged
	}
	selected := []int{}
	representatives := map[string]int{}
	if q.ClusterID != "" {
		if q.SourceVersion != source {
			return p, identity.ErrControlChanged
		}
		found := false
		for _, g := range groups {
			if len(g) > 1 && mapClusterID(historyReader(f), leaves, g) == q.ClusterID {
				selected = g
				found = true
				break
			}
		}
		if !found {
			return p, content.ErrNotFound
		}
		start := 0
		if q.Cursor != "" {
			c, err := decodeJourneyHistoryCursor(q.Cursor)
			if err != nil || c == nil || c.Owner != a.ID || c.Reader != historyReader(f) || c.Query != q.ClusterID || c.Source != source || !c.Until.After(at) {
				return p, identity.ErrControlChanged
			}
			foundBefore := false
			for j, li := range selected {
				if strings.TrimPrefix(strings.TrimPrefix(leaves[li].pin.MarkerID, "place:"), "anchor:") == c.Before {
					start = j + 1
					foundBefore = true
					break
				}
			}
			if !foundBefore || start >= len(selected) {
				return p, content.ErrInvalid
			}
			// A cursor proves only the signed position in this exact unchanged
			// result. Current ACL and source were read above, so an old content
			// lease must not cap a freshly authorized page's thirty seconds.
		}
		end := min(start+40, len(selected))
		if end < len(selected) {
			c, err := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: a.ID, Reader: historyReader(f), Query: q.ClusterID, Source: source, Before: strings.TrimPrefix(strings.TrimPrefix(leaves[selected[end-1]].pin.MarkerID, "place:"), "anchor:"), Until: at.Add(30 * time.Minute)})
			if err != nil {
				return p, err
			}
			p.NextCursor = &c
		}
		selected = selected[start:end]
		for _, i := range selected {
			p.Pins = append(p.Pins, leaves[i].pin)
		}
	} else {
		query := "viewport:" + content.MomentControlHash([]any{q.Bounds, q.Zoom, q.CityID, "DEFAULT_CONTENT"})
		start := 0
		if q.Cursor != "" {
			c, err := decodeJourneyHistoryCursor(q.Cursor)
			if err != nil || c == nil || c.Owner != a.ID || c.Reader != historyReader(f) || c.Query != query || c.Source != source || !c.Until.After(at) {
				return p, identity.ErrControlChanged
			}
			found := false
			for i, g := range groups {
				before := strings.TrimPrefix(strings.TrimPrefix(leaves[g[0]].pin.MarkerID, "place:"), "anchor:")
				if before == c.Before {
					start = i + 1
					found = true
					break
				}
			}
			if !found || start >= len(groups) {
				return p, content.ErrInvalid
			}
		}
		end := min(start+40, len(groups))
		if end < len(groups) {
			before := strings.TrimPrefix(strings.TrimPrefix(leaves[groups[end-1][0]].pin.MarkerID, "place:"), "anchor:")
			cursor, err := encodeJourneyHistoryCursor(journeyHistoryCursor{Version: 1, Owner: a.ID, Reader: historyReader(f), Query: query, Source: source, Before: before, Until: at.Add(30 * time.Minute)})
			if err != nil {
				return p, err
			}
			p.NextCursor = &cursor
		}
		for _, g := range groups[start:end] {
			if len(g) == 1 {
				i := g[0]
				selected = append(selected, i)
				p.Pins = append(p.Pins, leaves[i].pin)
				continue
			}
			first := leaves[g[0]].pin
			name, category := mapClusterCategory(leaves, g)
			x := content.MapContentPin{Kind: "CLUSTER", Name: name, CategoryCode: category, Anchor: first.Anchor, AnchorCount: len(g), VisibilitySources: []string{}, Moments: []content.MapContentMomentSelector{}}
			x.MarkerID = mapClusterID(historyReader(f), leaves, g)
			x.EntityID = x.MarkerID
			x.RepresentativeMarkerID = first.MarkerID
			x.RepresentativePlaceID = first.PlaceID
			x.RepresentativeMomentID = first.MomentID
			representatives[x.MarkerID] = g[0]
			selected = append(selected, g[0])
			x.MembersPath = mapMembersPath(q, x.MarkerID, source, "")
			bounds := mapClusterBounds(q, leaves, g)
			x.Bounds = &bounds
			ms := map[string]content.MapContentMomentSelector{}
			for _, i := range g {
				for id, m := range leaves[i].moments {
					ms[id] = m
				}
				for _, scope := range leaves[i].pin.VisibilitySources {
					if !containsMapString(x.VisibilitySources, scope) {
						x.VisibilitySources = append(x.VisibilitySources, scope)
					}
				}
			}
			ids := []string{}
			for id := range ms {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				if len(x.Moments) < 20 {
					x.Moments = append(x.Moments, ms[id])
				}
			}
			sort.Strings(x.VisibilitySources)
			x.MomentCount = len(ms)
			x.MomentsTruncated = len(ms) > 20
			x.SourceVersion = mapClusterSourceVersion(historyReader(f), leaves, g)
			p.Pins = append(p.Pins, x)
		}
	}
	// Only returned leaves and one real representative per coincident group
	// consume metadata/thumbnail budget; never hydrate every group member. Counts above already include all leaves.
	filter := &mapContentReadFilter{PlaceIDs: []string{}, MomentIDs: []string{}}
	for _, i := range selected {
		x := leaves[i]
		if x.pin.PlaceID != "" {
			filter.PlaceIDs = append(filter.PlaceIDs, x.pin.PlaceID)
		}
		for _, selector := range x.pin.Moments {
			id := selector.ID
			if !containsMapString(filter.MomentIDs, id) {
				filter.MomentIDs = append(filter.MomentIDs, id)
			}
		}
	}
	sort.Strings(filter.PlaceIDs)
	sort.Strings(filter.MomentIDs)
	v, e := readMapContentSnapshot(ctx, f, filter)
	if e != nil {
		return p, e
	}
	shrinkMapLease(&p.ValidUntil, &v.until)
	targets := make([]mapContentCoverTarget, 0, len(p.Pins))
	wantedMedia := map[string]string{}
	for _, i := range selected {
		wantedMedia[leaves[i].pin.MarkerID] = leaves[i].media
	}
	for i, pin := range p.Pins {
		target := pin
		if li, ok := representatives[pin.MarkerID]; ok {
			target = leaves[li].pin
		}
		targets = append(targets, mapContentCoverTarget{i, target, wantedMedia[target.MarkerID]})
	}
	e = attachMapContentCovers(ctx, p.Pins, targets, q.SelectedMarkerID,
		func(target content.MapContentPin) ([]content.MapContentImage, error) {
			if target.Kind == "PLACE_CONTENT" {
				cover, err := s.mapContentCoverTx(ctx, f, &v, target.PlaceID)
				if err != nil || cover == nil {
					return nil, err
				}
				return []content.MapContentImage{*cover}, nil
			}
			m := v.moments[target.MomentID]
			if m == nil {
				return nil, nil
			}
			assets, err := s.mapContentMomentMediaTx(ctx, f, &v, m)
			if err != nil {
				return nil, err
			}
			images := make([]content.MapContentImage, 0, len(assets))
			for _, asset := range assets {
				images = append(images, asset.image)
			}
			return images, nil
		},
		func(original content.MapContentImage) (*content.MapContentImage, error) {
			return s.mapMarkerThumbnailTx(ctx, f, &v, original)
		})
	if e != nil {
		return p, e
	}
	shrinkMapLease(&p.ValidUntil, &v.until)
	if !p.Valid(time.Now().UTC()) {
		return p, content.ErrUnavailable
	}
	// Re-evaluate the exact spatial query before commit; late withdraw/block or
	// newly visible content cannot escape by changing only a selected subset.
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.ValidUntil, func() error {
		_, fresh, _, end, err := readMapSpatial(ctx, f, q, s)
		if err != nil {
			return err
		}
		if fresh != source || end.Before(p.ValidUntil) {
			return identity.ErrControlChanged
		}
		stable, err := readMapContentSnapshot(ctx, f, v.filter)
		if err != nil {
			return err
		}
		if stable.source != v.source || stable.until.Before(p.ValidUntil) {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return content.MapContentPage{}, e
	}
	p.ResponseProof = &mapContentResponseProof{s: s, d: d, a: a, kind: "map", q: q, source: p.SourceVersion, until: p.ValidUntil}
	return p, nil
}

const mapContentCoverBudget = 24

type mapContentCoverTarget struct {
	index  int
	pin    content.MapContentPin
	wanted string
}
type mapContentCoverSource struct{ kind, id string }
type mapContentCoverDescriptor struct {
	source mapContentCoverSource
	image  content.MapContentImage
}

// Request-local dual budgets count source containers and exact authorized image
// descriptors, never anchor projections. Failed/empty attempts count too. A
// later projection may reuse an admitted descriptor even after both limits fill.
// The source key prevents an equal file SHA (or even image fields) from lending
// one Moment/Place's authority to another; every new request rebuilds its ACL.
func attachMapContentCovers(ctx context.Context, pins []content.MapContentPin, targets []mapContentCoverTarget, selected string,
	resolve func(content.MapContentPin) ([]content.MapContentImage, error),
	thumbnail func(content.MapContentImage) (*content.MapContentImage, error)) error {
	ordered := append([]mapContentCoverTarget{}, targets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i].index, ordered[j].index
		return a >= 0 && a < len(pins) && pins[a].MarkerID == selected &&
			!(b >= 0 && b < len(pins) && pins[b].MarkerID == selected)
	})
	for i := range pins {
		pins[i].Cover = nil
	}
	sources := map[mapContentCoverSource][]content.MapContentImage{}
	descriptors := map[mapContentCoverDescriptor]*content.MapContentImage{}
	for _, candidate := range ordered {
		if ctx.Err() != nil {
			return content.ErrUnavailable
		}
		if candidate.index < 0 || candidate.index >= len(pins) {
			continue
		}
		target := candidate.pin
		source := mapContentCoverSource{kind: target.Kind}
		switch target.Kind {
		case "PLACE_CONTENT":
			source.id = target.PlaceID
		case "MOMENT_ANCHOR":
			source.id = target.MomentID
		default:
			continue
		}
		if source.id == "" {
			continue
		}
		images, admitted := sources[source]
		if !admitted {
			if len(sources) >= mapContentCoverBudget {
				continue
			}
			var err error
			images, err = resolve(target)
			if err != nil {
				if ctx.Err() != nil {
					return content.ErrUnavailable
				}
				images = nil
			}
			sources[source] = images
		}
		if ctx.Err() != nil {
			return content.ErrUnavailable
		}
		var original *content.MapContentImage
		for _, image := range images {
			if candidate.wanted == "" || image.ID == candidate.wanted {
				if image.Valid() {
					copy := image
					original = &copy
				}
				break
			}
		}
		if original == nil {
			continue
		}
		key := mapContentCoverDescriptor{source, *original}
		cover, attempted := descriptors[key]
		if !attempted {
			if len(descriptors) >= mapContentCoverBudget {
				continue
			}
			var err error
			cover, err = thumbnail(*original)
			if err != nil {
				if ctx.Err() != nil {
					return content.ErrUnavailable
				}
				cover = nil
			}
			descriptors[key] = cover
		}
		if ctx.Err() != nil {
			return content.ErrUnavailable
		}
		if cover == nil {
			continue
		}
		pin := &pins[candidate.index]
		copy := *cover
		pin.Cover = &copy
		if !containsMapString(pin.VisibilitySources, cover.VisibilitySource) {
			pin.VisibilitySources = append(pin.VisibilitySources, cover.VisibilitySource)
			sort.Strings(pin.VisibilitySources)
		}
	}
	return nil
}

// Unknown import timestamps sort last rather than looking newly published.
func mapContentPinNewer(a, b content.MapContentPin) bool {
	if a.ContentTime != nil && b.ContentTime != nil && !a.ContentTime.Equal(*b.ContentTime) {
		return a.ContentTime.After(*b.ContentTime)
	}
	if (a.ContentTime == nil) != (b.ContentTime == nil) {
		return a.ContentTime != nil
	}
	if a.MomentID != b.MomentID {
		return a.MomentID < b.MomentID
	}
	return a.MarkerID < b.MarkerID
}
