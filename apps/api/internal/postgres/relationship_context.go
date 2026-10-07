package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/relationshipcontext"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/jackc/pgx/v5"
)

func (s *Store) OwnRelationshipConsent(ctx context.Context, actor string) (relationshipcontext.Consent, error) {
	var out relationshipcontext.Consent
	err := s.pool.QueryRow(ctx, `SELECT coalesce(c.enabled,false) FROM accounts a
 LEFT JOIN person_agent_relationship_consent c ON c.account_id=a.id
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`, actor).Scan(&out.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, relationshipcontext.ErrNotFound
	}
	return out, err
}
func (s *Store) SetRelationshipConsent(ctx context.Context, actor string, in relationshipcontext.Consent) (relationshipcontext.Consent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	var currentActor string
	if err = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR NO KEY UPDATE`, actor).Scan(&currentActor); errors.Is(err, pgx.ErrNoRows) {
		return in, relationshipcontext.ErrNotFound
	} else if err != nil {
		return in, err
	}
	row, err := tx.Exec(ctx, `INSERT INTO person_agent_relationship_consent(account_id,enabled)
 SELECT id,$2 FROM accounts WHERE id=$1 AND account_type='person' AND status='active'
 ON CONFLICT(account_id) DO UPDATE SET enabled=EXCLUDED.enabled,updated_at=now()
 WHERE person_agent_relationship_consent.enabled IS DISTINCT FROM EXCLUDED.enabled`, actor, in.Enabled)
	if err != nil {
		return in, err
	}
	if row.RowsAffected() == 0 {
		return in, tx.Commit(ctx)
	}
	_, err = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)
 VALUES($1::uuid,'update','agent_relationship_consent',$1::uuid::text,'allowed',CASE WHEN $2 THEN 'explicit_enable' ELSE 'explicit_revoke' END)`, actor, in.Enabled)
	if err != nil {
		return in, err
	}
	return in, tx.Commit(ctx)
}

