// Package legacycollections projects verified Civu preservation material into
// private historical records. A record grants no current Place, membership,
// media, publication, login, or Agent authority.
package legacycollections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

const (
	Protocol        = "birdtie-civu-personal-archive-v1"
	SourceSystem    = legacymigration.SourceSystem
	PageSize        = 20
	Journey         = "journey"
	ThemeMap        = "theme_map"
	SavedPlace      = "saved_place"
	MaxPayloadBytes = 8 * 1024 * 1024
	MaxRecords      = 300000
)

var ErrInvalid = errors.New("legacy_personal_archive_invalid")
var ErrNotFound = errors.New("legacy_personal_archive_unavailable")
var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var numberRE = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type Counts struct {
	Journeys           int `json:"journeys"`
	JourneyTombstones  int `json:"journeyTombstones"`
	ThemeMaps          int `json:"themeMaps"`
	ThemeMapTombstones int `json:"themeMapTombstones"`
	SavedPlaces        int `json:"savedPlaces"`
}

func (v Counts) Total() int { return v.Journeys + v.ThemeMaps + v.SavedPlaces }

type Archive struct {
	Kind            string          `json:"kind"`
	SourceKey       string          `json:"sourceKey"`
	SourceOwnerID   string          `json:"sourceOwnerId"`
	SourceRevision  *int64          `json:"sourceRevision"`
	SourceSHA256    string          `json:"sourceSha256"`
	Deleted         bool            `json:"deleted"`
	SourceDeletedAt *time.Time      `json:"sourceDeletedAt"`
	Title           string          `json:"title"`
	SourceCreatedAt *time.Time      `json:"sourceCreatedAt"`
	SourceUpdatedAt *time.Time      `json:"sourceUpdatedAt"`
	Payload         json.RawMessage `json:"payload"`
}
type Plan struct {
	RunID              string    `json:"runId"`
	SourceFileSHA256   string    `json:"-"`
	ManifestFileSHA256 string    `json:"-"`
	SnapshotSHA256     string    `json:"-"`
	ProjectionSHA256   string    `json:"-"`
	Counts             Counts    `json:"counts"`
	Records            []Archive `json:"-"`
}
type Summary struct {
	ID              string                     `json:"id"`
	Kind            string                     `json:"kind"`
	SourceKey       string                     `json:"sourceKey"`
	SourceRevision  *int64                     `json:"sourceRevision"`
	SourceSHA256    string                     `json:"sourceSha256"`
	Deleted         bool                       `json:"deleted"`
	SourceDeletedAt *time.Time                 `json:"sourceDeletedAt"`
	Title           string                     `json:"title"`
	ImportedAt      time.Time                  `json:"importedAt"`
	ResponseProof   identity.HumanControlProof `json:"-"`
}
type Detail struct {
	Summary
	Payload json.RawMessage `json:"payload"`
}
type Page struct {
	Items         []Summary                  `json:"items"`
	NextCursor    string                     `json:"nextCursor,omitempty"`
	ResponseProof identity.HumanControlProof `json:"-"`
}
type HumanStore interface {
	ListHumanLegacyPersonalArchives(context.Context, [32]byte, identity.Actor, string, string) (Page, error)
	ReadHumanLegacyPersonalArchive(context.Context, [32]byte, identity.Actor, string) (Detail, error)
	FinalizeHumanLegacyPersonalArchivePage(context.Context, [32]byte, identity.Actor, Page) error
	FinalizeHumanLegacyPersonalArchiveDetail(context.Context, [32]byte, identity.Actor, Detail) error
}

func (p Plan) String() string {
	return fmt.Sprintf("private legacy personal archive: records=%d; NOT_IMPORTED", len(p.Records))
}
func (a Archive) String() string { return "private legacy personal archive record" }

// Each field is an explicit scalar projection: no opaque source payload, media
// URL/key, formal target ID or implicitly enabled current permission is kept.
type field struct {
	source, target, typ string
	nullable            bool
}

func f(source, target, typ string, nullable ...bool) field {
	return field{source, target, typ, len(nullable) > 0 && nullable[0]}
}

