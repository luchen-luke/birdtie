package modelgateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const fixtureRun = "74000000-0000-4000-8000-000000000001"
const fixtureOwner = "74000000-0000-4000-8000-000000000002"
const fixtureAgent = "74000000-0000-4000-8000-000000000003"
const fixtureContext = "74000000-0000-4000-8000-000000000004"
const fixturePolicy = "74000000-0000-4000-8000-000000000005"
const fixtureBudget = "74000000-0000-4000-8000-000000000006"
const fixtureEntity = "74000000-0000-4000-8000-000000000007"

func testNow() time.Time { return time.Now().UTC() }

func testRequest(mode OutputMode) Request {
	capabilities := []string{"text"}
	if mode == Structured || mode == ToolProposals {
		capabilities = append(capabilities, "structured_output_validatable")
	}
	if mode == ToolProposals {
		capabilities = append(capabilities, "tools")
	}
	return Request{SchemaVersion: RequestVersion, RunID: fixtureRun,
		Agent:    agentcognitive.AgentReference{AgentID: fixtureAgent, Principal: actorref.PrincipalRef{Type: actorref.Person, ID: fixtureOwner}, Role: agentruntime.PersonalAgent},
		TaskKind: ActivityQuery, PromptVersion: "activity_query.v1", InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1",
		ContextSnapshotRef: fixtureContext, DataPolicyRef: fixturePolicy, BudgetRef: fixtureBudget, Budget: Budget{MaxOutputTokens: 128},
		Messages:   []Message{{Role: "system", Content: "合成固定prompt"}, {Role: "user", Content: "合成问句：周末有什么活动"}},
		OutputMode: mode, ToolAllowlist: []string{"activity.search", "activity.detail"}, CapabilitiesRequired: capabilities, DeadlineAt: testNow().Add(time.Minute)}
}

func testDescriptor() ProviderDescriptor {
	return ProviderDescriptor{"fake-fixture", "fake-text", "fixture-v1", OfflineContract}
}

func TestModelRequestContractPositive(t *testing.T) {
	for _, mode := range []OutputMode{Text, Structured, ToolProposals} {
		t.Run(string(mode), func(t *testing.T) {
			r := testRequest(mode)
			if err := ValidateRequest(r, testNow()); err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(r)
			got, err := DecodeRequest(data, testNow())
			if err != nil || got.Agent != r.Agent || got.OutputMode != mode {
				t.Fatal("closed request roundtrip", err)
			}
		})
	}
	r := testRequest(Structured)
	r.TaskKind = MemoryCandidateExtraction
	r.OutputSchemaVersion = "air.candidate_proposal.v1"
	r.ToolAllowlist = []string{}
	if err := ValidateRequest(r, testNow()); err != nil {
		t.Fatal("candidate request contract", err)
	}
	r = testRequest(Text)
	r.Agent.Principal.Type = actorref.Organization
	r.Agent.Role = agentruntime.OrganizationAgent
	if err := ValidateRequest(r, testNow()); err != nil {
		t.Fatal("typed Organization structural reference", err)
	}
}

