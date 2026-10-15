package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacysocial"
)

func TestLegacySocialAuthenticatedProtectedImageDecode(t *testing.T) {
	root := os.Getenv("BIRDTIE_LEGACY_SOCIAL_PROTECTED_ROOT")
	if root == "" {
		t.Skip("NOT_RUN: exact protected image not configured")
	}
	read := func(parts ...string) []byte {
		b, e := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
		if e != nil {
			t.Fatal("protected input unavailable")
		}
		return b
	}
	original := read("civu-private-extract-20261015-01", "source.json")
	om := read("civu-private-extract-20261015-01", "manifest.json")
	source := read("civu-private-social-supplement-20261015-01", "social-source.private.json")
	sm := read("civu-private-social-supplement-20261015-01", "social-manifest.private.json")
	match := read("civu-private-chat-image-20261015-03", "chat-image-match.private.json")
	seal := read("civu-private-chat-image-20261015-03", "chat-image-authenticated-seal.private.json")
	var m struct {
		Selector struct {
			SHA string `json:"sha256"`
		} `json:"selector"`
	}
	if json.Unmarshal(match, &m) != nil || len(m.Selector.SHA) != 64 {
		t.Fatal("exact image selector unavailable")
	}
	raw := read("civu-private-chat-image-20261015-03", m.Selector.SHA+".blob")
	result, e := PrepareLegacySocial(context.Background(), "c5df0d7c-f361-4275-9838-feb6d1d92352", original, om, source, sm, LegacySocialImageInput{Matcher: match, Seal: seal, Content: raw, ExpectedSealSHA256: legacysocial.SHA(seal)})
	if e != nil || result.Counts.Messages != 39 || result.Counts.Images != 1 || result.ImageDecode != "FULL_DECODE_REENCODED" || result.Scanner != "NOT_RUN" || result.PrivacyRisk != "UNKNOWN" {
		t.Fatal("authenticated source image did not actually decode/reencode")
	}
}

