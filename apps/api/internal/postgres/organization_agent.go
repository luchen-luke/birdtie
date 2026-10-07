package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganization"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5"
)

// AuthenticateOrganizationAgent resolves the same actual Session/account as
// AccessStore without the legacy UPDATE's statement-time expiry loophole. Locks
// precede a fresh clock eligibility check; only then is idle expiry refreshed.
func (s *Store) AuthenticateOrganizationAgent(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	if ctx == nil || s == nil || s.pool == nil || digest == ([32]byte{}) {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	if e := ctx.Err(); e != nil {
		return identity.Actor{}, e
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return identity.Actor{}, e
	}
	defer tx.Rollback(context.Background())
	var accountID string
	e = tx.QueryRow(ctx, `SELECT account_id FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&accountID)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	if e != nil {
		return identity.Actor{}, e
	}
	var actor identity.Actor
	// Human Profile writes lock Account before Session. Never rely on the join
	// planner to acquire our exclusive Session lock in that same order.
	e = tx.QueryRow(ctx, `SELECT id,account_type,handle FROM accounts WHERE id=$1 FOR SHARE`, accountID).Scan(&actor.ID, &actor.AccountType, &actor.Handle)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	if e != nil {
		return identity.Actor{}, e
	}
	var sessionID string
	e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 AND account_id=$2::uuid FOR UPDATE`, digest[:], actor.ID).Scan(&sessionID)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	if e != nil {
		return identity.Actor{}, e
	}
	// The guarded UPDATE starts after all actual waits, not before them.
	var refreshed bool
	e = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at), refreshed AS (
	 UPDATE sessions se SET idle_expires_at=LEAST(se.expires_at,stamp.at+interval '30 minutes')
	 FROM accounts a,stamp WHERE se.token_sha256=$1 AND a.id=se.account_id AND a.status='active'
	 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at
	 AND ($2::boolean OR se.authentication_method<>'dev_phone') RETURNING se.id)
	 SELECT EXISTS(SELECT 1 FROM refreshed)`, digest[:], s.devPhoneEnabled).Scan(&refreshed)
	if e != nil {
		return identity.Actor{}, e
	}
	if !refreshed {
		return identity.Actor{}, identity.ErrUnauthorized
	}
	if e = ctx.Err(); e != nil {
		return identity.Actor{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return identity.Actor{}, e
	}
	if e = ctx.Err(); e != nil {
		return identity.Actor{}, e
	}
	return actor, nil
}

// ValidateOrganizationAgentSession is a narrow final read guard. The local
// AuthenticateOrganizationAgent resolves identity. This guard never refreshes idle
// expiry: it acquires real session/account SHARE locks, then checks the actual
// PG clock in a fresh RC statement. It grants no organization role or purpose.
func (s *Store) ValidateOrganizationAgentSession(ctx context.Context, digest [32]byte, initial identity.Actor) error {
	if ctx == nil || s == nil || s.pool == nil || digest == ([32]byte{}) || initial.ID == "" || initial.AccountType == "" {
		return identity.ErrUnauthorized
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if _, e := actorref.ParsePrincipal(strings.ToUpper(initial.AccountType), initial.ID); e != nil {
		return identity.ErrUnauthorized
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	var owner, kind string
	e = tx.QueryRow(ctx, `SELECT se.account_id,a.account_type FROM sessions se JOIN accounts a ON a.id=se.account_id
	 WHERE se.token_sha256=$1 AND a.id=$2::uuid AND a.account_type=$3 FOR SHARE OF se,a`, digest[:], initial.ID, initial.AccountType).Scan(&owner, &kind)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.ErrUnauthorized
	}
	if e != nil {
		return e
	}
	if owner != initial.ID || kind != initial.AccountType {
		return identity.ErrUnauthorized
	}
	var valid bool
	e = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED (SELECT clock_timestamp() AS at)
	 SELECT EXISTS(SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id CROSS JOIN stamp
	 WHERE se.token_sha256=$1 AND a.id=$2::uuid AND a.account_type=$3 AND a.status='active'
	 AND se.revoked_at IS NULL AND se.expires_at>stamp.at AND se.idle_expires_at>stamp.at
	 AND ($4::boolean OR se.authentication_method<>'dev_phone'))`, digest[:], initial.ID, initial.AccountType, s.devPhoneEnabled).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return identity.ErrUnauthorized
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return ctx.Err()
}

