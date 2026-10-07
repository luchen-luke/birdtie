package modelconfiguration

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// DecodeConfiguration is a closed, bounded metadata parser. It cannot decode a
// source/model grant. Registered prompt/policy existence is checked separately.
func DecodeConfiguration(raw []byte) (Configuration, error) {
	if len(raw) == 0 || len(raw) > MaxConfigurationBytes || !utf8.Valid(raw) {
		return Configuration{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return Configuration{}, ErrInvalid
	}
	allowed := map[string]bool{"schema_version": true, "version": true, "task_kind": true, "prompt_version": true, "input_schema_version": true, "output_schema_version": true, "output_mode": true, "tool_allowlist": true, "policy_version": true, "capabilities_required": true}
	values := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || !allowed[name] || values[name] != nil {
			return Configuration{}, ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return Configuration{}, ErrInvalid
		}
		values[name] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(values) != len(allowed) {
		return Configuration{}, ErrInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return Configuration{}, ErrInvalid
	}
	type wire Configuration
	var out wire
	if json.Unmarshal(raw, &out) != nil {
		return Configuration{}, ErrInvalid
	}
	return NormalizeConfiguration(Configuration(out))
}

// Standard JSON decoding uses the same closed parser so callers cannot erase
// unknown, duplicate or alias fields before registry validation sees them.
func (c *Configuration) UnmarshalJSON(raw []byte) error {
	if c == nil {
		return ErrInvalid
	}
	parsed, err := DecodeConfiguration(raw)
	if err != nil {
		*c = Configuration{}
		return err
	}
	*c = parsed
	return nil
}
