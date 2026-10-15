package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ connection.ContactPagedStore = (*Store)(nil)

// These projections preserve the original row shapes, friend source proof,
// bilateral Block and profile-field policy. LIMIT is applied in SQL after ACLs.
const contactTiesBaseSQL = `SELECT t.id,other.id,
        CASE WHEN other.status='active' AND birdtie_profile_visible_to(other.id,$1::uuid) AND birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
            THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
            ELSE 'Birdtie 成员' END,t.created_at,birdtie_person_avatar_metadata(other.id,$1::uuid)
        FROM person_ties t
        JOIN connection_requests source_request ON source_request.id=t.request_id
            AND source_request.scope='friend' AND source_request.state='accepted'
            AND LEAST(source_request.sender_account_id,source_request.recipient_account_id)=t.person_a_account_id
            AND GREATEST(source_request.sender_account_id,source_request.recipient_account_id)=t.person_b_account_id
        JOIN accounts own ON own.id=$1 AND own.account_type='person' AND own.status='active'
        JOIN accounts other ON other.id=CASE WHEN t.person_a_account_id=$1
            THEN t.person_b_account_id ELSE t.person_a_account_id END
            AND other.account_type='person' AND (other.status='active' OR source_request.legacy_source_id IS NOT NULL)
        LEFT JOIN user_profiles p ON p.account_id=other.id
        WHERE t.status='active' AND (t.person_a_account_id=$1 OR t.person_b_account_id=$1)
          AND NOT EXISTS (SELECT 1 FROM account_blocks b
              WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=other.id) OR
                    (b.blocker_account_id=other.id AND b.blocked_account_id=$1))`
const contactTiesPageSQL = contactTiesBaseSQL + `
 AND t.created_at<=$2::timestamptz
 AND ($4::uuid IS NULL OR (t.created_at,t.id)<($3::timestamptz,$4::uuid))
 ORDER BY t.created_at DESC,t.id DESC LIMIT $5`
const contactFollowsBaseSQL = `SELECT f.id,
		CASE WHEN f.person_account_id IS NOT NULL THEN 'PERSON'
			WHEN f.organization_id IS NOT NULL THEN 'ORGANIZATION'
			WHEN f.community_id IS NOT NULL THEN 'COMMUNITY' ELSE 'BUSINESS' END,
		COALESCE(f.person_account_id,f.organization_id,f.community_id,f.business_id)::text,
		CASE WHEN f.person_account_id IS NOT NULL THEN
            CASE WHEN birdtie_profile_visible_to(person.id,$1::uuid) AND birdtie_agent_profile_field_allowed(person.id,$1::uuid,'displayName')
                THEN COALESCE(NULLIF(p.display_name,''),NULLIF(person.handle,''),'Birdtie 成员')
                ELSE 'Birdtie 成员' END
            ELSE COALESCE(o.name,c.name,b.name,'Birdtie 成员') END,
		f.created_at,birdtie_person_avatar_metadata(f.person_account_id,$1::uuid),
		CASE WHEN c.id IS NOT NULL THEN birdtie_community_avatar_metadata(c.id,$1::uuid) END,
		jsonb_build_array(f.xmin::text,c.xmin::text,
		 (SELECT creator.xmin::text FROM accounts creator WHERE creator.id=c.owner_account_id),
		 (SELECT ca.xmin::text FROM community_avatars ca WHERE ca.community_id=c.id),
		 (SELECT coalesce(jsonb_agg(jsonb_build_array(m.user_account_id,m.xmin::text) ORDER BY m.user_account_id),'[]'::jsonb) FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id IN($1,c.owner_account_id)),
		 (SELECT coalesce(jsonb_agg(jsonb_build_array(block.blocker_account_id,block.blocked_account_id,block.xmin::text) ORDER BY block.blocker_account_id,block.blocked_account_id),'[]'::jsonb) FROM account_blocks block WHERE c.owner_account_id IS NOT NULL AND ((block.blocker_account_id=$1 AND block.blocked_account_id=c.owner_account_id) OR(block.blocker_account_id=c.owner_account_id AND block.blocked_account_id=$1))),
		 coalesce((SELECT max(safety.id) FROM audit_events safety WHERE safety.resource_type='account' AND safety.decision='allowed' AND safety.purpose='personal_safety' AND safety.action IN('block','unblock') AND ((safety.actor_account_id=$1 AND safety.resource_id=c.owner_account_id::text) OR(safety.actor_account_id=c.owner_account_id AND safety.resource_id=$1::uuid::text))),0))::text
		FROM follows f
		LEFT JOIN accounts person ON person.id=f.person_account_id AND person.status='active'
		LEFT JOIN user_profiles p ON p.account_id=person.id AND birdtie_profile_visible_to(person.id,$1::uuid)
		LEFT JOIN organizations o ON o.id=f.organization_id AND o.status='active'
		LEFT JOIN communities c ON c.id=f.community_id AND c.lifecycle_status='active'
			AND c.publication_status='published' AND c.visibility='public'
		LEFT JOIN businesses b ON b.id=f.business_id AND b.status='active' AND b.claim_status='verified'
		WHERE f.follower_account_id=$1 AND
		 ((f.legacy_source_id IS NOT NULL AND f.person_account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE (block.blocker_account_id=$1 AND block.blocked_account_id=f.person_account_id) OR(block.blocker_account_id=f.person_account_id AND block.blocked_account_id=$1))) OR
 (p.account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE
			(block.blocker_account_id=$1 AND block.blocked_account_id=person.id) OR
			(block.blocker_account_id=person.id AND block.blocked_account_id=$1))) OR
		  (o.id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=o.account_id AND a.status='active')) OR
		  c.id IS NOT NULL OR
		  (b.id IS NOT NULL AND EXISTS(SELECT 1 FROM accounts a WHERE a.id=b.account_id AND a.status='active')))`
