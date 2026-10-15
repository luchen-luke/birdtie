package legacyjourney

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
)

var ErrInvalid = errors.New("legacy_journey_details_invalid")

func text(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func integer(m map[string]any, k string) *int {
	n, ok := m[k].(json.Number)
	if !ok {
		return nil
	}
	v, e := n.Int64()
	if e != nil {
		return nil
	}
	x := int(v)
	return &x
}
func rows(m map[string]any, k string) []map[string]any {
	r := []map[string]any{}
	a, _ := m[k].([]any)
	for _, v := range a {
		if o, ok := v.(map[string]any); ok {
			r = append(r, o)
		}
	}
	sort.SliceStable(r, func(i, j int) bool {
		a, b := integer(r[i], "position"), integer(r[j], "position")
		if a != nil && b != nil && *a != *b {
			return *a < *b
		}
		return text(r[i], "sourceId") < text(r[j], "sourceId")
	})
	return r
}
func Build(run string, source, manifest []byte) (Plan, error) {
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	archives, e := legacycollections.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	cat, e := legacycatalog.Build(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	p := Plan{RunID: run, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, Journeys: []Journey{}}
	for _, a := range archives.Records {
		if a.Kind != "journey" {
			continue
		}
		if a.Deleted {
			p.Counts.Tombstones++
			continue
		}
		var payload map[string]any
		d := json.NewDecoder(bytes.NewReader(a.Payload))
		d.UseNumber()
		if d.Decode(&payload) != nil {
			return Plan{}, ErrInvalid
		}
		meta, _ := payload["metadata"].(map[string]any)
		if meta == nil {
			return Plan{}, ErrInvalid
		}
		j := Journey{SourceKey: a.SourceKey, SourceOwnerID: a.SourceOwnerID, SourceSHA256: a.SourceSHA256, TargetID: legacynative.StableTargetID("journey", a.SourceOwnerID, a.SourceKey), Title: a.Title, CityID: cat.CityMappings[text(meta, "sourceCityId")], Details: content.JourneyDetails{SchemaVersion: content.JourneyDetailsSchema, StartDate: text(meta, "sourceStartDate"), DateLabel: text(meta, "sourceDateLabel"), Itinerary: []content.JourneyEntry{}}, Origins: []Origin{}}
		if n := integer(meta, "sourceDays"); n != nil {
			j.Details.Days = *n
		}
		if text(meta, "sourceCityId") != "" && j.CityID == "" {
			p.Counts.UnmappedCities++
		}
		stops := map[string]string{}
		for _, r := range rows(payload, "stops") {
			sid := text(r, "sourceId")
			stops[sid] = legacynative.StableTargetID("journey-stop", a.SourceOwnerID, a.SourceKey+":"+sid)
		}
		for _, section := range []struct{ key, typ string }{{"stops", "STOP"}, {"arrangements", "ARRANGEMENT"}, {"notes", "NOTE"}} {
			for _, r := range rows(payload, section.key) {
				sid := text(r, "sourceId")
				entry := content.JourneyEntry{ID: legacynative.StableTargetID("journey-"+section.key, a.SourceOwnerID, a.SourceKey+":"+sid), EntryType: section.typ, Title: text(r, "sourceName"), Note: text(r, "sourcePrivateNote"), CityID: cat.CityMappings[text(r, "sourceCityId")], Kind: text(r, "sourceKind"), DayIndex: integer(r, "sourceDayIndex"), StartDay: integer(r, "sourceStartDay"), EndDay: integer(r, "sourceEndDay"), StartDate: text(r, "sourceStartDate"), EndDate: text(r, "sourceEndDate"), StartTime: text(r, "sourceStartTime"), EndTime: text(r, "sourceEndTime"), TimeLabel: text(r, "sourceTimeLabel"), TransportMode: text(r, "sourceTransportMode"), ServiceNumber: text(r, "sourceServiceNumber"), DepartureName: text(r, "sourceDepartureName"), ArrivalName: text(r, "sourceArrivalName"), CustomAddress: text(r, "sourceCustomAddress"), SourceLabel: text(r, "sourceLabel"), SourceText: text(r, "sourceText")}
				if section.typ == "STOP" {
					entry.ID = stops[sid]
					p.Counts.Stops++
				} else if section.typ == "ARRANGEMENT" {
					entry.Kind = text(r, "sourceType")
					p.Counts.Arrangements++
				} else {
					entry.Note = text(r, "sourceBody")
					p.Counts.Notes++
				}
				origin := content.JourneyEntryOrigin{SourceID: sid, SourceType: section.key, SourcePlaceID: text(r, "sourcePlaceId"), SourceCityID: text(r, "sourceCityId"), SourceDestinationID: text(r, "sourceDestinationId"), SourceStopID: text(r, "sourceStopId"), SourceFromStopID: text(r, "sourceFromStopId"), SourceToStopID: text(r, "sourceToStopId")}
				if n := integer(r, "position"); n != nil {
					origin.SourcePosition = int64(*n)
				}
				if b, ok := r["sourceConfirmed"].(bool); ok {
					origin.SourceConfirmed = &b
				}
				if n, ok := r["sourceConfidence"].(json.Number); ok {
					v, e := n.Float64()
					if e != nil {
						return Plan{}, ErrInvalid
					}
					origin.SourceConfidence = &v
				}
				entry.StopID, entry.FromStopID, entry.ToStopID = stops[origin.SourceStopID], stops[origin.SourceFromStopID], stops[origin.SourceToStopID]
				if origin.SourcePlaceID != "" {
					entry.PlaceID = cat.PlaceMappings[origin.SourcePlaceID]
					if entry.PlaceID == "" {
						p.Counts.UnmappedPlaces++
					}
				}
				// A Place carries its own real City only when the source explicitly
				// supplied that City. Do not infer a Journey or stop destination.
				if entry.CityID == "" && origin.SourceCityID != "" {
					p.Counts.UnmappedCities++
				}
				j.Details.Itinerary = append(j.Details.Itinerary, entry)
				j.Origins = append(j.Origins, Origin{entry.ID, entry.PlaceID, origin})
			}
		}
		var e error
		j.Details, e = content.NormalizeJourneyDetails(j.Details)
		if e != nil {
			return Plan{}, ErrInvalid
		}
		if len(j.Title) > 160 {
			return Plan{}, ErrInvalid
		}
		p.Journeys = append(p.Journeys, j)
		p.Counts.Journeys++
		p.Counts.Entries += len(j.Details.Itinerary)
	}
	value := struct {
		Protocol, Snapshot string
		Journeys           []Journey
		Counts             Counts
	}{Protocol, p.SnapshotSHA256, p.Journeys, p.Counts}
	raw, e := json.Marshal(value)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return Plan{}, ErrInvalid
	}
	p.ProjectionSHA256, e = legacymigration.Digest(v)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
