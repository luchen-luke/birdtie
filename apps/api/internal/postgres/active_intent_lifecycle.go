package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
)

type humanActiveIntents struct {
	store  *Store
	cipher cipher.AEAD
}

var _ ai.Gateway = (*humanActiveIntents)(nil)

func NewHumanActiveIntents(s *Store) (ai.Gateway, error) {
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return nil, e
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	c, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	return &humanActiveIntents{s, c}, nil
}

type activeBinding struct {
	agent, authority string
	now, end         time.Time
}
type activeCaptured struct {
	Item   ai.Item
	frame  string
	source string
	end    time.Time
}
type activeSeal struct {
	Owner, Agent, Authority, ID, Frame, NextFrame string
	Input                                         ai.Input
	ExpiresAt                                     time.Time
}

func activeHash(raw []byte) string { v := sha256.Sum256(raw); return hex.EncodeToString(v[:]) }
func activeError(e error) error {
	if errors.Is(e, pgx.ErrNoRows) {
		return ai.ErrNotFound
	}
	return ai.ErrUnavailable
}
func (g *humanActiveIntents) begin(ctx context.Context, a ai.Access, write bool) (pgx.Tx, activeBinding, error) {
	var b activeBinding
	if ctx == nil || ctx.Err() != nil || g == nil || g.store == nil || g.store.pool == nil || !ai.ValidAccess(a) {
		return nil, b, ai.ErrDenied
	}
	tx, e := g.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, b, ai.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, activeBinding, error) { _ = tx.Rollback(context.Background()); return nil, b, e }
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';LOCK TABLE accounts,agents,agent_profiles,sessions,user_profiles,social_intents,social_intent_audience_targets,social_intent_invitations,cities,places,contexts,person_contexts,communities,community_memberships,account_blocks,person_ties,connection_requests IN ACCESS SHARE MODE`); e != nil {
		return fail(ai.ErrUnavailable)
	}
	if write {
		if _, e = tx.Exec(ctx, `LOCK TABLE social_intents,social_intent_audience_targets,social_intent_invitations,audit_events IN ROW EXCLUSIVE MODE`); e != nil {
			return fail(ai.ErrUnavailable)
		}
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.Actor.ID).Scan(&id); e != nil {
		return fail(ai.ErrDenied)
	}
	if e = tx.QueryRow(ctx, `SELECT ag.id FROM agents ag JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=ag.principal_account_id AND ap.owner_type='PERSON' WHERE ag.principal_account_id=$1 AND ag.agent_type='personal' AND ag.status='active' ORDER BY ag.id LIMIT 1 FOR SHARE OF ag,ap`, a.Actor.ID).Scan(&b.agent); e != nil {
		return fail(ai.ErrDenied)
	}
	if e = g.authority(ctx, tx, a, &b); e != nil {
		return fail(e)
	}
	return tx, b, nil
}
func (g *humanActiveIntents) authority(ctx context.Context, tx pgx.Tx, a ai.Access, b *activeBinding) error {
	var raw []byte
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) SELECT jsonb_build_array(a.id,a.xmin::text,ag.id,ag.xmin::text,ap.xmin::text,ap.profile_version,se.id,se.created_at,se.authentication_method,se.expires_at,encode(se.token_sha256,'hex')),stamp.n,least(se.expires_at,se.idle_expires_at)
 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN sessions se ON se.account_id=a.id,stamp
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ag.id=$2 AND ag.agent_type='personal' AND ag.status='active' AND se.token_sha256=$3 AND se.revoked_at IS NULL AND se.expires_at>stamp.n AND se.idle_expires_at>stamp.n AND ($4 OR se.authentication_method<>'dev_phone')`, a.Actor.ID, b.agent, a.SessionDigest[:], g.store.devPhoneEnabled).Scan(&raw, &b.now, &b.end)
	if e != nil {
		return ai.ErrDenied
	}
	h := activeHash(raw)
	if b.authority != "" && b.authority != h {
		return ai.ErrConflict
	}
	b.authority = h
	return nil
}
func envelope(a ai.Access, b activeBinding) ai.Envelope {
	return ai.Envelope{SchemaVersion: ai.Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: a.Actor.ID}, AgentID: b.agent, ObservedAt: b.now}
}
func activeDraft(r socialintent.Record) socialintent.DraftInput { return ai.DraftOf(r) }
func (g *humanActiveIntents) capture(ctx context.Context, tx pgx.Tx, a ai.Access, id string) (activeCaptured, error) {
	var c activeCaptured
	var raw []byte
	var epoch string
	if e := tx.QueryRow(ctx, `SELECT i.xmin::text FROM social_intents i WHERE i.id=$1 AND i.creator_account_id=$2 FOR SHARE NOWAIT`, id, a.Actor.ID).Scan(&epoch); e != nil {
		return c, activeError(e)
	}
	e := tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) SELECT jsonb_build_object('id',i.id,'creatorAccountId',i.creator_account_id,'type',i.intent_type,'title',i.title,'constraints',i.constraints,'audience',i.audience,'modality',i.modality,'contextId',i.context_id,
 'cityId',coalesce(t.city_id,''),'communityId',coalesce(t.community_id::text,''),'inviteeAccountIds',coalesce((SELECT jsonb_agg(inv.invitee_account_id ORDER BY inv.invitee_account_id) FROM social_intent_invitations inv WHERE inv.intent_id=i.id AND inv.status='invited'),'[]'),
 'status',CASE WHEN i.status IN('DRAFT','ACTIVE','MATCHED') AND i.expires_at<=stamp.n THEN 'EXPIRED' ELSE i.status END,'expiresAt',i.expires_at,'createdAt',i.created_at,'updatedAt',i.updated_at)
 FROM social_intents i LEFT JOIN social_intent_audience_targets t ON t.intent_id=i.id,stamp WHERE i.id=$1 AND i.creator_account_id=$2`, id, a.Actor.ID).Scan(&raw)
	if e != nil {
		return c, activeError(e)
	}
	if e = json.Unmarshal(raw, &c.Item.Intent); e != nil {
		return c, ai.ErrUnavailable
	}
	// Old invitation audience DTO shape is normalized before validating it.
	if c.Item.Intent.Audience != "INVITE_ONLY" {
		c.Item.Intent.InviteeIDs = nil
	}
	source, live, label, audience, end, e := g.sources(ctx, tx, a, id, activeDraft(c.Item.Intent), false)
	if e != nil {
		return c, e
	}
	c.Item.SourceAvailable = live
	c.source = source
	c.Item.LocationLabel = label
	c.Item.AudienceLabel = audience
	c.end = end
	var base []byte
	if e = tx.QueryRow(ctx, `SELECT jsonb_build_array(i.xmin::text,i.source_agent_task_id,coalesce((SELECT jsonb_agg(jsonb_build_array(t.xmin::text,t.city_id,t.community_id) ORDER BY t.intent_id) FROM social_intent_audience_targets t WHERE t.intent_id=i.id),'[]'),coalesce((SELECT jsonb_agg(jsonb_build_array(v.xmin::text,v.invitee_account_id,v.status) ORDER BY v.invitee_account_id) FROM social_intent_invitations v WHERE v.intent_id=i.id),'[]')) FROM social_intents i WHERE i.id=$1 AND i.creator_account_id=$2`, id, a.Actor.ID).Scan(&base); e != nil {
		return c, activeError(e)
	}
	c.frame = activeHash(append(base, []byte(source)...))
	c.Item.Version = c.frame
	if c.Item.Intent.ExpiresAt.Before(c.end) {
		c.end = c.Item.Intent.ExpiresAt
	}
	if e = ai.ValidateItem(c.Item, a.Actor.ID); e != nil {
		return c, e
	}
	return c, nil
}