var journeyMeta = []field{
	f("city", "sourceCityLabel", "s"), f("date_label", "sourceDateLabel", "s"), f("start_date", "sourceStartDate", "date", true),
	f("days", "sourceDays", "i"), f("city_id", "sourceCityId", "id", true), f("visibility", "sourceVisibility", "s"),
	f("published_revision_id", "sourcePublishedRevisionId", "id", true), f("allow_copy", "sourceAllowCopy", "b"),
	f("source_public_journey_id", "sourcePublicJourneyId", "id", true), f("archived_at", "sourceArchivedAt", "t", true),
	f("team_revision", "sourceTeamRevision", "i"), f("scope_label", "sourceScopeLabel", "s"),
	f("deleted_by", "sourceDeletedBy", "id", true), f("purge_after", "sourcePurgeAfter", "t", true),
	f("operator_created", "sourceOperatorCreated", "b"), f("operator_created_by", "sourceOperatorCreatedBy", "id", true),
}
var themeMeta = []field{
	f("summary", "sourceSummary", "s"), f("visibility", "sourceVisibility", "s"), f("status", "sourceStatus", "s"),
	f("audit_status", "sourceAuditStatus", "s"), f("published_at", "sourcePublishedAt", "t", true),
	f("purge_after", "sourcePurgeAfter", "t", true), f("moderation_status", "sourceModerationStatus", "s"),
	f("commercial_disclosure", "sourceCommercialDisclosure", "s"), f("topic", "sourceTopic", "s"),
	f("review_reason", "sourceReviewReason", "s"), f("decision_reason", "sourceDecisionReason", "s"),
	f("moderated_by", "sourceModeratedBy", "id", true), f("moderated_at", "sourceModeratedAt", "t", true),
	f("operator_created", "sourceOperatorCreated", "b"), f("operator_created_by", "sourceOperatorCreatedBy", "id", true),
}

type relation struct {
	table, key, payload string
	fields              []field
	primary             []string
}

