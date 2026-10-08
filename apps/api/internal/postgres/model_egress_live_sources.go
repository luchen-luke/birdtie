package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

// Stored only in the original immutable preview. This is public source data
// and provenance, not a reconstructible permission or another task store.
type liveSourceBatchData struct {
	Evidence modelegressbudget.LiveSourceEvidence `json:"evidence"`
	Sources  []modelegressbudget.LivePublicSource `json:"sources"`
}

// OwnLiveSourceBatch is authenticated server-local input to a separate exact
// source export preview. Possession never approves or releases model input.
type OwnLiveSourceBatch struct {
	data                                                   liveSourceBatchData
	access                                                 agentevent.Access
	owner, session, root, task, binding, source, authority string
	evidenceDigest                                         string
	valid                                                  bool
	association                                            *nativeLiveSourceRunAssociation
}

func (OwnLiveSourceBatch) MarshalJSON() ([]byte, error) { return nil, modelegressbudget.ErrServerOnly }
func (b *OwnLiveSourceBatch) UnmarshalJSON([]byte) error {
	if b != nil {
		*b = OwnLiveSourceBatch{}
	}
	return modelegressbudget.ErrServerOnly
}
func (OwnLiveSourceBatch) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "OwnLiveSourceBatch{redacted}")
}
func (b OwnLiveSourceBatch) Evidence() modelegressbudget.LiveSourceEvidence { return b.data.Evidence }
func (b OwnLiveSourceBatch) EvidenceDigest() string                         { return b.evidenceDigest }
func (b OwnLiveSourceBatch) Sources() []modelegressbudget.LivePublicSource {
	return append([]modelegressbudget.LivePublicSource(nil), b.data.Sources...)
}

func validateLiveSourceBatchData(data liveSourceBatchData, expected string, now time.Time) error {
	digest, err := modelegressbudget.LiveSourceEvidenceDigest(data.Evidence, now)
	if err != nil || digest != expected {
		return modelegressbudget.ErrDenied
	}
	selected, err := modelegressbudget.LivePublicSourcesDigest(data.Sources)
	if err != nil || selected != data.Evidence.SelectedSourcesDigest {
		return modelegressbudget.ErrDenied
	}
	return nil
}

func readLiveSourceBatchData(raw []byte) (liveSourceBatchData, error) {
	var data liveSourceBatchData
	if len(raw) < 1 || len(raw) > 16*1024 {
		return data, modelegressbudget.ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&data) != nil {
		return liveSourceBatchData{}, modelegressbudget.ErrDenied
	}
	if d.Decode(new(any)) != io.EOF {
		return liveSourceBatchData{}, modelegressbudget.ErrDenied
	}
	return data, nil
}

func oneLiveSourceRunAssociation(in []*nativeLiveSourceRunAssociation) (*nativeLiveSourceRunAssociation, error) {
	if len(in) > 1 {
		return nil, modelegressbudget.ErrDenied
	}
	if len(in) == 0 {
		return nil, nil
	}
	return in[0], nil
}

// Closed until an original 088 typed association is supplied. This internal
// seam deliberately does not call the public CheckOwnLiveAttempt method.
// It cannot make a linked operation eligible by supplying a boolean flag.
func checkLiveSourceOperationAssociationTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, batch OwnLiveSourceBatch, association *nativeLiveSourceRunAssociation) error {
	if association == nil {
		return denyLinkedModelOperation(ctx, tx, batch.data.Evidence.OperationID)
	}
	return validateLiveSourceRunAssociationTx(ctx, tx, a, association, batch)
}

