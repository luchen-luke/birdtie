// Package legacymapanchor restores only explicit historical map coordinates.
// It does not parse EXIF, infer a city point, create a Place, or publish a draft.
package legacymapanchor

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacycontext"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
	"math"
	"sort"
)

const Protocol = "birdtie-civu-map-anchors-v1"

var ErrInvalid = errors.New("legacy_map_anchor_invalid")

type Counts struct {
	Anchors      int `json:"anchors"`
	MediaAnchors int `json:"mediaAnchors"`
	NoteAnchors  int `json:"noteAnchors"`
}
type Anchor struct {
	ID, MomentID, ContextID, OwnerSourceID, SourceNoteID, SourceNoteSHA256, SourceMediaID, SourceSHA256, Proof, Precision string
	Latitude, Longitude                                                                                                   float64
}
type Plan struct {
	RunID                                                                  string         `json:"runId"`
	SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, ProjectionSHA256 string         `json:"-"`
	Anchors                                                                []Anchor       `json:"-"`
	Counts                                                                 Counts         `json:"counts"`
	Deferred                                                               map[string]int `json:"deferred"`
}

func (Plan) String() string { return "private map anchor proof plan; NOT_IMPORTED" }

func hash(v any) string {
	raw, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	var normalized any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&normalized) != nil {
		return ""
	}
	h, _ := legacymigration.Digest(normalized)
	return h
}
func numeric(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, e := n.Float64()
	return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}
func coordinate(lat, lon any) (float64, float64, bool) {
	a, ok := numeric(lat)
	b, ok2 := numeric(lon)
	return a, b, ok && ok2 && a >= -90 && a <= 90 && b >= -180 && b <= 180 && (a != 0 || b != 0)
}
func Build(run string, source, manifest []byte) (Plan, error) {
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	contexts, e := legacycontext.Build(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	dec := json.NewDecoder(bytes.NewReader(source))
	dec.UseNumber()
	if dec.Decode(&doc) != nil {
		return Plan{}, ErrInvalid
	}
	p := Plan{RunID: run, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, Anchors: []Anchor{}, Deferred: map[string]int{}}
	notes := map[string]legacymigration.NotePlan{}
	explicitMedia := map[string]bool{}
	for _, row := range doc.Tables["notes"] {
		id, _ := row["id"].(string)
		explicitMedia[id] = row["media_locations_set"] == true
	}
	for _, n := range base.Notes {
		if !n.Deleted {
			notes[n.SourceID] = n
		}
	}
	for _, c := range contexts.MediaContexts {
		n, ok := notes[c.ParentKey]
		if !ok || !explicitMedia[c.ParentKey] || !c.OriginalMapEnabled || len(c.Places) > 0 {
			continue
		}
		var lat, lon any
		if c.Latitude != nil {
			lat = *c.Latitude
		}
		if c.Longitude != nil {
			lon = *c.Longitude
		}
		a, b, valid := coordinate(lat, lon)
		if !valid {
			p.Deferred["invalid_coordinate"]++
			continue
		}
		p.Anchors = append(p.Anchors, Anchor{ID: legacynative.StableTargetID("map_anchor", c.OwnerSourceID, c.SourceKey), MomentID: c.MomentID, ContextID: c.TargetID, OwnerSourceID: c.OwnerSourceID, SourceNoteID: n.SourceID, SourceNoteSHA256: n.SourceSHA256, SourceMediaID: c.SourceMediaID, SourceSHA256: c.SourceSHA256, Proof: "CIVU_EXPLICIT_MEDIA_MAP_WGS84_V1", Precision: "point", Latitude: a, Longitude: b})
		p.Counts.MediaAnchors++
	}
	for _, row := range doc.Tables["notes"] {
		id, _ := row["id"].(string)
		n, ok := notes[id]
		if !ok || row["media_locations_set"] == true {
			continue
		}
		hasPlace := false
		for _, r := range doc.Tables["note_places"] {
			if r["note_id"] == id && r["place_id"] != nil {
				hasPlace = true
			}
		}
		if hasPlace {
			continue
		}
		precision, _ := row["map_anchor_precision"].(string)
		provenance, _ := row["map_anchor_source"].(string)
		if precision != "exact" && precision != "approximate" || provenance != "manual" && provenance != "media_location" {
			continue
		}
		a, b, valid := coordinate(row["map_anchor_lat"], row["map_anchor_lng"])
		if !valid {
			p.Deferred["invalid_coordinate"]++
			continue
		}
		if precision == "exact" {
			precision = "point"
		}
		p.Anchors = append(p.Anchors, Anchor{ID: legacynative.StableTargetID("map_anchor", n.OwnerSourceID, id), MomentID: legacynative.StableTargetID("note", n.OwnerSourceID, id), OwnerSourceID: n.OwnerSourceID, SourceNoteID: id, SourceNoteSHA256: n.SourceSHA256, SourceSHA256: n.SourceSHA256, Proof: "CIVU_EXPLICIT_NOTE_MAP_WGS84_V1", Precision: precision, Latitude: a, Longitude: b})
		p.Counts.NoteAnchors++
	}
	p.Counts.Anchors = len(p.Anchors)
	sort.Slice(p.Anchors, func(i, j int) bool { return p.Anchors[i].ID < p.Anchors[j].ID })
	p.ProjectionSHA256 = hash(map[string]any{"protocol": Protocol, "source": p.SourceFileSHA256, "manifest": p.ManifestFileSHA256, "snapshot": p.SnapshotSHA256, "anchors": p.Anchors, "counts": p.Counts, "deferred": p.Deferred})
	if p.ProjectionSHA256 == "" {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
