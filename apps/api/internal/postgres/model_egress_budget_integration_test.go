package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
)

type egressFixture struct {
	f             *configurationFixture
	root, binding string
	price         modelegressbudget.Price
	limits        modelegressbudget.Limits
	gate          *agentfeature.Controller
}

func egressID(t *testing.T, f *egressFixture) string {
	t.Helper()
	var id string
	if e := f.f.native.private.base.pool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func egressGate(t *testing.T, on bool) *agentfeature.Controller {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": map[string]bool{"agent_enrichment": on, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}, "pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
	cfg, e := agentfeature.ParseConfig(body)
	if e != nil {
		t.Fatal(e)
	}
	c, e := agentfeature.NewController(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func newEgressFixture(t *testing.T, requests int64) *egressFixture {
	t.Helper()
	f := &egressFixture{f: configurationNativeFixture(t), gate: egressGate(t, true)}
	b := f.f.native.private.base
	t.Cleanup(func() {
		ctx, c := context.WithTimeout(context.Background(), 15*time.Second)
		defer c()
		for _, q := range []string{`DELETE FROM model_budget_audit WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_egress_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_tasks WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_roots WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_accounts WHERE owner_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, q, b.accounts); e != nil {
				t.Errorf("owned budget cleanup: %v", e)
			}
		}
		if f.price.Version != "" {
			if _, e := b.pool.Exec(ctx, `DELETE FROM model_local_price_versions WHERE version=$1`, f.price.Version); e != nil {
				t.Error(e)
			}
		}
	})
	route := activateConfigurationFixture(t, f.f, 0, 0)
	f.binding = egressID(t, f)
	r := f.f.request(t, 0, f.binding)
	if _, e := b.store.BindModelTaskConfiguration(b.ctx, f.f.native.access, f.f.native.task.ID, route.Version, route.Revision, r); e != nil {
		t.Fatal("real pre-run binding", e)
	}
	f.price = modelegressbudget.Price{Version: "price_" + strings.ReplaceAll(b.person.ID, "-", ""), Destination: modelcapability.Key{Provider: "fake", Model: "local", Version: "v1", WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: modelegressbudget.Retention, Currency: "GBP", InputMicrosPerToken: 2, OutputMicrosPerToken: 3, InputTokenCeiling: 100, OutputTokenCeiling: 128, Evidence: modelegressbudget.LocalPrice, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	if e := b.store.RegisterLocalModelPrice(b.ctx, f.price); e != nil {
		t.Fatal("real immutable synthetic price registration", e)
	}
	f.limits = modelegressbudget.Limits{Requests: requests, InputTokens: 100000, OutputTokens: 100000, CostMicros: 10000000}
	if e := b.store.ConfigureOwnModelBudget(b.ctx, f.f.native.access, f.f.native.task.ID, f.binding, "GBP", f.limits, f.limits); e != nil {
		t.Fatal("native budget account", e)
	}
	f.root = egressID(t, f)
	in := modelegressbudget.RootInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, BindingID: f.binding, Currency: "GBP", Limits: f.limits, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)}
	if e := b.store.CreateOwnModelBudgetRoot(b.ctx, f.f.native.access, in); e != nil {
		t.Fatal("native root", e)
	}
	return f
}
func (f *egressFixture) preview(t *testing.T, task string, approve bool) modelegressbudget.Preview {
	t.Helper()
	b := f.f.native.private.base
	p, e := b.store.PreviewOwnModelEgress(b.ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: task, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal("native source preview", e)
	}
	if approve {
		if e = b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
			t.Fatal("exact current human approval", e)
		}
	}
	return p
}
func (f *egressFixture) reserve(t *testing.T, p modelegressbudget.Preview) modelegressbudget.Reservation {
	t.Helper()
	b := f.f.native.private.base
	r, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.gate)
	if e != nil {
		t.Fatal("native reserve", e)
	}
	return r
}
func egressUsage(t *testing.T, in, out int64) modelegressbudget.LocalUsage {
	t.Helper()
	u, e := modelegressbudget.NewLocalUsage(modelgateway.Usage{Status: "KNOWN", CostStatus: "UNKNOWN", InputTokens: &in, OutputTokens: &out})
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func TestModelEgressNativePreviewApprovalReservationPersistenceIntegration(t *testing.T) {
	f := newEgressFixture(t, 8)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, false)
	if len(p.Request.Messages) != 2 || p.Request.Messages[0].Content != f.f.configs[0].Prompt.Text || p.Request.Messages[1].Content != f.f.native.task.Query || p.Request.Agent.AgentID != b.personID {
		t.Fatal("request was not server assembled from exact own source")
	}
	if e := b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, strings.Repeat("b", 64)); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("changed human approval accepted", e)
	}
	input := modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}
	if _, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, input, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("flag ON created missing approval", e)
	}
	if e := b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
		t.Fatal(e)
	}
	r, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, input, f.gate)
	if e != nil || r.ExecutionStatus != "UNAVAILABLE" || r.Upper != (modelegressbudget.Amount{InputTokens: 100, OutputTokens: 64, CostMicros: 392}) {
		t.Fatal(r, e)
	}
	pool, e := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	restarted := New(pool, false)
	duplicate, e := restarted.ReserveOwnModelAttempt(b.ctx, f.f.native.access, input, f.gate)
	if e != nil || !duplicate.CreatedAt.Equal(r.CreatedAt) {
		t.Fatal("restart/idempotency changed reservation", e)
	}
	views, e := restarted.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil || len(views) != 4 {
		t.Fatal(e)
	}
	for _, v := range views {
		if v.Allocated.Requests != 1 || v.Allocated.CostMicros != 392 {
			t.Fatal("duplicate charged twice", v)
		}
	}
	request, e := restarted.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate)
	if e != nil || modelegressbudget.Digest(request, p.Price) != p.RequestDigest {
		t.Fatal("current begin changed immutable input", e)
	}
	if _, e = restarted.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("same reservation returned second attempt", e)
	}
	result, e := modelegressbudget.NewService(nil).Complete(b.ctx, request)
	if !errors.Is(e, modelgateway.ErrUnavailable) || result.ProviderID != "" || result.Status != modelgateway.Unavailable {
		t.Fatal("native approval opened real provider", e)
	}
	unknown, e := restarted.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, modelegressbudget.LocalUsage{})
	if e != nil || unknown.State != "UNKNOWN" {
		t.Fatal(e)
	}
	views, e = restarted.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil || views[0].Allocated.CostMicros != 392 {
		t.Fatal("unknown refunded budget", e)
	}
	settled, e := restarted.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, egressUsage(t, 12, 7))
	if e != nil || settled.State != "SETTLED" {
		t.Fatal(e)
	}
	if _, e = restarted.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, egressUsage(t, 12, 7)); e != nil {
		t.Fatal("idempotent settlement", e)
	}
	if _, e = restarted.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, egressUsage(t, 13, 7)); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("different settlement overwrote bill", e)
	}
	views, e = restarted.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range views {
		if v.Allocated.Requests != 1 || v.Allocated.InputTokens != 12 || v.Allocated.OutputTokens != 7 || v.Allocated.CostMicros != 45 {
			t.Fatal("settlement counters wrong", v)
		}
	}
	for _, table := range []string{"model_budget_roots", "model_budget_tasks", "model_egress_previews", "model_budget_reservations", "model_budget_audit"} {
		var raw []byte
		if e = b.pool.QueryRow(b.ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM `+table+` t WHERE owner_id=$1`, b.person.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		for _, canary := range []string{f.f.native.task.Query, f.f.native.task.Conversation[0].Text, f.f.configs[0].Prompt.Text, "token_sha256", "Bearer", "messages"} {
			if strings.Contains(string(raw), canary) {
				t.Fatal("private body in new ledger", table)
			}
		}
	}
}
func TestModelEgressCurrentSourceAndIdentityFailClosedIntegration(t *testing.T) {
	cases := []struct {
		name   string
		change func(*egressFixture)
		access func(*egressFixture) agentevent.Access
	}{
		{"anonymous", func(*egressFixture) {}, func(*egressFixture) agentevent.Access { return agentevent.Access{} }},
		{"wrong_session", func(*egressFixture) {}, func(*egressFixture) agentevent.Access { return agentevent.Access{SessionDigest: [32]byte{8}} }},
		{"peer", func(*egressFixture) {}, func(f *egressFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
		}},
		{"organization", func(*egressFixture) {}, func(f *egressFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.f.native.private.org.SessionDigest}
		}},
		{"revoke", func(f *egressFixture) {
			f.f.native.private.base.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.f.native.private.ownerSession)
		}, nil},
		{"suspended", func(f *egressFixture) {
			f.f.native.private.base.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.f.native.private.base.person.ID)
		}, nil},
		{"task_failed", func(f *egressFixture) {
			f.f.native.private.base.exec(`UPDATE agent_tasks SET status='FAILED',updated_at=clock_timestamp() WHERE id=$1`, f.f.native.task.ID)
		}, nil},
		{"task_conversation_changed", func(f *egressFixture) {
			f.f.native.private.base.exec(`UPDATE agent_tasks SET conversation='[]'::jsonb WHERE id=$1`, f.f.native.task.ID)
		}, nil},
		{"task_query_changed", func(f *egressFixture) {
			f.f.native.private.base.exec(`UPDATE agent_tasks SET query='新query' WHERE id=$1`, f.f.native.task.ID)
		}, nil},
		{"task_deleted", func(f *egressFixture) {
			f.f.native.private.base.exec(`DELETE FROM agent_tasks WHERE id=$1`, f.f.native.task.ID)
		}, nil},
		{"agent_retired", func(f *egressFixture) {
			f.f.native.private.base.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.f.native.private.base.personID)
		}, nil},
		{"metadata_deleted", func(f *egressFixture) {
			f.f.native.private.base.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.f.native.private.base.personID)
		}, nil},
		{"metadata_recreated", func(f *egressFixture) {
			b := f.f.native.private.base
			b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
			b.exec(`INSERT INTO agent_profiles(agent_id,owner_type,owner_id) VALUES($1,'PERSON',$2)`, b.personID, b.person.ID)
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			c.change(f)
			a := f.f.native.access
			if c.access != nil {
				a = c.access(f)
			}
			if r, e := b.store.ReserveOwnModelAttempt(b.ctx, a, modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.gate); e == nil || r.OperationID != "" {
				t.Fatal("late or wrong subject allowed", e)
			}
			var n int
			if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&n); e != nil || n != 0 {
				t.Fatal("denied request wrote attempt", n, e)
			}
		})
	}
}
func TestModelEgressRevocationCancellationAndUnknownHoldIntegration(t *testing.T) {
	f := newEgressFixture(t, 6)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	reserved := f.reserve(t, p)
	if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, reserved.OperationID, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("revoked request began", e)
	}
	if e := b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, p.RequestDigest); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("revoked grant restored", e)
	}
	cancelled, e := b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, reserved.OperationID)
	if e != nil || cancelled.State != "CANCELLED_BEFORE_SEND" {
		t.Fatal(e)
	}
	if _, e = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, reserved.OperationID); e != nil {
		t.Fatal(e)
	}
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil || views[0].Allocated.Requests != 1 || views[0].Allocated.CostMicros != 0 {
		t.Fatal("cancel reset count or doubled refund", e)
	}
	p2 := f.preview(t, f.f.native.task.ID, true)
	r := f.reserve(t, p2)
	if _, e = b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, r.OperationID); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("in-flight claimed not sent", e)
	}
	if e = b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p2.ID); e != nil {
		t.Fatal(e)
	}
	b.exec(`DELETE FROM agent_tasks WHERE id=$1`, p2.TaskID)
	if _, e = b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, modelegressbudget.LocalUsage{}); e != nil {
		t.Fatal("source deletion lost accounting", e)
	}
	views, e = b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p2.TaskID)
	if e != nil || views[0].Allocated.Requests != 2 || views[0].Allocated.CostMicros != 392 {
		t.Fatal("unknown after deletion refunded", e)
	}
}
func TestModelEgressConcurrentBudgetAtomicityAndChildShareIntegration(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	child, e := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: "aberdeen-gb", Query: "合成下级Task", Intent: "PENDING", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{}})
	if e != nil {
		t.Fatal(e)
	}
	binding := egressID(t, f)
	req := f.f.request(t, 0, binding)
	if _, e = b.store.BindModelTaskConfiguration(b.ctx, f.f.native.access, child.ID, f.f.route.Version, f.f.route.Revision, req); e != nil {
		t.Fatal(e)
	}
	if e = b.store.BindOwnModelBudgetTask(b.ctx, f.f.native.access, modelegressbudget.TaskInput{RootTraceID: f.root, TaskID: child.ID, BindingID: binding, Limits: f.limits}); e != nil {
		t.Fatal(e)
	}
	p := f.preview(t, f.f.native.task.ID, true)
	p2 := f.preview(t, child.ID, true)
	ids := make([]string, 12)
	for i := range ids {
		ids[i] = egressID(t, f)
	}
	var mu sync.Mutex
	success, budget := 0, 0
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id string) {
			defer wg.Done()
			sel := p
			if i%2 == 1 {
				sel = p2
			}
			_, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: id, PreviewID: sel.ID, RootTraceID: f.root, TaskID: sel.TaskID}, f.gate)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				success++
			} else if errors.Is(e, modelegressbudget.ErrBudget) {
				budget++
			} else {
				t.Errorf("concurrency error: %v", e)
			}
		}(i, id)
	}
	wg.Wait()
	if success != 3 || budget != 9 {
		t.Fatal("shared root exceeded or lost atomic cap", success, budget)
	}
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, child.ID)
	if e != nil || views[2].Allocated.Requests != 3 || views[0].Allocated.CostMicros != 1176 {
		t.Fatal("shared root budget not retained", e)
	}
	if e = b.store.ConfigureOwnModelBudget(b.ctx, f.f.native.access, f.f.native.task.ID, f.binding, "GBP", f.limits, f.limits); e != nil {
		t.Fatal("idempotent account configure", e)
	}
	larger := f.limits
	larger.Requests++
	if e = b.store.ConfigureOwnModelBudget(b.ctx, f.f.native.access, f.f.native.task.ID, f.binding, "GBP", larger, larger); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("reset/raise account cap", e)
	}
}
func TestModelEgressSameOperationConcurrentAndChangedSelectorIntegration(t *testing.T) {
	f := newEgressFixture(t, 10)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	in := modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, in, f.gate)
			if e != nil || r.OperationID != in.OperationID {
				t.Errorf("duplicate op: %v", e)
			}
		}()
	}
	wg.Wait()
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil || views[0].Allocated.Requests != 1 {
		t.Fatal("duplicate attempts charged", e)
	}
	p2 := f.preview(t, p.TaskID, true)
	in.PreviewID = p2.ID
	if _, e = b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, in, f.gate); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("same operation changed tuple", e)
	}
}
func TestModelEgressMissingPriceOffGateAndBudgetExhaustionIntegration(t *testing.T) {
	f := newEgressFixture(t, 1)
	b := f.f.native.private.base
	if _, e := b.store.PreviewOwnModelEgress(b.ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, PriceVersion: "missing.v1", MaxOutputTokens: 64, DeadlineAt: time.Now().Add(time.Minute)}); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("missing price became free", e)
	}
	p := f.preview(t, f.f.native.task.ID, true)
	in := modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}
	for _, g := range []*agentfeature.Controller{nil, egressGate(t, false)} {
		if _, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, in, g); !errors.Is(e, modelegressbudget.ErrUnavailable) {
			t.Fatal("off flag allowed attempt", e)
		}
	}
	r := f.reserve(t, p)
	if e := f.gate.Disable(agentfeature.Enrichment); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate); !errors.Is(e, modelegressbudget.ErrUnavailable) {
		t.Fatal("kill failed", e)
	}
	if e := b.store.RevokeOwnModelEgress(b.ctx, f.f.native.access, p.ID); e != nil {
		t.Fatal("kill disabled human revoke", e)
	}
	if _, e := b.store.CancelOwnReservedModelAttempt(b.ctx, f.f.native.access, r.OperationID); e != nil {
		t.Fatal(e)
	}
	f.gate = egressGate(t, true)
	p2 := f.preview(t, p.TaskID, true)
	if _, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p2.ID, RootTraceID: f.root, TaskID: p2.TaskID}, f.gate); !errors.Is(e, modelegressbudget.ErrBudget) {
		t.Fatal("failed attempt did not count", e)
	}
}
func TestModelEgressLateDeadlineSessionAndKillBarrierIntegration(t *testing.T) {
	for _, kind := range []string{"deadline", "session", "kill"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			app := "air011_" + strings.ReplaceAll(b.person.ID, "-", "")
			cfg.ConnConfig.RuntimeParams["application_name"] = app
			cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
			cfg.MaxConns = 1
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			store := New(pool, false)
			cutoff := time.Now().Add(650 * time.Millisecond)
			if kind == "deadline" {
				p, e = b.store.PreviewOwnModelEgress(b.ctx, f.f.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: p.TaskID, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: cutoff})
				if e != nil {
					t.Fatal(e)
				}
				if e = b.store.ApproveOwnModelEgress(b.ctx, f.f.native.access, p.ID, p.RequestDigest); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "session" {
				b.exec(`UPDATE sessions SET idle_expires_at=$2 WHERE id=$1`, f.f.native.private.ownerSession, cutoff)
			}
			blocker, e := b.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			if e = egressOwnerLock(ctx, blocker, b.person.ID); e != nil {
				t.Fatal(e)
			}
			op := egressID(t, f)
			done := make(chan error, 1)
			go func() {
				r, e := store.ReserveOwnModelAttempt(ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: op, PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.gate)
				if e == nil || r.OperationID != "" {
					done <- errors.New("late barrier returned attempt")
				} else {
					done <- e
				}
			}()
			for {
				var blocked bool
				if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, app).Scan(&blocked); e != nil {
					t.Fatal(e)
				}
				if blocked {
					break
				}
				select {
				case e := <-done:
					t.Fatal("did not wait", e)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if kind == "kill" {
				if e = f.gate.Disable(agentfeature.Enrichment); e != nil {
					t.Fatal(e)
				}
			} else {
				for time.Now().Before(cutoff.Add(30 * time.Millisecond)) {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if e = blocker.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			e = <-done
			if !errors.Is(e, modelegressbudget.ErrDenied) && !errors.Is(e, modelegressbudget.ErrUnavailable) {
				t.Fatal("late current guard wrong", e)
			}
			var n int
			if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM model_budget_reservations WHERE operation_id=$1`, op).Scan(&n); e != nil || n != 0 {
				t.Fatal("late barrier retained side effect", e)
			}
		})
	}
}

func TestModelEgressNativePriceImmutableAndLedgerSQLGuardsIntegration(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	if e := b.store.RegisterLocalModelPrice(b.ctx, f.price); e != nil {
		t.Fatal("same price not idempotent", e)
	}
	changed := f.price
	changed.OutputMicrosPerToken++
	if e := b.store.RegisterLocalModelPrice(b.ctx, changed); !errors.Is(e, modelegressbudget.ErrConflict) {
		t.Fatal("same price version mutated", e)
	}
	p := f.preview(t, f.f.native.task.ID, true)
	r := f.reserve(t, p)
	cases := []struct{ name, sql, id string }{
		{"price_rate", `UPDATE model_local_price_versions SET input_rate=input_rate+1 WHERE version=$1`, f.price.Version},
		{"root_raise", `UPDATE model_budget_roots SET max_requests=max_requests+1 WHERE root_trace_id=$1`, f.root},
		{"account_reset", `UPDATE model_budget_accounts SET used_requests=0 WHERE owner_id=$1`, b.person.ID},
		{"task_reset", `UPDATE model_budget_tasks SET used_requests=0 WHERE root_trace_id=$1`, f.root},
		{"preview_scope", `UPDATE model_egress_previews SET scope='PRIVATE_MEMORY',revision=revision+1 WHERE id=$1`, p.ID},
		{"preview_bodydigest", `UPDATE model_egress_previews SET request_digest=repeat('b',64),revision=revision+1 WHERE id=$1`, p.ID},
		{"preview_restore", `UPDATE model_egress_previews SET status='DRAFT',revision=revision+1 WHERE id=$1`, p.ID},
		{"reservation_free", `UPDATE model_budget_reservations SET upper_cost=0 WHERE operation_id=$1`, r.OperationID},
		{"reservation_early_settle", `UPDATE model_budget_reservations SET state='SETTLED',reported_input=1,reported_output=1,settled_cost=5 WHERE operation_id=$1`, r.OperationID},
		{"fake_live", `UPDATE model_budget_reservations SET execution_status='LIVE' WHERE operation_id=$1`, r.OperationID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, e := b.pool.Exec(b.ctx, c.sql, c.id); e == nil {
				t.Fatal("SQL protection missing")
			}
		})
	}
	if _, e := b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate); e != nil {
		t.Fatal(e)
	}
	if _, e := b.store.SettleOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, egressUsage(t, 101, 64)); !errors.Is(e, modelegressbudget.ErrInvalid) {
		t.Fatal("usage exceeded original upper", e)
	}
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil || views[0].Allocated.CostMicros != 392 {
		t.Fatal("invalid receipt changed allocation", e)
	}
}

func TestModelEgressNativeRevokeBeforeReservationBarrierIntegration(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	app := "air011_revoke_" + strings.ReplaceAll(b.person.ID, "-", "")
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	cfg.MaxConns = 1
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store := New(pool, false)
	blocker, e := b.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(ctx, `SELECT id FROM model_egress_previews WHERE id=$1 FOR UPDATE`, p.ID); e != nil {
		t.Fatal(e)
	}
	revoked := make(chan error, 1)
	go func() { revoked <- store.RevokeOwnModelEgress(ctx, f.f.native.access, p.ID) }()
	for {
		var blocked bool
		if e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, app).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		select {
		case e := <-revoked:
			t.Fatal("native revoke did not wait", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	op := egressID(t, f)
	reserved := make(chan error, 1)
	go func() {
		_, e := b.store.ReserveOwnModelAttempt(ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: op, PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.gate)
		reserved <- e
	}()
	if e = blocker.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-revoked; e != nil {
		t.Fatal(e)
	}
	if e = <-reserved; !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("revoke preceding commit did not deny", e)
	}
	var count int
	if e = b.pool.QueryRow(ctx, `SELECT count(*) FROM model_budget_reservations WHERE operation_id=$1`, op).Scan(&count); e != nil || count != 0 {
		t.Fatal("revoked-before reserve persisted", e)
	}
}

var _ pgx.Tx

type egressContractOnGate struct{}

func (egressContractOnGate) InferenceEnabled(context.Context) bool { return true }

func TestModelEgressSessionIdleRefreshDoesNotReplaceApprovalIntegration(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	// Ordinary native session maintenance changes the retained session row,
	// not the owner/Agent source tuple or exact explicitly approved payload.
	if _, e := b.store.Authenticate(b.ctx, f.f.native.access.SessionDigest); e != nil {
		t.Fatal("native Authenticate/idle refresh", e)
	}
	r := f.reserve(t, p)
	request, e := b.store.BeginOwnLocalModelAttempt(b.ctx, f.f.native.access, r.OperationID, f.gate)
	if e != nil {
		t.Fatal("idle refresh invalidated current scope", e)
	}
	for _, gate := range []modelgateway.LiveGate{nil, egressContractOnGate{}} {
		result, e := modelegressbudget.NewService(gate).Complete(b.ctx, request)
		if !errors.Is(e, modelgateway.ErrUnavailable) || result.ProviderID != "" || result.Status != modelgateway.Unavailable {
			t.Fatal("valid native approval/ON gate opened live provider", e)
		}
	}
	// Try the real current-record down against a native IN_FLIGHT ledger in a
	// disposable transaction. The actual migration must refuse before any DDL.
	down, e := os.ReadFile("../../migrations/062_model_egress_budget.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, downErr := tx.Exec(b.ctx, strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;"))
	_ = tx.Rollback(context.Background())
	if downErr == nil || !strings.Contains(downErr.Error(), "records must be preserved") {
		t.Fatal("actual nonempty ledger down did not refuse", downErr)
	}
	views, e := b.store.ReadOwnModelBudget(b.ctx, f.f.native.access, f.root, p.TaskID)
	if e != nil || views[0].Allocated.CostMicros != 392 || views[0].Allocated.Requests != 1 {
		t.Fatal("failed down changed accounting", e)
	}
}

func TestModelEgressSuspendRestoreOldApprovalMustRejectIntegration(t *testing.T) {
	for _, kind := range []string{"account", "agent"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			if kind == "account" {
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
			} else {
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			}
			if _, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.gate); e == nil {
				t.Fatal("disabled principal should deny")
			}
			if kind == "account" {
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			} else {
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			}
			r, e := b.store.ReserveOwnModelAttempt(b.ctx, f.f.native.access, modelegressbudget.ReserveInput{OperationID: egressID(t, f), PreviewID: p.ID, RootTraceID: f.root, TaskID: p.TaskID}, f.gate)
			if !errors.Is(e, modelegressbudget.ErrDenied) || r.OperationID != "" {
				t.Fatalf("old approval revived after actual %s disable/restore; reservation=%s execution=%s error=%v", kind, r.OperationID, r.ExecutionStatus, e)
			}
			// The restored native identity can explicitly create a fresh budget
			// group/preview without resetting its retained subject/tenant counters.
			f.root = egressID(t, f)
			if e = b.store.CreateOwnModelBudgetRoot(b.ctx, f.f.native.access, modelegressbudget.RootInput{RootTraceID: f.root, TaskID: p.TaskID, BindingID: f.binding, Currency: "GBP", Limits: f.limits, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)}); e != nil {
				t.Fatal("restored owner cannot create new exact scope", e)
			}
			fresh := f.preview(t, p.TaskID, true)
			f.reserve(t, fresh)
			if fresh.AuthorityToken == p.AuthorityToken {
				t.Fatal("native row generation was not bound")
			}
		})
	}
}
