package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ identity.HumanProfileStore = (*Store)(nil)

// UpdateHumanProfile is only the ordinary current human's profile edit. It
// updates an existing native user_profiles row, never an Agent metadata row,
// private fields, permissions, Memory or an organization role. The original
// internal owner-ID method is deliberately not a fallback for this gateway.
func (s *Store) UpdateHumanProfile(ctx context.Context, digest [32]byte, initialActor identity.Actor, input identity.ProfileInput) (identity.Profile, error) {
	if ctx == nil || s == nil || s.pool == nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	principal, err := actorref.ParsePrincipal(initialActor.AccountType, initialActor.ID)
	if err != nil || principal.Type == actorref.Community || principal.ID != initialActor.ID ||
		strings.ToLower(string(principal.Type)) != initialActor.AccountType || digest == ([32]byte{}) {
		return identity.Profile{}, identity.ErrUnauthorized
	}
	input, err = identity.NormalizeHumanProfileInput(input)
	if err != nil {
		return identity.Profile{}, err
	}
	if ctx.Err() != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	// Preserve Account -> Profile lock order. Authenticate updates only Session
	// with an MVCC Account join, and RevokeSession updates only Session. Session
	// checks happen after an actual Account wait, not in an older snapshot.
	var ownerID string
	err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type=$2 AND status='active'
		FOR NO KEY UPDATE`, principal.ID, initialActor.AccountType).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrUnauthorized
	}
	if err != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	var sessionID string
	err = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2
		AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp()
		AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, digest[:], ownerID, s.devPhoneEnabled).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrUnauthorized
	}
	if err != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	profile, err := updateOwnProfileInTx(ctx, tx, ownerID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	// Even a no-op must pass the final current wall clock. A failure rolls back
	// Profile, old public Intent withdrawal and audit atomically.
	var current bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id
		WHERE s.id=$1 AND s.token_sha256=$2 AND a.id=$3 AND a.account_type=$4 AND a.status='active'
		AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
		AND ($5::boolean OR s.authentication_method<>'dev_phone'))`, sessionID, digest[:], ownerID, initialActor.AccountType, s.devPhoneEnabled).Scan(&current)
	if err != nil || ctx.Err() != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	if !current {
		return identity.Profile{}, identity.ErrUnauthorized
	}
	if err = tx.Commit(ctx); err != nil {
		return identity.Profile{}, identity.ErrProfileUnavailable
	}
	return profile, nil
}
