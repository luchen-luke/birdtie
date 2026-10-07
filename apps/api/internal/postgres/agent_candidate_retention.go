package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

var _ acr.Store = (*Store)(nil)

const retentionGuardSQL = `SELECT to_regclass('public.agent_candidate_retention_previews') IS NOT NULL AND to_regclass('public.agent_candidate_retention_bindings') IS NOT NULL
 AND(SELECT count(*)=3 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND NOT t.tgisinternal AND t.tgenabled='O' AND
 ((c.relname='agent_candidate_retention_previews' AND t.tgname='agent_candidate_retention_preview_guard' AND t.tgfoid=to_regprocedure('public.birdtie_candidate_retention_preview_guard()'))
 OR(c.relname='agent_candidate_retention_bindings' AND t.tgname='agent_candidate_retention_binding_guard' AND t.tgfoid=to_regprocedure('public.birdtie_candidate_retention_binding_guard()'))
 OR(c.relname='consent_grants' AND t.tgname='agent_candidate_retention_grant_guard' AND t.tgfoid=to_regprocedure('public.birdtie_candidate_retention_grant_guard()'))))`

func retentionError(e error) error {
	switch {
	case errors.Is(e, aep.ErrInvalid):
		return acr.ErrInvalid
	case errors.Is(e, aep.ErrDenied), errors.Is(e, pgx.ErrNoRows):
		return acr.ErrDenied
	case errors.Is(e, aep.ErrConflict):
		return acr.ErrConflict
	case errors.Is(e, aep.ErrExpired):
		return acr.ErrExpired
	default:
		return acr.ErrUnavailable
	}
}
func (s *Store) beginRetention(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, contextBuilderBinding, error) {
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, write)
	if e != nil {
		return nil, b, retentionError(e)
	}
	fail := func() (pgx.Tx, contextBuilderBinding, error) {
		tx.Rollback(context.Background())
		return nil, contextBuilderBinding{}, acr.ErrUnavailable
	}
	var ready bool
	if tx.QueryRow(ctx, retentionGuardSQL).Scan(&ready) != nil || !ready {
		return fail()
	}
	mode := "ACCESS SHARE"
	if write {
		mode = "ROW EXCLUSIVE"
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_candidate_retention_previews,agent_candidate_retention_bindings IN `+mode+` MODE`); e != nil {
		return fail()
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_domain_outbox,moment_activity_links,moment_community_links,moment_organization_links IN ACCESS SHARE MODE`); e != nil {
		return fail()
	}
	if tx.QueryRow(ctx, retentionGuardSQL).Scan(&ready) != nil || !ready {
		return fail()
	}
	return tx, b, nil
}

type retentionStored struct {
	id, owner, agent, session, analysis, analysisPreview, authority, frame, digest, event, operation, algorithm string
	selection                                                                                                   acr.Selection
	observed, end                                                                                               time.Time
}
type retentionCapture struct {
	review                                                      acr.Review
	authority, frame, digest, analysisPreview, event, operation string
	at, end                                                     time.Time
}

