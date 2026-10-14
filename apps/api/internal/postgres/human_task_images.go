package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
)

var _ media.HumanTaskImageStore = (*Store)(nil)

const taskImageColumns = `id::text,operation_id::text,task_id::text,owner_account_id::text,message_index,prefix_digest,
 expected_task_updated_at,mime_type,input_size,COALESCE(octet_length(derivative),0),input_sha256,COALESCE(derivative_sha256,''),
 pixel_risk,purpose,status,revision,preview_expires_at,retain_until,source_binding,session_id::text,session_authorization_generation`

// Cast both references to the same native UUID type. A direct $1::text in the
// storage key makes PostgreSQL infer incompatible text/uuid parameter types.
const taskImageAssetInsertSQL = `INSERT INTO media_assets(id,owner_account_id,storage_key,mime_type,byte_size,sha256_hex,state) VALUES($1::uuid,$2,'private-task/'||($1::uuid)::text,$3,$4,$5,'ready_private')`

type taskImageRow struct {
	receipt           media.TaskImageReceipt
	source, session   string
	sessionGeneration int64
}

func scanTaskImage(row scanner) (taskImageRow, error) {
	var v taskImageRow
	r := &v.receipt
	e := row.Scan(&r.ID, &r.OperationID, &r.TaskID, &r.OwnerID, &r.MessageIndex, &r.PrefixDigest, &r.TaskUpdatedAt, &r.MIME, &r.InputSize, &r.ByteSize, &r.InputSHA256, &r.SHA256, &r.PixelRisk, &r.Purpose, &r.Status, &r.Revision, &r.PreviewExpiresAt, &r.RetainUntil, &v.source, &v.session, &v.sessionGeneration)
	return v, e
}
func taskImageInput(v media.TaskImageReceipt) media.TaskImageInput {
	return media.TaskImageInput{OperationID: v.OperationID, MessageIndex: v.MessageIndex, PrefixDigest: v.PrefixDigest, TaskUpdatedAt: v.TaskUpdatedAt, MIME: v.MIME, ByteSize: v.InputSize, SHA256: v.InputSHA256, PixelRisk: v.PixelRisk, Purpose: v.Purpose}
}

func taskImageTaskFailure(err error) error {
	// scanAgentTask converts a missing or inaccessible task row before this
	// image boundary. Keep that disposition distinct from database failures.
	if errors.Is(err, agentworkspace.ErrNotFound) {
		return content.ErrNotFound
	}
	return imageFailure(err)
}