const contactFollowsPageSQL = contactFollowsBaseSQL + `
 AND f.created_at<=$2::timestamptz
 AND ($4::uuid IS NULL OR (f.created_at,f.id)<($3::timestamptz,$4::uuid))
 ORDER BY f.created_at DESC,f.id DESC LIMIT $5`
const contactRequestsBaseSQL = `SELECT r.id,
            CASE WHEN r.sender_account_id = $1 THEN 'outgoing' ELSE 'incoming' END,
            CASE WHEN r.sender_account_id = $1 THEN r.recipient_account_id
                 ELSE r.sender_account_id END,
            CASE WHEN other.status = 'active' AND NOT EXISTS (
                SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                    OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
                THEN CASE WHEN birdtie_agent_profile_field_allowed(other.id,$1::uuid,'displayName')
                    THEN COALESCE(NULLIF(p.display_name,''),NULLIF(other.handle,''),'Birdtie 成员')
                    ELSE 'Birdtie 成员' END ELSE '账号暂不可用' END,
            COALESCE(r.city_id,''),
            CASE WHEN other.status = 'active' AND NOT EXISTS (
                SELECT 1 FROM account_blocks b WHERE
                    (b.blocker_account_id = $1 AND b.blocked_account_id = other.id)
                    OR (b.blocker_account_id = other.id AND b.blocked_account_id = $1))
                THEN r.note ELSE '' END,
            r.scope,
            CASE WHEN r.state = 'pending' AND r.expires_at <= clock_timestamp()
                 THEN 'expired' ELSE r.state END,
            COALESCE(cv.id::text, ''), COALESCE(r.expires_at,'0001-01-01T00:00:00Z'::timestamptz), r.created_at,COALESCE(pb.disposition,''),CASE WHEN pb.disposition='SCREEN' AND r.state='pending' AND r.expires_at>clock_timestamp() THEN 'PENDING_REVIEW' ELSE '' END,
            birdtie_person_avatar_metadata(other.id,$1::uuid),CASE WHEN r.legacy_source_id IS NOT NULL THEN 'LEGACY_IMPORT' ELSE '' END
        FROM connection_requests r
        JOIN accounts other ON other.id = CASE WHEN r.sender_account_id = $1
            THEN r.recipient_account_id ELSE r.sender_account_id END
        JOIN user_profiles p ON p.account_id = other.id
        LEFT JOIN conversations cv ON cv.request_id = r.id
 LEFT JOIN connection_request_policy_bindings pb ON pb.request_id=r.id
        WHERE (r.sender_account_id = $1 OR r.recipient_account_id = $1)`
const contactRequestsPageSQL = contactRequestsBaseSQL + `
 AND r.created_at<=$2::timestamptz
 AND ($4::uuid IS NULL OR (r.created_at,r.id)<($3::timestamptz,$4::uuid))
 ORDER BY r.created_at DESC,r.id DESC LIMIT $5`

