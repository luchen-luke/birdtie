// Verify the selected TokenHub model through Birdtie's original native budget
// and dispatch ports. The command is limited to the authorized loopback trial.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const tariffHash = "7b018d27ae8dbfa4dceb7282a95e55173a74dee2767ade557e4de3fd1165b5fd"
const priceVersion = "tariff_deepseek0813_20261008_7b018d27"
const probeQuery = "请只回答：连接成功。"

type liveConfig struct {
	OwnerID              string `json:"ownerId"`
	ConfigurationVersion string `json:"configurationVersion"`
	RouteRevision        int64  `json:"routeRevision"`
	SearchPriceVersion   string `json:"searchPriceVersion"`
	ModelPriceVersion    string `json:"modelPriceVersion"`
}

type receipt struct {
	AtUTC          time.Time          `json:"atUTC"`
	Action         string             `json:"action"`
	Stage          string             `json:"stage"`
	Status         string             `json:"status"`
	Model          string             `json:"model"`
	Endpoint       string             `json:"endpoint"`
	HTTPStatus     int                `json:"httpStatus"`
	BusinessCode   string             `json:"businessCode"`
	RequestID      string             `json:"request_id"`
	TaskID         string             `json:"taskId,omitempty"`
	OperationID    string             `json:"operationId,omitempty"`
	RootID         string             `json:"rootTraceId,omitempty"`
	Diagnostic     string             `json:"diagnostic,omitempty"`
	UpperMicros    int64              `json:"upperMicrosCNY"`
	Usage          modelgateway.Usage `json:"usage"`
	PriceVersion   string             `json:"priceVersion"`
	CashStatus     string             `json:"cashStatus"`
	ProviderCalls  int                `json:"providerCalls"`
	RuntimeUpdated bool               `json:"runtimeUpdated"`
	BoundBasis     string             `json:"boundBasis"`
	ParityStatus   string             `json:"hostedTokenizerParity"`
}

type options struct {
	database, dir, tariffs, action, output string
}

func validateOptions(o options, rawDSN string) (*url.URL, error) {
	u, e := url.Parse(rawDSN)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || !strings.HasPrefix(o.database, "birdtie_ui_live_20261008_") || strings.TrimPrefix(u.Path, "/") != o.database || !filepath.IsAbs(o.dir) || !filepath.IsAbs(o.tariffs) || !filepath.IsAbs(o.output) || (o.action != "register" && o.action != "probe") {
		return nil, modelgateway.ErrInvalid
	}
	ip := net.ParseIP(u.Hostname())
	if u.User == nil || !(u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil, modelgateway.ErrInvalid
	}
	query, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return nil, modelgateway.ErrInvalid
	}
	for key, values := range query {
		if key != "sslmode" || len(values) != 1 || values[0] != "disable" {
			return nil, modelgateway.ErrInvalid
		}
	}
	return u, nil
}

func validatedPoolConfig(u *url.URL, database string) (*pgxpool.Config, error) {
	c, e := pgxpool.ParseConfig(u.String())
	if e != nil || c.ConnConfig.Database != database || !localHost(c.ConnConfig.Host) {
		return nil, modelgateway.ErrInvalid
	}
	for _, fallback := range c.ConnConfig.Fallbacks {
		if !localHost(fallback.Host) || fallback.Port != c.ConnConfig.Port {
			return nil, modelgateway.ErrInvalid
		}
	}
	return c, nil
}

func localHost(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

// Receipts are exclusive: an interrupted outcome is never retried automatically.
func saveReceipt(path string, v receipt) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(v)
}

func loadConfig(path string) (liveConfig, error) {
	var v liveConfig
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return v, modelgateway.ErrInvalid
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return v, modelgateway.ErrInvalid
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&v) != nil || d.Decode(&extra) != io.EOF || v.OwnerID == "" || v.ConfigurationVersion != "now_public_sources_20261008_v1" || v.RouteRevision < 1 || v.SearchPriceVersion == "" || v.ModelPriceVersion == "" {
		return liveConfig{}, modelgateway.ErrInvalid
	}
	return v, nil
}

