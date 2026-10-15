package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacymapanchor"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type LegacyMapAnchorRestoreResult struct {
	RunID    string                 `json:"runId"`
	Status   string                 `json:"status"`
	Counts   legacymapanchor.Counts `json:"counts"`
	Deferred map[string]int         `json:"deferred"`
}

func PrepareLegacyMapAnchorRestore(run string, source, manifest []byte) (LegacyMapAnchorRestoreResult, error) {
	p, e := legacymapanchor.Build(run, source, manifest)
	if e != nil {
		return LegacyMapAnchorRestoreResult{}, ErrLegacyMigrationInvalid
	}
	return LegacyMapAnchorRestoreResult{run, "PREPARED_NOT_IMPORTED", p.Counts, p.Deferred}, nil
}
func readLegacyMapAnchorRestore(ctx context.Context, tx pgx.Tx, run string) (LegacyMapAnchorRestoreResult, string, error) {
	v := LegacyMapAnchorRestoreResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var c, d []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,deferred FROM legacy_map_anchor_runs WHERE run_id=$1`, run).Scan(&digest, &c, &d)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(c, &v.Counts) != nil || json.Unmarshal(d, &v.Deferred) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	var actual legacymapanchor.Counts
	e = tx.QueryRow(ctx, `SELECT count(*),count(*)FILTER(WHERE context_id IS NOT NULL),count(*)FILTER(WHERE context_id IS NULL) FROM legacy_map_anchor_facts WHERE run_id=$1`, run).Scan(&actual.Anchors, &actual.MediaAnchors, &actual.NoteAnchors)
	if e != nil || actual != v.Counts {
		return v, "", ErrLegacyMigrationConflict
	}
	return v, digest, nil
}
func (s *LegacyMigrationImporter) ApplyLegacyMapAnchorRestore(ctx context.Context, run string, source, manifest []byte) (LegacyMapAnchorRestoreResult, error) {
	unknown := LegacyMapAnchorRestoreResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacymapanchor.Build(run, source, manifest)
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,171))`, legacymigration.SourceSystem); e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, base) {
		return unknown, ErrLegacyMigrationConflict
	}
	if prev, d, e := readLegacyMapAnchorRestore(ctx, tx, run); e == nil {
		if d != p.ProjectionSHA256 {
			return unknown, ErrLegacyMigrationConflict
		}
		if tx.Commit(ctx) != nil {
			return unknown, ErrLegacyMigrationUnknown
		}
		return prev, nil
	} else if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return unknown, e
	}
	if _, _, e = contextAdaptationRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	for _, v := range p.Anchors {
		var media any
		if v.ContextID != "" {
			var asset string
			e = tx.QueryRow(ctx, `SELECT coalesce(image_id,video_id)::text FROM private_moment_media_contexts WHERE id=$1 AND moment_id=$2 AND original_map_enabled AND status='ACTIVE'`, v.ContextID, v.MomentID).Scan(&asset)
			if e != nil {
				return unknown, ErrLegacyMigrationConflict
			}
			media = asset
		}
		tag, e := tx.Exec(ctx, `INSERT INTO legacy_map_anchor_facts(id,run_id,moment_id,context_id,media_id,source_note_id,source_note_sha256,source_media_id,source_sha256,coordinate_proof,precision,latitude,longitude,native_binding)
 SELECT $1,$2,m.id,NULLIF($4,'')::uuid,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10,$11,$12,$13,birdtie_legacy_map_moment_binding(m) FROM moments m WHERE m.id=$3 AND m.revision=1 AND m.status='draft' AND m.visibility='private'`, v.ID, run, v.MomentID, v.ContextID, media, v.SourceNoteID, v.SourceNoteSHA256, v.SourceMediaID, v.SourceSHA256, v.Proof, v.Precision, v.Latitude, v.Longitude)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if tag.RowsAffected() != 1 {
			return unknown, ErrLegacyMigrationConflict
		}
	}
	c, _ := json.Marshal(p.Counts)
	d, _ := json.Marshal(p.Deferred)
	_, e = tx.Exec(ctx, `INSERT INTO legacy_map_anchor_runs(run_id,protocol,source_file_sha256,manifest_file_sha256,snapshot_sha256,projection_sha256,counts,deferred)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, run, legacymapanchor.Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.ProjectionSHA256, c, d)
	if e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	v, digest, e := readLegacyMapAnchorRestore(ctx, tx, run)
	if e != nil || digest != p.ProjectionSHA256 {
		return unknown, ErrLegacyMigrationConflict
	}
	if tx.Commit(ctx) != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return v, nil
}
func (s *LegacyMigrationImporter) InspectLegacyMapAnchorRestore(ctx context.Context, run string) (LegacyMapAnchorRestoreResult, error) {
	v := LegacyMapAnchorRestoreResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || !legacymigration.ValidRunID(run) {
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
	v, _, e = readLegacyMapAnchorRestore(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if tx.Commit(ctx) != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	return v, nil
}