// Account/optional metadata -> active personal Agent -> original Task ->
// image/assets -> Session. No model, profile completeness or synthetic Task.
func (s *Store) taskImageTx(ctx context.Context, d [32]byte, a identity.Actor, id string) (pgx.Tx, agentworkspace.Task, string, error) {
	if !media.ValidPrivateImageID(id) {
		return nil, agentworkspace.Task{}, "", content.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, d, a)
	if e != nil {
		return nil, agentworkspace.Task{}, "", e
	}
	fail := func(e error) (pgx.Tx, agentworkspace.Task, string, error) {
		tx.Rollback(context.Background())
		return nil, agentworkspace.Task{}, "", e
	}
	var agent string
	if e = tx.QueryRow(ctx, `SELECT id::text FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active' ORDER BY id LIMIT 1 FOR SHARE`, a.ID).Scan(&agent); e != nil {
		return fail(imageFailure(e))
	}
	t, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND acting_user_account_id=$2 AND principal_type='person' FOR UPDATE`, id, a.ID))
	if e != nil {
		return fail(taskImageTaskFailure(e))
	}
	if t.PrincipalID != a.ID || t.ContextType != "CITY" || t.CityID == "" || !media.ValidPrivateImageID(t.ContextID) {
		return fail(content.ErrNotFound)
	}
	var source string
	e = tx.QueryRow(ctx, taskImageSourceSQL+` FOR SHARE OF c,cc,cx`, a.ID, agent, t.ID).Scan(&source)
	if e != nil {
		return fail(imageFailure(e))
	}
	return tx, t, source, nil
}

const taskImageSourceSQL = `SELECT jsonb_build_array(a.id,a.xmin::text,ag.id,ag.xmin::text,t.id,t.xmin::text,t.updated_at,c.id,c.xmin::text,cc.xmin::text,cx.id,cx.xmin::text)::text
 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
 JOIN agent_tasks t ON t.owner_account_id=a.id JOIN cities c ON c.id=t.city_context_id
 JOIN city_contexts cc ON cc.city_id=c.id JOIN contexts cx ON cx.id=t.context_id
 WHERE a.id=$1 AND ag.id=$2 AND t.id=$3 AND a.account_type='person' AND a.status='active'
 AND ag.agent_type='personal' AND ag.status='active' AND t.principal_type='person' AND t.acting_user_account_id=a.id
 AND t.context_type='CITY' AND cx.context_type='CITY' AND cx.city_id=c.id
 AND c.publication_status='published' AND cc.status='active'
 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())`

// Encoded rows and binary share the same final clock. All source rows are held
// under native locks; expiry is checked again after resource waits and encode.
func (s *Store) taskImageFinal(ctx context.Context, tx pgx.Tx, d [32]byte, a identity.Actor, t agentworkspace.Task, source string, rows []taskImageRow, binary, confirm, invalidPreviewMetadata bool) error {
	if _, e := s.imageSession(ctx, tx, d, a); e != nil {
		return e
	}
	var current string
	if e := tx.QueryRow(ctx, taskImageSourceSQL, a.ID, taskImageAgentSQLArgument(source), t.ID).Scan(&current); e != nil || current != source {
		return content.ErrConflict
	}
	expected := make([]map[string]any, 0, len(rows))
	for _, v := range rows {
		r := v.receipt
		expected = append(expected, map[string]any{"id": r.ID, "status": r.Status, "revision": r.Revision, "hash": r.SHA256})
	}
	raw, e := json.Marshal(expected)
	if e != nil {
		return content.ErrUnavailable
	}
	var valid bool
	e = tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at), expected AS(
 SELECT * FROM jsonb_to_recordset($5::jsonb) AS x(id uuid,status text,revision bigint,hash text))
 SELECT EXISTS(SELECT 1 FROM sessions ss JOIN accounts a ON a.id=ss.account_id
 JOIN agents ag ON ag.principal_account_id=a.id AND ag.id=$9
 JOIN agent_tasks t ON t.owner_account_id=a.id AND t.id=$4
 JOIN cities c ON c.id=t.city_context_id JOIN city_contexts cc ON cc.city_id=c.id
 JOIN contexts cx ON cx.id=t.context_id CROSS JOIN cl
 WHERE ss.token_sha256=$1 AND a.id=$2 AND a.status='active' AND a.account_type='person'
 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at
 AND ($3 OR ss.authentication_method<>'dev_phone')
 AND ag.status='active' AND ag.agent_type='personal' AND t.principal_type='person' AND t.acting_user_account_id=a.id
 AND t.context_type='CITY' AND cx.context_type='CITY' AND cx.city_id=c.id
 AND c.publication_status='published' AND cc.status='active' AND (c.expires_at IS NULL OR c.expires_at>cl.at)
 AND NOT EXISTS(SELECT 1 FROM expected x LEFT JOIN human_task_message_images i ON i.id=x.id
 LEFT JOIN media_assets ma ON ma.id=i.id WHERE i.id IS NULL OR i.owner_account_id<>a.id OR i.task_id<>$4
 OR i.status<>x.status OR i.revision<>x.revision OR (i.status<>'deleted' AND i.retain_until<=cl.at)
 OR (i.status='preview' AND NOT $10::boolean AND (i.source_binding<>$6 OR i.session_id<>ss.id OR i.session_authorization_generation<>ss.authorization_generation OR i.preview_expires_at<=cl.at))
 OR ($8::boolean AND (i.source_binding<>$6 OR i.session_id<>ss.id OR i.session_authorization_generation<>ss.authorization_generation OR i.preview_expires_at<=cl.at))
 OR ($7::boolean AND i.status<>'ready_private')
 OR (i.status='ready_private' AND (i.derivative_sha256 IS DISTINCT FROM x.hash OR ma.id IS NULL
 OR ma.state<>'ready_private' OR ma.owner_account_id<>a.id OR ma.sha256_hex IS DISTINCT FROM i.derivative_sha256
 OR ma.mime_type<>i.mime_type OR ma.byte_size<>octet_length(i.derivative)))))`, d[:], a.ID, s.devPhoneEnabled, t.ID, string(raw), source, binary, confirm, taskImageAgentSQLArgument(source), invalidPreviewMetadata).Scan(&valid)
	if e != nil || ctx.Err() != nil {
		return content.ErrUnavailable
	}
	if !valid {
		return content.ErrConflict
	}
	return nil
}

