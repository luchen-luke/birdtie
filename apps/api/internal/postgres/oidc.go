package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/oidcauth"
	"github.com/jackc/pgx/v5"
)

func (s *Store) SaveOIDCFlow(ctx context.Context, stateDigest [32]byte, flow oidcauth.Flow) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_login_flows
        WHERE expires_at <= now()`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO oidc_login_flows
        (state_sha256, nonce, provider_verifier, client_challenge)
        VALUES ($1, $2, $3, $4)`,
		stateDigest[:], flow.Nonce, flow.ProviderVerifier, flow.ClientChallenge)
	return err
}

func (s *Store) TakeOIDCFlow(ctx context.Context, stateDigest [32]byte) (oidcauth.Flow, error) {
	var flow oidcauth.Flow
	err := s.pool.QueryRow(ctx, `DELETE FROM oidc_login_flows
        WHERE state_sha256 = $1 AND expires_at > now()
        RETURNING nonce, provider_verifier, client_challenge`, stateDigest[:]).
		Scan(&flow.Nonce, &flow.ProviderVerifier, &flow.ClientChallenge)
	if errors.Is(err, pgx.ErrNoRows) {
		return oidcauth.Flow{}, oidcauth.ErrInvalidFlow
	}
	return flow, err
}

func (s *Store) SaveOIDCExchangeCode(
	ctx context.Context, issuer, subject, displayName string,
	codeDigest [32]byte, clientChallenge string,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize first enrollment for one verified issuer/subject.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(
        hashtext($1 || ':' || $2))`, issuer, subject); err != nil {
		return err
	}
	var accountID, status string
	err = tx.QueryRow(ctx, `SELECT a.id, a.status
        FROM account_auth_identities ai
        JOIN accounts a ON a.id = ai.account_id
        WHERE ai.issuer = $1 AND ai.subject = $2
        FOR UPDATE OF ai, a`, issuer, subject).Scan(&accountID, &status)
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
            VALUES ($1, $2, 'private')`, accountID, displayName); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO account_auth_identities
            (issuer, subject, account_id, verified_at)
            VALUES ($1, $2, $3, now())`, issuer, subject, accountID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if status != "active" {
		return identity.ErrUnauthorized
	} else {
		if _, err := tx.Exec(ctx, `UPDATE account_auth_identities
            SET verified_at = now() WHERE issuer = $1 AND subject = $2`,
			issuer, subject); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oidc_exchange_codes
        WHERE expires_at <= now()`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oidc_exchange_codes
        (code_sha256, account_id, client_challenge)
        VALUES ($1, $2, $3)`, codeDigest[:], accountID, clientChallenge); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RedeemOIDCExchangeCode(
	ctx context.Context, codeDigest [32]byte, clientChallenge string,
	sessionDigest [32]byte,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var accountID string
	err = tx.QueryRow(ctx, `DELETE FROM oidc_exchange_codes code
        USING accounts a
        WHERE code.code_sha256 = $1 AND code.client_challenge = $2
          AND code.expires_at > now()
          AND a.id = code.account_id AND a.status = 'active'
        RETURNING code.account_id`, codeDigest[:], clientChallenge).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oidcauth.ErrInvalidFlow
	}
	if err != nil {
		return err
	}
	var sessionID string
	err = tx.QueryRow(ctx, `INSERT INTO sessions (
        account_id, token_sha256, authentication_method,
        expires_at, idle_expires_at
    ) VALUES (
        $1, $2, 'oidc', now() + interval '8 hours',
        now() + interval '30 minutes'
    ) RETURNING id`, accountID, sessionDigest[:]).Scan(&sessionID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'session_issue', 'session', $2, 'allowed', 'login')`,
		accountID, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
