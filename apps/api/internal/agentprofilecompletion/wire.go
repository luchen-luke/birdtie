package agentprofilecompletion

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

// Closed input parser rejects duplicate, unknown and null fields. None of the
// current native authority, source body or after value can be supplied here.
func closedObject(raw []byte, required map[string]bool) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes {
		return nil, agentprofile.ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, agentprofile.ErrInvalid
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || !required[k] || out[k] != nil {
			return nil, agentprofile.ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, agentprofile.ErrInvalid
		}
		out[k] = v
	}
	t, e = d.Token()
	if e != nil || t != json.Delim('}') || len(out) != len(required) {
		return nil, agentprofile.ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, agentprofile.ErrInvalid
	}
	return out, nil
}
func DecodePreviewInput(raw []byte) (PreviewInput, error) {
	m, e := closedObject(raw, map[string]bool{"previewId": true, "memoryId": true, "memoryVersion": true, "expectedProfileVersion": true})
	if e != nil {
		return PreviewInput{}, e
	}
	var v PreviewInput
	if json.Unmarshal(m["previewId"], &v.PreviewID) != nil || json.Unmarshal(m["memoryId"], &v.MemoryID) != nil || json.Unmarshal(m["memoryVersion"], &v.MemoryVersion) != nil || json.Unmarshal(m["expectedProfileVersion"], &v.ExpectedProfileVersion) != nil {
		return PreviewInput{}, agentprofile.ErrInvalid
	}
	return NormalizePreviewInput(v)
}
func DecodeAcceptInput(raw []byte) (AcceptInput, error) {
	m, e := closedObject(raw, map[string]bool{"planDigest": true})
	if e != nil {
		return AcceptInput{}, e
	}
	var v AcceptInput
	if json.Unmarshal(m["planDigest"], &v.PlanDigest) != nil {
		return AcceptInput{}, agentprofile.ErrInvalid
	}
	return NormalizeAcceptInput(v)
}
