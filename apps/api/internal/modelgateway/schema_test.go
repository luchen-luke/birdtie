package modelgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProviderResponseRejectsUnknownAmbiguousAndOversizedFields(t *testing.T) {
	text := `{"status":"COMPLETED","request_id":"fake-response","finish_reason":"stop","text":"synthetic"}`
	answer := `{"status":"COMPLETED","request_id":"fake-answer","finish_reason":"stop","structured":{"answer":"synthetic","entity_refs":[{"type":"ACTIVITY","id":"` + fixtureEntity + `"}]}}`
	tools := `{"status":"COMPLETED","request_id":"fake-tool","finish_reason":"stop","tool_proposals":[{"tool":"activity.search","arguments":{"query":"合成羽毛球"},"reason_summary":"仅提议"}]}`
	candidate := `{"status":"COMPLETED","request_id":"fake-candidate","finish_reason":"stop","structured":{"status":"CANDIDATE","predicate":"ACTIVITY_CATEGORY","value":"badminton","source_ref_ids":["` + fixtureContext + `"],"subject_attribution":"UNVERIFIED"}}`
	cases := []struct {
		name   string
		mode   OutputMode
		memory bool
		raw    string
	}{
		{"unknown_authority", Text, false, strings.Replace(text, `"text":`, `"approved":true,"text":`, 1)},
		{"unknown_trace", Text, false, strings.Replace(text, `"text":`, `"chain_of_thought":"private","text":`, 1)},
		{"provider_agent_override", Text, false, strings.Replace(text, `"text":`, `"agent_id":"`+fixtureAgent+`","text":`, 1)},
		{"duplicate", Text, false, strings.Replace(text, `"status":`, `"status":"REFUSED","status":`, 1)},
		{"escaped_duplicate", Text, false, strings.Replace(text, `"status":`, `"\u0073tatus":"REFUSED","status":`, 1)},
		{"case_alias", Text, false, strings.Replace(text, `"status":`, `"Status":`, 1)},
		{"unknown_status", Text, false, strings.Replace(text, `"COMPLETED"`, `"SUCCESS"`, 1)},
		{"provider_invalid_status", Text, false, strings.Replace(text, `"COMPLETED"`, `"INVALID"`, 1)},
		{"status_null", Text, false, strings.Replace(text, `"COMPLETED"`, `null`, 1)},
		{"missing_status", Text, false, strings.Replace(text, `"status":"COMPLETED",`, ``, 1)},
		{"request_id_secret", Text, false, strings.Replace(text, `"fake-response"`, `"Bearer private-secret"`, 1)},
		{"request_id_url", Text, false, strings.Replace(text, `"fake-response"`, `"https://example.invalid"`, 1)},
		{"request_id_null", Text, false, strings.Replace(text, `"fake-response"`, `null`, 1)},
		{"bad_finish", Text, false, strings.Replace(text, `"stop"`, `"tool_execute"`, 1)},
		{"empty_finish", Text, false, strings.Replace(text, `"stop"`, `""`, 1)},
		{"blank_text", Text, false, strings.Replace(text, `"synthetic"`, `" "`, 1)},
		{"null_text", Text, false, strings.Replace(text, `"synthetic"`, `null`, 1)},
		{"numeric_text", Text, false, strings.Replace(text, `"synthetic"`, `1`, 1)},
		{"nul_text", Text, false, strings.Replace(text, `"synthetic"`, `"x\u0000y"`, 1)},
		{"oversized_text", Text, false, strings.Replace(text, `"synthetic"`, `"`+strings.Repeat("x", 8193)+`"`, 1)},
		{"wrong_variant", Text, false, answer},
		{"mixed_variants", Text, false, strings.Replace(text, `"text":`, `"structured":{"answer":"x","entity_refs":[]},"text":`, 1)},
		{"refusal_payload", Text, false, strings.Replace(strings.Replace(text, `"COMPLETED"`, `"REFUSED"`, 1), `"stop"`, `"refusal"`, 1)},
		{"unavailable_payload", Text, false, strings.Replace(strings.Replace(text, `"COMPLETED"`, `"UNAVAILABLE"`, 1), `"stop"`, `"unavailable"`, 1)},
		{"refusal_wrong_finish", Text, false, `{"status":"REFUSED","request_id":"r","finish_reason":"stop"}`},
		{"unavailable_wrong_finish", Text, false, `{"status":"UNAVAILABLE","request_id":"r","finish_reason":"stop"}`},
		{"truncated_structured", Structured, false, strings.Replace(strings.Replace(answer, `"COMPLETED"`, `"TRUNCATED"`, 1), `"stop"`, `"length"`, 1)},
		{"truncated_tools", ToolProposals, false, strings.Replace(strings.Replace(tools, `"COMPLETED"`, `"TRUNCATED"`, 1), `"stop"`, `"length"`, 1)},
		{"truncated_wrong_finish", Text, false, strings.Replace(text, `"COMPLETED"`, `"TRUNCATED"`, 1)},
		{"answer_unknown", Structured, false, strings.Replace(answer, `"entity_refs":`, `"confirmed":true,"entity_refs":`, 1)},
		{"answer_duplicate", Structured, false, strings.Replace(answer, `"answer":`, `"answer":"evil","answer":`, 1)},
		{"answer_blank", Structured, false, strings.Replace(answer, `"synthetic"`, `" "`, 1)},
		{"answer_null_refs", Structured, false, strings.Replace(answer, `[{"type":"ACTIVITY","id":"`+fixtureEntity+`"}]`, `null`, 1)},
		{"entity_unknown", Structured, false, strings.Replace(answer, `"id":"`+fixtureEntity+`"`, `"id":"`+fixtureEntity+`","verified":true`, 1)},
		{"entity_type", Structured, false, strings.Replace(answer, `"ACTIVITY"`, `"PRIVATE_PERSON"`, 1)},
		{"entity_id", Structured, false, strings.Replace(answer, fixtureEntity, "https://private.invalid", 1)},
		{"entity_duplicate", Structured, false, strings.Replace(answer, `[{"type":"ACTIVITY","id":"`+fixtureEntity+`"}]`, `[{"type":"ACTIVITY","id":"`+fixtureEntity+`"},{"type":"ACTIVITY","id":"`+fixtureEntity+`"}]`, 1)},
		{"tool_unknown", ToolProposals, false, strings.Replace(tools, `"activity.search"`, `"message.send"`, 1)},
		{"tool_case", ToolProposals, false, strings.Replace(tools, `"tool":`, `"Tool":`, 1)},
		{"tool_unknown_receipt", ToolProposals, false, strings.Replace(tools, `"arguments":`, `"executed":true,"arguments":`, 1)},
		{"tool_unknown_argument", ToolProposals, false, strings.Replace(tools, `"query":"合成羽毛球"`, `"query":"合成羽毛球","approved":true`, 1)},
		{"tool_duplicate_argument", ToolProposals, false, strings.Replace(tools, `"query":"合成羽毛球"`, `"query":"evil","query":"合成羽毛球"`, 1)},
		{"tool_nonstring_argument", ToolProposals, false, strings.Replace(tools, `"query":"合成羽毛球"`, `"query":5`, 1)},
		{"tool_blank_argument", ToolProposals, false, strings.Replace(tools, `"query":"合成羽毛球"`, `"query":" "`, 1)},
		{"tool_url_city", ToolProposals, false, strings.Replace(tools, `"query":"合成羽毛球"`, `"query":"合成羽毛球","city_id":"https://private.invalid"`, 1)},
		{"tool_oversized_city", ToolProposals, false, strings.Replace(tools, `"query":"合成羽毛球"`, `"query":"合成羽毛球","city_id":"`+strings.Repeat("x", 81)+`"`, 1)},
		{"tool_detail_id", ToolProposals, false, strings.Replace(strings.Replace(tools, `"activity.search"`, `"activity.detail"`, 1), `"query":"合成羽毛球"`, `"activity_id":"not-an-id"`, 1)},
		{"tool_reason_null", ToolProposals, false, strings.Replace(tools, `"仅提议"`, `null`, 1)},
		{"tool_reason_oversized", ToolProposals, false, strings.Replace(tools, `"仅提议"`, `"`+strings.Repeat("x", 501)+`"`, 1)},
		{"candidate_active", Structured, true, strings.Replace(candidate, `"CANDIDATE"`, `"ACTIVE"`, 1)},
		{"candidate_confirmed", Structured, true, strings.Replace(candidate, `"predicate":`, `"confirmed":true,"predicate":`, 1)},
		{"candidate_duplicate", Structured, true, strings.Replace(candidate, `"predicate":`, `"predicate":"PRIVATE_TRAIT","predicate":`, 1)},
		{"candidate_sensitive_predicate", Structured, true, strings.Replace(candidate, `"ACTIVITY_CATEGORY"`, `"HEALTH_CONDITION"`, 1)},
		{"candidate_sensitive_value", Structured, true, strings.Replace(candidate, `"badminton"`, `"depression"`, 1)},
		{"candidate_unknown_value", Structured, true, strings.Replace(candidate, `"badminton"`, `"fictional-category"`, 1)},
		{"candidate_wrong_attribution", Structured, true, strings.Replace(candidate, `"UNVERIFIED"`, `"VERIFIED_OWNER"`, 1)},
		{"candidate_no_sources", Structured, true, strings.Replace(candidate, `["`+fixtureContext+`"]`, `[]`, 1)},
		{"candidate_source_url", Structured, true, strings.Replace(candidate, fixtureContext, "https://private.invalid", 1)},
		{"candidate_source_duplicate", Structured, true, strings.Replace(candidate, `["`+fixtureContext+`"]`, `["`+fixtureContext+`","`+fixtureContext+`"]`, 1)},
		{"array", Text, false, `[]`}, {"null", Text, false, `null`}, {"malformed", Text, false, `{`},
		{"trailing", Text, false, text + `{}`}, {"oversized_body", Text, false, text + strings.Repeat(" ", MaxResultBytes)},
		{"invalid_utf8", Text, false, strings.Replace(text, "synthetic", string([]byte{0xff}), 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := testRequest(tc.mode)
			if tc.memory {
				r.TaskKind = MemoryCandidateExtraction
				r.OutputSchemaVersion = "air.candidate_proposal.v1"
				r.ToolAllowlist = nil
			}
			h, _ := harness(t, tc.raw)
			got, err := h.Complete(context.Background(), r)
			if !errors.Is(err, ErrAdapter) || got.Status != Invalid || got.Text != "" || got.Answer != nil || got.Candidate != nil || len(got.ToolProposals) != 0 || got.ProviderID != "" || got.ProviderRequestID != "" || got.Usage.Status != "UNKNOWN" {
				t.Fatal("ambiguous/untrusted payload escaped invalid result", err)
			}
		})
	}
}

