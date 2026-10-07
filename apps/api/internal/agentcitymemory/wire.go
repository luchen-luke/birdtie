package agentcitymemory

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

// All declaration fields are flat scalar values; exact keys and complete shape
// prevent clients from smuggling owner, Agent, dates, inference or approvals.
func flatObject(raw []byte, names []string) error {
	if len(raw) == 0 || len(raw) > 2048 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return ErrInvalid
	}
	allowed := map[string]bool{}
	for _, n := range names {
		allowed[n] = true
	}
	seen := map[string]bool{}
	for d.More() {
		v, e := d.Token()
		n, ok := v.(string)
		if e != nil || !ok || !allowed[n] || seen[n] {
			return ErrInvalid
		}
		seen[n] = true
		v, e = d.Token()
		if e != nil || v == nil {
			return ErrInvalid
		}
		if _, nested := v.(json.Delim); nested {
			return ErrInvalid
		}
	}
	last, e := d.Token()
	if e != nil || last != json.Delim('}') || len(seen) != len(names) {
		return ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrInvalid
	}
	return nil
}
func DecodePut(raw []byte, now time.Time) (PutDeclarationInput, error) {
	if flatObject(raw, []string{"expectedVersion", "cityId", "kind", "visibility", "validUntil"}) != nil {
		return PutDeclarationInput{}, ErrInvalid
	}
	var in PutDeclarationInput
	if json.Unmarshal(raw, &in) != nil {
		return PutDeclarationInput{}, ErrInvalid
	}
	return NormalizePut(in, now)
}
