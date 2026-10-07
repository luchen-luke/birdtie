package agentpolicysettings

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// The entire settings vocabulary is closed machine enums/timestamps. No free
// user text, native authority, owner, Agent or source facts can enter this DTO.
func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, ErrInvalid
		}
		if _, exists := out[key]; exists {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, ErrInvalid
		}
		out[key] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	return out, nil
}
func hasKeys(object map[string]json.RawMessage, required, optional []string) bool {
	for _, key := range required {
		if object[key] == nil {
			return false
		}
	}
	for key := range object {
		known := false
		for _, r := range required {
			known = known || key == r
		}
		for _, o := range optional {
			known = known || key == o
		}
		if !known {
			return false
		}
	}
	return true
}
func strictArray(raw []byte) ([]map[string]json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil, ErrInvalid
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil || len(items) > 32 {
		return nil, ErrInvalid
	}
	out := []map[string]json.RawMessage{}
	for _, item := range items {
		o, err := strictObject(item)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}
func DecodePutInput(f Family, raw []byte) (PutInput, error) {
	o, err := strictObject(raw)
	if err != nil || !hasKeys(o, []string{"expectedVersion", "settings", "expiresAt"}, nil) {
		return PutInput{}, ErrInvalid
	}
	var in PutInput
	if json.Unmarshal(o["expectedVersion"], &in.ExpectedVersion) != nil || in.ExpectedVersion < 0 || in.ExpectedVersion == MaxRevision || json.Unmarshal(o["expiresAt"], &in.ExpiresAt) != nil || !ValidTime(in.ExpiresAt) {
		return PutInput{}, ErrInvalid
	}
	in.Settings = append(json.RawMessage(nil), o["settings"]...)
	// Real expiry checks use the current PG clock in Store. A fixed shape-only
	// anchor here cannot grant current validity or extend stored expiration.
	anchor := in.ExpiresAt.UTC().Add(-MaxValidity)
	if !ValidTime(anchor) {
		return PutInput{}, ErrInvalid
	}
	if _, err = NormalizeSettings(f, in.Settings, anchor, in.ExpiresAt); err != nil {
		return PutInput{}, ErrInvalid
	}
	return in, nil
}