func socialSyntheticInput(t *testing.T) ([]byte, []byte, []byte, []byte, LegacySocialImageInput) {
	t.Helper()
	const a = "14600000-0000-4000-8000-000000000010"
	const b = "14600000-0000-4000-8000-000000000011"
	const c = "14600000-0000-4000-8000-000000000012"
	original, om := legacySyntheticInput(t, func(tables map[string]any) {
		u := tables["users"].([]any)[0].(map[string]any)
		for i, id := range []string{b, c} {
			copy := map[string]any{}
			for k, v := range u {
				copy[k] = v
			}
			copy["id"] = id
			copy["handle"] = fmt.Sprintf("social-synthetic-%d", i)
			tables["users"] = append(tables["users"].([]any), copy)
		}
	})
	stamp := "2026-10-14T00:00:00Z"
	cid := "16300000-0000-4000-8000-000000000001"
	group := "16300000-0000-4000-8000-000000000002"
	noaccept := "16300000-0000-4000-8000-000000000003"
	tables := map[string]any{}
	names := []string{"follows", "friendships", "conversations", "conversation_members", "messages", "message_requests", "message_attachments", "private_message_media"}
	for _, n := range names {
		tables[n] = []any{}
	}
	add := func(table string, values map[string]any) {
		r := map[string]any{}
		for _, k := range legacysocial.ColumnNames(table) {
			r[k] = nil
		}
		for k, v := range values {
			r[k] = v
		}
		tables[table] = append(tables[table].([]any), r)
	}
	add("friendships", map[string]any{"user_low_id": a, "user_high_id": b, "requester_id": a, "status": "accepted", "created_at": stamp, "updated_at": stamp, "responded_at": stamp})
	add("friendships", map[string]any{"user_low_id": a, "user_high_id": c, "requester_id": a, "status": "pending", "created_at": stamp, "updated_at": stamp})
	add("follows", map[string]any{"follower_id": a, "followee_id": c, "created_at": stamp})
	for i, cv := range []string{cid, group, noaccept} {
		r := map[string]any{"id": cv, "type": "direct", "user_low_id": a, "user_high_id": b, "status": "active", "consent_epoch": 1, "last_seq": 10, "created_at": stamp, "updated_at": stamp, "title": "", "join_policy": "invite", "member_limit": 10, "history_for_new_members": true, "school_label": "", "show_on_profile": false}
		members := []string{a, b}
		if i == 1 {
			r["type"] = "group"
			r["user_low_id"] = nil
			r["user_high_id"] = nil
			r["last_seq"] = 1
			r["title"] = "合成私人群聊"
			r["owner_id"] = a
			members = []string{a, b, c}
		}
		if i == 2 {
			r["user_high_id"] = c
			r["last_seq"] = 0
			members = []string{a, c}
		}
		add("conversations", r)
		for _, member := range members {
			add("conversation_members", map[string]any{"conversation_id": cv, "user_id": member, "last_read_seq": 0, "last_delivered_seq": 0, "unread_count": 0, "joined_at": stamp, "updated_at": stamp, "pinned": false, "hidden": false, "marked_unread": false, "cleared_through_seq": 0, "role": "member"})
		}
	}
	mids := []string{}
	for i, kind := range []string{"text", "system", "sticker", "entity", "image"} {
		mid := fmt.Sprintf("16300000-0000-4000-8000-%012d", i+10)
		mids = append(mids, mid)
		r := map[string]any{"id": mid, "conversation_id": cid, "sender_id": b, "seq": []int{1, 2, 4, 8, 10}[i], "client_message_id": fmt.Sprintf("old-%d", i), "payload_hash": "source-old-hash", "type": kind, "text": "原合成消息", "created_at": stamp, "sticker_pack_id": "", "sticker_id": ""}
		if kind == "entity" {
			r["entity_type"] = "note"
			r["entity_id"] = "14600000-0000-4000-8000-000000000020"
			r["text"] = ""
		}
		if kind == "image" {
			r["text"] = ""
		}
		if kind == "sticker" {
			r["sticker_pack_id"] = "civu-bird-v1"
			r["sticker_id"] = "hello"
			r["text"] = "[栖游鸟·你好]"
		}
		add("messages", r)
	}
	add("messages", map[string]any{"id": "16300000-0000-4000-8000-000000000020", "conversation_id": group, "sender_id": c, "seq": 1, "client_message_id": "old-group", "payload_hash": "group-source-hash", "type": "text", "text": "原群聊正文", "created_at": stamp, "sticker_pack_id": "", "sticker_id": ""})
	add("message_requests", map[string]any{"id": "16300000-0000-4000-8000-000000000030", "conversation_id": cid, "epoch": 1, "requester_id": b, "recipient_id": a, "status": "accepted", "first_message_id": mids[0], "resolved_at": stamp, "created_at": stamp})
	im := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			im.Set(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), B: 50, A: 255})
		}
	}
	var encoded bytes.Buffer
	if e := jpeg.Encode(&encoded, im, &jpeg.Options{Quality: 85}); e != nil {
		t.Fatal(e)
	}
	content := encoded.Bytes()
	media := "16300000-0000-4000-8000-000000000040"
	key := "16300000-0000-4000-8000-000000000041"
	add("private_message_media", map[string]any{"id": media, "owner_id": b, "conversation_id": cid, "status": "ready", "width": 16, "height": 12, "size_bytes": len(content), "storage_key": key, "created_at": stamp, "ready_at": stamp, "kind": "image", "duration_ms": 0, "file_name": ""})
	add("message_attachments", map[string]any{"message_id": mids[4], "media_id": media, "position": 0})
	source, manifest := socialSyntheticSeal(t, original, om, tables)
	plan, e := legacysocial.Build(legacyTestRunID, original, om, source, manifest)
	if e != nil {
		t.Fatal("synthetic closed social source", e)
	}
	v := plan.Images[0]
	var messageSHA string
	for _, m := range plan.Messages {
		if m.SourceKey == mids[4] {
			messageSHA = m.SourceSHA256
		}
	}
	matcher, _ := json.Marshal(map[string]any{"schemaVersion": "birdtie-civu-private-chat-image-match-v1", "socialSourceFileSha256": plan.SourceFileSHA256, "socialManifestFileSha256": plan.ManifestFileSHA256, "archivePlaintextSHA256": legacysocial.SHA([]byte("synthetic archive")), "extractedFiles": 1, "selector": map[string]any{"sourceMessageId": v.MessageID, "sourceConversationId": v.ConversationID, "sourceMediaId": v.SourceKey, "sourceOwnerId": v.OwnerID, "sourceStorageKey": v.StorageKey, "sourceMessageSha256": messageSHA, "sourceAttachmentSha256": v.AttachmentSHA256, "sourceMediaSha256": v.SourceSHA256, "archiveMember": "media/private-messaging/" + key + ".jpg", "storageKeyPolicy": "CIVU_PRIVATE_MESSAGING_FULL_JPEG_V1", "sha256": legacysocial.SHA(content), "bytes": len(content), "sourceBytes": len(content), "sourceWidth": 16, "sourceHeight": 12, "observedMIME": "image/jpeg", "binaryMatched": true}})
	seal, _ := json.Marshal(map[string]any{"format": "birdtie-civu-private-chat-image-authenticated-match-v1", "aesGcmAuthenticated": true, "plaintextSHA256": legacysocial.SHA([]byte("synthetic archive")), "matcherFileSHA256": legacysocial.SHA(matcher), "socialSourceFileSha256": plan.SourceFileSHA256, "socialManifestFileSha256": plan.ManifestFileSHA256, "extractedFiles": 1, "imageBytes": len(content), "readerExit": 0, "sourceWrites": 0, "nativeWrites": 0})
	return original, om, source, manifest, LegacySocialImageInput{Matcher: matcher, Seal: seal, Content: content, ExpectedSealSHA256: legacysocial.SHA(seal)}
}
func socialSyntheticSeal(t *testing.T, original, om []byte, tables map[string]any) ([]byte, []byte) {
	t.Helper()
	p, e := legacymigration.BuildPlan(legacyTestRunID, original, om)
	if e != nil {
		t.Fatal(e)
	}
	h := map[string]any{"sourceSystem": legacymigration.SourceSystem, "runId": legacyTestRunID, "backupCipherSha256": legacymigration.BackupCipherSHA256, "originalSourceFileSha256": p.Run.SourceFileSHA256, "originalManifestFileSha256": p.Run.ManifestFileSHA256, "originalSnapshotSha256": p.Run.SnapshotSHA256, "socialSchemaSha256": legacysocial.SourceSchemaSHA256}
	deferred := map[string]any{}
	h["snapshotSha256"], e = legacymigration.Digest(map[string]any{"source": h, "tables": tables, "deferredCounts": deferred})
	if e != nil {
		t.Fatal(e)
	}
	source, e := legacymigration.Canonical(map[string]any{"schemaVersion": legacysocial.Protocol, "source": h, "capture": map[string]any{}, "tables": tables, "deferredCounts": deferred})
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, '\n')
	counts, hashes := map[string]any{}, map[string]any{}
	for k, v := range tables {
		counts[k] = len(v.([]any))
		hashes[k], _ = legacymigration.Digest(v)
	}
	manifest, e := legacymigration.Canonical(map[string]any{"schemaVersion": legacysocial.ManifestProtocol, "source": h, "sourceFileSha256": legacysocial.SHA(source), "counts": counts, "tableSha256": hashes, "deferredCounts": deferred})
	if e != nil {
		t.Fatal(e)
	}
	return source, append(manifest, '\n')
}

