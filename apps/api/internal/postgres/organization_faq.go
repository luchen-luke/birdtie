package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const faqColumns = `id, organization_id, question, answer, published, created_at, updated_at`
const auditedFAQColumns = `f.id, f.organization_id, f.question, f.answer, f.published, f.created_at, f.updated_at`

func scanFAQ(row scanner) (organization.FAQ, error) {
	var item organization.FAQ
	err := row.Scan(&item.ID, &item.OrganizationID, &item.Question, &item.Answer,
		&item.Published, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func faqWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return organization.ErrFAQConflict
	}
	return err
}

func (s *Store) requireFAQAdmin(ctx context.Context, userID, organizationID string) error {
	err := s.organizationAdmin(ctx, userID, organizationID)
	if errors.Is(err, activitypublish.ErrForbidden) {
		return organization.ErrForbidden
	}
	return err
}

func (s *Store) ListFAQs(ctx context.Context, userID, organizationID string) ([]organization.FAQ, error) {
	if err := s.requireFAQAdmin(ctx, userID, organizationID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+faqColumns+` FROM organization_faqs
		WHERE organization_id=$1 ORDER BY updated_at DESC, id DESC LIMIT 100`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []organization.FAQ{}
	for rows.Next() {
		item, err := scanFAQ(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateFAQ(ctx context.Context, userID, organizationID string, input organization.FAQInput) (organization.FAQ, error) {
	if err := s.requireFAQAdmin(ctx, userID, organizationID); err != nil {
		return organization.FAQ{}, err
	}
	item, err := scanFAQ(auditPoolQueryRow(ctx, s.pool, `WITH inserted AS (INSERT INTO organization_faqs
		(organization_id, question, answer, published, created_by_account_id)
		SELECT $1, $3, $4, $5, $2 WHERE EXISTS (
			SELECT 1 FROM organization_memberships m JOIN organizations o ON o.id=m.organization_id
			WHERE m.organization_id=$1 AND m.user_account_id=$2 AND m.status='active'
			AND m.role IN ('owner','admin') AND o.status='active')
		RETURNING *), logged AS (
		INSERT INTO admin_audit_events
		(actor_account_id,organization_id,resource_type,resource_id,action)
		SELECT $2,$1,'faq',id,'faq_create' FROM inserted RETURNING id)
		SELECT `+auditedFAQColumns+` FROM inserted f JOIN logged l ON true`, organizationID, userID, input.Question, input.Answer, input.Published))
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.FAQ{}, organization.ErrForbidden
	}
	return item, faqWriteError(err)
}

func (s *Store) UpdateFAQ(ctx context.Context, userID, organizationID, faqID string, input organization.FAQInput) (organization.FAQ, error) {
	if err := s.requireFAQAdmin(ctx, userID, organizationID); err != nil {
		return organization.FAQ{}, err
	}
	item, err := scanFAQ(auditPoolQueryRow(ctx, s.pool, `WITH updated AS (UPDATE organization_faqs f
		SET question=$4, answer=$5, published=$6, updated_at=now()
		WHERE f.id=$3 AND f.organization_id=$1 AND EXISTS (
			SELECT 1 FROM organization_memberships m JOIN organizations o ON o.id=m.organization_id
			WHERE m.organization_id=$1 AND m.user_account_id=$2 AND m.status='active'
			AND m.role IN ('owner','admin') AND o.status='active')
		RETURNING f.*), logged AS (
		INSERT INTO admin_audit_events
		(actor_account_id,organization_id,resource_type,resource_id,action)
		SELECT $2,$1,'faq',id,'faq_update' FROM updated RETURNING id)
		SELECT `+auditedFAQColumns+` FROM updated f JOIN logged l ON true`, organizationID, userID, faqID, input.Question, input.Answer, input.Published))
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.FAQ{}, organization.ErrFAQNotFound
	}
	return item, faqWriteError(err)
}

func (s *Store) DeleteFAQ(ctx context.Context, userID, organizationID, faqID string) error {
	if err := s.requireFAQAdmin(ctx, userID, organizationID); err != nil {
		return err
	}
	var deletedID string
	err := auditPoolQueryRow(ctx, s.pool, `WITH deleted AS (DELETE FROM organization_faqs f
		WHERE f.id=$3 AND f.organization_id=$1 AND EXISTS (
			SELECT 1 FROM organization_memberships m JOIN organizations o ON o.id=m.organization_id
			WHERE m.organization_id=$1 AND m.user_account_id=$2 AND m.status='active'
			AND m.role IN ('owner','admin') AND o.status='active')
		RETURNING f.id), logged AS (
		INSERT INTO admin_audit_events
		(actor_account_id,organization_id,resource_type,resource_id,action)
		SELECT $2,$1,'faq',id,'faq_delete' FROM deleted RETURNING id)
		SELECT id FROM logged`, organizationID, userID, faqID).Scan(&deletedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.ErrFAQNotFound
	}
	return err
}

func (s *Store) AnswerOrganization(ctx context.Context, organizationID, viewerID, query string) (organization.AgentAnswer, error) {
	return s.answerCurrentOrganization(ctx, organizationID, viewerID, query)
}