type contactPageProof struct {
	store        *Store
	access       ea.Access
	kind         string
	input        connection.PageRequest
	snapshot     time.Time
	digest       [32]byte
	followSource string
	sessionID    string
	generation   int64
}
type contactPageQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func contactFailure(e error) error {
	if e == nil || errors.Is(e, connection.ErrInvalidContactPage) {
		return e
	}
	return chatPageNativeFailure(e)
}
func contactDigest(value any) ([32]byte, error) {
	b, e := json.Marshal(value)
	return sha256.Sum256(b), e
}
func scanContactTie(r pgx.Row) (connection.Tie, error) {
	var v connection.Tie
	e := r.Scan(&v.ID, &v.OtherAccountID, &v.OtherName, &v.CreatedAt, &v.Avatar)
	v.CreatedAt = v.CreatedAt.UTC()
	return v, e
}
func scanContactFollow(r pgx.Row) (follow.Record, error) {
	var v follow.Record
	var source string
	e := r.Scan(&v.ID, &v.Target.Type, &v.Target.ID, &v.Label, &v.CreatedAt, &v.Avatar, &v.CommunityAvatar, &source)
	v.SourceProof = source
	v.CreatedAt = v.CreatedAt.UTC()
	return v, e
}

func followPageSessionBinding(ctx context.Context, tx pgx.Tx, a ea.Access) (string, int64, error) {
	var id string
	var generation int64
	e := tx.QueryRow(ctx, `SELECT id,authorization_generation FROM sessions WHERE account_id=$1 AND token_sha256=$2`, a.Actor.ID, a.SessionDigest[:]).Scan(&id, &generation)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", 0, identity.ErrUnauthorized
	}
	if e != nil {
		return "", 0, contactFailure(e)
	}
	return id, generation, nil
}
func followPageSourceBinding(value any) (string, error) {
	p, ok := value.(connection.ContactPage[follow.Record])
	if !ok {
		return "", mp.ErrUnavailable
	}
	rows := make([]string, len(p.Items))
	for i, r := range p.Items {
		stamp, ok := r.SourceProof.(string)
		if !ok || stamp == "" {
			return "", mp.ErrUnavailable
		}
		rows[i] = stamp
	}
	b, e := json.Marshal(rows)
	if e != nil {
		return "", mp.ErrUnavailable
	}
	return string(b), nil
}
func scanContactRequest(r pgx.Row) (connection.Request, error) {
	var v connection.Request
	e := r.Scan(&v.ID, &v.Direction, &v.OtherAccountID, &v.OtherName, &v.CityID, &v.Note, &v.Scope, &v.State, &v.ConversationID, &v.ExpiresAt, &v.CreatedAt, &v.PolicyDisposition, &v.ScreeningStatus, &v.Avatar, &v.Origin)
	v.CreatedAt = v.CreatedAt.UTC()
	v.ExpiresAt = v.ExpiresAt.UTC()
	return v, e
}
func readContactPage[T any](ctx context.Context, q contactPageQuery, owner, kind string, in connection.PageRequest, c *connection.ContactCursor, snapshot time.Time, sql string, scan func(pgx.Row) (T, error), key func(T) (string, time.Time, bool)) (connection.ContactPage[T], error) {
	out := connection.ContactPage[T]{Items: []T{}}
	var lastAt time.Time
	var lastID any
	if c != nil {
		lastAt = c.Time
		lastID = c.ID
	}
	rows, e := q.Query(ctx, sql, owner, snapshot, lastAt, lastID, in.Limit+1)
	if e != nil {
		return out, contactFailure(e)
	}
	defer rows.Close()
	for rows.Next() {
		item, e := scan(rows)
		if e != nil {
			return out, contactFailure(e)
		}
		out.Items = append(out.Items, item)
		if len(out.Items) > in.Limit+1 {
			return out, mp.ErrUnavailable
		}
	}
	if e = rows.Err(); e != nil {
		return out, contactFailure(e)
	}
	if len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
		id, at, _ := key(out.Items[len(out.Items)-1])
		raw, e := connection.EncodeContactCursor(connection.ContactCursor{Version: 1, OwnerID: owner, Kind: kind, Snapshot: snapshot, Time: at, ID: id})
		if e != nil {
			return out, mp.ErrUnavailable
		}
		out.NextCursor = &raw
	}
	if connection.ValidateContactPage(out, in, owner, kind, key) != nil {
		return out, mp.ErrUnavailable
	}
	// An empty last page still has an internal snapshot proof, never wire fields.
	return out, nil
}
func nativeContactPage[T any](s *Store, ctx context.Context, a ea.Access, kind string, in connection.PageRequest, sql string, scan func(pgx.Row) (T, error), key func(T) (string, time.Time, bool)) (connection.ContactPage[T], error) {
	empty := connection.ContactPage[T]{Items: []T{}}
	in, c, e := connection.NormalizeContactPage(in, a.Actor.ID, kind)
	if e != nil {
		return empty, e
	}
	tx, e := s.beginChatPage(ctx, a)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback(context.Background())
	var sessionID string
	var generation int64
	if kind == connection.ContactFollows {
		sessionID, generation, e = followPageSessionBinding(ctx, tx, a)
		if e != nil {
			return empty, e
		}
	}
	var snapshot time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&snapshot); e != nil {
		return empty, contactFailure(e)
	}
	snapshot = snapshot.UTC()
	if c != nil {
		if c.Snapshot.After(snapshot) {
			return empty, connection.ErrInvalidContactPage
		}
		snapshot = c.Snapshot
	}
	out, e := readContactPage(ctx, tx, a.Actor.ID, kind, in, c, snapshot, sql, scan, key)
	if e != nil {
		return empty, e
	}
	digest, e := contactDigest(out)
	if e != nil {
		return empty, mp.ErrUnavailable
	}
	var followSource string
	if kind == connection.ContactFollows {
		followSource, e = followPageSourceBinding(any(out))
		if e != nil {
			return empty, e
		}
		if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
			return empty, contactFailure(e)
		}
		id, gen, e := followPageSessionBinding(ctx, tx, a)
		if e != nil {
			return empty, e
		}
		if id != sessionID || gen != generation {
			return empty, connection.ErrConflict
		}
	}
	if e = s.finishChatPage(ctx, tx, a); e != nil {
		return empty, e
	}
	out.ResponseProof = &contactPageProof{store: s, access: a, kind: kind, input: in, snapshot: snapshot, digest: digest, followSource: followSource, sessionID: sessionID, generation: generation}
	return out, nil
}
func (s *Store) ListContactTiesPage(ctx context.Context, a ea.Access, in connection.PageRequest) (connection.ContactPage[connection.Tie], error) {
	return nativeContactPage(s, ctx, a, connection.ContactTies, in, contactTiesPageSQL, scanContactTie, func(v connection.Tie) (string, time.Time, bool) { return connection.ContactTieRow(v, a.Actor.ID) })
}
func (s *Store) ListContactFollowsPage(ctx context.Context, a ea.Access, in connection.PageRequest) (connection.ContactPage[follow.Record], error) {
	return nativeContactPage(s, ctx, a, connection.ContactFollows, in, contactFollowsPageSQL, scanContactFollow, connection.ContactFollowRow)
}
func (s *Store) ListContactRequestsPage(ctx context.Context, a ea.Access, in connection.PageRequest) (connection.ContactPage[connection.Request], error) {
	return nativeContactPage(s, ctx, a, connection.ContactRequests, in, contactRequestsPageSQL, scanContactRequest, func(v connection.Request) (string, time.Time, bool) {
		return connection.ContactRequestRow(v, a.Actor.ID)
	})
}
func validateNativeContactPage[T any](s *Store, ctx context.Context, a ea.Access, kind string, in connection.PageRequest, expected connection.ContactPage[T], sql string, scan func(pgx.Row) (T, error), key func(T) (string, time.Time, bool)) error {
	in, c, e := connection.NormalizeContactPage(in, a.Actor.ID, kind)
	if e != nil {
		return e
	}
	proof, ok := expected.ResponseProof.(*contactPageProof)
	digest, e := contactDigest(expected)
	if e != nil || !ok || proof == nil || proof.store != s || proof.access != a || proof.kind != kind || proof.input != in || digest != proof.digest || c != nil && !proof.snapshot.Equal(c.Snapshot) {
		return mp.ErrUnavailable
	}
	tx, e := s.beginChatPage(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	// Acquire relation/schema and Session waits before the final current source
	// projection. These read locks grant no domain writes or Agent permission.
	if _, e = tx.Exec(ctx, humanSocialRelations); e != nil {
		return contactFailure(e)
	}
	if kind == connection.ContactFollows {
		if _, e = tx.Exec(ctx, `LOCK TABLE community_avatars IN ACCESS SHARE MODE`); e != nil {
			return contactFailure(e)
		}
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return contactFailure(e)
	}
	if kind == connection.ContactFollows {
		expectedSource, e := followPageSourceBinding(any(expected))
		if e != nil || expectedSource != proof.followSource {
			return mp.ErrUnavailable
		}
		id, gen, e := followPageSessionBinding(ctx, tx, a)
		if e != nil {
			return e
		}
		if id != proof.sessionID || gen != proof.generation {
			return connection.ErrConflict
		}
	}
	current, e := readContactPage(ctx, tx, a.Actor.ID, kind, in, c, proof.snapshot, sql, scan, key)
	if e != nil {
		return e
	}
	if e = checkHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID, s.devPhoneEnabled); e != nil {
		return contactFailure(e)
	}
	actual, e := contactDigest(current)
	if e != nil {
		return mp.ErrUnavailable
	}
	if actual != proof.digest {
		return connection.ErrConflict
	}
	if kind == connection.ContactFollows {
		source, e := followPageSourceBinding(any(current))
		if e != nil {
			return e
		}
		if source != proof.followSource {
			return connection.ErrConflict
		}
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return mp.ErrUnavailable
	}
	return nil
}
func (s *Store) ValidateContactPageResponse(ctx context.Context, a ea.Access, kind string, in connection.PageRequest, expected any) error {
	switch v := expected.(type) {
	case connection.ContactPage[connection.Tie]:
		if kind != connection.ContactTies {
			return mp.ErrUnavailable
		}
		return validateNativeContactPage(s, ctx, a, kind, in, v, contactTiesPageSQL, scanContactTie, func(x connection.Tie) (string, time.Time, bool) { return connection.ContactTieRow(x, a.Actor.ID) })
	case connection.ContactPage[follow.Record]:
		if kind != connection.ContactFollows {
			return mp.ErrUnavailable
		}
		return validateNativeContactPage(s, ctx, a, kind, in, v, contactFollowsPageSQL, scanContactFollow, connection.ContactFollowRow)
	case connection.ContactPage[connection.Request]:
		if kind != connection.ContactRequests {
			return mp.ErrUnavailable
		}
		return validateNativeContactPage(s, ctx, a, kind, in, v, contactRequestsPageSQL, scanContactRequest, func(x connection.Request) (string, time.Time, bool) {
			return connection.ContactRequestRow(x, a.Actor.ID)
		})
	default:
		return mp.ErrUnavailable
	}
}

