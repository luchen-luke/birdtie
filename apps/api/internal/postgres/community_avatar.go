package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	ca "github.com/birdtie/birdtie/apps/api/internal/communityavatar"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"github.com/jackc/pgx/v5"
)

var _ ca.Store = (*Store)(nil)

func communityAvatarFailure(e error) error {
	if e == nil {
		return nil
	}
	for _, known := range []error{identity.ErrUnauthorized, ca.ErrInvalid, ca.ErrChanged, ca.ErrForbidden, ca.ErrNotFound} {
		if errors.Is(e, known) {
			return known
		}
	}
	return ca.ErrUnavailable
}

type communityAvatarFence struct {
	tx            pgx.Tx
	access        ca.Access
	id, authority string
	state         ca.State
	approvalUntil int64
}

const communityAvatarVisibleSQL = `SELECT EXISTS(SELECT 1 FROM communities c JOIN accounts creator ON creator.id=c.owner_account_id WHERE c.id=$1 AND creator.account_type='person' AND creator.status='active' AND c.lifecycle_status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND(c.visibility<>'hidden' OR EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2 AND m.status IN('active','invited'))) AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$2 AND b.blocked_account_id=c.owner_account_id) OR(b.blocker_account_id=c.owner_account_id AND b.blocked_account_id=$2)))`

