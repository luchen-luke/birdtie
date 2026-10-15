package legacynative

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

type sourceTables map[string][]map[string]any

type catalogIndex struct {
	cities map[string]string
	places map[string]legacycatalog.Place
}

// Build verifies the complete preservation source before deriving any target.
// IDs are independent of run/time/content, so importing the same source identity
// cannot create a second editable object. Source hashes remain the exact hashes
// of the existing immutable 146/149 archives, not hashes of this projection.
func Build(run string, source, manifest []byte) (Plan, error) {
	base, err := legacymigration.BuildPlan(run, source, manifest)
	if err != nil {
		return Plan{}, ErrInvalid
	}
	archives, err := legacycollections.BuildPlan(run, source, manifest)
	if err != nil {
		return Plan{}, ErrInvalid
	}
	catalog, err := legacycatalog.Build(run, source, manifest)
	if err != nil {
		return Plan{}, ErrInvalid
	}
	tables, err := decodeTables(source)
	if err != nil {
		return Plan{}, ErrInvalid
	}
	index, err := indexCatalog(catalog)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{
		RunID: run, SourceFileSHA256: base.Run.SourceFileSHA256,
		ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256,
		Notes: []Note{}, Collections: []Collection{}, SavedItems: []SavedItem{},
		Residuals: []Residual{}, Deferred: map[string]int{},
	}
	rows := map[string]map[string]any{}
	for _, row := range tables["notes"] {
		rows[row["id"].(string)] = row
	}
	for _, sourceNote := range base.Notes {
		p.Counts.SourceNotes++
		if sourceNote.Deleted {
			p.Counts.NoteTombstones++
			p.recordResidual("note", sourceNote.SourceID, sourceNote.OwnerSourceID, sourceNote.SourceSHA256, "", []string{"source_deleted_tombstone"})
			continue
		}
		note, reasons, err := buildNote(sourceNote, rows[sourceNote.SourceID], index)
		if err != nil {
			return Plan{}, err
		}
		if note.TargetID == "" {
			p.Counts.DeferredNotes++
		} else {
			p.Notes = append(p.Notes, note)
			p.Counts.NativeNotes++
			if note.GeneratedTitle {
				p.Counts.GeneratedTitles++
			}
			if len(reasons) != 0 {
				p.Counts.PartialNotes++
			}
		}
		p.recordResidual("note", sourceNote.SourceID, sourceNote.OwnerSourceID, sourceNote.SourceSHA256, note.TargetID, reasons)
	}
	for _, archive := range archives.Records {
		if archive.Kind == legacycollections.SavedPlace {
			p.Counts.SourceSavedItems++
			item, reasons, err := buildSaved(archive, index)
			if err != nil {
				return Plan{}, err
			}
			if item.TargetID == "" {
				p.Counts.DeferredSavedItems++
			} else {
				p.SavedItems = append(p.SavedItems, item)
				p.Counts.NativeSavedItems++
			}
			p.recordResidual(archive.Kind, archive.SourceKey, archive.SourceOwnerID, archive.SourceSHA256, item.TargetID, reasons)
			continue
		}
		p.Counts.SourceCollections++
		if archive.Deleted {
			p.Counts.CollectionTombstones++
			p.recordResidual(archive.Kind, archive.SourceKey, archive.SourceOwnerID, archive.SourceSHA256, "", []string{"source_deleted_tombstone"})
			continue
		}
		collection, reasons, err := buildCollection(archive, index)
		if err != nil {
			return Plan{}, err
		}
		if collection.TargetID == "" {
			p.Counts.DeferredCollections++
		} else {
			p.Collections = append(p.Collections, collection)
			if collection.Kind == legacycollections.Journey {
				p.Counts.NativeJourneys++
			} else {
				p.Counts.NativeThemeMaps++
			}
			if len(reasons) != 0 {
				p.Counts.PartialCollections++
			}
		}
		p.recordResidual(archive.Kind, archive.SourceKey, archive.SourceOwnerID, archive.SourceSHA256, collection.TargetID, reasons)
	}
	// Source builders sort their records. Sort every derived list too, avoiding
	// map iteration in the projection while still binding the exact snapshot.
	sort.Slice(p.Notes, func(i, j int) bool { return p.Notes[i].SourceKey < p.Notes[j].SourceKey })
	sort.Slice(p.Collections, func(i, j int) bool {
		if p.Collections[i].Kind != p.Collections[j].Kind {
			return p.Collections[i].Kind < p.Collections[j].Kind
		}
		return p.Collections[i].SourceKey < p.Collections[j].SourceKey
	})
	sort.Slice(p.SavedItems, func(i, j int) bool { return p.SavedItems[i].SourceKey < p.SavedItems[j].SourceKey })
	sort.Slice(p.Residuals, func(i, j int) bool {
		if p.Residuals[i].Kind != p.Residuals[j].Kind {
			return p.Residuals[i].Kind < p.Residuals[j].Kind
		}
		return p.Residuals[i].SourceKey < p.Residuals[j].SourceKey
	})
	p.ProjectionSHA256, err = projectionHash(p)
	if err != nil {
		return Plan{}, ErrInvalid
	}
	return p, nil
}

