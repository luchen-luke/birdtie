package postgres

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5/pgconn"
)

// Actual original PostgreSQL fixtures and ledgers. Provider transport, names,
// tariff artifacts and credentials are explicitly UNIT synthetic. The worker
// compiles these tests without DB; root runs them only on its isolated DB.
func resolvedLiveNativeFixture(t *testing.T) (*liveNativeFixture, string) {
	t.Helper()
	f := newLiveNativeFixtureWithAccount(t, "CNY", modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: 10_000_000}, false)
	b := f.configuration.native.private.base
	city := "unit-resolved-" + liveNativeID(t, f)
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'UNIT Aberdeen','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:resolved-native','合成维护者',$2)`, city, b.other.ID)
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, city)
	// Delete only this fixture's records, in dependency order. Inherited cleanup
	// remains in place and safely observes these rows already removed.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{`DELETE FROM model_request_runs WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_audit WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_reservations WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_egress_previews WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_tasks WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_roots WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM model_budget_accounts WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, sql, b.accounts); e != nil {
				t.Error("owned resolved fixture cleanup", e)
			}
		}
		for _, sql := range []string{`DELETE FROM contexts WHERE city_id=$1`, `DELETE FROM city_contexts WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := b.pool.Exec(ctx, sql, city); e != nil {
				t.Error("owned public City fixture cleanup", e)
			}
		}
	})
	query := "帮我找周末的羽毛球活动"
	task, e := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: city, Query: query, Intent: agentworkspace.FindActivity, Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": query, "targetIntent": agentworkspace.FindActivity, "category": "badminton", "timePreference": "weekend", "resultIDs": "PRIVATE_RESULT_CANARY", "privateProfile": "PRIVATE_PROFILE_CANARY", "mapWest": "-2.204871233"}, Conversation: []agentworkspace.Message{{Role: "user", Text: "PRIVATE_HISTORY_CANARY"}, {Role: "assistant", Text: "PRIVATE_ASSISTANT_CANARY"}, {Role: "user", Text: query}}})
	if e != nil {
		t.Fatal("native current Task in own public City", e)
	}
	f.configuration.native.task = task
	return f, city
}

func resolvedLiveNativeRun(t *testing.T, f *liveNativeFixture) *nativeLiveSourceAnswerRun {
	t.Helper()
	c, b := f.configuration, f.configuration.native.private.base
	f.binding = liveNativeID(t, f)
	request := c.request(t, len(c.configs)-1, f.binding)
	if _, e := b.store.BindModelTaskConfiguration(b.ctx, c.native.access, c.native.task.ID, c.route.Version, c.route.Revision, request); e != nil {
		t.Fatal("new current native binding", e)
	}
	limit := modelegressbudget.Limits{Requests: 1000, InputTokens: 1_000_000_000, OutputTokens: 1_000_000_000, CostMicros: 10_000_000}
	if e := b.store.ConfigureOwnModelBudget(b.ctx, c.native.access, c.native.task.ID, f.binding, "CNY", limit, limit); e != nil {
		t.Fatal("original owner accounts reused", e)
	}
	f.root = liveNativeID(t, f)
	deadline := time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)
	if e := b.store.CreateOwnModelBudgetRoot(b.ctx, c.native.access, modelegressbudget.RootInput{RootTraceID: f.root, TaskID: c.native.task.ID, BindingID: f.binding, Currency: "CNY", Limits: modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680}, ExpiresAt: deadline}); e != nil {
		t.Fatal("same-root two API boundary", e)
	}
	call, e := b.store.PreviewOwnResolvedLiveEgress(b.ctx, c.native.access, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: c.native.task.ID, PriceVersion: f.call.Base.Version, DeadlineAt: deadline}, nil)
	if e != nil || call.Scope != modelegressbudget.LiveResolvedSearchScope || !call.Prepared().resolved.valid {
		t.Fatal("native resolved public preview", e)
	}
	if e = b.store.ApproveOwnLiveEgress(b.ctx, c.native.access, call.ID, call.RequestDigest, nil); e != nil {
		t.Fatal("exact new scope approval", e)
	}
	ticket, e := f.controller.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	h, _, e := b.store.CreateOwnLiveSourceAnswerRun(b.ctx, c.native.access, LiveSourceAnswerRunInput{RunID: liveNativeID(t, f), Search: f.input(t, call), ModelOperationID: liveNativeID(t, f), ModelPriceVersion: f.token.Base.Version, MaxOutputTokens: 768}, f.controller, ticket, f.projector, liveNativeGate{})
	if e != nil {
		t.Fatal("original typed Run for resolved scope", e)
	}
	return h
}

func resolvedNativeCompleteModel(t *testing.T, f *liveNativeFixture, h *nativeLiveSourceAnswerRun, preview LiveEgressPreview) {
	t.Helper()
	b := f.configuration.native.private.base
	calls := 0
	adapter := liveNativeModelAdapter(t, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		assertLiveNativePhase(t, f, h.modelOperation, "IN_FLIGHT", "LIVE_IN_FLIGHT")
		raw, e := io.ReadAll(req.Body)
		if e != nil || !bytes.Equal(raw, preview.Prepared().ModelWire().ExactWire()) {
			t.Fatal("actual MODEL exact wire", e)
		}
		var wire struct {
			Messages []modelgateway.Message `json:"messages"`
		}
		if e = json.Unmarshal(raw, &wire); e != nil || len(wire.Messages) != 2 {
			t.Fatal("closed MODEL messages", e)
		}
		var payload struct {
			Schema       string `json:"schema"`
			CurrentQuery string `json:"currentQuery"`
			Public       struct {
				City    modelegressbudget.LiveSelectedCity   `json:"selectedCity"`
				Slots   modelegressbudget.LiveResolvedSlots  `json:"resolvedSlots"`
				Sources []modelegressbudget.LivePublicSource `json:"sources"`
			} `json:"publicSearchContext"`
		}
		if e = json.Unmarshal([]byte(wire.Messages[1].Content), &payload); e != nil || payload.Schema != modelegressbudget.LiveResolvedPublicSearchSchema || payload.CurrentQuery != h.original.query || payload.Public.City != h.original.resolved.context.SelectedCity || payload.Public.Slots != h.original.resolved.context.ResolvedSlots || len(payload.Public.Sources) != 1 {
			t.Fatal("MODEL differs from same sealed current search context", e)
		}
		for _, private := range []string{"PRIVATE_HISTORY_CANARY", "PRIVATE_ASSISTANT_CANARY", "PRIVATE_RESULT_CANARY", "PRIVATE_PROFILE_CANARY", "-2.204871233", b.person.ID, h.search.TaskID, "cityRow", "stateRow"} {
			if bytes.Contains(raw, []byte(private)) || bytes.Contains(h.original.SearchWire(), []byte(private)) {
				t.Fatal("private native field entered supplier bytes")
			}
		}
		return liveNativeResponse(`{"id":"unit-resolved-model","model":"hy3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"UNIT resolved public source answer"}}],"usage":{"prompt_tokens":41,"completion_tokens":12,"total_tokens":53}}`, 200), nil
	}))
	if result, e := h.CompleteSourceModel(b.ctx, adapter); e != nil || calls != 1 || result.Text != "UNIT resolved public source answer" {
		t.Fatal("one actual native MODEL fake transport completion", e, calls)
	}
}

