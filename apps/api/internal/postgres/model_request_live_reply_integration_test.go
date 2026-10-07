package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

// SQL is exercised only by the parent's disposable-database run. Provider
// responses below are transport fixtures, never real retrieval/model evidence.
const liveSourceReplyFixtureText = "UNIT 模型逐字返回的说明：请核对本条公开来源；这段文字不是结果数量或规则生成的回答。"

func liveSourceReplyCompleteFixture(t *testing.T, f *liveNativeFixture, h *nativeLiveSourceAnswerRun) (modelgateway.Result, LiveEgressPreview, *int) {
	t.Helper()
	_, preview := liveSourceRunBindFixture(t, f, h)
	b := f.configuration.native.private.base
	calls := new(int)
	adapter := liveNativeModelAdapter(t, liveNativeTransport(func(req *http.Request) (*http.Response, error) {
		*calls++
		raw, e := io.ReadAll(req.Body)
		if e != nil || !bytes.Equal(raw, preview.Prepared().ModelWire().ExactWire()) {
			t.Fatal("reply model wire differs from exact approved source wire", e)
		}
		wire, e := json.Marshal(map[string]any{
			"id": "unit-live-source-reply", "model": "hy3",
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": liveSourceReplyFixtureText}}},
			"usage":   map[string]any{"prompt_tokens": 41, "completion_tokens": 12, "total_tokens": 53},
		})
		if e != nil {
			t.Fatal(e)
		}
		return liveNativeResponse(string(wire), 200), nil
	}))
	result, e := h.CompleteSourceModel(b.ctx, adapter)
	if e != nil || result.Text != liveSourceReplyFixtureText || *calls != 1 {
		t.Fatal("validated fixture model text failed before reply finalization", e, *calls)
	}
	return result, preview, calls
}

func liveSourceReplyReadTask(t *testing.T, f *liveNativeFixture, id string) agentworkspace.Task {
	t.Helper()
	b := f.configuration.native.private.base
	task, e := b.store.GetTask(b.ctx, b.person.ID, id)
	if e != nil {
		t.Fatal("read original persisted Task", e)
	}
	return task
}

func liveSourceReplyRequireNoTaskWrite(t *testing.T, f *liveNativeFixture, h *nativeLiveSourceAnswerRun, expected agentworkspace.Task) {
	t.Helper()
	current := liveSourceReplyReadTask(t, f, h.search.TaskID)
	if !reflect.DeepEqual(current, expected) {
		t.Fatal("denied reply overwrote the original Task or conversation")
	}
}

