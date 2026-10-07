package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

// These tests use the original native SQL fixtures, not a fake accounting
// service. HTTP responses, tariff artifacts and credentials remain synthetic.
// They cannot prove supplier operation, a primary-source price artifact, paid
// billing, production authentication, or API05/APK05/device acceptance.
type liveNativeFixture struct {
	configuration *configurationFixture
	root, binding string
	controller    *agentfeature.Controller
	token, call   modelegressbudget.LivePrice
	projector     *modelgateway.TencentTokenHubAdapter
}

type liveNativeGate struct{}

func (liveNativeGate) InferenceEnabled(context.Context) bool { return true }

type liveNativeTransport func(*http.Request) (*http.Response, error)

func (f liveNativeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func liveNativeID(t *testing.T, f *liveNativeFixture) string {
	t.Helper()
	var id string
	b := f.configuration.native.private.base
	if err := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func newLiveNativeFixture(t *testing.T) *liveNativeFixture {
	return newLiveNativeFixtureWithAccount(t, "CNY", modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: 10_000_000}, true)
}

// Conflicting-account tests create their original immutable limits correctly;
// they must never rewrite an already-created account even when it is unused.
func newLiveNativeFixtureWithAccount(t *testing.T, currency string, account modelegressbudget.Limits, createRoot bool) *liveNativeFixture {
	t.Helper()
	f := &liveNativeFixture{configuration: configurationNativeFixture(t), controller: egressGate(t, true)}
	c := f.configuration
	b := c.native.private.base
	var installed bool
	if err := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='model_budget_reservations' AND column_name='billing_kind')`).Scan(&installed); err != nil || !installed {
		t.Fatal("native LIVE integration requires migration 107", err)
	}
	// Register a new TEXT artifact; the inherited immutable STRUCTURED versions
	// remain intact. Preserve an earlier active TEXT version via its ordinary
	// activation CAS; do not reset its monotonic revision.
	var previous ModelConfigurationRoute
	err := b.pool.QueryRow(b.ctx, `SELECT task_kind,output_mode,version,revision FROM model_configuration_routes WHERE task_kind=$1 AND output_mode=$2`, modelgateway.ActivityQuery, modelgateway.Text).Scan(&previous.TaskKind, &previous.OutputMode, &previous.Version, &previous.Revision)
	hadPrevious := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	version := "unit_live_" + strings.ReplaceAll(b.person.ID, "-", "") + "_text"
	c.versions = append(c.versions, version)
	definition := modelconfiguration.Configuration{SchemaVersion: modelconfiguration.SchemaVersion, Version: version, TaskKind: modelgateway.ActivityQuery, PromptVersion: version, InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", OutputMode: modelgateway.Text, ToolAllowlist: []string{}, PolicyVersion: version, CapabilitiesRequired: []string{"text"}}
	registered, err := b.store.RegisterModelConfiguration(b.ctx, definition, modelconfiguration.PromptDefinition{Version: version, Text: "合成 native LIVE 测试提示：仅解释获准的当前问题。"}, modelconfiguration.PolicyVersionReference{Version: version, ArtifactSHA256: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal("register immutable TEXT fixture", err)
	}
	c.configs = append(c.configs, registered)
	route := activateConfigurationFixture(t, c, len(c.configs)-1, previous.Revision)
	t.Cleanup(func() {
		if hadPrevious {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := b.store.ActivateModelConfiguration(ctx, previous.Version, route.Revision); err != nil {
				t.Errorf("restore prior TEXT route via native CAS: %v", err)
			}
		}
	})
	// Cleanup only this fixture's native account rows, before inherited Task,
	// binding, configuration and session cleanup. Never reset shared tables.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{`DELETE FROM model_budget_audit WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_egress_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_tasks WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_roots WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_accounts WHERE owner_id=ANY($1::uuid[])`} {
			if _, err := b.pool.Exec(ctx, sql, b.accounts); err != nil {
				t.Errorf("owned LIVE fixture cleanup: %v", err)
			}
		}
		for _, price := range []modelegressbudget.LivePrice{f.token, f.call} {
			if price.Base.Version != "" {
				if _, err := b.pool.Exec(ctx, `DELETE FROM model_local_price_versions WHERE version=$1`, price.Base.Version); err != nil {
					t.Errorf("owned LIVE price cleanup: %v", err)
				}
			}
		}
	})
	f.binding = liveNativeID(t, f)
	r := c.request(t, len(c.configs)-1, f.binding)
	if _, err := b.store.BindModelTaskConfiguration(b.ctx, c.native.access, c.native.task.ID, route.Version, route.Revision, r); err != nil {
		t.Fatal("bind own actual Task to TEXT configuration", err)
	}
	f.projector = liveLedgerAdapter(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	// liveLedgerPrice's fixed 'a' hash is deliberately UNIT evidence. Merely
	// naming the tariff URL does not authenticate a downloaded primary artifact.
	f.token = liveLedgerPrice(now, modelegressbudget.LiveToken)
	f.token.Base.Version = "unit_live_" + strings.ReplaceAll(b.person.ID, "-", "") + "_token"
	f.call = liveLedgerPrice(now, modelegressbudget.LiveCall)
	f.call.Base.Version = "unit_live_" + strings.ReplaceAll(b.person.ID, "-", "") + "_call"
	for _, p := range []modelegressbudget.LivePrice{f.token, f.call} {
		if err := b.store.RegisterLivePrice(b.ctx, p); err != nil {
			t.Fatal("register UNIT price snapshot", err)
		}
	}
	if err := b.store.ConfigureOwnModelBudget(b.ctx, c.native.access, c.native.task.ID, f.binding, currency, account, account); err != nil {
		t.Fatal("configure original CNY owner budgets", err)
	}
	if !createRoot {
		return f
	}
	f.root = liveNativeID(t, f)
	limits := modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680}
	if err := b.store.CreateOwnModelBudgetRoot(b.ctx, c.native.access, modelegressbudget.RootInput{RootTraceID: f.root, TaskID: c.native.task.ID, BindingID: f.binding, Currency: "CNY", Limits: limits, ExpiresAt: now.Add(10 * time.Minute)}); err != nil {
		t.Fatal("create original same-root budgets", err)
	}
	return f
}

func (f *liveNativeFixture) preview(t *testing.T, kind modelegressbudget.LiveChargeKind, approve bool) LiveEgressPreview {
	t.Helper()
	b := f.configuration.native.private.base
	price, maxOut, projector := f.call, 0, LiveWireProjector(nil)
	if kind == modelegressbudget.LiveToken {
		price, maxOut, projector = f.token, 768, f.projector
	}
	p, err := b.store.PreviewOwnLiveEgress(b.ctx, f.configuration.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: f.configuration.native.task.ID, PriceVersion: price.Base.Version, MaxOutputTokens: maxOut, DeadlineAt: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}, projector)
	if err != nil {
		t.Fatal("native LIVE source preview", err)
	}
	if approve {
		if err := b.store.ApproveOwnLiveEgress(b.ctx, f.configuration.native.access, p.ID, p.RequestDigest, projector); err != nil {
			t.Fatal("approve exact native LIVE preview", err)
		}
	}
	return p
}

func (f *liveNativeFixture) input(t *testing.T, p LiveEgressPreview) modelegressbudget.ReserveInput {
	t.Helper()
	return modelegressbudget.ReserveInput{OperationID: liveNativeID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}
}

func (f *liveNativeFixture) reserve(t *testing.T, p LiveEgressPreview) modelegressbudget.Reservation {
	t.Helper()
	projector := LiveWireProjector(nil)
	if p.Kind == modelegressbudget.LiveToken {
		projector = f.projector
	}
	b := f.configuration.native.private.base
	r, err := b.store.ReserveOwnLiveAttempt(b.ctx, f.configuration.native.access, f.input(t, p), f.controller, projector)
	if err != nil {
		t.Fatal("reserve original native LIVE attempt", err)
	}
	return r
}

func assertLiveNativeViews(t *testing.T, f *liveNativeFixture, expected modelegressbudget.Limits) {
	t.Helper()
	b := f.configuration.native.private.base
	views, err := b.store.ReadOwnModelBudget(b.ctx, f.configuration.native.access, f.root, f.configuration.native.task.ID)
	if err != nil || len(views) != 4 {
		t.Fatal("read original four budget scopes", err)
	}
	seen := map[string]bool{}
	for _, v := range views {
		if seen[v.Scope] || v.Currency != "CNY" || v.Allocated != expected {
			t.Fatal("four-scope hold differs or duplicate request charged twice", v.Scope)
		}
		seen[v.Scope] = true
		if v.Scope == "ROOT" || v.Scope == "TASK" {
			if v.Limits.Requests != 2 || v.Limits.CostMicros != 279680 {
				t.Fatal("same-root two API cap changed")
			}
		} else if v.Limits.CostMicros != 10_000_000 {
			t.Fatal("owner cumulative cash cap changed")
		}
	}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK"} {
		if !seen[scope] {
			t.Fatal("missing original scope", scope)
		}
	}
}

func assertLiveNativePhase(t *testing.T, f *liveNativeFixture, id, state, execution string) {
	t.Helper()
	b := f.configuration.native.private.base
	var gotState, gotExecution, cash string
	var settled *int64
	if err := b.pool.QueryRow(b.ctx, `SELECT state,execution_status,cash_status,settled_cost FROM model_budget_reservations WHERE operation_id=$1 AND owner_id=$2`, id, b.person.ID).Scan(&gotState, &gotExecution, &cash, &settled); err != nil {
		t.Fatal(err)
	}
	if gotState != state || gotExecution != execution || cash != "UNKNOWN" || settled != nil {
		t.Fatal("native durable phase/cash hold incorrect", gotState, gotExecution, cash)
	}
}

func liveNativeResponse(raw string, status int) *http.Response {
	return &http.Response{StatusCode: status, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}
}

func liveNativeModelAdapter(t *testing.T, transport liveNativeTransport) *modelgateway.TencentTokenHubAdapter {
	t.Helper()
	config, err := modelgateway.ParseTencentTokenHubConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy3","maxOutputTokens":768,"apiKey":"unit-test-placeholder"}`))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := modelgateway.NewTencentTokenHubAdapter(config, transport)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestModelLiveNativeSameRootRealSQLQueryAndGatewayHoldIntegration(t *testing.T) {
	f := newLiveNativeFixture(t)
	b := f.configuration.native.private.base
	a := f.configuration.native.access
	search := f.preview(t, modelegressbudget.LiveCall, true)
	if search.Upper.Amount != (modelegressbudget.Amount{CostMicros: 80000}) || search.Purpose != modelegressbudget.LiveSearchPurpose {
		t.Fatal("CALL invented tokens or changed fixed one-call cost")
	}
	sr := f.reserve(t, search)
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
	if _, err := b.store.BeginOwnLocalModelAttempt(b.ctx, a, sr.OperationID, f.controller); err == nil {
		t.Fatal("original LOCAL execution accepted LIVE call")
	}
	if _, err := b.store.CancelOwnReservedModelAttempt(b.ctx, a, sr.OperationID); err == nil {
		t.Fatal("original LOCAL cancellation accepted LIVE call hold")
	}
	assertLiveNativePhase(t, f, sr.OperationID, "RESERVED", "LIVE_RESERVED")
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
	searchCalls := 0
	config, err := agenttool.ParseTencentWSAConfig([]byte(`{"apiKey":"unit-test-placeholder","keyName":"unit-native-wsa"}`))
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := agenttool.NewTencentWSAAdapter(config, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		searchCalls++
		assertLiveNativePhase(t, f, sr.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
		raw, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(raw, search.Prepared().SearchWire()) || req.URL.String() != agenttool.TencentWSAEndpoint || req.GetBody != nil || !req.Close {
			t.Fatal("WSA sent a different or replayable query")
		}
		page, _ := json.Marshal(map[string]string{"title": "UNIT 未核验来源", "url": "https://example.com/unit-source", "passage": "UNIT source text, not model export authority"})
		response, _ := json.Marshal(map[string]any{"Response": map[string]any{"RequestId": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Query": search.Prepared().SearchQuery(), "Version": "standard", "Pages": []string{string(page)}}})
		return liveNativeResponse(string(response), 200), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.store.ExecuteOwnLiveSearch(b.ctx, a, sr.OperationID, f.controller, liveNativeGate{}, adapter)
	if err != nil || len(out.Result().Sources) != 1 || out.Result().CashStatus != "UNKNOWN" || searchCalls != 1 {
		t.Fatal("native SQL -> fake WSA envelope failed", err)
	}
	assertLiveNativePhase(t, f, sr.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	if _, err := New(b.pool, false).ExecuteOwnLiveSearch(b.ctx, a, sr.OperationID, f.controller, liveNativeGate{}, adapter); err == nil || searchCalls != 1 {
		t.Fatal("restarted native WSA bridge sent the same operation again")
	}
	model := f.preview(t, modelegressbudget.LiveToken, true)
	if model.Upper.Amount != (modelegressbudget.Amount{InputTokens: 196608, OutputTokens: 768, CostMicros: 199680}) || model.Upper.Amount.CostMicros > 200000 {
		t.Fatal("TOKEN lost its fixed provider universal per-API bound")
	}
	mr := f.reserve(t, model)
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
	duplicate, err := New(b.pool, false).ReserveOwnLiveAttempt(b.ctx, a, modelegressbudget.ReserveInput{OperationID: mr.OperationID, PreviewID: model.ID, RootTraceID: f.root, TaskID: model.TaskID}, f.controller, f.projector)
	if err != nil || !duplicate.CreatedAt.Equal(mr.CreatedAt) {
		t.Fatal("durable duplicate Reserve changed native record", err)
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
	if _, err := b.store.PreviewOwnModelEgress(b.ctx, a, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: model.TaskID, PriceVersion: f.token.Base.Version, MaxOutputTokens: 768, DeadlineAt: model.ExpiresAt}); err == nil {
		t.Fatal("original LOCAL price path accepted LIVE snapshot")
	}
	if _, err := b.store.ReserveOwnModelAttempt(b.ctx, a, f.input(t, model), f.controller); err == nil {
		t.Fatal("original LOCAL Reserve accepted LIVE preview")
	}
	if _, err := b.store.BeginOwnLocalModelAttempt(b.ctx, a, mr.OperationID, f.controller); err == nil {
		t.Fatal("original LOCAL Begin accepted LIVE token hold")
	}
	if _, err := b.store.CancelOwnReservedModelAttempt(b.ctx, a, mr.OperationID); err == nil {
		t.Fatal("original LOCAL cancellation accepted LIVE token hold")
	}
	assertLiveNativePhase(t, f, mr.OperationID, "RESERVED", "LIVE_RESERVED")
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
	port, request, err := b.store.NewOwnLiveModelDispatch(b.ctx, a, mr.OperationID, f.controller, f.projector, liveNativeGate{})
	if err != nil {
		t.Fatal(err)
	}
	modelCalls := 0
	modelAdapter := liveNativeModelAdapter(t, func(req *http.Request) (*http.Response, error) {
		modelCalls++
		assertLiveNativePhase(t, f, mr.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
		raw, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(raw, model.Prepared().ModelWire().ExactWire()) || req.GetBody != nil || !req.Close {
			t.Fatal("model transport changed approved exact wire")
		}
		for _, forbidden := range []string{f.configuration.native.moment.Body, f.configuration.native.task.Conversation[0].Text, "UNIT source text"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatal("query-only grant exported unapproved history/source")
			}
		}
		return liveNativeResponse(`{"id":"unit-hy3-1","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"合成当前问题解释，尚未核验真实来源。"},"finish_reason":"stop"}],"usage":{"prompt_tokens":31,"completion_tokens":12,"total_tokens":43}}`, 200), nil
	})
	gateway, err := modelgateway.NewNativeLiveGateway(liveNativeGate{}, modelAdapter, port)
	if err != nil {
		t.Fatal(err)
	}
	result, err := gateway.Complete(b.ctx, request)
	if err != nil || result.Mode != modelgateway.Live || result.Status != modelgateway.Completed || result.Text == "" || result.Answer != nil || result.Usage.Status != "KNOWN" || result.Usage.CostStatus != "UNKNOWN" || modelCalls != 1 {
		t.Fatal("actual native dispatch -> controlled Gateway failed", err)
	}
	assertLiveNativePhase(t, f, mr.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	if _, err := b.store.SettleOwnLocalModelAttempt(b.ctx, a, mr.OperationID, modelegressbudget.LocalUsage{}); err == nil {
		t.Fatal("original LOCAL UNKNOWN settlement accepted LIVE operation")
	}
	assertLiveNativePhase(t, f, mr.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
	finished, err := b.store.FinishOwnLiveAttempt(b.ctx, a, mr.OperationID, result.Usage)
	if err != nil || finished.ReportedInput == nil || *finished.ReportedInput != 31 || finished.ReportedOutput == nil || *finished.ReportedOutput != 12 {
		t.Fatal("known token observation failed", err)
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
	if _, err := b.store.SettleOwnLocalModelAttempt(b.ctx, a, mr.OperationID, egressUsage(t, 31, 12)); err == nil {
		t.Fatal("original LOCAL settlement refunded LIVE UNKNOWN cash")
	}
	if _, err := b.store.BeginOwnLiveAttempt(b.ctx, a, mr.OperationID, f.controller, f.projector); err == nil {
		t.Fatal("durable Begin allowed second model attempt")
	}
	restartedPort, restartedRequest, err := New(b.pool, false).NewOwnLiveModelDispatch(b.ctx, a, mr.OperationID, f.controller, f.projector, liveNativeGate{})
	if err != nil {
		t.Fatal("current UNKNOWN read unavailable", err)
	}
	restartedGateway, err := modelgateway.NewNativeLiveGateway(liveNativeGate{}, modelAdapter, restartedPort)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := restartedGateway.Complete(b.ctx, restartedRequest); err == nil || result.Text != "" || modelCalls != 1 {
		t.Fatal("new native port replayed consumed operation")
	}
	assertLiveNativePhase(t, f, mr.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
}

func TestModelLiveNativeApprovalIdentityAndCurrentTaskIntegration(t *testing.T) {
	for _, name := range []string{"unapproved", "anonymous", "other_person", "organization", "changed_query", "failed_task", "revoked_session", "revoked_preview", "disabled_ticket", "changed_request_root"} {
		t.Run(name, func(t *testing.T) {
			f := newLiveNativeFixture(t)
			b := f.configuration.native.private.base
			a := f.configuration.native.access
			p := f.preview(t, modelegressbudget.LiveToken, name != "unapproved")
			in := f.input(t, p)
			if name == "unapproved" {
				if _, err := b.store.ReserveOwnLiveAttempt(b.ctx, a, in, f.controller, f.projector); err == nil {
					t.Fatal("unapproved preview reserved funds")
				}
				assertLiveNativeViews(t, f, modelegressbudget.Limits{})
				return
			}
			r, err := b.store.ReserveOwnLiveAttempt(b.ctx, a, in, f.controller, f.projector)
			if err != nil {
				t.Fatal(err)
			}
			port, request, err := b.store.NewOwnLiveModelDispatch(b.ctx, a, r.OperationID, f.controller, f.projector, liveNativeGate{})
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "anonymous":
				a = agentevent.Access{}
			case "other_person":
				a = agentevent.Access{SessionDigest: f.configuration.native.private.peer.SessionDigest}
			case "organization":
				a = agentevent.Access{SessionDigest: f.configuration.native.private.org.SessionDigest}
			case "changed_query":
				b.exec(`UPDATE agent_tasks SET query=query||' changed-current-question',updated_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2`, p.TaskID, b.person.ID)
			case "failed_task":
				b.exec(`UPDATE agent_tasks SET status='FAILED',updated_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2`, p.TaskID, b.person.ID)
			case "revoked_session":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1 AND account_id=$2`, f.configuration.native.private.ownerSession, b.person.ID)
			case "revoked_preview":
				if err := b.store.RevokeOwnModelEgress(b.ctx, a, p.ID); err != nil {
					t.Fatal("revoke exact own native preview", err)
				}
			case "disabled_ticket":
				if err := f.controller.Disable(agentfeature.Enrichment); err != nil {
					t.Fatal(err)
				}
			case "changed_request_root":
				request.BudgetRef = liveNativeID(t, f)
			}
			calls := 0
			adapter := liveNativeModelAdapter(t, func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("unexpected fixture dispatch")
			})
			if name == "anonymous" || name == "other_person" || name == "organization" {
				if _, _, err := b.store.NewOwnLiveModelDispatch(b.ctx, a, r.OperationID, f.controller, f.projector, liveNativeGate{}); err == nil {
					t.Fatal("non-own native identity constructed dispatch")
				}
				if _, err := b.store.BeginOwnLiveAttempt(b.ctx, a, r.OperationID, f.controller, f.projector); err == nil {
					t.Fatal("non-own identity consumed reservation")
				}
			} else {
				gateway, err := modelgateway.NewNativeLiveGateway(liveNativeGate{}, adapter, port)
				if err != nil {
					t.Fatal(err)
				}
				result, err := gateway.Complete(b.ctx, request)
				if err == nil || result.Text != "" {
					t.Fatal("stale/revoked selector released live text")
				}
			}
			if calls != 0 {
				t.Fatal("identity/current Task failure sent a provider request")
			}
			assertLiveNativePhase(t, f, r.OperationID, "RESERVED", "LIVE_RESERVED")
		})
	}
}

func TestModelLiveNativeBudgetTwoRequestAndOwnerCashCapsIntegration(t *testing.T) {
	t.Run("same_root_third_call", func(t *testing.T) {
		f := newLiveNativeFixture(t)
		b := f.configuration.native.private.base
		for i := 0; i < 2; i++ {
			f.reserve(t, f.preview(t, modelegressbudget.LiveCall, true))
		}
		third := f.preview(t, modelegressbudget.LiveCall, true)
		if _, err := b.store.ReserveOwnLiveAttempt(b.ctx, f.configuration.native.access, f.input(t, third), f.controller, nil); !errors.Is(err, modelegressbudget.ErrBudget) {
			t.Fatal("third same-root call bypassed two-request cap", err)
		}
		assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, CostMicros: 160000})
	})
	t.Run("owner_cumulative_ten_yuan", func(t *testing.T) {
		f := newLiveNativeFixture(t)
		b := f.configuration.native.private.base
		p := f.preview(t, modelegressbudget.LiveCall, true)
		// Fixture-only prior allocations exercise the real account cap without
		// generating fifty synthetic remote operations or claiming paid usage.
		b.exec(`UPDATE model_budget_accounts SET used_requests=1,allocated_input=1,allocated_output=1,allocated_cost=9950000 WHERE owner_id=$1`, b.person.ID)
		if _, err := b.store.ReserveOwnLiveAttempt(b.ctx, f.configuration.native.access, f.input(t, p), f.controller, nil); !errors.Is(err, modelegressbudget.ErrBudget) {
			t.Fatal("owner cumulative ten-yuan cap exceeded", err)
		}
		var count int
		if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_accounts WHERE owner_id=$1 AND allocated_cost=9950000 AND used_requests=1`, b.person.ID).Scan(&count); err != nil || count != 2 {
			t.Fatal("failed reservation modified prior account allocations", err)
		}
		var reservations int
		if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&reservations); err != nil || reservations != 0 {
			t.Fatal("over-limit operation created reservation", err)
		}
	})
}

func TestModelLiveNativeProviderFailureAndPostWireRevocationHoldIntegration(t *testing.T) {
	for _, name := range []string{"rate_limit", "temporary", "lost_response", "caller_cancelled", "changed_source_after_send"} {
		t.Run(name, func(t *testing.T) {
			f := newLiveNativeFixture(t)
			b := f.configuration.native.private.base
			a := f.configuration.native.access
			r := f.reserve(t, f.preview(t, modelegressbudget.LiveToken, true))
			port, request, err := b.store.NewOwnLiveModelDispatch(b.ctx, a, r.OperationID, f.controller, f.projector, liveNativeGate{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			calls := 0
			adapter := liveNativeModelAdapter(t, func(*http.Request) (*http.Response, error) {
				calls++
				assertLiveNativePhase(t, f, r.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
				switch name {
				case "rate_limit":
					return liveNativeResponse(`{}`, 429), nil
				case "temporary":
					return liveNativeResponse(`{}`, 503), nil
				case "lost_response":
					return nil, errors.New("unit lost response")
				case "caller_cancelled":
					cancel()
				case "changed_source_after_send":
					b.exec(`UPDATE agent_tasks SET query=query||' revoked-current-source',updated_at=clock_timestamp() WHERE id=$1 AND owner_account_id=$2`, r.TaskID, b.person.ID)
				}
				return liveNativeResponse(`{"id":"unit-late-text","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"不得释放的迟到文本"},"finish_reason":"stop"}]}`, 200), nil
			})
			gateway, err := modelgateway.NewNativeLiveGateway(liveNativeGate{}, adapter, port)
			if err != nil {
				t.Fatal(err)
			}
			result, err := gateway.Complete(ctx, request)
			if err == nil || result.Text != "" || calls != 1 {
				t.Fatal("failure/revocation retried or released text")
			}
			assertLiveNativePhase(t, f, r.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
			// SQL assertions remain valid after source revocation; no business
			// authorization is obtained by reading fixture counters directly.
			var roots int
			if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_roots WHERE root_trace_id=$1 AND allocated_cost=199680 AND allocated_input=196608 AND allocated_output=768 AND used_requests=1`, f.root).Scan(&roots); err != nil || roots != 1 {
				t.Fatal("failed operation refunded UNKNOWN hold", err)
			}
		})
	}
}

func TestModelLiveNativeOriginalLocalSyntheticRegressionIntegration(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	r := f.reserve(t, p)
	if r.ExecutionStatus != "UNAVAILABLE" || r.Currency != "GBP" || r.Upper.CostMicros != 392 {
		t.Fatal("original LOCAL synthetic bound changed")
	}
	if _, err := b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate); err != nil {
		t.Fatal("original local Begin regressed", err)
	}
	settled, err := b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, egressUsage(t, 20, 10))
	if err != nil || settled.State != "SETTLED" {
		t.Fatal("original synthetic local settlement regressed", err)
	}
	views, err := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if err != nil || len(views) != 4 {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Allocated != (modelegressbudget.Limits{Requests: 1, InputTokens: 20, OutputTokens: 10, CostMicros: 70}) {
			t.Fatal("LIVE UNKNOWN semantics changed old LOCAL settlement", v.Scope)
		}
	}
}