func TestModelLiveResolvedRealSQLTwoTurnsSameTaskIntegration(t *testing.T) {
	f, _ := resolvedLiveNativeFixture(t)
	b := f.configuration.native.private.base
	var originalTaskID string
	for turn := 1; turn <= 2; turn++ {
		h := resolvedLiveNativeRun(t, f)
		if turn == 1 {
			originalTaskID = h.search.TaskID
		} else if h.search.TaskID != originalTaskID || h.original.query != "近一点的呢？" || h.original.resolved.context.ResolvedSlots.Category != "badminton" || h.original.resolved.context.ResolvedSlots.TimePreference != "weekend" || h.original.resolved.context.ResolvedSlots.DistancePreference != "closer" || !strings.Contains(h.original.SearchQuery(), "UNIT Aberdeen GB activities badminton weekend closer to city centre") {
			t.Fatal("second turn lost current resolved city/category/time/distance")
		}
		_, preview := liveSourceRunBindFixture(t, f, h)
		if preview.Scope != modelegressbudget.LiveResolvedSourceScope {
			t.Fatal("new MODEL scope lost")
		}
		resolvedNativeCompleteModel(t, f, h, preview)
		reply, e := h.FinalizeSourceReply(b.ctx)
		if e != nil || reply.Task().Status != agentworkspace.TaskCompleted {
			t.Fatal("native sourced reply completion", e)
		}
		views, e := b.store.ReadOwnModelBudget(b.ctx, f.configuration.native.access, f.root, h.search.TaskID)
		if e != nil || len(views) != 4 {
			t.Fatal("original budget scopes", e)
		}
		for _, view := range views {
			want := int64(279680)
			if view.Scope == "TENANT_PERSON" || view.Scope == "SUBJECT_PERSON" {
				want *= int64(turn)
			}
			if view.Allocated.CostMicros != want || (view.Scope == "ROOT" && view.Allocated.Requests != 2) {
				t.Fatal("follow-up reset owner hold or exceeded same-root cap", view.Scope)
			}
		}
		if turn == 1 {
			task := reply.Task()
			intent := agentworkspace.ParseMVPIntent("近一点的呢？", &task)
			if !intent.Supported || intent.Operation != agentworkspace.RefineResults {
				t.Fatal("original parser failed the native follow-up")
			}
			task.Intent, task.Status = intent.Operation, agentworkspace.TaskActive
			task.Filters["currentQuery"], task.Filters["targetIntent"] = "近一点的呢？", intent.Target
			task.Filters["category"], task.Filters["timePreference"], task.Filters["distancePreference"] = intent.Category, intent.TimePreference, intent.DistancePreference
			task.Conversation = append(task.Conversation, agentworkspace.Message{Role: "user", Text: "近一点的呢？"})
			f.configuration.native.task, e = b.store.UpdateTask(b.ctx, task)
			if e != nil {
				t.Fatal("same native Task incremental turn", e)
			}
		}
	}
}

