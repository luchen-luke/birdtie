package postgres

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

const devPhoneIssuer = "urn:birdtie:local-dev-phone"

func (s *Store) RequestDevPhoneChallenge(ctx context.Context, phoneDigest [32]byte) error {
	if !s.devPhoneEnabled {
		return devauth.ErrDisabled
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM dev_phone_challenges
        WHERE expires_at < now() - interval '1 day'`); err != nil {
		return err
	}
	var accepted bool
	err := s.pool.QueryRow(ctx, `INSERT INTO dev_phone_challenges
        (phone_digest, requested_at, expires_at, attempts)
        VALUES ($1, now(), now() + interval '5 minutes', 0)
        ON CONFLICT (phone_digest) DO UPDATE SET
            requested_at = now(), expires_at = now() + interval '5 minutes', attempts = 0
        WHERE dev_phone_challenges.requested_at <= now() - interval '60 seconds'
        RETURNING true`, phoneDigest[:]).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return devauth.ErrRateLimited
	}
	return err
}

func (s *Store) VerifyDevPhoneChallenge(ctx context.Context, phoneDigest [32]byte, codeValid bool, sessionDigest [32]byte) error {
	if !s.devPhoneEnabled {
		return devauth.ErrDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int
	err = tx.QueryRow(ctx, `SELECT attempts FROM dev_phone_challenges
        WHERE phone_digest = $1 AND expires_at > now() FOR UPDATE`, phoneDigest[:]).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return devauth.ErrInvalid
	}
	if err != nil {
		return err
	}
	if attempts >= 5 {
		return devauth.ErrRateLimited
	}
	if !codeValid {
		_, err = tx.Exec(ctx, `UPDATE dev_phone_challenges SET attempts = attempts + 1
            WHERE phone_digest = $1`, phoneDigest[:])
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return devauth.ErrInvalid
	}
	if _, err := tx.Exec(ctx, `DELETE FROM dev_phone_challenges WHERE phone_digest = $1`, phoneDigest[:]); err != nil {
		return err
	}
	subject := hex.EncodeToString(phoneDigest[:])
	// Serialize first enrollment for this local-only identity.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		devPhoneIssuer+":"+subject); err != nil {
		return err
	}
	var accountID, status string
	err = tx.QueryRow(ctx, `SELECT a.id, a.status FROM account_auth_identities ai
        JOIN accounts a ON a.id = ai.account_id
        WHERE ai.issuer = $1 AND ai.subject = $2 FOR UPDATE OF ai, a`,
		devPhoneIssuer, subject).Scan(&accountID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO accounts (id, account_type)
            VALUES (gen_random_uuid(), 'person') RETURNING id`).Scan(&accountID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agents (agent_type, principal_account_id)
            VALUES ('personal', $1) ON CONFLICT (agent_type, principal_account_id) DO NOTHING`, accountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_profiles
            (account_id, display_name, visibility)
            VALUES ($1, 'Birdtie 测试用户', 'private')`, accountID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_auth_identities
            (issuer, subject, account_id, verified_at)
            VALUES ($1, $2, $3, now())`, devPhoneIssuer, subject, accountID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if status != "active" {
		return identity.ErrUnauthorized
	}
	var sessionID string
	err = tx.QueryRow(ctx, `INSERT INTO sessions
        (account_id, token_sha256, authentication_method, expires_at, idle_expires_at)
        VALUES ($1, $2, 'dev_phone', now() + interval '8 hours',
                now() + interval '30 minutes') RETURNING id`,
		accountID, sessionDigest[:]).Scan(&sessionID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'session_issue', 'session', $2, 'allowed', 'local_dev_login')`,
		accountID, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
