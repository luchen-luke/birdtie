package legacyreferencemedia

import (
	"bytes"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"sort"
	"strconv"
)

func text(v any) string { s, _ := v.(string); return s }
func integer(v any) int64 {
	n, _ := v.(json.Number)
	i, _ := strconv.ParseInt(string(n), 10, 64)
	return i
}
func digest(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var x any
	if d.Decode(&x) != nil {
		return ""
	}
	h, _ := legacymigration.Digest(x)
	return h
}
func Build(run string, source, manifest, matched, seal []byte, pin string) (Plan, error) {
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	personal, e := legacycollections.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	personalHashes := map[string]string{}
	for _, r := range personal.Records {
		if r.Kind == "journey" {
			personalHashes[r.SourceKey] = r.SourceSHA256
		}
	}
	if _, e = legacymedia.Build(run, source, manifest, matched, seal, pin); e != nil {
		return Plan{}, ErrInvalid
	}
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		return Plan{}, ErrInvalid
	}
	var matcher struct {
		Selectors []struct {
			ID          string   `json:"sourceMediaId"`
			Owner       string   `json:"sourceOwnerId"`
			Role        string   `json:"role"`
			Status      string   `json:"sourceStatus"`
			Key         string   `json:"sourceStorageKey"`
			Member      string   `json:"archiveMember"`
			MIME        string   `json:"observedMIME"`
			SourceMIME  string   `json:"sourceMIME"`
			SHA         string   `json:"sha256"`
			Bytes       *int64   `json:"bytes"`
			SourceBytes *int64   `json:"sourceBytes"`
			Matched     bool     `json:"matched"`
			Reasons     []string `json:"reasons"`
		} `json:"selectors"`
	}
	if json.Unmarshal(matched, &matcher) != nil {
		return Plan{}, ErrInvalid
	}
	by := map[string]int{}
	for i, s := range matcher.Selectors {
		if s.Role != "original" {
			continue
		}
		if _, ok := by[s.ID]; ok {
			return Plan{}, ErrInvalid
		}
		by[s.ID] = i
	}
	t := doc.Tables
	index := func(name string) map[string]map[string]any {
		m := map[string]map[string]any{}
		for _, r := range t[name] {
			m[text(r["id"])] = r
		}
		return m
	}
	assets, notes, places, journeys := index("media_assets"), index("notes"), index("places"), index("journeys")
	owners := map[string]bool{}
	for _, o := range base.Owners {
		owners[o.SourceID] = true
	}
	liveUse := map[string]string{}
	deletedUse := map[string]bool{}
	used := map[string]bool{}
	for _, r := range t["note_media"] {
		id, nid := text(r["media_id"]), text(r["note_id"])
		used[id] = true
		n := notes[nid]
		if n == nil {
			return Plan{}, ErrInvalid
		}
		if n["deleted_at"] != nil {
			deletedUse[id] = true
			continue
		}
		if liveUse[id] == "" || nid < liveUse[id] {
			liveUse[id] = nid
		}
	}
	p := Plan{RunID: run, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, MatcherFileSHA256: legacymedia.Hash(matched), SealSHA256: pin, Assets: []Asset{}, References: []Reference{}, Artworks: []Artwork{}}
	needed := map[string]bool{}
	systems := map[string]bool{}
	assetOwners := map[string]string{}
	add := func(r Reference) error {
		a := assets[r.AssetSourceID]
		if a == nil || r.ParentSourceID == "" || r.SourceID == "" || r.ParentSHA256 == "" {
			return ErrInvalid
		}
		owner := text(a["owner_id"])
		if !owners[owner] || owner != r.OwnerSourceID {
			return ErrInvalid
		}
		r.ID = StableID(r.Kind, r.SourceID)
		if r.Tombstone {
			p.Counts.RelationTombstones++
		} else {
			needed[r.AssetSourceID] = true
			assetOwners[r.AssetSourceID] = owner
		}
		used[r.AssetSourceID] = true
		p.References = append(p.References, r)
		return nil
	}
	for _, r := range t["place_images"] {
		a := assets[text(r["media_id"])]
		parent := places[text(r["place_id"])]
		if a == nil || parent == nil {
			return Plan{}, ErrInvalid
		}
		status := text(r["status"])
		if status != "approved" && status != "rejected" && status != "removed" && status != "pending" {
			return Plan{}, ErrInvalid
		}
		x := Reference{Kind: "PLACE_IMAGE", TargetType: "PLACE", SourceID: text(r["id"]), SourceSHA256: digest(r), ParentSourceID: text(r["place_id"]), ParentSHA256: digest(parent), AssetSourceID: text(r["media_id"]), OwnerSourceID: text(r["contributor_id"]), SourceType: text(r["source_type"]), ReviewStatus: status, RightsBasis: text(r["rights_basis"]), Attribution: text(r["attribution"]), SourceNoteID: text(r["source_note_id"]), Position: integer(r["sort_order"]), IsCover: r["is_cover"] == true, Tombstone: status == "removed"}
		// A deleted source Note is not, on its own, a Place-image withdrawal.
		if e = add(x); e != nil {
			return Plan{}, e
		}
		p.Counts.PlaceRelations++
		if x.Tombstone {
			p.Counts.PlaceTombstones++
		}
		if liveUse[x.AssetSourceID] != "" {
			p.Counts.ReusedPlaceReferences++
		}
		if !x.Tombstone && liveUse[x.AssetSourceID] == "" {
			p.Counts.PlaceImages++
			if x.IsCover {
				p.Counts.PlaceCovers++
			} else {
				p.Counts.PlaceGalleryImages++
			}
			if status == "rejected" {
				p.Counts.RejectedPlaceImages++
			}
		}
	}
	for _, r := range t["journey_media"] {
		j := journeys[text(r["journey_id"])]
		if j == nil {
			return Plan{}, ErrInvalid
		}
		pub, ok := r["publishable"].(bool)
		if !ok {
			return Plan{}, ErrInvalid
		}
		x := Reference{Kind: "JOURNEY_MEDIA", TargetType: "JOURNEY", SourceID: text(r["id"]), SourceSHA256: digest(r), ParentSourceID: text(r["journey_id"]), ParentSHA256: personalHashes[text(r["journey_id"])], AssetSourceID: text(r["media_id"]), OwnerSourceID: text(j["owner_id"]), SourceType: text(r["source"]), SourceNoteID: text(r["source_note_id"]), ReviewStatus: "none", Position: integer(r["position"]), Publishable: &pub, Tombstone: j["deleted_at"] != nil || r["hidden"] == true}
		if e = add(x); e != nil {
			return Plan{}, e
		}
		p.Counts.JourneyMedia++
	}
	urls := map[string][]string{}
	for id, a := range assets {
		if u := text(a["public_url"]); u != "" {
			urls[u] = append(urls[u], id)
		}
	}
	for _, n := range t["notes"] {
		matches := urls[text(n["cover_url"])]
		if len(matches) > 1 {
			return Plan{}, ErrInvalid
		}
		if len(matches) == 0 {
			continue
		}
		x := Reference{Kind: "MOMENT_COVER", TargetType: "MOMENT", SourceID: text(n["id"]), SourceSHA256: digest(n), ParentSourceID: text(n["id"]), ParentSHA256: digest(n), AssetSourceID: matches[0], OwnerSourceID: text(n["author_id"]), SourceType: "note_cover", ReviewStatus: "none", IsCover: true, Tombstone: n["deleted_at"] != nil}
		if e = add(x); e != nil {
			return Plan{}, e
		}
		p.Counts.MomentCovers++
	}
	for _, g := range t["geo_areas"] {
		id := text(g["artwork_media_id"])
		if id == "" {
			continue
		}
		a := assets[id]
		if a == nil {
			return Plan{}, ErrInvalid
		}
		used[id] = true
		systems[id] = true
		p.Artworks = append(p.Artworks, Artwork{SourceID: id, SourceSHA256: digest(a), GeoID: text(g["id"]), GeoSHA256: digest(g), Version: text(g["artwork_version"]), OwnerSourceID: text(a["owner_id"])})
	}
	for id := range deletedUse {
		if liveUse[id] == "" {
			p.Counts.ArchivedDeletedNoteAssets++
		}
	}
	for id := range assets {
		if !used[id] {
			p.Counts.ArchivedUnreferencedAssets++
		}
	}
	for id := range systems {
		if needed[id] {
			return Plan{}, ErrInvalid
		}
		needed[id] = true
	}
	for id := range needed {
		a := assets[id]
		i, ok := by[id]
		if !ok {
			return Plan{}, ErrInvalid
		}
		s := matcher.Selectors[i]
		owner := text(a["owner_id"])
		system := systems[id]
		if !system && (!owners[owner] || owner != assetOwners[id]) {
			return Plan{}, ErrInvalid
		}
		if s.Owner != owner || s.Status != "ready" || a["status"] != "ready" || a["kind"] != "image" || s.Key == "" || s.Member == "" || s.Key != a["storage_key"] || s.Bytes == nil || *s.Bytes < 1 || *s.Bytes > 16*1024*1024 || !legacymedia.ValidSHA(s.SHA) {
			return Plan{}, ErrInvalid
		}
		reuse := liveUse[id]
		if system {
			reuse = ""
		}
		if reuse == "" {
			if s.MIME != "image/jpeg" && s.MIME != "image/png" || s.SourceMIME != s.MIME || text(a["content_type"]) != s.MIME {
				return Plan{}, ErrInvalid
			}
			if !s.Matched {
				want := map[string]bool{"NOT_SCANNED": true, "RIGHTS_NOT_REVIEWED": true, "SOURCE_SIZE_UNKNOWN": true}
				if len(s.Reasons) != 3 || integer(a["size_bytes"]) != 0 || s.SourceBytes != nil && *s.SourceBytes != 0 {
					return Plan{}, ErrInvalid
				}
				for _, v := range s.Reasons {
					if !want[v] {
						return Plan{}, ErrInvalid
					}
					delete(want, v)
				}
			} else if s.SourceBytes == nil || *s.SourceBytes != *s.Bytes || integer(a["size_bytes"]) != *s.Bytes {
				return Plan{}, ErrInvalid
			}
		}
		p.Assets = append(p.Assets, Asset{ID: StableID("material", id), SourceID: id, SourceSHA256: digest(a), OwnerSourceID: owner, InputSHA256: s.SHA, InputBytes: *s.Bytes, MIME: s.MIME, ReuseNoteID: reuse, System: system})
		if system {
			p.Counts.SystemArtworkArchives++
		} else if reuse != "" {
			p.Counts.ReusedImages++
		} else {
			p.Counts.PrivateImages++
		}
	}
	sort.Slice(p.Assets, func(i, j int) bool { return p.Assets[i].ID < p.Assets[j].ID })
	sort.Slice(p.References, func(i, j int) bool { return p.References[i].ID < p.References[j].ID })
	sort.Slice(p.Artworks, func(i, j int) bool { return p.Artworks[i].GeoID < p.Artworks[j].GeoID })
	p.ProjectionSHA256 = digest([]any{Protocol, run, p.SourceFileSHA256, p.ManifestFileSHA256, p.MatcherFileSHA256, p.SealSHA256, p.Assets, p.References, p.Artworks, p.Counts})
	if !legacymedia.ValidSHA(p.ProjectionSHA256) {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
