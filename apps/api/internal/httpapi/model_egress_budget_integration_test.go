package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Real registered HTTP, native sessions and migration062; no SQL grants, fake
// authority adapter or dispatch. Config and price are explicitly synthetic
// trusted maintenance fixtures, never served by a public management endpoint.
type modelEgressHTTPNativeFixture struct {
	*contextBuilderHTTPFixture
	task                   agentworkspace.Task
	root, binding, version string
	price                  modelegressbudget.Price
	access                 agentevent.Access
}

// The configuration route is a real global singleton per task/output mode.
// Do not mutate the database concurrently used by the postgres package. Keep
// default Go package concurrency and isolate this fixture's actual migrations.
func modelEgressHTTPOwnedDatabase(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set disposable PostgreSQL for native model egress HTTP")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("native model egress HTTP requires explicitly disposable database")
	}
	u, e := url.Parse(dsn)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("owned HTTP fixture requires PostgreSQL URI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		cancel()
		t.Fatal("invalid disposable PostgreSQL configuration")
	}
	config.ConnConfig.Database = "postgres"
	admin, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		cancel()
		t.Fatal("cannot connect local database administration")
	}
	var random [12]byte
	if _, e = rand.Read(random[:]); e != nil {
		admin.Close()
		cancel()
		t.Fatal(e)
	}
	name := "birdtie_owned_egress_http_" + hex.EncodeToString(random[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+quoted); e != nil {
		admin.Close()
		cancel()
		t.Fatal("cannot create owned HTTP database", e)
	}
	var owned *pgxpool.Pool
	t.Cleanup(func() {
		if owned != nil {
			owned.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, e := admin.Exec(cleanup, "DROP DATABASE "+quoted); e != nil {
			t.Error("owned HTTP database cleanup failed", e)
		} else {
			t.Log("LOCAL_SYNTHETIC independently owned model HTTP database dropped", name)
		}
		admin.Close()
		cancel()
	})
	u.Path = "/" + name
	u.RawPath = ""
	ownedDSN := u.String()
	owned, e = pgxpool.New(ctx, ownedDSN)
	if e != nil {
		t.Fatal("cannot connect owned HTTP database")
	}
	files, e := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(files)
	count := 0
	for _, path := range files {
		if strings.HasSuffix(path, ".down.sql") {
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = owned.Exec(ctx, string(raw)); e != nil {
			t.Fatal("owned actual migration", filepath.Base(path), e)
		}
		count++
		var seeds []string
		if strings.HasPrefix(filepath.Base(path), "029_") {
			seeds = []string{"001_badminton.sql", "002_functional_mvp.sql"}
		}
		if strings.HasPrefix(filepath.Base(path), "033_") {
			seeds = []string{"003_community_social.sql"}
		}
		for _, seed := range seeds {
			raw, e := os.ReadFile(filepath.Join("..", "..", "dev-seeds", seed))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = owned.Exec(ctx, string(raw)); e != nil {
				t.Fatal("owned explicitly synthetic seed", seed, e)
			}
		}
	}
	if count < 76 {
		t.Fatal("owned HTTP baseline requires actual current schema076 or later")
	}
	t.Logf("LOCAL_SYNTHETIC owned model HTTP database %s actual migrations=%d", name, count)
	t.Setenv("BIRDTIE_DATABASE_URL", ownedDSN)
}

func modelEgressHTTPNative(t *testing.T) *modelEgressHTTPNativeFixture {
	t.Helper()
	modelEgressHTTPOwnedDatabase(t)
	f := &modelEgressHTTPNativeFixture{contextBuilderHTTPFixture: contextBuilderHTTPNative(t)}
	f.access = agentevent.Access{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0])}
	var e error
	if _, e = f.store.EnsureAgentProfile(f.ctx, f.agentIDs[0], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}); e != nil {
		t.Fatal("native current Agent metadata", e)
	}
	f.task, e = f.store.SaveTask(f.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: f.accountIDs[0], ActingUserID: f.accountIDs[0], CityID: f.city, Query: "帮我查询周末羽毛球", Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{{Role: "user", Text: "PRIVATE_CONVERSATION_NOT_EGRESS"}}})
	if e != nil {
		t.Fatal("native actual Task", e)
	}
	f.root = f.uuid(t)
	f.binding = f.uuid(t)
	f.version = "http_egress_" + strings.ReplaceAll(f.accountIDs[0], "-", "")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM model_budget_audit WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_egress_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_tasks WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_roots WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_accounts WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`,
		} {
			if _, e := f.pool.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error("owned native model HTTP cleanup", e)
			}
		}
		for _, q := range []string{`DELETE FROM model_local_price_versions WHERE version=$1`, `DELETE FROM model_configuration_routes WHERE version=$1`, `DELETE FROM model_configuration_versions WHERE version=$1`, `DELETE FROM model_prompt_versions WHERE version=$1`, `DELETE FROM model_configuration_policy_versions WHERE version=$1`} {
			if _, e := f.pool.Exec(ctx, q, f.version); e != nil {
				t.Error("owned native configuration cleanup", e)
			}
		}
	})
	c := modelconfiguration.Configuration{SchemaVersion: modelconfiguration.SchemaVersion, Version: f.version, TaskKind: modelgateway.ActivityQuery, PromptVersion: f.version, InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", OutputMode: modelgateway.Structured, ToolAllowlist: []string{}, PolicyVersion: f.version, CapabilitiesRequired: []string{"text", "structured_output_validatable"}}
	configuration, e := f.store.RegisterModelConfiguration(f.ctx, c, modelconfiguration.PromptDefinition{Version: f.version, Text: "合成配置：仅核查本人查询，不执行操作。"}, modelconfiguration.PolicyVersionReference{Version: f.version, ArtifactSHA256: strings.Repeat("a", 64)})
	if e != nil {
		t.Fatal("native immutable configuration", e)
	}
	route, e := f.store.ActivateModelConfiguration(f.ctx, f.version, 0)
	if e != nil {
		t.Fatal("native version activation", e)
	}
	req := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: f.binding, Agent: agentcognitive.AgentReference{AgentID: f.agentIDs[0], Principal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}, Role: agentruntime.PersonalAgent}, ContextSnapshotRef: f.task.ID, DataPolicyRef: f.root, BudgetRef: f.root, Budget: modelgateway.Budget{MaxOutputTokens: 64}, Messages: []modelgateway.Message{{Role: "user", Content: f.task.Query}}, DeadlineAt: time.Now().UTC().Add(time.Minute)}
	req, _, e = modelconfiguration.PrepareRequest(configuration, req, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.BindModelTaskConfiguration(f.ctx, f.access, f.task.ID, f.version, route.Revision, req); e != nil {
		t.Fatal("native pinned pre-run binding, not Run", e)
	}
	f.price = modelegressbudget.Price{Version: f.version, Destination: modelcapability.Key{Provider: "fake", Model: "local", Version: "v1", WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: modelegressbudget.Retention, Currency: "GBP", InputMicrosPerToken: 2, OutputMicrosPerToken: 3, InputTokenCeiling: 100, OutputTokenCeiling: 128, Evidence: modelegressbudget.LocalPrice, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	if e = f.store.RegisterLocalModelPrice(f.ctx, f.price); e != nil {
		t.Fatal("synthetic local price maintenance", e)
	}
	limits := modelegressbudget.Limits{Requests: 4, InputTokens: 100000, OutputTokens: 100000, CostMicros: 10000000}
	if e = f.store.ConfigureOwnModelBudget(f.ctx, f.access, f.task.ID, f.binding, "GBP", limits, limits); e != nil {
		t.Fatal("native four-scope accounting setup", e)
	}
	if e = f.store.CreateOwnModelBudgetRoot(f.ctx, f.access, modelegressbudget.RootInput{RootTraceID: f.root, TaskID: f.task.ID, BindingID: f.binding, Currency: "GBP", Limits: limits, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)}); e != nil {
		t.Fatal("native bounded budget root", e)
	}
	return f
}
func (f *modelEgressHTTPNativeFixture) uuid(t *testing.T) string {
	t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *modelEgressHTTPNativeFixture) body(t *testing.T, deadline time.Time) string {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"rootTraceId": f.root, "taskId": f.task.ID, "priceVersion": f.version, "maxOutputTokens": 64, "deadlineAt": deadline})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func (f *modelEgressHTTPNativeFixture) preview(t *testing.T) humanModelEgressPreview {
	t.Helper()
	w := f.request(t, f.handler, "POST", modelEgressHTTPBase+"/previews", f.body(t, time.Now().UTC().Add(time.Minute)), f.tokens[0], 200, nil)
	var p struct {
		Data humanModelEgressPreview `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil || p.Data.ID == "" {
		t.Fatal("human exact preview decode", e)
	}
	for _, canary := range []string{"PRIVATE_CONVERSATION_NOT_EGRESS", "authorityToken", "sourceToken", "token_sha256", "CANARY"} {
		if strings.Contains(w.Body.String(), canary) {
			t.Fatal("human preview leaked unapproved data", canary)
		}
	}
	if p.Data.SchemaVersion != modelegressbudget.SchemaVersion || p.Data.Status != "DRAFT" || p.Data.Evidence != modelegressbudget.LocalPrice || p.Data.ModelAccess != "UNAVAILABLE" || p.Data.Request.Messages[len(p.Data.Request.Messages)-1].Content != f.task.Query {
		t.Fatal("preview not real exact local task query")
	}
	return p.Data
}
func modelEgressHTTPApproval(p humanModelEgressPreview) string {
	return fmt.Sprintf(`{"previewId":%q,"requestDigest":%q}`, p.ID, p.RequestDigest)
}
func (f *modelEgressHTTPNativeFixture) budgetPath() string {
	return modelEgressHTTPBase + "/roots/" + f.root + "/tasks/" + f.task.ID + "/budget"
}

