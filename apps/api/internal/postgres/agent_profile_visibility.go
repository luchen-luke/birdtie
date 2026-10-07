package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
)

// ReadOwnAgentProfileVisibility returns explicit field audience settings for a
// human owner review. The absent policy is the canonical conservative default;
// this read never persists settings or grants a model access to any field.
func (s *Store) ReadOwnAgentProfileVisibility(ctx context.Context, access agentprofile.PrivateAccess) (agentprofile.VisibilityRecord, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	metadata, err := lockAgentPrivateMetadata(ctx, tx, binding, false)
	if err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	rules, configured, err := readAgentFieldRules(ctx, tx, binding)
	if err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	record, err := agentprofile.NewVisibilityRecord(metadata, rules, configured)
	if err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	return record, nil
}

// ReplaceOwnAgentProfileVisibility is an ordinary owner's CAS-protected
// complete replacement, not a consent/Memory/analysis/runtime permission.
func (s *Store) ReplaceOwnAgentProfileVisibility(ctx context.Context, access agentprofile.PrivateAccess, input agentprofile.ReplaceVisibilityInput) (agentprofile.VisibilityRecord, error) {
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	normalized, err := agentprofile.NormalizeReplaceVisibilityInput(input)
	if err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	binding, err := lockOwnAgentPrivateBinding(ctx, tx, access, s.devPhoneEnabled)
	if err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	metadata, err := lockAgentPrivateMetadata(ctx, tx, binding, true)
	if err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	if metadata.ProfileVersion != normalized.ExpectedVersion || metadata.ProfileVersion == math.MaxInt64 {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrConflict
	}
	if err = lockVisibilityCommunities(ctx, tx, binding.accountID, visibilityCommunityIDs(normalized.Rules), true); err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	err = tx.QueryRow(ctx, `UPDATE agent_profiles SET profile_version=profile_version+1
		WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' AND profile_version=$3
		RETURNING `+agentProfileColumns, binding.agentID, binding.accountID, normalized.ExpectedVersion).Scan(
		&metadata.AgentID, &metadata.OwnerType, &metadata.OwnerID,
		&metadata.ProfileVersion, &metadata.CreatedAt, &metadata.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrConflict
	}
	if err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	configured := !agentprofile.FieldRulesDefault(normalized.Rules)
	if configured {
		encoded, encodeErr := json.Marshal(normalized.Rules)
		if encodeErr != nil {
			return agentprofile.VisibilityRecord{}, agentprofile.ErrInvalid
		}
		_, err = tx.Exec(ctx, `INSERT INTO agent_profile_field_visibility
			(agent_id,owner_id,owner_type,rules,written_profile_version)
			VALUES($1,$2,'PERSON',$3::jsonb,$4)
			ON CONFLICT(agent_id) DO UPDATE SET rules=EXCLUDED.rules,
				written_profile_version=EXCLUDED.written_profile_version,updated_at=clock_timestamp()`,
			binding.agentID, binding.accountID, encoded, metadata.ProfileVersion)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM agent_profile_field_visibility
			WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`, binding.agentID, binding.accountID)
	}
	if err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	if err = insertDomainAudit(ctx, tx, binding.accountID, "replace", "agent_field_visibility", binding.agentID, "human_field_audience_edit", nil); err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	// Time may have advanced while the metadata or Community rows were locked.
	// Final eligibility is checked again; no prior membership is treated as proof.
	if err = validateVisibilityCommunities(ctx, tx, binding.accountID, visibilityCommunityIDs(normalized.Rules)); err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	if err = recheckAgentPrivateSession(ctx, tx, binding); err != nil {
		return agentprofile.VisibilityRecord{}, err
	}
	record, err := agentprofile.NewVisibilityRecord(metadata, normalized.Rules, configured)
	if err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return agentprofile.VisibilityRecord{}, agentprofile.ErrUnavailable
	}
	return record, nil
}

func readAgentFieldRules(ctx context.Context, tx pgx.Tx, binding agentPrivateBinding) (agentprofile.FieldRules, bool, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT rules FROM agent_profile_field_visibility
		WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' FOR SHARE`, binding.agentID, binding.accountID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.DefaultFieldRules(), false, nil
	}
	if err != nil {
		return nil, false, agentprofile.ErrUnavailable
	}
	rules, err := agentprofile.DecodeFieldRules(raw)
	if err != nil {
		return nil, false, agentprofile.ErrUnavailable
	}
	return rules, true, nil
}

