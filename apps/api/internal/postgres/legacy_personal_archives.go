package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacycollections"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type LegacyPersonalArchiveResult struct {
	RunID             string                   `json:"runId"`
	Status            string                   `json:"status"`
	Counts            legacycollections.Counts `json:"counts"`
	IdentityActivated bool                     `json:"identityActivated"`
	MediaImported     bool                     `json:"mediaImported"`
}

func (s *LegacyMigrationImporter) personalArchiveRead(ctx context.Context, tx pgx.Tx, run string) (LegacyPersonalArchiveResult, string, error) {
	r := LegacyPersonalArchiveResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts FROM legacy_personal_archive_runs WHERE run_id=$1`, run).Scan(&digest, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, "", ErrLegacyMigrationNotFound
	}
	if e != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	if json.Unmarshal(raw, &r.Counts) != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	var actual legacycollections.Counts
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='journey'),count(*) FILTER(WHERE kind='journey' AND deleted),count(*) FILTER(WHERE kind='theme_map'),count(*) FILTER(WHERE kind='theme_map' AND deleted),count(*) FILTER(WHERE kind='saved_place') FROM legacy_personal_archives WHERE run_id=$1`, run).Scan(&actual.Journeys, &actual.JourneyTombstones, &actual.ThemeMaps, &actual.ThemeMapTombstones, &actual.SavedPlaces)
	if e != nil || actual != r.Counts {
		return r, "", ErrLegacyMigrationUnavailable
	}
	return r, digest, nil
}

func (s *LegacyMigrationImporter) ApplyPersonalArchives(ctx context.Context, run string, source, manifest []byte) (LegacyPersonalArchiveResult, error) {
	r := LegacyPersonalArchiveResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return r, ErrLegacyMigrationGuard
	}
	plan, e := legacycollections.BuildPlan(run, source, manifest)
	if e != nil {
		return r, ErrLegacyMigrationInvalid
	}
	original, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return r, ErrLegacyMigrationInvalid
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return r, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return r, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,149))`, legacymigration.SourceSystem); e != nil {
		return r, legacyMigrationFailure(e)
	}
	fact, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil {
		return r, e
	}
	if !legacyMigrationSameRun(fact, original) {
		return r, ErrLegacyMigrationConflict
	}
	previous, digest, e := s.personalArchiveRead(ctx, tx, run)
	if e == nil {
		if digest != plan.ProjectionSHA256 || previous.Counts != plan.Counts {
			return r, ErrLegacyMigrationConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return r, ErrLegacyMigrationUnknown
		}
		return previous, nil
	}
	if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return r, e
	}
	counts, e := json.Marshal(plan.Counts)
	if e != nil {
		return r, ErrLegacyMigrationInvalid
	}
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_personal_archive_runs(run_id,projection_protocol,projection_sha256,counts)VALUES($1,'birdtie-civu-personal-archive-v1',$2,$3)`, run, plan.ProjectionSHA256, counts); e != nil {
		return r, legacyMigrationFailure(e)
	}
	for _, a := range plan.Records {
		id, err := legacyMigrationUUID()
		if err != nil {
			return r, err
		}
		var owner string
		if e = tx.QueryRow(ctx, `SELECT target_id FROM legacy_entity_mappings WHERE source_system=$1 AND entity_kind='user' AND source_id=$2 AND run_id=$3`, legacymigration.SourceSystem, a.SourceOwnerID, run).Scan(&owner); e != nil {
			return r, ErrLegacyMigrationConflict
		}
		var payload any
		if a.Payload != nil {
			payload = []byte(a.Payload)
		}
		_, e = tx.Exec(ctx, `INSERT INTO legacy_personal_archives(id,run_id,source_system,kind,source_key,source_owner_id,owner_account_id,source_revision,source_sha256,deleted,source_deleted_at,title,source_created_at,source_updated_at,payload)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, id, run, legacymigration.SourceSystem, a.Kind, a.SourceKey, a.SourceOwnerID, owner, a.SourceRevision, a.SourceSHA256, a.Deleted, a.SourceDeletedAt, a.Title, a.SourceCreatedAt, a.SourceUpdatedAt, payload)
		if e != nil {
			return r, legacyMigrationFailure(e)
		}
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return r, legacyMigrationFailure(e)
	}
	result, digest, e := s.personalArchiveRead(ctx, tx, run)
	if e != nil || digest != plan.ProjectionSHA256 {
		return r, ErrLegacyMigrationUnavailable
	}
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return r, ErrLegacyMigrationUnknown
	}
	return result, nil
}

func (s *LegacyMigrationImporter) InspectPersonalArchives(ctx context.Context, run string) (LegacyPersonalArchiveResult, error) {
	r := LegacyPersonalArchiveResult{RunID: run, Status: "UNKNOWN"}
	if !legacymigration.ValidRunID(run) || s == nil || s.database == nil {
		return r, ErrLegacyMigrationGuard
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return r, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return r, e
	}
	result, _, e := s.personalArchiveRead(ctx, tx, run)
	if e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil {
		return r, ErrLegacyMigrationUnknown
	}
	return result, nil
}
