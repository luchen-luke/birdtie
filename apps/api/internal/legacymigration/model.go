// Package legacymigration validates an untrusted protected preservation input.
// A valid plan is neither authentication, source permission, nor import proof.
package legacymigration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
)

const SourceSystem = "hangzhou-civu-20261014"
const SourceProtocol = "birdtie-civu-hangzhou-source-v1"
const ManifestProtocol = "birdtie-civu-hangzhou-preservation-manifest-v1"
const BackupCipherSHA256 = "5dbc323ae7f16df233dea5a412d30c6f563632150e1c639443c4a3997ec5838f"
const SourceSchemaSHA256 = "b4989ea80d771d454e61b93fb6b65b8692bde9e9a68c4a686cad57068801cd9c"
const AuditedOutputSHA256 = "77c8c987b64b1656c3d402f073861c015cfc2a669ab07b49b0e43519f92a9ec3"
const NoteDeletedStatus = "DELETED"
const NoteLiveStatus = "ARCHIVED"
const NoteDataOrigin = "legacy_note_owner_archive"
const MaxInputBytes = 128 * 1024 * 1024

var ErrInvalid = errors.New("invalid legacy preservation input")
var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var numberRE = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
var snapshotRE = regexp.MustCompile(`^[0-9]+:[0-9]+:(?:[0-9]+(?:,[0-9]+)*)?$`)