// Re-read the original approved CALL and current authority under the same
// owner lock and source locks. UNKNOWN remains a full hold, never success or
// free cash. Actual successful output is established only by batch minting.
func (s *Store) revalidateLiveSourceBatchTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, b OwnLiveSourceBatch, association *nativeLiveSourceRunAssociation) (PreparedLiveEgress, error) {
	var empty PreparedLiveEgress
	if !b.valid || b.access != a || a.SessionDigest == ([32]byte{}) || !egressUUID(b.data.Evidence.OperationID) {
		return empty, modelegressbudget.ErrDenied
	}
	if b.association != association {
		return empty, modelegressbudget.ErrDenied
	}
	if e := checkLiveSourceOperationAssociationTx(ctx, tx, a, b, association); e != nil {
		return empty, e
	}
	r, kind, e := readLiveReservation(ctx, tx, b.data.Evidence.OperationID, b.owner)
	if e != nil {
		return empty, e
	}
	var cash string
	if e = tx.QueryRow(ctx, `SELECT cash_status FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2`, r.OperationID, b.owner).Scan(&cash); e != nil {
		return empty, egressError(e)
	}
	if kind != modelegressbudget.LiveCall || cash != "UNKNOWN" || r.State != "UNKNOWN" || r.ExecutionStatus != "LIVE_ATTEMPTED" || r.Upper != (modelegressbudget.Amount{CostMicros: modelegressbudget.LiveSearchCallMicros}) || r.PreviewID != b.data.Evidence.PreviewID || r.RequestDigest != b.data.Evidence.SourceRequestDigest || r.RootTraceID != b.root || r.TaskID != b.task || b.data.Evidence.ObservedAt.Before(r.CreatedAt) {
		return empty, modelegressbudget.ErrDenied
	}
	v, e := readLiveEgressPreview(ctx, tx, r.PreviewID)
	if e != nil {
		return empty, e
	}
	if v.status != "APPROVED" || !liveStandaloneCallScope(v.scope) || v.kind != modelegressbudget.LiveCall || len(v.sourceBatch) != 0 || v.sourceEvidenceDigest != "" || v.owner != b.owner || v.session != b.session || v.binding != b.binding || v.source != b.source || v.authority != b.authority || !v.in.DeadlineAt.Equal(b.data.Evidence.DeadlineAt) {
		return empty, modelegressbudget.ErrDenied
	}
	p, e := s.prepareStandaloneLivePreviewTx(ctx, tx, a, v, nil)
	if e != nil {
		return empty, e
	}
	if !matchLivePreview(v, p) || p.digest != b.data.Evidence.SourceRequestDigest || p.payloadDigest != b.data.Evidence.SourcePayloadDigest || p.queryDigest != b.data.Evidence.QueryEvidenceDigest || p.owner != b.owner || p.session != b.session {
		return empty, modelegressbudget.ErrDenied
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return empty, egressError(e)
	}
	if e = validateLiveSourceBatchData(b.data, b.evidenceDigest, now); e != nil {
		return empty, e
	}
	return p, nil
}

func sourceBatchFromOutput(a agentevent.Access, output OwnLiveSearchOutput, now time.Time) (OwnLiveSourceBatch, error) {
	var empty OwnLiveSourceBatch
	p := output.prepared
	if a.SessionDigest == ([32]byte{}) || output.access != a || !egressUUID(output.operation) || p.price.Kind != modelegressbudget.LiveCall || output.query != p.SearchQuery() || output.result.CashStatus != "UNKNOWN" || !egressUUID(output.result.RequestID) || strings.TrimSpace(output.result.Version) == "" || len(output.result.Version) > 128 || !p.input.DeadlineAt.After(now) {
		return empty, modelegressbudget.ErrDenied
	}
	sources := make([]modelegressbudget.LivePublicSource, len(output.result.Sources))
	for i, source := range output.result.Sources {
		sources[i] = modelegressbudget.LivePublicSource{Title: source.Title, URL: source.URL, Passage: source.Passage}
	}
	selected, e := selectLiveModelMessageSources(p, sources)
	if e != nil {
		return empty, e
	}
	selectedDigest, e := modelegressbudget.LivePublicSourcesDigest(selected)
	if e != nil {
		return empty, e
	}
	artifact, e := json.Marshal(output.result)
	if e != nil {
		return empty, modelegressbudget.ErrInvalid
	}
	evidence := modelegressbudget.LiveSourceEvidence{OperationID: output.operation, ProviderRequestID: output.result.RequestID, SourceRequestDigest: p.digest, SourcePayloadDigest: p.payloadDigest, QueryEvidenceDigest: p.queryDigest, ArtifactSHA256: liveDigestBytes("birdtie.public-search-original-artifact.v1", artifact), SelectedSourcesDigest: selectedDigest, ObservedAt: output.observedAt, DeadlineAt: p.input.DeadlineAt}
	// Original preview ID must be resolved from the actual reservation in the
	// transaction; no provider field or JSON caller can claim that association.
	return OwnLiveSourceBatch{data: liveSourceBatchData{Evidence: evidence, Sources: selected}, access: a, owner: p.owner, session: p.session, root: p.input.RootTraceID, task: p.input.TaskID, binding: p.binding, source: p.source, authority: p.authority}, nil
}

