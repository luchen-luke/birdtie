package agentmessagepolicy

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

func DecodePut(raw []byte) (PutInput, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes || !utf8.Valid(raw) {
		return PutInput{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return PutInput{}, ErrInvalid
	}
	m := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		s, ok := k.(string)
		if e != nil || !ok {
			return PutInput{}, ErrInvalid
		}
		if _, dup := m[s]; dup {
			return PutInput{}, ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(v, []byte("null")) {
			return PutInput{}, ErrInvalid
		}
		m[s] = v
	}
	if _, e = d.Token(); e != nil {
		return PutInput{}, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return PutInput{}, ErrInvalid
	}
	if len(m) != 3 {
		return PutInput{}, ErrInvalid
	}
	for _, k := range []string{"expectedVersion", "incomingRequests", "expiresAt"} {
		if _, ok := m[k]; !ok {
			return PutInput{}, ErrInvalid
		}
	}
	var in PutInput
	b, _ := json.Marshal(m)
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil || !ValidInput(in) {
		return PutInput{}, ErrInvalid
	}
	in.ExpiresAt = in.ExpiresAt.UTC()
	return in, nil
}
