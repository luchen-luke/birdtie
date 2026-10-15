package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacyhistory"
	"github.com/jackc/pgx/v5"
)

type LegacySocialHistoryResult struct {
	RunID  string               `json:"runId"`
	Status string               `json:"status"`
	Counts legacyhistory.Counts `json:"counts"`
}

func PrepareLegacySocialHistory(run string, os, om, ss, sm, hs, hm []byte) (LegacySocialHistoryResult, error) {
	p, e := legacyhistory.Build(run, os, om, ss, sm, hs, hm)
	if e != nil {
		return LegacySocialHistoryResult{}, ErrLegacyMigrationInvalid
	}
	return LegacySocialHistoryResult{run, "PREPARED_NOT_IMPORTED", p.Counts}, nil
}
func legacyHistoryRead(ctx context.Context, tx pgx.Tx, run string) (LegacySocialHistoryResult, string, error) {
	v := LegacySocialHistoryResult{RunID: run, Status: "COMMITTED"}
	var raw []byte
	var sha string
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts FROM legacy_social_history_runs WHERE run_id=$1`, run).Scan(&sha, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(raw, &v.Counts) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	var threads, messages, notifications, prefs int
	e = tx.QueryRow(ctx, `SELECT count(*)FILTER(WHERE kind='thread'),count(*)FILTER(WHERE kind='message'),count(*)FILTER(WHERE kind='notification'),count(*)FILTER(WHERE kind='preferences')FROM legacy_social_history_facts WHERE run_id=$1`, run).Scan(&threads, &messages, &notifications, &prefs)
	if e != nil || threads != v.Counts.Threads || messages != v.Counts.Messages || notifications != v.Counts.Notifications || prefs != v.Counts.Preferences {
		return v, "", ErrLegacyMigrationConflict
	}
	return v, sha, nil
}
func (s *LegacyMigrationImporter) InspectLegacySocialHistory(ctx context.Context, run string) (LegacySocialHistoryResult, error) {
	v := LegacySocialHistoryResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return v, ErrLegacyMigrationGuard
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return v, e
	}
	out, _, e := legacyHistoryRead(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if tx.Commit(ctx) != nil {
		return v, ErrLegacyMigrationUnknown
	}
	return out, nil
}
func (s *LegacyMigrationImporter) ApplyLegacySocialHistory(ctx context.Context, run string, os, om, ss, sm, hs, hm []byte) (LegacySocialHistoryResult, error) {
	unknown := LegacySocialHistoryResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacyhistory.Build(run, os, om, ss, sm, hs, hm)
	if e != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,165))`, run); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, p.Original) {
		return unknown, ErrLegacyMigrationConflict
	}
	var social bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_social_import_runs WHERE run_id=$1 AND source_file_sha256=$2 AND manifest_file_sha256=$3 AND snapshot_sha256=$4)`, run, p.SocialSourceFileSHA256, p.SocialManifestFileSHA256, p.SocialSnapshotSHA256).Scan(&social)
	if e != nil || !social {
		return unknown, ErrLegacyMigrationConflict
	}
	if prior, sha, e := legacyHistoryRead(ctx, tx, run); e == nil {
		if sha != p.ProjectionSHA256 || prior.Counts != p.Counts {
			return unknown, ErrLegacyMigrationConflict
		}
		if tx.Commit(ctx) != nil {
			return unknown, ErrLegacyMigrationUnknown
		}
		return prior, nil
	} else if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return unknown, e
	}
	owners := map[string]string{}
	for _, o := range p.Original.Owners {
		var id string
		e = tx.QueryRow(ctx, `SELECT target_id::text FROM legacy_entity_mappings WHERE run_id=$1 AND entity_kind='user' AND source_id=$2 AND source_sha256=$3 AND target_id=owner_account_id`, run, o.SourceID, o.SourceSHA256).Scan(&id)
		if e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		owners[o.SourceID] = id
	}
	record := func(kind string, f legacyhistory.Fact, owner string) error {
		_, e := tx.Exec(ctx, `INSERT INTO legacy_social_history_facts(run_id,kind,source_key,source_sha256,target_id,owner_account_id)VALUES($1,$2,$3,$4,$5,$6)`, run, kind, f.SourceKey, f.SourceSHA256, f.TargetID, owner)
		return e
	}
	threads := map[string]legacyhistory.Thread{}
	for _, t := range p.Threads {
		owner := owners[t.OwnerSourceID]
		var journey *string
		if t.JourneySourceID != "" {
			var id string
			e = tx.QueryRow(ctx, `SELECT a.target_id::text FROM legacy_journey_adaptations a JOIN journeys j ON j.id=a.target_id AND j.owner_account_id=$3 AND j.status<>'withdrawn' WHERE a.run_id=$1 AND a.source_key=$2 AND a.owner_account_id=$3`, run, t.JourneySourceID, owner).Scan(&id)
			if e == nil {
				journey = &id
			} else if !errors.Is(e, pgx.ErrNoRows) {
				return unknown, ErrLegacyMigrationUnavailable
			}
		}
		if _, e = tx.Exec(ctx, `INSERT INTO personal_journey_history_threads(id,owner_account_id,journey_id,source_journey_id,source_draft_context_id,summary,source_status,source_context_revision,source_created_at,source_updated_at)VALUES($1,$2,$3,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10)`, t.TargetID, owner, journey, t.JourneySourceID, t.DraftContextID, t.Summary, t.SourceStatus, t.ContextRevision, t.CreatedAt, t.UpdatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if e = record("thread", t.Fact, owner); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		threads[t.SourceKey] = t
	}
	for _, m := range p.Messages {
		t := threads[m.ThreadSourceID]
		if _, e = tx.Exec(ctx, `INSERT INTO personal_journey_history_messages(id,history_thread_id,role,body,source_request_id,source_created_at)VALUES($1,$2,$3,$4,$5,$6)`, m.TargetID, t.TargetID, m.Role, m.Body, m.RequestID, m.CreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if e = record("message", m.Fact, owners[t.OwnerSourceID]); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, n := range p.Notifications {
		owner := owners[n.OwnerSourceID]
		var actor *string
		if n.ActorSourceID != "" {
			v := owners[n.ActorSourceID]
			actor = &v
		}
		var target *string
		kind := ""
		if n.TargetSourceID != "" {
			var id string
			switch n.TargetKind {
			case "note", "moment":
				kind = "moment"
				e = tx.QueryRow(ctx, `SELECT target_id FROM legacy_native_adaptation_entities WHERE run_id=$1 AND kind='moment' AND source_key=$2`, run, n.TargetSourceID).Scan(&id)
			case "journey":
				kind = "journey"
				e = tx.QueryRow(ctx, `SELECT target_id::text FROM legacy_journey_adaptations WHERE run_id=$1 AND source_key=$2`, run, n.TargetSourceID).Scan(&id)
			case "user":
				kind = "person"
				id = owners[n.TargetSourceID]
				e = nil
			default:
				e = pgx.ErrNoRows
			}
			if e == nil && id != "" {
				target = &id
			} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return unknown, ErrLegacyMigrationUnavailable
			}
		}
		if _, e = tx.Exec(ctx, `INSERT INTO historical_notification_facts(id,owner_account_id,actor_account_id,original_type,event_type,source_category,source_target_kind,source_target_id,native_target_kind,native_target_id,source_anchor_kind,source_anchor_id,original_action,source_created_at,source_read_at,source_seen_at,source_deleted_at,payload_schema_version,source_inbox_version,residual_keys)VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,NULLIF($9,''),$10,$11,NULLIF($12,'')::uuid,$13,$14,$15,$16,$17,$18,$19,$20)`, n.TargetID, owner, actor, n.OriginalType, n.EventType, n.Category, n.TargetKind, n.TargetSourceID, kind, target, n.AnchorType, n.AnchorID, n.Action, n.CreatedAt, n.ReadAt, n.SeenAt, n.DeletedAt, n.PayloadSchemaVersion, n.InboxVersion, n.ResidualKeys); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if e = record("notification", n.Fact, owner); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if n.DeletedAt == nil {
			if _, e = tx.Exec(ctx, `INSERT INTO inbox_items(id,recipient_account_id,category,title,detail,resource_type,resource_id,created_at,read_at,origin)VALUES($1,$2,'updates','原通知记录','原事件仅作历史保留，关联内容以当前权限为准。','legacy_history',$1,$3,$4,'LEGACY_HISTORY')`, n.TargetID, owner, n.CreatedAt, n.ReadAt); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
		}
	}
	for _, v := range p.Preferences {
		if _, e = tx.Exec(ctx, `INSERT INTO personal_notification_preferences(id,owner_account_id,chat_push,request_push,comment_push,follow_push,praise_save_push,journey_push,system_push,preview_enabled,collaboration_push,time_zone,quiet_start,quiet_end,source_version,source_updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, v.TargetID, owners[v.OwnerSourceID], v.Settings["chatPush"], v.Settings["requestPush"], v.Settings["commentPush"], v.Settings["followPush"], v.Settings["praiseSavePush"], v.Settings["journeyPush"], v.Settings["systemPush"], v.Settings["previewEnabled"], v.CollaborationPush, v.TimeZone, v.QuietStart, v.QuietEnd, v.Version, v.UpdatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if e = record("preferences", v.Fact, owners[v.OwnerSourceID]); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	counts, _ := json.Marshal(p.Counts)
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_social_history_runs(run_id,protocol,source_file_sha256,manifest_file_sha256,snapshot_sha256,social_source_file_sha256,social_manifest_file_sha256,social_snapshot_sha256,projection_sha256,counts)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, run, legacyhistory.Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.SocialSourceFileSHA256, p.SocialManifestFileSHA256, p.SocialSnapshotSHA256, p.ProjectionSHA256, counts); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return LegacySocialHistoryResult{run, "COMMITTED", p.Counts}, nil
}
