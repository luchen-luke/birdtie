package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacyvideo"
	"github.com/jackc/pgx/v5"
)

var ErrLegacyPrivateVideoDerivative = errors.New("legacy_private_video_derivative_rejected")

type LegacyPrivateVideoInput struct {
	Source, Manifest, Matched, AuthenticatedSeal []byte
	ExpectedSealSHA256                           string
}

// A controlled CLI supplies bounded local file readers. This is never a URL,
// arbitrary archive extractor, credential, or a public upload capability.
type LegacyPrivateVideoBlobs func(context.Context, legacyvideo.Record) (io.ReadCloser, error)

type LegacyPrivateVideoResult struct {
	RunID    string             `json:"runId"`
	Status   string             `json:"status"`
	Counts   legacyvideo.Counts `json:"counts"`
	Deferred map[string]int     `json:"deferred"`
}

func legacyPrivateVideoRead(ctx context.Context, tx pgx.Tx, run string) (LegacyPrivateVideoResult, string, error) {
	r := LegacyPrivateVideoResult{RunID: run, Status: "COMMITTED", Deferred: map[string]int{}}
	var digest string
	var counts []byte
	e := tx.QueryRow(ctx, `SELECT projection_sha256,counts FROM legacy_private_video_import_runs WHERE run_id=$1`, run).Scan(&digest, &counts)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(counts, &r.Counts) != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	var actual legacyvideo.Counts
	e = tx.QueryRow(ctx, `SELECT count(*),count(DISTINCT source_note_id),count(DISTINCT owner_account_id) FROM legacy_private_video_import_items WHERE run_id=$1`, run).Scan(&actual.Videos, &actual.Notes, &actual.Owners)
	if e != nil || actual != r.Counts {
		return r, "", ErrLegacyMigrationUnavailable
	}
	return r, digest, nil
}

func buildLegacyPrivateVideo(ctx context.Context, r legacyvideo.Record, blobs LegacyPrivateVideoBlobs, codec *legacyvideo.Normalizer) ([]byte, legacyvideo.Derivative, error) {
	var out legacyvideo.Derivative
	input, e := blobs(ctx, r)
	if e != nil {
		return nil, out, ErrLegacyPrivateVideoDerivative
	}
	raw, e := io.ReadAll(io.LimitReader(input, legacyvideo.MaxInputBytes+1))
	if closed := input.Close(); e == nil {
		e = closed
	}
	if e != nil || int64(len(raw)) != r.InputBytes || legacyvideo.Hash(raw) != r.InputSHA256 {
		clear(raw)
		return nil, out, ErrLegacyPrivateVideoDerivative
	}
	out, e = codec.Normalize(ctx, raw)
	if e != nil {
		clear(raw)
		out.Erase()
		return nil, out, ErrLegacyPrivateVideoDerivative
	}
	return raw, out, nil
}

// ApplyLegacyPrivateVideo requires the original preservation and committed156
// native mappings. It writes only complete private originals and scrubbed playback derivatives, no source files,
// user Session, approval, activation, public material or old URL.
func (s *LegacyMigrationImporter) ApplyLegacyPrivateVideo(ctx context.Context, run string, input LegacyPrivateVideoInput, blobs LegacyPrivateVideoBlobs, codec *legacyvideo.Normalizer) (LegacyPrivateVideoResult, error) {
	unknown := LegacyPrivateVideoResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || blobs == nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := legacyvideo.Build(run, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
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
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,159))`, legacymigration.SourceSystem); e != nil {
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
	var marker string
	if e = tx.QueryRow(ctx, `SELECT birdtie_legacy_private_video_contract()`).Scan(&marker); e != nil || marker != "private-moment-video-v1" {
		return unknown, ErrLegacyMigrationGuard
	}

	previous, digest, e := legacyPrivateVideoRead(ctx, tx, run)
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
	if codec.Validate() != nil {
		return unknown, ErrLegacyMigrationGuard
	}
	if len(p.Records) == 0 {
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
	_, e = tx.Exec(ctx, `INSERT INTO legacy_private_video_import_runs(run_id,protocol,projection_sha256,source_file_sha256,manifest_file_sha256,matcher_file_sha256,authenticated_seal_sha256,counts)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, run, legacyvideo.Protocol, p.ProjectionSHA256, p.SourceFileSHA256, p.ManifestFileSHA256, p.MatcherFileSHA256, p.AuthenticatedSealSHA256, counts)
	if e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	for _, r := range p.Records {
		v := targets[r.SourceNoteID]
		raw, derived, err := buildLegacyPrivateVideo(ctx, r, blobs, codec)
		if err != nil {
			return unknown, err
		}
		_, e = tx.Exec(ctx, `INSERT INTO media_assets(id,owner_account_id,storage_key,mime_type,byte_size,sha256_hex,state)VALUES($1::uuid,$2,'legacy-private-video/'||($1::uuid)::text,'video/mp4',$3,$4,'ready_private')`, r.VideoID, v.owner, len(derived.Content), derived.ContentSHA256)
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO private_moment_videos(id,run_id,owner_account_id,moment_id,source_position,input_size,input_sha256,original,content,poster,content_size,content_sha256,poster_size,poster_sha256,width,height,duration_ms,frame_count,audio_codec,audio_packet_count,audio_packet_sha256,validation_kind,antivirus_scan,privacy_risk,status,revision,retain_until)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,'FULL_VIDEO_DECODE_CANONICAL_REENCODE_V1','NOT_RUN','UNKNOWN','ready_private',1,'9999-12-31T00:00:00Z'::timestamptz)`, r.VideoID, run, v.owner, v.id, r.Position, r.InputBytes, r.InputSHA256, raw, derived.Content, derived.Poster, len(derived.Content), derived.ContentSHA256, len(derived.Poster), derived.PosterSHA256, derived.Width, derived.Height, derived.DurationMS, derived.Frames, derived.AudioCodec, derived.AudioPackets, derived.AudioPacketSHA256)
		}
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO legacy_private_video_import_items(run_id,source_note_id,source_media_id,source_note_sha256,source_media_sha256,source_input_sha256,source_input_bytes,source_position,owner_account_id,moment_id,video_id,content_sha256,content_bytes,poster_sha256,poster_bytes)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, run, r.SourceNoteID, r.SourceMediaID, r.SourceNoteSHA256, r.SourceMediaSHA256, r.InputSHA256, r.InputBytes, r.Position, v.owner, v.id, r.VideoID, derived.ContentSHA256, len(derived.Content), derived.PosterSHA256, len(derived.Poster))
		}
		clear(raw)
		derived.Erase()
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}

	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	result, digest, e := legacyPrivateVideoRead(ctx, tx, run)
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
func (s *LegacyMigrationImporter) InspectLegacyPrivateVideo(ctx context.Context, run string) (LegacyPrivateVideoResult, error) {
	unknown := LegacyPrivateVideoResult{RunID: run, Status: "UNKNOWN"}
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
	r, _, e := legacyPrivateVideoRead(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if e = tx.Commit(ctx); e != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return r, nil
}
