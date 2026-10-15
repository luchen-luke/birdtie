package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/birdtie/birdtie/apps/api/internal/legacyimagerecovery"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/jackc/pgx/v5"
)

var ErrLegacyPrivateImageRecoveryDerivative = errors.New("legacy_private_image_recovery_derivative_rejected")

// A controlled CLI supplies bounded local file readers. This is never a URL,
// arbitrary archive extractor, credential, or a public upload capability.
type LegacyPrivateImageRecoveryBlobs func(context.Context, legacyimagerecovery.Record) (io.ReadCloser, error)

type LegacyPrivateImageRecoveryResult struct {
	RunID    string                     `json:"runId"`
	Status   string                     `json:"status"`
	Counts   legacyimagerecovery.Counts `json:"counts"`
	Deferred map[string]int             `json:"deferred"`
}

func legacyPrivateImageRecoveryRead(ctx context.Context, tx pgx.Tx, run string) (LegacyPrivateImageRecoveryResult, string, error) {
	r := LegacyPrivateImageRecoveryResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var counts, deferred []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,deferred FROM legacy_private_image_recovery_runs WHERE run_id=$1`, run).Scan(&digest, &counts, &deferred)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(counts, &r.Counts) != nil || json.Unmarshal(deferred, &r.Deferred) != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	var actual legacyimagerecovery.Counts
	e = tx.QueryRow(ctx, `SELECT count(*),count(*)FILTER(WHERE recovery_reason='JPEG_UNKNOWN_SOURCE_METADATA'),count(*)FILTER(WHERE recovery_reason='HEIC_MISCLASSIFIED_FTYP'),count(DISTINCT source_note_id),count(DISTINCT owner_account_id) FROM legacy_private_image_recovery_items WHERE run_id=$1`, run).Scan(&actual.Images, &actual.JPEG, &actual.HEIC, &actual.Notes, &actual.Owners)
	if e != nil || actual != r.Counts {
		return r, "", ErrLegacyMigrationUnavailable
	}
	return r, digest, nil
}

func buildLegacyRecoveredJPEG(ctx context.Context, r legacyimagerecovery.Record, blobs LegacyPrivateImageRecoveryBlobs, n *legacyimagerecovery.Normalizer) (legacyimagerecovery.Derivative, error) {
	source, e := blobs(ctx, r)
	if e != nil || source == nil {
		return legacyimagerecovery.Derivative{}, ErrLegacyPrivateImageRecoveryDerivative
	}
	raw, e := io.ReadAll(io.LimitReader(source, r.InputBytes+1))
	closed := source.Close()
	defer clear(raw)
	if e != nil || closed != nil || int64(len(raw)) != r.InputBytes || legacymedia.Hash(raw) != r.InputSHA256 {
		return legacyimagerecovery.Derivative{}, ErrLegacyPrivateImageRecoveryDerivative
	}
	out, e := n.Normalize(ctx, r, raw)
	if e != nil {
		return out, ErrLegacyPrivateImageRecoveryDerivative
	}
	return out, nil
}

// ApplyLegacyPrivateImageRecovery requires the original preservation and committed156
// native mappings. It writes only scrubbed private derivatives, no source files,
// user Session, approval, activation, public material, EXIF or old URL.
func (s *LegacyMigrationImporter) ApplyLegacyPrivateImageRecovery(ctx context.Context, run string, input LegacyPrivateMediaInput, blobs LegacyPrivateImageRecoveryBlobs, n *legacyimagerecovery.Normalizer) (LegacyPrivateImageRecoveryResult, error) {
	unknown := LegacyPrivateImageRecoveryResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || blobs == nil || n == nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacyimagerecovery.Build(run, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	original, e := legacymigration.BuildPlan(run, input.Source, input.Manifest)
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
	var contract string
	if e = tx.QueryRow(ctx, `SELECT birdtie_legacy_private_image_recovery_contract()`).Scan(&contract); e != nil || contract != "legacy-private-image-recovery-v1" {
		return unknown, ErrLegacyMigrationGuard
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,161))`, legacymigration.SourceSystem); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	fact, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if !legacyMigrationSameRun(fact, original) {
		return unknown, ErrLegacyMigrationConflict
	}
	if _, _, e = nativeAdaptationRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	previous, digest, e := legacyPrivateImageRecoveryRead(ctx, tx, run)
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
	if len(p.Records) == 0 || p.Counts.HEIC > 0 && n.Validate() != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	// Canonical native UUID lock order, rather than source/map iteration order.
	ownerIDs := []string{}
	noteIDs := []string{}
	seenOwners := map[string]bool{}
	seenNotes := map[string]bool{}
	for _, r := range p.Records {
		if !seenOwners[r.OwnerSourceID] {
			ownerIDs = append(ownerIDs, r.OwnerSourceID)
			seenOwners[r.OwnerSourceID] = true
		}
		if !seenNotes[r.SourceNoteID] {
			noteIDs = append(noteIDs, r.SourceNoteID)
			seenNotes[r.SourceNoteID] = true
		}
	}
	owners := map[string]string{}
	rows, e := tx.Query(ctx, `SELECT m.source_id::text,a.id::text FROM legacy_entity_mappings m JOIN accounts a ON a.id=m.target_id WHERE m.source_system=$1 AND m.entity_kind='user' AND m.run_id=$2 AND m.source_id=ANY($3::uuid[]) AND a.account_type='person' AND a.status IN('active','suspended') ORDER BY a.id FOR SHARE OF a`, legacymigration.SourceSystem, run, ownerIDs)
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	for rows.Next() {
		var source, target string
		if rows.Scan(&source, &target) != nil {
			rows.Close()
			return unknown, ErrLegacyMigrationUnavailable
		}
		owners[source] = target
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(owners) != p.Counts.Owners {
		return unknown, ErrLegacyMigrationConflict
	}
	type target struct {
		id, owner, noteSHA string
		revision           int64
	}
	targets := map[string]target{}
	rows, e = tx.Query(ctx, `SELECT n.source_note_id::text,m.id::text,m.author_account_id::text,n.source_sha256,m.revision FROM legacy_native_adaptation_entities x JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key AND n.owner_account_id=x.owner_account_id AND n.source_sha256=x.source_sha256 JOIN moments m ON m.id::text=x.target_id AND m.author_account_id=x.owner_account_id WHERE x.run_id=$1 AND x.kind='moment' AND n.source_note_id=ANY($2::uuid[]) AND NOT n.deleted AND m.visibility='private' AND m.status='draft' ORDER BY m.id FOR UPDATE OF m`, run, noteIDs)
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	for rows.Next() {
		var source string
		var v target
		if rows.Scan(&source, &v.id, &v.owner, &v.noteSHA, &v.revision) != nil {
			rows.Close()
			return unknown, ErrLegacyMigrationUnavailable
		}
		targets[source] = v
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(targets) != p.Counts.Notes {
		return unknown, ErrLegacyMigrationConflict
	}
	for _, r := range p.Records {
		v := targets[r.SourceNoteID]
		if v.owner != owners[r.OwnerSourceID] || v.noteSHA != r.SourceNoteSHA256 {
			return unknown, ErrLegacyMigrationConflict
		}
	}
	counts, _ := json.Marshal(p.Counts)
	deferred, _ := json.Marshal(p.Deferred)
	_, e = tx.Exec(ctx, `INSERT INTO legacy_private_image_recovery_runs(run_id,protocol,projection_sha256,source_file_sha256,manifest_file_sha256,matcher_file_sha256,authenticated_seal_sha256,counts,deferred)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, run, legacyimagerecovery.Protocol, p.ProjectionSHA256, p.SourceFileSHA256, p.ManifestFileSHA256, p.MatcherFileSHA256, p.AuthenticatedSealSHA256, counts, deferred)
	if e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	for _, r := range p.Records {
		v := targets[r.SourceNoteID]
		derivative, err := buildLegacyRecoveredJPEG(ctx, r, blobs, n)
		if err != nil {
			return unknown, err
		}
		derived := derivative.Bytes
		hash := legacymedia.Hash(derived)
		// created_at is this import's database time, not a forged author date.
		// Sequential inserts preserve source order; provenance retains exact gaps.
		_, e = tx.Exec(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) INSERT INTO private_moment_images(id,owner_account_id,moment_id,operation_id,source_binding,moment_revision,session_id,mime_type,input_size,input_sha256,pixel_risk,purpose,status,revision,derivative,derivative_sha256,preview_expires_at,retain_until,created_at,updated_at) SELECT $1,$2,$3,$1,$4,$5,NULL,'image/jpeg',$6,$7,'UNKNOWN','PRIVATE_MOMENT_ATTACHMENT','ready_private',1,$8,$9,cl.at+interval '5 minutes','9999-12-31T00:00:00Z'::timestamptz,cl.at,cl.at FROM cl`, r.ImageID, v.owner, v.id, "legacy-private-recovery:"+p.ProjectionSHA256, v.revision, r.InputBytes, r.InputSHA256, derived, hash)
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO media_assets(id,owner_account_id,storage_key,mime_type,byte_size,sha256_hex,state)VALUES($1::uuid,$2,'legacy-private-moment/'||($1::uuid)::text,'image/jpeg',$3,$4,'ready_private')`, r.ImageID, v.owner, len(derived), hash)
		}
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO legacy_private_image_recovery_items(run_id,source_note_id,source_media_id,source_note_sha256,source_media_sha256,source_input_sha256,source_input_bytes,source_position,source_mime,source_declared_bytes,recovery_reason,owner_account_id,moment_id,image_id,derivative_sha256,derivative_bytes,width,height,validation_kind,antivirus_scan,privacy_risk)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,'NOT_RUN','UNKNOWN')`, run, r.SourceNoteID, r.SourceMediaID, r.SourceNoteSHA256, r.SourceMediaSHA256, r.InputSHA256, r.InputBytes, r.Position, r.SourceMIME, r.SourceDeclaredBytes, r.RecoveryReason, v.owner, v.id, r.ImageID, hash, len(derived), derivative.Width, derivative.Height, derivative.ValidationKind)
		}
		derivative.Erase()
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	result, digest, e := legacyPrivateImageRecoveryRead(ctx, tx, run)
	if e != nil || digest != p.ProjectionSHA256 {
		return unknown, ErrLegacyMigrationUnavailable
	}
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return result, nil
}

// Inspection returns only original import facts. Deleting an image, changing a
// Session or editing the Moment never recreates bytes or changes this receipt.
func (s *LegacyMigrationImporter) InspectLegacyPrivateImageRecovery(ctx context.Context, run string) (LegacyPrivateImageRecoveryResult, error) {
	unknown := LegacyPrivateImageRecoveryResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || !legacymigration.ValidRunID(run) {
		return unknown, ErrLegacyMigrationGuard
	}
	tx, e := s.database.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	defer legacyMigrationRollback(tx)
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	r, _, e := legacyPrivateImageRecoveryRead(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if e = tx.Commit(ctx); e != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return r, nil
}
