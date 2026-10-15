package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacymap"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type LegacyMapRestoreResult struct {
	RunID    string           `json:"runId"`
	Status   string           `json:"status"`
	Counts   legacymap.Counts `json:"counts"`
	Deferred map[string]int   `json:"deferred"`
}

func PrepareLegacyMapRestore(run string, source, manifest []byte) (LegacyMapRestoreResult, error) {
	p, e := legacymap.Build(run, source, manifest)
	if e != nil {
		return LegacyMapRestoreResult{}, ErrLegacyMigrationInvalid
	}
	return LegacyMapRestoreResult{run, "PREPARED_NOT_IMPORTED", p.Counts, p.Deferred}, nil
}
func readLegacyMapRestore(ctx context.Context, tx pgx.Tx, run string) (LegacyMapRestoreResult, string, error) {
	v := LegacyMapRestoreResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var c, d []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,deferred FROM legacy_map_visibility_runs WHERE run_id=$1`, run).Scan(&digest, &c, &d)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(c, &v.Counts) != nil || json.Unmarshal(d, &v.Deferred) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	var actual legacymap.Counts
	e = tx.QueryRow(ctx, `SELECT count(*),count(*)FILTER(WHERE public_catalog),count(*)FILTER(WHERE latitude IS NOT NULL),(SELECT count(*) FROM legacy_note_archives WHERE run_id=$1 AND NOT deleted AND source_visibility='public' AND source_moderation_status='ok') FROM legacy_map_place_facts WHERE run_id=$1`, run).Scan(&actual.Places, &actual.PublicPlaces, &actual.CoordinatePlaces, &actual.PublicMoments)
	if e != nil || actual != v.Counts {
		return v, "", ErrLegacyMigrationConflict
	}
	return v, digest, nil
}
func (s *LegacyMigrationImporter) ApplyLegacyMapRestore(ctx context.Context, run string, source, manifest []byte) (LegacyMapRestoreResult, error) {
	unknown := LegacyMapRestoreResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacymap.Build(run, source, manifest)
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,170))`, legacymigration.SourceSystem); e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, base) {
		return unknown, ErrLegacyMigrationConflict
	}
	if prev, d, e := readLegacyMapRestore(ctx, tx, run); e == nil {
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
	for _, v := range p.Places {
		_, e = tx.Exec(ctx, `INSERT INTO legacy_map_place_facts(place_id,run_id,source_place_id,source_sha256,public_catalog,coordinate_proof,latitude,longitude,native_binding)
 SELECT p.id,$2,$3,$4,$5,$6,$7,$8,CASE WHEN p.xmin=x.import_xid THEN birdtie_legacy_map_place_binding(p) END FROM places p JOIN(
 SELECT e.run_id,e.source_key,e.source_sha256,e.target_id,r.xmin import_xid FROM legacy_native_adaptation_entities e JOIN legacy_native_adaptation_runs r USING(run_id) WHERE e.kind='place'
 UNION ALL SELECT e.run_id,e.source_key,e.source_sha256,e.target_id::text,r.xmin FROM legacy_native_context_entities e JOIN legacy_native_context_runs r USING(run_id) WHERE e.kind='unplaced_place') x ON x.target_id=p.id::text
 WHERE p.id=$1 AND x.run_id=$2 AND x.source_key=($3::uuid)::text AND x.source_sha256=$4`, v.TargetID, run, v.SourceID, v.SourceSHA256, v.Public, v.CoordinateProof, v.Latitude, v.Longitude)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	c, _ := json.Marshal(p.Counts)
	d, _ := json.Marshal(p.Deferred)
	_, e = tx.Exec(ctx, `INSERT INTO legacy_map_visibility_runs(run_id,protocol,source_file_sha256,manifest_file_sha256,snapshot_sha256,projection_sha256,counts,deferred)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, run, legacymap.Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.ProjectionSHA256, c, d)
	if e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	v, digest, e := readLegacyMapRestore(ctx, tx, run)
	if e != nil || digest != p.ProjectionSHA256 {
		return unknown, ErrLegacyMigrationConflict
	}
	if tx.Commit(ctx) != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return v, nil
}
func (s *LegacyMigrationImporter) InspectLegacyMapRestore(ctx context.Context, run string) (LegacyMapRestoreResult, error) {
	v := LegacyMapRestoreResult{RunID: run, Status: "UNKNOWN"}
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
	v, _, e = readLegacyMapRestore(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if tx.Commit(ctx) != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	return v, nil
}
