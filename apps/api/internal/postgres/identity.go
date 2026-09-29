package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) Authenticate(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	var actor identity.Actor
	err := s.pool.QueryRow(ctx, `UPDATE sessions AS s
        SET idle_expires_at = LEAST(s.expires_at, now() + interval '30 minutes')
        FROM accounts AS a
        WHERE s.token_sha256 = $1 AND a.id = s.account_id
          AND a.status = 'active' AND s.revoked_at IS NULL
          AND s.expires_at > now() AND s.idle_expires_at > now()
        RETURNING a.id, a.account_type, a.handle`, digest[:]).
		Scan(&actor.ID, &actor.AccountType, &actor.Handle)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	return actor, err
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
	err := s.pool.QueryRow(ctx, `SELECT p.account_id, p.display_name, p.bio, p.visibility
        FROM user_profiles p
        JOIN accounts a ON a.id = p.account_id
        WHERE p.account_id = $1 AND a.status = 'active'
          AND NOT EXISTS (
              SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
                AND ((b.blocker_account_id = p.account_id AND b.blocked_account_id = $2)
                  OR (b.blocker_account_id = $2 AND b.blocked_account_id = p.account_id))
          )
          AND (p.visibility = 'public' OR p.account_id = $2
            OR EXISTS (
                SELECT 1 FROM consent_grants g
                WHERE g.owner_account_id = p.account_id
                  AND g.recipient_account_id = $2
                  AND g.resource_type = 'profile'
                  AND g.resource_id = p.account_id::text
                  AND g.purpose = 'profile_view'
                  AND g.actions @> ARRAY['read']::text[]
                  AND g.revoked_at IS NULL
                  AND (g.expires_at IS NULL OR g.expires_at > now())
            ))`, targetID, actor).
		Scan(&profile.AccountID, &profile.DisplayName, &profile.Bio, &profile.Visibility)
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
          AND resource_id = $1::text AND purpose = 'profile_view'
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
        gen_random_uuid(), $1, $2, 'profile', $1::text,
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
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'grant', 'profile', $1::text, 'allowed', 'profile_view')`, ownerID)
	if err != nil {
		return identity.Grant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Grant{}, err
	}
	return grant, nil
}

func (s *Store) RevokeProfileGrant(ctx context.Context, ownerID, grantID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var resourceID string
	err = tx.QueryRow(ctx, `UPDATE consent_grants
        SET revoked_at = now(), revision = revision + 1
        WHERE id = $1 AND owner_account_id = $2
          AND resource_type = 'profile' AND resource_id = $2::text
          AND purpose = 'profile_view' AND revoked_at IS NULL
        RETURNING resource_id`, grantID, ownerID).Scan(&resourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'revoke', 'profile', $2, 'allowed', 'profile_view')`,
		ownerID, resourceID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
