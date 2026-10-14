package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

type NowLicensedPlaceAnswerOptions struct {
	OwnerID, ConfigurationVersion, ModelPriceVersion string
	RouteRevision                                    int64
}
type ownNowLicensedPlaceAnswers struct {
	store      *Store
	controller *agentfeature.Controller
	gate       modelgateway.LiveGate
	model      *modelgateway.TencentTokenHubAdapter
	options    NowLicensedPlaceAnswerOptions
}

func NewOwnNowLicensedPlaceAnswers(s *Store, c *agentfeature.Controller, gate modelgateway.LiveGate, model *modelgateway.TencentTokenHubAdapter, o NowLicensedPlaceAnswerOptions) (agentworkspace.LiveAnswers, error) {
	if s == nil || s.pool == nil || c == nil || !nativeLiveGatePresent(gate) || model == nil || !egressUUID(o.OwnerID) || o.ConfigurationVersion != modelegressbudget.LiveLicensedPlacePolicyVersion || !modelconfiguration.ValidVersion(o.ModelPriceVersion) || o.RouteRevision < 1 {
		return nil, modelegressbudget.ErrInvalid
	}
	if d := model.Descriptor(); d.ProviderID != "tencent_tokenhub" || d.ModelID != modelgateway.TencentTokenHubDeepSeekModel || d.ModelVersion != d.ModelID || d.Mode != modelgateway.Live {
		return nil, modelegressbudget.ErrInvalid
	}
	return &ownNowLicensedPlaceAnswers{s, c, gate, model, o}, nil
}
func (n *ownNowLicensedPlaceAnswers) Eligible(ctx context.Context, digest [32]byte, t agentworkspace.Task) bool {
	return n != nil && ctx != nil && ctx.Err() == nil && digest != ([32]byte{}) && t.PrincipalType == "person" && t.PrincipalID == n.options.OwnerID && t.ActingUserID == n.options.OwnerID && t.Status == agentworkspace.TaskActive && t.ContextType == "CITY" && t.CityID != "" && t.Intent == agentworkspace.FindPlace
}
func (n *ownNowLicensedPlaceAnswers) Execute(ctx context.Context, digest [32]byte, expected agentworkspace.Task) (agentworkspace.LiveReply, error) {
	if !n.Eligible(ctx, digest, expected) || !n.gate.InferenceEnabled(ctx) {
		return nil, modelegressbudget.ErrDenied
	}
	ticket, e := n.controller.Capture(agentfeature.Enrichment)
	if e != nil {
		return nil, e
	}
	deadline := time.Now().UTC().Add(28 * time.Second).Truncate(time.Microsecond)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d.UTC().Truncate(time.Microsecond)
	}
	callctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	a := agentevent.Access{SessionDigest: digest}
	tx, e := n.store.beginEgress(callctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(callctx)
	current, e := lockModelConfigurationTask(callctx, tx, a, expected.ID, n.store.devPhoneEnabled)
	if e != nil || current.status != agentworkspace.TaskActive || current.agent.Principal.ID != n.options.OwnerID {
		return nil, modelegressbudget.ErrDenied
	}
	task, e := scanAgentTask(tx.QueryRow(callctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2`, expected.ID, n.options.OwnerID))
	if e != nil {
		return nil, e
	}
	want, e := agentworkspace.SourcedAnswerTaskDigest(expected)
	if e != nil {
		return nil, e
	}
	got, e := agentworkspace.SourcedAnswerTaskDigest(task)
	if e != nil || want != got {
		return nil, modelegressbudget.ErrDenied
	}
	var binding, root, op string
	if e = tx.QueryRow(callctx, `SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid()`).Scan(&binding, &root, &op); e != nil {
		return nil, e
	}
	if e = tx.Commit(callctx); e != nil {
		return nil, egressError(e)
	}
	config, e := n.store.ReadModelConfiguration(callctx, n.options.ConfigurationVersion)
	if e != nil {
		return nil, e
	}
	query := task.Filters["currentQuery"]
	if query == "" {
		query = task.Query
	}
	request := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: binding, Agent: current.agent, ContextSnapshotRef: task.ID, DataPolicyRef: root, BudgetRef: root, Budget: modelgateway.Budget{MaxOutputTokens: 768}, Messages: []modelgateway.Message{{Role: "user", Content: query}}, DeadlineAt: deadline}
	request, _, e = modelconfiguration.PrepareRequest(config, request, time.Now())
	if e != nil {
		return nil, e
	}
	if _, e = n.store.BindModelTaskConfiguration(callctx, a, task.ID, n.options.ConfigurationVersion, n.options.RouteRevision, request); e != nil {
		return nil, e
	}
	account := modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: liveOwnerCashLimit}
	if e = n.store.ConfigureOwnModelBudget(callctx, a, task.ID, binding, "CNY", account, account); e != nil {
		return nil, e
	}
	limits := modelegressbudget.Limits{Requests: 1, InputTokens: modelgateway.TencentLiveMaxInputTokens, OutputTokens: 768, CostMicros: liveRequestCashLimit}
	if e = n.store.CreateOwnModelBudgetRoot(callctx, a, modelegressbudget.RootInput{RootTraceID: root, TaskID: task.ID, BindingID: binding, Currency: "CNY", Limits: limits, ExpiresAt: deadline}); e != nil {
		return nil, e
	}
	preview, e := n.store.PreviewOwnLicensedPlaceEgress(callctx, a, modelegressbudget.PreviewInput{RootTraceID: root, TaskID: task.ID, PriceVersion: n.options.ModelPriceVersion, MaxOutputTokens: 768, DeadlineAt: deadline}, n.model)
	if e != nil {
		return nil, e
	}
	// Same trusted owner trial consent, exact public-only payload and cash cap.
	// It neither approves private Memory nor changes any old policy/version.
	if e = n.store.ApproveOwnLiveEgress(callctx, a, preview.ID, preview.RequestDigest, n.model); e != nil {
		return nil, e
	}
	if _, e = n.store.ReserveOwnLiveAttempt(callctx, a, modelegressbudget.ReserveInput{OperationID: op, PreviewID: preview.ID, RootTraceID: root, TaskID: task.ID}, n.controller, n.model); e != nil {
		return nil, e
	}
	port, req, e := n.store.NewOwnLiveModelDispatch(callctx, a, op, n.controller, n.model, n.gate)
	if e != nil {
		return nil, e
	}
	gateway, e := modelgateway.NewNativeLiveGateway(n.gate, n.model, port)
	if e != nil {
		return nil, e
	}
	result, e := gateway.Complete(callctx, req)
	if e != nil {
		return nil, modelgateway.WithLiveFailureReason(e, result.ReasonCode)
	}
	_, modelErr := licensedPlaceResultModel(n.model.Descriptor(), result)
	if modelErr != nil || result.Mode != modelgateway.Live || result.Status != modelgateway.Completed || result.Text == "" || len(result.Text) > 8192 || result.RunID != binding || result.Agent != req.Agent || result.Candidate != nil || result.Answer != nil || len(result.ToolProposals) != 0 || result.Usage.CostStatus != "UNKNOWN" || !n.controller.Current(ticket) {
		return nil, modelegressbudget.ErrDenied
	}
	// Retain the entire original UNKNOWN cash hold; observe tokens separately.
	if _, e = n.store.FinishOwnLiveAttempt(callctx, a, op, result.Usage); e != nil {
		return nil, e
	}
	return n.store.finalizeLicensedPlaceReply(callctx, a, task, op, preview.Prepared(), result, n.controller, ticket, n.model)
}

// Result carries ModelID@ModelVersion; the native price and typed reply carry
// the bare audited model ID. Only the original trusted wire descriptor maps it.
func licensedPlaceResultModel(d modelgateway.ProviderDescriptor, r modelgateway.Result) (string, error) {
	if d.Mode != modelgateway.Live || d.ProviderID != "tencent_tokenhub" || d.ModelID != modelgateway.TencentTokenHubDeepSeekModel || d.ModelVersion != d.ModelID || r.ProviderID != d.ProviderID || r.ProviderModelVersion != d.ModelID+"@"+d.ModelVersion || r.ProviderRequestID == "" {
		return "", modelegressbudget.ErrDenied
	}
	return d.ModelID, nil
}

// An older approved dispatch may already hold the owner ledger while waiting
// on this Task. Never wait on that ledger with our Task UPDATE lock held.
// Contention withholds publication and leaves its original UNKNOWN cash hold.
func licensedReplyTryOwnerLock(ctx context.Context, tx pgx.Tx, owner string) error {
	var acquired bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('birdtie.model-budget.owner:'||$1,0))`, owner).Scan(&acquired); err != nil || !acquired {
		return arp.ErrChanged
	}
	return nil
}

// The ordinary native history reader disables JIT for these bounded, complex
// ACL projections. Keep that same transaction setting during model publication;
// compilation time must not consume the short current-source read lease.
func licensedReplyProjectionSettings(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return arp.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `SET LOCAL jit=off`); err != nil {
		return arp.ErrUnavailable
	}
	return nil
}

