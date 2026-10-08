package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

// Only native preparation creates this proof. Public data and a compiled query
// cannot reconstruct its retained City generations or original Task authority.
type nativeResolvedPublicSearch struct {
	context       modelegressbudget.LiveResolvedPublicSearchContext
	compiledQuery string
	evidence      string
	place         nativeResolvedPublicPlace
	valid         bool
}

func liveStandaloneCallScope(scope string) bool {
	return scope == modelegressbudget.Scope || scope == modelegressbudget.LiveResolvedSearchScope
}
func liveModelSourceScope(p PreparedLiveEgress) string {
	if p.resolved.valid {
		return modelegressbudget.LiveResolvedSourceScope
	}
	return modelegressbudget.LivePublicSearchScope
}

func resolvedPublicTaskSlots(task agentworkspace.Task) (modelegressbudget.LiveResolvedSlots, error) {
	target := task.Filters["targetIntent"]
	switch task.Intent {
	case agentworkspace.FindActivity, agentworkspace.FindPlace, agentworkspace.FindOrganization:
		if target != "" && target != task.Intent {
			return modelegressbudget.LiveResolvedSlots{}, modelegressbudget.ErrDenied
		}
		target = task.Intent
	case agentworkspace.AreaDiscovery:
		if target != "" && target != agentworkspace.FindActivity {
			return modelegressbudget.LiveResolvedSlots{}, modelegressbudget.ErrDenied
		}
		target = agentworkspace.FindActivity
	case agentworkspace.RefineResults, agentworkspace.CompareResults:
	default:
		return modelegressbudget.LiveResolvedSlots{}, modelegressbudget.ErrDenied
	}
	return modelegressbudget.LiveResolvedSlots{Operation: task.Intent, Target: target, Category: task.Filters["category"], TimePreference: task.Filters["timePreference"], DistancePreference: task.Filters["distancePreference"], SearchTerm: task.Filters["searchTerm"]}, nil
}

func nativeResolvedPublicCityTx(ctx context.Context, tx pgx.Tx, cityID string, deadline time.Time) (modelegressbudget.LiveSelectedCity, []byte, error) {
	var selected modelegressbudget.LiveSelectedCity
	var nativeCity []byte
	var expiry *time.Time
	e := tx.QueryRow(ctx, `SELECT c.id,c.name,c.country_code,c.expires_at,
 jsonb_build_object('city',c.id,'name',c.name,'country',c.country_code,'updatedAt',c.updated_at,'expiry',c.expires_at,'cityRow',c.xmin::text,'context',cx.id,'contextRow',cx.xmin::text,'stateRow',cc.xmin::text)
 FROM cities c JOIN contexts cx ON cx.city_id=c.id AND cx.context_type='CITY' JOIN city_contexts cc ON cc.city_id=c.id
 WHERE c.id=$1 AND c.publication_status='published' AND cc.status='active'
 AND (c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND c.updated_at<=clock_timestamp() AND cx.created_at<=clock_timestamp() AND cc.updated_at<=clock_timestamp()
 FOR SHARE OF c,cx,cc`, cityID).Scan(&selected.ID, &selected.Name, &selected.CountryCode, &expiry, &nativeCity)
	if e != nil {
		return selected, nil, egressError(e)
	}
	if expiry != nil && deadline.After(*expiry) {
		return selected, nil, modelegressbudget.ErrDenied
	}
	return selected, nativeCity, nil
}

func resolvedPublicContextEvidence(public modelegressbudget.LiveResolvedPublicSearchContext, nativeCity []byte) (string, error) {
	raw, e := json.Marshal(struct {
		Context modelegressbudget.LiveResolvedPublicSearchContext
		City    json.RawMessage
	}{public, nativeCity})
	if e != nil {
		return "", modelegressbudget.ErrDenied
	}
	return liveDigestBytes("birdtie.native-public-search-context.v1", raw), nil
}

