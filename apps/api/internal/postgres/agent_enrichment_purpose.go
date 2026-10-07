package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"time"
)

var _ aep.Store = (*Store)(nil)

type enrichmentCapture struct {
	authority, source, task string
	review                  aep.Review
	at, end                 time.Time
}

func enrichmentError(e error) error {
	if errors.Is(e, pgx.ErrNoRows) || errors.Is(e, acb.ErrDenied) {
		return aep.ErrDenied
	}
	return aep.ErrUnavailable
}

const enrichmentGuardSQL = `SELECT to_regclass('public.agent_enrichment_purpose_previews') IS NOT NULL
 AND to_regclass('public.agent_enrichment_purpose_bindings') IS NOT NULL
 AND (SELECT count(*)=3 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='public' AND NOT t.tgisinternal AND t.tgenabled='O' AND
   ((c.relname='agent_enrichment_purpose_previews' AND t.tgname='agent_enrichment_preview_guard' AND t.tgfoid=to_regprocedure('public.birdtie_enrichment_preview_guard()'))
    OR(c.relname='agent_enrichment_purpose_bindings' AND t.tgname='agent_enrichment_binding_guard' AND t.tgfoid=to_regprocedure('public.birdtie_enrichment_binding_guard()'))
    OR(c.relname='consent_grants' AND t.tgname='agent_enrichment_grant_guard' AND t.tgfoid=to_regprocedure('public.birdtie_enrichment_grant_guard()'))))`

func (s *Store) beginEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, contextBuilderBinding, error) {
	tx, b, e := s.beginContextBuilder(ctx, a)
	if e != nil {
		return nil, b, enrichmentError(e)
	}
	fail := func(e error) (pgx.Tx, contextBuilderBinding, error) {
		tx.Rollback(context.Background())
		return nil, contextBuilderBinding{}, e
	}
	var installed bool
	if tx.QueryRow(ctx, enrichmentGuardSQL).Scan(&installed) != nil || !installed {
		return fail(aep.ErrUnavailable)
	}
	// Relation waits happen before source capture. Write locks are taken up front,
	// not silently upgraded after the final permission/clock check.
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_enrichment_purpose_previews,agent_enrichment_purpose_bindings,consent_grants,audit_events IN `+map[bool]string{true: "ROW EXCLUSIVE", false: "ACCESS SHARE"}[write]+` MODE`); e != nil {
		return fail(aep.ErrUnavailable)
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE accounts,agents,agent_profiles,sessions,agent_tasks,moments,cities,contexts,city_contexts IN ACCESS SHARE MODE`); e != nil {
		return fail(aep.ErrUnavailable)
	}
	// The initial gate may itself have waited while a migration/guard changed.
	if tx.QueryRow(ctx, enrichmentGuardSQL).Scan(&installed) != nil || !installed {
		return fail(aep.ErrUnavailable)
	}
	return tx, b, nil
}

