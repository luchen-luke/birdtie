package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	la "github.com/birdtie/birdtie/apps/api/internal/legacymomentauthor"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/jackc/pgx/v5"
	"image"
	"image/color"
	"image/jpeg"
)

type LegacyMomentAuthorRestoreResult struct {
	RunID    string         `json:"runId"`
	Status   string         `json:"status"`
	Counts   la.Counts      `json:"counts"`
	Deferred map[string]int `json:"deferred"`
}

func authorPlan(run string, i LegacyPrivateMediaInput) (la.Plan, error) {
	return la.Build(run, i.Source, i.Manifest, i.Matched, i.AuthenticatedSeal, i.ExpectedSealSHA256)
}
func PrepareLegacyMomentAuthorRestore(run string, i LegacyPrivateMediaInput) (LegacyMomentAuthorRestoreResult, error) {
	p, e := authorPlan(run, i)
	if e != nil {
		return LegacyMomentAuthorRestoreResult{}, ErrLegacyMigrationInvalid
	}
	return LegacyMomentAuthorRestoreResult{run, "PREPARED_NOT_IMPORTED", p.Counts, p.Deferred}, nil
}
func readLegacyMomentAuthorRestore(ctx context.Context, tx pgx.Tx, run string) (LegacyMomentAuthorRestoreResult, string, error) {
	v := LegacyMomentAuthorRestoreResult{RunID: run, Status: "COMMITTED"}
	var digest string
	var c, d []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts,deferred FROM legacy_moment_author_runs WHERE run_id=$1`, run).Scan(&digest, &c, &d)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(c, &v.Counts) != nil || json.Unmarshal(d, &v.Deferred) != nil {
		return v, "", ErrLegacyMigrationUnavailable
	}
	var actual la.Counts
	e = tx.QueryRow(ctx, `SELECT(SELECT count(*)FROM legacy_moment_author_facts WHERE run_id=$1),count(*),count(*)FILTER(WHERE source_media_id IS NOT NULL)FROM legacy_moment_author_snapshots WHERE run_id=$1`, run).Scan(&actual.Moments, &actual.Authors, &actual.Avatars)
	if e != nil || actual != v.Counts {
		return v, "", ErrLegacyMigrationConflict
	}
	return v, digest, nil
}
func (s *LegacyMigrationImporter) ApplyLegacyMomentAuthorRestore(ctx context.Context, run string, i LegacyPrivateMediaInput, blobs LegacyReferenceMediaBlobs) (LegacyMomentAuthorRestoreResult, error) {
	unknown := LegacyMomentAuthorRestoreResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := authorPlan(run, i)
	if e != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	base, e := legacymigration.BuildPlan(run, i.Source, i.Manifest)
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,172))`, legacymigration.SourceSystem); e != nil {
		return unknown, ErrLegacyMigrationUnavailable
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, base) {
		return unknown, ErrLegacyMigrationConflict
	}
	if prev, d, e := readLegacyMomentAuthorRestore(ctx, tx, run); e == nil {
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
	for _, a := range p.Authors {
		var mid, msha, isha, isz, body, bsha any
		if a.Avatar != nil {
			if blobs == nil {
				return unknown, ErrLegacyMigrationInvalid
			}
			thumb, e := BuildLegacyMomentAuthorAvatar(ctx, *a.Avatar, blobs)
			if e != nil {
				return unknown, e
			}
			defer clear(thumb.Bytes)
			mid, msha, isha, isz, body, bsha = a.Avatar.SourceID, a.Avatar.SourceSHA256, a.Avatar.InputSHA256, a.Avatar.InputBytes, thumb.Bytes, thumb.SHA256
		}
		tag, e := tx.Exec(ctx, `INSERT INTO legacy_moment_author_snapshots(run_id,source_user_id,owner_account_id,source_user_sha256,source_version,display_name,source_media_id,source_media_sha256,input_sha256,input_bytes,thumbnail,thumbnail_sha256)
 SELECT $1,$2,u.target_id,$3,$4,$5,$6,$7,$8,$9,$10,$11 FROM legacy_entity_mappings u WHERE u.run_id=$1 AND u.entity_kind='user'AND u.source_id=$2 AND u.owner_account_id=u.target_id AND u.source_sha256=$3 AND u.source_version=$4 AND u.source_status='active'`, run, a.SourceID, a.SourceSHA256, a.Version, a.DisplayName, mid, msha, isha, isz, body, bsha)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		if tag.RowsAffected() != 1 {
			return unknown, ErrLegacyMigrationConflict
		}
	}
	for _, f := range p.Facts {
		_, e = tx.Exec(ctx, `INSERT INTO legacy_moment_author_facts(moment_id,run_id,source_user_id,source_note_id,source_note_sha256,source_created_at)VALUES($1,$2,$3,$4,$5,$6)`, f.MomentID, run, f.SourceUserID, f.SourceNoteID, f.SourceNoteSHA256, f.CreatedAt)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	c, _ := json.Marshal(p.Counts)
	d, _ := json.Marshal(p.Deferred)
	_, e = tx.Exec(ctx, `INSERT INTO legacy_moment_author_runs(run_id,protocol,source_file_sha256,manifest_file_sha256,snapshot_sha256,matcher_file_sha256,seal_sha256,projection_sha256,counts,deferred)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, run, la.Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.MatcherFileSHA256, p.SealSHA256, p.ProjectionSHA256, c, d)
	if e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	v, digest, e := readLegacyMomentAuthorRestore(ctx, tx, run)
	if e != nil || digest != p.ProjectionSHA256 {
		return unknown, ErrLegacyMigrationConflict
	}
	if tx.Commit(ctx) != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return v, nil
}
func (s *LegacyMigrationImporter) InspectLegacyMomentAuthorRestore(ctx context.Context, run string) (LegacyMomentAuthorRestoreResult, error) {
	v := LegacyMomentAuthorRestoreResult{RunID: run, Status: "UNKNOWN"}
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
	v, _, e = readLegacyMomentAuthorRestore(ctx, tx, run)
	if e != nil {
		return v, e
	}
	if tx.Commit(ctx) != nil {
		return v, ErrLegacyMigrationUnavailable
	}
	return v, nil
}

// Authentication and the legacy bounded decoder precede resampling. A large
// preserved source never enlarges the public profile-upload input limit.
func BuildLegacyMomentAuthorAvatar(ctx context.Context, a rm.Asset, blobs LegacyReferenceMediaBlobs) (profileavatar.Thumbnail, error) {
	derived, _, _, _, e := BuildLegacyReferenceDerivative(ctx, a, blobs)
	if e != nil {
		return profileavatar.Thumbnail{}, e
	}
	defer clear(derived)
	source, _, e := image.Decode(bytes.NewReader(derived))
	if e != nil {
		return profileavatar.Thumbnail{}, ErrLegacyReferenceMediaDerivative
	}
	b := source.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	small := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		if ctx.Err() != nil {
			return profileavatar.Thumbnail{}, ErrLegacyReferenceMediaDerivative
		}
		for x := 0; x < 256; x++ {
			r, g, b, a := source.At(x0+(2*x+1)*side/512, y0+(2*y+1)*side/512).RGBA()
			small.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - a) >> 8), uint8((g + 65535 - a) >> 8), uint8((b + 65535 - a) >> 8), 255})
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, small, &jpeg.Options{Quality: 90}) != nil {
		return profileavatar.Thumbnail{}, ErrLegacyReferenceMediaDerivative
	}
	defer clear(out.Bytes())
	thumb, e := profileavatar.Prepare(out.Bytes(), "image/jpeg")
	if e != nil {
		return profileavatar.Thumbnail{}, ErrLegacyReferenceMediaDerivative
	}
	return thumb, nil
}