// A final reply may legally change Task status to COMPLETED. It must still
// validate the same retained public City generations without minting a new
// dispatch binding from that completed Task.
func (s *Store) revalidateResolvedPublicCityTx(ctx context.Context, tx pgx.Tx, p PreparedLiveEgress) error {
	if !p.resolved.valid {
		return nil
	}
	selected, nativeCity, e := nativeResolvedPublicCityTx(ctx, tx, p.resolved.context.SelectedCity.ID, p.input.DeadlineAt)
	if e != nil || selected != p.resolved.context.SelectedCity {
		return modelegressbudget.ErrDenied
	}
	evidence, e := resolvedPublicContextEvidence(p.resolved.context, nativeCity)
	if p.resolved.place.valid {
		current, placeErr := nativePublicFollowupPlaceTx(ctx, tx, p.resolved.place.id, selected.ID, p.input.DeadlineAt)
		if placeErr != nil || current != p.resolved.place {
			return modelegressbudget.ErrDenied
		}
		evidence, e = resolvedPublicPlaceContextEvidence(p.resolved.context, nativeCity, current)
	}
	if e != nil || evidence != p.resolved.evidence {
		return modelegressbudget.ErrDenied
	}
	return nil
}

func (s *Store) nativeResolvedPublicSearchTx(ctx context.Context, tx pgx.Tx, p PreparedLiveEgress) (nativeResolvedPublicSearch, error) {
	var empty nativeResolvedPublicSearch
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, p.input.TaskID, p.owner))
	if e != nil {
		return empty, egressError(e)
	}
	if task.Status != agentworkspace.TaskActive || task.PrincipalType != "person" || task.PrincipalID != p.owner || task.ActingUserID != p.owner || task.CityID == "" || task.ContextType != "CITY" || task.ContextID == "" {
		return empty, modelegressbudget.ErrDenied
	}
	slots, e := resolvedPublicTaskSlots(task)
	if e != nil {
		return empty, e
	}
	// Match the original PUBLIC_CITY native read shape. The ordinary process
	// context Purpose is not an export grant; the exact preview approves this.
	selected, nativeCity, e := nativeResolvedPublicCityTx(ctx, tx, task.CityID, p.input.DeadlineAt)
	if e != nil {
		return empty, e
	}
	var retainedCity struct {
		Context string `json:"context"`
	}
	if json.Unmarshal(nativeCity, &retainedCity) != nil || retainedCity.Context != task.ContextID {
		return empty, modelegressbudget.ErrDenied
	}
	public := modelegressbudget.LiveResolvedPublicSearchContext{SelectedCity: selected, ResolvedSlots: slots}
	place, term, e := nativeResolvedPlaceFromTaskTx(ctx, tx, task, p.input.DeadlineAt)
	if e != nil {
		return empty, e
	}
	if place.valid {
		public.ResolvedSlots.SearchTerm = term
	}
	compiled, e := modelegressbudget.CompileLiveResolvedPublicSearchQuery(p.query, public)
	if e != nil {
		return empty, e
	}
	evidence, e := resolvedPublicContextEvidence(public, nativeCity)
	if place.valid {
		evidence, e = resolvedPublicPlaceContextEvidence(public, nativeCity, place)
	}
	if e != nil {
		return empty, modelegressbudget.ErrDenied
	}
	return nativeResolvedPublicSearch{context: public, compiledQuery: compiled, evidence: evidence, place: place, valid: true}, nil
}

func liveResolvedDigestInput(p PreparedLiveEgress, scope, purpose string) modelegressbudget.LiveDigestInput {
	return modelegressbudget.LiveDigestInput{Scope: scope, Purpose: purpose, TaskID: p.input.TaskID, RootTraceID: p.input.RootTraceID, BindingID: p.binding, SourceToken: p.source, AuthorityToken: p.authority, CurrentQueryEvidenceDigest: p.queryDigest, EgressPayloadDigest: p.payloadDigest, MaxOutputTokens: p.input.MaxOutputTokens, DeadlineAt: p.input.DeadlineAt}
}

