package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Authenticate uses the same original current Session/account resolver as
// HumanSocial. Its locks precede a fresh native clock: a queued original actor()
// request must not shorten a newer refresh or revive a Session expired while
// waiting. This resolver adds no social, Agent, organization, or model authority.
func (s *Store) Authenticate(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return s.AuthenticateOrganizationAgent(ctx, digest)
}

func (s *Store) RevokeSession(ctx context.Context, digest [32]byte) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET revoked_at = now()
        WHERE token_sha256 = $1 AND revoked_at IS NULL`, digest[:])
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return identity.ErrUnauthorized
	}
	return nil
}

func (s *Store) ReadProfile(ctx context.Context, actorID, targetID string) (identity.Profile, error) {
	var actor any
	if actorID != "" {
		actor = actorID
	}
	var profile identity.Profile
	err := s.pool.QueryRow(ctx, `SELECT p.account_id,
        CASE WHEN birdtie_agent_profile_field_allowed(p.account_id,$2::uuid,'displayName') THEN p.display_name ELSE '' END,
        CASE WHEN birdtie_agent_profile_field_allowed(p.account_id,$2::uuid,'bio') THEN p.bio ELSE '' END, p.visibility,
        birdtie_person_avatar_metadata(p.account_id,$2::uuid)
        FROM user_profiles p
        JOIN accounts a ON a.id = p.account_id
        WHERE p.account_id = $1 AND a.status = 'active'
          AND NOT EXISTS (
              SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
                AND ((b.blocker_account_id = p.account_id AND b.blocked_account_id = $2)
                  OR (b.blocker_account_id = $2 AND b.blocked_account_id = p.account_id))
          )
          AND birdtie_profile_visible_to(p.account_id,$2::uuid)`, targetID, actor).
		Scan(&profile.AccountID, &profile.DisplayName, &profile.Bio, &profile.Visibility, &profile.Avatar)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrNotFound
	}
	return profile, err
}

func (s *Store) ListProfileGrants(ctx context.Context, ownerID string) ([]identity.Grant, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, recipient_account_id, resource_type,
        resource_id, purpose, actions, revision, created_at, expires_at, revoked_at
        FROM consent_grants
        WHERE owner_account_id = $1 AND resource_type = 'profile'
          AND resource_id = $1::uuid::text AND purpose = 'profile_view'
          AND recipient_account_id IS NOT NULL
        ORDER BY created_at DESC, id DESC LIMIT 100`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := make([]identity.Grant, 0)
	for rows.Next() {
		var grant identity.Grant
		if err := rows.Scan(&grant.ID, &grant.RecipientAccountID,
			&grant.ResourceType, &grant.ResourceID, &grant.Purpose,
			&grant.Actions, &grant.Revision, &grant.CreatedAt,
			&grant.ExpiresAt, &grant.RevokedAt); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func (s *Store) GrantProfileRead(ctx context.Context, ownerID, recipientID string, expiresAt time.Time) (identity.Grant, error) {
	if ownerID == recipientID || expiresAt.Before(time.Now().Add(time.Minute)) ||
		expiresAt.After(time.Now().Add(90*24*time.Hour)) {
		return identity.Grant{}, identity.ErrInvalidGrant
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.Grant{}, err
	}
	defer tx.Rollback(ctx)
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM accounts owner
        JOIN user_profiles p ON p.account_id = owner.id
        JOIN accounts recipient ON recipient.id = $2 AND recipient.status = 'active'
        WHERE owner.id = $1 AND owner.status = 'active'
          AND NOT EXISTS (
              SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id = owner.id AND b.blocked_account_id = recipient.id)
                 OR (b.blocker_account_id = recipient.id AND b.blocked_account_id = owner.id)
          )
    )`, ownerID, recipientID).Scan(&eligible)
	if err != nil {
		return identity.Grant{}, err
	}
	if !eligible {
		return identity.Grant{}, identity.ErrInvalidGrant
	}
	var grant identity.Grant
	err = tx.QueryRow(ctx, `INSERT INTO consent_grants (
        id, owner_account_id, recipient_account_id, resource_type,
        resource_id, purpose, actions, expires_at
    ) VALUES (
        gen_random_uuid(), $1, $2, 'profile', $1::uuid::text,
        'profile_view', ARRAY['read']::text[], $3
    )
    RETURNING id, recipient_account_id, resource_type, resource_id,
        purpose, actions, revision, created_at, expires_at, revoked_at`,
		ownerID, recipientID, expiresAt).
		Scan(&grant.ID, &grant.RecipientAccountID, &grant.ResourceType,
			&grant.ResourceID, &grant.Purpose, &grant.Actions,
			&grant.Revision, &grant.CreatedAt, &grant.ExpiresAt, &grant.RevokedAt)
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			return identity.Grant{}, identity.ErrConflict
		}
		return identity.Grant{}, err
	}
	err = insertDomainAudit(ctx, tx, ownerID, "grant", "profile", ownerID, "profile_view", &grant.ID)
	if err != nil {
		return identity.Grant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Grant{}, err
	}
	return grant, nil
}

func revokeProfileGrantTx(ctx context.Context, tx pgx.Tx, ownerID, grantID string) error {
	var resourceID string
	var err error
	err = tx.QueryRow(ctx, `UPDATE consent_grants
        SET revoked_at = now(), revision = LEAST(revision + 1,8999999999999999)
        WHERE id = $1 AND owner_account_id = $2
          AND resource_type = 'profile' AND resource_id = $2::uuid::text
          AND purpose = 'profile_view' AND revoked_at IS NULL
        RETURNING resource_id`, grantID, ownerID).Scan(&resourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	if err != nil {
		return err
	}
	err = insertDomainAudit(ctx, tx, ownerID, "revoke", "profile", resourceID, "profile_view", &grantID)
	if err != nil {
		return err
	}
	return nil
}
func (s *Store) RevokeProfileGrant(ctx context.Context, ownerID, grantID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err = revokeProfileGrantTx(ctx, tx, ownerID, grantID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

var _ identity.GrantRevokeControlStore = (*Store)(nil)

func grantControlRow(ctx context.Context, f humanWriteFrame, id string, lock bool) (identity.Grant, string, error) {
	var g identity.Grant
	var source string
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	e := f.tx.QueryRow(ctx, `SELECT id,COALESCE(recipient_account_id::text,''),resource_type,resource_id,purpose,actions,revision,created_at,expires_at,revoked_at,xmin::text FROM consent_grants WHERE id=$1 AND owner_account_id=$2 AND resource_type='profile' AND resource_id=$2::uuid::text AND purpose='profile_view'`+suffix, id, f.a.ID).Scan(&g.ID, &g.RecipientAccountID, &g.ResourceType, &g.ResourceID, &g.Purpose, &g.Actions, &g.Revision, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt, &source)
	if errors.Is(e, pgx.ErrNoRows) {
		return g, source, identity.ErrNotFound
	}
	if e != nil {
		return g, source, identity.ErrControlUnavailable
	}
	if g.Revision < 1 || g.Revision > identity.HumanControlMaxVersion {
		return g, source, identity.ErrControlChanged
	}
	return g, source, nil
}
func (s *Store) ReadGrantRevokeControl(ctx context.Context, d [32]byte, a identity.Actor, id string) (identity.GrantRevokeControlState, error) {
	if !identity.HumanControlUUID(id) {
		return identity.GrantRevokeControlState{}, identity.ErrControlInvalid
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "GRANT_REVOKE", "")
	if e != nil {
		return identity.GrantRevokeControlState{}, e
	}
	defer f.tx.Rollback(context.Background())
	g, source, e := grantControlRow(ctx, f, id, false)
	if e != nil {
		return identity.GrantRevokeControlState{}, e
	}
	now, until, e := s.humanWriteNow(ctx, f, nil)
	if e != nil {
		return identity.GrantRevokeControlState{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		current, epoch, e := grantControlRow(ctx, next, id, false)
		if e != nil {
			return e
		}
		if epoch != source || humanWriteHashJSON(current) != humanWriteHashJSON(g) {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error { return check(ctx, f) })); e != nil {
		return identity.GrantRevokeControlState{}, e
	}
	return identity.GrantRevokeControlState{Grant: g, CanRevoke: g.RevokedAt == nil, ObservedAt: now, ExpiresAt: until, ResponseProof: humanWriteAttachProof(s, f, until, check)}, nil
}
func (s *Store) PreviewGrantRevokeControl(ctx context.Context, d [32]byte, a identity.Actor, in identity.GrantRevokeControlInput) (identity.GrantRevokeControlPreview, error) {
	in, e := identity.NormalizeGrantRevokeControl(in, false)
	if e != nil {
		return identity.GrantRevokeControlPreview{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "GRANT_REVOKE", in.OperationID)
	if e != nil {
		return identity.GrantRevokeControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = humanWriteReceiptTx(ctx, f); e == nil {
		return identity.GrantRevokeControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return identity.GrantRevokeControlPreview{}, identity.ErrControlUnavailable
	}
	g, source, e := grantControlRow(ctx, f, in.GrantID, false)
	if e != nil {
		return identity.GrantRevokeControlPreview{}, e
	}
	if g.Revision != in.ExpectedVersion || g.RevokedAt != nil {
		return identity.GrantRevokeControlPreview{}, identity.ErrControlChanged
	}
	base, e := s.humanWritePreview(ctx, f, "", in.Action, identity.GrantRevokeControlDigest(in), source, nil)
	if e != nil {
		return identity.GrantRevokeControlPreview{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		current, epoch, e := grantControlRow(ctx, next, in.GrantID, false)
		if e != nil {
			return e
		}
		if epoch != source || current.Revision != in.ExpectedVersion || current.RevokedAt != nil {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error { return check(ctx, f) })); e != nil {
		return identity.GrantRevokeControlPreview{}, e
	}
	base.ResponseProof = humanWriteAttachProof(s, f, base.ExpiresAt, check)
	return identity.GrantRevokeControlPreview{HumanControlPreview: base, GrantID: in.GrantID, ExpectedVersion: in.ExpectedVersion}, nil
}
func (s *Store) CommitGrantRevokeControl(ctx context.Context, d [32]byte, a identity.Actor, in identity.GrantRevokeControlInput) (identity.HumanControlReceipt, error) {
	in, e := identity.NormalizeGrantRevokeControl(in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "GRANT_REVOKE", in.OperationID)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := identity.GrantRevokeControlDigest(in)
	if r, done, e := s.humanWriteReplay(ctx, f, "", digest); done {
		return r, e
	}
	g, source, e := grantControlRow(ctx, f, in.GrantID, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	if g.Revision != in.ExpectedVersion || g.RevokedAt != nil {
		return identity.HumanControlReceipt{}, identity.ErrControlChanged
	}
	deadline, e := s.humanWriteApprove(ctx, f, "", in.Action, digest, source, in.Approval, nil)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	if e = revokeProfileGrantTx(ctx, f.tx, a.ID, in.GrantID); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	version := identity.HumanControlNextVersion(in.ExpectedVersion)
	r, e := humanWriteSaveReceipt(ctx, f, "", in.Action, in.GrantID, "", digest, version)
	if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, deadline, func() error {
		current, _, e := grantControlRow(ctx, f, in.GrantID, false)
		if e != nil {
			return e
		}
		if current.Revision != version || current.RevokedAt == nil {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	return r, nil
}
func (s *Store) ReadGrantRevokeOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "GRANT_REVOKE", op, "", false)
}
func (s *Store) StopGrantRevokeOperation(ctx context.Context, d [32]byte, a identity.Actor, op string) (identity.HumanControlReceipt, error) {
	return s.humanWriteHistory(ctx, d, a, "GRANT_REVOKE", op, "", true)
}
