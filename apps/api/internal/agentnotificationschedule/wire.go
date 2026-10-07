package agentnotificationschedule

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// Exact keys, duplicates (including escaped aliases), nulls and nested shape
// are checked before encoding/json can erase facts. Quiet=null is explicit off.
func object(d *json.Decoder, allowed map[string]bool, quiet bool) (map[string]json.RawMessage, error) {
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return nil, ErrInvalid
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		s, ok := k.(string)
		if e != nil || !ok || !allowed[s] {
			return nil, ErrInvalid
		}
		if _, ok = m[s]; ok {
			return nil, ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || (!quiet || s != "quiet") && bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, ErrInvalid
		}
		m[s] = v
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') {
		return nil, ErrInvalid
	}
	return m, nil
}
func DecodePut(data []byte) (PutInput, error) {
	if len(data) == 0 || len(data) > MaxBodyBytes || !utf8.Valid(data) {
		return PutInput{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	allowed := map[string]bool{"expectedVersion": true, "enabled": true, "timeZone": true, "localMinute": true, "gapPolicy": true, "foldPolicy": true, "quiet": true, "maxContactsPerDay": true, "categories": true, "expiresAt": true}
	m, e := object(d, allowed, true)
	if e != nil || len(m) != len(allowed) {
		return PutInput{}, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return PutInput{}, ErrInvalid
	}
	if !bytes.Equal(bytes.TrimSpace(m["quiet"]), []byte("null")) {
		q := json.NewDecoder(bytes.NewReader(m["quiet"]))
		shape, e := object(q, map[string]bool{"startMinute": true, "endMinute": true}, false)
		if e != nil || len(shape) != 2 {
			return PutInput{}, ErrInvalid
		}
		if _, e = q.Token(); e != io.EOF {
			return PutInput{}, ErrInvalid
		}
	}
	var in PutInput
	if json.Unmarshal(data, &in) != nil {
		return PutInput{}, ErrInvalid
	}
	if _, e = NormalizeSettings(in.Settings); e != nil {
		return PutInput{}, e
	}
	return in, nil
}
func DecodeSettings(data []byte) (Settings, error) {
	if len(data) == 0 || len(data) > MaxBodyBytes || !utf8.Valid(data) {
		return Settings{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	allowed := map[string]bool{"enabled": true, "timeZone": true, "localMinute": true, "gapPolicy": true, "foldPolicy": true, "quiet": true, "maxContactsPerDay": true, "categories": true}
	m, e := object(d, allowed, true)
	if e != nil || len(m) != 8 {
		return Settings{}, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return Settings{}, ErrInvalid
	}
	if !bytes.Equal(bytes.TrimSpace(m["quiet"]), []byte("null")) {
		q := json.NewDecoder(bytes.NewReader(m["quiet"]))
		shape, e := object(q, map[string]bool{"startMinute": true, "endMinute": true}, false)
		if e != nil || len(shape) != 2 {
			return Settings{}, ErrInvalid
		}
	}
	var s Settings
	if json.Unmarshal(data, &s) != nil {
		return Settings{}, ErrInvalid
	}
	return NormalizeSettings(s)
}