const contactRequestCurrentSQL = contactRequestsBaseSQL + `
 AND r.id=$2::uuid AND (other.status='active' OR r.legacy_source_id IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE
 (b.blocker_account_id=$1 AND b.blocked_account_id=other.id) OR
 (b.blocker_account_id=other.id AND b.blocked_account_id=$1))`

func (s *Store) ReadContactRequestCurrent(ctx context.Context, a ea.Access, id string) (connection.Request, error) {
	if !connection.ValidChatPageID(id) {
		return connection.Request{}, connection.ErrInvalidContactPage
	}
	tx, e := s.beginChatPage(ctx, a)
	if e != nil {
		return connection.Request{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, humanSocialRelations); e != nil {
		return connection.Request{}, contactFailure(e)
	}
	if e = s.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return connection.Request{}, contactFailure(e)
	}
	out, e := scanContactRequest(tx.QueryRow(ctx, contactRequestCurrentSQL, a.Actor.ID, id))
	if errors.Is(e, pgx.ErrNoRows) {
		return connection.Request{}, connection.ErrNotFound
	}
	if e != nil {
		return connection.Request{}, contactFailure(e)
	}
	if out.ID != id {
		return connection.Request{}, mp.ErrUnavailable
	}
	if e = s.finishChatPage(ctx, tx, a); e != nil {
		return connection.Request{}, e
	}
	return out, nil
}
func (s *Store) ValidateContactRequestResponse(ctx context.Context, a ea.Access, expected connection.Request) error {
	out, e := s.ReadContactRequestCurrent(ctx, a, expected.ID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(out, expected) {
		return connection.ErrConflict
	}
	return nil
}