func taskImagePreviewCurrent(v taskImageRow, source, session string, generation int64, at time.Time) bool {
	return v.receipt.Status == "preview" && v.source == source && v.session == session &&
		v.sessionGeneration == generation && v.receipt.PreviewExpiresAt.After(at)
}

// The source is native JSON, not a client selector. Keep extraction local and
// closed rather than accepting an Agent id in the upload API.
func taskImageAgentSQLArgument(source string) string {
	var values []json.RawMessage
	if json.Unmarshal([]byte(source), &values) != nil || len(values) != 12 {
		return ""
	}
	var id string
	if json.Unmarshal(values[2], &id) != nil || !media.ValidPrivateImageID(id) {
		return ""
	}
	return id
}
func (s *Store) PreviewHumanTaskImage(ctx context.Context, d [32]byte, a identity.Actor, task string, p media.TaskImageInput) (media.TaskImageReceipt, error) {
	if e := media.ValidateTaskImageInput(p); e != nil {
		return media.TaskImageReceipt{}, e
	}
	tx, t, source, e := s.taskImageTx(ctx, d, a, task)
	if e != nil {
		return media.TaskImageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	if e = media.TaskImageMatches(t, p, true); e != nil {
		return media.TaskImageReceipt{}, e
	}
	var session string
	var sessionGeneration int64
	if e = tx.QueryRow(ctx, `SELECT id::text,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.ID, d[:]).Scan(&session, &sessionGeneration); e != nil {
		return media.TaskImageReceipt{}, identity.ErrUnauthorized
	}
	_, e = tx.Exec(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 INSERT INTO human_task_message_images(owner_account_id,task_id,operation_id,message_index,prefix_digest,expected_task_updated_at,source_binding,session_id,mime_type,input_size,input_sha256,pixel_risk,purpose,session_authorization_generation,preview_expires_at,retain_until)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,cl.at+interval '5 minutes',cl.at+interval '30 days' FROM cl
 WHERE (SELECT count(*) FROM human_task_message_images WHERE owner_account_id=$1 AND task_id=$2 AND status<>'deleted' AND retain_until>cl.at AND (status='ready_private' OR preview_expires_at>cl.at))<12
 ON CONFLICT(owner_account_id,operation_id) DO NOTHING`, a.ID, task, p.OperationID, p.MessageIndex, p.PrefixDigest, p.TaskUpdatedAt, source, session, p.MIME, p.ByteSize, p.SHA256, p.PixelRisk, p.Purpose, sessionGeneration)
	if e != nil {
		return media.TaskImageReceipt{}, content.ErrUnavailable
	}
	v, e := scanTaskImage(tx.QueryRow(ctx, `SELECT `+taskImageColumns+` FROM human_task_message_images WHERE owner_account_id=$1 AND operation_id=$2 FOR UPDATE`, a.ID, p.OperationID))
	if e != nil {
		return media.TaskImageReceipt{}, imageFailure(e)
	}
	expected := taskImageInput(v.receipt)
	expected.TaskUpdatedAt = p.TaskUpdatedAt
	if v.receipt.TaskID != task || expected != p || !v.receipt.TaskUpdatedAt.Equal(p.TaskUpdatedAt) || v.source != source || v.session != session || v.sessionGeneration != sessionGeneration || v.receipt.Status == "deleted" {
		return media.TaskImageReceipt{}, content.ErrConflict
	}
	if v.receipt.Status == "ready_private" {
		var locked string
		if e = tx.QueryRow(ctx, `SELECT id::text FROM media_assets WHERE id=$1 FOR SHARE`, v.receipt.ID).Scan(&locked); e != nil {
			return media.TaskImageReceipt{}, imageFailure(e)
		}
	}
	if e = insertDomainAudit(ctx, tx, a.ID, "preview", "agent_task_image", v.receipt.ID, media.TaskImagePurpose, nil); e != nil {
		return media.TaskImageReceipt{}, content.ErrUnavailable
	}
	if e = s.taskImageFinal(ctx, tx, d, a, t, source, []taskImageRow{v}, false, true, false); e != nil {
		return media.TaskImageReceipt{}, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return media.TaskImageReceipt{}, e
	}
	return v.receipt, nil
}
func (s *Store) SaveHumanTaskImage(ctx context.Context, d [32]byte, a identity.Actor, task, id, mime string, data []byte) (media.TaskImageReceipt, error) {
	if !media.ValidPrivateImageID(task) || !media.ValidPrivateImageID(id) || len(data) < 1 || len(data) > media.MaxImageInputBytes {
		return media.TaskImageReceipt{}, content.ErrInvalid
	}
	// Bounded pixel processing happens before original Task locks. No source
	// permission is inferred from successful decoding/reencoding.
	derivativeReader, kind, e := (&media.LocalImageDerivativeBuilder{}).BuildPublic(ctx, bytes.NewReader(data), mime)
	if e != nil {
		return media.TaskImageReceipt{}, e
	}
	derivative, e := io.ReadAll(io.LimitReader(derivativeReader, media.MaxImageOutputBytes+1))
	derivativeReader.Close()
	if e != nil || len(derivative) > media.MaxImageOutputBytes {
		return media.TaskImageReceipt{}, media.ErrImageLimit
	}
	inputHash := sha256.Sum256(data)
	outHash := sha256.Sum256(derivative)
	hash := hex.EncodeToString(outHash[:])
	tx, t, source, e := s.taskImageTx(ctx, d, a, task)
	if e != nil {
		return media.TaskImageReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := scanTaskImage(tx.QueryRow(ctx, `SELECT `+taskImageColumns+` FROM human_task_message_images WHERE id=$1 AND owner_account_id=$2 AND task_id=$3 FOR UPDATE`, id, a.ID, task))
	if e != nil {
		return media.TaskImageReceipt{}, imageFailure(e)
	}
	p := taskImageInput(v.receipt)
	if e = media.TaskImageMatches(t, p, true); e != nil {
		return media.TaskImageReceipt{}, e
	}
	if v.source != source || v.receipt.Status == "deleted" || v.receipt.MIME != kind || v.receipt.InputSize != int64(len(data)) || v.receipt.InputSHA256 != hex.EncodeToString(inputHash[:]) {
		return media.TaskImageReceipt{}, content.ErrConflict
	}
	if v.receipt.Status == "ready_private" {
		if v.receipt.SHA256 != hash {
			return media.TaskImageReceipt{}, content.ErrConflict
		}
		var locked string
		if e = tx.QueryRow(ctx, `SELECT id::text FROM media_assets WHERE id=$1 FOR SHARE`, id).Scan(&locked); e != nil {
			return media.TaskImageReceipt{}, imageFailure(e)
		}
	} else {
		_, e = tx.Exec(ctx, `UPDATE human_task_message_images SET status='ready_private',derivative=$2,derivative_sha256=$3,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id, derivative, hash)
		if e != nil {
			return media.TaskImageReceipt{}, content.ErrUnavailable
		}
		_, e = tx.Exec(ctx, taskImageAssetInsertSQL, id, a.ID, kind, len(derivative), hash)
		if e != nil {
			return media.TaskImageReceipt{}, content.ErrUnavailable
		}
		v.receipt.Status = "ready_private"
		v.receipt.Revision++
		v.receipt.SHA256 = hash
		v.receipt.ByteSize = int64(len(derivative))
	}
	if e = insertDomainAudit(ctx, tx, a.ID, "attach", "agent_task_image", id, media.TaskImagePurpose, nil); e != nil {
		return media.TaskImageReceipt{}, content.ErrUnavailable
	}
	if e = s.taskImageFinal(ctx, tx, d, a, t, source, []taskImageRow{v}, false, true, false); e != nil {
		return media.TaskImageReceipt{}, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return media.TaskImageReceipt{}, e
	}
	return v.receipt, nil
}
func (s *Store) ReadHumanTaskImages(ctx context.Context, d [32]byte, a identity.Actor, task, id string, binary, operation bool, encode media.TaskImageEncode) ([]byte, error) {
	if encode == nil || (id != "" && !media.ValidPrivateImageID(id)) || (binary && (operation || id == "")) {
		return nil, content.ErrInvalid
	}
	tx, t, source, e := s.taskImageTx(ctx, d, a, task)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	column := "id"
	if operation {
		column = "operation_id"
	}
	query := `SELECT ` + taskImageColumns + ` FROM human_task_message_images WHERE owner_account_id=$1 AND task_id=$2`
	args := []any{a.ID, task}
	if id != "" {
		query += ` AND ` + column + `=$3`
		args = append(args, id)
	} else {
		query += ` AND status='ready_private' AND retain_until>clock_timestamp()`
	}
	query += ` ORDER BY created_at,id LIMIT 13 FOR SHARE`
	rs, e := tx.Query(ctx, query, args...)
	if e != nil {
		return nil, content.ErrUnavailable
	}
	rows := make([]taskImageRow, 0)
	receipts := make([]media.TaskImageReceipt, 0)
	for rs.Next() {
		v, readErr := scanTaskImage(rs)
		if readErr != nil {
			rs.Close()
			return nil, content.ErrUnavailable
		}
		if readErr = media.TaskImageMatches(t, taskImageInput(v.receipt), false); readErr != nil {
			rs.Close()
			return nil, readErr
		}
		rows = append(rows, v)
		receipts = append(receipts, v.receipt)
	}
	rs.Close()
	if rs.Err() != nil {
		return nil, content.ErrUnavailable
	}
	if len(rows) > 12 {
		return nil, content.ErrConflict
	}
	if id != "" && len(rows) != 1 {
		return nil, content.ErrNotFound
	}
	var image []byte
	invalidPreviewMetadata := false
	if operation && len(rows) == 1 && rows[0].receipt.Status == "preview" {
		// Recovery reads use current human authorization and the original message
		// prefix. Expired upload approval may be inspected, never renewed here.
		var session string
		var generation int64
		var at time.Time
		if e = tx.QueryRow(ctx, `SELECT id::text,authorization_generation,clock_timestamp() FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.ID, d[:]).Scan(&session, &generation, &at); e != nil {
			return nil, identity.ErrUnauthorized
		}
		if !taskImagePreviewCurrent(rows[0], source, session, generation, at) {
			invalidPreviewMetadata = true
			receipts[0].Status = media.TaskImageInvalidatedPreview
		}
	}
	for _, v := range rows {
		if v.receipt.Status == "ready_private" {
			var locked string
			if e = tx.QueryRow(ctx, `SELECT id::text FROM media_assets WHERE id=$1 FOR SHARE`, v.receipt.ID).Scan(&locked); e != nil {
				return nil, imageFailure(e)
			}
		}
	}
	if binary {
		if rows[0].receipt.Status != "ready_private" {
			return nil, content.ErrNotFound
		}
		if e = tx.QueryRow(ctx, `SELECT derivative FROM human_task_message_images WHERE id=$1`, id).Scan(&image); e != nil {
			return nil, content.ErrUnavailable
		}
		hash := sha256.Sum256(image)
		if len(image) == 0 || len(image) > media.MaxImageOutputBytes || int64(len(image)) != rows[0].receipt.ByteSize || hex.EncodeToString(hash[:]) != rows[0].receipt.SHA256 {
			return nil, content.ErrConflict
		}
	}
	raw, e := encode(receipts, image)
	if e != nil {
		return nil, e
	}
	if e = s.taskImageFinal(ctx, tx, d, a, t, source, rows, binary, false, invalidPreviewMetadata); e != nil {
		return nil, e
	}
	if e = commitPrivateImage(ctx, tx); e != nil {
		return nil, e
	}
	return raw, nil
}
func (s *Store) DeleteHumanTaskImage(ctx context.Context, d [32]byte, a identity.Actor, task, id string, revision int64) error {
	if !media.ValidPrivateImageID(id) || revision < 1 {
		return content.ErrInvalid
	}
	tx, t, source, e := s.taskImageTx(ctx, d, a, task)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	v, e := scanTaskImage(tx.QueryRow(ctx, `SELECT `+taskImageColumns+` FROM human_task_message_images WHERE id=$1 AND owner_account_id=$2 AND task_id=$3 FOR UPDATE`, id, a.ID, task))
	if e != nil {
		return imageFailure(e)
	}
	if e = media.TaskImageMatches(t, taskImageInput(v.receipt), false); e != nil {
		return e
	}
	if v.receipt.Status != "deleted" {
		if v.receipt.Revision != revision {
			return content.ErrConflict
		}
		_, e = tx.Exec(ctx, `UPDATE media_assets SET state='deleted',deleted_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2`, id, a.ID)
		if e != nil {
			return content.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `UPDATE human_task_message_images SET status='deleted',derivative=NULL,derivative_sha256=NULL,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, id)
		if e != nil {
			return content.ErrUnavailable
		}
		v.receipt.Status = "deleted"
		v.receipt.Revision++
		v.receipt.SHA256 = ""
		v.receipt.ByteSize = 0
	} else if revision != v.receipt.Revision && revision+1 != v.receipt.Revision {
		return content.ErrConflict
	}
	if e = insertDomainAudit(ctx, tx, a.ID, "delete", "agent_task_image", id, media.TaskImagePurpose, nil); e != nil {
		return content.ErrUnavailable
	}
	if e = s.taskImageFinal(ctx, tx, d, a, t, source, []taskImageRow{v}, false, false, false); e != nil {
		return e
	}
	return commitPrivateImage(ctx, tx)
}