// Public field reuse for history deliberately ignores the consumed preview's
// original 28s dispatch expiry. No new provider call or new egress is granted.
func validateLicensedPlaceFieldsTx(ctx context.Context, tx pgx.Tx, d licensedPlaceBatchData, now time.Time) ([]cityseed.PlaceFieldEvidence, time.Time, error) {
	_, cityRaw, e := nativeResolvedPublicCityTx(ctx, tx, d.Context.City.ID, now)
	if e != nil || liveDigestBytes("birdtie.licensed-native-city.v1", cityRaw) != d.CityProof {
		return nil, time.Time{}, arp.ErrChanged
	}
	fields := []cityseed.PlaceFieldEvidence{}
	var cityLife struct {
		Expiry *time.Time `json:"expiry"`
	}
	if json.Unmarshal(cityRaw, &cityLife) != nil {
		return nil, time.Time{}, arp.ErrChanged
	}
	until := time.Time{}
	if cityLife.Expiry != nil {
		until = *cityLife.Expiry
	}
	for i, p := range d.Places {
		current, err := nativePublicFollowupPlaceTx(ctx, tx, p.ID, d.Context.City.ID, now)
		if err != nil || current.generation != p.Generation || current.name != d.Context.Places[i].Name {
			return nil, time.Time{}, arp.ErrChanged
		}
		var placeLife struct {
			Expiry *time.Time `json:"expiry"`
		}
		if json.Unmarshal([]byte(current.raw), &placeLife) != nil {
			return nil, time.Time{}, arp.ErrChanged
		}
		if placeLife.Expiry != nil {
			until = retryMinimum(until, *placeLife.Expiry)
		}
		place, err := scanPlace(tx.QueryRow(ctx, `SELECT `+placeColumns+` FROM places p JOIN cities c ON c.id=p.city_id WHERE p.id=$1 AND p.city_id=$2 AND p.publication_status='published' AND c.publication_status='published' AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` FOR SHARE OF p,c`, p.ID, d.Context.City.ID))
		if err != nil {
			return nil, time.Time{}, arp.ErrChanged
		}
		keys := []string{}
		for _, ref := range d.Facts {
			if ref.PlaceID == p.ID {
				keys = append(keys, ref.Field)
			}
		}
		// One key may have multiple agreeing independent sources; query keys once.
		unique := []string{}
		seen := map[string]bool{}
		for _, key := range keys {
			if !seen[key] {
				unique = append(unique, key)
				seen[key] = true
			}
		}
		rows, err := ReadPlaceFieldEvidenceTx(ctx, tx, p.ID, unique, now)
		if err != nil || !licensedPlaceFieldsAllowed(place, rows, now) {
			return nil, time.Time{}, arp.ErrChanged
		}
		for _, ref := range d.Facts {
			if ref.PlaceID != p.ID {
				continue
			}
			matched := false
			for _, f := range rows {
				if f.ID == ref.EvidenceID && f.Version == ref.Version && f.PermissionID == ref.PermissionID {
					matched = true
					fields = append(fields, f)
					until = retryMinimum(until, retryMinimum(f.ValidTo, retryMinimum(f.RefreshDue, f.Permission.RetentionUntil)))
					break
				}
			}
			if !matched {
				return nil, time.Time{}, arp.ErrChanged
			}
		}
	}
	if localFieldDigest(fields, nil) != d.FieldProof || !until.After(time.Now().UTC()) {
		return nil, time.Time{}, arp.ErrChanged
	}
	return fields, until, nil
}

// Called by original message-results reading under its current owner/session
// locks. The typed selectors correlate an already published native message;
// UNKNOWN billing alone never proves completion or permits redispatch.
func ValidateLicensedPlaceModelEvidenceTx(ctx context.Context, tx pgx.Tx, owner, taskID string, m agentworkspace.Message, now time.Time) ([]cityseed.PlaceFieldEvidence, time.Time, error) {
	proof := m.LicensedPlaceModelEvidence
	if tx == nil || !egressUUID(owner) || !egressUUID(taskID) || proof == nil || proof.Schema != agentworkspace.LicensedPlaceModelReplySchema || !egressUUID(proof.OperationID) || !egressUUID(proof.PreviewID) || !egressUUID(proof.BindingID) || proof.ModelCalls != 1 || proof.SearchCalls != 0 || proof.SourceEvidenceDigest != m.SourceEvidenceDigest || proof.TextDigest != liveDigestBytes("birdtie.licensed-model-text.v1", []byte(m.Text)) || proof.Provider != "tencent_tokenhub" || proof.Model != modelgateway.TencentTokenHubDeepSeekModel || proof.ProviderRequestID == "" || m.SourceRunID != "" || m.LocalReplyEvidence != nil {
		return nil, time.Time{}, arp.ErrChanged
	}
	r, kind, e := readLiveReservation(ctx, tx, proof.OperationID, owner)
	if e != nil || kind != modelegressbudget.LiveToken || r.TaskID != taskID || r.PreviewID != proof.PreviewID || r.State != "UNKNOWN" || r.ExecutionStatus != "LIVE_ATTEMPTED" || r.RequestDigest != proof.RequestDigest {
		return nil, time.Time{}, arp.ErrChanged
	}
	var cash string
	if tx.QueryRow(ctx, `SELECT cash_status FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2`, proof.OperationID, owner).Scan(&cash) != nil || cash != "UNKNOWN" {
		return nil, time.Time{}, arp.ErrChanged
	}
	v, e := readLiveEgressPreview(ctx, tx, proof.PreviewID)
	if e != nil || v.owner != owner || v.in.TaskID != taskID || v.status != "APPROVED" || v.scope != modelegressbudget.LiveLicensedPlaceScope || v.binding != proof.BindingID || v.digest != proof.RequestDigest || v.payloadDigest != proof.PayloadDigest || v.queryDigest != proof.CurrentQueryDigest || v.sourceEvidenceDigest != proof.SourceEvidenceDigest {
		return nil, time.Time{}, arp.ErrChanged
	}
	price, e := readLivePrice(ctx, tx, v.in.PriceVersion)
	if e != nil || price.Base.Destination.Provider != proof.Provider || price.Base.Destination.Model != proof.Model {
		return nil, time.Time{}, arp.ErrChanged
	}
	d, e := readLicensedPlaceBatch(v.sourceBatch)
	if e != nil || licensedPlaceBatchDigest(d) != proof.SourceEvidenceDigest || d.QueryDigest != proof.CurrentQueryDigest || !reflect.DeepEqual(d.Facts, proof.Facts) || modelegressbudget.ValidateLivePublicSourceAnswer(m.Text, d.Context.Sources) != nil {
		return nil, time.Time{}, arp.ErrChanged
	}
	expectedSources, e := liveReplyAnswerSources(modelegressbudget.LiveSourceAnswerPolicyVersion, m.Text, d.Context.Sources)
	if e != nil || !reflect.DeepEqual(expectedSources, m.Sources) {
		return nil, time.Time{}, arp.ErrChanged
	}
	return validateLicensedPlaceFieldsTx(ctx, tx, d, now)
}

func (s *Store) finalizeLicensedPlaceReply(ctx context.Context, a agentevent.Access, expected agentworkspace.Task, op string, original PreparedLiveEgress, result modelgateway.Result, c *agentfeature.Controller, ticket agentfeature.Ticket, projector LiveWireProjector) (agentworkspace.LiveReply, error) {
	if !c.Current(ticket) || !original.licensed.valid {
		return nil, modelegressbudget.ErrDenied
	}
	model, modelErr := licensedPlaceResultModel(original.modelWire.Descriptor(), result)
	if modelErr != nil {
		return nil, modelErr
	}
	actor := identity.Actor{ID: original.owner, AccountType: "person"}
	tx, b, policy, e := s.beginCurrentToolRead(ctx, actor, a.SessionDigest, resultProjectionRelations+`;LOCK TABLE place_source_permissions,place_field_evidence IN ACCESS SHARE MODE`)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = licensedReplyProjectionSettings(ctx, tx); e != nil {
		return nil, e
	}
	if b.accountID != original.owner || b.sessionID != original.session || b.agentID != original.request.Agent.AgentID {
		return nil, modelegressbudget.ErrDenied
	}
	raw, e := json.Marshal(agentworkspace.SanitizeTaskForResponse(expected))
	if e != nil {
		return nil, e
	}
	access := arp.Access{Actor: actor, SessionDigest: a.SessionDigest, TaskID: expected.ID, ExpectedTask: raw}
	// Take the write lock before replay, avoiding a SHARE -> UPDATE upgrade.
	// The ledger try-lock below avoids waiting against older owner-first paths.
	t, e := ownReplyTaskTx(ctx, tx, access, true)
	if e != nil {
		return nil, e
	}
	if e = licensedReplyTryOwnerLock(ctx, tx, b.accountID); e != nil {
		return nil, e
	}
	reservation, kind, e := readLiveReservation(ctx, tx, op, b.accountID)
	if e != nil || kind != modelegressbudget.LiveToken || reservation.State != "UNKNOWN" || reservation.ExecutionStatus != "LIVE_ATTEMPTED" {
		return nil, modelegressbudget.ErrDenied
	}
	v, e := readLiveEgressPreview(ctx, tx, reservation.PreviewID)
	if e != nil {
		return nil, e
	}
	current, e := s.replayLicensedPlaceEgressTx(ctx, tx, a, v, projector)
	if e != nil || !matchLivePreview(v, current) || !nativeLiveRequestEqual(original.Request(), current.Request()) {
		return nil, modelegressbudget.ErrDenied
	}
	sources, e := liveReplyAnswerSources(modelegressbudget.LiveSourceAnswerPolicyVersion, result.Text, current.licensed.data.Context.Sources)
	if e != nil {
		return nil, e
	}
	q, source, e := s.captureReplyCurrentSourceTx(ctx, tx, access, t, policy)
	if e != nil || q.Kind != "place" {
		return nil, fmt.Errorf("licensed place publication current_sources: %w", arp.ErrChanged)
	}
	wanted := map[string]bool{}
	for _, p := range current.licensed.data.Places {
		wanted[p.ID] = true
	}
	items := []arp.Item{}
	places := []foundation.Place{}
	found := map[string]bool{}
	for _, p := range source.Places {
		if wanted[p.ID] {
			places = append(places, p)
			found[p.ID] = true
		}
	}
	for _, item := range source.Items {
		if item.Entity.Type == "place" && wanted[item.Entity.ID] {
			items = append(items, item)
		}
	}
	if len(found) != len(wanted) || len(items) != len(wanted) {
		return nil, fmt.Errorf("licensed place publication selected_membership: %w", arp.ErrChanged)
	}
	source.Places, source.Items = places, items
	proof := &agentworkspace.LicensedPlaceModelEvidence{Schema: agentworkspace.LicensedPlaceModelReplySchema, OperationID: op, PreviewID: reservation.PreviewID, BindingID: current.binding, ProviderRequestID: result.ProviderRequestID, Provider: result.ProviderID, Model: model, RequestDigest: current.digest, PayloadDigest: current.payloadDigest, CurrentQueryDigest: current.queryDigest, TextDigest: liveDigestBytes("birdtie.licensed-model-text.v1", []byte(result.Text)), SourceEvidenceDigest: current.licensed.digest, Facts: append([]agentworkspace.LocalPlaceFactRef(nil), current.licensed.data.Facts...), ValidUntil: retryMinimum(source.ValidUntil, current.input.DeadlineAt), SearchCalls: 0, ModelCalls: 1}
	message := agentworkspace.Message{Role: "assistant", Text: result.Text, Sources: sources, SourceEvidenceDigest: proof.SourceEvidenceDigest, LicensedPlaceModelEvidence: proof}
	_, fieldUntil, e := ValidateLicensedPlaceModelEvidenceTx(ctx, tx, b.accountID, t.ID, message, time.Now().UTC())
	if e != nil {
		return nil, fmt.Errorf("licensed place publication validate_fact_refs: %w", e)
	}
	proof.ValidUntil = retryMinimum(proof.ValidUntil, fieldUntil)
	t, history, e := s.persistReplyMembershipTx(ctx, tx, access, t, policy, "place", source, message)
	if e != nil {
		return nil, fmt.Errorf("licensed place publication persist_membership: %w", e)
	}
	access.ExpectedTask, e = json.Marshal(agentworkspace.SanitizeTaskForResponse(t))
	if e != nil {
		return nil, e
	}
	if e = s.finalReplySourceProofsTx(ctx, tx, access, t, policy, history.state()); e != nil {
		return nil, fmt.Errorf("licensed place publication final_source_fence: %w", e)
	}
	if _, _, e = ValidateLicensedPlaceModelEvidenceTx(ctx, tx, b.accountID, t.ID, t.Conversation[len(t.Conversation)-1], time.Now().UTC()); e != nil {
		return nil, e
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil || !c.Current(ticket) || !proof.ValidUntil.After(time.Now().UTC()) {
		return nil, arp.ErrChanged
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, egressError(e)
	}
	reply := &nativeLicensedPlaceReply{store: s, access: access, task: t, history: history, until: proof.ValidUntil, controller: c, ticket: ticket}
	if e = reply.Revalidate(ctx); e != nil {
		return nil, fmt.Errorf("licensed place publication post_commit_revalidate: %w", e)
	}
	return reply, nil
}

type nativeLicensedPlaceReply struct {
	store      *Store
	access     arp.Access
	task       agentworkspace.Task
	history    *nativeMessageResultsRead
	until      time.Time
	controller *agentfeature.Controller
	ticket     agentfeature.Ticket
}

func (h *nativeLicensedPlaceReply) Task() agentworkspace.Task {
	raw, _ := json.Marshal(h.task)
	var t agentworkspace.Task
	_ = json.Unmarshal(raw, &t)
	return t
}
func (h *nativeLicensedPlaceReply) Answer(request string) (*agentworkspace.SourcedAnswer, error) {
	t := agentworkspace.SanitizeTaskForResponse(h.task)
	m := t.Conversation[len(t.Conversation)-1]
	q, e := agentworkspace.SourcedAnswerQueryDigest(t)
	if e != nil {
		return nil, e
	}
	d, e := agentworkspace.SourcedAnswerTaskDigest(t)
	if e != nil {
		return nil, e
	}
	return agentworkspace.NewSourcedAnswer(agentworkspace.SourcedAnswerBinding{TaskID: t.ID, RequestID: request, CurrentQueryDigest: q, TaskSnapshotDigest: d, SourceEvidenceDigest: m.SourceEvidenceDigest, RunID: m.LicensedPlaceModelEvidence.BindingID, GeneratedAt: t.UpdatedAt, ValidUntil: h.until}, m.Text, m.Sources)
}
func (h *nativeLicensedPlaceReply) Revalidate(ctx context.Context) error {
	if h == nil || h.controller == nil || !h.controller.Current(h.ticket) || !h.until.After(time.Now().UTC()) {
		return arp.ErrChanged
	}
	if e := h.history.Revalidate(ctx); e != nil {
		return e
	}
	tx, b, _, e := h.store.beginCurrentToolRead(ctx, h.access.Actor, h.access.SessionDigest, resultProjectionRelations+`;LOCK TABLE place_source_permissions,place_field_evidence IN ACCESS SHARE MODE`)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	t, e := ownReplyTaskTx(ctx, tx, h.access, false)
	if e != nil {
		return e
	}
	if _, _, e = ValidateLicensedPlaceModelEvidenceTx(ctx, tx, b.accountID, t.ID, t.Conversation[len(t.Conversation)-1], time.Now().UTC()); e != nil {
		return e
	}
	if e = recheckAgentPrivateSession(ctx, tx, b); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	if !h.controller.Current(h.ticket) || !h.until.After(time.Now().UTC()) {
		return arp.ErrChanged
	}
	return nil
}
