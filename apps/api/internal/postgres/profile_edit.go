package postgres

import (
	"context"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func (s *Store) UpdateOwnProfile(ctx context.Context, ownerID string, input identity.ProfileInput) (identity.Profile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return identity.Profile{}, err
	}
	defer tx.Rollback(ctx)
	var current identity.Profile
	err = tx.QueryRow(ctx, `SELECT p.account_id, p.display_name, p.bio, p.visibility
        FROM user_profiles p JOIN accounts a ON a.id = p.account_id AND a.status = 'active'
        WHERE p.account_id = $1 FOR UPDATE OF p, a`, ownerID).Scan(
		&current.AccountID, &current.DisplayName, &current.Bio, &current.Visibility)
	if err != nil {
		return identity.Profile{}, err
	}
	if current.DisplayName == input.DisplayName && current.Bio == input.Bio &&
		current.Visibility == input.Visibility {
		return current, nil
	}
	var updated identity.Profile
	err = tx.QueryRow(ctx, `UPDATE user_profiles SET display_name = $2, bio = $3,
        visibility = $4, updated_at = now() WHERE account_id = $1
        RETURNING account_id, display_name, bio, visibility`, ownerID,
		input.DisplayName, input.Bio, input.Visibility).Scan(
		&updated.AccountID, &updated.DisplayName, &updated.Bio, &updated.Visibility)
	if err != nil {
		return identity.Profile{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE intents SET state = 'withdrawn', updated_at = now()
        WHERE owner_account_id = $1 AND audience = 'public'
          AND state IN ('draft', 'active')`, ownerID)
	if err != nil {
		return identity.Profile{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events
        (actor_account_id, action, resource_type, resource_id, decision, purpose)
        VALUES ($1, 'update', 'profile', $2, 'allowed', 'owner_profile_edit')`, ownerID, ownerID)
	if err != nil {
		return identity.Profile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Profile{}, err
	}
	return updated, nil
}