func (s *Store) prepareResolvedLiveEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.PreviewInput) (PreparedLiveEgress, error) {
	p, e := s.prepareLiveEgressTx(ctx, tx, a, in, nil)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if p.price.Kind != modelegressbudget.LiveCall {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p.resolved, e = s.nativeResolvedPublicSearchTx(ctx, tx, p)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	// The current-query digest remains the original raw scalar digest. This
	// separate payload digest describes the actual resolved WSA Query bytes.
	p.payloadDigest = liveWireHash(p.SearchWire())
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	p.digest, e = modelegressbudget.DigestLiveResolvedSearch(modelegressbudget.LiveResolvedDigestInput{Input: liveResolvedDigestInput(p, modelegressbudget.LiveResolvedSearchScope, modelegressbudget.LiveSearchPurpose), ResolvedContextEvidenceDigest: p.resolved.evidence}, p.price, now)
	return p, e
}

// Stored closed scope selects a formatter, never grants authority. Every path
// still re-reads the original current binding, session, Task and native rows.
func (s *Store) prepareStandaloneLivePreviewTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, v storedLiveEgressPreview, projector LiveWireProjector) (PreparedLiveEgress, error) {
	if len(v.sourceBatch) != 0 || v.sourceEvidenceDigest != "" {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	switch v.scope {
	case modelegressbudget.Scope:
		return s.prepareLiveEgressTx(ctx, tx, a, v.in, projector)
	case modelegressbudget.LiveResolvedSearchScope:
		if v.kind != modelegressbudget.LiveCall {
			return PreparedLiveEgress{}, modelegressbudget.ErrDenied
		}
		return s.prepareResolvedLiveEgressTx(ctx, tx, a, v.in)
	default:
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
}

func (s *Store) PreviewOwnResolvedLiveEgress(ctx context.Context, a agentevent.Access, in modelegressbudget.PreviewInput, _ LiveWireProjector) (LiveEgressPreview, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	defer tx.Rollback(ctx)
	p, e := s.prepareResolvedLiveEgressTx(ctx, tx, a, in)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	if e = egressOwnerLock(ctx, tx, p.owner); e != nil {
		return LiveEgressPreview{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return LiveEgressPreview{}, e
	}
	out := livePreview(p, "", "DRAFT")
	e = tx.QueryRow(ctx, `INSERT INTO model_egress_previews(root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,scope,purpose,billing_kind,current_query_digest,egress_payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`, p.input.RootTraceID, p.input.TaskID, p.owner, p.session, p.request.Agent.AgentID, p.binding, p.source, p.authority, p.digest, p.price.Base.Version, p.input.MaxOutputTokens, p.input.DeadlineAt, out.Scope, out.Purpose, p.price.Kind, p.queryDigest, p.payloadDigest).Scan(&out.ID)
	if e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, p.owner, p.input.RootTraceID, "", "PREVIEW"); e != nil {
		return LiveEgressPreview{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return LiveEgressPreview{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	return out, nil
}

func prepareResolvedLiveSourceWire(query string, resolved nativeResolvedPublicSearch, sources []modelegressbudget.LivePublicSource, r modelgateway.Request, price modelegressbudget.LivePrice, projector LiveWireProjector, now time.Time) (modelgateway.PreparedTencentWire, string, string, modelegressbudget.LiveAmount, error) {
	var empty modelgateway.PreparedTencentWire
	if !resolved.valid || resolved.evidence == "" {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	compiled, e := modelegressbudget.CompileLiveResolvedPublicSearchQuery(query, resolved.context)
	if e != nil || compiled != resolved.compiledQuery {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	payload, e := modelegressbudget.LiveResolvedPublicSearchPayload(query, resolved.context, sources)
	if e != nil {
		return empty, "", "", modelegressbudget.LiveAmount{}, e
	}
	if modelgateway.ValidateRequest(r, now) != nil {
		return empty, "", "", modelegressbudget.LiveAmount{}, nativeLiveStage("source-wire-request-validation", modelegressbudget.ErrDenied)
	}
	if modelegressbudget.ValidateLivePrice(price, now) != nil || !modelegressbudget.HasNativeLiveInputBound(price) || r.OutputMode != modelgateway.Text || len(r.ToolAllowlist) != 0 || len(r.Messages) != 2 || r.Messages[0].Role != "system" || r.Messages[1].Role != "user" || r.Messages[1].Content != payload || !liveProjectorPresent(projector) {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	provider := liveProviderRequest(r)
	wire, e := projector.Prepare(liveProviderRequest(r))
	if e != nil || !liveTokenWireMatches(price, provider, wire, now) {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	upper, e := modelegressbudget.BoundLive(price, r.Budget.MaxOutputTokens, now)
	if e != nil || upper.Amount.CostMicros > liveRequestCashLimit {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrBudget
	}
	return wire, liveDigestBytes("birdtie.live-current-query.v1", []byte(query)), wire.WireDigest(), upper, nil
}
