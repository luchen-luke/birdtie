package postgres

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/activityplan"
	ic "github.com/birdtie/birdtie/apps/api/internal/intentconversion"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type humanIntentConversions struct{ base *humanActiveIntents }

func NewHumanIntentConversions(s *Store) (ic.Gateway, error) {
	v, e := NewHumanActiveIntents(s)
	if e != nil {
		return nil, e
	}
	return &humanIntentConversions{v.(*humanActiveIntents)}, nil
}

var _ ic.Gateway = (*humanIntentConversions)(nil)

type conversionCaptured struct {
	intent                activeCaptured
	rowProof, sourceProof string
	plans                 ownPlansFrame
	choice                ic.Choice
	category              string
}
type conversionSeal struct {
	Owner, Agent, Authority, IntentID, ActivityID, ParticipationID, IntentVersion, RowProof, SourceProof, PlansProof string
	ExpiresAt                                                                                                        time.Time
}

func conversionEnvelope(a ic.Access, b activeBinding) ic.Envelope {
	return ic.Envelope{SchemaVersion: ic.Schema, OwnerID: a.Actor.ID, AgentID: b.agent, ObservedAt: b.now}
}
func conversionError(e error) error {
	switch {
	case errors.Is(e, ai.ErrDenied):
		return ic.ErrDenied
	case errors.Is(e, ai.ErrConflict) || errors.Is(e, ai.ErrNotFound):
		return ic.ErrConflict
	default:
		return ic.ErrUnavailable
	}
}
func (g *humanIntentConversions) begin(ctx context.Context, a ic.Access, write bool) (pgx.Tx, activeBinding, error) {
	tx, b, e := g.base.begin(ctx, a, write)
	if e != nil {
		return nil, b, conversionError(e)
	}
	fail := func() (pgx.Tx, activeBinding, error) {
		tx.Rollback(context.Background())
		return nil, b, ic.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE activity_plans,activity_participations,activities,activity_organizers,organizations,organization_memberships,businesses,business_memberships,business_venue_relations,venues,venue_candidates,activity_invitations IN ACCESS SHARE MODE`); e != nil {
		return fail()
	}
	var ready bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='social_intents'::regclass AND tgname='guard_intent_activity_conversion' AND tgenabled IN('O','A'))`).Scan(&ready); e != nil || !ready {
		return fail()
	}
	return tx, b, nil
}

const conversionIntentRowSQL = `SELECT jsonb_build_array(i.xmin::text,i.source_agent_task_id,i.status,i.converted_activity_id,i.converted_participation_id,i.converted_at,i.conversion_preview_digest,
 coalesce((SELECT jsonb_agg(jsonb_build_array(t.xmin::text,t.city_id,t.community_id) ORDER BY t.intent_id) FROM social_intent_audience_targets t WHERE t.intent_id=i.id),'[]'),
 coalesce((SELECT jsonb_agg(jsonb_build_array(v.xmin::text,v.invitee_account_id,v.status) ORDER BY v.invitee_account_id) FROM social_intent_invitations v WHERE v.intent_id=i.id),'[]'))
 FROM social_intents i WHERE i.id=$6 AND i.creator_account_id=$1`

// Authenticate may legitimately slide idle expiry between human HTTP requests.
// Bind the immutable native session identity, not its sliding row xmin; retain
// the original preview ceiling and current absolute/idle/revoked checks.
var conversionPlansFrameSQL = strings.ReplaceAll(ownPlansFrameSQL, "ss.id,ss.xmin::text,ss.created_at", "ss.id,ss.created_at")

