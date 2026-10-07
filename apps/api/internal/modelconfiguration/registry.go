// Package modelconfiguration pins centrally registered model configuration
// metadata. It grants no identity, source, budget, provider or execution access.
package modelconfiguration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

const SchemaVersion = "air.model_configuration.v1"
const MaxConfigurationBytes = 16 * 1024

var (
	ErrInvalid     = errors.New("invalid model configuration")
	ErrMissing     = errors.New("model configuration version missing")
	ErrConflict    = errors.New("immutable model configuration conflict")
	ErrDenied      = errors.New("model configuration binding denied")
	ErrUnavailable = errors.New("model configuration execution unavailable")
	versionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)
)

type PromptDefinition struct {
	Version string `json:"version"`
	Text    string `json:"text"`
}

// A policy artifact reference is an immutable configuration dependency, never
// proof that the policy was evaluated or a runtime/model export was authorized.
type PolicyVersionReference struct {
	Version        string `json:"version"`
	ArtifactSHA256 string `json:"artifact_sha256"`
}

type Configuration struct {
	SchemaVersion        string                  `json:"schema_version"`
	Version              string                  `json:"version"`
	TaskKind             modelgateway.TaskKind   `json:"task_kind"`
	PromptVersion        string                  `json:"prompt_version"`
	InputSchemaVersion   string                  `json:"input_schema_version"`
	OutputSchemaVersion  string                  `json:"output_schema_version"`
	OutputMode           modelgateway.OutputMode `json:"output_mode"`
	ToolAllowlist        []string                `json:"tool_allowlist"`
	PolicyVersion        string                  `json:"policy_version"`
	CapabilitiesRequired []string                `json:"capabilities_required"`
}

type ResolvedConfiguration struct {
	Configuration Configuration
	Prompt        PromptDefinition
	Policy        PolicyVersionReference
	Fingerprint   string
}

// Registry is immutable once loaded. It is not persistent run history or an
// activation pointer; native store transactions separately select new bindings.
type Registry struct {
	prompts        map[string]PromptDefinition
	policies       map[string]PolicyVersionReference
	configurations map[string]ResolvedConfiguration
}

func ValidVersion(version string) bool { return versionPattern.MatchString(version) }
func ValidDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size
}

func ValidatePrompt(p PromptDefinition) error {
	if !ValidVersion(p.Version) || len(p.Text) == 0 || len(p.Text) > 4096 || !utf8.ValidString(p.Text) || strings.TrimSpace(p.Text) == "" || strings.ContainsRune(p.Text, 0) {
		return ErrInvalid
	}
	return nil
}

