package legacysocial

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/legacyarchive"
	"github.com/birdtie/birdtie/apps/api/internal/legacycatalog"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
)

// Columns is a copy of the audited supplement allowlist, not caller controlled.
var columns = map[string]string{
	"follows":               "follower_id followee_id created_at",
	"friendships":           "user_low_id user_high_id requester_id status created_at updated_at responded_at",
	"conversations":         "id type user_low_id user_high_id status consent_epoch last_seq last_message_id last_message_at created_at updated_at title join_policy member_limit history_for_new_members journey_id owner_id school_label archived_at show_on_profile",
	"conversation_members":  "conversation_id user_id last_read_seq last_delivered_seq unread_count muted_until joined_at updated_at pinned hidden marked_unread cleared_through_seq role removed_at",
	"messages":              "id conversation_id sender_id seq client_message_id payload_hash type text entity_type entity_id context_type context_id request_id recalled_at moderated_at created_at sticker_pack_id sticker_id",
	"message_requests":      "id conversation_id epoch requester_id recipient_id status first_message_id seen_at resolved_at retry_after created_at",
	"message_attachments":   "message_id media_id position",
	"private_message_media": "id owner_id conversation_id status width height size_bytes storage_key created_at ready_at kind duration_ms file_name",
}

func ColumnNames(table string) []string { return strings.Fields(columns[table]) }

type row = map[string]any

