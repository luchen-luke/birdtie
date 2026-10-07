package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	cg "github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/jackc/pgx/v5"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"
)

var _ cg.CommunityInterestStore = (*Store)(nil)

// Process-only key. Never derive a signing key from a client bearer/session digest.
// A new OS process creates a different key; a new Store in the same process is not a restart.
var interestProcess struct {
	once sync.Once
	key  [32]byte
	err  error
}

func interestAEAD() (cipher.AEAD, error) {
	interestProcess.once.Do(func() { _, interestProcess.err = io.ReadFull(rand.Reader, interestProcess.key[:]) })
	if interestProcess.err != nil {
		return nil, cg.ErrInterestUnavailable
	}
	block, e := aes.NewCipher(interestProcess.key[:])
	if e != nil {
		return nil, cg.ErrInterestUnavailable
	}
	return cipher.NewGCM(block)
}

const interestAAD = "BIRDTIE_HUMAN_COMMUNITY_INTEREST_PREVIEW_V1"

type interestFrame struct {
	Authority, ContextToken, StatementToken string
	Epoch                                   int64
	Record                                  cg.CommunityInterestRecord
	Observed, ReadEnd                       time.Time
}
type interestSealed struct {
	Version         string
	Owner           string
	Input           cg.CommunityInterestInput
	Frame           interestFrame
	Issued, Expires time.Time
}

func sealInterest(p interestSealed) (string, error) {
	a, e := interestAEAD()
	if e != nil {
		return "", e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return "", cg.ErrInterestUnavailable
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return "", cg.ErrInterestUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(a.Seal(nonce, nonce, raw, []byte(interestAAD))), nil
}
func openInterest(token string) (interestSealed, error) {
	var p interestSealed
	a, e := interestAEAD()
	if e != nil {
		return p, e
	}
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || len(raw) < a.NonceSize()+a.Overhead() || len(raw) > 18000 {
		return p, cg.ErrInterestChanged
	}
	raw, e = a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(interestAAD))
	if e != nil || json.Unmarshal(raw, &p) != nil || p.Version != interestAAD || cg.ValidateCommunityInterestInput(p.Input) != nil {
		return p, cg.ErrInterestChanged
	}
	return p, nil
}
func interestError(e error) error {
	if e == nil {
		return nil
	}
	for _, x := range []error{cg.ErrInterestDenied, cg.ErrInterestInvalid, cg.ErrInterestChanged, cg.ErrInterestUnavailable} {
		if errors.Is(e, x) {
			return x
		}
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return cg.ErrInterestDenied
	}
	return cg.ErrInterestUnavailable
}
func (s *Store) beginInterest(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, cg.ErrInterestUnavailable
	}
	if agentprofile.ValidatePrivateAccess(a) != nil {
		return nil, cg.ErrInterestDenied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, cg.ErrInterestUnavailable
	}
	fail := func(e error) (pgx.Tx, error) { tx.Rollback(context.Background()); return nil, interestError(e) }
	// Acquire all relation locks before any final wall-clock/current capture. No late relation wait can follow it.
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC';LOCK TABLE accounts,agents,agent_profiles,sessions,communities,cities,contexts,person_contexts,audit_events IN ACCESS SHARE MODE`); e != nil {
		return fail(e)
	}
	if write {
		if _, e = tx.Exec(ctx, `LOCK TABLE contexts,person_contexts,audit_events IN ROW EXCLUSIVE MODE`); e != nil {
			return fail(e)
		}
	}
	var protected bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='audit_events'::regclass AND tgname='community_interest_audit_guard' AND tgtype=31 AND tgenabled IN ('O','A') AND tgfoid=to_regprocedure('birdtie_community_interest_audit_guard()')) AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='audit_events'::regclass AND tgname='community_interest_audit_truncate_guard' AND tgtype=34 AND tgenabled IN ('O','A') AND tgfoid=to_regprocedure('birdtie_community_interest_audit_guard()')) AND to_regclass('community_interest_audit_epoch') IS NOT NULL`).Scan(&protected); e != nil || !protected {
		return fail(cg.ErrInterestUnavailable)
	}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`+lock, a.WorkspacePrincipal.ID).Scan(&id); e != nil {
		return fail(e)
	}
	if e = tx.QueryRow(ctx, `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active' FOR SHARE`, id).Scan(&id); e != nil {
		return fail(e)
	}
	if e = tx.QueryRow(ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON' FOR SHARE`, id, a.WorkspacePrincipal.ID).Scan(&id); e != nil {
		return fail(e)
	}
	return tx, nil
}

