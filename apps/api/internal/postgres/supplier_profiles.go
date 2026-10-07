package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/supplierprofile"
	"github.com/jackc/pgx/v5"
)

var _ supplierprofile.Store = (*Store)(nil)

// Internal authority snapshot: no body, personnel or hash is publicly returned.
// xmin binds removal/restoration or same-version replacement as well as CAS.
const businessPublicationSourceSQL = `SELECT p.version,p.facts,p.reviewed_at,p.valid_until,m.user_account_id,
 encode(sha256(convert_to(jsonb_build_array(to_jsonb(b),b.xmin::text,to_jsonb(principal),principal.xmin::text,
 to_jsonb(m),m.xmin::text,to_jsonb(owner_person),owner_person.xmin::text,
 to_jsonb(claim),claim.xmin::text,to_jsonb(p),p.xmin::text,
 to_jsonb(submitter),submitter.xmin::text,to_jsonb(sm),sm.xmin::text,
 to_jsonb(reviewer),reviewer.xmin::text,to_jsonb(g),g.xmin::text)::text,'UTF8')),'hex') AS source_snapshot
 FROM business_console_profiles p
 JOIN businesses b ON b.id=p.business_id AND b.status='active' AND b.claim_status='verified'
 JOIN accounts principal ON principal.id=b.account_id AND principal.account_type='business' AND principal.status='active'
 JOIN business_claim_controls claim ON claim.business_id=b.id AND claim.state='verified'
 JOIN business_memberships m ON m.business_id=b.id AND m.role='owner' AND m.status='active'
 JOIN accounts owner_person ON owner_person.id=m.user_account_id AND owner_person.account_type='person' AND owner_person.status='active'
 JOIN business_memberships sm ON sm.business_id=b.id AND sm.user_account_id=p.submitted_by AND sm.status='active' AND sm.role IN ('owner','admin')
 JOIN accounts submitter ON submitter.id=sm.user_account_id AND submitter.account_type='person' AND submitter.status='active'
 JOIN accounts reviewer ON reviewer.id=p.reviewed_by AND reviewer.account_type='person' AND reviewer.status='active'
 JOIN business_review_grants g ON g.business_id=b.id AND g.reviewer_account_id=reviewer.id AND g.state='active'
 AND 'profile'=ANY(g.permissions) AND g.valid_from<=clock_timestamp() AND g.valid_until>clock_timestamp()
 WHERE p.business_id=$1 AND ($2::uuid IS NULL OR m.user_account_id=$2) AND p.state='verified'
 AND p.valid_until>clock_timestamp() AND p.reviewed_at IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM business_memberships self WHERE self.business_id=b.id AND self.user_account_id=reviewer.id AND self.status='active')`

func publicationPreview(ctx context.Context, tx pgx.Tx, business, owner string) (*supplierprofile.PublicationPreview, error) {
	var p supplierprofile.PublicationPreview
	var raw []byte
	var ownerID string
	e := tx.QueryRow(ctx, businessPublicationSourceSQL, business, owner).Scan(&p.ProfileVersion, &raw, &p.ReviewedAt, &p.ValidUntil, &ownerID, &p.SourceSnapshot)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, businessconsole.ErrUnavailable
	}
	var facts businessconsole.ProfileFacts
	if json.Unmarshal(raw, &facts) != nil || businessconsole.ValidateProfile(facts) != nil {
		return nil, businessconsole.ErrUnavailable
	}
	p.Name = facts.Name
	p.ReviewedAt = p.ReviewedAt.UTC()
	p.ValidUntil = p.ValidUntil.UTC()
	p.Description = facts.Description
	p.OfficialLinks = append([]string{}, facts.OfficialLinks...)
	return &p, nil
}

const publicationPermissionColumns = `version,profile_version,state,valid_until`

