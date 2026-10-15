package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacysocial"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

// Image bytes are accepted only with the independently operator-pinned seal of
// the authenticated exact chat-storage-key extraction. This is not a user API.
type LegacySocialImageInput struct {
	Matcher, Seal, Content []byte
	ExpectedSealSHA256     string
}
type LegacySocialResult struct {
	RunID       string              `json:"runId"`
	Status      string              `json:"status"`
	Counts      legacysocial.Counts `json:"counts"`
	CreatedAt   time.Time           `json:"createdAt,omitempty"`
	ImageDecode string              `json:"imageDecode"`
	Scanner     string              `json:"scanner"`
	PrivacyRisk string              `json:"privacyRisk"`
}
type legacySocialPrepared struct {
	plan                 legacysocial.Plan
	image                *media.ChatImageDerivatives
	inputSHA, projection string
}

func legacySocialImage(ctx context.Context, p legacysocial.Plan, in LegacySocialImageInput) (*media.ChatImageDerivatives, string, error) {
	if len(p.Images) == 0 {
		if len(in.Matcher)+len(in.Seal)+len(in.Content) != 0 || in.ExpectedSealSHA256 != "" {
			return nil, "", ErrLegacyMigrationInvalid
		}
		return nil, "", nil
	}
	if len(p.Images) != 1 || len(in.Matcher) > 65536 || len(in.Seal) > 16384 || len(in.Content) > connection.MaxChatImageInputBytes || len(in.ExpectedSealSHA256) != 64 || legacysocial.SHA(in.Seal) != in.ExpectedSealSHA256 {
		return nil, "", ErrLegacyMigrationInvalid
	}
	var seal struct {
		Format        string `json:"format"`
		Authenticated bool   `json:"aesGcmAuthenticated"`
		PlainSHA      string `json:"plaintextSHA256"`
		MatcherSHA    string `json:"matcherFileSHA256"`
		SourceSHA     string `json:"socialSourceFileSha256"`
		ManifestSHA   string `json:"socialManifestFileSha256"`
		Files         int    `json:"extractedFiles"`
		Bytes         int    `json:"imageBytes"`
		Exit          int    `json:"readerExit"`
		SourceWrites  int    `json:"sourceWrites"`
		NativeWrites  int    `json:"nativeWrites"`
	}
	var match struct {
		Schema      string `json:"schemaVersion"`
		SourceSHA   string `json:"socialSourceFileSha256"`
		ManifestSHA string `json:"socialManifestFileSha256"`
		PlainSHA    string `json:"archivePlaintextSHA256"`
		Files       int    `json:"extractedFiles"`
		Selector    struct {
			MessageID      string `json:"sourceMessageId"`
			ConversationID string `json:"sourceConversationId"`
			MediaID        string `json:"sourceMediaId"`
			OwnerID        string `json:"sourceOwnerId"`
			StorageKey     string `json:"sourceStorageKey"`
			MessageSHA     string `json:"sourceMessageSha256"`
			AttachmentSHA  string `json:"sourceAttachmentSha256"`
			MediaSHA       string `json:"sourceMediaSha256"`
			Member         string `json:"archiveMember"`
			Policy         string `json:"storageKeyPolicy"`
			SHA            string `json:"sha256"`
			Bytes          int64  `json:"bytes"`
			SourceBytes    int64  `json:"sourceBytes"`
			Width          int64  `json:"sourceWidth"`
			Height         int64  `json:"sourceHeight"`
			MIME           string `json:"observedMIME"`
			Matched        bool   `json:"binaryMatched"`
		} `json:"selector"`
	}
	if json.Unmarshal(in.Seal, &seal) != nil || json.Unmarshal(in.Matcher, &match) != nil {
		return nil, "", ErrLegacyMigrationInvalid
	}
	image := p.Images[0]
	var message legacysocial.Message
	for _, m := range p.Messages {
		if m.SourceKey == image.MessageID {
			message = m
		}
	}
	x := match.Selector
	if seal.Format != "birdtie-civu-private-chat-image-authenticated-match-v1" || !seal.Authenticated || seal.MatcherSHA != legacysocial.SHA(in.Matcher) || seal.SourceSHA != p.SourceFileSHA256 || seal.ManifestSHA != p.ManifestFileSHA256 || seal.Files != 1 || seal.Bytes != len(in.Content) || seal.Exit != 0 || seal.SourceWrites != 0 || seal.NativeWrites != 0 ||
		match.Schema != "birdtie-civu-private-chat-image-match-v1" || match.SourceSHA != p.SourceFileSHA256 || match.ManifestSHA != p.ManifestFileSHA256 || match.PlainSHA != seal.PlainSHA || match.Files != 1 ||
		x.MessageID != image.MessageID || x.ConversationID != image.ConversationID || x.MediaID != image.SourceKey || x.OwnerID != image.OwnerID || x.StorageKey != image.StorageKey || x.MessageSHA != message.SourceSHA256 || x.AttachmentSHA != image.AttachmentSHA256 || x.MediaSHA != image.SourceSHA256 ||
		x.Policy != "CIVU_PRIVATE_MESSAGING_FULL_JPEG_V1" || x.Member != "media/private-messaging/"+image.StorageKey+".jpg" || !x.Matched || x.MIME != "image/jpeg" || x.Bytes != int64(len(in.Content)) || x.SourceBytes != image.ByteSize || x.Bytes != image.ByteSize || x.Width != image.Width || x.Height != image.Height || x.SHA != legacysocial.SHA(in.Content) {
		return nil, "", ErrLegacyMigrationInvalid
	}
	derivative, e := (&media.LocalChatImageDerivativeBuilder{}).Build(ctx, in.Content, "image/jpeg")
	if e != nil || int64(derivative.Metadata.Width) != image.Width || int64(derivative.Metadata.Height) != image.Height {
		return nil, "", ErrLegacyMigrationInvalid
	}
	return &derivative, x.SHA, nil
}
func prepareLegacySocial(ctx context.Context, run string, originalSource, originalManifest, source, manifest []byte, image LegacySocialImageInput) (legacySocialPrepared, error) {
	p, e := legacysocial.Build(run, originalSource, originalManifest, source, manifest)
	if e != nil {
		return legacySocialPrepared{}, ErrLegacyMigrationInvalid
	}
	d, input, e := legacySocialImage(ctx, p, image)
	if e != nil {
		return legacySocialPrepared{}, e
	}
	var meta any
	if d != nil {
		meta = d.Metadata
	}
	raw, e := json.Marshal([]any{p.ProjectionSHA256, input, meta})
	if e != nil {
		return legacySocialPrepared{}, ErrLegacyMigrationInvalid
	}
	return legacySocialPrepared{plan: p, image: d, inputSHA: input, projection: legacysocial.SHA(raw)}, nil
}
func socialResult(p legacysocial.Plan, status string) LegacySocialResult {
	result := LegacySocialResult{RunID: p.RunID, Status: status, Counts: p.Counts, ImageDecode: "NOT_APPLICABLE", Scanner: "NOT_RUN", PrivacyRisk: "UNKNOWN"}
	if len(p.Images) > 0 {
		result.ImageDecode = "FULL_DECODE_REENCODED"
	}
	return result
}
func PrepareLegacySocial(ctx context.Context, run string, originalSource, originalManifest, source, manifest []byte, image LegacySocialImageInput) (LegacySocialResult, error) {
	p, e := prepareLegacySocial(ctx, run, originalSource, originalManifest, source, manifest, image)
	if e != nil {
		return LegacySocialResult{}, e
	}
	return socialResult(p.plan, "VALIDATED_NOT_IMPORTED"), nil
}
func legacySocialReadRun(ctx context.Context, tx pgx.Tx, run string) (LegacySocialResult, string, error) {
	v := LegacySocialResult{RunID: run, Status: "SOCIAL_NATIVE_COMMITTED", ImageDecode: "NOT_APPLICABLE", Scanner: "NOT_RUN", PrivacyRisk: "UNKNOWN"}
	var raw []byte
	var projection string
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,created_at FROM legacy_social_import_runs WHERE run_id=$1`, run).Scan(&projection, &raw, &v.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(raw, &v.Counts) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	if v.Counts.Images > 0 {
		v.ImageDecode = "FULL_DECODE_REENCODED"
	}
	return v, projection, nil
}
func (s *LegacyMigrationImporter) InspectLegacySocial(ctx context.Context, run string) (LegacySocialResult, error) {
	if s == nil || s.database == nil || !legacymigration.ValidRunID(run) {
		return LegacySocialResult{}, ErrLegacyMigrationGuard
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return LegacySocialResult{}, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return LegacySocialResult{}, e
	}
	v, _, e := legacySocialReadRun(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if e = tx.Commit(ctx); e != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	return v, nil
}

func (s *LegacyMigrationImporter) ApplyLegacySocial(ctx context.Context, run string, originalSource, originalManifest, source, manifest []byte, image LegacySocialImageInput) (LegacySocialResult, error) {
	unknown := LegacySocialResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	prepared, e := prepareLegacySocial(ctx, run, originalSource, originalManifest, source, manifest, image)
	if e != nil {
		return unknown, e
	}
	p := prepared.plan
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,163))`, legacymigration.SourceSystem); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, p.Original) {
		return unknown, ErrLegacyMigrationConflict
	}
	previous, digest, e := legacySocialReadRun(ctx, tx, run)
	if e == nil {
		if digest != prepared.projection || previous.Counts != p.Counts {
			return unknown, ErrLegacyMigrationConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return unknown, ErrLegacyMigrationUnknown
		}
		return previous, nil
	}
	if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return unknown, e
	}
	counts, _ := json.Marshal(p.Counts)
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_social_import_runs(run_id,protocol,source_file_sha256,manifest_file_sha256,snapshot_sha256,projection_sha256,counts)VALUES($1,$2,$3,$4,$5,$6,$7)`, run, legacysocial.Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, prepared.projection, counts); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	owners := map[string]string{}
	for _, o := range p.Original.Owners {
		var target string
		if e = tx.QueryRow(ctx, `SELECT m.target_id FROM legacy_entity_mappings m JOIN accounts a ON a.id=m.target_id WHERE m.run_id=$1 AND m.source_system=$2 AND m.entity_kind='user' AND m.source_id=$3 AND m.source_sha256=$4 AND a.account_type='person' AND a.status IN('active','suspended') FOR SHARE OF a`, run, legacymigration.SourceSystem, o.SourceID, o.SourceSHA256).Scan(&target); e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		owners[o.SourceID] = target
	}
	factID := func(kind, key string) string { return legacysocial.TargetID("fact:"+kind, key) }
	insertFact := func(kind string, f legacysocial.Fact, target, parent, a, b, state string, created time.Time) error {
		_, err := tx.Exec(ctx, `INSERT INTO legacy_social_import_entities(id,run_id,kind,source_key,source_sha256,target_id,parent_id,actor_account_id,other_account_id,source_state,source_created_at)VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10,$11)`, factID(kind, f.SourceKey), run, kind, f.SourceKey, f.SourceSHA256, target, parent, a, b, state, created)
		return err
	}
	for _, v := range p.Friendships {
		a, b := owners[v.RequesterID], owners[v.LowID]
		if v.RequesterID == v.LowID {
			b = owners[v.HighID]
		}
		if e = insertFact("friendship", v.Fact, v.TargetID, "", a, b, v.State, v.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,note,scope,state,expires_at,created_at,decided_at,legacy_source_id)VALUES($1,$2,$3,'','friend',$4,NULL,$5,$6,$7)`, v.TargetID, a, b, v.State, v.CreatedAt, v.RespondedAt, factID("friendship", v.SourceKey)); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if v.State == "accepted" {
			if a > b {
				a, b = b, a
			}
			if _, e = tx.Exec(ctx, `INSERT INTO person_ties(id,person_a_account_id,person_b_account_id,request_id,status,created_at,updated_at)VALUES($1,$2,$3,$4,'active',$5,$6)`, legacysocial.TargetID("tie", v.SourceKey), a, b, v.TargetID, v.RespondedAt, v.UpdatedAt); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
		}
	}
	for _, v := range p.Follows {
		a, b := owners[v.FollowerID], owners[v.FolloweeID]
		if e = insertFact("follow", v.Fact, v.TargetID, "", a, b, "", v.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO follows(id,follower_account_id,person_account_id,created_at,legacy_source_id)VALUES($1,$2,$3,$4,$5)`, v.TargetID, a, b, v.CreatedAt, factID("follow", v.SourceKey)); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	// Preserve the one explicit source message request even when a friendship
	// already supplies this pair's consent. It is not a second accepted event.
	for _, v := range p.Requests {
		a, b := owners[v.RequesterID], owners[v.RecipientID]
		if e = insertFact("message_request", v.Fact, v.TargetID, legacysocial.TargetID("conversation", v.ConversationID), a, b, v.State, v.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,note,scope,state,expires_at,created_at,decided_at,legacy_source_id)VALUES($1,$2,$3,'','conversation',$4,NULL,$5,$6,$7)`, v.TargetID, a, b, v.State, v.CreatedAt, v.ResolvedAt, factID("message_request", v.SourceKey)); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.Conversations {
		a, b := owners[v.LowID], owners[v.HighID]
		if e = insertFact("conversation", v.Fact, v.TargetID, "", a, b, v.State, v.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO conversations(id,request_id,member_a_account_id,member_b_account_id,created_at,legacy_source_id,kind,legacy_status,legacy_title,legacy_join_policy,legacy_history_policy,legacy_consent_epoch,legacy_last_sequence,legacy_updated_at,legacy_archived_at,legacy_owner_id,legacy_journey_id,legacy_school_label,legacy_member_limit,legacy_show_on_profile)VALUES($1,NULLIF($2,'')::uuid,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,NULLIF($16,'')::uuid,NULLIF($17,'')::uuid,$18,$19,$20)`, v.TargetID, v.AcceptanceSource, a, b, v.CreatedAt, factID("conversation", v.SourceKey), v.Kind, v.State, v.Title, v.JoinPolicy, v.HistoryForNewMembers, v.ConsentEpoch, v.LastSequence, v.UpdatedAt, v.ArchivedAt, owners[v.OwnerID], v.JourneyID, v.SchoolLabel, v.MemberLimit, v.ShowOnProfile); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.Members {
		cid, owner := legacysocial.TargetID("conversation", v.ConversationID), owners[v.OwnerID]
		if e = insertFact("member", v.Fact, owner, cid, owner, "", "", v.JoinedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO conversation_member_states(conversation_id,member_account_id,baseline_at,created_at,legacy_source_id,legacy_joined_at,legacy_updated_at,legacy_removed_at,legacy_read_sequence,legacy_delivered_sequence,legacy_clear_sequence,legacy_unread_snapshot,legacy_muted_until,legacy_hidden,legacy_pinned,legacy_marked_unread,legacy_role)VALUES($1,$2,$3,$3,$4,$3,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, cid, owner, v.JoinedAt, factID("member", v.SourceKey), v.UpdatedAt, v.RemovedAt, v.ReadSequence, v.DeliveredSequence, v.ClearedSequence, v.UnreadCount, v.MutedUntil, v.Hidden, v.Pinned, v.MarkedUnread, v.Role); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, v := range p.Messages {
		cid, sender := legacysocial.TargetID("conversation", v.ConversationID), owners[v.SenderID]
		if e = insertFact("message", v.Fact, v.TargetID, cid, sender, "", v.Kind, v.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		// Source references resolve only to existing imported native entities. A
		// missing/deleted entity remains a typed unavailable card, never a title.
		entity := v.EntityTargetID
		if entity != "" {
			var exists bool
			table := "moments"
			if v.EntityKind == "place" {
				table = "places"
			}
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1)`, entity).Scan(&exists); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if !exists {
				entity = ""
			}
		}
		speaker := "human"
		if v.Kind == "SYSTEM" {
			speaker = "system"
		}
		if _, e = tx.Exec(ctx, `INSERT INTO conversation_messages(id,conversation_id,sender_account_id,speaker_kind,body,created_at,entity_type,entity_id,legacy_source_id,message_kind,legacy_sequence,legacy_entity_kind,legacy_context_kind,legacy_context_id,legacy_request_source_id,legacy_client_message_id,legacy_payload_hash,legacy_sticker_pack_id,legacy_sticker_id,legacy_recalled_at,legacy_moderated_at)VALUES($1,$2,$3,$4,$5,$6,CASE WHEN $7='' THEN NULL ELSE $8 END,NULLIF($7,'')::uuid,$9,$10,$11,NULLIF($8,''),NULLIF($12,''),NULLIF($13,'')::uuid,NULLIF($14,'')::uuid,$15,$16,NULLIF($17,''),NULLIF($18,''),$19,$20)`, v.TargetID, cid, sender, speaker, v.Body, v.CreatedAt, entity, v.EntityKind, factID("message", v.SourceKey), v.Kind, v.Sequence, v.ContextKind, v.ContextID, v.RequestID, v.ClientMessageID, v.PayloadHash, v.StickerPackID, v.StickerID, v.RecalledAt, v.ModeratedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	if prepared.image != nil {
		v := p.Images[0]
		d := prepared.image
		m := d.Metadata
		mid, cid, owner := legacysocial.TargetID("message", v.MessageID), legacysocial.TargetID("conversation", v.ConversationID), owners[v.OwnerID]
		var recipient string
		if e = tx.QueryRow(ctx, `SELECT CASE WHEN member_a_account_id=$2 THEN member_b_account_id ELSE member_a_account_id END FROM conversations WHERE id=$1 AND kind='DIRECT'`, cid, owner).Scan(&recipient); e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		if e = insertFact("image", v.Fact, mid, cid, owner, recipient, "ready", v.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		var created time.Time
		if e = tx.QueryRow(ctx, `SELECT created_at FROM conversation_messages WHERE id=$1`, mid).Scan(&created); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		_, e = tx.Exec(ctx, `INSERT INTO human_conversation_images(message_id,owner_account_id,recipient_account_id,conversation_id,operation_id,purpose,pixel_risk,input_mime_type,input_size,input_sha256,caption_present,caption_sha256,mime_type,byte_size,sha256,width,height,content,thumbnail_byte_size,thumbnail_sha256,thumbnail_width,thumbnail_height,thumbnail_content,created_at,legacy_source_id)VALUES($1,$2,$3,$4,NULL,'HUMAN_CONVERSATION_IMAGE_ATTACHMENT','UNKNOWN','image/jpeg',$5,$6,false,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, mid, owner, recipient, cid, v.ByteSize, prepared.inputSHA, legacysocial.SHA(nil), m.MIME, m.ByteSize, m.SHA256, m.Width, m.Height, d.Full, m.Thumbnail.ByteSize, m.Thumbnail.SHA256, m.Thumbnail.Width, m.Thumbnail.Height, d.Thumbnail, created, factID("image", v.SourceKey))
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	result, _, e := legacySocialReadRun(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if e = tx.Commit(ctx); e != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return result, nil
}
