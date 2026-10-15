package legacycontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
	"sort"
	"time"
)

type row = map[string]any

func text(r row, k string) string { v, _ := r[k].(string); return v }
func integer(r row, k string) (int64, bool) {
	n, ok := r[k].(json.Number)
	if !ok {
		return 0, false
	}
	v, e := n.Int64()
	return v, e == nil
}
func optionalInteger(r row, k string) *int64 {
	if r[k] == nil {
		return nil
	}
	v, ok := integer(r, k)
	if !ok || v < 0 {
		return nil
	}
	return &v
}
func parsedTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	v, e := time.Parse(time.RFC3339Nano, s)
	if e != nil {
		return nil
	}
	v = v.UTC()
	return &v
}
func stable(kind, owner, key string) string {
	return legacynative.StableTargetID("context-v1-"+kind, owner, key)
}
func hash(v any) string {
	raw, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if d.Decode(&value) != nil {
		return ""
	}
	s, _ := legacymigration.Digest(value)
	return s
}

func Build(run string, source, manifest []byte) (Plan, error) {
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	cat, e := legacycatalog.Build(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	native, e := legacynative.Build(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	personal, e := legacycollections.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	var doc map[string]any
	d := json.NewDecoder(bytes.NewReader(source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		return Plan{}, ErrInvalid
	}
	tables := doc["tables"].(map[string]any)
	rows := func(k string) []row {
		out := []row{}
		for _, v := range tables[k].([]any) {
			out = append(out, v.(map[string]any))
		}
		return out
	}
	p := Plan{RunID: run, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, Places: []Place{}, SavedItems: []legacynative.SavedItem{}, NoteLocations: []NoteLocations{}, MediaContexts: []MediaContext{}, Threads: []Thread{}, Deferred: map[string]int{}}
	areaRows := map[string]row{}
	for _, r := range rows("geo_areas") {
		areaRows[text(r, "id")] = r
	}
	placeMap := map[string]string{}
	for k, v := range cat.PlaceMappings {
		placeMap[k] = v
	}
	for _, r := range rows("places") {
		id := text(r, "id")
		if placeMap[id] != "" {
			continue
		}
		if text(r, "status") != "active" || text(r, "admin_status") != "active" || text(r, "lifecycle_status") != "active" {
			continue
		}
		if text(r, "name") == "" || text(r, "category_code") == "" {
			return Plan{}, ErrInvalid
		}
		v := legacycatalog.Place{SourceID: id, TargetID: legacycatalog.StablePlaceTargetID(id), Name: text(r, "name"), CategoryCode: text(r, "category_code"), Summary: text(r, "description"), SourceSHA256: hash(r), SourceLabel: "Civu preserved private catalog", SourceRef: "civu:place:" + id}
		area := areaRows[text(r, "admin_area_id")]
		p.Places = append(p.Places, Place{Place: v, SourceRegion: text(area, "name_local"), SourceCountryCode: text(area, "country_code"), SourceAreaKind: text(area, "kind")})
		placeMap[id] = v.TargetID
	}
	saved := map[string]bool{}
	for _, v := range native.SavedItems {
		saved[v.SourceKey] = true
	}
	for _, a := range personal.Records {
		if a.Kind != "saved_place" || a.Deleted || saved[a.SourceKey] {
			continue
		}
		var payload row
		if json.Unmarshal(a.Payload, &payload) != nil {
			return Plan{}, ErrInvalid
		}
		sid := text(payload, "sourcePlaceId")
		target := placeMap[sid]
		if target == "" {
			p.Deferred["saved_unmapped"]++
			continue
		}
		p.SavedItems = append(p.SavedItems, legacynative.SavedItem{SourceID: sid, SourceKey: a.SourceKey, OwnerSourceID: a.SourceOwnerID, SourceSHA256: a.SourceSHA256, TargetID: legacynative.StableTargetID("saved_place", a.SourceOwnerID, a.SourceKey), PlaceID: target, SourceCreatedAt: a.SourceCreatedAt})
	}
	placeRelationHashes := map[string]string{}
	for _, r := range rows("note_places") {
		position, _ := integer(r, "position")
		placeRelationHashes[fmt.Sprintf("%s:%d:%s", text(r, "note_id"), position, text(r, "place_id"))] = hash(r)
	}
	mediaContextHashes := map[string]string{}
	for _, r := range rows("note_media_locations") {
		mediaContextHashes[text(r, "note_id")+":"+text(r, "media_id")] = hash(r)
	}
	noteRows := map[string]row{}
	for _, r := range rows("notes") {
		noteRows[text(r, "id")] = r
	}
	nativeNotes := map[string]legacynative.Note{}
	for _, v := range native.Notes {
		nativeNotes[v.SourceID] = v
	}
	mediaRows := map[string]row{}
	for _, r := range rows("media_assets") {
		mediaRows[text(r, "id")] = r
	}
	threadRows := map[string]row{}
	for _, r := range rows("moment_threads") {
		threadRows[text(r, "id")] = r
	}
	groups := map[string]*Thread{}
	deletedThreads := map[string]bool{}
	for _, n := range base.Notes {
		if n.Deleted {
			if tid := text(noteRows[n.SourceID], "thread_id"); tid != "" {
				deletedThreads[tid] = true
			}
			continue
		}
		rel, e := legacyarchive.DecodeRelationships(n.Payload)
		if e != nil {
			return Plan{}, ErrInvalid
		}
		original, ok := nativeNotes[n.SourceID]
		if !ok {
			p.Deferred["note_not_native"]++
			continue
		}
		origin := Origin{SourceKey: n.SourceID, OwnerSourceID: n.OwnerSourceID, SourceSHA256: n.SourceSHA256, TargetID: original.TargetID, ParentKey: n.SourceID, ParentSHA256: n.SourceSHA256}
		if len(rel.Places) > 1 {
			nl := NoteLocations{Origin: origin, Original: original, Items: []Location{}}
			seen := map[string]bool{}
			for _, v := range rel.Places {
				if v.SourcePlaceID == nil {
					p.Deferred["note_location_raw_only"]++
					continue
				}
				sid := *v.SourcePlaceID
				target := placeMap[sid]
				if target == "" {
					p.Deferred["note_location_unmapped"]++
					continue
				}
				if seen[target] {
					p.Deferred["note_location_duplicate"]++
					continue
				}
				seen[target] = true
				key := fmt.Sprintf("%s:%d:%s", n.SourceID, v.Position, sid)
				nl.Items = append(nl.Items, Location{ID: stable("note-location", n.OwnerSourceID, key), PlaceID: target, SourcePlaceID: sid, SourcePosition: v.Position, SourceSHA256: placeRelationHashes[key]})
			}
			if len(nl.Items) > 50 {
				return Plan{}, ErrInvalid
			}
			p.NoteLocations = append(p.NoteLocations, nl)
		}
		positions := map[string]int64{}
		for _, v := range rel.Media {
			positions[v.SourceMediaID] = v.Position
		}
		for _, v := range rel.MediaLocations {
			raw := mediaRows[v.SourceMediaID]
			position, ok := positions[v.SourceMediaID]
			if !ok || raw == nil {
				return Plan{}, ErrInvalid
			}
			key := n.SourceID + ":" + v.SourceMediaID
			mc := MediaContext{Origin: Origin{SourceKey: key, OwnerSourceID: n.OwnerSourceID, SourceSHA256: mediaContextHashes[key], TargetID: stable("media-context", n.OwnerSourceID, key), ParentKey: n.SourceID, ParentSHA256: n.SourceSHA256}, MomentID: original.TargetID, SourceMediaID: v.SourceMediaID, SourceMediaSHA256: hash(raw), SourcePosition: position, CapturedDate: v.CapturedDate, OriginalMapEnabled: v.MapEnabled, Latitude: v.Latitude, Longitude: v.Longitude, Places: []Location{}}
			for i, sid := range v.SourcePlaceIDs {
				target := placeMap[sid]
				if target == "" {
					p.Deferred["media_place_unmapped"]++
					continue
				}
				mc.Places = append(mc.Places, Location{ID: stable("media-context-place", n.OwnerSourceID, fmt.Sprintf("%s:%d", key, i)), PlaceID: target, SourcePlaceID: sid, SourcePosition: int64(i), SourceSHA256: hash([]any{sid, i})})
			}
			p.MediaContexts = append(p.MediaContexts, mc)
		}
		if rel.Thread != nil {
			tr := rel.Thread
			if tr.Position == nil || *tr.Position > 19 {
				return Plan{}, ErrInvalid
			}
			g := groups[tr.SourceThreadID]
			if g == nil {
				r := threadRows[tr.SourceThreadID]
				if r == nil || text(r, "author_id") != n.OwnerSourceID {
					return Plan{}, ErrInvalid
				}
				g = &Thread{Origin: Origin{SourceKey: tr.SourceThreadID, OwnerSourceID: n.OwnerSourceID, SourceSHA256: hash(r), TargetID: stable("thread", n.OwnerSourceID, tr.SourceThreadID)}, SourceCreatedAt: parsedTime(tr.CreatedAt), RouteRevision: tr.RouteRevision, Members: []Member{}, Legs: []Leg{}}
				groups[tr.SourceThreadID] = g
			}
			if g.OwnerSourceID != n.OwnerSourceID {
				return Plan{}, ErrInvalid
			}
			nr := noteRows[n.SourceID]
			g.Members = append(g.Members, Member{Origin: origin, MomentID: original.TargetID, Position: int(*tr.Position), InboundOrigin: text(nr, "inbound_origin"), InboundMode: text(nr, "inbound_mode"), DurationMinutes: optionalInteger(nr, "inbound_duration_minutes"), EstimatedDurationMinutes: optionalInteger(nr, "inbound_estimated_duration_minutes")})
		}
	}
	for sid, g := range groups {
		sort.Slice(g.Members, func(i, j int) bool { return g.Members[i].Position < g.Members[j].Position })
		if len(g.Members) < 1 || len(g.Members) > 20 {
			return Plan{}, ErrInvalid
		}
		for i, m := range g.Members {
			if i != m.Position {
				return Plan{}, ErrInvalid
			}
		}
		for _, r := range rows("moment_thread_legs") {
			if text(r, "thread_id") != sid {
				continue
			}
			from, ok := nativeNotes[text(r, "from_note_id")]
			to, ok2 := nativeNotes[text(r, "to_note_id")]
			if !ok || !ok2 {
				p.Deferred["thread_leg_deleted_endpoint"]++
				continue
			}
			fp, ok := integer(r, "from_position")
			tp, ok2 := integer(r, "to_position")
			if !ok || !ok2 || fp < 0 || tp < 0 || int(fp) >= len(g.Members) || int(tp) >= len(g.Members) || g.Members[fp].MomentID != from.TargetID || g.Members[tp].MomentID != to.TargetID {
				return Plan{}, ErrInvalid
			}
			key := fmt.Sprintf("%s:%d:%d", sid, fp, tp)
			g.Legs = append(g.Legs, Leg{Origin: Origin{SourceKey: key, OwnerSourceID: g.OwnerSourceID, SourceSHA256: hash(r), TargetID: stable("thread-leg", g.OwnerSourceID, key), ParentKey: sid, ParentSHA256: g.SourceSHA256}, FromMomentID: from.TargetID, ToMomentID: to.TargetID, FromPosition: int(fp), ToPosition: int(tp), Mode: text(r, "mode"), FromPlaceID: placeMap[text(r, "from_anchor_place_id")], ToPlaceID: placeMap[text(r, "to_anchor_place_id")], SourceCreatedAt: parsedTime(text(r, "created_at")), SourceUpdatedAt: parsedTime(text(r, "updated_at"))})
		}
		p.Threads = append(p.Threads, *g)
		delete(deletedThreads, sid)
	}
	sort.Slice(p.Places, func(i, j int) bool { return p.Places[i].SourceID < p.Places[j].SourceID })
	sort.Slice(p.SavedItems, func(i, j int) bool { return p.SavedItems[i].SourceKey < p.SavedItems[j].SourceKey })
	sort.Slice(p.NoteLocations, func(i, j int) bool { return p.NoteLocations[i].SourceKey < p.NoteLocations[j].SourceKey })
	sort.Slice(p.MediaContexts, func(i, j int) bool { return p.MediaContexts[i].SourceKey < p.MediaContexts[j].SourceKey })
	sort.Slice(p.Threads, func(i, j int) bool { return p.Threads[i].SourceKey < p.Threads[j].SourceKey })
	p.Counts = Counts{Places: len(p.Places), SavedItems: len(p.SavedItems), NoteLocations: 0, MediaContexts: len(p.MediaContexts), Threads: len(p.Threads), ThreadTombstones: len(deletedThreads)}
	for _, v := range p.NoteLocations {
		p.Counts.NoteLocations += len(v.Items)
	}
	for _, v := range p.MediaContexts {
		p.Counts.MediaPlaces += len(v.Places)
	}
	for _, v := range p.Threads {
		p.Counts.Members += len(v.Members)
		p.Counts.Legs += len(v.Legs)
	}
	p.ProjectionSHA256 = hash(struct {
		Protocol, Snapshot string
		Places             []Place
		Saved              []legacynative.SavedItem
		Locations          []NoteLocations
		Media              []MediaContext
		Threads            []Thread
		Counts             Counts
		Deferred           map[string]int
	}{Protocol, p.SnapshotSHA256, p.Places, p.SavedItems, p.NoteLocations, p.MediaContexts, p.Threads, p.Counts, p.Deferred})
	return p, nil
}
