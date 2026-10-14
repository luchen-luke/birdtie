package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ identity.CurrentProfileStore = (*Store)(nil)

type currentProfileProof struct {
	store    *Store
	access   identity.ProfileAccess
	target   string
	source   [32]byte
	deadline time.Time
	profile  identity.Profile
}

func validProfileAccess(access identity.ProfileAccess) bool {
	if access.SessionDigest == ([32]byte{}) {
		return access.Actor.ID == "" && access.Actor.AccountType == "" && access.Actor.Handle == nil
	}
	principal, err := actorref.ParsePrincipal(access.Actor.AccountType, access.Actor.ID)
	return err == nil && principal.Type != actorref.Community && principal.ID == access.Actor.ID &&
		strings.ToLower(string(principal.Type)) == access.Actor.AccountType
}

// The proof holds only opaque native epochs and the already permitted ordinary
// Profile. It is bound to this Store, the original session and the original
// deadline. Authenticate extending idle expiry cannot extend this response.
func (s *Store) ReadProfileCurrent(ctx context.Context, access identity.ProfileAccess, target string) (identity.Profile, error) {
	profile, source, observed, deadline, err := s.readCurrentProfile(ctx, access, target)
	if err != nil {
		return identity.Profile{}, err
	}
	if !deadline.After(observed) {
		return identity.Profile{}, identity.ErrConflict
	}
	profile.ResponseProof = &currentProfileProof{store: s, access: access, target: target, source: source, deadline: deadline, profile: profile}
	return profile, nil
}

func (s *Store) ValidateProfileCurrent(ctx context.Context, access identity.ProfileAccess, target string, profile identity.Profile) error {
	proof, ok := profile.ResponseProof.(*currentProfileProof)
	if !ok || proof == nil || proof.store != s || proof.target != target ||
		proof.access.Actor.ID != access.Actor.ID || proof.access.Actor.AccountType != access.Actor.AccountType ||
		proof.access.SessionDigest != access.SessionDigest || !sameCurrentProfile(proof.profile, profile) {
		return identity.ErrProfileUnavailable
	}
	current, source, observed, _, err := s.readCurrentProfile(ctx, access, target)
	if err != nil {
		return err
	}
	if !observed.Before(proof.deadline) || source != proof.source || !sameCurrentProfile(current, proof.profile) {
		return identity.ErrConflict
	}
	return nil
}

func sameCurrentProfile(a, b identity.Profile) bool {
	return a.AccountID == b.AccountID && a.DisplayName == b.DisplayName && a.Bio == b.Bio && a.Visibility == b.Visibility && reflect.DeepEqual(a.Avatar, b.Avatar)
}