func reviewedPrice(dir string, now time.Time) (modelegressbudget.LivePrice, error) {
	var m struct {
		Artifacts []struct {
			Name, SourceURL, SHA256, ObservedAtUTC string
		}
	}
	raw, e := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if e != nil || len(raw) > 16384 || json.Unmarshal(raw, &m) != nil {
		return modelegressbudget.LivePrice{}, modelgateway.ErrInvalid
	}
	for _, a := range m.Artifacts {
		if a.Name != "models" {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(dir, "models.html"))
		if e != nil || len(raw) > 4*1024*1024 {
			return modelegressbudget.LivePrice{}, modelgateway.ErrInvalid
		}
		h := sha256.Sum256(raw)
		observed, e := time.Parse(time.RFC3339Nano, a.ObservedAtUTC)
		if e != nil || a.SourceURL != modelegressbudget.LiveModelPriceURL || a.SHA256 != tariffHash || hex.EncodeToString(h[:]) != tariffHash {
			return modelegressbudget.LivePrice{}, modelgateway.ErrInvalid
		}
		observed = observed.UTC().Truncate(time.Microsecond)
		expires := observed.Add(23 * time.Hour)
		p := modelegressbudget.LivePrice{Kind: modelegressbudget.LiveToken, RequestCeiling: 1,
			Base: modelegressbudget.Price{Version: priceVersion,
				Destination: modelcapability.Key{Provider: "tencent_tokenhub", Model: "deepseek-v4-pro-0813", Version: "deepseek-v4-pro-0813", WireContract: modelgateway.TencentLiveWireContract},
				Region:      modelcapability.APAC, Retention: modelegressbudget.LiveRetentionUnknown, Currency: "CNY", InputMicrosPerToken: 9, OutputMicrosPerToken: 27,
				InputTokenCeiling: modelgateway.TencentDeepSeekMaxInputTokens, OutputTokenCeiling: 768, Evidence: modelegressbudget.LiveTariffEvidence, ExpiresAt: expires},
			Snapshot: modelegressbudget.LiveSnapshot{SourceURL: a.SourceURL, ArtifactSHA256: tariffHash, ObservedAt: observed, ExpiresAt: expires, DocumentUpdatedAt: time.Date(2026, 9, 24, 12, 8, 15, 0, time.UTC)}}
		return p, modelegressbudget.ValidateLivePrice(p, now)
	}
	return modelegressbudget.LivePrice{}, modelgateway.ErrInvalid
}

type featureGate struct{ controller *agentfeature.Controller }

func (g featureGate) InferenceEnabled(ctx context.Context) bool {
	if ctx == nil || ctx.Err() != nil || g.controller == nil {
		return false
	}
	ticket, e := g.controller.Capture(agentfeature.Enrichment)
	return e == nil && g.controller.Current(ticket)
}