var journeyRelations = []relation{
	{"journey_stops", "journey_id", "stops", []field{
		f("id", "sourceId", "id"), f("journey_id", "sourceJourneyId", "id"), f("position", "position", "i"), f("name", "sourceName", "s"),
		f("kind", "sourceKind", "s"), f("city_id", "sourceCityId", "id", true), f("start_day", "sourceStartDay", "i", true), f("end_day", "sourceEndDay", "i", true),
		f("source", "sourceLabel", "s"), f("confidence", "sourceConfidence", "n"), f("confirmed", "sourceConfirmed", "b"),
		f("created_at", "sourceCreatedAt", "t"), f("destination_id", "sourceDestinationId", "id", true)}, []string{"id"}},
	{"journey_arrangements", "journey_id", "arrangements", []field{
		f("id", "sourceId", "id"), f("journey_id", "sourceJourneyId", "id"), f("type", "sourceType", "s"), f("place_id", "sourcePlaceId", "id", true),
		f("name", "sourceName", "s"), f("day_index", "sourceDayIndex", "i", true), f("position", "position", "i"), f("time_label", "sourceTimeLabel", "s"),
		f("private_note", "sourcePrivateNote", "s"), f("created_at", "sourceCreatedAt", "t"), f("updated_at", "sourceUpdatedAt", "t"),
		f("start_date", "sourceStartDate", "date", true), f("start_time", "sourceStartTime", "s"), f("end_date", "sourceEndDate", "date", true),
		f("end_time", "sourceEndTime", "s"), f("transport_mode", "sourceTransportMode", "s"), f("service_number", "sourceServiceNumber", "s"),
		f("departure_name", "sourceDepartureName", "s"), f("arrival_name", "sourceArrivalName", "s"), f("stop_id", "sourceStopId", "id", true),
		f("from_stop_id", "sourceFromStopId", "id", true), f("to_stop_id", "sourceToStopId", "id", true), f("custom_address", "sourceCustomAddress", "s"),
		f("custom_lat", "sourceCustomLatitude", "n", true), f("custom_lng", "sourceCustomLongitude", "n", true), f("source_text", "sourceText", "s")}, []string{"id"}},
	{"journey_notes", "journey_id", "notes", []field{f("id", "sourceId", "id"), f("journey_id", "sourceJourneyId", "id"), f("body", "sourceBody", "s"), f("position", "position", "i"), f("updated_at", "sourceUpdatedAt", "t")}, []string{"id"}},
	{"journey_members", "journey_id", "members", []field{f("journey_id", "sourceJourneyId", "id"), f("user_id", "sourceUserId", "id"), f("role", "sourceRole", "s"), f("status", "sourceStatus", "s"), f("attribution_consent", "sourceAttributionConsent", "b"), f("joined_at", "sourceJoinedAt", "t"), f("ended_at", "sourceEndedAt", "t", true)}, []string{"journey_id", "user_id"}},
	{"journey_media", "journey_id", "media", []field{f("id", "sourceId", "id"), f("journey_id", "sourceJourneyId", "id"), f("media_id", "sourceMediaId", "id"), f("source", "sourceLabel", "s"), f("source_note_id", "sourceNoteId", "id", true), f("hidden", "sourceHidden", "b"), f("publishable", "sourcePublishable", "b"), f("position", "position", "i"), f("", "state", "unavailable")}, []string{"id"}},
	{"journey_publications", "journey_id", "publications", []field{
		f("id", "sourceId", "id"), f("journey_id", "sourceJourneyId", "id"), f("working_revision", "sourceWorkingRevision", "i"), f("published_by", "sourcePublishedBy", "id"),
		f("published_at", "sourcePublishedAt", "t"), f("snapshot", "sourceSnapshotSha256", "snapshot"), f("withdrawn_at", "sourceWithdrawnAt", "t", true),
		f("moderation_status", "sourceModerationStatus", "s"), f("review_reason", "sourceReviewReason", "s"), f("decision_reason", "sourceDecisionReason", "s", true),
		f("moderated_by", "sourceModeratedBy", "id", true), f("moderated_at", "sourceModeratedAt", "t", true),
		f("moderation_updated_at", "sourceModerationUpdatedAt", "t"), f("moderation_version", "sourceModerationVersion", "i")}, []string{"id"}},
	{"journey_publication_places", "journey_id", "publicationPlaces", []field{f("publication_id", "sourcePublicationId", "id"), f("journey_id", "sourceJourneyId", "id"), f("place_id", "sourcePlaceId", "id"), f("position", "position", "i")}, []string{"publication_id", "place_id"}},
}
var themeRelations = []relation{
	{"theme_map_entries", "map_id", "entries", []field{f("map_id", "sourceMapId", "id"), f("place_id", "sourcePlaceId", "id"), f("position", "position", "i"), f("section", "sourceSection", "s"), f("creator_note", "sourceCreatorNote", "s"), f("collaboration_disclosure", "sourceCollaborationDisclosure", "b"), f("created_at", "sourceCreatedAt", "t"), f("updated_at", "sourceUpdatedAt", "t"), f("primary_moment_id", "sourcePrimaryMomentId", "id", true)}, []string{"map_id", "place_id"}},
	{"theme_map_entry_sources", "target_map_id", "entrySources", []field{f("target_map_id", "sourceTargetMapId", "id"), f("place_id", "sourcePlaceId", "id"), f("source_map_key", "sourceMapKey", "s"), f("source_revision", "sourceRevision", "i"), f("source_title_snapshot", "sourceTitleSnapshot", "s"), f("source_owner_id", "sourceOwnerId", "id", true), f("source_owner_name_snapshot", "sourceOwnerNameSnapshot", "s"), f("created_at", "sourceCreatedAt", "t")}, []string{"target_map_id", "place_id", "source_map_key"}},
}
var savedSources = relation{"place_save_sources", "", "sources", []field{f("user_id", "sourceUserId", "id"), f("place_id", "sourcePlaceId", "id"), f("source_type", "sourceType", "s"), f("source_key", "sourceKey", "s"), f("source_title_snapshot", "sourceTitleSnapshot", "s"), f("source_owner_id", "sourceOwnerId", "id", true), f("source_owner_name_snapshot", "sourceOwnerNameSnapshot", "s"), f("created_at", "sourceCreatedAt", "t")}, []string{"user_id", "place_id", "source_type", "source_key"}}

