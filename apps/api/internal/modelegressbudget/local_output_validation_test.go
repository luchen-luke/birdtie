package modelegressbudget

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"testing"
	"time"
)

func outputLocalRequest() modelgateway.Request {
	return modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: "74000000-0000-4000-8000-000000000001", Agent: agentcognitive.AgentReference{AgentID: "74000000-0000-4000-8000-000000000003", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "74000000-0000-4000-8000-000000000002"}, Role: agentruntime.PersonalAgent}, TaskKind: modelgateway.ActivityQuery, PromptVersion: "activity_query.v1", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", ContextSnapshotRef: "74000000-0000-4000-8000-000000000004", DataPolicyRef: "74000000-0000-4000-8000-000000000005", BudgetRef: "74000000-0000-4000-8000-000000000006", Budget: modelgateway.Budget{MaxOutputTokens: 128}, Messages: []modelgateway.Message{{Role: "user", Content: "原本地合成查询"}}, OutputMode: modelgateway.Structured, ToolAllowlist: []string{}, CapabilitiesRequired: []string{"text", "structured_output_validatable"}, DeadlineAt: time.Now().UTC().Add(time.Minute)}
}
func TestLocalOutputFailureProvenanceCannotBeSerializedOrMutated(t *testing.T) {
	r := outputLocalRequest()
	f := newLocalOutputFailure("original-operation", r, "INVALID_JSON")
	if f == nil {
		t.Fatal("structured bounded failure not signed")
	}
	r.Messages[0].Content = "caller mutation"
	op, got, ok := f.OperationRequest()
	if !ok || op != "original-operation" || got.Messages[0].Content != "原本地合成查询" {
		t.Fatal("input mutated private provenance")
	}
	got.Messages[0].Content = "getter mutation"
	_, got, ok = f.OperationRequest()
	if !ok || got.Messages[0].Content != "原本地合成查询" {
		t.Fatal("getter mutated private provenance")
	}
	if _, e := json.Marshal(f); !errors.Is(e, ErrServerOnly) {
		t.Fatal("format failure persisted as grant", e)
	}
	var fake LocalOutputFailure
	if e := json.Unmarshal([]byte(`{"operation":"original-operation","code":"INVALID_JSON"}`), &fake); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	if _, _, ok = fake.OperationRequest(); ok {
		t.Fatal("JSON reconstructed format provenance")
	}
	f.request.RunID = "changed"
	if _, _, ok = f.OperationRequest(); ok {
		t.Fatal("digest mutation accepted")
	}
}
func TestLocalOutputFailureOnlyBoundedStructuredCodes(t *testing.T) {
	for _, kind := range []string{"empty-operation", "unknown-code", "text", "expired", "invalid-schema", "tools"} {
		t.Run(kind, func(t *testing.T) {
			r := outputLocalRequest()
			op, code := "operation", "INVALID_JSON"
			switch kind {
			case "empty-operation":
				op = ""
			case "unknown-code":
				code = "PROVIDER_UNKNOWN"
			case "text":
				r.OutputMode = modelgateway.Text
			case "expired":
				r.DeadlineAt = time.Now().Add(-time.Second)
			case "invalid-schema":
				r.SchemaVersion = "unknown"
			case "tools":
				r.ToolAllowlist = []string{"shell"}
			}
			if newLocalOutputFailure(op, r, code) != nil {
				t.Fatal("invalid format fact signed", kind)
			}
		})
	}
	if newLocalOutputFailure("operation", outputLocalRequest(), "TRUNCATED") == nil {
		t.Fatal("valid actual truncation unsupported")
	}
}