func (g *humanIntentConversions) capture(ctx context.Context, tx pgx.Tx, a ic.Access, b *activeBinding, id, activity string) (conversionCaptured, error) {
	var c conversionCaptured
	v, e := g.base.capture(ctx, tx, a, id)
	if e != nil {
		return c, conversionError(e)
	}
	c.intent = v
	if e = tx.QueryRow(ctx, `SELECT converted_activity_id,converted_participation_id,converted_at FROM social_intents WHERE id=$1 AND creator_account_id=$2`, id, a.Actor.ID).Scan(&c.intent.Item.Intent.ConvertedActivityID, &c.intent.Item.Intent.ConvertedParticipationID, &c.intent.Item.Intent.ConvertedAt); e != nil {
		return c, ic.ErrUnavailable
	}
	draft := activeDraft(v.Item.Intent)
	raw, e := json.Marshal(draft)
	if e != nil {
		return c, ic.ErrInvalid
	}
	source, live, _, _, end, e := g.base.sources(ctx, tx, a, id, draft, true)
	if e != nil {
		return c, conversionError(e)
	}
	if !live {
		return c, ic.ErrConflict
	}
	c.sourceProof = source
	var row []byte
	// Native placeholders are shared with finalSQL, so use it as a derived query.
	q := strings.ReplaceAll(conversionIntentRowSQL, "$6", "$2")
	if e = tx.QueryRow(ctx, q, a.Actor.ID, id).Scan(&row); e != nil {
		return c, ic.ErrConflict
	}
	c.rowProof = activeHash(row)
	if e = tx.QueryRow(ctx, conversionPlansFrameSQL, a.Actor.ID, a.SessionDigest[:], g.base.store.devPhoneEnabled).Scan(&c.plans.proof, &c.plans.observed, &c.plans.expires); e != nil {
		return c, ic.ErrDenied
	}
	if end.Before(c.plans.expires) {
		c.plans.expires = end
	}
	if (v.Item.Intent.Status == socialintent.Active || v.Item.Intent.Status == socialintent.Matched) && v.end.Before(c.plans.expires) {
		c.plans.expires = v.end
	}
	if b.end.Before(c.plans.expires) {
		c.plans.expires = b.end
	}
	if activity == "" {
		return c, nil
	}
	if e = tx.QueryRow(ctx, `SELECT a.id,a.title,a.city_id,a.starts_at,a.ends_at,a.modality,a.physical_place_status,coalesce(loc.id::text,''),coalesce(loc.name,''),a.time_zone,a.category_code,p.id,a.created_at
 FROM activity_participations p JOIN activities a ON a.id=p.activity_id JOIN cities city ON city.id=a.city_id
 LEFT JOIN places loc ON loc.id=a.place_id AND loc.city_id=a.city_id AND loc.publication_status='published' AND(loc.expires_at IS NULL OR loc.expires_at>clock_timestamp())
 WHERE p.participant_account_id=$1 AND a.id=$2 AND p.status='going' AND a.publication_status='published'
 AND a.cancelled_at IS NULL AND a.ends_at>clock_timestamp() AND(a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND city.publication_status='published' AND(city.expires_at IS NULL OR city.expires_at>clock_timestamp()) AND birdtie_activity_visible_to(a.id,$1)
 AND NOT EXISTS(SELECT 1 FROM account_blocks block WHERE(block.blocker_account_id=$1 AND block.blocked_account_id=a.host_account_id) OR(block.blocked_account_id=$1 AND block.blocker_account_id=a.host_account_id))`, a.Actor.ID, activity).Scan(&c.choice.Activity.ActivityID, &c.choice.Activity.Title, &c.choice.Activity.CityID, &c.choice.Activity.StartsAt, &c.choice.Activity.EndsAt, &c.choice.Activity.Modality, &c.choice.Activity.PhysicalPlaceStatus, &c.choice.Activity.PlaceID, &c.choice.Activity.PlaceName, &c.choice.Activity.TimeZone, &c.category, &c.choice.ParticipationID, &c.choice.Activity.CreatedAt); e != nil {
		return c, ic.ErrConflict
	}
	c.choice.Activity.ID = c.choice.Activity.ActivityID // original Activity ID, not a new private reminder
	c.choice.Activity.Available = true
	c.choice.Activity.Status = "upcoming"
	if !c.choice.Activity.StartsAt.After(c.plans.observed) {
		c.choice.Activity.Status = "ongoing"
	}
	if c.choice.Activity.EndsAt.Before(c.plans.expires) {
		c.plans.expires = *c.choice.Activity.EndsAt
	}
	if e = conversionCompatible(v.Item.Intent, c.choice.Activity, c.category); e != nil {
		return c, e
	}
	_ = raw
	return c, nil
}
func conversionCompatible(i socialintent.Record, a activityplan.Plan, category string) error {
	if i.Type != "FIND_ACTIVITY" || (i.Status != socialintent.Active && i.Status != socialintent.Matched && i.Status != socialintent.Converted) {
		return ic.ErrConflict
	}
	c, _, e := socialintent.ParseConstraints(i.Constraints, i.Modality)
	if e != nil {
		return ic.ErrInvalid
	}
	if c.AreaLabel != "" || c.OnlinePlatform != "" || c.MinParticipants != 0 || c.MaxParticipants != 0 {
		return ic.ErrUnavailable
	}
	if c.Category != "" && c.Category != category {
		return ic.ErrConflict
	}
	mode := map[string]string{"ONLINE": "online", "IN_PERSON": "in_person", "HYBRID": "hybrid"}[i.Modality]
	if mode == "" || mode != a.Modality {
		return ic.ErrConflict
	}
	if i.CityID != "" && i.CityID != a.CityID {
		return ic.ErrConflict
	}
	if c.PlaceID != "" && (a.PlaceID == "" || c.PlaceID != a.PlaceID) {
		return ic.ErrConflict
	}
	if a.StartsAt == nil || a.EndsAt == nil {
		return ic.ErrConflict
	}
	if c.StartsAt != nil && (a.StartsAt.Before(*c.StartsAt) || a.EndsAt.After(*c.EndsAt)) {
		return ic.ErrConflict
	}
	return nil
}