func TestModelLiveResolvedNativeCityAndTaskBeforeSendIntegration(t *testing.T) {
	for _, mutation := range []string{"city_name", "pause_restore", "expiry", "context_generation", "task_filter", "task_target", "session", "profile", "gate"} {
		t.Run(mutation, func(t *testing.T) {
			f, city := resolvedLiveNativeFixture(t)
			h := resolvedLiveNativeRun(t, f)
			b := f.configuration.native.private.base
			switch mutation {
			case "city_name":
				b.exec(`UPDATE cities SET name='UNIT changed City',updated_at=clock_timestamp() WHERE id=$1`, city)
			case "pause_restore":
				b.exec(`UPDATE city_contexts SET status='paused',updated_at=clock_timestamp() WHERE city_id=$1`, city)
				b.exec(`UPDATE city_contexts SET status='active',updated_at=clock_timestamp() WHERE city_id=$1`, city)
			case "expiry":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second',updated_at=clock_timestamp() WHERE id=$1`, city)
			case "context_generation":
				b.exec(`UPDATE contexts SET created_at=created_at WHERE city_id=$1 AND context_type='CITY'`, city)
			case "task_filter":
				b.exec(`UPDATE agent_tasks SET filters=filters||'{"category":"basketball"}'::jsonb,updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
			case "task_target":
				b.exec(`UPDATE agent_tasks SET filters=filters||'{"targetIntent":"FIND_PLACE"}'::jsonb,updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
			case "session":
				if e := b.store.RevokeSession(b.ctx, f.configuration.native.access.SessionDigest); e != nil {
					t.Fatal(e)
				}
			case "profile":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, h.original.request.Agent.AgentID)
			case "gate":
				if e := f.controller.Disable(agentfeature.Enrichment); e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			if _, e := h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, &calls, nil)); e == nil || calls != 0 {
				t.Fatal("stale current native proof sent query", e, calls)
			}
			var reservations int
			if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1`, b.person.ID).Scan(&reservations); e != nil || reservations != 0 {
				t.Fatal("denied pre-send request consumed a reservation", e)
			}
		})
	}
}

func TestModelLiveResolvedCityWireReadAndFinalReplyIntegration(t *testing.T) {
	for _, stage := range []string{"source_body", "source_http1_body", "after_model", "after_reply"} {
		t.Run(stage, func(t *testing.T) {
			f, city := resolvedLiveNativeFixture(t)
			h := resolvedLiveNativeRun(t, f)
			b := f.configuration.native.private.base
			if stage == "source_body" || stage == "source_http1_body" {
				calls, sent := 0, 0
				config, e := agenttool.ParseTencentWSAConfig([]byte(`{"apiKey":"unit-test-placeholder","keyName":"unit-resolved-native"}`))
				if e != nil {
					t.Fatal(e)
				}
				var transport http.RoundTripper = liveNativeTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					b.exec(`UPDATE city_contexts SET status='paused',updated_at=clock_timestamp() WHERE city_id=$1`, city)
					raw, readErr := io.ReadAll(req.Body)
					sent += len(raw)
					if readErr == nil {
						t.Fatal("City revoked before wire read was accepted")
					}
					return nil, readErr
				})
				var observed chan int
				if stage == "source_http1_body" {
					// Exercise net/http's HTTP/1 writer after connection setup, not
					// only a direct mock Body.Read. net.Pipe has no external network.
					observed = make(chan int, 1)
					http1 := &http.Transport{ForceAttemptHTTP2: false, DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						calls++
						assertLiveNativePhase(t, f, h.search.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
						b.exec(`UPDATE city_contexts SET status='paused',updated_at=clock_timestamp() WHERE city_id=$1`, city)
						client, server := net.Pipe()
						go func() {
							defer server.Close()
							request, readErr := http.ReadRequest(bufio.NewReader(server))
							if readErr != nil {
								observed <- 0
								return
							}
							body, _ := io.ReadAll(request.Body)
							request.Body.Close()
							observed <- len(body)
						}()
						return client, nil
					}}
					defer http1.CloseIdleConnections()
					transport = http1
				}
				adapter, e := agenttool.NewTencentWSAAdapter(config, transport)
				if e != nil {
					t.Fatal(e)
				}
				_, e = h.ExecuteSourceSearch(b.ctx, adapter)
				if observed != nil {
					select {
					case sent = <-observed:
					case <-time.After(5 * time.Second):
						t.Fatal("actual HTTP/1 body observation did not finish")
					}
				}
				if e == nil || calls != 1 || sent != 0 {
					t.Fatal("revoked City released query bytes/retried", e, calls, sent)
				}
				assertLiveNativePhase(t, f, h.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
				assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
				return
			}
			_, preview := liveSourceRunBindFixture(t, f, h)
			resolvedNativeCompleteModel(t, f, h, preview)
			if stage == "after_model" {
				b.exec(`UPDATE city_contexts SET status='paused',updated_at=clock_timestamp() WHERE city_id=$1`, city)
				if _, e := h.FinalizeSourceReply(b.ctx); !errors.Is(e, modelegressbudget.ErrDenied) {
					t.Fatal("revoked City finalized text", e)
				}
			} else {
				reply, e := h.FinalizeSourceReply(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE city_contexts SET status='paused',updated_at=clock_timestamp() WHERE city_id=$1`, city)
				b.exec(`UPDATE city_contexts SET status='active',updated_at=clock_timestamp() WHERE city_id=$1`, city)
				if e = reply.Revalidate(b.ctx); !errors.Is(e, modelegressbudget.ErrDenied) {
					t.Fatal("restored City revived final reply", e)
				}
			}
			assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
		})
	}
}