const enrichmentCaptureSQL = `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT encode(sha256(convert_to(jsonb_build_object('owner',a.id,'ownerRow',a.xmin::text,'agent',ag.id,'agentRow',ag.xmin::text,
 'metadataRow',ap.xmin::text,'metadataVersion',ap.profile_version,'session',se.id,'created',se.created_at,'method',se.authentication_method,
 'absoluteExpiry',se.expires_at,'digest',encode(se.token_sha256,'hex'))::text,'UTF8')),'hex'),
 encode(sha256(convert_to(jsonb_build_object('moment',m.id,'row',m.xmin::text,'revision',m.revision,'updated',m.updated_at,
 'selected',(CASE WHEN 'title'=ANY($6::text[]) THEN jsonb_build_object('title',m.title) ELSE '{}'::jsonb END)||
 (CASE WHEN 'body'=ANY($6::text[]) THEN jsonb_build_object('body',m.body) ELSE '{}'::jsonb END))::text,'UTF8')),'hex'),
 encode(sha256(convert_to(jsonb_build_object('task',t.id,'row',t.xmin::text,'updated',t.updated_at,'context',cx.id,'contextRow',cx.xmin::text,
 'cityRow',c.xmin::text,'cityStateRow',cc.xmin::text,'query',encode(sha256(convert_to(COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),'UTF8')),'hex'))::text,'UTF8')),'hex'),
 COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query),t.updated_at,
 (CASE WHEN 'title'=ANY($6::text[]) THEN jsonb_build_object('title',m.title) ELSE '{}'::jsonb END)||
 (CASE WHEN 'body'=ANY($6::text[]) THEN jsonb_build_object('body',m.body) ELSE '{}'::jsonb END),
 clk.at,least($7::timestamptz,se.expires_at,se.idle_expires_at,m.updated_at+interval '15 minutes',c.expires_at)
 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions se ON se.account_id=a.id
 JOIN agent_tasks t ON t.owner_account_id=a.id
 JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=t.city_context_id
 JOIN cities c ON c.id=cx.city_id JOIN city_contexts cc ON cc.city_id=c.id
 JOIN moments m ON m.author_account_id=a.id CROSS JOIN clk
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active'
 AND ag.id=$2 AND ag.agent_type='personal' AND ag.status='active' AND ap.profile_version>0
 AND ag.created_at<=clk.at AND ap.updated_at<=clk.at
 AND se.id=$3 AND se.revoked_at IS NULL AND se.expires_at>clk.at AND se.idle_expires_at>clk.at
 AND t.id=$4 AND t.principal_type='person' AND t.acting_user_account_id=a.id AND t.context_type='CITY' AND t.status='ACTIVE' AND t.updated_at<=clk.at
 AND COALESCE(NULLIF(t.filters->>'currentQuery',''),t.query)<>''
 AND c.publication_status='published' AND cc.status='active' AND (c.expires_at IS NULL OR c.expires_at>clk.at)
 AND m.id=$5 AND m.visibility='private' AND m.status='draft' AND m.revision=$8
 AND m.updated_at<=clk.at AND ($9::boolean OR se.authentication_method<>'dev_phone')
 AND ($10::text='' OR EXISTS(SELECT 1 FROM consent_grants g JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id
  WHERE g.id=$10::uuid AND g.owner_account_id=a.id AND g.recipient_account_id=a.id AND g.revision=$11
   AND g.resource_type='agent_context' AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.actions=ARRAY['analyze_local']::text[]
   AND g.revoked_at IS NULL AND g.expires_at>clk.at AND eb.preview_id::text=g.resource_id))`