// One bounded payload statement owns organization and public source visibility.
// No initial GetPublicProfile/FAQ snapshot is reused after a lock or revocation.
// RC/READ ONLY explicitly overrides a pool's default repeatable-read setting.
const organizationKnowledgeSQL = `WITH stamp AS MATERIALIZED (SELECT clock_timestamp() AS at),
 org AS MATERIALIZED (
  SELECT o.* FROM organizations o JOIN accounts principal ON principal.id=o.account_id
    AND principal.account_type='organization' AND principal.status='active'
  WHERE o.id=$1::uuid AND o.status='active' AND o.visibility='public'
    AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL
      AND ((b.blocker_account_id=$2 AND b.blocked_account_id=o.account_id)
       OR(b.blocker_account_id=o.account_id AND b.blocked_account_id=$2)))
 ), speaker AS MATERIALIZED (
  SELECT ag.id,ap.profile_version FROM org o JOIN agents ag ON ag.principal_account_id=o.account_id
   AND ag.agent_type='organization' AND ag.status='active'
  JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=o.account_id AND ap.owner_type='ORGANIZATION'
 ), public_faq AS MATERIALIZED (
  SELECT f.id,f.organization_id,f.question,f.answer,f.published,f.created_at,f.updated_at
  FROM organization_faqs f JOIN org o ON o.id=f.organization_id
  WHERE f.published AND o.verification_status='verified' AND EXISTS(SELECT 1 FROM speaker)
  ORDER BY f.updated_at DESC,f.id DESC LIMIT 100
 ), public_activity AS MATERIALIZED (
  SELECT a.id,a.title,a.starts_at,a.time_zone,a.expires_at,c.expires_at AS city_expiry,
   CASE WHEN p.id IS NOT NULL THEN p.name ELSE '' END AS place_name,
   p.expires_at AS place_expiry
  FROM activities a JOIN org o ON a.organization_id=o.id
  JOIN activity_organizers ao ON ao.activity_id=a.id AND ao.organization_id=o.id
   AND ao.person_account_id IS NULL AND ao.community_id IS NULL AND ao.business_id IS NULL
  JOIN cities c ON c.id=a.city_id AND c.publication_status='published'
  CROSS JOIN stamp
  LEFT JOIN places p ON p.id=a.place_id AND p.publication_status='published'
    AND (p.expires_at IS NULL OR p.expires_at>stamp.at)
  WHERE o.verification_status='verified' AND EXISTS(SELECT 1 FROM speaker)
    AND a.visibility='public' AND a.publication_status='published' AND a.cancelled_at IS NULL
    AND a.starts_at>stamp.at AND a.ends_at>a.starts_at
    AND (a.expires_at IS NULL OR a.expires_at>stamp.at)
    AND (c.expires_at IS NULL OR c.expires_at>stamp.at)
    AND birdtie_activity_visible_to(a.id,$2::uuid)
    AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND a.host_account_id IS NOT NULL
      AND ((b.blocker_account_id=$2 AND b.blocked_account_id=a.host_account_id)
       OR(b.blocker_account_id=a.host_account_id AND b.blocked_account_id=$2)))
  ORDER BY a.starts_at,a.id LIMIT 20
 ) SELECT o.id,o.account_id,coalesce((SELECT id::text FROM speaker),''),
 coalesce((SELECT profile_version FROM speaker),0),o.organization_type,o.name,
 CASE WHEN o.verification_status='verified' AND EXISTS(SELECT 1 FROM speaker) THEN o.description ELSE '' END,
 CASE WHEN o.verification_status='verified' AND EXISTS(SELECT 1 FROM speaker) THEN o.official_links ELSE '[]'::jsonb END,
 o.verification_status,coalesce((SELECT jsonb_agg(jsonb_build_object('id',f.id,'organizationId',f.organization_id,
 'question',f.question,'answer',f.answer,'published',f.published,'createdAt',f.created_at,'updatedAt',f.updated_at)
 ORDER BY f.updated_at DESC,f.id DESC) FROM public_faq f),'[]'::jsonb),
 coalesce((SELECT jsonb_agg(to_jsonb(a) ORDER BY a.starts_at,a.id) FROM public_activity a),'[]'::jsonb),stamp.at
 FROM org o CROSS JOIN stamp`

