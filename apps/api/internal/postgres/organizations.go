package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
)

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
