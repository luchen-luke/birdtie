package agentoutbox

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Only metadata projection can cross JSON. All keys, including nested typed
// references, are exact and closed. Decode never supplies current authority.
func strictShape(data []byte, allowed map[string][]string, maximum int) error {
	if len(data) == 0 || len(data) > maximum || !utf8.Valid(data) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var read func(string, int) error
	read = func(path string, depth int) error {
		if depth > 8 {
			return ErrInvalid
		}
		token, err := d.Token()
		if err != nil {
			return ErrInvalid
		}
		names, object := allowed[path]
		if !object {
			if token == nil && !strings.HasSuffix(path, ".causation_id") {
				return ErrInvalid
			}
			if delim, ok := token.(json.Delim); ok && (delim == '{' || delim == '[') {
				return ErrInvalid
			}
			return nil
		}
		if token != json.Delim('{') {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok || seen[key] {
				return ErrInvalid
			}
			known := false
			for _, name := range names {
				if key == name {
					known = true
					break
				}
			}
			if !known {
				return ErrInvalid
			}
			seen[key] = true
			if err := read(path+"."+key, depth+1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
		for _, name := range names {
			if path == "" && (name == "lease_owner" || name == "lease_until") {
				continue
			}
			if !seen[name] {
				return ErrInvalid
			}
		}
		return nil
	}
	if err := read("", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func envelopeShape(prefix string) map[string][]string {
	return map[string][]string{
		prefix:             {"schema_version", "event_id", "event_type", "tenant", "subject", "actor", "agent_id", "logical_operation_id", "occurred_at", "received_at", "expires_at", "source", "root_trace_id", "causation_id"},
		prefix + ".tenant": {"type", "id"}, prefix + ".subject": {"type", "id"}, prefix + ".actor": {"type", "id"},
		prefix + ".source": {"type", "id", "owner", "revision", "status", "fingerprint"}, prefix + ".source.owner": {"type", "id"},
	}
}

func decodeStrict(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func DecodeEnvelope(data []byte, now time.Time) (Envelope, error) {
	if err := strictShape(data, envelopeShape(""), MaxEnvelopeBytes); err != nil {
		return Envelope{}, err
	}
	type plain Envelope
	var raw plain
	if err := decodeStrict(data, &raw); err != nil {
		return Envelope{}, err
	}
	e := Envelope(raw)
	if err := ValidateEnvelope(e, now); err != nil {
		return Envelope{}, err
	}
	return cloneEnvelope(e), nil
}

func (e *Envelope) UnmarshalJSON(data []byte) error {
	if e == nil {
		return ErrInvalid
	}
	*e = Envelope{}
	var header struct {
		ReceivedAt time.Time `json:"received_at"`
	}
	if len(data) > MaxEnvelopeBytes || json.Unmarshal(data, &header) != nil {
		return ErrInvalid
	}
	decoded, err := DecodeEnvelope(data, header.ReceivedAt)
	if err != nil {
		return err
	}
	*e = decoded
	return nil
}

func DecodeRecord(data []byte) (Record, error) {
	shape := envelopeShape(".event")
	shape[""] = []string{"event", "state", "attempt", "fence", "lease_owner", "lease_until", "next_attempt_at", "created_at", "updated_at"}
	if err := strictShape(data, shape, MaxRecordBytes); err != nil {
		return Record{}, err
	}
	type plain Record
	var raw plain
	if err := decodeStrict(data, &raw); err != nil {
		return Record{}, err
	}
	r := Record(raw)
	if err := ValidateRecord(r); err != nil {
		return Record{}, err
	}
	return r, nil
}

func (r *Record) UnmarshalJSON(data []byte) error {
	if r == nil {
		return ErrInvalid
	}
	*r = Record{}
	decoded, err := DecodeRecord(data)
	if err != nil {
		return err
	}
	*r = decoded
	return nil
}
