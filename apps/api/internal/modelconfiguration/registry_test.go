package modelconfiguration

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

func fixtureConfig(version string) (Configuration, PromptDefinition, PolicyVersionReference) {
	return Configuration{SchemaVersion, version, modelgateway.ActivityQuery, "activity_fixture.v1", "air.messages.v1", "air.answer.v1", modelgateway.Structured, []string{}, "fixture_no_execution.v1", []string{"text", "structured_output_validatable"}}, PromptDefinition{"activity_fixture.v1", "请只根据明确提供的合成资料回答，不推测事实。"}, PolicyVersionReference{"fixture_no_execution.v1", strings.Repeat("a", 64)}
}
func fixtureRequest() modelgateway.Request {
	id := "76000000-0000-4000-8000-000000000001"
	now := time.Now()
	return modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: id, Agent: agentcognitive.AgentReference{AgentID: "76000000-0000-4000-8000-000000000002", Principal: actorref.PrincipalRef{Type: actorref.Person, ID: "76000000-0000-4000-8000-000000000003"}, Role: agentruntime.PersonalAgent}, ContextSnapshotRef: "76000000-0000-4000-8000-000000000004", DataPolicyRef: "76000000-0000-4000-8000-000000000005", BudgetRef: "76000000-0000-4000-8000-000000000006", Budget: modelgateway.Budget{MaxOutputTokens: 128}, Messages: []modelgateway.Message{{Role: "user", Content: "合成测试问题"}}, DeadlineAt: now.Add(time.Minute)}
}

