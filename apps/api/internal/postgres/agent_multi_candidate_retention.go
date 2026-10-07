package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	single "github.com/birdtie/birdtie/apps/api/internal/agentcandidateretention"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentlocalcandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	acr "github.com/birdtie/birdtie/apps/api/internal/agentmulticandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentreinforcement"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

var _ acr.RetentionStore = (*Store)(nil)
var _ acr.PreviewReceiptStore = (*multiCandidateExecutor)(nil)

const multiRetentionGuardSQL = `SELECT to_regclass('public.agent_multi_candidate_previews') IS NOT NULL AND to_regclass('public.agent_multi_candidate_bindings') IS NOT NULL
 AND(SELECT count(*)=3 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND NOT t.tgisinternal AND t.tgenabled='O' AND
 ((c.relname='agent_multi_candidate_previews' AND t.tgname='agent_multi_candidate_preview_guard' AND t.tgfoid=to_regprocedure('public.birdtie_multi_candidate_preview_guard()'))
 OR(c.relname='agent_multi_candidate_bindings' AND t.tgname='agent_multi_candidate_binding_guard' AND t.tgfoid=to_regprocedure('public.birdtie_multi_candidate_binding_guard()'))
 OR(c.relname='consent_grants' AND t.tgname='agent_multi_candidate_grant_guard' AND t.tgfoid=to_regprocedure('public.birdtie_multi_candidate_grant_guard()'))))`