func (s *Store) readCurrentProfile(ctx context.Context, access identity.ProfileAccess, target string) (identity.Profile, [32]byte, time.Time, time.Time, error) {
	var profile identity.Profile
	var empty [32]byte
	var observed, deadline time.Time
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return profile, empty, observed, deadline, identity.ErrProfileUnavailable
	}
	if !validProfileAccess(access) {
		return profile, empty, observed, deadline, identity.ErrUnauthorized
	}
	principal, err := actorref.ParsePrincipal("person", target)
	if err != nil || principal.ID != target {
		return profile, empty, observed, deadline, identity.ErrNotFound
	}
	var viewer any
	if access.Actor.ID != "" {
		viewer = access.Actor.ID
	}
	var source []byte
	var sessionValid bool
	var sessionID string
	var generation int64
	err = s.pool.QueryRow(ctx, currentProfileSQL, target, viewer, access.SessionDigest[:], access.Actor.AccountType, s.devPhoneEnabled).Scan(
		&sessionValid, &observed, &deadline, &profile.AccountID, &profile.DisplayName, &profile.Bio, &profile.Visibility, &profile.Avatar, &source, &sessionID, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, empty, observed, deadline, identity.ErrNotFound
	}
	if err != nil || ctx.Err() != nil {
		return identity.Profile{}, empty, observed, deadline, identity.ErrProfileUnavailable
	}
	if !sessionValid {
		return identity.Profile{}, empty, observed, deadline, identity.ErrUnauthorized
	}
	if profile.AccountID == "" {
		return identity.Profile{}, empty, observed, deadline, identity.ErrNotFound
	}
	if !identity.ProfileVisibilityValid(profile.Visibility) {
		return identity.Profile{}, empty, observed, deadline, identity.ErrProfileUnavailable
	}
	// Source materialization itself can take time. Check a fresh native clock
	// afterwards, retaining the snapshot's exact Session ID/generation/deadline.
	// A normal idle refresh cannot extend the captured response's validity.
	err = s.pool.QueryRow(ctx, `WITH wall AS MATERIALIZED(SELECT clock_timestamp() n)
	 SELECT wall.n,($1::uuid IS NULL OR EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id
	 WHERE s.id::text=$2 AND s.token_sha256=$3 AND s.authorization_generation=$4
	 AND a.id=$1 AND a.account_type=$5 AND a.status='active' AND s.revoked_at IS NULL
	 AND s.expires_at>wall.n AND s.idle_expires_at>wall.n AND ($6::boolean OR s.authentication_method<>'dev_phone'))) FROM wall`,
		viewer, sessionID, access.SessionDigest[:], generation, access.Actor.AccountType, s.devPhoneEnabled).Scan(&observed, &sessionValid)
	if err != nil || ctx.Err() != nil {
		return identity.Profile{}, empty, observed, deadline, identity.ErrProfileUnavailable
	}
	if !sessionValid {
		return identity.Profile{}, empty, observed, deadline, identity.ErrUnauthorized
	}
	if !observed.Before(deadline) {
		return identity.Profile{}, empty, observed, deadline, identity.ErrConflict
	}
	return profile, sha256.Sum256(source), observed, deadline, nil
}