func PromptDigest(p PromptDefinition) string {
	if ValidatePrompt(p) != nil {
		return ""
	}
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func sortedUnique(values []string, allowed map[string]bool, max int) ([]string, error) {
	if values == nil || len(values) > max {
		return nil, ErrInvalid
	}
	copyValues := append([]string{}, values...)
	seen := map[string]bool{}
	for _, v := range copyValues {
		if !allowed[v] || seen[v] {
			return nil, ErrInvalid
		}
		seen[v] = true
	}
	sort.Strings(copyValues)
	return copyValues, nil
}

func NormalizeConfiguration(input Configuration) (Configuration, error) {
	c := input
	if c.SchemaVersion != SchemaVersion || !ValidVersion(c.Version) || !ValidVersion(c.PromptVersion) || !ValidVersion(c.PolicyVersion) || c.InputSchemaVersion != "air.messages.v1" {
		return Configuration{}, ErrInvalid
	}
	switch c.TaskKind {
	case modelgateway.ActivityQuery:
		if c.OutputSchemaVersion != "air.answer.v1" || (c.OutputMode != modelgateway.Text && c.OutputMode != modelgateway.Structured && c.OutputMode != modelgateway.ToolProposals) {
			return Configuration{}, ErrInvalid
		}
	case modelgateway.MemoryCandidateExtraction:
		if c.OutputSchemaVersion != "air.candidate_proposal.v1" || c.OutputMode != modelgateway.Structured || len(c.ToolAllowlist) != 0 {
			return Configuration{}, ErrInvalid
		}
	default:
		return Configuration{}, ErrInvalid
	}
	var err error
	c.ToolAllowlist, err = sortedUnique(c.ToolAllowlist, map[string]bool{"activity.search": true, "activity.detail": true}, 4)
	if err != nil {
		return Configuration{}, err
	}
	if c.OutputMode == modelgateway.ToolProposals && len(c.ToolAllowlist) == 0 {
		return Configuration{}, ErrInvalid
	}
	required := map[string]bool{"text": true}
	if c.OutputMode != modelgateway.Text {
		required["structured_output_validatable"] = true
	}
	if c.OutputMode == modelgateway.ToolProposals {
		required["tools"] = true
	}
	if len(c.CapabilitiesRequired) != len(required) {
		return Configuration{}, ErrInvalid
	}
	c.CapabilitiesRequired, err = sortedUnique(c.CapabilitiesRequired, required, 3)
	if err != nil {
		return Configuration{}, err
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > MaxConfigurationBytes {
		return Configuration{}, ErrInvalid
	}
	return c, nil
}

func cloneConfiguration(c Configuration) Configuration {
	c.ToolAllowlist = append([]string{}, c.ToolAllowlist...)
	c.CapabilitiesRequired = append([]string{}, c.CapabilitiesRequired...)
	return c
}
func cloneResolved(r ResolvedConfiguration) ResolvedConfiguration {
	r.Configuration = cloneConfiguration(r.Configuration)
	return r
}

func ResolveConfiguration(c Configuration, p PromptDefinition, policy PolicyVersionReference) (ResolvedConfiguration, error) {
	normalized, err := NormalizeConfiguration(c)
	if err != nil || ValidatePrompt(p) != nil || !ValidVersion(policy.Version) || !ValidDigest(policy.ArtifactSHA256) {
		return ResolvedConfiguration{}, ErrInvalid
	}
	if normalized.PromptVersion != p.Version || normalized.PolicyVersion != policy.Version {
		return ResolvedConfiguration{}, ErrMissing
	}
	// Fingerprint binds exact centrally registered prompt content and policy
	// artifact identity as well as all immutable configuration selectors.
	raw, _ := json.Marshal(struct {
		Configuration Configuration
		PromptSHA256  string
		Policy        PolicyVersionReference
	}{normalized, PromptDigest(p), policy})
	hash := sha256.Sum256(raw)
	return ResolvedConfiguration{normalized, p, policy, hex.EncodeToString(hash[:])}, nil
}

func NewRegistry(prompts []PromptDefinition, policies []PolicyVersionReference, configs []Configuration) (*Registry, error) {
	if len(prompts) > 256 || len(policies) > 256 || len(configs) > 1024 {
		return nil, ErrInvalid
	}
	r := &Registry{prompts: map[string]PromptDefinition{}, policies: map[string]PolicyVersionReference{}, configurations: map[string]ResolvedConfiguration{}}
	for _, p := range prompts {
		if ValidatePrompt(p) != nil {
			return nil, ErrInvalid
		}
		if _, exists := r.prompts[p.Version]; exists {
			return nil, ErrConflict
		}
		r.prompts[p.Version] = p
	}
	for _, p := range policies {
		if !ValidVersion(p.Version) || !ValidDigest(p.ArtifactSHA256) {
			return nil, ErrInvalid
		}
		if _, exists := r.policies[p.Version]; exists {
			return nil, ErrConflict
		}
		r.policies[p.Version] = p
	}
	for _, c := range configs {
		if _, exists := r.configurations[c.Version]; exists {
			return nil, ErrConflict
		}
		p, present := r.prompts[c.PromptVersion]
		policy, policyPresent := r.policies[c.PolicyVersion]
		if !present || !policyPresent {
			return nil, ErrMissing
		}
		resolved, err := ResolveConfiguration(c, p, policy)
		if err != nil {
			return nil, err
		}
		r.configurations[c.Version] = resolved
	}
	return r, nil
}

func (r *Registry) Resolve(version string) (ResolvedConfiguration, error) {
	if r == nil {
		return ResolvedConfiguration{}, ErrUnavailable
	}
	if !ValidVersion(version) {
		return ResolvedConfiguration{}, ErrInvalid
	}
	found, exists := r.configurations[version]
	if !exists {
		return ResolvedConfiguration{}, ErrMissing
	}
	return cloneResolved(found), nil
}