func execute(o options, r *receipt) error {
	r.Stage = "local-scope"
	u, e := validateOptions(o, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		return e
	}
	if _, e = os.Lstat(o.output); !os.IsNotExist(e) {
		return modelgateway.ErrInvalid
	}
	r.Stage = "environment-credential"
	c, e := modelgateway.LoadTencentTokenHubConfigFromEnvironment(filepath.Join(o.dir, ".env.tencent.local.json"), os.LookupEnv)
	if e != nil {
		return e
	}
	adapter, e := modelgateway.NewTencentTokenHubAdapter(c, nil)
	if e != nil || adapter.Descriptor().ModelID != r.Model {
		return modelgateway.ErrInvalid
	}
	cfgPath := filepath.Join(o.dir, ".env.now.live.local.json")
	config, e := loadConfig(cfgPath)
	if e != nil {
		return e
	}
	r.Stage = "reviewed-tariff"
	price, e := reviewedPrice(o.tariffs, time.Now())
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	poolConfig, e := validatedPoolConfig(u, o.database)
	if e != nil {
		return e
	}
	pool, e := pgxpool.NewWithConfig(ctx, poolConfig)
	if e != nil {
		return modelgateway.ErrUnavailable
	}
	defer pool.Close()
	store := postgres.New(pool, true)
	r.Stage = "native-price-registration"
	if e = store.RegisterLivePrice(ctx, price); e != nil {
		return e
	}
	if o.action == "register" {
		// Select the new immutable price; preserve the account, original route
		// revision, search price and all old configuration and budget records.
		before, e := os.ReadFile(cfgPath)
		if e != nil {
			return modelgateway.ErrInvalid
		}
		old, e := loadConfig(cfgPath)
		if e != nil || old != config {
			return modelgateway.ErrInvalid
		}
		config.ModelPriceVersion = priceVersion
		out, e := json.MarshalIndent(config, "", "  ")
		if e != nil {
			return modelgateway.ErrInvalid
		}
		current, e := os.ReadFile(cfgPath)
		if e != nil || string(current) != string(before) {
			return modelgateway.ErrInvalid
		}
		temporary := cfgPath + ".model-price-update.tmp"
		file, e := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return modelgateway.ErrInvalid
		}
		_, writeErr := file.Write(append(out, '\n'))
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return modelgateway.ErrInvalid
		}
		current, e = os.ReadFile(cfgPath)
		if e != nil || string(current) != string(before) {
			return modelgateway.ErrInvalid
		}
		if e = os.Rename(temporary, cfgPath); e != nil {
			return modelgateway.ErrInvalid
		}
		r.Stage, r.Status = "registered", "PASS"
		return nil
	}
	if config.ModelPriceVersion != priceVersion {
		return modelgateway.ErrInvalid
	}
	r.Stage = "original-development-session"
	phoneDigest := devauth.PhoneDigest("+447700900108")
	if e = store.RequestDevPhoneChallenge(ctx, phoneDigest); e != nil {
		return e
	}
	_, sessionDigest, e := identity.NewToken()
	if e != nil {
		return modelgateway.ErrUnavailable
	}
	if e = store.VerifyDevPhoneChallenge(ctx, phoneDigest, true, sessionDigest); e != nil {
		return e
	}
	actor, e := store.Authenticate(ctx, sessionDigest)
	if e != nil || actor.ID != config.OwnerID || actor.AccountType != "person" {
		return modelgateway.ErrUnavailable
	}
	r.Stage = "new-own-probe-task"
	var city, contextType, contextID, agentID, binding, root, operation string
	// Reuse only the existing owner's context selectors, not its conversation.
	if e = pool.QueryRow(ctx, `SELECT COALESCE(city_context_id,''),context_type,COALESCE(context_id::text,'') FROM agent_tasks WHERE owner_account_id=$1 AND principal_type='person' ORDER BY created_at DESC LIMIT 1`, actor.ID).Scan(&city, &contextType, &contextID); e != nil {
		return modelgateway.ErrUnavailable
	}
	if e = pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active'`, actor.ID).Scan(&agentID); e != nil {
		return modelgateway.ErrUnavailable
	}
	if e = pool.QueryRow(ctx, `SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid()`).Scan(&binding, &root, &operation); e != nil {
		return modelgateway.ErrUnavailable
	}
	task, e := store.SaveTask(ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: actor.ID, ActingUserID: actor.ID, CityID: city, ContextType: contextType, ContextID: contextID,
		Query: probeQuery, Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": probeQuery}, Conversation: []agentworkspace.Message{{Role: "user", Text: probeQuery}}})
	if e != nil {
		return e
	}
	r.TaskID, r.OperationID, r.RootID = task.ID, operation, root
	a := agentevent.Access{SessionDigest: sessionDigest}
	r.Stage = "original-model-binding"
	resolved, e := store.ReadModelConfiguration(ctx, config.ConfigurationVersion)
	if e != nil {
		return e
	}
	deadline := time.Now().UTC().Add(28 * time.Second).Truncate(time.Microsecond)
	principal := actorref.PrincipalRef{Type: actorref.Person, ID: actor.ID}
	request := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: binding, Agent: agentcognitive.AgentReference{AgentID: agentID, Principal: principal, Role: agentruntime.ForType(principal.Type).Role},
		ContextSnapshotRef: task.ID, DataPolicyRef: root, BudgetRef: root, Budget: modelgateway.Budget{MaxOutputTokens: 32}, Messages: []modelgateway.Message{{Role: "user", Content: probeQuery}}, DeadlineAt: deadline}
	request, _, e = modelconfiguration.PrepareRequest(resolved, request, time.Now())
	if e != nil {
		return e
	}
	if _, e = store.BindModelTaskConfiguration(ctx, a, task.ID, config.ConfigurationVersion, config.RouteRevision, request); e != nil {
		return e
	}
	r.Stage = "original-budget"
	account := modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: 10_000_000}
	if e = store.ConfigureOwnModelBudget(ctx, a, task.ID, binding, "CNY", account, account); e != nil {
		return e
	}
	limits := modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680}
	if e = store.CreateOwnModelBudgetRoot(ctx, a, modelegressbudget.RootInput{RootTraceID: root, TaskID: task.ID, BindingID: binding, Currency: "CNY", Limits: limits, ExpiresAt: deadline}); e != nil {
		return e
	}
	flags, e := agentfeature.LoadConfig(os.LookupEnv)
	if e != nil {
		return e
	}
	controller, e := agentfeature.NewController(flags)
	if e != nil {
		return e
	}
	gate := featureGate{controller: controller}
	r.Stage = "original-preview-approval-reservation"
	preview, e := store.PreviewOwnLiveEgress(ctx, a, modelegressbudget.PreviewInput{RootTraceID: root, TaskID: task.ID, PriceVersion: priceVersion, MaxOutputTokens: 32, DeadlineAt: deadline}, adapter)
	if e != nil {
		return e
	}
	r.UpperMicros = preview.Upper.Amount.CostMicros
	if r.UpperMicros > 200_000 || r.UpperMicros != 148_320 {
		return modelegressbudget.ErrBudget
	}
	if e = store.ApproveOwnLiveEgress(ctx, a, preview.ID, preview.RequestDigest, adapter); e != nil {
		return e
	}
	if _, e = store.ReserveOwnLiveAttempt(ctx, a, modelegressbudget.ReserveInput{OperationID: operation, PreviewID: preview.ID, RootTraceID: root, TaskID: task.ID}, controller, adapter); e != nil {
		return e
	}
	r.Stage = "original-native-dispatch"
	port, request, e := store.NewOwnLiveModelDispatch(ctx, a, operation, controller, adapter, gate)
	if e != nil {
		return e
	}
	gateway, e := modelgateway.NewNativeLiveGateway(gate, adapter, port)
	if e != nil {
		return e
	}
	r.ProviderCalls = -1 // Until a provider response, transport outcome is unknown.
	result, e := gateway.Complete(ctx, request)
	r.Usage = result.Usage
	if e != nil {
		if summary, ok := modelgateway.SafeModelFailureSummary(e); ok {
			r.Diagnostic = summary
			// Read only the already sanitized, fixed diagnostic fields.
			for _, field := range strings.Fields(summary) {
				if strings.HasPrefix(field, "status=") {
					r.HTTPStatus, _ = strconv.Atoi(strings.TrimPrefix(field, "status="))
				}
				if strings.HasPrefix(field, "business=") {
					r.BusinessCode = strings.TrimPrefix(field, "business=")
				}
			}
			if r.HTTPStatus != 0 {
				r.ProviderCalls = 1
			}
		}
		return e
	}
	if result.Mode != modelgateway.Live || result.Status != modelgateway.Completed || result.ProviderModelVersion != r.Model+"@"+r.Model || result.Text == "" || result.ProviderRequestID == "" {
		return modelgateway.ErrInvalid
	}
	// Adapter completion requires HTTP 200. The ID is normalized provider data,
	// not proof of hosted weights or actual cash; credentials are never emitted.
	if key, present := os.LookupEnv("BIRDTIE_TENCENT_TOKENHUB_API_KEY"); !present || strings.Contains(result.ProviderRequestID, key) {
		return modelgateway.ErrInvalid
	}
	r.HTTPStatus, r.BusinessCode, r.RequestID, r.ProviderCalls = 200, "NONE", result.ProviderRequestID, 1
	r.Stage, r.Status = "native-model-completed", "PASS"
	return nil
}