func TestProviderUsageIsExplicitBoundedAndNeverFabricatesAccounting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage string
		valid bool
		known bool
	}{
		{"missing", "", true, false}, {"explicit_unknown", `{"status":"UNKNOWN"}`, true, false},
		{"real_zero_fixture", `{"status":"KNOWN","input_tokens":0,"output_tokens":0}`, true, true},
		{"budget_boundary", `{"status":"KNOWN","input_tokens":1000000,"output_tokens":128}`, true, true},
		{"null", `null`, false, false}, {"unknown_with_zero", `{"status":"UNKNOWN","input_tokens":0}`, false, false},
		{"missing_count", `{"status":"KNOWN","input_tokens":1}`, false, false},
		{"null_count", `{"status":"KNOWN","input_tokens":null,"output_tokens":1}`, false, false},
		{"fraction", `{"status":"KNOWN","input_tokens":1.1,"output_tokens":1}`, false, false},
		{"negative", `{"status":"KNOWN","input_tokens":-1,"output_tokens":1}`, false, false},
		{"negative_output", `{"status":"KNOWN","input_tokens":1,"output_tokens":-1}`, false, false},
		{"oversized_input", `{"status":"KNOWN","input_tokens":1000001,"output_tokens":1}`, false, false},
		{"exceeded_budget", `{"status":"KNOWN","input_tokens":1,"output_tokens":129}`, false, false},
		{"cost_claim", `{"status":"KNOWN","input_tokens":1,"output_tokens":1,"cost":0}`, false, false},
		{"case_alias", `{"Status":"KNOWN","input_tokens":1,"output_tokens":1}`, false, false},
		{"duplicate", `{"status":"UNKNOWN","status":"KNOWN","input_tokens":1,"output_tokens":1}`, false, false},
		{"unknown_status", `{"status":"ESTIMATED"}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"status":"COMPLETED","request_id":"usage","finish_reason":"stop","text":"synthetic"`
			if tc.usage != "" {
				raw += `,"usage":` + tc.usage
			}
			raw += `}`
			h, _ := harness(t, raw)
			result, err := h.Complete(context.Background(), testRequest(Text))
			if tc.valid {
				if err != nil || result.Usage.CostStatus != "UNKNOWN" {
					t.Fatal("valid usage failed", err)
				}
				if tc.known {
					if result.Usage.Status != "KNOWN" || result.Usage.InputTokens == nil || result.Usage.OutputTokens == nil {
						t.Fatal("explicit counts lost")
					}
				} else if result.Usage.Status != "UNKNOWN" || result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
					t.Fatal("missing accounting fabricated")
				}
			} else if !errors.Is(err, ErrAdapter) || result.Usage.Status != "UNKNOWN" || result.Text != "" {
				t.Fatal("invalid usage released result", err)
			}
		})
	}
}

