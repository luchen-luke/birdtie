package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

var _ media.HumanPrivateImageStore = (*Store)(nil)

// The account/optional Agent locks are the original human gateway. Resource
// order remains Account -> optional Agent metadata -> Moment -> image -> Session.
// Source xmin includes even private -> public -> private or withdraw/restore ABA.
const privateImageSourceSQL = `SELECT jsonb_build_array(a.id,a.xmin::text,m.id,m.xmin::text,m.revision,m.visibility,m.status)::text,m.revision
 FROM accounts a JOIN moments m ON m.author_account_id=a.id
 WHERE a.id=$1 AND a.status='active' AND a.account_type='person' AND m.id=$2
 AND m.visibility='private' AND m.status='draft' FOR UPDATE OF m`
const privateImageSessionSQL = `SELECT id::text FROM sessions WHERE account_id=$1 AND token_sha256=$2
 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp()
 AND ($3 OR authentication_method<>'dev_phone') FOR SHARE`
const privateImageColumns = `id::text,operation_id::text,moment_id::text,owner_account_id::text,moment_revision,
 mime_type,input_size,input_sha256,COALESCE(derivative_sha256,''),pixel_risk,purpose,status,revision,
 preview_expires_at,retain_until,source_binding,session_id::text,NULL::bytea`

type privateImageRow struct {
	receipt         media.PrivateImageReceipt
	source, session string
	bytes           []byte
}

func currentPrivateImageRead(v privateImageRow, source, session string, binary bool) (privateImageRow, error) {
	if v.receipt.Status == "preview" && (v.source != source || v.session != session) {
		return privateImageRow{}, content.ErrConflict
	}
	if v.receipt.Status == "deleted" && binary {
		return privateImageRow{}, content.ErrNotFound
	}
	if v.receipt.Status != "preview" {
		v.source = source
	}
	return v, nil
}

func scanPrivateImage(row scanner) (privateImageRow, error) {
	var v privateImageRow
	r := &v.receipt
	err := row.Scan(&r.ID, &r.OperationID, &r.MomentID, &r.OwnerID, &r.MomentRevision, &r.MIME, &r.ByteSize, &r.InputSHA256, &r.SHA256, &r.PixelRisk, &r.Purpose, &r.Status, &r.Revision, &r.PreviewExpiresAt, &r.RetainUntil, &v.source, &v.session, &v.bytes)
	return v, err
}
func imageFailure(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return content.ErrNotFound
	}
	return humanMomentFailure(e)
}
func (s *Store) imageTx(ctx context.Context, d [32]byte, a identity.Actor, id string) (pgx.Tx, string, int64, error) {
	if !media.ValidPrivateImageID(id) {
		return nil, "", 0, content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return nil, "", 0, e
	}
	var source string
	var revision int64
	e = tx.QueryRow(ctx, privateImageSourceSQL, a.ID, id).Scan(&source, &revision)
	if e != nil {
		tx.Rollback(context.Background())
		return nil, "", 0, imageFailure(e)
	}
	return tx, source, revision, nil
}
func (s *Store) imageSession(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor) (string, error) {
	var id string
	e := tx.QueryRow(ctx, privateImageSessionSQL, a.ID, d[:], s.devPhoneEnabled).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", identity.ErrUnauthorized
	}
	if e != nil {
		return "", content.ErrUnavailable
	}
	return id, nil
}