// Every read resolves current ownership and consent. Only metadata is selected;
// no message body, embedding, private Moment or location enters this tool.
func (s *Store) OwnRelationshipContext(ctx context.Context, actor string) (relationshipcontext.Context, error) {
	out := relationshipcontext.Context{WindowDays: 30, Peers: []relationshipcontext.Peer{}}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var active bool
	err = tx.QueryRow(ctx, `SELECT coalesce(c.enabled,false),EXISTS(SELECT 1 FROM agents ag
 WHERE ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active')
 FROM accounts a LEFT JOIN person_agent_relationship_consent c ON c.account_id=a.id
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`, actor).Scan(&out.Enabled, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, relationshipcontext.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if !active {
		return out, relationshipcontext.ErrAgentUnavailable
	}
	if !out.Enabled {
		return out, tx.Commit(ctx)
	}
	// The projection statement is the read's authorization point: consent,
	// exact current FriendTie, names and shared disclosure share one snapshot.
	rows, err := tx.Query(ctx, `WITH current_viewer AS (
 SELECT coalesce(c.enabled,false) enabled,EXISTS(SELECT 1 FROM agents ag
 WHERE ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active') agent_active
 FROM accounts a LEFT JOIN person_agent_relationship_consent c ON c.account_id=a.id
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active'), ties AS (
 SELECT t.id,CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END peer
 FROM person_ties t JOIN connection_requests fr ON fr.id=t.request_id AND fr.scope='friend' AND fr.state='accepted'
 AND LEAST(fr.sender_account_id,fr.recipient_account_id)=t.person_a_account_id
 AND GREATEST(fr.sender_account_id,fr.recipient_account_id)=t.person_b_account_id
 WHERE t.status='active' AND $1 IN(t.person_a_account_id,t.person_b_account_id))
 SELECT viewer.enabled,viewer.agent_active,peer.id,peer.account_id,peer.name,
 coalesce(peer.messages,0),coalesce(peer.days,0),coalesce(peer.shared,'[]'::jsonb)
 FROM current_viewer viewer LEFT JOIN LATERAL (
 SELECT t.id,a.id,coalesce(CASE WHEN birdtie_agent_profile_field_allowed(a.id,$1::uuid,'displayName')
 THEN p.display_name END,'好友') name,coalesce(x.messages,0) messages,coalesce(x.days,0) days,
 coalesce(shared.items,'[]'::jsonb) shared
 FROM ties t JOIN accounts a ON a.id=t.peer AND a.account_type='person' AND a.status='active'
 LEFT JOIN user_profiles p ON p.account_id=a.id
 LEFT JOIN LATERAL (SELECT count(*) messages,count(DISTINCT (m.created_at AT TIME ZONE 'UTC')::date) days
 FROM conversations cv JOIN connection_requests cr ON cr.id=cv.request_id AND cr.scope IN('conversation','friend') AND cr.state='accepted'
 JOIN conversation_messages m ON m.conversation_id=cv.id AND m.sender_account_id=$1 AND m.speaker_kind='human'
 WHERE ((cv.member_a_account_id=$1 AND cv.member_b_account_id=a.id) OR
 (cv.member_b_account_id=$1 AND cv.member_a_account_id=a.id))
 AND m.created_at>=statement_timestamp()-interval '30 days' AND m.created_at<=statement_timestamp()) x ON true
 LEFT JOIN LATERAL (SELECT jsonb_agg(jsonb_build_object('id',item.id,'title',item.title)
 ORDER BY item.starts_at DESC,item.id) items FROM (
 SELECT activity.id,activity.title,activity.starts_at FROM activity_participations v
 JOIN activity_participations target ON target.activity_id=v.activity_id AND target.participant_account_id=a.id AND target.status='going'
 JOIN person_social_disclosure vd ON vd.account_id=v.participant_account_id AND vd.shared_activities
 JOIN person_social_disclosure td ON td.account_id=target.participant_account_id AND td.shared_activities
 JOIN user_profiles public_profile ON public_profile.account_id=a.id AND public_profile.visibility='public'
 JOIN activities activity ON activity.id=v.activity_id AND activity.visibility='public' AND activity.publication_status='published'
 AND activity.cancelled_at IS NULL AND (activity.expires_at IS NULL OR activity.expires_at>statement_timestamp())
 JOIN cities city ON city.id=activity.city_id AND city.publication_status='published'
 WHERE v.participant_account_id=$1 AND v.status='going'
 AND activity.starts_at>=statement_timestamp()-interval '30 days' AND activity.starts_at<=statement_timestamp()+interval '30 days'
 AND birdtie_activity_visible_to(activity.id,$1) AND birdtie_activity_visible_to(activity.id,a.id)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
 (b.blocker_account_id=activity.host_account_id AND b.blocked_account_id IN($1,a.id)) OR
 (b.blocked_account_id=activity.host_account_id AND b.blocker_account_id IN($1,a.id)))
 ORDER BY activity.starts_at DESC,activity.id LIMIT 11) item) shared ON true
 WHERE viewer.enabled AND viewer.agent_active AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
 (b.blocker_account_id=$1 AND b.blocked_account_id=a.id) OR (b.blocker_account_id=a.id AND b.blocked_account_id=$1))
 ORDER BY coalesce(x.days,0) DESC,coalesce(x.messages,0) DESC,t.id LIMIT 51
 ) peer(id,account_id,name,messages,days,shared) ON true
 ORDER BY coalesce(peer.days,0) DESC,coalesce(peer.messages,0) DESC,peer.id`, actor)
	if err != nil {
		return out, err
	}
	foundViewer := false
	for rows.Next() {
		foundViewer = true
		peer := relationshipcontext.Peer{InteractionCategories: []string{}, SharedActivities: []socialcontext.Reference{}}
		var tieID, accountID, displayName *string
		var shared []byte
		if err = rows.Scan(&out.Enabled, &active, &tieID, &accountID, &displayName, &peer.SentMessages, &peer.ActiveDays, &shared); err != nil {
			rows.Close()
			return out, err
		}
		if tieID == nil {
			continue
		}
		if accountID == nil || displayName == nil || json.Unmarshal(shared, &peer.SharedActivities) != nil {
			rows.Close()
			return relationshipcontext.Context{}, relationshipcontext.ErrNotFound
		}
		peer.TieID, peer.AccountID, peer.DisplayName = *tieID, *accountID, *displayName
		peer.Frequency = relationshipcontext.Frequency(peer.ActiveDays)
		if peer.SentMessages > 0 {
			peer.InteractionCategories = append(peer.InteractionCategories, "direct_chat")
		}
		if len(peer.SharedActivities) > 10 {
			peer.ActivitiesTruncated = true
			peer.SharedActivities = peer.SharedActivities[:10]
		}
		if len(peer.SharedActivities) > 0 {
			peer.InteractionCategories = append(peer.InteractionCategories, "shared_public_activity")
		}
		out.Peers = append(out.Peers, peer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if !foundViewer {
		return relationshipcontext.Context{}, relationshipcontext.ErrNotFound
	}
	if !active {
		return relationshipcontext.Context{}, relationshipcontext.ErrAgentUnavailable
	}
	if len(out.Peers) > 50 {
		out.Truncated = true
		out.Peers = out.Peers[:50]
	}
	return out, tx.Commit(ctx)
}
