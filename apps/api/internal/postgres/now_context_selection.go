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
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	ncs "github.com/birdtie/birdtie/apps/api/internal/nowcontextselection"
	"github.com/jackc/pgx/v5"
	"time"
)

type nowContextSelection struct {
	store  *Store
	cipher cipher.AEAD
}

var _ ncs.Gateway = (*nowContextSelection)(nil)

func NewNowContextSelection(s *Store) (ncs.Gateway, error) {
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return nil, e
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	return &nowContextSelection{s, a}, nil
}

type nowSelectionFrame struct {
	Agent, Authority, Frame string
	Items                   []ncs.Option
	Now, End                time.Time
	Truncated               bool
}
type nowSelectionToken struct {
	Kind, Owner, Agent, Authority, Frame, Body string
	ExpiresAt                                  time.Time
}

func nowSelectionHash(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func (g *nowContextSelection) seal(p nowSelectionToken) (string, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return "", ncs.ErrUnavailable
	}
	nonce := make([]byte, g.cipher.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", ncs.ErrUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(append(nonce, g.cipher.Seal(nil, nonce, b, []byte(ncs.Schema))...)), nil
}
func (g *nowContextSelection) open(s string) (nowSelectionToken, error) {
	var p nowSelectionToken
	if !ncs.Token(s) {
		return p, ncs.ErrInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil || len(b) < g.cipher.NonceSize() {
		return p, ncs.ErrConflict
	}
	raw, e := g.cipher.Open(nil, b[:g.cipher.NonceSize()], b[g.cipher.NonceSize():], []byte(ncs.Schema))
	if e != nil || json.Unmarshal(raw, &p) != nil {
		return p, ncs.ErrConflict
	}
	return p, nil
}

// The projection and its current authority/source frame share one snapshot and
// one native clock. No entire private source row is read or sealed.
const nowSelectionSQL = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n),
 auth AS(SELECT a.id owner,ag.id agent,se.id session,
 jsonb_build_array(a.id,a.xmin::text,ag.id,ag.xmin::text,ap.xmin::text,ap.profile_version,se.id,se.created_at,se.authentication_method,se.expires_at,encode(se.token_sha256,'hex')) authority,
 least(se.expires_at,se.idle_expires_at) deadline
 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions se ON se.account_id=a.id CROSS JOIN stamp
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND se.token_sha256=$2 AND se.revoked_at IS NULL AND se.expires_at>stamp.n AND se.idle_expires_at>stamp.n AND ($3::boolean OR se.authentication_method<>'dev_phone')),
 candidates AS(
 SELECT c.id cid,'CITY'::text kind,city.id cityid,city.name label,''::text relation,'DESTINATION'::text mode,false declared,'CITY'::text route,city.expires_at expiry,
 jsonb_build_array(c.xmin::text,city.xmin::text,cc.xmin::text) version
 FROM contexts c JOIN cities city ON city.id=c.city_id JOIN city_contexts cc ON cc.city_id=city.id CROSS JOIN stamp
 WHERE c.context_type='CITY' AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>stamp.n) AND cc.status='active'
 UNION ALL
 SELECT c.id,c.context_type,coalesce(c.city_id,''),
 CASE c.context_type WHEN 'CITY' THEN city.name WHEN 'ONLINE' THEN c.online_key WHEN 'INSTITUTION' THEN c.institution_key WHEN 'COUNTRY' THEN c.country_code WHEN 'COMMUNITY' THEN g.name END,
 pc.relation,CASE WHEN c.context_type='ONLINE' THEN 'ONLINE' ELSE upper(pc.relation) END,true,
 CASE WHEN c.context_type='CITY' THEN 'CITY' WHEN c.context_type='ONLINE' AND pc.visibility='private' AND pc.relation IN('interest','current','affiliation') THEN 'ONLINE' ELSE 'UNAVAILABLE' END,
 least(city.expires_at,g.expires_at,gcity.expires_at),
 jsonb_build_array(c.xmin::text,pc.xmin::text,pc.created_at,pc.visibility,city.xmin::text,cc.xmin::text,g.xmin::text,gcity.xmin::text,gowner.xmin::text)
 FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id LEFT JOIN cities city ON city.id=c.city_id LEFT JOIN city_contexts cc ON cc.city_id=c.city_id
 LEFT JOIN communities g ON g.id=c.community_id LEFT JOIN cities gcity ON gcity.id=g.city_id LEFT JOIN accounts gowner ON gowner.id=g.owner_account_id CROSS JOIN stamp
 WHERE pc.person_account_id=$1
 AND (c.context_type<>'CITY' OR (pc.relation IN('current','destination','past','home') AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>stamp.n) AND cc.status='active'))
 AND (c.context_type<>'COMMUNITY' OR (pc.relation='interest' AND g.visibility='public' AND g.publication_status='published' AND g.lifecycle_status='active' AND g.owner_confirmed_at IS NOT NULL AND (g.expires_at IS NULL OR g.expires_at>stamp.n) AND (g.city_id IS NULL OR (gcity.publication_status='published' AND (gcity.expires_at IS NULL OR gcity.expires_at>stamp.n))) AND gowner.status='active' AND NOT EXISTS(SELECT 1 FROM account_blocks bl WHERE (bl.blocker_account_id=$1 AND bl.blocked_account_id=g.owner_account_id) OR (bl.blocked_account_id=$1 AND bl.blocker_account_id=g.owner_account_id))))
 ), bounded AS(SELECT * FROM candidates ORDER BY declared DESC,mode,cid,relation LIMIT 101),
 projected AS(SELECT jsonb_build_object('contextId',cid,'contextType',kind,'cityId',cityid,'label',label,'relation',relation,'viewMode',mode,'declared',declared,'queryRoute',route) item,version,expiry FROM bounded)
 SELECT auth.agent::text,auth.authority,
 coalesce((SELECT jsonb_agg(jsonb_build_array(item,version) ORDER BY item::text) FROM projected),'[]'::jsonb),
 coalesce((SELECT jsonb_agg(item ORDER BY item::text) FROM projected),'[]'::jsonb),stamp.n,
 least(auth.deadline,(SELECT min(expiry) FROM projected)),(SELECT count(*)>100 FROM projected)
 FROM auth CROSS JOIN stamp`

func (g *nowContextSelection) begin(ctx context.Context, a ncs.Access) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || g == nil || g.store == nil || g.store.pool == nil || !a.Valid() {
		return nil, ncs.ErrDenied
	}
	tx, e := g.store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, ncs.ErrUnavailable
	}
	fail := func() (pgx.Tx, error) { _ = tx.Rollback(context.Background()); return nil, ncs.ErrUnavailable }
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';LOCK TABLE accounts,agents,agent_profiles,sessions,contexts,person_contexts,cities,city_contexts,communities,account_blocks IN ACCESS SHARE MODE`); e != nil {
		return fail()
	}
	return tx, nil
}
func (g *nowContextSelection) capture(ctx context.Context, tx pgx.Tx, a ncs.Access) (nowSelectionFrame, error) {
	var f nowSelectionFrame
	var authority, frame, items []byte
	e := tx.QueryRow(ctx, nowSelectionSQL, a.Actor.ID, a.Digest[:], g.store.devPhoneEnabled).Scan(&f.Agent, &authority, &frame, &items, &f.Now, &f.End, &f.Truncated)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, ncs.ErrDenied
	}
	if e != nil {
		return f, ncs.ErrUnavailable
	}
	f.Authority = nowSelectionHash(authority)
	f.Frame = nowSelectionHash(frame)
	if json.Unmarshal(items, &f.Items) != nil {
		return f, ncs.ErrUnavailable
	}
	if len(f.Items) > ncs.Limit {
		f.Items = f.Items[:ncs.Limit]
	}
	for i := range f.Items {
		b, _ := json.Marshal(f.Items[i])
		f.Items[i].OptionID = nowSelectionHash(b)
		if ncs.ValidateOption(f.Items[i]) != nil {
			return f, ncs.ErrUnavailable
		}
	}
	return f, nil
}
func (g *nowContextSelection) finish(ctx context.Context, tx pgx.Tx, a ncs.Access, prior nowSelectionFrame, end time.Time) (nowSelectionFrame, error) {
	var id string
	if e := tx.QueryRow(ctx, `SELECT id::text FROM sessions WHERE account_id=$1 AND token_sha256=$2 FOR SHARE`, a.Actor.ID, a.Digest[:]).Scan(&id); e != nil {
		return nowSelectionFrame{}, ncs.ErrDenied
	}
	current, e := g.capture(ctx, tx, a)
	if e != nil {
		return current, e
	}
	if current.Agent != prior.Agent || current.Authority != prior.Authority || current.Frame != prior.Frame || !current.Now.Before(end) {
		return current, ncs.ErrConflict
	}
	return current, nil
}
func nowSelectionEnvelope(a ncs.Access, f nowSelectionFrame, end time.Time) ncs.Envelope {
	return ncs.Envelope{SchemaVersion: ncs.Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: a.Actor.ID}, AgentID: f.Agent, ObservedAt: f.Now, ExpiresAt: end, ViewOnly: true}
}
func nowSelectionDeadline(f nowSelectionFrame) time.Time {
	end := f.Now.Add(ncs.TTL)
	if f.End.Before(end) {
		end = f.End
	}
	return end
}
func (g *nowContextSelection) tokenFor(a ncs.Access, f nowSelectionFrame, kind string, body any, end time.Time) (string, error) {
	var digest string
	if body != nil {
		raw, e := json.Marshal(body)
		if e != nil {
			return "", ncs.ErrUnavailable
		}
		digest = nowSelectionHash(raw)
	}
	return g.seal(nowSelectionToken{kind, a.Actor.ID, f.Agent, f.Authority, f.Frame, digest, end})
}
func (g *nowContextSelection) check(ctx context.Context, tx pgx.Tx, a ncs.Access, p nowSelectionToken) (nowSelectionFrame, error) {
	f, e := g.capture(ctx, tx, a)
	if e != nil {
		return f, e
	}
	if p.Owner != a.Actor.ID || p.Agent != f.Agent || p.Authority != f.Authority || p.Frame != f.Frame || !f.Now.Before(p.ExpiresAt) {
		return f, ncs.ErrConflict
	}
	return g.finish(ctx, tx, a, f, p.ExpiresAt)
}
func (g *nowContextSelection) ReadOptions(ctx context.Context, a ncs.Access) (ncs.OptionsReceipt, error) {
	var out ncs.OptionsReceipt
	tx, e := g.begin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := g.capture(ctx, tx, a)
	if e != nil {
		return out, e
	}
	end := nowSelectionDeadline(f)
	f, e = g.finish(ctx, tx, a, f, end)
	if e != nil {
		return out, e
	}
	token, e := g.tokenFor(a, f, "OPTIONS", nil, end)
	if e != nil {
		return out, e
	}
	out.Response = ncs.Options{Envelope: nowSelectionEnvelope(a, f, end), OptionsToken: token, Items: f.Items, Limit: ncs.Limit, Truncated: f.Truncated}
	out.Proof, e = g.tokenFor(a, f, "OPTIONS_RECEIPT", out.Response, end)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ncs.OptionsReceipt{}, ncs.ErrUnavailable
	}
	return out, nil
}
func (g *nowContextSelection) Resolve(ctx context.Context, a ncs.Access, in ncs.Input) (ncs.SelectionReceipt, error) {
	var out ncs.SelectionReceipt
	if ncs.ValidateInput(in) != nil {
		return out, ncs.ErrInvalid
	}
	p, e := g.open(in.OptionsToken)
	if e != nil {
		return out, e
	}
	if p.Kind != "OPTIONS" {
		return out, ncs.ErrConflict
	}
	tx, e := g.begin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := g.check(ctx, tx, a, p)
	if e != nil {
		return out, e
	}
	var choice *ncs.Option
	for _, v := range f.Items {
		if v.OptionID == in.OptionID {
			x := v
			choice = &x
		}
	}
	if choice == nil {
		return out, ncs.ErrInvalid
	}
	out.Response = ncs.Selection{Envelope: nowSelectionEnvelope(a, f, p.ExpiresAt), Choice: *choice}
	out.Proof, e = g.tokenFor(a, f, "SELECTION_RECEIPT", out.Response, p.ExpiresAt)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return ncs.SelectionReceipt{}, ncs.ErrUnavailable
	}
	return out, nil
}
func (g *nowContextSelection) revalidate(ctx context.Context, a ncs.Access, kind string, body any, proof string) error {
	p, e := g.open(proof)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(body)
	if e != nil || p.Kind != kind || p.Body != nowSelectionHash(raw) {
		return ncs.ErrDenied
	}
	tx, e := g.begin(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	if _, e = g.check(ctx, tx, a, p); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return ncs.ErrUnavailable
	}
	return nil
}
func (g *nowContextSelection) RevalidateOptions(ctx context.Context, a ncs.Access, v ncs.OptionsReceipt) error {
	return g.revalidate(ctx, a, "OPTIONS_RECEIPT", v.Response, v.Proof)
}
func (g *nowContextSelection) RevalidateSelection(ctx context.Context, a ncs.Access, v ncs.SelectionReceipt) error {
	return g.revalidate(ctx, a, "SELECTION_RECEIPT", v.Response, v.Proof)
}