// One clock is materialized after all lock waits/encoding. Never transaction now().
const privateImageFinalSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM private_moment_images i JOIN accounts a ON a.id=i.owner_account_id
 JOIN moments m ON m.id=i.moment_id AND m.author_account_id=a.id
 JOIN sessions ss ON ss.account_id=a.id LEFT JOIN media_assets ma ON ma.id=i.id CROSS JOIN cl
 WHERE i.id=$1 AND a.id=$2 AND a.status='active' AND a.account_type='person'
 AND m.visibility='private' AND m.status='draft' AND (i.status<>'preview' OR i.source_binding=$3)
 AND jsonb_build_array(a.id,a.xmin::text,m.id,m.xmin::text,m.revision,m.visibility,m.status)::text=$3
 AND i.status=$4 AND i.revision=$5 AND (i.status='deleted' OR i.retain_until>cl.at)
 AND (i.status<>'ready_private' OR (ma.state='ready_private' AND ma.owner_account_id=a.id
  AND ma.sha256_hex=i.derivative_sha256 AND ma.mime_type=i.mime_type AND ma.byte_size=octet_length(i.derivative)))
 AND (($4<>'preview' AND NOT $8::boolean) OR (i.preview_expires_at>cl.at AND i.session_id=ss.id))
 AND ss.token_sha256=$6 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at
 AND ($7 OR ss.authentication_method<>'dev_phone'))`

func (s *Store) imageFinal(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, v privateImageRow, preview bool) error {
	var valid bool
	e := tx.QueryRow(ctx, privateImageFinalSQL, v.receipt.ID, a.ID, v.source, v.receipt.Status, v.receipt.Revision, d[:], s.devPhoneEnabled, preview).Scan(&valid)
	if e != nil || ctx.Err() != nil {
		return content.ErrUnavailable
	}
	if !valid {
		return content.ErrConflict
	}
	return nil
}
func commitPrivateImage(ctx context.Context, tx pgx.Tx) error {
	if ctx.Err() != nil {
		return content.ErrUnavailable
	}
	if e := tx.Commit(ctx); e != nil {
		return content.ErrUnavailable
	}
	if ctx.Err() != nil {
		return content.ErrUnavailable
	}
	return nil
}

// All encoded items and the empty list share one final clock. Asset rows are
// SHARE-locked before Session, so a separate canonical delete cannot cross it.
const privateImageCollectionFinalSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at), expected AS (
 SELECT * FROM jsonb_to_recordset($6::jsonb) AS x(id uuid,status text,revision bigint,hash text))
 SELECT EXISTS(SELECT 1 FROM accounts a JOIN moments m ON m.author_account_id=a.id JOIN sessions ss ON ss.account_id=a.id CROSS JOIN cl
 WHERE a.id=$1 AND a.status='active' AND a.account_type='person' AND m.id=$2 AND m.visibility='private' AND m.status='draft'
 AND jsonb_build_array(a.id,a.xmin::text,m.id,m.xmin::text,m.revision,m.visibility,m.status)::text=$3
 AND ss.token_sha256=$4 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($5 OR ss.authentication_method<>'dev_phone')
 AND NOT EXISTS(SELECT 1 FROM expected x LEFT JOIN private_moment_images i ON i.id=x.id LEFT JOIN media_assets ma ON ma.id=i.id
 WHERE i.id IS NULL OR i.owner_account_id<>a.id OR i.moment_id<>m.id OR i.status<>x.status OR i.revision<>x.revision
 OR (i.status<>'deleted' AND i.retain_until<=cl.at)
 OR (i.status='preview' AND (i.source_binding<>$3 OR i.session_id<>ss.id OR i.preview_expires_at<=cl.at))
 OR (i.status='ready_private' AND (i.derivative_sha256 IS DISTINCT FROM x.hash OR ma.id IS NULL OR ma.state<>'ready_private'
  OR ma.owner_account_id<>a.id OR ma.sha256_hex IS DISTINCT FROM i.derivative_sha256 OR ma.mime_type<>i.mime_type
  OR ma.byte_size<>octet_length(i.derivative)))))`

