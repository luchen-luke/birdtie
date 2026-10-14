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

	ec "github.com/birdtie/birdtie/apps/api/internal/entitycover"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
	"github.com/jackc/pgx/v5"
)

var _ pp.Store = (*Store)(nil)
var _ pp.SessionStore = (*Store)(nil)

func (s *Store) CapturePlacePhotoSession(ctx context.Context, a pp.Access) (string, error) {
	if a.AccountType != "person" {
		return "", pp.ErrForbidden
	}
	tx, e := s.beginHumanMomentTx(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	if e != nil {
		return "", placePhotoError(e)
	}
	defer tx.Rollback(context.Background())
	var id string
	var gen int64
	if e = tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.AccountID, a.SessionDigest[:]).Scan(&id, &gen); e != nil {
		return "", pp.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID); e != nil {
		return "", placePhotoError(e)
	}
	var current int64
	if e = tx.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, id).Scan(&current); e != nil {
		return "", pp.ErrUnavailable
	}
	if current != gen {
		return "", pp.ErrChanged
	}
	if e = s.finishHumanMomentTx(ctx, tx, a.SessionDigest, a.AccountID); e != nil {
		return "", placePhotoError(e)
	}
	return id + ":" + strconv.FormatInt(gen, 10), nil
}
func (s *Store) ValidatePlacePhotoSession(ctx context.Context, a pp.Access, proof string) error {
	current, e := s.CapturePlacePhotoSession(ctx, a)
	if e != nil {
		return e
	}
	if proof == "" || current != proof {
		return pp.ErrChanged
	}
	return nil
}

const placePhotoRelations = `LOCK TABLE place_photo_candidates,place_photo_publications,place_photo_operations,places,cities,city_editor_memberships,accounts,sessions,place_sources,place_field_evidence,place_source_permissions,place_candidates IN ACCESS SHARE MODE`

type placePhotoFence struct {
	tx                         pgx.Tx
	access                     pp.Access
	id, city, session, source  string
	generation                 int64
	state                      pp.State
	until                      time.Time
	write                      bool
	effectAction, effectOrigin string
}