func decodeTables(raw []byte) (sourceTables, error) {
	var source struct {
		Tables sourceTables `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&source); err != nil || source.Tables == nil {
		return nil, ErrInvalid
	}
	return source.Tables, nil
}

func indexCatalog(plan legacycatalog.Plan) (catalogIndex, error) {
	index := catalogIndex{cities: plan.CityMappings, places: map[string]legacycatalog.Place{}}
	byTarget := map[string]legacycatalog.Place{}
	for _, place := range plan.Places {
		byTarget[place.TargetID] = place
	}
	for sourceID, targetID := range plan.PlaceMappings {
		place, ok := byTarget[targetID]
		if !ok || !legacyarchive.UUID(targetID) || place.CityID == "" {
			return catalogIndex{}, ErrInvalid
		}
		index.places[sourceID] = place
	}
	return index, nil
}

func stableTarget(kind, owner, key string) string {
	value, _ := legacymigration.Canonical([]any{Protocol, legacymigration.SourceSystem, kind, owner, key})
	sum := sha256.Sum256(value)
	// UUID version 8 identifies this explicit SHA-256 namespace construction.
	sum[6] = (sum[6] & 0x0f) | 0x80
	sum[8] = (sum[8] & 0x3f) | 0x80
	h := hex.EncodeToString(sum[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func optionalTime(row map[string]any, key string) (*time.Time, error) {
	if row[key] == nil {
		return nil, nil
	}
	value, ok := row[key].(string)
	if !ok {
		return nil, ErrInvalid
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || t.IsZero() {
		return nil, ErrInvalid
	}
	t = t.UTC()
	return &t, nil
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func numberValue(value any) (int64, bool) {
	n, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}

func optionalString(value any) *string {
	s, ok := value.(string)
	if !ok {
		return nil
	}
	return &s
}

func uniqueReasons(reasons []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, reason := range reasons {
		if !seen[reason] {
			seen[reason] = true
			out = append(out, reason)
		}
	}
	sort.Strings(out)
	return out
}

func (p *Plan) recordResidual(kind, key, owner, sourceHash, target string, reasons []string) {
	reasons = uniqueReasons(reasons)
	if len(reasons) == 0 {
		return
	}
	p.Residuals = append(p.Residuals, Residual{Kind: kind, SourceKey: key, OwnerSourceID: owner, SourceSHA256: sourceHash, TargetID: target, Reasons: reasons})
	for _, reason := range reasons {
		p.Deferred[reason]++
	}
}

func buildNote(source legacymigration.NotePlan, row map[string]any, index catalogIndex) (Note, []string, error) {
	if row == nil {
		return Note{}, nil, ErrInvalid
	}
	rel, err := legacyarchive.DecodeRelationships(source.Payload)
	if err != nil {
		return Note{}, nil, ErrInvalid
	}
	reasons := []string{}
	city, anchor := "", ""
	destination := optionalString(row["destination_id"])
	if destination != nil {
		city = index.cities[*destination]
		if city != "" {
			anchor = "explicit_destination_city_mapping"
		} else {
			reasons = append(reasons, "legacy_destination_unmapped")
		}
	}
	placeCity := ""
	allPlacesMapped := len(rel.Places) != 0
	for _, selector := range rel.Places {
		if selector.SourcePlaceID == nil {
			allPlacesMapped = false
			continue
		}
		place, ok := index.places[*selector.SourcePlaceID]
		if !ok {
			allPlacesMapped = false
			continue
		}
		if placeCity == "" {
			placeCity = place.CityID
		} else if placeCity != place.CityID {
			allPlacesMapped = false
			reasons = append(reasons, "legacy_cross_city_places_archive_only")
		}
	}
	if city == "" && allPlacesMapped {
		city, anchor = placeCity, "unique_mapped_places_city"
	}
	if city == "" {
		reasons = append(reasons, "city_unresolved")
		anchor = "unrecorded_city"
	}
	if len(rel.Places) == 1 && allPlacesMapped && placeCity != city {
		reasons = append(reasons, "conflicting_city_anchors")
		city, anchor = "", "unrecorded_city"
	}
	placeID, precision := "", "city"
	if city == "" {
		precision = "none"
	}
	if len(rel.Places) == 1 && allPlacesMapped && placeCity == city {
		placeID, precision = index.places[*rel.Places[0].SourcePlaceID].TargetID, "place"
	}
	if len(rel.Places) > 1 {
		reasons = append(reasons, "legacy_multiple_places_archive_only")
	}
	if len(rel.Places) != 0 && !allPlacesMapped {
		reasons = append(reasons, "legacy_unmapped_places_archive_only")
	}
	title := source.Title
	generated := false // Preserve the author's actual title, including its absence.
	if len(title) > 160 || len(source.Body) > 5000 {
		return Note{}, append(reasons, "native_text_limit_exceeded"), nil
	}
	input, err := content.NormalizeMomentInput(content.MomentInput{
		CityID: city, PlaceID: placeID, Title: title, Body: source.Body,
		TimePrecision: "unknown", LocationPrecision: precision,
	})
	if err != nil {
		return Note{}, append(reasons, "native_moment_shape_incompatible"), nil
	}
	if input.Title != title || input.Body != source.Body {
		reasons = append(reasons, "source_text_spacing_archive_only")
	}
	if source.RecordedStartDate != nil || source.RecordedEndDate != nil {
		reasons = append(reasons, "legacy_recorded_date_range_archive_only")
	}
	if len(rel.Media) != 0 || stringValue(row["cover_url"]) != "" {
		reasons = append(reasons, "legacy_media_unavailable")
	}
	if len(rel.MediaLocations) != 0 {
		reasons = append(reasons, "legacy_media_locations_archive_only")
	}
	if rel.Thread != nil || len(rel.ThreadLegs) != 0 {
		reasons = append(reasons, "legacy_thread_archive_only")
	}
	if hasValue(row, "journey_id", "journey_day_index", "suspended_journey_id", "suspended_journey_day_index", "map_anchor_lat", "map_anchor_lng", "inbound_origin", "inbound_mode", "inbound_duration_minutes", "inbound_estimated_duration_minutes", "tag", "location_label", "language") {
		reasons = append(reasons, "legacy_moment_metadata_archive_only")
	}
	created, err := optionalTime(row, "created_at")
	if err != nil {
		return Note{}, nil, err
	}
	updated, err := optionalTime(row, "updated_at")
	if err != nil {
		return Note{}, nil, err
	}
	reasons = uniqueReasons(reasons)
	return Note{
		SourceID: source.SourceID, SourceKey: source.SourceID, OwnerSourceID: source.OwnerSourceID,
		SourceSHA256: source.SourceSHA256, SourceVersion: source.SourceVersion,
		TargetID: stableTarget("note", source.OwnerSourceID, source.SourceID),
		CityID:   input.CityID, PlaceID: input.PlaceID, Title: input.Title, Body: input.Body,
		TimePrecision: "unknown", LocationPrecision: input.LocationPrecision, Visibility: "private", Status: "draft",
		GeneratedTitle: generated, AnchorSource: anchor, SourceDestinationID: destination,
		SourceDestinationSource: stringValue(row["destination_source"]), SourceCreatedAt: created, SourceUpdatedAt: updated,
		RecordedStartDate: source.RecordedStartDate, RecordedEndDate: source.RecordedEndDate, ResidualReasons: reasons,
	}, reasons, nil
}

func hasValue(row map[string]any, fields ...string) bool {
	for _, key := range fields {
		switch value := row[key].(type) {
		case nil:
			continue
		case string:
			if value == "" {
				continue
			}
		case bool:
			if !value {
				continue
			}
		}
		return true
	}
	return false
}

func payloadMap(raw []byte) (map[string]any, error) {
	var value map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil || value == nil {
		return nil, ErrInvalid
	}
	return value, nil
}

func rowsFrom(payload map[string]any, key string) []map[string]any {
	list, _ := payload[key].([]any)
	rows := make([]map[string]any, 0, len(list))
	for _, value := range list {
		row, _ := value.(map[string]any)
		rows = append(rows, row)
	}
	return rows
}

func orderRows(rows []map[string]any) bool {
	seen := map[int64]bool{}
	for _, row := range rows {
		position, ok := numberValue(row["position"])
		if !ok || position < 0 || seen[position] {
			return false
		}
		seen[position] = true
	}
	sort.Slice(rows, func(i, j int) bool {
		a, _ := numberValue(rows[i]["position"])
		b, _ := numberValue(rows[j]["position"])
		return a < b
	})
	return true
}

func buildSaved(source legacycollections.Archive, index catalogIndex) (SavedItem, []string, error) {
	payload, err := payloadMap(source.Payload)
	if err != nil {
		return SavedItem{}, nil, err
	}
	sourcePlace := stringValue(payload["sourcePlaceId"])
	place, ok := index.places[sourcePlace]
	if !ok {
		return SavedItem{}, []string{"saved_place_unmapped"}, nil
	}
	reasons := []string{}
	if len(rowsFrom(payload, "sources")) != 0 {
		reasons = append(reasons, "legacy_saved_attribution_archive_only")
	}
	return SavedItem{
		SourceID: sourcePlace, SourceKey: source.SourceKey, OwnerSourceID: source.SourceOwnerID,
		SourceSHA256: source.SourceSHA256, TargetID: stableTarget("saved_place", source.SourceOwnerID, source.SourceKey),
		PlaceID: place.TargetID, CityID: place.CityID, Title: place.Name, SourceCreatedAt: source.SourceCreatedAt,
	}, reasons, nil
}

func buildCollection(source legacycollections.Archive, index catalogIndex) (Collection, []string, error) {
	payload, err := payloadMap(source.Payload)
	if err != nil {
		return Collection{}, nil, err
	}
	meta, _ := payload["metadata"].(map[string]any)
	if meta == nil {
		return Collection{}, nil, ErrInvalid
	}
	if strings.TrimSpace(source.Title) == "" || len(source.Title) > 160 {
		return Collection{}, []string{"native_collection_title_incompatible"}, nil
	}
	var entries []content.CollectionEntry
	var entrySources []EntrySource
	var city, summary string
	var reasons []string
	if source.Kind == legacycollections.ThemeMap {
		entries, entrySources, city, reasons = themeEntries(payload, index)
		summary = stringValue(meta["sourceSummary"])
		if len(rowsFrom(payload, "entrySources")) != 0 {
			reasons = append(reasons, "legacy_theme_attribution_archive_only")
		}
	} else {
		entries, entrySources, city, summary, reasons = journeyEntries(payload, meta, index)
	}
	if len(entries) == 0 {
		return Collection{}, uniqueReasons(reasons), nil
	}
	input, err := content.NormalizeCollectionInput(content.CollectionInput{Kind: source.Kind, CityID: city, Title: source.Title, Summary: summary, Entries: entries})
	if err != nil || len(summary) > 5000 {
		return Collection{}, append(reasons, "native_collection_shape_incompatible"), nil
	}
	if input.Title != source.Title || input.Summary != summary {
		reasons = append(reasons, "source_text_spacing_archive_only")
	}
	for i := range entries {
		if input.Entries[i].Note != entries[i].Note {
			reasons = append(reasons, "source_text_spacing_archive_only")
		}
	}
	reasons = uniqueReasons(reasons)
	return Collection{
		Kind: source.Kind, SourceID: source.SourceKey, SourceKey: source.SourceKey, OwnerSourceID: source.SourceOwnerID,
		SourceSHA256: source.SourceSHA256, SourceRevision: source.SourceRevision,
		TargetID: stableTarget(source.Kind, source.SourceOwnerID, source.SourceKey),
		CityID:   input.CityID, Title: input.Title, Summary: input.Summary, Visibility: "private", Status: "draft",
		Entries: input.Entries, EntrySources: entrySources, SourceCreatedAt: source.SourceCreatedAt,
		SourceUpdatedAt: source.SourceUpdatedAt, ResidualReasons: reasons,
	}, reasons, nil
}

func themeEntries(payload map[string]any, index catalogIndex) ([]content.CollectionEntry, []EntrySource, string, []string) {
	rows := rowsFrom(payload, "entries")
	if len(rows) < 1 || len(rows) > 50 {
		return nil, nil, "", []string{"native_collection_entry_count_incompatible"}
	}
	if !orderRows(rows) {
		return nil, nil, "", []string{"legacy_entry_order_incompatible"}
	}
	entries, sources := []content.CollectionEntry{}, []EntrySource{}
	city, reasons, seen := "", []string{}, map[string]bool{}
	for _, row := range rows {
		sourcePlace := stringValue(row["sourcePlaceId"])
		place, ok := index.places[sourcePlace]
		if !ok {
			return nil, nil, "", []string{"collection_place_unmapped"}
		}
		if city == "" {
			city = place.CityID
		}
		if city != place.CityID {
			return nil, nil, "", []string{"legacy_cross_city_collection"}
		}
		if seen[place.TargetID] {
			return nil, nil, "", []string{"legacy_repeated_place_collection"}
		}
		seen[place.TargetID] = true
		section := stringValue(row["sourceSection"])
		creatorNote := stringValue(row["sourceCreatorNote"])
		collaboration, _ := row["sourceCollaborationDisclosure"].(bool)
		note := creatorNote
		if section != "" {
			note = "原分组：" + section
			if creatorNote != "" {
				note += "\n原说明：" + creatorNote
			}
			reasons = append(reasons, "legacy_theme_section_labeled_in_note")
		}
		if collaboration {
			if note != "" {
				note += "\n"
			}
			note += "原协作披露：是"
			reasons = append(reasons, "legacy_theme_collaboration_labeled_in_note")
		}
		if len(note) > 2000 {
			return nil, nil, "", []string{"native_collection_note_limit_exceeded"}
		}
		primary := optionalString(row["sourcePrimaryMomentId"])
		if primary != nil {
			reasons = append(reasons, "legacy_theme_primary_moment_archive_only")
		}
		position, _ := numberValue(row["position"])
		sources = append(sources, EntrySource{NativePosition: len(entries), SourcePosition: position, SourcePlaceID: sourcePlace,
			Section: section, CreatorNote: creatorNote, CollaborationDisclosure: collaboration, SourcePrimaryMomentID: primary})
		entries = append(entries, content.CollectionEntry{PlaceID: place.TargetID, Note: note, LocationVisibility: "hidden"})
	}
	return entries, sources, city, reasons
}

func journeyEntries(payload, meta map[string]any, index catalogIndex) ([]content.CollectionEntry, []EntrySource, string, string, []string) {
	// Stop/destination ranges and transport/lodging schedules cannot become a
	// place-only Collection without losing their meaning. Defer the entire route.
	if len(rowsFrom(payload, "stops")) != 0 {
		return nil, nil, "", "", []string{"legacy_journey_stops_require_schedule_model"}
	}
	rows := rowsFrom(payload, "arrangements")
	if len(rows) < 1 || len(rows) > 50 {
		return nil, nil, "", "", []string{"native_collection_entry_count_incompatible"}
	}
	if !orderRows(rows) {
		return nil, nil, "", "", []string{"legacy_entry_order_incompatible"}
	}
	city := index.cities[stringValue(meta["sourceCityId"])]
	if meta["sourceCityId"] != nil && city == "" {
		return nil, nil, "", "", []string{"journey_city_unmapped"}
	}
	entries, sources, reasons := []content.CollectionEntry{}, []EntrySource{}, []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		if row["sourceType"] != "place" || hasValue(row,
			"sourceDayIndex", "sourceTimeLabel", "sourceStartDate", "sourceStartTime", "sourceEndDate", "sourceEndTime",
			"sourceTransportMode", "sourceServiceNumber", "sourceDepartureName", "sourceArrivalName", "sourceStopId",
			"sourceFromStopId", "sourceToStopId", "sourceCustomAddress", "sourceCustomLatitude", "sourceCustomLongitude", "sourceText") {
			return nil, nil, "", "", []string{"legacy_journey_arrangement_requires_schedule_model"}
		}
		sourcePlace := stringValue(row["sourcePlaceId"])
		place, ok := index.places[sourcePlace]
		if !ok {
			return nil, nil, "", "", []string{"collection_place_unmapped"}
		}
		if city == "" {
			city = place.CityID
		}
		if city != place.CityID {
			return nil, nil, "", "", []string{"legacy_cross_city_collection"}
		}
		if seen[place.TargetID] {
			return nil, nil, "", "", []string{"legacy_repeated_place_collection"}
		}
		seen[place.TargetID] = true
		note := stringValue(row["sourcePrivateNote"])
		name := stringValue(row["sourceName"])
		if name != "" && name != place.Name {
			if note == "" {
				note = "原名称：" + name
			} else {
				note = "原名称：" + name + "\n原备注：" + note
			}
		}
		if len(note) > 2000 {
			return nil, nil, "", "", []string{"native_collection_note_limit_exceeded"}
		}
		position, _ := numberValue(row["position"])
		sources = append(sources, EntrySource{NativePosition: len(entries), SourcePosition: position, SourcePlaceID: sourcePlace, CreatorNote: stringValue(row["sourcePrivateNote"])})
		entries = append(entries, content.CollectionEntry{PlaceID: place.TargetID, Note: note, LocationVisibility: "hidden"})
	}
	notes := rowsFrom(payload, "notes")
	if len(notes) != 0 && !orderRows(notes) {
		return nil, nil, "", "", []string{"legacy_journey_note_order_incompatible"}
	}
	summaries := []string{}
	for _, note := range notes {
		position, _ := numberValue(note["position"])
		summaries = append(summaries, fmt.Sprintf("原旅程备注（序号%d）：\n%s", position, stringValue(note["sourceBody"])))
	}
	if len(notes) != 0 {
		reasons = append(reasons, "legacy_journey_notes_labeled_in_summary")
	}
	if hasValue(meta, "sourceStartDate", "sourceDateLabel") || meta["sourceDays"] != json.Number("0") {
		reasons = append(reasons, "legacy_journey_date_metadata_archive_only")
	}
	for _, field := range []string{"members", "media", "publications", "publicationPlaces"} {
		if len(rowsFrom(payload, field)) != 0 {
			reasons = append(reasons, "legacy_journey_"+field+"_archive_only")
		}
	}
	return entries, sources, city, strings.Join(summaries, "\n\n"), reasons
}

func projectionHash(plan Plan) (string, error) {
	// Marshal the private projection explicitly, then use the preservation
	// canonicalizer with UseNumber. Plan.MarshalJSON intentionally hides arrays.
	value := struct {
		Protocol, Snapshot string
		Notes              []Note
		Collections        []Collection
		SavedItems         []SavedItem
		Residuals          []Residual
		Counts             Counts
	}{Protocol, plan.SnapshotSHA256, plan.Notes, plan.Collections, plan.SavedItems, plan.Residuals, plan.Counts}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var decoded any
	if err := d.Decode(&decoded); err != nil {
		return "", err
	}
	return legacymigration.Digest(decoded)
}
