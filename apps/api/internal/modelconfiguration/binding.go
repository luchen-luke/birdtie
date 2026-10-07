package modelconfiguration

import (
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

const ReferenceVersion = "air.model_configuration_reference.v1"

// RunReference is a pinned configuration contract, not an AgentRun ledger row,
// a processed run, source/egress/consent authority or an execution receipt.
type RunReference struct {
	SchemaVersion            string
	RunID                    string
	Agent                    agentcognitive.AgentReference
	ConfigurationVersion     string
	ConfigurationFingerprint string
	PromptVersion            string
	InputSchemaVersion       string
	OutputSchemaVersion      string
	ToolAllowlist            []string
	PolicyVersion            string
	CapabilitiesRequired     []string
	TaskKind                 modelgateway.TaskKind
	OutputMode               modelgateway.OutputMode
}

func copyRequest(r modelgateway.Request) modelgateway.Request {
	r.Messages = append([]modelgateway.Message{}, r.Messages...)
	r.ToolAllowlist = append([]string{}, r.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string{}, r.CapabilitiesRequired...)
	return r
}
func CloneReference(r RunReference) RunReference {
	r.ToolAllowlist = append([]string{}, r.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string{}, r.CapabilitiesRequired...)
	return r
}

func referenceFor(r modelgateway.Request, c ResolvedConfiguration) RunReference {
	v := c.Configuration
	return RunReference{ReferenceVersion, r.RunID, r.Agent, v.Version, c.Fingerprint, v.PromptVersion, v.InputSchemaVersion, v.OutputSchemaVersion, append([]string{}, v.ToolAllowlist...), v.PolicyVersion, append([]string{}, v.CapabilitiesRequired...), v.TaskKind, v.OutputMode}
}

// BindRequest refuses a missing or changed version before any model task can
// begin. Current request messages must contain the central prompt exactly;
// a claimed prompt_version cannot substitute new hidden system instructions.
func BindRequest(c ResolvedConfiguration, r modelgateway.Request, now time.Time) (RunReference, error) {
	actual, err := ResolveConfiguration(c.Configuration, c.Prompt, c.Policy)
	if err != nil || actual.Fingerprint != c.Fingerprint {
		return RunReference{}, ErrInvalid
	}
	if modelgateway.ValidateRequest(r, now) != nil {
		return RunReference{}, ErrInvalid
	}
	normalized, err := NormalizeConfiguration(Configuration{SchemaVersion: SchemaVersion, Version: c.Configuration.Version, TaskKind: r.TaskKind, PromptVersion: r.PromptVersion, InputSchemaVersion: r.InputSchemaVersion, OutputSchemaVersion: r.OutputSchemaVersion, OutputMode: r.OutputMode, ToolAllowlist: r.ToolAllowlist, PolicyVersion: c.Configuration.PolicyVersion, CapabilitiesRequired: r.CapabilitiesRequired})
	if err != nil || !reflect.DeepEqual(normalized, c.Configuration) || len(r.Messages) == 0 || r.Messages[0].Role != "system" || r.Messages[0].Content != c.Prompt.Text {
		return RunReference{}, ErrDenied
	}
	return referenceFor(r, c), nil
}

func (registry *Registry) Bind(version string, r modelgateway.Request, now time.Time) (RunReference, error) {
	c, err := registry.Resolve(version)
	if err != nil {
		return RunReference{}, err
	}
	return BindRequest(c, r, now)
}

// PrepareRequest applies registered static fields and central prompt to a
// server-assembled request. It cannot fetch context or authorize its messages.
// Unknown configuration fails; a caller-provided system message is rejected.
func PrepareRequest(c ResolvedConfiguration, r modelgateway.Request, now time.Time) (modelgateway.Request, RunReference, error) {
	for _, m := range r.Messages {
		if m.Role == "system" {
			return modelgateway.Request{}, RunReference{}, ErrDenied
		}
	}
	r = copyRequest(r)
	v := c.Configuration
	r.TaskKind, r.PromptVersion, r.InputSchemaVersion, r.OutputSchemaVersion, r.OutputMode = v.TaskKind, v.PromptVersion, v.InputSchemaVersion, v.OutputSchemaVersion, v.OutputMode
	r.ToolAllowlist = append([]string{}, v.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string{}, v.CapabilitiesRequired...)
	r.Messages = append([]modelgateway.Message{{Role: "system", Content: c.Prompt.Text}}, r.Messages...)
	ref, err := BindRequest(c, r, now)
	if err != nil {
		return modelgateway.Request{}, RunReference{}, err
	}
	return r, ref, nil
}

func ValidateReference(reference RunReference, c ResolvedConfiguration) error {
	ref, idErr := actorref.Parse("PERSON", reference.RunID)
	if idErr != nil || ref.ID != reference.RunID || reference.RunID == "00000000-0000-0000-0000-000000000000" {
		return ErrInvalid
	}
	resolved, err := ResolveConfiguration(c.Configuration, c.Prompt, c.Policy)
	if err != nil || resolved.Fingerprint != c.Fingerprint || reference.SchemaVersion != ReferenceVersion || !ValidDigest(reference.ConfigurationFingerprint) || agentcognitive.ValidateAgentReference(reference.Agent) != nil {
		return ErrInvalid
	}
	// Native IDs are selectors here. The actual persistent store rechecks the
	// session, exact Agent/owner, source and metadata before returning a binding.
	expected := referenceFor(modelgateway.Request{RunID: reference.RunID, Agent: reference.Agent}, c)
	if !reflect.DeepEqual(reference, expected) {
		return ErrDenied
	}
	return nil
}

func (registry *Registry) ResolveReference(reference RunReference) (ResolvedConfiguration, error) {
	c, err := registry.Resolve(reference.ConfigurationVersion)
	if err != nil {
		return ResolvedConfiguration{}, err
	}
	if err = ValidateReference(reference, c); err != nil {
		return ResolvedConfiguration{}, err
	}
	return c, nil
}
