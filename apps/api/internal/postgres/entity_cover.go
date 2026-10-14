package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/jackc/pgx/v5"
)

var _ ec.Store = (*Store)(nil)
var _ ec.SessionStore = (*Store)(nil)
var _ ec.OriginalContentStore = (*Store)(nil)

func (s *Store) coverCurrentSession(ctx context.Context, tx pgx.Tx, a ec.Access, lock bool) (ec.SessionFrame, error) {
	if a.AccountID == "" {
		if a.AccountType != "" || a.SessionDigest != ([32]byte{}) {
			return ec.SessionFrame{}, ec.ErrForbidden
		}
		return ec.SessionFrame{}, nil
	}
	if a.AccountType != "person" || !av.ValidID(a.AccountID) || a.SessionDigest == ([32]byte{}) {
		return ec.SessionFrame{}, ec.ErrForbidden
	}
	if lock {
		var id string
		e := tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND status='active' AND account_type='person' FOR SHARE`, a.AccountID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return ec.SessionFrame{}, identity.ErrUnauthorized
		}
		if e != nil {
			return ec.SessionFrame{}, ec.ErrUnavailable
		}
		if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID); e != nil {
			return ec.SessionFrame{}, entityCoverError(e)
		}
	}
	var out ec.SessionFrame
	e := tx.QueryRow(ctx, `SELECT s.id,s.authorization_generation FROM sessions s JOIN accounts a ON a.id=s.account_id AND a.status='active' AND a.account_type='person' WHERE s.account_id=$1 AND s.token_sha256=$2 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp() AND($3::boolean OR s.authentication_method<>'dev_phone')`, a.AccountID, a.SessionDigest[:], s.devPhoneEnabled).Scan(&out.ID, &out.Generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, identity.ErrUnauthorized
	}
	if e != nil || ctx.Err() != nil {
		return out, ec.ErrUnavailable
	}
	return out, nil
}
func (s *Store) CaptureEntityCoverSession(ctx context.Context, a ec.Access) (ec.SessionFrame, error) {
	if a.AccountID == "" {
		if a.AccountType != "" || a.SessionDigest != ([32]byte{}) {
			return ec.SessionFrame{}, ec.ErrForbidden
		}
		return ec.SessionFrame{}, nil
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ec.SessionFrame{}, ec.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	first, e := s.coverCurrentSession(ctx, tx, a, false)
	if e != nil {
		return first, e
	}
	current, e := s.coverCurrentSession(ctx, tx, a, true)
	if e != nil {
		return first, e
	}
	if first != current {
		return first, ec.ErrChanged
	}
	return first, entityCoverError(tx.Commit(ctx))
}
func (s *Store) ValidateEntityCoverSession(ctx context.Context, a ec.Access, first ec.SessionFrame) error {
	current, e := s.CaptureEntityCoverSession(ctx, a)
	if e != nil {
		return e
	}
	if current != first {
		return ec.ErrChanged
	}
	return nil
}
func (s *Store) entityCoverOriginalFrame(ctx context.Context, a ec.Access) (ec.SessionFrame, error) {
	if f, ok := ec.OriginalSession(ctx, a); ok {
		if a.AccountID != "" && (!av.ValidID(f.ID) || f.Generation < 1) {
			return f, ec.ErrChanged
		}
		if a.AccountID == "" && f != (ec.SessionFrame{}) {
			return f, ec.ErrChanged
		}
		return f, nil
	}
	if ec.HasOriginalSession(ctx) {
		return ec.SessionFrame{}, ec.ErrChanged
	}
	return s.CaptureEntityCoverSession(ctx, a)
}
func (s *Store) coverFrameGate(ctx context.Context, tx pgx.Tx, a ec.Access, first ec.SessionFrame) error {
	current, e := s.coverCurrentSession(ctx, tx, a, true)
	if e != nil {
		return e
	}
	if current != first {
		return ec.ErrChanged
	}
	return nil
}

func coverFreeToolActivity(activity foundation.Activity) foundation.Activity {
	activity.Cover = nil
	activity.CoverSourceProof = ""
	return activity
}

// Align only the newly observed cover lease. The old lease is never renewed;
// all other fields and source hashes remain subject to the original comparator.
func preserveCoverProjectionLeases(old, current arp.Receipt) (arp.Receipt, bool) {
	if len(old.Items) != len(current.Items) || len(old.Activities) != len(current.Activities) {
		return current, false
	}
	current.Items = append(current.Items[:0:0], current.Items...)
	current.Activities = append(current.Activities[:0:0], current.Activities...)
	for i := range current.Items {
		if current.Items[i].Cover != nil {
			copy := *current.Items[i].Cover
			current.Items[i].Cover = &copy
		}
		if !ec.PreserveLease(old.Items[i].Cover, current.Items[i].Cover, time.Now().UTC()) {
			return current, false
		}
	}
	for i := range current.Activities {
		if current.Activities[i].Cover != nil {
			copy := *current.Activities[i].Cover
			current.Activities[i].Cover = &copy
		}
		if !ec.PreserveLease(old.Activities[i].Cover, current.Activities[i].Cover, time.Now().UTC()) {
			return current, false
		}
	}
	return current, true
}

type entityCoverFence struct {
	tx         pgx.Tx
	access     ec.Access
	ref        ec.Ref
	session    string
	generation int64
	source     string
	state      ec.State
	until      time.Time
	write      bool
}

func entityCoverError(e error) error {
	if e == nil {
		return nil
	}
	for _, known := range []error{ec.ErrInvalid, ec.ErrChanged, ec.ErrForbidden, ec.ErrNotFound, identity.ErrUnauthorized} {
		if errors.Is(e, known) {
			return known
		}
	}
	return ec.ErrUnavailable
}
func entityCoverViewer(a ec.Access) any {
	if a.AccountID == "" {
		return nil
	}
	return a.AccountID
}
func entityCoverSession(ctx context.Context, tx pgx.Tx, a ec.Access) (string, int64, error) {
	var id string
	var gen int64
	e := tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.AccountID, a.SessionDigest[:]).Scan(&id, &gen)
	return id, gen, e
}

const entityCoverRelations = `LOCK TABLE entity_covers,entity_cover_operations,activities,activity_organizers,organizations,organization_memberships,communities,community_memberships,businesses,business_memberships,cities,accounts,sessions,account_blocks,audit_events IN ACCESS SHARE MODE`

func entityCoverState(ctx context.Context, tx pgx.Tx, a ec.Access, r ec.Ref, at time.Time) (ec.State, error) {
	out := ec.State{}
	e := tx.QueryRow(ctx, `SELECT COALESCE((SELECT version FROM entity_covers WHERE entity_type=$1 AND entity_id=$2),0),birdtie_entity_cover_metadata($1,$2,$3,$4),birdtie_entity_cover_authority($1,$2,$3)||':'||COALESCE((SELECT xmin::text FROM entity_covers WHERE entity_type=$1 AND entity_id=$2),'none')`, r.Type, r.ID, entityCoverViewer(a), at).Scan(&out.Version, &out.Cover, &out.SourceProof)
	if e != nil || out.Version < 0 || out.Version > ec.MaxVersion {
		return ec.State{}, ec.ErrUnavailable
	}
	if out.Cover != nil {
		out.Cover.ExpiresAt = out.Cover.ExpiresAt.UTC()
		if out.Cover.Ref != r || out.Cover.Version != out.Version || !out.Cover.Valid(at) {
			return ec.State{}, ec.ErrUnavailable
		}
	}
	return out, nil
}
func (s *Store) beginEntityCover(ctx context.Context, a ec.Access, r ec.Ref, manage, write bool, op string) (entityCoverFence, error) {
	f := entityCoverFence{access: a, ref: r, write: write}
	if !r.Valid() || (manage && !av.ValidID(op) && op != "") {
		return f, ec.ErrInvalid
	}
	var e error
	frame, e := s.entityCoverOriginalFrame(ctx, a)
	if e != nil {
		return f, e
	}
	f.session, f.generation = frame.ID, frame.Generation
	if a.AccountID == "" {
		if manage || a.AccountType != "" || a.SessionDigest != ([32]byte{}) {
			return f, ec.ErrForbidden
		}
		f.tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	} else {
		if a.AccountType != "person" || !av.ValidID(a.AccountID) {
			return f, ec.ErrForbidden
		}
		f.tx, e = s.beginHumanMomentTx(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	}
	if e != nil {
		return f, entityCoverError(e)
	}
	fail := func(e error) (entityCoverFence, error) {
		f.tx.Rollback(context.Background())
		return entityCoverFence{}, entityCoverError(e)
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';`+entityCoverRelations); e != nil {
		return fail(e)
	}
	if op != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,137))`, a.AccountID+":"+op); e != nil {
			return fail(e)
		}
	}
	if manage {
		table := "activities"
		if r.Type == "organization" {
			table = "organizations"
		}
		lock := " FOR SHARE"
		if write {
			lock = " FOR UPDATE"
		}
		var id string
		e = f.tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1`+lock, r.ID).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return fail(ec.ErrNotFound)
		}
		if e != nil {
			return fail(e)
		}
		var yes bool
		e = f.tx.QueryRow(ctx, `SELECT birdtie_entity_cover_manage($1,$2,$3)`, r.Type, r.ID, a.AccountID).Scan(&yes)
		if e != nil {
			return fail(e)
		}
		if !yes {
			return fail(ec.ErrForbidden)
		}
	}
	var visible bool
	var now time.Time
	e = f.tx.QueryRow(ctx, `SELECT birdtie_entity_cover_visible($1,$2,$3,clock_timestamp()),birdtie_entity_cover_authority($1,$2,$3),clock_timestamp()`, r.Type, r.ID, entityCoverViewer(a)).Scan(&visible, &f.source, &now)
	if e != nil {
		return fail(e)
	}
	if !visible {
		return fail(ec.ErrNotFound)
	}
	f.state, e = entityCoverState(ctx, f.tx, a, r, now.UTC())
	if e != nil {
		return fail(e)
	}
	return f, nil
}
func (s *Store) finishEntityCover(ctx context.Context, f entityCoverFence, manage bool) error {
	if f.access.AccountID != "" {
		if e := s.coverFrameGate(ctx, f.tx, f.access, ec.SessionFrame{ID: f.session, Generation: f.generation}); e != nil {
			return e
		}
	}
	var visible, allowed bool
	var source string
	var now time.Time
	e := f.tx.QueryRow(ctx, `SELECT birdtie_entity_cover_visible($1,$2,$3,clock_timestamp()),birdtie_entity_cover_manage($1,$2,$3),birdtie_entity_cover_authority($1,$2,$3),clock_timestamp()`, f.ref.Type, f.ref.ID, entityCoverViewer(f.access)).Scan(&visible, &allowed, &source, &now)
	if e != nil {
		return ec.ErrUnavailable
	}
	if !visible {
		return ec.ErrNotFound
	}
	if manage && !allowed {
		return ec.ErrForbidden
	}
	if source != f.source || (!f.until.IsZero() && !f.until.After(now)) {
		return ec.ErrChanged
	}
	if !f.write {
		current, err := entityCoverState(ctx, f.tx, f.access, f.ref, now.UTC())
		if err != nil {
			return err
		}
		if current.SourceProof != f.state.SourceProof || !ec.PreserveLease(f.state.Cover, current.Cover, now.UTC()) {
			return ec.ErrChanged
		}
	}
	if f.access.AccountID != "" {
		return entityCoverError(s.finishHumanMomentTx(ctx, f.tx, f.access.SessionDigest, f.access.AccountID))
	}
	if ctx.Err() != nil {
		return ec.ErrUnavailable
	}
	return entityCoverError(f.tx.Commit(ctx))
}
func (s *Store) ReadEntityCover(ctx context.Context, a ec.Access, r ec.Ref) (ec.State, error) {
	f, e := s.beginEntityCover(ctx, a, r, false, false, "")
	if e != nil {
		return ec.State{}, e
	}
	defer f.tx.Rollback(context.Background())
	if e = s.finishEntityCover(ctx, f, false); e != nil {
		return ec.State{}, e
	}
	return f.state, nil
}
func entityCoverMaterial(f entityCoverFence, in ec.Input, sum, rights string) string {
	raw, _ := json.Marshal([]any{"ENTITY_COVER_APPROVED_V1", f.ref, f.access.AccountID, f.session, f.generation, f.source, in.ExpectedVersion, ec.Digest(f.ref, in, sum, rights)})
	return string(raw)
}
func entityCoverToken(d [32]byte, material string, issued, end int64) string {
	h := hmac.New(sha256.New, d[:])
	h.Write([]byte(material + ":" + strconv.FormatInt(issued, 10) + ":" + strconv.FormatInt(end, 10)))
	return "ec1." + strconv.FormatInt(issued, 10) + "." + strconv.FormatInt(end, 10) + "." + hex.EncodeToString(h.Sum(nil))
}
func validEntityCoverToken(d [32]byte, material, token string, now time.Time) (time.Time, bool) {
	p := strings.Split(token, ".")
	if len(p) != 4 || p[0] != "ec1" {
		return time.Time{}, false
	}
	issued, e := strconv.ParseInt(p[1], 10, 64)
	if e != nil {
		return time.Time{}, false
	}
	end, e := strconv.ParseInt(p[2], 10, 64)
	if e != nil || issued > now.UnixNano() || end <= now.UnixNano() || end-issued != int64(30*time.Second) {
		return time.Time{}, false
	}
	return time.Unix(0, end).UTC(), hmac.Equal([]byte(token), []byte(entityCoverToken(d, material, issued, end)))
}
func (s *Store) PreviewEntityCover(ctx context.Context, a ec.Access, r ec.Ref, in ec.Input) (ec.Approval, error) {
	d, sum, rights, e := ec.Normalize(in, time.Now().UTC())
	if e != nil {
		return ec.Approval{}, e
	}
	f, e := s.beginEntityCover(ctx, a, r, true, false, "")
	if e != nil {
		return ec.Approval{}, e
	}
	defer f.tx.Rollback(context.Background())
	if f.state.Version != in.ExpectedVersion {
		return ec.Approval{}, ec.ErrChanged
	}
	var now time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return ec.Approval{}, ec.ErrUnavailable
	}
	if _, _, _, e = ec.Normalize(in, now); e != nil {
		return ec.Approval{}, e
	}
	until := now.Add(30 * time.Second)
	out := ec.Approval{Ref: r, OperationID: in.OperationID, ExpectedVersion: in.ExpectedVersion, Action: in.Action, InputSHA256: sum, RightsSHA256: rights, ExpiresAt: until.UTC(), Token: entityCoverToken(a.SessionDigest, entityCoverMaterial(f, in, sum, rights), now.UnixNano(), until.UnixNano())}
	if in.Action == "SAVE" {
		out.PreviewDerivative = &ec.PreviewDerivative{ContentType: "image/jpeg", Width: ec.Width, Height: ec.Height, ByteCount: len(d.Bytes), SHA256: sum, ContentBase64: base64.StdEncoding.EncodeToString(d.Bytes)}
	}
	f.until = until
	if e = s.finishEntityCover(ctx, f, true); e != nil {
		return ec.Approval{}, e
	}
	return out, nil
}
func entityCoverReceipt(ctx context.Context, tx pgx.Tx, a ec.Access, r ec.Ref, op string) (ec.Receipt, string, int64, error) {
	out := ec.Receipt{OperationID: op}
	var digest string
	var expected int64
	e := tx.QueryRow(ctx, `SELECT entity_type,entity_id,action,applied_version,status,input_sha256,expected_version FROM entity_cover_operations WHERE actor_account_id=$1 AND operation_id=$2`, a.AccountID, op).Scan(&out.Type, &out.ID, &out.Action, &out.AppliedVersion, &out.Status, &digest, &expected)
	if e == nil && !out.Valid(r, op) {
		return ec.Receipt{}, "", 0, ec.ErrChanged
	}
	return out, digest, expected, e
}
func (s *Store) ChangeEntityCover(ctx context.Context, a ec.Access, r ec.Ref, in ec.Input) (ec.Receipt, error) {
	d, sum, rights, e := ec.Normalize(in, time.Now().UTC())
	if e != nil {
		return ec.Receipt{}, e
	}
	f, e := s.beginEntityCover(ctx, a, r, true, true, in.OperationID)
	if e != nil {
		return ec.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := ec.Digest(r, in, sum, rights)
	out, old, expected, e := entityCoverReceipt(ctx, f.tx, a, r, in.OperationID)
	if e == nil {
		if expected != in.ExpectedVersion || (out.Status != "STOPPED" && old != digest) {
			return ec.Receipt{}, ec.ErrChanged
		}
		if e = s.finishEntityCover(ctx, f, true); e != nil {
			return ec.Receipt{}, e
		}
		return out, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return ec.Receipt{}, entityCoverError(e)
	}
	if f.state.Version != in.ExpectedVersion {
		return ec.Receipt{}, ec.ErrChanged
	}
	var now time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return ec.Receipt{}, ec.ErrUnavailable
	}
	if _, _, _, e = ec.Normalize(in, now); e != nil {
		return ec.Receipt{}, e
	}
	until, ok := validEntityCoverToken(a.SessionDigest, entityCoverMaterial(f, in, sum, rights), in.Approval, now)
	if !ok {
		return ec.Receipt{}, ec.ErrChanged
	}
	f.until = until
	var raw, sha, license, retention any
	attribution := ""
	if in.Action == "SAVE" {
		raw, sha = d.Bytes, sum
		copyRights := *in.Rights
		copyRights.RetentionUntil = copyRights.RetentionUntil.UTC()
		b, _ := json.Marshal(copyRights)
		license = b
		retention = copyRights.RetentionUntil
		attribution = copyRights.Attribution
		if retention.(time.Time).Before(f.until) {
			f.until = retention.(time.Time)
		}
	}
	version := in.ExpectedVersion + 1
	_, e = f.tx.Exec(ctx, `INSERT INTO entity_covers(entity_type,entity_id,version,thumbnail,sha256,rights,uploaded_by,retention_until,attribution)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(entity_type,entity_id) DO UPDATE SET version=EXCLUDED.version,thumbnail=EXCLUDED.thumbnail,sha256=EXCLUDED.sha256,rights=EXCLUDED.rights,uploaded_by=EXCLUDED.uploaded_by,retention_until=EXCLUDED.retention_until,attribution=EXCLUDED.attribution,updated_at=clock_timestamp()`, r.Type, r.ID, version, raw, sha, license, a.AccountID, retention, attribution)
	if e != nil {
		return ec.Receipt{}, ec.ErrUnavailable
	}
	_, e = f.tx.Exec(ctx, `INSERT INTO entity_cover_operations(actor_account_id,operation_id,entity_type,entity_id,expected_version,applied_version,action,status,input_sha256)VALUES($1,$2,$3,$4,$5,$6,$7,'SUCCEEDED',$8)`, a.AccountID, in.OperationID, r.Type, r.ID, in.ExpectedVersion, version, in.Action, digest)
	if e != nil {
		return ec.Receipt{}, ec.ErrUnavailable
	}
	if _, e = auditExec(ctx, f.tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,$2,$3,$4,'allowed','entity_cover')`, a.AccountID, "cover_"+strings.ToLower(in.Action), r.Type, r.ID); e != nil {
		return ec.Receipt{}, ec.ErrUnavailable
	}
	out = ec.Receipt{Ref: r, OperationID: in.OperationID, Action: in.Action, AppliedVersion: version, Status: "SUCCEEDED"}
	if e = s.finishEntityCover(ctx, f, true); e != nil {
		return ec.Receipt{}, e
	}
	return out, nil
}

// Own immutable recovery does not grant current entity visibility or management.
func (s *Store) beginEntityCoverHistory(ctx context.Context, a ec.Access, r ec.Ref, op string) (entityCoverFence, error) {
	f := entityCoverFence{access: a, ref: r}
	if !r.Valid() || !av.ValidID(op) {
		return f, ec.ErrInvalid
	}
	if a.AccountType != "person" || !av.ValidID(a.AccountID) {
		return f, ec.ErrForbidden
	}
	original, e := s.entityCoverOriginalFrame(ctx, a)
	if e != nil {
		return f, e
	}
	f.session, f.generation = original.ID, original.Generation
	f.tx, e = s.beginHumanMomentTx(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	if e != nil {
		return f, entityCoverError(e)
	}
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,137))`, a.AccountID+":"+op); e != nil {
		f.tx.Rollback(context.Background())
		return f, ec.ErrUnavailable
	}
	return f, nil
}
func (s *Store) finishEntityCoverHistory(ctx context.Context, f entityCoverFence) error {
	if e := s.coverFrameGate(ctx, f.tx, f.access, ec.SessionFrame{ID: f.session, Generation: f.generation}); e != nil {
		return e
	}
	return entityCoverError(s.finishHumanMomentTx(ctx, f.tx, f.access.SessionDigest, f.access.AccountID))
}
func (s *Store) ReadEntityCoverOperation(ctx context.Context, a ec.Access, r ec.Ref, op string) (ec.Receipt, error) {
	f, e := s.beginEntityCoverHistory(ctx, a, r, op)
	if e != nil {
		return ec.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	out, _, _, e := entityCoverReceipt(ctx, f.tx, a, r, op)
	if errors.Is(e, pgx.ErrNoRows) {
		return ec.Receipt{}, ec.ErrNotFound
	}
	if e != nil {
		return ec.Receipt{}, entityCoverError(e)
	}
	if e = s.finishEntityCoverHistory(ctx, f); e != nil {
		return ec.Receipt{}, e
	}
	return out, nil
}
func (s *Store) StopEntityCoverOperation(ctx context.Context, a ec.Access, r ec.Ref, op string, expected int64) (ec.Receipt, error) {
	if expected < 0 || expected >= ec.MaxVersion {
		return ec.Receipt{}, ec.ErrInvalid
	}
	f, e := s.beginEntityCoverHistory(ctx, a, r, op)
	if e != nil {
		return ec.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	out, _, prior, e := entityCoverReceipt(ctx, f.tx, a, r, op)
	if e == nil {
		if prior != expected {
			return ec.Receipt{}, ec.ErrChanged
		}
	} else if errors.Is(e, pgx.ErrNoRows) {
		sum := sha256.Sum256([]byte("ENTITY_COVER_STOP_V1:" + r.Type + ":" + r.ID + ":" + op + ":" + strconv.FormatInt(expected, 10)))
		_, e = f.tx.Exec(ctx, `INSERT INTO entity_cover_operations(actor_account_id,operation_id,entity_type,entity_id,expected_version,applied_version,action,status,input_sha256)VALUES($1,$2,$3,$4,$5,0,'STOP','STOPPED',$6)`, a.AccountID, op, r.Type, r.ID, expected, hex.EncodeToString(sum[:]))
		out = ec.Receipt{Ref: r, OperationID: op, Action: "STOP", Status: "STOPPED"}
	}
	if e != nil {
		return ec.Receipt{}, entityCoverError(e)
	}
	if e = s.finishEntityCoverHistory(ctx, f); e != nil {
		return ec.Receipt{}, e
	}
	return out, nil
}
func (s *Store) ReadEntityCoverContent(ctx context.Context, a ec.Access, r ec.Ref, v int64) (ec.Metadata, []byte, error) {
	out, e := s.ReadEntityCoverOriginalContent(ctx, a, r, v)
	return out.Metadata, out.Bytes, e
}
func (s *Store) ReadEntityCoverOriginalContent(ctx context.Context, a ec.Access, r ec.Ref, version int64) (ec.OriginalContent, error) {
	if version < 1 || version > ec.MaxVersion {
		return ec.OriginalContent{}, ec.ErrInvalid
	}
	f, e := s.beginEntityCover(ctx, a, r, false, false, "")
	if e != nil {
		return ec.OriginalContent{}, e
	}
	defer f.tx.Rollback(context.Background())
	if f.state.Cover == nil || f.state.Version != version {
		return ec.OriginalContent{}, ec.ErrNotFound
	}
	var raw []byte
	e = f.tx.QueryRow(ctx, `SELECT thumbnail FROM entity_covers WHERE entity_type=$1 AND entity_id=$2 AND version=$3 AND thumbnail IS NOT NULL`, r.Type, r.ID, version).Scan(&raw)
	sum := sha256.Sum256(raw)
	if e != nil || len(raw) != f.state.Cover.ByteCount || hex.EncodeToString(sum[:]) != f.state.Cover.SHA256 {
		return ec.OriginalContent{}, ec.ErrUnavailable
	}
	if e = s.finishEntityCover(ctx, f, false); e != nil {
		return ec.OriginalContent{}, e
	}
	return ec.OriginalContent{Metadata: *f.state.Cover, Bytes: raw, SourceProof: f.state.SourceProof}, nil
}
func (s *Store) ValidateEntityCoverProjection(ctx context.Context, a ec.Access, r ec.Ref, old *ec.Metadata, source string) error {
	current, e := s.ReadEntityCover(ctx, a, r)
	if e != nil {
		return e
	}
	if source == "" || source != current.SourceProof || !ec.PreserveLease(old, current.Cover, time.Now().UTC()) {
		return ec.ErrChanged
	}
	return nil
}
func (s *Store) ValidateEntityCoverPreview(ctx context.Context, a ec.Access, r ec.Ref, in ec.Input, out ec.Approval) error {
	_, sum, rights, e := ec.Normalize(in, time.Now().UTC())
	if e != nil {
		return e
	}
	f, e := s.beginEntityCover(ctx, a, r, true, false, "")
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	if f.state.Version != in.ExpectedVersion || out.Ref != r || out.OperationID != in.OperationID || out.InputSHA256 != sum || out.RightsSHA256 != rights {
		return ec.ErrChanged
	}
	var now time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return ec.ErrUnavailable
	}
	until, ok := validEntityCoverToken(a.SessionDigest, entityCoverMaterial(f, in, sum, rights), out.Token, now)
	if !ok || !until.Equal(out.ExpiresAt) {
		return ec.ErrChanged
	}
	f.until = until
	return s.finishEntityCover(ctx, f, true)
}
