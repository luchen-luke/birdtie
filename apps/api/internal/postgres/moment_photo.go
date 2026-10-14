package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"github.com/jackc/pgx/v5"
)

var _ mp.Store = (*Store)(nil)

func momentPhotoActor(a mp.Access) identity.Actor {
	return identity.Actor{ID: a.AccountID, AccountType: a.AccountType}
}
func (s *Store) beginMomentPhoto(ctx context.Context, a mp.Access, moment, op string, resource bool) (context.Context, humanWriteFrame, error) {
	ctx, first, e := s.humanWriteBound(ctx, a.SessionDigest, momentPhotoActor(a))
	f := humanWriteFrame{d: a.SessionDigest, a: momentPhotoActor(a), session: first, family: "MOMENT_PUBLIC_PHOTO", op: op}
	if e != nil {
		return ctx, f, e
	}
	if !identity.HumanControlUUID(moment) || (op != "" && !identity.HumanControlUUID(op)) {
		return ctx, f, mp.ErrInvalid
	}
	f.tx, e = s.pool.Begin(ctx)
	if e != nil {
		return ctx, f, mp.ErrUnavailable
	}
	fail := func(e error) (context.Context, humanWriteFrame, error) {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, e
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(mp.ErrUnavailable)
	}
	if op != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,145))`, a.AccountID+":"+op); e != nil {
			return fail(mp.ErrUnavailable)
		}
	}
	// Account -> Moment -> photo -> final original Session. No Agent resources.
	if resource {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,145))`, "moment-photo-slot:"+moment); e != nil {
			return fail(mp.ErrUnavailable)
		}
		var id string
		if e = f.tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.AccountID).Scan(&id); e != nil {
			return fail(mp.ErrChanged)
		}
		if e = f.tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR SHARE`, moment, a.AccountID).Scan(&id); errors.Is(e, pgx.ErrNoRows) {
			return fail(mp.ErrForbidden)
		} else if e != nil {
			return fail(mp.ErrUnavailable)
		}
		rows, e := f.tx.Query(ctx, `SELECT moment_id FROM public_moment_photos WHERE moment_id=$1 FOR UPDATE`, moment)
		if e != nil {
			return fail(mp.ErrUnavailable)
		}
		rows.Close()
	}
	return ctx, f, nil
}

type momentPhotoSource struct {
	state                          mp.State
	stamp, ownStamp, publicBinding string
	deadline                       *time.Time
}

func momentPhotoSourceTx(ctx context.Context, f humanWriteFrame, moment string) (momentPhotoSource, error) {
	var out momentPhotoSource
	out.state.MomentID = moment
	var raw, slot string
	var public *string
	var until *time.Time
	var photo []byte
	e := f.tx.QueryRow(ctx, `SELECT m.revision,jsonb_build_array(m.xmin::text,a.xmin::text)::text,
 image.photo_id::text,coalesce(image.version,0),coalesce(jsonb_build_array(image.xmin::text,image.photo_id,image.version,image.status)::text,'none'),
 source.binding,source.deadline,clock_timestamp(),birdtie_public_moment_photo_metadata(m.id,$2,clock_timestamp())
 FROM moments m JOIN accounts a ON a.id=m.author_account_id LEFT JOIN public_moment_photos image ON image.moment_id=m.id
 LEFT JOIN LATERAL birdtie_public_moment_photo_source(m.id,clock_timestamp()) source ON true
 WHERE m.id=$1 AND m.author_account_id=$2 AND a.account_type='person' AND a.status='active'`, moment, f.a.ID).Scan(&out.state.MomentRevision, &raw, &out.state.PhotoID, &out.state.PhotoVersion, &slot, &public, &until, &out.state.ObservedAt, &photo)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, mp.ErrChanged
	}
	if e != nil {
		return out, mp.ErrUnavailable
	}
	if public != nil {
		out.publicBinding = *public
	}
	out.deadline = until
	if len(photo) > 0 && json.Unmarshal(photo, &out.state.Photo) != nil {
		return out, mp.ErrUnavailable
	}
	out.ownStamp = identity.HumanControlTextHash(raw + ":" + slot)
	out.stamp = identity.HumanControlTextHash(raw + ":" + slot + ":" + out.publicBinding)
	out.state.SourceVersion = out.stamp
	out.state.CanSave = public != nil && out.state.PhotoVersion < mp.MaxVersion
	out.state.CanDelete = out.state.PhotoID != nil && out.state.PhotoVersion > 0
	return out, nil
}
func momentPhotoInputSource(in mp.Input, src momentPhotoSource) error {
	if in.ExpectedMomentRevision != src.state.MomentRevision || in.ExpectedPhotoVersion != src.state.PhotoVersion || (src.state.PhotoID != nil && in.PhotoID != *src.state.PhotoID) {
		return mp.ErrChanged
	}
	if in.Action == "SAVE" && !src.state.CanSave || in.Action == "DELETE" && !src.state.CanDelete {
		return mp.ErrChanged
	}
	return nil
}
func momentPhotoReceiptTx(ctx context.Context, f humanWriteFrame, moment string) (mp.Receipt, error) {
	r := mp.Receipt{OperationID: f.op}
	var target string
	var action, id *string
	var digest []byte
	var at time.Time
	e := f.tx.QueryRow(ctx, `SELECT moment_id,status,action,photo_id,expected_moment_revision,expected_photo_version,applied_photo_version,payload_sha256,committed_at FROM public_moment_photo_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&target, &r.Status, &action, &id, &r.ExpectedMomentRevision, &r.ExpectedPhotoVersion, &r.AppliedPhotoVersion, &digest, &at)
	if e != nil {
		return r, e
	}
	if target != moment {
		return r, mp.ErrChanged
	}
	if r.Status == "STOPPED" {
		return r, nil
	}
	if action == nil || id == nil || len(digest) != 32 {
		return r, mp.ErrUnavailable
	}
	r.MomentID = target
	r.PhotoID = *id
	r.Action = *action
	r.PayloadDigest = hex.EncodeToString(digest)
	at = at.UTC()
	r.CommittedAt = &at
	if !r.Valid(moment, f.op) {
		return r, mp.ErrUnavailable
	}
	return r, nil
}
func momentPhotoControlStamp(action string, src momentPhotoSource) string {
	if action == "DELETE" {
		return src.ownStamp
	}
	return src.stamp
}