// Fixed audited keys; caller input can never widen the table/column allowlist.
var sourceColumns = map[string][]string{
	"auth_identities":            {"id", "user_id", "provider", "subject", "verified_at", "created_at"},
	"cities":                     {"id", "name", "name_norm", "province", "level", "adcode", "sort_weight", "created_at"},
	"geo_areas":                  {"id", "parent_id", "kind", "stable_key", "name_zh", "name_en", "name_local", "aliases", "country_code", "admin_code", "center_lat", "center_lng", "bbox", "status", "legacy_region_id", "created_at", "updated_at", "canonical_id", "travel_profile", "artwork_media_id", "artwork_url", "artwork_version", "artwork_updated_at"},
	"journey_arrangements":       {"id", "journey_id", "type", "place_id", "name", "day_index", "position", "time_label", "private_note", "created_at", "updated_at", "start_date", "start_time", "end_date", "end_time", "transport_mode", "service_number", "departure_name", "arrival_name", "stop_id", "from_stop_id", "to_stop_id", "custom_address", "custom_lat", "custom_lng", "source_text"},
	"journey_media":              {"id", "journey_id", "media_id", "source", "source_note_id", "hidden", "publishable", "position"},
	"journey_members":            {"journey_id", "user_id", "role", "status", "attribution_consent", "joined_at", "ended_at"},
	"journey_notes":              {"id", "journey_id", "body", "position", "updated_at"},
	"journey_publication_places": {"publication_id", "journey_id", "place_id", "position"},
	"journey_publications":       {"id", "journey_id", "working_revision", "published_by", "published_at", "snapshot", "withdrawn_at", "moderation_status", "review_reason", "decision_reason", "moderated_by", "moderated_at", "moderation_updated_at", "moderation_version"},
	"journey_stops":              {"id", "journey_id", "position", "name", "kind", "city_id", "start_day", "end_day", "source", "confidence", "confirmed", "created_at", "destination_id"},
	"journeys":                   {"id", "owner_id", "title", "city", "date_label", "start_date", "days", "cover_url", "visibility", "working_revision", "published_revision_id", "allow_copy", "source_public_journey_id", "archived_at", "deleted_at", "created_at", "updated_at", "city_id", "team_revision", "scope_label", "deleted_by", "purge_after", "operator_created", "operator_created_by"},
	"media_assets":               {"id", "owner_id", "kind", "storage_key", "public_url", "width", "height", "status", "created_at", "content_type", "size_bytes", "duration_ms", "motion_storage_key", "motion_url", "motion_content_type", "motion_size_bytes", "updated_at"},
	"moment_thread_legs":         {"thread_id", "from_note_id", "to_note_id", "from_position", "to_position", "mode", "from_anchor_place_id", "to_anchor_place_id", "created_at", "updated_at"},
	"moment_threads":             {"id", "author_id", "entry_count", "created_at", "route_revision"},
	"note_media":                 {"note_id", "media_id", "position"},
	"note_media_locations":       {"note_id", "media_id", "latitude", "longitude", "map_enabled", "place_ids", "captured_date"},
	"note_places":                {"note_id", "place_id", "raw_name", "position"},
	"notes":                      {"id", "author_id", "type", "title", "caption", "tag", "location_label", "cover_url", "language", "visibility", "like_count", "save_count", "created_at", "updated_at", "deleted_at", "moderation_status", "journey_id", "journey_day_index", "version", "suspended_journey_id", "suspended_journey_day_index", "suspended_journey_visibility", "suspended_journey_version", "suspended_journey_at", "review_reason", "decision_reason", "moderated_by", "moderated_at", "operator_created", "operator_created_by", "destination_id", "destination_source", "recorded_start_date", "recorded_end_date", "comments_enabled", "like_count_hidden", "share_count_hidden", "share_count", "map_anchor_lat", "map_anchor_lng", "map_anchor_precision", "map_anchor_source", "media_locations_set", "thread_id", "thread_position", "inbound_origin", "inbound_mode", "inbound_duration_minutes", "inbound_estimated_duration_minutes"},
	"place_area_relations":       {"place_id", "area_id", "relation_type", "source", "confidence", "review_status", "created_at", "updated_at"},
	"place_images":               {"id", "place_id", "media_id", "contributor_id", "image_url", "source_type", "rights_basis", "attribution", "status", "is_cover", "sort_order", "reviewed_by", "reviewed_at", "created_at", "updated_at", "source_note_id", "suggest_cover", "cover_eligible", "review_reason", "preview_media_id"},
	"place_provider_refs":        {"provider", "external_id", "place_id", "created_at", "raw_category", "category_code", "mapping_status", "source_payload", "domain_code", "subcategory_code", "type_detail_codes"},
	"place_save_sources":         {"user_id", "place_id", "source_type", "source_key", "source_title_snapshot", "source_owner_id", "source_owner_name_snapshot", "created_at"},
	"places":                     {"id", "region_id", "name", "category", "city", "address", "description", "phone", "lat", "lng", "cover_url", "source", "provider_links", "status", "created_by", "created_at", "updated_at", "category_code", "domain_code", "subcategory_code", "experience_codes", "access_features", "opening_hours", "source_url", "admin_area_id", "website_url", "website_verified_at", "website_verification_source", "lifecycle_status", "canonical_place_id", "lifecycle_reason", "admin_status", "governance_version", "decision_reason", "moderated_by", "moderated_at", "type_detail_codes", "name_zh", "name_en", "search_aliases", "details"},
	"regions":                    {"id", "name", "slug", "country_code", "center_lat", "center_lng", "bbox", "created_at"},
	"theme_map_entries":          {"map_id", "place_id", "position", "section", "creator_note", "collaboration_disclosure", "created_at", "updated_at", "primary_moment_id"},
	"theme_map_entry_sources":    {"target_map_id", "place_id", "source_map_key", "source_revision", "source_title_snapshot", "source_owner_id", "source_owner_name_snapshot", "created_at"},
	"theme_maps":                 {"id", "owner_id", "title", "summary", "cover_url", "visibility", "status", "audit_status", "revision", "published_at", "created_at", "updated_at", "deleted_at", "purge_after", "moderation_status", "commercial_disclosure", "topic", "review_reason", "decision_reason", "moderated_by", "moderated_at", "operator_created", "operator_created_by"},
	"user_saved_places":          {"user_id", "place_id", "created_at"},
	"users":                      {"id", "email", "display_name", "password_hash", "created_at", "handle", "avatar_url", "bio", "city", "profile_visibility", "show_location", "show_saved_count", "allow_journey_copy", "status", "version", "updated_at", "data_origin", "decision_reason", "moderated_by", "moderated_at", "last_sessions_revoked_at", "background_url", "background_preset", "background_history"},
}
var families = map[string][]string{
	"accounts":    {"users", "auth_identities"},
	"places":      {"cities", "regions", "geo_areas", "places", "place_area_relations", "place_provider_refs"},
	"saved":       {"user_saved_places", "place_save_sources"},
	"collections": {"journeys", "journey_stops", "journey_arrangements", "journey_notes", "journey_members", "journey_media", "journey_publications", "journey_publication_places", "theme_maps", "theme_map_entries", "theme_map_entry_sources"},
	"notes":       {"notes", "note_places", "note_media", "note_media_locations", "moment_threads", "moment_thread_legs"},
	"media":       {"media_assets", "place_images"},
}

