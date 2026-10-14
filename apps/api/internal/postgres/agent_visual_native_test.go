package postgres

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	mb "github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type agentVisualNativeGate struct{ enabled bool }

func (g *agentVisualNativeGate) InferenceEnabled(context.Context) bool { return g.enabled }

type agentVisualRoundTripper func(*http.Request) (*http.Response, error)

func (f agentVisualRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type agentVisualNativeFixture struct {
	t                     *testing.T
	ctx                   context.Context
	pool                  *pgxpool.Pool
	store                 *Store
	access                av.Access
	gateway               av.Gateway
	gate                  *agentVisualNativeGate
	city, contextID, mode string
	calls, bytes          int
	last                  []byte
	sequence              int
}

func newAgentVisualNativeFixture(t *testing.T) *agentVisualNativeFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	f := &agentVisualNativeFixture{t: t, ctx: ctx, pool: pool, store: New(pool, false), gate: &agentVisualNativeGate{true}}
	var owner string
	if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id::text`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO user_profiles(account_id,display_name) VALUES($1,'Synthetic visual owner')`, owner)
	f.exec(`INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1)`, owner)
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.access = av.Access{AccountID: owner, AccountType: "person", SessionDigest: digest}
	f.exec(`WITH clock AS MATERIALIZED (SELECT clock_timestamp() AS now) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT $1,$2,'test',now+interval '1 hour',now+interval '30 minutes' FROM clock`, owner, digest[:])
	if e = pool.QueryRow(ctx, `SELECT c.city_id,c.id::text FROM contexts c JOIN cities ci ON ci.id=c.city_id JOIN city_contexts cc ON cc.city_id=c.city_id WHERE c.context_type='CITY' AND ci.publication_status='published' AND cc.status='active' AND (ci.expires_at IS NULL OR ci.expires_at>clock_timestamp()) ORDER BY c.id LIMIT 1`).Scan(&f.city, &f.contextID); e != nil {
		t.Fatal("owned seeded city", e)
	}
	// Explicit synthetic server funding in the owned DB. Preview must never
	// provision or increase these tenant/subject accounts itself.
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		f.exec(`INSERT INTO model_budget_accounts(owner_id,scope,currency,max_requests,max_input,max_output,max_cost) VALUES($1,$2,'CNY',10,1000000,10000,3000000)`, owner, scope)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiry := now.Add(time.Hour)
	price := mb.LivePrice{Base: mb.Price{Version: "synthetic-visual-v1", Destination: mb.VisualDestination(), Region: modelcapability.APAC, Retention: "UNKNOWN", Currency: "CNY", InputMicrosPerToken: 8, OutputMicrosPerToken: 18, InputTokenCeiling: 24576, OutputTokenCeiling: 768, Evidence: mb.LiveTariffEvidence, ExpiresAt: expiry}, Kind: mb.LiveToken, RequestCeiling: 1, Snapshot: mb.LiveSnapshot{SourceURL: mb.LiveModelPriceURL, ArtifactSHA256: av.Digest([]byte("explicit synthetic tariff artifact; no real provider evidence")), DocumentUpdatedAt: now.Add(-time.Hour), ObservedAt: now, ExpiresAt: expiry}}
	if e = f.store.RegisterAgentVisualPrice(ctx, price); e != nil {
		t.Fatal("visual price", e)
	}
	config, e := modelgateway.ParseTencentVisionConfig([]byte(`{"provider":"tencent_tokenhub","baseUrl":"https://tokenhub.tencentmaas.com/v1","model":"hy-vision-2.0-instruct","region":"APAC","maxOutputTokens":768}`), "SYNTHETIC-VISION-SECRET")
	if e != nil {
		t.Fatal(e)
	}
	adapter, e := modelgateway.NewTencentVisionAdapter(config, agentVisualRoundTripper(func(r *http.Request) (*http.Response, error) {
		f.calls++
		if r.URL.String() != "https://tokenhub.tencentmaas.com/v1/chat/completions" || r.GetBody != nil || !r.Close {
			t.Fatal("native dispatch escaped fixed endpoint")
		}
		if f.mode == "session-revoke" {
			f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:])
		}
		if f.mode == "context-revoke" {
			f.exec(`DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=$2`, owner, f.contextID)
		}
		body, e := io.ReadAll(r.Body)
		f.bytes += len(body)
		f.last = append([]byte{}, body...)
		if e != nil {
			return nil, e
		}
		if f.mode == "uncertain" {
			return nil, errors.New("synthetic unknown transport outcome")
		}
		report := `{"summary":"合成测试画面","observations":[{"kind":"VISIBLE_OBJECT","value":"测试色块","confidence":{"kind":"QUALITATIVE","level":"LOW"}}],"unknowns":["拍摄地点未知","拍摄时间未知"]}`
		response, _ := json.Marshal(map[string]any{"id": "synthetic-visual-native", "model": modelgateway.TencentVisionModel, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": report}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 300, "completion_tokens": 70, "total_tokens": 370}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(response))}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	f.gateway = NewAgentVisualGateway(f.store, AgentVisualOptions{Adapter: adapter, Gate: f.gate, PriceVersion: price.Base.Version, Region: "APAC"})
	return f
}
func (f *agentVisualNativeFixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, e := f.pool.Exec(f.ctx, q, args...); e != nil {
		f.t.Fatal(e)
	}
}
func (f *agentVisualNativeFixture) id() string {
	f.t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		f.t.Fatal(e)
	}
	return id
}
func (f *agentVisualNativeFixture) input() av.PreviewInput {
	f.t.Helper()
	f.sequence++
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 12)))
	return av.PreviewInput{OperationID: f.id(), ClientDraftID: f.id(), ClientDraftRevision: int64(f.sequence), SourceContextType: "CITY", SourceContextID: f.city, Purpose: av.Purpose, Image: av.ImageInput{MIME: "image/png", ContentBase64: base64.StdEncoding.EncodeToString(b.Bytes()), InputSHA256: av.Digest(b.Bytes()), PixelRisk: "UNKNOWN"}}
}
func (f *agentVisualNativeFixture) preview(in av.PreviewInput) av.Preview {
	f.t.Helper()
	p, e := f.gateway.Preview(f.ctx, f.access, in)
	if e != nil {
		f.t.Fatal("preview", e)
	}
	return p
}
func (f *agentVisualNativeFixture) approval(p av.Preview) av.ApproveInput {
	return av.ApproveInput{OperationID: f.id(), ExpectedVersion: p.Version, PayloadDigest: p.PayloadDigest, PixelSHA256: p.Pixel.SHA256, Purpose: av.Purpose, Confirmed: true}
}
func (f *agentVisualNativeFixture) held(p av.Preview, op string) {
	f.t.Helper()
	var root string
	if e := f.pool.QueryRow(f.ctx, `SELECT root_trace_id::text FROM agent_visual_previews WHERE id=$1`, p.PreviewID).Scan(&root); e != nil {
		f.t.Fatal(e)
	}
	for _, q := range []string{`SELECT used_requests,allocated_input,allocated_output,allocated_cost FROM model_budget_roots WHERE root_trace_id=$1`, `SELECT used_requests,allocated_input,allocated_output,allocated_cost FROM model_budget_tasks WHERE root_trace_id=$1`} {
		var requests, input, output, cost int64
		if e := f.pool.QueryRow(f.ctx, q, root).Scan(&requests, &input, &output, &cost); e != nil || requests != 1 || input != 24576 || output != 768 || cost != 210432 {
			f.t.Fatal("exact original root/task hold", requests, input, output, cost, e)
		}
	}
	var state, cash string
	if e := f.pool.QueryRow(f.ctx, `SELECT state,cash_status FROM model_budget_reservations WHERE operation_id=$1`, op).Scan(&state, &cash); e != nil || state != "UNKNOWN" || cash != "UNKNOWN" {
		f.t.Fatal("attempt released unknown cash", state, cash, e)
	}
}