func decode(raw []byte) (row, error) {
	if len(raw) == 0 || len(raw) > MaxInputBytes {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v row
	if d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	// A canonical parse also rejects duplicate keys, because the sealed source
	// has exactly the extractor's canonical bytes plus one newline.
	b, e := legacymigration.Canonical(v)
	if e != nil || !bytes.Equal(bytes.TrimSuffix(raw, []byte("\n")), b) {
		return nil, ErrInvalid
	}
	return v, nil
}
func closed(m row, keys []string) bool {
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
func id(r row, k string, optional bool) string {
	v := text(r, k)
	if optional && v == "" {
		return ""
	}
	if !legacyarchive.UUID(v) {
		panic(ErrInvalid)
	}
	return v
}
func num(r row, k string) int64 {
	v, ok := r[k].(json.Number)
	if !ok {
		panic(ErrInvalid)
	}
	n, e := strconv.ParseInt(string(v), 10, 64)
	if e != nil || n < 0 {
		panic(ErrInvalid)
	}
	return n
}
func boolean(r row, k string) bool {
	v, ok := r[k].(bool)
	if !ok {
		panic(ErrInvalid)
	}
	return v
}
func clock(r row, k string, optional bool) *time.Time {
	if r[k] == nil && optional {
		return nil
	}
	s := text(r, k)
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || t.IsZero() || t.Year() < 1 || t.Year() > 9999 {
		panic(ErrInvalid)
	}
	t = t.UTC()
	return &t
}
func one(v string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	panic(ErrInvalid)
}
func digest(v any) string {
	raw, e := json.Marshal(v)
	if e != nil {
		panic(ErrInvalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var normalized any
	if d.Decode(&normalized) != nil {
		panic(ErrInvalid)
	}
	s, e := legacymigration.Digest(normalized)
	if e != nil {
		panic(ErrInvalid)
	}
	return s
}
func fact(kind, key string, r row) Fact {
	return Fact{SourceKey: key, SourceSHA256: digest(r), TargetID: TargetID(kind, key)}
}
func pair(a, b string) string { return a + ":" + b }

// Build verifies the original closed29 source and the independently sealed8
// supplement. All original rows remain in protected preservation; no source
// token, password, invite, upload lease or URL becomes a native capability.
func Build(run string, originalSource, originalManifest, source, manifest []byte) (p Plan, err error) {
	defer func() {
		if recover() != nil {
			p = Plan{}
			err = ErrInvalid
		}
	}()
	original, e := legacymigration.BuildPlan(run, originalSource, originalManifest)
	if e != nil {
		return p, ErrInvalid
	}
	catalog, e := legacycatalog.Build(run, originalSource, originalManifest)
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
	if !closed(s, strings.Fields("schemaVersion source capture tables deferredCounts")) || text(s, "schemaVersion") != Protocol || text(m, "schemaVersion") != ManifestProtocol || text(m, "sourceFileSha256") != SHA(source) {
		return p, ErrInvalid
	}
	h, ok := s["source"].(map[string]any)
	if !ok {
		return p, ErrInvalid
	}
	header := row(h)
	if !closed(header, strings.Fields("sourceSystem runId backupCipherSha256 originalSourceFileSha256 originalManifestFileSha256 originalSnapshotSha256 socialSchemaSha256 snapshotSha256")) || digest(header) != digest(m["source"]) || text(header, "sourceSystem") != legacymigration.SourceSystem || text(header, "runId") != run || text(header, "socialSchemaSha256") != SourceSchemaSHA256 || text(header, "backupCipherSha256") != legacymigration.BackupCipherSHA256 || text(header, "originalSourceFileSha256") != original.Run.SourceFileSHA256 || text(header, "originalManifestFileSha256") != original.Run.ManifestFileSHA256 || text(header, "originalSnapshotSha256") != original.Run.SnapshotSHA256 {
		return p, ErrInvalid
	}
	tables, ok := s["tables"].(map[string]any)
	if !ok || len(tables) != len(columns) {
		return p, ErrInvalid
	}
	base := row{}
	for k, v := range header {
		if k != "snapshotSha256" {
			base[k] = v
		}
	}
	if digest(row{"source": base, "tables": tables, "deferredCounts": s["deferredCounts"]}) != text(header, "snapshotSha256") || digest(s["deferredCounts"]) != digest(m["deferredCounts"]) {
		return p, ErrInvalid
	}
	counts, ok := m["counts"].(map[string]any)
	if !ok {
		return p, ErrInvalid
	}
	hashes, ok := m["tableSha256"].(map[string]any)
	if !ok {
		return p, ErrInvalid
	}
	rows := map[string][]row{}
	for table, cols := range columns {
		values, ok := tables[table].([]any)
		if !ok || len(values) > 10000 || num(row(counts), table) != int64(len(values)) || text(row(hashes), table) != digest(values) {
			return p, ErrInvalid
		}
		for _, value := range values {
			r, ok := value.(map[string]any)
			if !ok || !closed(row(r), strings.Fields(cols)) {
				return p, ErrInvalid
			}
			rows[table] = append(rows[table], row(r))
		}
	}
	p = Plan{RunID: run, Original: original, SourceFileSHA256: SHA(source), ManifestFileSHA256: SHA(manifest), SnapshotSHA256: text(header, "snapshotSha256")}
	owners := map[string]bool{}
	for _, o := range original.Owners {
		owners[o.SourceID] = true
	}
	owner := func(v string) string {
		if !owners[v] {
			panic(ErrInvalid)
		}
		return v
	}
	seen := map[string]bool{}
	unique := func(kind, key string) {
		k := kind + ":" + key
		if seen[k] {
			panic(ErrInvalid)
		}
		seen[k] = true
	}
	accepted := map[string]string{}
	for _, r := range rows["friendships"] {
		a, b := owner(id(r, "user_low_id", false)), owner(id(r, "user_high_id", false))
		requester := owner(id(r, "requester_id", false))
		if a >= b || (requester != a && requester != b) {
			return Plan{}, ErrInvalid
		}
		key := pair(a, b)
		unique("friendship", key)
		f := Friendship{Fact: fact("friendship", key, r), LowID: a, HighID: b, RequesterID: requester, State: one(text(r, "status"), "accepted", "pending", "declined", "withdrawn"), CreatedAt: *clock(r, "created_at", false), UpdatedAt: *clock(r, "updated_at", false), RespondedAt: clock(r, "responded_at", true)}
		if f.State == "accepted" {
			if f.RespondedAt == nil {
				return Plan{}, ErrInvalid
			}
			accepted[key] = f.TargetID
			p.Counts.AcceptedFriends++
		}
		if f.State == "pending" {
			p.Counts.PendingFriends++
		}
		p.Friendships = append(p.Friendships, f)
	}
	for _, r := range rows["follows"] {
		a, b := owner(id(r, "follower_id", false)), owner(id(r, "followee_id", false))
		if a == b {
			return Plan{}, ErrInvalid
		}
		key := pair(a, b)
		unique("follow", key)
		p.Follows = append(p.Follows, Follow{Fact: fact("follow", key, r), FollowerID: a, FolloweeID: b, CreatedAt: *clock(r, "created_at", false)})
	}
	conversations := map[string]Conversation{}
	for _, r := range rows["conversations"] {
		key := id(r, "id", false)
		unique("conversation", key)
		kind := one(text(r, "type"), "direct", "group")
		c := Conversation{Fact: fact("conversation", key, r), Kind: strings.ToUpper(kind), LowID: id(r, "user_low_id", true), HighID: id(r, "user_high_id", true), Title: text(r, "title"), State: one(text(r, "status"), "active", "empty", "pending", "blocked", "archived"), ConsentEpoch: num(r, "consent_epoch"), LastSequence: num(r, "last_seq"), JoinPolicy: text(r, "join_policy"), HistoryForNewMembers: boolean(r, "history_for_new_members"), MemberLimit: num(r, "member_limit"), JourneyID: id(r, "journey_id", true), OwnerID: id(r, "owner_id", true), SchoolLabel: text(r, "school_label"), ArchivedAt: clock(r, "archived_at", true), ShowOnProfile: boolean(r, "show_on_profile"), CreatedAt: *clock(r, "created_at", false), UpdatedAt: *clock(r, "updated_at", false)}
		if kind == "direct" {
			if c.LowID >= c.HighID || c.LowID == "" {
				return Plan{}, ErrInvalid
			}
			owner(c.LowID)
			owner(c.HighID)
			c.AcceptanceSource = accepted[pair(c.LowID, c.HighID)]
		} else {
			c.Kind = "PERSONAL_GROUP"
			if c.LowID != "" || c.HighID != "" {
				return Plan{}, ErrInvalid
			}
			if c.OwnerID != "" {
				owner(c.OwnerID)
			}
		}
		conversations[key] = c
		p.Conversations = append(p.Conversations, c)
	}
	members := map[string]Member{}
	for _, r := range rows["conversation_members"] {
		cid, uid := id(r, "conversation_id", false), owner(id(r, "user_id", false))
		c, exists := conversations[cid]
		if !exists {
			return Plan{}, ErrInvalid
		}
		if c.Kind == "DIRECT" && uid != c.LowID && uid != c.HighID {
			return Plan{}, ErrInvalid
		}
		key := pair(cid, uid)
		unique("member", key)
		v := Member{Fact: fact("member", key, r), ConversationID: cid, OwnerID: uid, Role: text(r, "role"), ReadSequence: num(r, "last_read_seq"), DeliveredSequence: num(r, "last_delivered_seq"), UnreadCount: num(r, "unread_count"), ClearedSequence: num(r, "cleared_through_seq"), JoinedAt: *clock(r, "joined_at", false), UpdatedAt: *clock(r, "updated_at", false), MutedUntil: clock(r, "muted_until", true), RemovedAt: clock(r, "removed_at", true), Pinned: boolean(r, "pinned"), Hidden: boolean(r, "hidden"), MarkedUnread: boolean(r, "marked_unread")}
		if v.ReadSequence > c.LastSequence || v.DeliveredSequence > c.LastSequence || v.ClearedSequence > c.LastSequence {
			return Plan{}, ErrInvalid
		}
		members[key] = v
		p.Members = append(p.Members, v)
	}
	notes := map[string]legacymigration.NotePlan{}
	for _, n := range original.Notes {
		notes[n.SourceID] = n
	}
	seqs := map[string]bool{}
	for _, r := range rows["messages"] {
		key, cid, sender := id(r, "id", false), id(r, "conversation_id", false), owner(id(r, "sender_id", false))
		unique("message", key)
		c, exists := conversations[cid]
		if !exists {
			return Plan{}, ErrInvalid
		}
		if _, ok := members[pair(cid, sender)]; !ok {
			return Plan{}, ErrInvalid
		}
		v := Message{Fact: fact("message", key, r), ConversationID: cid, SenderID: sender, Kind: strings.ToUpper(one(text(r, "type"), "text", "image", "entity", "system", "sticker")), Body: text(r, "text"), Sequence: num(r, "seq"), CreatedAt: *clock(r, "created_at", false), RecalledAt: clock(r, "recalled_at", true), ModeratedAt: clock(r, "moderated_at", true), EntityKind: text(r, "entity_type"), EntitySourceID: id(r, "entity_id", true), ContextKind: text(r, "context_type"), ContextID: id(r, "context_id", true), RequestID: id(r, "request_id", true), ClientMessageID: text(r, "client_message_id"), PayloadHash: text(r, "payload_hash"), StickerPackID: text(r, "sticker_pack_id"), StickerID: text(r, "sticker_id")}
		if v.Sequence < 1 || v.Sequence > c.LastSequence || len(v.Body) > 2000 || ((v.Kind == "TEXT" || v.Kind == "SYSTEM" || v.Kind == "STICKER") && v.Body == "") {
			return Plan{}, ErrInvalid
		}
		sk := fmt.Sprintf("%s:%d", cid, v.Sequence)
		if seqs[sk] {
			return Plan{}, ErrInvalid
		}
		seqs[sk] = true
		if v.Kind == "ENTITY" {
			switch v.EntityKind {
			case "note":
				n, ok := notes[v.EntitySourceID]
				if !ok {
					return Plan{}, ErrInvalid
				}
				v.EntityKind = "moment"
				if !n.Deleted {
					v.EntityTargetID = legacynative.StableTargetID("note", n.OwnerSourceID, n.SourceID)
				}
			case "place":
				v.EntityTargetID = catalog.PlaceMappings[v.EntitySourceID]
				if v.EntityTargetID == "" {
					v.EntityTargetID = legacycatalog.StablePlaceTargetID(v.EntitySourceID)
				}
			default:
				return Plan{}, ErrInvalid
			}
		}
		if v.Kind == "STICKER" && (v.StickerPackID == "" || v.StickerID == "") {
			return Plan{}, ErrInvalid
		}
		p.Messages = append(p.Messages, v)
	}
	messages := map[string]Message{}
	for _, v := range p.Messages {
		messages[v.SourceKey] = v
	}
	for _, r := range rows["message_requests"] {
		key, cid := id(r, "id", false), id(r, "conversation_id", false)
		unique("message_request", key)
		c, ok := conversations[cid]
		if !ok || c.Kind != "DIRECT" {
			return Plan{}, ErrInvalid
		}
		v := Request{Fact: fact("message_request", key, r), ConversationID: cid, RequesterID: owner(id(r, "requester_id", false)), RecipientID: owner(id(r, "recipient_id", false)), State: one(text(r, "status"), "accepted", "pending", "declined", "cancelled", "expired"), FirstMessageID: id(r, "first_message_id", true), Epoch: num(r, "epoch"), CreatedAt: *clock(r, "created_at", false), SeenAt: clock(r, "seen_at", true), ResolvedAt: clock(r, "resolved_at", true), RetryAfter: clock(r, "retry_after", true)}
		if v.RequesterID == v.RecipientID || !(v.RequesterID == c.LowID || v.RequesterID == c.HighID) || !(v.RecipientID == c.LowID || v.RecipientID == c.HighID) {
			return Plan{}, ErrInvalid
		}
		if v.FirstMessageID != "" && messages[v.FirstMessageID].ConversationID != cid {
			return Plan{}, ErrInvalid
		}
		if v.State == "accepted" && v.ResolvedAt == nil {
			return Plan{}, ErrInvalid
		}
		p.Requests = append(p.Requests, v)
		if v.State == "accepted" {
			for i := range p.Conversations {
				if p.Conversations[i].SourceKey == cid && p.Conversations[i].AcceptanceSource == "" {
					p.Conversations[i].AcceptanceSource = v.TargetID
				}
			}
		}
	}
	attachments := map[string]row{}
	for _, r := range rows["message_attachments"] {
		mid, media := id(r, "message_id", false), id(r, "media_id", false)
		if _, ok := attachments[media]; ok {
			return Plan{}, ErrInvalid
		}
		if messages[mid].Kind != "IMAGE" {
			return Plan{}, ErrInvalid
		}
		attachments[media] = r
	}
	for _, r := range rows["private_message_media"] {
		key := id(r, "id", false)
		unique("image", key)
		a, ok := attachments[key]
		if !ok {
			return Plan{}, ErrInvalid
		}
		mid := id(a, "message_id", false)
		msg := messages[mid]
		v := Image{Fact: fact("image", key, r), MessageID: mid, ConversationID: id(r, "conversation_id", false), OwnerID: owner(id(r, "owner_id", false)), StorageKey: id(r, "storage_key", false), AttachmentSHA256: digest(a), State: one(text(r, "status"), "ready"), Kind: one(text(r, "kind"), "image"), FileName: text(r, "file_name"), Width: num(r, "width"), Height: num(r, "height"), ByteSize: num(r, "size_bytes"), Position: num(a, "position"), CreatedAt: *clock(r, "created_at", false), ReadyAt: clock(r, "ready_at", true)}
		if r["duration_ms"] != nil {
			n := num(r, "duration_ms")
			v.DurationMillis = &n
		}
		if msg.ConversationID != v.ConversationID || msg.SenderID != v.OwnerID || v.Width < 1 || v.Height < 1 || v.ByteSize < 1 || v.ByteSize > 10<<20 || v.Position != 0 {
			return Plan{}, ErrInvalid
		}
		p.Images = append(p.Images, v)
	}
	if len(p.Images) != len(attachments) {
		return Plan{}, ErrInvalid
	}
	for _, c := range p.Conversations {
		n := 0
		for _, m := range p.Members {
			if m.ConversationID == c.SourceKey {
				n++
			}
		}
		if n < 2 || (c.Kind == "DIRECT" && n != 2) {
			return Plan{}, ErrInvalid
		}
	}
	sort.Slice(p.Messages, func(i, j int) bool {
		a, b := p.Messages[i], p.Messages[j]
		if a.ConversationID != b.ConversationID {
			return a.ConversationID < b.ConversationID
		}
		return a.Sequence < b.Sequence
	})
	p.Counts.Friendships = len(p.Friendships)
	p.Counts.Follows = len(p.Follows)
	p.Counts.Conversations = len(p.Conversations)
	p.Counts.Members = len(p.Members)
	p.Counts.Messages = len(p.Messages)
	p.Counts.MessageRequests = len(p.Requests)
	p.Counts.Images = len(p.Images)
	p.ProjectionSHA256 = digest([]any{Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.Friendships, p.Follows, p.Conversations, p.Members, p.Messages, p.Requests, p.Images})
	return p, nil
}
