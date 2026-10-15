package postgres

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

const devEmailIssuer = "urn:birdtie:local-dev-email"

var _ devauth.EmailStore = (*Store)(nil)

func (s *Store) RequestDevEmailChallenge(ctx context.Context, emailDigest [32]byte) error {
	if s == nil || s.pool == nil || !s.devPhoneEnabled {
		return devauth.ErrDisabled
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM dev_email_challenges
        WHERE expires_at < now() - interval '1 day'`); err != nil {
		return err
	}
	var accepted bool
	err := s.pool.QueryRow(ctx, `INSERT INTO dev_email_challenges
        (email_digest, requested_at, expires_at, attempts)
        VALUES ($1, now(), now() + interval '5 minutes', 0)
        ON CONFLICT (email_digest) DO UPDATE SET
            requested_at = now(), expires_at = now() + interval '5 minutes', attempts = 0
        WHERE dev_email_challenges.requested_at <= now() - interval '60 seconds'
        RETURNING true`, emailDigest[:]).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return devauth.ErrRateLimited
	}
	return err
}

func (s *Store) VerifyDevEmailChallenge(ctx context.Context, emailDigest [32]byte, codeValid bool, sessionDigest [32]byte) error {
	if s == nil || s.pool == nil || !s.devPhoneEnabled {
		return devauth.ErrDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT attempts,expires_at FROM dev_email_challenges
        WHERE email_digest = $1 AND expires_at > clock_timestamp() FOR UPDATE`, emailDigest[:]).Scan(&attempts, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return devauth.ErrInvalid
	}
	if err != nil {
		return err
	}
	if attempts >= 5 {
		return devauth.ErrRateLimited
	}
	var observed time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
		return err
	}
	if !expires.After(observed) {
		return devauth.ErrInvalid
	}
	if !codeValid {
		_, err = tx.Exec(ctx, `UPDATE dev_email_challenges SET attempts = attempts + 1
            WHERE email_digest = $1`, emailDigest[:])
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return devauth.ErrInvalid
	}
	if _, err := tx.Exec(ctx, `DELETE FROM dev_email_challenges WHERE email_digest = $1`, emailDigest[:]); err != nil {
		return err
	}
	subject := hex.EncodeToString(emailDigest[:])
	// Serialize first enrollment for this local-only identity.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		devEmailIssuer+":"+subject); err != nil {
		return err
	}
	var accountID, status string
	err = tx.QueryRow(ctx, `SELECT a.id, a.status FROM account_auth_identities ai
        JOIN accounts a ON a.id = ai.account_id
        WHERE ai.issuer = $1 AND ai.subject = $2 FOR UPDATE OF ai, a`,
		devEmailIssuer, subject).Scan(&accountID, &status)
	missingIdentity := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !missingIdentity {
		return err
	}
	nativeOwner := accountID
	if missingIdentity {
		nativeOwner = ""
	}
	mappedOwner, firstEnrollment, err := s.legacyDevEmailOwner(ctx, tx, emailDigest, nativeOwner)
	if err != nil {
		return err
	}
	if missingIdentity {
		if mappedOwner == "" {
			return identity.ErrUnauthorized
		}
		accountID = mappedOwner

		if _, err := tx.Exec(ctx, `INSERT INTO account_auth_identities
            (issuer, subject, account_id, verified_at)
            VALUES ($1, $2, $3, now())`, devEmailIssuer, subject, accountID); err != nil {
			return err
		}
	} else if status != "active" {
		return identity.ErrUnauthorized
	}
	var sessionID string
	err = tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp()at) INSERT INTO sessions
        (account_id, token_sha256, authentication_method,development_credential_kind, expires_at, idle_expires_at)
        SELECT $1, $2, 'dev_phone','email', cl.at + interval '8 hours',
                cl.at + interval '30 minutes' FROM cl RETURNING id`,
		accountID, sessionDigest[:]).Scan(&sessionID)
	if err != nil {
		return err
	}
	if firstEnrollment {
		if err = s.finishLegacyDevEmailEnrollment(ctx, tx, emailDigest, accountID, sessionID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'session_issue', 'session', $2, 'allowed', 'local_dev_email_login')`,
		accountID, sessionID); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
		return err
	}
	if !expires.After(observed) {
		return devauth.ErrInvalid
	}
	return tx.Commit(ctx)
}