func TestModelEgressHTTPNativeExactReviewLifecycleAndNoDispatch(t *testing.T) {
	f := modelEgressHTTPNative(t)
	p := f.preview(t)
	for i := 0; i < 2; i++ {
		f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", modelEgressHTTPApproval(p), f.tokens[0], 200, nil)
	}
	w := f.request(t, f.handler, "GET", f.budgetPath(), "", f.tokens[0], 200, nil)
	var out struct {
		Data []humanModelBudgetView `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data) != 4 {
		t.Fatal("actual four budget scopes not exposed")
	}
	seen := map[string]bool{}
	for _, v := range out.Data {
		seen[v.Scope] = true
		if v.Allocated.Requests != 0 || v.Allocated.InputTokens != 0 || v.Allocated.OutputTokens != 0 || v.Allocated.CostMicros != 0 {
			t.Fatal("human approval reserved or dispatched work")
		}
	}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK"} {
		if !seen[scope] {
			t.Fatal("native budget scope omitted", scope)
		}
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, f.accountIDs[0]).Scan(&count); e != nil || count != 0 {
		t.Fatal("human endpoint created model attempt", e)
	}
	for i := 0; i < 2; i++ {
		f.request(t, f.handler, "DELETE", modelEgressHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
	}
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", modelEgressHTTPApproval(p), f.tokens[0], 403, nil)
	var status string
	var revision int64
	if e := f.pool.QueryRow(f.ctx, `SELECT status,revision FROM model_egress_previews WHERE id=$1`, p.ID).Scan(&status, &revision); e != nil || status != "REVOKED" || revision != 3 {
		t.Fatal("terminal exact approval lifecycle", status, revision, e)
	}
	var approvals, revokes int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE decision='APPROVE'),count(*) FILTER(WHERE decision='REVOKE') FROM model_budget_audit WHERE owner_id=$1`, f.accountIDs[0]).Scan(&approvals, &revokes); e != nil || approvals != 1 || revokes != 1 {
		t.Fatal("retry duplicated approval or revocation", e)
	}
}
func TestModelEgressHTTPNativeIdentityExactDigestAndNoFallback(t *testing.T) {
	f := modelEgressHTTPNative(t)
	p := f.preview(t)
	body := modelEgressHTTPApproval(p)
	for _, token := range []string{"", f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
		f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", body, token, 401, nil)
	}
	for _, token := range f.tokens[1:] {
		f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", body, token, 403, nil)
		f.request(t, f.handler, "DELETE", modelEgressHTTPBase+"/previews/"+p.ID, "", token, 403, nil)
		f.request(t, f.handler, "GET", f.budgetPath(), "", token, 403, nil)
	}
	workspace := ""
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", body, f.tokens[0], 403, &workspace)
	wrong := p
	wrong.RequestDigest = strings.Repeat("b", 64)
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", modelEgressHTTPApproval(wrong), f.tokens[0], 403, nil)
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/previews", strings.Replace(f.body(t, time.Now().Add(time.Minute)), f.version, "missing.v1", 1), f.tokens[0], 403, nil)
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", body, f.tokens[0], 200, nil)
	// Normal native Authenticate idle updates preserve the concrete approval.
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", body, f.tokens[0], 200, nil)
	fresh := f.newSession(f.accountIDs[0], false, false)
	f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", body, fresh, 403, nil)
	// Current owner can withdraw old approval even from a replacement session.
	f.request(t, f.handler, "DELETE", modelEgressHTTPBase+"/previews/"+p.ID, "", fresh, 200, nil)
}
func TestModelEgressHTTPNativeSourceAndAuthorityABACannotRevive(t *testing.T) {
	for _, kind := range []string{"task", "accountABA", "agentABA", "metadataABA"} {
		t.Run(kind, func(t *testing.T) {
			f := modelEgressHTTPNative(t)
			p := f.preview(t)
			switch kind {
			case "task":
				f.exec(`UPDATE agent_tasks SET query='已修改的具体问题',updated_at=clock_timestamp() WHERE id=$1`, f.task.ID)
			case "accountABA":
				f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0])
				f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.accountIDs[0])
			case "agentABA":
				f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
				f.exec(`UPDATE agents SET status='active' WHERE id=$1`, f.agentIDs[0])
			case "metadataABA":
				f.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.agentIDs[0])
				if _, e := f.store.EnsureAgentProfile(f.ctx, f.agentIDs[0], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}); e != nil {
					t.Fatal(e)
				}
			}
			f.request(t, f.handler, "POST", modelEgressHTTPBase+"/approvals", modelEgressHTTPApproval(p), f.tokens[0], 403, nil)
			// Accounting and withdrawal must remain possible after original source
			// versions disappear; neither returns private task/profile contents.
			f.request(t, f.handler, "GET", f.budgetPath(), "", f.tokens[0], 200, nil)
			f.request(t, f.handler, "DELETE", modelEgressHTTPBase+"/previews/"+p.ID, "", f.tokens[0], 200, nil)
		})
	}
}
func TestModelEgressHTTPNativeWaitedLockRechecksPreviewExpiry(t *testing.T) {
	f := modelEgressHTTPNative(t)
	conn, e := f.pool.Acquire(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Release()
	tx, e := conn.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.model-budget.owner:'||$1,0))`, f.accountIDs[0]); e != nil {
		t.Fatal(e)
	}
	body := f.body(t, time.Now().UTC().Add(600*time.Millisecond))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, privateProfileHTTPRequest("POST", modelEgressHTTPBase+"/previews", body, f.tokens[0], "application/json"))
		done <- w
	}()
	// Observe actual PostgreSQL blocking, rather than assume a sleeping request
	// reached the permission boundary. Only the owned database is inspected.
	deadline := time.Now().Add(2 * time.Second)
	waited := false
	for time.Now().Before(deadline) {
		var count int
		if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND query LIKE '%birdtie.model-budget.owner:%'`).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count > 0 {
			waited = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waited {
		t.Fatal("native HTTP never reached actual owner lock")
	}
	time.Sleep(650 * time.Millisecond)
	if e = tx.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		if w.Code != 403 || strings.Contains(w.Body.String(), f.task.Query) {
			t.Fatal("expired lock-waited preview leaked", w.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("lock-waited HTTP did not finish")
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM model_egress_previews WHERE owner_id=$1`, f.accountIDs[0]).Scan(&count); e != nil || count != 0 {
		t.Fatal("expiry denial failed transaction rollback", e)
	}
}