// Only minimum public asset versions/ACL enter this frame. No private foreign
// rights/contact rows and no exact personal location are copied into preview.
func (g *humanActiveIntents) sources(ctx context.Context, tx pgx.Tx, a ai.Access, id string, in socialintent.DraftInput, publication bool) (string, bool, string, string, time.Time, error) {
	raw, e := json.Marshal(in)
	if e != nil {
		return "", false, "", "", time.Time{}, ai.ErrInvalid
	}
	var frame []byte
	var valid bool
	var label, audience string
	var end time.Time
	e = tx.QueryRow(ctx, activeIntentSourcesSQL, a.Actor.ID, string(raw), publication).Scan(&frame, &valid, &label, &audience, &end)
	if e != nil {
		return "", false, "", "", end, ai.ErrUnavailable
	}
	canonical, _ := json.Marshal([]any{json.RawMessage(frame), valid, label, audience})
	return activeHash(canonical), valid, label, audience, end, nil
}

const activeIntentSourcesSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n),d AS MATERIALIZED(SELECT $2::jsonb v),src AS MATERIALIZED(
 SELECT d.v,stamp.n,p.id pid,p.name pname,p.xmin::text px,p.publication_status pstatus,p.expires_at pend,p.city_id pcity,
 pc.id pcid,pc.xmin::text pcx,pc.publication_status pcstatus,pc.expires_at pcend,
 city.id cityid,city.name cityname,city.xmin::text cityx,city.publication_status citystatus,city.expires_at cityend,
 c.id cid,c.name cname,c.xmin::text cx,c.lifecycle_status cstatus,m.id mid,m.xmin::text mx,m.status mstatus,
 context.id contextid,context.context_type,context.city_id contextcity,context.xmin::text contextx,
 contextcity.id contextcityid,contextcity.xmin::text contextcityx,contextcity.publication_status contextcitystatus,contextcity.expires_at contextcityend,
 profile.visibility,profile.xmin::text profilex,
 coalesce((SELECT jsonb_agg(jsonb_build_array(a.id,a.xmin::text,a.status,a.account_type,EXISTS(SELECT 1 FROM account_blocks b WHERE (b.blocker_account_id=$1 AND b.blocked_account_id=a.id) OR(b.blocked_account_id=$1 AND b.blocker_account_id=a.id))) ORDER BY a.id) FROM accounts a WHERE a.id::text IN(SELECT jsonb_array_elements_text(coalesce(d.v->'inviteeAccountIds','[]')))),'[]') peers,
 coalesce((SELECT jsonb_agg(jsonb_build_array(b.blocker_account_id,b.blocked_account_id,b.xmin::text) ORDER BY b.blocker_account_id,b.blocked_account_id) FROM account_blocks b WHERE (b.blocker_account_id=$1 AND b.blocked_account_id::text IN(SELECT jsonb_array_elements_text(coalesce(d.v->'inviteeAccountIds','[]')))) OR(b.blocked_account_id=$1 AND b.blocker_account_id::text IN(SELECT jsonb_array_elements_text(coalesce(d.v->'inviteeAccountIds','[]'))))),'[]') blocks
 FROM d CROSS JOIN stamp LEFT JOIN places p ON p.id::text=d.v->'constraints'->>'placeId'
 LEFT JOIN cities pc ON pc.id=p.city_id LEFT JOIN cities city ON city.id=d.v->>'cityId' LEFT JOIN communities c ON c.id::text=d.v->>'communityId'
 LEFT JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$1
 LEFT JOIN contexts context ON context.id::text=d.v->>'contextId' LEFT JOIN cities contextcity ON contextcity.id=context.city_id
 LEFT JOIN user_profiles profile ON profile.account_id=$1)
 SELECT jsonb_build_array(pid,px,pstatus,pend,pcity,pcid,pcx,pcstatus,pcend,cityid,cityx,citystatus,cityend,cid,cx,cstatus,mid,mx,mstatus,contextid,context_type,contextcity,contextx,contextcityx,contextcitystatus,contextcityend,profilex,visibility,peers,blocks),
 (coalesce(v->'constraints'->>'placeId','')='' OR(pid IS NOT NULL AND pstatus='published' AND(pend IS NULL OR pend>n) AND pcid IS NOT NULL AND pcstatus='published' AND(pcend IS NULL OR pcend>n) AND(v->>'audience'<>'LOCAL' OR pcity=v->>'cityId') AND(coalesce(v->>'contextId','')='' OR context_type<>'CITY' OR pcity=contextcity)))
 AND(coalesce(v->>'contextId','')='' OR(contextid IS NOT NULL AND ((v->>'modality'='ONLINE' AND context_type='ONLINE') OR(v->>'modality'<>'ONLINE' AND context_type='CITY' AND contextcityid IS NOT NULL AND contextcitystatus='published' AND(contextcityend IS NULL OR contextcityend>n)))))
 AND CASE v->>'audience' WHEN 'LOCAL' THEN cityid IS NOT NULL AND citystatus='published' AND(cityend IS NULL OR cityend>n)
 WHEN 'COMMUNITY' THEN cid IS NOT NULL AND cstatus='active' AND mid IS NOT NULL AND mstatus='active'
 WHEN 'INVITE_ONLY' THEN jsonb_array_length(peers)=jsonb_array_length(v->'inviteeAccountIds') AND jsonb_array_length(blocks)=0 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(peers) r WHERE r->>0=$1::text OR r->>2<>'active' OR r->>3<>'person') ELSE TRUE END
 AND(NOT $3 OR v->>'audience' NOT IN('PUBLIC','LOCAL') OR visibility='public'),
 CASE WHEN v->>'modality'='ONLINE' THEN coalesce(nullif(v->'constraints'->>'onlinePlatform',''),'线上方式未设定')
 WHEN pid IS NOT NULL AND pstatus='published' AND(pend IS NULL OR pend>n) AND pcid IS NOT NULL AND pcstatus='published' AND(pcend IS NULL OR pcend>n) THEN pname
 WHEN coalesce(v->'constraints'->>'placeId','')<>'' THEN '地点暂不可用' ELSE coalesce(nullif(v->'constraints'->>'areaLabel',''),'地点未设定') END,
 CASE v->>'audience' WHEN 'PRIVATE' THEN '仅自己' WHEN 'PUBLIC' THEN '公开' WHEN 'FRIENDS' THEN '好友'
 WHEN 'LOCAL' THEN CASE WHEN cityid IS NOT NULL AND citystatus='published' AND(cityend IS NULL OR cityend>n) THEN cityname||'本地用户' ELSE '本地范围暂不可用' END
 WHEN 'COMMUNITY' THEN CASE WHEN cid IS NOT NULL AND cstatus='active' AND mstatus='active' THEN cname||'社群成员' ELSE '社群范围暂不可用' END WHEN 'INVITE_ONLY' THEN '仅指定邀请对象' ELSE '范围未知' END,
 least(coalesce(pend,'infinity'),coalesce(pcend,'infinity'),coalesce(cityend,'infinity'),coalesce(contextcityend,'infinity'),n+interval '90 days') FROM src`

func (g *humanActiveIntents) finish(ctx context.Context, tx pgx.Tx, a ai.Access, b *activeBinding) error {
	if e := g.store.lockHumanMomentSession(ctx, tx, a.SessionDigest, a.Actor.ID); e != nil {
		return ai.ErrDenied
	}
	return g.authority(ctx, tx, a, b)
}

type activeExpectation struct {
	Draft       socialintent.DraftInput `json:"draft"`
	Publication bool                    `json:"publication"`
	Source      string                  `json:"-"`
	Deadline    time.Time               `json:"-"`
}

func activeExpected(c activeCaptured) activeExpectation {
	end := time.Time{}
	if c.Item.Intent.Status == "DRAFT" || c.Item.Intent.Status == "ACTIVE" || c.Item.Intent.Status == "MATCHED" {
		end = c.Item.Intent.ExpiresAt
	}
	return activeExpectation{Draft: activeDraft(c.Item.Intent), Source: c.source, Deadline: end}
}

// Every selected source and the current native identity is checked in ONE RC
// statement after all real waits. A phantom Block or a deadline of an earlier
// list row cannot be hidden by a later Session-only query.
func (g *humanActiveIntents) allCurrent(ctx context.Context, tx pgx.Tx, a ai.Access, b *activeBinding, expected []activeExpectation) error {
	if len(expected) == 0 {
		return g.authority(ctx, tx, a, b)
	}
	raw, e := json.Marshal(expected)
	if e != nil {
		return ai.ErrUnavailable
	}
	q := strings.Replace(activeIntentSourcesSQL, "d AS MATERIALIZED(SELECT $2::jsonb v)", "d AS MATERIALIZED(SELECT x.v->'draft' v,x.ord,(x.v->>'publication')::boolean pub FROM jsonb_array_elements($2::jsonb) WITH ORDINALITY x(v,ord))", 1)
	q = strings.Replace(q, "SELECT d.v,stamp.n", "SELECT d.v,d.ord,d.pub,stamp.n", 1)
	q = strings.Replace(q, "NOT $3", "NOT pub", 1)
	q = strings.Replace(q, "n+interval '90 days') FROM src", `n+interval '90 days'),n,
 (SELECT jsonb_build_array(a.id,a.xmin::text,ag.id,ag.xmin::text,ap.xmin::text,ap.profile_version,se.id,se.created_at,se.authentication_method,se.expires_at,encode(se.token_sha256,'hex')) FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN sessions se ON se.account_id=a.id WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ag.id=$3 AND ag.agent_type='personal' AND ag.status='active' AND se.token_sha256=$4 AND se.revoked_at IS NULL AND se.expires_at>n AND se.idle_expires_at>n AND($5 OR se.authentication_method<>'dev_phone')),
 (SELECT least(expires_at,idle_expires_at) FROM sessions WHERE account_id=$1 AND token_sha256=$4) FROM src ORDER BY ord`, 1)
	rows, e := tx.Query(ctx, q, a.Actor.ID, string(raw), b.agent, a.SessionDigest[:], g.store.devPhoneEnabled)
	if e != nil {
		return ai.ErrUnavailable
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var frame, authority []byte
		var live bool
		var label, audience string
		var end, now, sessionEnd time.Time
		if e = rows.Scan(&frame, &live, &label, &audience, &end, &now, &authority, &sessionEnd); e != nil {
			return ai.ErrUnavailable
		}
		if i >= len(expected) || len(authority) == 0 || activeHash(authority) != b.authority || !sessionEnd.After(now) {
			return ai.ErrDenied
		}
		canonical, _ := json.Marshal([]any{json.RawMessage(frame), live, label, audience})
		if activeHash(canonical) != expected[i].Source || (live && !end.After(now)) || (!expected[i].Deadline.IsZero() && !expected[i].Deadline.After(now)) {
			return ai.ErrConflict
		}
		b.now = now
		b.end = sessionEnd
		i++
	}
	if rows.Err() != nil || i != len(expected) {
		return ai.ErrUnavailable
	}
	return nil
}
func (g *humanActiveIntents) ReadOwn(ctx context.Context, a ai.Access, id string) (ai.Detail, error) {
	out := ai.Detail{}
	if !ai.UUID(id) {
		return out, ai.ErrInvalid
	}
	tx, b, e := g.begin(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	c, e := g.capture(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if e = g.finish(ctx, tx, a, &b); e != nil {
		return out, e
	}
	last, e := g.capture(ctx, tx, a, id)
	if e != nil || c.frame != last.frame {
		return out, ai.ErrConflict
	}
	if e = g.allCurrent(ctx, tx, a, &b, []activeExpectation{activeExpected(last)}); e != nil {
		return out, e
	}
	out.Envelope = envelope(a, b)
	out.Item = last.Item
	if e = tx.Commit(ctx); e != nil {
		return ai.Detail{}, ai.ErrUnavailable
	}
	return out, nil
}
func (g *humanActiveIntents) ListOwn(ctx context.Context, a ai.Access) (ai.List, error) {
	out := ai.List{Items: []ai.Item{}, Limit: 100}
	tx, b, e := g.begin(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT id::text FROM social_intents WHERE creator_account_id=$1 ORDER BY created_at DESC,id DESC LIMIT 101`, a.Actor.ID)
	if e != nil {
		return out, ai.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, ai.ErrUnavailable
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, ai.ErrUnavailable
	}
	out.Truncated = len(ids) > 100
	if out.Truncated {
		ids = ids[:100]
	}
	frames := map[string]string{}
	finalSources := []activeExpectation{}
	for _, id := range ids {
		c, e := g.capture(ctx, tx, a, id)
		if e != nil {
			return out, e
		}
		frames[id] = c.frame
	}
	if e = g.finish(ctx, tx, a, &b); e != nil {
		return out, e
	}
	for _, id := range ids {
		c, e := g.capture(ctx, tx, a, id)
		if e != nil || frames[id] != c.frame {
			return ai.List{}, ai.ErrConflict
		}
		out.Items = append(out.Items, c.Item)
		finalSources = append(finalSources, activeExpected(c))
	}
	if e = g.allCurrent(ctx, tx, a, &b, finalSources); e != nil {
		return ai.List{}, e
	}
	out.Envelope = envelope(a, b)
	if e = tx.Commit(ctx); e != nil {
		return ai.List{}, ai.ErrUnavailable
	}
	return out, nil
}
func (g *humanActiveIntents) PreviewOwn(ctx context.Context, a ai.Access, id string, in ai.Input) (ai.Preview, error) {
	out := ai.Preview{}
	if !ai.UUID(id) || ai.ValidateInput(in) != nil {
		return out, ai.ErrInvalid
	}
	tx, b, e := g.begin(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	c, e := g.capture(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	if c.Item.Version != in.ExpectedVersion {
		return out, ai.ErrConflict
	}
	after, next, e := g.proposal(ctx, tx, a, b, c, in)
	if e != nil {
		return out, e
	}
	if e = g.finish(ctx, tx, a, &b); e != nil {
		return out, e
	}
	last, e := g.capture(ctx, tx, a, id)
	if e != nil || c.frame != last.frame {
		return out, ai.ErrConflict
	}
	after2, next2, e := g.proposal(ctx, tx, a, b, last, in)
	if e != nil || next != next2 || !ai.JSONEqual(after, after2) {
		return out, ai.ErrConflict
	}
	if e = g.allCurrent(ctx, tx, a, &b, []activeExpectation{activeExpected(last), {Draft: activeDraft(after2.Intent), Publication: in.Operation == "ACTIVATE", Source: next2}}); e != nil {
		return out, e
	}
	end := b.now.Add(ai.PreviewTTL)
	bounds := []time.Time{b.end, c.Item.Intent.ExpiresAt, after.Intent.ExpiresAt}
	if in.Operation != "CANCEL" {
		bounds = append(bounds, c.end)
	}
	for _, v := range bounds {
		if v.Before(end) {
			end = v
		}
	}
	if !end.After(b.now) {
		return out, ai.ErrConflict
	}
	seal := activeSeal{Owner: a.Actor.ID, Agent: b.agent, Authority: b.authority, ID: id, Frame: c.frame, NextFrame: next, Input: in, ExpiresAt: end}
	token, e := g.seal(seal)
	if e != nil {
		return out, e
	}
	out = ai.Preview{Envelope: envelope(a, b), PreviewID: token, Operation: in.Operation, Before: c.Item, After: after, ExpiresAt: end, Explanation: activeExplanation(in.Operation)}
	if e = tx.Commit(ctx); e != nil {
		return ai.Preview{}, ai.ErrUnavailable
	}
	return out, nil
}
func activeExplanation(op string) string {
	switch op {
	case "EDIT":
		return "保存修改后回到草稿，停止原公开发现；需要新的具体确认才能重新激活。不会清除对话或发送邀请。"
	case "ACTIVATE":
		return "确认后按所示受众激活，不会发送邀请、报名或调用模型。"
	default:
		return "仅取消此意图，停止发现；不会删除对话、取消报名或撤销已经发送的申请。"
	}
}
func (g *humanActiveIntents) proposal(ctx context.Context, tx pgx.Tx, a ai.Access, b activeBinding, c activeCaptured, in ai.Input) (ai.Item, string, error) {
	after := c.Item
	if c.Item.Intent.Status != "DRAFT" && c.Item.Intent.Status != "ACTIVE" && c.Item.Intent.Status != "MATCHED" {
		return after, "", ai.ErrConflict
	}
	d := activeDraft(c.Item.Intent)
	switch in.Operation {
	case "EDIT":
		var e error
		d, e = ai.NormalizeDraft(*in.Edit)
		if e != nil {
			return after, "", e
		}
		if !d.ExpiresAt.After(b.now.Add(time.Minute)) || d.ExpiresAt.After(b.now.Add(90*24*time.Hour)) {
			return after, "", ai.ErrInvalid
		}
		after.Intent.Type = d.Type
		after.Intent.Title = d.Title
		after.Intent.Constraints = d.Constraints
		after.Intent.Audience = d.Audience
		after.Intent.Modality = d.Modality
		after.Intent.ContextID = nil
		if d.ContextID != "" {
			v := d.ContextID
			after.Intent.ContextID = &v
		}
		after.Intent.CityID = d.CityID
		after.Intent.CommunityID = d.CommunityID
		after.Intent.InviteeIDs = d.InviteeIDs
		after.Intent.ExpiresAt = d.ExpiresAt
		after.Intent.Status = "DRAFT"
	case "ACTIVATE":
		if c.Item.Intent.Status != "DRAFT" {
			return after, "", ai.ErrConflict
		}
		after.Intent.Status = "ACTIVE"
	case "CANCEL":
		after.Intent.Status = "CANCELLED"
	default:
		return after, "", ai.ErrInvalid
	}
	source, valid, label, audience, _, e := g.sources(ctx, tx, a, c.Item.Intent.ID, d, in.Operation == "ACTIVATE")
	if e != nil {
		return after, "", e
	}
	if !valid && in.Operation != "CANCEL" {
		return after, "", ai.ErrDenied
	}
	after.SourceAvailable = valid
	after.LocationLabel = label
	after.AudienceLabel = audience
	return after, source, nil
}
func (g *humanActiveIntents) seal(v activeSeal) (string, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return "", ai.ErrUnavailable
	}
	nonce := make([]byte, g.cipher.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", ai.ErrUnavailable
	}
	encrypted := g.cipher.Seal(nonce, nonce, raw, []byte(ai.Schema))
	return base64.RawURLEncoding.EncodeToString(encrypted), nil
}
func (g *humanActiveIntents) open(token string) (activeSeal, error) {
	var v activeSeal
	if len(token) > 20000 {
		return v, ai.ErrInvalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || len(raw) <= g.cipher.NonceSize() {
		return v, ai.ErrInvalid
	}
	raw, e = g.cipher.Open(nil, raw[:g.cipher.NonceSize()], raw[g.cipher.NonceSize():], []byte(ai.Schema))
	if e != nil || json.Unmarshal(raw, &v) != nil {
		return v, ai.ErrConflict
	}
	return v, nil
}
func (g *humanActiveIntents) ApproveOwn(ctx context.Context, a ai.Access, id, token string) (ai.Receipt, error) {
	out := ai.Receipt{}
	seal, e := g.open(token)
	if e != nil {
		return out, e
	}
	if seal.ID != id || seal.Owner != a.Actor.ID {
		return out, ai.ErrDenied
	}
	tx, b, e := g.begin(ctx, a, true)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	if seal.Authority != b.authority || seal.Agent != b.agent || !seal.ExpiresAt.After(b.now) {
		return out, ai.ErrConflict
	}
	var currentID string
	if e = tx.QueryRow(ctx, `SELECT id FROM social_intents WHERE id=$1 AND creator_account_id=$2 FOR UPDATE NOWAIT`, id, a.Actor.ID).Scan(&currentID); e != nil {
		return out, ai.ErrConflict
	}
	c, e := g.capture(ctx, tx, a, id)
	if e != nil || c.frame != seal.Frame {
		return out, ai.ErrConflict
	}
	after, next, e := g.proposal(ctx, tx, a, b, c, seal.Input)
	if e != nil {
		return out, e
	}
	if next != seal.NextFrame {
		return out, ai.ErrConflict
	}
	if e = g.finish(ctx, tx, a, &b); e != nil {
		return out, e
	}
	last, e := g.capture(ctx, tx, a, id)
	if e != nil || last.frame != seal.Frame {
		return out, ai.ErrConflict
	}
	_, next, e = g.proposal(ctx, tx, a, b, last, seal.Input)
	if e != nil || next != seal.NextFrame || !seal.ExpiresAt.After(b.now) {
		return out, ai.ErrConflict
	}
	if seal.Input.Operation == "EDIT" {
		d := activeDraft(after.Intent)
		var cid any
		if d.ContextID != "" {
			cid = d.ContextID
		}
		if _, e = tx.Exec(ctx, `UPDATE social_intents SET intent_type=$3,title=$4,constraints=$5::jsonb,audience=$6,modality=$7,context_id=$8,expires_at=$9,status='DRAFT',updated_at=clock_timestamp() WHERE id=$1 AND creator_account_id=$2`, id, a.Actor.ID, d.Type, d.Title, []byte(d.Constraints), d.Audience, d.Modality, cid, d.ExpiresAt); e != nil {
			return out, ai.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `DELETE FROM social_intent_audience_targets WHERE intent_id=$1`, id); e != nil {
			return out, ai.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `DELETE FROM social_intent_invitations WHERE intent_id=$1`, id); e != nil {
			return out, ai.ErrUnavailable
		}
		if d.Audience == "LOCAL" {
			_, e = tx.Exec(ctx, `INSERT INTO social_intent_audience_targets(intent_id,city_id) VALUES($1,$2)`, id, d.CityID)
		} else if d.Audience == "COMMUNITY" {
			_, e = tx.Exec(ctx, `INSERT INTO social_intent_audience_targets(intent_id,community_id) VALUES($1,$2)`, id, d.CommunityID)
		}
		if e != nil {
			return out, ai.ErrUnavailable
		}
		sort.Strings(d.InviteeIDs)
		for _, peer := range d.InviteeIDs {
			if _, e = tx.Exec(ctx, `INSERT INTO social_intent_invitations(intent_id,invitee_account_id) VALUES($1,$2)`, id, peer); e != nil {
				return out, ai.ErrUnavailable
			}
		}
	} else {
		if _, e = tx.Exec(ctx, `UPDATE social_intents SET status=$3,updated_at=clock_timestamp() WHERE id=$1 AND creator_account_id=$2`, id, a.Actor.ID, after.Intent.Status); e != nil {
			return out, ai.ErrUnavailable
		}
	}
	if _, e = auditExec(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'social_intent',$3,'allowed','human_concrete_version')`, a.Actor.ID, seal.Input.Operation, id); e != nil {
		return out, ai.ErrUnavailable
	}
	if seal.Input.Operation == "ACTIVATE" {
		if e = routeOpportunityIntentOwner(ctx, tx, a.Actor.ID); e != nil {
			return out, ai.ErrUnavailable
		}
	}
	if _, e = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
		return out, ai.ErrUnavailable
	}
	final, e := g.capture(ctx, tx, a, id)
	if e != nil {
		return out, e
	}
	finalFrame, live, _, _, finalEnd, e := g.sources(ctx, tx, a, id, activeDraft(final.Item.Intent), seal.Input.Operation == "ACTIVATE")
	if e != nil || finalFrame != seal.NextFrame || (!live && seal.Input.Operation != "CANCEL") {
		return out, ai.ErrDenied
	}
	if e = g.allCurrent(ctx, tx, a, &b, []activeExpectation{{Draft: activeDraft(final.Item.Intent), Publication: seal.Input.Operation == "ACTIVATE", Source: seal.NextFrame}}); e != nil || !seal.ExpiresAt.After(b.now) {
		return out, ai.ErrConflict
	}
	if seal.Input.Operation != "CANCEL" && !finalEnd.After(b.now) {
		return out, ai.ErrConflict
	}
	out = ai.Receipt{Envelope: envelope(a, b), Item: final.Item, Operation: seal.Input.Operation, Committed: true, Explanation: activeExplanation(seal.Input.Operation)}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return ai.Receipt{}, ai.ErrUnavailable
	}
	return out, nil
}

func (g *humanActiveIntents) OptionsOwn(ctx context.Context, a ai.Access) (ai.Options, error) {
	out := ai.Options{Cities: []ai.Option{}, Places: []ai.Option{}, Communities: []ai.Option{}, Invitees: []ai.Option{}, Limit: 100}
	tx, b, e := g.begin(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	if e = g.finish(ctx, tx, a, &b); e != nil {
		return out, e
	}
	query := `SELECT kind,id,label FROM(
 (SELECT 'CITY' kind,id,name label FROM cities WHERE publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp()) ORDER BY id LIMIT 101)
 UNION ALL(SELECT 'PLACE',p.id::text,p.name FROM places p JOIN cities c ON c.id=p.city_id AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) WHERE p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) ORDER BY p.id LIMIT 101)
 UNION ALL(SELECT 'COMMUNITY',c.id::text,c.name FROM communities c JOIN community_memberships m ON m.community_id=c.id AND m.user_account_id=$1 AND m.status='active' WHERE c.lifecycle_status='active' ORDER BY c.id LIMIT 101)
 UNION ALL(SELECT 'PERSON',a.id::text,coalesce(nullif(p.display_name,''),'好友') FROM accounts a JOIN person_ties t ON a.id=CASE WHEN t.person_a_account_id=$1 THEN t.person_b_account_id ELSE t.person_a_account_id END JOIN connection_requests r ON r.id=t.request_id AND r.state='accepted' AND r.scope='friend' LEFT JOIN user_profiles p ON p.account_id=a.id AND p.visibility='public' WHERE t.status='active' AND(t.person_a_account_id=$1 OR t.person_b_account_id=$1) AND a.account_type='person' AND a.status='active' AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=$1 AND b.blocked_account_id=a.id) OR(b.blocked_account_id=$1 AND b.blocker_account_id=a.id)) ORDER BY a.id LIMIT 101)) choices ORDER BY kind,id`
	rows, e := tx.Query(ctx, query, a.Actor.ID)
	if e != nil {
		return out, ai.ErrUnavailable
	}
	counts := map[string]int{}
	for rows.Next() {
		var kind string
		var v ai.Option
		if e = rows.Scan(&kind, &v.ID, &v.Label); e != nil {
			rows.Close()
			return out, ai.ErrUnavailable
		}
		counts[kind]++
		if counts[kind] > 100 {
			out.Truncated = true
			continue
		}
		switch kind {
		case "CITY":
			out.Cities = append(out.Cities, v)
		case "PLACE":
			out.Places = append(out.Places, v)
		case "COMMUNITY":
			out.Communities = append(out.Communities, v)
		case "PERSON":
			out.Invitees = append(out.Invitees, v)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return out, ai.ErrUnavailable
	}
	if e = g.authority(ctx, tx, a, &b); e != nil {
		return out, e
	}
	out.Envelope = envelope(a, b)
	if e = tx.Commit(ctx); e != nil {
		return ai.Options{}, ai.ErrUnavailable
	}
	return out, nil
}
