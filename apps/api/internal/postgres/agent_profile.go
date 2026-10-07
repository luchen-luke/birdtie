package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

const agentProfileColumns = `agent_id, owner_type, owner_id, profile_version, created_at, updated_at`

// GetAgentProfile reads only native Agent metadata. The supplied owner is an
// internal binding claim checked against current database rows, not a human
// session, membership, model tool, purpose/consent, or private-profile grant.
// A future gateway must resolve those permissions before using this primitive.
func (s *Store) GetAgentProfile(ctx context.Context, agentID string, owner actorref.PrincipalRef) (agentprofile.Record, error) {
	binding, err := agentprofile.New(agentID, owner, time.Now())
	if err != nil {
		return agentprofile.Record{}, err
	}
	if binding.OwnerType == actorref.Business {
		return agentprofile.Record{}, agentprofile.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.Record{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockAgentProfileBinding(ctx, tx, binding); err != nil {
		return agentprofile.Record{}, err
	}
	profile, err := readAgentProfileMetadata(ctx, tx, binding)
	if err != nil {
		return agentprofile.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentprofile.Record{}, err
	}
	return profile, nil
}

// EnsureAgentProfile creates missing metadata for an existing current Agent.
// It never creates identities, changes contents, resets a revision, updates an
// existing row, or enables Business runtime capabilities. Initial version 1
// belongs to this new authoritative record, not to any copied UserProfile.
func (s *Store) EnsureAgentProfile(ctx context.Context, agentID string, owner actorref.PrincipalRef) (agentprofile.Record, error) {
	binding, err := agentprofile.New(agentID, owner, time.Now())
	if err != nil {
		return agentprofile.Record{}, err
	}
	if binding.OwnerType == actorref.Business {
		return agentprofile.Record{}, agentprofile.ErrUnavailable
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.Record{}, err
	}
	defer tx.Rollback(ctx)
	if err := lockAgentProfileBinding(ctx, tx, binding); err != nil {
		return agentprofile.Record{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agent_profiles (agent_id, owner_type, owner_id)
		VALUES ($1, $2, $3) ON CONFLICT (agent_id) DO NOTHING`,
		binding.AgentID, string(binding.OwnerType), binding.OwnerID); err != nil {
		return agentprofile.Record{}, err
	}
	profile, err := readAgentProfileMetadata(ctx, tx, binding)
	if err != nil {
		return agentprofile.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return agentprofile.Record{}, err
	}
	return profile, nil
}

func lockAgentProfileBinding(ctx context.Context, tx pgx.Tx, binding agentprofile.Record) error {
	agentType, accountType := "personal", "person"
	if binding.OwnerType == actorref.Organization {
		agentType, accountType = "organization", "organization"
	}
	var found string
	err := tx.QueryRow(ctx, `SELECT ag.id FROM agents ag
		JOIN accounts a ON a.id = ag.principal_account_id
		WHERE ag.id = $1 AND ag.principal_account_id = $2
			AND ag.agent_type = $3 AND ag.status = 'active'
			AND a.account_type = $4 AND a.status = 'active'
		FOR SHARE OF ag, a`, binding.AgentID, binding.OwnerID, agentType, accountType).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.ErrForbidden
	}
	if err != nil {
		return err
	}
	if binding.OwnerType == actorref.Organization {
		err = tx.QueryRow(ctx, `SELECT id FROM organizations
			WHERE account_id = $1 AND status = 'active' FOR SHARE`, binding.OwnerID).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return agentprofile.ErrForbidden
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func readAgentProfileMetadata(ctx context.Context, tx pgx.Tx, binding agentprofile.Record) (agentprofile.Record, error) {
	var profile agentprofile.Record
	err := tx.QueryRow(ctx, `SELECT `+agentProfileColumns+` FROM agent_profiles
		WHERE agent_id = $1 AND owner_type = $2 AND owner_id = $3`,
		binding.AgentID, string(binding.OwnerType), binding.OwnerID).Scan(
		&profile.AgentID, &profile.OwnerType, &profile.OwnerID,
		&profile.ProfileVersion, &profile.CreatedAt, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.Record{}, agentprofile.ErrNotFound
	}
	if err != nil {
		return agentprofile.Record{}, err
	}
	if err := agentprofile.Validate(profile); err != nil {
		return agentprofile.Record{}, err
	}
	return profile, nil
}
