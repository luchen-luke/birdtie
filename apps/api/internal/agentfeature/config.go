// Package agentfeature supplies server-owned rollout brakes. A feature flag is
// never authentication, a source version, consent or permission to use a tool.
package agentfeature

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const SchemaVersion = "agent-feature-flags-v1"
const EnvironmentKey = "BIRDTIE_AGENT_FEATURE_FLAGS"
const MaxConfigBytes = 8 * 1024

type Feature string

const (
	Enrichment      Feature = "agent_enrichment"
	Memory          Feature = "agent_memory"
	AttentionPolicy Feature = "agent_attention_policy"
	SocialPolicy    Feature = "agent_social_policy"
	LifeMap         Feature = "life_map"
)

var (
	ErrInvalidConfig = errors.New("Agent 功能配置无效")
	ErrUnsafeConfig  = errors.New("Agent 功能配置超出当前安全范围")
	ErrDisabled      = errors.New("该 Agent 功能当前关闭")
	ErrConflict      = errors.New("Agent 功能配置已更新，请重新读取")
	ErrServerOnly    = errors.New("Agent 功能配置仅供服务端管理")
)

var features = [...]Feature{Enrichment, Memory, AttentionPolicy, SocialPolicy, LifeMap}

// Config's private fields cannot be populated by an HTTP/model JSON decoder.
// ParseConfig is intended exclusively for trusted server startup/admin input.
type Config struct {
	flags map[Feature]bool
}

type PilotLimits struct {
	MemoryMode         string
	InferenceMode      string
	AutonomousAction   bool
	SensitiveInference bool
}

func Limits() PilotLimits {
	return PilotLimits{MemoryMode: "basic", InferenceMode: "conservative"}
}

func DefaultConfig() Config {
	flags := make(map[Feature]bool, len(features))
	for _, feature := range features {
		flags[feature] = false
	}
	return Config{flags: flags}
}

func (Config) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (v *Config) UnmarshalJSON([]byte) error {
	*v = Config{}
	return ErrServerOnly
}

func known(feature Feature) bool {
	for _, value := range features {
		if feature == value {
			return true
		}
	}
	return false
}

func (c Config) enabled(feature Feature) bool {
	return known(feature) && c.flags[Enrichment] && c.flags[feature]
}

func (c Config) clone() Config {
	copy := Config{flags: make(map[Feature]bool, len(c.flags))}
	for key, value := range c.flags {
		copy.flags[key] = value
	}
	return copy
}

func (c Config) validate() error {
	if len(c.flags) != len(features) {
		return ErrInvalidConfig
	}
	all := true
	for _, feature := range features {
		value, exists := c.flags[feature]
		if !exists {
			return ErrInvalidConfig
		}
		all = all && value
	}
	if all {
		return ErrUnsafeConfig
	}
	return nil
}

// strictObject rejects duplicate keys at every object boundary, null, scalar,
// extra trailing values and oversized input without including caller text.
func strictObject(body []byte, keys ...string) (map[string]json.RawMessage, error) {
	if len(body) == 0 || len(body) > MaxConfigBytes {
		return nil, ErrInvalidConfig
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrInvalidConfig
	}
	object := make(map[string]json.RawMessage, len(keys))
	for decoder.More() {
		key, keyErr := decoder.Token()
		name, valid := key.(string)
		if keyErr != nil || !valid {
			return nil, ErrInvalidConfig
		}
		if _, duplicate := object[name]; duplicate {
			return nil, ErrInvalidConfig
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInvalidConfig
		}
		object[name] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(object) != len(keys) {
		return nil, ErrInvalidConfig
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, ErrInvalidConfig
	}
	for _, key := range keys {
		if _, exists := object[key]; !exists {
			return nil, ErrInvalidConfig
		}
	}
	return object, nil
}

func ParseConfig(body []byte) (Config, error) {
	object, err := strictObject(body, "schemaVersion", "flags", "pilot")
	if err != nil {
		return Config{}, err
	}
	var version string
	if json.Unmarshal(object["schemaVersion"], &version) != nil || version != SchemaVersion {
		return Config{}, ErrInvalidConfig
	}
	flags, err := strictObject(object["flags"], "agent_enrichment", "agent_memory", "agent_attention_policy", "agent_social_policy", "life_map")
	if err != nil {
		return Config{}, err
	}
	config := DefaultConfig()
	for _, feature := range features {
		var value bool
		if json.Unmarshal(flags[string(feature)], &value) != nil {
			return Config{}, ErrInvalidConfig
		}
		config.flags[feature] = value
	}
	pilot, err := strictObject(object["pilot"], "memory", "inference", "autonomousAction", "sensitiveInference")
	if err != nil {
		return Config{}, err
	}
	var memory, inference string
	var autonomous, sensitive bool
	if json.Unmarshal(pilot["memory"], &memory) != nil || json.Unmarshal(pilot["inference"], &inference) != nil ||
		json.Unmarshal(pilot["autonomousAction"], &autonomous) != nil || json.Unmarshal(pilot["sensitiveInference"], &sensitive) != nil {
		return Config{}, ErrInvalidConfig
	}
	if memory != "basic" || inference != "conservative" || autonomous || sensitive {
		return Config{}, ErrUnsafeConfig
	}
	if err = config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func LoadConfig(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		return Config{}, ErrInvalidConfig
	}
	value, exists := lookup(EnvironmentKey)
	if !exists {
		return DefaultConfig(), nil
	}
	return ParseConfig([]byte(value))
}