// The original AIR message contract permits at most 4096 bytes per message.
// The source data formatter's larger total bound does not enlarge that contract.
// Select an ordered prefix before authenticating its exact selection digest;
// the separate artifact digest still describes every original provider source.
const liveSourceModelMessageMaxBytes = 4096

func liveSourceModelPayload(p PreparedLiveEgress, sources []modelegressbudget.LivePublicSource) (string, error) {
	if p.resolved.valid {
		if len(p.request.Messages) != 2 || p.request.Messages[0].Role != "system" || p.request.Messages[1].Role != "user" {
			return "", modelegressbudget.ErrDenied
		}
		// Use the exact original scalar, including whitespace. The compiled
		// search query is separate and is never substituted for this question.
		return modelegressbudget.LiveResolvedPublicSearchPayload(p.request.Messages[1].Content, p.resolved.context, sources)
	}
	return modelegressbudget.LivePublicSearchPayload(p.query, sources)
}

func selectLiveModelMessageSources(p PreparedLiveEgress, sources []modelegressbudget.LivePublicSource) ([]modelegressbudget.LivePublicSource, error) {
	canonical, e := modelegressbudget.SelectLivePublicSources(sources)
	if e != nil {
		return nil, nativeLiveStage("source-payload-selection", e)
	}
	count := 0
	for n := 1; n <= len(canonical); n++ {
		payload, e := liveSourceModelPayload(p, canonical[:n])
		if e != nil {
			return nil, nativeLiveStage("source-payload-encoding", e)
		}
		if len(payload) > liveSourceModelMessageMaxBytes {
			break
		}
		count = n
	}
	if count == 0 {
		// No empty or invented source context can stand in for an oversized
		// first result. Titles and URLs are not truncated or silently replaced.
		return nil, nativeLiveStage("source-payload-message-bound", modelegressbudget.ErrDenied)
	}
	return append([]modelegressbudget.LivePublicSource(nil), canonical[:count]...), nil
}

func (s *Store) authenticateLiveSearchSourcesTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, output OwnLiveSearchOutput, association *nativeLiveSourceRunAssociation) (OwnLiveSourceBatch, error) {
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return OwnLiveSourceBatch{}, egressError(e)
	}
	b, e := sourceBatchFromOutput(a, output, now)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	r, _, e := readLiveReservation(ctx, tx, output.operation, b.owner)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	b.data.Evidence.PreviewID = r.PreviewID
	b.evidenceDigest, e = modelegressbudget.LiveSourceEvidenceDigest(b.data.Evidence, now)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	b.valid = true
	b.association = association
	if _, e = s.revalidateLiveSourceBatchTx(ctx, tx, a, b, association); e != nil {
		return OwnLiveSourceBatch{}, e
	}
	return b, nil
}

