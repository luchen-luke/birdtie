package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/organizationannouncement"
	"github.com/jackc/pgx/v5"
)

const announcementColumns = `id,organization_id,organization_account_id,revision,title,body,audience,state,valid_until,created_by,updated_by,created_at,updated_at,published_at,withdrawn_at,publication_context`

// A publication is bound to the actual public Organization version at human approval.
const announcementContextSQL = `jsonb_build_object('organizationId',o.id,'accountId',o.account_id,'name',o.name,'organizationType',o.organization_type,'status',o.status,'visibility',o.visibility,'verificationStatus',o.verification_status,'updatedAt',o.updated_at)`
const announcementPublicPredicate = ` n.organization_id=o.id AND n.organization_account_id=o.account_id AND n.state='PUBLISHED' AND n.audience='PUBLIC'
 AND n.published_at<=clock_timestamp() AND n.valid_until>clock_timestamp()
 AND (n.publication_context-'updatedAt')=(` + announcementContextSQL + `-'updatedAt')
 AND (n.publication_context->>'updatedAt')::timestamptz=o.updated_at
 AND o.status='active' AND o.visibility='public' AND o.verification_status='verified'
 AND EXISTS(SELECT 1 FROM accounts pa WHERE pa.id=o.account_id AND pa.account_type='organization' AND pa.status='active')
 AND EXISTS(SELECT 1 FROM agents ag JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_type='ORGANIZATION' AND ap.owner_id=o.account_id
 WHERE ag.principal_account_id=o.account_id AND ag.agent_type='organization' AND ag.status='active' AND ap.profile_version>0)
 AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$3::uuid AND bl.blocked_account_id=o.account_id) OR (bl.blocker_account_id=o.account_id AND bl.blocked_account_id=$3::uuid))`

