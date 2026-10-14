package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/jackc/pgx/v5"
	"math"
)

var _ av.Store = (*Store)(nil)

func avatarFailure(err error) error {
	if errors.Is(err, identity.ErrConflict) {
		return av.ErrConflict
	}
	if err == nil || errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, av.ErrInvalid) || errors.Is(err, av.ErrConflict) || errors.Is(err, av.ErrNotFound) {
		return err
	}
	return av.ErrUnavailable
}
func (s *Store) beginAvatar(ctx context.Context, a av.Access, write bool) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, av.ErrUnavailable
	}
	if !av.ValidID(a.AccountID) || a.AccountType != "person" || a.SessionDigest == ([32]byte{}) {
		return nil, identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, av.ErrUnavailable
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR NO KEY UPDATE"
	}
	var id string
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`+lock, a.AccountID).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		e = identity.ErrUnauthorized
	}
	if e == nil {
		e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID, s.devPhoneEnabled)
	}
	if e == nil {
		var owner string
		e = tx.QueryRow(ctx, `SELECT account_id FROM user_profiles WHERE account_id=$1`, a.AccountID).Scan(&owner)
		if errors.Is(e, pgx.ErrNoRows) {
			e = av.ErrNotFound
		}
	}
	if e != nil {
		_ = tx.Rollback(context.Background())
		return nil, avatarFailure(e)
	}
	return tx, nil
}
func avatarState(ctx context.Context, tx pgx.Tx, id string) (av.State, error) {
	var state av.State
	e := tx.QueryRow(ctx, `SELECT version,birdtie_person_avatar_metadata(account_id,$1::uuid) FROM person_avatars WHERE account_id=$1`, id).Scan(&state.Version, &state.Avatar)
	if errors.Is(e, pgx.ErrNoRows) {
		return state, nil
	}
	if e == nil && (state.Version < 1 || state.Avatar != nil && (!state.Avatar.Valid() || state.Avatar.Version != state.Version || state.Avatar.AccountID != id)) {
		e = av.ErrUnavailable
	}
	return state, avatarFailure(e)
}
func (s *Store) finishAvatar(ctx context.Context, tx pgx.Tx, a av.Access) error {
	if e := s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID); e != nil {
		return avatarFailure(e)
	}
	if e := checkHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID, s.devPhoneEnabled); e != nil {
		return avatarFailure(e)
	}
	if e := tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return av.ErrUnavailable
	}
	return nil
}
func (s *Store) ReadOwnAvatar(ctx context.Context, a av.Access) (av.State, error) {
	tx, e := s.beginAvatar(ctx, a, false)
	if e != nil {
		return av.State{}, e
	}
	defer tx.Rollback(context.Background())
	state, e := avatarState(ctx, tx, a.AccountID)
	if e == nil {
		e = s.finishAvatar(ctx, tx, a)
	}
	return state, e
}
func (s *Store) ChangeOwnAvatar(ctx context.Context, a av.Access, in av.Input) (av.Result, error) {
	if !av.ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion == math.MaxInt64 {
		return av.Result{}, av.ErrInvalid
	}
	action, inputSum := "DELETE", ""
	var thumb av.Thumbnail
	if in.Delete {
		if len(in.Content) != 0 || in.ContentType != "" {
			return av.Result{}, av.ErrInvalid
		}
	} else {
		var e error
		thumb, e = av.Prepare(in.Content, in.ContentType)
		if e != nil {
			return av.Result{}, e
		}
		action, inputSum = "SAVE", thumb.InputSHA256
	}
	tx, e := s.beginAvatar(ctx, a, true)
	if e != nil {
		return av.Result{}, e
	}
	defer tx.Rollback(context.Background())
	var originalAction, originalSum string
	var expected, applied int64
	e = tx.QueryRow(ctx, `SELECT action,expected_version,applied_version,input_sha256 FROM person_avatar_operations WHERE account_id=$1 AND operation_id=$2`, a.AccountID, in.OperationID).Scan(&originalAction, &expected, &applied, &originalSum)
	if e == nil {
		if originalAction != action || expected != in.ExpectedVersion || originalSum != inputSum {
			return av.Result{}, av.ErrConflict
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return av.Result{}, av.ErrUnavailable
	} else {
		state, e := avatarState(ctx, tx, a.AccountID)
		if e != nil {
			return av.Result{}, e
		}
		if state.Version != in.ExpectedVersion {
			return av.Result{}, av.ErrConflict
		}
		applied = state.Version + 1
		var content, hash any
		if !in.Delete {
			content, hash = thumb.Bytes, thumb.SHA256
		}
		_, e = tx.Exec(ctx, `INSERT INTO person_avatars(account_id,version,thumbnail,sha256) VALUES($1,$2,$3,$4)
   ON CONFLICT(account_id) DO UPDATE SET version=EXCLUDED.version,thumbnail=EXCLUDED.thumbnail,sha256=EXCLUDED.sha256,updated_at=clock_timestamp()`, a.AccountID, applied, content, hash)
		if e != nil {
			return av.Result{}, av.ErrUnavailable
		}
		_, e = tx.Exec(ctx, `INSERT INTO person_avatar_operations(account_id,operation_id,action,expected_version,applied_version,input_sha256) VALUES($1,$2,$3,$4,$5,$6)`, a.AccountID, in.OperationID, action, in.ExpectedVersion, applied, inputSum)
		if e != nil {
			return av.Result{}, av.ErrUnavailable
		}
		_, e = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'profile',$1::uuid::text,'allowed','owner_avatar_edit')`, a.AccountID, action)
		if e != nil {
			return av.Result{}, av.ErrUnavailable
		}
	}
	state, e := avatarState(ctx, tx, a.AccountID)
	if e != nil {
		return av.Result{}, e
	}
	result := av.Result{OperationID: in.OperationID, Action: action, AppliedVersion: applied, Current: state}
	return result, s.finishAvatar(ctx, tx, a)
}
func (s *Store) ReadOwnAvatarOperation(ctx context.Context, a av.Access, id string) (av.Result, error) {
	if !av.ValidID(id) {
		return av.Result{}, av.ErrInvalid
	}
	tx, e := s.beginAvatar(ctx, a, false)
	if e != nil {
		return av.Result{}, e
	}
	defer tx.Rollback(context.Background())
	result := av.Result{OperationID: id}
	e = tx.QueryRow(ctx, `SELECT action,applied_version FROM person_avatar_operations WHERE account_id=$1 AND operation_id=$2`, a.AccountID, id).Scan(&result.Action, &result.AppliedVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		return av.Result{}, av.ErrNotFound
	}
	if e != nil {
		return av.Result{}, av.ErrUnavailable
	}
	result.Current, e = avatarState(ctx, tx, a.AccountID)
	if e == nil {
		e = s.finishAvatar(ctx, tx, a)
	}
	return result, e
}
func (s *Store) ReadAvatarContent(ctx context.Context, a av.Access, target string, version int64) (av.Metadata, []byte, error) {
	if !av.ValidID(target) || version < 1 {
		return av.Metadata{}, nil, av.ErrInvalid
	}
	access := identity.ProfileAccess{Actor: identity.Actor{ID: a.AccountID, AccountType: a.AccountType}, SessionDigest: a.SessionDigest}
	profile, e := s.ReadProfileCurrent(ctx, access, target)
	if errors.Is(e, identity.ErrNotFound) {
		return av.Metadata{}, nil, av.ErrNotFound
	}
	if e != nil {
		return av.Metadata{}, nil, avatarFailure(e)
	}
	if profile.Avatar == nil || profile.Avatar.Version != version || !profile.Avatar.Valid() {
		return av.Metadata{}, nil, av.ErrNotFound
	}
	var raw []byte
	e = s.pool.QueryRow(ctx, `SELECT thumbnail FROM person_avatars WHERE account_id=$1 AND version=$2 AND thumbnail IS NOT NULL`, target, version).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return av.Metadata{}, nil, av.ErrNotFound
	}
	if e != nil {
		return av.Metadata{}, nil, av.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	if len(raw) != profile.Avatar.ByteCount || hex.EncodeToString(sum[:]) != profile.Avatar.SHA256 {
		return av.Metadata{}, nil, av.ErrUnavailable
	}
	if e = s.ValidateProfileCurrent(ctx, access, target, profile); e != nil {
		return av.Metadata{}, nil, avatarFailure(e)
	}
	return *profile.Avatar, raw, nil
}