const interestPublicSQL = `g.visibility='public' AND g.publication_status='published' AND g.lifecycle_status='active' AND g.owner_confirmed_at IS NOT NULL AND (g.expires_at IS NULL OR g.expires_at>cl.at) AND (g.city_id IS NULL OR (city.id IS NOT NULL AND city.publication_status='published' AND (city.expires_at IS NULL OR city.expires_at>cl.at)))`
const interestFrameSQL = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT
 jsonb_build_array(a.id,a.xmin::text,ag.id,ag.xmin::text,ap.xmin::text,ss.id,ss.created_at,ss.authentication_method,ss.expires_at,encode(ss.token_sha256,'hex'),
 g.id,g.xmin::text,g.city_id,g.visibility,g.publication_status,g.lifecycle_status,g.owner_confirmed_at,g.expires_at,city.id,city.xmin::text,city.publication_status,city.expires_at)::text,
 coalesce(c.id::text||':'||c.xmin::text,''),coalesce(pc.xmin::text,''),
 coalesce((SELECT max(id) FROM audit_events WHERE actor_account_id=a.id AND resource_type='person_community_declaration' AND purpose='HUMAN_COMMUNITY_INTEREST_DECLARATION' AND resource_id=$3::text||':interest'),0),
 g.id,coalesce(c.id::text,''),CASE WHEN ` + interestPublicSQL + ` THEN g.name ELSE '已不可公开展示的社群声明' END,(` + interestPublicSQL + `),coalesce(upper(pc.visibility),'ABSENT'),cl.at,
 least(ss.expires_at,ss.idle_expires_at,CASE WHEN ` + interestPublicSQL + ` THEN g.expires_at ELSE NULL END,CASE WHEN ` + interestPublicSQL + ` THEN city.expires_at ELSE NULL END)
 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.account_id=a.id AND ss.token_sha256=$2 AND ss.revoked_at IS NULL JOIN communities g ON g.id=$3::uuid LEFT JOIN cities city ON city.id=g.city_id
 LEFT JOIN contexts c ON c.context_type='COMMUNITY' AND c.community_id=g.id LEFT JOIN person_contexts pc ON pc.person_account_id=a.id AND pc.context_id=c.id AND pc.relation='interest' CROSS JOIN cl
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($4::boolean OR ss.authentication_method<>'dev_phone')`

func (s *Store) interestCapture(ctx context.Context, tx pgx.Tx, a agentprofile.PrivateAccess, communityID string, locks bool) (interestFrame, error) {
	var out interestFrame
	if locks {
		var id string
		if e := tx.QueryRow(ctx, `SELECT id FROM communities WHERE id=$1 FOR SHARE`, communityID).Scan(&id); e != nil {
			return out, interestError(e)
		}
		// Lock actual city, context and prior row without manufacturing an absent node.
		rows, e := tx.Query(ctx, `SELECT city.id FROM cities city JOIN communities g ON g.city_id=city.id WHERE g.id=$1 FOR SHARE OF city`, communityID)
		if e != nil {
			return out, interestError(e)
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, interestError(e)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, interestError(e)
		}
		rows, e = tx.Query(ctx, `SELECT c.id FROM contexts c WHERE c.context_type='COMMUNITY' AND c.community_id=$1 FOR SHARE`, communityID)
		if e != nil {
			return out, interestError(e)
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, interestError(e)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, interestError(e)
		}
		rows, e = tx.Query(ctx, `SELECT pc.context_id FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id WHERE pc.person_account_id=$1 AND c.community_id=$2 AND c.context_type='COMMUNITY' AND pc.relation='interest' FOR SHARE OF pc`, a.WorkspacePrincipal.ID, communityID)
		if e != nil {
			return out, interestError(e)
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, interestError(e)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, interestError(e)
		}
		if e = tx.QueryRow(ctx, `SELECT id FROM sessions WHERE account_id=$1 AND token_sha256=$2 FOR SHARE`, a.WorkspacePrincipal.ID, a.SessionDigest[:]).Scan(&id); e != nil {
			return out, interestError(e)
		}
	}
	e := tx.QueryRow(ctx, interestFrameSQL, a.WorkspacePrincipal.ID, a.SessionDigest[:], communityID, s.devPhoneEnabled).Scan(&out.Authority, &out.ContextToken, &out.StatementToken, &out.Epoch, &out.Record.CommunityID, &out.Record.ContextID, &out.Record.Name, &out.Record.SourceAvailable, &out.Record.State, &out.Observed, &out.ReadEnd)
	out.Record.Relation = "interest"
	return out, interestError(e)
}
func interestSame(a, b interestFrame) bool {
	return a.Authority == b.Authority && a.ContextToken == b.ContextToken && a.StatementToken == b.StatementToken && a.Epoch == b.Epoch && reflect.DeepEqual(a.Record, b.Record)
}
func interestAllowed(f interestFrame, in cg.CommunityInterestInput) bool {
	if (in.Operation == "PUBLIC" || f.Record.State == "ABSENT") && !f.Record.SourceAvailable {
		return false
	}
	if in.Operation == "DELETE" {
		return f.Record.State != "ABSENT"
	}
	return f.Record.State != in.Operation
}
func interestConsequence(op string) string {
	switch op {
	case "PUBLIC":
		return "公开我对这个社群的兴趣声明，可作为共同社群声明依据，持续至我撤回；不代表成员资格，不会加入、通知、引荐或开启模型权限。"
	case "PRIVATE":
		return "仅自己可见，不再作为共同公开社群依据；不会退出社群或修改成员身份。"
	default:
		return "移除我的兴趣声明，不会退出社群、发送消息或修改成员身份。"
	}
}
func (s *Store) PreviewOwnCommunityInterest(ctx context.Context, a agentprofile.PrivateAccess, in cg.CommunityInterestInput) (cg.CommunityInterestPreview, error) {
	var out cg.CommunityInterestPreview
	if cg.ValidateCommunityInterestInput(in) != nil {
		return out, cg.ErrInterestInvalid
	}
	in.CommunityID = strings.ToLower(in.CommunityID)
	tx, e := s.beginInterest(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := s.interestCapture(ctx, tx, a, in.CommunityID, true)
	if e != nil {
		return out, e
	}
	if !interestAllowed(f, in) {
		return out, cg.ErrInterestChanged
	}
	expires := f.Observed.Add(90 * time.Second)
	if f.ReadEnd.Before(expires) {
		expires = f.ReadEnd
	}
	p := interestSealed{Version: interestAAD, Owner: a.WorkspacePrincipal.ID, Input: in, Frame: f, Issued: f.Observed, Expires: expires}
	token, e := sealInterest(p)
	if e != nil {
		return out, e
	}
	last, e := s.interestCapture(ctx, tx, a, in.CommunityID, false)
	if e != nil {
		return out, e
	}
	if !interestSame(f, last) || !last.Observed.Before(expires) {
		return out, cg.ErrInterestChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return out, cg.ErrInterestUnavailable
	}
	target := in.Operation
	if target == "DELETE" {
		target = "ABSENT"
	}
	return cg.CommunityInterestPreview{SchemaVersion: cg.CommunityInterestSchema, OwnerID: p.Owner, AgentID: interestAgentFromAuthority(f.Authority), CommunityInterestRecord: f.Record, Operation: in.Operation, TargetState: target, Preview: token, ObservedAt: f.Observed, ExpiresAt: expires, Consequence: interestConsequence(in.Operation)}, nil
}
func interestAgentFromAuthority(raw string) string {
	var v []json.RawMessage
	if json.Unmarshal([]byte(raw), &v) != nil || len(v) < 3 {
		return ""
	}
	var s string
	json.Unmarshal(v[2], &s)
	return s
}
func interestView(a agentprofile.PrivateAccess, agent string, now time.Time) cg.CommunityInterestView {
	return cg.CommunityInterestView{Limit: 100, SchemaVersion: cg.CommunityInterestSchema, OwnerID: a.WorkspacePrincipal.ID, AgentID: agent, ObservedAt: now, Records: []cg.CommunityInterestRecord{}, Options: []cg.CommunityInterestOption{}}
}
func (s *Store) ApproveOwnCommunityInterest(ctx context.Context, a agentprofile.PrivateAccess, token string) (cg.CommunityInterestView, error) {
	var out cg.CommunityInterestView
	p, e := openInterest(token)
	if e != nil {
		return out, e
	}
	if p.Owner != a.WorkspacePrincipal.ID {
		return out, cg.ErrInterestDenied
	}
	tx, e := s.beginInterest(ctx, a, true)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	f, e := s.interestCapture(ctx, tx, a, p.Input.CommunityID, true)
	if e != nil {
		return out, e
	}
	if !interestSame(p.Frame, f) || !f.Observed.Before(p.Expires) || f.Observed.Before(p.Issued) || !interestAllowed(f, p.Input) {
		return out, cg.ErrInterestChanged
	}
	id := f.Record.ContextID
	if p.Input.Operation == "DELETE" {
		_, e = tx.Exec(ctx, `DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=$2 AND relation='interest'`, p.Owner, id)
	} else {
		if id == "" {
			if _, e = tx.Exec(ctx, `INSERT INTO contexts(context_type,community_id) VALUES('COMMUNITY',$1) ON CONFLICT DO NOTHING`, p.Input.CommunityID); e != nil {
				return out, interestError(e)
			}
			if e = tx.QueryRow(ctx, `SELECT id FROM contexts WHERE context_type='COMMUNITY' AND community_id=$1 FOR SHARE`, p.Input.CommunityID).Scan(&id); e != nil {
				return out, interestError(e)
			}
		}
		_, e = tx.Exec(ctx, `INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'interest',$3) ON CONFLICT(person_account_id,context_id,relation) DO UPDATE SET visibility=EXCLUDED.visibility`, p.Owner, id, strings.ToLower(p.Input.Operation))
	}
	if e != nil {
		return out, interestError(e)
	}
	var auditID int64
	e = auditQueryRow(ctx, tx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'person_community_declaration',$3||':interest','allowed','HUMAN_COMMUNITY_INTEREST_DECLARATION') RETURNING id`, p.Owner, p.Input.Operation, p.Input.CommunityID).Scan(&auditID)
	if e != nil {
		return out, interestError(e)
	}
	// Audit inserts can wait. Current SQL is deliberately after that final possible write wait.
	last, e := s.interestCapture(ctx, tx, a, p.Input.CommunityID, false)
	if e != nil {
		return out, e
	}
	desired := p.Input.Operation
	if desired == "DELETE" {
		desired = "ABSENT"
	}
	if last.Authority != p.Frame.Authority || last.Epoch != auditID || last.Record.State != desired || !last.Observed.Before(p.Expires) || (p.Input.Operation == "PUBLIC" && !last.Record.SourceAvailable) {
		return out, cg.ErrInterestChanged
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return out, cg.ErrInterestUnavailable
	}
	out = interestView(a, interestAgentFromAuthority(last.Authority), last.Observed)
	out.Records = append(out.Records, last.Record)
	return out, nil
}
func (s *Store) ReadOwnCommunityInterests(ctx context.Context, a agentprofile.PrivateAccess, options bool) (cg.CommunityInterestView, error) {
	var out cg.CommunityInterestView
	tx, e := s.beginInterest(ctx, a, false)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	// Bounded current IDs only; options never creates contexts. Owner statements can be removed even after source publication disappears.
	q := `SELECT g.id FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id AND c.context_type='COMMUNITY' JOIN communities g ON g.id=c.community_id WHERE pc.person_account_id=$1 AND pc.relation='interest' ORDER BY g.id LIMIT 101`
	if options {
		q = `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT g.id FROM communities g LEFT JOIN cities city ON city.id=g.city_id CROSS JOIN cl WHERE ` + interestPublicSQL + ` ORDER BY g.name,g.id LIMIT 101`
	}
	var rows pgx.Rows
	if options {
		rows, e = tx.Query(ctx, q)
	} else {
		rows, e = tx.Query(ctx, q, a.WorkspacePrincipal.ID)
	}
	if e != nil {
		return out, interestError(e)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, interestError(e)
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, interestError(e)
	}
	var agent, session string
	if e = tx.QueryRow(ctx, `SELECT ag.id,ss.id FROM agents ag JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=$1 AND ap.owner_type='PERSON' JOIN sessions ss ON ss.account_id=$1 WHERE ag.principal_account_id=$1 AND ag.agent_type='personal' AND ag.status='active' AND ss.token_sha256=$2 FOR SHARE OF ss`, a.WorkspacePrincipal.ID, a.SessionDigest[:]).Scan(&agent, &session); e != nil {
		return out, interestError(e)
	}
	out = interestView(a, agent, time.Time{})
	if len(ids) > 100 {
		out.Truncated = true
		ids = ids[:100]
	}
	first := []interestFrame{}
	readDeadline := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range ids {
		f, e := s.interestCapture(ctx, tx, a, id, true)
		if e != nil {
			return out, e
		}
		first = append(first, f)
		if f.ReadEnd.Before(readDeadline) {
			readDeadline = f.ReadEnd
		}
	}
	// Re-capture ALL loaded rows after all row waits. No earlier expiry/body is released.
	for n, id := range ids {
		f, e := s.interestCapture(ctx, tx, a, id, false)
		if e != nil {
			return out, e
		}
		if !interestSame(first[n], f) {
			return out, cg.ErrInterestChanged
		}
		if options {
			if !f.Record.SourceAvailable {
				return out, cg.ErrInterestChanged
			}
			out.Options = append(out.Options, cg.CommunityInterestOption{CommunityID: id, Name: f.Record.Name})
		} else {
			out.Records = append(out.Records, f.Record)
		}
	}
	// One final no-wait SQL verifies current identity/clock and every previously loaded source expiry.
	raw, _ := json.Marshal(ids)
	e = tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at) SELECT cl.at FROM cl WHERE cl.at<$8::timestamptz AND EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' JOIN sessions ss ON ss.account_id=a.id WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ag.id=$3 AND ss.id=$4 AND ss.token_sha256=$2 AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND ($5::boolean OR ss.authentication_method<>'dev_phone')) AND (NOT $6::boolean OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text($7::jsonb) selected(id) LEFT JOIN communities g ON g.id::text=selected.id LEFT JOIN cities city ON city.id=g.city_id WHERE g.id IS NULL OR NOT (`+interestPublicSQL+`)))`, a.WorkspacePrincipal.ID, a.SessionDigest[:], agent, session, s.devPhoneEnabled, options, raw, readDeadline).Scan(&out.ObservedAt)
	if e != nil {
		return out, interestError(e)
	}
	if e = tx.Commit(ctx); e != nil || ctx.Err() != nil {
		return out, cg.ErrInterestUnavailable
	}
	return out, nil
}
