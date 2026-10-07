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
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/jackc/pgx/v5/pgconn"
)

// Actual original SQL fixtures; synthetic tariff hashes/HTTP responses only.
// No external network/key, downloaded tariff or device proof is represented.
func liveSourceRunFixture(t *testing.T) (*liveNativeFixture, *nativeLiveSourceAnswerRun, modelrequestrun.Control) {
	t.Helper()
	f := newLiveNativeFixture(t)
	b := f.configuration.native.private.base
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='model_request_runs' AND column_name='run_kind')`).Scan(&installed); e != nil || !installed {
		t.Fatal("requires isolated original migrations 108/109", e)
	}
	call := f.preview(t, modelegressbudget.LiveCall, true)
	in := LiveSourceAnswerRunInput{RunID: liveNativeID(t, f), Search: f.input(t, call), ModelOperationID: liveNativeID(t, f), ModelPriceVersion: f.token.Base.Version, MaxOutputTokens: 768}
	ticket, e := f.controller.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	h, c, e := b.store.CreateOwnLiveSourceAnswerRun(b.ctx, f.configuration.native.access, in, f.controller, ticket, f.projector, liveNativeGate{})
	if e != nil {
		t.Fatal("create original typed live pipeline", e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := b.pool.Exec(ctx, `DELETE FROM model_request_runs WHERE id=$1 AND owner_id=$2`, in.RunID, b.person.ID); e != nil {
			t.Errorf("owned original typed Run cleanup: %v", e)
		}
	})
	return f, h, c
}

func liveSourceRunWSA(t *testing.T, f *liveNativeFixture, h *nativeLiveSourceAnswerRun, calls *int, hook func()) *agenttool.TencentWSAAdapter {
	t.Helper()
	config, e := agenttool.ParseTencentWSAConfig([]byte(`{"apiKey":"unit-test-placeholder","keyName":"unit-source-run"}`))
	if e != nil {
		t.Fatal(e)
	}
	adapter, e := agenttool.NewTencentWSAAdapter(config, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		*calls++
		assertLiveNativePhase(t, f, h.search.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
		raw, e := io.ReadAll(req.Body)
		if e != nil || !bytes.Equal(raw, h.original.SearchWire()) || req.GetBody != nil || !req.Close {
			t.Fatal("exact original WSA query wire changed", e)
		}
		if hook != nil {
			hook()
		}
		page, _ := json.Marshal(map[string]string{"title": "UNIT verified fixture source", "url": "https://example.com/unit-source-run", "passage": "UNIT public source fixture passage"})
		response, _ := json.Marshal(map[string]any{"Response": map[string]any{"RequestId": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Query": h.original.SearchQuery(), "Version": "standard", "Pages": []string{string(page)}}})
		return liveNativeResponse(string(response), 200), nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	return adapter
}

func liveSourceRunBindFixture(t *testing.T, f *liveNativeFixture, h *nativeLiveSourceAnswerRun) (OwnLiveSourceBatch, LiveEgressPreview) {
	t.Helper()
	b := f.configuration.native.private.base
	calls := 0
	batch, e := h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, &calls, nil))
	if e != nil || calls != 1 {
		t.Fatal("actual native successful WSA envelope", e, calls)
	}
	if batch.EvidenceDigest() == "" || batch.Evidence().OperationID != h.search.OperationID {
		t.Fatal("WSA success did not mint exact source evidence")
	}
	preview, e := h.PreviewSourceModel(b.ctx, batch)
	if e != nil {
		t.Fatal("original explicit source model preview", e)
	}
	// Exact native replay is required independently of the approval action.
	if preview.Prepared().input != h.modelInput {
		t.Fatalf("fresh source preview changed planned input: equalDeadline=%t equalUTC=%t", preview.Prepared().input.DeadlineAt.Equal(h.modelInput.DeadlineAt), preview.Prepared().input.DeadlineAt.UTC() == h.modelInput.DeadlineAt.UTC())
	}
	tx, e := b.store.beginEgress(b.ctx)
	if e != nil {
		t.Fatal("source replay transaction", e)
	}
	stored, replayErr := readLiveEgressPreview(b.ctx, tx, preview.ID)
	var replay PreparedLiveEgress
	if replayErr == nil {
		replay, replayErr = b.store.prepareStoredLiveEgressWithSourceRunTx(b.ctx, tx, f.configuration.native.access, stored, h.projector, h.association())
	}
	_ = tx.Rollback(b.ctx)
	if replayErr != nil || !matchLivePreview(stored, replay) {
		t.Fatal("exact stored source preview replay", replayErr)
	}
	if replay.input != h.modelInput || replay.sourceBatch.evidenceDigest != batch.evidenceDigest {
		t.Fatalf("stored source preview changed planned input: equalDeadline=%t equalUTC=%t exactEvidence=%t", replay.input.DeadlineAt.Equal(h.modelInput.DeadlineAt), replay.input.DeadlineAt.UTC() == h.modelInput.DeadlineAt.UTC(), replay.sourceBatch.evidenceDigest == batch.evidenceDigest)
	}
	if e = h.ApproveSourceModel(b.ctx, preview.ID, preview.RequestDigest); e != nil {
		t.Fatal("original exact source approval", e)
	}
	c, e := h.Read(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	bound, e := h.BindSourceModel(b.ctx, preview.ID, preview.RequestDigest, c.Revision)
	if e != nil || bound.Steps[1].BindingState != modelrequestrun.Bound {
		t.Fatal("one approved late binding", e)
	}
	return batch, preview
}

func TestModelLiveSourceRunRealSQLTypedPipelineIntegration(t *testing.T) {
	f, h, initial := liveSourceRunFixture(t)
	b := f.configuration.native.private.base
	a := f.configuration.native.access
	var nativeNow time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&nativeNow); e != nil {
		t.Fatal("native metadata validation clock", e)
	}
	if initial.Kind != modelrequestrun.LiveSourceAnswer || initial.Steps[1].BindingState != modelrequestrun.WaitingSource || initial.Steps[1].PreviewID != "" || modelrequestrun.ValidatePlan(initial, nativeNow) != nil {
		t.Fatal("original two-step uncharged plan shape")
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{})
	// Store public endpoints retain their original refusal of linked operations.
	if _, e := b.store.ReserveOwnLiveAttempt(b.ctx, a, h.search, f.controller, nil); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("public Reserve bypassed Run", e)
	}
	if _, e := b.store.BeginOwnLiveAttempt(b.ctx, a, h.search.OperationID, f.controller, nil); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("public Begin bypassed Run", e)
	}
	if _, e := b.store.CheckOwnLiveAttempt(b.ctx, a, h.search.OperationID, f.controller, nil); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("public Check bypassed Run", e)
	}
	if _, e := b.store.FinishOwnLiveAttempt(b.ctx, a, h.search.OperationID, modelgateway.Usage{Status: "UNKNOWN", CostStatus: "UNKNOWN"}); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("public Finish bypassed Run", e)
	}
	if _, e := b.store.CancelOwnLocalModelRun(b.ctx, a, h.id, initial.Revision); e == nil {
		t.Fatal("old LOCAL cancellation accepted typed live Run")
	}
	if _, e := h.BindSourceModel(b.ctx, initial.Steps[0].PreviewID, initial.Steps[0].RequestDigest, initial.Revision); e == nil {
		t.Fatal("bound model before WSA")
	}
	first, e := h.ReserveSource(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	duplicate, e := h.ReserveSource(b.ctx)
	if e != nil || !duplicate.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("duplicate original Run reserve changed operation", e)
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
	batch, preview := liveSourceRunBindFixture(t, f, h)
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
	c, e := h.Read(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.BindSourceModel(b.ctx, preview.ID, preview.RequestDigest, c.Revision); e == nil {
		t.Fatal("second model binding")
	}
	if e = b.store.ApproveOwnLiveEgress(b.ctx, a, preview.ID, preview.RequestDigest, f.projector); e == nil {
		t.Fatal("public approve reconstructed linked source association")
	}
	raw, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	for _, private := range []string{h.original.query, batch.Sources()[0].Passage, "messages", "ticket"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("Run metadata exposed source/authority payload")
		}
	}
	// Duplicate first reservation consumes no extra budget.
	if _, e = h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, new(int), nil)); e == nil {
		t.Fatal("replayed completed source call")
	}
	modelCalls := 0
	adapter := liveNativeModelAdapter(t, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		modelCalls++
		assertLiveNativePhase(t, f, h.modelOperation, "IN_FLIGHT", "LIVE_IN_FLIGHT")
		raw, e := io.ReadAll(req.Body)
		if e != nil || !bytes.Equal(raw, preview.Prepared().ModelWire().ExactWire()) || req.GetBody != nil || !req.Close {
			t.Fatal("model wire differs from exact approved public source envelope", e)
		}
		if !bytes.Contains(raw, []byte(batch.Sources()[0].Passage)) || bytes.Contains(raw, []byte("合成私人会话014")) {
			t.Fatal("public evidence absent or private history exported")
		}
		return liveNativeResponse(`{"id":"unit-source-model","model":"hy3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"UNIT source-backed fixture response"}}],"usage":{"prompt_tokens":41,"completion_tokens":12,"total_tokens":53}}`, 200), nil
	}))
	result, e := h.CompleteSourceModel(b.ctx, adapter)
	if e != nil || result.Mode != modelgateway.Live || result.Text != "UNIT source-backed fixture response" || modelCalls != 1 {
		t.Fatal("native model pipeline completion", e, modelCalls)
	}
	if h.completedResult == nil || h.completedResult.Text != result.Text || h.completedResult.Usage.InputTokens == result.Usage.InputTokens || h.completedResult.Usage.OutputTokens == result.Usage.OutputTokens {
		t.Fatal("validated committed completion was not kept privately")
	}
	*result.Usage.InputTokens, *result.Usage.OutputTokens = 900, 901
	if *h.completedResult.Usage.InputTokens != 41 || *h.completedResult.Usage.OutputTokens != 12 {
		t.Fatal("returned usage mutated native completion")
	}
	finished, e := h.Read(b.ctx)
	if e != nil || finished.State != modelrequestrun.RunFinished || finished.Steps[1].State != modelrequestrun.StepUnknown {
		t.Fatal("validated text did not finish original Run", e)
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
	assertLiveNativePhase(t, f, h.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativePhase(t, f, h.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
	if _, e = h.CompleteSourceModel(b.ctx, adapter); e == nil || modelCalls != 1 {
		t.Fatal("UNKNOWN/FINISHED inference replay")
	}
	var input, output int64
	if e = b.pool.QueryRow(b.ctx, `SELECT reported_input,reported_output FROM model_budget_reservations WHERE operation_id=$1`, h.modelOperation).Scan(&input, &output); e != nil || input != 41 || output != 12 {
		t.Fatal("provider known observation missing while UNKNOWN cash retained", e)
	}
}

func TestModelLiveSourceRunRejectsFutureDatabaseControlIntegration(t *testing.T) {
	f, h, _ := liveSourceRunFixture(t)
	b := f.configuration.native.private.base
	// This fixture-owned malformed clock value must still fail against the
	// actual database clock; the fix never grants a future timestamp grace.
	b.exec(`UPDATE model_request_runs SET updated_at=clock_timestamp()+interval '1 second',revision=revision+1 WHERE id=$1`, h.id)
	if _, e := h.ReserveSource(b.ctx); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("future database control admitted", e)
	} else {
		var stage *nativeLiveRunStageError
		if !errors.As(e, &stage) || stage.stage != "current-control-db-clock" {
			t.Fatal("future control failed outside native database clock validation", e)
		}
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{})
}

func TestModelLiveSourceRunStopAndSourceWithdrawalIntegration(t *testing.T) {
	for _, kind := range []string{"stop_before_search_body", "stop_during_search", "source_changed_after_search", "revoked_model_preview", "stop_before_model_body", "stop_during_model", "late_model_response"} {
		t.Run(kind, func(t *testing.T) {
			f, h, _ := liveSourceRunFixture(t)
			b := f.configuration.native.private.base
			if kind == "stop_before_search_body" {
				config, e := agenttool.ParseTencentWSAConfig([]byte(`{"apiKey":"unit-test-placeholder","keyName":"unit-source-wire-stop"}`))
				if e != nil {
					t.Fatal(e)
				}
				calls := 0
				adapter, e := agenttool.NewTencentWSAAdapter(config, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
					calls++
					assertLiveNativePhase(t, f, h.search.OperationID, "IN_FLIGHT", "LIVE_IN_FLIGHT")
					if e := h.Stop(b.ctx); e != nil {
						t.Fatal("stop after transport handoff", e)
					}
					raw, e := io.ReadAll(req.Body)
					if e == nil || len(raw) != 0 {
						t.Fatal("stopped original Run released search body bytes", e, len(raw))
					}
					return nil, e
				}))
				if e != nil {
					t.Fatal(e)
				}
				if _, e = h.ExecuteSourceSearch(b.ctx, adapter); e == nil || calls != 1 {
					t.Fatal("stopped WSA wire returned successful evidence", e, calls)
				}
				assertLiveNativePhase(t, f, h.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
				assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
				return
			}
			if kind == "stop_during_search" {
				calls := 0
				_, e := h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, &calls, func() {
					if e := h.Stop(b.ctx); e != nil {
						t.Fatal(e)
					}
				}))
				if e == nil || calls != 1 {
					t.Fatal("stopped WSA source released", e)
				}
				assertLiveNativePhase(t, f, h.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
				assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
				return
			}
			if kind == "source_changed_after_search" {
				calls := 0
				batch, e := h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, &calls, nil))
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE agent_tasks SET query=query||' UNIT changed',updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
				if _, e = h.PreviewSourceModel(b.ctx, batch); e == nil {
					t.Fatal("stale source preview")
				}
				return
			}
			_, preview := liveSourceRunBindFixture(t, f, h)
			if kind == "revoked_model_preview" {
				if e := b.store.RevokeOwnModelEgress(b.ctx, f.configuration.native.access, preview.ID); e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			adapter := liveNativeModelAdapter(t, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				assertLiveNativePhase(t, f, h.modelOperation, "IN_FLIGHT", "LIVE_IN_FLIGHT")
				if kind == "stop_before_model_body" {
					if e := h.Stop(b.ctx); e != nil {
						t.Fatal("stop model after transport handoff", e)
					}
					raw, e := io.ReadAll(req.Body)
					if e == nil || len(raw) != 0 {
						t.Fatal("stopped original Run released model body bytes", e, len(raw))
					}
					return nil, e
				}
				if _, e := io.ReadAll(req.Body); e != nil {
					t.Fatal("current original Run model body read", e)
				}
				if kind == "stop_during_model" {
					if e := h.Stop(b.ctx); e != nil {
						t.Fatal(e)
					}
				}
				if kind == "late_model_response" {
					b.exec(`UPDATE model_request_runs SET lease_until=clock_timestamp(),revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
				}
				return liveNativeResponse(`{"id":"unit-withdrawn","model":"hy3","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"UNIT must not release"}}]}`, 200), nil
			}))
			result, e := h.CompleteSourceModel(b.ctx, adapter)
			if e == nil || result.Text != "" {
				t.Fatal("withdrawn result released", e)
			}
			if kind == "revoked_model_preview" {
				if calls != 0 {
					t.Fatal("revoked preview sent")
				}
				assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
			} else {
				if calls != 1 {
					t.Fatal("unexpected sends", calls)
				}
				assertLiveNativePhase(t, f, h.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
				assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
			}
		})
	}
}