type organizationKnowledgeActivity struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	StartsAt    time.Time  `json:"starts_at"`
	TimeZone    string     `json:"time_zone"`
	PlaceName   string     `json:"place_name"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CityExpiry  *time.Time `json:"city_expiry"`
	PlaceExpiry *time.Time `json:"place_expiry"`
}

func (s *Store) answerCurrentOrganization(ctx context.Context, organizationID, viewerID, query string) (organization.AgentAnswer, error) {
	if ctx == nil || s == nil || s.pool == nil || !agentorganization.ValidQuery(query) {
		return organization.AgentAnswer{}, agentorganization.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return organization.AgentAnswer{}, err
	}
	if _, err := actorref.ParsePrincipal("ORGANIZATION", organizationID); err != nil {
		return organization.AgentAnswer{}, organization.ErrNotFound
	}
	var viewer any
	if viewerID != "" {
		if _, err := actorref.ParsePrincipal("PERSON", viewerID); err != nil {
			return organization.AgentAnswer{}, organization.ErrNotFound
		}
		viewer = viewerID
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return organization.AgentAnswer{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); err != nil {
		return organization.AgentAnswer{}, err
	}
	p := agentorganization.Projection{Actor: actorref.ActorRef{Type: actorref.Organization}, Principal: actorref.PrincipalRef{Type: actorref.Organization}, FAQs: []organization.FAQ{}}
	var faqs, activities []byte
	var observed time.Time
	err = tx.QueryRow(ctx, organizationKnowledgeSQL, organizationID, viewer).Scan(&p.Actor.ID, &p.Principal.ID, &p.AgentID, &p.MetadataVersion, &p.Profile.OrganizationType, &p.Profile.Name, &p.Profile.Description, &p.Profile.OfficialLinks, &p.Profile.VerificationStatus, &faqs, &activities, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return organization.AgentAnswer{}, organization.ErrNotFound
	}
	if err != nil {
		return organization.AgentAnswer{}, err
	}
	p.Profile.ID = p.Actor.ID
	p.Profile.AgentAvailable = p.AgentID != "" && p.MetadataVersion > 0
	if p.Profile.OfficialLinks == nil {
		p.Profile.OfficialLinks = []string{}
	}
	p.Profile.UpcomingActivities = []foundation.Activity{}
	if err = json.Unmarshal(faqs, &p.FAQs); err != nil {
		return organization.AgentAnswer{}, err
	}
	var public []organizationKnowledgeActivity
	if err = json.Unmarshal(activities, &public); err != nil {
		return organization.AgentAnswer{}, err
	}
	// Capture no location/body. The original rule response uses only these public
	// names, stable entity references and a locale-formatted activity schedule.
	for _, a := range public {
		zone, e := time.LoadLocation(a.TimeZone)
		if e != nil {
			return organization.AgentAnswer{}, agentorganization.ErrInvalid
		}
		for _, deadline := range []*time.Time{&a.StartsAt, a.ExpiresAt, a.CityExpiry, a.PlaceExpiry} {
			if deadline != nil && !deadline.After(observed) {
				return organization.AgentAnswer{}, organization.ErrNotFound
			}
		}
		id := p.Actor.ID
		p.Profile.UpcomingActivities = append(p.Profile.UpcomingActivities, foundation.Activity{ID: a.ID, Title: a.Title, StartsAt: a.StartsAt, TimeZone: a.TimeZone, Schedule: formatActivitySchedule(a.StartsAt.In(zone)), PlaceName: a.PlaceName, OrganizationID: &id, Organizer: foundation.ActivityOrganizer{Type: "ORGANIZATION", ID: id}, Visibility: "public", Status: "upcoming"})
	}
	answer, err := agentorganization.Answer(query, p)
	if err != nil {
		return organization.AgentAnswer{}, err
	}
	if err = ctx.Err(); err != nil {
		return organization.AgentAnswer{}, err
	}
	// Only the cited activity's deadline can invalidate this answer. Unselected
	// supply must not expire a valid FAQ response. This is a final TIME check;
	// source ACL's linearization point remains the coherent payload statement.
	if len(answer.Sources) == 1 && answer.Sources[0].Type == "activity" {
		var current time.Time
		if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&current); err != nil {
			return organization.AgentAnswer{}, err
		}
		for _, a := range public {
			if a.ID == answer.Sources[0].ID {
				for _, deadline := range []*time.Time{&a.StartsAt, a.ExpiresAt, a.CityExpiry, a.PlaceExpiry} {
					if deadline != nil && !deadline.After(current) {
						return organization.AgentAnswer{}, organization.ErrNotFound
					}
				}
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return organization.AgentAnswer{}, err
	}
	return answer, nil
}