func visibilityCommunityIDs(rules agentprofile.FieldRules) []string {
	set := make(map[string]bool)
	for _, rule := range rules {
		if rule.Visibility == agentprofile.VisibilityCommunity {
			for _, id := range rule.CommunityIDs {
				set[id] = true
			}
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Communities always precede memberships in the lock order, matching existing
// owner transfers/archives. requireEligible distinguishes an explicit owner
// write (all references must be current) from a projection of an old policy.
func lockVisibilityCommunities(ctx context.Context, tx pgx.Tx, ownerID string, ids []string, requireEligible bool) error {
	if len(ids) == 0 {
		return nil
	}
	if err := lockVisibilityRows(ctx, tx, `SELECT id FROM communities
		WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, ids); err != nil {
		return err
	}
	if err := lockVisibilityRows(ctx, tx, `SELECT id FROM community_memberships
		WHERE community_id=ANY($1::uuid[]) AND user_account_id=$2 ORDER BY community_id,user_account_id FOR SHARE`, ids, ownerID); err != nil {
		return err
	}
	if requireEligible {
		return validateVisibilityCommunities(ctx, tx, ownerID, ids)
	}
	return nil
}

func validateVisibilityCommunities(ctx context.Context, tx pgx.Tx, ownerID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	var eligible int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM communities c
		JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$2 AND m.status='active'
		WHERE c.id=ANY($1::uuid[]) AND c.lifecycle_status='active' AND c.publication_status='published'
			AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())`, ids, ownerID).Scan(&eligible)
	if err != nil {
		return agentprofile.ErrUnavailable
	}
	if eligible != len(ids) {
		return agentprofile.ErrForbidden
	}
	return nil
}

func lockVisibilityRows(ctx context.Context, tx pgx.Tx, query string, args ...any) error {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return agentprofile.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return agentprofile.ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return agentprofile.ErrUnavailable
	}
	return nil
}

// ReadAgentProfileFields exposes a current human-readable field projection.
// A nonzero digest must remain a valid session; invalid sessions never degrade
// to anonymous. The existing whole-profile source ACL and each explicit field
// rule are conjunctive, and neither constitutes a model-context permission.
func (s *Store) ReadAgentProfileFields(ctx context.Context, viewerDigest [32]byte, targetID string) (agentprofile.ProjectedRecord, error) {
	target, err := actorref.ParsePrincipal(string(actorref.Person), targetID)
	if err != nil || target.ID == "00000000-0000-0000-0000-000000000000" {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	defer tx.Rollback(ctx)
	viewer, err := lockVisibilityViewerSession(ctx, tx, viewerDigest, s.devPhoneEnabled)
	if err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	accountIDs := []string{target.ID}
	if viewer.accountID != "" && viewer.accountID != target.ID {
		accountIDs = append(accountIDs, viewer.accountID)
	}
	// NO KEY UPDATE conflicts with Block's target SHARE but allows the audit
	// foreign-key KEY SHARE used by revocation and membership mutations.
	if err = lockVisibilityRows(ctx, tx, `SELECT id FROM accounts WHERE id=ANY($1::uuid[])
		ORDER BY id FOR NO KEY UPDATE`, accountIDs); err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	var agentID string
	err = tx.QueryRow(ctx, `SELECT ag.id FROM agents ag JOIN accounts a ON a.id=ag.principal_account_id
		WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
			AND ag.agent_type='personal' AND ag.status='active' FOR SHARE OF ag`, target.ID).Scan(&agentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrNotFound
	}
	if err != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	binding := agentPrivateBinding{accountID: target.ID, agentID: agentID}
	if _, err = lockAgentPrivateMetadata(ctx, tx, binding, false); err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	var ownerID string
	err = tx.QueryRow(ctx, `SELECT account_id FROM user_profiles WHERE account_id=$1 FOR SHARE`, target.ID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrNotFound
	}
	if err != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	if err = lockVisibilityRows(ctx, tx, `SELECT agent_id FROM agent_private_profiles WHERE agent_id=$1 FOR SHARE`, agentID); err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	rules, _, err := readAgentFieldRules(ctx, tx, binding)
	if err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	if viewer.accountID != "" && viewer.accountID != target.ID {
		if err = lockVisibilityProjectionRelations(ctx, tx, target.ID, viewer.accountID, rules); err != nil {
			return agentprofile.ProjectedRecord{}, err
		}
	}
	// Repeat the entire SQL qualification after all source/relationship locks.
	// No denied private value is read into application memory for filtering.
	if _, err = queryAgentFieldProjection(ctx, tx, target.ID, viewer.accountID); err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	if viewer.sessionID != "" {
		if err = recheckAgentPrivateSession(ctx, tx, viewer); err != nil {
			return agentprofile.ProjectedRecord{}, err
		}
	}
	projection, err := queryAgentFieldProjection(ctx, tx, target.ID, viewer.accountID)
	if err != nil {
		return agentprofile.ProjectedRecord{}, err
	}
	if viewer.sessionID != "" {
		if err = recheckAgentPrivateSession(ctx, tx, viewer); err != nil {
			return agentprofile.ProjectedRecord{}, err
		}
	}
	if err = agentprofile.ValidateProjectedRecord(projection); err != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	return projection, nil
}

func lockVisibilityViewerSession(ctx context.Context, tx pgx.Tx, digest [32]byte, devPhoneEnabled bool) (agentPrivateBinding, error) {
	if digest == ([32]byte{}) {
		return agentPrivateBinding{}, nil
	}
	var viewer agentPrivateBinding
	err := tx.QueryRow(ctx, `SELECT s.id,a.id FROM sessions s JOIN accounts a ON a.id=s.account_id
		WHERE s.token_sha256=$1 AND a.status='active' AND s.revoked_at IS NULL
			AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp()
			AND ($2::boolean OR s.authentication_method<>'dev_phone') FOR SHARE OF s`, digest[:], devPhoneEnabled).Scan(&viewer.sessionID, &viewer.accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return agentPrivateBinding{}, agentprofile.ErrForbidden
	}
	if err != nil {
		return agentPrivateBinding{}, agentprofile.ErrUnavailable
	}
	return viewer, nil
}

func lockVisibilityProjectionRelations(ctx context.Context, tx pgx.Tx, targetID, viewerID string, rules agentprofile.FieldRules) error {
	if err := lockVisibilityRows(ctx, tx, `SELECT id FROM consent_grants
		WHERE owner_account_id=$1 AND recipient_account_id=$2 AND resource_type='profile'
			AND resource_id=$1::uuid::text AND purpose='profile_view' ORDER BY id FOR SHARE`, targetID, viewerID); err != nil {
		return err
	}
	connections := false
	for _, rule := range rules {
		connections = connections || rule.Visibility == agentprofile.VisibilityConnections
	}
	if connections {
		// Accepted friend request proof precedes the durable Tie lock, matching
		// DecideRequest's request-before-Tie order. A conversation is not a Tie.
		if err := lockVisibilityRows(ctx, tx, `SELECT id FROM connection_requests
			WHERE (sender_account_id=$1 AND recipient_account_id=$2) OR
				(sender_account_id=$2 AND recipient_account_id=$1) ORDER BY id FOR SHARE`, targetID, viewerID); err != nil {
			return err
		}
		if err := lockVisibilityRows(ctx, tx, `SELECT id FROM person_ties
			WHERE person_a_account_id=LEAST($1::uuid,$2::uuid)
				AND person_b_account_id=GREATEST($1::uuid,$2::uuid) ORDER BY id FOR SHARE`, targetID, viewerID); err != nil {
			return err
		}
	}
	ids := visibilityCommunityIDs(rules)
	if len(ids) != 0 {
		if err := lockVisibilityRows(ctx, tx, `SELECT id FROM communities
			WHERE id=ANY($1::uuid[]) ORDER BY id FOR SHARE`, ids); err != nil {
			return err
		}
		if err := lockVisibilityRows(ctx, tx, `SELECT id FROM community_memberships
			WHERE community_id=ANY($1::uuid[]) AND user_account_id=ANY($2::uuid[])
			ORDER BY community_id,user_account_id FOR SHARE`, ids, []string{targetID, viewerID}); err != nil {
			return err
		}
	}
	return nil
}

// Fixed source expressions are declared here, not accepted from a caller or
// model. SQL only returns nonempty values satisfying both the old source ACL
// and the current additional field rule; rules and audience IDs stay internal.
func queryAgentFieldProjection(ctx context.Context, tx pgx.Tx, targetID, viewerID string) (agentprofile.ProjectedRecord, error) {
	var viewer any
	if viewerID != "" {
		viewer = viewerID
	}
	rows, err := tx.Query(ctx, `SELECT candidate.key,candidate.value FROM user_profiles p
		JOIN accounts owner ON owner.id=p.account_id AND owner.account_type='person' AND owner.status='active'
		JOIN agents ag ON ag.principal_account_id=owner.id AND ag.agent_type='personal' AND ag.status='active'
		LEFT JOIN agent_private_profiles private ON private.agent_id=ag.id AND private.owner_id=owner.id AND private.owner_type='PERSON'
		CROSS JOIN LATERAL (VALUES
			('displayName',to_jsonb(p.display_name)),('bio',to_jsonb(p.bio)),
			('personalPreferences',private.fields->'personalPreferences'),('socialPreferences',private.fields->'socialPreferences'),
			('availability',private.fields->'availability'),('preferredActivityTypes',private.fields->'preferredActivityTypes'),
			('travelPreferences',private.fields->'travelPreferences'),('interactionPreferences',private.fields->'interactionPreferences'),
			('privateCityHistory',private.fields->'privateCityHistory'),('languagePreferences',private.fields->'languagePreferences'),
			('agentNotes',private.fields->'agentNotes')) candidate(key,value)
		WHERE p.account_id=$1 AND ($2::uuid IS NULL OR EXISTS(SELECT 1 FROM accounts viewer WHERE viewer.id=$2 AND viewer.status='active'))
			AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND
				((b.blocker_account_id=$1 AND b.blocked_account_id=$2) OR (b.blocker_account_id=$2 AND b.blocked_account_id=$1)))
			AND (p.visibility='public' OR p.account_id=$2 OR EXISTS(SELECT 1 FROM consent_grants g
				WHERE g.owner_account_id=p.account_id AND g.recipient_account_id=$2
					AND g.resource_type='profile' AND g.resource_id=p.account_id::text AND g.purpose='profile_view'
					AND g.actions @> ARRAY['read']::text[] AND g.revoked_at IS NULL
					AND (g.expires_at IS NULL OR g.expires_at>clock_timestamp())))
			AND birdtie_agent_profile_field_allowed(p.account_id,$2::uuid,candidate.key)
			AND candidate.value IS NOT NULL AND candidate.value NOT IN ('null'::jsonb,'""'::jsonb,'[]'::jsonb)
		ORDER BY candidate.key`, targetID, viewer)
	if err != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	defer rows.Close()
	projection := agentprofile.ProjectedRecord{AccountID: targetID, Fields: make(map[agentprofile.FieldKey]json.RawMessage)}
	for rows.Next() {
		var key string
		var value []byte
		if err = rows.Scan(&key, &value); err != nil {
			return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
		}
		projection.Fields[agentprofile.FieldKey(key)] = append(json.RawMessage{}, value...)
	}
	if rows.Err() != nil {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	if len(projection.Fields) == 0 {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrNotFound
	}
	// Reject corrupt/unsupported persisted projections, without logging data.
	if strings.TrimSpace(projection.AccountID) == "" {
		return agentprofile.ProjectedRecord{}, agentprofile.ErrUnavailable
	}
	return projection, nil
}
