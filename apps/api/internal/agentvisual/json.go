package agentvisual

import (
	"bytes"
	"encoding/json"
	"io"
)

func strictObject(raw []byte, required, optional []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, ErrInvalid
	}
	out := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !allowed[k] {
			return nil, ErrInvalid
		}
		if _, exists := out[k]; exists {
			return nil, ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, ErrInvalid
		}
		out[k] = v
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return nil, ErrInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	for _, k := range required {
		if _, ok := out[k]; !ok {
			return nil, ErrInvalid
		}
	}
	return out, nil
}
func DecodePreview(raw []byte) (in PreviewInput, e error) {
	o, e := strictObject(raw, []string{"operationId", "clientDraftId", "clientDraftRevision", "sourceContextType", "sourceContextId", "userText", "purpose", "image"}, []string{"taskId", "expectedTaskUpdatedAt"})
	if e != nil {
		return in, e
	}
	if _, e = strictObject(o["image"], []string{"mimeType", "contentBase64", "inputSha256", "pixelRisk"}, nil); e != nil {
		return in, e
	}
	if json.Unmarshal(raw, &in) != nil {
		return in, ErrInvalid
	}
	return in, ValidateInput(in)
}
func DecodeApproval(raw []byte) (in ApproveInput, e error) {
	if _, e = strictObject(raw, []string{"operationId", "expectedVersion", "payloadDigest", "pixelSha256", "purpose", "confirmed"}, nil); e != nil {
		return in, e
	}
	if json.Unmarshal(raw, &in) != nil {
		return in, ErrInvalid
	}
	return in, ValidateApproval(in)
}