func scanAnnouncement(row pgx.Row) (organizationannouncement.Record, error) {
	r := organizationannouncement.Record{SchemaVersion: organizationannouncement.Schema}
	e := row.Scan(&r.ID, &r.OrganizationID, &r.OrganizationAccountID, &r.Revision, &r.Title, &r.Body, &r.Audience, &r.State, &r.ValidUntil, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &r.PublishedAt, &r.WithdrawnAt, &r.PublicationContext)
	if e != nil {
		return r, e
	}
	if organizationannouncement.ValidateRecord(r) != nil {
		return organizationannouncement.Record{}, agentmemory.ErrUnavailable
	}
	return r, nil
}
func announcementError(e error) error {
	if e == nil {
		return nil
	}
	return orgMemoryError(e)
}
func announcementRead(ctx context.Context, tx pgx.Tx, b orgMemoryBinding, id string) (organizationannouncement.Record, error) {
	if !organizationannouncement.Canonical(id) {
		return organizationannouncement.Record{}, agentmemory.ErrInvalid
	}
	r, e := scanAnnouncement(tx.QueryRow(ctx, `SELECT `+announcementColumns+` FROM organization_announcements WHERE id=$1 AND organization_id=$2 AND organization_account_id=$3 FOR UPDATE`, id, b.orgID, b.principalID))
	if errors.Is(e, pgx.ErrNoRows) {
		return r, agentmemory.ErrNotFound
	}
	return r, announcementError(e)
}
func announcementAudit(ctx context.Context, tx pgx.Tx, b orgMemoryBinding, r organizationannouncement.Record, action string) error {
	_, e := auditExec(ctx, tx, `INSERT INTO organization_announcement_audit(organization_id,announcement_id,actor_id,action,revision,created_at) VALUES($1,$2,$3,$4,$5,clock_timestamp())`, b.orgID, r.ID, b.actorID, action, r.Revision)
	return announcementError(e)
}
func announcementCommit(ctx context.Context, s *Store, tx pgx.Tx, b orgMemoryBinding, deadline *time.Time) error {
	if _, e := s.orgMemoryBoundary(ctx, tx, b, deadline); e != nil {
		return e
	}
	return announcementError(tx.Commit(ctx))
}
func (s *Store) ReadOrganizationAnnouncement(ctx context.Context, a agentorganizationmemory.Access, id string) (organizationannouncement.Record, error) {
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := announcementRead(ctx, tx, b, id)
	if e != nil {
		return r, e
	}
	if e = announcementCommit(ctx, s, tx, b, nil); e != nil {
		return organizationannouncement.Record{}, e
	}
	return r, nil
}
func (s *Store) ListOrganizationAnnouncements(ctx context.Context, a agentorganizationmemory.Access) ([]organizationannouncement.Record, error) {
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT `+announcementColumns+` FROM organization_announcements WHERE organization_id=$1 AND organization_account_id=$2 ORDER BY updated_at DESC,id LIMIT 129`, b.orgID, b.principalID)
	if e != nil {
		return nil, agentmemory.ErrUnavailable
	}
	out := []organizationannouncement.Record{}
	for rows.Next() {
		r, e := scanAnnouncement(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, r)
	}
	rows.Close()
	if rows.Err() != nil || len(out) > organizationannouncement.MaxResources {
		return nil, agentmemory.ErrUnavailable
	}
	if e = announcementCommit(ctx, s, tx, b, nil); e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Store) PutOrganizationAnnouncement(ctx context.Context, a agentorganizationmemory.Access, id string, in organizationannouncement.DraftInput) (organizationannouncement.Record, error) {
	if !organizationannouncement.Canonical(id) {
		return organizationannouncement.Record{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	defer tx.Rollback(context.Background())
	now, e := s.orgMemoryBoundary(ctx, tx, b, nil)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	in, e = organizationannouncement.NormalizeDraft(in, now)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	r, e := announcementRead(ctx, tx, b, id)
	missing := errors.Is(e, agentmemory.ErrNotFound)
	if e != nil && !missing {
		return r, e
	}
	if !missing && r.State == organizationannouncement.Draft && r.Title == in.Title && r.Body == in.Body && r.ValidUntil.Equal(in.ValidUntil) && r.Revision == in.ExpectedRevision+1 && r.UpdatedBy == b.actorID {
		if e = announcementCommit(ctx, s, tx, b, &r.ValidUntil); e != nil {
			return organizationannouncement.Record{}, e
		}
		return r, nil
	}
	if missing {
		if in.ExpectedRevision != 0 {
			return r, agentmemory.ErrConflict
		}
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM organization_announcements WHERE organization_id=$1`, b.orgID).Scan(&count); e != nil {
			return r, agentmemory.ErrUnavailable
		}
		if count >= organizationannouncement.MaxResources {
			return r, agentorganizationmemory.ErrLimit
		}
		r, e = scanAnnouncement(tx.QueryRow(ctx, `INSERT INTO organization_announcements(id,organization_id,organization_account_id,revision,title,body,state,valid_until,created_by,updated_by,created_at,updated_at) VALUES($1,$2,$3,1,$4,$5,'DRAFT',$6,$7,$7,$8,$8) RETURNING `+announcementColumns, id, b.orgID, b.principalID, in.Title, in.Body, in.ValidUntil, b.actorID, now))
	} else {
		if r.Revision != in.ExpectedRevision || r.State == organizationannouncement.Withdrawn {
			return r, agentmemory.ErrConflict
		}
		r, e = scanAnnouncement(tx.QueryRow(ctx, `UPDATE organization_announcements SET revision=revision+1,title=$2,body=$3,valid_until=$4,state='DRAFT',published_at=NULL,withdrawn_at=NULL,publication_context=NULL,updated_by=$5,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+announcementColumns, id, in.Title, in.Body, in.ValidUntil, b.actorID))
	}
	if e != nil {
		return organizationannouncement.Record{}, announcementError(e)
	}
	if e = announcementAudit(ctx, tx, b, r, "DRAFT"); e != nil {
		return organizationannouncement.Record{}, e
	}
	if e = announcementCommit(ctx, s, tx, b, &r.ValidUntil); e != nil {
		return organizationannouncement.Record{}, e
	}
	return r, nil
}
func announcementHash(prefix string, data []byte) string {
	sum := sha256.Sum256(append([]byte(prefix+"\x00"), data...))
	return hex.EncodeToString(sum[:])
}
func announcementSourceHash(r organizationannouncement.Record) string {
	raw, _ := json.Marshal(r)
	return announcementHash("birdtie.organization-announcement.source.v1", raw)
}

// Idle expiry is checked freshly but deliberately excluded: Authenticate extends
// it on each legitimate request. All authority versions here are actual native rows.
func announcementAuthority(ctx context.Context, tx pgx.Tx, b orgMemoryBinding) (name, authority string, publicContext []byte, e error) {
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT o.name,`+announcementContextSQL+`,jsonb_build_object('organization',`+announcementContextSQL+`,'person',jsonb_build_object('id',p.id,'status',p.status,'updatedAt',p.updated_at),
 'principal',jsonb_build_object('id',pa.id,'status',pa.status,'updatedAt',pa.updated_at),'membership',jsonb_build_object('id',m.id,'role',m.role,'status',m.status,'updatedAt',m.updated_at),
 'session',jsonb_build_object('id',se.id,'createdAt',se.created_at,'expiresAt',se.expires_at,'method',se.authentication_method),
 'agent',jsonb_build_object('id',ag.id,'status',ag.status,'createdAt',ag.created_at,'profileVersion',ap.profile_version,'profileCreatedAt',ap.created_at,'profileUpdatedAt',ap.updated_at))
 FROM organizations o JOIN accounts pa ON pa.id=o.account_id JOIN accounts p ON p.id=$3 JOIN organization_memberships m ON m.id=$4
 JOIN sessions se ON se.id=$5 JOIN agents ag ON ag.id=$6 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_type='ORGANIZATION' AND ap.owner_id=o.account_id
 WHERE o.id=$1 AND o.account_id=$2 AND o.status='active' AND o.visibility='public' AND o.verification_status='verified'`, b.orgID, b.principalID, b.actorID, b.membershipID, b.sessionID, b.agentID).Scan(&name, &publicContext, &raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", "", nil, agentmemory.ErrForbidden
	}
	if e != nil {
		return "", "", nil, agentmemory.ErrUnavailable
	}
	return name, announcementHash("birdtie.organization-announcement.authority.v1", raw), publicContext, nil
}
func (s *Store) PreviewOrganizationAnnouncement(ctx context.Context, a agentorganizationmemory.Access, id string, expected int64) (organizationannouncement.Preview, error) {
	var out organizationannouncement.Preview
	if expected < 1 {
		return out, agentmemory.ErrInvalid
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	r, e := announcementRead(ctx, tx, b, id)
	if e != nil {
		return out, e
	}
	if r.Revision != expected || r.State != organizationannouncement.Draft {
		return out, agentmemory.ErrConflict
	}
	name, authority, _, e := announcementAuthority(ctx, tx, b)
	if e != nil {
		return out, e
	}
	now, e := s.orgMemoryBoundary(ctx, tx, b, &r.ValidUntil)
	if e != nil {
		return out, e
	}
	deadline := now.Add(organizationannouncement.PreviewTTL)
	if r.ValidUntil.Before(deadline) {
		deadline = r.ValidUntil
	}
	snapshot := announcementSourceHash(r)
	// Bound receipts per resource; expired unused receipts carry no approval.
	if _, e = tx.Exec(ctx, `DELETE FROM organization_announcement_previews WHERE announcement_id=$1 AND consumed_at IS NULL AND expires_at<=clock_timestamp()`, id); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	var count int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM organization_announcement_previews WHERE announcement_id=$1 AND consumed_at IS NULL`, id).Scan(&count); e != nil {
		return out, agentmemory.ErrUnavailable
	}
	if count >= 128 {
		return out, agentorganizationmemory.ErrLimit
	}
	out = organizationannouncement.Preview{Announcement: r, ActingPersonID: b.actorID, OrganizationName: name, SourceSnapshot: snapshot, CreatedAt: now, ExpiresAt: deadline, Consequence: organizationannouncement.PublicationConsequence}
	e = tx.QueryRow(ctx, `INSERT INTO organization_announcement_previews(announcement_id,organization_id,organization_account_id,actor_id,session_id,announcement_revision,source_snapshot,authority_snapshot,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, id, b.orgID, b.principalID, b.actorID, b.sessionID, expected, snapshot, authority, now, deadline).Scan(&out.ID)
	if e != nil {
		return organizationannouncement.Preview{}, agentmemory.ErrUnavailable
	}
	if e = announcementCommit(ctx, s, tx, b, &deadline); e != nil {
		return organizationannouncement.Preview{}, e
	}
	return out, nil
}
func (s *Store) PublishOrganizationAnnouncement(ctx context.Context, a agentorganizationmemory.Access, id string, in organizationannouncement.PublishInput) (organizationannouncement.Record, error) {
	if in.ExpectedRevision < 1 || !organizationannouncement.Canonical(in.PreviewID) {
		return organizationannouncement.Record{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := announcementRead(ctx, tx, b, id)
	if e != nil {
		return r, e
	}
	var snapshot, authority string
	var deadline time.Time
	var consumed *time.Time
	var published *int64
	e = tx.QueryRow(ctx, `SELECT source_snapshot,authority_snapshot,expires_at,consumed_at,published_revision FROM organization_announcement_previews WHERE id=$1 AND announcement_id=$2 AND organization_id=$3 AND organization_account_id=$4 AND actor_id=$5 AND session_id=$6 AND announcement_revision=$7 FOR UPDATE`, in.PreviewID, id, b.orgID, b.principalID, b.actorID, b.sessionID, in.ExpectedRevision).Scan(&snapshot, &authority, &deadline, &consumed, &published)
	if errors.Is(e, pgx.ErrNoRows) {
		return organizationannouncement.Record{}, agentmemory.ErrConflict
	}
	if e != nil {
		return organizationannouncement.Record{}, agentmemory.ErrUnavailable
	}
	_, current, publicationContext, e := announcementAuthority(ctx, tx, b)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	if current != authority {
		return organizationannouncement.Record{}, agentmemory.ErrConflict
	}
	if _, e = s.orgMemoryBoundary(ctx, tx, b, &deadline); e != nil {
		return organizationannouncement.Record{}, e
	}
	if consumed != nil {
		if published == nil || r.State != organizationannouncement.Published || r.Revision != *published {
			return organizationannouncement.Record{}, agentmemory.ErrConflict
		}
		if e = announcementCommit(ctx, s, tx, b, &r.ValidUntil); e != nil {
			return organizationannouncement.Record{}, e
		}
		return r, nil
	}
	if r.Revision != in.ExpectedRevision || r.State != organizationannouncement.Draft || announcementSourceHash(r) != snapshot {
		return organizationannouncement.Record{}, agentmemory.ErrConflict
	}
	r, e = scanAnnouncement(tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE organization_announcements SET state='PUBLISHED',revision=revision+1,published_at=stamp.at,updated_at=stamp.at,updated_by=$2,publication_context=$3::jsonb FROM stamp WHERE id=$1 RETURNING `+announcementColumns, id, b.actorID, publicationContext))
	if e != nil {
		return organizationannouncement.Record{}, announcementError(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE organization_announcement_previews SET consumed_at=clock_timestamp(),published_revision=$2 WHERE id=$1`, in.PreviewID, r.Revision); e != nil {
		return organizationannouncement.Record{}, agentmemory.ErrUnavailable
	}
	if e = announcementAudit(ctx, tx, b, r, "PUBLISH"); e != nil {
		return organizationannouncement.Record{}, e
	}
	if e = announcementCommit(ctx, s, tx, b, &deadline); e != nil {
		return organizationannouncement.Record{}, e
	}
	if !r.ValidUntil.After(r.UpdatedAt) {
		return organizationannouncement.Record{}, agentmemory.ErrInvalid
	}
	return r, nil
}
func (s *Store) WithdrawOrganizationAnnouncement(ctx context.Context, a agentorganizationmemory.Access, id string, expected int64) (organizationannouncement.Record, error) {
	if expected < 1 {
		return organizationannouncement.Record{}, agentmemory.ErrInvalid
	}
	tx, b, e := s.orgMemoryTx(ctx, a)
	if e != nil {
		return organizationannouncement.Record{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := announcementRead(ctx, tx, b, id)
	if e != nil {
		return r, e
	}
	if r.State == organizationannouncement.Withdrawn && r.Revision == expected+1 && r.UpdatedBy == b.actorID {
		if e = announcementCommit(ctx, s, tx, b, nil); e != nil {
			return organizationannouncement.Record{}, e
		}
		return r, nil
	}
	if r.State == organizationannouncement.Withdrawn || r.Revision != expected {
		return organizationannouncement.Record{}, agentmemory.ErrConflict
	}
	r, e = scanAnnouncement(tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE organization_announcements SET state='WITHDRAWN',revision=revision+1,withdrawn_at=stamp.at,updated_at=stamp.at,updated_by=$2 FROM stamp WHERE id=$1 RETURNING `+announcementColumns, id, b.actorID))
	if e != nil {
		return organizationannouncement.Record{}, announcementError(e)
	}
	if e = announcementAudit(ctx, tx, b, r, "WITHDRAW"); e != nil {
		return organizationannouncement.Record{}, e
	}
	if e = announcementCommit(ctx, s, tx, b, nil); e != nil {
		return organizationannouncement.Record{}, e
	}
	return r, nil
}

// The final payload SELECT owns both publication and current session validity.
// Timestamp fingerprints are UTC semantic values, independent of pool TimeZone.
func (s *Store) ReadPublicOrganizationAnnouncement(ctx context.Context, org, id string, a organizationannouncement.PublicAccess) (organizationannouncement.PublicRecord, error) {
	var r organizationannouncement.PublicRecord
	if !organizationannouncement.Canonical(org) || !organizationannouncement.Canonical(id) {
		return r, agentmemory.ErrInvalid
	}
	if e := organizationannouncement.ValidatePublicAccess(a); e != nil {
		return r, e
	}
	var viewer any
	if a.ViewerID != "" {
		viewer = a.ViewerID
	}
	var now time.Time
	r.SchemaVersion = organizationannouncement.Schema
	r.Disclaimer = organizationannouncement.Disclaimer
	e := s.pool.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at),
 auth AS MATERIALIZED(SELECT encode(sha256(convert_to(jsonb_build_object(
 'sessionId',se.id,'accountId',sc.id,'accountType',sc.account_type,'accountStatus',sc.status,
 'accountUpdatedAt',sc.updated_at AT TIME ZONE 'UTC','sessionCreatedAt',se.created_at AT TIME ZONE 'UTC',
 'absoluteExpiresAt',se.expires_at AT TIME ZONE 'UTC','authenticationMethod',se.authentication_method)::text,'UTF8')),'hex') snapshot
 FROM sessions se JOIN accounts sc ON sc.id=se.account_id CROSS JOIN stamp
 WHERE se.token_sha256=$4 AND sc.id=$3::uuid AND sc.account_type='person' AND sc.status='active'
 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at
 AND isfinite(se.created_at) AND isfinite(se.expires_at) AND isfinite(sc.updated_at)
 AND ($6::boolean OR se.authentication_method<>'dev_phone'))
 SELECT n.id,o.id,o.name,n.revision,n.title,n.body,n.audience,n.state,n.published_at,n.valid_until,stamp.at,coalesce(auth.snapshot,'')
 FROM organization_announcements n JOIN organizations o ON o.id=$1 CROSS JOIN stamp LEFT JOIN auth ON true
 WHERE n.id=$2 AND `+announcementPublicPredicate+`
 AND ($3::uuid IS NULL OR (auth.snapshot IS NOT NULL AND ($5='' OR auth.snapshot=$5)))`, org, id, viewer, a.SessionDigest[:], a.ExpectedSessionSnapshot, s.devPhoneEnabled).Scan(&r.ID, &r.OrganizationID, &r.OrganizationName, &r.Revision, &r.Title, &r.Body, &r.Audience, &r.State, &r.PublishedAt, &r.ValidUntil, &now, &r.SessionSnapshot)
	if errors.Is(e, pgx.ErrNoRows) {
		return organizationannouncement.PublicRecord{}, agentmemory.ErrNotFound
	}
	if e != nil || ctx.Err() != nil || organizationannouncement.ValidatePublic(r, org, id, now) != nil {
		return organizationannouncement.PublicRecord{}, agentmemory.ErrUnavailable
	}
	return r, nil
}
