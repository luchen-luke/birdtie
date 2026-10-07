package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
)

func (s *Store) UpdateProfile(ctx context.Context, userID, organizationID string, input organization.ProfileInput) (organization.Organization, error) {
	links, err := json.Marshal(input.OfficialLinks)
	if err != nil {
		return organization.Organization{}, err
	}
	var out organization.Organization
	err = auditPoolQueryRow(ctx, s.pool, `WITH updated AS (UPDATE organizations o
        SET name = $3, description = $4, official_links = $5::jsonb, updated_at = now()
        FROM organization_memberships m JOIN accounts u ON u.id = m.user_account_id
        WHERE o.id = $1 AND o.status = 'active'
          AND m.organization_id = o.id AND m.user_account_id = $2
          AND m.status = 'active' AND m.role IN ('owner', 'admin')
          AND u.account_type = 'person' AND u.status = 'active'
        RETURNING o.id, o.account_id, o.organization_type, o.name, m.role,
                  o.description, o.official_links), logged AS (
          INSERT INTO admin_audit_events
            (actor_account_id,organization_id,resource_type,resource_id,action)
          SELECT $2,id,'organization',id,'organization_profile_update' FROM updated
          RETURNING id)
        SELECT u.id,u.account_id,u.organization_type,u.name,u.role,u.description,u.official_links
        FROM updated u JOIN logged l ON true`, organizationID, userID,
		input.Name, input.Description, links).Scan(
		&out.ID, &out.AccountID, &out.OrganizationType, &out.Name,
		&out.Role, &out.Description, &out.OfficialLinks)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.Organization{}, organization.ErrForbidden
	}
	return out, err
}

func (s *Store) CreateOrganization(ctx context.Context, userID string, input organization.CreateInput) (organization.Organization, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return organization.Organization{}, err
	}
	defer tx.Rollback(ctx)
	var out organization.Organization
	err = tx.QueryRow(ctx, `INSERT INTO accounts (id, account_type)
        SELECT gen_random_uuid(), 'organization' WHERE EXISTS (
            SELECT 1 FROM accounts WHERE id = $1 AND account_type = 'person' AND status = 'active'
        ) RETURNING id`, userID).Scan(&out.AccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.Organization{}, organization.ErrForbidden
	}
	if err != nil {
		return organization.Organization{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO organizations (account_id, organization_type, name)
        VALUES ($1, $2, $3) RETURNING id, organization_type, name`,
		out.AccountID, input.OrganizationType, input.Name).
		Scan(&out.ID, &out.OrganizationType, &out.Name)
	if err != nil {
		return organization.Organization{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agents (agent_type, principal_account_id)
        VALUES ('organization', $1)`, out.AccountID); err != nil {
		return organization.Organization{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO organization_memberships (organization_id, user_account_id, role)
        VALUES ($1, $2, 'owner')`, out.ID, userID); err != nil {
		return organization.Organization{}, err
	}
	if err := insertAdminAudit(ctx, tx, userID, out.ID, "organization", out.ID, "organization_create"); err != nil {
		return organization.Organization{}, err
	}
	out.Role = "owner"
	if err := tx.Commit(ctx); err != nil {
		return organization.Organization{}, err
	}
	return out, nil
}

func (s *Store) ResolveWorkspace(ctx context.Context, userID, organizationID string) (string, string, error) {
	var principalID, role string
	err := s.pool.QueryRow(ctx, `SELECT o.account_id, m.role
        FROM organizations o JOIN organization_memberships m ON m.organization_id = o.id
        JOIN accounts u ON u.id = m.user_account_id AND u.account_type = 'person' AND u.status = 'active'
        JOIN accounts principal ON principal.id = o.account_id
          AND principal.account_type = 'organization' AND principal.status = 'active'
        JOIN agents ag ON ag.principal_account_id = principal.id
          AND ag.agent_type = 'organization' AND ag.status = 'active'
        WHERE m.user_account_id = $1 AND o.id = $2 AND m.status = 'active' AND o.status = 'active'`,
		userID, organizationID).Scan(&principalID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", organization.ErrForbidden
	}
	return principalID, role, err
}

func (s *Store) ListOrganizations(ctx context.Context, userID string) ([]organization.Organization, error) {
	rows, err := s.pool.Query(ctx, `SELECT o.id, o.account_id, o.organization_type, o.name, m.role
        FROM organization_memberships m JOIN organizations o ON o.id = m.organization_id
        WHERE m.user_account_id = $1 AND m.status = 'active' AND o.status = 'active'
        ORDER BY lower(o.name), o.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]organization.Organization, 0)
	for rows.Next() {
		var item organization.Organization
		if err := rows.Scan(&item.ID, &item.AccountID, &item.OrganizationType, &item.Name, &item.Role); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetPublicProfile(ctx context.Context, organizationID, viewerID string) (organization.PublicProfile, error) {
	return s.readCoherentPublicOrganization(ctx, organizationID, viewerID)
}
