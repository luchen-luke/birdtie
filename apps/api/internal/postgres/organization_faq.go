package postgres

import (
	"context"
	"errors"

	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const faqColumns = `id, organization_id, question, answer, published, created_at, updated_at,revision`
const auditedFAQColumns = `f.id, f.organization_id, f.question, f.answer, f.published, f.created_at, f.updated_at,f.revision`

func scanFAQ(row scanner) (organization.FAQ, error) {
	var item organization.FAQ
	err := row.Scan(&item.ID, &item.OrganizationID, &item.Question, &item.Answer,
		&item.Published, &item.CreatedAt, &item.UpdatedAt, &item.Revision)
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

func (s *Store) faqCompatibilityTx(ctx context.Context, userID, orgID string, in organization.FAQControlInput) (organization.FAQ, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return organization.FAQ{}, e
	}
	defer tx.Rollback(context.Background())
	f := humanWriteFrame{tx: tx, a: identity.Actor{ID: userID, AccountType: "person"}}
	if _, _, e = faqControlAuthority(ctx, f, orgID, true); e != nil {
		return organization.FAQ{}, e
	}
	if in.Action != "CREATE" {
		e = tx.QueryRow(ctx, `SELECT revision FROM organization_faqs WHERE organization_id=$1 AND id=$2 FOR UPDATE`, orgID, in.FAQID).Scan(&in.ExpectedVersion)
		if errors.Is(e, pgx.ErrNoRows) {
			return organization.FAQ{}, organization.ErrFAQNotFound
		}
		if e != nil {
			return organization.FAQ{}, e
		}
	}
	id, _, e := faqControlEffectTx(ctx, f, orgID, in)
	if e != nil {
		return organization.FAQ{}, e
	}
	var out organization.FAQ
	if in.Action != "DELETE" {
		out, e = scanFAQ(tx.QueryRow(ctx, `SELECT `+faqColumns+` FROM organization_faqs WHERE id=$1`, id))
		if e != nil {
			return out, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return out, nil
}
func (s *Store) CreateFAQ(ctx context.Context, userID, orgID string, in organization.FAQInput) (organization.FAQ, error) {
	return s.faqCompatibilityTx(ctx, userID, orgID, organization.FAQControlInput{Action: "CREATE", Question: in.Question, Answer: in.Answer, Published: &in.Published})
}
func (s *Store) UpdateFAQ(ctx context.Context, userID, orgID, id string, in organization.FAQInput) (organization.FAQ, error) {
	return s.faqCompatibilityTx(ctx, userID, orgID, organization.FAQControlInput{Action: "UPDATE", FAQID: id, Question: in.Question, Answer: in.Answer, Published: &in.Published})
}
func (s *Store) DeleteFAQ(ctx context.Context, userID, orgID, id string) error {
	_, e := s.faqCompatibilityTx(ctx, userID, orgID, organization.FAQControlInput{Action: "DELETE", FAQID: id})
	return e
}

func (s *Store) AnswerOrganization(ctx context.Context, organizationID, viewerID, query string) (organization.AgentAnswer, error) {
	return s.answerCurrentOrganization(ctx, organizationID, viewerID, query)
}

var _ organization.FAQControlStore = (*Store)(nil)

func faqControlAuthority(ctx context.Context, f humanWriteFrame, orgID string, lock bool) (string, string, error) {
	var role, source string
	suffix := ""
	if lock {
		suffix = " FOR SHARE OF m,o,principal,u"
	}
	e := f.tx.QueryRow(ctx, `SELECT m.role,concat_ws(':',m.xmin::text,o.xmin::text,principal.xmin::text,u.xmin::text) FROM organizations o JOIN accounts principal ON principal.id=o.account_id JOIN organization_memberships m ON m.organization_id=o.id JOIN accounts u ON u.id=m.user_account_id WHERE o.id=$1 AND o.status='active' AND principal.account_type='organization' AND principal.status='active' AND m.user_account_id=$2 AND m.status='active' AND m.role IN('owner','admin') AND u.account_type='person' AND u.status='active'`+suffix, orgID, f.a.ID).Scan(&role, &source)
	if errors.Is(e, pgx.ErrNoRows) {
		return role, source, organization.ErrForbidden
	}
	if e != nil {
		return role, source, identity.ErrControlUnavailable
	}
	return role, source, nil
}
func faqControlItems(ctx context.Context, f humanWriteFrame, orgID string) ([]organization.FAQ, error) {
	rows, e := f.tx.Query(ctx, `SELECT `+faqColumns+` FROM organization_faqs WHERE organization_id=$1 ORDER BY updated_at DESC,id DESC LIMIT 100`, orgID)
	if e != nil {
		return nil, identity.ErrControlUnavailable
	}
	defer rows.Close()
	items := []organization.FAQ{}
	for rows.Next() {
		v, e := scanFAQ(rows)
		if e != nil {
			return nil, identity.ErrControlUnavailable
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
func faqControlTarget(ctx context.Context, f humanWriteFrame, orgID string, in organization.FAQControlInput, lock bool) (string, error) {
	if in.Action == "CREATE" {
		var exists bool
		e := f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organization_faqs WHERE organization_id=$1 AND question=$2)`, orgID, in.Question).Scan(&exists)
		if e != nil {
			return "", identity.ErrControlUnavailable
		}
		if exists {
			return "", identity.ErrControlChanged
		}
		return "absent", nil
	}
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var version int64
	var epoch string
	e := f.tx.QueryRow(ctx, `SELECT revision,xmin::text FROM organization_faqs WHERE organization_id=$1 AND id=$2`+suffix, orgID, in.FAQID).Scan(&version, &epoch)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	if e != nil {
		return "", identity.ErrControlUnavailable
	}
	if version != in.ExpectedVersion {
		return "", identity.ErrControlChanged
	}
	return epoch, nil
}
func faqControlSource(ctx context.Context, f humanWriteFrame, orgID string, in organization.FAQControlInput, lock bool) (string, string, error) {
	_, authority, e := faqControlAuthority(ctx, f, orgID, false)
	if e != nil {
		return "", "", e
	}
	target, e := faqControlTarget(ctx, f, orgID, in, lock)
	if e != nil {
		return "", "", e
	}
	return authority, identity.HumanControlTextHash(authority + ":" + target), nil
}

// Native effect only. Both compatibility methods and the controlled writer
// use the original resource SQL and its single admin audit in this transaction.
func faqControlEffectTx(ctx context.Context, f humanWriteFrame, orgID string, in organization.FAQControlInput) (string, int64, error) {
	var item organization.FAQ
	var e error
	published := false
	if in.Published != nil {
		published = *in.Published
	}
	switch in.Action {
	case "CREATE":
		item, e = scanFAQ(auditQueryRow(ctx, f.tx, `WITH inserted AS(INSERT INTO organization_faqs(organization_id,question,answer,published,created_by_account_id)VALUES($1,$3,$4,$5,$2)RETURNING *),logged AS(INSERT INTO admin_audit_events(actor_account_id,organization_id,resource_type,resource_id,action)SELECT $2,$1,'faq',id,'faq_create' FROM inserted RETURNING id)SELECT `+auditedFAQColumns+` FROM inserted f JOIN logged l ON true`, orgID, f.a.ID, in.Question, in.Answer, published))
	case "UPDATE":
		item, e = scanFAQ(auditQueryRow(ctx, f.tx, `WITH updated AS(UPDATE organization_faqs SET question=$4,answer=$5,published=$6,updated_at=clock_timestamp() WHERE organization_id=$1 AND id=$3 AND revision=$7 RETURNING *),logged AS(INSERT INTO admin_audit_events(actor_account_id,organization_id,resource_type,resource_id,action)SELECT $2,$1,'faq',id,'faq_update' FROM updated RETURNING id)SELECT `+auditedFAQColumns+` FROM updated f JOIN logged l ON true`, orgID, f.a.ID, in.FAQID, in.Question, in.Answer, published, in.ExpectedVersion))
	case "DELETE":
		var id string
		e = auditQueryRow(ctx, f.tx, `WITH deleted AS(DELETE FROM organization_faqs WHERE organization_id=$1 AND id=$3 AND revision=$4 RETURNING id),logged AS(INSERT INTO admin_audit_events(actor_account_id,organization_id,resource_type,resource_id,action)SELECT $2,$1,'faq',id,'faq_delete' FROM deleted RETURNING id)SELECT id FROM deleted`, orgID, f.a.ID, in.FAQID, in.ExpectedVersion).Scan(&id)
		return id, identity.HumanControlNextVersion(in.ExpectedVersion), e
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return "", 0, identity.ErrControlChanged
	}
	if e != nil {
		return "", 0, faqWriteError(e)
	}
	return item.ID, item.Revision, nil
}
func (s *Store) ReadFAQControl(ctx context.Context, d [32]byte, a identity.Actor, orgID string) (organization.FAQControlState, error) {
	if !identity.HumanControlUUID(orgID) {
		return organization.FAQControlState{}, identity.ErrControlInvalid
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "FAQ", "")
	if e != nil {
		return organization.FAQControlState{}, e
	}
	defer f.tx.Rollback(context.Background())
	role, authority, e := faqControlAuthority(ctx, f, orgID, false)
	if e != nil {
		return organization.FAQControlState{}, e
	}
	items, e := faqControlItems(ctx, f, orgID)
	if e != nil {
		return organization.FAQControlState{}, e
	}
	now, until, e := s.humanWriteNow(ctx, f, nil)
	if e != nil {
		return organization.FAQControlState{}, e
	}
	digest := humanWriteHashJSON(items)
	check := func(ctx context.Context, next humanWriteFrame) error {
		currentRole, currentAuth, e := faqControlAuthority(ctx, next, orgID, true)
		if e != nil {
			return e
		}
		current, e := faqControlItems(ctx, next, orgID)
		if e != nil {
			return e
		}
		if currentRole != role || currentAuth != authority || humanWriteHashJSON(current) != digest {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error { return check(ctx, f) })); e != nil {
		return organization.FAQControlState{}, e
	}
	return organization.FAQControlState{OrganizationID: orgID, Role: role, Items: items, ObservedAt: now, ExpiresAt: until, ResponseProof: humanWriteAttachProof(s, f, until, check)}, nil
}
func (s *Store) PreviewFAQControl(ctx context.Context, d [32]byte, a identity.Actor, orgID string, in organization.FAQControlInput) (organization.FAQControlPreview, error) {
	in, e := organization.NormalizeFAQControl(orgID, in, false)
	if e != nil {
		return organization.FAQControlPreview{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "FAQ", in.OperationID)
	if e != nil {
		return organization.FAQControlPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = humanWriteReceiptTx(ctx, f); e == nil {
		return organization.FAQControlPreview{}, identity.ErrControlChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return organization.FAQControlPreview{}, identity.ErrControlUnavailable
	}
	_, source, e := faqControlSource(ctx, f, orgID, in, false)
	if e != nil {
		return organization.FAQControlPreview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, orgID, in.Action, organization.FAQControlDigest(orgID, in), source, nil)
	if e != nil {
		return organization.FAQControlPreview{}, e
	}
	check := func(ctx context.Context, next humanWriteFrame) error {
		_, current, e := faqControlSource(ctx, next, orgID, in, false)
		if e != nil {
			return e
		}
		if current != source {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error { return check(ctx, f) })); e != nil {
		return organization.FAQControlPreview{}, e
	}
	base.ResponseProof = humanWriteAttachProof(s, f, base.ExpiresAt, check)
	return organization.FAQControlPreview{HumanControlPreview: base, OrganizationID: orgID, FAQID: in.FAQID, ExpectedVersion: in.ExpectedVersion, Question: in.Question, Answer: in.Answer, Published: in.Published}, nil
}
func (s *Store) CommitFAQControl(ctx context.Context, d [32]byte, a identity.Actor, orgID string, in organization.FAQControlInput) (identity.HumanControlReceipt, error) {
	in, e := organization.NormalizeFAQControl(orgID, in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	ctx, f, e := s.humanWriteBegin(ctx, d, a, "FAQ", in.OperationID)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := organization.FAQControlDigest(orgID, in)
	if r, done, e := s.humanWriteReplay(ctx, f, orgID, digest); done {
		return r, e
	}
	authority, source, e := faqControlSource(ctx, f, orgID, in, true)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	deadline, e := s.humanWriteApprove(ctx, f, orgID, in.Action, digest, source, in.Approval, nil)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	id, version, e := faqControlEffectTx(ctx, f, orgID, in)
	if e != nil {
		return identity.HumanControlReceipt{}, e
	}
	r, e := humanWriteSaveReceipt(ctx, f, orgID, in.Action, id, id, digest, version)
	if e != nil {
		return r, e
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, deadline, func() error {
		_, current, e := faqControlAuthority(ctx, f, orgID, true)
		if e != nil {
			return e
		}
		if current != authority {
			return identity.ErrControlChanged
		}
		if in.Action == "DELETE" {
			var exists bool
			if e = f.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM organization_faqs WHERE id=$1)`, id).Scan(&exists); e != nil || exists {
				return identity.ErrControlChanged
			}
			return nil
		}
		currentItem, e := scanFAQ(f.tx.QueryRow(ctx, `SELECT `+faqColumns+` FROM organization_faqs WHERE organization_id=$1 AND id=$2`, orgID, id))
		if e != nil || currentItem.Revision != version || currentItem.Question != in.Question || currentItem.Answer != in.Answer || currentItem.Published != *in.Published {
			return identity.ErrControlChanged
		}
		return nil
	})); e != nil {
		return identity.HumanControlReceipt{}, e
	}
	return r, nil
}
func (s *Store) ReadFAQControlOperation(ctx context.Context, d [32]byte, a identity.Actor, orgID, op string) (identity.HumanControlReceipt, error) {
	if !identity.HumanControlUUID(orgID) {
		return identity.HumanControlReceipt{}, identity.ErrControlInvalid
	}
	return s.humanWriteHistory(ctx, d, a, "FAQ", op, orgID, false)
}
func (s *Store) StopFAQControlOperation(ctx context.Context, d [32]byte, a identity.Actor, orgID, op string) (identity.HumanControlReceipt, error) {
	if !identity.HumanControlUUID(orgID) {
		return identity.HumanControlReceipt{}, identity.ErrControlInvalid
	}
	return s.humanWriteHistory(ctx, d, a, "FAQ", op, orgID, true)
}
