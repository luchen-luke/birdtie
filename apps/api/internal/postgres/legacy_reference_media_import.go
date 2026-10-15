package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
	"image"
	"io"
)

type LegacyReferenceMediaResult struct {
	RunID  string    `json:"runId"`
	Status string    `json:"status"`
	Counts rm.Counts `json:"counts"`
}
type LegacyReferenceMediaBlobs func(context.Context, rm.Asset) (io.ReadCloser, error)

var ErrLegacyReferenceMediaDerivative = errors.New("legacy_reference_media_derivative_rejected")

func BuildLegacyReferenceDerivative(ctx context.Context, a rm.Asset, blobs LegacyReferenceMediaBlobs) ([]byte, string, int, int, error) {
	r, e := blobs(ctx, a)
	if e != nil || r == nil {
		return nil, "", 0, 0, ErrLegacyReferenceMediaDerivative
	}
	raw, e := io.ReadAll(io.LimitReader(r, a.InputBytes+1))
	closed := r.Close()
	defer clear(raw)
	if e != nil || closed != nil || int64(len(raw)) != a.InputBytes || legacymedia.Hash(raw) != a.InputSHA256 {
		return nil, "", 0, 0, ErrLegacyReferenceMediaDerivative
	}
	if a.System {
		return append([]byte(nil), raw...), a.MIME, 0, 0, nil
	}
	out, mime, e := (&media.LocalImageDerivativeBuilder{}).BuildLegacyPrivateImage(ctx, bytes.NewReader(raw), a.MIME)
	if e != nil {
		return nil, "", 0, 0, ErrLegacyReferenceMediaDerivative
	}
	defer out.Close()
	b, e := io.ReadAll(io.LimitReader(out, media.MaxImageOutputBytes+1))
	if e != nil || len(b) < 1 || len(b) > media.MaxImageOutputBytes {
		clear(b)
		return nil, "", 0, 0, ErrLegacyReferenceMediaDerivative
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || (format != "jpeg" && format != "png") {
		clear(b)
		return nil, "", 0, 0, ErrLegacyReferenceMediaDerivative
	}
	return b, mime, cfg.Width, cfg.Height, nil
}
func readLegacyReferenceMedia(ctx context.Context, tx pgx.Tx, run string) (LegacyReferenceMediaResult, string, error) {
	r := LegacyReferenceMediaResult{RunID: run, Status: "COMMITTED"}
	var raw []byte
	var hash string
	e := tx.QueryRow(ctx, `SELECT counts,projection_sha256 FROM legacy_reference_media_runs WHERE run_id=$1`, run).Scan(&raw, &hash)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, "", ErrLegacyMigrationNotFound
	}
	if e != nil || json.Unmarshal(raw, &r.Counts) != nil {
		return r, "", ErrLegacyMigrationUnavailable
	}
	var physical, reused, refs, tombs, art int
	e = tx.QueryRow(ctx, `SELECT(SELECT count(*)FROM legacy_reference_media_assets a JOIN private_reference_image_materials m ON m.id=a.material_id WHERE a.run_id=$1 AND m.backing_image_id IS NULL),(SELECT count(*)FROM legacy_reference_media_assets a JOIN private_reference_image_materials m ON m.id=a.material_id WHERE a.run_id=$1 AND m.backing_image_id IS NOT NULL),(SELECT count(*)FROM legacy_reference_media_relations WHERE run_id=$1),(SELECT count(*)FROM legacy_reference_media_relations WHERE run_id=$1 AND tombstone),(SELECT count(*)FROM legacy_system_artwork_archives WHERE run_id=$1)`, run).Scan(&physical, &reused, &refs, &tombs, &art)
	if e != nil || physical != r.Counts.PrivateImages || reused != r.Counts.ReusedImages || refs != r.Counts.PlaceRelations+r.Counts.JourneyMedia+r.Counts.MomentCovers || tombs != r.Counts.RelationTombstones || art != r.Counts.SystemArtworkArchives {
		return r, "", ErrLegacyMigrationConflict
	}
	return r, hash, nil
}
func (s *LegacyMigrationImporter) InspectLegacyReferenceMedia(ctx context.Context, run string) (LegacyReferenceMediaResult, error) {
	unknown := LegacyReferenceMediaResult{RunID: run, Status: "UNKNOWN"}
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
	r, _, e := readLegacyReferenceMedia(ctx, tx, run)
	if e != nil {
		return unknown, e
	}
	if tx.Commit(ctx) != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return r, nil
}
func (s *LegacyMigrationImporter) ApplyLegacyReferenceMedia(ctx context.Context, run string, input LegacyPrivateMediaInput, blobs LegacyReferenceMediaBlobs) (LegacyReferenceMediaResult, error) {
	unknown := LegacyReferenceMediaResult{RunID: run, Status: "UNKNOWN"}
	if s == nil || s.database == nil || ctx == nil || ctx.Err() != nil || blobs == nil {
		return unknown, ErrLegacyMigrationGuard
	}
	p, e := rm.Build(run, input.Source, input.Manifest, input.Matched, input.AuthenticatedSeal, input.ExpectedSealSHA256)
	if e != nil {
		return unknown, ErrLegacyMigrationInvalid
	}
	base, e := legacymigration.BuildPlan(run, input.Source, input.Manifest)
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
	var marker string
	if tx.QueryRow(ctx, `SELECT birdtie_legacy_reference_media_contract()`).Scan(&marker) != nil || marker != "legacy-reference-media-v1" {
		return unknown, ErrLegacyMigrationGuard
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,167))`, legacymigration.SourceSystem); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	original, e := legacyMigrationReadRun(ctx, tx, run)
	if e != nil || !legacyMigrationSameRun(original, base) {
		return unknown, ErrLegacyMigrationConflict
	}
	if _, _, e = nativeAdaptationRead(ctx, tx, run); e != nil {
		return unknown, e
	}
	old, hash, e := readLegacyReferenceMedia(ctx, tx, run)
	if e == nil {
		if hash != p.ProjectionSHA256 || old.Counts != p.Counts {
			return unknown, ErrLegacyMigrationConflict
		}
		if tx.Commit(ctx) != nil {
			return unknown, ErrLegacyMigrationUnknown
		}
		return old, nil
	}
	if !errors.Is(e, ErrLegacyMigrationNotFound) {
		return unknown, e
	}
	owners := map[string]string{}
	for _, a := range p.Assets {
		if a.System {
			continue
		}
		if _, ok := owners[a.OwnerSourceID]; ok {
			continue
		}
		var owner string
		e = tx.QueryRow(ctx, `SELECT u.target_id::text FROM legacy_entity_mappings u JOIN accounts a ON a.id=u.target_id WHERE u.run_id=$1 AND u.entity_kind='user' AND u.source_id=$2::uuid AND u.owner_account_id=u.target_id AND a.account_type='person' AND a.status IN('active','suspended') FOR SHARE OF a`, run, a.OwnerSourceID).Scan(&owner)
		if e != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		owners[a.OwnerSourceID] = owner
	}
	// Tombstones still retain their exact original owner, without material or activation.
	for _, r := range p.References {
		if owners[r.OwnerSourceID] != "" {
			continue
		}
		var owner string
		if tx.QueryRow(ctx, `SELECT target_id::text FROM legacy_entity_mappings WHERE run_id=$1 AND entity_kind='user' AND source_id=$2::uuid AND owner_account_id=target_id`, run, r.OwnerSourceID).Scan(&owner) != nil {
			return unknown, ErrLegacyMigrationConflict
		}
		owners[r.OwnerSourceID] = owner
	}
	targets := map[string]string{}
	for _, r := range p.References {
		var id string
		switch r.TargetType {
		case "PLACE":
			e = tx.QueryRow(ctx, `SELECT p.id::text FROM places p JOIN(SELECT run_id,source_key,source_sha256,target_id FROM legacy_native_adaptation_entities WHERE kind='place' UNION ALL SELECT run_id,source_key,source_sha256,target_id::text FROM legacy_native_context_entities WHERE kind='unplaced_place')x ON x.target_id=p.id::text WHERE x.run_id=$1 AND x.source_key=$2 AND x.source_sha256=$3 AND p.publication_status IN('draft','published') AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` FOR SHARE OF p`, run, r.ParentSourceID, r.ParentSHA256).Scan(&id)
		case "MOMENT":
			e = tx.QueryRow(ctx, `SELECT m.id::text FROM legacy_native_adaptation_entities x JOIN legacy_note_archives n ON n.run_id=x.run_id AND n.source_note_id::text=x.source_key AND n.source_sha256=x.source_sha256 AND n.owner_account_id=x.owner_account_id JOIN moments m ON m.id::text=x.target_id AND m.author_account_id=x.owner_account_id WHERE x.run_id=$1 AND x.kind='moment' AND x.source_key=$2 AND x.source_sha256=$3 AND x.owner_account_id=$4 AND NOT n.deleted AND m.status<>'withdrawn' FOR SHARE OF m`, run, r.ParentSourceID, r.ParentSHA256, owners[r.OwnerSourceID]).Scan(&id)
		case "JOURNEY":
			e = tx.QueryRow(ctx, `SELECT j.id::text FROM legacy_journey_adaptations x JOIN legacy_personal_archives ar ON ar.run_id=x.run_id AND ar.kind='journey' AND ar.source_key=x.source_key AND ar.source_sha256=x.source_sha256 AND ar.owner_account_id=x.owner_account_id JOIN journeys j ON j.id=x.target_id AND j.owner_account_id=x.owner_account_id WHERE x.run_id=$1 AND x.source_key=$2 AND x.source_sha256=$3 AND x.owner_account_id=$4 AND NOT ar.deleted AND j.status<>'withdrawn' FOR SHARE OF j`, run, r.ParentSourceID, r.ParentSHA256, owners[r.OwnerSourceID]).Scan(&id)
		default:
			return unknown, ErrLegacyMigrationInvalid
		}
		if e != nil {
			if r.Tombstone && errors.Is(e, pgx.ErrNoRows) {
				id = ""
			} else {
				return unknown, ErrLegacyMigrationConflict
			}
		}
		targets[r.ID] = id
	}
	counts, _ := json.Marshal(p.Counts)
	if _, e = tx.Exec(ctx, `INSERT INTO legacy_reference_media_runs(run_id,protocol,source_file_sha256,manifest_file_sha256,snapshot_sha256,matcher_file_sha256,seal_sha256,projection_sha256,counts)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, run, rm.Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.MatcherFileSHA256, p.SealSHA256, p.ProjectionSHA256, counts); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	for _, a := range p.Assets {
		var content []byte
		mime := a.MIME
		w, h := 0, 0
		backing := ""
		owner := owners[a.OwnerSourceID]
		if a.ReuseNoteID != "" {
			var sourceHash string
			e = tx.QueryRow(ctx, `SELECT i.id::text,i.derivative,i.mime_type,f.source_media_sha256 FROM(SELECT run_id,source_note_id,source_media_id,source_media_sha256,owner_account_id,moment_id,image_id FROM legacy_private_media_import_items UNION ALL SELECT run_id,source_note_id,source_media_id,source_media_sha256,owner_account_id,moment_id,image_id FROM legacy_private_image_recovery_items)f JOIN private_moment_images i ON i.id=f.image_id AND i.owner_account_id=f.owner_account_id AND i.moment_id=f.moment_id JOIN moments m ON m.id=i.moment_id AND m.author_account_id=i.owner_account_id JOIN media_assets ma ON ma.id=i.id AND ma.owner_account_id=i.owner_account_id AND ma.state='ready_private' WHERE f.run_id=$1 AND f.source_media_id=$2 AND f.source_note_id=$3 AND f.owner_account_id=$4 AND i.status='ready_private' AND i.retain_until>clock_timestamp() AND m.visibility='private' AND m.status='draft' FOR SHARE OF i,m,ma`, run, a.SourceID, a.ReuseNoteID, owner).Scan(&backing, &content, &mime, &sourceHash)
			if e != nil || sourceHash != a.SourceSHA256 {
				return unknown, ErrLegacyMigrationConflict
			}
			cfg, _, ce := image.DecodeConfig(bytes.NewReader(content))
			if ce != nil {
				return unknown, ErrLegacyReferenceMediaDerivative
			}
			w, h = cfg.Width, cfg.Height
		} else {
			content, mime, w, h, e = BuildLegacyReferenceDerivative(ctx, a, blobs)
			if e != nil {
				return unknown, e
			}
		}
		if a.System {
			_, e = tx.Exec(ctx, `INSERT INTO legacy_reference_media_assets(run_id,source_media_id,source_media_sha256,source_owner_id,input_sha256,input_bytes,system_archive,original,validation_kind)VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,true,$7,'AUTHENTICATED_SYSTEM_ARCHIVE_ONLY')`, run, a.SourceID, a.SourceSHA256, a.OwnerSourceID, a.InputSHA256, a.InputBytes, content)
		} else {
			body := content
			if backing != "" {
				body = nil
			}
			_, e = tx.Exec(ctx, `INSERT INTO private_reference_image_materials(id,owner_account_id,backing_image_id,mime_type,byte_size,sha256,width,height,content,status)VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7,$8,$9,'READY_PRIVATE')`, a.ID, owner, backing, mime, len(content), legacymedia.Hash(content), w, h, body)
			if e == nil {
				validation := "STATIC_IMAGE_DECODE_REENCODE_V1"
				if backing != "" {
					validation = "EXISTING_NATIVE_PRIVATE_DERIVATIVE"
				}
				_, e = tx.Exec(ctx, `INSERT INTO legacy_reference_media_assets(run_id,source_media_id,source_media_sha256,source_owner_id,owner_account_id,material_id,input_sha256,input_bytes,system_archive,validation_kind)VALUES($1,$2,$3,$4,$5,$6,$7,$8,false,$9)`, run, a.SourceID, a.SourceSHA256, a.OwnerSourceID, owner, a.ID, a.InputSHA256, a.InputBytes, validation)
			}
		}
		clear(content)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, r := range p.References {
		_, e = tx.Exec(ctx, `INSERT INTO legacy_reference_media_relations(id,run_id,kind,source_key,source_sha256,source_parent_id,source_parent_sha256,source_media_id,source_owner_id,owner_account_id,target_type,target_id,source_type,review_status,rights_basis,attribution,source_note_id,source_position,source_is_cover,source_publishable,tombstone)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,'')::uuid,$13,$14,$15,$16,NULLIF($17,'')::uuid,$18,$19,$20,$21)`, r.ID, run, r.Kind, r.SourceID, r.SourceSHA256, r.ParentSourceID, r.ParentSHA256, r.AssetSourceID, r.OwnerSourceID, owners[r.OwnerSourceID], r.TargetType, targets[r.ID], r.SourceType, r.ReviewStatus, r.RightsBasis, r.Attribution, r.SourceNoteID, r.Position, r.IsCover, r.Publishable, r.Tombstone)
		if e != nil {
			return unknown, legacyMigrationFailure(e)
		}
		material := ""
		status := "TOMBSTONE"
		if !r.Tombstone {
			material = rm.StableID("material", r.AssetSourceID)
			status = "ACTIVE"
		}
		if _, e = tx.Exec(ctx, `INSERT INTO private_reference_image_links(id,material_id,status)VALUES($1,NULLIF($2,'')::uuid,$3)`, r.ID, material, status); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	for _, a := range p.Artworks {
		if _, e = tx.Exec(ctx, `INSERT INTO legacy_system_artwork_archives(run_id,source_geo_id,source_geo_sha256,source_media_id,source_version,source_owner_id)VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid)`, run, a.GeoID, a.GeoSHA256, a.SourceID, a.Version, a.OwnerSourceID); e != nil {
			return unknown, legacyMigrationFailure(e)
		}
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return unknown, legacyMigrationFailure(e)
	}
	result, hash, e := readLegacyReferenceMedia(ctx, tx, run)
	if e != nil || hash != p.ProjectionSHA256 {
		return unknown, ErrLegacyMigrationConflict
	}
	if e = legacyMigrationCheckTarget(ctx, tx, s.expected); e != nil {
		return unknown, e
	}
	if tx.Commit(ctx) != nil || ctx.Err() != nil {
		return unknown, ErrLegacyMigrationUnknown
	}
	return result, nil
}