// This is actual PostgreSQL and actual adapter wire, with an explicitly fake
// RoundTripper and tariff fixture. It is not real provider/vision acceptance.
func TestAgentVisualNativePureImageApprovedPixelsFourBudgetsAndRecovery(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newAgentVisualNativeFixture(t)
	in := f.input()
	p := f.preview(in)
	if p.UserText != "" || p.Version != 1 || p.MemoryWriteAllowed || p.PlaceFactWriteAllowed {
		t.Fatal("pure image changed native purpose")
	}
	var before int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_tasks WHERE owner_account_id=$1`, f.access.AccountID).Scan(&before)
	if before != 0 {
		t.Fatal("pure image created a fake TEXT task")
	}
	var used int64
	f.pool.QueryRow(f.ctx, `SELECT sum(used_requests) FROM model_budget_accounts WHERE owner_id=$1`, f.access.AccountID).Scan(&used)
	if used != 0 {
		t.Fatal("preview reserved before approval")
	}
	duplicate, e := f.gateway.Preview(f.ctx, f.access, in)
	if e != nil || duplicate.PreviewID != p.PreviewID {
		t.Fatal("preview operation replay", e)
	}
	meta, pixels, e := f.gateway.Pixels(f.ctx, f.access, p.PreviewID)
	if e != nil || av.Digest(pixels) != p.Pixel.SHA256 || meta.ByteSize != len(pixels) {
		t.Fatal("exact review pixels", e)
	}
	approval := f.approval(p)
	bad := approval
	bad.PixelSHA256 = strings.Repeat("b", 64)
	if _, e = f.gateway.Analyze(f.ctx, f.access, p.PreviewID, bad); !errors.Is(e, av.ErrConflict) || f.calls != 0 {
		t.Fatal("wrong pixels released", e)
	}
	result, e := f.gateway.Analyze(f.ctx, f.access, p.PreviewID, approval)
	if e != nil || result.Status != "COMPLETED" || result.Report == nil || len(result.Report.Observations) != 1 || f.calls != 1 {
		t.Fatal("complete", result.Status, e, f.calls)
	}
	obs := result.Report.Observations[0]
	if obs.Source.ID != p.Pixel.ID || obs.Source.SHA256 != p.Pixel.SHA256 || obs.Source.Revision != p.Pixel.Revision || obs.ObservedAt.IsZero() {
		t.Fatal("provider invented source/time")
	}
	if !bytes.Contains(f.last, []byte("data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(pixels))) || bytes.Contains(f.last, []byte(f.access.AccountID)) || bytes.Contains(f.last, []byte(p.PreviewID)) || bytes.Contains(f.last, []byte(f.city)) {
		t.Fatal("actual wire omitted exact pixels or disclosed native IDs/context")
	}
	replay, e := f.gateway.Analyze(f.ctx, f.access, p.PreviewID, approval)
	if e != nil || replay.Status != "COMPLETED" || f.calls != 1 {
		t.Fatal("approved operation re-egressed", e)
	}
	recovered, e := f.gateway.Operation(f.ctx, f.access, approval.OperationID)
	if e != nil || recovered.Status != "COMPLETED" {
		t.Fatal("GET recovery", e)
	}
	f.held(p, approval.OperationID)
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		var requests, input, output, cost int64
		e = f.pool.QueryRow(f.ctx, `SELECT used_requests,allocated_input,allocated_output,allocated_cost FROM model_budget_accounts WHERE owner_id=$1 AND scope=$2`, f.access.AccountID, scope).Scan(&requests, &input, &output, &cost)
		if e != nil || requests != 1 || input != 24576 || output != 768 || cost != 210432 {
			t.Fatal("exact tenant/subject hold", e)
		}
	}
	var retained int
	f.pool.QueryRow(f.ctx, `SELECT coalesce(octet_length(pixels),0) FROM agent_visual_previews WHERE id=$1`, p.PreviewID).Scan(&retained)
	if retained != 0 {
		t.Fatal("terminal retained temporary pixels")
	}
	uncertain := f.preview(f.input())
	uncertainApproval := f.approval(uncertain)
	f.mode = "uncertain"
	unknown, e := f.gateway.Analyze(f.ctx, f.access, uncertain.PreviewID, uncertainApproval)
	if e != nil || unknown.Status != "UNKNOWN" || unknown.Report != nil {
		t.Fatal("unknown outcome", e)
	}
	calls := f.calls
	unknown, e = f.gateway.Analyze(f.ctx, f.access, uncertain.PreviewID, uncertainApproval)
	if e != nil || unknown.Status != "UNKNOWN" || f.calls != calls {
		t.Fatal("UNKNOWN POST replayed", e)
	}
	unknown, e = f.gateway.Operation(f.ctx, f.access, uncertainApproval.OperationID)
	if e != nil || unknown.Status != "UNKNOWN" {
		t.Fatal("unknown recovery", e)
	}
	f.held(uncertain, uncertainApproval.OperationID)
	// Approved scalar or human-only purposes cannot be selected as visual.
	oldPurpose := f.input()
	oldPurpose.Purpose = "HUMAN_TASK_MESSAGE_ATTACHMENT"
	if _, e = f.gateway.Preview(f.ctx, f.access, oldPurpose); !errors.Is(e, av.ErrInvalid) {
		t.Fatal("legacy purpose borrowed", e)
	}
	f.mode = "session-revoke"
	revoked := f.preview(f.input())
	revokedApproval := f.approval(revoked)
	bytesBefore := f.bytes
	out, e := f.gateway.Analyze(f.ctx, f.access, revoked.PreviewID, revokedApproval)
	if e != nil || out.Status != "UNKNOWN" || f.bytes != bytesBefore {
		t.Fatal("revoked session leaked first body bytes", out.Status, e)
	}
	f.held(revoked, revokedApproval.OperationID)
	f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, f.access.SessionDigest[:])
	if _, _, e = f.gateway.Pixels(f.ctx, f.access, revoked.PreviewID); !errors.Is(e, av.ErrDenied) {
		t.Fatal("session restore revived old exact approval", e)
	}
}

func TestAgentVisualNativeCurrentSourceRetiresApproval(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newAgentVisualNativeFixture(t)
	var online string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO contexts(context_type,online_key) VALUES('ONLINE',$1) RETURNING id::text`, "visual-fixture-"+f.id()).Scan(&online); e != nil {
		t.Fatal(e)
	}
	f.contextID = online
	f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'interest','private')`, f.access.AccountID, online)
	in := f.input()
	in.SourceContextType = "ONLINE"
	in.SourceContextID = online
	in.UserText = "画面有什么？"
	p := f.preview(in)
	f.mode = "context-revoke"
	result, e := f.gateway.Analyze(f.ctx, f.access, p.PreviewID, f.approval(p))
	if e != nil || result.Status != "UNKNOWN" || f.bytes != 0 {
		t.Fatal("withdrawn current context egressed pixels", result.Status, e, f.bytes)
	}
	f.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'interest','private')`, f.access.AccountID, online)
	if _, _, e = f.gateway.Pixels(f.ctx, f.access, p.PreviewID); !errors.Is(e, av.ErrDenied) {
		t.Fatal("redeclaration revived original source proof", e)
	}
	f.mode = ""
	in.OperationID = f.id()
	fresh := f.preview(in)
	replacement := f.preview(inWithVisualNewOperation(f, in))
	if _, _, e = f.gateway.Pixels(f.ctx, f.access, fresh.PreviewID); !errors.Is(e, av.ErrDenied) {
		t.Fatal("new preview revived retired pixels", e)
	}
	if _, _, e = f.gateway.Pixels(f.ctx, f.access, replacement.PreviewID); e != nil {
		t.Fatal(e)
	}
	f.gate.enabled = false
	if _, e = f.gateway.Analyze(f.ctx, f.access, replacement.PreviewID, f.approval(replacement)); !errors.Is(e, av.ErrDenied) {
		t.Fatal("disabled gate dispatched", e)
	}
	// SQL cannot reuse the vision tariff under an older TEXT-purpose preview.
	_, e = f.pool.Exec(f.ctx, `INSERT INTO model_egress_previews SELECT (jsonb_populate_record(NULL::model_egress_previews,to_jsonb(x)||jsonb_build_object('id',gen_random_uuid(),'scope','SELF_TASK_QUERY','purpose','MODEL_CONTEXT_EGRESS'))).* FROM model_egress_previews x WHERE id=$1`, replacement.PreviewID)
	if e == nil {
		t.Fatal("SQL visual tariff borrowed TEXT scope")
	}
	if _, e = f.pool.Exec(f.ctx, `UPDATE model_budget_accounts SET used_requests=max_requests WHERE owner_id=$1`, f.access.AccountID); e == nil {
		t.Fatal("original attempt monotonic guard relaxed")
	}
	f.gate.enabled = true
	f.exec(`DELETE FROM model_budget_accounts WHERE owner_id=$1`, f.access.AccountID)
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		f.exec(`INSERT INTO model_budget_accounts(owner_id,scope,currency,max_requests,max_input,max_output,max_cost) VALUES($1,$2,'CNY',10,1000000,10000,200000)`, f.access.AccountID, scope)
	}
	if _, e = f.gateway.Preview(f.ctx, f.access, inWithVisualNewOperation(f, in)); !errors.Is(e, av.ErrBudget) {
		t.Fatal("visual raised preexisting cash funding", e)
	}
}
func inWithVisualNewOperation(f *agentVisualNativeFixture, in av.PreviewInput) av.PreviewInput {
	in.OperationID = f.id()
	return in
}