func TestLegacySocialNativeAtomicHistoryCurrentImageAndOriginalRetry(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	source, manifest, social, sm, imageInput := socialSyntheticInput(t)
	if _, e := f.Importer.Apply(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("original preservation", e)
	}
	if _, e := f.Pool.Exec(f.Context, `CREATE FUNCTION birdtie_social_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic social fault'; END $$;CREATE TRIGGER birdtie_social_fault BEFORE INSERT ON conversation_messages FOR EACH ROW EXECUTE FUNCTION birdtie_social_fault()`); e != nil {
		t.Fatal(e)
	}
	if _, e := f.Importer.ApplyLegacySocial(f.Context, legacyTestRunID, source, manifest, social, sm, imageInput); !errors.Is(e, ErrLegacyMigrationConflict) {
		t.Fatal("source fault did not reject", e)
	}
	var zero bool
	if e := f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM legacy_social_import_runs) AND NOT EXISTS(SELECT 1 FROM conversation_messages) AND NOT EXISTS(SELECT 1 FROM person_ties) AND NOT EXISTS(SELECT 1 FROM follows)`).Scan(&zero); e != nil || !zero {
		t.Fatal("partial social import persisted")
	}
	if _, e := f.Pool.Exec(f.Context, `DROP TRIGGER birdtie_social_fault ON conversation_messages;DROP FUNCTION birdtie_social_fault()`); e != nil {
		t.Fatal(e)
	}
	first, e := f.Importer.ApplyLegacySocial(f.Context, legacyTestRunID, source, manifest, social, sm, imageInput)
	if e != nil || first.Counts.Messages != 6 || first.Counts.Images != 1 {
		t.Fatal("native source projection", e)
	}
	var owner, peer, third string
	for src, dst := range map[string]*string{"14600000-0000-4000-8000-000000000010": &owner, "14600000-0000-4000-8000-000000000011": &peer, "14600000-0000-4000-8000-000000000012": &third} {
		if e = f.Pool.QueryRow(f.Context, `SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user' AND source_id=$1`, src).Scan(dst); e != nil {
			t.Fatal(e)
		}
	}
	var preserved bool
	e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM connection_request_decision_receipts) AND NOT EXISTS(SELECT 1 FROM sessions) AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=ANY($1::uuid[]) AND status<>'suspended') AND EXISTS(SELECT 1 FROM connection_requests WHERE state='pending' AND expires_at IS NULL AND note='') AND EXISTS(SELECT 1 FROM conversation_messages WHERE message_kind='IMAGE' AND body='' AND client_operation_id IS NULL)`, []string{owner, peer, third}).Scan(&preserved)
	if e != nil || !preserved {
		t.Fatal("import fabricated consent, auth or message body", e)
	}
	store := New(f.Pool, true)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	d := sha256.Sum256([]byte("social163 synthetic session"))
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1;`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, owner, d[:]); e != nil {
		t.Fatal(e)
	}
	frame, e := store.CaptureHumanControlSession(f.Context, d, actor)
	if e != nil {
		t.Fatal(e)
	}
	ctx := identity.WithHumanControlFrame(f.Context, d, actor, frame)
	a := ea.Access{Actor: actor, SessionDigest: d}
	pendingID := legacysocial.TargetID("friendship", "14600000-0000-4000-8000-000000000010:14600000-0000-4000-8000-000000000012")
	pendingView, e := store.ReadContactRequestCurrent(ctx, a, pendingID)
	if e != nil || pendingView.Origin != connection.LegacySocialOrigin || !pendingView.ExpiresAt.IsZero() || pendingView.Note != "" {
		t.Fatal("original pending history with dormant peer", e)
	}
	pendingJSON, _ := json.Marshal(pendingView)
	if !bytes.Contains(pendingJSON, []byte(`"expiresAt":null`)) {
		t.Fatal("unknown original expiry became a fabricated clock")
	}
	ties, e := store.ListContactTiesPage(ctx, a, connection.PageRequest{})
	if e != nil || len(ties.Items) != 1 {
		t.Fatal("dormant original tie selector", e)
	}
	follows, e := store.ListContactFollowsPage(ctx, a, connection.PageRequest{})
	if e != nil || len(follows.Items) != 1 {
		t.Fatal("private original follow selector", e)
	}
	page, e := store.ListConversationsPage(ctx, a, connection.PageRequest{})
	if e != nil || len(page.Items) != 3 {
		t.Fatal("source history membership lost dormant peers/group", e)
	}
	direct := legacysocial.TargetID("conversation", "16300000-0000-4000-8000-000000000001")
	group := legacysocial.TargetID("conversation", "16300000-0000-4000-8000-000000000002")
	current, e := store.ConversationCurrent(ctx, a, group)
	if e != nil || current.Kind != "PERSONAL_GROUP" || current.OtherAccountID != "" || current.CanSend == nil || *current.CanSend {
		t.Fatal("group became pair send capability", e)
	}
	history, e := store.ListMessagesPage(ctx, a, direct, connection.PageRequest{Limit: 2})
	if e != nil || len(history.Items) != 2 || history.NextCursor == nil || history.Items[0].Sequence != 8 || history.Items[1].Sequence != 10 || history.Items[1].Body != "" || history.Items[1].Image == nil {
		t.Fatal("typed source sequence/image", e)
	}
	if e = store.ValidateChatPageResponse(ctx, a, []string{direct}, history); e != nil {
		t.Fatal("original encoded history gate", e)
	}
	earlier, e := store.ListMessagesPage(ctx, a, direct, connection.PageRequest{Limit: 5, Cursor: *history.NextCursor})
	if e != nil || len(earlier.Items) != 3 || earlier.Items[1].Kind != "SYSTEM" || earlier.Items[1].SpeakerKind != "system" || earlier.Items[2].Sticker == nil {
		t.Fatal("source kinds lost on pagination", e)
	}
	mid := history.Items[1].ID
	raw, imported, e := store.ReadImportedImageMessageContent(ctx, a, direct, mid, "full", func(m connection.ImageDerivativeMetadata, b []byte) ([]byte, error) {
		if !connection.ValidImageContent(m, b, false) {
			return nil, connection.ErrConflict
		}
		return append([]byte(nil), b...), nil
	})
	if e != nil || !imported || len(raw) == 0 {
		t.Fatal("current source image unreadable", e)
	}
	if _, e = store.MarkRead(ctx, owner, direct, mid); e != nil {
		t.Fatal("source read state", e)
	}
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM follows WHERE follower_account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE person_ties SET status='removed' WHERE person_a_account_id=$1 OR person_b_account_id=$1`, owner); e != nil {
		t.Fatal("source native removal", e)
	}
	again, e := f.Importer.ApplyLegacySocial(f.Context, legacyTestRunID, source, manifest, social, sm, imageInput)
	if e != nil || !reflect.DeepEqual(first, again) {
		t.Fatal("original receipt changed", e)
	}
	var unchanged bool
	e = f.Pool.QueryRow(f.Context, `SELECT NOT EXISTS(SELECT 1 FROM follows WHERE follower_account_id=$1) AND NOT EXISTS(SELECT 1 FROM person_ties WHERE status='active') AND EXISTS(SELECT 1 FROM conversation_member_states WHERE member_account_id=$1 AND legacy_read_sequence=10)`, owner).Scan(&unchanged)
	if e != nil || !unchanged {
		t.Fatal("retry restored removed relationships/read state", e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id)VALUES($1,$2)`, owner, peer); e != nil {
		t.Fatal(e)
	}
	if _, _, e = store.ReadImportedImageMessageContent(ctx, a, direct, mid, "full", func(_ connection.ImageDerivativeMetadata, b []byte) ([]byte, error) {
		t.Fatal("blocked private pixels reached encoder")
		return b, nil
	}); e == nil {
		t.Fatal("Block did not retire historical image")
	}
	if e = store.ValidateChatPageResponse(ctx, a, []string{direct}, history); e == nil {
		t.Fatal("cached history survived current Block")
	}
	// One original pending is decided only now by a current recipient; the
	// successful old import itself still has no invented decision receipt.
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, third); e != nil {
		t.Fatal(e)
	}
	td := sha256.Sum256([]byte("social163 recipient session"))
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, third, td[:]); e != nil {
		t.Fatal(e)
	}
	pending := legacysocial.TargetID("friendship", "14600000-0000-4000-8000-000000000010:14600000-0000-4000-8000-000000000012")
	ta := ea.Access{Actor: identity.Actor{ID: third, AccountType: "person"}, SessionDigest: td}
	op := "16300000-0000-4000-8000-000000000099"
	// Source preservation did not publish either profile. A new accept still
	// honors the normal current request policy; the fixture explicitly changes
	// only this synthetic recipient's current profile before the human action.
	if _, e = store.DecideRequestOperation(f.Context, ta, pending, "accept", op); !errors.Is(e, connection.ErrForbidden) {
		t.Fatal("private current policy was bypassed", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, third); e != nil {
		t.Fatal(e)
	}
	receipt, e := store.DecideRequestOperation(f.Context, ta, pending, "accept", op)
	if e != nil || receipt.Status != "COMMITTED" {
		t.Fatal("current explicit legacy pending acceptance", e)
	}
	retry, e := store.ReadRequestDecisionOperation(f.Context, ta, pending, op)
	if e != nil || retry != receipt {
		t.Fatal("original current decision receipt changed", e)
	}
}