func readPublicationPermission(ctx context.Context, tx pgx.Tx, business string) (supplierprofile.Permission, error) {
	p := supplierprofile.Permission{State: "unpublished"}
	e := tx.QueryRow(ctx, `SELECT `+publicationPermissionColumns+` FROM business_public_profile_permissions WHERE business_id=$1`, business).Scan(&p.Version, &p.ProfileVersion, &p.State, &p.ValidUntil)
	if p.ValidUntil != nil {
		utc := p.ValidUntil.UTC()
		p.ValidUntil = &utc
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return p, nil
	}
	return p, businessErrorOrNil(e)
}
func (s *Store) ReadBusinessPublicPermission(ctx context.Context, a businessconsole.Access) (supplierprofile.PermissionView, error) {
	var out supplierprofile.PermissionView
	tx, b, e := s.businessBegin(ctx, a, "owner")
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	out.Permission, e = readPublicationPermission(ctx, tx, a.BusinessID)
	if e != nil {
		return out, e
	}
	out.Preview, e = publicationPreview(ctx, tx, a.BusinessID, a.ActingPersonID)
	if e != nil {
		return out, e
	}
	out.DisclosureStatus = "unpublished"
	if out.Permission.Version > 0 {
		out.DisclosureStatus = out.Permission.State
		if out.Permission.State == "active" {
			var current bool
			e = tx.QueryRow(ctx, `SELECT approved_by=$2 AND source_snapshot=$3 AND valid_until>clock_timestamp() FROM business_public_profile_permissions WHERE business_id=$1`, a.BusinessID, a.ActingPersonID, func() string {
				if out.Preview != nil {
					return out.Preview.SourceSnapshot
				}
				return ""
			}()).Scan(&current)
			if e != nil {
				return out, businessconsole.ErrUnavailable
			}
			if !current {
				out.DisclosureStatus = "source_changed"
			}
		}
	}
	if e = s.finishSupplierPublication(ctx, tx, b, nil, out.Preview); e != nil {
		return supplierprofile.PermissionView{}, e
	}
	return out, nil
}

