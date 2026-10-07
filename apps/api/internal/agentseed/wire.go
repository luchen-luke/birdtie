package agentseed

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"unicode/utf8"
)

// JSON permits escaped UTF-16 pairs; encoding/json replaces an unpaired
// surrogate with RuneError. Reject the malformed wire before that replacement.
func validUnicodeEscapes(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

// All objects reject duplicate keys before typed decoding. None of the wire
// fields establishes authority; actual current session and source are Store work.
func strictValue(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return ErrInvalid
	}
	if t == nil {
		return ErrInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			t, e = d.Token()
			if e != nil {
				return ErrInvalid
			}
			k, ok := t.(string)
			if !ok || seen[k] {
				return ErrInvalid
			}
			seen[k] = true
			if e = strictValue(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = strictValue(d); e != nil {
				return e
			}
		}
	default:
		return ErrInvalid
	}
	end, e := d.Token()
	if e != nil || delim == '{' && end != json.Delim('}') || delim == '[' && end != json.Delim(']') {
		return ErrInvalid
	}
	return nil
}
func Decode(raw []byte) (Input, error) {
	if len(raw) == 0 || len(raw) > MaxBody || !utf8.Valid(raw) || !validUnicodeEscapes(raw) {
		return Input{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if strictValue(d) != nil {
		return Input{}, ErrInvalid
	}
	if _, e := d.Token(); e != io.EOF {
		return Input{}, ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var v Input
	if d.Decode(&v) != nil {
		return Input{}, ErrInvalid
	}
	// encoding/json accepts case-insensitive keys, which this contract rejects.
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return Input{}, ErrInvalid
	}
	for k := range keys {
		switch k {
		case "expectedSnapshot", "action", "displayName", "currentCityId", "currentCitySnapshot", "languagePreferences", "basicIntent", "interestChoice", "interests":
		default:
			return Input{}, ErrInvalid
		}
	}
	if keys["expectedSnapshot"] == nil || keys["action"] == nil {
		return Input{}, ErrInvalid
	}
	return Normalize(v)
}