func TestModelRequestRejectsInvalidSelectorsAndBounds(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"schema", func(r *Request) { r.SchemaVersion = "air.model_request.v2" }},
		{"run_secret", func(r *Request) { r.RunID = "Bearer secret" }},
		{"zero_agent", func(r *Request) { r.Agent.AgentID = "00000000-0000-0000-0000-000000000000" }},
		{"model_as_agent", func(r *Request) { r.Agent.AgentID = "fake-text-v1" }},
		{"wrong_role", func(r *Request) { r.Agent.Role = agentruntime.OrganizationAgent }},
		{"business", func(r *Request) {
			r.Agent.Principal.Type = actorref.Business
			r.Agent.Role = agentruntime.BusinessAgent
		}},
		{"community", func(r *Request) { r.Agent.Principal.Type = actorref.Community }},
		{"principal_zero", func(r *Request) { r.Agent.Principal.ID = "00000000-0000-0000-0000-000000000000" }},
		{"kind", func(r *Request) { r.TaskKind = "EXECUTE_WRITE" }},
		{"prompt_url", func(r *Request) { r.PromptVersion = "https://example.invalid/prompt" }},
		{"input_schema", func(r *Request) { r.InputSchemaVersion = "free_form" }},
		{"output_schema", func(r *Request) { r.OutputSchemaVersion = "unknown.v1" }},
		{"context_url", func(r *Request) { r.ContextSnapshotRef = "https://example.invalid/private" }},
		{"fake_policy", func(r *Request) { r.DataPolicyRef = "confirmed" }},
		{"fake_budget", func(r *Request) { r.BudgetRef = "unlimited" }},
		{"zero_tokens", func(r *Request) { r.Budget.MaxOutputTokens = 0 }},
		{"negative_tokens", func(r *Request) { r.Budget.MaxOutputTokens = -1 }},
		{"too_many_tokens", func(r *Request) { r.Budget.MaxOutputTokens = 4097 }},
		{"missing_messages", func(r *Request) { r.Messages = nil }},
		{"unknown_role", func(r *Request) { r.Messages[1].Role = "assistant_tool_executor" }},
		{"blank_message", func(r *Request) { r.Messages[1].Content = " " }},
		{"secret_nul", func(r *Request) { r.Messages[1].Content = "x\x00y" }},
		{"oversized_message", func(r *Request) { r.Messages[1].Content = strings.Repeat("x", 4097) }},
		{"too_many_messages", func(r *Request) { r.Messages = make([]Message, 17) }},
		{"oversized_total", func(r *Request) {
			r.Messages = []Message{}
			for i := 0; i < 5; i++ {
				r.Messages = append(r.Messages, Message{"user", strings.Repeat("x", 4096)})
			}
		}},
		{"second_system", func(r *Request) { r.Messages[1].Role = "system" }},
		{"unknown_tool", func(r *Request) { r.ToolAllowlist = []string{"message.send"} }},
		{"duplicate_tool", func(r *Request) { r.ToolAllowlist = []string{"activity.search", "activity.search"} }},
		{"too_many_tools", func(r *Request) { r.ToolAllowlist = make([]string, 5) }},
		{"empty_tool_mode", func(r *Request) { r.OutputMode = ToolProposals; r.ToolAllowlist = nil }},
		{"unknown_mode", func(r *Request) { r.OutputMode = "AUTO_EXECUTE" }},
		{"missing_capabilities", func(r *Request) { r.CapabilitiesRequired = nil }},
		{"unknown_capability", func(r *Request) { r.CapabilitiesRequired = []string{"vision"} }},
		{"duplicate_capability", func(r *Request) { r.OutputMode = Structured; r.CapabilitiesRequired = []string{"text", "text"} }},
		{"wrong_output_capabilities", func(r *Request) { r.OutputMode = Structured }},
		{"deadline_elapsed", func(r *Request) { r.DeadlineAt = testNow() }},
		{"unbounded_deadline", func(r *Request) { r.DeadlineAt = testNow().Add(MaxDeadline + time.Second) }},
		{"candidate_tools", func(r *Request) {
			r.TaskKind = MemoryCandidateExtraction
			r.OutputSchemaVersion = "air.candidate_proposal.v1"
		}},
		{"candidate_text", func(r *Request) {
			r.TaskKind = MemoryCandidateExtraction
			r.ToolAllowlist = nil
			r.OutputSchemaVersion = "air.candidate_proposal.v1"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := testRequest(Text)
			tc.change(&r)
			if !errors.Is(ValidateRequest(r, testNow()), ErrInvalid) {
				t.Fatal("invalid normalized request accepted")
			}
		})
	}
}

func TestModelRequestStrictJSON(t *testing.T) {
	r := testRequest(Text)
	data, _ := json.Marshal(r)
	cases := map[string][]byte{
		"unknown_confirmed": bytes.Replace(data, []byte(`"schema_version":`), []byte(`"confirmed":true,"schema_version":`), 1),
		"provider_selected": bytes.Replace(data, []byte(`"schema_version":`), []byte(`"model":"fake-text","schema_version":`), 1),
		"duplicate":         bytes.Replace(data, []byte(`"run_id":`), []byte(`"run_id":"evil","run_id":`), 1),
		"case_alias":        bytes.Replace(data, []byte(`"run_id":`), []byte(`"Run_ID":`), 1),
		"nested_permission": bytes.Replace(data, []byte(`"max_output_tokens":128`), []byte(`"max_output_tokens":128,"approved":true`), 1),
		"nested_duplicate":  bytes.Replace(data, []byte(`"max_output_tokens":128`), []byte(`"max_output_tokens":1,"max_output_tokens":128`), 1),
		"principal_fake":    bytes.Replace(data, []byte(`"Principal":{`), []byte(`"Principal":{"verified":true,`), 1),
		"message_unknown":   bytes.Replace(data, []byte(`"role":"system"`), []byte(`"role":"system","authority":true`), 1),
		"message_duplicate": bytes.Replace(data, []byte(`"role":"system"`), []byte(`"role":"user","role":"system"`), 1),
		"trailing":          append(append([]byte{}, data...), []byte(`{}`)...),
		"oversized":         append(append([]byte{}, data...), bytes.Repeat([]byte(" "), MaxRequestBytes)...),
		"null":              []byte(`null`), "array": []byte(`[]`), "malformed": []byte(`{`),
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeRequest(value, testNow())
			if !errors.Is(err, ErrInvalid) || got.RunID != "" || got.Messages != nil {
				t.Fatal("unknown/wire authority accepted", err)
			}
		})
	}
}

