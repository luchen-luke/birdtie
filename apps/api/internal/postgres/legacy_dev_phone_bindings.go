package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

type LegacyDevPhoneBindingResult struct {
	RunID          string                         `json:"runId"`
	Status         string                         `json:"status"`
	Counts         legacymigration.DevPhoneCounts `json:"counts"`
	EnrolledOwners int64                          `json:"enrolledOwners"`
}

const legacyDevPhoneSourceCurrentSQL = `EXISTS(SELECT 1 FROM legacy_entity_mappings m JOIN legacy_import_runs r ON r.run_id=m.run_id
 WHERE m.source_system=b.source_system AND m.entity_kind='user' AND m.source_id=b.source_user_id
 AND m.target_id=b.owner_account_id AND m.owner_account_id=b.owner_account_id AND m.run_id=b.run_id
 AND m.source_version=b.source_owner_version AND m.source_sha256=b.source_owner_sha256
 AND m.source_status='active' AND m.claim_status='UNCLAIMED' AND r.snapshot_sha256=b.snapshot_sha256
 AND r.source_file_sha256=b.source_file_sha256 AND r.manifest_file_sha256=b.manifest_file_sha256)`

// Uses the existing exact target guard and original immutable preservation run.
// All canonical identities are locked in the same order and imported atomically.
func (s *LegacyMigrationImporter) ApplyDevPhoneBindings(ctx context.Context, run string, source, manifest []byte) (LegacyDevPhoneBindingResult, error) {
	result := LegacyDevPhoneBindingResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return result, ErrLegacyMigrationGuard
	}
	if len(source) < 1 || len(source) > LegacyMigrationMaxInputBytes || len(manifest) < 1 || len(manifest) > LegacyMigrationMaxInputBytes {
		return result, ErrLegacyMigrationInvalid
	}
	plan, e := legacymigration.BuildDevPhonePlan(run, source, manifest)
	if e != nil {
		return result, ErrLegacyMigrationInvalid
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return result, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return result, e
	}
	var contract string
	if e = tx.QueryRow(ctx, `SELECT birdtie_legacy_dev_phone_contract()`).Scan(&contract); e != nil || contract != "legacy-dev-phone-binding-v1" {
		return result, ErrLegacyMigrationGuard
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,154))`, legacymigration.SourceSystem); e != nil {
		return result, legacyMigrationFailure(e)
	}
	fact, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil {
		return result, e
	}
	if !legacyMigrationSameRun(fact, plan.Original) {
		return result, ErrLegacyMigrationConflict
	}
	var existing int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM legacy_dev_phone_bindings WHERE source_system=$1`, legacymigration.SourceSystem).Scan(&existing); e != nil {
		return result, ErrLegacyMigrationUnavailable
	}
	if existing != 0 && existing != len(plan.Bindings) {
		return result, ErrLegacyMigrationConflict
	}
	for _, b := range plan.Bindings {
		subject := hex.EncodeToString(b.PhoneDigest[:])
		if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, devPhoneIssuer+":"+subject); e != nil {
			return result, legacyMigrationFailure(e)
		}
		var owner, status, stamp, profile, agent string
		e = tx.QueryRow(ctx, `SELECT a.id::text,a.status,a.xmin::text,p.visibility,ag.status
   FROM legacy_entity_mappings m JOIN accounts a ON a.id=m.target_id
   JOIN user_profiles p ON p.account_id=a.id JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal'
   WHERE m.source_system=$1 AND m.entity_kind='user' AND m.source_id=$2 AND m.owner_account_id=m.target_id
   AND m.run_id=$3 AND m.source_version=$4 AND m.source_sha256=$5 AND m.source_status='active'
   AND m.claim_status='UNCLAIMED' AND a.account_type='person' FOR SHARE OF a,p,ag`, legacymigration.SourceSystem, b.SourceOwnerID, run, b.SourceOwnerVersion, b.SourceOwnerSHA256).Scan(&owner, &status, &stamp, &profile, &agent)
		if e != nil {
			return result, ErrLegacyMigrationConflict
		}
		var nativeOwner string
		e = tx.QueryRow(ctx, `SELECT account_id::text FROM account_auth_identities WHERE issuer=$1 AND subject=$2 FOR SHARE`, devPhoneIssuer, subject).Scan(&nativeOwner)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return result, ErrLegacyMigrationUnavailable
		}
		if e == nil && nativeOwner != owner {
			return result, ErrLegacyMigrationConflict
		}
		if existing == 0 {
			if profile != "private" || agent != "suspended" || (status != "suspended" && !(status == "active" && nativeOwner == owner)) {
				return result, ErrLegacyMigrationConflict
			}
			_, e = tx.Exec(ctx, `INSERT INTO legacy_dev_phone_bindings(phone_digest,source_system,source_user_id,owner_account_id,run_id,
    source_owner_version,source_owner_sha256,source_identity_sha256,snapshot_sha256,source_file_sha256,manifest_file_sha256,initial_account_stamp)
    VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, b.PhoneDigest[:], legacymigration.SourceSystem, b.SourceOwnerID, owner, run, b.SourceOwnerVersion, b.SourceOwnerSHA256, b.SourceIdentitySHA256, plan.Original.Run.SnapshotSHA256, plan.Original.Run.SourceFileSHA256, plan.Original.Run.ManifestFileSHA256, stamp)
			if e != nil {
				return result, legacyMigrationFailure(e)
			}
		} else {
			var same bool
			e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM legacy_dev_phone_bindings b WHERE phone_digest=$1 AND source_system=$2
    AND source_user_id=$3 AND owner_account_id=$4 AND run_id=$5 AND source_owner_version=$6 AND source_owner_sha256=$7
    AND source_identity_sha256=$8 AND snapshot_sha256=$9 AND source_file_sha256=$10 AND manifest_file_sha256=$11 AND `+legacyDevPhoneSourceCurrentSQL+`)`, b.PhoneDigest[:], legacymigration.SourceSystem, b.SourceOwnerID, owner, run, b.SourceOwnerVersion, b.SourceOwnerSHA256, b.SourceIdentitySHA256, plan.Original.Run.SnapshotSHA256, plan.Original.Run.SourceFileSHA256, plan.Original.Run.ManifestFileSHA256).Scan(&same)
			if e != nil {
				return result, ErrLegacyMigrationUnavailable
			}
			if !same {
				return result, ErrLegacyMigrationConflict
			}
		}
	}
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return result, e
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*),count(first_enrolled_at) FROM legacy_dev_phone_bindings WHERE run_id=$1`, run).Scan(&count, &result.EnrolledOwners); e != nil || count != len(plan.Bindings) {
		return result, ErrLegacyMigrationConflict
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return LegacyDevPhoneBindingResult{RunID: run, Status: "UNKNOWN"}, ErrLegacyMigrationUnknown
	}
	result.Status = "DEV_PHONE_BINDINGS_COMMITTED"
	result.Counts = plan.Counts
	return result, nil
}

// Caller already owns the original per-phone advisory lock. Missing 154 in an
// older non-migration test database retains ordinary dev enrollment behavior;
// managed startup independently requires the 154 contract.
func (s *Store) legacyDevPhoneOwner(ctx context.Context, tx pgx.Tx, digest [32]byte, nativeOwner string) (string, bool, error) {
	var present bool
	if e := tx.QueryRow(ctx, `SELECT to_regclass('public.legacy_dev_phone_bindings') IS NOT NULL`).Scan(&present); e != nil {
		return "", false, e
	}
	if !present {
		return "", false, nil
	}
	var owner, initial string
	var enrolled *time.Time
	var sourceCurrent bool
	e := tx.QueryRow(ctx, `SELECT b.owner_account_id::text,b.initial_account_stamp,b.first_enrolled_at,`+legacyDevPhoneSourceCurrentSQL+`
 FROM legacy_dev_phone_bindings b WHERE b.phone_digest=$1 FOR UPDATE OF b`, digest[:]).Scan(&owner, &initial, &enrolled, &sourceCurrent)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", false, nil
	}
	if e != nil {
		return "", false, e
	}
	if !sourceCurrent || (nativeOwner != "" && nativeOwner != owner) {
		return "", false, identity.ErrUnauthorized
	}
	var status, stamp, profile, agent string
	e = tx.QueryRow(ctx, `SELECT a.status,a.xmin::text,p.visibility,ag.status FROM accounts a
 JOIN user_profiles p ON p.account_id=a.id JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal'
 WHERE a.id=$1 AND a.account_type='person' FOR UPDATE OF a FOR SHARE OF p,ag`, owner).Scan(&status, &stamp, &profile, &agent)
	if e != nil {
		return "", false, identity.ErrUnauthorized
	}
	if nativeOwner != "" {
		if status != "active" {
			return "", false, identity.ErrUnauthorized
		}
		if enrolled == nil && (profile != "private" || agent != "suspended") {
			return "", false, identity.ErrUnauthorized
		}
		return owner, enrolled == nil, nil
	}
	if enrolled != nil || status != "suspended" || stamp != initial || profile != "private" || agent != "suspended" {
		return "", false, identity.ErrUnauthorized
	}
	tag, e := tx.Exec(ctx, `UPDATE accounts SET status='active',updated_at=clock_timestamp() WHERE id=$1 AND status='suspended' AND xmin::text=$2`, owner, initial)
	if e != nil {
		return "", false, e
	}
	if tag.RowsAffected() != 1 {
		return "", false, identity.ErrUnauthorized
	}
	return owner, true, nil
}

func (s *Store) finishLegacyDevPhoneEnrollment(ctx context.Context, tx pgx.Tx, digest [32]byte, owner, session string) error {
	tag, e := tx.Exec(ctx, `UPDATE legacy_dev_phone_bindings SET first_enrolled_at=clock_timestamp()
 WHERE phone_digest=$1 AND owner_account_id=$2 AND first_enrolled_at IS NULL`, digest[:], owner)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrUnauthorized
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
 VALUES($1,'legacy_dev_phone_first_enrollment','session',$2,'allowed','FIRST_FIXED_TEST_LOGIN')`, owner, session)
	return e
}
