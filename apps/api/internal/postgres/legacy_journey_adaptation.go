package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/legacyjourney"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type LegacyJourneyDetailsResult struct {
	RunID  string               `json:"runId"`
	Status string               `json:"status"`
	Counts legacyjourney.Counts `json:"counts"`
}

func PrepareLegacyJourneyDetails(run string, source, manifest []byte) (LegacyJourneyDetailsResult, error) {
	p, e := legacyjourney.Build(run, source, manifest)
	if e != nil {
		return LegacyJourneyDetailsResult{}, ErrLegacyMigrationInvalid
	}
	return LegacyJourneyDetailsResult{run, "PREPARED_NOT_IMPORTED", p.Counts}, nil
}
func readLegacyJourneyDetails(ctx context.Context, tx pgx.Tx, run string) (LegacyJourneyDetailsResult, string, error) {
	v := LegacyJourneyDetailsResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts FROM legacy_journey_adaptation_runs WHERE run_id=$1`, run).Scan(&digest, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(raw, &v.Counts) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	var journeys, entries int
	e = tx.QueryRow(ctx, `SELECT(SELECT count(*) FROM legacy_journey_adaptations WHERE run_id=$1),(SELECT count(*) FROM legacy_journey_entry_origins WHERE run_id=$1)`, run).Scan(&journeys, &entries)
	if e != nil || journeys != v.Counts.Journeys || entries != v.Counts.Entries {
		return v, "", ErrLegacyMigrationConflict
	}
	return v, digest, nil
}
func (s *LegacyMigrationImporter) ApplyJourneyDetails(ctx context.Context, run string, source, manifest []byte) (LegacyJourneyDetailsResult, error) {
	unknown := LegacyJourneyDetailsResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacyjourney.Build(run, source, manifest)
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,158))`, legacymigration.SourceSystem); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, base) {
		return unknown, ErrLegacyMigrationConflict
	}
	if _, _, e = s.personalArchiveRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	if _, _, e = nativeAdaptationRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	previous, digest, e := readLegacyJourneyDetails(ctx, tx, run)
	if e == nil {
		if digest != p.ProjectionSHA256 || previous.Counts != p.Counts {
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
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_journey_adaptation_runs(run_id,protocol,projection_sha256,counts)VALUES($1,$2,$3,$4)`, run, legacyjourney.Protocol, p.ProjectionSHA256, counts); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	for _, j := range p.Journeys {
		var owner string
		e = tx.QueryRow(ctx, `SELECT m.target_id FROM legacy_entity_mappings m JOIN legacy_personal_archives ar ON ar.run_id=m.run_id AND ar.source_system=m.source_system AND ar.source_owner_id=m.source_id AND ar.owner_account_id=m.target_id JOIN accounts a ON a.id=m.target_id WHERE m.entity_kind='user' AND m.source_id=$1 AND m.owner_account_id=m.target_id AND m.run_id=$2 AND ar.kind='journey' AND ar.source_key=$3 AND ar.source_sha256=$4 AND NOT ar.deleted AND a.account_type='person' AND a.status IN('active','suspended') FOR SHARE OF a`, j.SourceOwnerID, run, j.SourceKey, j.SourceSHA256).Scan(&owner)
		if e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		var exists, mapped bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM journeys WHERE id=$1),EXISTS(SELECT 1 FROM legacy_native_adaptation_entities WHERE kind='journey' AND target_id=$1::text AND run_id=$2 AND source_key=$3 AND source_sha256=$4 AND owner_account_id=$5)`, j.TargetID, run, j.SourceKey, j.SourceSHA256, owner).Scan(&exists, &mapped)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if mapped && !exists {
			return unknown, ErrLegacyMigrationConflict
		}
		if exists {
			if !mapped {
				return unknown, ErrLegacyMigrationConflict
			}
			var same bool
			e = tx.QueryRow(ctx, `SELECT owner_account_id=$2 AND status='draft' AND visibility='private' AND revision=1 AND detail_schema IS NULL FROM journeys WHERE id=$1 FOR UPDATE`, j.TargetID, owner).Scan(&same)
			if e != nil || !same {
				return unknown, ErrLegacyMigrationConflict
			}
			if _, e = tx.Exec(ctx, `DELETE FROM journey_stops WHERE journey_id=$1`, j.TargetID); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
			if _, e = tx.Exec(ctx, `UPDATE journeys SET city_id=NULLIF($2,''),title=$3,summary='',detail_schema=$4 WHERE id=$1`, j.TargetID, j.CityID, j.Title, content.JourneyDetailsSchema); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
		} else {
			if _, e = tx.Exec(ctx, `INSERT INTO journeys(id,owner_account_id,city_id,title,summary,visibility,status,detail_schema)VALUES($1,$2,NULLIF($3,''),$4,'','private','draft',$5)`, j.TargetID, owner, j.CityID, j.Title, content.JourneyDetailsSchema); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
		}
		if e = writeJourneyDetailsTx(ctx, tx, j.TargetID, j.Details); e != nil {
			return unknown, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO legacy_journey_adaptations(run_id,source_key,source_sha256,owner_account_id,target_id)VALUES($1,$2,$3,$4,$5)`, run, j.SourceKey, j.SourceSHA256, owner, j.TargetID); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		for _, o := range j.Origins {
			raw, _ := json.Marshal(o.Value)
			if _, e = tx.Exec(ctx, `INSERT INTO legacy_journey_entry_origins(run_id,source_key,journey_id,entry_id,original_place_id,origin)VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,$6)`, run, j.SourceKey, j.TargetID, o.EntryID, o.PlaceID, raw); e != nil {
				return unknown, legacyMigrationFailure(e)
			}
		}
	}
	result, digest, e := readLegacyJourneyDetails(ctx, tx, run)
	if e != nil || digest != p.ProjectionSHA256 || result.Counts != p.Counts {
		return unknown, ErrLegacyMigrationConflict
	}
	if e = tx.Commit(ctx); e != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return result, nil
}
func (s *LegacyMigrationImporter) InspectJourneyDetails(ctx context.Context, run string) (LegacyJourneyDetailsResult, error) {
	v := LegacyJourneyDetailsResult{RunID: run, Status: "UNKNOWN"}
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
	v, _, e = readLegacyJourneyDetails(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if e = tx.Commit(ctx); e != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	return v, nil
}