// Session and membership are locked by businessBegin. Finish their checks before
// the last current source projection, then commit without another resource wait.
func (s *Store) finishSupplierPublication(ctx context.Context, tx pgx.Tx, b businessBinding, deadline *time.Time, expected *supplierprofile.PublicationPreview) error {
	if _, e := s.businessBoundary(ctx, tx, b, deadline); e != nil {
		return e
	}
	if expected != nil {
		current, e := publicationPreview(ctx, tx, b.access.BusinessID, b.access.ActingPersonID)
		if e != nil {
			return e
		}
		if current == nil || current.SourceSnapshot != expected.SourceSnapshot {
			return businessconsole.ErrConflict
		}
	}
	return businessErrorOrNil(tx.Commit(ctx))
}
func (s *Store) ChangeBusinessPublicPermission(ctx context.Context, a businessconsole.Access, in supplierprofile.PermissionInput) (supplierprofile.Permission, error) {
	var out supplierprofile.Permission
	tx, b, e := s.businessBegin(ctx, a, "owner")
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	producer, e := prepareBusinessPublicUpdate(ctx, tx)
	if e != nil { return out, businessError(e) }
	now, e := s.businessBoundary(ctx, tx, b, nil)
	if e != nil {
		return out, e
	}
	deadline, e := supplierprofile.ValidatePermission(in, now)
	if e != nil {
		return out, e
	}
	var oldOwner, oldSnapshot string
	e = tx.QueryRow(ctx, `SELECT `+publicationPermissionColumns+`,approved_by,source_snapshot FROM business_public_profile_permissions WHERE business_id=$1 FOR UPDATE`, a.BusinessID).Scan(&out.Version, &out.ProfileVersion, &out.State, &out.ValidUntil, &oldOwner, &oldSnapshot)
	if out.ValidUntil != nil {
		utc := out.ValidUntil.UTC()
		out.ValidUntil = &utc
	}
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return out, businessError(e)
	}
	if errors.Is(e, pgx.ErrNoRows) {
		out = supplierprofile.Permission{State: "unpublished"}
	}
	var preview *supplierprofile.PublicationPreview
	if in.Action == "publish" {
		preview, e = publicationPreview(ctx, tx, a.BusinessID, a.ActingPersonID)
		if e != nil {
			return out, e
		}
		if preview == nil || preview.ProfileVersion != in.ExpectedProfileVersion || preview.SourceSnapshot != in.SourceSnapshot || deadline.After(preview.ValidUntil) {
			return out, businessconsole.ErrConflict
		}
	}
	state := "active"
	if in.Action == "revoke" {
		state = "revoked"
	}
	// Exact committed intent only: repeat does not mint a new audit/revision.
	if out.Version == in.ExpectedVersion+1 && out.State == state && oldOwner == a.ActingPersonID &&
		(in.Action == "revoke" || (out.ProfileVersion == in.ExpectedProfileVersion && oldSnapshot == in.SourceSnapshot && out.ValidUntil != nil && out.ValidUntil.Equal(*deadline))) {
		return out, s.finishSupplierPublication(ctx, tx, b, deadline, preview)
	}
	if out.Version != in.ExpectedVersion {
		return out, businessconsole.ErrConflict
	}
	if in.Action == "publish" {
		e = tx.QueryRow(ctx, `INSERT INTO business_public_profile_permissions(business_id,version,profile_version,approved_by,source_snapshot,state,valid_until)
 VALUES($1,1,$2,$3,$4,'active',$5) ON CONFLICT(business_id) DO UPDATE SET version=business_public_profile_permissions.version+1,profile_version=EXCLUDED.profile_version,approved_by=EXCLUDED.approved_by,source_snapshot=EXCLUDED.source_snapshot,state='active',valid_until=EXCLUDED.valid_until,updated_at=clock_timestamp() RETURNING `+publicationPermissionColumns, a.BusinessID, in.ExpectedProfileVersion, a.ActingPersonID, in.SourceSnapshot, deadline).Scan(&out.Version, &out.ProfileVersion, &out.State, &out.ValidUntil)
	} else {
		e = tx.QueryRow(ctx, `UPDATE business_public_profile_permissions SET version=version+1,state='revoked',approved_by=$2,updated_at=clock_timestamp() WHERE business_id=$1 RETURNING `+publicationPermissionColumns, a.BusinessID, a.ActingPersonID).Scan(&out.Version, &out.ProfileVersion, &out.State, &out.ValidUntil)
	}
	if e != nil {
		return out, businessError(e)
	}
	if out.ValidUntil != nil {
		utc := out.ValidUntil.UTC()
		out.ValidUntil = &utc
	}
	if producer && in.Action == "publish" {
		var auditID string
		e = auditQueryRow(ctx, tx, `INSERT INTO business_public_profile_audit(business_id,actor_account_id,action,permission_version,profile_version,notification_source) VALUES($1,$2,$3,$4,$5,true) RETURNING id`, a.BusinessID, a.ActingPersonID, in.Action, out.Version, out.ProfileVersion).Scan(&auditID)
		if e == nil { e = routeBusinessPublicUpdate(ctx, tx, auditID) }
	} else {
	_, e = auditExec(ctx, tx, `INSERT INTO business_public_profile_audit(business_id,actor_account_id,action,permission_version,profile_version) VALUES($1,$2,$3,$4,$5)`, a.BusinessID, a.ActingPersonID, in.Action, out.Version, out.ProfileVersion)
	}
	if e != nil {
		return out, businessError(e)
	}
	if in.Action == "publish" {
		current, ce := publicationPreview(ctx, tx, a.BusinessID, a.ActingPersonID)
		if ce != nil {
			return out, ce
		}
		if current == nil || current.SourceSnapshot != preview.SourceSnapshot {
			return out, businessconsole.ErrConflict
		}
	}
	return out, s.finishSupplierPublication(ctx, tx, b, deadline, preview)
}

// Explicit aliases for the existing canonical activityColumns. Internal flat
// JSON is reshaped into the same foundation Activity, never raw activities.*.
const supplierActivityAliases = `("id","cityId","placeId","placeName","hostLabel","title","summary","description","categoryCode","capacity","participantCount","priceMinor","currency","eligibility","languageCode","officialUrl","startsAt","endsAt","timeZone","cancelledAt","latitude","longitude","sourceLabel","sourceReference","sourceMaintainer","sourceUpdatedAt","sourceVerifiedAt","sourceExpiresAt","organizationId","visibility","organizerType","organizerId","organizerName","organizerAvatarUrl","modality","physicalPlaceStatus","venuePlaceId")`