func (s *Store) AuthenticateOwnLiveSearchSources(ctx context.Context, a agentevent.Access, output OwnLiveSearchOutput) (OwnLiveSourceBatch, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	if output.prepared.owner != owner || output.prepared.session != session {
		return OwnLiveSourceBatch{}, modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return OwnLiveSourceBatch{}, e
	}
	b, e := s.authenticateLiveSearchSourcesTx(ctx, tx, a, output, nil)
	if e != nil {
		return OwnLiveSourceBatch{}, e
	}
	if e = egressFinish(ctx, tx, session, nil, b.data.Evidence.DeadlineAt); e != nil {
		return OwnLiveSourceBatch{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return OwnLiveSourceBatch{}, egressError(e)
	}
	return b, nil
}

// Separate typed branch: the original query-only wire validator is unchanged.
func prepareLiveSourceWire(query string, sources []modelegressbudget.LivePublicSource, r modelgateway.Request, price modelegressbudget.LivePrice, projector LiveWireProjector, now time.Time) (modelgateway.PreparedTencentWire, string, string, modelegressbudget.LiveAmount, error) {
	var empty modelgateway.PreparedTencentWire
	payload, e := modelegressbudget.LivePublicSearchPayload(strings.TrimSpace(query), sources)
	if e != nil {
		return empty, "", "", modelegressbudget.LiveAmount{}, e
	}
	if modelgateway.ValidateRequest(r, now) != nil {
		return empty, "", "", modelegressbudget.LiveAmount{}, nativeLiveStage("source-wire-request-validation", modelegressbudget.ErrDenied)
	}
	if modelegressbudget.ValidateLivePrice(price, now) != nil || price.Kind != modelegressbudget.LiveToken || price.Base.InputTokenCeiling != modelgateway.TencentLiveMaxInputTokens || r.OutputMode != modelgateway.Text || len(r.ToolAllowlist) != 0 || len(r.Messages) != 2 || r.Messages[0].Role != "system" || r.Messages[1].Role != "user" || r.Messages[1].Content != payload || !liveProjectorPresent(projector) {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	provider := liveProviderRequest(r)
	wire, e := projector.Prepare(liveProviderRequest(r))
	if e != nil || !wire.Matches(provider, now) || wire.InputTokenBound() != modelgateway.TencentLiveMaxInputTokens || wire.InputBoundEvidence() != modelgateway.TencentLiveInputBoundEvidence || wire.InputBoundSource() != modelgateway.TencentLiveInputBoundSource {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	upper, e := modelegressbudget.BoundLive(price, r.Budget.MaxOutputTokens, now)
	if e != nil || upper.Amount.CostMicros > liveRequestCashLimit {
		return empty, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrBudget
	}
	return wire, liveDigestBytes("birdtie.live-current-query.v1", []byte(query)), wire.WireDigest(), upper, nil
}

func (s *Store) prepareLiveSourceEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.PreviewInput, b OwnLiveSourceBatch, projector LiveWireProjector) (PreparedLiveEgress, error) {
	source, e := s.revalidateLiveSourceBatchTx(ctx, tx, a, b, b.association)
	if e != nil {
		return PreparedLiveEgress{}, nativeLiveStage("source-batch-current", e)
	}
	// Native query-only preparation supplies the original 058 prompt and all
	// current bindings. Only its already-approved current user query is replaced
	// by this separately scoped, canonical public evidence envelope.
	p, e := s.prepareLiveEgressTx(ctx, tx, a, in, projector)
	if e != nil {
		return PreparedLiveEgress{}, nativeLiveStage("source-native-query-preparation", e)
	}
	if p.price.Kind != modelegressbudget.LiveToken || p.owner != b.owner || p.session != b.session || p.input.RootTraceID != b.root || p.input.TaskID != b.task || p.binding != b.binding || p.source != b.source || p.authority != b.authority || p.query != source.query || p.queryDigest != b.data.Evidence.QueryEvidenceDigest || p.input.DeadlineAt.After(b.data.Evidence.DeadlineAt) {
		return PreparedLiveEgress{}, nativeLiveStage("source-model-identity", modelegressbudget.ErrDenied)
	}
	var payload string
	exactCurrentQuery := p.request.Messages[1].Content
	if source.resolved.valid {
		p.resolved, e = s.nativeResolvedPublicSearchTx(ctx, tx, p)
		if e != nil || p.resolved != source.resolved {
			return PreparedLiveEgress{}, nativeLiveStage("source-city-current", modelegressbudget.ErrDenied)
		}
		payload, e = modelegressbudget.LiveResolvedPublicSearchPayload(exactCurrentQuery, p.resolved.context, b.data.Sources)
	} else {
		payload, e = modelegressbudget.LivePublicSearchPayload(p.query, b.data.Sources)
	}
	if e != nil {
		return PreparedLiveEgress{}, nativeLiveStage("source-payload-encoding", e)
	}
	p.request.Messages = append([]modelgateway.Message(nil), p.request.Messages...)
	p.request.Messages[1].Content = payload
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	if p.resolved.valid {
		p.modelWire, p.queryDigest, p.payloadDigest, p.upper, e = prepareResolvedLiveSourceWire(exactCurrentQuery, p.resolved, b.data.Sources, p.request, p.price, projector, now)
	} else {
		p.modelWire, p.queryDigest, p.payloadDigest, p.upper, e = prepareLiveSourceWire(exactCurrentQuery, b.data.Sources, p.request, p.price, projector, now)
	}
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	input := modelegressbudget.LiveSourceExportDigestInput{Input: liveResolvedDigestInput(p, liveModelSourceScope(p), modelegressbudget.Purpose), Evidence: b.data.Evidence}
	if p.resolved.valid {
		p.digest, e = modelegressbudget.DigestLiveResolvedSourceExport(modelegressbudget.LiveResolvedSourceExportDigestInput{Input: input, ResolvedContextEvidenceDigest: p.resolved.evidence}, p.price, now)
	} else {
		p.digest, e = modelegressbudget.DigestLiveSourceExport(input, p.price, now)
	}
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	p.sourceBatch = b
	return p, nil
}

// Reconstruct only data from trusted storage, then authenticate against the
// original CALL and current authority. This is never an HTTP permission parser.
func (s *Store) prepareStoredLiveEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, v storedLiveEgressPreview, projector LiveWireProjector) (PreparedLiveEgress, error) {
	return s.prepareStoredLiveEgressWithSourceRunTx(ctx, tx, a, v, projector, nil)
}

func (s *Store) prepareStoredLiveEgressWithSourceRunTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, v storedLiveEgressPreview, projector LiveWireProjector, association *nativeLiveSourceRunAssociation) (PreparedLiveEgress, error) {
	if liveStandaloneCallScope(v.scope) {
		return s.prepareStandaloneLivePreviewTx(ctx, tx, a, v, projector)
	}
	if (v.scope != modelegressbudget.LivePublicSearchScope && v.scope != modelegressbudget.LiveResolvedSourceScope) || v.kind != modelegressbudget.LiveToken {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	data, e := readLiveSourceBatchData(v.sourceBatch)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	b := OwnLiveSourceBatch{data: data, access: a, owner: v.owner, session: v.session, root: v.in.RootTraceID, task: v.in.TaskID, binding: v.binding, source: v.source, authority: v.authority, evidenceDigest: v.sourceEvidenceDigest, valid: true, association: association}
	return s.prepareLiveSourceEgressTx(ctx, tx, a, v.in, b, projector)
}

func (s *Store) PreviewOwnLiveSourceEgress(ctx context.Context, a agentevent.Access, in modelegressbudget.PreviewInput, b OwnLiveSourceBatch, projector LiveWireProjector) (LiveEgressPreview, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	if !b.valid || b.owner != owner || b.session != session || b.access != a {
		return LiveEgressPreview{}, modelegressbudget.ErrDenied
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return LiveEgressPreview{}, e
	}
	out, e := s.previewLiveSourceEgressTx(ctx, tx, a, in, b, projector)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	return out, nil
}

func (s *Store) previewLiveSourceEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.PreviewInput, b OwnLiveSourceBatch, projector LiveWireProjector) (LiveEgressPreview, error) {
	p, e := s.prepareLiveSourceEgressTx(ctx, tx, a, in, b, projector)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	raw, e := json.Marshal(b.data)
	if e != nil || len(raw) > 16*1024 {
		return LiveEgressPreview{}, modelegressbudget.ErrInvalid
	}
	out := livePreview(p, "", "DRAFT")
	e = tx.QueryRow(ctx, `INSERT INTO model_egress_previews(root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,scope,purpose,billing_kind,current_query_digest,egress_payload_digest,source_batch,source_evidence_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING id`, p.input.RootTraceID, p.input.TaskID, p.owner, p.session, p.request.Agent.AgentID, p.binding, p.source, p.authority, p.digest, p.price.Base.Version, p.input.MaxOutputTokens, p.input.DeadlineAt, out.Scope, out.Purpose, p.price.Kind, p.queryDigest, p.payloadDigest, raw, b.evidenceDigest).Scan(&out.ID)
	if e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, p.owner, p.input.RootTraceID, "", "PREVIEW"); e != nil {
		return LiveEgressPreview{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return LiveEgressPreview{}, e
	}
	return out, nil
}