func placePhotoError(e error) error {
	if e == nil {
		return nil
	}
	for _, known := range []error{pp.ErrInvalid, pp.ErrChanged, pp.ErrForbidden, pp.ErrNotFound, identity.ErrUnauthorized} {
		if errors.Is(e, known) {
			return known
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return pp.ErrNotFound
	}
	return pp.ErrUnavailable
}
func (s *Store) beginPlacePhoto(ctx context.Context, a pp.Access, id, op string, write bool) (placePhotoFence, error) {
	f := placePhotoFence{access: a, id: id, write: write}
	if !pp.ValidID(id) || op != "" && !pp.ValidID(op) {
		return f, pp.ErrInvalid
	}
	var e error
	if a.AccountID == "" {
		if a.AccountType != "" || a.SessionDigest != ([32]byte{}) || write {
			return f, pp.ErrForbidden
		}
		f.tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	} else {
		if a.AccountType != "person" {
			return f, pp.ErrForbidden
		}
		f.tx, e = s.beginHumanMomentTx(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	}
	if e != nil {
		return f, placePhotoError(e)
	}
	fail := func(e error) (placePhotoFence, error) {
		f.tx.Rollback(context.Background())
		return placePhotoFence{}, placePhotoError(e)
	}
	if a.AccountID != "" {
		if e = f.tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.AccountID, a.SessionDigest[:]).Scan(&f.session, &f.generation); e != nil {
			return fail(e)
		}
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';`+placePhotoRelations); e != nil {
		return fail(e)
	}
	if op != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,138))`, a.AccountID+":"+op); e != nil {
			return fail(e)
		}
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	if e = f.tx.QueryRow(ctx, `SELECT city_id FROM places WHERE id=$1`+lock, id).Scan(&f.city); e != nil {
		return fail(e)
	}
	if e = f.tx.QueryRow(ctx, `SELECT birdtie_place_photo_authority($1)`, id).Scan(&f.source); e != nil {
		return fail(e)
	}
	f.state, e = placePhotoState(ctx, f.tx, id, time.Now().UTC())
	if e != nil {
		return fail(e)
	}
	return f, nil
}
func placePhotoState(ctx context.Context, tx pgx.Tx, id string, at time.Time) (pp.State, error) {
	out := pp.State{}
	e := tx.QueryRow(ctx, `SELECT coalesce((SELECT version FROM place_photo_publications WHERE place_id=$1),0),birdtie_place_photo_metadata($1,$2),birdtie_place_photo_proof($1)`, id, at).Scan(&out.Version, &out.PlacePhoto, &out.SourceProof)
	if e != nil || out.Version < 0 || out.Version > pp.MaxVersion {
		return out, pp.ErrUnavailable
	}
	if out.PlacePhoto != nil {
		out.PlacePhoto.ExpiresAt = out.PlacePhoto.ExpiresAt.UTC()
		if !out.PlacePhoto.Valid(at) || out.PlacePhoto.EntityID != id || out.PlacePhoto.Version != out.Version {
			return pp.State{}, pp.ErrUnavailable
		}
	}
	return out, nil
}
func (s *Store) finishPlacePhoto(ctx context.Context, f placePhotoFence) error {
	if f.access.AccountID != "" {
		if e := s.lockHumanMomentSession(ctx, f.tx, f.access.SessionDigest, f.access.AccountID); e != nil {
			return placePhotoError(e)
		}
		var id string
		var gen int64
		if e := f.tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, f.access.AccountID, f.access.SessionDigest[:]).Scan(&id, &gen); e != nil {
			return pp.ErrUnavailable
		} else if id != f.session || gen != f.generation {
			return pp.ErrChanged
		}
	}
	var source string
	var at time.Time
	if e := f.tx.QueryRow(ctx, `SELECT birdtie_place_photo_authority($1),clock_timestamp()`, f.id).Scan(&source, &at); e != nil {
		return pp.ErrUnavailable
	}
	if source != f.source || !f.until.IsZero() && !f.until.After(at) {
		return pp.ErrChanged
	}
	if f.effectAction == "SUBMIT" || f.effectAction == "APPROVE" {
		if e := photoVisible(ctx, f.tx, f.id); e != nil {
			return pp.ErrChanged
		}
		if !photoEditor(ctx, f.tx, f.id, f.access.AccountID, f.effectAction == "APPROVE") || f.effectAction == "APPROVE" && !photoEditor(ctx, f.tx, f.id, f.effectOrigin, false) {
			return pp.ErrChanged
		}
	}
	if !f.write {
		current, e := placePhotoState(ctx, f.tx, f.id, at.UTC())
		if e != nil {
			return e
		}
		if current.SourceProof != f.state.SourceProof || !pp.PreserveLease(f.state.PlacePhoto, current.PlacePhoto, at.UTC()) {
			return pp.ErrChanged
		}
	}
	if f.access.AccountID != "" {
		return placePhotoError(s.finishHumanMomentTx(ctx, f.tx, f.access.SessionDigest, f.access.AccountID))
	}
	if ctx.Err() != nil {
		return pp.ErrUnavailable
	}
	return placePhotoError(f.tx.Commit(ctx))
}
func photoVisible(ctx context.Context, tx pgx.Tx, id string) error {
	var yes bool
	if e := tx.QueryRow(ctx, `SELECT birdtie_place_photo_visible($1,clock_timestamp())`, id).Scan(&yes); e != nil {
		return pp.ErrUnavailable
	}
	if !yes {
		return pp.ErrNotFound
	}
	return nil
}
func photoEditor(ctx context.Context, tx pgx.Tx, id, actor string, review bool) bool {
	var yes bool
	return tx.QueryRow(ctx, `SELECT birdtie_place_photo_editor($1,$2,$3)`, id, actor, review).Scan(&yes) == nil && yes
}
func photoSessionProof(f placePhotoFence, proof string) string {
	if f.access.AccountID == "" {
		return proof
	}
	return proof + ";session=" + f.session + ":" + strconv.FormatInt(f.generation, 10)
}
func (s *Store) ReadPlacePhoto(ctx context.Context, a pp.Access, id string) (pp.State, error) {
	f, e := s.beginPlacePhoto(ctx, a, id, "", false)
	if e != nil {
		return pp.State{}, e
	}
	defer f.tx.Rollback(context.Background())
	if e = photoVisible(ctx, f.tx, id); e != nil {
		return pp.State{}, e
	}
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return pp.State{}, e
	}
	out := f.state
	out.SourceProof = photoSessionProof(f, out.SourceProof)
	return out, nil
}
func (s *Store) ValidatePlacePhotoProjection(ctx context.Context, a pp.Access, id string, m *pp.Metadata, proof string) error {
	readAccess := a
	if !strings.Contains(proof, ";session=") {
		readAccess = pp.Access{}
	}
	current, e := s.ReadPlacePhoto(ctx, readAccess, id)
	if e != nil {
		return e
	}
	if proof == "" || current.SourceProof != proof || !pp.PreserveLease(m, current.PlacePhoto, time.Now().UTC()) {
		return pp.ErrChanged
	}
	return nil
}
func photoCandidate(ctx context.Context, tx pgx.Tx, id, place string, write bool) (pp.Candidate, error) {
	var c pp.Candidate
	var raw []byte
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT id,place_id,submitted_by,version,status,expected_version,rights,thumbnail,sha256,xmin::text FROM place_photo_candidates WHERE id=$1 AND place_id=$2`+lock, id, place).Scan(&c.ID, &c.PlaceID, &c.SubmittedBy, &c.Version, &c.Status, &c.ExpectedVersion, &raw, &c.Bytes, &c.SHA256, &c.SourceProof)
	if e != nil {
		return c, placePhotoError(e)
	}
	if json.Unmarshal(raw, &c.Rights) != nil {
		return c, pp.ErrUnavailable
	}
	c.Rights.RetentionUntil = c.Rights.RetentionUntil.UTC()
	return c, nil
}
func photoActionAllowed(ctx context.Context, f placePhotoFence, in pp.Input, c pp.Candidate) error {
	if f.access.AccountType != "person" {
		return pp.ErrForbidden
	}
	switch in.Action {
	case "SUBMIT":
		if !photoEditor(ctx, f.tx, f.id, f.access.AccountID, false) {
			return pp.ErrForbidden
		}
		return photoVisible(ctx, f.tx, f.id)
	case "APPROVE", "REJECT":
		if !photoEditor(ctx, f.tx, f.id, f.access.AccountID, true) || c.SubmittedBy == f.access.AccountID {
			return pp.ErrForbidden
		}
		if c.Status != "PENDING" || c.Version != in.CandidateVersion || len(c.Bytes) == 0 || !c.Rights.Valid(time.Now().UTC()) {
			return pp.ErrChanged
		}
		if in.Action == "APPROVE" {
			if !photoEditor(ctx, f.tx, f.id, c.SubmittedBy, false) {
				return pp.ErrChanged
			}
			return photoVisible(ctx, f.tx, f.id)
		}
	case "WITHDRAW":
		if c.SubmittedBy != f.access.AccountID && !photoEditor(ctx, f.tx, f.id, f.access.AccountID, true) {
			return pp.ErrForbidden
		}
		if c.Version != in.CandidateVersion || c.Status == "WITHDRAWN" || c.Status == "EXPIRED" {
			return pp.ErrChanged
		}
	default:
		return pp.ErrInvalid
	}
	return nil
}
func photoInput(ctx context.Context, f placePhotoFence, in pp.Input) (pp.Candidate, ec.Derivative, string, string, error) {
	d, e := pp.Normalize(in, time.Now().UTC())
	if e != nil {
		return pp.Candidate{}, d, "", "", e
	}
	c := pp.Candidate{ID: in.OperationID, PlaceID: f.id, SubmittedBy: f.access.AccountID}
	sum, rights := d.SHA256, ""
	if in.Action == "SUBMIT" {
		c.Rights = *in.Rights
		rights = c.Rights.Digest()
	} else {
		c, e = photoCandidate(ctx, f.tx, in.CandidateID, f.id, f.write)
		if e != nil {
			return c, d, "", "", e
		}
		sum, rights = c.SHA256, c.Rights.Digest()
		d = ec.Derivative{Bytes: c.Bytes, SHA256: c.SHA256}
	}
	if e = photoActionAllowed(ctx, f, in, c); e != nil {
		return c, d, "", "", e
	}
	return c, d, sum, rights, nil
}
func photoMaterial(f placePhotoFence, in pp.Input, c pp.Candidate, sum, rights string) string {
	raw, _ := json.Marshal([]any{"PLACE_PHOTO_APPROVAL_138", f.id, f.city, f.access.AccountID, f.session, f.generation, f.source, c.ID, c.SourceProof, pp.Digest(f.id, in, sum, rights)})
	return string(raw)
}
func photoToken(a pp.Access, material string, start, end int64) string {
	payload := strconv.FormatInt(start, 10) + "." + strconv.FormatInt(end, 10)
	m := hmac.New(sha256.New, a.SessionDigest[:])
	m.Write([]byte(material + "\x00" + payload))
	return "pp1." + payload + "." + hex.EncodeToString(m.Sum(nil))
}
func validPhotoToken(a pp.Access, material, token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "pp1" {
		return false
	}
	start, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		return false
	}
	end, e := strconv.ParseInt(parts[2], 10, 64)
	if e != nil || start > now.UnixNano() || end <= now.UnixNano() || end-start > int64(30*time.Second) || end <= start {
		return false
	}
	return hmac.Equal([]byte(token), []byte(photoToken(a, material, start, end)))
}
func photoDerivative(d ec.Derivative) *pp.Derivative {
	return &pp.Derivative{ContentType: "image/jpeg", Width: ec.Width, Height: ec.Height, ByteCount: len(d.Bytes), SHA256: d.SHA256, ContentBase64: base64.StdEncoding.EncodeToString(d.Bytes)}
}
func (s *Store) PreviewPlacePhoto(ctx context.Context, a pp.Access, id string, in pp.Input) (pp.Approval, error) {
	f, e := s.beginPlacePhoto(ctx, a, id, "", false)
	if e != nil {
		return pp.Approval{}, e
	}
	defer f.tx.Rollback(context.Background())
	c, d, sum, rights, e := photoInput(ctx, f, in)
	if e != nil {
		return pp.Approval{}, e
	}
	if f.state.Version != in.ExpectedVersion {
		return pp.Approval{}, pp.ErrChanged
	}
	var at time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return pp.Approval{}, pp.ErrUnavailable
	}
	until := at.Add(30 * time.Second)
	if in.Action != "WITHDRAW" && c.Rights.RetentionUntil.Before(until) {
		until = c.Rights.RetentionUntil
	}
	if !until.After(at) {
		return pp.Approval{}, pp.ErrChanged
	}
	if in.Action == "SUBMIT" || in.Action == "APPROVE" {
		var deadline *time.Time
		e = f.tx.QueryRow(ctx, `SELECT least(p.expires_at,city.expires_at,`+currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()")+`) FROM places p JOIN cities city ON city.id=p.city_id WHERE p.id=$1`, id).Scan(&deadline)
		if e != nil {
			return pp.Approval{}, pp.ErrUnavailable
		}
		if deadline != nil && deadline.Before(until) {
			until = *deadline
		}
		if !until.After(at) {
			return pp.Approval{}, pp.ErrChanged
		}
	}
	out := pp.Approval{PlaceID: id, OperationID: in.OperationID, ExpectedVersion: in.ExpectedVersion, Action: in.Action, CandidateID: c.ID, CandidateVersion: in.CandidateVersion, InputSHA256: sum, RightsSHA256: rights, ExpiresAt: until.UTC(), Token: photoToken(a, photoMaterial(f, in, c, sum, rights), at.UnixNano(), until.UnixNano())}
	if in.Action != "WITHDRAW" {
		out.PreviewDerivative = photoDerivative(d)
	}
	f.until = until
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return pp.Approval{}, e
	}
	return out, nil
}
func (s *Store) ValidatePlacePhotoPreview(ctx context.Context, a pp.Access, id string, in pp.Input, out pp.Approval) error {
	f, e := s.beginPlacePhoto(ctx, a, id, "", false)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	c, _, sum, rights, e := photoInput(ctx, f, in)
	if e != nil {
		return e
	}
	if out.PlaceID != id || out.OperationID != in.OperationID || out.Action != in.Action || out.ExpectedVersion != f.state.Version || out.CandidateID != c.ID || out.CandidateVersion != in.CandidateVersion || out.InputSHA256 != sum || out.RightsSHA256 != rights || !validPhotoToken(a, photoMaterial(f, in, c, sum, rights), out.Token, time.Now().UTC()) {
		return pp.ErrChanged
	}
	f.until = out.ExpiresAt
	return s.finishPlacePhoto(ctx, f)
}
func photoReceipt(ctx context.Context, tx pgx.Tx, a pp.Access, id, op string) (pp.Receipt, string, int64, error) {
	r := pp.Receipt{PlaceID: id, OperationID: op}
	var digest string
	var expected int64
	e := tx.QueryRow(ctx, `SELECT place_id,action,coalesce(candidate_id::text,''),candidate_version,applied_version,status,input_sha256,expected_version FROM place_photo_operations WHERE actor_account_id=$1 AND operation_id=$2`, a.AccountID, op).Scan(&r.PlaceID, &r.Action, &r.CandidateID, &r.CandidateVersion, &r.AppliedVersion, &r.Status, &digest, &expected)
	if e == nil && !r.Valid(id, op) {
		return pp.Receipt{}, "", 0, pp.ErrChanged
	}
	return r, digest, expected, e
}
func (s *Store) ChangePlacePhoto(ctx context.Context, a pp.Access, id string, in pp.Input) (pp.Receipt, error) {
	if _, e := pp.Normalize(in, time.Now().UTC()); e != nil {
		return pp.Receipt{}, e
	}
	f, e := s.beginPlacePhoto(ctx, a, id, in.OperationID, true)
	if e != nil {
		return pp.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	old, digest, expected, e := photoReceipt(ctx, f.tx, a, id, in.OperationID)
	if e == nil {
		if expected != in.ExpectedVersion {
			return pp.Receipt{}, pp.ErrChanged
		}
		if old.Status != "STOPPED" {
			image, rights := "", ""
			if in.Action == "SUBMIT" {
				d, _ := pp.Normalize(in, time.Now().UTC())
				image, rights = d.SHA256, in.Rights.Digest()
			} else {
				c, e := photoCandidate(ctx, f.tx, in.CandidateID, id, false)
				if e != nil {
					return pp.Receipt{}, e
				}
				image, rights = c.SHA256, c.Rights.Digest()
			}
			if digest != pp.Digest(id, in, image, rights) {
				return pp.Receipt{}, pp.ErrChanged
			}
		}
		if e = s.finishPlacePhoto(ctx, f); e != nil {
			return pp.Receipt{}, e
		}
		old.ResponseProof = f.session + ":" + strconv.FormatInt(f.generation, 10)
		return old, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return pp.Receipt{}, placePhotoError(e)
	}
	c, d, sum, rights, e := photoInput(ctx, f, in)
	if e != nil {
		return pp.Receipt{}, e
	}
	if f.state.Version != in.ExpectedVersion || !validPhotoToken(a, photoMaterial(f, in, c, sum, rights), in.Approval, time.Now().UTC()) {
		return pp.Receipt{}, pp.ErrChanged
	}
	r := pp.Receipt{PlaceID: id, OperationID: in.OperationID, Action: in.Action, CandidateID: c.ID, Status: "SUCCEEDED"}
	f.until = time.Now().Add(30 * time.Second)
	f.effectAction, f.effectOrigin = in.Action, c.SubmittedBy
	parts := strings.Split(in.Approval, ".")
	end, _ := strconv.ParseInt(parts[2], 10, 64)
	f.until = time.Unix(0, end)
	switch in.Action {
	case "SUBMIT":
		raw, _ := json.Marshal(c.Rights)
		_, e = f.tx.Exec(ctx, `INSERT INTO place_photo_candidates(id,place_id,city_id,submitted_by,updated_by,version,expected_version,status,thumbnail,sha256,rights,rights_sha256,retention_until)VALUES($1,$2,$3,$4,$4,1,$5,'PENDING',$6,$7,$8,$9,$10)`, c.ID, id, f.city, a.AccountID, in.ExpectedVersion, d.Bytes, sum, raw, rights, c.Rights.RetentionUntil)
		r.CandidateVersion = 1
	case "APPROVE":
		if in.ExpectedVersion >= pp.MaxVersion || c.Version >= pp.MaxVersion {
			return pp.Receipt{}, pp.ErrChanged
		}
		_, e = f.tx.Exec(ctx, `UPDATE place_photo_candidates SET status='APPROVED',reviewed_by=$2,updated_by=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, c.ID, a.AccountID)
		r.CandidateVersion = c.Version + 1
		r.AppliedVersion = in.ExpectedVersion + 1
		if e == nil {
			_, e = f.tx.Exec(ctx, `INSERT INTO place_photo_publications(place_id,version,candidate_id)VALUES($1,$2,$3) ON CONFLICT(place_id) DO UPDATE SET version=EXCLUDED.version,candidate_id=EXCLUDED.candidate_id,updated_at=clock_timestamp()`, id, r.AppliedVersion, c.ID)
		}
	case "REJECT", "WITHDRAW":
		status := "REJECTED"
		var reviewer any = a.AccountID
		if in.Action == "WITHDRAW" {
			status = "WITHDRAWN"
			reviewer = nil
		}
		_, e = f.tx.Exec(ctx, `UPDATE place_photo_candidates SET status=$2,thumbnail=NULL,version=least(version+1,9007199254740991),reviewed_by=coalesce($3,reviewed_by),updated_by=$4,updated_at=clock_timestamp() WHERE id=$1`, c.ID, status, reviewer, a.AccountID)
		r.CandidateVersion = c.Version + 1
		if r.CandidateVersion > pp.MaxVersion {
			r.CandidateVersion = pp.MaxVersion
		}
		if e == nil && in.Action == "WITHDRAW" {
			var v int64
			e = f.tx.QueryRow(ctx, `UPDATE place_photo_publications SET candidate_id=NULL,version=least(version+1,9007199254740991),updated_at=clock_timestamp() WHERE place_id=$1 AND candidate_id=$2 RETURNING version`, id, c.ID).Scan(&v)
			if errors.Is(e, pgx.ErrNoRows) {
				e = nil
			} else {
				r.AppliedVersion = v
			}
		}
	}
	if e != nil {
		return pp.Receipt{}, placePhotoError(e)
	}
	_, e = f.tx.Exec(ctx, `INSERT INTO place_photo_operations(actor_account_id,operation_id,place_id,expected_version,action,candidate_id,candidate_version,applied_version,status,input_sha256)VALUES($1,$2,$3,$4,$5,$6,$7,$8,'SUCCEEDED',$9)`, a.AccountID, in.OperationID, id, in.ExpectedVersion, in.Action, c.ID, r.CandidateVersion, r.AppliedVersion, pp.Digest(id, in, sum, rights))
	if e != nil {
		return pp.Receipt{}, placePhotoError(e)
	}
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return pp.Receipt{}, e
	}
	r.ResponseProof = f.session + ":" + strconv.FormatInt(f.generation, 10)
	return r, nil
}
func (s *Store) ReadPlacePhotoOperation(ctx context.Context, a pp.Access, id, op string) (pp.Receipt, error) {
	if a.AccountType != "person" || !pp.ValidID(op) {
		return pp.Receipt{}, pp.ErrForbidden
	}
	f, e := s.beginPlacePhoto(ctx, a, id, "", false)
	if e != nil {
		return pp.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	f.write = true
	r, _, _, e := photoReceipt(ctx, f.tx, a, id, op)
	if e != nil {
		return pp.Receipt{}, placePhotoError(e)
	}
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return pp.Receipt{}, e
	}
	r.ResponseProof = f.session + ":" + strconv.FormatInt(f.generation, 10)
	return r, nil
}
func (s *Store) StopPlacePhotoOperation(ctx context.Context, a pp.Access, id, op string, expected int64) (pp.Receipt, error) {
	if a.AccountType != "person" || !pp.ValidID(op) || expected < 0 || expected > pp.MaxVersion {
		return pp.Receipt{}, pp.ErrInvalid
	}
	f, e := s.beginPlacePhoto(ctx, a, id, op, true)
	if e != nil {
		return pp.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, _, oldExpected, e := photoReceipt(ctx, f.tx, a, id, op)
	if e == nil {
		if oldExpected != expected {
			return pp.Receipt{}, pp.ErrChanged
		}
		if e = s.finishPlacePhoto(ctx, f); e != nil {
			return pp.Receipt{}, e
		}
		r.ResponseProof = f.session + ":" + strconv.FormatInt(f.generation, 10)
		return r, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return pp.Receipt{}, placePhotoError(e)
	}
	var own bool
	if e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM place_photo_candidates WHERE place_id=$1 AND submitted_by=$2)`, id, a.AccountID).Scan(&own); e != nil {
		return pp.Receipt{}, pp.ErrUnavailable
	}
	if !own && !photoEditor(ctx, f.tx, id, a.AccountID, false) {
		return pp.Receipt{}, pp.ErrForbidden
	}
	empty := sha256.Sum256([]byte("PLACE_PHOTO_STOPPED_138"))
	_, e = f.tx.Exec(ctx, `INSERT INTO place_photo_operations(actor_account_id,operation_id,place_id,expected_version,action,candidate_version,applied_version,status,input_sha256)VALUES($1,$2,$3,$4,'STOP',0,0,'STOPPED',$5)`, a.AccountID, op, id, expected, hex.EncodeToString(empty[:]))
	if e != nil {
		return pp.Receipt{}, placePhotoError(e)
	}
	r = pp.Receipt{PlaceID: id, OperationID: op, Action: "STOP", Status: "STOPPED"}
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return pp.Receipt{}, e
	}
	r.ResponseProof = f.session + ":" + strconv.FormatInt(f.generation, 10)
	return r, nil
}
func (s *Store) ReadPlacePhotoContent(ctx context.Context, a pp.Access, id string, version int64) (pp.Metadata, []byte, error) {
	f, e := s.beginPlacePhoto(ctx, a, id, "", false)
	if e != nil {
		return pp.Metadata{}, nil, e
	}
	defer f.tx.Rollback(context.Background())
	if f.state.PlacePhoto == nil || f.state.Version != version {
		return pp.Metadata{}, nil, pp.ErrNotFound
	}
	var raw []byte
	if e = f.tx.QueryRow(ctx, `SELECT c.thumbnail FROM place_photo_candidates c JOIN place_photo_publications p ON p.candidate_id=c.id WHERE p.place_id=$1 AND p.version=$2`, id, version).Scan(&raw); e != nil {
		return pp.Metadata{}, nil, placePhotoError(e)
	}
	sum := sha256.Sum256(raw)
	if len(raw) != f.state.PlacePhoto.ByteCount || hex.EncodeToString(sum[:]) != f.state.PlacePhoto.SHA256 {
		return pp.Metadata{}, nil, pp.ErrUnavailable
	}
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return pp.Metadata{}, nil, e
	}
	m := *f.state.PlacePhoto
	m.ResponseProof = photoSessionProof(f, f.state.SourceProof)
	return m, raw, nil
}
func photoEditorialProof(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var proof string
	e := tx.QueryRow(ctx, `SELECT birdtie_place_photo_proof($1)||':'||coalesce((SELECT jsonb_agg(jsonb_build_array(id,version,xmin::text)ORDER BY id)::text FROM place_photo_candidates WHERE place_id=$1),'none')`, id).Scan(&proof)
	return proof, placePhotoError(e)
}
func (s *Store) ReadPlacePhotoEditorial(ctx context.Context, a pp.Access, id string) (pp.Editorial, error) {
	out := pp.Editorial{PlaceID: id, Candidates: []pp.Candidate{}}
	if a.AccountType != "person" {
		return out, pp.ErrForbidden
	}
	f, e := s.beginPlacePhoto(ctx, a, id, "", false)
	if e != nil {
		return out, e
	}
	defer f.tx.Rollback(context.Background())
	out.CityID, out.Version = f.city, f.state.Version
	out.CanContribute = photoEditor(ctx, f.tx, id, a.AccountID, false)
	out.CanReview = photoEditor(ctx, f.tx, id, a.AccountID, true)
	publicCurrent := photoVisible(ctx, f.tx, id) == nil
	editorRole := out.CanContribute
	out.CanContribute = out.CanContribute && publicCurrent
	var own bool
	if e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM place_photo_candidates WHERE place_id=$1 AND submitted_by=$2)`, id, a.AccountID).Scan(&own); e != nil {
		return out, pp.ErrUnavailable
	}
	if !editorRole && !own {
		return out, pp.ErrForbidden
	}
	rows, e := f.tx.Query(ctx, `SELECT id FROM place_photo_candidates WHERE place_id=$1 AND(submitted_by=$2 OR $3)ORDER BY created_at DESC,id LIMIT 20`, id, a.AccountID, out.CanReview)
	if e != nil {
		return out, pp.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var c string
		if e = rows.Scan(&c); e != nil {
			rows.Close()
			return out, pp.ErrUnavailable
		}
		ids = append(ids, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, pp.ErrUnavailable
	}
	out.ObservedAt = time.Now().UTC()
	out.ExpiresAt = out.ObservedAt.Add(30 * time.Second)
	for _, cid := range ids {
		c, e := photoCandidate(ctx, f.tx, cid, id, false)
		if e != nil {
			return out, e
		}
		c.CanWithdraw = c.Status != "WITHDRAWN" && c.Status != "EXPIRED" && (c.SubmittedBy == a.AccountID || out.CanReview)
		c.CanApprove = c.Status == "PENDING" && c.Rights.Valid(out.ObservedAt) && publicCurrent && out.CanReview && c.SubmittedBy != a.AccountID && photoEditor(ctx, f.tx, id, c.SubmittedBy, false)
		c.CanReject = c.Status == "PENDING" && c.Rights.Valid(out.ObservedAt) && out.CanReview && c.SubmittedBy != a.AccountID
		if len(c.Bytes) > 0 && c.Rights.Valid(out.ObservedAt) {
			c.Thumbnail = &pp.Thumbnail{ContentType: "image/jpeg", Width: 320, Height: 180, ByteCount: len(c.Bytes), SHA256: c.SHA256}
			if c.Rights.RetentionUntil.Before(out.ExpiresAt) {
				out.ExpiresAt = c.Rights.RetentionUntil
			}
		}
		c.Bytes = nil
		out.Candidates = append(out.Candidates, c)
	}
	out.SourceProof, e = photoEditorialProof(ctx, f.tx, id)
	if e != nil {
		return out, e
	}
	out.SourceProof = photoSessionProof(f, out.SourceProof)
	f.until = out.ExpiresAt
	if e = s.finishPlacePhoto(ctx, f); e != nil {
		return out, e
	}
	return out, nil
}
func (s *Store) ValidatePlacePhotoEditorial(ctx context.Context, a pp.Access, id string, out pp.Editorial) error {
	current, e := s.ReadPlacePhotoEditorial(ctx, a, id)
	if e != nil {
		return e
	}
	if !out.ExpiresAt.After(time.Now().UTC()) || current.SourceProof != out.SourceProof || current.ExpiresAt.Before(out.ExpiresAt) {
		return pp.ErrChanged
	}
	current.ObservedAt, current.ExpiresAt = out.ObservedAt, out.ExpiresAt
	oldWire, _ := json.Marshal(out)
	newWire, _ := json.Marshal(current)
	if string(oldWire) != string(newWire) {
		return pp.ErrChanged
	}
	return nil
}