func validText(v string) bool { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) }
func scalar(v any, typ string, nullable bool) bool {
	if v == nil {
		return nullable
	}
	switch typ {
	case "s":
		s, ok := v.(string)
		return ok && validText(s)
	case "id":
		s, ok := v.(string)
		return ok && legacyarchive.UUID(s)
	case "b":
		_, ok := v.(bool)
		return ok
	case "i":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, e := strconv.ParseInt(n.String(), 10, 64)
		return e == nil
	case "n":
		n, ok := v.(json.Number)
		return ok && numberRE.MatchString(n.String())
	case "t":
		s, ok := v.(string)
		if !ok {
			return false
		}
		t, e := time.Parse(time.RFC3339Nano, s)
		return e == nil && !t.IsZero()
	case "date":
		s, ok := v.(string)
		if !ok {
			return false
		}
		_, e := time.Parse("2006-01-02", s)
		return e == nil
	case "snapshot":
		s, ok := v.(string)
		return ok && shaRE.MatchString(s)
	case "unavailable":
		return v == "UNAVAILABLE"
	}
	return false
}
func project(row map[string]any, fields []field) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range fields {
		v, ok := row[f.source]
		if f.typ == "unavailable" {
			v, ok = "UNAVAILABLE", true
		}
		if !ok {
			return nil, ErrInvalid
		}
		if f.typ == "snapshot" {
			var e error
			v, e = legacymigration.Digest(v)
			if e != nil {
				return nil, ErrInvalid
			}
		}
		if !scalar(v, f.typ, f.nullable) {
			return nil, ErrInvalid
		}
		out[f.target] = v
	}
	return out, nil
}
func closedProjection(value any, fields []field) (map[string]any, error) {
	m, ok := value.(map[string]any)
	if !ok || len(m) != len(fields) {
		return nil, ErrInvalid
	}
	for _, f := range fields {
		v, ok := m[f.target]
		if !ok || !scalar(v, f.typ, f.nullable) {
			return nil, ErrInvalid
		}
	}
	return m, nil
}
func text(row map[string]any, key string) (string, error) {
	s, ok := row[key].(string)
	if !ok || !validText(s) {
		return "", ErrInvalid
	}
	return s, nil
}
func id(row map[string]any, key string) (string, error) {
	s, e := text(row, key)
	if e != nil || !legacyarchive.UUID(s) {
		return "", ErrInvalid
	}
	return s, nil
}
func timestamp(row map[string]any, key string, nullable bool) (*time.Time, error) {
	v, ok := row[key]
	if !ok {
		return nil, ErrInvalid
	}
	if v == nil && nullable {
		return nil, nil
	}
	if !scalar(v, "t", false) {
		return nil, ErrInvalid
	}
	t, _ := time.Parse(time.RFC3339Nano, v.(string))
	t = t.UTC()
	return &t, nil
}
func revision(row map[string]any, key string) (*int64, error) {
	v, ok := row[key]
	if !ok || !scalar(v, "i", false) {
		return nil, ErrInvalid
	}
	n, _ := strconv.ParseInt(v.(json.Number).String(), 10, 64)
	if n < 1 || n > legacyarchive.MaxVersion {
		return nil, ErrInvalid
	}
	return &n, nil
}

// decode is also used on writer-provided projections. Require unique keys,
// valid UTF-8 and complete scalar JSON; encoding/json alone accepts duplicates.
func decode(raw []byte, max int) (any, error) {
	if len(raw) == 0 || len(raw) > max || !utf8.Valid(raw) || !validEscapes(raw) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, e := readJSON(d, 0)
	if e != nil {
		return nil, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return v, nil
}
func validEscapes(raw []byte) bool {
	inside := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inside = !inside
			continue
		}
		if !inside || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			lo, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || lo < 0xdc00 || lo > 0xdfff {
				return false
			}
			i += 6
		} else if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
	}
	return true
}
func readJSON(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, ErrInvalid
	}
	t, e := d.Token()
	if e != nil {
		return nil, ErrInvalid
	}
	if sep, ok := t.(json.Delim); ok {
		switch sep {
		case '{':
			m := map[string]any{}
			for d.More() {
				k, e := d.Token()
				s, ok := k.(string)
				if e != nil || !ok {
					return nil, ErrInvalid
				}
				if _, seen := m[s]; seen {
					return nil, ErrInvalid
				}
				v, e := readJSON(d, depth+1)
				if e != nil {
					return nil, e
				}
				m[s] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, ErrInvalid
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, e := readJSON(d, depth+1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, ErrInvalid
			}
			return a, nil
		}
		return nil, ErrInvalid
	}
	return t, nil
}

type rows = []map[string]any
type groups = map[string]map[string]rows

