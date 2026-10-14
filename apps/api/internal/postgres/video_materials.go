package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

var _ media.VideoMaterialRepository = (*Store)(nil)

const videoColumns = `v.id::text,v.owner_account_id::text,v.operation_id::text,v.mime_type,v.byte_size,v.input_sha256,v.duration_ms,v.purpose,v.storage_policy_id,v.status,v.revision,v.width,v.height,v.failure_code,v.upload_expires_at,v.retain_until,v.quarantine_key,v.private_key,v.approval_session_id::text,v.approval_generation,v.approval_session_digest,COALESCE(v.lease_id::text,''),v.lease_until,v.cleanup_pending`

func scanVideo(row scanner) (media.VideoMaterialRecord, error) {
	var v media.VideoMaterialRecord
	var digest []byte
	var lease *time.Time
	r := &v.Receipt
	e := row.Scan(&r.ID, &r.OwnerID, &r.OperationID, &r.MIME, &r.ByteSize, &r.SHA256, &r.DurationMS, &r.Purpose, &r.StoragePolicyID, &r.Status, &r.Revision, &r.Width, &r.Height, &r.FailureCode, &r.UploadExpiresAt, &r.RetainUntil, &v.QuarantineKey, &v.PrivateKey, &v.ApprovalSessionID, &v.ApprovalGeneration, &digest, &v.LeaseID, &lease, &v.CleanupPending)
	if e == nil {
		copy(v.SessionDigest[:], digest)
		r.ConfirmedPrivateSave = true
		if lease != nil {
			v.LeaseUntil = *lease
		}
	}
	return v, e
}
func (s *Store) videoSession(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor) (string, int64, error) {
	var id string
	var generation int64
	e := tx.QueryRow(ctx, `SELECT id::text,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3 OR authentication_method<>'dev_phone') FOR SHARE`, a.ID, d[:], s.devPhoneEnabled).Scan(&id, &generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", 0, identity.ErrUnauthorized
	}
	if e != nil {
		return "", 0, content.ErrUnavailable
	}
	return id, generation, nil
}
func (s *Store) videoCommit(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor) error {
	if _, _, e := s.videoSession(ctx, tx, d, a); e != nil {
		return e
	}
	return s.finishHumanMomentTx(ctx, tx, d, a.ID)
}
func (s *Store) ReserveHumanVideo(ctx context.Context, d [32]byte, a identity.Actor, in media.VideoMaterialInput) (media.VideoMaterialRecord, error) {
	if e := media.ValidateVideoMaterialInput(in); e != nil {
		return media.VideoMaterialRecord{}, e
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return media.VideoMaterialRecord{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, a.ID+":"+in.OperationID); e != nil {
		return media.VideoMaterialRecord{}, content.ErrUnavailable
	}
	v, e := scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.owner_account_id=$1 AND v.operation_id=$2 FOR UPDATE`, a.ID, in.OperationID))
	if e == nil {
		if v.Receipt.ByteSize != in.ByteSize || v.Receipt.SHA256 != in.SHA256 || v.Receipt.DurationMS != in.DurationMS || v.Receipt.Purpose != in.Purpose || v.Receipt.StoragePolicyID != in.StoragePolicyID {
			return v, content.ErrConflict
		}
		session, generation, e := s.videoSession(ctx, tx, d, a)
		if e != nil {
			return v, e
		}
		if v.Receipt.Status == "quarantine" && (v.ApprovalSessionID != session || v.ApprovalGeneration != generation || !v.Receipt.UploadExpiresAt.After(time.Now().UTC())) {
			return v, content.ErrConflict
		}
		if e = s.videoCommit(ctx, tx, d, a); e != nil {
			return media.VideoMaterialRecord{}, e
		}
		return v, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrUnavailable
	}
	var id, key, privateKey string
	if e = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text,'vq-'||gen_random_uuid()::text,'vp-'||gen_random_uuid()::text`).Scan(&id, &key, &privateKey); e != nil {
		return v, content.ErrUnavailable
	}
	session, generation, e := s.videoSession(ctx, tx, d, a)
	if e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO media_assets(id,owner_account_id,storage_key,mime_type,byte_size,sha256_hex,state) VALUES($1,$2,$3,'video/mp4',$4,$5,'quarantine')`, id, a.ID, key, in.ByteSize, in.SHA256); e != nil {
		return v, content.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `INSERT INTO private_video_materials(id,owner_account_id,operation_id,input_sha256,byte_size,duration_ms,mime_type,purpose,storage_policy_id,quarantine_key,private_key,approval_session_id,approval_session_digest,approval_generation,upload_expires_at,retain_until) VALUES($1,$2,$3,$4,$5,$6,'video/mp4',$7,$8,$9,$10,$11,$12,$13,clock_timestamp()+interval '5 minutes',clock_timestamp()+interval '30 days')`, id, a.ID, in.OperationID, in.SHA256, in.ByteSize, in.DurationMS, in.Purpose, in.StoragePolicyID, key, privateKey, session, d[:], generation); e != nil {
		return v, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, a.ID, "video_material", id, "create_private_upload"); e != nil {
		return v, e
	}
	v, e = scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1`, id))
	if e != nil {
		return v, content.ErrUnavailable
	}
	if e = s.videoCommit(ctx, tx, d, a); e != nil {
		return media.VideoMaterialRecord{}, e
	}
	return v, nil
}
func (s *Store) QueueHumanVideo(ctx context.Context, d [32]byte, a identity.Actor, id string, revision int64) (media.VideoMaterialReceipt, error) {
	if !media.ValidPrivateImageID(id) || revision < 1 {
		return media.VideoMaterialReceipt{}, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1 AND v.owner_account_id=$2 FOR UPDATE`, id, a.ID))
	if e != nil {
		return media.VideoMaterialReceipt{}, imageFailure(e)
	}
	session, generation, e := s.videoSession(ctx, tx, d, a)
	if e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	if v.Receipt.Status == "quarantine" {
		if v.Receipt.Revision != revision || v.ApprovalSessionID != session || v.ApprovalGeneration != generation {
			return media.VideoMaterialReceipt{}, content.ErrConflict
		}
		var valid bool
		if e = tx.QueryRow(ctx, `SELECT upload_expires_at>clock_timestamp() AND retain_until>clock_timestamp() FROM private_video_materials WHERE id=$1`, id).Scan(&valid); e != nil {
			return media.VideoMaterialReceipt{}, content.ErrUnavailable
		}
		if !valid {
			return media.VideoMaterialReceipt{}, content.ErrConflict
		}
		if _, e = tx.Exec(ctx, `UPDATE private_video_materials SET status='queued',revision=revision+1,updated_at=now() WHERE id=$1`, id); e != nil {
			return media.VideoMaterialReceipt{}, content.ErrUnavailable
		}
		if e = collectionAuditTx(ctx, tx, a.ID, "video_material", id, "queue_private_scan"); e != nil {
			return media.VideoMaterialReceipt{}, e
		}
	}
	if v.Receipt.Status == "deleted" || v.Receipt.Status == "deleting" || v.Receipt.Status == "rejected" {
		return media.VideoMaterialReceipt{}, content.ErrConflict
	}
	v, e = scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1`, id))
	if e != nil {
		return media.VideoMaterialReceipt{}, content.ErrUnavailable
	}
	if e = s.videoCommit(ctx, tx, d, a); e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	return v.Receipt, nil
}
func (s *Store) ReadHumanVideo(ctx context.Context, d [32]byte, a identity.Actor, id string, operation bool, encode func(media.VideoMaterialRecord) ([]byte, error)) ([]byte, error) {
	if !media.ValidPrivateImageID(id) || encode == nil {
		return nil, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	field := "id"
	if operation {
		field = "operation_id"
	}
	v, e := scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.`+field+`=$1 AND v.owner_account_id=$2 FOR SHARE`, id, a.ID))
	if e != nil {
		return nil, imageFailure(e)
	}
	// Lock the generic asset as well: other media withdrawal paths cannot race
	// a private content read. Recheck the clock after provider I/O below.
	var assetState string
	if e = tx.QueryRow(ctx, `SELECT state FROM media_assets WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, v.Receipt.ID, a.ID).Scan(&assetState); e != nil {
		return nil, imageFailure(e)
	}
	check := func() error {
		var active bool
		e := tx.QueryRow(ctx, `SELECT (retain_until>clock_timestamp() OR status='deleted') AND (status<>'ready_private' OR $2='ready_private') FROM private_video_materials WHERE id=$1`, v.Receipt.ID, assetState).Scan(&active)
		if e != nil {
			return content.ErrUnavailable
		}
		if !active {
			return content.ErrNotFound
		}
		return nil
	}
	if e = check(); e != nil {
		return nil, e
	}
	raw, e := encode(v)
	if e != nil {
		return nil, e
	}
	if e = check(); e != nil {
		return nil, e
	}
	if e = s.videoCommit(ctx, tx, d, a); e != nil {
		return nil, e
	}
	e = txContext(ctx)
	if e != nil {
		return nil, e
	}
	return raw, nil
}
func txContext(ctx context.Context) error {
	if ctx.Err() != nil {
		return content.ErrUnavailable
	}
	return nil
}
func (s *Store) ListHumanVideos(ctx context.Context, d [32]byte, a identity.Actor, cursor string) (media.VideoMaterialPage, error) {
	if cursor != "" && !media.ValidPrivateImageID(cursor) {
		return media.VideoMaterialPage{}, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return media.VideoMaterialPage{}, e
	}
	defer tx.Rollback(context.Background())
	if cursor != "" {
		var id string
		if e = tx.QueryRow(ctx, `SELECT id FROM private_video_materials WHERE id=$1 AND owner_account_id=$2`, cursor, a.ID).Scan(&id); e != nil {
			return media.VideoMaterialPage{}, imageFailure(e)
		}
	}
	rows, e := tx.Query(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.owner_account_id=$1 AND v.status<>'deleted' AND v.retain_until>clock_timestamp() AND ($2='' OR (v.created_at,v.id)<(SELECT created_at,id FROM private_video_materials WHERE id=NULLIF($2,'')::uuid AND owner_account_id=$1)) ORDER BY v.created_at DESC,v.id DESC LIMIT 21 FOR SHARE`, a.ID, cursor)
	if e != nil {
		return media.VideoMaterialPage{}, content.ErrUnavailable
	}
	page := media.VideoMaterialPage{Items: []media.VideoMaterialReceipt{}}
	for rows.Next() {
		v, e := scanVideo(rows)
		if e != nil {
			rows.Close()
			return page, content.ErrUnavailable
		}
		page.Items = append(page.Items, v.Receipt)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return page, content.ErrUnavailable
	}
	if len(page.Items) > 20 {
		page.Items = page.Items[:20]
		page.NextCursor = page.Items[19].ID
	}
	if e = s.videoCommit(ctx, tx, d, a); e != nil {
		return media.VideoMaterialPage{}, e
	}
	return page, nil
}
func (s *Store) DeleteHumanVideo(ctx context.Context, d [32]byte, a identity.Actor, id string, revision int64) (media.VideoMaterialReceipt, error) {
	if !media.ValidPrivateImageID(id) || revision < 1 {
		return media.VideoMaterialReceipt{}, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1 AND v.owner_account_id=$2 FOR UPDATE`, id, a.ID))
	if e != nil {
		return media.VideoMaterialReceipt{}, imageFailure(e)
	}
	if v.Receipt.Status != "deleting" && v.Receipt.Status != "deleted" {
		if v.Receipt.Revision != revision {
			return media.VideoMaterialReceipt{}, content.ErrConflict
		}
		// A revoked scanning lease remains a cleanup barrier until all bounded
		// provider I/O from that worker has ended. The revision fences its result.
		if _, e = tx.Exec(ctx, `UPDATE private_video_materials SET status='deleting',revision=revision+1,cleanup_pending=true,lease_id=CASE WHEN status='scanning' THEN COALESCE(lease_id,gen_random_uuid()) ELSE NULL END,lease_until=CASE WHEN status='scanning' THEN GREATEST(lease_until,clock_timestamp()+interval '3 minutes') ELSE NULL END,updated_at=now() WHERE id=$1`, id); e != nil {
			return media.VideoMaterialReceipt{}, content.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `UPDATE media_assets SET state='deleted',deleted_at=clock_timestamp() WHERE id=$1`, id); e != nil {
			return media.VideoMaterialReceipt{}, content.ErrUnavailable
		}
		if e = collectionAuditTx(ctx, tx, a.ID, "video_material", id, "delete_private_video_requested"); e != nil {
			return media.VideoMaterialReceipt{}, e
		}
	}
	v, e = scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1`, id))
	if e != nil {
		return media.VideoMaterialReceipt{}, content.ErrUnavailable
	}
	if e = s.videoCommit(ctx, tx, d, a); e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	return v.Receipt, nil
}

const videoApprovalCurrentSQL = `SELECT EXISTS(SELECT 1 FROM private_video_materials v JOIN accounts a ON a.id=v.owner_account_id JOIN sessions ss ON ss.id=v.approval_session_id WHERE v.id=$1 AND v.lease_id=$2::uuid AND v.revision=$3 AND v.status='scanning' AND v.lease_until>clock_timestamp() AND v.retain_until>clock_timestamp() AND v.upload_expires_at>clock_timestamp() AND a.account_type='person' AND a.status='active' AND ss.account_id=a.id AND ss.token_sha256=v.approval_session_digest AND ss.authorization_generation=v.approval_generation AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp() AND ($4 OR ss.authentication_method<>'dev_phone'))`

func (s *Store) ClaimVideoJob(ctx context.Context) (media.VideoMaterialRecord, error) {
	if s == nil || s.pool == nil {
		return media.VideoMaterialRecord{}, content.ErrUnavailable
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return media.VideoMaterialRecord{}, content.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	// Retention/unused upload expiry fences reads before later physical cleanup.
	if _, e = tx.Exec(ctx, `WITH expired AS(SELECT id FROM private_video_materials WHERE status NOT IN('deleted','deleting') AND (retain_until<=clock_timestamp() OR (status='quarantine' AND upload_expires_at<=clock_timestamp())) ORDER BY created_at,id LIMIT 10 FOR UPDATE SKIP LOCKED), changed AS(UPDATE private_video_materials v SET status='deleting',revision=revision+1,cleanup_pending=true,lease_id=CASE WHEN v.status='scanning' THEN COALESCE(v.lease_id,gen_random_uuid()) ELSE NULL END,lease_until=CASE WHEN v.status='scanning' THEN GREATEST(v.lease_until,clock_timestamp()+interval '3 minutes') ELSE NULL END,updated_at=now() FROM expired WHERE v.id=expired.id RETURNING v.id) UPDATE media_assets m SET state='deleted',deleted_at=clock_timestamp() FROM changed WHERE m.id=changed.id`); e != nil {
		return media.VideoMaterialRecord{}, content.ErrUnavailable
	}
	v, e := scanVideo(tx.QueryRow(ctx, `WITH next AS(SELECT id FROM private_video_materials WHERE (status='queued' OR (status='scanning' AND lease_until<=clock_timestamp()) OR cleanup_pending) AND (lease_until IS NULL OR lease_until<=clock_timestamp()) ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE private_video_materials v SET status=CASE WHEN v.cleanup_pending THEN v.status ELSE 'scanning' END,revision=v.revision+1,lease_id=gen_random_uuid(),lease_until=clock_timestamp()+interval '3 minutes',updated_at=now() FROM next WHERE v.id=next.id RETURNING `+videoColumns))
	if errors.Is(e, pgx.ErrNoRows) {
		return v, content.ErrNotFound
	}
	if e != nil {
		return v, content.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return media.VideoMaterialRecord{}, content.ErrUnavailable
	}
	return v, nil
}
func (s *Store) VideoJobCurrent(ctx context.Context, v media.VideoMaterialRecord) error {
	if s == nil || s.pool == nil {
		return content.ErrUnavailable
	}
	var valid bool
	e := s.pool.QueryRow(ctx, videoApprovalCurrentSQL, v.Receipt.ID, v.LeaseID, v.Receipt.Revision, s.devPhoneEnabled).Scan(&valid)
	if e != nil {
		return content.ErrUnavailable
	}
	if !valid {
		return identity.ErrUnauthorized
	}
	return nil
}
func (s *Store) FinishVideoJob(ctx context.Context, v media.VideoMaterialRecord, descriptor media.VideoDescriptor) (media.VideoMaterialReceipt, error) {
	if descriptor.Width < 1 || descriptor.Height < 1 || descriptor.Width > 1920 || descriptor.Height > 1920 || int64(descriptor.Width)*int64(descriptor.Height) > 1920*1080 || descriptor.DurationMS < 1 || descriptor.DurationMS > media.MaxVideoDurationMS {
		return media.VideoMaterialReceipt{}, content.ErrInvalid
	}
	a := identity.Actor{ID: v.Receipt.OwnerID, AccountType: "person"}
	tx, e := s.beginHumanMomentTx(ctx, v.SessionDigest, a)
	if e != nil {
		_ = s.RejectVideoJob(ctx, v, "source_retired")
		return media.VideoMaterialReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	current, e := scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1 FOR UPDATE`, v.Receipt.ID))
	if e != nil {
		return media.VideoMaterialReceipt{}, imageFailure(e)
	}
	if current.LeaseID != v.LeaseID || current.Receipt.Revision != v.Receipt.Revision || current.Receipt.Status != "scanning" {
		return media.VideoMaterialReceipt{}, content.ErrConflict
	}
	if _, _, e = s.videoSession(ctx, tx, v.SessionDigest, a); e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	var valid bool
	if e = tx.QueryRow(ctx, videoApprovalCurrentSQL, v.Receipt.ID, v.LeaseID, v.Receipt.Revision, s.devPhoneEnabled).Scan(&valid); e != nil {
		return media.VideoMaterialReceipt{}, content.ErrUnavailable
	}
	if !valid {
		return media.VideoMaterialReceipt{}, identity.ErrUnauthorized
	}
	if _, e = tx.Exec(ctx, `UPDATE private_video_materials SET status='ready_private',revision=revision+1,width=$2,height=$3,duration_ms=$4,cleanup_pending=true,lease_id=NULL,lease_until=NULL,updated_at=now() WHERE id=$1`, v.Receipt.ID, descriptor.Width, descriptor.Height, descriptor.DurationMS); e != nil {
		return media.VideoMaterialReceipt{}, content.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `UPDATE media_assets SET state='ready_private',storage_key=$2 WHERE id=$1`, v.Receipt.ID, v.PrivateKey); e != nil {
		return media.VideoMaterialReceipt{}, content.ErrUnavailable
	}
	if e = collectionAuditTx(ctx, tx, a.ID, "video_material", v.Receipt.ID, "private_video_scan_ready"); e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	ready, e := scanVideo(tx.QueryRow(ctx, `SELECT `+videoColumns+` FROM private_video_materials v WHERE v.id=$1`, v.Receipt.ID))
	if e != nil {
		return media.VideoMaterialReceipt{}, content.ErrUnavailable
	}
	if e = s.videoCommit(ctx, tx, v.SessionDigest, a); e != nil {
		return media.VideoMaterialReceipt{}, e
	}
	return ready.Receipt, nil
}
func (s *Store) RejectVideoJob(ctx context.Context, v media.VideoMaterialRecord, code string) error {
	switch code {
	case "source_retired", "storage_unavailable", "invalid_bytes", "checksum_mismatch", "invalid_mp4", "decode_invalid", "video_limit", "scan_rejected", "provider_changed":
	default:
		return content.ErrInvalid
	}
	tag, e := s.pool.Exec(ctx, `WITH changed AS(UPDATE private_video_materials SET status='rejected',revision=revision+1,failure_code=$4,cleanup_pending=true,lease_id=NULL,lease_until=NULL,updated_at=now() WHERE id=$1 AND lease_id=$2::uuid AND revision=$3 AND status='scanning' RETURNING id) UPDATE media_assets m SET state='rejected' FROM changed WHERE m.id=changed.id`, v.Receipt.ID, v.LeaseID, v.Receipt.Revision, code)
	if e != nil {
		return content.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return content.ErrConflict
	}
	return nil
}
func (s *Store) FinishVideoCleanup(ctx context.Context, v media.VideoMaterialRecord) error {
	tag, e := s.pool.Exec(ctx, `WITH changed AS(UPDATE private_video_materials SET status=CASE WHEN status='deleting' THEN 'deleted' ELSE status END,revision=revision+1,cleanup_pending=false,lease_id=NULL,lease_until=NULL,updated_at=now() WHERE id=$1 AND lease_id=$2::uuid AND revision=$3 AND cleanup_pending=true AND lease_until>clock_timestamp() RETURNING id,status) UPDATE media_assets m SET state=CASE WHEN changed.status IN('deleted','rejected') THEN changed.status ELSE m.state END,deleted_at=CASE WHEN changed.status='deleted' THEN clock_timestamp() ELSE m.deleted_at END FROM changed WHERE m.id=changed.id`, v.Receipt.ID, v.LeaseID, v.Receipt.Revision)
	if e != nil {
		return content.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return content.ErrConflict
	}
	return nil
}