// Capture uses the original real analysis grant, never a caller's resolution,
// query, score, Source JSON or Task metadata as authority.
func (s *Store) captureRetention(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, sel acr.Selection, reviewEnd *time.Time, retGrant string, retRevision int64, pinnedAlgorithm ...string) (retentionCapture, error) {
	var out retentionCapture
	algorithm := agentlocalcandidate.Version
	if len(pinnedAlgorithm) > 1 {
		return out, acr.ErrUnavailable
	}
	if len(pinnedAlgorithm) == 1 {
		algorithm = pinnedAlgorithm[0]
	}
	if algorithm != agentlocalcandidate.Version && algorithm != agentlocalcandidate.LegacyVersion {
		return out, acr.ErrUnavailable
	}
	if algorithm == agentlocalcandidate.Version {
		var ready bool
		if tx.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_candidate_algorithm_vocabulary(text,text)') IS NOT NULL
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='public.agent_candidate_retention_previews'::regclass AND conname='agent_candidate_retention_previews_algorithm_version_check' AND convalidated AND pg_get_constraintdef(oid) LIKE '%moment-lexical-category-v2%')
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='public.agent_multi_candidate_previews'::regclass AND conname='agent_multi_candidate_previews_algorithm_version_check' AND convalidated AND pg_get_constraintdef(oid) LIKE '%moment-lexical-category-v2%')
 AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='public.agent_memory_candidates'::regclass AND convalidated AND pg_get_constraintdef(oid) LIKE '%hiking%')
 AND EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('public.birdtie_agent_effect_writer_unavailable()') AND prosrc LIKE '%birdtie_candidate_algorithm_vocabulary%')
 AND EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('public.birdtie_multi_candidate_effect_validate(agent_effect_ledger)') AND prosrc LIKE '%birdtie_candidate_algorithm_vocabulary%')`).Scan(&ready) != nil || !ready {
			return out, acr.ErrUnavailable
		}
	}
	ag, p, e := enrichmentGrantTx(ctx, tx, b, sel.AnalysisGrantID)
	if e != nil {
		return out, retentionError(e)
	}
	c, e := s.finalEnrichment(ctx, tx, b, p, &ag, true)
	if e != nil {
		return out, retentionError(e)
	}
	if !sel.RetainUntil.After(c.at) || sel.RetainUntil.Sub(c.at) > acr.MaxRequestedRetention {
		return out, acr.ErrExpired
	}
	var noLinks bool
	if tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM moment_activity_links WHERE moment_id=$1)
 AND NOT EXISTS(SELECT 1 FROM moment_community_links WHERE moment_id=$1) AND NOT EXISTS(SELECT 1 FROM moment_organization_links WHERE moment_id=$1)`, p.selection.MomentID).Scan(&noLinks) != nil {
		return out, acr.ErrUnavailable
	}
	if !noLinks {
		return out, acr.ErrDenied
	}
	proposal, e := agentlocalcandidate.ExtractVersion(algorithm, c.review.Content)
	if e != nil {
		return out, acr.ErrUnavailable
	}
	var meta int64
	if tx.QueryRow(ctx, `SELECT profile_version FROM agent_profiles WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`, b.agent, b.owner).Scan(&meta) != nil {
		return out, acr.ErrUnavailable
	}
	refs, _, e := candidateSources(ctx, tx, agentPrivateBinding{accountID: b.owner, agentID: b.agent, sessionID: b.session}, meta, []agentmemorycandidate.Selector{{Type: agentevent.MomentSource, ID: p.selection.MomentID}})
	if e != nil || len(refs) != 1 {
		return out, acr.ErrDenied
	}
	if len(refs[0].Anchors) != 1 || refs[0].Anchors[0] != "MOMENT:"+p.selection.MomentID {
		return out, acr.ErrDenied
	}
	kind := agentoutbox.MomentCreated
	if p.selection.MomentRevision > 1 {
		kind = agentoutbox.MomentUpdated
	}
	envelope, _, e := resolveOutboxMomentTx(ctx, tx, p.selection.MomentID, kind)
	if e != nil {
		return out, acr.ErrUnavailable
	}
	// This is a metadata read of the actual captured mutation, not a claim or a
	// source grant. It intentionally does not lock/approve delivery controls.
	var actual bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_domain_outbox WHERE event_id=$1 AND subject_id=$2 AND agent_id=$3 AND source_id=$4 AND source_revision=$5 AND source_fingerprint=$6 AND logical_operation_id=$7)`, envelope.EventID, b.owner, b.agent, p.selection.MomentID, p.selection.MomentRevision, envelope.Source.Fingerprint, envelope.LogicalOperationID).Scan(&actual) != nil {
		return out, acr.ErrUnavailable
	}
	if !actual {
		return out, acr.ErrUnavailable
	}
	out.end = c.end
	contextPurposeLimit(&out.end, ag.ExpiresAt)
	contextPurposeLimit(&out.end, sel.RetainUntil)
	contextPurposeLimit(&out.end, envelope.ExpiresAt)
	if reviewEnd == nil {
		contextPurposeLimit(&out.end, c.at.Add(acr.PreviewTTL))
	}
	bound := out.end
	if reviewEnd != nil {
		bound = *reviewEnd
	}
	out.review = acr.Review{Proposal: proposal, Source: refs[0], TaskID: p.selection.TaskID, SelectedFields: append([]string{}, p.selection.Fields...), RetainUntil: bound, Clusters: 1}
	out.digest, e = acr.DigestReview(out.review)
	if e != nil {
		return retentionCapture{}, acr.ErrUnavailable
	}
	out.authority = c.authority
	out.analysisPreview = p.id
	out.event = envelope.EventID
	out.operation = envelope.LogicalOperationID
	out.frame, _ = agentreinforcement.Digest(struct {
		Source, Task, Event, Operation, EventFingerprint string
		Reference                                        agentmemorycandidate.Source
	}{c.source, c.task, out.event, out.operation, envelope.Source.Fingerprint, refs[0]})
	last, e := s.finalEnrichment(ctx, tx, b, p, &ag, true)
	if e != nil {
		return retentionCapture{}, retentionError(e)
	}
	if !sameEnrichment(c, last) {
		return retentionCapture{}, acr.ErrConflict
	}
	// All prior selected rows are held; this final one-statement check includes
	// Session/source/Task/City/analysis/retention clocks and link absence after
	// every possible metadata, relation or native source wait.
	var alive bool
	e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT n.at,EXISTS(
 SELECT 1 FROM sessions se JOIN accounts a ON a.id=se.account_id JOIN agents ag ON ag.principal_account_id=a.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN agent_tasks t ON t.owner_account_id=a.id JOIN moments m ON m.author_account_id=a.id
 JOIN contexts cx ON cx.id=t.context_id AND cx.context_type='CITY' AND cx.city_id=t.city_context_id
 JOIN cities ci ON ci.id=cx.city_id JOIN city_contexts cc ON cc.city_id=ci.id
 JOIN consent_grants g ON g.owner_account_id=a.id JOIN agent_domain_outbox d ON d.event_id=$7
 WHERE a.id=$1 AND a.account_type='person' AND a.status='active' AND ag.id=$2 AND ag.agent_type='personal' AND ag.status='active'
 AND se.id=$3 AND se.revoked_at IS NULL AND se.expires_at>n.at AND se.idle_expires_at>n.at AND($11::boolean OR se.authentication_method<>'dev_phone')
 AND t.id=$4 AND t.acting_user_account_id=a.id AND t.principal_type='person' AND t.status='ACTIVE' AND t.context_type='CITY' AND t.updated_at<=n.at
 AND ci.publication_status='published' AND cc.status='active' AND(ci.expires_at IS NULL OR ci.expires_at>n.at)
 AND m.id=$5 AND m.revision=$6 AND m.status='draft' AND m.visibility='private' AND m.updated_at+interval '15 minutes'>n.at
 AND NOT EXISTS(SELECT 1 FROM moment_activity_links WHERE moment_id=m.id) AND NOT EXISTS(SELECT 1 FROM moment_community_links WHERE moment_id=m.id) AND NOT EXISTS(SELECT 1 FROM moment_organization_links WHERE moment_id=m.id)
 AND g.id=$8 AND g.recipient_account_id=a.id AND g.purpose='MOMENT_LOCAL_ANALYSIS' AND g.actions=ARRAY['analyze_local']::text[] AND g.revoked_at IS NULL AND g.expires_at>n.at
 AND d.source_id=m.id AND d.source_revision=m.revision AND d.source_status='draft' AND d.subject_id=a.id AND d.agent_id=ag.id AND d.expires_at>n.at
 AND($9::text='' OR EXISTS(SELECT 1 FROM consent_grants rg JOIN agent_candidate_retention_bindings rb ON rb.grant_id=rg.id WHERE rg.id=$9::uuid AND rg.owner_account_id=a.id AND rg.recipient_account_id=a.id AND rg.resource_type='agent_context' AND rg.resource_id=rb.preview_id::text AND rg.purpose='STAGE_MEMORY_CANDIDATE' AND rg.actions=ARRAY['stage_candidate']::text[] AND rg.revision=$10 AND rg.revoked_at IS NULL AND rg.expires_at>n.at))) FROM n`, b.owner, b.agent, b.session, p.selection.TaskID, p.selection.MomentID, p.selection.MomentRevision, envelope.EventID, ag.ID, retGrant, retRevision, s.devPhoneEnabled).Scan(&out.at, &alive)
	if e != nil {
		return retentionCapture{}, acr.ErrUnavailable
	}
	out.at = out.at.UTC()
	if !alive {
		return retentionCapture{}, acr.ErrDenied
	}
	if !out.end.After(out.at) {
		return retentionCapture{}, acr.ErrExpired
	}
	return out, nil
}
func retentionLoad(ctx context.Context, tx pgx.Tx, id string) (retentionStored, error) {
	var p retentionStored
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT id,owner_id,agent_id,session_id,analysis_grant_id,analysis_preview_id,selection,authority,source_frame,review_digest,event_id,logical_operation_id,observed_at,expires_at,algorithm_version FROM agent_candidate_retention_previews WHERE id=$1 FOR SHARE`, id).Scan(&p.id, &p.owner, &p.agent, &p.session, &p.analysis, &p.analysisPreview, &raw, &p.authority, &p.frame, &p.digest, &p.event, &p.operation, &p.observed, &p.end, &p.algorithm)
	if e != nil {
		return p, retentionError(e)
	}
	if json.Unmarshal(raw, &p.selection) != nil {
		return p, acr.ErrUnavailable
	}
	p.observed = p.observed.UTC()
	p.end = p.end.UTC()
	p.selection, e = acr.Normalize(p.selection)
	return p, e
}
func retentionMatch(p retentionStored, c retentionCapture) bool {
	return p.authority == c.authority && p.frame == c.frame && p.digest == c.digest && p.analysisPreview == c.analysisPreview && p.event == c.event && p.operation == c.operation && p.algorithm == c.review.Proposal.AlgorithmVersion
}
func retentionDTO(p retentionStored, c retentionCapture) acr.Preview {
	return acr.Preview{SchemaVersion: acr.Schema, State: "CURRENT_REVIEW", ID: p.id, Purpose: acr.Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: p.owner}, AgentID: p.agent, Selection: p.selection, Review: &c.review, ObservedAt: c.at, ExpiresAt: p.end.UTC(), Explanation: "仅批准上面这项关键词假设的有限私密候选保留；不证明偏好或到场。批准不会自动提交候选；原生提交还需当前服务开关与原许可。不会发送模型或写正式记忆。"}
}
func (s *Store) PreviewOwnCandidateRetention(ctx context.Context, a agentprofile.PrivateAccess, sel acr.Selection) (acr.Preview, error) {
	return s.previewOwnCandidateRetentionVersion(ctx, a, sel, agentlocalcandidate.Version)
}

// Only the current algorithm is exported through the human gateway. The pinned
// internal path also lets native regression fixtures replay real prior v1
// previews, and does not accept client-selected algorithm/authority.
func (s *Store) previewOwnCandidateRetentionVersion(ctx context.Context, a agentprofile.PrivateAccess, sel acr.Selection, algorithm string) (acr.Preview, error) {
	sel, e := acr.Normalize(sel)
	if e != nil {
		return acr.Preview{}, e
	}
	tx, b, e := s.beginRetention(ctx, a, true)
	if e != nil {
		return acr.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := s.captureRetention(ctx, tx, b, sel, nil, "", 0, algorithm)
	if e != nil {
		return acr.Preview{}, e
	}
	p := retentionStored{owner: b.owner, agent: b.agent, session: b.session, analysis: sel.AnalysisGrantID, analysisPreview: c.analysisPreview, selection: sel, authority: c.authority, frame: c.frame, digest: c.digest, event: c.event, operation: c.operation, algorithm: c.review.Proposal.AlgorithmVersion, observed: c.at, end: c.end}
	raw, _ := json.Marshal(sel)
	e = tx.QueryRow(ctx, `INSERT INTO agent_candidate_retention_previews(id,owner_id,agent_id,session_id,analysis_grant_id,analysis_preview_id,selection,authority,source_frame,review_digest,event_id,logical_operation_id,algorithm_version,observed_at,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`, p.owner, p.agent, p.session, p.analysis, p.analysisPreview, raw, p.authority, p.frame, p.digest, p.event, p.operation, p.algorithm, p.observed, p.end).Scan(&p.id)
	if e != nil {
		return acr.Preview{}, acr.ErrUnavailable
	}
	last, e := s.captureRetention(ctx, tx, b, sel, &p.end, "", 0, p.algorithm)
	if e != nil {
		return acr.Preview{}, e
	}
	if !retentionMatch(p, last) || !p.end.After(last.at) {
		return acr.Preview{}, acr.ErrConflict
	}
	out := retentionDTO(p, last)
	if acr.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
		return acr.Preview{}, acr.ErrUnavailable
	}
	return out, nil
}
func (s *Store) ReadOwnCandidateRetentionPreview(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Preview, error) {
	if !aep.ValidID(id) {
		return acr.Preview{}, acr.ErrInvalid
	}
	tx, b, e := s.beginRetention(ctx, a, false)
	if e != nil {
		return acr.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := retentionLoad(ctx, tx, id)
	if e != nil {
		return acr.Preview{}, e
	}
	if p.owner != b.owner || p.agent != b.agent {
		return acr.Preview{}, acr.ErrDenied
	}
	var consumed string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_candidate_retention_bindings WHERE preview_id=$1`, id).Scan(&consumed)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return acr.Preview{}, acr.ErrUnavailable
	}
	if consumed != "" {
		at, e := s.finishContextBuilder(ctx, tx, b, nil)
		if e != nil {
			return acr.Preview{}, retentionError(e)
		}
		out := retentionDTO(p, retentionCapture{at: at})
		out.State = "RECEIPT_ONLY"
		out.Review = nil
		out.ConsumedGrantID = consumed
		if acr.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
			return acr.Preview{}, acr.ErrUnavailable
		}
		return out, nil
	}
	if p.session != b.session {
		return acr.Preview{}, acr.ErrDenied
	}
	c, e := s.captureRetention(ctx, tx, b, p.selection, &p.end, "", 0, p.algorithm)
	if e != nil {
		return acr.Preview{}, e
	}
	if !retentionMatch(p, c) || !p.end.After(c.at) {
		return acr.Preview{}, acr.ErrConflict
	}
	out := retentionDTO(p, c)
	if acr.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
		return acr.Preview{}, acr.ErrUnavailable
	}
	return out, nil
}
func retentionGrantTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, id string) (acr.Grant, retentionStored, error) {
	var g acr.Grant
	var preview string
	e := tx.QueryRow(ctx, `SELECT g.id,g.resource_id,g.revision,g.created_at,g.expires_at,g.revoked_at FROM consent_grants g JOIN agent_candidate_retention_bindings rb ON rb.grant_id=g.id WHERE g.id=$1 AND g.owner_account_id=$2 AND g.recipient_account_id=$2 AND g.resource_type='agent_context' AND g.purpose='STAGE_MEMORY_CANDIDATE' AND g.actions=ARRAY['stage_candidate']::text[] FOR UPDATE OF g`, id, b.owner).Scan(&g.ID, &preview, &g.Revision, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt)
	if e != nil {
		return g, retentionStored{}, retentionError(e)
	}
	p, e := retentionLoad(ctx, tx, preview)
	if e != nil {
		return g, p, e
	}
	if p.owner != b.owner || p.agent != b.agent {
		return acr.Grant{}, p, acr.ErrDenied
	}
	g.SchemaVersion = acr.Schema
	g.PreviewID = p.id
	g.Purpose = acr.Purpose
	g.Owner = actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}
	g.AgentID = b.agent
	g.Selection = p.selection
	g.CreatedAt = g.CreatedAt.UTC()
	g.ExpiresAt = g.ExpiresAt.UTC()
	return g, p, nil
}
func (s *Store) ApproveOwnCandidateRetention(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Grant, error) {
	if !aep.ValidID(id) {
		return acr.Grant{}, acr.ErrInvalid
	}
	tx, b, e := s.beginRetention(ctx, a, true)
	if e != nil {
		return acr.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,80015))`, b.owner); e != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	p, e := retentionLoad(ctx, tx, id)
	if e != nil {
		return acr.Grant{}, e
	}
	if p.owner != b.owner || p.agent != b.agent || p.session != b.session {
		return acr.Grant{}, acr.ErrDenied
	}
	var gid string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_candidate_retention_bindings WHERE preview_id=$1`, id).Scan(&gid)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return acr.Grant{}, acr.ErrUnavailable
	}
	if gid == "" {
		c, e := s.captureRetention(ctx, tx, b, p.selection, &p.end, "", 0, p.algorithm)
		if e != nil {
			return acr.Grant{}, e
		}
		if !retentionMatch(p, c) {
			return acr.Grant{}, acr.ErrConflict
		}
		if !p.end.After(c.at) {
			return acr.Grant{}, acr.ErrExpired
		}
		end := c.end
		contextPurposeLimit(&end, p.end)
		e = tx.QueryRow(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,revision,created_at,expires_at) VALUES(gen_random_uuid(),$1,$1,'agent_context',$2,'STAGE_MEMORY_CANDIDATE',ARRAY['stage_candidate']::text[],1,$3,$4) RETURNING id`, b.owner, p.id, c.at, end).Scan(&gid)
		if e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `INSERT INTO agent_candidate_retention_bindings(grant_id,preview_id) VALUES($1,$2)`, gid, p.id); e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "grant", "agent_context", gid, "STAGE_MEMORY_CANDIDATE", &gid); e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
	}
	g, p, e := retentionGrantTx(ctx, tx, b, gid)
	if e != nil {
		return acr.Grant{}, e
	}
	if g.RevokedAt != nil {
		return acr.Grant{}, acr.ErrExpired
	}
	c, e := s.captureRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return acr.Grant{}, e
	}
	if !retentionMatch(p, c) || g.ExpiresAt.After(p.end) {
		return acr.Grant{}, acr.ErrConflict
	}
	g.ObservedAt = c.at
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	return g, nil
}
func (s *Store) ReadOwnCandidateRetention(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Grant, error) {
	if !aep.ValidID(id) {
		return acr.Grant{}, acr.ErrInvalid
	}
	tx, b, e := s.beginRetention(ctx, a, false)
	if e != nil {
		return acr.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := retentionGrantTx(ctx, tx, b, id)
	if e != nil {
		return acr.Grant{}, e
	}
	g.ObservedAt, e = s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return acr.Grant{}, retentionError(e)
	}
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	return g, nil
}
func (s *Store) RevokeOwnCandidateRetention(ctx context.Context, a agentprofile.PrivateAccess, id string, expected int64) (acr.Grant, error) {
	if !aep.ValidID(id) || expected < 1 || expected == int64(^uint64(0)>>1) {
		return acr.Grant{}, acr.ErrInvalid
	}
	tx, b, e := s.beginRetention(ctx, a, true)
	if e != nil {
		return acr.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := retentionGrantTx(ctx, tx, b, id)
	if e != nil {
		return acr.Grant{}, e
	}
	if g.RevokedAt != nil {
		if g.Revision != expected+1 {
			return acr.Grant{}, acr.ErrConflict
		}
	} else {
		if g.Revision != expected {
			return acr.Grant{}, acr.ErrConflict
		}
		if e = tx.QueryRow(ctx, `UPDATE consent_grants SET revision=revision+1,revoked_at=clock_timestamp() WHERE id=$1 AND revision=$2 RETURNING revision,revoked_at`, id, expected).Scan(&g.Revision, &g.RevokedAt); e != nil {
			return acr.Grant{}, retentionError(e)
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "revoke", "agent_context", id, "STAGE_MEMORY_CANDIDATE", &id); e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
	}
	g.ObservedAt, e = s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return acr.Grant{}, retentionError(e)
	}
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	return g, nil
}
func (s *Store) ResolveOwnCandidateRetention(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Resolution, error) {
	if !aep.ValidID(id) {
		return acr.Resolution{}, acr.ErrInvalid
	}
	tx, b, e := s.beginRetention(ctx, a, false)
	if e != nil {
		return acr.Resolution{}, e
	}
	defer tx.Rollback(context.Background())
	g, p, e := retentionGrantTx(ctx, tx, b, id)
	if e != nil {
		return acr.Resolution{}, e
	}
	if g.RevokedAt != nil {
		return acr.Resolution{}, acr.ErrExpired
	}
	if p.session != b.session {
		return acr.Resolution{}, acr.ErrDenied
	}
	c, e := s.captureRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return acr.Resolution{}, e
	}
	if !retentionMatch(p, c) || g.ExpiresAt.After(p.end) {
		return acr.Resolution{}, acr.ErrConflict
	}
	last, e := s.captureRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return acr.Resolution{}, e
	}
	if !retentionMatch(p, last) || !reflect.DeepEqual(c.review, last.review) {
		return acr.Resolution{}, acr.ErrConflict
	}
	g.ObservedAt = last.at
	contextPurposeLimit(&g.ExpiresAt, last.end)
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Resolution{}, acr.ErrUnavailable
	}
	return acr.Resolution{Grant: g, Review: last.review, Authority: last.authority, AnalysisSource: last.frame, AnalysisTask: c.review.TaskID}, nil
}
