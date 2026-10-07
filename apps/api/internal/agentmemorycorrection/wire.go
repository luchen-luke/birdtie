package agentmemorycorrection

import (
	"bytes"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"io"
)

// Duplicate keys are rejected before JSON loses them, including nested input.
func closed(raw []byte, v any) error {
	if len(raw) == 0 || len(raw) > MaxBodyBytes {
		return agentmemory.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 10 {
			return agentmemory.ErrInvalid
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		x, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch x {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return agentmemory.ErrInvalid
				}
				seen[s] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		default:
			return agentmemory.ErrInvalid
		}
		_, e = d.Token()
		return e
	}
	if walk(0) != nil {
		return agentmemory.ErrInvalid
	}
	if _, e := d.Token(); e != io.EOF {
		return agentmemory.ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return agentmemory.ErrInvalid
	}
	// Encoding/json field matching is case insensitive: require exact canonical keys.
	b, e := json.Marshal(v)
	if e != nil {
		return agentmemory.ErrInvalid
	}
	var a, c map[string]json.RawMessage
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(b, &c) != nil {
		return agentmemory.ErrInvalid
	}
	for k := range a {
		if _, ok := c[k]; !ok {
			return agentmemory.ErrInvalid
		}
	}
	return nil
}
func DecodeInput(raw []byte) (Input, error) {
	var v Input
	if e := closed(raw, &v); e != nil {
		return Input{}, e
	}
	return NormalizeInput(v)
}
func DecodeConfirm(raw []byte) (ConfirmInput, error) {
	var v ConfirmInput
	if e := closed(raw, &v); e != nil || !ValidDigest(v.PlanDigest) {
		return ConfirmInput{}, agentmemory.ErrInvalid
	}
	return v, nil
}