func rowKey(row map[string]any, keys []string) (string, error) {
	a := []any{}
	for _, k := range keys {
		v, ok := row[k]
		if !ok {
			return "", ErrInvalid
		}
		s, ok := v.(string)
		if !ok || !validText(s) {
			return "", ErrInvalid
		}
		a = append(a, s)
	}
	b, e := legacymigration.Canonical(a)
	if e != nil {
		return "", ErrInvalid
	}
	return string(b), nil
}
func groupRelations(tables map[string]rows, parents map[string]bool, specs []relation) (groups, error) {
	out := groups{}
	for _, r := range specs {
		out[r.table] = map[string]rows{}
		seen := map[string]bool{}
		for _, row := range tables[r.table] {
			parent, e := id(row, r.key)
			if e != nil || !parents[parent] {
				return nil, ErrInvalid
			}
			key, e := rowKey(row, r.primary)
			if e != nil || seen[key] {
				return nil, ErrInvalid
			}
			seen[key] = true
			out[r.table][parent] = append(out[r.table][parent], row)
		}
	}
	return out, nil
}
func sortedRows(rs rows) ([]any, error) {
	type entry struct {
		key string
		v   any
	}
	es := []entry{}
	for _, r := range rs {
		b, e := legacymigration.Canonical(r)
		if e != nil {
			return nil, ErrInvalid
		}
		es = append(es, entry{string(b), r})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].key < es[j].key })
	out := []any{}
	for _, e := range es {
		out = append(out, e.v)
	}
	return out, nil
}
func projectRelations(parent string, group groups, specs []relation) (map[string]any, map[string]any, error) {
	payload, original := map[string]any{}, map[string]any{}
	for _, r := range specs {
		raw, e := sortedRows(group[r.table][parent])
		if e != nil {
			return nil, nil, e
		}
		original[r.table] = raw
		list := []any{}
		for _, x := range raw {
			v, e := project(x.(map[string]any), r.fields)
			if e != nil {
				return nil, nil, e
			}
			list = append(list, v)
		}
		payload[r.payload] = list
	}
	return payload, original, nil
}
func parents(tables map[string]rows, name string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, r := range tables[name] {
		key, e := id(r, "id")
		if e != nil || out[key] {
			return nil, ErrInvalid
		}
		out[key] = true
	}
	return out, nil
}

func buildCollection(row map[string]any, kind string, owners map[string]bool, group groups, specs []relation, meta []field) (Archive, error) {
	a := Archive{Kind: kind}
	var e error
	if a.SourceKey, e = id(row, "id"); e != nil {
		return a, e
	}
	if a.SourceOwnerID, e = id(row, "owner_id"); e != nil || !owners[a.SourceOwnerID] {
		return a, ErrInvalid
	}
	rev := "working_revision"
	if kind == ThemeMap {
		rev = "revision"
	}
	if a.SourceRevision, e = revision(row, rev); e != nil {
		return a, e
	}
	if a.SourceCreatedAt, e = timestamp(row, "created_at", false); e != nil {
		return a, e
	}
	if a.SourceUpdatedAt, e = timestamp(row, "updated_at", false); e != nil {
		return a, e
	}
	if a.SourceDeletedAt, e = timestamp(row, "deleted_at", true); e != nil {
		return a, e
	}
	a.Deleted = a.SourceDeletedAt != nil
	// Full family hash includes discarded covers, snapshots and tombstone child
	// rows, but none of those bytes become readable deleted content.
	original := map[string]any{}
	for _, r := range specs {
		raw, e := sortedRows(group[r.table][a.SourceKey])
		if e != nil {
			return a, e
		}
		original[r.table] = raw
	}
	a.SourceSHA256, e = legacymigration.Digest(map[string]any{"kind": kind, "sourceKey": a.SourceKey, "root": row, "relations": original})
	if e != nil {
		return a, ErrInvalid
	}
	if a.Deleted {
		return a, nil
	}
	if a.Title, e = text(row, "title"); e != nil {
		return a, e
	}
	payload, _, e := projectRelations(a.SourceKey, group, specs)
	if e != nil {
		return a, e
	}
	metadata, e := project(row, meta)
	if e != nil {
		return a, e
	}
	payload["metadata"] = metadata
	a.Payload, e = legacymigration.Canonical(payload)
	if e != nil {
		return a, ErrInvalid
	}
	return a, nil
}