type Run struct {
	RunID              string
	SourceSystem       string
	SourceProtocol     string
	BackupCipherSHA256 string
	SchemaSHA256       string
	SnapshotSHA256     string
	SourceFileSHA256   string
	ManifestFileSHA256 string
	Header             json.RawMessage
	Counts             map[string]int
}
type OwnerPlan struct {
	SourceID      string
	SourceVersion int64
	SourceSHA256  string
	SourceStatus  string
	DataOrigin    string
	DisplayName   string
	Bio           string
	Handle        *string
}
type NotePlan struct {
	SourceID               string
	OwnerSourceID          string
	SourceVersion          int64
	SourceSHA256           string
	Deleted                bool
	Title                  string
	Body                   string
	RecordedStartDate      *string
	RecordedEndDate        *string
	SourceVisibility       string
	SourceModerationStatus string
	Payload                json.RawMessage
}
type Plan struct {
	Run      Run
	Owners   []OwnerPlan `json:"-"`
	Notes    []NotePlan  `json:"-"`
	Deferred map[string]int
}

func (p Plan) String() string {
	return fmt.Sprintf("legacy plan: users=%d notes=%d; NOT_IMPORTED", len(p.Owners), len(p.Notes))
}
func (p OwnerPlan) String() string { return "private legacy owner proposal" }
func (p NotePlan) String() string  { return "private legacy note proposal" }
func ValidRunID(id string) bool    { return legacyarchive.UUID(id) }
func Tables() []string {
	out := make([]string, 0, len(sourceColumns))
	for k := range sourceColumns {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func ColumnNames(table string) []string { return append([]string(nil), sourceColumns[table]...) }

func closed(v any, keys ...string) (map[string]any, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) != len(keys) {
		return nil, ErrInvalid
	}
	for _, k := range keys {
		if _, ok = m[k]; !ok {
			return nil, ErrInvalid
		}
	}
	return m, nil
}
func originalText(v any) (string, error) {
	s, ok := v.(string)
	if !ok || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", ErrInvalid
	}
	return s, nil
}
func maybeText(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s, e := originalText(v)
	if e != nil {
		return nil, e
	}
	return &s, nil
}
func integer(v any) (int64, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, ErrInvalid
	}
	x, e := strconv.ParseInt(string(n), 10, 64)
	if e != nil {
		return 0, ErrInvalid
	}
	return x, nil
}
func optionalInteger(v any) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	n, e := integer(v)
	if e != nil {
		return nil, e
	}
	return &n, nil
}
func id(v any) (string, error) {
	s, e := originalText(v)
	if e != nil || !legacyarchive.UUID(s) {
		return "", ErrInvalid
	}
	return s, nil
}
func maybeID(v any) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s, e := id(v)
	if e != nil {
		return nil, e
	}
	return &s, nil
}
func originalBool(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, ErrInvalid
	}
	return b, nil
}
func number(v any) (*json.Number, error) {
	if v == nil {
		return nil, nil
	}
	n, ok := v.(json.Number)
	if !ok || !numberRE.MatchString(n.String()) {
		return nil, ErrInvalid
	}
	return &n, nil
}