func (s *Store) imageCollectionFinal(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, moment, source string, rs []media.PrivateImageReceipt) error {
	expected := make([]map[string]any, 0, len(rs))
	for _, r := range rs {
		expected = append(expected, map[string]any{"id": r.ID, "status": r.Status, "revision": r.Revision, "hash": r.SHA256})
	}
	raw, e := json.Marshal(expected)
	if e != nil {
		return content.ErrUnavailable
	}
	var valid bool
	e = tx.QueryRow(ctx, privateImageCollectionFinalSQL, a.ID, moment, source, d[:], s.devPhoneEnabled, string(raw)).Scan(&valid)
	if e != nil || ctx.Err() != nil {
		return content.ErrUnavailable
	}
	if !valid {
		return content.ErrConflict
	}
	return nil
}
func (s *Store) PreviewHumanPrivateImage(ctx context.Context, d [32]byte, a identity.Actor, moment string, p media.PrivateImageInput) (media.PrivateImageReceipt, error) {
	if e := media.ValidatePrivateImageInput(p); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	tx, source, revision, e := s.imageTx(ctx, d, a, moment)
	if e != nil {
		return media.PrivateImageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	if revision != p.MomentRevision {
		return media.PrivateImageReceipt{}, content.ErrConflict
	}
	// Capture a session ID without locking Session ahead of media. Final lock and
	// wall-clock gate below are required; this first observation grants nothing.
	var session string
	e = tx.QueryRow(ctx, `SELECT id::text FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.ID, d[:]).Scan(&session)
	if e != nil {
		return media.PrivateImageReceipt{}, identity.ErrUnauthorized
	}
	_, e = tx.Exec(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 INSERT INTO private_moment_images(owner_account_id,moment_id,operation_id,source_binding,moment_revision,session_id,mime_type,input_size,input_sha256,pixel_risk,purpose,preview_expires_at,retain_until,created_at,updated_at)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,cl.at+interval '5 minutes',cl.at+interval '30 days',cl.at,cl.at FROM cl
 WHERE (SELECT count(*) FROM private_moment_images WHERE owner_account_id=$1 AND moment_id=$2 AND
 ((status='preview' AND preview_expires_at>cl.at) OR (status='ready_private' AND retain_until>cl.at)))<12
 ON CONFLICT(owner_account_id,operation_id) DO NOTHING`, a.ID, moment, p.OperationID, source, revision, session, p.MIME, p.ByteSize, p.SHA256, p.PixelRisk, p.Purpose)
	if e != nil {
		return media.PrivateImageReceipt{}, content.ErrUnavailable
	}
	v, e := scanPrivateImage(tx.QueryRow(ctx, `SELECT `+privateImageColumns+` FROM private_moment_images WHERE owner_account_id=$1 AND operation_id=$2 FOR UPDATE`, a.ID, p.OperationID))
	if e != nil {
		return media.PrivateImageReceipt{}, imageFailure(e)
	}
	if v.source != source || v.session != session || v.receipt.MomentID != moment || v.receipt.MomentRevision != p.MomentRevision || v.receipt.MIME != p.MIME || v.receipt.ByteSize != p.ByteSize || v.receipt.InputSHA256 != p.SHA256 || v.receipt.PixelRisk != p.PixelRisk || v.receipt.Purpose != p.Purpose || v.receipt.Status == "deleted" {
		return media.PrivateImageReceipt{}, content.ErrConflict
	}
	if _, e = s.imageSession(ctx, tx, d, a); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	if e = s.imageFinal(ctx, tx, d, a, v, true); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	return v.receipt, nil
}
func (s *Store) SaveHumanPrivateImage(ctx context.Context, d [32]byte, a identity.Actor, moment, id, mime string, data []byte) (media.PrivateImageReceipt, error) {
	if !media.ValidPrivateImageID(id) {
		return media.PrivateImageReceipt{}, content.ErrInvalid
	}
	tx, source, _, e := s.imageTx(ctx, d, a, moment)
	if e != nil {
		return media.PrivateImageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := scanPrivateImage(tx.QueryRow(ctx, `SELECT `+privateImageColumns+` FROM private_moment_images WHERE id=$1 AND owner_account_id=$2 AND moment_id=$3 FOR UPDATE`, id, a.ID, moment))
	if e != nil {
		return media.PrivateImageReceipt{}, imageFailure(e)
	}
	// Observation only: lock Session after all media/asset mutations below.
	var session string
	e = tx.QueryRow(ctx, `SELECT id::text FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.ID, d[:]).Scan(&session)
	if e != nil {
		return media.PrivateImageReceipt{}, imageFailure(e)
	}
	if v.source != source || v.session != session || v.receipt.MIME != mime || v.receipt.Status == "deleted" {
		return media.PrivateImageReceipt{}, content.ErrConflict
	}
	if e = s.imageFinal(ctx, tx, d, a, v, true); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	p := media.PrivateImageInput{OperationID: v.receipt.OperationID, MomentRevision: v.receipt.MomentRevision, MIME: v.receipt.MIME, ByteSize: v.receipt.ByteSize, SHA256: v.receipt.InputSHA256, PixelRisk: v.receipt.PixelRisk, Purpose: v.receipt.Purpose}
	derivative, hash, e := media.PreparePrivateImage(ctx, p, data)
	if e != nil {
		return media.PrivateImageReceipt{}, e
	}
	if v.receipt.Status == "ready_private" {
		// Exact operation replay is idempotent, not a new confirmation or attachment.
		if v.receipt.SHA256 != hash {
			return media.PrivateImageReceipt{}, content.ErrConflict
		}
		var locked string
		if e = tx.QueryRow(ctx, `SELECT id::text FROM media_assets WHERE id=$1 FOR SHARE`, id).Scan(&locked); e != nil {
			return media.PrivateImageReceipt{}, imageFailure(e)
		}
	} else {
		_, e = tx.Exec(ctx, `UPDATE private_moment_images SET status='ready_private',derivative=$2,derivative_sha256=$3,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id, derivative, hash)
		if e != nil {
			return media.PrivateImageReceipt{}, content.ErrUnavailable
		}
		v.receipt.Status = "ready_private"
		v.receipt.Revision++
		v.receipt.SHA256 = hash
		_, e = tx.Exec(ctx, `INSERT INTO media_assets(id,owner_account_id,storage_key,mime_type,byte_size,sha256_hex,state) VALUES($1,$2,'private-moment/'||$1::text,$3,$4,$5,'ready_private')`, id, a.ID, mime, len(derivative), hash)
		if e != nil {
			return media.PrivateImageReceipt{}, content.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id) VALUES($1,'private_image_saved','media',$2)`, a.ID, id)
		if e != nil {
			return media.PrivateImageReceipt{}, content.ErrUnavailable
		}
	}
	if _, e = s.imageSession(ctx, tx, d, a); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	if e = s.imageFinal(ctx, tx, d, a, v, true); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return media.PrivateImageReceipt{}, e
	}
	return v.receipt, nil
}
func (s *Store) ReadHumanPrivateImages(ctx context.Context, d [32]byte, a identity.Actor, moment, id string, binary bool, encode media.PrivateImageEncode) ([]byte, error) {
	return s.readHumanPrivateImages(ctx, d, a, moment, id, binary, false, encode)
}
func (s *Store) ReadHumanPrivateImageOperation(ctx context.Context, d [32]byte, a identity.Actor, moment, op string, encode media.PrivateImageEncode) ([]byte, error) {
	if !media.ValidPrivateImageID(op) {
		return nil, content.ErrInvalid
	}
	return s.readHumanPrivateImages(ctx, d, a, moment, op, false, true, encode)
}
func (s *Store) readHumanPrivateImages(ctx context.Context, d [32]byte, a identity.Actor, moment, id string, binary, operation bool, encode media.PrivateImageEncode) ([]byte, error) {
	if encode == nil || (id != "" && !media.ValidPrivateImageID(id)) || (binary && id == "") {
		return nil, content.ErrInvalid
	}
	tx, source, _, e := s.imageTx(ctx, d, a, moment)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	columns := privateImageColumns
	if binary {
		columns = columns[:len(columns)-len("NULL::bytea")] + "derivative"
	}
	// Filter expired rows before LIMIT, not only after it; otherwise older expired
	// receipts could hide later live attachments. This is not the final read gate.
	rows, e := tx.Query(ctx, `WITH eligible_clock AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT `+columns+` FROM private_moment_images i CROSS JOIN eligible_clock ec WHERE owner_account_id=$1 AND moment_id=$2
 AND ($3='' OR (NOT $4::boolean AND id=NULLIF($3,'')::uuid) OR ($4::boolean AND operation_id=NULLIF($3,'')::uuid))
 AND ($3<>'' OR (status='ready_private' AND retain_until>ec.at)) ORDER BY created_at,id LIMIT 12 FOR SHARE OF i`, a.ID, moment, id, operation)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	all := []privateImageRow{}
	for rows.Next() {
		v, err := scanPrivateImage(rows)
		if err != nil {
			rows.Close()
			return nil, content.ErrUnavailable
		}
		all = append(all, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, content.ErrUnavailable
	}
	assetIDs := []string{}
	for _, v := range all {
		if v.receipt.Status == "ready_private" {
			assetIDs = append(assetIDs, v.receipt.ID)
		}
	}
	assetRows, e := tx.Query(ctx, `SELECT id FROM media_assets WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, assetIDs)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	for assetRows.Next() {
		var locked string
		if e = assetRows.Scan(&locked); e != nil {
			assetRows.Close()
			return nil, content.ErrUnavailable
		}
	}
	e = assetRows.Err()
	assetRows.Close()
	if e != nil {
		return nil, content.ErrUnavailable
	}
	session, e := s.imageSession(ctx, tx, d, a)
	if e != nil {
		return nil, e
	}
	receipts := []media.PrivateImageReceipt{}
	var payload []byte
	for _, v := range all {
		v, e = currentPrivateImageRead(v, source, session, binary)
		if e != nil {
			if id != "" {
				return nil, e
			}
			continue
		}
		// Viewing an existing owned attachment is a fresh human read, not reuse of
		// the old upload approval. Text edits are allowed; lifecycle trigger tombstones
		// scope/status/author transitions so public -> private cannot revive bytes.
		if e = s.imageFinal(ctx, tx, d, a, v, false); e != nil {
			if id != "" || !errors.Is(e, content.ErrConflict) {
				return nil, e
			}
			continue
		}
		if binary {
			if v.receipt.Status != "ready_private" {
				return nil, content.ErrConflict
			}
			sum := sha256.Sum256(v.bytes)
			if hex.EncodeToString(sum[:]) != v.receipt.SHA256 {
				return nil, content.ErrUnavailable
			}
			payload = v.bytes
		}
		receipts = append(receipts, v.receipt)
	}
	if id != "" && len(receipts) != 1 {
		return nil, content.ErrNotFound
	}
	encoded, e := encode(receipts, payload)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	// The callback may take time. Recheck after encoding, not just before it.
	if e = s.imageCollectionFinal(ctx, tx, d, a, moment, source, receipts); e != nil {
		return nil, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return nil, e
	}
	return encoded, nil
}
func (s *Store) DeleteHumanPrivateImage(ctx context.Context, d [32]byte, a identity.Actor, moment, id string, revision int64) error {
	if !media.ValidPrivateImageID(id) || revision < 1 {
		return content.ErrInvalid
	}
	tx, source, _, e := s.imageTx(ctx, d, a, moment)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	v, e := scanPrivateImage(tx.QueryRow(ctx, `SELECT `+privateImageColumns+` FROM private_moment_images WHERE id=$1 AND owner_account_id=$2 AND moment_id=$3 FOR UPDATE`, id, a.ID, moment))
	if e != nil {
		return imageFailure(e)
	}
	if v.receipt.Revision != revision {
		return content.ErrConflict
	}
	v.source = source
	if v.receipt.Status != "deleted" {
		_, e = tx.Exec(ctx, `UPDATE private_moment_images SET status='deleted',derivative=NULL,derivative_sha256=NULL,source_binding=$2,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id, source)
		if e != nil {
			return content.ErrUnavailable
		}
		v.receipt.Status = "deleted"
		v.receipt.Revision++
		v.source = source
		_, e = tx.Exec(ctx, `UPDATE media_assets SET state='deleted',deleted_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2`, id, a.ID)
		if e != nil {
			return content.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id) VALUES($1,'private_image_deleted','media',$2)`, a.ID, id)
		if e != nil {
			return content.ErrUnavailable
		}
	}
	if _, e = s.imageSession(ctx, tx, d, a); e != nil {
		return e
	}
	if e = s.imageFinal(ctx, tx, d, a, v, false); e != nil {
		return e
	}
	return commitPrivateImage(ctx, tx)
}

// Retention is an absolute read/write deadline. Background physical pruning is
// not part of this batch; bytes remain inaccessible after it and can be deleted.