func main() {
	o := options{}
	flag.StringVar(&o.database, "expected-database", "", "Exact authorized isolated database")
	flag.StringVar(&o.dir, "config-dir", "", "Ignored local server configuration directory")
	flag.StringVar(&o.tariffs, "tariff-dir", "", "Reviewed public tariff directory")
	flag.StringVar(&o.action, "action", "", "register or probe; no automatic retry")
	flag.StringVar(&o.output, "receipt", "", "New exclusive sanitized receipt")
	flag.Parse()
	r := receipt{AtUTC: time.Now().UTC(), Action: o.action, Status: "FAILED", Model: "deepseek-v4-pro-0813", Endpoint: modelgateway.TencentTokenHubBaseURL + "/chat/completions", BusinessCode: "NOT_AVAILABLE", RequestID: "NOT_AVAILABLE", CashStatus: "UNKNOWN", PriceVersion: priceVersion,
		BoundBasis: "SELECTED_OFFICIAL_MODEL_BYTE_BPE_UPPER_BOUND", ParityStatus: "INFERRED_FROM_SELECTED_MODEL_NOT_MEASURED"}
	// Persist a separate exclusive claim before any native mutation or transport.
	// A crash leaves it in place, preventing the same invocation from replaying.
	if _, e := validateOptions(o, os.Getenv("BIRDTIE_DATABASE_URL")); e != nil {
		fmt.Fprintln(os.Stderr, "验证范围无效；未调用供应商。")
		os.Exit(1)
	}
	claim := r
	claim.Stage, claim.Status = "exclusive-invocation-claim", "PENDING"
	if saveReceipt(o.output+".claim.json", claim) != nil {
		fmt.Fprintln(os.Stderr, "该验证已领取或收据无法创建；不要自动重发。")
		os.Exit(1)
	}
	e := execute(o, &r)
	if e != nil && r.Diagnostic == "" {
		for _, classified := range []struct {
			cause error
			name  string
		}{{modelegressbudget.ErrBudget, "BUDGET"}, {modelegressbudget.ErrDenied, "DENIED"}, {modelegressbudget.ErrConflict, "CONFLICT"}} {
			if errors.Is(e, classified.cause) {
				r.Diagnostic = classified.name
			}
		}
		if r.Diagnostic == "" {
			r.Diagnostic = "LOCAL_OR_NATIVE_FAILURE"
		}
	}
	if filepath.IsAbs(o.output) {
		if saveReceipt(o.output, r) != nil {
			fmt.Fprintln(os.Stderr, "脱敏验证收据未保存；不要自动重发。")
			os.Exit(1)
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if e != nil {
		os.Exit(1)
	}
}
