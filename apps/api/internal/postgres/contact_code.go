package postgres

import (
	"context"
	"errors"
	cc "github.com/birdtie/birdtie/apps/api/internal/contactcode"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"time"
)

var _ cc.Store = (*Store)(nil)

func contactCodeAccess(a identity.ProfileAccess) bool {
	return a.Actor.AccountType == "person" && cc.ValidID(a.Actor.ID) && a.SessionDigest != ([32]byte{})
}
func contactCodeError(e error) error {
	if errors.Is(e, identity.ErrUnauthorized) || errors.Is(e, identity.ErrNotFound) || errors.Is(e, cc.ErrConflict) || errors.Is(e, cc.ErrInvalid) || errors.Is(e, cc.ErrNotFound) {
		return e
	}
	return cc.ErrUnavailable
}
func readCodeRow(ctx context.Context, tx pgx.Tx, owner string, lock bool) (cc.View, error) {
	var v cc.View
	sql := `SELECT revision,code_id::text,expires_at FROM person_contact_codes WHERE owner_account_id=$1`
	if lock {
		sql += ` FOR UPDATE`
	}
	e := tx.QueryRow(ctx, sql, owner).Scan(&v.Version, &v.CodeID, &v.ExpiresAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return cc.View{}, nil
	}
	return v, e
}
func (s *Store) ReadOwnContactCode(ctx context.Context, a identity.ProfileAccess) (cc.View, error) {
	if !contactCodeAccess(a) {
		return cc.View{}, identity.ErrUnauthorized
	}
	tx, e := s.beginHumanMomentTx(ctx, a.SessionDigest, a.Actor)
	if e != nil {
		return cc.View{}, contactCodeError(e)
	}
	defer tx.Rollback(context.Background())
	v, e := readCodeRow(ctx, tx, a.Actor.ID, true)
	if e != nil {
		return cc.View{}, cc.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e == nil {
		e = s.finishHumanMomentTx(ctx, tx, a.SessionDigest, a.Actor.ID)
	}
	if e != nil {
		return cc.View{}, contactCodeError(e)
	}
	return v, nil
}
func (s *Store) ChangeOwnContactCode(ctx context.Context, a identity.ProfileAccess, in cc.Input, revoke bool) (cc.View, error) {
	if !contactCodeAccess(a) {
		return cc.View{}, identity.ErrUnauthorized
	}
	if e := cc.ValidateInput(in); e != nil {
		return cc.View{}, e
	}
	tx, e := s.beginHumanMomentTx(ctx, a.SessionDigest, a.Actor)
	if e != nil {
		return cc.View{}, contactCodeError(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `INSERT INTO person_contact_codes(owner_account_id) VALUES($1) ON CONFLICT DO NOTHING`, a.Actor.ID); e != nil {
		return cc.View{}, cc.ErrUnavailable
	}
	v, e := readCodeRow(ctx, tx, a.Actor.ID, true)
	if e != nil {
		return cc.View{}, cc.ErrUnavailable
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return cc.View{}, contactCodeError(e)
	}
	var oldExpected, oldResult int64
	var oldRevoke bool
	e = tx.QueryRow(ctx, `SELECT expected_revision,resulting_revision,revoke FROM person_contact_code_operations WHERE owner_account_id=$1 AND operation_id=$2`, a.Actor.ID, in.OperationID).Scan(&oldExpected, &oldResult, &oldRevoke)
	if e == nil {
		if oldExpected != in.ExpectedVersion || oldRevoke != revoke || oldResult != v.Version {
			return cc.View{}, cc.ErrConflict
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return cc.View{}, cc.ErrUnavailable
	} else {
		if v.Version != in.ExpectedVersion {
			return cc.View{}, cc.ErrConflict
		}
		query := `UPDATE person_contact_codes SET revision=revision+1,code_id=gen_random_uuid(),expires_at=clock_timestamp()+interval '24 hours',updated_at=clock_timestamp() WHERE owner_account_id=$1 RETURNING revision,code_id::text,expires_at`
		if revoke {
			query = `UPDATE person_contact_codes SET revision=revision+1,code_id=NULL,expires_at=NULL,updated_at=clock_timestamp() WHERE owner_account_id=$1 RETURNING revision,code_id::text,expires_at`
		}
		if e = tx.QueryRow(ctx, query, a.Actor.ID).Scan(&v.Version, &v.CodeID, &v.ExpiresAt); e != nil {
			return cc.View{}, cc.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `INSERT INTO person_contact_code_operations(owner_account_id,operation_id,expected_revision,revoke,resulting_revision) VALUES($1,$2,$3,$4,$5)`, a.Actor.ID, in.OperationID, in.ExpectedVersion, revoke, v.Version); e != nil {
			return cc.View{}, cc.ErrUnavailable
		}
	}
	if e = s.finishHumanMomentTx(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return cc.View{}, contactCodeError(e)
	}
	return v, nil
}
func sameCode(a, b cc.View) bool {
	if a.Version != b.Version || (a.CodeID == nil) != (b.CodeID == nil) || (a.ExpiresAt == nil) != (b.ExpiresAt == nil) {
		return false
	}
	return a.CodeID == nil || (*a.CodeID == *b.CodeID && a.ExpiresAt.Equal(*b.ExpiresAt))
}
func (s *Store) ValidateOwnContactCode(ctx context.Context, a identity.ProfileAccess, v cc.View) error {
	current, e := s.ReadOwnContactCode(ctx, a)
	if e != nil {
		return e
	}
	if !sameCode(current, v) {
		return cc.ErrConflict
	}
	return nil
}

type contactResolutionProof struct {
	store       *Store
	access      identity.ProfileAccess
	code, owner string
	version     int64
	expires     time.Time
	profile     identity.Profile
}

func (s *Store) resolveCodeRow(ctx context.Context, code string) (string, int64, time.Time, error) {
	var owner string
	var version int64
	var expires time.Time
	if !cc.ValidID(code) {
		return owner, version, expires, cc.ErrInvalid
	}
	if s == nil || s.pool == nil || ctx == nil || ctx.Err() != nil {
		return owner, version, expires, cc.ErrUnavailable
	}
	e := s.pool.QueryRow(ctx, `SELECT c.owner_account_id::text,c.revision,c.expires_at FROM person_contact_codes c JOIN accounts a ON a.id=c.owner_account_id AND a.status='active' AND a.account_type='person' WHERE c.code_id=$1 AND c.expires_at>clock_timestamp()`, code).Scan(&owner, &version, &expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return owner, version, expires, cc.ErrNotFound
	}
	return owner, version, expires, contactCodeNullableError(e)
}
func contactCodeNullableError(e error) error {
	if e == nil {
		return nil
	}
	return contactCodeError(e)
}
func (s *Store) ResolveContactCode(ctx context.Context, a identity.ProfileAccess, code string) (cc.Resolution, error) {
	if !contactCodeAccess(a) {
		return cc.Resolution{}, identity.ErrUnauthorized
	}
	owner, version, expires, e := s.resolveCodeRow(ctx, code)
	if e != nil {
		return cc.Resolution{}, e
	}
	p, e := s.ReadProfileCurrent(ctx, a, owner)
	if e != nil {
		return cc.Resolution{}, contactCodeError(e)
	}
	return cc.Resolution{AccountID: owner, Proof: &contactResolutionProof{s, a, code, owner, version, expires, p}}, nil
}
func (s *Store) ValidateContactResolution(ctx context.Context, a identity.ProfileAccess, code string, v cc.Resolution) error {
	p, ok := v.Proof.(*contactResolutionProof)
	if !ok || p == nil || p.store != s || p.access.SessionDigest != a.SessionDigest || p.access.Actor.ID != a.Actor.ID || p.access.Actor.AccountType != a.Actor.AccountType || p.code != code || p.owner != v.AccountID {
		return cc.ErrUnavailable
	}
	if e := s.ValidateHumanSocialResponse(ctx, a.SessionDigest, a.Actor); e != nil {
		return contactCodeError(e)
	}
	if e := s.ValidateProfileCurrent(ctx, a, p.owner, p.profile); e != nil {
		return contactCodeError(e)
	}
	owner, version, expires, e := s.resolveCodeRow(ctx, code)
	if e != nil {
		return e
	}
	if owner != p.owner || version != p.version || !expires.Equal(p.expires) {
		return cc.ErrConflict
	}
	// Session/resource waits above cannot make a prior code or ACL check final.
	// This last nonlocking snapshot checks the code, ACL and current session at
	// one fresh clock; no subsequent wait precedes releasing the selector.
	var current bool
	e = s.pool.QueryRow(ctx, `WITH wall AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT EXISTS(SELECT 1 FROM person_contact_codes c
 JOIN accounts target ON target.id=c.owner_account_id AND target.status='active' AND target.account_type='person'
 JOIN sessions ss ON ss.token_sha256=$5
 JOIN accounts viewer ON viewer.id=ss.account_id AND viewer.id=$6 AND viewer.account_type='person' AND viewer.status='active',wall
 WHERE c.code_id=$1 AND c.owner_account_id=$2 AND c.revision=$3 AND c.expires_at=$4 AND c.expires_at>wall.at
 AND ss.revoked_at IS NULL AND ss.expires_at>wall.at AND ss.idle_expires_at>wall.at
 AND ($7::boolean OR ss.authentication_method<>'dev_phone')
 AND birdtie_profile_visible_to(c.owner_account_id,viewer.id))`, code, p.owner, p.version, p.expires, a.SessionDigest[:], a.Actor.ID, s.devPhoneEnabled).Scan(&current)
	if e != nil || ctx.Err() != nil {
		return cc.ErrUnavailable
	}
	if !current {
		return cc.ErrNotFound
	}
	return nil
}