// One statement captures every native source, intent revision and actor after
// relation/row/session/audit waits. No subsequent authentication waits.
func (g *humanIntentConversions) current(ctx context.Context, tx pgx.Tx, a ic.Access, b *activeBinding, c conversionCaptured) error {
	raw, e := json.Marshal(activeDraft(c.intent.Item.Intent))
	if e != nil {
		return ic.ErrInvalid
	}
	src := strings.ReplaceAll(strings.ReplaceAll(activeIntentSourcesSQL, "$3", "$5"), "$2", "$4")
	// All nested closures and authority compare against the same current native
	// timestamp after all relation/session waits, never independent clocks.
	src = strings.ReplaceAll(src, "clock_timestamp()", "(SELECT at FROM conversion_clock)")
	plansSQL := strings.ReplaceAll(conversionPlansFrameSQL, "clock_timestamp()", "(SELECT at FROM conversion_clock)")
	q := `WITH conversion_clock AS MATERIALIZED(SELECT clock_timestamp() at),plans AS MATERIALIZED (` + plansSQL + `),scope AS MATERIALIZED (` + src + `),own_intent AS MATERIALIZED (` + conversionIntentRowSQL + `)
 SELECT plans.*,scope.*,own_intent.*,(SELECT at FROM conversion_clock),
 (SELECT jsonb_build_array(owner.id,owner.xmin::text,ag.id,ag.xmin::text,ap.xmin::text,ap.profile_version,se.id,se.created_at,se.authentication_method,se.expires_at,encode(se.token_sha256,'hex'))
 FROM accounts owner JOIN agents ag ON ag.principal_account_id=owner.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=owner.id AND ap.owner_type='PERSON' JOIN sessions se ON se.account_id=owner.id
 WHERE owner.id=$1 AND owner.account_type='person' AND owner.status='active' AND ag.id=$7 AND ag.agent_type='personal' AND ag.status='active' AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>(SELECT at FROM conversion_clock) AND se.idle_expires_at>(SELECT at FROM conversion_clock) AND($3 OR se.authentication_method<>'dev_phone'))
 FROM plans CROSS JOIN scope CROSS JOIN own_intent`
	var pp string
	var observed, end, sourceEnd, now time.Time
	var frame, row, authority []byte
	var live bool
	var label, audience string
	e = tx.QueryRow(ctx, q, a.Actor.ID, a.SessionDigest[:], g.base.store.devPhoneEnabled, string(raw), true, c.intent.Item.Intent.ID, b.agent).Scan(&pp, &observed, &end, &frame, &live, &label, &audience, &sourceEnd, &row, &now, &authority)
	if e != nil || len(authority) == 0 {
		return ic.ErrDenied
	}
	canonical, _ := json.Marshal([]any{json.RawMessage(frame), live, label, audience})
	if pp != c.plans.proof || activeHash(row) != c.rowProof || activeHash(canonical) != c.sourceProof || activeHash(authority) != b.authority || !live || observed.Before(c.plans.observed) || !now.Before(c.plans.expires) || !now.Before(sourceEnd) {
		return ic.ErrConflict
	}
	b.now = now
	return nil
}
func (g *humanIntentConversions) ListOwn(ctx context.Context, a ic.Access, id string) (ic.List, error) {
	if !ai.UUID(id) {
		return ic.List{}, ic.ErrInvalid
	}
	tx, b, e := g.begin(ctx, a, false)
	if e != nil {
		return ic.List{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := g.capture(ctx, tx, a, &b, id, "")
	if e != nil {
		return ic.List{}, e
	}
	out := ic.List{Envelope: conversionEnvelope(a, b), Intent: c.intent.Item.Intent, Version: c.intent.Item.Version, Choices: []ic.Choice{}, Limit: 100, Explanation: "仅选择当前已报名且仍可见的活动；不会重新报名、邀请或公开意图。"}
	rows, e := tx.Query(ctx, `SELECT activity_id FROM activity_participations WHERE participant_account_id=$1 AND status='going' ORDER BY updated_at DESC,id LIMIT 101`, a.Actor.ID)
	if e != nil {
		return out, ic.ErrUnavailable
	}
	ids := []string{}
	for rows.Next() {
		var v string
		if e = rows.Scan(&v); e != nil {
			rows.Close()
			return out, ic.ErrUnavailable
		}
		ids = append(ids, v)
	}
	rows.Close()
	if rows.Err() != nil {
		return out, ic.ErrUnavailable
	}
	if len(ids) > 100 {
		out.Truncated = true
		ids = ids[:100]
	}
	for _, activity := range ids {
		v, e := g.capture(ctx, tx, a, &b, id, activity)
		if e == nil {
			out.Choices = append(out.Choices, v.choice)
		} else if errors.Is(e, ic.ErrUnavailable) {
			out.Explanation = "人数、粗区域或线上平台缺少可核验依据；请先明确意图约束，不按标题猜测。"
		}
	}
	if e = g.base.finish(ctx, tx, a, &b); e != nil {
		return ic.List{}, conversionError(e)
	}
	if e = g.current(ctx, tx, a, &b, c); e != nil {
		return ic.List{}, e
	}
	out.ObservedAt = b.now
	out.Revalidate = g.validation(a, b, c)
	return out, tx.Commit(ctx)
}
func (g *humanIntentConversions) PreviewOwn(ctx context.Context, a ic.Access, id string, in ic.Input) (ic.Preview, error) {
	if !ai.UUID(id) || !ai.UUID(in.ActivityID) || len(in.ExpectedVersion) != 64 {
		return ic.Preview{}, ic.ErrInvalid
	}
	tx, b, e := g.begin(ctx, a, false)
	if e != nil {
		return ic.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := g.capture(ctx, tx, a, &b, id, in.ActivityID)
	if e != nil {
		return ic.Preview{}, e
	}
	if c.intent.Item.Version != in.ExpectedVersion || (c.intent.Item.Intent.Status != socialintent.Active && c.intent.Item.Intent.Status != socialintent.Matched) {
		return ic.Preview{}, ic.ErrConflict
	}
	end := b.now.Add(ic.PreviewTTL)
	if c.plans.expires.Before(end) {
		end = c.plans.expires
	}
	c.plans.expires = end
	seal := conversionSeal{Owner: a.Actor.ID, Agent: b.agent, Authority: b.authority, IntentID: id, ActivityID: in.ActivityID, ParticipationID: c.choice.ParticipationID, IntentVersion: c.intent.Item.Version, RowProof: c.rowProof, SourceProof: c.sourceProof, PlansProof: c.plans.proof, ExpiresAt: end}
	data, e := json.Marshal(seal)
	if e != nil {
		return ic.Preview{}, ic.ErrUnavailable
	}
	nonce := make([]byte, g.base.cipher.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return ic.Preview{}, ic.ErrUnavailable
	}
	token := base64.RawURLEncoding.EncodeToString(g.base.cipher.Seal(nonce, nonce, data, []byte(ic.Schema)))
	if e = g.base.finish(ctx, tx, a, &b); e != nil {
		return ic.Preview{}, conversionError(e)
	}
	if e = g.current(ctx, tx, a, &b, c); e != nil {
		return ic.Preview{}, e
	}
	out := ic.Preview{Envelope: conversionEnvelope(a, b), PreviewID: token, Intent: c.intent.Item.Intent, Version: c.intent.Item.Version, Choice: c.choice, ExpiresAt: end, Explanation: "确认将这条寻找活动意图关联到原已报名活动并结束寻找；报名ID保持，不再次报名，不邀请、不公开、不通知其他人。"}
	out.Revalidate = g.validation(a, b, c)
	return out, tx.Commit(ctx)
}
func (g *humanIntentConversions) ApproveOwn(ctx context.Context, a ic.Access, id, token string) (ic.Receipt, error) {
	var seal conversionSeal
	data, e := base64.RawURLEncoding.DecodeString(token)
	n := g.base.cipher.NonceSize()
	if e != nil || len(data) < n || len(data) > 4096 {
		return ic.Receipt{}, ic.ErrInvalid
	}
	raw, e := g.base.cipher.Open(nil, data[:n], data[n:], []byte(ic.Schema))
	if e != nil || json.Unmarshal(raw, &seal) != nil || seal.Owner != a.Actor.ID || seal.IntentID != id {
		return ic.Receipt{}, ic.ErrDenied
	}
	tx, b, e := g.begin(ctx, a, true)
	if e != nil {
		return ic.Receipt{}, e
	}
	defer tx.Rollback(context.Background())
	if b.agent != seal.Agent || b.authority != seal.Authority || !b.now.Before(seal.ExpiresAt) {
		return ic.Receipt{}, ic.ErrConflict
	}
	// This lock order is Intent -> Activity -> original Participation; the
	// original Join/Cancel uses Activity -> Participation and never locks Intent.
	var locked string
	if e = tx.QueryRow(ctx, `SELECT id FROM social_intents WHERE id=$1 AND creator_account_id=$2 FOR UPDATE NOWAIT`, id, a.Actor.ID).Scan(&locked); e != nil {
		return ic.Receipt{}, ic.ErrConflict
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM activities WHERE id=$1 FOR UPDATE NOWAIT`, seal.ActivityID).Scan(&locked); e != nil {
		return ic.Receipt{}, ic.ErrConflict
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM activity_participations WHERE id=$1 AND activity_id=$2 AND participant_account_id=$3 AND status='going' FOR SHARE NOWAIT`, seal.ParticipationID, seal.ActivityID, a.Actor.ID).Scan(&locked); e != nil {
		return ic.Receipt{}, ic.ErrConflict
	}
	c, e := g.capture(ctx, tx, a, &b, id, seal.ActivityID)
	if e != nil {
		return ic.Receipt{}, e
	}
	var digest *string
	if e = tx.QueryRow(ctx, `SELECT conversion_preview_digest FROM social_intents WHERE id=$1`, id).Scan(&digest); e != nil {
		return ic.Receipt{}, ic.ErrUnavailable
	}
	repeat := c.intent.Item.Intent.Status == socialintent.Converted && digest != nil && *digest == activeHash([]byte(token)) && c.intent.Item.Intent.ConvertedActivityID != nil && *c.intent.Item.Intent.ConvertedActivityID == seal.ActivityID && c.intent.Item.Intent.ConvertedParticipationID != nil && *c.intent.Item.Intent.ConvertedParticipationID == seal.ParticipationID
	if !repeat && (c.intent.Item.Version != seal.IntentVersion || c.rowProof != seal.RowProof || c.sourceProof != seal.SourceProof || c.plans.proof != seal.PlansProof) {
		return ic.Receipt{}, ic.ErrConflict
	}
	if c.plans.expires.After(seal.ExpiresAt) {
		c.plans.expires = seal.ExpiresAt
	}
	if e = g.current(ctx, tx, a, &b, c); e != nil {
		return ic.Receipt{}, e
	}
	if !repeat {
		if c.intent.Item.Intent.Status == socialintent.Active {
			if !socialintent.CanTransition(socialintent.Active, socialintent.Matched) {
				return ic.Receipt{}, ic.ErrConflict
			}
			if _, e = tx.Exec(ctx, `UPDATE social_intents SET status='MATCHED',updated_at=clock_timestamp() WHERE id=$1 AND creator_account_id=$2 AND status='ACTIVE'`, id, a.Actor.ID); e != nil {
				return ic.Receipt{}, ic.ErrUnavailable
			}
		}
		if !socialintent.CanTransition(socialintent.Matched, socialintent.Converted) {
			return ic.Receipt{}, ic.ErrConflict
		}
		cmd, e := tx.Exec(ctx, `UPDATE social_intents SET status='CONVERTED',converted_activity_id=$3,converted_participation_id=$4,conversion_preview_digest=$5,converted_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 AND creator_account_id=$2 AND status='MATCHED'`, id, a.Actor.ID, seal.ActivityID, seal.ParticipationID, activeHash([]byte(token)))
		if e != nil || cmd.RowsAffected() != 1 {
			return ic.Receipt{}, ic.ErrConflict
		}
		if e = insertDomainAudit(ctx, tx, a.Actor.ID, "convert", "social_intent", id, "specific_existing_participation_approval", &seal.ParticipationID); e != nil {
			return ic.Receipt{}, ic.ErrUnavailable
		}
	}
	post, e := g.capture(ctx, tx, a, &b, id, seal.ActivityID)
	if e != nil {
		return ic.Receipt{}, e
	}
	post.plans.expires = c.plans.expires
	if post.intent.Item.Intent.Status != socialintent.Converted || post.intent.Item.Intent.ConvertedActivityID == nil || *post.intent.Item.Intent.ConvertedActivityID != seal.ActivityID || post.intent.Item.Intent.ConvertedParticipationID == nil || *post.intent.Item.Intent.ConvertedParticipationID != seal.ParticipationID {
		return ic.Receipt{}, ic.ErrConflict
	}
	if post.plans.proof != c.plans.proof || post.sourceProof != c.sourceProof {
		return ic.Receipt{}, ic.ErrConflict
	}
	if e = g.base.finish(ctx, tx, a, &b); e != nil {
		return ic.Receipt{}, conversionError(e)
	}
	if e = g.current(ctx, tx, a, &b, post); e != nil {
		return ic.Receipt{}, e
	}
	out := ic.Receipt{Envelope: conversionEnvelope(a, b), Intent: post.intent.Item.Intent, ActivityID: seal.ActivityID, ParticipationID: seal.ParticipationID, Committed: true, Explanation: "意图已关联原报名活动；报名、提醒和受众均未新增或扩大。"}
	out.Revalidate = g.validation(a, b, post)
	return out, tx.Commit(ctx)
}

func (g *humanIntentConversions) validation(a ic.Access, original activeBinding, c conversionCaptured) func(context.Context) error {
	return func(ctx context.Context) error {
		tx, b, e := g.begin(ctx, a, false)
		if e != nil {
			return e
		}
		defer tx.Rollback(context.Background())
		if b.agent != original.agent || b.authority != original.authority {
			return ic.ErrDenied
		}
		if e = g.base.finish(ctx, tx, a, &b); e != nil {
			return conversionError(e)
		}
		if e = g.current(ctx, tx, a, &b, c); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}
}