// BuildPlan first verifies the complete original 29-table source/manifest.
// There is no network, target ID generation, source mutation or authorization.
func BuildPlan(runID string, sourceRaw, manifestRaw []byte) (Plan, error) {
	base, e := legacymigration.BuildPlan(runID, sourceRaw, manifestRaw)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	decoded, e := decode(sourceRaw, legacymigration.MaxInputBytes)
	if e != nil {
		return Plan{}, e
	}
	rawTables := decoded.(map[string]any)["tables"].(map[string]any)
	tables := map[string]rows{}
	for table, raw := range rawTables {
		for _, x := range raw.([]any) {
			tables[table] = append(tables[table], x.(map[string]any))
		}
	}
	owners := map[string]bool{}
	for _, o := range base.Owners {
		owners[o.SourceID] = true
	}
	jp, e := parents(tables, "journeys")
	if e != nil {
		return Plan{}, e
	}
	tp, e := parents(tables, "theme_maps")
	if e != nil {
		return Plan{}, e
	}
	jg, e := groupRelations(tables, jp, journeyRelations)
	if e != nil {
		return Plan{}, e
	}
	tg, e := groupRelations(tables, tp, themeRelations)
	if e != nil {
		return Plan{}, e
	}
	p := Plan{RunID: runID, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, Records: []Archive{}}
	for _, r := range tables["journeys"] {
		a, e := buildCollection(r, Journey, owners, jg, journeyRelations, journeyMeta)
		if e != nil {
			return Plan{}, e
		}
		p.Records = append(p.Records, a)
	}
	for _, r := range tables["theme_maps"] {
		a, e := buildCollection(r, ThemeMap, owners, tg, themeRelations, themeMeta)
		if e != nil {
			return Plan{}, e
		}
		p.Records = append(p.Records, a)
	}
	savedParents := map[string]map[string]any{}
	for _, r := range tables["user_saved_places"] {
		owner, e := id(r, "user_id")
		if e != nil || !owners[owner] {
			return Plan{}, ErrInvalid
		}
		place, e := id(r, "place_id")
		if e != nil {
			return Plan{}, e
		}
		key := owner + ":" + place
		if savedParents[key] != nil {
			return Plan{}, ErrInvalid
		}
		savedParents[key] = r
	}
	sg := map[string]rows{}
	seen := map[string]bool{}
	for _, r := range tables["place_save_sources"] {
		owner, e := id(r, "user_id")
		if e != nil {
			return Plan{}, e
		}
		place, e := id(r, "place_id")
		if e != nil {
			return Plan{}, e
		}
		parent := owner + ":" + place
		if savedParents[parent] == nil {
			return Plan{}, ErrInvalid
		}
		key, e := rowKey(r, savedSources.primary)
		if e != nil || seen[key] {
			return Plan{}, ErrInvalid
		}
		seen[key] = true
		sg[parent] = append(sg[parent], r)
	}
	for key, r := range savedParents {
		a := Archive{Kind: SavedPlace, SourceKey: key, SourceOwnerID: r["user_id"].(string)}
		if a.SourceCreatedAt, e = timestamp(r, "created_at", false); e != nil {
			return Plan{}, e
		}
		original, e := sortedRows(sg[key])
		if e != nil {
			return Plan{}, e
		}
		sources := []any{}
		for _, x := range original {
			v, e := project(x.(map[string]any), savedSources.fields)
			if e != nil {
				return Plan{}, e
			}
			sources = append(sources, v)
		}
		a.SourceSHA256, e = legacymigration.Digest(map[string]any{"kind": SavedPlace, "sourceKey": key, "root": r, "sources": original})
		if e != nil {
			return Plan{}, ErrInvalid
		}
		a.Payload, e = legacymigration.Canonical(map[string]any{"sourcePlaceId": r["place_id"], "sourceCreatedAt": r["created_at"], "sources": sources})
		if e != nil {
			return Plan{}, ErrInvalid
		}
		p.Records = append(p.Records, a)
	}
	sort.Slice(p.Records, func(i, j int) bool {
		a, b := p.Records[i], p.Records[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.SourceKey < b.SourceKey
	})
	p.Counts = countRecords(p.Records)
	p.ProjectionSHA256, e = projectionHash(p.Records)
	if e != nil || ValidatePlan(p) != nil {
		return Plan{}, ErrInvalid
	}
	return p, nil
}

func closedKeys(m map[string]any, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
func validatePayload(a Archive) error {
	v, e := decode(a.Payload, MaxPayloadBytes)
	if e != nil {
		return e
	}
	m, ok := v.(map[string]any)
	if !ok {
		return ErrInvalid
	}
	if a.Kind == SavedPlace {
		if !closedKeys(m, "sourcePlaceId", "sourceCreatedAt", "sources") || !scalar(m["sourcePlaceId"], "id", false) || !scalar(m["sourceCreatedAt"], "t", false) || a.SourceKey != a.SourceOwnerID+":"+m["sourcePlaceId"].(string) {
			return ErrInvalid
		}
		t, _ := time.Parse(time.RFC3339Nano, m["sourceCreatedAt"].(string))
		if !t.Equal(*a.SourceCreatedAt) {
			return ErrInvalid
		}
		list, ok := m["sources"].([]any)
		if !ok {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, x := range list {
			r, e := closedProjection(x, savedSources.fields)
			if e != nil || r["sourceUserId"] != a.SourceOwnerID || r["sourcePlaceId"] != m["sourcePlaceId"] {
				return ErrInvalid
			}
			key := fmt.Sprint(r["sourceType"], "\x00", r["sourceKey"])
			if seen[key] {
				return ErrInvalid
			}
			seen[key] = true
		}
		return nil
	}
	specs, meta := journeyRelations, journeyMeta
	if a.Kind == ThemeMap {
		specs, meta = themeRelations, themeMeta
	}
	keys := []string{"metadata"}
	for _, r := range specs {
		keys = append(keys, r.payload)
	}
	if !closedKeys(m, keys...) {
		return ErrInvalid
	}
	if _, e = closedProjection(m["metadata"], meta); e != nil {
		return e
	}
	pubs, entries := map[string]bool{}, map[string]bool{}
	for _, spec := range specs {
		list, ok := m[spec.payload].([]any)
		if !ok || len(list) > 100000 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, x := range list {
			r, e := closedProjection(x, spec.fields)
			if e != nil {
				return e
			}
			parent := "sourceJourneyId"
			if a.Kind == ThemeMap {
				parent = "sourceMapId"
				if spec.payload == "entrySources" {
					parent = "sourceTargetMapId"
				}
			}
			if r[parent] != a.SourceKey {
				return ErrInvalid
			}
			keyParts := []any{}
			for _, k := range spec.primary {
				for _, f := range spec.fields {
					if f.source == k {
						keyParts = append(keyParts, r[f.target])
						break
					}
				}
			}
			key, _ := legacymigration.Canonical(keyParts)
			if seen[string(key)] {
				return ErrInvalid
			}
			seen[string(key)] = true
			if spec.payload == "publications" {
				pubs[r["sourceId"].(string)] = true
			}
			if spec.payload == "entries" {
				entries[r["sourcePlaceId"].(string)] = true
			}
		}
	}
	if a.Kind == Journey {
		for _, x := range m["publicationPlaces"].([]any) {
			if !pubs[x.(map[string]any)["sourcePublicationId"].(string)] {
				return ErrInvalid
			}
		}
	} else {
		for _, x := range m["entrySources"].([]any) {
			if !entries[x.(map[string]any)["sourcePlaceId"].(string)] {
				return ErrInvalid
			}
		}
	}
	return nil
}

// Validate verifies an independently supplied record's closed read-only shape.
// It cannot authenticate its owner or prove correspondence with original bytes;
// a writer must additionally reparse the original source and verify its run.
func Validate(a Archive) error {
	if !legacyarchive.UUID(a.SourceOwnerID) || !shaRE.MatchString(a.SourceSHA256) || !validText(a.Title) || a.SourceCreatedAt == nil || a.SourceCreatedAt.IsZero() {
		return ErrInvalid
	}
	switch a.Kind {
	case Journey, ThemeMap:
		if !legacyarchive.UUID(a.SourceKey) || a.SourceRevision == nil || *a.SourceRevision < 1 || *a.SourceRevision > legacyarchive.MaxVersion || a.SourceUpdatedAt == nil || a.SourceUpdatedAt.IsZero() {
			return ErrInvalid
		}
	case SavedPlace:
		if a.SourceRevision != nil || a.SourceUpdatedAt != nil || a.Title != "" || a.Deleted {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if a.Deleted {
		if a.SourceDeletedAt == nil || a.SourceDeletedAt.IsZero() || a.Title != "" || len(a.Payload) != 0 {
			return ErrInvalid
		}
		return nil
	}
	if a.SourceDeletedAt != nil {
		return ErrInvalid
	}
	return validatePayload(a)
}
func countRecords(records []Archive) Counts {
	v := Counts{}
	for _, a := range records {
		switch a.Kind {
		case Journey:
			v.Journeys++
			if a.Deleted {
				v.JourneyTombstones++
			}
		case ThemeMap:
			v.ThemeMaps++
			if a.Deleted {
				v.ThemeMapTombstones++
			}
		case SavedPlace:
			v.SavedPlaces++
		}
	}
	return v
}
func projectionHash(records []Archive) (string, error) {
	// Convert through UseNumber so time.Time/RawMessage and int64 revisions enter
	// the same canonical representation without binary floating point.
	raw, e := json.Marshal(records)
	if e != nil {
		return "", ErrInvalid
	}
	v, e := decode(raw, legacymigration.MaxInputBytes)
	if e != nil {
		return "", e
	}
	return legacymigration.Digest(map[string]any{"protocol": Protocol, "records": v})
}
func ValidatePlan(p Plan) error {
	if !legacymigration.ValidRunID(p.RunID) || !shaRE.MatchString(p.SourceFileSHA256) || !shaRE.MatchString(p.ManifestFileSHA256) || !shaRE.MatchString(p.SnapshotSHA256) || !shaRE.MatchString(p.ProjectionSHA256) || p.Records == nil || len(p.Records) > MaxRecords {
		return ErrInvalid
	}
	for i, a := range p.Records {
		if Validate(a) != nil {
			return ErrInvalid
		}
		if i > 0 {
			last := p.Records[i-1]
			if last.Kind > a.Kind || last.Kind == a.Kind && last.SourceKey >= a.SourceKey {
				return ErrInvalid
			}
		}
	}
	if p.Counts != countRecords(p.Records) {
		return ErrInvalid
	}
	h, e := projectionHash(p.Records)
	if e != nil || h != p.ProjectionSHA256 {
		return ErrInvalid
	}
	return nil
}
func ValidateSummary(v Summary) error {
	if !legacyarchive.UUID(v.ID) || !shaRE.MatchString(v.SourceSHA256) || !validText(v.Title) || v.ImportedAt.IsZero() {
		return ErrInvalid
	}
	switch v.Kind {
	case Journey, ThemeMap:
		if !legacyarchive.UUID(v.SourceKey) || v.SourceRevision == nil || *v.SourceRevision < 1 || *v.SourceRevision > legacyarchive.MaxVersion {
			return ErrInvalid
		}
	case SavedPlace:
		parts := strings.Split(v.SourceKey, ":")
		if len(parts) != 2 || !legacyarchive.UUID(parts[0]) || !legacyarchive.UUID(parts[1]) || v.SourceRevision != nil || v.Title != "" || v.Deleted {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if v.Deleted {
		if v.SourceDeletedAt == nil || v.SourceDeletedAt.IsZero() || v.Title != "" {
			return ErrInvalid
		}
	} else if v.SourceDeletedAt != nil {
		return ErrInvalid
	}
	return nil
}
func ValidateDetail(v Detail) error {
	if ValidateSummary(v.Summary) != nil {
		return ErrInvalid
	}
	if v.Deleted {
		if len(v.Payload) != 0 {
			return ErrInvalid
		}
		return nil
	}
	a := Archive{Kind: v.Kind, SourceKey: v.SourceKey, Payload: v.Payload}
	if v.Kind == SavedPlace {
		x, e := decode(v.Payload, MaxPayloadBytes)
		if e != nil {
			return e
		}
		m, ok := x.(map[string]any)
		if !ok || !scalar(m["sourceCreatedAt"], "t", false) {
			return ErrInvalid
		}
		t, _ := time.Parse(time.RFC3339Nano, m["sourceCreatedAt"].(string))
		a.SourceCreatedAt = &t
		a.SourceOwnerID = strings.Split(v.SourceKey, ":")[0]
	}
	return validatePayload(a)
}
