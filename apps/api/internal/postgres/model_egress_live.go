package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

const liveRequestCashLimit int64 = 200_000
const liveOwnerCashLimit int64 = 10_000_000
const liveRootCashLimit int64 = modelegressbudget.LiveSearchCallMicros + modelgateway.TencentLiveMaxInputTokens + 4*modelgateway.TencentLiveMaxOutputTokens

// LiveWireProjector is a trusted server dependency. The native store supplies
// its current query and the original 058 prompt; callers cannot supply either.
// Prepare does not contact the provider, approve egress or reserve any budget.
type LiveWireProjector interface {
	Prepare(modelgateway.ProviderRequest) (modelgateway.PreparedTencentWire, error)
}

// PreparedLiveEgress holds no transferable permission. Dispatch must consume
// the original reservation and revalidate current facts immediately before use.
// In particular, this type cannot be reconstructed from HTTP/JSON input.
type PreparedLiveEgress struct {
	input                                              modelegressbudget.PreviewInput
	request                                            modelgateway.Request
	price                                              modelegressbudget.LivePrice
	modelWire                                          modelgateway.PreparedTencentWire
	query, queryDigest, payloadDigest                  string
	owner, session, binding, source, authority, digest string
	rootExpiry                                         time.Time
	upper                                              modelegressbudget.LiveAmount
	sourceBatch                                        OwnLiveSourceBatch
	resolved                                           nativeResolvedPublicSearch
}

func (PreparedLiveEgress) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "PreparedLiveEgress{redacted}")
}
func (PreparedLiveEgress) MarshalJSON() ([]byte, error) { return nil, modelegressbudget.ErrServerOnly }
func (p *PreparedLiveEgress) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = PreparedLiveEgress{}
	}
	return modelegressbudget.ErrServerOnly
}
func (p PreparedLiveEgress) Kind() modelegressbudget.LiveChargeKind      { return p.price.Kind }
func (p PreparedLiveEgress) RequestDigest() string                       { return p.digest }
func (p PreparedLiveEgress) PayloadDigest() string                       { return p.payloadDigest }
func (p PreparedLiveEgress) QueryEvidenceDigest() string                 { return p.queryDigest }
func (p PreparedLiveEgress) Upper() modelegressbudget.LiveAmount         { return p.upper }
func (p PreparedLiveEgress) DeadlineAt() time.Time                       { return p.input.DeadlineAt }
func (p PreparedLiveEgress) ModelWire() modelgateway.PreparedTencentWire { return p.modelWire }
func (p PreparedLiveEgress) SearchQuery() string {
	if p.price.Kind != modelegressbudget.LiveCall {
		return ""
	}
	if p.resolved.valid {
		return p.resolved.compiledQuery
	}
	return p.query
}
func (p PreparedLiveEgress) SearchWire() []byte {
	if p.price.Kind != modelegressbudget.LiveCall {
		return nil
	}
	b, _ := json.Marshal(struct {
		Query string `json:"Query"`
	}{p.SearchQuery()})
	return b
}
func (p PreparedLiveEgress) Request() modelgateway.Request {
	if p.price.Kind != modelegressbudget.LiveToken {
		return modelgateway.Request{}
	}
	r := p.request
	r.Messages = append([]modelgateway.Message(nil), r.Messages...)
	r.ToolAllowlist = append([]string(nil), r.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string(nil), r.CapabilitiesRequired...)
	return r
}

type LiveEgressPreview struct {
	ID, RequestDigest, PriceVersion, RootTraceID, TaskID, Scope, Purpose string
	Kind                                                                 modelegressbudget.LiveChargeKind
	Upper                                                                modelegressbudget.LiveAmount
	ExpiresAt                                                            time.Time
	Status                                                               string
	prepared                                                             PreparedLiveEgress
}

func (LiveEgressPreview) MarshalJSON() ([]byte, error) { return nil, modelegressbudget.ErrServerOnly }
func (p *LiveEgressPreview) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = LiveEgressPreview{}
	}
	return modelegressbudget.ErrServerOnly
}
func (LiveEgressPreview) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "LiveEgressPreview{redacted}")
}
func (p LiveEgressPreview) Prepared() PreparedLiveEgress { return p.prepared }

