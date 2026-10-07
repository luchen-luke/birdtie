package agentnotification

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"time"
	"unicode/utf8"
)

// Parse only the bounded request shape before typed decoding. This prevents
// duplicates, case-insensitive Go field aliases, null values and nested facts
// from being erased by encoding/json before validation.
func policyObject(decoder *json.Decoder, rule bool) (map[string]json.RawMessage, error) {
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrInvalid
	}
	allowed := map[string]bool{"expectedVersion": true, "enabled": true, "defaultRoute": true, "rules": true, "pauseUntil": true, "expiresAt": true}
	if rule {
		allowed = map[string]bool{"category": true, "route": true}
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] {
			return nil, ErrInvalid
		}
		if _, exists := fields[key]; exists {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInvalid
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, ErrInvalid
	}
	return fields, nil
}

func strictJSONString(raw json.RawMessage) (string, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrInvalid
	}
	// This contract contains only ASCII enum tokens and RFC3339 timestamps;
	// replacement characters from malformed UTF-16 escapes have no valid use.
	for _, r := range value {
		if r < 0x20 || r > 0x7e {
			return "", ErrInvalid
		}
	}
	return value, nil
}

// DecodeRules also validates persisted JSON rules without silently dropping
// unknown keys. It is shape validation, not a policy/source authorization.
func DecodeRules(raw []byte) ([]Rule, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	array := json.NewDecoder(bytes.NewReader(raw))
	start, err := array.Token()
	if err != nil || start != json.Delim('[') {
		return nil, ErrInvalid
	}
	rules := make([]Rule, 0)
	for array.More() {
		if len(rules) >= MaxRules {
			return nil, ErrInvalid
		}
		object, err := policyObject(array, true)
		if err != nil || len(object) != 2 {
			return nil, ErrInvalid
		}
		category, err := strictJSONString(object["category"])
		if err != nil {
			return nil, ErrInvalid
		}
		route, err := strictJSONString(object["route"])
		if err != nil {
			return nil, ErrInvalid
		}
		rules = append(rules, Rule{Category: Category(category), Route: Route(route)})
	}
	end, err := array.Token()
	if err != nil || end != json.Delim(']') {
		return nil, ErrInvalid
	}
	if _, err := array.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	return NormalizeRules(rules)
}

func DecodePutInput(raw []byte) (PutInput, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes || !utf8.Valid(raw) {
		return PutInput{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	fields, err := policyObject(decoder, false)
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	if _, err := decoder.Token(); err != io.EOF {
		return PutInput{}, ErrInvalid
	}
	for _, key := range []string{"expectedVersion", "enabled", "defaultRoute", "rules", "expiresAt"} {
		if _, ok := fields[key]; !ok {
			return PutInput{}, ErrInvalid
		}
	}
	versionToken := string(bytes.TrimSpace(fields["expectedVersion"]))
	for _, r := range versionToken {
		if r < '0' || r > '9' {
			return PutInput{}, ErrInvalid
		}
	}
	version, err := strconv.ParseUint(versionToken, 10, 64)
	if err != nil || version > MaxVersion {
		return PutInput{}, ErrInvalid
	}
	var enabled bool
	if json.Unmarshal(fields["enabled"], &enabled) != nil {
		return PutInput{}, ErrInvalid
	}
	route, err := strictJSONString(fields["defaultRoute"])
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	if _, err := RoutePriority(Route(route)); err != nil {
		return PutInput{}, ErrInvalid
	}
	rules, err := DecodeRules(fields["rules"])
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	expiresText, err := strictJSONString(fields["expiresAt"])
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresText)
	if err != nil || !validTime(expires) {
		return PutInput{}, ErrInvalid
	}
	input := PutInput{ExpectedVersion: version, Enabled: enabled, DefaultRoute: Route(route), Rules: rules, ExpiresAt: expires.UTC()}
	if rawPause, exists := fields["pauseUntil"]; exists {
		pauseText, err := strictJSONString(rawPause)
		if err != nil {
			return PutInput{}, ErrInvalid
		}
		pause, err := time.Parse(time.RFC3339Nano, pauseText)
		if err != nil || !validTime(pause) {
			return PutInput{}, ErrInvalid
		}
		pause = pause.UTC()
		input.PauseUntil = &pause
	}
	return input, nil // Future/30-day checks use the real store's fresh PG clock.
}

func (input *PutInput) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodePutInput(raw)
	if err != nil {
		*input = PutInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}