func TestModelLiveResolvedRejectsMixedLegacySourceScopeIntegration(t *testing.T) {
	for _, branch := range []string{"resolved_to_legacy", "legacy_to_resolved"} {
		t.Run(branch, func(t *testing.T) {
			var f *liveNativeFixture
			var h *nativeLiveSourceAnswerRun
			wrongScope := modelegressbudget.LivePublicSearchScope
			if branch == "resolved_to_legacy" {
				f, _ = resolvedLiveNativeFixture(t)
				h = resolvedLiveNativeRun(t, f)
			} else {
				f, h, _ = liveSourceRunFixture(t)
				wrongScope = modelegressbudget.LiveResolvedSourceScope
			}
			_, preview := liveSourceRunBindFixture(t, f, h)
			b := f.configuration.native.private.base
			var before, after int
			if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_egress_previews WHERE owner_id=$1`, b.person.ID).Scan(&before); e != nil {
				t.Fatal(e)
			}
			// Insert an exact data copy with the opposite MODEL scope. Updating
			// an immutable existing preview would exercise a different guard.
			_, e := b.pool.Exec(b.ctx, `INSERT INTO model_egress_previews(root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,scope,purpose,billing_kind,current_query_digest,egress_payload_digest,source_batch,source_evidence_digest)
 SELECT root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,$2,purpose,billing_kind,current_query_digest,egress_payload_digest,source_batch,source_evidence_digest FROM model_egress_previews WHERE id=$1`, preview.ID, wrongScope)
			var failure *pgconn.PgError
			if !errors.As(e, &failure) || failure.Code != "P0001" || failure.Message != "source export reference mismatch" {
				t.Fatal("mixed old/new evidence pairing was not rejected by reference guard", e)
			}
			if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_egress_previews WHERE owner_id=$1`, b.person.ID).Scan(&after); e != nil || after != before {
				t.Fatal("rejected scope pair wrote a new preview", e)
			}
		})
	}
}