func multiRetentionError(e error) error {
	switch {
	case errors.Is(e, aep.ErrInvalid), errors.Is(e, single.ErrInvalid):
		return acr.ErrInvalid
	case errors.Is(e, aep.ErrDenied), errors.Is(e, single.ErrDenied), errors.Is(e, pgx.ErrNoRows):
		return acr.ErrDenied
	case errors.Is(e, aep.ErrConflict), errors.Is(e, single.ErrConflict):
		return acr.ErrConflict
	case errors.Is(e, aep.ErrExpired), errors.Is(e, single.ErrExpired):
		return acr.ErrExpired
	default:
		return acr.ErrUnavailable
	}
}
func (s *Store) beginMultiRetention(ctx context.Context, a agentprofile.PrivateAccess, write bool) (pgx.Tx, contextBuilderBinding, error) {
	tx, b, e := s.beginEnrichmentPurpose(ctx, a, write)
	if e != nil {
		return nil, b, multiRetentionError(e)
	}
	fail := func() (pgx.Tx, contextBuilderBinding, error) {
		tx.Rollback(context.Background())
		return nil, contextBuilderBinding{}, acr.ErrUnavailable
	}
	var ready bool
	if tx.QueryRow(ctx, multiRetentionGuardSQL).Scan(&ready) != nil || !ready {
		return fail()
	}
	mode := "ACCESS SHARE"
	if write {
		mode = "ROW EXCLUSIVE"
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_multi_candidate_previews,agent_multi_candidate_bindings IN `+mode+` MODE`); e != nil {
		return fail()
	}
	if _, e = tx.Exec(ctx, `LOCK TABLE agent_domain_outbox,moment_activity_links,moment_community_links,moment_organization_links IN ACCESS SHARE MODE`); e != nil {
		return fail()
	}
	if tx.QueryRow(ctx, multiRetentionGuardSQL).Scan(&ready) != nil || !ready {
		return fail()
	}
	return tx, b, nil
}

type multiRetentionStored struct {
	id, owner, agent, session, authority, frame, digest, event, operation, algorithm string
	selection                                                                        acr.Selection
	observed, end                                                                    time.Time
}
type multiRetentionCapture struct {
	review                                     acr.Review
	authority, frame, digest, event, operation string
	at, end                                    time.Time
}

// Source order is fixed by actual Moment ID, never grant ID or caller ordering.
func (s *Store) captureMultiRetention(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, sel acr.Selection, reviewEnd *time.Time, retGrant string, retRevision int64, pinnedAlgorithm ...string) (multiRetentionCapture, error) {
	var out multiRetentionCapture
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
	type member struct{ id, moment string }
	members := make([]member, 0, len(sel.AnalysisGrantIDs))
	seen := map[string]bool{}
	for _, id := range sel.AnalysisGrantIDs {
		var m string
		e := tx.QueryRow(ctx, `SELECT ep.moment_id FROM consent_grants g JOIN agent_enrichment_purpose_bindings eb ON eb.grant_id=g.id JOIN agent_enrichment_purpose_previews ep ON ep.id=eb.preview_id WHERE g.id=$1 AND g.owner_account_id=$2`, id, b.owner).Scan(&m)
		if e != nil || seen[m] {
			return out, acr.ErrDenied
		}
		seen[m] = true
		members = append(members, member{id, m})
	}
	sort.Slice(members, func(i, j int) bool { return members[i].moment < members[j].moment })
	frames := []string{}
	out.end = sel.RetainUntil
	for i, m := range members {
		c, e := s.captureRetention(ctx, tx, b, single.Selection{AnalysisGrantID: m.id, RetainUntil: sel.RetainUntil}, reviewEnd, "", 0, algorithm)
		if e != nil {
			return out, multiRetentionError(e)
		}
		_, p, e := enrichmentGrantTx(ctx, tx, b, m.id)
		if e != nil {
			return out, multiRetentionError(e)
		}
		if i == 0 {
			out.authority = c.authority
			out.event = c.event
			out.operation = c.operation
			out.review.Proposal = c.review.Proposal
			out.review.TaskID = c.review.TaskID
			out.review.AnchorSource = c.review.Source
			out.review.AnchorEventID = c.event
			out.review.LogicalOperationID = c.operation
		}
		if out.authority != c.authority || out.review.TaskID != c.review.TaskID || out.review.Proposal.Category != c.review.Proposal.Category {
			return out, acr.ErrDenied
		}
		out.review.Sources = append(out.review.Sources, c.review.Source)
		out.review.SourceSelections = append(out.review.SourceSelections, acr.SourceSelection{AnalysisGrantID: m.id, AnalysisPreviewID: p.id, Source: c.review.Source, SelectedFields: append([]string(nil), p.selection.Fields...)})
		frames = append(frames, c.frame)
		out.at = c.at
		contextPurposeLimit(&out.end, c.end)
	}
	var e error
	out.review.Clusters, e = agentmemorycandidate.ClusterCount(out.review.Sources)
	if e != nil || out.review.Clusters < 2 {
		return out, acr.ErrDenied
	}
	if reviewEnd != nil {
		contextPurposeLimit(&out.end, *reviewEnd)
		out.review.RetainUntil = *reviewEnd
	} else {
		out.review.RetainUntil = out.end
	}
	out.frame, _ = agentreinforcement.Digest(frames)
	out.digest, e = acr.DigestReview(out.review)
	if e != nil {
		return out, acr.ErrUnavailable
	}
	raw, _ := json.Marshal(sel)
	var alive bool
	if e = tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() at) SELECT n.at,birdtie_multi_candidate_sources_current($1,$2,$3,$4,$5) AND $6::timestamptz>n.at AND ($7::text='' OR birdtie_candidate_pipeline_current($7::uuid)) FROM n`, raw, b.owner, b.agent, b.session, out.authority, out.end, retGrant).Scan(&out.at, &alive); e != nil {
		return out, acr.ErrUnavailable
	}
	out.at = out.at.UTC()
	if !alive {
		return out, acr.ErrDenied
	}
	if !out.end.After(out.at) {
		return out, acr.ErrExpired
	}
	if acr.ValidateReview(out.review, out.at) != nil {
		return out, acr.ErrUnavailable
	}
	return out, nil
}
func multiRetentionLoad(ctx context.Context, tx pgx.Tx, id string) (multiRetentionStored, error) {
	var p multiRetentionStored
	var raw []byte
	e := tx.QueryRow(ctx, `SELECT id,owner_id,agent_id,session_id,selection,authority,source_frame,review_digest,event_id,logical_operation_id,observed_at,expires_at,algorithm_version FROM agent_multi_candidate_previews WHERE id=$1 FOR SHARE`, id).Scan(&p.id, &p.owner, &p.agent, &p.session, &raw, &p.authority, &p.frame, &p.digest, &p.event, &p.operation, &p.observed, &p.end, &p.algorithm)
	if e != nil {
		return p, multiRetentionError(e)
	}
	if json.Unmarshal(raw, &p.selection) != nil {
		return p, acr.ErrUnavailable
	}
	p.observed = p.observed.UTC()
	p.end = p.end.UTC()
	p.selection, e = acr.Normalize(p.selection)
	return p, e
}
func multiRetentionMatch(p multiRetentionStored, c multiRetentionCapture) bool {
	return p.authority == c.authority && p.frame == c.frame && p.digest == c.digest && p.event == c.event && p.operation == c.operation && p.algorithm == c.review.Proposal.AlgorithmVersion
}
func multiRetentionDTO(p multiRetentionStored, c multiRetentionCapture) acr.Preview {
	return acr.Preview{SchemaVersion: acr.Schema, State: "CURRENT_REVIEW", ID: p.id, Purpose: acr.Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: p.owner}, AgentID: p.agent, Selection: p.selection, Review: &c.review, ObservedAt: c.at, ExpiresAt: p.end.UTC(), Explanation: "仅批准上面这项关键词假设的有限私密候选保留；不证明偏好或到场。批准不会自动提交候选；原生提交还需当前服务开关与原许可。不会发送模型或写正式记忆。"}
}
func (s *Store) PreviewOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, sel acr.Selection) (acr.Preview, error) {
	return s.previewOwnMultiCandidateVersion(ctx, a, sel, agentlocalcandidate.Version)
}