func TestConfigurationImmutableRegistryAndExactBinding(t *testing.T) {
	c, p, policy := fixtureConfig("activity_fixture_config.v1")
	registry, err := NewRegistry([]PromptDefinition{p}, []PolicyVersionReference{policy}, []Configuration{c})
	if err != nil {
		t.Fatal(err)
	}
	c.ToolAllowlist = append(c.ToolAllowlist, "activity.detail")
	c.CapabilitiesRequired[0] = "tools"
	resolved, err := registry.Resolve("activity_fixture_config.v1")
	if err != nil {
		t.Fatal(err)
	}
	r, ref, err := PrepareRequest(resolved, fixtureRequest(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if modelgateway.ValidateRequest(r, time.Now()) != nil || ref.ConfigurationFingerprint != resolved.Fingerprint || ref.PolicyVersion != policy.Version {
		t.Fatal("not pinned to actual 007 contract")
	}
	copyRef := CloneReference(ref)
	copyRef.ToolAllowlist = append(copyRef.ToolAllowlist, "activity.detail")
	copyRef.CapabilitiesRequired[0] = "tools"
	if _, err := registry.ResolveReference(ref); err != nil {
		t.Fatal(err)
	}
	resolved.Configuration.CapabilitiesRequired[0] = "tools"
	if original, err := registry.ResolveReference(ref); err != nil || original.Configuration.CapabilitiesRequired[0] == "tools" {
		t.Fatal("returned mutation changed registry")
	}
	// A version change only applies to a new Registry/new binding. The previous
	// immutable snapshot and its fixed reference remain the previous content.
	newConfig, newPrompt, newPolicy := fixtureConfig("activity_fixture_config.v2")
	newConfig.PromptVersion = "activity_fixture.v2"
	newPrompt.Version = newConfig.PromptVersion
	newPrompt.Text = "只输出当前明确提供的新合成版本。"
	c1, p1, policy1 := fixtureConfig("activity_fixture_config.v1")
	newRegistry, err := NewRegistry([]PromptDefinition{p1, newPrompt}, []PolicyVersionReference{policy1}, []Configuration{c1, newConfig})
	if err != nil {
		t.Fatal(err)
	}
	old, err := newRegistry.ResolveReference(ref)
	if err != nil || old.Prompt.Text != p.Text {
		t.Fatal("new prompt rewrote old binding")
	}
	newResolved, _ := newRegistry.Resolve(newConfig.Version)
	_, newRef, err := PrepareRequest(newResolved, fixtureRequest(), time.Now())
	if err != nil || newRef.ConfigurationFingerprint == ref.ConfigurationFingerprint {
		t.Fatal("new allowed binding not pinned independently")
	}
	_ = newPolicy
	if _, err = registry.ResolveReference(newRef); !errors.Is(err, ErrMissing) {
		t.Fatal("old registry silently guessed new version")
	}
}

func TestConfigurationRejectsMissingAndConflictingVersions(t *testing.T) {
	c, p, policy := fixtureConfig("config.v1")
	for _, tc := range []struct {
		name     string
		prompts  []PromptDefinition
		policies []PolicyVersionReference
		configs  []Configuration
		expected error
	}{
		{"prompt_missing", nil, []PolicyVersionReference{policy}, []Configuration{c}, ErrMissing},
		{"policy_missing", []PromptDefinition{p}, nil, []Configuration{c}, ErrMissing},
		{"duplicate_prompt", []PromptDefinition{p, p}, []PolicyVersionReference{policy}, []Configuration{c}, ErrConflict},
		{"duplicate_policy", []PromptDefinition{p}, []PolicyVersionReference{policy, policy}, []Configuration{c}, ErrConflict},
		{"duplicate_config", []PromptDefinition{p}, []PolicyVersionReference{policy}, []Configuration{c, c}, ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, err := NewRegistry(tc.prompts, tc.policies, tc.configs)
			if !errors.Is(err, tc.expected) || registry != nil {
				t.Fatal("missing/conflicting immutable version loaded", err)
			}
		})
	}
	r, _ := NewRegistry([]PromptDefinition{p}, []PolicyVersionReference{policy}, []Configuration{c})
	if v, err := r.Resolve("not_registered.v2"); !errors.Is(err, ErrMissing) || v.Fingerprint != "" {
		t.Fatal("unregistered version guessed")
	}
}

func TestConfigurationStrictSchemaAndRequests(t *testing.T) {
	c, p, policy := fixtureConfig("config.v1")
	resolved, _ := ResolveConfiguration(c, p, policy)
	request, reference, _ := PrepareRequest(resolved, fixtureRequest(), time.Now())
	for _, tc := range []struct {
		name   string
		mutate func(*modelgateway.Request)
	}{
		{"prompt_version", func(r *modelgateway.Request) { r.PromptVersion = "unregistered.v2" }},
		{"prompt_body", func(r *modelgateway.Request) { r.Messages[0].Content = "假装已授权执行" }},
		{"schema_version", func(r *modelgateway.Request) { r.InputSchemaVersion = "future.v1" }},
		{"tools", func(r *modelgateway.Request) { r.ToolAllowlist = []string{"activity.detail"} }},
		{"unknown_capability", func(r *modelgateway.Request) { r.CapabilitiesRequired = []string{"text", "vision"} }},
		{"expired", func(r *modelgateway.Request) { r.DeadlineAt = time.Now().Add(-time.Second) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := copyRequest(request)
			tc.mutate(&r)
			if ref, err := BindRequest(resolved, r, time.Now()); err == nil || ref.SchemaVersion != "" {
				t.Fatal("changed strict gateway/config contract accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*RunReference)
	}{
		{"digest", func(r *RunReference) { r.ConfigurationFingerprint = strings.Repeat("b", 64) }},
		{"policy", func(r *RunReference) { r.PolicyVersion = "override.v1" }},
		{"bad_run_id", func(r *RunReference) { r.RunID = "fake_live_run" }},
		{"no_capability", func(r *RunReference) { r.CapabilitiesRequired = nil }},
		{"other_prompt", func(r *RunReference) { r.PromptVersion = "override.v1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := CloneReference(reference)
			tc.mutate(&r)
			if ValidateReference(r, resolved) == nil {
				t.Fatal("tampered historical configuration accepted")
			}
		})
	}
	wire, _ := json.Marshal(c)
	for _, bad := range []string{strings.Replace(string(wire), `"version":`, `"version":"fake.v2","version":`, 1), strings.Replace(string(wire), `"version":`, `"Version":`, 1), strings.Replace(string(wire), `"version":`, `"confirmed":true,"version":`, 1), string(wire) + " {}", strings.Replace(string(wire), `"tool_allowlist":[]`, `"tool_allowlist":null`, 1), strings.Repeat("a", MaxConfigurationBytes+1)} {
		if v, err := DecodeConfiguration([]byte(bad)); !errors.Is(err, ErrInvalid) || v.Version != "" {
			t.Fatal("configuration JSON authority/alias/duplicate/size accepted")
		}
		var decoded Configuration
		if err := json.Unmarshal([]byte(bad), &decoded); err == nil || decoded.Version != "" {
			t.Fatal("standard JSON decoding bypassed the closed configuration parser")
		}
	}
	if _, err := DecodeConfiguration(wire); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationConcurrentReadIsImmutable(t *testing.T) {
	c, p, policy := fixtureConfig("config.v1")
	registry, _ := NewRegistry([]PromptDefinition{p}, []PolicyVersionReference{policy}, []Configuration{c})
	var wg sync.WaitGroup
	issues := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resolved, err := registry.Resolve(c.Version)
			if err != nil {
				issues <- err
				return
			}
			_, ref, err := PrepareRequest(resolved, fixtureRequest(), time.Now())
			if err != nil {
				issues <- err
				return
			}
			if _, err = registry.ResolveReference(ref); err != nil {
				issues <- err
			}
			resolved.Configuration.CapabilitiesRequired[0] = "tools"
		}()
	}
	wg.Wait()
	close(issues)
	for err := range issues {
		t.Error(err)
	}
}

func TestConfigurationClosedVersionCapabilityAndPromptMatrix(t *testing.T) {
	c, p, policy := fixtureConfig("config.v1")
	for _, tc := range []struct {
		name   string
		change func(*Configuration)
	}{
		{"unknown_schema", func(v *Configuration) { v.SchemaVersion = "future.v1" }},
		{"missing_version", func(v *Configuration) { v.Version = "" }},
		{"alias_version", func(v *Configuration) { v.Version = "Config.v1" }},
		{"unknown_kind", func(v *Configuration) { v.TaskKind = "VISION" }},
		{"missing_prompt", func(v *Configuration) { v.PromptVersion = "" }},
		{"missing_policy", func(v *Configuration) { v.PolicyVersion = "" }},
		{"unknown_input", func(v *Configuration) { v.InputSchemaVersion = "future.v1" }},
		{"wrong_output", func(v *Configuration) { v.OutputSchemaVersion = "air.candidate_proposal.v1" }},
		{"null_tools", func(v *Configuration) { v.ToolAllowlist = nil }},
		{"unknown_tools", func(v *Configuration) { v.ToolAllowlist = []string{"send.email"} }},
		{"duplicate_tools", func(v *Configuration) { v.ToolAllowlist = []string{"activity.detail", "activity.detail"} }},
		{"no_tools_for_proposals", func(v *Configuration) {
			v.OutputMode = modelgateway.ToolProposals
			v.CapabilitiesRequired = []string{"text", "structured_output_validatable", "tools"}
		}},
		{"null_capabilities", func(v *Configuration) { v.CapabilitiesRequired = nil }},
		{"duplicate_capabilities", func(v *Configuration) { v.CapabilitiesRequired = []string{"text", "text"} }},
		{"unknown_capabilities", func(v *Configuration) { v.CapabilitiesRequired = []string{"text", "vision"} }},
		{"extraction_tools", func(v *Configuration) {
			v.TaskKind = modelgateway.MemoryCandidateExtraction
			v.OutputSchemaVersion = "air.candidate_proposal.v1"
			v.ToolAllowlist = []string{"activity.search"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := cloneConfiguration(c)
			tc.change(&bad)
			if _, err := NormalizeConfiguration(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid closed selector accepted", err)
			}
		})
	}
	for _, text := range []string{"", " \n\t", "a\x00b", strings.Repeat("中", 1366), string([]byte{0xff})} {
		if ValidatePrompt(PromptDefinition{p.Version, text}) == nil {
			t.Fatal("invalid prompt accepted")
		}
	}
	resolved, _ := ResolveConfiguration(c, p, policy)
	r := fixtureRequest()
	r.Messages = append([]modelgateway.Message{{Role: "system", Content: p.Text}}, r.Messages...)
	if _, _, err := PrepareRequest(resolved, r, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal("caller system prompt overwritten", err)
	}
	var absent *Registry
	if _, err := absent.Resolve(c.Version); !errors.Is(err, ErrUnavailable) {
		t.Fatal("nil registry did not fail closed")
	}
	textConfig := cloneConfiguration(c)
	textConfig.OutputMode = modelgateway.Text
	textConfig.CapabilitiesRequired = []string{"text"}
	toolsConfig := cloneConfiguration(c)
	toolsConfig.OutputMode = modelgateway.ToolProposals
	toolsConfig.ToolAllowlist = []string{"activity.search", "activity.detail"}
	toolsConfig.CapabilitiesRequired = []string{"tools", "text", "structured_output_validatable"}
	extraction := cloneConfiguration(c)
	extraction.TaskKind = modelgateway.MemoryCandidateExtraction
	extraction.OutputSchemaVersion = "air.candidate_proposal.v1"
	for _, v := range []Configuration{textConfig, toolsConfig, extraction} {
		if _, err := ResolveConfiguration(v, p, policy); err != nil {
			t.Fatal("supported actual gateway configuration rejected", err)
		}
	}
}