func TestClosedNestedCollectionBoundsAndAllCandidateValues(t *testing.T) {
	for _, value := range []string{"badminton", "basketball", "football", "sports", "culture"} {
		t.Run(value, func(t *testing.T) {
			raw := []byte(`{"status":"CANDIDATE","predicate":"ACTIVITY_CATEGORY","value":"` + value + `","source_ref_ids":["` + fixtureContext + `"],"subject_attribution":"UNVERIFIED"}`)
			proposal, err := decodeCandidate(raw)
			if err != nil || proposal.Value != value || proposal.Status != "CANDIDATE" {
				t.Fatal("closed non-sensitive proposal failed", err)
			}
		})
	}
	t.Run("candidate_source_bound", func(t *testing.T) {
		ids := make([]string, 9)
		for i := range ids {
			ids[i] = fmt.Sprintf("74000000-0000-4000-8000-%012d", i+20)
		}
		raw, _ := json.Marshal(CandidateProposal{Status: "CANDIDATE", Predicate: "ACTIVITY_CATEGORY", Value: "sports", SourceRefIDs: ids, SubjectAttribution: "UNVERIFIED"})
		if _, err := decodeCandidate(raw); !errors.Is(err, ErrAdapter) {
			t.Fatal("source collection unbounded")
		}
	})
	t.Run("entity_bound", func(t *testing.T) {
		refs := make([]EntityProposal, 33)
		for i := range refs {
			refs[i] = EntityProposal{Type: "ACTIVITY", ID: fmt.Sprintf("74000000-0000-4000-8000-%012d", i+20)}
		}
		raw, _ := json.Marshal(AnswerProposal{Answer: "synthetic", EntityRefs: refs})
		if _, err := decodeAnswer(raw); !errors.Is(err, ErrAdapter) {
			t.Fatal("entity collection unbounded")
		}
	})
	for _, n := range []int{0, 5} {
		t.Run(fmt.Sprintf("tool_bound_%d", n), func(t *testing.T) {
			proposals := make([]ToolProposal, n)
			for i := range proposals {
				proposals[i] = ToolProposal{Tool: "activity.search", Arguments: map[string]string{"query": "synthetic"}, ReasonSummary: "synthetic"}
			}
			raw, _ := json.Marshal(proposals)
			if _, err := decodeTools(raw, []string{"activity.search"}); !errors.Is(err, ErrAdapter) {
				t.Fatal("tool collection unbounded")
			}
		})
	}
	t.Run("tool_not_allowed_for_this_request", func(t *testing.T) {
		raw := []byte(`[{"tool":"activity.search","arguments":{"query":"synthetic"},"reason_summary":"synthetic"}]`)
		if _, err := decodeTools(raw, []string{"activity.detail"}); !errors.Is(err, ErrAdapter) {
			t.Fatal("request-specific allowlist bypassed")
		}
	})
}