func (s *Store) previewOwnMultiCandidateVersion(ctx context.Context, a agentprofile.PrivateAccess, sel acr.Selection, algorithm string) (acr.Preview, error) {
	sel, e := acr.Normalize(sel)
	if e != nil {
		return acr.Preview{}, e
	}
	tx, b, e := s.beginMultiRetention(ctx, a, true)
	if e != nil {
		return acr.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	c, e := s.captureMultiRetention(ctx, tx, b, sel, nil, "", 0, algorithm)
	if e != nil {
		return acr.Preview{}, e
	}
	p := multiRetentionStored{owner: b.owner, agent: b.agent, session: b.session, selection: sel, authority: c.authority, frame: c.frame, digest: c.digest, event: c.event, operation: c.operation, algorithm: c.review.Proposal.AlgorithmVersion, observed: c.at, end: c.end}
	raw, _ := json.Marshal(sel)
	e = tx.QueryRow(ctx, `INSERT INTO agent_multi_candidate_previews(id,owner_id,agent_id,session_id,selection,authority,source_frame,review_digest,event_id,logical_operation_id,algorithm_version,observed_at,expires_at) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, p.owner, p.agent, p.session, raw, p.authority, p.frame, p.digest, p.event, p.operation, p.algorithm, p.observed, p.end).Scan(&p.id)
	if e != nil {
		return acr.Preview{}, acr.ErrUnavailable
	}
	last, e := s.captureMultiRetention(ctx, tx, b, sel, &p.end, "", 0, p.algorithm)
	if e != nil {
		return acr.Preview{}, e
	}
	if !multiRetentionMatch(p, last) || !p.end.After(last.at) {
		return acr.Preview{}, acr.ErrConflict
	}
	out := multiRetentionDTO(p, last)
	if acr.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
		return acr.Preview{}, acr.ErrUnavailable
	}
	return out, nil
}
func (s *Store) ReadOwnMultiCandidatePreview(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Preview, error) {
	if !aep.ValidID(id) {
		return acr.Preview{}, acr.ErrInvalid
	}
	tx, b, e := s.beginMultiRetention(ctx, a, false)
	if e != nil {
		return acr.Preview{}, e
	}
	defer tx.Rollback(context.Background())
	p, e := multiRetentionLoad(ctx, tx, id)
	if e != nil {
		return acr.Preview{}, e
	}
	if p.owner != b.owner || p.agent != b.agent {
		return acr.Preview{}, acr.ErrDenied
	}
	var consumed string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_multi_candidate_bindings WHERE preview_id=$1`, id).Scan(&consumed)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return acr.Preview{}, acr.ErrUnavailable
	}
	if consumed != "" {
		at, e := s.finishContextBuilder(ctx, tx, b, nil)
		if e != nil {
			return acr.Preview{}, multiRetentionError(e)
		}
		out := multiRetentionDTO(p, multiRetentionCapture{at: at})
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
	c, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, "", 0, p.algorithm)
	if e != nil {
		return acr.Preview{}, e
	}
	if !multiRetentionMatch(p, c) || !p.end.After(c.at) {
		return acr.Preview{}, acr.ErrConflict
	}
	out := multiRetentionDTO(p, c)
	if acr.ValidatePreview(out) != nil || tx.Commit(ctx) != nil {
		return acr.Preview{}, acr.ErrUnavailable
	}
	return out, nil
}
func multiRetentionGrantTx(ctx context.Context, tx pgx.Tx, b contextBuilderBinding, id string) (acr.Grant, multiRetentionStored, error) {
	var g acr.Grant
	var preview string
	e := tx.QueryRow(ctx, `SELECT g.id,g.resource_id,g.revision,g.created_at,g.expires_at,g.revoked_at FROM consent_grants g JOIN agent_multi_candidate_bindings rb ON rb.grant_id=g.id WHERE g.id=$1 AND g.owner_account_id=$2 AND g.recipient_account_id=$2 AND g.resource_type='agent_context' AND g.purpose='STAGE_MEMORY_CANDIDATE_MULTI' AND g.actions=ARRAY['stage_candidate']::text[] FOR UPDATE OF g`, id, b.owner).Scan(&g.ID, &preview, &g.Revision, &g.CreatedAt, &g.ExpiresAt, &g.RevokedAt)
	if e != nil {
		return g, multiRetentionStored{}, multiRetentionError(e)
	}
	p, e := multiRetentionLoad(ctx, tx, preview)
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
func (s *Store) ApproveOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Grant, error) {
	if !aep.ValidID(id) {
		return acr.Grant{}, acr.ErrInvalid
	}
	tx, b, e := s.beginMultiRetention(ctx, a, true)
	if e != nil {
		return acr.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,80015))`, b.owner); e != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	p, e := multiRetentionLoad(ctx, tx, id)
	if e != nil {
		return acr.Grant{}, e
	}
	if p.owner != b.owner || p.agent != b.agent || p.session != b.session {
		return acr.Grant{}, acr.ErrDenied
	}
	var gid string
	e = tx.QueryRow(ctx, `SELECT grant_id FROM agent_multi_candidate_bindings WHERE preview_id=$1`, id).Scan(&gid)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return acr.Grant{}, acr.ErrUnavailable
	}
	if gid == "" {
		c, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, "", 0, p.algorithm)
		if e != nil {
			return acr.Grant{}, e
		}
		if !multiRetentionMatch(p, c) {
			return acr.Grant{}, acr.ErrConflict
		}
		if !p.end.After(c.at) {
			return acr.Grant{}, acr.ErrExpired
		}
		end := c.end
		contextPurposeLimit(&end, p.end)
		e = tx.QueryRow(ctx, `INSERT INTO consent_grants(id,owner_account_id,recipient_account_id,resource_type,resource_id,purpose,actions,revision,created_at,expires_at) VALUES(gen_random_uuid(),$1,$1,'agent_context',$2,'STAGE_MEMORY_CANDIDATE_MULTI',ARRAY['stage_candidate']::text[],1,$3,$4) RETURNING id`, b.owner, p.id, c.at, end).Scan(&gid)
		if e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
		if _, e = tx.Exec(ctx, `INSERT INTO agent_multi_candidate_bindings(grant_id,preview_id) VALUES($1,$2)`, gid, p.id); e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "grant", "agent_context", gid, "STAGE_MEMORY_CANDIDATE_MULTI", &gid); e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
	}
	g, p, e := multiRetentionGrantTx(ctx, tx, b, gid)
	if e != nil {
		return acr.Grant{}, e
	}
	if g.RevokedAt != nil {
		return acr.Grant{}, acr.ErrExpired
	}
	c, e := s.captureMultiRetention(ctx, tx, b, p.selection, &p.end, g.ID, g.Revision, p.algorithm)
	if e != nil {
		return acr.Grant{}, e
	}
	if !multiRetentionMatch(p, c) || g.ExpiresAt.After(p.end) {
		return acr.Grant{}, acr.ErrConflict
	}
	g.ObservedAt = c.at
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	return g, nil
}
func (s *Store) ReadOwnMultiCandidateGrant(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.Grant, error) {
	if !aep.ValidID(id) {
		return acr.Grant{}, acr.ErrInvalid
	}
	tx, b, e := s.beginMultiRetention(ctx, a, false)
	if e != nil {
		return acr.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := multiRetentionGrantTx(ctx, tx, b, id)
	if e != nil {
		return acr.Grant{}, e
	}
	g.ObservedAt, e = s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return acr.Grant{}, multiRetentionError(e)
	}
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	return g, nil
}
func (s *Store) RevokeOwnMultiCandidate(ctx context.Context, a agentprofile.PrivateAccess, id string, expected int64) (acr.Grant, error) {
	if !aep.ValidID(id) || expected < 1 || expected == int64(^uint64(0)>>1) {
		return acr.Grant{}, acr.ErrInvalid
	}
	tx, b, e := s.beginMultiRetention(ctx, a, true)
	if e != nil {
		return acr.Grant{}, e
	}
	defer tx.Rollback(context.Background())
	g, _, e := multiRetentionGrantTx(ctx, tx, b, id)
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
			return acr.Grant{}, multiRetentionError(e)
		}
		if e = insertDomainAudit(ctx, tx, b.owner, "revoke", "agent_context", id, "STAGE_MEMORY_CANDIDATE_MULTI", &id); e != nil {
			return acr.Grant{}, acr.ErrUnavailable
		}
	}
	g.ObservedAt, e = s.finishContextBuilder(ctx, tx, b, nil)
	if e != nil {
		return acr.Grant{}, multiRetentionError(e)
	}
	if acr.ValidateGrant(g) != nil || tx.Commit(ctx) != nil {
		return acr.Grant{}, acr.ErrUnavailable
	}
	return g, nil
}

// Absent 091 remains compatible only with native manual/single history.
func candidateMultiHistory(ctx context.Context, tx pgx.Tx, ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	var column bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass('public.agent_effect_ledger') AND attname='handler_version' AND NOT attisdropped)`).Scan(&column) != nil {
		return false, agentmemory.ErrUnavailable
	}
	if !column {
		return false, nil
	}
	var multi bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_effect_ledger WHERE candidate_id=ANY($1::uuid[]) AND handler_version='mom-candidate-multi-v1')`, ids).Scan(&multi) != nil {
		return false, agentmemory.ErrUnavailable
	}
	if !multi {
		return false, nil
	}
	var ready bool
	if tx.QueryRow(ctx, multiRetentionGuardSQL).Scan(&ready) != nil || !ready {
		return false, agentmemory.ErrUnavailable
	}
	if tx.QueryRow(ctx, candidatePipelineGateSQL).Scan(&ready) != nil || !ready {
		return false, agentmemory.ErrUnavailable
	}
	if tx.QueryRow(ctx, `SELECT to_regprocedure('public.birdtie_multi_candidate_sources_current(jsonb,uuid,uuid,uuid,text)') IS NOT NULL AND to_regprocedure('public.birdtie_candidate_pipeline_current(uuid)') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='public.agent_memory_candidates'::regclass AND tgname='agent_multi_candidate_human_decision_guard' AND tgenabled='O' AND tgfoid=to_regprocedure('public.birdtie_multi_candidate_human_decision_guard()'))`).Scan(&ready) != nil || !ready {
		return false, agentmemory.ErrUnavailable
	}
	return true, nil
}
func candidateMultiSupportCurrent(ctx context.Context, tx pgx.Tx, b agentPrivateBinding, id string) (bool, error) {
	multi, e := candidateMultiHistory(ctx, tx, []string{id})
	if e != nil {
		return false, e
	}
	if !multi {
		return true, nil
	}
	var current bool
	if tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_effect_ledger e JOIN agent_multi_candidate_bindings b ON b.grant_id=e.retention_grant_id JOIN agent_multi_candidate_previews p ON p.id=b.preview_id WHERE e.candidate_id=$1 AND e.handler_version='mom-candidate-multi-v1' AND p.session_id=$2 AND birdtie_candidate_pipeline_current(e.retention_grant_id))`, id, b.sessionID).Scan(&current) != nil {
		return false, agentmemory.ErrUnavailable
	}
	return current, nil
}

func (s *Store) ReadOwnMultiCandidatePreviewReceipt(ctx context.Context, a agentprofile.PrivateAccess, id string) (acr.PreviewReceipt, error) {
	out, e := s.readOwnApprovalPreviewReceipt(ctx, a, id, true)
	if e != nil {
		return acr.PreviewReceipt{}, multiRetentionError(e)
	}
	return out, nil
}
