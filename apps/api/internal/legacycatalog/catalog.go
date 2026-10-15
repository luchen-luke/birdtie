// Package legacycatalog prepares native drafts from the preserved, closed Civu
// snapshot. It grants no publication, ownership, media, or provider rights.
package legacycatalog

import (
	"bytes"
	"crypto/sha1" // UUID v5 identity only, never a security digest.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

const Protocol = "birdtie-legacy-catalog-draft-v1"

var ErrInvalid = errors.New("invalid preserved catalog source")

type City struct {
	SourceID, TargetID, Name, Region, CountryCode, TimeZone, SourceSHA256 string
	ReuseExisting                                                         bool
}

type Place struct {
	SourceID, TargetID, CityID, Name, CategoryCode, Summary, SourceSHA256 string
	Latitude, Longitude                                                   *float64
	SourceLabel, SourceRef                                                string
}

type Counts struct {
	SourceCities     int `json:"sourceCities"`
	SourceGeoAreas   int `json:"sourceGeoAreas"`
	SourcePlaces     int `json:"sourcePlaces"`
	NativeCities     int `json:"nativeCities"`
	NativePlaces     int `json:"nativePlaces"`
	CityMappings     int `json:"cityMappings"`
	PlaceMappings    int `json:"placeMappings"`
	MergedAliases    int `json:"mergedAliases"`
	PointCoordinates int `json:"pointCoordinates"`
}

type Plan struct {
	RunID              string            `json:"runId"`
	SourceFileSHA256   string            `json:"-"`
	ManifestFileSHA256 string            `json:"-"`
	SnapshotSHA256     string            `json:"-"`
	ProjectionSHA256   string            `json:"-"`
	Cities             []City            `json:"-"`
	CityMappings       map[string]string `json:"-"`
	Places             []Place           `json:"-"`
	PlaceMappings      map[string]string `json:"-"`
	// MergedAliases preserves source -> canonical source identity. Its target
	// may be deferred; it is never an assertion that a native Place exists.
	MergedAliases map[string]string `json:"-"`
	Deferred      map[string]int    `json:"deferred"`
	Counts        Counts            `json:"counts"`
}

func (Plan) String() string  { return "preserved catalog draft plan; NOT_IMPORTED" }
func (City) String() string  { return "preserved city draft" }
func (Place) String() string { return "preserved place draft" }

type row = map[string]any

// Build first applies the original full-source/manifest/hash/closed-column
// guard. The second decode retains JSON numbers for original-row digests.
func Build(run string, source, manifest []byte) (Plan, error) {
	original, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, e
	}
	var document row
	d := json.NewDecoder(bytes.NewReader(source))
	d.UseNumber()
	if d.Decode(&document) != nil {
		return Plan{}, ErrInvalid
	}
	tables := document["tables"].(map[string]any)
	rows := func(table string) []row {
		out := []row{}
		for _, v := range tables[table].([]any) {
			out = append(out, v.(map[string]any))
		}
		return out
	}
	areas, e := index(rows("geo_areas"))
	if e != nil {
		return Plan{}, e
	}
	if e = validateAreaTrees(areas); e != nil {
		return Plan{}, e
	}
	oldCities, e := index(rows("cities"))
	if e != nil {
		return Plan{}, e
	}
	places, e := index(rows("places"))
	if e != nil {
		return Plan{}, e
	}
	p := Plan{RunID: run, SourceFileSHA256: original.Run.SourceFileSHA256,
		ManifestFileSHA256: original.Run.ManifestFileSHA256, SnapshotSHA256: original.Run.SnapshotSHA256,
		Cities: []City{}, Places: []Place{}, CityMappings: map[string]string{}, PlaceMappings: map[string]string{},
		MergedAliases: map[string]string{}, Deferred: map[string]int{},
		Counts: Counts{SourceCities: len(oldCities), SourceGeoAreas: len(areas), SourcePlaces: len(places)}}
	// An area may resolve through several immutable merge links. Every merge
	// and parent chain is bounded by the source row count and rejects cycles.
	resolve := func(id string) (row, error) { return resolveArea(areas, id) }
	for _, id := range sortedKeys(areas) {
		a := areas[id]
		if text(a, "status") != "active" || text(a, "kind") != "city" {
			continue
		}
		zone, known := timeZone(text(a, "country_code"))
		if !known {
			p.Deferred["cities_unknown_time_zone"]++
			continue
		}
		name := first(text(a, "name_local"), text(a, "name_en"), text(a, "name_zh"))
		if strings.TrimSpace(name) == "" {
			return Plan{}, ErrInvalid
		}
		region := text(a, "country_code")
		parent, err := resolve(text(a, "parent_id"))
		if err != nil || parent == nil || text(parent, "country_code") != text(a, "country_code") {
			return Plan{}, ErrInvalid
		}
		if text(parent, "kind") != "country" {
			region = first(text(parent, "name_local"), text(parent, "name_en"), text(parent, "name_zh"), region)
		}
		hash, err := legacymigration.Digest(a)
		if err != nil {
			return Plan{}, ErrInvalid
		}
		city := City{SourceID: id, TargetID: "civu-geo-" + id, Name: name, Region: region,
			CountryCode: text(a, "country_code"), TimeZone: zone, SourceSHA256: hash}
		if id == "34611cd6-62bf-41fb-808f-7010b7b7fef6" {
			// This exact preserved administrative identity was reconciled with
			// Birdtie's existing Aberdeen. Names alone never merge cities.
			if text(a, "stable_key") != "city:GB:03ae5ad3ce26b7a77719" || text(a, "country_code") != "GB" ||
				text(a, "name_local") != "Aberdeen" || text(a, "name_en") != "Aberdeen" || text(a, "name_zh") != "Aberdeen" ||
				text(a, "parent_id") != "a1cbc7cc-dbf5-4ad7-99bc-601843a78c0e" || text(parent, "kind") != "admin_area" ||
				text(parent, "stable_key") != "admin_area:GB:3e2db8254d32003009ac" || text(parent, "name_local") != "Aberdeen City" ||
				text(parent, "parent_id") != "9d5e11f7-d995-408d-aa2b-32484170973c" {
				return Plan{}, ErrInvalid
			}
			city.TargetID, city.ReuseExisting = "aberdeen-gb", true
		}
		p.Cities = append(p.Cities, city)
		p.CityMappings[id] = city.TargetID
	}
	for _, id := range sortedKeys(areas) {
		if text(areas[id], "status") == "merged" {
			canonical, err := resolve(id)
			if err != nil {
				return Plan{}, err
			}
			if canonical == nil {
				p.Deferred["geo_areas_unresolved_merges"]++
			}
		}
		a, err := ancestorCity(areas, id)
		if err != nil {
			return Plan{}, err
		}
		if a != nil {
			if target := p.CityMappings[text(a, "id")]; target != "" {
				p.CityMappings[id] = target
			}
		}
	}
	// Old Chinese City IDs are aliases only through one exact, nonempty
	// administrative code in the same country. Labels never decide identity.
	for _, id := range sortedKeys(oldCities) {
		c := oldCities[id]
		code := text(c, "adcode")
		var matches []string
		for _, city := range p.Cities {
			a := areas[city.SourceID]
			if code != "" && text(a, "country_code") == "CN" && text(a, "admin_code") == code {
				matches = append(matches, city.TargetID)
			}
		}
		if len(matches) == 1 {
			if previous := p.CityMappings[id]; previous != "" && previous != matches[0] {
				return Plan{}, ErrInvalid
			}
			p.CityMappings[id] = matches[0]
		} else {
			p.Deferred["legacy_cities_without_unique_admin_code"]++
		}
	}
	refs := map[string][]row{}
	seenRefs := map[string]bool{}
	for _, r := range rows("place_provider_refs") {
		placeID, provider, external := text(r, "place_id"), text(r, "provider"), text(r, "external_id")
		if places[placeID] == nil || provider == "" || external == "" || seenRefs[provider+"\x00"+external] {
			return Plan{}, ErrInvalid
		}
		seenRefs[provider+"\x00"+external] = true
		refs[placeID] = append(refs[placeID], r)
	}
	for _, id := range sortedKeys(places) {
		r := places[id]
		if text(r, "lifecycle_status") == "merged" {
			target, err := canonicalPlace(places, id)
			if err != nil {
				return Plan{}, err
			}
			p.MergedAliases[id] = text(target, "id")
			continue
		}
		if text(r, "status") != "active" || text(r, "admin_status") != "active" || text(r, "lifecycle_status") != "active" {
			p.Deferred["places_inactive"]++
			continue
		}
		cityID := p.CityMappings[text(r, "admin_area_id")]
		if cityID == "" {
			p.Deferred["places_without_city_ancestor"]++
			continue
		}
		name, category := text(r, "name"), text(r, "category_code")
		if strings.TrimSpace(name) == "" || strings.TrimSpace(category) == "" {
			return Plan{}, ErrInvalid
		}
		hash, err := legacymigration.Digest(r)
		if err != nil {
			return Plan{}, ErrInvalid
		}
		place := Place{SourceID: id, TargetID: placeUUID(id), CityID: cityID, Name: name, CategoryCode: category,
			Summary: text(r, "description"), SourceSHA256: hash,
			SourceLabel: first(text(r, "source"), "Civu preserved catalog"), SourceRef: text(r, "source_url")}
		lat, lng, known, conflicting := wgsCoordinates(r, refs[id])
		if known && !conflicting {
			place.Latitude, place.Longitude = &lat, &lng
			p.Counts.PointCoordinates++
		} else if conflicting {
			p.Deferred["place_coordinates_conflicting"]++
		} else {
			p.Deferred["place_coordinates_without_explicit_wgs84"]++
		}
		p.Places = append(p.Places, place)
		p.PlaceMappings[id] = place.TargetID
	}
	p.Counts.NativeCities, p.Counts.NativePlaces = len(p.Cities), len(p.Places)
	p.Counts.CityMappings, p.Counts.PlaceMappings, p.Counts.MergedAliases = len(p.CityMappings), len(p.PlaceMappings), len(p.MergedAliases)
	// Include all projections and mappings in a deterministic SHA-256 without
	// exposing them through Plan's ordinary JSON or String diagnostics.
	raw, e := json.Marshal(struct {
		Protocol, RunID, Source, Manifest, Snapshot string
		Cities                                      []City
		Places                                      []Place
		CityMappings, PlaceMappings, MergedAliases  map[string]string
		Deferred                                    map[string]int
	}{Protocol, run, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.Cities, p.Places, p.CityMappings, p.PlaceMappings, p.MergedAliases, p.Deferred})
	if e != nil {
		return Plan{}, ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var projection any
	if d.Decode(&projection) != nil {
		return Plan{}, ErrInvalid
	}
	p.ProjectionSHA256, e = legacymigration.Digest(projection)
	return p, e
}

func index(rows []row) (map[string]row, error) {
	out := map[string]row{}
	for _, r := range rows {
		id := text(r, "id")
		if !legacyarchive.UUID(id) || out[id] != nil {
			return nil, ErrInvalid
		}
		out[id] = r
	}
	return out, nil
}

func sortedKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func text(r row, key string) string { v, _ := r[key].(string); return v }
func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveArea(areas map[string]row, id string) (row, error) {
	seen := map[string]bool{}
	for id != "" {
		if seen[id] {
			return nil, ErrInvalid
		}
		if areas[id] == nil {
			return nil, nil
		}
		seen[id] = true
		a := areas[id]
		if text(a, "status") == "merged" {
			id = text(a, "canonical_id")
			if id == "" {
				return nil, nil
			}
			continue
		}
		if text(a, "status") != "active" {
			return nil, nil
		}
		return a, nil
	}
	return nil, nil
}

func ancestorCity(areas map[string]row, id string) (row, error) {
	seen := map[string]bool{}
	for id != "" {
		a, e := resolveArea(areas, id)
		if e != nil || a == nil {
			return nil, e
		}
		id = text(a, "id")
		if seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		if text(a, "kind") == "city" {
			return a, nil
		}
		// A country/admin/travel area never becomes a city by name or center.
		if text(a, "kind") == "country" || text(a, "kind") == "travel_region" {
			return nil, nil
		}
		id = text(a, "parent_id")
	}
	return nil, nil
}

func validateAreaTrees(areas map[string]row) error {
	for id, original := range areas {
		country := text(original, "country_code")
		seen := map[string]bool{}
		for id != "" {
			a, e := resolveArea(areas, id)
			if e != nil {
				return e
			}
			if a == nil {
				break
			}
			id = text(a, "id")
			if seen[id] || country == "" || text(a, "country_code") != country {
				return ErrInvalid
			}
			seen[id] = true
			parent := text(a, "parent_id")
			if text(a, "kind") == "country" {
				if parent != "" {
					return ErrInvalid
				}
				break
			}
			if parent == "" {
				return ErrInvalid
			}
			id = parent
		}
	}
	return nil
}

func canonicalPlace(places map[string]row, id string) (row, error) {
	seen := map[string]bool{}
	for id != "" {
		if seen[id] || places[id] == nil {
			return nil, ErrInvalid
		}
		seen[id] = true
		p := places[id]
		if text(p, "lifecycle_status") != "merged" {
			if text(p, "lifecycle_status") != "active" || text(p, "status") != "active" || text(p, "admin_status") != "active" {
				return nil, ErrInvalid
			}
			return p, nil
		}
		id = text(p, "canonical_place_id")
	}
	return nil, ErrInvalid
}

func timeZone(country string) (string, bool) {
	zones := map[string]string{"CN": "Asia/Shanghai", "IS": "Atlantic/Reykjavik", "GB": "Europe/London"}
	v, ok := zones[country]
	return v, ok
}

func number(v any) (float64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	f, e := n.Float64()
	return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

func wgsCoordinates(place row, refs []row) (float64, float64, bool, bool) {
	lat, latOK := number(place["lat"])
	lng, lngOK := number(place["lng"])
	if !latOK || !lngOK || lat < -90 || lat > 90 || lng < -180 || lng > 180 || (lat == 0 && lng == 0) {
		return 0, 0, false, true
	}
	known, conflict := false, false
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if raw, present := v["storedCoordinateSystem"]; present {
				stored, ok := raw.(string)
				if !ok {
					conflict = true
				} else if strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(stored)), "-", "") == "WGS84" {
					known = true
				} else if strings.TrimSpace(stored) != "" {
					conflict = true
				}
			}
			for _, keys := range [][2]string{{"wgs84Lat", "wgs84Lng"}, {"wgs84_lat", "wgs84_lng"}} {
				a, presentA := v[keys[0]]
				b, presentB := v[keys[1]]
				if !presentA && !presentB {
					continue
				}
				x, xOK := number(a)
				y, yOK := number(b)
				if !xOK || !yOK || math.Abs(x-lat) > 1e-6 || math.Abs(y-lng) > 1e-6 {
					conflict = true
				} else {
					known = true
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	for _, ref := range refs {
		walk(ref["source_payload"])
	}
	return lat, lng, known, conflict
}

func placeUUID(source string) string {
	// Fixed Birdtie-owned namespace. Changing it would break permanent source
	// identity, so it is part of this versioned projection contract.
	namespace, _ := hex.DecodeString("bf54d771f76551bda599b40213897cb7")
	h := sha1.New()
	_, _ = h.Write(namespace)
	_, _ = h.Write([]byte("hangzhou-civu-20261014:place:" + source))
	id := h.Sum(nil)[:16]
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}