func TestModelLiveSourceReplyPersistRestoreExactTextIntegration(t *testing.T) {
	f, h, _ := liveSourceRunFixture(t)
	b := f.configuration.native.private.base
	before := liveSourceReplyReadTask(t, f, h.search.TaskID)
	result, _, calls := liveSourceReplyCompleteFixture(t, f, h)
	// Returned Gateway data is caller-owned. Changing it must not overwrite the
	// validated process-local completion retained by the original native Run.
	result.Text = "UNIT forged caller replacement"
	*result.Usage.InputTokens = 999999
	reply, e := h.FinalizeSourceReply(b.ctx)
	if e != nil {
		t.Fatal("finalize exact validated reply", e)
	}
	task := reply.Task()
	if task.Status != agentworkspace.TaskCompleted || task.ID != before.ID || len(task.Conversation) != len(before.Conversation)+1 {
		t.Fatal("one original Task completion and one message required")
	}
	if !reflect.DeepEqual(task.Conversation[:len(before.Conversation)], before.Conversation) || !reflect.DeepEqual(task.Filters, before.Filters) || task.Query != before.Query || task.CityID != before.CityID || task.PrincipalID != before.PrincipalID || task.Intent != before.Intent {
		t.Fatal("reply changed native history, filters, city, principal or intent")
	}
	message := task.Conversation[len(task.Conversation)-1]
	if message.Role != "assistant" || message.Text != liveSourceReplyFixtureText || message.SourceRunID != h.id || message.SourceEvidenceDigest != h.batch.EvidenceDigest() || len(message.Sources) != len(h.batch.Sources()) {
		t.Fatal("source provenance and actual model text did not share one message")
	}
	for i, source := range h.batch.Sources() {
		if message.Sources[i] != (agentworkspace.AnswerSource{ID: fmt.Sprintf("source-%d", i+1), Title: source.Title, URL: source.URL}) {
			t.Fatal("message source differs from original retrieved batch")
		}
	}
	restored := liveSourceReplyReadTask(t, f, task.ID)
	if !reflect.DeepEqual(restored, task) || reply.Revalidate(b.ctx) != nil {
		t.Fatal("persisted reply did not recover exact source message")
	}
	answer, e := reply.Answer("unit-reply-request-1")
	if e != nil {
		t.Fatal("bind actual completion to current presentation request", e)
	}
	projected := agentworkspace.Results{RequestID: "unit-reply-request-1", ConversationID: task.ID, NativeProjection: true, ResultSet: agentworkspace.ResultSet{ID: "unit-native-result-set", TaskID: task.ID, GeneratedAt: task.UpdatedAt, Status: "empty"}}
	applied, e := agentworkspace.ApplySourcedAnswer(projected, agentworkspace.SanitizeTaskForResponse(task), projected.RequestID, answer, time.Now())
	if e != nil || applied.Message != liveSourceReplyFixtureText || !reflect.DeepEqual(applied.ResultSet.Sources, message.Sources) || applied.ResultSet.AnswerBinding.RunID != h.id {
		t.Fatal("presentation lost exact model text or original source binding", e)
	}
	if e = reply.Revalidate(b.ctx); e != nil || *calls != 1 {
		t.Fatal("presentation revalidation made another model call", e)
	}
	if _, e = h.FinalizeSourceReply(b.ctx); e == nil {
		t.Fatal("second finalization appended a duplicate reply")
	}
	liveSourceReplyRequireNoTaskWrite(t, f, h, task)
	if *calls != 1 || reply.Revalidate(b.ctx) != nil {
		t.Fatal("duplicate finalization changed completed reply authority")
	}
	// All public Task and presentation getters must copy nested mutable state.
	task.Conversation[len(task.Conversation)-1].Sources[0].Title = "UNIT mutated source"
	task.Conversation[len(task.Conversation)-1].Text = "UNIT mutated text"
	task.Filters["unit-local-mutation"] = "no authority"
	if !reflect.DeepEqual(reply.Task(), restored) {
		t.Fatal("Task getter exposed retained map/conversation/source aliases")
	}
	applied.ResultSet.Sources[0].URL = "https://example.org/unit-mutated"
	answer2, e := reply.Answer("unit-reply-request-2")
	if e != nil {
		t.Fatal(e)
	}
	projected.RequestID = "unit-reply-request-2"
	applied2, e := agentworkspace.ApplySourcedAnswer(projected, agentworkspace.SanitizeTaskForResponse(restored), projected.RequestID, answer2, time.Now())
	if e != nil || !reflect.DeepEqual(applied2.ResultSet.Sources, restored.Conversation[len(restored.Conversation)-1].Sources) || *calls != 1 {
		t.Fatal("presentation mutation changed retained sources", e)
	}
	if raw, e := json.Marshal(reply); !errors.Is(e, modelegressbudget.ErrServerOnly) || len(raw) != 0 {
		t.Fatal("native reply serialized an authority handle", e)
	}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		formatted := fmt.Sprintf(verb, reply)
		if !strings.Contains(formatted, "redacted") || strings.Contains(formatted, liveSourceReplyFixtureText) {
			t.Fatal("native reply formatting exposed completion")
		}
	}
	assertLiveNativePhase(t, f, h.search.OperationID, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativePhase(t, f, h.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
	assertLiveNativeViews(t, f, modelegressbudget.Limits{Requests: 2, InputTokens: 196608, OutputTokens: 768, CostMicros: 279680})
}

func TestModelLiveSourceReplyRejectBeforeValidatedCompletionIntegration(t *testing.T) {
	for _, mode := range []string{"before_search", "bound_before_model", "forged_result_before_finished"} {
		t.Run(mode, func(t *testing.T) {
			f, h, _ := liveSourceRunFixture(t)
			b := f.configuration.native.private.base
			before := liveSourceReplyReadTask(t, f, h.search.TaskID)
			if mode != "before_search" {
				liveSourceRunBindFixture(t, f, h)
			}
			if mode == "forged_result_before_finished" {
				// A matching caller-created result alone must not replace the
				// actual finished original run and both attempted native steps.
				h.completedResult = &modelgateway.Result{Mode: modelgateway.Live, Status: modelgateway.Completed, Text: "UNIT forged completion", RunID: h.original.request.RunID, Agent: h.original.request.Agent}
			}
			if reply, e := h.FinalizeSourceReply(b.ctx); e == nil || reply != nil {
				t.Fatal("unvalidated completion finalized original conversation", e)
			}
			liveSourceReplyRequireNoTaskWrite(t, f, h, before)
		})
	}
}

// Every mutation is fixture-owned and intentionally occurs after provider
// completion, before the final authorized message write or release check.
func liveSourceReplyWithdraw(t *testing.T, f *liveNativeFixture, h *nativeLiveSourceAnswerRun, preview LiveEgressPreview, mode string) {
	t.Helper()
	b := f.configuration.native.private.base
	switch mode {
	case "cross_owner":
		h.access = agentevent.Access{SessionDigest: f.configuration.native.private.peer.SessionDigest}
	case "new_turn":
		b.exec(`UPDATE agent_tasks SET filters=filters||'{"currentQuery":"UNIT 新一轮问题"}'::jsonb,conversation=conversation||'[{"role":"user","text":"UNIT 新一轮问题"}]'::jsonb,updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
	case "late_query":
		b.exec(`UPDATE agent_tasks SET query=query||' UNIT changed',updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
	case "same_value_generation":
		b.exec(`UPDATE agent_tasks SET query=query WHERE id=$1`, h.search.TaskID)
	case "profile_disable_restore":
		// Retire the actual authority generations without deleting a Profile:
		// its native cascading bindings are intentionally retained by 109.
		b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, b.personID)
		b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, b.personID)
	case "agent_disable_restore":
		b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
		b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
	case "current_preview_revoked":
		if e := b.store.RevokeOwnModelEgress(b.ctx, f.configuration.native.access, preview.ID); e != nil {
			t.Fatal(e)
		}
	case "search_preview_revoked":
		if e := b.store.RevokeOwnModelEgress(b.ctx, f.configuration.native.access, h.search.PreviewID); e != nil {
			t.Fatal(e)
		}
	case "stop_fence":
		b.exec(`UPDATE model_request_runs SET revision=revision+1,fence=fence+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
	case "session_revoked":
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.configuration.native.private.ownerSession)
	case "expired":
		b.exec(`UPDATE model_request_runs SET lease_until=clock_timestamp(),revision=revision+1,updated_at=clock_timestamp() WHERE id=$1`, h.id)
	case "baseline_terminal_task":
		b.exec(`UPDATE agent_tasks SET status='FAILED',updated_at=clock_timestamp() WHERE id=$1`, h.search.TaskID)
	default:
		t.Fatal("unknown withdrawal boundary")
	}
}

func TestModelLiveSourceReplyFreshFinalizationFencesIntegration(t *testing.T) {
	for _, mode := range []string{"cross_owner", "new_turn", "late_query", "same_value_generation", "profile_disable_restore", "agent_disable_restore", "current_preview_revoked", "search_preview_revoked", "stop_fence", "session_revoked", "expired", "baseline_terminal_task"} {
		t.Run(mode, func(t *testing.T) {
			f, h, _ := liveSourceRunFixture(t)
			b := f.configuration.native.private.base
			_, preview, calls := liveSourceReplyCompleteFixture(t, f, h)
			liveSourceReplyWithdraw(t, f, h, preview, mode)
			before := liveSourceReplyReadTask(t, f, h.search.TaskID)
			if reply, e := h.FinalizeSourceReply(b.ctx); e == nil || reply != nil {
				t.Fatal("withdrawn native completion wrote a reply", e)
			}
			liveSourceReplyRequireNoTaskWrite(t, f, h, before)
			if *calls != 1 {
				t.Fatal("denied finalization dispatched another provider request")
			}
			assertLiveNativePhase(t, f, h.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
		})
	}
}

func TestModelLiveSourceReplyFreshReleaseFencesIntegration(t *testing.T) {
	for _, mode := range []string{"cross_owner", "new_turn", "late_query", "same_value_generation", "profile_disable_restore", "agent_disable_restore", "current_preview_revoked", "search_preview_revoked", "stop_fence", "session_revoked", "expired", "baseline_terminal_task"} {
		t.Run(mode, func(t *testing.T) {
			f, h, _ := liveSourceRunFixture(t)
			b := f.configuration.native.private.base
			_, preview, calls := liveSourceReplyCompleteFixture(t, f, h)
			reply, e := h.FinalizeSourceReply(b.ctx)
			if e != nil || reply.Revalidate(b.ctx) != nil {
				t.Fatal("legal completion baseline failed", e)
			}
			liveSourceReplyWithdraw(t, f, h, preview, mode)
			before := liveSourceReplyReadTask(t, f, h.search.TaskID)
			if e = reply.Revalidate(b.ctx); e == nil {
				t.Fatal("reply release revived withdrawn native authority")
			}
			liveSourceReplyRequireNoTaskWrite(t, f, h, before)
			if *calls != 1 {
				t.Fatal("late release check sent another provider request")
			}
			assertLiveNativePhase(t, f, h.modelOperation, "UNKNOWN", "LIVE_ATTEMPTED")
		})
	}
}

func TestModelLiveSourceReplyNilAndForgedHandlesUnit(t *testing.T) {
	var run *nativeLiveSourceAnswerRun
	if reply, e := run.FinalizeSourceReply(context.Background()); !errors.Is(e, modelegressbudget.ErrDenied) || reply != nil {
		t.Fatal("nil run finalized a reply")
	}
	for _, reply := range []*nativeLiveSourceReply{nil, {}, {run: &nativeLiveSourceAnswerRun{}}} {
		if answer, e := reply.Answer("unit-unbound-request"); !errors.Is(e, modelegressbudget.ErrDenied) || answer != nil || reply.Revalidate(context.Background()) == nil {
			t.Fatal("unbound handle minted a sourced reply")
		}
	}
}
