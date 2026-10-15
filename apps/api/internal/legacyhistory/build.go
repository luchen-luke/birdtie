package legacyhistory

import (
	"bytes"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacysocial"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var columns = map[string]string{"ai_journey_threads": "id owner_id journey_id draft_context_id summary status context_revision created_at updated_at", "ai_journey_messages": "id thread_id role content created_at request_id", "notifications": "id recipient_id actor_id type aggregate_type aggregate_id event_id payload read_at created_at event_type category anchor_type anchor_id seen_at payload_schema_version inbox_version dedupe_key deleted_at", "notification_settings": "user_id settings version updated_at"}

func ColumnNames(k string) []string { return strings.Fields(columns[k]) }

type row = map[string]any

func closed(v row, keys []string) bool {
	if len(v) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := v[k]; !ok {
			return false
		}
	}
	return true
}
func decode(raw []byte) (row, error) {
	if len(raw) < 1 || len(raw) > MaxInputBytes {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v row
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	b, e := legacymigration.Canonical(v)
	if e != nil || !bytes.Equal(bytes.TrimSuffix(raw, []byte("\n")), b) {
		return nil, ErrInvalid
	}
	return v, nil
}
func text(r row, k string) string {
	if r[k] == nil {
		return ""
	}
	s, ok := r[k].(string)
	if !ok || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		panic(ErrInvalid)
	}
	return s
}
func uuid(r row, k string, optional bool) string {
	v := text(r, k)
	if optional && v == "" {
		return ""
	}
	if !legacyarchive.UUID(v) {
		panic(ErrInvalid)
	}
	return v
}
func number(r row, k string) int64 {
	v, ok := r[k].(json.Number)
	if !ok {
		panic(ErrInvalid)
	}
	n, e := strconv.ParseInt(string(v), 10, 64)
	if e != nil || n < 0 || n > 8999999999999999 {
		panic(ErrInvalid)
	}
	return n
}
func clock(r row, k string, optional bool) *time.Time {
	if r[k] == nil && optional {
		return nil
	}
	v, e := time.Parse(time.RFC3339Nano, text(r, k))
	if e != nil || v.IsZero() || v.Year() < 1 || v.Year() > 9999 {
		panic(ErrInvalid)
	}
	v = v.UTC()
	return &v
}
func hash(v any) string {
	raw, e := json.Marshal(v)
	if e != nil {
		panic(ErrInvalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var n any
	if d.Decode(&n) != nil {
		panic(ErrInvalid)
	}
	h, e := legacymigration.Digest(n)
	if e != nil {
		panic(ErrInvalid)
	}
	return h
}
func fact(kind, key string, r row) Fact { return Fact{key, hash(r), TargetID(kind, key)} }
func Build(run string, os, om, ss, sm, source, manifest []byte) (p Plan, err error) {
	defer func() {
		if recover() != nil {
			p = Plan{}
			err = ErrInvalid
		}
	}()
	original, e := legacymigration.BuildPlan(run, os, om)
	if e != nil {
		return p, ErrInvalid
	}
	social, e := legacysocial.Build(run, os, om, ss, sm)
	if e != nil {
		return p, ErrInvalid
	}
	s, e := decode(source)
	if e != nil {
		return p, e
	}
	m, e := decode(manifest)
	if e != nil {
		return p, e
	}
	if !closed(s, strings.Fields("schemaVersion source capture tables")) || !closed(m, strings.Fields("schemaVersion source sourceFileSha256 counts tableSha256 execution")) || text(s, "schemaVersion") != Protocol || text(m, "schemaVersion") != ManifestProtocol || text(m, "sourceFileSha256") != SHA(source) {
		return p, ErrInvalid
	}
	header, ok := s["source"].(map[string]any)
	if !ok || !closed(header, strings.Fields("sourceSystem runId backupCipherSha256 originalSourceFileSha256 originalManifestFileSha256 originalSnapshotSha256 socialSourceFileSha256 socialManifestFileSha256 socialSnapshotSha256 historySchemaSha256 snapshotSha256")) || hash(header) != hash(m["source"]) {
		return p, ErrInvalid
	}
	if text(header, "historySchemaSha256") != SourceSchemaSHA256 || text(header, "sourceSystem") != legacymigration.SourceSystem || text(header, "runId") != run || text(header, "backupCipherSha256") != legacymigration.BackupCipherSHA256 || text(header, "originalSourceFileSha256") != original.Run.SourceFileSHA256 || text(header, "originalManifestFileSha256") != original.Run.ManifestFileSHA256 || text(header, "originalSnapshotSha256") != original.Run.SnapshotSHA256 || text(header, "socialSourceFileSha256") != social.SourceFileSHA256 || text(header, "socialManifestFileSha256") != social.ManifestFileSHA256 || text(header, "socialSnapshotSha256") != social.SnapshotSHA256 {
		return p, ErrInvalid
	}
	base := row{}
	for k, v := range header {
		if k != "snapshotSha256" {
			base[k] = v
		}
	}
	if hash(row{"source": base, "tables": s["tables"]}) != text(header, "snapshotSha256") {
		return p, ErrInvalid
	}
	tables, ok := s["tables"].(map[string]any)
	if !ok || len(tables) != 4 {
		return p, ErrInvalid
	}
	counts, ok := m["counts"].(map[string]any)
	if !ok || len(counts) != 4 {
		return p, ErrInvalid
	}
	hashes, ok := m["tableSha256"].(map[string]any)
	if !ok || len(hashes) != 4 {
		return p, ErrInvalid
	}
	rs := map[string][]row{}
	for k, cols := range columns {
		values, ok := tables[k].([]any)
		if !ok || len(values) > 10000 || number(counts, k) != int64(len(values)) || text(hashes, k) != hash(values) {
			return p, ErrInvalid
		}
		for _, v := range values {
			r, ok := v.(map[string]any)
			if !ok || !closed(r, strings.Fields(cols)) {
				return p, ErrInvalid
			}
			rs[k] = append(rs[k], r)
		}
	}
	p = Plan{RunID: run, Original: original, SourceFileSHA256: SHA(source), ManifestFileSHA256: SHA(manifest), SnapshotSHA256: text(header, "snapshotSha256"), SocialSourceFileSHA256: social.SourceFileSHA256, SocialManifestFileSHA256: social.ManifestFileSHA256, SocialSnapshotSHA256: social.SnapshotSHA256, Threads: []Thread{}, Messages: []Message{}, Notifications: []Notification{}, Preferences: []Preferences{}}
	project(&p, rs)
	// A missing/deleted native Journey never changes the original thread owner.
	originalDoc, decodeError := decodeOriginalHistorySource(os)
	if decodeError != nil {
		return Plan{}, ErrInvalid
	}
	journeyIDs := map[string]bool{}
	for _, value := range originalDoc["tables"].(map[string]any)["journeys"].([]any) {
		r := value.(map[string]any)
		journeyIDs[uuid(r, "id", false)] = true
	}
	for _, thread := range p.Threads {
		if thread.JourneySourceID != "" && !journeyIDs[thread.JourneySourceID] {
			return Plan{}, ErrInvalid
		}
	}

	p.ProjectionSHA256 = hash([]any{Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.Threads, p.Messages, p.Notifications, p.Preferences, p.Counts})
	return p, nil
}
func project(p *Plan, rs map[string][]row) {
	owners := map[string]bool{}
	for _, o := range p.Original.Owners {
		owners[o.SourceID] = true
	}
	owner := func(id string) string {
		if !owners[id] {
			panic(ErrInvalid)
		}
		return id
	}
	seen := map[string]bool{}
	unique := func(kind, key string) {
		if seen[kind+key] {
			panic(ErrInvalid)
		}
		seen[kind+key] = true
	}
	threads := map[string]Thread{}
	for _, r := range rs["ai_journey_threads"] {
		key := uuid(r, "id", false)
		unique("thread", key)
		v := Thread{Fact: fact("thread", key, r), OwnerSourceID: owner(uuid(r, "owner_id", false)), JourneySourceID: uuid(r, "journey_id", true), DraftContextID: uuid(r, "draft_context_id", true), Summary: text(r, "summary"), SourceStatus: text(r, "status"), ContextRevision: number(r, "context_revision"), CreatedAt: *clock(r, "created_at", false), UpdatedAt: *clock(r, "updated_at", false)}
		if (v.JourneySourceID == "") == (v.DraftContextID == "") || v.SourceStatus != "active" || len(v.Summary) > 32768 {
			panic(ErrInvalid)
		}
		threads[key] = v
		p.Threads = append(p.Threads, v)
	}
	for _, r := range rs["ai_journey_messages"] {
		key, tid := uuid(r, "id", false), uuid(r, "thread_id", false)
		unique("message", key)
		if _, ok := threads[tid]; !ok {
			panic(ErrInvalid)
		}
		role := text(r, "role")
		if role != "user" && role != "assistant" {
			panic(ErrInvalid)
		}
		body := text(r, "content")
		req := text(r, "request_id")
		if len(body) > 32768 || len(req) > 1024 {
			panic(ErrInvalid)
		}
		p.Messages = append(p.Messages, Message{Fact: fact("message", key, r), ThreadSourceID: tid, Role: strings.ToUpper(role), Body: body, RequestID: req, CreatedAt: *clock(r, "created_at", false)})
	}
	known := map[string]bool{}
	for _, k := range strings.Fields("action commentId followerId journeyId noteId reason replyToCommentId requestId targetAvailable targetCoverUrl targetTitle targetType userId") {
		known[k] = true
	}
	for _, r := range rs["notifications"] {
		key := uuid(r, "id", false)
		unique("notification", key)
		v := Notification{Fact: fact("notification", key, r), OwnerSourceID: owner(uuid(r, "recipient_id", false)), ActorSourceID: uuid(r, "actor_id", true), OriginalType: text(r, "type"), EventType: text(r, "event_type"), Category: text(r, "category"), TargetKind: text(r, "aggregate_type"), TargetSourceID: uuid(r, "aggregate_id", true), AnchorType: text(r, "anchor_type"), AnchorID: uuid(r, "anchor_id", true), CreatedAt: *clock(r, "created_at", false), ReadAt: clock(r, "read_at", true), SeenAt: clock(r, "seen_at", true), DeletedAt: clock(r, "deleted_at", true), PayloadSchemaVersion: number(r, "payload_schema_version"), InboxVersion: number(r, "inbox_version"), ResidualKeys: []string{}}
		if v.ActorSourceID != "" {
			owner(v.ActorSourceID)
		}
		payload, ok := r["payload"].(map[string]any)
		if !ok {
			panic(ErrInvalid)
		}
		for k := range payload {
			if !known[k] {
				v.ResidualKeys = append(v.ResidualKeys, k)
			}
		}
		sort.Strings(v.ResidualKeys)
		if value, ok := payload["action"].(string); ok && len(value) <= 100 {
			v.Action = value
		}
		p.Notifications = append(p.Notifications, v)
		if v.DeletedAt != nil {
			p.Counts.DeletedNotifications++
		} else if v.ReadAt == nil {
			p.Counts.HistoricalUnread++
		}
	}
	required := strings.Fields("chatPush requestPush commentPush followPush praiseSavePush journeyPush systemPush previewEnabled")
	for _, r := range rs["notification_settings"] {
		id := owner(uuid(r, "user_id", false))
		unique("preferences", id)
		settings, ok := r["settings"].(map[string]any)
		if !ok {
			panic(ErrInvalid)
		}
		v := Preferences{Fact: fact("preferences", id, r), OwnerSourceID: id, Settings: map[string]bool{}, Version: number(r, "version"), UpdatedAt: *clock(r, "updated_at", false)}
		allowed := map[string]bool{}
		for _, k := range required {
			b, ok := settings[k].(bool)
			if !ok {
				panic(ErrInvalid)
			}
			v.Settings[k] = b
			allowed[k] = true
		}
		for _, k := range strings.Fields("collaborationPush timezone quietStart quietEnd") {
			allowed[k] = true
		}
		for k := range settings {
			if !allowed[k] {
				panic(ErrInvalid)
			}
		}
		if value, ok := settings["collaborationPush"]; ok {
			b, ok := value.(bool)
			if !ok {
				panic(ErrInvalid)
			}
			v.CollaborationPush = &b
		}
		v.TimeZone = text(settings, "timezone")
		if v.TimeZone != "" {
			if _, e := time.LoadLocation(v.TimeZone); e != nil {
				panic(ErrInvalid)
			}
		}
		v.QuietStart = text(settings, "quietStart")
		v.QuietEnd = text(settings, "quietEnd")
		p.Preferences = append(p.Preferences, v)
	}
	sort.Slice(p.Threads, func(i, j int) bool { return p.Threads[i].SourceKey < p.Threads[j].SourceKey })
	sort.Slice(p.Messages, func(i, j int) bool {
		a, b := p.Messages[i], p.Messages[j]
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.SourceKey < b.SourceKey
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	sort.Slice(p.Notifications, func(i, j int) bool { return p.Notifications[i].SourceKey < p.Notifications[j].SourceKey })
	sort.Slice(p.Preferences, func(i, j int) bool { return p.Preferences[i].SourceKey < p.Preferences[j].SourceKey })
	p.Counts.Threads = len(p.Threads)
	p.Counts.Messages = len(p.Messages)
	p.Counts.Notifications = len(p.Notifications)
	p.Counts.Preferences = len(p.Preferences)
}

// The original146 envelope is separately strict-validated before this decode.
func decodeOriginalHistorySource(raw []byte) (row, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v row
	if d.Decode(&v) != nil {
		return nil, ErrInvalid
	}
	return v, nil
}