func liveDigestBytes(domain string, raw []byte) string {
	h := sha256.Sum256(append([]byte(domain+"\x00"), raw...))
	return hex.EncodeToString(h[:])
}
func liveWireHash(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func liveProjectorPresent(p LiveWireProjector) bool {
	if p == nil {
		return false
	}
	v := reflect.ValueOf(p)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return !v.IsNil()
	}
	return true
}
func liveProviderRequest(r modelgateway.Request) modelgateway.ProviderRequest {
	return modelgateway.ProviderRequest{TaskKind: r.TaskKind, PromptVersion: r.PromptVersion, Messages: append([]modelgateway.Message(nil), r.Messages...), OutputMode: r.OutputMode, OutputSchemaVersion: r.OutputSchemaVersion, ToolAllowlist: append([]string(nil), r.ToolAllowlist...), MaxOutputTokens: r.Budget.MaxOutputTokens, DeadlineAt: r.DeadlineAt}
}

// This check supplements the published tariff helper with the exact private
// wire proof. No configured lower input ceiling is accepted for live HY3.
func prepareLiveWire(query string, r modelgateway.Request, price modelegressbudget.LivePrice, projector LiveWireProjector, now time.Time) (modelgateway.PreparedTencentWire, string, string, modelegressbudget.LiveAmount, error) {
	if modelegressbudget.ValidateLivePrice(price, now) != nil || strings.TrimSpace(query) == "" || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return modelgateway.PreparedTencentWire{}, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	queryDigest := liveDigestBytes("birdtie.live-current-query.v1", []byte(query))
	if price.Kind == modelegressbudget.LiveCall {
		q := strings.TrimSpace(query)
		if len(q) > 240 {
			return modelgateway.PreparedTencentWire{}, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrInvalid
		}
		body, e := json.Marshal(struct {
			Query string `json:"Query"`
		}{q})
		if e != nil {
			return modelgateway.PreparedTencentWire{}, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrInvalid
		}
		upper, e := modelegressbudget.BoundLive(price, 0, now)
		return modelgateway.PreparedTencentWire{}, queryDigest, liveWireHash(body), upper, e
	}
	if price.Kind != modelegressbudget.LiveToken || price.Base.InputTokenCeiling != modelgateway.TencentLiveMaxInputTokens || r.OutputMode != modelgateway.Text || len(r.Messages) != 2 || r.Messages[0].Role != "system" || r.Messages[1].Role != "user" || r.Messages[1].Content != query || !liveProjectorPresent(projector) || modelgateway.ValidateRequest(r, now) != nil {
		return modelgateway.PreparedTencentWire{}, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	provider := liveProviderRequest(r)
	// A projector cannot mutate the independent native comparison snapshot by
	// editing slices in its provider-only argument.
	wire, e := projector.Prepare(liveProviderRequest(r))
	if e != nil || !wire.Matches(provider, now) || wire.InputTokenBound() != modelgateway.TencentLiveMaxInputTokens || wire.InputBoundEvidence() != modelgateway.TencentLiveInputBoundEvidence || wire.InputBoundSource() != modelgateway.TencentLiveInputBoundSource {
		return modelgateway.PreparedTencentWire{}, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrDenied
	}
	upper, e := modelegressbudget.BoundLive(price, r.Budget.MaxOutputTokens, now)
	if e != nil || upper.Amount.CostMicros > liveRequestCashLimit {
		return modelgateway.PreparedTencentWire{}, "", "", modelegressbudget.LiveAmount{}, modelegressbudget.ErrBudget
	}
	return wire, queryDigest, wire.WireDigest(), upper, nil
}

func readLivePrice(ctx context.Context, tx pgx.Tx, version string) (p modelegressbudget.LivePrice, err error) {
	err = tx.QueryRow(ctx, `SELECT version,provider_id,model_id,model_version,wire_contract,region,retention,currency,input_rate,output_rate,input_ceiling,output_ceiling,evidence,expires_at,billing_kind,request_ceiling,call_micros,snapshot_source_url,snapshot_sha256,snapshot_document_updated_at,snapshot_observed_at,snapshot_expires_at FROM model_local_price_versions WHERE version=$1 AND billing_kind IN ('TOKEN','CALL') FOR SHARE`, version).Scan(&p.Base.Version, &p.Base.Destination.Provider, &p.Base.Destination.Model, &p.Base.Destination.Version, &p.Base.Destination.WireContract, &p.Base.Region, &p.Base.Retention, &p.Base.Currency, &p.Base.InputMicrosPerToken, &p.Base.OutputMicrosPerToken, &p.Base.InputTokenCeiling, &p.Base.OutputTokenCeiling, &p.Base.Evidence, &p.Base.ExpiresAt, &p.Kind, &p.RequestCeiling, &p.CallMicros, &p.Snapshot.SourceURL, &p.Snapshot.ArtifactSHA256, &p.Snapshot.DocumentUpdatedAt, &p.Snapshot.ObservedAt, &p.Snapshot.ExpiresAt)
	return p, egressError(err)
}

// RegisterLivePrice is server maintenance, not a tool, provider grant or
// accounting receipt. It shares the immutable original 062 price table.
func (s *Store) RegisterLivePrice(ctx context.Context, p modelegressbudget.LivePrice) error {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return egressError(e)
	}
	if e = modelegressbudget.ValidateLivePrice(p, now); e != nil {
		return e
	}
	if p.Kind == modelegressbudget.LiveToken && p.Base.InputTokenCeiling != modelgateway.TencentLiveMaxInputTokens {
		return modelegressbudget.ErrInvalid
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_local_price_versions(version,provider_id,model_id,model_version,wire_contract,region,retention,currency,input_rate,output_rate,input_ceiling,output_ceiling,evidence,expires_at,billing_kind,request_ceiling,call_micros,snapshot_source_url,snapshot_sha256,snapshot_document_updated_at,snapshot_observed_at,snapshot_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) ON CONFLICT DO NOTHING`, p.Base.Version, p.Base.Destination.Provider, p.Base.Destination.Model, p.Base.Destination.Version, p.Base.Destination.WireContract, p.Base.Region, p.Base.Retention, p.Base.Currency, p.Base.InputMicrosPerToken, p.Base.OutputMicrosPerToken, p.Base.InputTokenCeiling, p.Base.OutputTokenCeiling, p.Base.Evidence, p.Base.ExpiresAt, p.Kind, p.RequestCeiling, p.CallMicros, p.Snapshot.SourceURL, p.Snapshot.ArtifactSHA256, p.Snapshot.DocumentUpdatedAt, p.Snapshot.ObservedAt, p.Snapshot.ExpiresAt)
	if e != nil {
		return egressError(e)
	}
	stored, e := readLivePrice(ctx, tx, p.Base.Version)
	if e != nil {
		return e
	}
	if !equalLivePrice(stored, p) {
		return modelegressbudget.ErrConflict
	}
	return egressError(tx.Commit(ctx))
}
func equalLivePrice(a, b modelegressbudget.LivePrice) bool {
	a.Base.ExpiresAt = a.Base.ExpiresAt.UTC()
	b.Base.ExpiresAt = b.Base.ExpiresAt.UTC()
	a.Snapshot.DocumentUpdatedAt = a.Snapshot.DocumentUpdatedAt.UTC()
	b.Snapshot.DocumentUpdatedAt = b.Snapshot.DocumentUpdatedAt.UTC()
	a.Snapshot.ObservedAt = a.Snapshot.ObservedAt.UTC()
	b.Snapshot.ObservedAt = b.Snapshot.ObservedAt.UTC()
	a.Snapshot.ExpiresAt = a.Snapshot.ExpiresAt.UTC()
	b.Snapshot.ExpiresAt = b.Snapshot.ExpiresAt.UTC()
	return a == b
}
func (s *Store) ReadLivePrice(ctx context.Context, version string) (modelegressbudget.LivePrice, error) {
	if !modelconfiguration.ValidVersion(version) {
		return modelegressbudget.LivePrice{}, modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelegressbudget.LivePrice{}, e
	}
	defer tx.Rollback(ctx)
	p, e := readLivePrice(ctx, tx, version)
	if e != nil {
		return p, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.LivePrice{}, egressError(e)
	}
	return p, nil
}

func (s *Store) prepareLiveEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.PreviewInput, projector LiveWireProjector) (p PreparedLiveEgress, err error) {
	in.DeadlineAt = in.DeadlineAt.UTC().Truncate(time.Microsecond)
	if !egressUUID(in.RootTraceID) || !egressUUID(in.TaskID) || !modelconfiguration.ValidVersion(in.PriceVersion) {
		return p, modelegressbudget.ErrInvalid
	}
	root, rt, e := s.currentEgressRoot(ctx, tx, a, in.RootTraceID)
	if e != nil {
		return p, e
	}
	v, e := readEgressTask(ctx, tx, in.RootTraceID, in.TaskID)
	if e != nil {
		return p, e
	}
	current, authority, e := s.egressTask(ctx, tx, a, in.TaskID, v.binding)
	if e != nil {
		return p, e
	}
	if current.agent != rt.agent || v.owner != root.owner || v.source != current.version.Token || v.authority != authority {
		return p, modelegressbudget.ErrDenied
	}
	price, e := readLivePrice(ctx, tx, in.PriceVersion)
	if e != nil {
		return p, e
	}
	if modelegressbudget.ValidateLivePrice(price, current.now) != nil || root.currency != "CNY" || price.Base.Currency != root.currency || !in.DeadlineAt.After(current.now) || in.DeadlineAt.After(current.now.Add(modelgateway.MaxDeadline)) || in.DeadlineAt.After(root.expires) || in.DeadlineAt.After(price.Base.ExpiresAt) {
		return p, modelegressbudget.ErrDenied
	}
	if (price.Kind == modelegressbudget.LiveCall && in.MaxOutputTokens != 0) || (price.Kind == modelegressbudget.LiveToken && (in.MaxOutputTokens < 1 || in.MaxOutputTokens > modelgateway.TencentLiveMaxOutputTokens)) {
		return p, modelegressbudget.ErrInvalid
	}
	b, e := readTaskModelBinding(ctx, tx, v.binding)
	if e != nil {
		return p, egressError(e)
	}
	config, e := readModelConfiguration(ctx, tx, b.Reference.ConfigurationVersion)
	if e != nil {
		return p, egressError(e)
	}
	if config.Configuration.TaskKind != modelgateway.ActivityQuery || config.Configuration.OutputMode != modelgateway.Text || len(config.Configuration.ToolAllowlist) != 0 {
		return p, modelegressbudget.ErrDenied
	}
	var original string
	var filters, conversation []byte
	if e = tx.QueryRow(ctx, `SELECT query,filters,conversation FROM agent_tasks WHERE id=$1`, in.TaskID).Scan(&original, &filters, &conversation); e != nil {
		return p, egressError(e)
	}
	query, e := currentEgressQuery(original, filters, conversation)
	if e != nil {
		return p, e
	}
	maxOut := in.MaxOutputTokens
	if price.Kind == modelegressbudget.LiveCall {
		maxOut = 1
	}
	r := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: v.binding, Agent: current.agent, ContextSnapshotRef: in.TaskID, DataPolicyRef: in.RootTraceID, BudgetRef: in.RootTraceID, Budget: modelgateway.Budget{MaxOutputTokens: maxOut}, Messages: []modelgateway.Message{{Role: "user", Content: query}}, DeadlineAt: in.DeadlineAt}
	r, _, e = modelconfiguration.PrepareRequest(config, r, current.now)
	if e != nil {
		return p, modelegressbudget.ErrDenied
	}
	wire, queryDigest, payloadDigest, upper, e := prepareLiveWire(query, r, price, projector, current.now)
	if e != nil {
		return p, e
	}
	purpose := modelegressbudget.Purpose
	if price.Kind == modelegressbudget.LiveCall {
		purpose = modelegressbudget.LiveSearchPurpose
	}
	digest, e := modelegressbudget.DigestLive(modelegressbudget.LiveDigestInput{Scope: modelegressbudget.Scope, Purpose: purpose, TaskID: in.TaskID, RootTraceID: in.RootTraceID, BindingID: v.binding, SourceToken: v.source, AuthorityToken: authority, CurrentQueryEvidenceDigest: queryDigest, EgressPayloadDigest: payloadDigest, MaxOutputTokens: in.MaxOutputTokens, DeadlineAt: in.DeadlineAt}, price, current.now)
	if e != nil {
		return p, e
	}
	p = PreparedLiveEgress{input: in, request: r, price: price, modelWire: wire, query: strings.TrimSpace(query), queryDigest: queryDigest, payloadDigest: payloadDigest, owner: root.owner, session: current.sessionID, binding: v.binding, source: v.source, authority: authority, digest: digest, rootExpiry: root.expires, upper: upper}
	return p, nil
}
func finishLivePreparation(ctx context.Context, tx pgx.Tx, p PreparedLiveEgress) error {
	return egressFinish(ctx, tx, p.session, &p.request, p.rootExpiry, p.price.Base.ExpiresAt, p.input.DeadlineAt)
}
func (s *Store) PrepareOwnLiveEgress(ctx context.Context, a agentevent.Access, in modelegressbudget.PreviewInput, projector LiveWireProjector) (PreparedLiveEgress, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	defer tx.Rollback(ctx)
	p, e := s.prepareLiveEgressTx(ctx, tx, a, in, projector)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = egressOwnerLock(ctx, tx, p.owner); e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	return p, nil
}
func livePreview(p PreparedLiveEgress, id, status string) LiveEgressPreview {
	scope := modelegressbudget.Scope
	if p.sourceBatch.valid {
		scope = modelegressbudget.LivePublicSearchScope
	}
	if p.resolved.valid {
		scope = modelegressbudget.LiveResolvedSearchScope
		if p.sourceBatch.valid {
			scope = modelegressbudget.LiveResolvedSourceScope
		}
	}
	purpose := modelegressbudget.Purpose
	if p.price.Kind == modelegressbudget.LiveCall {
		purpose = modelegressbudget.LiveSearchPurpose
	}
	return LiveEgressPreview{ID: id, RequestDigest: p.digest, PriceVersion: p.price.Base.Version, RootTraceID: p.input.RootTraceID, TaskID: p.input.TaskID, Scope: scope, Purpose: purpose, Kind: p.price.Kind, Upper: p.upper, ExpiresAt: p.input.DeadlineAt, Status: status, prepared: p}
}
func (s *Store) PreviewOwnLiveEgress(ctx context.Context, a agentevent.Access, in modelegressbudget.PreviewInput, projector LiveWireProjector) (LiveEgressPreview, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	defer tx.Rollback(ctx)
	p, e := s.prepareLiveEgressTx(ctx, tx, a, in, projector)
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
	e = tx.QueryRow(ctx, `INSERT INTO model_egress_previews(root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,purpose,billing_kind,current_query_digest,egress_payload_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`, in.RootTraceID, in.TaskID, p.owner, p.session, p.request.Agent.AgentID, p.binding, p.source, p.authority, p.digest, p.price.Base.Version, in.MaxOutputTokens, p.input.DeadlineAt, out.Purpose, p.price.Kind, p.queryDigest, p.payloadDigest).Scan(&out.ID)
	if e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, p.owner, in.RootTraceID, "", "PREVIEW"); e != nil {
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

type storedLiveEgressPreview struct {
	storedEgressPreview
	kind                                modelegressbudget.LiveChargeKind
	queryDigest, payloadDigest, purpose string
	scope, sourceEvidenceDigest         string
	sourceBatch                         []byte
}

func readLiveEgressPreview(ctx context.Context, tx pgx.Tx, id string) (v storedLiveEgressPreview, e error) {
	v.storedEgressPreview, e = readEgressPreview(ctx, tx, id)
	if e != nil {
		return v, e
	}
	e = tx.QueryRow(ctx, `SELECT billing_kind,current_query_digest,egress_payload_digest,purpose,scope,source_batch,COALESCE(source_evidence_digest,'') FROM model_egress_previews WHERE id=$1 AND billing_kind IN ('TOKEN','CALL')`, id).Scan(&v.kind, &v.queryDigest, &v.payloadDigest, &v.purpose, &v.scope, &v.sourceBatch, &v.sourceEvidenceDigest)
	return v, egressError(e)
}
func matchLivePreview(v storedLiveEgressPreview, p PreparedLiveEgress) bool {
	expected := livePreview(p, "", "")
	return v.owner == p.owner && v.session == p.session && v.agent == p.request.Agent.AgentID && v.binding == p.binding && v.source == p.source && v.authority == p.authority && v.digest == p.digest && v.kind == p.price.Kind && v.queryDigest == p.queryDigest && v.payloadDigest == p.payloadDigest && v.purpose == expected.Purpose && v.scope == expected.Scope && v.sourceEvidenceDigest == p.sourceBatch.evidenceDigest
}
func (s *Store) ApproveOwnLiveEgress(ctx context.Context, a agentevent.Access, id, digest string, projector LiveWireProjector) error {
	if !egressUUID(id) || !modelconfiguration.ValidDigest(digest) {
		return modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return e
	}
	if _, e = s.approveLiveEgressTx(ctx, tx, a, id, digest, owner, session, projector); e != nil {
		return e
	}
	return egressError(tx.Commit(ctx))
}

func (s *Store) approveLiveEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, id, digest, owner, session string, projector LiveWireProjector, associations ...*nativeLiveSourceRunAssociation) (PreparedLiveEgress, error) {
	if !egressUUID(id) || !modelconfiguration.ValidDigest(digest) {
		return PreparedLiveEgress{}, modelegressbudget.ErrInvalid
	}
	association, e := oneLiveSourceRunAssociation(associations)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	v, e := readLiveEgressPreview(ctx, tx, id)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if v.owner != owner || v.session != session || v.digest != digest || v.status == "REVOKED" {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p, e := s.prepareStoredLiveEgressWithSourceRunTx(ctx, tx, a, v, projector, association)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if !matchLivePreview(v, p) {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	if v.status == "DRAFT" {
		if _, e = tx.Exec(ctx, `UPDATE model_egress_previews SET status='APPROVED',revision=revision+1,approved_at=clock_timestamp() WHERE id=$1`, id); e != nil {
			return PreparedLiveEgress{}, egressError(e)
		}
		if e = egressAudit(ctx, tx, owner, p.input.RootTraceID, "", "APPROVE"); e != nil {
			return PreparedLiveEgress{}, e
		}
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return PreparedLiveEgress{}, e
	}
	return p, nil
}

func fitsLiveBudget(r egressBudgetRow, a modelegressbudget.LiveAmount) bool {
	if r.currency != "CNY" || a.Amount.CostMicros > liveRequestCashLimit || !modelegressbudget.FitsLive(r.limit, r.used, a) {
		return false
	}
	cap := liveOwnerCashLimit
	if r.scope == "ROOT" || r.scope == "TASK" {
		cap = liveRootCashLimit
		if r.used.Requests >= 2 {
			return false
		}
	}
	return r.used.CostMicros <= cap-a.Amount.CostMicros
}
func readLiveReservation(ctx context.Context, tx pgx.Tx, id, owner string) (r modelegressbudget.Reservation, kind modelegressbudget.LiveChargeKind, err error) {
	r, err = readEgressReservation(ctx, tx, id, owner)
	if err != nil {
		return r, kind, err
	}
	err = tx.QueryRow(ctx, `SELECT billing_kind FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2 AND billing_kind IN ('TOKEN','CALL')`, id, owner).Scan(&kind)
	return r, kind, egressError(err)
}
func (s *Store) ReserveOwnLiveAttempt(ctx context.Context, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller, projector LiveWireProjector) (modelegressbudget.Reservation, error) {
	if !egressUUID(in.OperationID) || !egressUUID(in.PreviewID) || !egressUUID(in.RootTraceID) || !egressUUID(in.TaskID) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrInvalid
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = denyLinkedModelOperation(ctx, tx, in.OperationID); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	r, e := s.reserveLiveAttemptTx(ctx, tx, a, in, c, ticket, owner, session, projector)
	if e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.Reservation{}, egressError(e)
	}
	return r, nil
}
func (s *Store) reserveLiveAttemptTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.ReserveInput, c *agentfeature.Controller, ticket agentfeature.Ticket, owner, session string, projector LiveWireProjector, associations ...*nativeLiveSourceRunAssociation) (modelegressbudget.Reservation, error) {
	association, e := oneLiveSourceRunAssociation(associations)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	v, e := readLiveEgressPreview(ctx, tx, in.PreviewID)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if v.owner != owner || v.session != session || v.status != "APPROVED" || v.in.RootTraceID != in.RootTraceID || v.in.TaskID != in.TaskID {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	p, e := s.prepareStoredLiveEgressWithSourceRunTx(ctx, tx, a, v, projector, association)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if !matchLivePreview(v, p) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrDenied
	}
	old, kind, oldErr := readLiveReservation(ctx, tx, in.OperationID, owner)
	if oldErr == nil {
		if old.PreviewID != in.PreviewID || old.RootTraceID != in.RootTraceID || old.TaskID != in.TaskID || old.RequestDigest != p.digest || kind != p.price.Kind || old.Upper != p.upper.Amount {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
		}
		if e = finishLivePreparation(ctx, tx, p); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		if !c.Current(ticket) {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
		}
		return old, nil
	}
	if !errors.Is(oldErr, modelegressbudget.ErrDenied) {
		return modelegressbudget.Reservation{}, oldErr
	}
	rows, e := readEgressBudgets(ctx, tx, owner, in.RootTraceID, in.TaskID, "CNY")
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	for _, r := range rows {
		if !fitsLiveBudget(r, p.upper) {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrBudget
		}
	}
	if e = changeEgressBudgets(ctx, tx, owner, in.RootTraceID, in.TaskID, 1, p.upper.Amount); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_budget_reservations(operation_id,preview_id,root_trace_id,task_id,owner_id,price_version,request_digest,currency,upper_input,upper_output,upper_cost,billing_kind,cash_status,execution_status) VALUES($1,$2,$3,$4,$5,$6,$7,'CNY',$8,$9,$10,$11,'UNKNOWN','LIVE_RESERVED') ON CONFLICT DO NOTHING`, in.OperationID, in.PreviewID, in.RootTraceID, in.TaskID, owner, p.price.Base.Version, p.digest, p.upper.Amount.InputTokens, p.upper.Amount.OutputTokens, p.upper.Amount.CostMicros, p.price.Kind)
	if e != nil {
		return modelegressbudget.Reservation{}, egressError(e)
	}
	r, kind, e := readLiveReservation(ctx, tx, in.OperationID, owner)
	if e != nil || r.PreviewID != in.PreviewID || r.RequestDigest != p.digest || kind != p.price.Kind {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
	}
	if e = egressAudit(ctx, tx, owner, in.RootTraceID, in.OperationID, "RESERVE"); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if !c.Current(ticket) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrUnavailable
	}
	return r, nil
}
func (s *Store) BeginOwnLiveAttempt(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, projector LiveWireProjector) (PreparedLiveEgress, error) {
	if !egressUUID(operation) {
		return PreparedLiveEgress{}, modelegressbudget.ErrInvalid
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = denyLinkedModelOperation(ctx, tx, operation); e != nil {
		return PreparedLiveEgress{}, e
	}
	p, e := s.beginLiveAttemptTx(ctx, tx, a, operation, c, ticket, owner, session, projector)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	return p, nil
}

// CheckOwnLiveAttempt does not consume, dispatch or refund. The native bridge
// retains its original feature ticket as well as this current check. UNKNOWN
// is accepted only to revalidate a late response against still-current source;
// it never permits a second Begin or a new request.
func (s *Store) CheckOwnLiveAttempt(ctx context.Context, a agentevent.Access, operation string, c *agentfeature.Controller, projector LiveWireProjector) (PreparedLiveEgress, error) {
	if !egressUUID(operation) {
		return PreparedLiveEgress{}, modelegressbudget.ErrInvalid
	}
	ticket, e := egressTicket(c)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = denyLinkedModelOperation(ctx, tx, operation); e != nil {
		return PreparedLiveEgress{}, e
	}
	r, kind, e := readLiveReservation(ctx, tx, operation, owner)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if !liveCheckPhase(r) {
		return PreparedLiveEgress{}, modelegressbudget.ErrConflict
	}
	v, e := readLiveEgressPreview(ctx, tx, r.PreviewID)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if v.status != "APPROVED" || v.owner != owner || v.session != session {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p, e := s.prepareStoredLiveEgressTx(ctx, tx, a, v, projector)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if !matchLivePreview(v, p) || r.RequestDigest != p.digest || kind != p.price.Kind || r.Upper != p.upper.Amount {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	rows, e := readEgressBudgets(ctx, tx, owner, r.RootTraceID, r.TaskID, "CNY")
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	for _, row := range rows {
		if row.used.CostMicros > liveOwnerCashLimit || ((row.scope == "ROOT" || row.scope == "TASK") && (row.used.CostMicros > liveRootCashLimit || row.used.Requests > 2)) {
			return PreparedLiveEgress{}, modelegressbudget.ErrBudget
		}
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return PreparedLiveEgress{}, e
	}
	if !c.Current(ticket) {
		return PreparedLiveEgress{}, modelegressbudget.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	return p, nil
}
func liveCheckPhase(r modelegressbudget.Reservation) bool {
	return (r.State == "RESERVED" && r.ExecutionStatus == "LIVE_RESERVED") || (r.State == "IN_FLIGHT" && r.ExecutionStatus == "LIVE_IN_FLIGHT") || (r.State == "UNKNOWN" && r.ExecutionStatus == "LIVE_ATTEMPTED")
}
func (s *Store) beginLiveAttemptTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, operation string, c *agentfeature.Controller, ticket agentfeature.Ticket, owner, session string, projector LiveWireProjector, associations ...*nativeLiveSourceRunAssociation) (PreparedLiveEgress, error) {
	association, e := oneLiveSourceRunAssociation(associations)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	r, kind, e := readLiveReservation(ctx, tx, operation, owner)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if r.State != "RESERVED" || r.ExecutionStatus != "LIVE_RESERVED" {
		return PreparedLiveEgress{}, modelegressbudget.ErrConflict
	}
	v, e := readLiveEgressPreview(ctx, tx, r.PreviewID)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if v.status != "APPROVED" || v.owner != owner || v.session != session {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p, e := s.prepareStoredLiveEgressWithSourceRunTx(ctx, tx, a, v, projector, association)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if !matchLivePreview(v, p) || r.RequestDigest != p.digest || kind != p.price.Kind || r.Upper != p.upper.Amount {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	rows, e := readEgressBudgets(ctx, tx, owner, r.RootTraceID, r.TaskID, "CNY")
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	for _, row := range rows {
		if row.used.CostMicros > liveOwnerCashLimit || ((row.scope == "ROOT" || row.scope == "TASK") && (row.used.CostMicros > liveRootCashLimit || row.used.Requests > 2)) {
			return PreparedLiveEgress{}, modelegressbudget.ErrBudget
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE model_budget_reservations SET state='IN_FLIGHT',execution_status='LIVE_IN_FLIGHT' WHERE operation_id=$1`, operation); e != nil {
		return PreparedLiveEgress{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, owner, r.RootTraceID, operation, "IN_FLIGHT"); e != nil {
		return PreparedLiveEgress{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return PreparedLiveEgress{}, e
	}
	if !c.Current(ticket) {
		return PreparedLiveEgress{}, modelegressbudget.ErrUnavailable
	}
	return p, nil
}

// Accounting deliberately needs the current owner session only. A revoked or
// expired source cannot erase a possibly billed attempt. Releasing provider
// data is a separate boundary requiring fresh source checks and is not here.
func (s *Store) FinishOwnLiveAttempt(ctx context.Context, a agentevent.Access, operation string, usage modelgateway.Usage) (modelegressbudget.Reservation, error) {
	if !egressUUID(operation) {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrInvalid
	}
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	defer tx.Rollback(ctx)
	owner, session, e := s.egressOwner(ctx, tx, a)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = egressOwnerLock(ctx, tx, owner); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if e = denyLinkedModelOperation(ctx, tx, operation); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	r, e := s.finishLiveAttemptTx(ctx, tx, operation, usage, owner, session)
	if e != nil {
		return r, e
	}
	if e = tx.Commit(ctx); e != nil {
		return modelegressbudget.Reservation{}, egressError(e)
	}
	return r, nil
}
func liveUsageObservation(kind modelegressbudget.LiveChargeKind, upper modelegressbudget.Amount, usage modelgateway.Usage) (input, output *int64, err error) {
	if usage.CostStatus != "UNKNOWN" {
		return nil, nil, modelegressbudget.ErrInvalid
	}
	if usage.Status == "UNKNOWN" && usage.InputTokens == nil && usage.OutputTokens == nil {
		return nil, nil, nil
	}
	if kind != modelegressbudget.LiveToken || usage.Status != "KNOWN" || usage.InputTokens == nil || usage.OutputTokens == nil || *usage.InputTokens < 0 || *usage.InputTokens > upper.InputTokens || *usage.OutputTokens < 0 || *usage.OutputTokens > upper.OutputTokens {
		return nil, nil, modelegressbudget.ErrInvalid
	}
	i, o := *usage.InputTokens, *usage.OutputTokens
	return &i, &o, nil
}
func (s *Store) finishLiveAttemptTx(ctx context.Context, tx pgx.Tx, operation string, usage modelgateway.Usage, owner, session string) (modelegressbudget.Reservation, error) {
	r, kind, e := readLiveReservation(ctx, tx, operation, owner)
	if e != nil {
		return r, e
	}
	i, o, e := liveUsageObservation(kind, r.Upper, usage)
	if e != nil {
		return modelegressbudget.Reservation{}, e
	}
	if r.State != "IN_FLIGHT" && r.State != "UNKNOWN" {
		return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
	}
	if r.State == "UNKNOWN" && r.ReportedInput != nil {
		if i != nil && (*r.ReportedInput != *i || r.ReportedOutput == nil || *r.ReportedOutput != *o) {
			return modelegressbudget.Reservation{}, modelegressbudget.ErrConflict
		}
	} else if r.State == "IN_FLIGHT" || i != nil {
		if _, e = readEgressBudgets(ctx, tx, owner, r.RootTraceID, r.TaskID, r.Currency); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		status := "UNKNOWN"
		if i != nil {
			status = "KNOWN"
		}
		// No changeEgressBudgets call: even known token observations retain the
		// full original token/cash hold. Unknown billing never becomes zero.
		if _, e = tx.Exec(ctx, `UPDATE model_budget_reservations SET state='UNKNOWN',reported_input=$2,reported_output=$3,usage_status=$4,cash_status='UNKNOWN',settled_cost=NULL,execution_status='LIVE_ATTEMPTED' WHERE operation_id=$1`, operation, i, o, status); e != nil {
			return modelegressbudget.Reservation{}, egressError(e)
		}
		if e = egressAudit(ctx, tx, owner, r.RootTraceID, operation, "UNKNOWN"); e != nil {
			return modelegressbudget.Reservation{}, e
		}
		r, _, e = readLiveReservation(ctx, tx, operation, owner)
		if e != nil {
			return modelegressbudget.Reservation{}, e
		}
	}
	if e = egressFinish(ctx, tx, session, nil); e != nil {
		return modelegressbudget.Reservation{}, e
	}
	return r, nil
}