func (s *Store) captureEnrichment(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, sel aep.Selection) (enrichmentCapture, error) {
	return s.captureEnrichmentGrant(ctx, tx, b, sel, "", 0)
}
func (s *Store) captureEnrichmentGrant(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, sel aep.Selection, grant string, revision int64) (enrichmentCapture, error) {
	var c enrichmentCapture
	var now time.Time
	if tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
		return c, aep.ErrUnavailable
	}
	if !sel.DeadlineAt.After(now) || sel.DeadlineAt.Sub(now) > aep.MaxDeadline {
		return c, aep.ErrExpired
	}
	// Resolve row waits first; the clock in the subsequent statement is fresh.
	var ignored string
	for _, q := range []struct {
		sql string
		id  string
	}{
		{`SELECT id::text FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, sel.TaskID},
		{`SELECT id::text FROM moments WHERE id=$1 AND author_account_id=$2 FOR SHARE`, sel.MomentID},
	} {
		if e := tx.QueryRow(ctx, q.sql, q.id, b.owner).Scan(&ignored); e != nil {
			return c, enrichmentError(e)
		}
	}
	if e := tx.QueryRow(ctx, `SELECT c.id FROM agent_tasks t JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY'
 JOIN cities c ON c.id=cx.city_id JOIN city_contexts cc ON cc.city_id=c.id WHERE t.id=$1 AND t.owner_account_id=$2 FOR SHARE OF c,cx,cc`, sel.TaskID, b.owner).Scan(&ignored); e != nil {
		return c, enrichmentError(e)
	}
	var content []byte
	e := tx.QueryRow(ctx, enrichmentCaptureSQL, b.owner, b.agent, b.session, sel.TaskID, sel.MomentID, sel.Fields, sel.DeadlineAt, sel.MomentRevision, s.devPhoneEnabled, grant, revision).Scan(&c.authority, &c.source, &c.task, &c.review.TaskQuery, &c.review.TaskUpdatedAt, &content, &c.at, &c.end)
	if e != nil {
		return enrichmentCapture{}, enrichmentError(e)
	}
	c.at = c.at.UTC()
	c.end = c.end.UTC()
	c.review.TaskUpdatedAt = c.review.TaskUpdatedAt.UTC()
	if !c.end.After(c.at) {
		return enrichmentCapture{}, aep.ErrExpired
	}
	if json.Unmarshal(content, &c.review.Content) != nil {
		return enrichmentCapture{}, aep.ErrUnavailable
	}
	return c, nil
}
func sameEnrichment(a, b enrichmentCapture) bool {
	return a.authority == b.authority && a.source == b.source && a.task == b.task
}

type enrichmentStored struct {
	id, owner, agent, session, authority, source, task string
	selection                                          aep.Selection
	observed, end                                      time.Time
}

func enrichmentPreviewTx(ctx context.Context, tx pgx.Tx, id string) (enrichmentStored, error) {
	var p enrichmentStored
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT id,owner_id,agent_id,session_id,selection,authority,source_binding,task_binding,observed_at,expires_at FROM agent_enrichment_purpose_previews WHERE id=$1 FOR SHARE`, id).Scan(&p.id, &p.owner, &p.agent, &p.session, &raw, &p.authority, &p.source, &p.task, &p.observed, &p.end)
	if e != nil {
		return p, enrichmentError(e)
	}
	if json.Unmarshal(raw, &p.selection) != nil {
		return p, aep.ErrUnavailable
	}
	p.selection, e = aep.Normalize(p.selection)
	return p, e
}
func matchEnrichment(p enrichmentStored, c enrichmentCapture) bool {
	return p.authority == c.authority && p.source == c.source && p.task == c.task
}
func enrichmentPreviewDTO(p enrichmentStored, c enrichmentCapture) aep.Preview {
	return aep.Preview{State: "CURRENT_REVIEW", SchemaVersion: aep.Schema, ID: p.id, Purpose: aep.Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: p.owner}, AgentID: p.agent, Selection: p.selection, Review: c.review, ObservedAt: c.at, ExpiresAt: p.end.UTC(), Explanation: "仅在当前任务中本地分析所选 Moment 字段；不发送给模型，不保存候选或长期记忆，不代表已核实偏好。"}
}
func (s *Store) PreviewOwnEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, sel aep.Selection) (aep.Preview, error) {
	sel, e := aep.Normalize(sel)
	if e != nil {
		return aep.Preview{}, e
	}
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, true)
	if e != nil {
		return aep.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := s.captureEnrichment(ctx, tx, b, sel)
	if e != nil {
		return aep.Preview{}, e
	}
	end := c.end
	contextPurposeLimit(&end, c.at.Add(aep.PreviewTTL))
	contextPurposeLimit(&end, b.limit)
	raw, _ := json.Marshal(sel)
	p := enrichmentStored{owner: b.owner, agent: b.agent, session: b.session, selection: sel, authority: c.authority, source: c.source, task: c.task, observed: c.at, end: end}
	if e = tx.QueryRow(ctx, `INSERT INTO agent_enrichment_purpose_previews(id,owner_id,agent_id,session_id,task_id,moment_id,selection,authority,source_binding,task_binding,observed_at,expires_at)
 VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, b.owner, b.agent, b.session, sel.TaskID, sel.MomentID, raw, c.authority, c.source, c.task, c.at, end).Scan(&p.id); e != nil {
		return aep.Preview{}, aep.ErrUnavailable
	}
	last, e := s.captureEnrichment(ctx, tx, b, sel)
	if e != nil {
		return aep.Preview{}, e
	}
	if !sameEnrichment(c, last) || !end.After(last.at) {
		return aep.Preview{}, aep.ErrConflict
	}
	out := enrichmentPreviewDTO(p, last)
	if aep.ValidatePreview(out) != nil {
		return aep.Preview{}, aep.ErrUnavailable
	}
	if tx.Commit(ctx) != nil {
		return aep.Preview{}, aep.ErrUnavailable
	}
	return out, nil
}
func (s *Store) ReadOwnEnrichmentPurposePreview(ctx context.Context, a agentprofile.PrivateAccess, id string) (aep.Preview, error) {
	if !aep.ValidID(id) {
		return aep.Preview{}, aep.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, false)
	if e != nil {
		return aep.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := enrichmentPreviewTx(ctx, tx, id)
	if e != nil {
		return aep.Preview{}, e
	}
	if p.owner != b.owner || p.agent != b.agent {
		return aep.Preview{}, aep.ErrDenied
	}
	var consumed string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_enrichment_purpose_bindings WHERE preview_id=$1`, id).Scan(&consumed)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return aep.Preview{}, aep.ErrUnavailable
	}
	if consumed != "" {
		at, e := s.finishContextBuilder(ctx, tx, b, nil)
		if e != nil {
			return aep.Preview{}, enrichmentError(e)
		}
		out := enrichmentPreviewDTO(p, enrichmentCapture{at: at})
		out.State = "RECEIPT_ONLY"
		out.ConsumedGrantID = consumed
		if aep.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
			return aep.Preview{}, aep.ErrUnavailable
		}
		return out, nil
	}
	if p.session != b.session {
		return aep.Preview{}, aep.ErrDenied
	}
	c, e := s.captureEnrichment(ctx, tx, b, p.selection)
	if e != nil {
		return aep.Preview{}, e
	}
	if !matchEnrichment(p, c) {
		return aep.Preview{}, aep.ErrConflict
	}
	if !p.end.After(c.at) {
		return aep.Preview{}, aep.ErrExpired
	}
	out := enrichmentPreviewDTO(p, c)
	out.ConsumedGrantID = consumed
	if aep.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
		return aep.Preview{}, aep.ErrUnavailable
	}
	return out, nil
}
func enrichmentGrantTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, id string) (aep.Grant, enrichmentStored, error) {
	var g aep.Grant
	var preview string
	e := tx.QueryRow(ctx, `SELECT g.id,g.resource_id,g.revision,g.created_at,g.expires_at,g.revoked_at FROM consent_grants g JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id
 WHERE g.id=$1 AND g.owner_account_id=$2 AND g.recipient_account_id=$2 AND g.resource_type='agent_context' AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.actions=ARRAY['analyze_local']::text[] FOR UPDATE OF g`, id, b.owner).Scan(&g.ID, &preview, &g.Revision, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt)
	if e != nil {
		return g, enrichmentStored{}, enrichmentError(e)
	}
	p, e := enrichmentPreviewTx(ctx, tx, preview)
	if e != nil {
		return g, p, e
	}
	if p.owner != b.owner || p.agent != b.agent {
		return aep.Grant{}, p, aep.ErrDenied
	}
	g.SchemaVersion = aep.Schema
	g.PreviewID = preview
	g.Purpose = aep.Purpose
	g.Owner = actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}
	g.AgentID = b.agent
	g.Selection = p.selection
	g.CreatedAt = g.CreatedAt.UTC()
	g.ExpiresAt = g.ExpiresAt.UTC()
	return g, p, nil
}
func (s *Store) finalEnrichment(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, p enrichmentStored, g *aep.Grant, require bool) (enrichmentCapture, error) {
	if g != nil && g.RevokedAt != nil {
		return enrichmentCapture{}, aep.ErrExpired
	}
	grantID := ""
	revision := int64(0)
	if require && g != nil {
		grantID = g.ID
		revision = g.Revision
	}
	c, e := s.captureEnrichmentGrant(ctx, tx, b, p.selection, grantID, revision)
	if e != nil {
		return c, e
	}
	if p.session != b.session || !matchEnrichment(p, c) {
		return enrichmentCapture{}, aep.ErrConflict
	}
	if g != nil {
		if !g.ExpiresAt.After(c.at) || g.RevokedAt != nil {
			return enrichmentCapture{}, aep.ErrExpired
		}
		if g.ExpiresAt.After(p.end) {
			return enrichmentCapture{}, aep.ErrDenied
		}
	}
	return c, nil
}
func (s *Store) ApproveOwnEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, id string) (aep.Grant, error) {
	if !aep.ValidID(id) {
		return aep.Grant{}, aep.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, true)
	if e != nil {
		return aep.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,79015))`, b.owner); e != nil {
		return aep.Grant{}, aep.ErrUnavailable
	}
	p, e := enrichmentPreviewTx(ctx, tx, id)
	if e != nil {
		return aep.Grant{}, e
	}
	if p.owner != b.owner || p.agent != b.agent || p.session != b.session {
		return aep.Grant{}, aep.ErrDenied
	}
	var grantID string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_enrichment_purpose_bindings WHERE preview_id=$1`, id).Scan(&grantID)
	existing := e == nil
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return aep.Grant{}, aep.ErrUnavailable
	}
	if !existing {
		c, e := s.finalEnrichment(ctx, tx, b, p, nil, false)
		if e != nil {
			return aep.Grant{}, e
		}
		if !p.end.After(c.at) {
			return aep.Grant{}, aep.ErrExpired
		}
		end := c.end
		contextPurposeLimit(&end, p.end)
		if e = tx.QueryRow(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,revision,expires_at,created_at)
 VALUES(gen_random_uuid(),$1,$1,'agent_context',$2,'MOMENT_LOCAL_ANALYSIS',ARRAY['analyze_local']::text[],1,$3,$4) RETURNING id`, b.owner, p.id, end, c.at).Scan(&grantID); e != nil {
			return aep.Grant{}, aep.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `INSERT INTO agent_enrichment_purpose_bindings(grant_id,preview_id) VALUES($1,$2)`, grantID, p.id); e != nil {
			return aep.Grant{}, aep.ErrUnavailable
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "grant", "agent_context", grantID, "MOMENT_LOCAL_ANALYSIS", &grantID); e != nil {
			return aep.Grant{}, aep.ErrUnavailable
		}
	}
	g, p, e := enrichmentGrantTx(ctx, tx, b, grantID)
	if e != nil {
		return aep.Grant{}, e
	}
	c, e := s.finalEnrichment(ctx, tx, b, p, &g, true)
	if e != nil {
		return aep.Grant{}, e
	}
	g.ObservedAt = c.at
	if aep.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return aep.Grant{}, aep.ErrUnavailable
	}
	return g, nil
}

// Human reconciliation can use a new current self Session. It never returns
// source text or revives the original Session's analysis authority.
func (s *Store) ReadOwnEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, id string) (aep.Grant, error) {
	if !aep.ValidID(id) {
		return aep.Grant{}, aep.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, false)
	if e != nil {
		return aep.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := enrichmentGrantTx(ctx, tx, b, id)
	if e != nil {
		return aep.Grant{}, e
	}
	at, e := s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return aep.Grant{}, enrichmentError(e)
	}
	g.ObservedAt = at
	if aep.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return aep.Grant{}, aep.ErrUnavailable
	}
	return g, nil
}
func (s *Store) RevokeOwnEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, id string, revision int64) (aep.Grant, error) {
	if !aep.ValidID(id) || revision < 1 || revision == int64(^uint64(0)>>1) {
		return aep.Grant{}, aep.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, true)
	if e != nil {
		return aep.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := enrichmentGrantTx(ctx, tx, b, id)
	if e != nil {
		return aep.Grant{}, e
	}
	if g.RevokedAt != nil {
		if g.Revision != revision+1 {
			return aep.Grant{}, aep.ErrConflict
		}
	} else {
		if g.Revision != revision {
			return aep.Grant{}, aep.ErrConflict
		}
		if e = tx.QueryRow(ctx, `WITH clk AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE consent_grants g SET revision=revision+1,revoked_at=clk.at FROM clk WHERE g.id=$1 RETURNING g.revision,g.revoked_at`, id).Scan(&g.Revision, &g.RevokedAt); e != nil {
			return aep.Grant{}, aep.ErrUnavailable
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "revoke", "agent_context", id, "MOMENT_LOCAL_ANALYSIS", &id); e != nil {
			return aep.Grant{}, aep.ErrUnavailable
		}
	}
	at, e := s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return aep.Grant{}, enrichmentError(e)
	}
	g.ObservedAt = at
	if aep.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return aep.Grant{}, aep.ErrUnavailable
	}
	return g, nil
}
func (s *Store) ResolveOwnEnrichmentPurpose(ctx context.Context, a agentprofile.PrivateAccess, id string) (aep.Resolution, error) {
	if !aep.ValidID(id) {
		return aep.Resolution{}, aep.ErrInvalid
	}
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, false)
	if e != nil {
		return aep.Resolution{}, e
	}
	defer tx.Rollback(context.Background())
	g, p, e := enrichmentGrantTx(ctx, tx, b, id)
	if e != nil {
		return aep.Resolution{}, e
	}
	first, e := s.finalEnrichment(ctx, tx, b, p, &g, true)
	if e != nil {
		return aep.Resolution{}, e
	}
	last, e := s.finalEnrichment(ctx, tx, b, p, &g, true)
	if e != nil {
		return aep.Resolution{}, e
	}
	if !sameEnrichment(first, last) || !reflect.DeepEqual(first.review, last.review) {
		return aep.Resolution{}, aep.ErrConflict
	}
	if tx.Commit(ctx) != nil {
		return aep.Resolution{}, aep.ErrUnavailable
	}
	end := g.ExpiresAt
	contextPurposeLimit(&end, last.end)
	return aep.Resolution{GrantID: g.ID, GrantRevision: g.Revision, Owner: g.Owner, AgentID: g.AgentID, Selection: g.Selection, Content: last.review.Content, ObservedAt: last.at, ExpiresAt: end}, nil
}

func (s *Store) ReadOwnEnrichmentPurposePreviewReceipt(ctx context.Context, a agentprofile.PrivateAccess, id string) (aep.PreviewReceipt, error) {
	return s.readOwnApprovalPreviewReceipt(ctx, a, id, false)
}

// This reader intentionally never calls a full preview loader or a source
// capture. Only immutable preview IDs/times and the original consent linkage
// are needed to observe approval history. Missing current sources are irrelevant.
// Approval history depends on current identity and immutable approval metadata,
// not on current Task/Moment/source availability or their relation locks.
func (s *Store) beginApprovalPreviewReceipt(ctx context.Context, a agentprofile.PrivateAccess, multi bool) (pgx.Tx, contextBuilderBinding, error) {
	tx, b, e := s.beginContextBuilder(ctx, a)
	if e != nil {
		return nil, b, enrichmentError(e)
	}
	fail := func() (pgx.Tx, contextBuilderBinding, error) {
		tx.Rollback(context.Background())
		return nil, contextBuilderBinding{}, aep.ErrUnavailable
	}
	guard := enrichmentGuardSQL
	tables := "agent_enrichment_purpose_previews,agent_enrichment_purpose_bindings,consent_grants,accounts,agents,agent_profiles,sessions"
	if multi {
		guard = `SELECT (` + strings.TrimPrefix(enrichmentGuardSQL, "SELECT ") + `) AND (` + strings.TrimPrefix(multiRetentionGuardSQL, "SELECT ") + `)`
		tables += ",agent_multi_candidate_previews,agent_multi_candidate_bindings"
	}
	var ready bool
	if tx.QueryRow(ctx, guard).Scan(&ready) != nil || !ready {
		return fail()
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE `+tables+` IN ACCESS SHARE MODE`); e != nil {
		return fail()
	}
	if tx.QueryRow(ctx, guard).Scan(&ready) != nil || !ready {
		return fail()
	}
	return tx, b, nil
}