type momentPhotoResponseProof struct {
	s                         *Store
	a                         mp.Access
	moment, op, stamp, action string
	frame                     identity.HumanControlFrame
	until                     time.Time
}

func (p *momentPhotoResponseProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.a.SessionDigest, momentPhotoActor(p.a), p.frame)
	ctx, f, e := p.s.beginMomentPhoto(ctx, p.a, p.moment, p.op, true)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	return p.s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.until, func() error {
		src, e := momentPhotoSourceTx(ctx, f, p.moment)
		if e != nil {
			return e
		}
		if momentPhotoControlStamp(p.action, src) != p.stamp || p.action != "DELETE" && src.deadline != nil && src.deadline.Before(p.until) {
			return mp.ErrChanged
		}
		return nil
	}))
}
func (s *Store) ReadMomentPhotoState(ctx context.Context, a mp.Access, moment string) (mp.State, error) {
	ctx, f, e := s.beginMomentPhoto(ctx, a, moment, "", true)
	if e != nil {
		return mp.State{}, e
	}
	defer f.tx.Rollback(context.Background())
	src, e := momentPhotoSourceTx(ctx, f, moment)
	if e != nil {
		return mp.State{}, e
	}
	deadline := src.deadline
	if src.state.Photo != nil {
		momentControlAddDeadline(&deadline, &src.state.Photo.ExpiresAt)
	}
	now, until, e := s.humanWriteNow(ctx, f, deadline)
	if e != nil {
		return mp.State{}, e
	}
	src.state.ObservedAt = now
	src.state.ExpiresAt = until
	if src.state.Photo != nil {
		if src.state.Photo.ExpiresAt.After(until) {
			src.state.Photo.ExpiresAt = until
		}
		if !src.state.Photo.Valid(now) {
			src.state.Photo = nil
		}
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		fresh, e := momentPhotoSourceTx(ctx, f, moment)
		if e != nil {
			return e
		}
		if fresh.stamp != src.stamp {
			return mp.ErrChanged
		}
		return nil
	})); e != nil {
		return mp.State{}, e
	}
	src.state.ResponseProof = &momentPhotoResponseProof{s: s, a: a, moment: moment, stamp: src.stamp, frame: f.session, until: until}
	return src.state, nil
}
func (s *Store) PreviewMomentPhoto(ctx context.Context, a mp.Access, moment string, in mp.Input) (mp.Preview, error) {
	d, sum, rights, e := mp.Normalize(in, false, time.Now().UTC())
	if e != nil {
		return mp.Preview{}, e
	}
	ctx, f, e := s.beginMomentPhoto(ctx, a, moment, in.OperationID, true)
	if e != nil {
		return mp.Preview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = momentPhotoReceiptTx(ctx, f, moment); e == nil {
		return mp.Preview{}, mp.ErrChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return mp.Preview{}, e
	}
	src, e := momentPhotoSourceTx(ctx, f, moment)
	if e != nil {
		return mp.Preview{}, e
	}
	if e = momentPhotoInputSource(in, src); e != nil {
		return mp.Preview{}, e
	}
	deadline := src.deadline
	if in.Action == "DELETE" {
		deadline = nil
	} else {
		momentControlAddDeadline(&deadline, &in.Rights.RetentionUntil)
	}
	stamp := momentPhotoControlStamp(in.Action, src)
	base, e := s.humanWritePreview(ctx, f, moment, in.Action, mp.Digest(moment, in, sum, rights), stamp, deadline)
	if e != nil {
		return mp.Preview{}, e
	}
	v := mp.Preview{HumanControlPreview: base, MomentID: moment, PhotoID: in.PhotoID, ExpectedMomentRevision: in.ExpectedMomentRevision, ExpectedPhotoVersion: in.ExpectedPhotoVersion, InputSHA256: sum, RightsSHA256: rights, SourceVersion: stamp}
	if in.Action == "SAVE" {
		v.PreviewDerivative = &mp.PreviewDerivative{ContentType: "image/jpeg", Width: mp.Width, Height: mp.Height, ByteCount: len(d.Bytes), SHA256: sum, ContentBase64: base64.StdEncoding.EncodeToString(d.Bytes)}
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error {
		fresh, e := momentPhotoSourceTx(ctx, f, moment)
		if e != nil {
			return e
		}
		if momentPhotoControlStamp(in.Action, fresh) != stamp {
			return mp.ErrChanged
		}
		return nil
	})); e != nil {
		return mp.Preview{}, e
	}
	v.ResponseProof = &momentPhotoResponseProof{s: s, a: a, moment: moment, op: in.OperationID, stamp: stamp, action: in.Action, frame: f.session, until: base.ExpiresAt}
	return v, nil
}
func (s *Store) CommitMomentPhoto(ctx context.Context, a mp.Access, moment string, in mp.Input) (mp.Receipt, error) {
	d, sum, rights, e := mp.Normalize(in, true, time.Now().UTC())
	if e != nil {
		return mp.Receipt{}, e
	}
	ctx, f, e := s.beginMomentPhoto(ctx, a, moment, in.OperationID, false)
	if e != nil {
		return mp.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := mp.Digest(moment, in, sum, rights)
	if old, e := momentPhotoReceiptTx(ctx, f, moment); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return mp.Receipt{}, mp.ErrChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return mp.Receipt{}, e
		}
		return old, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return mp.Receipt{}, e
	}
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,145))`, "moment-photo-slot:"+moment); e != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	var id string
	if e = f.tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.AccountID).Scan(&id); e != nil {
		return mp.Receipt{}, mp.ErrChanged
	}
	if e = f.tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR SHARE`, moment, a.AccountID).Scan(&id); e != nil {
		return mp.Receipt{}, mp.ErrForbidden
	}
	rows, e := f.tx.Query(ctx, `SELECT moment_id FROM public_moment_photos WHERE moment_id=$1 FOR UPDATE`, moment)
	if e != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	rows.Close()
	src, e := momentPhotoSourceTx(ctx, f, moment)
	if e != nil {
		return mp.Receipt{}, e
	}
	if e = momentPhotoInputSource(in, src); e != nil {
		return mp.Receipt{}, e
	}
	stamp := momentPhotoControlStamp(in.Action, src)
	if stamp != in.SourceVersion {
		return mp.Receipt{}, mp.ErrChanged
	}
	deadline := src.deadline
	if in.Action == "DELETE" {
		deadline = nil
	} else {
		momentControlAddDeadline(&deadline, &in.Rights.RetentionUntil)
	}
	until, e := s.humanWriteApprove(ctx, f, moment, in.Action, digest, stamp, in.Approval, deadline)
	if e != nil {
		return mp.Receipt{}, e
	}
	version := mp.NextVersion(in.ExpectedPhotoVersion)
	if in.Action == "SAVE" {
		license, _ := json.Marshal(in.Rights)
		_, e = f.tx.Exec(ctx, `INSERT INTO public_moment_photos(moment_id,photo_id,version,approved_moment_revision,owner_account_id,thumbnail,sha256,rights,retention_until,source_binding,attribution,status)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'READY') ON CONFLICT(moment_id) DO UPDATE SET version=EXCLUDED.version,approved_moment_revision=EXCLUDED.approved_moment_revision,owner_account_id=EXCLUDED.owner_account_id,thumbnail=EXCLUDED.thumbnail,sha256=EXCLUDED.sha256,rights=EXCLUDED.rights,retention_until=EXCLUDED.retention_until,source_binding=EXCLUDED.source_binding,attribution=EXCLUDED.attribution,status='READY',updated_at=clock_timestamp()`, moment, in.PhotoID, version, in.ExpectedMomentRevision, a.AccountID, d.Bytes, sum, license, in.Rights.RetentionUntil, src.publicBinding, in.Rights.Attribution)
	} else {
		_, e = f.tx.Exec(ctx, `UPDATE public_moment_photos SET version=$2,status='DELETED',thumbnail=NULL,sha256=NULL,updated_at=clock_timestamp() WHERE moment_id=$1`, moment, version)
	}
	if e != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	if _, e = auditExec(ctx, f.tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,$2,'moment',$3,'allowed','explicit_public_moment_photo')`, a.AccountID, "public_photo_"+in.Action, moment); e != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	post, e := momentPhotoSourceTx(ctx, f, moment)
	if e != nil {
		return mp.Receipt{}, e
	}
	b, _ := hex.DecodeString(digest)
	var at time.Time
	e = f.tx.QueryRow(ctx, `INSERT INTO public_moment_photo_operations(actor_account_id,operation_id,moment_id,status,action,photo_id,expected_moment_revision,expected_photo_version,applied_photo_version,payload_sha256)VALUES($1,$2,$3,'SUCCEEDED',$4,$5,$6,$7,$8,$9)RETURNING committed_at`, a.AccountID, in.OperationID, moment, in.Action, in.PhotoID, in.ExpectedMomentRevision, in.ExpectedPhotoVersion, version, b).Scan(&at)
	if e != nil {
		return mp.Receipt{}, mp.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		fresh, e := momentPhotoSourceTx(ctx, f, moment)
		if e != nil {
			return e
		}
		if momentPhotoControlStamp(in.Action, fresh) != momentPhotoControlStamp(in.Action, post) || in.Action == "SAVE" && (fresh.publicBinding != src.publicBinding || fresh.deadline != nil && fresh.deadline.Before(until)) {
			return mp.ErrChanged
		}
		return nil
	})); e != nil {
		return mp.Receipt{}, e
	}
	oldRevision, oldVersion := in.ExpectedMomentRevision, in.ExpectedPhotoVersion
	at = at.UTC()
	return mp.Receipt{OperationID: in.OperationID, Status: "SUCCEEDED", Action: in.Action, MomentID: moment, PhotoID: in.PhotoID, ExpectedMomentRevision: &oldRevision, ExpectedPhotoVersion: &oldVersion, AppliedPhotoVersion: version, PayloadDigest: digest, CommittedAt: &at}, nil
}
func (s *Store) momentPhotoHistory(ctx context.Context, a mp.Access, moment, op string, stop bool) (mp.Receipt, error) {
	ctx, f, e := s.beginMomentPhoto(ctx, a, moment, op, false)
	if e != nil {
		return mp.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := momentPhotoReceiptTx(ctx, f, moment)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.humanWriteFinish(ctx, f); e != nil {
				return r, e
			}
			return r, mp.ErrNotFound
		}
		if _, e = f.tx.Exec(ctx, `INSERT INTO public_moment_photo_operations(actor_account_id,operation_id,moment_id,status,applied_photo_version)VALUES($1,$2,$3,'STOPPED',0)`, a.AccountID, op, moment); e != nil {
			return r, mp.ErrUnavailable
		}
		r.Status = "STOPPED"
	} else if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return mp.Receipt{}, e
	}
	return r, nil
}
func (s *Store) ReadMomentPhotoOperation(ctx context.Context, a mp.Access, moment, op string) (mp.Receipt, error) {
	return s.momentPhotoHistory(ctx, a, moment, op, false)
}
func (s *Store) StopMomentPhotoOperation(ctx context.Context, a mp.Access, moment, op string) (mp.Receipt, error) {
	return s.momentPhotoHistory(ctx, a, moment, op, true)
}

func (s *Store) ReadMomentPhotoContent(ctx context.Context, a mp.Access, selector mp.Selector, encode mp.Encode) ([]byte, error) {
	if !selector.Valid() || encode == nil {
		return nil, mp.ErrInvalid
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, a, selector.MomentID)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	var rawMeta, raw []byte
	var proof string
	e = tx.QueryRow(ctx, `SELECT birdtie_public_moment_photo_metadata(image.moment_id,$2,clock_timestamp()),image.thumbnail,birdtie_public_moment_photo_proof(image.moment_id,$2) FROM public_moment_photos image WHERE image.moment_id=$1 AND image.photo_id=$3 AND image.version=$4 AND image.approved_moment_revision=$5`, selector.MomentID, momentPhotoViewer(a), selector.PhotoID, selector.Version, selector.MomentRevision).Scan(&rawMeta, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, mp.ErrNotFound
	}
	if e != nil {
		return nil, mp.ErrUnavailable
	}
	var meta *mp.Metadata
	if json.Unmarshal(rawMeta, &meta) != nil || meta == nil || !meta.Valid(time.Now().UTC()) {
		return nil, mp.ErrNotFound
	}
	sum := sha256.Sum256(raw)
	if len(raw) != meta.ByteCount || hex.EncodeToString(sum[:]) != meta.SHA256 {
		return nil, mp.ErrUnavailable
	}
	encoded, e := encode(*meta, append([]byte(nil), raw...))
	if e != nil {
		return nil, mp.ErrUnavailable
	}
	if e = s.finishPublicMomentPhoto(ctx, tx, a, frame, func() error {
		var latest string
		var current, lease bool
		e := tx.QueryRow(ctx, `SELECT birdtie_public_moment_photo_proof($1,$2),birdtie_public_moment_photo_current($1,$2,clock_timestamp()),clock_timestamp()<$3`, selector.MomentID, momentPhotoViewer(a), meta.ExpiresAt).Scan(&latest, &current, &lease)
		if e != nil {
			return mp.ErrUnavailable
		}
		if !current || !lease || latest != proof || !meta.ExpiresAt.After(time.Now().UTC()) {
			return mp.ErrChanged
		}
		return nil
	}); e != nil {
		return nil, e
	}
	return encoded, nil
}