func TestStandardRequestJSONUsesClosedParserAndTypedInputsRemainBounded(t *testing.T) {
	r := testRequest(Text)
	raw, _ := json.Marshal(r)
	var got Request
	if err := json.Unmarshal(raw, &got); err != nil || got.Agent != r.Agent {
		t.Fatal("standard closed request roundtrip", err)
	}
	unknown := bytes.Replace(raw, []byte(`"run_id":`), []byte(`"approved":true,"run_id":`), 1)
	if err := json.Unmarshal(unknown, &got); !errors.Is(err, ErrInvalid) || got.RunID != "" {
		t.Fatal("standard decoder bypassed unknown-field guard", err)
	}
	var nilRequest *Request
	if err := nilRequest.UnmarshalJSON(raw); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil request receiver accepted")
	}
	if _, err := DecodeRequest(raw, time.Time{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing clock accepted")
	}
	r = testRequest(Text)
	r.Messages[1].Content = string([]byte{0xff})
	if !errors.Is(ValidateRequest(r, testNow()), ErrInvalid) {
		t.Fatal("typed invalid UTF-8 accepted")
	}
	r = testRequest(Text)
	r.Messages = nil
	for i := 0; i < 3; i++ {
		r.Messages = append(r.Messages, Message{"user", "x" + strings.Repeat("\x01", 4095)})
	}
	if !errors.Is(ValidateRequest(r, testNow()), ErrInvalid) {
		t.Fatal("JSON escaping bypassed wire byte bound")
	}
}