func TestProviderPayloadDropsNativeAuthoritySelectors(t *testing.T) {
	r := testRequest(Text)
	p := providerRequest(r)
	data, _ := json.Marshal(p)
	for _, secret := range []string{fixtureRun, fixtureAgent, fixtureOwner, fixtureContext, fixturePolicy, fixtureBudget, "agent_ref", "principal", "budget_ref", "data_policy_ref", "context_snapshot_ref", "confirmed"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("internal selector sent to provider", secret)
		}
	}
	p.Messages[0].Content = "adapter mutation"
	p.ToolAllowlist[0] = "message.send"
	if r.Messages[0].Content == "adapter mutation" || r.ToolAllowlist[0] != "activity.search" {
		t.Fatal("adapter aliases original request slices")
	}
}

func TestCandidateExtractionSchemaVersionsRemainExplicit(t *testing.T) {
	for _, schema := range []string{CandidateProposalVersion, CandidateProposalV2Version} {
		t.Run(schema, func(t *testing.T) {
			r := testRequest(Structured)
			r.TaskKind = MemoryCandidateExtraction
			r.OutputSchemaVersion = schema
			r.ToolAllowlist = []string{}
			wire, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeRequest(wire, testNow())
			if err != nil || decoded.OutputSchemaVersion != schema || decoded.TaskKind != MemoryCandidateExtraction {
				t.Fatal("candidate schema was rejected or changed", err)
			}
			for name, mutate := range map[string]func(*Request){
				"activity_task": func(v *Request) { v.TaskKind = ActivityQuery },
				"text_mode": func(v *Request) {
					v.OutputMode = Text
					v.CapabilitiesRequired = []string{"text"}
				},
				"tools":          func(v *Request) { v.ToolAllowlist = []string{"activity.search"} },
				"unknown_schema": func(v *Request) { v.OutputSchemaVersion = "air.candidate_proposal.v3" },
			} {
				t.Run(name, func(t *testing.T) {
					bad := r
					mutate(&bad)
					if !errors.Is(ValidateRequest(bad, testNow()), ErrInvalid) {
						t.Fatal("candidate schema acquired another task/mode/tool")
					}
				})
			}
		})
	}
}

func TestCandidateProposalVersionedJSONPreservesV1Shape(t *testing.T) {
	c := CandidateProposal{Status: "CANDIDATE", Predicate: "ACTIVITY_CATEGORY", Value: "sports", SourceRefIDs: []string{fixtureContext}, SubjectAttribution: "UNVERIFIED"}
	for _, tc := range []struct {
		name  string
		value CandidateProposal
		want  string
	}{
		{"candidate", c, `{"status":"CANDIDATE","predicate":"ACTIVITY_CATEGORY","value":"sports","source_ref_ids":["` + fixtureContext + `"],"subject_attribution":"UNVERIFIED"}`},
		{"legacy_empty_shape", CandidateProposal{Status: "CANDIDATE"}, `{"status":"CANDIDATE","predicate":"","value":"","source_ref_ids":null,"subject_attribution":""}`},
		{"no_candidate", CandidateProposal{Status: NoCandidateStatus}, `{"status":"NO_CANDIDATE"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire, err := json.Marshal(tc.value)
			if err != nil || string(wire) != tc.want {
				t.Fatalf("candidate JSON changed: %s, %v", wire, err)
			}
		})
	}
	for name, mutate := range map[string]func(*CandidateProposal){
		"predicate":     func(c *CandidateProposal) { c.Predicate = "ACTIVITY_CATEGORY" },
		"value":         func(c *CandidateProposal) { c.Value = "sports" },
		"empty_sources": func(c *CandidateProposal) { c.SourceRefIDs = []string{} },
		"sources":       func(c *CandidateProposal) { c.SourceRefIDs = []string{fixtureContext} },
		"attribution":   func(c *CandidateProposal) { c.SubjectAttribution = "UNVERIFIED" },
	} {
		t.Run("mixed_"+name, func(t *testing.T) {
			bad := CandidateProposal{Status: NoCandidateStatus}
			mutate(&bad)
			if wire, err := json.Marshal(bad); !errors.Is(err, ErrAdapter) || wire != nil {
				t.Fatal("mixed no-candidate value serialized", err)
			}
		})
	}
}