// One native snapshot binds source and payload. Both reads preserve the existing
// resource ACL AND Agent field restriction. No denied value is read for later
// filtering. Epochs never cross HTTP and are not Memory/model authorization.
const currentProfileSQL = `WITH wall AS MATERIALIZED(SELECT clock_timestamp() n),
 session_source AS MATERIALIZED(SELECT s.id,s.authorization_generation,s.created_at,s.expires_at,s.idle_expires_at,s.authentication_method
  FROM sessions s JOIN accounts a ON a.id=s.account_id,wall
  WHERE $2::uuid IS NOT NULL AND s.account_id=$2 AND a.account_type=$4 AND a.status='active'
  AND s.token_sha256=$3 AND s.revoked_at IS NULL AND s.expires_at>wall.n AND s.idle_expires_at>wall.n
  AND ($5::boolean OR s.authentication_method<>'dev_phone')),
 binding AS MATERIALIZED(SELECT ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM session_source)) valid),
 permitted AS MATERIALIZED(SELECT p.account_id,
  CASE WHEN birdtie_agent_profile_field_allowed(p.account_id,$2::uuid,'displayName') THEN p.display_name ELSE '' END display_name,
  CASE WHEN birdtie_agent_profile_field_allowed(p.account_id,$2::uuid,'bio') THEN p.bio ELSE '' END bio,p.visibility,
  birdtie_person_avatar_metadata(p.account_id,$2::uuid) avatar
  FROM user_profiles p,binding WHERE binding.valid AND p.account_id=$1 AND birdtie_profile_visible_to(p.account_id,$2::uuid)),
 grants AS MATERIALIZED(SELECT g.id,g.xmin::text epoch,g.expires_at FROM consent_grants g
  WHERE g.owner_account_id=$1 AND g.recipient_account_id=$2 AND g.resource_type='profile'
  AND g.resource_id=$1::uuid::text AND g.purpose='profile_view' AND g.actions @> ARRAY['read']::text[] AND g.revoked_at IS NULL),
 related_communities AS MATERIALIZED(SELECT c.id,c.xmin::text epoch,c.expires_at FROM communities c
  WHERE EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id IN($1,$2))),
 epochs AS MATERIALIZED(SELECT jsonb_build_array(
  COALESCE((SELECT jsonb_agg(jsonb_build_array(a.id,a.xmin::text) ORDER BY a.id) FROM accounts a WHERE a.id IN($1,$2)),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(p.account_id,p.xmin::text) ORDER BY p.account_id) FROM user_profiles p WHERE p.account_id=$1),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(a.account_id,a.version,a.xmin::text)) FROM person_avatars a WHERE a.account_id=$1),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(a.id,a.xmin::text) ORDER BY a.id) FROM agents a WHERE a.principal_account_id=$1),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(p.agent_id,p.xmin::text) ORDER BY p.agent_id) FROM agent_profiles p WHERE p.owner_id=$1 AND p.owner_type='PERSON'),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(v.agent_id,v.xmin::text) ORDER BY v.agent_id) FROM agent_profile_field_visibility v WHERE v.owner_id=$1),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(f.id,f.xmin::text) ORDER BY f.id) FROM follows f WHERE f.person_account_id=$1 AND f.follower_account_id=$2),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(t.id,t.request_id,t.xmin::text) ORDER BY t.id) FROM person_ties t
   WHERE t.person_a_account_id=LEAST($1::uuid,$2::uuid) AND t.person_b_account_id=GREATEST($1::uuid,$2::uuid) AND $2::uuid IS NOT NULL),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(r.id,r.xmin::text) ORDER BY r.id) FROM connection_requests r
   WHERE (r.sender_account_id=$1 AND r.recipient_account_id=$2) OR (r.sender_account_id=$2 AND r.recipient_account_id=$1)),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(c.owner_account_id,c.person_account_id,c.xmin::text,c.native_revision) ORDER BY c.owner_account_id,c.person_account_id)
   FROM person_close_connections c WHERE c.owner_account_id=$1 AND c.person_account_id=$2),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(g.id,g.epoch) ORDER BY g.id) FROM grants g),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(b.blocker_account_id,b.blocked_account_id,b.xmin::text) ORDER BY b.blocker_account_id,b.blocked_account_id)
   FROM account_blocks b WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=$2) OR (b.blocker_account_id=$2 AND b.blocked_account_id=$1)),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(a.id,a.xmin::text) ORDER BY a.id) FROM audit_events a
   WHERE a.resource_type='account' AND a.action IN('block','unblock') AND a.purpose='personal_safety'
   AND ((a.actor_account_id=$1 AND a.resource_id=$2::uuid::text) OR (a.actor_account_id=$2 AND a.resource_id=$1::uuid::text))),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(m.id,m.xmin::text) ORDER BY m.id) FROM community_memberships m WHERE m.user_account_id IN($1,$2)),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(c.id,c.epoch) ORDER BY c.id) FROM related_communities c),'[]'::jsonb),
  COALESCE((SELECT jsonb_agg(jsonb_build_array(s.id,s.authorization_generation,s.created_at,s.expires_at,s.authentication_method) ORDER BY s.id) FROM session_source s),'[]'::jsonb)
 )::text AS value)
 SELECT binding.valid,wall.n,LEAST(wall.n+interval '30 seconds',
  (SELECT min(LEAST(s.expires_at,s.idle_expires_at)) FROM session_source s),
  (SELECT min(g.expires_at) FROM grants g WHERE g.expires_at>wall.n),
  (SELECT min(c.expires_at) FROM related_communities c WHERE c.expires_at>wall.n)),
  COALESCE(p.account_id::text,''),COALESCE(p.display_name,''),COALESCE(p.bio,''),COALESCE(p.visibility,''),p.avatar,convert_to(epochs.value,'UTF8'),
  COALESCE((SELECT id::text FROM session_source),''),COALESCE((SELECT authorization_generation FROM session_source),0)
 FROM wall,binding,epochs LEFT JOIN permitted p ON true`