func TestAdapterNilContextsDeadlinesAndPointerErrorsAreSafe(t *testing.T) {
	for _, adapter := range []ProviderAdapter{nil, (*fakeAdapter)(nil)} {
		if _, err := NewOfflineHarness(adapter); !errors.Is(err, ErrUnavailable) {
			t.Fatal("nil adapter accepted")
		}
	}
	var h *OfflineHarness
	if _, err := h.Complete(context.Background(), testRequest(Text)); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil harness accepted")
	}
	g := NewGateway(nil)
	if _, err := g.Complete(nil, testRequest(Text)); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil gateway context accepted")
	}
	h, f := harness(t, `{}`)
	if _, err := h.Complete(nil, testRequest(Text)); !errors.Is(err, ErrInvalid) || f.calls != 0 {
		t.Fatal("nil context dispatched")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Complete(ctx, testRequest(Text)); !errors.Is(err, context.Canceled) {
		t.Fatal("gateway cancellation ignored")
	}
	r := testRequest(Text)
	r.RunID = "provider-id"
	if _, err := g.Complete(context.Background(), r); !errors.Is(err, ErrInvalid) {
		t.Fatal("default gateway accepted invalid identity")
	}
	t.Run("real_child_deadline", func(t *testing.T) {
		h, f := harness(t, `{"status":"COMPLETED","request_id":"late","finish_reason":"stop","text":"do not release"}`)
		r := testRequest(Text)
		r.DeadlineAt = time.Now().Add(20 * time.Millisecond)
		f.hook = func(ctx context.Context) { <-ctx.Done() }
		result, err := h.Complete(context.Background(), r)
		if !errors.Is(err, ErrDeadline) || result.Text != "" {
			t.Fatal("adapter ignored actual deadline and released body", err)
		}
	})
	for _, tc := range []struct {
		err   error
		code  string
		retry bool
	}{
		{&ProviderError{Code: "TEMPORARY"}, "TEMPORARY", true},
		{fmt.Errorf("wrapped private: %w", &ProviderError{Code: "INVALID_REQUEST", Retryable: true}), "INVALID_REQUEST", false},
		{ProviderError{Code: "REFUSED", Retryable: true}, "REFUSED", false},
		{context.Canceled, "CANCELLED", false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			got := normalizedProviderError(tc.err)
			if got.Code != tc.code || got.Retryable != tc.retry || strings.Contains(got.Error(), "private") {
				t.Fatal("wrapped/pointer provider error bypassed redaction")
			}
		})
	}
}

func TestOfflineAdapterConcurrentCallsKeepEachNativeRequest(t *testing.T) {
	h, f := harness(t, `{"status":"COMPLETED","request_id":"fixture-concurrent","finish_reason":"stop","text":"synthetic"}`)
	var wg sync.WaitGroup
	failures := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			r := testRequest(Text)
			r.RunID = fmt.Sprintf("74000000-0000-4000-8000-%012d", index+100)
			result, err := h.Complete(context.Background(), r)
			if err != nil {
				failures <- err
				return
			}
			if result.RunID != r.RunID || result.Agent != r.Agent || result.Mode != OfflineContract {
				failures <- errors.New("concurrent native request identity changed")
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if f.calls != 32 {
		t.Fatal("concurrent fixture call count", f.calls)
	}
}
