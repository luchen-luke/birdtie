package legacymap

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

type row = map[string]any

func text(v any) string { s, _ := v.(string); return s }
func hash(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	var normalized any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(&normalized) != nil {
		return ""
	}
	s, _ := legacymigration.Digest(normalized)
	return s
}

var osmIdentity = regexp.MustCompile(`^(node|way|relation)/[1-9][0-9]*$`)
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func numeric(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, e := n.Float64()
	return f, e == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
}

// This is a source-identity proof, not a country/range inference. Civu's
// 4438472 promotion command copies the OSM provider coordinates unchanged and
// adds exactly these two provenance keys. The OSM identity must be canonical;
// conflicting/unknown providers or explicit non-WGS storage remain unlocated.
func coordinateProof(p row, refs []row) (string, *float64, *float64) {
	lat, a := numeric(p["lat"])
	lng, b := numeric(p["lng"])
	if !a || !b || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return "UNPROVEN", nil, nil
	}
	proof := ""
	for _, ref := range refs {
		payload, _ := ref["source_payload"].(map[string]any)
		stored := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(text(payload["storedCoordinateSystem"]), "-", ""), "_", ""))
		if stored != "" && stored != "WGS84" {
			return "CONFLICTING", nil, nil
		}
		if stored == "WGS84" {
			proof = "EXPLICIT_STORED_WGS84_V1"
			continue
		}
		provider := strings.ToLower(text(ref["provider"]))
		if (provider == "osm" || provider == "openstreetmap") && osmIdentity.MatchString(text(ref["external_id"])) &&
			(payload["promotedFrom"] == "local-civu-catalog") && uuid.MatchString(text(payload["sourcePlaceId"])) &&
			(text(p["source"]) == "osm" || text(p["source"]) == "openstreetmap") {
			if proof == "" {
				proof = "OSM_PROVIDER_IDENTITY_PROMOTION_V1"
			}
		}
	}
	if proof == "" {
		return "UNPROVEN", nil, nil
	}
	return proof, &lat, &lng
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
	var doc map[string]any
	d := json.NewDecoder(bytes.NewReader(source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		return Plan{}, ErrInvalid
	}
	tables := doc["tables"].(map[string]any)
	refs := map[string][]row{}
	for _, v := range tables["place_provider_refs"].([]any) {
		r := v.(map[string]any)
		refs[text(r["place_id"])] = append(refs[text(r["place_id"])], r)
	}
	p := Plan{RunID: run, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, Places: []Place{}, Deferred: map[string]int{}}
	for _, v := range tables["places"].([]any) {
		r := v.(map[string]any)
		id := text(r["id"])
		// The native stable namespace also covers the 162 NULL-city Places.
		if text(r["status"]) == "merged" || text(r["lifecycle_status"]) == "merged" || text(r["canonical_place_id"]) != "" {
			p.Deferred["merged_place"]++
			continue
		}
		pub := text(r["status"]) == "active" && text(r["admin_status"]) == "active" && text(r["lifecycle_status"]) == "active"
		proof, lat, lng := coordinateProof(r, refs[id])
		p.Places = append(p.Places, Place{SourceID: id, SourceSHA256: hash(r), TargetID: legacycatalog.StablePlaceTargetID(id), Public: pub, Latitude: lat, Longitude: lng, CoordinateProof: proof})
		p.Counts.Places++
		if pub {
			p.Counts.PublicPlaces++
		}
		if lat != nil {
			p.Counts.CoordinatePlaces++
		} else {
			p.Deferred["coordinates_unproven"]++
		}
	}
	// Original public status is already preserved immutably in phase146.
	for _, n := range base.Notes {
		if !n.Deleted && n.SourceVisibility == "public" && n.SourceModerationStatus == "ok" {
			p.Counts.PublicMoments++
		}
	}
	sort.Slice(p.Places, func(i, j int) bool { return p.Places[i].SourceID < p.Places[j].SourceID })
	p.ProjectionSHA256 = hash(map[string]any{"protocol": Protocol, "source": p.SourceFileSHA256, "manifest": p.ManifestFileSHA256, "snapshot": p.SnapshotSHA256, "places": p.Places, "counts": p.Counts, "deferred": p.Deferred, "catalog": cat.ProjectionSHA256})
	if p.ProjectionSHA256 == "" {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