func (s *Store) readOwnApprovalPreviewReceipt(ctx context.Context, a agentprofile.PrivateAccess, id string, multi bool) (aep.PreviewReceipt, error) {
	if !aep.ValidID(id) {
		return aep.PreviewReceipt{}, aep.ErrInvalid
	}
	var tx pgx.Tx
	var b contextBuilderBinding
	var e error
	table, bindings, purpose, action, schema, advisory := "agent_enrichment_purpose_previews", "agent_enrichment_purpose_bindings", aep.Purpose, "analyze_local", "agent-enrichment-purpose-preview-receipt-v1", int64(79015)
	if multi {
		table, bindings, purpose, action, schema, advisory = "agent_multi_candidate_previews", "agent_multi_candidate_bindings", "STAGE_MEMORY_CANDIDATE_MULTI", "stage_candidate", "agent-multi-candidate-preview-receipt-v1", 80015
		tx, b, e = s.beginApprovalPreviewReceipt(ctx, a, true)
	} else {
		tx, b, e = s.beginApprovalPreviewReceipt(ctx, a, false)
	}
	if e != nil {
		if errors.Is(e, acr.ErrDenied) || errors.Is(e, aep.ErrDenied) {
			return aep.PreviewReceipt{}, aep.ErrDenied
		}
		return aep.PreviewReceipt{}, enrichmentError(e)
	}
	defer tx.Rollback(context.Background())
	// Same owner lock and ordering as original Approve. No absence decision is
	// made before a possibly in-flight approval has committed or rolled back.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,$2))`, b.owner, advisory); e != nil {
		return aep.PreviewReceipt{}, aep.ErrUnavailable
	}
	var owner, agent string
	if e = tx.QueryRow(ctx, `SELECT owner_id::text,agent_id::text FROM `+table+` WHERE id=$1 FOR SHARE`, id).Scan(&owner, &agent); e != nil {
		return aep.PreviewReceipt{}, enrichmentError(e)
	}
	if owner != b.owner || agent != b.agent {
		return aep.PreviewReceipt{}, aep.ErrDenied
	}
	var out aep.PreviewReceipt
	var alive, linked, installed bool
	// Every relation/row/advisory wait precedes this one current-clock statement.
	// The table names are internal fixed choices, never user-provided SQL.
	guard := strings.TrimPrefix(enrichmentGuardSQL, "SELECT ")
	if multi {
		guard = "(" + guard + ") AND (" + strings.TrimPrefix(multiRetentionGuardSQL, "SELECT ") + ")"
	}
	q := `WITH n AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT p.id::text,p.owner_id::text,p.agent_id::text,p.observed_at,p.expires_at,n.at,
 least(n.at+interval '30 seconds',se.expires_at,se.idle_expires_at,$7::timestamptz),coalesce(rb.grant_id::text,''),
 (rb.grant_id IS NULL OR (g.id IS NOT NULL AND g.owner_account_id=p.owner_id AND g.recipient_account_id=p.owner_id AND g.resource_type='agent_context' AND g.resource_id=p.id::text AND g.purpose=$4 AND g.actions=ARRAY[$5]::text[])),
 (actor.id=$2 AND actor.account_type='person' AND actor.status='active' AND ag.id=$3 AND ag.principal_account_id=actor.id AND ag.agent_type='personal' AND ag.status='active' AND ag.created_at<=n.at
 AND ap.owner_id=actor.id AND ap.owner_type='PERSON' AND ap.profile_version>0 AND ap.created_at<=n.at AND ap.updated_at<=n.at
 AND se.id=$6 AND se.account_id=actor.id AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at AND $7::timestamptz>n.at AND ($8::boolean OR se.authentication_method<>'dev_phone'))
 FROM ` + table + ` p CROSS JOIN n LEFT JOIN ` + bindings + ` rb ON rb.preview_id=p.id LEFT JOIN consent_grants g ON g.id=rb.grant_id
 JOIN accounts actor ON actor.id=p.owner_id JOIN agents ag ON ag.id=p.agent_id JOIN agent_profiles ap ON ap.agent_id=ag.id
 JOIN sessions se ON se.id=$6 WHERE p.id=$1 AND p.owner_id=$2 AND p.agent_id=$3`
	q = strings.Replace(q, "(actor.id=$2", "("+guard+"), (actor.id=$2", 1)
	if e = tx.QueryRow(ctx, q, id, b.owner, b.agent, purpose, action, b.session, b.limit, s.devPhoneEnabled).Scan(&out.PreviewID, &owner, &out.AgentID, &out.PreviewObservedAt, &out.PreviewExpiresAt, &out.ObservedAt, &out.ValidUntil, &out.ConsumedGrantID, &linked, &installed, &alive); e != nil {
		return aep.PreviewReceipt{}, enrichmentError(e)
	}
	if ctx.Err() != nil {
		return aep.PreviewReceipt{}, aep.ErrUnavailable
	}
	if !installed {
		return aep.PreviewReceipt{}, aep.ErrUnavailable
	}
	if !alive {
		return aep.PreviewReceipt{}, aep.ErrDenied
	}
	if !linked {
		return aep.PreviewReceipt{}, aep.ErrUnavailable
	}
	out.Owner = actorref.PrincipalRef{Type: actorref.Person, ID: owner}
	out.SchemaVersion = schema
	out.Purpose = purpose
	out.PreviewObservedAt = out.PreviewObservedAt.UTC()
	out.PreviewExpiresAt = out.PreviewExpiresAt.UTC()
	out.ObservedAt = out.ObservedAt.UTC()
	out.ValidUntil = out.ValidUntil.UTC()
	out.State = "OPEN_UNCONSUMED"
	if out.ConsumedGrantID != "" {
		out.State = "APPROVAL_RECORDED"
	} else if !out.PreviewExpiresAt.After(out.ObservedAt) {
		out.State = "CLOSED_UNCONSUMED"
	}
	if aep.ValidatePreviewReceipt(out, purpose) != nil || tx.Commit(ctx) != nil || ctx.Err() != nil {
		return aep.PreviewReceipt{}, aep.ErrUnavailable
	}
	return out, nil
}
