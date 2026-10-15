package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacycontext"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type LegacyContextAdaptationResult struct {
	RunID    string               `json:"runId"`
	Status   string               `json:"status"`
	Counts   legacycontext.Counts `json:"counts"`
	Deferred map[string]int       `json:"deferred"`
}

func PrepareLegacyContextAdaptation(run string, source, manifest []byte) (LegacyContextAdaptationResult, error) {
	p, e := legacycontext.Build(run, source, manifest)
	if e != nil {
		return LegacyContextAdaptationResult{}, ErrLegacyMigrationInvalid
	}
	return LegacyContextAdaptationResult{run, "PREPARED_NOT_IMPORTED", p.Counts, p.Deferred}, nil
}
func contextAdaptationRead(ctx context.Context, tx pgx.Tx, run string) (LegacyContextAdaptationResult, string, error) {
	v := LegacyContextAdaptationResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var counts, deferred []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,deferred FROM legacy_native_context_runs WHERE run_id=$1`, run).Scan(&digest, &counts, &deferred)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(counts, &v.Counts) != nil || json.Unmarshal(deferred, &v.Deferred) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	var actual legacycontext.Counts
	e = tx.QueryRow(ctx, `SELECT count(*)FILTER(WHERE kind='unplaced_place'),count(*)FILTER(WHERE kind='saved_place'),count(*)FILTER(WHERE kind='note_location'),count(*)FILTER(WHERE kind='media_context'),count(*)FILTER(WHERE kind='media_place'),count(*)FILTER(WHERE kind='thread'),count(*)FILTER(WHERE kind='thread_member'),count(*)FILTER(WHERE kind='thread_leg')FROM legacy_native_context_entities WHERE run_id=$1`, run).Scan(&actual.Places, &actual.SavedItems, &actual.NoteLocations, &actual.MediaContexts, &actual.MediaPlaces, &actual.Threads, &actual.Members, &actual.Legs)
	actual.ThreadTombstones = v.Counts.ThreadTombstones
	if e != nil || actual != v.Counts {
		return v, "", ErrLegacyMigrationConflict
	}
	return v, digest, nil
}
func (s *LegacyMigrationImporter) ApplyContextAdaptation(ctx context.Context, run string, source, manifest []byte) (LegacyContextAdaptationResult, error) {
	unknown := LegacyContextAdaptationResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacycontext.Build(run, source, manifest)
	if e != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	base, e := legacymigration.BuildPlan(run, source, manifest)
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,162))`, legacymigration.SourceSystem); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	old, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(old, base) {
		return unknown, ErrLegacyMigrationConflict
	}
	if _, _, e = nativeAdaptationRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	if _, _, e = s.personalArchiveRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	if previous, digest, e := contextAdaptationRead(ctx, tx, run); e == nil {
		if digest != p.ProjectionSHA256 {
			return unknown, ErrLegacyMigrationConflict
		}
		if tx.Commit(ctx) != nil {
			return unknown, ErrLegacyMigrationUnknown
		}
		return previous, nil
	} else if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return unknown, e
	}
	result := LegacyContextAdaptationResult{RunID: run, Status: "COMMITTED", Counts: legacycontext.Counts{ThreadTombstones: p.Counts.ThreadTombstones}, Deferred: p.Deferred}
	record := func(kind, key, hash, target, owner, parent, parentHash string) error {
		_, err := tx.Exec(ctx, `INSERT INTO legacy_native_context_entities(run_id,kind,source_key,source_sha256,target_id,owner_account_id,source_parent_key,source_parent_sha256)VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,NULLIF($7,''),NULLIF($8,''))`, run, kind, key, hash, target, owner, parent, parentHash)
		return err
	}
	// Insert the immutable run with deferred counts only after all planned effects.
	// Its FK is deferred within this one transaction, never a mutable import state.
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL DEFERRED`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	owners := map[string]string{}
	for _, o := range base.Owners {
		var target string
		e = tx.QueryRow(ctx, `SELECT u.target_id FROM legacy_entity_mappings u JOIN accounts a ON a.id=u.target_id WHERE u.run_id=$1 AND u.entity_kind='user' AND u.source_system=$2 AND u.source_id=$3 AND u.target_id=u.owner_account_id AND a.account_type='person' AND a.status IN('active','suspended') FOR SHARE OF a`, run, legacymigration.SourceSystem, o.SourceID).Scan(&target)
		if e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		owners[o.SourceID] = target
	}
	for _, v := range p.Places {
		if _, e = tx.Exec(ctx, `INSERT INTO places(id,city_id,name,category_code,summary,location_precision,publication_status,source_label,source_ref,maintainer_label)VALUES($1,NULL,$2,$3,$4,'none','draft',$5,$6,'Birdtie private migration')`, v.TargetID, v.Name, v.CategoryCode, v.Summary, v.SourceLabel, v.SourceRef); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO legacy_native_context_entities(run_id,kind,source_key,source_sha256,target_id,source_region,source_country_code,source_area_kind)VALUES($1,'unplaced_place',$2,$3,$4,$5,$6,$7)`, run, v.SourceID, v.SourceSHA256, v.TargetID, v.SourceRegion, v.SourceCountryCode, v.SourceAreaKind); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		result.Counts.Places++
	}
	for _, v := range p.SavedItems {
		owner := owners[v.OwnerSourceID]
		var valid bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_personal_archives WHERE run_id=$1 AND kind='saved_place' AND source_key=$2 AND source_sha256=$3 AND owner_account_id=$4 AND NOT deleted)`, run, v.SourceKey, v.SourceSHA256, owner).Scan(&valid)
		if e != nil || !valid {
			return unknown, ErrLegacyMigrationConflict
		}
		if _, e = tx.Exec(ctx, `INSERT INTO saved_items(id,owner_account_id,place_id,created_at)VALUES($1,$2,$3,coalesce($4,clock_timestamp()))`, v.TargetID, owner, v.PlaceID, v.SourceCreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if e = record("saved_place", v.SourceKey, v.SourceSHA256, v.TargetID, owner, "", ""); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		result.Counts.SavedItems++
	}
	for _, v := range p.NoteLocations {
		owner := owners[v.OwnerSourceID]
		var valid bool
		e = tx.QueryRow(ctx, `SELECT m.author_account_id=$2 AND m.status='draft' AND m.visibility='private' AND m.revision=1 AND m.title=$3 AND m.body=$4 AND EXISTS(SELECT 1 FROM legacy_native_adaptation_entities x WHERE x.run_id=$5 AND x.kind='moment' AND x.target_id=m.id::text AND x.owner_account_id=$2 AND x.source_sha256=$6) FROM moments m WHERE m.id=$1 FOR UPDATE`, v.TargetID, owner, v.Original.Title, v.Original.Body, run, v.ParentSHA256).Scan(&valid)
		if e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		old, e := readMomentLocationRows(ctx, tx, v.TargetID)
		if e != nil {
			return unknown, e
		}
		var touched bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moment_location_links WHERE moment_id=$1 AND(revision<>1 OR status<>'ACTIVE')) OR coalesce((SELECT version FROM moment_location_epochs WHERE moment_id=$1),0)<>$2`, v.TargetID, len(old)).Scan(&touched)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if !valid || touched || len(old) > 1 {
			result.Deferred["note_location_target_changed"] += len(v.Items)
			continue
		}
		keep := []string{}
		for _, item := range v.Items {
			id := item.ID
			for _, old := range old {
				if old.input.PlaceID == item.PlaceID {
					id = old.input.ID
				}
			}
			keep = append(keep, id)
		}
		if _, e = tx.Exec(ctx, `UPDATE moment_location_links SET status='REMOVED',revision=revision+1,updated_at=clock_timestamp() WHERE moment_id=$1 AND status='ACTIVE' AND NOT(id=ANY($2::uuid[]))`, v.TargetID, keep); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `UPDATE moments SET city_id=NULL,place_id=NULL,location_precision='none' WHERE id=$1`, v.TargetID); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		for i, item := range v.Items {
			if _, e = tx.Exec(ctx, `INSERT INTO moment_location_links(id,moment_id,kind,place_id,precision,position)VALUES($1,$2,'PLACE',$3,'none',$4)ON CONFLICT(id)DO UPDATE SET precision='none',position=EXCLUDED.position,revision=moment_location_links.revision+1 WHERE moment_location_links.moment_id=EXCLUDED.moment_id AND moment_location_links.status='ACTIVE'`, keep[i], v.TargetID, item.PlaceID, i); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if e = record("note_location", item.ID, item.SourceSHA256, keep[i], owner, v.SourceKey, v.SourceSHA256); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			result.Counts.NoteLocations++
		}
	}
	for _, v := range p.MediaContexts {
		owner := owners[v.OwnerSourceID]
		var asset, kind string
		e = tx.QueryRow(ctx, `SELECT asset,kind FROM(SELECT image_id asset,'IMAGE' kind FROM legacy_private_media_import_items WHERE run_id=$1 AND source_note_id=$2 AND source_media_id=$3 AND owner_account_id=$4 AND moment_id=$5 AND source_media_sha256=$6 UNION ALL SELECT image_id,'IMAGE' FROM legacy_private_image_recovery_items WHERE run_id=$1 AND source_note_id=$2 AND source_media_id=$3 AND owner_account_id=$4 AND moment_id=$5 AND source_media_sha256=$6 UNION ALL SELECT video_id,'VIDEO' FROM legacy_private_video_import_items WHERE run_id=$1 AND source_note_id=$2 AND source_media_id=$3 AND owner_account_id=$4 AND moment_id=$5 AND source_media_sha256=$6) matched`, run, v.ParentKey, v.SourceMediaID, owner, v.MomentID, v.SourceMediaSHA256).Scan(&asset, &kind)
		if errors.Is(e, pgx.ErrNoRows) {
			result.Deferred["media_context_asset_unmapped"]++
			continue
		}
		if e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		var live bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM moments m JOIN media_assets a ON a.id=$2 AND a.owner_account_id=m.author_account_id WHERE m.id=$1 AND m.author_account_id=$3 AND m.status<>'withdrawn' AND a.state='ready_private' AND(( $4='IMAGE' AND EXISTS(SELECT 1 FROM private_moment_images i WHERE i.id=a.id AND i.moment_id=m.id AND i.status='ready_private' AND i.derivative IS NOT NULL))OR($4='VIDEO' AND EXISTS(SELECT 1 FROM private_moment_videos v WHERE v.id=a.id AND v.moment_id=m.id AND v.status='ready_private' AND v.content IS NOT NULL))))`, v.MomentID, asset, owner, kind).Scan(&live)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if !live {
			result.Deferred["media_context_asset_deleted"]++
			continue
		}
		image, video := "", ""
		if kind == "IMAGE" {
			image = asset
		} else {
			video = asset
		}
		var lat, lon any
		if v.Latitude != nil {
			lat = v.Latitude.String()
		}
		if v.Longitude != nil {
			lon = v.Longitude.String()
		}
		if _, e = tx.Exec(ctx, `INSERT INTO private_moment_media_contexts(id,moment_id,owner_account_id,asset_kind,image_id,video_id,source_position,captured_date,original_map_enabled,source_latitude,source_longitude)VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($6,'')::uuid,$7,$8,$9,$10::numeric,$11::numeric)`, v.TargetID, v.MomentID, owner, kind, image, video, v.SourcePosition, v.CapturedDate, v.OriginalMapEnabled, lat, lon); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if e = record("media_context", v.SourceKey, v.SourceSHA256, v.TargetID, owner, v.ParentKey, v.ParentSHA256); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		result.Counts.MediaContexts++
		for i, item := range v.Places {
			if _, e = tx.Exec(ctx, `INSERT INTO private_moment_media_context_places(id,context_id,position,place_id)VALUES($1,$2,$3,$4)`, item.ID, v.TargetID, i, item.PlaceID); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if e = record("media_place", item.ID, item.SourceSHA256, item.ID, owner, v.ParentKey, v.ParentSHA256); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			result.Counts.MediaPlaces++
		}
	}
	for _, v := range p.Threads {
		owner := owners[v.OwnerSourceID]
		for _, m := range v.Members {
			var valid bool
			e = tx.QueryRow(ctx, `SELECT author_account_id=$2 AND status='draft' AND visibility='private' AND EXISTS(SELECT 1 FROM legacy_native_adaptation_entities x WHERE x.run_id=$3 AND x.kind='moment' AND x.target_id=moments.id::text AND x.owner_account_id=$2 AND x.source_key=$4 AND x.source_sha256=$5)FROM moments WHERE id=$1 FOR UPDATE`, m.MomentID, owner, run, m.SourceKey, m.SourceSHA256).Scan(&valid)
			if e != nil || !valid {
				return unknown, ErrLegacyMigrationConflict
			}
		}
		if e = record("thread", v.SourceKey, v.SourceSHA256, v.TargetID, owner, "", ""); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO moment_threads(id,author_account_id,root_moment_id,audience,status,created_at,updated_at)VALUES($1,$2,$3,'PRIVATE','draft',coalesce($4,clock_timestamp()),coalesce($4,clock_timestamp()))`, v.TargetID, owner, v.Members[0].MomentID, v.SourceCreatedAt); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		result.Counts.Threads++
		for _, m := range v.Members {
			if _, e = tx.Exec(ctx, `INSERT INTO moment_thread_members(thread_id,moment_id,position)VALUES($1,$2,$3)`, v.TargetID, m.MomentID, m.Position); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if _, e = tx.Exec(ctx, `INSERT INTO moment_thread_entry_contexts(thread_id,moment_id,position,inbound_origin,inbound_mode,duration_minutes,estimated_duration_minutes)VALUES($1,$2,$3,$4,$5,$6,$7)`, v.TargetID, m.MomentID, m.Position, m.InboundOrigin, m.InboundMode, m.DurationMinutes, m.EstimatedDurationMinutes); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if e = record("thread_member", m.SourceKey, m.SourceSHA256, m.MomentID, owner, v.SourceKey, v.SourceSHA256); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			result.Counts.Members++
		}
		for _, l := range v.Legs {
			if _, e = tx.Exec(ctx, `INSERT INTO moment_thread_route_legs(id,thread_id,from_moment_id,to_moment_id,from_position,to_position,mode,from_place_id,to_place_id,source_created_at,source_updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10,$11)`, l.TargetID, v.TargetID, l.FromMomentID, l.ToMomentID, l.FromPosition, l.ToPosition, l.Mode, l.FromPlaceID, l.ToPlaceID, l.SourceCreatedAt, l.SourceUpdatedAt); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if e = record("thread_leg", l.SourceKey, l.SourceSHA256, l.TargetID, owner, v.SourceKey, v.SourceSHA256); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			result.Counts.Legs++
		}
	}
	counts, _ := json.Marshal(result.Counts)
	deferred, _ := json.Marshal(result.Deferred)
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_native_context_runs(run_id,protocol,projection_sha256,source_file_sha256,manifest_file_sha256,counts,deferred)VALUES($1,$2,$3,$4,$5,$6,$7)`, run, legacycontext.Protocol, p.ProjectionSHA256, p.SourceFileSHA256, p.ManifestFileSHA256, counts, deferred); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	verified, digest, e := contextAdaptationRead(ctx, tx, run)
	if e != nil || digest != p.ProjectionSHA256 {
		return unknown, ErrLegacyMigrationConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return verified, nil
}
func (s *LegacyMigrationImporter) InspectContextAdaptation(ctx context.Context, run string) (LegacyContextAdaptationResult, error) {
	v := LegacyContextAdaptationResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || !legacymigration.ValidRunID(run) {
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
	v, _, e = contextAdaptationRead(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if tx.Commit(ctx) != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	return v, nil
}