func communityAvatarState(ctx context.Context, tx pgx.Tx, id, viewer string) (ca.State, error) {
	var out ca.State
	e := tx.QueryRow(ctx, `SELECT version,birdtie_community_avatar_metadata(community_id,$2::uuid) FROM community_avatars WHERE community_id=$1`, id, viewer).Scan(&out.Version, &out.Avatar)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil
	}
	if e != nil || out.Version < 1 || out.Version > ca.MaxVersion || (out.Avatar != nil && (!out.Avatar.Valid() || out.Avatar.CommunityID != id || out.Avatar.Version != out.Version)) {
		return ca.State{}, ca.ErrUnavailable
	}
	return out, nil
}
func (s *Store) beginCommunityAvatar(ctx context.Context, a ca.Access, id string, write, manage bool) (communityAvatarFence, error) {
	f := communityAvatarFence{access: a, id: id}
	if !av.ValidID(id) {
		return f, ca.ErrInvalid
	}
	tx, e := s.beginHumanMomentTx(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	if e != nil {
		return f, communityAvatarFailure(e)
	}
	f.tx = tx
	fail := func(e error) (communityAvatarFence, error) {
		tx.Rollback(context.Background())
		return communityAvatarFence{}, communityAvatarFailure(e)
	}
	suffix := " FOR SHARE"
	if write {
		suffix = " FOR UPDATE"
	}
	var epoch string
	e = tx.QueryRow(ctx, `SELECT xmin::text FROM communities WHERE id=$1 AND lifecycle_status='active' AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp())`+suffix, id).Scan(&epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(ca.ErrNotFound)
	}
	if e != nil {
		return fail(e)
	}
	var creator string
	e = tx.QueryRow(ctx, `SELECT creator.id FROM accounts creator JOIN communities c ON c.owner_account_id=creator.id WHERE c.id=$1 AND creator.account_type='person' AND creator.status='active' FOR SHARE OF creator`, id).Scan(&creator)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(ca.ErrNotFound)
	}
	if e != nil {
		return fail(e)
	}
	// Community -> membership matches the original management lock order. Hold
	// existing viewer membership through the final thumbnail/Session check.
	rows, e := tx.Query(ctx, `SELECT id FROM community_memberships WHERE community_id=$1 AND user_account_id=$2 FOR SHARE`, id, a.AccountID)
	if e != nil {
		return fail(e)
	}
	for rows.Next() {
		var member string
		if e = rows.Scan(&member); e != nil {
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return fail(e)
	}
	// All resource/membership waits above can outlive the early Session check.
	// Recheck without locking so a committed revoke reports unauthorized before
	// its changed authorization_generation is mistaken for an avatar CAS error.
	// The locked final gate still runs after writes/audit waits, not here.
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID, s.devPhoneEnabled); e != nil {
		return fail(e)
	}
	var visible bool
	if e = tx.QueryRow(ctx, communityAvatarVisibleSQL, id, a.AccountID).Scan(&visible); e != nil {
		return fail(e)
	}
	if !visible {
		return fail(ca.ErrNotFound)
	}
	if manage {
		role, status, e := socialRole(ctx, tx, a.AccountID, id)
		if e != nil {
			return fail(e)
		}
		if status != "active" || (role != "owner" && role != "admin") {
			return fail(ca.ErrForbidden)
		}
	}
	f.state, e = communityAvatarState(ctx, tx, id, a.AccountID)
	if e != nil {
		return fail(e)
	}
	f.authority, e = communityAvatarAuthority(ctx, tx, a, id)
	if e != nil {
		return fail(e)
	}
	return f, nil
}
func communityAvatarAuthority(ctx context.Context, tx pgx.Tx, a ca.Access, id string) (string, error) {
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT jsonb_build_array(c.id,c.xmin::text,creator.xmin::text,owner.xmin::text,se.id,se.created_at,se.expires_at,se.authentication_method,se.authorization_generation,(SELECT jsonb_build_array(m.id,m.role,m.status,m.xmin::text) FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=$2)) FROM communities c JOIN accounts creator ON creator.id=c.owner_account_id JOIN accounts owner ON owner.id=$2 JOIN sessions se ON se.account_id=owner.id WHERE c.id=$1 AND se.token_sha256=$3`, id, a.AccountID, a.SessionDigest[:]).Scan(&raw)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *Store) finishCommunityAvatar(ctx context.Context, f communityAvatarFence) error {
	if e := s.lockHumanMomentSession(ctx, f.tx, f.access.SessionDigest, f.access.AccountID); e != nil {
		return communityAvatarFailure(e)
	}
	var visible bool
	if e := f.tx.QueryRow(ctx, communityAvatarVisibleSQL, f.id, f.access.AccountID).Scan(&visible); e != nil {
		return ca.ErrUnavailable
	}
	if !visible {
		return ca.ErrNotFound
	}
	authority, e := communityAvatarAuthority(ctx, f.tx, f.access, f.id)
	if e != nil {
		return ca.ErrUnavailable
	}
	if authority != f.authority {
		return ca.ErrChanged
	}
	if f.approvalUntil != 0 {
		var now time.Time
		if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return ca.ErrUnavailable
		}
		if now.UnixNano() >= f.approvalUntil {
			return ca.ErrChanged
		}
	}
	return communityAvatarFailure(s.finishHumanMomentTx(ctx, f.tx, f.access.SessionDigest, f.access.AccountID))
}
func communityAvatarMaterial(f communityAvatarFence, in ca.Input, action, sum string) string {
	raw, _ := json.Marshal([]any{"COMMUNITY_AVATAR_APPROVED_V1", f.id, f.access.AccountID, f.authority, f.state.Version, ca.Digest(f.id, in, action, sum)})
	return string(raw)
}
func avatarApprovalToken(d [32]byte, material string, issued, expiry int64) string {
	return strings.Replace(communityApprovalToken(d, material, issued, expiry), "cm1.", "ca1.", 1)
}
func validAvatarApproval(token string, d [32]byte, material string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "ca1" {
		return false
	}
	issued, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		return false
	}
	expiry, e := strconv.ParseInt(parts[2], 10, 64)
	if e != nil || issued <= 0 || issued > now.UnixNano() || expiry <= now.UnixNano() || expiry <= issued || expiry-issued != communityApprovalTTL.Nanoseconds() {
		return false
	}
	expected := avatarApprovalToken(d, material, issued, expiry)
	return hmac.Equal([]byte(expected), []byte(token))
}
func (s *Store) ReadCommunityAvatar(ctx context.Context, a ca.Access, id string) (ca.State, error) {
	f, e := s.beginCommunityAvatar(ctx, a, id, false, false)
	if e != nil {
		return ca.State{}, e
	}
	defer f.tx.Rollback(context.Background())
	if e = s.finishCommunityAvatar(ctx, f); e != nil {
		return ca.State{}, e
	}
	return f.state, nil
}
func (s *Store) PreviewCommunityAvatar(ctx context.Context, a ca.Access, id string, in ca.Input) (ca.Approval, error) {
	in, _, action, sum, e := ca.Normalize(in)
	if e != nil {
		return ca.Approval{}, e
	}
	f, e := s.beginCommunityAvatar(ctx, a, id, false, true)
	if e != nil {
		return ca.Approval{}, e
	}
	defer f.tx.Rollback(context.Background())
	if f.state.Version != in.ExpectedVersion {
		return ca.Approval{}, ca.ErrChanged
	}
	var now time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return ca.Approval{}, ca.ErrUnavailable
	}
	end := now.Add(communityApprovalTTL)
	v := ca.Approval{CommunityID: id, OperationID: in.OperationID, ExpectedVersion: in.ExpectedVersion, Action: action, InputSHA256: sum, ExpiresAt: end, Token: avatarApprovalToken(a.SessionDigest, communityAvatarMaterial(f, in, action, sum), now.UnixNano(), end.UnixNano())}
	if e = s.finishCommunityAvatar(ctx, f); e != nil {
		return ca.Approval{}, e
	}
	return v, nil
}
func (s *Store) ChangeCommunityAvatar(ctx context.Context, a ca.Access, id string, in ca.Input) (ca.Result, error) {
	in, thumb, action, sum, e := ca.Normalize(in)
	if e != nil {
		return ca.Result{}, e
	}
	f, e := s.beginCommunityAvatar(ctx, a, id, true, true)
	if e != nil {
		return ca.Result{}, e
	}
	defer f.tx.Rollback(context.Background())
	v := ca.Result{CommunityID: id, OperationID: in.OperationID, Action: action}
	var oldAction, oldSum string
	var expected int64
	e = f.tx.QueryRow(ctx, `SELECT action,input_sha256,expected_version,applied_version FROM community_avatar_operations WHERE community_id=$1 AND actor_account_id=$2 AND operation_id=$3`, id, a.AccountID, in.OperationID).Scan(&oldAction, &oldSum, &expected, &v.AppliedVersion)
	if e == nil {
		if oldAction != action || oldSum != sum || expected != in.ExpectedVersion {
			return ca.Result{}, ca.ErrChanged
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return ca.Result{}, ca.ErrUnavailable
	} else {
		if f.state.Version != in.ExpectedVersion {
			return ca.Result{}, ca.ErrChanged
		}
		var now time.Time
		if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return ca.Result{}, ca.ErrUnavailable
		}
		if !validAvatarApproval(in.Approval, a.SessionDigest, communityAvatarMaterial(f, in, action, sum), now) {
			return ca.Result{}, ca.ErrChanged
		}
		f.approvalUntil, _ = strconv.ParseInt(strings.Split(in.Approval, ".")[2], 10, 64)
		v.AppliedVersion = f.state.Version + 1
		var data, hash any
		if !in.Delete {
			data, hash = thumb.Bytes, sum
		}
		_, e = f.tx.Exec(ctx, `INSERT INTO community_avatars(community_id,version,thumbnail,sha256) VALUES($1,$2,$3,$4) ON CONFLICT(community_id) DO UPDATE SET version=EXCLUDED.version,thumbnail=EXCLUDED.thumbnail,sha256=EXCLUDED.sha256,updated_at=clock_timestamp()`, id, v.AppliedVersion, data, hash)
		if e != nil {
			return ca.Result{}, ca.ErrUnavailable
		}
		_, e = f.tx.Exec(ctx, `INSERT INTO community_avatar_operations(community_id,actor_account_id,operation_id,action,expected_version,applied_version,input_sha256) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, a.AccountID, in.OperationID, action, in.ExpectedVersion, v.AppliedVersion, sum)
		if e != nil {
			return ca.Result{}, ca.ErrUnavailable
		}
		if e = socialAudit(ctx, f.tx, a.AccountID, "community_avatar_"+strings.ToLower(action), id); e != nil {
			return ca.Result{}, ca.ErrUnavailable
		}
	}
	v.Current, e = communityAvatarState(ctx, f.tx, id, a.AccountID)
	if e != nil {
		return ca.Result{}, e
	}
	if e = s.finishCommunityAvatar(ctx, f); e != nil {
		return ca.Result{}, e
	}
	return v, nil
}
func (s *Store) ReadCommunityAvatarOperation(ctx context.Context, a ca.Access, id, op string) (ca.Result, error) {
	if !av.ValidID(op) {
		return ca.Result{}, ca.ErrInvalid
	}
	f, e := s.beginCommunityAvatar(ctx, a, id, false, true)
	if e != nil {
		return ca.Result{}, e
	}
	defer f.tx.Rollback(context.Background())
	v := ca.Result{CommunityID: id, OperationID: op, Current: f.state}
	e = f.tx.QueryRow(ctx, `SELECT action,applied_version FROM community_avatar_operations WHERE community_id=$1 AND actor_account_id=$2 AND operation_id=$3`, id, a.AccountID, op).Scan(&v.Action, &v.AppliedVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		return ca.Result{}, ca.ErrNotFound
	}
	if e != nil {
		return ca.Result{}, ca.ErrUnavailable
	}
	if e = s.finishCommunityAvatar(ctx, f); e != nil {
		return ca.Result{}, e
	}
	return v, nil
}
func (s *Store) ReadCommunityAvatarContent(ctx context.Context, a ca.Access, id string, version int64) (ca.Metadata, []byte, error) {
	if version < 1 || version > ca.MaxVersion {
		return ca.Metadata{}, nil, ca.ErrInvalid
	}
	f, e := s.beginCommunityAvatar(ctx, a, id, false, false)
	if e != nil {
		return ca.Metadata{}, nil, e
	}
	defer f.tx.Rollback(context.Background())
	if f.state.Avatar == nil || f.state.Version != version {
		return ca.Metadata{}, nil, ca.ErrNotFound
	}
	var raw []byte
	e = f.tx.QueryRow(ctx, `SELECT thumbnail FROM community_avatars WHERE community_id=$1 AND version=$2 AND thumbnail IS NOT NULL`, id, version).Scan(&raw)
	if e != nil {
		return ca.Metadata{}, nil, ca.ErrNotFound
	}
	sum := sha256.Sum256(raw)
	if len(raw) != f.state.Avatar.ByteCount || hex.EncodeToString(sum[:]) != f.state.Avatar.SHA256 {
		return ca.Metadata{}, nil, ca.ErrUnavailable
	}
	if e = s.finishCommunityAvatar(ctx, f); e != nil {
		return ca.Metadata{}, nil, e
	}
	return *f.state.Avatar, raw, nil
}
