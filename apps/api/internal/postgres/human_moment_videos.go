package postgres

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyvideo"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

var _ media.HumanPrivateVideoStore = (*Store)(nil)

const privateVideoColumns = `v.id::text,v.moment_id::text,v.owner_account_id::text,v.status,v.revision,v.source_position,v.input_size,v.content_size,v.content_sha256,v.width,v.height,v.duration_ms,v.frame_count,v.audio_codec,v.poster_size,v.poster_sha256,v.validation_kind,v.antivirus_scan,v.privacy_risk,v.retain_until`

func scanPrivateVideo(row scanner) (media.PrivateVideoReceipt, error) {
	v := media.PrivateVideoReceipt{SchemaVersion: media.PrivateVideoSchema, Origin: "LEGACY_PRIVATE_IMPORT", MIME: "video/mp4"}
	e := row.Scan(&v.ID, &v.MomentID, &v.OwnerID, &v.Status, &v.Revision, &v.Position, &v.InputByteSize, &v.ByteSize, &v.SHA256, &v.Width, &v.Height, &v.DurationMS, &v.Frames, &v.AudioCodec, &v.PosterBytes, &v.PosterSHA256, &v.ValidationKind, &v.AntivirusScan, &v.PrivacyRisk, &v.RetainUntil)
	if e == nil && !v.Valid() {
		e = content.ErrUnavailable
	}
	return v, e
}

// Original provenance remains mandatory even for the deleted receipt. It is
// never sufficient without the same current native owner and private Moment.
const privateVideoProvenance = `EXISTS(SELECT 1 FROM legacy_private_video_import_items i
 JOIN legacy_native_adaptation_entities x ON x.run_id=i.run_id AND x.kind='moment' AND x.source_key=i.source_note_id::text AND x.target_id=i.moment_id::text AND x.owner_account_id=i.owner_account_id
 JOIN legacy_note_archives n ON n.source_note_id=i.source_note_id AND n.run_id=x.run_id AND n.owner_account_id=x.owner_account_id AND n.source_sha256=x.source_sha256 AND n.source_sha256=i.source_note_sha256 AND NOT n.deleted
 WHERE i.video_id=v.id AND i.run_id=v.run_id AND i.owner_account_id=v.owner_account_id AND i.moment_id=v.moment_id AND i.source_input_sha256=v.input_sha256 AND i.source_input_bytes=v.input_size AND i.content_sha256=v.content_sha256 AND i.content_bytes=v.content_size AND i.poster_sha256=v.poster_sha256 AND i.poster_bytes=v.poster_size)`