// Decoder.Token with UseNumber rejects duplicate keys before maps overwrite them.
func readValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, ErrInvalid
	}
	t, e := d.Token()
	if e != nil {
		return nil, ErrInvalid
	}
	if delim, ok := t.(json.Delim); ok {
		if delim == '{' {
			m := map[string]any{}
			for d.More() {
				k, e := d.Token()
				s, ok := k.(string)
				if e != nil || !ok {
					return nil, ErrInvalid
				}
				if _, exists := m[s]; exists {
					return nil, ErrInvalid
				}
				v, e := readValue(d, depth+1)
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
		}
		if delim == '[' {
			a := []any{}
			for d.More() {
				v, e := readValue(d, depth+1)
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
func validateEscapes(raw []byte) bool {
	// encoding/json replaces unpaired surrogates; preservation must reject them.
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		i++
		for ; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] != '\\' {
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
	}
	return true
}
func decode(raw []byte) (any, error) {
	if len(raw) > MaxInputBytes || !utf8.Valid(raw) || !validateEscapes(raw) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, e := readValue(d, 0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	return v, nil
}
func quote(out *bytes.Buffer, s string) error {
	if !utf8.ValidString(s) {
		return ErrInvalid
	}
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 32 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return nil
}
func canonicalTo(out *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if x {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		return quote(out, x)
	case json.Number:
		if !numberRE.MatchString(x.String()) {
			return ErrInvalid
		}
		out.WriteString(x.String())
	case int:
		out.WriteString(strconv.Itoa(x))
	case int64:
		out.WriteString(strconv.FormatInt(x, 10))
	case []any:
		out.WriteByte('[')
		for i, v := range x {
			if i > 0 {
				out.WriteByte(',')
			}
			if e := canonicalTo(out, v); e != nil {
				return e
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			if e := quote(out, k); e != nil {
				return e
			}
			out.WriteByte(':')
			if e := canonicalTo(out, x[k]); e != nil {
				return e
			}
		}
		out.WriteByte('}')
	default:
		return ErrInvalid
	}
	return nil
}
func Canonical(v any) ([]byte, error) {
	var out bytes.Buffer
	if e := canonicalTo(&out, v); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
func Digest(v any) (string, error) {
	raw, e := Canonical(v)
	if e != nil {
		return "", e
	}
	return fileHash(raw), nil
}
func fileHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func BuildPlan(runID string, sourceRaw, manifestRaw []byte) (Plan, error) {
	var p Plan
	if !ValidRunID(runID) {
		return p, ErrInvalid
	}
	source, e := decode(sourceRaw)
	if e != nil {
		return p, e
	}
	manifest, e := decode(manifestRaw)
	if e != nil {
		return p, e
	}
	src, e := closed(source, "schemaVersion", "source", "capture", "tables")
	if e != nil || src["schemaVersion"] != SourceProtocol {
		return p, ErrInvalid
	}
	hdr, e := closed(src["source"], "instanceTag", "container", "database", "backupCipherSha256", "schemaSha256", "snapshotSha256")
	if e != nil {
		return p, e
	}
	if hdr["instanceTag"] != SourceSystem || hdr["container"] != "birdtie-civu-restore-20261014" || hdr["database"] != "civu_restore" || hdr["backupCipherSha256"] != BackupCipherSHA256 || hdr["schemaSha256"] != SourceSchemaSHA256 {
		return p, ErrInvalid
	}
	snapshot, ok := hdr["snapshotSha256"].(string)
	if !ok || !shaRE.MatchString(snapshot) {
		return p, ErrInvalid
	}
	capture, e := closed(src["capture"], "observedAt", "transactionSnapshot")
	if e != nil {
		return p, e
	}
	for _, v := range capture {
		if _, e = originalText(v); e != nil {
			return p, e
		}
	}
	if _, e = time.Parse(time.RFC3339Nano, capture["observedAt"].(string)); e != nil || !strings.HasSuffix(capture["observedAt"].(string), "Z") || !snapshotRE.MatchString(capture["transactionSnapshot"].(string)) {
		return p, ErrInvalid
	}
	tables, ok := src["tables"].(map[string]any)
	if !ok || len(tables) != len(sourceColumns) {
		return p, ErrInvalid
	}
	rows := map[string][]map[string]any{}
	counts := map[string]int{}
	for table, columns := range sourceColumns {
		list, ok := tables[table].([]any)
		if !ok || len(list) > 100000 {
			return p, ErrInvalid
		}
		counts[table] = len(list)
		rows[table] = []map[string]any{}
		for _, raw := range list {
			row, e := closed(raw, columns...)
			if e != nil {
				return p, e
			}
			rows[table] = append(rows[table], row)
		}
	}
	stripped := map[string]any{}
	for k, v := range hdr {
		if k != "snapshotSha256" {
			stripped[k] = v
		}
	}
	sum, e := Digest(map[string]any{"source": stripped, "tables": tables})
	if e != nil || sum != snapshot {
		return p, ErrInvalid
	}
	mf, e := closed(manifest, "schemaVersion", "source", "sourceFileSha256", "auditedSchemaOutputSha256", "counts", "families", "tableSha256", "execution")
	if e != nil || mf["schemaVersion"] != ManifestProtocol || mf["sourceFileSha256"] != fileHash(sourceRaw) || mf["auditedSchemaOutputSha256"] != AuditedOutputSHA256 {
		return p, ErrInvalid
	}
	mh, e := Canonical(mf["source"])
	hh, e2 := Canonical(hdr)
	if e != nil || e2 != nil || !bytes.Equal(mh, hh) {
		return p, ErrInvalid
	}
	mc, ok := mf["counts"].(map[string]any)
	hashes, ok2 := mf["tableSha256"].(map[string]any)
	if !ok || !ok2 || len(mc) != len(counts) || len(hashes) != len(counts) {
		return p, ErrInvalid
	}
	for t, n := range counts {
		v, e := integer(mc[t])
		h, he := Digest(tables[t])
		if e != nil || v != int64(n) || he != nil || hashes[t] != h {
			return p, ErrInvalid
		}
	}
	fs, ok := mf["families"].(map[string]any)
	if !ok || len(fs) != len(families) {
		return p, ErrInvalid
	}
	for k, want := range families {
		got, ok := fs[k].([]any)
		if !ok || len(got) != len(want) {
			return p, ErrInvalid
		}
		for i, v := range want {
			if got[i] != v {
				return p, ErrInvalid
			}
		}
	}
	ex, e := closed(mf["execution"], "preservation", "targetImport", "identityVerification", "sourceRights", "mediaBytesMatched", "mediaAuthorization", "sessionMigration", "relationshipEffects", "sourceWrites", "runtimeStarted")
	if e != nil {
		return p, e
	}
	for _, k := range []string{"targetImport", "identityVerification", "sourceRights", "mediaBytesMatched", "mediaAuthorization", "sessionMigration"} {
		if ex[k] != "NOT_RUN" {
			return p, ErrInvalid
		}
	}
	if ex["preservation"] != "EXTRACTED_ONLY" || ex["runtimeStarted"] != false {
		return p, ErrInvalid
	}
	for _, k := range []string{"sourceWrites", "relationshipEffects"} {
		n, e := integer(ex[k])
		if e != nil || n != 0 {
			return p, ErrInvalid
		}
	}
	p.Run = Run{RunID: runID, SourceSystem: SourceSystem, SourceProtocol: SourceProtocol, BackupCipherSHA256: BackupCipherSHA256, SchemaSHA256: SourceSchemaSHA256, SnapshotSHA256: snapshot, SourceFileSHA256: fileHash(sourceRaw), ManifestFileSHA256: fileHash(manifestRaw), Header: hh, Counts: counts}
	p.Owners = []OwnerPlan{}
	p.Notes = []NotePlan{}
	p.Deferred = map[string]int{}
	for _, t := range []string{"places", "user_saved_places", "journeys", "theme_maps", "media_assets", "auth_identities"} {
		p.Deferred[t] = counts[t]
	}
	owners := map[string]bool{}
	for _, r := range rows["users"] {
		o := OwnerPlan{}
		if o.SourceID, e = id(r["id"]); e != nil || owners[o.SourceID] {
			return Plan{}, ErrInvalid
		}
		owners[o.SourceID] = true
		if o.SourceVersion, e = integer(r["version"]); e != nil || o.SourceVersion < 1 || o.SourceVersion > legacyarchive.MaxVersion {
			return Plan{}, ErrInvalid
		}
		if o.SourceStatus, e = originalText(r["status"]); e != nil {
			return Plan{}, e
		}
		if o.DataOrigin, e = originalText(r["data_origin"]); e != nil {
			return Plan{}, e
		}
		if o.DisplayName, e = originalText(r["display_name"]); e != nil {
			return Plan{}, e
		}
		if o.Bio, e = originalText(r["bio"]); e != nil {
			return Plan{}, e
		}
		if o.Handle, e = maybeText(r["handle"]); e != nil {
			return Plan{}, e
		}
		o.SourceSHA256, _ = Digest(map[string]any{"id": o.SourceID, "version": r["version"], "status": o.SourceStatus, "data_origin": o.DataOrigin, "display_name": o.DisplayName, "bio": o.Bio, "handle": r["handle"]})
		p.Owners = append(p.Owners, o)
	}
	seen := map[string]bool{}
	for _, r := range rows["notes"] {
		n := NotePlan{}
		if n.SourceID, e = id(r["id"]); e != nil || seen[n.SourceID] {
			return Plan{}, ErrInvalid
		}
		seen[n.SourceID] = true
		if n.OwnerSourceID, e = id(r["author_id"]); e != nil || !owners[n.OwnerSourceID] {
			return Plan{}, ErrInvalid
		}
		if n.SourceVersion, e = integer(r["version"]); e != nil || n.SourceVersion < 1 || n.SourceVersion > legacyarchive.MaxVersion {
			return Plan{}, ErrInvalid
		}
		n.SourceSHA256, _ = Digest(r)
		n.Deleted = r["deleted_at"] != nil
		if n.Deleted {
			if _, e = originalText(r["deleted_at"]); e != nil {
				return Plan{}, e
			}
		}
		if n.Deleted {
			n.Payload = json.RawMessage(`{}`)
			p.Notes = append(p.Notes, n)
			continue
		}
		if n.Title, e = originalText(r["title"]); e != nil {
			return Plan{}, e
		}
		if n.Body, e = originalText(r["caption"]); e != nil {
			return Plan{}, e
		}
		if n.RecordedStartDate, e = maybeText(r["recorded_start_date"]); e != nil {
			return Plan{}, e
		}
		if n.RecordedEndDate, e = maybeText(r["recorded_end_date"]); e != nil {
			return Plan{}, e
		}
		if n.SourceVisibility, e = originalText(r["visibility"]); e != nil {
			return Plan{}, e
		}
		if n.SourceModerationStatus, e = originalText(r["moderation_status"]); e != nil {
			return Plan{}, e
		}
		rel, e := relationships(n, r, rows)
		if e != nil {
			return Plan{}, e
		}
		encoded, e := json.Marshal(rel)
		if e != nil {
			return Plan{}, ErrInvalid
		}
		v, e := decode(encoded)
		if e != nil {
			return Plan{}, e
		}
		n.Payload, e = Canonical(v)
		if e != nil {
			return Plan{}, e
		}
		p.Notes = append(p.Notes, n)
	}
	sort.Slice(p.Owners, func(i, j int) bool { return p.Owners[i].SourceID < p.Owners[j].SourceID })
	sort.Slice(p.Notes, func(i, j int) bool { return p.Notes[i].SourceID < p.Notes[j].SourceID })
	return p, nil
}

func relationships(n NotePlan, note map[string]any, rows map[string][]map[string]any) (legacyarchive.Relationships, error) {
	v := legacyarchive.Relationships{Places: []legacyarchive.PlaceSelector{}, Media: []legacyarchive.MediaSelector{}, MediaLocations: []legacyarchive.MediaLocation{}, ThreadLegs: []legacyarchive.ThreadLeg{}}
	var e error
	for _, r := range rows["note_places"] {
		if r["note_id"] != n.SourceID {
			continue
		}
		p := legacyarchive.PlaceSelector{}
		if p.SourcePlaceID, e = maybeID(r["place_id"]); e != nil {
			return v, e
		}
		if p.RawName, e = originalText(r["raw_name"]); e != nil {
			return v, e
		}
		if p.Position, e = integer(r["position"]); e != nil {
			return v, e
		}
		v.Places = append(v.Places, p)
	}
	for _, r := range rows["note_media"] {
		if r["note_id"] != n.SourceID {
			continue
		}
		m := legacyarchive.MediaSelector{State: "UNAVAILABLE"}
		if m.SourceMediaID, e = id(r["media_id"]); e != nil {
			return v, e
		}
		if m.Position, e = integer(r["position"]); e != nil {
			return v, e
		}
		v.Media = append(v.Media, m)
	}
	for _, r := range rows["note_media_locations"] {
		if r["note_id"] != n.SourceID {
			continue
		}
		m := legacyarchive.MediaLocation{SourcePlaceIDs: []string{}}
		if m.SourceMediaID, e = id(r["media_id"]); e != nil {
			return v, e
		}
		if m.Latitude, e = number(r["latitude"]); e != nil {
			return v, e
		}
		if m.Longitude, e = number(r["longitude"]); e != nil {
			return v, e
		}
		if m.MapEnabled, e = originalBool(r["map_enabled"]); e != nil {
			return v, e
		}
		if m.CapturedDate, e = maybeText(r["captured_date"]); e != nil {
			return v, e
		}
		a, ok := r["place_ids"].([]any)
		if !ok {
			return v, ErrInvalid
		}
		for _, raw := range a {
			x, e := id(raw)
			if e != nil {
				return v, e
			}
			m.SourcePlaceIDs = append(m.SourcePlaceIDs, x)
		}
		v.MediaLocations = append(v.MediaLocations, m)
	}
	if tid := note["thread_id"]; tid != nil {
		key, e := id(tid)
		if e != nil {
			return v, e
		}
		for _, r := range rows["moment_threads"] {
			if r["id"] != key {
				continue
			}
			if r["author_id"] != n.OwnerSourceID {
				return v, ErrInvalid
			}
			t := &legacyarchive.Thread{SourceThreadID: key}
			if t.Position, e = optionalInteger(note["thread_position"]); e != nil {
				return v, e
			}
			if t.EntryCount, e = integer(r["entry_count"]); e != nil {
				return v, e
			}
			if t.RouteRevision, e = integer(r["route_revision"]); e != nil {
				return v, e
			}
			if t.CreatedAt, e = originalText(r["created_at"]); e != nil {
				return v, e
			}
			v.Thread = t
		}
		if v.Thread == nil {
			return v, ErrInvalid
		}
		for _, r := range rows["moment_thread_legs"] {
			if r["thread_id"] != key {
				continue
			}
			l := legacyarchive.ThreadLeg{}
			if l.SourceFromNoteID, e = id(r["from_note_id"]); e != nil {
				return v, e
			}
			if l.SourceToNoteID, e = id(r["to_note_id"]); e != nil {
				return v, e
			}
			if l.FromPosition, e = integer(r["from_position"]); e != nil {
				return v, e
			}
			if l.ToPosition, e = integer(r["to_position"]); e != nil {
				return v, e
			}
			if l.Mode, e = originalText(r["mode"]); e != nil {
				return v, e
			}
			if l.SourceFromAnchorPlaceID, e = maybeID(r["from_anchor_place_id"]); e != nil {
				return v, e
			}
			if l.SourceToAnchorPlaceID, e = maybeID(r["to_anchor_place_id"]); e != nil {
				return v, e
			}
			if l.CreatedAt, e = originalText(r["created_at"]); e != nil {
				return v, e
			}
			if l.UpdatedAt, e = originalText(r["updated_at"]); e != nil {
				return v, e
			}
			v.ThreadLegs = append(v.ThreadLegs, l)
		}
	}
	sort.Slice(v.Places, func(i, j int) bool { return v.Places[i].Position < v.Places[j].Position })
	sort.Slice(v.Media, func(i, j int) bool { return v.Media[i].Position < v.Media[j].Position })
	sort.Slice(v.MediaLocations, func(i, j int) bool { return v.MediaLocations[i].SourceMediaID < v.MediaLocations[j].SourceMediaID })
	sort.Slice(v.ThreadLegs, func(i, j int) bool { return v.ThreadLegs[i].FromPosition < v.ThreadLegs[j].FromPosition })
	return v, legacyarchive.ValidateRelationships(v)
}