func supplierActivitiesJSON(organizerColumn string) string {
	// Only caller-owned closed literals, never external SQL identifiers.
	if organizerColumn != "ao.business_id" && organizerColumn != "ao.organization_id" {
		panic("invalid supplier column")
	}
	return `coalesce((SELECT jsonb_agg(to_jsonb(public_activity)) FROM (SELECT ` + activityColumns + publishedActivityFrom + `
 AND ` + organizerColumn + `=$1 AND a.visibility='public' AND a.cancelled_at IS NULL AND a.starts_at>clock_timestamp()
 AND (c.expires_at IS NULL OR (isfinite(c.expires_at) AND c.expires_at>clock_timestamp()))
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) ORDER BY a.starts_at,a.id LIMIT 20) public_activity` + supplierActivityAliases + `),'[]'::jsonb)`
}

func decodeSupplierActivities(raw []byte, now time.Time) ([]foundation.Activity, error) {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return nil, businessconsole.ErrUnavailable
	}
	out := make([]foundation.Activity, 0, len(rows))
	for _, row := range rows {
		encoded, e := json.Marshal(row)
		if e != nil {
			return nil, e
		}
		var a foundation.Activity
		if e = json.Unmarshal(encoded, &a); e != nil {
			return nil, e
		}
		read := func(name string, v any) error { return json.Unmarshal(row[name], v) }
		if read("sourceLabel", &a.Source.Label) != nil || read("sourceReference", &a.Source.Reference) != nil || read("sourceMaintainer", &a.Source.Maintainer) != nil || read("sourceUpdatedAt", &a.Source.UpdatedAt) != nil || read("sourceVerifiedAt", &a.Source.VerifiedAt) != nil || read("sourceExpiresAt", &a.Source.ExpiresAt) != nil || read("organizerType", &a.Organizer.Type) != nil || read("organizerId", &a.Organizer.ID) != nil || read("organizerName", &a.Organizer.Name) != nil || read("organizerAvatarUrl", &a.Organizer.AvatarURL) != nil {
			return nil, businessconsole.ErrUnavailable
		}
		var lat, lng *float64
		if read("latitude", &lat) != nil || read("longitude", &lng) != nil {
			return nil, businessconsole.ErrUnavailable
		}
		if lat != nil && lng != nil {
			a.Location = &foundation.Location{CoordinateSystem: "wgs84", Precision: "point", Latitude: lat, Longitude: lng}
		}
		a.Source.SetFreshness(now)
		if a.Organizer.Type == "ORGANIZATION" {
			organizationID := a.Organizer.ID
			a.OrganizationID = &organizationID
		}
		a.Status = "upcoming"
		if loc, e := time.LoadLocation(a.TimeZone); e == nil {
			a.Schedule = formatActivitySchedule(a.StartsAt.In(loc))
			a.EndSchedule = formatActivitySchedule(a.EndsAt.In(loc))
		}
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ReadPublicBusiness(ctx context.Context, id, viewer string) (supplierprofile.Business, error) {
	out := supplierprofile.Business{ID: id, VerificationStatus: "verified", ProfileStatus: "unpublished", OfficialLinks: []string{}, UpcomingActivities: []foundation.Activity{}}
	if ctx == nil || ctx.Err() != nil || !businessconsole.ValidID(id) || (viewer != "" && !businessconsole.ValidID(viewer)) {
		return out, businessconsole.ErrInvalid
	}
	var viewerArg any
	if viewer != "" {
		viewerArg = viewer
	}
	var facts, activities []byte
	var now time.Time
	// Public fields and Activity ACL/supply are obtained from ONE PG statement.
	query := `SELECT b.name,CASE WHEN public_permission.version IS NOT NULL THEN 'verified' ELSE 'unpublished' END,
 CASE WHEN public_permission.version IS NOT NULL THEN source.facts ELSE NULL END,
 CASE WHEN public_permission.version IS NOT NULL THEN source.version ELSE 0 END,
 CASE WHEN public_permission.version IS NOT NULL THEN source.reviewed_at ELSE NULL END,
 public_permission.valid_until,` + supplierActivitiesJSON("ao.business_id") + `,clock_timestamp()
 FROM businesses b JOIN accounts principal ON principal.id=b.account_id AND principal.account_type='business' AND principal.status='active'
 LEFT JOIN LATERAL (` + businessPublicationSourceSQL + `) source ON true
 LEFT JOIN business_public_profile_permissions public_permission ON public_permission.business_id=b.id AND public_permission.state='active'
 AND public_permission.profile_version=source.version AND public_permission.source_snapshot=source.source_snapshot
 AND public_permission.approved_by=source.user_account_id AND public_permission.valid_until>clock_timestamp()
 WHERE b.id=$1 AND b.status='active' AND b.claim_status='verified'
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE $2::uuid IS NOT NULL AND ((block.blocker_account_id=$2 AND block.blocked_account_id=b.account_id) OR (block.blocked_account_id=$2 AND block.blocker_account_id=b.account_id)))`
	// Source helper's $2 filters its owner. Public reads must not substitute the
	// viewer for owner; a separate fixed NULL expression preserves public ACL.
	query = replacePublicationOwnerWithNull(query)
	// The authority hash contains PostgreSQL JSON timestamps. Use the same
	// explicit UTC transaction as the owner preview, independently of DB zone.
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, businessconsole.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return out, businessconsole.ErrUnavailable
	}
	e = tx.QueryRow(ctx, query, id, viewerArg).Scan(&out.Name, &out.ProfileStatus, &facts, &out.ProfileVersion, &out.ReviewedAt, &out.ValidUntil, &activities, &now)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, businessconsole.ErrNotFound
	}
	if e != nil {
		return out, businessconsole.ErrUnavailable
	}
	if out.ProfileStatus == "verified" {
		if out.ReviewedAt != nil {
			utc := out.ReviewedAt.UTC()
			out.ReviewedAt = &utc
		}
		if out.ValidUntil != nil {
			utc := out.ValidUntil.UTC()
			out.ValidUntil = &utc
		}
		var f businessconsole.ProfileFacts
		if json.Unmarshal(facts, &f) != nil || businessconsole.ValidateProfile(f) != nil {
			return out, businessconsole.ErrUnavailable
		}
		out.Name = f.Name
		out.Description = f.Description
		out.OfficialLinks = append([]string{}, f.OfficialLinks...)
	}
	out.UpcomingActivities, e = decodeSupplierActivities(activities, now)
	if e != nil {
		return out, businessconsole.ErrUnavailable
	}
	return out, businessErrorOrNil(tx.Commit(ctx))
}

