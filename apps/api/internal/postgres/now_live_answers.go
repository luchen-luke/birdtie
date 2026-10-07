package postgres

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// Startup-only allowlist and immutable configuration/price references. This
// local trial does not activate inference for every account or Organization.
type NowLiveAnswerOptions struct {
	OwnerID                               string
	ConfigurationVersion                  string
	RouteRevision                         int64
	SearchPriceVersion, ModelPriceVersion string
}

type ownNowLiveAnswers struct {
	store      *Store
	controller *agentfeature.Controller
	gate       modelgateway.LiveGate
	model      *modelgateway.TencentTokenHubAdapter
	search     *agenttool.TencentWSAAdapter
	options    NowLiveAnswerOptions
}

var _ agentworkspace.LiveAnswers = (*ownNowLiveAnswers)(nil)

func (*ownNowLiveAnswers) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "ownNowLiveAnswers{redacted}")
}
func (*ownNowLiveAnswers) MarshalJSON() ([]byte, error) { return nil, modelegressbudget.ErrServerOnly }

func NewOwnNowLiveAnswers(store *Store, controller *agentfeature.Controller, gate modelgateway.LiveGate, model *modelgateway.TencentTokenHubAdapter, search *agenttool.TencentWSAAdapter, options NowLiveAnswerOptions) (agentworkspace.LiveAnswers, error) {
	if store == nil || store.pool == nil || controller == nil || !nativeLiveGatePresent(gate) || model == nil || search == nil || !egressUUID(options.OwnerID) || !modelconfiguration.ValidVersion(options.ConfigurationVersion) || options.RouteRevision < 1 || !modelconfiguration.ValidVersion(options.SearchPriceVersion) || !modelconfiguration.ValidVersion(options.ModelPriceVersion) {
		return nil, modelegressbudget.ErrInvalid
	}
	return &ownNowLiveAnswers{store: store, controller: controller, gate: gate, model: model, search: search, options: options}, nil
}

func (n *ownNowLiveAnswers) Eligible(ctx context.Context, digest [32]byte, task agentworkspace.Task) bool {
	if n == nil || ctx == nil || ctx.Err() != nil || digest == ([32]byte{}) || task.ID == "" || task.PrincipalType != "person" || task.PrincipalID != n.options.OwnerID || task.ActingUserID != n.options.OwnerID || task.ContextType == "ONLINE" || task.Status != agentworkspace.TaskActive {
		return false
	}
	switch task.Intent {
	case agentworkspace.FindActivity, agentworkspace.FindPlace, agentworkspace.FindOrganization, agentworkspace.AreaDiscovery, agentworkspace.RefineResults, agentworkspace.CompareResults:
		return true
	}
	return false
}

func (n *ownNowLiveAnswers) Execute(ctx context.Context, digest [32]byte, expected agentworkspace.Task) (agentworkspace.LiveReply, error) {
	if !n.Eligible(ctx, digest, expected) || !n.gate.InferenceEnabled(ctx) {
		return nil, modelegressbudget.ErrDenied
	}
	ticket, e := n.controller.Capture(agentfeature.Enrichment)
	if e != nil {
		return nil, e
	}
	// One native receipt window; neither provider nor a retry extends it.
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
	var binding, root, run, searchOp, modelOp string
	if e = tx.QueryRow(callctx, `SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),gen_random_uuid()`).Scan(&binding, &root, &run, &searchOp, &modelOp); e != nil {
		return nil, egressError(e)
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
	// The original durable owner accounts are insert-only here: existing limits,
	// cash holds and UNKNOWN attempts are never reset by a new Now question.
	account := modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: 10_000_000}
	if e = n.store.ConfigureOwnModelBudget(callctx, a, task.ID, binding, "CNY", account, account); e != nil {
		return nil, e
	}
	limits := modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680}
	if e = n.store.CreateOwnModelBudgetRoot(callctx, a, modelegressbudget.RootInput{RootTraceID: root, TaskID: task.ID, BindingID: binding, Currency: "CNY", Limits: limits, ExpiresAt: deadline}); e != nil {
		return nil, e
	}
	preview, e := n.store.PreviewOwnResolvedLiveEgress(callctx, a, modelegressbudget.PreviewInput{RootTraceID: root, TaskID: task.ID, PriceVersion: n.options.SearchPriceVersion, DeadlineAt: deadline}, nil)
	if e != nil {
		return nil, e
	}
	// These exact approvals implement the account's trusted local trial consent
	// for its own current question plus public web sources, within its cash cap.
	if e = n.store.ApproveOwnLiveEgress(callctx, a, preview.ID, preview.RequestDigest, nil); e != nil {
		return nil, e
	}
	h, _, e := n.store.CreateOwnLiveSourceAnswerRun(callctx, a, LiveSourceAnswerRunInput{RunID: run, Search: modelegressbudget.ReserveInput{OperationID: searchOp, PreviewID: preview.ID, RootTraceID: root, TaskID: task.ID}, ModelOperationID: modelOp, ModelPriceVersion: n.options.ModelPriceVersion, MaxOutputTokens: 768}, n.controller, ticket, n.model, n.gate)
	if e != nil {
		return nil, e
	}
	completed := false
	defer func() {
		if !completed {
			stopctx, stopcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stopcancel()
			_ = h.Stop(stopctx)
		}
	}()
	batch, e := h.ExecuteSourceSearch(callctx, n.search)
	if e != nil {
		return nil, e
	}
	modelPreview, e := h.PreviewSourceModel(callctx, batch)
	if e != nil {
		return nil, e
	}
	if e = h.ApproveSourceModel(callctx, modelPreview.ID, modelPreview.RequestDigest); e != nil {
		return nil, e
	}
	control, e := h.Read(callctx)
	if e != nil {
		return nil, e
	}
	if _, e = h.BindSourceModel(callctx, modelPreview.ID, modelPreview.RequestDigest, control.Revision); e != nil {
		return nil, e
	}
	if _, e = h.CompleteSourceModel(callctx, n.model); e != nil {
		return nil, e
	}
	reply, e := h.FinalizeSourceReply(callctx)
	if e != nil {
		return nil, e
	}
	completed = true
	return reply, nil
}