func TestModelLiveSourceRunExactIdentityAndBindingFencesIntegration(t *testing.T) {
	for _, kind := range []string{"unapproved_call", "already_reserved_call", "wrong_session", "other_person", "organization", "query_restore_generation", "model_digest_mutation", "stale_bind_revision", "stop_before_bind"} {
		t.Run(kind, func(t *testing.T) {
			f := newLiveNativeFixture(t)
			b := f.configuration.native.private.base
			call := f.preview(t, modelegressbudget.LiveCall, kind != "unapproved_call")
			in := LiveSourceAnswerRunInput{RunID: liveNativeID(t, f), Search: f.input(t, call), ModelOperationID: liveNativeID(t, f), ModelPriceVersion: f.token.Base.Version, MaxOutputTokens: 768}
			if kind == "already_reserved_call" {
				if _, e := b.store.ReserveOwnLiveAttempt(b.ctx, f.configuration.native.access, in.Search, f.controller, nil); e != nil {
					t.Fatal(e)
				}
			}
			a := f.configuration.native.access
			if kind == "wrong_session" {
				a.SessionDigest = [32]byte{7}
			}
			if kind == "other_person" {
				a = agentevent.Access{SessionDigest: f.configuration.native.private.peer.SessionDigest}
			}
			if kind == "organization" {
				a = agentevent.Access{SessionDigest: f.configuration.native.private.org.SessionDigest}
			}
			ticket, e := f.controller.Capture(agentfeature.Enrichment)
			if e != nil {
				t.Fatal(e)
			}
			h, initial, e := b.store.CreateOwnLiveSourceAnswerRun(b.ctx, a, in, f.controller, ticket, f.projector, liveNativeGate{})
			if kind == "unapproved_call" || kind == "already_reserved_call" || kind == "wrong_session" || kind == "other_person" || kind == "organization" {
				if e == nil {
					t.Fatal("invalid native Run created")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				if _, e := b.pool.Exec(context.Background(), `DELETE FROM model_request_runs WHERE id=$1`, h.id); e != nil {
					t.Errorf("owned typed run cleanup: %v", e)
				}
			})
			if kind == "query_restore_generation" {
				b.exec(`UPDATE agent_tasks SET updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
				if _, e = h.ReserveSource(b.ctx); e == nil {
					t.Fatal("visible query restore revived old source generation")
				}
				assertLiveNativeViews(t, f, modelegressbudget.Limits{})
				return
			}
			calls := 0
			batch, e := h.ExecuteSourceSearch(b.ctx, liveSourceRunWSA(t, f, h, &calls, nil))
			if e != nil {
				t.Fatal(e)
			}
			preview, e := h.PreviewSourceModel(b.ctx, batch)
			if e != nil {
				t.Fatal(e)
			}
			if e = h.ApproveSourceModel(b.ctx, preview.ID, preview.RequestDigest); e != nil {
				t.Fatal(e)
			}
			c, e := h.Read(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			digest := preview.RequestDigest
			revision := c.Revision
			if kind == "model_digest_mutation" {
				digest = strings.Repeat("f", 64)
			}
			if kind == "stale_bind_revision" {
				revision = initial.Revision
			}
			if kind == "stop_before_bind" {
				if e = h.Stop(b.ctx); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = h.BindSourceModel(b.ctx, preview.ID, digest, revision); e == nil {
				t.Fatal("invalid late binding accepted")
			}
			after, e := h.Read(b.ctx)
			if e != nil || after.Steps[1].BindingState != modelrequestrun.WaitingSource || after.Steps[1].PreviewID != "" {
				t.Fatal("failed binding mutated exact original step", e)
			}
			assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 1, CostMicros: 80000})
		})
	}
}

func TestModelLiveSourceRunSQLRejectsNullTypedOutputCeilingsIntegration(t *testing.T) {
	f, h, _ := liveSourceRunFixture(t)
	b := f.configuration.native.private.base
	id := liveNativeID(t, f)
	_, e := b.pool.Exec(b.ctx, `INSERT INTO model_request_runs(id,owner_id,session_id,agent_id,root_trace_id,task_id,binding_id,task_generation,source_token,authority_token,state,deadline_at,lease_until,run_kind) SELECT $2,owner_id,session_id,agent_id,root_trace_id,task_id,binding_id,task_generation,source_token,authority_token,'PLANNED',deadline_at,lease_until,run_kind FROM model_request_runs WHERE id=$1`, h.id, id)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DELETE FROM model_request_runs WHERE id=$1`, id); e != nil {
			t.Errorf("owned null-shape run cleanup: %v", e)
		}
	})
	for _, kind := range []string{"SOURCE_RETRIEVAL", "MODEL_INFERENCE"} {
		t.Run(kind, func(t *testing.T) {
			operation := liveNativeID(t, f)
			var e error
			if kind == "SOURCE_RETRIEVAL" {
				_, e = b.pool.Exec(b.ctx, `INSERT INTO model_request_run_steps(run_id,ordinal,operation_id,preview_id,price_version,request_digest,step_kind,binding_state,planned_max_output_tokens) VALUES($1,1,$2,$3,$4,$5,'SOURCE_RETRIEVAL','BOUND',NULL)`, id, operation, h.search.PreviewID, f.call.Base.Version, h.original.digest)
			} else {
				_, e = b.pool.Exec(b.ctx, `INSERT INTO model_request_run_steps(run_id,ordinal,operation_id,preview_id,price_version,request_digest,step_kind,binding_state,planned_max_output_tokens) VALUES($1,2,$2,NULL,$3,NULL,'MODEL_INFERENCE','WAITING_SOURCE',NULL)`, id, operation, f.token.Base.Version)
			}
			var pg *pgconn.PgError
			if !errors.As(e, &pg) || pg.Code != "23514" || pg.ConstraintName != "model_request_live_step_shape" {
				t.Fatal("nullable typed ceiling was not rejected by closed CHECK", e)
			}
		})
	}
	assertLiveNativeViews(t, f, modelegressbudget.Limits{})
}