func replacePublicationOwnerWithNull(query string) string {
	return strings.ReplaceAll(query, "($2::uuid IS NULL OR m.user_account_id=$2)", "true")
}

func (s *Store) readCoherentPublicOrganization(ctx context.Context, id, viewer string) (organization.PublicProfile, error) {
	var out organization.PublicProfile
	if ctx == nil || ctx.Err() != nil {
		return out, organization.ErrNotFound
	}
	var viewerArg any
	if viewer != "" {
		viewerArg = viewer
	}
	var activities []byte
	var now time.Time
	e := s.pool.QueryRow(ctx, `SELECT org.id,org.organization_type,org.name,org.description,org.official_links,org.verification_status,
 EXISTS(SELECT 1 FROM agents ag WHERE ag.principal_account_id=org.account_id AND ag.agent_type='organization' AND ag.status='active'),
 `+supplierActivitiesJSON("ao.organization_id")+`,clock_timestamp()
 FROM organizations org JOIN accounts principal ON principal.id=org.account_id AND principal.account_type='organization' AND principal.status='active'
 WHERE org.id=$1 AND org.status='active' AND org.visibility='public'
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE $2::uuid IS NOT NULL AND ((block.blocker_account_id=$2 AND block.blocked_account_id=org.account_id) OR (block.blocked_account_id=$2 AND block.blocker_account_id=org.account_id)))`, id, viewerArg).Scan(&out.ID, &out.OrganizationType, &out.Name, &out.Description, &out.OfficialLinks, &out.VerificationStatus, &out.AgentAvailable, &activities, &now)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, organization.ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if out.OfficialLinks == nil {
		out.OfficialLinks = []string{}
	}
	out.UpcomingActivities, e = decodeSupplierActivities(activities, now)
	return out, e
}