func (s *Store) privateVideoFinal(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, moment, source string, values []media.PrivateVideoReceipt) error {
	var fresh string
	var revision int64
	if e := tx.QueryRow(ctx, privateImageSourceSQL, a.ID, moment).Scan(&fresh, &revision); e != nil {
		return imageFailure(e)
	}
	if fresh != source {
		return content.ErrConflict
	}
	for _, v := range values {
		var current bool
		e := tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT EXISTS(SELECT 1 FROM private_moment_videos v JOIN media_assets asset ON asset.id=v.id AND asset.owner_account_id=v.owner_account_id CROSS JOIN cl WHERE v.id=$1 AND v.owner_account_id=$2 AND v.moment_id=$3 AND v.status=$4 AND v.revision=$5 AND(v.status='deleted' OR(v.retain_until>cl.at AND asset.state='ready_private' AND asset.mime_type='video/mp4' AND asset.sha256_hex=v.content_sha256 AND asset.byte_size=v.content_size)) AND `+privateVideoProvenance+`)`, v.ID, a.ID, moment, v.Status, v.Revision).Scan(&current)
		if e != nil {
			return content.ErrUnavailable
		}
		if !current {
			return content.ErrConflict
		}
	}
	return checkHumanMomentSession(ctx, tx, d, a.ID, s.devPhoneEnabled)
}
func (s *Store) ReadHumanPrivateVideos(ctx context.Context, d [32]byte, a identity.Actor, moment, id, part string, encode media.PrivateVideoEncode) ([]byte, error) {
	if encode == nil || (id != "" && !media.ValidPrivateImageID(id)) || (part != "" && part != "content" && part != "poster") || (part != "" && id == "") {
		return nil, content.ErrInvalid
	}
	tx, source, _, e := s.imageTx(ctx, d, a, moment)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT `+privateVideoColumns+` FROM private_moment_videos v WHERE v.owner_account_id=$1 AND v.moment_id=$2 AND($3='' OR v.id=NULLIF($3,'')::uuid) AND($3<>'' OR(v.status='ready_private' AND v.retain_until>clock_timestamp())) AND `+privateVideoProvenance+` ORDER BY v.source_position,v.id LIMIT 12 FOR SHARE OF v`, a.ID, moment, id)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	values := []media.PrivateVideoReceipt{}
	for rows.Next() {
		v, err := scanPrivateVideo(rows)
		if err != nil {
			rows.Close()
			return nil, content.ErrUnavailable
		}
		values = append(values, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, content.ErrUnavailable
	}
	if id != "" && len(values) != 1 {
		return nil, content.ErrNotFound
	}
	for _, v := range values {
		var ignored string
		if e = tx.QueryRow(ctx, `SELECT state FROM media_assets WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, v.ID, a.ID).Scan(&ignored); e != nil {
			return nil, imageFailure(e)
		}
	}
	if _, e = s.imageSession(ctx, tx, d, a); e != nil {
		return nil, e
	}
	if e = s.privateVideoFinal(ctx, tx, d, a, moment, source, values); e != nil {
		return nil, e
	}
	var payload []byte
	if part != "" {
		v := values[0]
		if v.Status != "ready_private" {
			return nil, content.ErrNotFound
		}
		column, max, size, hash := "content", legacyvideo.MaxContentBytes, v.ByteSize, v.SHA256
		if part == "poster" {
			column, max, size, hash = "poster", legacyvideo.MaxPosterBytes, v.PosterBytes, v.PosterSHA256
		}
		if e = tx.QueryRow(ctx, `SELECT `+column+` FROM private_moment_videos WHERE id=$1 AND status='ready_private' AND octet_length(`+column+`)<=$2`, id, max).Scan(&payload); e != nil {
			return nil, imageFailure(e)
		}
		if int64(len(payload)) != size || legacyvideo.Hash(payload) != hash {
			clear(payload)
			return nil, content.ErrUnavailable
		}
	}
	encoded, e := encode(values, payload)
	if e != nil {
		clear(payload)
		return nil, content.ErrUnavailable
	}
	if e = s.privateVideoFinal(ctx, tx, d, a, moment, source, values); e != nil {
		clear(payload)
		clear(encoded)
		return nil, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		clear(payload)
		clear(encoded)
		return nil, e
	}
	return encoded, nil
}
func (s *Store) DeleteHumanPrivateVideo(ctx context.Context, d [32]byte, a identity.Actor, moment, id string, revision int64) (media.PrivateVideoReceipt, error) {
	var zero media.PrivateVideoReceipt
	if !media.ValidPrivateImageID(id) || revision < 1 {
		return zero, content.ErrInvalid
	}
	tx, source, _, e := s.imageTx(ctx, d, a, moment)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback(context.Background())
	v, e := scanPrivateVideo(tx.QueryRow(ctx, `SELECT `+privateVideoColumns+` FROM private_moment_videos v WHERE v.id=$1 AND v.owner_account_id=$2 AND v.moment_id=$3 AND `+privateVideoProvenance+` FOR UPDATE OF v`, id, a.ID, moment))
	if e != nil {
		return zero, imageFailure(e)
	}
	if v.Revision != revision {
		return zero, content.ErrConflict
	}
	if v.Status != "deleted" {
		_, e = tx.Exec(ctx, `UPDATE private_moment_videos SET status='deleted',revision=revision+1,original=NULL,content=NULL,poster=NULL,updated_at=clock_timestamp() WHERE id=$1`, id)
		if e != nil {
			return zero, content.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE media_assets SET state='deleted',deleted_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2`, id, a.ID); e != nil {
			return zero, content.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,'private_video_deleted','media',$2,'allowed','PRIVATE_MOMENT_VIDEO_ATTACHMENT')`, a.ID, id); e != nil {
			return zero, content.ErrUnavailable
		}
		v.Status = "deleted"
		v.Revision++
	}
	if _, e = s.imageSession(ctx, tx, d, a); e != nil {
		return zero, e
	}
	if e = s.privateVideoFinal(ctx, tx, d, a, moment, source, []media.PrivateVideoReceipt{v}); e != nil {
		return zero, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return zero, e
	}
	return v, nil
}
